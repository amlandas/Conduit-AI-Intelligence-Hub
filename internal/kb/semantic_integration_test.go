package kb

// Integration tests for the semantic (vector) half of retrieval.
//
// WHY THESE ARE SKIP-GATED
//
// The semantic path has no injection seam: SemanticSearcher is a concrete
// struct that constructs its own *EmbeddingService and *VectorStore inside
// NewSemanticSearcher, and HybridSearcher takes *SemanticSearcher rather than
// an interface. There is therefore no way to substitute a fake embedder or a
// fake vector store without changing production code, which WP-1.1 is not
// allowed to do.
//
// So these tests probe for a live Ollama and a live Qdrant on the loopback
// interface with a very short timeout and skip when either is missing. On CI
// (no Ollama, no Qdrant, no network) they always skip. On a developer machine
// with the stack running they exercise the real path.
//
// THAT SKIP LIST IS THE MIGRATION'S BLIND SPOT. Everything covered here is
// unverified by CI, and the Qdrant -> sqlite-vec migration will change all of
// it. When sqlite-vec lands, these should become hermetic (sqlite-vec runs
// in-process, so the whole file can drop its gates) -- and an embedder
// interface should be introduced so the Ollama gate can go too.

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"testing"
	"time"
)

// probeTimeout is deliberately tiny: on a machine without the service, the
// connection is refused immediately; on a machine with it, the loopback
// handshake is sub-millisecond. It exists only so a firewalled port that
// blackholes SYNs cannot stall the suite.
const probeTimeout = 250 * time.Millisecond

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// requireOllama skips unless something is listening on the Ollama port.
func requireOllama(t *testing.T) string {
	t.Helper()
	host := envOr("CONDUIT_TEST_OLLAMA_HOST", "http://127.0.0.1:11434")
	addr := envOr("CONDUIT_TEST_OLLAMA_ADDR", "127.0.0.1:11434")

	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		t.Skipf("SKIP-GATED (CI blind spot): no Ollama on %s: %v", addr, err)
	}
	_ = conn.Close()
	return host
}

// requireQdrant skips unless something is listening on the Qdrant gRPC port.
func requireQdrant(t *testing.T) (host string, port int) {
	t.Helper()
	host = envOr("CONDUIT_TEST_QDRANT_HOST", "127.0.0.1")
	port = DefaultQdrantPort

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		t.Skipf("SKIP-GATED (CI blind spot): no Qdrant on %s: %v", addr, err)
	}
	_ = conn.Close()
	return host, port
}

// testCollectionName gives every run its own Qdrant collection so a failed run
// can never poison the developer's real knowledge base.
func testCollectionName(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("conduit_wp11_test_%d", time.Now().UnixNano())
}

// TestSemanticIntegration_EmbeddingService covers the embedding round trip.
// CI blind spot: everything about vector generation -- dimension, determinism,
// batch behaviour, model availability.
func TestSemanticIntegration_EmbeddingService(t *testing.T) {
	host := requireOllama(t)

	svc, err := NewEmbeddingService(EmbeddingConfig{OllamaHost: host})
	if err != nil {
		t.Fatalf("NewEmbeddingService: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := svc.HealthCheck(ctx); err != nil {
		t.Skipf("SKIP-GATED: Ollama is up but the %s model is not usable: %v", svc.Model(), err)
	}

	t.Run("dimension matches the configured value", func(t *testing.T) {
		vec, err := svc.Embed(ctx, "the keeper trims the lantern at dusk")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(vec) != svc.Dimension() {
			t.Errorf("embedding dimension: got %d, want %d", len(vec), svc.Dimension())
		}
		if len(vec) != DefaultEmbeddingDimension {
			t.Errorf("embedding dimension: got %d, want the nomic-embed-text default %d", len(vec), DefaultEmbeddingDimension)
		}
	})

	t.Run("embedding the same text twice is stable", func(t *testing.T) {
		a, err := svc.Embed(ctx, "identical input")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		b, err := svc.Embed(ctx, "identical input")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(a) != len(b) {
			t.Fatalf("dimension changed between calls: %d vs %d", len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("embedding is not deterministic at index %d: %v vs %v", i, a[i], b[i])
			}
		}
	})

	t.Run("batch preserves input order", func(t *testing.T) {
		texts := []string{"lantern", "ledger", "rabbit"}
		batch, err := svc.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("EmbedBatch: %v", err)
		}
		if len(batch) != len(texts) {
			t.Fatalf("got %d embeddings, want %d", len(batch), len(texts))
		}
		for i, text := range texts {
			single, err := svc.Embed(ctx, text)
			if err != nil {
				t.Fatalf("Embed(%q): %v", text, err)
			}
			for j := range single {
				if batch[i][j] != single[j] {
					t.Fatalf("batch entry %d does not match the single embedding of %q", i, text)
				}
			}
		}
	})

	t.Run("empty batch is a no-op", func(t *testing.T) {
		got, err := svc.EmbedBatch(ctx, nil)
		if err != nil || got != nil {
			t.Errorf("EmbedBatch(nil) = %v, %v; want nil, nil", got, err)
		}
	})
}

// TestSemanticIntegration_VectorStore covers the Qdrant round trip.
// CI blind spot: collection creation, upsert, filtered search, deletion --
// i.e. exactly the surface that sqlite-vec has to reproduce.
func TestSemanticIntegration_VectorStore(t *testing.T) {
	host, port := requireQdrant(t)

	collection := testCollectionName(t)
	vs, err := NewVectorStore(VectorStoreConfig{
		Host:           host,
		Port:           port,
		CollectionName: collection,
		Dimension:      8, // small, so the test does not depend on a model
	})
	if err != nil {
		t.Fatalf("NewVectorStore: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := vs.client.DeleteCollection(cleanupCtx, collection); err != nil {
			t.Logf("could not delete the test collection %s: %v", collection, err)
		}
		_ = vs.Close()
	})

	if err := vs.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}
	if err := vs.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}

	points := []VectorPoint{
		{
			ID: "chunk_wp11_a", Vector: unitVector(8, 0), DocumentID: "doc_a", ChunkIndex: 0,
			Path: "/corpus/a.txt", Title: "A", Content: "alpha",
			Metadata: map[string]string{"source_id": "src_one"},
		},
		{
			ID: "chunk_wp11_b", Vector: unitVector(8, 1), DocumentID: "doc_b", ChunkIndex: 0,
			Path: "/corpus/b.txt", Title: "B", Content: "beta",
			Metadata: map[string]string{"source_id": "src_two"},
		},
	}
	if err := vs.UpsertBatch(ctx, points); err != nil {
		t.Fatalf("UpsertBatch: %v", err)
	}

	t.Run("nearest neighbour is the matching basis vector", func(t *testing.T) {
		res, err := vs.Search(ctx, unitVector(8, 0), VectorSearchOptions{Limit: 2})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res) == 0 {
			t.Fatal("expected results")
		}
		if res[0].ID != "chunk_wp11_a" {
			t.Errorf("nearest neighbour: got %s, want chunk_wp11_a", res[0].ID)
		}
		// Qdrant cosine similarity is higher-is-better -- the opposite sign
		// convention from SQLite bm25. The fusion layer only uses ranks, so the
		// mismatch is invisible today; sqlite-vec must keep it that way.
		if res[0].Score <= 0 {
			t.Errorf("expected a positive cosine similarity, got %v", res[0].Score)
		}
	})

	t.Run("source filter narrows the result set", func(t *testing.T) {
		res, err := vs.Search(ctx, unitVector(8, 0), VectorSearchOptions{Limit: 10, SourceIDs: []string{"src_two"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		for _, r := range res {
			if r.ID != "chunk_wp11_b" {
				t.Errorf("source filter leaked %s", r.ID)
			}
		}
	})

	t.Run("delete by document removes only that document", func(t *testing.T) {
		if err := vs.DeleteByDocument(ctx, "doc_a"); err != nil {
			t.Fatalf("DeleteByDocument: %v", err)
		}
		res, err := vs.Search(ctx, unitVector(8, 0), VectorSearchOptions{Limit: 10})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		for _, r := range res {
			if r.ID == "chunk_wp11_a" {
				t.Errorf("chunk_wp11_a survived DeleteByDocument")
			}
		}
	})
}

// TestSemanticIntegration_HybridFusion is the only test anywhere that exercises
// two-strategy fusion. Without it, applyRRFWithAgreement is only ever run with
// an empty semantic list, and the agreement/confidence model is never exercised
// end to end.
//
// CI blind spot: the entire semantic branch of searchFusion, the agreement
// boost, and every confidence level above "medium".
func TestSemanticIntegration_HybridFusion(t *testing.T) {
	ollamaHost := requireOllama(t)
	qdrantHost, qdrantPort := requireQdrant(t)

	collection := testCollectionName(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	semantic, err := NewSemanticSearcher(nil, SemanticSearchConfig{
		EmbeddingConfig:   EmbeddingConfig{OllamaHost: ollamaHost},
		VectorStoreConfig: VectorStoreConfig{Host: qdrantHost, Port: qdrantPort, CollectionName: collection},
	})
	if err != nil {
		t.Fatalf("NewSemanticSearcher: %v", err)
	}
	if err := semantic.embeddings.HealthCheck(ctx); err != nil {
		t.Skipf("SKIP-GATED: Ollama is up but the embedding model is not usable: %v", err)
	}

	gi := ingestGoldenCorpus(t)
	semantic.db = gi.DB

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := semantic.vectorStore.client.DeleteCollection(cleanupCtx, collection); err != nil {
			t.Logf("could not delete the test collection %s: %v", collection, err)
		}
	})

	if err := semantic.vectorStore.EnsureCollection(ctx); err != nil {
		t.Fatalf("EnsureCollection: %v", err)
	}

	// Re-index the corpus through the semantic path as well.
	indexer := NewIndexer(gi.DB)
	indexer.SetSemanticSearcher(semantic)
	var sourceID string
	if err := gi.DB.QueryRowContext(ctx, `SELECT source_id FROM kb_sources LIMIT 1`).Scan(&sourceID); err != nil {
		t.Fatalf("read source id: %v", err)
	}
	for _, doc := range gi.Docs {
		chunks := gi.Chunker.Chunk(doc.Content, goldenChunkOptions)
		d := &Document{
			DocumentID: doc.DocumentID,
			SourceID:   sourceID,
			Path:       "/corpus/" + doc.FileName,
			Title:      doc.Title,
			MimeType:   "text/plain",
		}
		if err := indexer.Index(ctx, d, chunks); err != nil {
			t.Fatalf("index %s: %v", doc.DocumentID, err)
		}
	}
	if n := indexer.GetSemanticErrors(); n != 0 {
		t.Fatalf("%d documents failed semantic indexing", n)
	}

	hybrid := NewHybridSearcher(gi.Searcher, semantic)
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
	if res.StrategiesUsed != 2 {
		t.Errorf("StrategiesUsed = %d, want 2", res.StrategiesUsed)
	}
	if res.DegradedMode {
		t.Errorf("DegradedMode should be false when both strategies answered")
	}
	if res.Confidence != "very_high" && res.Confidence != "high" {
		t.Errorf("confidence with two contributing strategies = %q, want very_high or high", res.Confidence)
	}

	// A purely conceptual query has no lexical anchor; if it still returns
	// something, the semantic side is doing real work.
	conceptual, err := hybrid.Search(ctx, "what does a lighthouse worker do at night", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(conceptual.Results) == 0 {
		t.Errorf("expected the semantic side to answer a conceptual query with no exact term overlap")
	}
	if conceptual.QueryAnalysis.QueryType != QueryTypeConceptual {
		t.Errorf("query type = %s, want conceptual", conceptual.QueryAnalysis.QueryType)
	}
}

// TestSemanticIntegration_DegradedMode pins the failure path: when the semantic
// side errors, fusion must still return lexical results and flag itself as
// degraded.
//
// SKIP-GATED, and this one is worth calling out. The obvious hermetic version
// -- point the semantic searcher at a closed port and never touch the network
// -- is impossible, because NewVectorStore (vectorstore.go:122) calls
// ListCollections during construction and returns an error if Qdrant is not
// reachable. There is no way to build a *SemanticSearcher that is "present but
// broken" without a live Qdrant. So the degraded-mode branch of searchFusion,
// which is the branch every production semantic outage takes, cannot be
// covered in CI at all today.
//
// The test therefore needs a live Qdrant and deliberately breaks the embedding
// side instead, by pointing it at loopback port 1 (reserved, always refused).
func TestSemanticIntegration_DegradedMode(t *testing.T) {
	qdrantHost, qdrantPort := requireQdrant(t)

	gi := ingestGoldenCorpus(t)

	collection := testCollectionName(t)
	semantic, err := NewSemanticSearcher(gi.DB, SemanticSearchConfig{
		EmbeddingConfig:   EmbeddingConfig{OllamaHost: "http://127.0.0.1:1"},
		VectorStoreConfig: VectorStoreConfig{Host: qdrantHost, Port: qdrantPort, CollectionName: collection},
	})
	if err != nil {
		t.Fatalf("NewSemanticSearcher: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = semantic.vectorStore.client.DeleteCollection(cleanupCtx, collection)
	})

	hybrid := NewHybridSearcher(gi.Searcher, semantic)

	// A deadline is essential here: the embedding client has no timeout of its
	// own (see TestKnownBug_Issue71). A refused connection fails fast, so this
	// is belt and braces.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	res, err := hybrid.Search(ctx, "lantern", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(res.Results) == 0 {
		t.Errorf("fusion must still return lexical results when the semantic side is down")
	}
	if !res.DegradedMode {
		t.Errorf("DegradedMode should be true when semantic search fails")
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
}

// unitVector returns a dim-length vector that is 1 at index one and 0 elsewhere.
func unitVector(dim, one int) []float32 {
	v := make([]float32, dim)
	v[one] = 1
	return v
}
