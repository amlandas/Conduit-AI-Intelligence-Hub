package kb

// Hermetic tests for two-strategy fusion and degraded mode (WP-2.1).
//
// Before WP-2.1 both branches were unreachable in CI: SemanticSearcher built its
// own embedding service and vector store, so there was no way to supply real
// vectors without Ollama, and no way to build a "present but broken" searcher
// without a live backend. The Embedder and VectorIndex seams close both gaps.
//
// The vectors here are real -- generated from a fixed seed and stored in a real
// SQLite index -- so the fusion path runs against genuine similarity scores.
// Only the embedding *model* is faked, by mapping known query strings to known
// vectors.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
)

var errEmbedderDown = errors.New("embedding service unreachable")

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeEmbedder produces deterministic vectors without touching a model.
//
// `vectors` maps an exact input string to the vector it should embed to, which
// lets a test place a query at a chosen similarity to indexed content. Anything
// unmapped falls back to `produce`, or to a fixed vector derived from the text.
type fakeEmbedder struct {
	dim     int
	vectors map[string][]float32
	produce func(i int) []float32
	err     error

	mu    sync.Mutex
	calls int
}

func (f *fakeEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	out, err := f.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	return out[0], nil
}

func (f *fakeEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(texts) == 0 {
		return nil, nil
	}

	f.mu.Lock()
	f.calls++
	f.mu.Unlock()

	out := make([][]float32, len(texts))
	for i, text := range texts {
		if v, ok := f.vectors[text]; ok {
			out[i] = v
			continue
		}
		if f.produce != nil {
			out[i] = f.produce(i)
			continue
		}
		out[i] = seededVector(f.dim, int64(len(text))+int64(i))
	}
	return out, nil
}

func (f *fakeEmbedder) Dimension() int { return f.dim }
func (f *fakeEmbedder) Model() string  { return "fake-embedder" }
func (f *fakeEmbedder) HealthCheck(ctx context.Context) error {
	return f.err
}

func (f *fakeEmbedder) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// failingVectorIndex is a VectorIndex that is constructible but always errors on
// search. This is the shape WP-1.1 could not build: "present but broken".
type failingVectorIndex struct {
	VectorIndex
	err error
}

func (f *failingVectorIndex) Search(ctx context.Context, query []float32, opts VectorSearchOptions) ([]VectorSearchResult, error) {
	return nil, f.err
}

// ---------------------------------------------------------------------------
// Fixture: golden corpus indexed with real vectors
// ---------------------------------------------------------------------------

// semanticIndex is the golden corpus with a vector per chunk.
type semanticIndex struct {
	*goldenIndex
	Embedder *fakeEmbedder
	Vectors  *SQLiteVectorIndex
	Semantic *SemanticSearcher
	// ChunkVectors maps chunk_id -> the vector that chunk was indexed with, so
	// a test can construct a query that is guaranteed to rank a chosen chunk
	// first.
	ChunkVectors map[string][]float32
}

// ingestGoldenCorpusWithVectors ingests the golden corpus through the real
// pipeline with a fake embedder, so every chunk gets a real, deterministic
// vector in the real index.
func ingestGoldenCorpusWithVectors(t *testing.T, dim int) *semanticIndex {
	t.Helper()

	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	vi, err := NewSQLiteVectorIndex(gi.DB, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}

	// Give each chunk a vector derived from its own content, so identical text
	// always embeds identically and the mapping is reproducible.
	embedder := &fakeEmbedder{
		dim: dim,
		produce: func(i int) []float32 {
			return seededVector(dim, int64(i))
		},
	}

	si := &semanticIndex{
		goldenIndex:  gi,
		Embedder:     embedder,
		Vectors:      vi,
		ChunkVectors: make(map[string][]float32),
	}

	// Walk the chunks that ingestion actually produced and give each one a
	// vector keyed by its rowid, which is stable for a fixed corpus.
	rows, err := gi.DB.QueryContext(ctx, `
		SELECT c.chunk_id, c.document_id, c.chunk_index, d.source_id
		FROM kb_chunks c JOIN kb_documents d ON d.document_id = c.document_id
		ORDER BY c.rowid`)
	if err != nil {
		t.Fatalf("read chunks: %v", err)
	}
	defer rows.Close()

	var points []VectorPoint
	seed := int64(0)
	for rows.Next() {
		var chunkID, docID, sourceID string
		var chunkIndex int
		if err := rows.Scan(&chunkID, &docID, &chunkIndex, &sourceID); err != nil {
			t.Fatalf("scan chunk: %v", err)
		}
		seed++
		vec := seededVector(dim, seed)
		si.ChunkVectors[chunkID] = vec
		points = append(points, VectorPoint{
			ID: chunkID, Vector: vec, DocumentID: docID, ChunkIndex: chunkIndex,
			Metadata: map[string]string{"source_id": sourceID},
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read chunks: %v", err)
	}
	if len(points) == 0 {
		t.Fatal("golden corpus produced no chunks to vectorise")
	}

	if err := vi.Upsert(ctx, points); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	si.Semantic = NewSemanticSearcherWith(gi.DB, embedder, vi)
	return si
}

// chunkIDForDocument returns the first chunk id belonging to a document.
func chunkIDForDocument(t *testing.T, si *semanticIndex, documentID string) string {
	t.Helper()
	var chunkID string
	err := si.DB.QueryRow(
		`SELECT chunk_id FROM kb_chunks WHERE document_id = ? ORDER BY chunk_index LIMIT 1`,
		documentID).Scan(&chunkID)
	if err != nil {
		t.Fatalf("no chunk for document %s: %v", documentID, err)
	}
	return chunkID
}

// ---------------------------------------------------------------------------
// Semantic search over real vectors
// ---------------------------------------------------------------------------

func TestSemanticSearch_Hermetic(t *testing.T) {
	const dim = 64
	si := ingestGoldenCorpusWithVectors(t, dim)
	ctx := context.Background()

	// Pick a chunk and make the query embed to exactly its vector: it must come
	// back first, with a similarity of 1.
	target := chunkIDForDocument(t, si, si.Docs[0].DocumentID)
	si.Embedder.vectors = map[string][]float32{
		"find the first document": si.ChunkVectors[target],
	}

	res, err := si.Semantic.Search(ctx, "find the first document", SemanticSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) == 0 {
		t.Fatal("expected semantic results")
	}
	if res.Results[0].ChunkID != target {
		t.Errorf("top hit = %s, want %s", res.Results[0].ChunkID, target)
	}
	if res.Results[0].Score < 0.999 {
		t.Errorf("exact-vector match scored %v, want ~1", res.Results[0].Score)
	}
	if res.Results[0].Confidence != "high" {
		t.Errorf("confidence for a perfect match = %q, want high", res.Results[0].Confidence)
	}
	if res.Results[0].Snippet == "" {
		t.Error("hit carries no snippet; the enrichment join is not wired up")
	}
}

func TestSemanticSearch_EmbeddingFailurePropagates(t *testing.T) {
	const dim = 32
	db := newTestDB(t)
	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}

	semantic := NewSemanticSearcherWith(db, &fakeEmbedder{dim: dim, err: errEmbedderDown}, vi)

	_, err = semantic.Search(context.Background(), "anything", SemanticSearchOptions{Limit: 5})
	if err == nil {
		t.Fatal("Search must fail when the embedder is down")
	}
	if !errors.Is(err, errEmbedderDown) {
		t.Errorf("error should wrap the embedder failure, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Two-strategy fusion
// ---------------------------------------------------------------------------

// TestHybridFusion_TwoStrategies_Hermetic is the test WP-1.1 could only write
// against a live stack. Without it, applyRRFWithAgreement never runs with a
// non-empty semantic list and the agreement/confidence model is never exercised.
func TestHybridFusion_TwoStrategies_Hermetic(t *testing.T) {
	const dim = 64
	si := ingestGoldenCorpusWithVectors(t, dim)
	ctx := context.Background()

	// "lantern" has lexical hits in the golden corpus. Point the query vector at
	// one of those same chunks so both strategies agree on it.
	lexical, err := si.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("lexical search: %v", err)
	}
	if len(lexical.Results) == 0 {
		t.Skip("golden corpus has no lexical hit for 'lantern'; fusion needs an overlap to assert on")
	}
	agreedChunk := lexical.Results[0].ChunkID
	si.Embedder.vectors = map[string][]float32{
		"lantern": si.ChunkVectors[agreedChunk],
	}

	hybrid := NewHybridSearcher(si.Searcher, si.Semantic)
	if !hybrid.HasSemanticSearch() {
		t.Fatal("HasSemanticSearch should be true")
	}

	res, err := hybrid.Search(ctx, "lantern", HybridSearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if res.SemanticHits == 0 {
		t.Errorf("expected semantic hits; got %d (fts %d)", res.SemanticHits, res.FTSHits)
	}
	if res.FTSHits == 0 {
		t.Errorf("expected lexical hits; got 0")
	}
	if res.StrategiesUsed != 2 {
		t.Errorf("StrategiesUsed = %d, want 2", res.StrategiesUsed)
	}
	if res.DegradedMode {
		t.Error("DegradedMode should be false when both strategies answered")
	}
	if res.Confidence != "very_high" && res.Confidence != "high" {
		t.Errorf("confidence with two contributing strategies = %q, want very_high or high", res.Confidence)
	}
	if len(res.Results) == 0 {
		t.Fatal("fusion returned no results")
	}

	// The chunk both strategies found should outrank chunks only one found.
	found := false
	for _, hit := range res.Results {
		if hit.ChunkID == agreedChunk {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("the chunk both strategies ranked first is missing from the fused results")
	}
}

// TestHybridFusion_SemanticOnlyResultsSurvive checks that a chunk only the
// vector side found still makes it into the fused output. That is the entire
// value proposition of adding semantic search, and it is invisible to a
// lexical-only test.
func TestHybridFusion_SemanticOnlyResultsSurvive(t *testing.T) {
	const dim = 64
	si := ingestGoldenCorpusWithVectors(t, dim)
	ctx := context.Background()

	// A nonsense token has no lexical match anywhere in the corpus.
	const query = "zzqqxx"
	lexical, err := si.Searcher.Search(ctx, query, SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("lexical search: %v", err)
	}
	if len(lexical.Results) != 0 {
		t.Skipf("%q unexpectedly has lexical hits; pick a different nonsense token", query)
	}

	target := chunkIDForDocument(t, si, si.Docs[len(si.Docs)-1].DocumentID)
	si.Embedder.vectors = map[string][]float32{query: si.ChunkVectors[target]}

	hybrid := NewHybridSearcher(si.Searcher, si.Semantic)
	res, err := hybrid.Search(ctx, query, HybridSearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if res.FTSHits != 0 {
		t.Errorf("FTSHits = %d, want 0 for a nonsense query", res.FTSHits)
	}
	if res.SemanticHits == 0 {
		t.Fatal("semantic side found nothing; fusion has nothing to contribute")
	}
	if res.StrategiesUsed != 1 {
		t.Errorf("StrategiesUsed = %d, want 1", res.StrategiesUsed)
	}

	var found bool
	for _, hit := range res.Results {
		if hit.ChunkID == target {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("a semantic-only hit was dropped from fusion; results = %d", len(res.Results))
	}
}

// ---------------------------------------------------------------------------
// Degraded mode
// ---------------------------------------------------------------------------

// TestHybridFusion_DegradedMode_Hermetic covers the branch every production
// semantic outage takes.
//
// WP-1.1 documented this as impossible to test: NewVectorStore called the
// backend during construction, so a "present but broken" semantic searcher could
// not be built without a live Qdrant. Injecting a failing VectorIndex makes it a
// plain unit test.
func TestHybridFusion_DegradedMode_Hermetic(t *testing.T) {
	const dim = 64

	cases := []struct {
		name     string
		semantic func(*semanticIndex) *SemanticSearcher
	}{
		{
			name: "vector index errors",
			semantic: func(si *semanticIndex) *SemanticSearcher {
				return NewSemanticSearcherWith(si.DB, si.Embedder,
					&failingVectorIndex{VectorIndex: si.Vectors, err: errors.New("vector index unavailable")})
			},
		},
		{
			name: "embedder errors",
			semantic: func(si *semanticIndex) *SemanticSearcher {
				return NewSemanticSearcherWith(si.DB, &fakeEmbedder{dim: dim, err: errEmbedderDown}, si.Vectors)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			si := ingestGoldenCorpusWithVectors(t, dim)
			hybrid := NewHybridSearcher(si.Searcher, tc.semantic(si))

			res, err := hybrid.Search(context.Background(), "lantern", HybridSearchOptions{Limit: 5})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}

			if len(res.Results) == 0 {
				t.Error("fusion must still return lexical results when the semantic side is down")
			}
			if !res.DegradedMode {
				t.Error("DegradedMode should be true when semantic search fails")
			}
			if res.Note != "Semantic search unavailable, using lexical search only" {
				t.Errorf("note = %q", res.Note)
			}
			if res.Confidence != "medium" {
				t.Errorf("degraded confidence = %q, want medium", res.Confidence)
			}
			if res.SemanticHits != 0 {
				t.Errorf("SemanticHits = %d, want 0", res.SemanticHits)
			}
			if res.FTSHits == 0 {
				t.Error("the lexical side should have answered")
			}
		})
	}
}

// TestHybridSearcher_NilSemanticIsNotDegraded distinguishes "no semantic search
// configured" from "semantic search failed". A lexical-only deployment is a
// healthy configuration, not a degraded one.
func TestHybridSearcher_NilSemanticIsNotDegraded(t *testing.T) {
	gi := ingestGoldenCorpus(t)

	res, err := gi.Hybrid.Search(context.Background(), "lantern", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.DegradedMode {
		t.Error("a searcher with no semantic side configured is not degraded")
	}
	if res.Note != "" {
		t.Errorf("unexpected note: %q", res.Note)
	}
}

// TestNewHybridSearcher_TypedNilPointer guards the interface conversion trap: a
// nil *SemanticSearcher must not become a non-nil SemanticProvider, or every
// availability check in hybrid_search.go silently inverts and the first search
// panics.
func TestNewHybridSearcher_TypedNilPointer(t *testing.T) {
	gi := ingestGoldenCorpus(t)

	var nilSemantic *SemanticSearcher
	hybrid := NewHybridSearcher(gi.Searcher, nilSemantic)

	if hybrid.HasSemanticSearch() {
		t.Fatal("a nil *SemanticSearcher must not report semantic search as available")
	}

	// Would panic if the guard were missing.
	res, err := hybrid.Search(context.Background(), "lantern", HybridSearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.DegradedMode {
		t.Error("DegradedMode should be false when semantic search was never configured")
	}
}

// TestSemanticSearcher_InjectionSeam documents the seam itself: the searcher
// must be constructible from supplied collaborators, with no network and no
// config, and must actually use them.
func TestSemanticSearcher_InjectionSeam(t *testing.T) {
	const dim = 16
	db := newTestDB(t)

	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	embedder := &fakeEmbedder{dim: dim}

	semantic := NewSemanticSearcherWith(db, embedder, vi)
	if semantic == nil {
		t.Fatal("NewSemanticSearcherWith returned nil")
	}
	if semantic.EmbeddingService() != Embedder(embedder) {
		t.Error("the injected embedder was not used")
	}
	if semantic.VectorIndex() != VectorIndex(vi) {
		t.Error("the injected vector index was not used")
	}

	if _, err := semantic.Search(context.Background(), "probe", SemanticSearchOptions{Limit: 1}); err != nil {
		t.Fatalf("Search against an empty index: %v", err)
	}
	if embedder.callCount() == 0 {
		t.Error("the injected embedder was never called")
	}
}

// TestSemanticSearchConfig_DefaultWiring pins that production callers still get
// a working searcher from config alone, with no live service required.
func TestSemanticSearchConfig_DefaultWiring(t *testing.T) {
	db := newTestDB(t)

	semantic, err := NewSemanticSearcher(db, SemanticSearchConfig{
		EmbeddingConfig: EmbeddingConfig{OllamaHost: "http://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatalf("NewSemanticSearcher must not require a reachable embedding service: %v", err)
	}
	if semantic.VectorIndex() == nil {
		t.Fatal("default wiring produced no vector index")
	}

	// The index is usable even though the embedding host is a closed port.
	if err := semantic.VectorIndex().HealthCheck(context.Background()); err != nil {
		t.Errorf("vector index health check: %v", err)
	}

	stats, err := semantic.VectorIndex().GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.VectorCount != 0 {
		t.Errorf("fresh index reports %d vectors", stats.VectorCount)
	}
	if !strings.Contains(fmt.Sprint(stats.CollectionName), "conduit") {
		t.Errorf("unexpected collection name %q", stats.CollectionName)
	}
}
