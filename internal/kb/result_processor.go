package kb

import (
	"regexp"
	"sort"
	"strings"
)

// ResultProcessor post-processes search results for cleaner output.
type ResultProcessor struct {
	boilerplatePatterns []*regexp.Regexp
}

// NewResultProcessor creates a new result processor.
func NewResultProcessor() *ResultProcessor {
	return &ResultProcessor{
		boilerplatePatterns: compileBoilerplatePatterns(),
	}
}

// ProcessedResult contains a processed search result with merged chunks.
type ProcessedResult struct {
	DocumentID string            `json:"document_id"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Score      float64           `json:"score"`
	ChunkCount int               `json:"chunk_count"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Source     SourceInfo        `json:"source"`
}

// SourceInfo provides citation-ready source information.
type SourceInfo struct {
	File    string `json:"file"`
	Page    int    `json:"page,omitempty"`
	Section string `json:"section,omitempty"`
}

// ProcessResults processes raw search hits into cleaner, merged results.
func (p *ResultProcessor) ProcessResults(hits []SearchHit) []ProcessedResult {
	if len(hits) == 0 {
		return nil
	}

	// Group hits by document
	byDoc := make(map[string][]SearchHit)
	docOrder := make([]string, 0)

	for _, hit := range hits {
		if _, exists := byDoc[hit.DocumentID]; !exists {
			docOrder = append(docOrder, hit.DocumentID)
		}
		byDoc[hit.DocumentID] = append(byDoc[hit.DocumentID], hit)
	}

	var results []ProcessedResult
	for _, docID := range docOrder {
		docHits := byDoc[docID]
		if len(docHits) == 0 {
			continue
		}

		// Merge chunks from same document
		merged := p.mergeChunks(docHits)

		// Filter boilerplate
		cleaned := p.filterBoilerplate(merged)

		// Calculate average score
		var totalScore float64
		for _, h := range docHits {
			totalScore += h.Score
		}
		avgScore := totalScore / float64(len(docHits))

		result := ProcessedResult{
			DocumentID: docID,
			Path:       docHits[0].Path,
			Title:      docHits[0].Title,
			Content:    cleaned,
			Score:      avgScore,
			ChunkCount: len(docHits),
			Metadata:   docHits[0].Metadata,
			Source: SourceInfo{
				File: extractFilename(docHits[0].Path),
			},
		}

		// Extract page number if available in metadata
		if page, ok := docHits[0].Metadata["page"]; ok {
			if p, err := parseInt(page); err == nil {
				result.Source.Page = p
			}
		}

		results = append(results, result)
	}

	// Sort by score (best first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score < results[j].Score // BM25 scores are negative, lower is better
	})

	return results
}

// mergeChunks combines adjacent or overlapping chunks from the same document.
func (p *ResultProcessor) mergeChunks(hits []SearchHit) string {
	if len(hits) == 0 {
		return ""
	}
	if len(hits) == 1 {
		return hits[0].Snippet
	}

	// Sort by chunk position (if available in metadata)
	sortedHits := make([]SearchHit, len(hits))
	copy(sortedHits, hits)
	sort.Slice(sortedHits, func(i, j int) bool {
		posI := getChunkPosition(sortedHits[i])
		posJ := getChunkPosition(sortedHits[j])
		return posI < posJ
	})

	// Merge with overlap detection
	var parts []string
	for _, hit := range sortedHits {
		parts = append(parts, hit.Snippet)
	}

	return removeOverlaps(parts)
}

// removeOverlaps merges text parts by detecting and removing overlapping content.
func removeOverlaps(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	result := strings.TrimSpace(parts[0])
	for i := 1; i < len(parts); i++ {
		next := strings.TrimSpace(parts[i])
		if next == "" {
			continue
		}

		// Find overlap between end of result and start of next part
		overlapLen := 0
		maxOverlap := min(len(result), len(next), 150) // Check up to 150 chars

		for j := 10; j <= maxOverlap; j++ { // Start at 10 chars to avoid false positives
			if strings.HasSuffix(result, next[:j]) {
				overlapLen = j
			}
		}

		if overlapLen > 0 {
			result += " " + next[overlapLen:]
		} else {
			// No overlap detected, add separator
			result += "\n\n" + next
		}
	}

	return result
}

// filterBoilerplate removes common boilerplate patterns from content.
func (p *ResultProcessor) filterBoilerplate(content string) string {
	result := content

	for _, pattern := range p.boilerplatePatterns {
		result = pattern.ReplaceAllString(result, " ")
	}

	// Clean up excessive whitespace
	result = cleanWhitespace(result)

	return result
}

// compileBoilerplatePatterns returns compiled regex patterns for boilerplate detection.
func compileBoilerplatePatterns() []*regexp.Regexp {
	patterns := []string{
		// Page numbers and navigation
		`(?i)^\s*page\s+\d+\s*(of\s+\d+)?\s*$`,
		`(?m)^\s*\d+\s*$`, // Standalone page numbers
		`(?i)^\s*table of contents\s*$`,

		// PDF artifacts
		`(?i)this content downloaded from[\s\S]*?terms and conditions`,
		`(?i)all rights reserved`,
		`(?i)copyright\s*©?\s*\d{4}`,
		`(?i)^\s*\[?\d+\]?\s*$`, // Footnote numbers

		// JSTOR/Academic boilerplate
		`(?i)all use subject to.*?terms`,
		`(?i)jstor.*?conditions`,

		// Headers/footers patterns
		`(?m)^[-_=]{10,}\s*$`, // Separator lines

		// Hyphenation artifacts from PDF
		`(\w)-\s*\n\s*(\w)`, // Rejoin hyphenated words
	}

	var compiled []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// cleanWhitespace normalizes whitespace in content.
func cleanWhitespace(s string) string {
	// Replace multiple spaces with single space
	spaceRe := regexp.MustCompile(`[ \t]+`)
	s = spaceRe.ReplaceAllString(s, " ")

	// Replace multiple newlines with double newline
	nlRe := regexp.MustCompile(`\n{3,}`)
	s = nlRe.ReplaceAllString(s, "\n\n")

	return strings.TrimSpace(s)
}

// getChunkPosition extracts chunk index from metadata.
func getChunkPosition(hit SearchHit) int {
	if hit.Metadata == nil {
		return 0
	}
	if idx, ok := hit.Metadata["chunk_index"]; ok {
		if pos, err := parseInt(idx); err == nil {
			return pos
		}
	}
	return 0
}

// extractFilename extracts just the filename from a path.
func extractFilename(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return path
}

// parseInt is a helper to parse int from string.
func parseInt(s string) (int, error) {
	var i int
	_, err := strings.NewReader(s).Read([]byte{byte(i)})
	if err != nil {
		return 0, err
	}
	// Simple parsing
	var result int
	for _, c := range s {
		if c >= '0' && c <= '9' {
			result = result*10 + int(c-'0')
		} else {
			break
		}
	}
	return result, nil
}

// FormatForPrompt formats processed results in a prompt-ready format.
func (p *ResultProcessor) FormatForPrompt(results []ProcessedResult) string {
	if len(results) == 0 {
		return "No relevant documents found."
	}

	var sb strings.Builder
	sb.WriteString("## Relevant Context\n\n")

	for i, r := range results {
		sb.WriteString("### ")
		sb.WriteString(r.Title)
		sb.WriteString("\n")
		sb.WriteString("*Source: ")
		sb.WriteString(r.Source.File)
		if r.Source.Page > 0 {
			sb.WriteString(" (page ")
			sb.WriteString(strings.Trim(strings.Replace(strings.Trim(strings.Replace(strings.Trim(strings.Replace(string(rune(r.Source.Page+'0')), "\n", "", -1), " "), "\t", "", -1), " "), "\r", "", -1), " "))
			sb.WriteString(")")
		}
		sb.WriteString("*\n\n")
		sb.WriteString(r.Content)
		sb.WriteString("\n")

		if i < len(results)-1 {
			sb.WriteString("\n---\n\n")
		}
	}

	return sb.String()
}
