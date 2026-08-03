package kb

// Hermetic tests for the SQLite vector index (WP-2.1).
//
// Everything here runs against a real SQLite file with the real schema, using
// deterministic vectors generated from a fixed seed. No network, no Ollama, no
// container: the vectors are the point, not where they came from.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/store"
)

// ---------------------------------------------------------------------------
// Deterministic vector fixtures
// ---------------------------------------------------------------------------

// unitVector returns a dim-length vector that is 1 at index one and 0 elsewhere.
// Basis vectors make similarity assertions obvious: a query equal to a basis
// vector has cosine 1 against it and 0 against every other one.
func unitVector(dim, one int) []float32 {
	v := make([]float32, dim)
	v[one] = 1
	return v
}

// seededVector returns a reproducible pseudo-random unit vector. The seed is
// part of the contract: the same seed must produce the same vector on every
// machine and every run, so a ranking assertion cannot flake.
func seededVector(dim int, seed int64) []float32 {
	rng := rand.New(rand.NewSource(seed))
	v := make([]float32, dim)
	var norm float64
	for i := range v {
		v[i] = float32(rng.NormFloat64())
		norm += float64(v[i]) * float64(v[i])
	}
	inv := float32(1 / math.Sqrt(norm))
	for i := range v {
		v[i] *= inv
	}
	return v
}

// blendedVector mixes two vectors so a test can place a point at a known
// similarity between two poles.
func blendedVector(a, b []float32, weight float32) []float32 {
	out := make([]float32, len(a))
	for i := range a {
		out[i] = a[i]*(1-weight) + b[i]*weight
	}
	return out
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// newVectorIndex opens a throwaway database and a vector index over it.
func newVectorIndex(t *testing.T, dim int) (*sql.DB, *SQLiteVectorIndex) {
	t.Helper()
	db := newTestDB(t)
	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	return db, vi
}

// seedChunk inserts the source/document/chunk rows a vector points at, so that
// the enrichment join has something to find.
func seedChunk(t *testing.T, db *sql.DB, sourceID, docID, chunkID, path, title, content string) {
	t.Helper()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT OR IGNORE INTO kb_sources (source_id, path, name)
		VALUES (?, ?, ?)`, sourceID, "/src/"+sourceID, sourceID)
	if err != nil {
		t.Fatalf("insert source: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT OR IGNORE INTO kb_documents
			(document_id, source_id, path, title, mime_type, size, indexed_at)
		VALUES (?, ?, ?, ?, 'text/plain', 0, datetime('now'))`,
		docID, sourceID, path, title)
	if err != nil {
		t.Fatalf("insert document: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT OR IGNORE INTO kb_chunks (chunk_id, document_id, chunk_index, content)
		VALUES (?, ?, 0, ?)`, chunkID, docID, content)
	if err != nil {
		t.Fatalf("insert chunk: %v", err)
	}
}

// seedEntity inserts a KAG entity row so an entity vector can reference it.
func seedEntity(t *testing.T, db *sql.DB, entityID, name, entityType string, confidence float64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT OR IGNORE INTO kb_entities (entity_id, name, type, confidence)
		VALUES (?, ?, ?, ?)`, entityID, name, entityType, confidence)
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// TestVectorIndex_ConstructibleWhenEmpty pins the property WP-1.1 could not get
// from the old backend: an index over a database with no data must construct.
// The whole hermetic suite depends on it.
func TestVectorIndex_ConstructibleWhenEmpty(t *testing.T) {
	db, vi := newVectorIndex(t, 8)
	ctx := context.Background()

	if err := vi.HealthCheck(ctx); err != nil {
		t.Errorf("HealthCheck on an empty index: %v", err)
	}

	stats, err := vi.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.VectorCount != 0 {
		t.Errorf("VectorCount = %d, want 0", stats.VectorCount)
	}

	res, err := vi.Search(ctx, unitVector(8, 0), VectorSearchOptions{Limit: 5})
	if err != nil {
		t.Errorf("Search on an empty index should succeed, got %v", err)
	}
	if len(res) != 0 {
		t.Errorf("Search on an empty index returned %d results", len(res))
	}

	_ = db
}

func TestVectorIndex_RequiresDatabase(t *testing.T) {
	if _, err := NewSQLiteVectorIndex(nil, VectorIndexConfig{}); err == nil {
		t.Error("NewSQLiteVectorIndex(nil) should fail: there is nothing to store into")
	}
}

// ---------------------------------------------------------------------------
// Upsert / search / delete
// ---------------------------------------------------------------------------

func TestVectorIndex_UpsertSearchDelete(t *testing.T) {
	const dim = 8
	db, vi := newVectorIndex(t, dim)
	ctx := context.Background()

	seedChunk(t, db, "src_one", "doc_a", "chunk_a", "/corpus/a.txt", "A", "alpha")
	seedChunk(t, db, "src_two", "doc_b", "chunk_b", "/corpus/b.txt", "B", "beta")

	points := []VectorPoint{
		{
			ID: "chunk_a", Vector: unitVector(dim, 0), DocumentID: "doc_a", ChunkIndex: 0,
			Path: "/corpus/a.txt", Title: "A", Content: "alpha",
			Metadata: map[string]string{"source_id": "src_one"},
		},
		{
			ID: "chunk_b", Vector: unitVector(dim, 1), DocumentID: "doc_b", ChunkIndex: 0,
			Path: "/corpus/b.txt", Title: "B", Content: "beta",
			Metadata: map[string]string{"source_id": "src_two"},
		},
	}
	if err := vi.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	t.Run("nearest neighbour is the matching basis vector", func(t *testing.T) {
		res, err := vi.Search(ctx, unitVector(dim, 0), VectorSearchOptions{Limit: 2})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res) == 0 {
			t.Fatal("expected results")
		}
		if res[0].ID != "chunk_a" {
			t.Errorf("nearest neighbour: got %s, want chunk_a", res[0].ID)
		}
		// Cosine similarity is higher-is-better -- the opposite sign convention
		// from SQLite bm25. The fusion layer only uses ranks, so the mismatch is
		// invisible today, and this pins that it stays that way.
		if res[0].Score <= 0 {
			t.Errorf("expected a positive cosine similarity, got %v", res[0].Score)
		}
		if math.Abs(float64(res[0].Score)-1.0) > 1e-6 {
			t.Errorf("self-similarity should be 1, got %v", res[0].Score)
		}
	})

	t.Run("results carry the chunk text and document metadata", func(t *testing.T) {
		res, err := vi.Search(ctx, unitVector(dim, 0), VectorSearchOptions{Limit: 1})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		hit := res[0]
		if hit.Content != "alpha" {
			t.Errorf("Content = %q, want alpha", hit.Content)
		}
		if hit.Path != "/corpus/a.txt" {
			t.Errorf("Path = %q", hit.Path)
		}
		if hit.Title != "A" {
			t.Errorf("Title = %q", hit.Title)
		}
		if hit.DocumentID != "doc_a" {
			t.Errorf("DocumentID = %q", hit.DocumentID)
		}
		if hit.Metadata["source_id"] != "src_one" {
			t.Errorf("metadata source_id = %q", hit.Metadata["source_id"])
		}
		if hit.Metadata["mime_type"] != "text/plain" {
			t.Errorf("metadata mime_type = %q", hit.Metadata["mime_type"])
		}
	})

	t.Run("source filter narrows the result set", func(t *testing.T) {
		res, err := vi.Search(ctx, unitVector(dim, 0), VectorSearchOptions{
			Limit: 10, SourceIDs: []string{"src_two"},
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res) != 1 {
			t.Fatalf("got %d results, want 1", len(res))
		}
		if res[0].ID != "chunk_b" {
			t.Errorf("source filter leaked %s", res[0].ID)
		}
	})

	t.Run("upsert replaces rather than duplicates", func(t *testing.T) {
		updated := points[0]
		updated.Vector = unitVector(dim, 2)
		if err := vi.Upsert(ctx, []VectorPoint{updated}); err != nil {
			t.Fatalf("Upsert: %v", err)
		}

		stats, err := vi.GetStats(ctx)
		if err != nil {
			t.Fatalf("GetStats: %v", err)
		}
		if stats.VectorCount != 2 {
			t.Errorf("VectorCount = %d after re-upsert, want 2", stats.VectorCount)
		}

		res, err := vi.Search(ctx, unitVector(dim, 2), VectorSearchOptions{Limit: 1})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res) == 0 || res[0].ID != "chunk_a" {
			t.Errorf("re-upserted vector was not used for ranking")
		}

		// Put it back for the deletion subtest.
		if err := vi.Upsert(ctx, []VectorPoint{points[0]}); err != nil {
			t.Fatalf("Upsert restore: %v", err)
		}
	})

	t.Run("delete by document removes only that document", func(t *testing.T) {
		if err := vi.DeleteByDocument(ctx, "doc_a"); err != nil {
			t.Fatalf("DeleteByDocument: %v", err)
		}
		res, err := vi.Search(ctx, unitVector(dim, 0), VectorSearchOptions{Limit: 10})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		for _, r := range res {
			if r.ID == "chunk_a" {
				t.Errorf("chunk_a survived DeleteByDocument")
			}
		}
		if len(res) != 1 || res[0].ID != "chunk_b" {
			t.Errorf("DeleteByDocument removed too much: %+v", res)
		}
	})

	t.Run("delete by source reports how many rows went away", func(t *testing.T) {
		n, err := vi.DeleteBySource(ctx, "src_two")
		if err != nil {
			t.Fatalf("DeleteBySource: %v", err)
		}
		if n != 1 {
			t.Errorf("DeleteBySource deleted %d, want 1", n)
		}
		stats, _ := vi.GetStats(ctx)
		if stats.VectorCount != 0 {
			t.Errorf("VectorCount = %d after deleting every source", stats.VectorCount)
		}
	})
}

// TestVectorIndex_RejectsWrongDimension pins that an embedding-model swap is a
// loud error rather than silently unusable results.
func TestVectorIndex_RejectsWrongDimension(t *testing.T) {
	db, vi := newVectorIndex(t, 8)
	ctx := context.Background()
	seedChunk(t, db, "src", "doc", "chunk", "/p", "T", "c")

	err := vi.Upsert(ctx, []VectorPoint{{
		ID: "chunk", Vector: unitVector(4, 0), DocumentID: "doc",
		Metadata: map[string]string{"source_id": "src"},
	}})
	if err == nil {
		t.Fatal("a 4-dim vector in an 8-dim index should be rejected")
	}
	if !strings.Contains(err.Error(), "dimension") {
		t.Errorf("error should name the dimension mismatch, got: %v", err)
	}
}

// TestVectorIndex_RankingIsExact checks the ordering over a larger set of
// deterministic vectors, including that the source filter does not cost recall.
// An approximate index that post-filtered would fail the second half of this.
func TestVectorIndex_RankingIsExact(t *testing.T) {
	const dim = 32
	db, vi := newVectorIndex(t, dim)
	ctx := context.Background()

	target := seededVector(dim, 1)
	far := seededVector(dim, 2)

	// 40 points. Only every 10th belongs to the rare source, and the closest
	// rare-source point is deliberately ranked below many common-source ones.
	var points []VectorPoint
	for i := 0; i < 40; i++ {
		sourceID := "src_common"
		if i%10 == 0 {
			sourceID = "src_rare"
		}
		docID := fmt.Sprintf("doc_%d", i)
		chunkID := fmt.Sprintf("chunk_%d", i)
		seedChunk(t, db, sourceID, docID, chunkID, "/p/"+chunkID, chunkID, "content "+chunkID)

		// Blend from target towards far as i grows: rank should follow i.
		weight := float32(i) / 40.0
		points = append(points, VectorPoint{
			ID: chunkID, Vector: blendedVector(target, far, weight),
			DocumentID: docID, ChunkIndex: 0,
			Metadata: map[string]string{"source_id": sourceID},
		})
	}
	if err := vi.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	t.Run("scores decrease monotonically with rank", func(t *testing.T) {
		res, err := vi.Search(ctx, target, VectorSearchOptions{Limit: 10})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res) != 10 {
			t.Fatalf("got %d results, want 10", len(res))
		}
		if res[0].ID != "chunk_0" {
			t.Errorf("top hit = %s, want chunk_0", res[0].ID)
		}
		for i := 1; i < len(res); i++ {
			if res[i].Score > res[i-1].Score {
				t.Errorf("rank %d scored higher than rank %d: %v > %v",
					i, i-1, res[i].Score, res[i-1].Score)
			}
		}
	})

	t.Run("a selective source filter still returns every match", func(t *testing.T) {
		res, err := vi.Search(ctx, target, VectorSearchOptions{
			Limit: 10, SourceIDs: []string{"src_rare"},
		})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		// 4 of the 40 points belong to src_rare. Post-filtering a top-10 KNN
		// would find only 1; exact pre-filtering finds all 4.
		if len(res) != 4 {
			t.Errorf("got %d rare-source results, want 4 -- filter is costing recall", len(res))
		}
		for _, r := range res {
			if r.Metadata["source_id"] != "src_rare" {
				t.Errorf("source filter leaked %s from %s", r.ID, r.Metadata["source_id"])
			}
		}
	})

	t.Run("offset pages through the ranking", func(t *testing.T) {
		first, err := vi.Search(ctx, target, VectorSearchOptions{Limit: 3})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		second, err := vi.Search(ctx, target, VectorSearchOptions{Limit: 3, Offset: 3})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(second) != 3 {
			t.Fatalf("offset page returned %d results", len(second))
		}
		for _, a := range first {
			for _, b := range second {
				if a.ID == b.ID {
					t.Errorf("%s appears on both pages", a.ID)
				}
			}
		}
		if second[0].Score > first[len(first)-1].Score {
			t.Errorf("page 2 outranks page 1")
		}
	})

	t.Run("min score drops weak matches", func(t *testing.T) {
		all, err := vi.Search(ctx, target, VectorSearchOptions{Limit: 40})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(all) < 5 {
			t.Fatalf("expected a full result set, got %d", len(all))
		}
		floor := float64(all[2].Score)
		filtered, err := vi.Search(ctx, target, VectorSearchOptions{Limit: 40, MinScore: floor})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(filtered) == 0 || len(filtered) >= len(all) {
			t.Errorf("MinScore %v kept %d of %d results", floor, len(filtered), len(all))
		}
		for _, r := range filtered {
			if float64(r.Score) < floor {
				t.Errorf("%s scored %v, below the floor %v", r.ID, r.Score, floor)
			}
		}
	})
}

// TestVectorIndex_EncodingRoundTrip checks the BLOB format, including the
// unsafe zero-copy view that the scan relies on for its performance budget.
func TestVectorIndex_EncodingRoundTrip(t *testing.T) {
	original := []float32{0, 1, -1, 0.5, -0.25, 3.14159, 1e-8, -1e8}
	blob := encodeVector(original)

	if len(blob) != len(original)*4 {
		t.Fatalf("encoded %d floats into %d bytes", len(original), len(blob))
	}

	decoded := decodeVector(blob)
	if len(decoded) != len(original) {
		t.Fatalf("decoded %d floats, want %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Errorf("index %d: got %v, want %v", i, decoded[i], original[i])
		}
	}

	// The zero-copy view must agree with the copying decoder, or the fast path
	// and the fallback path would rank differently.
	if view, ok := viewFloat32(blob); ok {
		for i := range original {
			if view[i] != original[i] {
				t.Errorf("zero-copy view disagrees at index %d: %v vs %v", i, view[i], original[i])
			}
		}
	}
}

func TestVectorIndex_CosineSimilarity(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1},
		{"orthogonal", []float32{1, 0, 0}, []float32{0, 1, 0}, 0},
		{"opposite", []float32{1, 0, 0}, []float32{-1, 0, 0}, -1},
		{"scale invariant", []float32{1, 1, 0}, []float32{5, 5, 0}, 1},
		{"zero vector", []float32{0, 0, 0}, []float32{1, 0, 0}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cosineSimilarity(tc.a, tc.b)
			if math.Abs(float64(got-tc.want)) > 1e-6 {
				t.Errorf("cosineSimilarity = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVectorIndex_ZeroQueryVector pins that a degenerate query returns nothing
// rather than an arbitrary ordering produced by dividing by zero.
func TestVectorIndex_ZeroQueryVector(t *testing.T) {
	db, vi := newVectorIndex(t, 4)
	ctx := context.Background()
	seedChunk(t, db, "src", "doc", "chunk", "/p", "T", "c")
	if err := vi.Upsert(ctx, []VectorPoint{{
		ID: "chunk", Vector: unitVector(4, 0), DocumentID: "doc",
		Metadata: map[string]string{"source_id": "src"},
	}}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	res, err := vi.Search(ctx, []float32{0, 0, 0, 0}, VectorSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search with a zero query should not error: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("a zero query vector should rank nothing, got %d results", len(res))
	}
}

// ---------------------------------------------------------------------------
// Entity vectors (KAG)
// ---------------------------------------------------------------------------

func TestVectorIndex_EntityVectors(t *testing.T) {
	const dim = 8
	db, vi := newVectorIndex(t, dim)
	ctx := context.Background()

	// Entity vectors carry a foreign key onto kb_entities so that deleting an
	// entity reclaims its vector; the rows have to exist first.
	seedEntity(t, db, "ent_a", "Alpha", "concept", 0.9)
	seedEntity(t, db, "ent_b", "Beta", "person", 0.7)

	points := []EntityVectorPoint{
		{ID: "ent_a", Vector: unitVector(dim, 0), Name: "Alpha", Type: "concept", Confidence: 0.9},
		{ID: "ent_b", Vector: unitVector(dim, 1), Name: "Beta", Type: "person", Confidence: 0.7},
	}
	if err := vi.UpsertEntityBatch(ctx, points); err != nil {
		t.Fatalf("UpsertEntityBatch: %v", err)
	}

	res, err := vi.SearchEntities(ctx, unitVector(dim, 0), VectorEntitySearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(res) == 0 || res[0].ID != "ent_a" {
		t.Fatalf("nearest entity = %+v, want ent_a first", res)
	}
	if res[0].Name != "Alpha" || res[0].Confidence != 0.9 {
		t.Errorf("entity payload not round-tripped: %+v", res[0])
	}

	typed, err := vi.SearchEntities(ctx, unitVector(dim, 0), VectorEntitySearchOptions{
		Limit: 5, EntityType: "person",
	})
	if err != nil {
		t.Fatalf("SearchEntities: %v", err)
	}
	if len(typed) != 1 || typed[0].ID != "ent_b" {
		t.Errorf("entity type filter returned %+v", typed)
	}

	if err := vi.DeleteEntityByID(ctx, "ent_a"); err != nil {
		t.Fatalf("DeleteEntityByID: %v", err)
	}
	stats, err := vi.GetEntityStats(ctx)
	if err != nil {
		t.Fatalf("GetEntityStats: %v", err)
	}
	if stats.VectorCount != 1 {
		t.Errorf("entity count = %d after delete, want 1", stats.VectorCount)
	}
}

// ---------------------------------------------------------------------------
// Transactional ingestion
// ---------------------------------------------------------------------------

// TestIngestion_SingleTransaction is the WP-2.1 atomicity contract: chunk text,
// FTS rows and vectors either all land or none do.
func TestIngestion_SingleTransaction(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	sources := NewSourceManager(db)
	src, err := sources.Add(ctx, AddSourceRequest{Path: t.TempDir(), Name: "tx", SyncMode: "manual"})
	if err != nil {
		t.Fatalf("add source: %v", err)
	}

	const dim = 16
	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}

	doc := &Document{
		DocumentID: "doc_tx", SourceID: src.SourceID, Path: "/corpus/tx.txt",
		Title: "Transaction", MimeType: "text/plain", ModifiedAt: time.Now(),
	}
	chunks := []Chunk{
		{Index: 0, Content: "the first chunk of the transactional document"},
		{Index: 1, Content: "the second chunk of the transactional document"},
	}

	t.Run("a vector write failure rolls back the whole document", func(t *testing.T) {
		// An embedder that returns the wrong width makes the vector write fail
		// inside the transaction, after the chunk rows have been inserted.
		bad := &fakeEmbedder{dim: dim, produce: func(int) []float32 { return unitVector(dim/2, 0) }}
		semantic := NewSemanticSearcherWith(db, bad, vi)

		idx := NewIndexer(db)
		idx.SetSemanticSearcher(semantic)

		if err := idx.Index(ctx, doc, chunks); err == nil {
			t.Fatal("indexing should fail when the vector write fails")
		}

		assertCount(t, db, `SELECT COUNT(*) FROM kb_documents WHERE document_id = 'doc_tx'`, 0,
			"document row survived a rolled-back ingest")
		assertCount(t, db, `SELECT COUNT(*) FROM kb_chunks WHERE document_id = 'doc_tx'`, 0,
			"chunk rows survived a rolled-back ingest")
		assertCount(t, db, `SELECT COUNT(*) FROM kb_fts WHERE document_id = 'doc_tx'`, 0,
			"FTS rows survived a rolled-back ingest")
		assertCount(t, db, `SELECT COUNT(*) FROM kb_vectors WHERE document_id = 'doc_tx'`, 0,
			"vector rows survived a rolled-back ingest")
	})

	t.Run("a successful ingest lands text, FTS and vectors together", func(t *testing.T) {
		good := &fakeEmbedder{dim: dim, produce: func(i int) []float32 { return seededVector(dim, int64(i+1)) }}
		semantic := NewSemanticSearcherWith(db, good, vi)

		idx := NewIndexer(db)
		idx.SetSemanticSearcher(semantic)

		if err := idx.Index(ctx, doc, chunks); err != nil {
			t.Fatalf("Index: %v", err)
		}

		assertCount(t, db, `SELECT COUNT(*) FROM kb_chunks WHERE document_id = 'doc_tx'`, 2, "chunk rows")
		assertCount(t, db, `SELECT COUNT(*) FROM kb_fts WHERE document_id = 'doc_tx'`, 2, "FTS rows")
		assertCount(t, db, `SELECT COUNT(*) FROM kb_vectors WHERE document_id = 'doc_tx'`, 2, "vector rows")

		// Every vector must key onto a chunk that actually exists.
		assertCount(t, db, `
			SELECT COUNT(*) FROM kb_vectors v
			LEFT JOIN kb_chunks c ON c.chunk_id = v.chunk_id
			WHERE c.chunk_id IS NULL`, 0, "orphaned vector rows")
	})

	t.Run("re-indexing replaces vectors instead of accumulating them", func(t *testing.T) {
		good := &fakeEmbedder{dim: dim, produce: func(i int) []float32 { return seededVector(dim, int64(i+1)) }}
		semantic := NewSemanticSearcherWith(db, good, vi)
		idx := NewIndexer(db)
		idx.SetSemanticSearcher(semantic)

		if err := idx.Index(ctx, doc, chunks); err != nil {
			t.Fatalf("re-Index: %v", err)
		}
		assertCount(t, db, `SELECT COUNT(*) FROM kb_vectors WHERE document_id = 'doc_tx'`, 2,
			"vector rows after re-index")
	})

	t.Run("deleting a document removes its vectors", func(t *testing.T) {
		idx := NewIndexer(db)
		if err := idx.Delete(ctx, "doc_tx"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		assertCount(t, db, `SELECT COUNT(*) FROM kb_vectors WHERE document_id = 'doc_tx'`, 0,
			"vector rows after delete")
	})
}

// TestIngestion_EmbeddingFailureStillIndexesText pins the graceful-degradation
// contract: if Ollama is down, lexical ingestion must still succeed.
func TestIngestion_EmbeddingFailureStillIndexesText(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	sources := NewSourceManager(db)
	src, err := sources.Add(ctx, AddSourceRequest{Path: t.TempDir(), Name: "degraded", SyncMode: "manual"})
	if err != nil {
		t.Fatalf("add source: %v", err)
	}

	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: 16})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	semantic := NewSemanticSearcherWith(db, &fakeEmbedder{dim: 16, err: errEmbedderDown}, vi)

	idx := NewIndexer(db)
	idx.SetSemanticSearcher(semantic)
	idx.ResetSemanticErrors()

	doc := &Document{
		DocumentID: "doc_nolm", SourceID: src.SourceID, Path: "/corpus/nolm.txt",
		Title: "No Model", MimeType: "text/plain", ModifiedAt: time.Now(),
	}
	chunks := []Chunk{{Index: 0, Content: "text that must be searchable even without embeddings"}}

	if err := idx.Index(ctx, doc, chunks); err != nil {
		t.Fatalf("ingest must succeed without an embedder: %v", err)
	}

	assertCount(t, db, `SELECT COUNT(*) FROM kb_chunks WHERE document_id = 'doc_nolm'`, 1, "chunk rows")
	assertCount(t, db, `SELECT COUNT(*) FROM kb_fts WHERE document_id = 'doc_nolm'`, 1, "FTS rows")
	assertCount(t, db, `SELECT COUNT(*) FROM kb_vectors WHERE document_id = 'doc_nolm'`, 0, "vector rows")

	if n := idx.GetSemanticErrors(); n != 1 {
		t.Errorf("GetSemanticErrors = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// WAL concurrency
// ---------------------------------------------------------------------------

// TestVectorIndex_WALConcurrentReadDuringWrite covers the v2 concurrency story:
// N MCP processes plus the CLI share one file, so a reader on its own
// connection must keep serving during another connection's write.
//
// Two independent *sql.DB handles are used deliberately -- that is two separate
// pools, i.e. genuinely different SQLite connections, which is what separate
// processes would have. A single handle would prove nothing, because the pool
// serialises access.
func TestVectorIndex_WALConcurrentReadDuringWrite(t *testing.T) {
	const dim = 32

	dbPath := filepath.Join(t.TempDir(), "wal.db")

	writerStore, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping (build with CGO_ENABLED=1 -tags fts5)")
		}
		t.Fatalf("open writer store: %v", err)
	}
	defer writerStore.Close()

	readerStore, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("open reader store: %v", err)
	}
	defer readerStore.Close()

	writerDB, readerDB := writerStore.DB(), readerStore.DB()
	ctx := context.Background()

	writerIdx, err := NewSQLiteVectorIndex(writerDB, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("writer index: %v", err)
	}
	readerIdx, err := NewSQLiteVectorIndex(readerDB, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("reader index: %v", err)
	}

	// Seed a baseline so the reader always has something to find.
	const seeded = 50
	for i := 0; i < seeded; i++ {
		id := fmt.Sprintf("chunk_seed_%d", i)
		seedChunk(t, writerDB, "src_seed", fmt.Sprintf("doc_seed_%d", i), id, "/p/"+id, id, "seeded "+id)
	}
	var seedPoints []VectorPoint
	for i := 0; i < seeded; i++ {
		seedPoints = append(seedPoints, VectorPoint{
			ID:         fmt.Sprintf("chunk_seed_%d", i),
			Vector:     seededVector(dim, int64(1000+i)),
			DocumentID: fmt.Sprintf("doc_seed_%d", i),
			Metadata:   map[string]string{"source_id": "src_seed"},
		})
	}
	if err := writerIdx.Upsert(ctx, seedPoints); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}

	query := seededVector(dim, 1000)

	var wg sync.WaitGroup
	writeErr := make(chan error, 1)
	readErr := make(chan error, 1)
	done := make(chan struct{})

	// Writer: a stream of separate write transactions.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := 0; i < 40; i++ {
			id := fmt.Sprintf("chunk_w_%d", i)
			docID := fmt.Sprintf("doc_w_%d", i)
			seedChunk(t, writerDB, "src_w", docID, id, "/p/"+id, id, "written "+id)

			err := writerIdx.Upsert(ctx, []VectorPoint{{
				ID: id, Vector: seededVector(dim, int64(2000+i)), DocumentID: docID,
				Metadata: map[string]string{"source_id": "src_w"},
			}})
			if err != nil {
				writeErr <- fmt.Errorf("write %d: %w", i, err)
				return
			}
		}
		writeErr <- nil
	}()

	// Reader: keeps searching on its own connection for the whole write run.
	wg.Add(1)
	go func() {
		defer wg.Done()
		reads := 0
		for {
			select {
			case <-done:
				if reads == 0 {
					readErr <- fmt.Errorf("reader never completed a search during the write run")
					return
				}
				readErr <- nil
				return
			default:
			}

			res, err := readerIdx.Search(ctx, query, VectorSearchOptions{Limit: 5})
			if err != nil {
				readErr <- fmt.Errorf("read %d: %w", reads, err)
				return
			}
			if len(res) == 0 {
				readErr <- fmt.Errorf("read %d returned nothing; the seeded vectors should always be visible", reads)
				return
			}
			reads++
		}
	}()

	wg.Wait()

	if err := <-writeErr; err != nil {
		t.Errorf("writer: %v", err)
	}
	if err := <-readErr; err != nil {
		t.Errorf("reader: %v", err)
	}

	// The reader's connection must see everything the writer committed.
	stats, err := readerIdx.GetStats(ctx)
	if err != nil {
		t.Fatalf("reader GetStats: %v", err)
	}
	if stats.VectorCount != seeded+40 {
		t.Errorf("reader sees %d vectors, want %d", stats.VectorCount, seeded+40)
	}
}

// assertCount fails the test unless the scalar query returns want.
func assertCount(t *testing.T, db *sql.DB, query string, want int, what string) {
	t.Helper()
	var got int
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("%s: query failed: %v", what, err)
	}
	if got != want {
		t.Errorf("%s: got %d, want %d", what, got, want)
	}
}
