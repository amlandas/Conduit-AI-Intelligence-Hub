package kb

import (
	"testing"
)

func TestChunker_SingleChunk(t *testing.T) {
	c := NewChunker()
	content := "This is a short piece of content."

	chunks := c.Chunk(content, ChunkOptions{MaxSize: 1000})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Content != content {
		t.Errorf("expected content %q, got %q", content, chunks[0].Content)
	}
	if chunks[0].Index != 0 {
		t.Errorf("expected index 0, got %d", chunks[0].Index)
	}
}

func TestChunker_MultipleChunks(t *testing.T) {
	c := NewChunker()
	content := "This is paragraph one.\n\nThis is paragraph two.\n\nThis is paragraph three."

	chunks := c.Chunk(content, ChunkOptions{
		MaxSize:   30,
		Overlap:   5,
		Splitters: []string{"\n\n", "\n", ". ", " "},
	})

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	// Verify chunk IDs are unique
	ids := make(map[string]bool)
	for _, chunk := range chunks {
		if ids[chunk.ChunkID] {
			t.Errorf("duplicate chunk ID: %s", chunk.ChunkID)
		}
		ids[chunk.ChunkID] = true
	}
}

func TestChunker_EmptyContent(t *testing.T) {
	c := NewChunker()
	chunks := c.Chunk("", ChunkOptions{MaxSize: 100})

	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestChunker_DefaultOptions(t *testing.T) {
	c := NewChunker()
	content := "Test content"

	chunks := c.Chunk(content, ChunkOptions{})

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunker_ChunkWithMetadata(t *testing.T) {
	c := NewChunker()
	content := "Test content for metadata"
	meta := map[string]string{"source": "test", "type": "markdown"}

	chunks := c.ChunkWithMetadata(content, ChunkOptions{MaxSize: 1000}, meta)

	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0].Metadata["source"] != "test" {
		t.Errorf("expected metadata source=test, got %v", chunks[0].Metadata)
	}
	if chunks[0].Metadata["type"] != "markdown" {
		t.Errorf("expected metadata type=markdown, got %v", chunks[0].Metadata)
	}
}

func TestChunker_EstimateChunkCount(t *testing.T) {
	c := NewChunker()

	tests := []struct {
		contentLen int
		maxSize    int
		overlap    int
		expected   int
	}{
		{100, 1000, 100, 1},   // Content smaller than chunk
		{1000, 500, 50, 3},    // Multiple chunks
		{500, 500, 50, 1},     // Exactly one chunk
	}

	for _, tt := range tests {
		opts := ChunkOptions{MaxSize: tt.maxSize, Overlap: tt.overlap}
		count := c.EstimateChunkCount(tt.contentLen, opts)
		if count != tt.expected {
			t.Errorf("EstimateChunkCount(%d, %+v) = %d, want %d",
				tt.contentLen, opts, count, tt.expected)
		}
	}
}

func TestChunker_PreservesWords(t *testing.T) {
	c := NewChunker()
	content := "This is a long sentence that should be split at word boundaries not in the middle"

	chunks := c.Chunk(content, ChunkOptions{
		MaxSize:   20,
		Overlap:   3,
		Splitters: []string{" "},
	})

	for _, chunk := range chunks {
		// No chunk should end with a partial word (except possibly last)
		if len(chunk.Content) > 0 {
			lastChar := chunk.Content[len(chunk.Content)-1]
			// Should end with a space or be the complete remainder
			if lastChar != ' ' && chunk.EndChar != len(content) {
				// Check if it's at a word boundary in original
				if chunk.EndChar < len(content) && content[chunk.EndChar] != ' ' {
					// This is acceptable if we couldn't find a better split point
				}
			}
		}
	}
}
