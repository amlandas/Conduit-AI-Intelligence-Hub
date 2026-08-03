package kb

// Benchmarks for the SQLite vector index (WP-2.1).
//
// The budget is p95 < 100ms at 50K x 768, which is the top of the target corpus
// range. Run with:
//
//	CGO_ENABLED=1 go test -tags fts5 -run XXX -bench BenchmarkVectorSearch \
//	  -benchtime 20x ./internal/kb/
//
// Results are recorded in .eng-lead-kb/BENCH-WP-2.1.md.

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/store"
)

// benchDimension is the production embedding width (nomic-embed-text).
const benchDimension = DefaultEmbeddingDimension

// benchCorpusSizes spans the target corpus range: 5K is a small knowledge base,
// 50K the documented ceiling.
var benchCorpusSizes = []int{5000, 25000, 50000}

// newBenchIndex builds an index holding n synthetic 768-dim vectors, with the
// source, document and chunk rows they reference.
//
// The chunk rows are not decoration: the enrichment phase joins them, so a
// benchmark without them would measure only half the query. It writes rows
// directly rather than running the chunker, since chunking cost is not what is
// being measured here.
func newBenchIndex(b *testing.B, n int) (*SQLiteVectorIndex, func()) {
	b.Helper()

	dbPath := filepath.Join(b.TempDir(), "bench.db")
	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			b.Skip("FTS5 not available (build with CGO_ENABLED=1 -tags fts5)")
		}
		b.Fatalf("open store: %v", err)
	}

	db := st.DB()
	ctx := context.Background()

	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: benchDimension})
	if err != nil {
		st.Close()
		b.Fatalf("NewSQLiteVectorIndex: %v", err)
	}

	// One transaction for the whole load; per-row transactions would make setup
	// take longer than the benchmark.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		st.Close()
		b.Fatalf("begin: %v", err)
	}
	fail := func(format string, args ...any) {
		st.Close()
		b.Fatalf(format, args...)
	}

	for _, sourceID := range []string{"src_a", "src_b"} {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO kb_sources (source_id, path, name) VALUES (?, ?, ?)`,
			sourceID, "/bench/"+sourceID, sourceID); err != nil {
			fail("insert source: %v", err)
		}
	}

	docStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO kb_documents (document_id, source_id, path, title, mime_type, size, indexed_at)
		 VALUES (?, ?, ?, ?, 'text/plain', 0, datetime('now'))`)
	if err != nil {
		fail("prepare document: %v", err)
	}
	chunkStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO kb_chunks (chunk_id, document_id, chunk_index, content) VALUES (?, ?, ?, ?)`)
	if err != nil {
		fail("prepare chunk: %v", err)
	}
	vecStmt, err := tx.PrepareContext(ctx,
		`INSERT INTO kb_vectors (chunk_id, document_id, source_id, dim, norm, embedding)
		 VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		fail("prepare vector: %v", err)
	}

	// Ten chunks per document, matching the shape a real corpus produces.
	const chunksPerDoc = 10
	sourceFor := func(docIdx int) string {
		if docIdx%4 == 0 {
			return "src_b"
		}
		return "src_a"
	}

	for docIdx := 0; docIdx*chunksPerDoc < n; docIdx++ {
		docID := fmt.Sprintf("doc_%d", docIdx)
		if _, err := docStmt.ExecContext(ctx, docID, sourceFor(docIdx),
			fmt.Sprintf("/bench/doc_%d.txt", docIdx), fmt.Sprintf("Document %d", docIdx)); err != nil {
			fail("insert document %d: %v", docIdx, err)
		}
	}

	for i := 0; i < n; i++ {
		docIdx := i / chunksPerDoc
		docID := fmt.Sprintf("doc_%d", docIdx)
		chunkID := fmt.Sprintf("chunk_%d", i)

		if _, err := chunkStmt.ExecContext(ctx, chunkID, docID, i%chunksPerDoc,
			fmt.Sprintf("synthetic chunk %d of document %d", i, docIdx)); err != nil {
			fail("insert chunk %d: %v", i, err)
		}

		vec := seededVector(benchDimension, int64(i))
		if _, err := vecStmt.ExecContext(ctx, chunkID, docID, sourceFor(docIdx),
			benchDimension, l2Norm(vec), encodeVector(vec)); err != nil {
			fail("insert vector %d: %v", i, err)
		}
	}

	docStmt.Close()
	chunkStmt.Close()
	vecStmt.Close()
	if err := tx.Commit(); err != nil {
		fail("commit: %v", err)
	}

	return vi, func() { st.Close() }
}

// BenchmarkVectorSearch measures an unfiltered top-10 search across the target
// corpus range.
func BenchmarkVectorSearch(b *testing.B) {
	for _, n := range benchCorpusSizes {
		b.Run(fmt.Sprintf("%dx%d", n, benchDimension), func(b *testing.B) {
			vi, cleanup := newBenchIndex(b, n)
			defer cleanup()

			ctx := context.Background()
			query := seededVector(benchDimension, 999999)

			// Warm the page cache so the first iteration is not an outlier.
			if _, err := vi.Search(ctx, query, VectorSearchOptions{Limit: 10}); err != nil {
				b.Fatalf("warmup: %v", err)
			}

			latencies := make([]time.Duration, 0, b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				res, err := vi.Search(ctx, query, VectorSearchOptions{Limit: 10})
				elapsed := time.Since(start)
				if err != nil {
					b.Fatalf("Search: %v", err)
				}
				if len(res) != 10 {
					b.Fatalf("got %d results, want 10", len(res))
				}
				latencies = append(latencies, elapsed)
			}
			b.StopTimer()

			reportLatencies(b, latencies)
		})
	}
}

// BenchmarkVectorSearchFiltered measures a source-filtered search, the shape a
// multi-source knowledge base actually issues. The filter is a SQL predicate,
// so it shrinks the scan rather than post-filtering the result.
func BenchmarkVectorSearchFiltered(b *testing.B) {
	for _, n := range benchCorpusSizes {
		b.Run(fmt.Sprintf("%dx%d", n, benchDimension), func(b *testing.B) {
			vi, cleanup := newBenchIndex(b, n)
			defer cleanup()

			ctx := context.Background()
			query := seededVector(benchDimension, 999999)
			opts := VectorSearchOptions{Limit: 10, SourceIDs: []string{"src_b"}}

			if _, err := vi.Search(ctx, query, opts); err != nil {
				b.Fatalf("warmup: %v", err)
			}

			latencies := make([]time.Duration, 0, b.N)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				if _, err := vi.Search(ctx, query, opts); err != nil {
					b.Fatalf("Search: %v", err)
				}
				latencies = append(latencies, time.Since(start))
			}
			b.StopTimer()

			reportLatencies(b, latencies)
		})
	}
}

// BenchmarkVectorUpsert measures the write side: one transaction carrying a
// document's worth of vectors, which is what ingesting or re-ingesting a
// document costs.
func BenchmarkVectorUpsert(b *testing.B) {
	// A document of 100 chunks -- a large-ish document under the default 1000
	// byte chunk size.
	const batch = 100

	vi, cleanup := newBenchIndex(b, batch)
	defer cleanup()

	points := make([]VectorPoint, batch)
	for i := range points {
		points[i] = VectorPoint{
			ID:         fmt.Sprintf("chunk_%d", i),
			Vector:     seededVector(benchDimension, int64(i)),
			DocumentID: fmt.Sprintf("doc_%d", i/10),
			Metadata:   map[string]string{"source_id": "src_a"},
		}
	}

	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := vi.Upsert(ctx, points); err != nil {
			b.Fatalf("Upsert: %v", err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(batch), "vectors/op")
}

// reportLatencies attaches p50/p95/max to the benchmark output. The headline
// ns/op is a mean, and the budget in the work package is stated as a p95.
func reportLatencies(b *testing.B, latencies []time.Duration) {
	b.Helper()
	if len(latencies) == 0 {
		return
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })

	pick := func(q float64) time.Duration {
		idx := int(q * float64(len(latencies)))
		if idx >= len(latencies) {
			idx = len(latencies) - 1
		}
		return latencies[idx]
	}

	b.ReportMetric(float64(pick(0.50).Microseconds())/1000, "p50_ms")
	b.ReportMetric(float64(pick(0.95).Microseconds())/1000, "p95_ms")
	b.ReportMetric(float64(latencies[len(latencies)-1].Microseconds())/1000, "max_ms")
}
