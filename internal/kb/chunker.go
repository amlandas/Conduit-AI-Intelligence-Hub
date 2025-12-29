package kb

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
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
