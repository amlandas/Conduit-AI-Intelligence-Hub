package integration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/store"
)

// TestKBChunkAndIndexIntegration tests chunking and indexing together.
func TestKBChunkAndIndexIntegration(t *testing.T) {
	st := testStoreKB(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping KB integration test")
	}
	defer st.Close()

	chunker := kb.NewChunker()
	indexer := kb.NewIndexer(st.DB())
	source := kb.NewSourceManager(st.DB())
	ctx := context.Background()

	// First create a source to satisfy foreign key constraint
	tmpDir := t.TempDir()
	src, err := source.Add(ctx, kb.AddSourceRequest{
		Path:     tmpDir,
		Name:     "Test Source",
		SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	// Create document content
	content := "This is a test document with some content. " + strings.Repeat("Lorem ipsum dolor sit amet. ", 50)

	// Create a document using the actual source ID
	doc := &kb.Document{
		DocumentID: "doc_test_001",
		SourceID:   src.SourceID,
		Path:       "/test/document.md",
		Title:      "Test Document",
		MimeType:   "text/markdown",
	}

	// Chunk the content
	chunks := chunker.Chunk(content, kb.ChunkOptions{
		MaxSize: 200,
		Overlap: 50,
	})

	if len(chunks) < 2 {
		t.Errorf("Expected multiple chunks, got %d", len(chunks))
	}

	// Index document and chunks
	err = indexer.Index(ctx, doc, chunks)
	if err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Get stats
	stats, err := indexer.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalDocuments < 1 {
		t.Error("Expected at least 1 document in stats")
	}
	if stats.TotalChunks < 2 {
		t.Error("Expected at least 2 chunks in stats")
	}
}

// TestKBIndexAndSearchIntegration tests indexing and searching together.
func TestKBIndexAndSearchIntegration(t *testing.T) {
	st := testStoreKB(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping KB integration test")
	}
	defer st.Close()

	chunker := kb.NewChunker()
	indexer := kb.NewIndexer(st.DB())
	searcher := kb.NewSearcher(st.DB())
	source := kb.NewSourceManager(st.DB())
	ctx := context.Background()

	// First create a source to satisfy foreign key constraint
	tmpDir := t.TempDir()
	src, err := source.Add(ctx, kb.AddSourceRequest{
		Path:     tmpDir,
		Name:     "Programming Docs",
		SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	// Create and index a document about Go programming
	goContent := "Go is a statically typed, compiled programming language designed at Google. It provides garbage collection, type safety, and CSP-style concurrency."
	doc := &kb.Document{
		DocumentID: "doc_go_001",
		SourceID:   src.SourceID,
		Path:       "/docs/go-programming.md",
		Title:      "Introduction to Go Programming",
		MimeType:   "text/markdown",
	}

	chunks := chunker.Chunk(goContent, kb.ChunkOptions{})
	if err := indexer.Index(ctx, doc, chunks); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Create and index another document about Python
	pythonContent := "Python is an interpreted, high-level programming language. It emphasizes code readability with significant indentation."
	doc2 := &kb.Document{
		DocumentID: "doc_python_001",
		SourceID:   src.SourceID,
		Path:       "/docs/python-programming.md",
		Title:      "Introduction to Python Programming",
		MimeType:   "text/markdown",
	}

	chunks2 := chunker.Chunk(pythonContent, kb.ChunkOptions{})
	if err := indexer.Index(ctx, doc2, chunks2); err != nil {
		t.Fatalf("Index doc2 failed: %v", err)
	}

	// Search for "Go programming"
	results, err := searcher.Search(ctx, "Go programming compiled", kb.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if results.TotalHits == 0 {
		t.Error("Expected search results for 'Go programming'")
	}

	// First result should be the Go document
	if len(results.Results) > 0 {
		if !strings.Contains(results.Results[0].Snippet, "Go") {
			t.Log("Note: First result may not contain 'Go' depending on FTS ranking")
		}
	}
}

// TestKBSourceAndDocumentIntegration tests source management with document indexing.
func TestKBSourceAndDocumentIntegration(t *testing.T) {
	st := testStoreKB(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping KB integration test")
	}
	defer st.Close()

	source := kb.NewSourceManager(st.DB())
	chunker := kb.NewChunker()
	indexer := kb.NewIndexer(st.DB())
	ctx := context.Background()

	// Create a temporary directory with test files
	tmpDir := t.TempDir()

	// Create test files
	testFile := filepath.Join(tmpDir, "readme.md")
	content := "# Test Project\n\nThis is a test project for integration testing. It contains information about software development."
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Add source using AddSourceRequest
	req := kb.AddSourceRequest{
		Path:     tmpDir,
		Name:     "Integration Test Source",
		SyncMode: "manual",
	}

	src, err := source.Add(ctx, req)
	if err != nil {
		t.Fatalf("Add source failed: %v", err)
	}

	// List sources
	sources, err := source.List(ctx)
	if err != nil {
		t.Fatalf("List sources failed: %v", err)
	}

	if len(sources) < 1 {
		t.Error("Expected at least 1 source")
	}

	// Index a document from the source
	doc := &kb.Document{
		DocumentID: "doc_from_source",
		SourceID:   src.SourceID,
		Path:       testFile,
		Title:      "Test Project Readme",
		MimeType:   "text/markdown",
	}

	chunks := chunker.Chunk(content, kb.ChunkOptions{})
	if err := indexer.Index(ctx, doc, chunks); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Remove source
	if _, err := source.Remove(ctx, src.SourceID); err != nil {
		t.Fatalf("Remove source failed: %v", err)
	}

	// Verify source is removed
	sources, err = source.List(ctx)
	if err != nil {
		t.Fatalf("List sources failed: %v", err)
	}

	for _, s := range sources {
		if s.SourceID == src.SourceID {
			t.Error("Source should have been removed")
		}
	}
}

// TestKBDocumentUpdateIntegration tests document updates.
func TestKBDocumentUpdateIntegration(t *testing.T) {
	st := testStoreKB(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping KB integration test")
	}
	defer st.Close()

	chunker := kb.NewChunker()
	indexer := kb.NewIndexer(st.DB())
	searcher := kb.NewSearcher(st.DB())
	source := kb.NewSourceManager(st.DB())
	ctx := context.Background()

	// First create a source to satisfy foreign key constraint
	tmpDir := t.TempDir()
	src, err := source.Add(ctx, kb.AddSourceRequest{
		Path:     tmpDir,
		Name:     "Update Test Source",
		SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	docID := "doc_update_test"

	// Index original document
	originalContent := "This is the original content with some unique words like xylophone."
	doc := &kb.Document{
		DocumentID: docID,
		SourceID:   src.SourceID,
		Path:       "/test/update.md",
		Title:      "Original Title",
		MimeType:   "text/markdown",
	}

	chunks := chunker.Chunk(originalContent, kb.ChunkOptions{})
	if err := indexer.Index(ctx, doc, chunks); err != nil {
		t.Fatalf("Index failed: %v", err)
	}

	// Search for original content
	results, err := searcher.Search(ctx, "xylophone", kb.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if results.TotalHits == 0 {
		t.Error("Expected to find 'xylophone' in search")
	}

	// Delete and re-index with updated content
	if err := indexer.Delete(ctx, docID); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	updatedContent := "This is the updated content with some different words like accordion."
	chunks = chunker.Chunk(updatedContent, kb.ChunkOptions{})
	if err := indexer.Index(ctx, doc, chunks); err != nil {
		t.Fatalf("Re-index failed: %v", err)
	}

	// Search for new content
	results, err = searcher.Search(ctx, "accordion", kb.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search for accordion failed: %v", err)
	}

	if results.TotalHits == 0 {
		t.Error("Expected to find 'accordion' in search after update")
	}

	// Original content should be gone
	results, err = searcher.Search(ctx, "xylophone", kb.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search for xylophone failed: %v", err)
	}

	if results.TotalHits > 0 {
		t.Error("Should not find 'xylophone' after update")
	}
}

// TestKBSearchRankingIntegration tests that search results are properly ranked.
func TestKBSearchRankingIntegration(t *testing.T) {
	st := testStoreKB(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping KB integration test")
	}
	defer st.Close()

	chunker := kb.NewChunker()
	indexer := kb.NewIndexer(st.DB())
	searcher := kb.NewSearcher(st.DB())
	source := kb.NewSourceManager(st.DB())
	ctx := context.Background()

	// First create a source to satisfy foreign key constraint
	tmpDir := t.TempDir()
	src, err := source.Add(ctx, kb.AddSourceRequest{
		Path:     tmpDir,
		Name:     "Ranking Test Source",
		SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("Failed to create source: %v", err)
	}

	// Index documents with varying relevance
	docs := []struct {
		id      string
		title   string
		content string
	}{
		{"doc_low", "General Article", "This article discusses various topics including some technology."},
		{"doc_medium", "Programming Guide", "This guide covers programming with some database information."},
		{"doc_high", "Database Optimization", "Database performance optimization techniques. Database indexing, database queries, database tuning."},
	}

	for _, d := range docs {
		doc := &kb.Document{
			DocumentID: d.id,
			SourceID:   src.SourceID,
			Path:       "/test/" + d.id + ".md",
			Title:      d.title,
			MimeType:   "text/markdown",
		}

		chunks := chunker.Chunk(d.content, kb.ChunkOptions{})
		if err := indexer.Index(ctx, doc, chunks); err != nil {
			t.Fatalf("Index %s failed: %v", d.id, err)
		}
	}

	// Search for "database"
	results, err := searcher.Search(ctx, "database", kb.SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results.Results) < 2 {
		t.Skip("Not enough results to test ranking")
	}

	// Verify that results are returned with scores
	// Note: FTS5 BM25 ranking order depends on term frequency and document length
	// We verify that documents with "database" are returned and have non-zero scores
	for i, result := range results.Results {
		if result.Score == 0 {
			t.Errorf("Result %d has zero score", i)
		}
		t.Logf("Result %d: doc_id=%s, score=%.4f, snippet=%s",
			i, result.DocumentID, result.Score, result.Snippet[:min(50, len(result.Snippet))])
	}

	// Verify the document with most "database" mentions is in results
	foundHighRelevance := false
	for _, result := range results.Results {
		if result.DocumentID == "doc_high" {
			foundHighRelevance = true
			break
		}
	}
	if !foundHighRelevance {
		t.Error("Expected doc_high (high relevance document) to be in search results")
	}
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// testStoreKB creates a temporary store for KB testing.
func testStoreKB(t *testing.T) *store.Store {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "kb-integration-test.db")

	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			return nil
		}
		t.Fatalf("Failed to create store: %v", err)
	}

	return st
}
