package kb

// Table tests for the chunker: sizes, boundaries, positions and ids.
// chunker_test.go already covers the trivial cases; this file pins the exact
// windowing behaviour that retrieval quality depends on, including two defects
// that are NOT among the tracked issues #69-#73.

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestChunker_WindowingTable(t *testing.T) {
	c := NewChunker()

	tests := []struct {
		name string
		in   string
		opts ChunkOptions
		// want is the exact chunk content, in order.
		want []string
		// wantStart/wantEnd are the recorded character offsets.
		wantStart []int
		wantEnd   []int
		notes     string
	}{
		{
			name:      "content shorter than the window is a single chunk",
			in:        "One two three.",
			opts:      ChunkOptions{MaxSize: 1000},
			want:      []string{"One two three."},
			wantStart: []int{0},
			wantEnd:   []int{14},
		},
		{
			name:      "content exactly the window size is a single chunk",
			in:        "0123456789",
			opts:      ChunkOptions{MaxSize: 10, Overlap: 2},
			want:      []string{"0123456789"},
			wantStart: []int{0},
			wantEnd:   []int{10},
		},
		{
			name: "fixed windows with overlap, cutting mid-word",
			in:   "abcdefghijklmnopqrstuvwxyz",
			opts: ChunkOptions{MaxSize: 10, Overlap: 2, Splitters: []string{"\n\n"}},
			want: []string{"abcdefghij", "ijklmnopqr", "qrstuvwxyz", "yz"},
			// Note the final "yz": it is wholly contained in the previous chunk.
			wantStart: []int{0, 8, 16, 24},
			wantEnd:   []int{10, 18, 26, 26},
			notes:     "the trailing chunk is always a duplicate of the overlap tail",
		},
		{
			name: "KNOWN GAP: paragraph splitters do not move the cut point",
			in:   "AAAA BBBB.\n\nCCCC DDDD.\n\nEEEE FFFF.",
			opts: ChunkOptions{MaxSize: 15, Overlap: 2},
			want: []string{"AAAA BBBB.\n\nCCC", "CCC DDDD.\n\nEEEE", "EE FFFF.", "F."},
			// A splitter-aware chunker would cut after "AAAA BBBB.\n\n".
			wantStart: []int{0, 13, 26, 32},
			wantEnd:   []int{15, 28, 34, 34},
			notes:     "see TestGolden_ChunkerNeverUsesSplitters for why",
		},
		{
			name:      "KNOWN GAP: sentence splitters do not move the cut point either",
			in:        "One two three. Four five six. Seven eight nine.",
			opts:      ChunkOptions{MaxSize: 20, Overlap: 3},
			want:      []string{"One two three. Four", "ur five six. Seven e", "n eight nine.", "ne."},
			wantStart: []int{0, 17, 34, 44},
			wantEnd:   []int{20, 37, 47, 47},
		},
		{
			name:      "windows are trimmed of surrounding whitespace",
			in:        "alpha   \n   beta",
			opts:      ChunkOptions{MaxSize: 9, Overlap: 1},
			want:      []string{"alpha", "beta", "a"},
			wantStart: []int{0, 8, 15},
			wantEnd:   []int{9, 16, 16},
			notes:     "Content is trimmed but StartChar/EndChar keep the untrimmed offsets, so the offsets no longer address the stored text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := c.Chunk(tt.in, tt.opts)
			if len(got) != len(tt.want) {
				t.Fatalf("chunk count: got %d, want %d\ngot: %q\n%s", len(got), len(tt.want), contents(got), tt.notes)
			}
			for i := range tt.want {
				if got[i].Content != tt.want[i] {
					t.Errorf("chunk %d content: got %q, want %q", i, got[i].Content, tt.want[i])
				}
				if got[i].Index != i {
					t.Errorf("chunk %d index: got %d, want %d", i, got[i].Index, i)
				}
				if got[i].StartChar != tt.wantStart[i] {
					t.Errorf("chunk %d StartChar: got %d, want %d", i, got[i].StartChar, tt.wantStart[i])
				}
				if got[i].EndChar != tt.wantEnd[i] {
					t.Errorf("chunk %d EndChar: got %d, want %d", i, got[i].EndChar, tt.wantEnd[i])
				}
			}
		})
	}
}

func contents(chunks []Chunk) []string {
	out := make([]string, 0, len(chunks))
	for _, c := range chunks {
		out = append(out, c.Content)
	}
	return out
}

// TestGolden_ChunkerNeverUsesSplitters pins a defect that is NOT one of the
// five tracked issues (#69-#73), and is a candidate sixth bug.
//
// splitRecursive slices content[currentPos : currentPos+MaxSize] and hands that
// slice to findBestSplit along with the same MaxSize. findBestSplit's first
// statement is `if utf8.RuneCountInString(text) <= maxSize { return len(text) }`
// -- which is always true for an ASCII slice of exactly MaxSize bytes. So the
// splitter search below it is unreachable and every chunk boundary is a blind
// cut at MaxSize, mid-word and mid-sentence.
//
// The Splitters option is therefore inert for Chunk(). ChunkSmart() has its own
// content-type-specific paths and is not affected in the same way.
func TestGolden_ChunkerNeverUsesSplitters(t *testing.T) {
	c := NewChunker()

	// A paragraph break sits well inside the window, so a splitter-aware
	// chunker would cut there. It does not.
	text := "SHORT.\n\n" + strings.Repeat("x", 40)
	got := c.Chunk(text, ChunkOptions{MaxSize: 20, Overlap: 0, Splitters: []string{"\n\n"}})

	if len(got) == 0 {
		t.Fatal("expected chunks")
	}
	if got[0].Content == "SHORT." {
		t.Fatalf("the chunker now honours splitters; this test documented the opposite and must be updated")
	}
	if got[0].Content != "SHORT.\n\nxxxxxxxxxxxx" {
		t.Errorf("first chunk: got %q, want a blind 20-character cut", got[0].Content)
	}

	// Directly: findBestSplit refuses to look for a splitter when the text it
	// is given is not longer than maxSize -- which is the only way
	// splitRecursive ever calls it.
	window := "aa\n\nbbbbbbbbbbbbbbbbbb"
	if len(window) != 22 {
		t.Fatalf("fixture drifted: window is %d chars", len(window))
	}
	if got := c.findBestSplit(window, []string{"\n\n"}, 22); got != 22 {
		t.Errorf("findBestSplit with len(text) == maxSize returned %d, want 22 (early return, splitters skipped)", got)
	}
	// When it IS given a longer text than maxSize, the splitter logic works.
	if got := c.findBestSplit(window, []string{"\n\n"}, 10); got != 4 {
		t.Errorf("findBestSplit with len(text) > maxSize returned %d, want 4 (just after the paragraph break)", got)
	}
}

// TestGolden_ChunkerEmitsTrailingDuplicate pins the second chunker defect: the
// windowing loop always emits a final chunk that is entirely contained in the
// previous one, because it advances by (len - overlap) and then re-enters the
// loop for the remaining `overlap` characters.
//
// Candidate sixth bug; pinned, not fixed. It inflates chunk counts and vector
// storage by one chunk per multi-chunk document.
func TestGolden_ChunkerEmitsTrailingDuplicate(t *testing.T) {
	c := NewChunker()

	got := c.Chunk(strings.Repeat("ab", 13), ChunkOptions{MaxSize: 10, Overlap: 2, Splitters: []string{"\n\n"}})
	if len(got) < 2 {
		t.Fatalf("expected several chunks, got %d", len(got))
	}

	last := got[len(got)-1]
	prev := got[len(got)-2]
	if len(last.Content) != 2 {
		t.Errorf("trailing chunk length: got %d, want 2 (== Overlap)", len(last.Content))
	}
	if !strings.Contains(prev.Content, last.Content) {
		t.Errorf("trailing chunk %q is expected to be a substring of the previous chunk %q", last.Content, prev.Content)
	}

	// It happens on the real corpus too: 1485 characters at MaxSize 1000 /
	// Overlap 100 becomes 3 chunks, the third of which is redundant.
	gi := ingestGoldenCorpus(t)
	var gettysburg corpusDoc
	for _, d := range gi.Docs {
		if d.DocumentID == "01-gettysburg-address" {
			gettysburg = d
		}
	}
	chunks := c.Chunk(gettysburg.Content, goldenChunkOptions)
	if len(chunks) != 3 {
		t.Fatalf("gettysburg chunk count: got %d, want 3", len(chunks))
	}
	if !strings.Contains(chunks[1].Content, chunks[2].Content) {
		t.Errorf("the third gettysburg chunk is expected to be wholly contained in the second")
	}
}

func TestChunker_EstimateVsActual(t *testing.T) {
	c := NewChunker()

	tests := []struct {
		name         string
		length       int
		opts         ChunkOptions
		wantEstimate int
		wantActual   int
		estimateNote string
	}{
		{name: "single chunk", length: 100, opts: ChunkOptions{MaxSize: 1000, Overlap: 100}, wantEstimate: 1, wantActual: 1},
		{
			name:   "estimate undercounts because of the trailing duplicate",
			length: 26, opts: ChunkOptions{MaxSize: 10, Overlap: 2},
			wantEstimate: 4, wantActual: 4,
		},
		{
			name:   "estimate and actual diverge on the golden settings",
			length: 1485, opts: ChunkOptions{MaxSize: 1000, Overlap: 100},
			wantEstimate: 2, wantActual: 3,
			estimateNote: "EstimateChunkCount does not model the trailing overlap chunk",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.EstimateChunkCount(tt.length, tt.opts); got != tt.wantEstimate {
				t.Errorf("EstimateChunkCount(%d) = %d, want %d", tt.length, got, tt.wantEstimate)
			}
			got := c.Chunk(strings.Repeat("a", tt.length), tt.opts)
			if len(got) != tt.wantActual {
				t.Errorf("actual chunk count = %d, want %d. %s", len(got), tt.wantActual, tt.estimateNote)
			}
		})
	}
}

// TestChunker_IDsTable covers kb.ChunkID, the single chunk-id function left
// after issue #72 (see chunker.go). An id is a function of document identity,
// chunk index and chunk content -- all three.
func TestChunker_IDsTable(t *testing.T) {
	tests := []struct {
		name     string
		aDoc     string
		a        string
		aIndex   int
		bDoc     string
		b        string
		bIndex   int
		wantSame bool
		notes    string
	}{
		{name: "same document, content and index", aDoc: "d", a: "hello", aIndex: 0, bDoc: "d", b: "hello", bIndex: 0, wantSame: true},
		{
			name: "issue #72 fixed: same content at a different index is a different chunk",
			aDoc: "d", a: "hello", aIndex: 0, bDoc: "d", b: "hello", bIndex: 9, wantSame: false,
			notes: "the index is now part of the hashed payload",
		},
		{
			name: "issue #72 fixed: identical content in two documents does not collide",
			aDoc: "docA", a: "shared paragraph", aIndex: 0, bDoc: "docB", b: "shared paragraph", bIndex: 0, wantSame: false,
			notes: "the document id is part of the hashed payload",
		},
		{name: "different content", aDoc: "d", a: "hello", aIndex: 0, bDoc: "d", b: "world", bIndex: 0, wantSame: false},
		{name: "whitespace difference changes the id", aDoc: "d", a: "hello ", aIndex: 0, bDoc: "d", b: "hello", bIndex: 0, wantSame: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ChunkID(tt.aDoc, tt.aIndex, tt.a) == ChunkID(tt.bDoc, tt.bIndex, tt.b)
			if got != tt.wantSame {
				t.Errorf("ChunkID collision = %v, want %v\n%s", got, tt.wantSame, tt.notes)
			}
		})
	}

	// The id format is part of the on-disk contract for the vector store.
	id := ChunkID("doc", 0, "anything")
	if !strings.HasPrefix(id, "chunk_") || len(id) != len("chunk_")+16 {
		t.Errorf("chunk id format changed: %q (want chunk_ + 16 hex characters)", id)
	}

	// The hashed payload must stay byte-identical to the one the old
	// Indexer.generateUniqueChunkID used, or every already-indexed knowledge
	// base loses the link between its chunks and its stored vectors.
	want := sha256.Sum256([]byte("doc:3:body text"))
	if got := ChunkID("doc", 3, "body text"); got != "chunk_"+hex.EncodeToString(want[:8]) {
		t.Errorf("ChunkID payload changed: %s -- existing knowledge bases would be orphaned", got)
	}
}

func TestDetectContentTypeTable(t *testing.T) {
	tests := []struct {
		path string
		want ContentType
	}{
		{"main.go", ContentTypeCode},
		{"script.py", ContentTypeCode},
		{"Component.TS", ContentTypeCode},
		{"README.md", ContentTypeMarkdown},
		{"doc.markdown", ContentTypeMarkdown},
		{"guide.rst", ContentTypeMarkdown},
		{"paper.pdf", ContentTypePDF},
		{"notes.txt", ContentTypeText},
		{"no-extension", ContentTypeText},
		{"data.json", ContentTypeText},
		{"", ContentTypeText},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := DetectContentType(tt.path); got != tt.want {
				t.Errorf("DetectContentType(%q) = %s, want %s", tt.path, got, tt.want)
			}
		})
	}
}

// TestChunker_SmartDispatch pins that ChunkSmart routes by extension and that
// every route produces non-empty, index-ordered chunks.
func TestChunker_SmartDispatch(t *testing.T) {
	c := NewChunker()
	body := strings.Repeat("The keeper trims the lantern at dusk. ", 60)

	for _, path := range []string{"a.go", "a.md", "a.pdf", "a.txt"} {
		t.Run(path, func(t *testing.T) {
			chunks := c.ChunkSmart(body, path, ChunkOptions{MaxSize: 300, Overlap: 30})
			if len(chunks) == 0 {
				t.Fatalf("ChunkSmart(%s) produced no chunks", path)
			}
			for i, ch := range chunks {
				if ch.Index != i {
					t.Errorf("chunk %d has index %d", i, ch.Index)
				}
				if strings.TrimSpace(ch.Content) == "" {
					t.Errorf("chunk %d is blank", i)
				}
			}
		})
	}
}
