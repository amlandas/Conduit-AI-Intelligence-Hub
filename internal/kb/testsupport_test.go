package kb

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/store"
)

// init silences the zerolog global logger for the whole package test binary.
// The kb package logs at debug level on every search, which turns `go test -v`
// into an unreadable firehose. This lives in init rather than TestMain because
// kag_test.go already owns TestMain for this package.
func init() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
}

// goldenChunkOptions are the chunk settings every golden test uses. They match
// the production defaults in NewChunker so the golden corpus is chunked exactly
// the way a real ingest would chunk it. Do not tune these to make an assertion
// pass -- if chunking changes, the golden expectations are supposed to move.
var goldenChunkOptions = ChunkOptions{MaxSize: 1000, Overlap: 100}

// corpusDoc is one file from internal/kb/testdata/corpus.
type corpusDoc struct {
	DocumentID string // stable id derived from the file name, e.g. "01-gettysburg-address"
	FileName   string
	Title      string // first line of the file
	Content    string
}

// loadGoldenCorpus reads testdata/corpus in sorted file-name order. The order is
// part of the golden contract: SQLite hands back ties in rowid order, so a
// different insertion order can reshuffle equally scored results.
func loadGoldenCorpus(t *testing.T) []corpusDoc {
	t.Helper()

	dir := filepath.Join("testdata", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read corpus dir: %v", err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	if len(names) == 0 {
		t.Fatalf("golden corpus is empty: %s", dir)
	}

	docs := make([]corpusDoc, 0, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read corpus file %s: %v", name, err)
		}
		content := strings.ReplaceAll(string(raw), "\r\n", "\n")
		title := content
		if idx := strings.Index(content, "\n"); idx >= 0 {
			title = content[:idx]
		}
		docs = append(docs, corpusDoc{
			DocumentID: strings.TrimSuffix(name, ".txt"),
			FileName:   name,
			Title:      strings.TrimSpace(title),
			Content:    content,
		})
	}
	return docs
}

// newTestDB opens a throwaway SQLite database with the full Conduit schema.
// It skips (rather than fails) when the binary was built without FTS5, which is
// exactly what the integration suite does -- CI asserts separately that a real
// FTS5 build is used, so a skip here can never hide a broken release build.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "golden.db")
	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping (build with CGO_ENABLED=1 -tags fts5)")
		}
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return st.DB()
}

// goldenIndex is a fully ingested golden corpus plus the searchers under test.
type goldenIndex struct {
	DB       *sql.DB
	Docs     []corpusDoc
	Chunker  *Chunker
	Indexer  *Indexer
	Searcher *Searcher
	// Hybrid has a nil semantic searcher: everything hermetic runs lexical-only.
	Hybrid *HybridSearcher
	// ChunkCount maps document id -> number of chunks that were indexed.
	ChunkCount map[string]int
}

// ingestGoldenCorpus runs the real ingestion path -- source registration,
// chunking, FTS5 indexing -- over the committed corpus. No network, no Ollama,
// no Qdrant: the semantic searcher is deliberately nil.
func ingestGoldenCorpus(t *testing.T) *goldenIndex {
	t.Helper()

	db := newTestDB(t)
	ctx := context.Background()

	sources := NewSourceManager(db)
	src, err := sources.Add(ctx, AddSourceRequest{
		Path:     t.TempDir(),
		Name:     "golden-corpus",
		SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("add source: %v", err)
	}

	gi := &goldenIndex{
		DB:         db,
		Docs:       loadGoldenCorpus(t),
		Chunker:    NewChunker(),
		Indexer:    NewIndexer(db),
		Searcher:   NewSearcher(db),
		ChunkCount: make(map[string]int),
	}
	gi.Hybrid = NewHybridSearcher(gi.Searcher, nil)

	// A fixed modification timestamp keeps the indexed rows byte-identical
	// between runs; nothing in retrieval reads it, but a moving value would
	// make any future row-level golden file flap.
	modified := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, doc := range gi.Docs {
		chunks := gi.Chunker.Chunk(doc.Content, goldenChunkOptions)
		if len(chunks) == 0 {
			t.Fatalf("corpus document %s produced no chunks", doc.DocumentID)
		}
		gi.ChunkCount[doc.DocumentID] = len(chunks)

		d := &Document{
			DocumentID: doc.DocumentID,
			SourceID:   src.SourceID,
			Path:       "/corpus/" + doc.FileName,
			Title:      doc.Title,
			MimeType:   "text/plain",
			Size:       int64(len(doc.Content)),
			ModifiedAt: modified,
		}
		if err := gi.Indexer.Index(ctx, d, chunks); err != nil {
			t.Fatalf("index %s: %v", doc.DocumentID, err)
		}
	}

	return gi
}

// docIDs projects hits down to their document ids, preserving rank order.
func docIDs(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.DocumentID)
	}
	return out
}

// uniqueDocIDs projects hits to document ids in rank order, dropping repeats.
// Several corpus documents chunk into more than one row, so a query can return
// the same document twice; the golden expectations are written per document.
func uniqueDocIDs(hits []SearchHit) []string {
	seen := make(map[string]bool, len(hits))
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		if seen[h.DocumentID] {
			continue
		}
		seen[h.DocumentID] = true
		out = append(out, h.DocumentID)
	}
	return out
}

// equalStrings reports whether two string slices match element for element.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sortedCopy returns a sorted copy, for set comparisons where rank is not the
// thing under test.
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
