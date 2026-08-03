package kbservice

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kb"
)

// testConfig returns a config bound to a temporary knowledge base with
// embeddings off, which is the configuration that needs no model, no network
// and no sidecar process.
func testConfig(t *testing.T) *config.Config {
	t.Helper()

	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = dir
	cfg.DBPath = filepath.Join(dir, "kb.db")
	cfg.Embed.Provider = config.EmbedProviderNone
	return cfg
}

func openTestService(t *testing.T, cfg *config.Config) *Service {
	t.Helper()

	svc, err := Open(cfg)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			t.Skip("FTS5 not available, skipping")
		}
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// corpusDir writes a couple of documents and returns the directory.
func corpusDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"auth.md":  "# Authentication\n\nTokens are verified before authorisation runs.\n",
		"zebra.md": "# Zebra\n\nAn unrelated document about stripes.\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

// ---------------------------------------------------------------------------
// Opening
// ---------------------------------------------------------------------------

func TestOpen_CreatesKnowledgeBaseAtDBPath(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	if svc.DatabasePath() != cfg.DBPath {
		t.Errorf("DatabasePath() = %q, want %q", svc.DatabasePath(), cfg.DBPath)
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		t.Errorf("knowledge base was not created at db_path: %v", err)
	}
}

func TestOpen_LexicalOnlyWhenProviderIsNone(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	// "none" is a supported mode, not a failure: no embedder, no semantic
	// searcher, and a hybrid searcher that still works.
	if svc.Embedder() != nil {
		t.Error("embedder should be nil when embed.provider is none")
	}
	if svc.SemanticAvailable() {
		t.Error("semantic search should be unavailable when embed.provider is none")
	}
	if svc.Hybrid() == nil {
		t.Error("hybrid searcher must exist even in lexical-only mode")
	}

	info := svc.EmbedInfo()
	if info.Provider != config.EmbedProviderNone {
		t.Errorf("EmbedInfo().Provider = %q, want %q", info.Provider, config.EmbedProviderNone)
	}
	if info.Available {
		t.Error("EmbedInfo().Available should be false for provider none")
	}
}

func TestOpen_GraphIsInertUnlessEnabled(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	if svc.Graph().Enabled() {
		t.Error("graph should be off by default")
	}

	// Enabling the graph is what creates its tables; when off, it leaves no
	// trace in the database at all.
	var name string
	err := svc.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='kb_graph_edges'`).Scan(&name)
	if err == nil {
		t.Error("graph tables exist even though the graph is disabled")
	}
}

func TestOpen_GraphSchemaCreatedWhenEnabled(t *testing.T) {
	cfg := testConfig(t)
	cfg.KB.KAG.Enabled = true
	svc := openTestService(t, cfg)

	if !svc.Graph().Enabled() {
		t.Fatal("graph should be enabled")
	}

	var name string
	if err := svc.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='kb_graph_edges'`).Scan(&name); err != nil {
		t.Errorf("graph tables missing after enabling the graph: %v", err)
	}
}

func TestOpen_NilConfigIsAnError(t *testing.T) {
	if _, err := Open(nil); err == nil {
		t.Error("Open(nil) should fail")
	}
}

// ---------------------------------------------------------------------------
// Sources and sync
// ---------------------------------------------------------------------------

func TestSourceLifecycle(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)
	ctx := context.Background()

	docs := corpusDir(t)

	// --- empty ---
	list, err := svc.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if list.Count != 0 || len(list.Sources) != 0 {
		t.Fatalf("new knowledge base should have no sources, got %+v", list)
	}
	// An empty listing must marshal as [] rather than null, because the GUI
	// iterates it.
	if list.Sources == nil {
		t.Error("Sources should be an empty slice, not nil")
	}

	// --- add ---
	src, err := svc.AddSource(ctx, kb.AddSourceRequest{
		Path: docs, Name: "Docs", SyncMode: "manual",
	})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if src.SourceID == "" {
		t.Error("added source has no ID")
	}

	// --- find by id, name and path ---
	for _, key := range []string{src.SourceID, "Docs", docs} {
		found, err := svc.FindSource(ctx, key)
		if err != nil {
			t.Errorf("FindSource(%q): %v", key, err)
			continue
		}
		if found.SourceID != src.SourceID {
			t.Errorf("FindSource(%q) returned the wrong source", key)
		}
	}
	if _, err := svc.FindSource(ctx, "no-such-source"); err == nil {
		t.Error("FindSource should fail for an unknown key")
	}

	// --- sync ---
	res, err := svc.Sync(ctx, src.SourceID, false)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if res.Added != 2 {
		t.Errorf("Sync added %d documents, want 2", res.Added)
	}
	// With no embedding provider there is nothing to fail at, so a lexical
	// sync must report a clean run rather than a partial one.
	if res.SemanticErrors != 0 {
		t.Errorf("lexical-only sync reported %d semantic errors", res.SemanticErrors)
	}

	// --- stats ---
	totals, sources, err := svc.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if totals.Sources != 1 || totals.Documents != 2 {
		t.Errorf("Stats = %+v, want 1 source and 2 documents", totals)
	}
	if len(sources) != 1 {
		t.Errorf("Stats returned %d sources, want 1", len(sources))
	}

	// --- remove ---
	rm, err := svc.RemoveSource(ctx, src.SourceID)
	if err != nil {
		t.Fatalf("RemoveSource: %v", err)
	}
	if rm.DocumentsDeleted != 2 {
		t.Errorf("RemoveSource deleted %d documents, want 2", rm.DocumentsDeleted)
	}

	list, err = svc.ListSources(ctx)
	if err != nil {
		t.Fatalf("ListSources: %v", err)
	}
	if list.Count != 0 {
		t.Errorf("source survived removal: %+v", list)
	}
}

func TestMigrate_FailsWithoutEmbeddings(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	_, err := svc.Migrate(context.Background(), nil)
	if err == nil {
		t.Fatal("Migrate should fail when embeddings are off")
	}
	if err != ErrSemanticUnavailable {
		t.Errorf("Migrate error = %v, want ErrSemanticUnavailable", err)
	}
}

// ---------------------------------------------------------------------------
// Search
// ---------------------------------------------------------------------------

func TestSearch_LexicalFindsTheRightDocument(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)
	ctx := context.Background()

	docs := corpusDir(t)
	src, err := svc.AddSource(ctx, kb.AddSourceRequest{Path: docs, Name: "Docs", SyncMode: "manual"})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if _, err := svc.Sync(ctx, src.SourceID, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	req := NewSearchRequest("authentication")
	resp, err := svc.Search(ctx, req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	// The response shape is a compatibility contract inherited from the
	// deleted HTTP daemon.
	for _, key := range []string{"results", "total_hits", "query", "search_time", "search_mode", "processed"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("search response is missing key %q", key)
		}
	}
	if resp["query"] != "authentication" {
		t.Errorf("query echoed as %v", resp["query"])
	}
	if resp["processed"] != true {
		t.Errorf("processed = %v, want true for a non-raw search", resp["processed"])
	}
}

func TestSearch_EmptyQueryIsRejected(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	if _, err := svc.Search(context.Background(), NewSearchRequest("")); err == nil {
		t.Error("an empty query should be rejected")
	}
}

func TestSearch_SemanticModeFailsWithoutProvider(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	req := NewSearchRequest("anything")
	req.Mode = SearchModeSemantic

	// Forcing semantic with no provider must fail loudly rather than quietly
	// returning lexical hits under a "semantic" label.
	_, err := svc.Search(context.Background(), req)
	if err != ErrSemanticUnavailable {
		t.Errorf("semantic search error = %v, want ErrSemanticUnavailable", err)
	}
}

func TestSearch_RawSkipsProcessing(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)
	ctx := context.Background()

	docs := corpusDir(t)
	src, _ := svc.AddSource(ctx, kb.AddSourceRequest{Path: docs, Name: "Docs", SyncMode: "manual"})
	if _, err := svc.Sync(ctx, src.SourceID, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	req := NewSearchRequest("authentication")
	req.Mode = SearchModeFTS5
	req.Raw = true

	resp, err := svc.Search(ctx, req)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp["processed"] != false {
		t.Errorf("processed = %v, want false for a raw search", resp["processed"])
	}
	if resp["search_mode"] != SearchModeFTS5 {
		t.Errorf("search_mode = %v, want %q", resp["search_mode"], SearchModeFTS5)
	}
}

// ---------------------------------------------------------------------------
// Search option resolution
// ---------------------------------------------------------------------------

// TestHybridOpts_ConfigDefaults covers the config -> options mapping.
//
// WP-3.4 (#69) removed SemanticWeight, MMRLambda, DisableMMR and DisableRerank
// from SearchRequest and the RAG config. They fed kb.HybridSearchOptions fields
// the engine either never read or overwrote from the RecallMode preset, so
// setting them changed nothing observable. RecallMode replaces all four.
func TestHybridOpts_ConfigDefaults(t *testing.T) {
	cfg := testConfig(t)
	cfg.KB.RAG.RecallMode = "precise"
	cfg.KB.RAG.DefaultLimit = 7

	svc := openTestService(t, cfg)

	opts := svc.hybridOpts(NewSearchRequest("q"))

	if opts.RecallMode != kb.RecallModePrecise {
		t.Errorf("RecallMode = %v, want the configured precise", opts.RecallMode)
	}
	if opts.Limit != 7 {
		t.Errorf("Limit = %d, want the configured 7", opts.Limit)
	}
	if opts.Mode != kb.HybridModeAuto {
		t.Errorf("Mode = %v, want auto", opts.Mode)
	}
}

func TestHybridOpts_RequestOverridesConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.KB.RAG.RecallMode = "balanced"
	svc := openTestService(t, cfg)

	req := NewSearchRequest("q")
	req.RecallMode = "high"
	req.Limit = 3
	req.SourceID = "src_one"

	opts := svc.hybridOpts(req)
	if opts.RecallMode != kb.RecallModeHigh {
		t.Errorf("RecallMode = %v, want high", opts.RecallMode)
	}
	if opts.Limit != 3 {
		t.Errorf("Limit = %d, want 3", opts.Limit)
	}
	// The source filter used to be dropped on the hybrid path: --source was
	// honoured by semantic and fts5 mode only, and silently ignored by the
	// default mode.
	if len(opts.Filter.SourceIDs) != 1 || opts.Filter.SourceIDs[0] != "src_one" {
		t.Errorf("Filter.SourceIDs = %v, want [src_one]", opts.Filter.SourceIDs)
	}
}

func TestHybridOpts_UnknownRecallModeFallsBackToBalanced(t *testing.T) {
	cfg := testConfig(t)
	cfg.KB.RAG.RecallMode = "nonsense"
	svc := openTestService(t, cfg)

	if got := svc.hybridOpts(NewSearchRequest("q")).RecallMode; got != kb.RecallModeBalanced {
		t.Errorf("RecallMode = %v, want balanced for an unrecognised config value", got)
	}

	req := NewSearchRequest("q")
	req.RecallMode = "also nonsense"
	if got := svc.hybridOpts(req).RecallMode; got != kb.RecallModeBalanced {
		t.Errorf("RecallMode = %v, want balanced for an unrecognised request value", got)
	}
}

// TestSemanticOpts_MinScoreSentinel guards the sentinel semantics on the one
// tuning value that survived: a min-score of 0 is meaningful (no filtering), so
// "unset" cannot be the zero value -- it is Unset (-1).
func TestSemanticOpts_MinScoreSentinel(t *testing.T) {
	cfg := testConfig(t)
	cfg.KB.RAG.MinScore = 0.5
	svc := openTestService(t, cfg)

	if got := svc.semanticOpts(NewSearchRequest("q")).MinScore; got != 0.5 {
		t.Errorf("MinScore = %v, want the configured 0.5 when the request leaves it unset", got)
	}

	req := NewSearchRequest("q")
	req.MinScore = 0 // an explicit "do not filter"
	if got := svc.semanticOpts(req).MinScore; got != 0 {
		t.Errorf("MinScore = %v, want 0: an explicit zero must override the config", got)
	}

	req.MinScore = 4.2 // nonsense; out of range input is ignored
	if got := svc.semanticOpts(req).MinScore; got != 0.5 {
		t.Errorf("MinScore = %v, want the config value: out-of-range input is ignored", got)
	}
}

func TestFTSOpts_SourceFilterAndPaging(t *testing.T) {
	cfg := testConfig(t)
	svc := openTestService(t, cfg)

	req := NewSearchRequest("q")
	req.Limit = 3
	req.Offset = 6
	req.SourceID = "src-1"

	opts := svc.ftsOpts(req)
	if opts.Limit != 3 || opts.Offset != 6 {
		t.Errorf("paging not applied: limit=%d offset=%d", opts.Limit, opts.Offset)
	}
	if len(opts.SourceIDs) != 1 || opts.SourceIDs[0] != "src-1" {
		t.Errorf("source filter not applied: %v", opts.SourceIDs)
	}
	if !opts.Highlight {
		t.Error("lexical search should request highlighting, as the HTTP layer did")
	}
}

// ---------------------------------------------------------------------------
// Embedding provider selection
// ---------------------------------------------------------------------------

func TestNewEmbedder_None(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Embed.Provider = config.EmbedProviderNone

	embedder, info, err := newEmbedder(cfg)
	if err != nil {
		t.Fatalf("provider none should never error: %v", err)
	}
	if embedder != nil {
		t.Error("provider none should yield no embedder")
	}
	if info.Available {
		t.Error("provider none is not 'available'")
	}
}

func TestNewEmbedder_Ollama(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Embed.Provider = config.EmbedProviderOllama

	embedder, info, err := newEmbedder(cfg)
	if err != nil {
		t.Fatalf("building the Ollama provider should not require a running Ollama: %v", err)
	}
	if embedder == nil {
		t.Fatal("expected an embedder")
	}
	// Constructing must not have contacted anything; only Model/Dimension are
	// known up front.
	if info.Model == "" {
		t.Error("Ollama provider reported no model")
	}
	if embedder.Model() != info.Model {
		t.Errorf("adapter model %q disagrees with EmbedInfo %q", embedder.Model(), info.Model)
	}
	_ = embedder.(interface{ Close() error }).Close()
}

func TestNewEmbedder_LlamaServerIsLazy(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.Embed.Provider = config.EmbedProviderLlamaServer

	embedder, info, err := newEmbedder(cfg)
	if err != nil {
		t.Fatalf("building the llama-server provider should not start it: %v", err)
	}
	if embedder == nil {
		t.Fatal("expected an embedder")
	}
	defer embedder.(interface{ Close() error }).Close()

	// Dimensions come from the pinned registry, so they are known without
	// running anything.
	if info.Dimensions <= 0 {
		t.Errorf("dimensions should come from the model registry, got %d", info.Dimensions)
	}
	if embedder.Dimension() != info.Dimensions {
		t.Errorf("adapter dimension %d disagrees with EmbedInfo %d",
			embedder.Dimension(), info.Dimensions)
	}

	// No sidecar may have been spawned merely by constructing the provider:
	// `conduit kb list` must not load a model.
	if _, err := os.Stat(filepath.Join(cfg.DataDir, "embed", "sidecar.json")); err == nil {
		t.Error("constructing the provider started the sidecar; it must be lazy")
	}
}

func TestNewEmbedder_UnknownModelIsAnError(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DataDir = t.TempDir()
	cfg.Embed.Provider = config.EmbedProviderLlamaServer
	cfg.Embed.Model = "not-a-real-model"

	// Guessing here would index with the wrong vector width. Refuse instead.
	embedder, info, err := newEmbedder(cfg)
	if err == nil {
		t.Fatal("an unknown model should be rejected")
	}
	if embedder != nil {
		t.Error("no embedder should be returned for an unknown model")
	}
	if info.Err == "" {
		t.Error("EmbedInfo should record why the provider is unavailable")
	}
}

// TestOpen_EmbedderFailureDegradesToLexical checks that a broken embedding
// provider does not stop the knowledge base from opening: search degrades, the
// reason is recorded, and doctor is what surfaces it.
func TestOpen_EmbedderFailureDegradesToLexical(t *testing.T) {
	cfg := testConfig(t)
	cfg.Embed.Provider = config.EmbedProviderLlamaServer
	cfg.Embed.Model = "not-a-real-model"

	svc := openTestService(t, cfg)

	if svc.SemanticAvailable() {
		t.Error("semantic search should be unavailable when the provider failed to build")
	}
	if svc.Hybrid() == nil {
		t.Error("lexical retrieval must still work")
	}
	if svc.EmbedInfo().Err == "" {
		t.Error("the failure reason should be recorded for doctor to report")
	}
}
