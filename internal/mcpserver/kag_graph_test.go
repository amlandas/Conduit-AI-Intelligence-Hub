package mcpserver

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/querylog"
)

// graphTestServer builds a server over the fixture corpus with explicit options.
func graphTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	db, _ := testDB(t)
	return NewWithOptions(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil), opts)
}

// seedGraph turns the graph on for a server's database and writes a small
// entity/edge fixture into it.
func seedGraph(t *testing.T, s *Server) {
	t.Helper()
	ctx := context.Background()

	g := kb.NewGraphStore(s.db, true)
	if err := g.EnsureSchema(ctx); err != nil {
		t.Fatalf("ensure graph schema: %v", err)
	}

	entities := []struct {
		id, name string
		typ      kb.EntityType
	}{
		{"ent-token", "BearerToken", kb.EntityTypeConcept},
		{"ent-idp", "IdentityProvider", kb.EntityTypeOrganization},
		{"ent-handler", "RequestHandler", kb.EntityTypeTechnology},
	}
	for _, e := range entities {
		// SourceChunkID is deliberately empty: on a database that ran store
		// migration 004 it is a foreign key onto kb_chunks, and this fixture
		// asserts that unknown provenance is stored as NULL rather than "".
		err := g.UpsertEntity(ctx, &kb.Entity{
			ID:               e.id,
			Name:             e.name,
			Type:             e.typ,
			Description:      e.name + " appears in the authentication design",
			SourceDocumentID: "doc-auth",
			Confidence:       0.9,
		})
		if err != nil {
			t.Fatalf("seed entity %s: %v", e.id, err)
		}
	}

	edges := []struct {
		subject   string
		predicate kb.RelationType
		object    string
	}{
		{"ent-token", kb.RelationRelatesTo, "ent-idp"},
		{"ent-idp", kb.RelationRelatesTo, "ent-handler"},
	}
	for _, e := range edges {
		err := g.UpsertEdge(ctx, &kb.Relation{
			SubjectID:        e.subject,
			Predicate:        e.predicate,
			ObjectID:         e.object,
			SourceChunkID:    "chunk-auth",
			SourceDocumentID: "doc-auth",
			Confidence:       0.85,
		})
		if err != nil {
			t.Fatalf("seed edge: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Disabled path
// ---------------------------------------------------------------------------

// TestKagQueryDisabledDegradesInsteadOfFailing is the headline behavior: with
// the graph off (the default), kag_query must still return grounded content and
// say plainly why it is not graph content.
func TestKagQueryDisabledDegradesInsteadOfFailing(t *testing.T) {
	cs := connect(t, graphTestServer(t, Options{}))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      ToolKAGQuery,
		Arguments: map[string]any{"query": "token"},
	})
	if err != nil {
		t.Fatalf("kag_query returned a protocol error with the graph disabled: %v", err)
	}
	if res.IsError {
		t.Fatalf("kag_query returned a tool error with the graph disabled: %s", resultText(t, res))
	}

	text := resultText(t, res)

	// Machine-detectable marker so a client can tell degraded from real.
	if !strings.Contains(text, "graph: disabled") {
		t.Errorf("missing 'graph: disabled' marker:\n%s", text)
	}
	// Human-actionable explanation.
	if !strings.Contains(text, "kb.kag.enabled") {
		t.Errorf("response does not say how to enable the graph:\n%s", text)
	}
	// And actual retrieved content, not just an apology. The fixture corpus
	// contains a document about bearer tokens.
	if !strings.Contains(text, "Authentication Design") {
		t.Errorf("fallback returned no grounded content:\n%s", text)
	}
	if !strings.Contains(text, "Path: ") || !strings.Contains(text, "score: ") {
		t.Errorf("fallback hits are missing citation fields:\n%s", text)
	}
}

// TestKagQueryDisabledFallbackHonorsArguments checks the fallback respects the
// arguments the caller actually passed rather than ignoring them.
func TestKagQueryDisabledFallbackHonorsArguments(t *testing.T) {
	db, sourceID := testDB(t)
	s := NewWithOptions(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil), Options{})
	cs := connect(t, s)

	t.Run("limit is applied", func(t *testing.T) {
		res := callText(t, cs, ToolKAGQuery, map[string]any{
			"query": "the",
			"limit": 1,
		})
		// One note block plus at most one hit block.
		if len(res.Content) > 2 {
			t.Errorf("got %d content blocks with limit=1, want at most 2", len(res.Content))
		}
	})

	t.Run("source filter is applied", func(t *testing.T) {
		res := callText(t, cs, ToolKAGQuery, map[string]any{
			"query":     "token",
			"source_id": sourceID,
		})
		if res.IsError {
			t.Fatalf("source-filtered fallback errored: %s", resultText(t, res))
		}
	})

	t.Run("unknown source yields an empty but non-error answer", func(t *testing.T) {
		res := callText(t, cs, ToolKAGQuery, map[string]any{
			"query":     "token",
			"source_id": "src-does-not-exist",
		})
		if res.IsError {
			t.Fatalf("unknown source produced a tool error: %s", resultText(t, res))
		}
		text := resultText(t, res)
		if !strings.Contains(text, "graph: disabled") {
			t.Errorf("missing degraded marker:\n%s", text)
		}
		if !strings.Contains(text, "No results found") {
			t.Errorf("expected an explicit no-results note:\n%s", text)
		}
	})

	t.Run("entity hints are folded into the fallback query", func(t *testing.T) {
		res := callText(t, cs, ToolKAGQuery, map[string]any{
			"query":    "design",
			"entities": []string{"telemetry"},
		})
		if res.IsError {
			t.Fatalf("entity-hint fallback errored: %s", resultText(t, res))
		}
	})
}

// ---------------------------------------------------------------------------
// Enabled path
// ---------------------------------------------------------------------------

func TestKagQueryEnabledTraversesSQLiteEdges(t *testing.T) {
	db, _ := testDB(t)
	s := NewWithOptions(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil), Options{
		GraphEnabled: true,
	})
	seedGraph(t, s)
	cs := connect(t, s)

	res := callText(t, cs, ToolKAGQuery, map[string]any{
		"query":             "BearerToken",
		"include_relations": true,
		"max_hops":          2,
	})
	if res.IsError {
		t.Fatalf("enabled kag_query errored: %s", resultText(t, res))
	}

	text := resultText(t, res)

	// Same response shape as before: a "Knowledge Graph Results" context block
	// followed by an entity/relationship summary.
	if !strings.Contains(text, "Knowledge Graph Results") {
		t.Errorf("missing knowledge graph context block:\n%s", text)
	}
	if !strings.Contains(text, "BearerToken") {
		t.Errorf("matched entity missing from response:\n%s", text)
	}
	if !strings.Contains(text, "Relationships") {
		t.Errorf("no relationships section despite seeded edges:\n%s", text)
	}
	// Two hops from BearerToken reaches RequestHandler through IdentityProvider.
	if !strings.Contains(text, "RequestHandler") {
		t.Errorf("two-hop traversal did not reach the far entity:\n%s", text)
	}
	if !strings.Contains(text, "Found ") {
		t.Errorf("missing entity count summary:\n%s", text)
	}
	// The degraded marker must NOT be present when the graph really answered.
	if strings.Contains(text, "graph: disabled") {
		t.Errorf("enabled graph emitted the disabled marker:\n%s", text)
	}
}

// TestKagQueryEnabledButEmptyDegrades covers the honest middle case: the feature
// is on but nothing has been extracted for this query.
func TestKagQueryEnabledButEmptyDegrades(t *testing.T) {
	db, _ := testDB(t)
	s := NewWithOptions(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil), Options{
		GraphEnabled: true,
	})
	seedGraph(t, s)
	cs := connect(t, s)

	res := callText(t, cs, ToolKAGQuery, map[string]any{
		"query": "zzzznothingmatchesthis",
	})
	if res.IsError {
		t.Fatalf("empty graph result produced a tool error: %s", resultText(t, res))
	}

	text := resultText(t, res)
	if !strings.Contains(text, "graph: enabled, empty") {
		t.Errorf("missing 'graph: enabled, empty' marker:\n%s", text)
	}
	if !strings.Contains(text, "kag-sync") {
		t.Errorf("response does not suggest populating the graph:\n%s", text)
	}
}

// TestKagQueryToolSchemaUnchanged guards the client-facing contract. The
// backing implementation changed in WP-2.3; the tool description and schema must
// not, because client prompts are tuned against them.
func TestKagQueryToolSchemaUnchanged(t *testing.T) {
	cs := connect(t, graphTestServer(t, Options{}))

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	var found *mcp.Tool
	for _, tool := range tools.Tools {
		if tool.Name == ToolKAGQuery {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("kag_query is no longer registered")
	}

	const want = "Query the knowledge graph for entities and their relationships. " +
		"Use for multi-hop reasoning, aggregation queries, or finding connections between concepts. " +
		"Complements RAG search with structured entity lookups."
	if found.Description != want {
		t.Errorf("kag_query description changed:\ngot:  %q\nwant: %q", found.Description, want)
	}

	schema, ok := found.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("kag_query input schema is %T, want a JSON object", found.InputSchema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("kag_query schema properties are %T, want a JSON object", schema["properties"])
	}
	for _, prop := range []string{"query", "entities", "include_relations", "max_hops", "limit", "source_id"} {
		if _, ok := props[prop]; !ok {
			t.Errorf("kag_query input schema lost property %q", prop)
		}
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "query" {
		t.Errorf("kag_query required = %v, want [query]", schema["required"])
	}
}

// ---------------------------------------------------------------------------
// Query-shape logging
// ---------------------------------------------------------------------------

func readQueryLog(t *testing.T, dir string) []map[string]any {
	t.Helper()

	f, err := os.Open(filepath.Join(dir, querylog.FileName))
	if err != nil {
		t.Fatalf("open query log: %v", err)
	}
	defer f.Close()

	var out []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("bad JSONL line %q: %v", scanner.Text(), err)
		}
		out = append(out, rec)
	}
	return out
}

func TestQueryLogRecordsShapeNotContent(t *testing.T) {
	dir := t.TempDir()
	db, _ := testDB(t)
	s := NewWithOptions(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil), Options{
		QueryLogDir:     dir,
		QueryLogEnabled: true,
	})
	cs := connect(t, s)

	const secret = "ProjectVoyager"
	callText(t, cs, ToolKAGQuery, map[string]any{"query": "what about " + secret, "max_hops": 2})
	callText(t, cs, ToolSearch, map[string]any{"query": secret + " storage layout"})
	if err := s.queryLog.Close(); err != nil {
		t.Fatalf("close query log: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, querylog.FileName))
	if err != nil {
		t.Fatalf("read query log: %v", err)
	}
	if strings.Contains(string(raw), secret) {
		t.Errorf("query text leaked into the log:\n%s", raw)
	}

	records := readQueryLog(t, dir)
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2 (one per tool call)", len(records))
	}

	if records[0]["tool"] != ToolKAGQuery {
		t.Errorf("record 0 tool = %v, want %s", records[0]["tool"], ToolKAGQuery)
	}
	if records[0]["hop_depth"] != float64(2) {
		t.Errorf("record 0 hop_depth = %v, want 2", records[0]["hop_depth"])
	}
	if records[0]["graph_enabled"] != false {
		t.Errorf("record 0 graph_enabled = %v, want false", records[0]["graph_enabled"])
	}
	if records[0]["token_count"] != float64(3) {
		t.Errorf("record 0 token_count = %v, want 3", records[0]["token_count"])
	}
	if records[0]["has_entity_pattern"] != true {
		t.Errorf("record 0 has_entity_pattern = %v, want true", records[0]["has_entity_pattern"])
	}

	if records[1]["tool"] != ToolSearch {
		t.Errorf("record 1 tool = %v, want %s", records[1]["tool"], ToolSearch)
	}
	if records[1]["hop_depth"] != float64(0) {
		t.Errorf("record 1 hop_depth = %v, want 0", records[1]["hop_depth"])
	}
}

func TestQueryLogDisabledWritesNothing(t *testing.T) {
	dir := t.TempDir()
	db, _ := testDB(t)
	s := NewWithOptions(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil), Options{
		QueryLogDir:     dir,
		QueryLogEnabled: false,
	})
	cs := connect(t, s)

	callText(t, cs, ToolKAGQuery, map[string]any{"query": "anything"})
	callText(t, cs, ToolSearch, map[string]any{"query": "anything"})

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("query log disabled but %d files were created", len(entries))
	}
}

// TestDefaultServerHasGraphOffAndNoLog pins the default posture of New().
func TestDefaultServerHasGraphOffAndNoLog(t *testing.T) {
	s := testServer(t)

	if s.graph.Enabled() {
		t.Error("default server has the knowledge graph enabled")
	}
	if s.queryLog.Enabled() {
		t.Error("default server writes a query log")
	}
	if s.graphMaxHops != kb.MaxGraphHops {
		t.Errorf("graphMaxHops = %d, want %d", s.graphMaxHops, kb.MaxGraphHops)
	}
}

// TestGraphSchemaOnlyCreatedWhenEnabled proves the off-by-default promise at the
// server boundary: constructing a server with the graph off must not create the
// edge tables.
func TestGraphSchemaOnlyCreatedWhenEnabled(t *testing.T) {
	tableExists := func(t *testing.T, s *Server) bool {
		t.Helper()
		var n int
		err := s.db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='kb_graph_edges'`).Scan(&n)
		if err != nil {
			t.Fatalf("check table: %v", err)
		}
		return n > 0
	}

	t.Run("disabled", func(t *testing.T) {
		// Note: the store's own migrations do not create kb_graph_edges, so a
		// hit here means the server created it.
		if tableExists(t, graphTestServer(t, Options{})) {
			t.Error("kb_graph_edges exists on a server with the graph disabled")
		}
	})

	t.Run("enabled", func(t *testing.T) {
		if !tableExists(t, graphTestServer(t, Options{GraphEnabled: true})) {
			t.Error("kb_graph_edges missing on a server with the graph enabled")
		}
	})
}

// TestGraphMaxHopsClamped keeps the traversal budget honest regardless of what
// a caller or a config file asks for.
func TestGraphMaxHopsClamped(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   int
		want int
	}{
		{"zero falls back to the budget", 0, kb.MaxGraphHops},
		{"negative falls back to the budget", -3, kb.MaxGraphHops},
		{"one is honored", 1, 1},
		{"above the budget is clamped", 99, kb.MaxGraphHops},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := graphTestServer(t, Options{GraphEnabled: true, GraphMaxHops: tc.in})
			if s.graphMaxHops != tc.want {
				t.Errorf("graphMaxHops = %d, want %d", s.graphMaxHops, tc.want)
			}
		})
	}
}
