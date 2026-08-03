package integration

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/store"
)

// repoRoot walks up from this file to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	dir := filepath.Dir(thisFile)
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("cannot find go.mod above this test file")
	return ""
}

// TestGoRedisIsGone is the dependency half of WP-2.3. FalkorDB spoke the Redis
// protocol; deleting the store is only half the job if the client library is
// still in the module graph, because it keeps the attack surface, the CVE feed
// and the temptation to reintroduce the backend.
//
// This asserts on go.mod and go.sum directly rather than on compiled code: a
// dependency can linger in the module graph long after the last import is gone,
// and `go mod tidy` is the step people forget.
func TestGoRedisIsGone(t *testing.T) {
	root := repoRoot(t)

	for _, name := range []string{"go.mod", "go.sum"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		lineNo := 0
		for scanner.Scan() {
			lineNo++
			line := scanner.Text()
			if strings.Contains(strings.ToLower(line), "redis") {
				t.Errorf("%s:%d still references redis: %s\n"+
					"WP-2.3 removed FalkorDB; run `go mod tidy`.", name, lineNo, strings.TrimSpace(line))
			}
		}
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan %s: %v", name, err)
		}
	}
}

// TestNoFalkorDBSymbolsRemain guards against a partial revert: the store type
// and its constructor must not come back under any name.
func TestNoFalkorDBSymbolsRemain(t *testing.T) {
	root := repoRoot(t)
	kbDir := filepath.Join(root, "internal", "kb")

	entries, err := os.ReadDir(kbDir)
	if err != nil {
		t.Fatalf("read internal/kb: %v", err)
	}

	// Code, not prose: the doc comments in this package deliberately explain
	// what was removed and why, so the patterns below are written to match
	// declarations and imports rather than any mention of the old stack.
	banned := []string{
		"FalkorDBStore",
		"NewFalkorDBStore",
		`redis/go-redis/v9"`, // an import line, not a comment
		"GRAPH.QUERY",        // the Cypher-over-Redis command
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(kbDir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		content := string(data)
		for _, sym := range banned {
			if strings.Contains(content, sym) {
				t.Errorf("internal/kb/%s references removed symbol %q", entry.Name(), sym)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(kbDir, "falkordb_store.go")); !os.IsNotExist(err) {
		t.Error("internal/kb/falkordb_store.go still exists")
	}
}

// TestGraphDisabledByDefaultInConfig pins the shipped posture.
func TestGraphDisabledByDefaultInConfig(t *testing.T) {
	cfg := config.DefaultConfig()

	if cfg.KB.KAG.Enabled {
		t.Error("knowledge graph is enabled by default; it must be opt-in")
	}
	if cfg.KB.KAG.Graph.Backend != "sqlite" {
		t.Errorf("graph backend = %q, want sqlite", cfg.KB.KAG.Graph.Backend)
	}
	if cfg.KB.KAG.Provider != "pattern" {
		t.Errorf("extraction provider = %q, want pattern (no LLM on the default path)",
			cfg.KB.KAG.Provider)
	}
	if cfg.KB.KAG.PreloadModel {
		t.Error("model preloading is enabled by default")
	}

	// The local query log is the opposite default: on, because it is local-only
	// and the evidence it gathers cannot be collected retroactively.
	if !cfg.Telemetry.LocalQueryLog {
		t.Error("local query log is off by default; the evidence gate needs it on")
	}
	if want := filepath.Join(cfg.DataDir, "query-shape.jsonl"); cfg.QueryLogPath() != want {
		t.Errorf("query log path = %q, want %q", cfg.QueryLogPath(), want)
	}
}

// TestGraphOnRealStoreSchema exercises the graph against the full production
// schema, including the foreign keys store migration 004 puts on kb_entities.
// The in-package tests build their own bare tables, so this is the only place
// the FK behavior is actually proven.
func TestGraphOnRealStoreSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "graph.db")
	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping (build with CGO_ENABLED=1 -tags fts5)")
		}
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	db := st.DB()

	// A graph-disabled store must not create tables even against the real
	// schema, where kb_entities already exists.
	disabled := kb.NewGraphStore(db, false)
	if err := disabled.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema on disabled store: %v", err)
	}
	var edgeTable int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kb_graph_edges'`).Scan(&edgeTable)
	if edgeTable != 0 {
		t.Fatal("disabled store created kb_graph_edges against the real schema")
	}

	g := kb.NewGraphStore(db, true)
	if err := g.EnsureSchema(ctx); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Unknown provenance must land as NULL, not "", or the foreign keys on
	// kb_entities reject the write.
	for _, e := range []struct {
		id, name string
	}{{"e1", "Alpha"}, {"e2", "Beta"}} {
		err := g.UpsertEntity(ctx, &kb.Entity{
			ID:         e.id,
			Name:       e.name,
			Type:       kb.EntityTypeConcept,
			Confidence: 0.9,
		})
		if err != nil {
			t.Fatalf("upsert entity with no provenance: %v", err)
		}
	}

	var nullChunks int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM kb_entities WHERE source_chunk_id IS NULL`).Scan(&nullChunks); err != nil {
		t.Fatalf("count null chunk ids: %v", err)
	}
	if nullChunks != 2 {
		t.Errorf("%d entities have NULL source_chunk_id, want 2", nullChunks)
	}

	err = g.UpsertEdge(ctx, &kb.Relation{
		SubjectID:  "e1",
		Predicate:  kb.RelationRelatesTo,
		ObjectID:   "e2",
		Confidence: 0.8,
	})
	if err != nil {
		t.Fatalf("upsert edge: %v", err)
	}

	edges, reached, err := g.Traverse(ctx, []string{"e1"}, kb.MaxGraphHops)
	if err != nil {
		t.Fatalf("traverse: %v", err)
	}
	if len(edges) != 1 {
		t.Fatalf("got %d edges, want 1", len(edges))
	}
	if edges[0].SubjectName != "Alpha" || edges[0].ObjectName != "Beta" {
		t.Errorf("edge = %s -> %s, want Alpha -> Beta", edges[0].SubjectName, edges[0].ObjectName)
	}
	if len(reached) != 2 {
		t.Errorf("reached %d entities, want 2", len(reached))
	}
}

// TestPatternExtractionEndToEnd runs the default enabled path -- pattern
// extraction into the SQLite graph -- with no Ollama, no network and no model.
func TestPatternExtractionEndToEnd(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "extract.db")
	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping")
		}
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	db := st.DB()

	g := kb.NewGraphStore(db, true)
	if err := g.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure schema: %v", err)
	}

	kagCfg := kb.DefaultKAGConfig()
	kagCfg.Enabled = true
	kagCfg.Extraction.EnableBackground = false

	provider, err := kb.NewProviderFactory().CreateProvider(kagCfg)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	defer provider.Close()
	if provider.Name() != "pattern" {
		t.Fatalf("default provider = %q, want pattern", provider.Name())
	}

	extractor, err := kb.NewEntityExtractor(kb.EntityExtractorConfig{
		Provider:   provider,
		DB:         db,
		GraphStore: g,
		Config:     kagCfg,
	})
	if err != nil {
		t.Fatalf("create extractor: %v", err)
	}
	defer extractor.Close()

	content := `# Authentication

Conduit validates every BearerToken against the IdentityProvider.
The IdentityProvider signs each BearerToken before the RequestHandler runs.`

	result, err := extractor.ExtractFromChunk(ctx, "chunk-1", "", "Authentication", content)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(result.Entities) == 0 {
		t.Fatal("pattern extraction produced no entities")
	}

	var edgeCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_graph_edges`).Scan(&edgeCount); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if edgeCount == 0 {
		t.Error("pattern extraction produced no graph edges")
	}

	// Every stored edge must carry the predicates co-occurrence can justify.
	rows, err := db.Query(`SELECT DISTINCT predicate FROM kb_graph_edges`)
	if err != nil {
		t.Fatalf("read predicates: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatalf("scan predicate: %v", err)
		}
		if p != string(kb.RelationRelatesTo) && p != string(kb.RelationContains) {
			t.Errorf("stored predicate %q is not justifiable from co-occurrence", p)
		}
	}
}

// TestExtractionWritesNoEdgesWhenGraphDisabled is the mirror image: running the
// extractor with the graph off must populate the legacy tables only.
func TestExtractionWritesNoEdgesWhenGraphDisabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "extract-off.db")
	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping")
		}
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	ctx := context.Background()
	db := st.DB()

	kagCfg := kb.DefaultKAGConfig()
	kagCfg.Extraction.EnableBackground = false

	provider, err := kb.NewProviderFactory().CreateProvider(kagCfg)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	defer provider.Close()

	extractor, err := kb.NewEntityExtractor(kb.EntityExtractorConfig{
		Provider:   provider,
		DB:         db,
		GraphStore: kb.NewGraphStore(db, false),
		Config:     kagCfg,
	})
	if err != nil {
		t.Fatalf("create extractor: %v", err)
	}
	defer extractor.Close()

	_, err = extractor.ExtractFromChunk(ctx, "chunk-1", "", "Title",
		"Conduit validates every BearerToken against the IdentityProvider.")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var tableCount int
	db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kb_graph_edges'`).Scan(&tableCount)
	if tableCount != 0 {
		t.Error("extraction created kb_graph_edges with the graph disabled")
	}
}
