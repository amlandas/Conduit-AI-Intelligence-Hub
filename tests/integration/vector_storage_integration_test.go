package integration

// End-to-end check against a throwaway database: migrate a fresh file, ingest
// documents through the real pipeline, and confirm vectors land in the same
// SQLite file and drive retrieval. Uses a deterministic embedder so no service
// is required.

import (
	"context"
	"database/sql"
	"math"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/store"
)

const dim = 64

// detEmbedder maps text to a deterministic vector. `related` lets a test say
// "this query means the same thing as that passage", which is the one property
// a real embedding model has and a content hash does not -- without it, a query
// lands at an arbitrary point and ranking assertions test nothing.
type detEmbedder struct {
	dim     int
	related map[string]string
}

func vecFor(text string, dim int) []float32 {
	var seed int64
	for _, r := range text {
		seed = seed*31 + int64(r)
	}
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

func (d *detEmbedder) resolve(text string) string {
	if target, ok := d.related[text]; ok {
		return target
	}
	return text
}

func (d *detEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return vecFor(d.resolve(text), d.dim), nil
}
func (d *detEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = vecFor(d.resolve(t), d.dim)
	}
	return out, nil
}
func (d *detEmbedder) Dimension() int                              { return d.dim }
func (d *detEmbedder) Model() string                               { return "deterministic" }
func (d *detEmbedder) HealthCheck(ctx context.Context) error       { return nil }

func TestEndToEnd_FreshDatabaseIngestAndSearch(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "conduit.db")

	// 1. A brand new database must migrate cleanly, including migration 005.
	st, err := store.New(dbPath)
	if err != nil {
		t.Fatalf("store.New on a fresh file: %v", err)
	}
	defer st.Close()
	db := st.DB()
	ctx := context.Background()

	var version int
	if err := db.QueryRow(`SELECT MAX(version) FROM migrations`).Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version < 5 {
		t.Fatalf("migration version = %d, want >= 5", version)
	}

	// kb_vectors and kb_fts must live in the SAME file.
	for _, table := range []string{"kb_fts", "kb_vectors", "kb_entity_vectors"} {
		var name string
		if err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE name = ?`, table).Scan(&name); err != nil {
			t.Fatalf("table %s missing from the knowledge base file: %v", table, err)
		}
	}

	// 2. Wire the real ingestion pipeline.
	vi, err := kb.NewSQLiteVectorIndex(db, kb.VectorIndexConfig{Dimension: dim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}

	const lighthouseText = "The keeper trims the lantern at dusk and logs the weather in a ledger."
	embedder := &detEmbedder{
		dim: dim,
		// Stand in for what a real model does: put the query near the passage
		// that answers it.
		related: map[string]string{"lantern": lighthouseText},
	}
	semantic := kb.NewSemanticSearcherWith(db, embedder, vi)

	sources := kb.NewSourceManager(db)
	src, err := sources.Add(ctx, kb.AddSourceRequest{
		Path: t.TempDir(), Name: "e2e", SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("add source: %v", err)
	}

	indexer := kb.NewIndexer(db)
	indexer.SetSemanticSearcher(semantic)
	chunker := kb.NewChunker()

	docs := map[string]string{
		"lighthouse": lighthouseText,
		"orchard":    "Apple trees in the north orchard were pruned before the first frost.",
		"harbour":    "Fishing boats return to the harbour at dawn with the night's catch.",
	}
	for id, content := range docs {
		chunks := chunker.Chunk(content, kb.ChunkOptions{MaxSize: 1000, Overlap: 100})
		doc := &kb.Document{
			DocumentID: id, SourceID: src.SourceID, Path: "/e2e/" + id + ".txt",
			Title: id, MimeType: "text/plain", ModifiedAt: time.Now(),
		}
		if err := indexer.Index(ctx, doc, chunks); err != nil {
			t.Fatalf("index %s: %v", id, err)
		}
	}
	if n := indexer.GetSemanticErrors(); n != 0 {
		t.Fatalf("%d documents failed semantic indexing", n)
	}

	// 3. Text and vectors must both be present, in equal number.
	var chunkCount, vectorCount, ftsCount int
	db.QueryRow(`SELECT COUNT(*) FROM kb_chunks`).Scan(&chunkCount)
	db.QueryRow(`SELECT COUNT(*) FROM kb_vectors`).Scan(&vectorCount)
	db.QueryRow(`SELECT COUNT(*) FROM kb_fts`).Scan(&ftsCount)
	if chunkCount == 0 {
		t.Fatal("no chunks were indexed")
	}
	if vectorCount != chunkCount || ftsCount != chunkCount {
		t.Errorf("chunks=%d vectors=%d fts=%d -- the three must agree", chunkCount, vectorCount, ftsCount)
	}

	// 4. Hybrid search returns results, uses both strategies, and is not degraded.
	hybrid := kb.NewHybridSearcher(kb.NewSearcher(db), semantic)
	res, err := hybrid.Search(ctx, "lantern", kb.HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	if len(res.Results) == 0 {
		t.Fatal("hybrid search returned nothing")
	}
	if res.DegradedMode {
		t.Errorf("DegradedMode = true with a working stack; note = %q", res.Note)
	}
	if res.StrategiesUsed != 2 {
		t.Errorf("StrategiesUsed = %d, want 2 (lexical %d, semantic %d)",
			res.StrategiesUsed, res.FTSHits, res.SemanticHits)
	}
	if res.Results[0].DocumentID != "lighthouse" {
		t.Errorf("top hit = %s, want lighthouse -- both strategies rank it first",
			res.Results[0].DocumentID)
	}

	// 5. Removing a source reclaims its vectors. The SourceManager's own indexer
	// has no semantic searcher attached here, so this also proves the ON DELETE
	// CASCADE reclaims vectors on its own: deletion does not have to know that
	// vectors exist.
	if _, err := sources.Remove(ctx, src.SourceID); err != nil {
		t.Fatalf("remove source: %v", err)
	}
	var leftover int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_vectors`).Scan(&leftover); err != nil && err != sql.ErrNoRows {
		t.Fatalf("count leftover vectors: %v", err)
	}
	if leftover != 0 {
		t.Errorf("%d vectors survived source removal", leftover)
	}
}
