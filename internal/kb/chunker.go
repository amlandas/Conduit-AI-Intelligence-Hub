package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
			ChunkID:   ChunkID(opts.DocumentID, 0, content),
			Index:     0,
			Content:   content,
			StartChar: 0,
			EndChar:   len(content),
		}}
	}

	return c.splitRecursive(content, opts)
}

// splitRecursive splits content into overlapping windows, cutting at the best
// boundary the splitter list can find rather than blindly at MaxSize.
//
// Issue #76 was two defects in this loop:
//
//   - It pre-cut the window to content[currentPos : currentPos+MaxSize] and then
//     handed that slice to findBestSplit together with the same MaxSize.
//     findBestSplit's first statement returns early when the text already fits,
//     which was therefore always true, so the splitter search was unreachable
//     and every boundary was a blind cut mid-word. ChunkOptions.Splitters was
//     inert. It is now given the whole remaining text.
//   - It advanced by (len - Overlap) unconditionally, including on the window
//     that already reached the end of the document, so every multi-chunk
//     document ended with a chunk wholly contained in its predecessor. The loop
//     now stops when the window reaches the end.
func (c *Chunker) splitRecursive(content string, opts ChunkOptions) []Chunk {
	var chunks []Chunk
	var currentPos int
	var prevEnd int // absolute end of the previously emitted window
	index := 0

	for currentPos < len(content) {
		remaining := content[currentPos:]

		// The new window starts Overlap bytes inside the previous one. A cut at
		// or before prevEnd would therefore produce a chunk with no new content
		// at all -- the same redundancy the trailing chunk used to have, just in
		// the middle of the document. minSize forbids it.
		minSize := 0
		if prevEnd > currentPos {
			minSize = prevEnd - currentPos
		}

		splitPoint := c.findBestSplit(remaining, opts.Splitters, minSize, opts.MaxSize)
		if splitPoint <= 0 || splitPoint > len(remaining) {
			splitPoint = len(remaining)
		}
		chunkText := remaining[:splitPoint]
		prevEnd = currentPos + len(chunkText)

		// Trim whitespace but preserve for position tracking
		trimmedText := strings.TrimSpace(chunkText)
		if len(trimmedText) > 0 {
			chunks = append(chunks, Chunk{
				ChunkID:   ChunkID(opts.DocumentID, index, trimmedText),
				Index:     index,
				Content:   trimmedText,
				StartChar: currentPos,
				EndChar:   currentPos + len(chunkText),
			})
			index++
		}

		// This window reached the end of the document, so every character is
		// already covered. Advancing here is what used to emit the redundant
		// trailing chunk.
		if currentPos+len(chunkText) >= len(content) {
			break
		}

		advance := len(chunkText) - opts.Overlap
		if advance <= 0 {
			advance = len(chunkText)
		}
		if advance <= 0 {
			advance = 1 // never stall, whatever the splitter returned
		}
		currentPos += advance
	}

	return chunks
}

// findBestSplit returns how many bytes of text belong in the next chunk.
//
// text is the whole remaining document, not a pre-cut window: narrowing it to
// maxSize first is what made the early return below always fire and left the
// splitter search unreachable (issue #76).
//
// The returned cut is in (minSize, maxSize]. minSize is how much of text the
// previous chunk already covered, so a cut at or below it would add nothing.
func (c *Chunker) findBestSplit(text string, splitters []string, minSize, maxSize int) int {
	if maxSize <= 0 || len(text) <= maxSize {
		return len(text)
	}
	if minSize >= maxSize {
		minSize = 0 // an Overlap that wide leaves no room to be choosy
	}

	// The cut may land anywhere up to maxSize. Prefer the LAST occurrence of
	// the highest-priority splitter inside that window, which keeps chunks as
	// full as possible while still respecting the boundary.
	window := text[:maxSize]
	for _, splitter := range splitters {
		if splitter == "" {
			continue
		}
		if idx := strings.LastIndex(window, splitter); idx > 0 {
			if cut := idx + len(splitter); cut > minSize {
				return cut
			}
		}
	}

	// No splitter matched. Fall back to maxSize, preferring a word boundary and
	// never cutting through a multi-byte rune.
	floor := maxSize / 2
	if minSize > floor {
		floor = minSize
	}
	for i := maxSize; i > floor; i-- {
		if text[i] == ' ' {
			return i + 1
		}
	}
	cut := maxSize
	for cut > minSize && !utf8.RuneStart(text[cut]) {
		cut--
	}
	if cut <= minSize {
		return maxSize
	}
	return cut
}

// ChunkID derives the identifier for one chunk.
//
// This is the ONLY chunk-id function in the package. Issue #72 was two of them:
// Chunker.chunkID hashed content alone (so a paragraph repeated in two
// documents produced one id) while Indexer.generateUniqueChunkID hashed
// document + index + content and silently overwrote the chunker's id at insert
// time. The two could -- and did -- disagree, so anything consuming Chunker
// output directly saw colliding ids while the database saw unique ones.
//
// The hashed payload is "documentID:index:content", byte-for-byte what the
// indexer already used. That is deliberate: existing knowledge bases keep their
// chunk ids, so vectors stored against them are not orphaned.
func ChunkID(documentID string, index int, content string) string {
	h := sha256.Sum256(fmt.Appendf(nil, "%s:%d:%s", documentID, index, content))
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

// EstimateChunkCount estimates how many chunks Chunk will produce.
//
// It models the windowing exactly: windows of MaxSize advancing by
// (MaxSize - Overlap), stopping as soon as a window reaches the end. That makes
// it exact whenever no splitter matches, and an upper-bound-flavoured estimate
// otherwise, since a boundary-aware cut is never longer than MaxSize.
//
// Issue #76: the old formula was off by one in the other direction because it
// did not round up, while Chunk itself emitted one chunk too many. The two
// errors did not cancel -- 1485 characters at MaxSize 1000 / Overlap 100
// estimated 2 and produced 3.
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

	step := opts.MaxSize - opts.Overlap
	if step <= 0 {
		step = opts.MaxSize
	}

	// ceil((contentLength - Overlap) / step)
	return (contentLength - opts.Overlap + step - 1) / step
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
			ChunkID:   ChunkID(opts.DocumentID, 0, content),
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
					ChunkID:   ChunkID(opts.DocumentID, index, chunkContent),
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
				ChunkID:   ChunkID(opts.DocumentID, index, chunkContent),
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
			ChunkID:   ChunkID(opts.DocumentID, 0, content),
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
				sub.ChunkID = ChunkID(opts.DocumentID, index, sub.Content)
				if sub.Metadata == nil {
					sub.Metadata = make(map[string]string)
				}
				sub.Metadata["block_type"] = boundary.blockType
				chunks = append(chunks, sub)
				index++
			}
		} else {
			chunk := Chunk{
				ChunkID:   ChunkID(opts.DocumentID, index, blockContent),
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
			ChunkID:   ChunkID(opts.DocumentID, 0, content),
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
			ChunkID:   ChunkID(opts.DocumentID, startIndex, content),
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
			ChunkID:   ChunkID(opts.DocumentID, 0, content),
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
					ChunkID:   ChunkID(opts.DocumentID, index, chunkContent),
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
				ChunkID:   ChunkID(opts.DocumentID, index, chunkContent),
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
