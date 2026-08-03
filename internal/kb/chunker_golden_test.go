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
			name: "fixed windows with overlap, cutting mid-word when no splitter matches",
			in:   "abcdefghijklmnopqrstuvwxyz",
			opts: ChunkOptions{MaxSize: 10, Overlap: 2, Splitters: []string{"\n\n"}},
			want: []string{"abcdefghij", "ijklmnopqr", "qrstuvwxyz"},
			// #76: there is no trailing "yz" any more. The third window reaches
			// the end of the input, so the loop stops instead of re-emitting the
			// overlap tail as a fourth chunk.
			wantStart: []int{0, 8, 16},
			wantEnd:   []int{10, 18, 26},
			notes:     "issue #76: the redundant trailing chunk is gone",
		},
		{
			name: "paragraph splitters move the cut point",
			in:   "AAAA BBBB.\n\nCCCC DDDD.\n\nEEEE FFFF.",
			opts: ChunkOptions{MaxSize: 15, Overlap: 2},
			want: []string{"AAAA BBBB.", "CCCC DDDD.", "EEEE FFFF."},
			// #76: cuts land after each "\n\n" instead of blindly at MaxSize.
			wantStart: []int{0, 10, 22},
			wantEnd:   []int{12, 24, 34},
			notes:     "issue #76: ChunkOptions.Splitters is no longer inert",
		},
		{
			name:      "sentence splitters move the cut point too",
			in:        "One two three. Four five six. Seven eight nine.",
			opts:      ChunkOptions{MaxSize: 20, Overlap: 3},
			want:      []string{"One two three.", "e. Four five six.", "x. Seven eight nine."},
			wantStart: []int{0, 12, 27},
			wantEnd:   []int{15, 30, 47},
			notes:     `the default splitter list reaches ". " before falling back to " "`,
		},
		{
			name:      "windows are trimmed of surrounding whitespace",
			in:        "alpha   \n   beta",
			opts:      ChunkOptions{MaxSize: 9, Overlap: 1},
			want:      []string{"alpha", "beta"},
			wantStart: []int{0, 8},
			wantEnd:   []int{9, 16},
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

// TestGolden_ChunkerHonoursSplitters is the enforcement half of issue #76.
//
// Was: splitRecursive sliced content[currentPos : currentPos+MaxSize] and
// handed that slice to findBestSplit along with the same MaxSize, so
// findBestSplit's "the text already fits" early return always fired and the
// splitter search below it was unreachable. Every boundary was a blind cut at
// MaxSize, mid-word and mid-sentence, and ChunkOptions.Splitters was inert.
func TestGolden_ChunkerHonoursSplitters(t *testing.T) {
	c := NewChunker()

	// A paragraph break sits well inside the window, so the cut lands there.
	text := "SHORT.\n\n" + strings.Repeat("x", 40)
	got := c.Chunk(text, ChunkOptions{MaxSize: 20, Overlap: 0, Splitters: []string{"\n\n"}})

	if len(got) == 0 {
		t.Fatal("expected chunks")
	}
	if got[0].Content != "SHORT." {
		t.Errorf("first chunk: got %q, want a cut at the paragraph break (%q)", got[0].Content, "SHORT.")
	}

	// Directly: findBestSplit searches for a splitter whenever the text it is
	// given is longer than maxSize, which is how splitRecursive now calls it.
	window := "aa\n\nbbbbbbbbbbbbbbbbbb"
	if len(window) != 22 {
		t.Fatalf("fixture drifted: window is %d chars", len(window))
	}
	if got := c.findBestSplit(window, []string{"\n\n"}, 0, 22); got != 22 {
		t.Errorf("findBestSplit with len(text) == maxSize returned %d, want 22 (the whole text fits)", got)
	}
	if got := c.findBestSplit(window, []string{"\n\n"}, 0, 10); got != 4 {
		t.Errorf("findBestSplit returned %d, want 4 (just after the paragraph break)", got)
	}

	// minSize forbids a cut that the previous chunk already covered. With the
	// only paragraph break at byte 4 and 6 bytes already covered, the splitter
	// is skipped and the fallback cut is used instead.
	if got := c.findBestSplit(window, []string{"\n\n"}, 6, 10); got <= 6 {
		t.Errorf("findBestSplit returned %d, which adds no new content past minSize 6", got)
	}

	// Sentence splitters move the cut too, not just paragraph breaks.
	sentences := c.Chunk("One two three. Four five six. Seven eight nine.", ChunkOptions{MaxSize: 20, Overlap: 3})
	if len(sentences) == 0 || sentences[0].Content != "One two three." {
		t.Errorf("sentence-aware cut: got %q, want %q", contents(sentences), []string{"One two three.", "..."})
	}
}

// TestGolden_ChunkerEmitsNoRedundantChunk is the other half of issue #76.
//
// Was: the windowing loop advanced by (len - Overlap) even on the window that
// already reached the end of the document, so every multi-chunk document ended
// with a chunk wholly contained in its predecessor -- inflating chunk counts,
// FTS rows and vector storage by one per document. A second form of the same
// defect appeared mid-document once splitters started working: the window could
// cut before the previous window's end and emit a chunk with no new content.
func TestGolden_ChunkerEmitsNoRedundantChunk(t *testing.T) {
	c := NewChunker()

	got := c.Chunk(strings.Repeat("ab", 13), ChunkOptions{MaxSize: 10, Overlap: 2, Splitters: []string{"\n\n"}})
	if len(got) < 2 {
		t.Fatalf("expected several chunks, got %d", len(got))
	}
	assertNoContainedChunk(t, got)

	// The real corpus: every document, at the production chunk settings.
	gi := ingestGoldenCorpus(t)
	for _, d := range gi.Docs {
		t.Run(d.DocumentID, func(t *testing.T) {
			assertNoContainedChunk(t, c.Chunk(d.Content, goldenChunkOptions))
		})
	}
}

// assertNoContainedChunk fails if one chunk's character range is wholly inside
// another's, which is the shape every redundant chunk takes.
//
// The check is on offsets rather than content because repeated source text can
// legitimately produce two chunks with identical content at different
// positions; what must never happen is a chunk that covers no new characters.
func assertNoContainedChunk(t *testing.T, chunks []Chunk) {
	t.Helper()
	for i, a := range chunks {
		for j, b := range chunks {
			if i == j {
				continue
			}
			if b.StartChar <= a.StartChar && a.EndChar <= b.EndChar {
				t.Errorf("chunk %d [%d,%d) is wholly contained in chunk %d [%d,%d): %q",
					i, a.StartChar, a.EndChar, j, b.StartChar, b.EndChar, a.Content)
			}
		}
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
			name:   "estimate agrees with actual",
			length: 26, opts: ChunkOptions{MaxSize: 10, Overlap: 2},
			wantEstimate: 3, wantActual: 3,
		},
		{
			name:   "estimate agrees with actual on the golden settings",
			length: 1485, opts: ChunkOptions{MaxSize: 1000, Overlap: 100},
			wantEstimate: 2, wantActual: 2,
			estimateNote: "issue #76: EstimateChunkCount now models the same windowing Chunk performs",
		},
		{
			name:   "estimate agrees with actual across a range of lengths",
			length: 3000, opts: ChunkOptions{MaxSize: 1000, Overlap: 100},
			wantEstimate: 4, wantActual: 4,
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
