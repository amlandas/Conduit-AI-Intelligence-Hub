package kb

import (
	"regexp"
	"strings"
	"unicode"
)

// ContentCleaner performs pre-chunking content cleaning to improve embedding quality.
// This runs BEFORE chunking and embedding, ensuring clean data enters the vector store.
type ContentCleaner struct {
	boilerplatePatterns []*regexp.Regexp
	ocrFixPatterns      []ocrFix
}

type ocrFix struct {
	pattern     *regexp.Regexp
	replacement string
}

// NewContentCleaner creates a new content cleaner with compiled patterns.
func NewContentCleaner() *ContentCleaner {
	return &ContentCleaner{
		boilerplatePatterns: compileBoilerplateRemovalPatterns(),
		ocrFixPatterns:      compileOCRFixPatterns(),
	}
}

// Clean performs comprehensive content cleaning.
// Order matters: OCR fixes first, then boilerplate removal, then normalization.
func (c *ContentCleaner) Clean(content string, contentType ContentType) string {
	if content == "" {
		return ""
	}

	// Step 1: Fix OCR errors (especially for PDFs)
	if contentType == ContentTypePDF {
		content = c.fixOCRErrors(content)
		content = c.fixPDFArtifacts(content)
	}

	// Step 2: Remove boilerplate
	content = c.removeBoilerplate(content)

	// Step 3: Normalize whitespace
	content = c.normalizeWhitespace(content)

	return content
}

// fixOCRErrors corrects common OCR misrecognitions.
func (c *ContentCleaner) fixOCRErrors(content string) string {
	for _, fix := range c.ocrFixPatterns {
		content = fix.pattern.ReplaceAllString(content, fix.replacement)
	}
	return content
}

// fixPDFArtifacts handles PDF-specific text extraction issues.
func (c *ContentCleaner) fixPDFArtifacts(content string) string {
	// Fix hyphenated words split across lines
	// Pattern: word- \n continuation
	hyphenPattern := regexp.MustCompile(`(\w)-\s*\n\s*(\w)`)
	content = hyphenPattern.ReplaceAllString(content, "$1$2")

	// Fix words split by page breaks or column breaks
	// Pattern: word fragment followed by isolated continuation
	splitWordPattern := regexp.MustCompile(`(\w{2,})\s*\n\s*(\w{2,})`)
	// Only rejoin if both parts look like partial words (heuristic)
	content = splitWordPattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := splitWordPattern.FindStringSubmatch(match)
		if len(parts) == 3 {
			// Check if this looks like a split word vs. sentence break
			first := parts[1]
			second := parts[2]
			// If first part ends mid-word (no punctuation) and second starts lowercase
			if len(first) > 0 && len(second) > 0 {
				lastChar := rune(first[len(first)-1])
				firstChar := rune(second[0])
				if unicode.IsLetter(lastChar) && unicode.IsLower(firstChar) {
					return first + second
				}
			}
		}
		return match
	})

	// Remove page numbers appearing as standalone lines
	pageNumPattern := regexp.MustCompile(`(?m)^\s*\d{1,4}\s*$`)
	content = pageNumPattern.ReplaceAllString(content, "")

	// Remove form feed characters
	content = strings.ReplaceAll(content, "\f", "\n")

	return content
}

// removeBoilerplate removes common noise patterns from documents.
func (c *ContentCleaner) removeBoilerplate(content string) string {
	for _, pattern := range c.boilerplatePatterns {
		content = pattern.ReplaceAllString(content, " ")
	}
	return content
}

// normalizeWhitespace cleans up whitespace issues.
func (c *ContentCleaner) normalizeWhitespace(content string) string {
	// Replace tabs with spaces
	content = strings.ReplaceAll(content, "\t", " ")

	// Collapse multiple spaces into one
	spacePattern := regexp.MustCompile(`[ ]+`)
	content = spacePattern.ReplaceAllString(content, " ")

	// Collapse more than 2 consecutive newlines into 2
	newlinePattern := regexp.MustCompile(`\n{3,}`)
	content = newlinePattern.ReplaceAllString(content, "\n\n")

	// Remove leading/trailing whitespace from each line
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	content = strings.Join(lines, "\n")

	// Final trim
	content = strings.TrimSpace(content)

	return content
}

// compileBoilerplateRemovalPatterns returns compiled patterns for boilerplate detection.
// These are removed BEFORE chunking to prevent them from polluting embeddings.
func compileBoilerplateRemovalPatterns() []*regexp.Regexp {
	patterns := []string{
		// Academic/JSTOR boilerplate
		`(?i)this content downloaded from\s+[\d\.]+.*?(?:UTC|GMT)`,
		`(?i)all use subject to.*?terms`,
		`(?i)jstor.*?conditions`,
		`(?i)downloaded from\s+\S+\s+on\s+\w+,\s+\d+\s+\w+\s+\d+`,

		// Copyright notices
		`(?i)copyright\s*©?\s*\d{4}(?:\s*-\s*\d{4})?(?:\s+[^.]+)?`,
		`(?i)all rights reserved\.?`,
		`(?i)©\s*\d{4}`,

		// Page headers/footers
		`(?i)^\s*page\s+\d+\s*(?:of\s+\d+)?\s*$`,
		`(?m)^\s*-\s*\d+\s*-\s*$`, // Page numbers like "- 42 -"

		// Table of contents artifacts
		`(?i)^\s*table of contents\s*$`,
		`\.{5,}`, // Long dot leaders in TOC

		// Document metadata artifacts
		`(?i)^\s*\[?\d+\]?\s*$`, // Footnote reference numbers
		`(?m)^[-_=]{10,}\s*$`,   // Separator lines

		// Website/URL noise (but keep meaningful URLs)
		`(?i)click here to\s+\w+`,
		`(?i)visit\s+\S+\s+for more`,

		// IP addresses (download artifacts)
		`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`,

		// Common PDF extraction artifacts
		`\x00`, // Null characters
		`(?i)^\s*\d+\s+of\s+\d+\s*$`, // "1 of 10" page indicators
	}

	var compiled []*regexp.Regexp
	for _, p := range patterns {
		if re, err := regexp.Compile(p); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}

// compileOCRFixPatterns returns patterns to fix common OCR errors.
func compileOCRFixPatterns() []ocrFix {
	// Common OCR misrecognitions, especially from older PDFs
	fixes := []struct {
		pattern     string
		replacement string
	}{
		// Ligature issues (ff, fi, fl, ffi, ffl often misread)
		{`(?i)\bstaSs\b`, "staffs"},
		{`(?i)\baSect\b`, "affect"},
		{`(?i)\beSect\b`, "effect"},
		{`(?i)\beSorts\b`, "efforts"},
		{`(?i)\bdiSerent\b`, "different"},
		{`(?i)\bdiScult\b`, "difficult"},
		{`(?i)\bofSce\b`, "office"},
		{`(?i)\bofScial\b`, "official"},

		// Common 'fl' ligature issues
		{`(?i)\bSourish\b`, "flourish"},
		{`(?i)\bSow\b`, "flow"},
		{`(?i)\bSoor\b`, "floor"},
		{`(?i)\bSat\b`, "flat"},

		// Common 'fi' ligature issues
		{`(?i)\bScal\b`, "fiscal"},
		{`(?i)\bSnance\b`, "finance"},
		{`(?i)\bSnancial\b`, "financial"},
		{`(?i)\bSgure\b`, "figure"},
		{`(?i)\bSle\b`, "file"},
		{`(?i)\bSnd\b`, "find"},
		{`(?i)\bSrst\b`, "first"},

		// Spacing issues around punctuation
		{`\s+([.,;:!?])`, "$1"},       // Remove space before punctuation
		{`([.,;:!?])(\w)`, "$1 $2"},   // Add space after punctuation if missing

		// Common character substitutions
		{`(?i)\brn\b`, "m"},          // 'rn' often OCR'd instead of 'm'
		{`\bl\s+l\b`, "ll"},          // Split double-l
		{`(?i)\bvv\b`, "w"},          // 'vv' instead of 'w'

		// Quote normalization
		{`[""]`, `"`},                // Curly quotes to straight
		{`['']`, `'`},                // Curly apostrophes to straight
	}

	var compiled []ocrFix
	for _, f := range fixes {
		if re, err := regexp.Compile(f.pattern); err == nil {
			compiled = append(compiled, ocrFix{
				pattern:     re,
				replacement: f.replacement,
			})
		}
	}
	return compiled
}

// DetectRepeatedHeaders finds and removes headers/footers that repeat across pages.
// This is called for PDF content where headers/footers appear on every page.
func (c *ContentCleaner) DetectRepeatedHeaders(content string) string {
	lines := strings.Split(content, "\n")
	if len(lines) < 20 {
		return content // Too short to have meaningful repeated content
	}

	// Count line occurrences
	lineCounts := make(map[string]int)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 5 && len(trimmed) < 100 { // Reasonable header length
			lineCounts[trimmed]++
		}
	}

	// Lines appearing more than 3 times are likely headers/footers
	repeatThreshold := 3
	if len(lines) > 100 {
		repeatThreshold = len(lines) / 30 // ~3% of lines
	}

	repeatedLines := make(map[string]bool)
	for line, count := range lineCounts {
		if count >= repeatThreshold {
			repeatedLines[line] = true
		}
	}

	// Remove repeated lines
	if len(repeatedLines) > 0 {
		var cleaned []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if !repeatedLines[trimmed] {
				cleaned = append(cleaned, line)
			}
		}
		return strings.Join(cleaned, "\n")
	}

	return content
}
