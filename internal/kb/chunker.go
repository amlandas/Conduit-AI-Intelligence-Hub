package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Chunker splits document content into searchable chunks.
type Chunker struct {
	defaultMaxSize int
	defaultOverlap int
}

// NewChunker creates a new chunker with default settings.
func NewChunker() *Chunker {
	return &Chunker{
		defaultMaxSize: 1000,
		defaultOverlap: 100,
	}
}

// ContentType determines how content should be chunked.
type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeCode     ContentType = "code"
	ContentTypeMarkdown ContentType = "markdown"
	ContentTypePDF      ContentType = "pdf"
)

// DetectContentType determines the content type from file extension.
func DetectContentType(path string) ContentType {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".py", ".js", ".ts", ".java", ".rs", ".rb", ".c", ".cpp", ".h", ".hpp",
		".cs", ".swift", ".kt", ".scala", ".php", ".sh", ".bash", ".zsh":
		return ContentTypeCode
	case ".md", ".markdown", ".rst":
		return ContentTypeMarkdown
	case ".pdf":
		return ContentTypePDF
	default:
		return ContentTypeText
	}
}

// Chunk splits content into overlapping chunks.
func (c *Chunker) Chunk(content string, opts ChunkOptions) []Chunk {
	if opts.MaxSize <= 0 {
		opts.MaxSize = c.defaultMaxSize
	}
	if opts.Overlap <= 0 {
		opts.Overlap = c.defaultOverlap
	}
	if len(opts.Splitters) == 0 {
		opts.Splitters = []string{"\n\n", "\n", ". ", " "}
	}

	// Normalize content
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)

	// Handle empty content
	if len(content) == 0 {
		return []Chunk{}
	}

	if utf8.RuneCountInString(content) <= opts.MaxSize {
		return []Chunk{{
			ChunkID:   c.chunkID(content, 0),
			Index:     0,
			Content:   content,
			StartChar: 0,
			EndChar:   len(content),
		}}
	}

	return c.splitRecursive(content, opts)
}

// splitRecursive splits content using priority-ordered splitters.
func (c *Chunker) splitRecursive(content string, opts ChunkOptions) []Chunk {
	var chunks []Chunk
	var currentPos int
	index := 0

	for currentPos < len(content) {
		// Calculate chunk boundaries
		endPos := currentPos + opts.MaxSize
		if endPos > len(content) {
			endPos = len(content)
		}

		// Find best split point
		chunkText := content[currentPos:endPos]
		splitPoint := c.findBestSplit(chunkText, opts.Splitters, opts.MaxSize)

		if splitPoint < len(chunkText) {
			chunkText = chunkText[:splitPoint]
		}

		// Trim whitespace but preserve for position tracking
		trimmedText := strings.TrimSpace(chunkText)
		if len(trimmedText) > 0 {
			chunks = append(chunks, Chunk{
				ChunkID:   c.chunkID(trimmedText, index),
				Index:     index,
				Content:   trimmedText,
				StartChar: currentPos,
				EndChar:   currentPos + len(chunkText),
			})
			index++
		}

		// Move position with overlap
		advance := len(chunkText) - opts.Overlap
		if advance <= 0 {
			advance = len(chunkText)
		}
		currentPos += advance

		// Prevent infinite loop
		if len(chunkText) == 0 {
			currentPos++
		}
	}

	return chunks
}

// findBestSplit finds the best split point using priority-ordered splitters.
func (c *Chunker) findBestSplit(text string, splitters []string, maxSize int) int {
	// If text is short enough, no split needed
	if utf8.RuneCountInString(text) <= maxSize {
		return len(text)
	}

	// Try each splitter in priority order
	for _, splitter := range splitters {
		// Find the last occurrence of the splitter within maxSize
		lastIdx := -1
		searchText := text
		if len(searchText) > maxSize {
			searchText = text[:maxSize]
		}

		// Search from the end backwards
		idx := strings.LastIndex(searchText, splitter)
		if idx > 0 {
			lastIdx = idx + len(splitter)
		}

		if lastIdx > 0 && lastIdx <= maxSize {
			return lastIdx
		}
	}

	// No good split point found, split at maxSize
	// But try to avoid splitting in the middle of a word
	if maxSize < len(text) {
		for i := maxSize; i > maxSize/2; i-- {
			if text[i] == ' ' {
				return i + 1
			}
		}
	}

	return maxSize
}

// chunkID generates a unique ID for a chunk.
func (c *Chunker) chunkID(content string, index int) string {
	h := sha256.Sum256([]byte(content))
	return "chunk_" + hex.EncodeToString(h[:8])
}

// ChunkWithMetadata chunks content and adds document metadata to each chunk.
func (c *Chunker) ChunkWithMetadata(content string, opts ChunkOptions, docMeta map[string]string) []Chunk {
	chunks := c.Chunk(content, opts)
	for i := range chunks {
		if chunks[i].Metadata == nil {
			chunks[i].Metadata = make(map[string]string)
		}
		for k, v := range docMeta {
			chunks[i].Metadata[k] = v
		}
	}
	return chunks
}

// EstimateChunkCount estimates the number of chunks for content.
func (c *Chunker) EstimateChunkCount(contentLength int, opts ChunkOptions) int {
	if opts.MaxSize <= 0 {
		opts.MaxSize = c.defaultMaxSize
	}
	if opts.Overlap <= 0 {
		opts.Overlap = c.defaultOverlap
	}

	if contentLength <= opts.MaxSize {
		return 1
	}

	effectiveChunkSize := opts.MaxSize - opts.Overlap
	if effectiveChunkSize <= 0 {
		effectiveChunkSize = opts.MaxSize
	}

	return (contentLength-opts.Overlap)/effectiveChunkSize + 1
}

// ChunkSmart performs content-aware chunking based on file type.
func (c *Chunker) ChunkSmart(content string, path string, opts ChunkOptions) []Chunk {
	contentType := DetectContentType(path)

	switch contentType {
	case ContentTypeCode:
		return c.chunkCode(content, path, opts)
	case ContentTypeMarkdown:
		return c.chunkMarkdown(content, opts)
	case ContentTypePDF:
		return c.chunkPDF(content, opts)
	default:
		return c.chunkSentenceAware(content, opts)
	}
}

// chunkSentenceAware chunks text respecting sentence boundaries.
func (c *Chunker) chunkSentenceAware(content string, opts ChunkOptions) []Chunk {
	if opts.MaxSize <= 0 {
		opts.MaxSize = c.defaultMaxSize
	}
	if opts.Overlap <= 0 {
		opts.Overlap = c.defaultOverlap
	}

	// Normalize content
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)

	if len(content) == 0 {
		return []Chunk{}
	}

	if utf8.RuneCountInString(content) <= opts.MaxSize {
		return []Chunk{{
			ChunkID:   c.chunkID(content, 0),
			Index:     0,
			Content:   content,
			StartChar: 0,
			EndChar:   len(content),
		}}
	}

	// Split into sentences first
	sentences := splitIntoSentences(content)

	var chunks []Chunk
	var currentChunk strings.Builder
	var chunkStart int
	index := 0
	charPos := 0

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// If adding this sentence would exceed max size
		if currentChunk.Len() > 0 && currentChunk.Len()+len(sentence)+1 > opts.MaxSize {
			// Save current chunk
			chunkContent := strings.TrimSpace(currentChunk.String())
			if len(chunkContent) > 0 {
				chunks = append(chunks, Chunk{
					ChunkID:   c.chunkID(chunkContent, index),
					Index:     index,
					Content:   chunkContent,
					StartChar: chunkStart,
					EndChar:   charPos,
				})
				index++
			}

			// Start new chunk with overlap
			currentChunk.Reset()
			overlapText := getOverlapFromEnd(chunkContent, opts.Overlap)
			if overlapText != "" {
				currentChunk.WriteString(overlapText)
				currentChunk.WriteString(" ")
			}
			chunkStart = charPos - len(overlapText)
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString(" ")
		}
		currentChunk.WriteString(sentence)
		charPos += len(sentence) + 1
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunkContent := strings.TrimSpace(currentChunk.String())
		if len(chunkContent) > 0 {
			chunks = append(chunks, Chunk{
				ChunkID:   c.chunkID(chunkContent, index),
				Index:     index,
				Content:   chunkContent,
				StartChar: chunkStart,
				EndChar:   len(content),
			})
		}
	}

	return chunks
}

// splitIntoSentences splits text into sentences.
func splitIntoSentences(text string) []string {
	// First, normalize double newlines to single newlines for paragraph breaks
	text = regexp.MustCompile(`\n{2,}`).ReplaceAllString(text, "\n")

	// Split on sentence-ending punctuation followed by whitespace
	// We'll process this manually since Go doesn't support lookahead
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		current.WriteRune(runes[i])

		// Check for sentence ending: punctuation followed by space and uppercase
		if (runes[i] == '.' || runes[i] == '!' || runes[i] == '?') && i+2 < len(runes) {
			// Look for whitespace after punctuation
			if unicode.IsSpace(runes[i+1]) {
				// Look for uppercase letter or quote that starts next sentence
				nextNonSpace := i + 2
				for nextNonSpace < len(runes) && unicode.IsSpace(runes[nextNonSpace]) {
					nextNonSpace++
				}
				if nextNonSpace < len(runes) {
					nextChar := runes[nextNonSpace]
					if unicode.IsUpper(nextChar) || nextChar == '"' || nextChar == '\'' || nextChar == '(' || nextChar == '[' {
						// This is likely a sentence boundary
						s := strings.TrimSpace(current.String())
						if s != "" {
							sentences = append(sentences, s)
						}
						current.Reset()
						i++ // Skip the whitespace
						continue
					}
				}
			}
		}

		// Also break on newlines
		if runes[i] == '\n' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}

	// Don't forget the last sentence
	s := strings.TrimSpace(current.String())
	if s != "" {
		sentences = append(sentences, s)
	}

	return sentences
}

// getOverlapFromEnd extracts overlap text from the end of content.
func getOverlapFromEnd(content string, overlapSize int) string {
	if len(content) <= overlapSize {
		return content
	}

	// Try to find a sentence boundary in the overlap region
	overlapStart := len(content) - overlapSize
	overlapText := content[overlapStart:]

	// Find the start of the last sentence in the overlap
	sentenceStart := strings.LastIndex(overlapText, ". ")
	if sentenceStart > 0 && sentenceStart < len(overlapText)-10 {
		return strings.TrimSpace(overlapText[sentenceStart+2:])
	}

	return strings.TrimSpace(overlapText)
}

// chunkCode chunks source code respecting function/class boundaries.
func (c *Chunker) chunkCode(content string, path string, opts ChunkOptions) []Chunk {
	if opts.MaxSize <= 0 {
		opts.MaxSize = c.defaultMaxSize
	}
	if opts.Overlap <= 0 {
		opts.Overlap = c.defaultOverlap
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)

	if len(content) == 0 {
		return []Chunk{}
	}

	if utf8.RuneCountInString(content) <= opts.MaxSize {
		return []Chunk{{
			ChunkID:   c.chunkID(content, 0),
			Index:     0,
			Content:   content,
			StartChar: 0,
			EndChar:   len(content),
		}}
	}

	// Detect language-specific boundaries
	ext := strings.ToLower(filepath.Ext(path))
	boundaries := detectCodeBoundaries(content, ext)

	if len(boundaries) == 0 {
		// Fall back to sentence-aware chunking
		return c.chunkSentenceAware(content, opts)
	}

	var chunks []Chunk
	index := 0

	for _, boundary := range boundaries {
		blockContent := strings.TrimSpace(boundary.content)
		if len(blockContent) == 0 {
			continue
		}

		// If block is larger than max size, split it further
		if utf8.RuneCountInString(blockContent) > opts.MaxSize {
			subChunks := c.Chunk(blockContent, opts)
			for _, sub := range subChunks {
				sub.Index = index
				sub.StartChar += boundary.start
				sub.EndChar += boundary.start
				sub.ChunkID = c.chunkID(sub.Content, index)
				if sub.Metadata == nil {
					sub.Metadata = make(map[string]string)
				}
				sub.Metadata["block_type"] = boundary.blockType
				chunks = append(chunks, sub)
				index++
			}
		} else {
			chunk := Chunk{
				ChunkID:   c.chunkID(blockContent, index),
				Index:     index,
				Content:   blockContent,
				StartChar: boundary.start,
				EndChar:   boundary.end,
				Metadata: map[string]string{
					"block_type": boundary.blockType,
				},
			}
			chunks = append(chunks, chunk)
			index++
		}
	}

	return chunks
}

// codeBoundary represents a logical boundary in code.
type codeBoundary struct {
	start     int
	end       int
	content   string
	blockType string // "function", "class", "block"
}

// detectCodeBoundaries finds logical boundaries in source code.
func detectCodeBoundaries(content string, ext string) []codeBoundary {
	var boundaries []codeBoundary
	lines := strings.Split(content, "\n")

	// Patterns for different languages
	var funcPattern, classPattern *regexp.Regexp

	switch ext {
	case ".go":
		funcPattern = regexp.MustCompile(`^func\s+`)
		classPattern = regexp.MustCompile(`^type\s+\w+\s+struct`)
	case ".py":
		funcPattern = regexp.MustCompile(`^def\s+`)
		classPattern = regexp.MustCompile(`^class\s+`)
	case ".js", ".ts":
		funcPattern = regexp.MustCompile(`^(async\s+)?function\s+|^(const|let|var)\s+\w+\s*=\s*(async\s+)?\(|^(const|let|var)\s+\w+\s*=\s*(async\s+)?function`)
		classPattern = regexp.MustCompile(`^class\s+`)
	case ".java", ".kt", ".scala":
		funcPattern = regexp.MustCompile(`^\s*(public|private|protected)?\s*(static)?\s*\w+\s+\w+\s*\(`)
		classPattern = regexp.MustCompile(`^\s*(public|private)?\s*class\s+`)
	case ".rs":
		funcPattern = regexp.MustCompile(`^(pub\s+)?fn\s+`)
		classPattern = regexp.MustCompile(`^(pub\s+)?struct\s+|^(pub\s+)?impl\s+`)
	case ".rb":
		funcPattern = regexp.MustCompile(`^def\s+`)
		classPattern = regexp.MustCompile(`^class\s+`)
	case ".c", ".cpp", ".h", ".hpp":
		funcPattern = regexp.MustCompile(`^\w+[\s\*]+\w+\s*\([^)]*\)\s*\{?$`)
		classPattern = regexp.MustCompile(`^class\s+\w+|^struct\s+\w+`)
	default:
		// Generic: split on blank lines
		return splitOnBlankLines(content)
	}

	var currentBlock strings.Builder
	var blockStart int
	var blockType string
	charPos := 0

	for i, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Check if this line starts a new boundary
		isFunc := funcPattern != nil && funcPattern.MatchString(trimmedLine)
		isClass := classPattern != nil && classPattern.MatchString(trimmedLine)

		if isFunc || isClass {
			// Save previous block if exists
			if currentBlock.Len() > 0 {
				boundaries = append(boundaries, codeBoundary{
					start:     blockStart,
					end:       charPos,
					content:   currentBlock.String(),
					blockType: blockType,
				})
			}

			// Start new block
			currentBlock.Reset()
			blockStart = charPos
			if isClass {
				blockType = "class"
			} else {
				blockType = "function"
			}
		}

		currentBlock.WriteString(line)
		if i < len(lines)-1 {
			currentBlock.WriteString("\n")
		}
		charPos += len(line) + 1
	}

	// Don't forget the last block
	if currentBlock.Len() > 0 {
		boundaries = append(boundaries, codeBoundary{
			start:     blockStart,
			end:       len(content),
			content:   currentBlock.String(),
			blockType: blockType,
		})
	}

	return boundaries
}

// splitOnBlankLines splits content on blank lines for generic code.
func splitOnBlankLines(content string) []codeBoundary {
	var boundaries []codeBoundary
	blocks := regexp.MustCompile(`\n{2,}`).Split(content, -1)

	charPos := 0
	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block != "" {
			boundaries = append(boundaries, codeBoundary{
				start:     charPos,
				end:       charPos + len(block),
				content:   block,
				blockType: "block",
			})
		}
		charPos += len(block) + 2 // account for the blank lines
	}

	return boundaries
}

// chunkMarkdown chunks markdown respecting header boundaries.
func (c *Chunker) chunkMarkdown(content string, opts ChunkOptions) []Chunk {
	if opts.MaxSize <= 0 {
		opts.MaxSize = c.defaultMaxSize
	}
	if opts.Overlap <= 0 {
		opts.Overlap = c.defaultOverlap
	}

	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.TrimSpace(content)

	if len(content) == 0 {
		return []Chunk{}
	}

	if utf8.RuneCountInString(content) <= opts.MaxSize {
		return []Chunk{{
			ChunkID:   c.chunkID(content, 0),
			Index:     0,
			Content:   content,
			StartChar: 0,
			EndChar:   len(content),
		}}
	}

	// Split on markdown headers
	headerPattern := regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)
	matches := headerPattern.FindAllStringSubmatchIndex(content, -1)

	if len(matches) == 0 {
		return c.chunkSentenceAware(content, opts)
	}

	var chunks []Chunk
	index := 0
	prevEnd := 0

	for i, match := range matches {
		// Content before this header (or between headers)
		if match[0] > prevEnd {
			beforeContent := strings.TrimSpace(content[prevEnd:match[0]])
			if len(beforeContent) > 0 {
				subChunks := c.createChunksFromSection(beforeContent, prevEnd, opts, index)
				for _, ch := range subChunks {
					chunks = append(chunks, ch)
					index++
				}
			}
		}

		// Find end of this section (next header or end of content)
		sectionEnd := len(content)
		if i+1 < len(matches) {
			sectionEnd = matches[i+1][0]
		}

		sectionContent := strings.TrimSpace(content[match[0]:sectionEnd])
		if len(sectionContent) > 0 {
			// Extract header text for metadata
			headerText := content[match[4]:match[5]]

			subChunks := c.createChunksFromSection(sectionContent, match[0], opts, index)
			for j := range subChunks {
				if subChunks[j].Metadata == nil {
					subChunks[j].Metadata = make(map[string]string)
				}
				subChunks[j].Metadata["section"] = headerText
				chunks = append(chunks, subChunks[j])
				index++
			}
		}

		prevEnd = sectionEnd
	}

	return chunks
}

// createChunksFromSection creates chunks from a section of content.
func (c *Chunker) createChunksFromSection(content string, offset int, opts ChunkOptions, startIndex int) []Chunk {
	if utf8.RuneCountInString(content) <= opts.MaxSize {
		return []Chunk{{
			ChunkID:   c.chunkID(content, startIndex),
			Index:     startIndex,
			Content:   content,
			StartChar: offset,
			EndChar:   offset + len(content),
		}}
	}

	// Use sentence-aware chunking for large sections
	baseChunks := c.chunkSentenceAware(content, opts)

	// Adjust offsets
	for i := range baseChunks {
		baseChunks[i].StartChar += offset
		baseChunks[i].EndChar += offset
		baseChunks[i].Index = startIndex + i
	}

	return baseChunks
}

// chunkPDF chunks PDF-extracted text with section and paragraph awareness.
func (c *Chunker) chunkPDF(content string, opts ChunkOptions) []Chunk {
	if opts.MaxSize <= 0 {
		opts.MaxSize = c.defaultMaxSize
	}
	if opts.Overlap <= 0 {
		opts.Overlap = c.defaultOverlap
	}

	// Clean up PDF extraction artifacts
	content = cleanPDFContent(content)

	if len(content) == 0 {
		return []Chunk{}
	}

	if utf8.RuneCountInString(content) <= opts.MaxSize {
		return []Chunk{{
			ChunkID:   c.chunkID(content, 0),
			Index:     0,
			Content:   content,
			StartChar: 0,
			EndChar:   len(content),
		}}
	}

	// Try to detect sections/paragraphs
	paragraphs := splitPDFIntoParagraphs(content)

	var chunks []Chunk
	var currentChunk strings.Builder
	var chunkStart int
	index := 0
	charPos := 0

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If adding this paragraph would exceed max size
		if currentChunk.Len() > 0 && currentChunk.Len()+len(para)+2 > opts.MaxSize {
			// Save current chunk
			chunkContent := strings.TrimSpace(currentChunk.String())
			if len(chunkContent) > 0 {
				chunks = append(chunks, Chunk{
					ChunkID:   c.chunkID(chunkContent, index),
					Index:     index,
					Content:   chunkContent,
					StartChar: chunkStart,
					EndChar:   charPos,
				})
				index++
			}

			// Start new chunk with some overlap
			currentChunk.Reset()
			overlapText := getOverlapFromEnd(chunkContent, opts.Overlap)
			if overlapText != "" {
				currentChunk.WriteString(overlapText)
				currentChunk.WriteString("\n\n")
			}
			chunkStart = charPos - len(overlapText)
		}

		if currentChunk.Len() > 0 {
			currentChunk.WriteString("\n\n")
		}
		currentChunk.WriteString(para)
		charPos += len(para) + 2
	}

	// Don't forget the last chunk
	if currentChunk.Len() > 0 {
		chunkContent := strings.TrimSpace(currentChunk.String())
		if len(chunkContent) > 0 {
			chunks = append(chunks, Chunk{
				ChunkID:   c.chunkID(chunkContent, index),
				Index:     index,
				Content:   chunkContent,
				StartChar: chunkStart,
				EndChar:   len(content),
			})
		}
	}

	return chunks
}

// cleanPDFContent cleans up common PDF extraction artifacts.
func cleanPDFContent(content string) string {
	// Rejoin hyphenated words at line breaks
	hyphenPattern := regexp.MustCompile(`(\w)-\s*\n\s*(\w)`)
	content = hyphenPattern.ReplaceAllString(content, "$1$2")

	// Remove form feed characters
	content = strings.ReplaceAll(content, "\f", "\n\n")

	// Remove excessive whitespace
	spacePattern := regexp.MustCompile(`[ \t]+`)
	content = spacePattern.ReplaceAllString(content, " ")

	// Normalize line breaks
	content = strings.ReplaceAll(content, "\r\n", "\n")

	// Remove page number artifacts (standalone numbers)
	pageNumPattern := regexp.MustCompile(`(?m)^\s*\d+\s*$`)
	content = pageNumPattern.ReplaceAllString(content, "")

	// Clean up multiple blank lines
	blankLinePattern := regexp.MustCompile(`\n{3,}`)
	content = blankLinePattern.ReplaceAllString(content, "\n\n")

	return strings.TrimSpace(content)
}

// splitPDFIntoParagraphs splits PDF content into paragraphs.
func splitPDFIntoParagraphs(content string) []string {
	// Split on double newlines (paragraph breaks)
	paragraphs := regexp.MustCompile(`\n{2,}`).Split(content, -1)

	var result []string
	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p != "" && len(p) > 20 { // Skip very short fragments
			result = append(result, p)
		}
	}

	// If we got too few paragraphs, try single newlines with heuristics
	if len(result) < 3 && len(content) > 500 {
		lines := strings.Split(content, "\n")
		var currentPara strings.Builder

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				if currentPara.Len() > 0 {
					result = append(result, currentPara.String())
					currentPara.Reset()
				}
				continue
			}

			// Check if this line starts a new paragraph
			startsWithCap := len(line) > 0 && unicode.IsUpper(rune(line[0]))
			prevEndsWithPeriod := currentPara.Len() > 0 && strings.HasSuffix(strings.TrimSpace(currentPara.String()), ".")

			if prevEndsWithPeriod && startsWithCap && currentPara.Len() > 100 {
				result = append(result, currentPara.String())
				currentPara.Reset()
			}

			if currentPara.Len() > 0 {
				currentPara.WriteString(" ")
			}
			currentPara.WriteString(line)
		}

		if currentPara.Len() > 0 {
			result = append(result, currentPara.String())
		}
	}

	return result
}
