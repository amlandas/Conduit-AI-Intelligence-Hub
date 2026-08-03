package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/store"
)

// init silences the global zerolog logger for this test binary. The kb package
// logs at debug level on every search; a firehose would drown the assertions.
// It also matters for the stdout-purity helper process, which re-enables
// logging explicitly.
func init() {
	zerolog.SetGlobalLevel(zerolog.Disabled)
}

// ---------------------------------------------------------------------------
// Fixture
// ---------------------------------------------------------------------------

// fixtureDoc is one document in the minimal test corpus. The golden-corpus
// helpers live in package-internal _test.go files of internal/kb and are not
// importable, so this package carries its own tiny fixture built through the
// same exported ingestion APIs (SourceManager -> Chunker -> Indexer).
type fixtureDoc struct {
	id      string
	title   string
	path    string
	content string
}

var fixtureDocs = []fixtureDoc{
	{
		id:    "doc-auth",
		title: "Authentication Design",
		path:  "/corpus/authentication.md",
		content: "Authentication Design\n\n" +
			"Conduit validates every bearer token against the identity provider before a request reaches a handler. " +
			"The token signature and expiry claim are both checked. " +
			"A rejected token produces ErrUnauthorizedToken and the request is dropped.",
	},
	{
		id:    "doc-storage",
		title: "Storage Layout",
		path:  "/corpus/storage.md",
		content: "Storage Layout\n\n" +
			"Documents and chunks live in SQLite. " +
			"The chunk table is mirrored into an FTS5 virtual table so keyword queries can be answered without a vector store. " +
			"Vectors, when available, live in Qdrant.",
	},
	{
		id:    "doc-telemetry",
		title: "Telemetry Notes",
		path:  "/corpus/telemetry.md",
		content: "Telemetry Notes\n\n" +
			"Nothing leaves the machine. " +
			"Counters are kept in memory and printed on demand. " +
			"There is no phone-home and no crash reporter.",
	},
}

// testServer builds a Server over a throwaway SQLite database seeded with
// fixtureDocs, plus an FTS5-only hybrid searcher (no Ollama, no Qdrant).
func testServer(t *testing.T) *Server {
	t.Helper()

	db, _ := testDB(t)
	return New(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil))
}

// testDB opens a temp store with the full Conduit schema and ingests the
// fixture corpus. It returns the database handle and the registered source ID.
func testDB(t *testing.T) (*sql.DB, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "mcpserver-test.db")
	db, sourceID, err := openAndSeed(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping (build with CGO_ENABLED=1 -tags fts5)")
		}
		t.Fatalf("seed test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, sourceID
}

// openDB opens (or creates) a Conduit store at dbPath and returns its database
// handle. Migrations are idempotent, so re-opening a seeded file is safe.
func openDB(dbPath string) (*sql.DB, error) {
	st, err := store.New(dbPath)
	if err != nil {
		return nil, err
	}
	return st.DB(), nil
}

// openAndSeed opens a store and ingests the fixture corpus. It is used by the
// in-process tests and by the parent of the stdout-purity subprocess, which
// seeds the file once and then lets the subprocess re-open it read-write.
func openAndSeed(dbPath string) (*sql.DB, string, error) {
	db, err := openDB(dbPath)
	if err != nil {
		return nil, "", err
	}
	ctx := context.Background()

	sources := kb.NewSourceManager(db)
	src, err := sources.Add(ctx, kb.AddSourceRequest{
		Path:     filepath.Dir(dbPath),
		Name:     "mcpserver-fixture",
		SyncMode: "manual",
	})
	if err != nil {
		return nil, "", err
	}

	chunker := kb.NewChunker()
	indexer := kb.NewIndexer(db)
	modified := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	for _, fd := range fixtureDocs {
		chunks := chunker.Chunk(fd.content, kb.ChunkOptions{MaxSize: 1000, Overlap: 100})
		doc := &kb.Document{
			DocumentID: fd.id,
			SourceID:   src.SourceID,
			Path:       fd.path,
			Title:      fd.title,
			MimeType:   "text/plain",
			Size:       int64(len(fd.content)),
			ModifiedAt: modified,
		}
		if err := indexer.Index(ctx, doc, chunks); err != nil {
			return nil, "", err
		}
	}

	return db, src.SourceID, nil
}

// connect wires a client to the server over in-process pipes and returns the
// client session. The handshake (server/discover under the current spec, with
// automatic fallback for older servers) happens inside Client.Connect.
func connect(t *testing.T, s *Server) *mcp.ClientSession {
	t.Helper()

	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := s.Connect(ctx, serverTransport)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "mcpserver-test", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}

	t.Cleanup(func() {
		_ = clientSession.Close()
		_ = serverSession.Wait()
	})

	return clientSession
}

// callText runs a tool and returns the concatenation of its text content
// blocks, failing the test if the call errored at the protocol level.
func callText(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("call %s: %v", name, err)
	}
	return res
}

func resultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()

	var sb strings.Builder
	for _, c := range res.Content {
		tc, ok := c.(*mcp.TextContent)
		if !ok {
			t.Fatalf("unexpected content type %T", c)
		}
		sb.WriteString(tc.Text)
		sb.WriteString("\n")
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// Handshake
// ---------------------------------------------------------------------------

func TestHandshakeNegotiatesCurrentSpec(t *testing.T) {
	cs := connect(t, testServer(t))

	init := cs.InitializeResult()
	if init == nil {
		t.Fatal("nil InitializeResult after connect")
	}
	if init.ProtocolVersion != "2026-07-28" {
		t.Errorf("protocol version = %q, want 2026-07-28", init.ProtocolVersion)
	}
	if init.ServerInfo == nil {
		t.Fatal("nil ServerInfo")
	}
	if init.ServerInfo.Name != ServerName {
		t.Errorf("server name = %q, want %q", init.ServerInfo.Name, ServerName)
	}
	if init.ServerInfo.Version != ServerVersion {
		t.Errorf("server version = %q, want %q", init.ServerInfo.Version, ServerVersion)
	}
	if init.Capabilities == nil || init.Capabilities.Tools == nil {
		t.Error("server did not advertise the tools capability")
	}
	// The previous server advertised tools, resources and prompts but never
	// logging; that shape is preserved.
	if init.Capabilities != nil && init.Capabilities.Logging != nil {
		t.Error("server advertised the (deprecated) logging capability")
	}
	if init.Capabilities != nil && init.Capabilities.Resources == nil {
		t.Error("server did not advertise the resources capability")
	}
	if init.Capabilities != nil && init.Capabilities.Prompts == nil {
		t.Error("server did not advertise the prompts capability")
	}
}

// ---------------------------------------------------------------------------
// tools/list
// ---------------------------------------------------------------------------

func TestListToolsReturnsSevenDescribedTools(t *testing.T) {
	cs := connect(t, testServer(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	if len(res.Tools) != len(ToolNames) {
		names := make([]string, len(res.Tools))
		for i, tool := range res.Tools {
			names[i] = tool.Name
		}
		t.Fatalf("got %d tools %v, want %d", len(res.Tools), names, len(ToolNames))
	}

	byName := make(map[string]*mcp.Tool, len(res.Tools))
	for _, tool := range res.Tools {
		byName[tool.Name] = tool
	}

	for _, want := range ToolNames {
		tool, ok := byName[want]
		if !ok {
			t.Errorf("tool %q missing from tools/list", want)
			continue
		}
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("tool %q has an empty description", want)
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("tool %q input schema is %T, want a JSON object", want, tool.InputSchema)
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q input schema type = %v, want object", want, schema["type"])
		}
	}
}

// TestToolDescriptionsCarriedOverVerbatim pins the descriptions that AI clients
// have been prompt-tuned against. If one of these fails, the change was very
// probably unintentional.
func TestToolDescriptionsCarriedOverVerbatim(t *testing.T) {
	cs := connect(t, testServer(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	byName := make(map[string]string, len(res.Tools))
	for _, tool := range res.Tools {
		byName[tool.Name] = tool.Description
	}

	// Two of these were changed deliberately for #91, and only these two. The
	// original kb_search wording is preserved verbatim with one sentence
	// appended; kb_get_document was rewritten because the old text ("by its ID.
	// Use document IDs from search results") described a flow that did not
	// exist -- search results carried no ID.
	want := map[string]string{
		ToolSearch: "Search the knowledge base for relevant documents using hybrid search (FTS5 keyword matching + semantic similarity when available). Use short keyword phrases for best results. " +
			"Every hit prints a 'document_id:' line -- pass that value to kb_get_document to read the whole document.",
		ToolListSources: "List all knowledge base sources with their IDs, paths, document counts, and sync status. Use this to discover available sources before searching or filtering.",
		ToolGetDocument: "Retrieve the full content of a specific document. Supply exactly one of document_id or path. " +
			"Flow: run kb_search (or kb_lexical_search / kb_search_with_context), copy the 'document_id:' value printed under the hit you want, then pass it here.",
		ToolStats: "Get knowledge base statistics including source counts, document counts, chunk counts, and search capability status.",
		ToolSearchWithContext: "Search with processed, prompt-ready results. Returns merged chunks from same documents, filters boilerplate, and provides citation-ready source information. Best for RAG use cases.",
		ToolKAGQuery:          "Query the knowledge graph for entities and their relationships. Use for multi-hop reasoning, aggregation queries, or finding connections between concepts. Complements RAG search with structured entity lookups.",
	}

	for name, wantDesc := range want {
		if got := byName[name]; got != wantDesc {
			t.Errorf("description drift for %s:\n got: %q\nwant: %q", name, got, wantDesc)
		}
	}
}

// ---------------------------------------------------------------------------
// tools/call
// ---------------------------------------------------------------------------

func TestCallKBSearch(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolSearch, map[string]any{"query": "authentication token"})
	if res.IsError {
		t.Fatalf("kb_search reported a tool error: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Authentication Design") {
		t.Errorf("kb_search did not surface the expected document:\n%s", text)
	}
	// Result-shape contract: "**Title** (score: N)\nPath: ...\n\n<snippet>".
	if !strings.Contains(text, "(score: ") || !strings.Contains(text, "Path: /corpus/authentication.md") {
		t.Errorf("kb_search result lost its citation formatting:\n%s", text)
	}
}

func TestCallKBSearchNoResults(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolSearch, map[string]any{"query": "zzzznonexistentterm"})
	if res.IsError {
		t.Fatalf("kb_search reported a tool error: %s", resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, "No results found for: zzzznonexistentterm") {
		t.Errorf("missing empty-result message:\n%s", text)
	}
}

func TestCallKBLexicalSearch(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolLexicalSearch, map[string]any{"query": "FTS5 virtual"})
	if res.IsError {
		t.Fatalf("kb_lexical_search reported a tool error: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "Storage Layout") {
		t.Errorf("kb_lexical_search did not surface the expected document:\n%s", text)
	}
	// Same citation fields as kb_search.
	if !strings.Contains(text, "(score: ") || !strings.Contains(text, "Path: /corpus/storage.md") {
		t.Errorf("kb_lexical_search result lost its citation formatting:\n%s", text)
	}
}

func TestCallKBLexicalSearchRespectsSourceFilter(t *testing.T) {
	db, sourceID := testDB(t)
	s := New(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil))
	cs := connect(t, s)

	hit := callText(t, cs, ToolLexicalSearch, map[string]any{"query": "telemetry", "source_id": sourceID})
	if text := resultText(t, hit); !strings.Contains(text, "Telemetry Notes") {
		t.Errorf("matching source filter dropped the hit:\n%s", text)
	}

	miss := callText(t, cs, ToolLexicalSearch, map[string]any{"query": "telemetry", "source_id": "no-such-source"})
	if text := resultText(t, miss); !strings.Contains(text, "No results found for: telemetry") {
		t.Errorf("non-matching source filter should return no results:\n%s", text)
	}
}

func TestCallKBListSourcesAndStats(t *testing.T) {
	cs := connect(t, testServer(t))

	sources := resultText(t, callText(t, cs, ToolListSources, nil))
	if !strings.Contains(sources, "# Knowledge Base Sources") || !strings.Contains(sources, "mcpserver-fixture") {
		t.Errorf("kb_list_sources output changed shape:\n%s", sources)
	}

	stats := resultText(t, callText(t, cs, ToolStats, map[string]any{}))
	if !strings.Contains(stats, "# Knowledge Base Statistics") || !strings.Contains(stats, "- **Documents**:") {
		t.Errorf("kb_stats output changed shape:\n%s", stats)
	}
}

func TestCallKBGetDocument(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolGetDocument, map[string]any{"document_id": "doc-auth"})
	if res.IsError {
		t.Fatalf("kb_get_document reported a tool error: %s", resultText(t, res))
	}
	text := resultText(t, res)
	for _, want := range []string{"# Authentication Design", "Path: /corpus/authentication.md", "Type: text/plain", "ErrUnauthorizedToken"} {
		if !strings.Contains(text, want) {
			t.Errorf("kb_get_document output missing %q:\n%s", want, text)
		}
	}
}

func TestCallKBSearchWithContext(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolSearchWithContext, map[string]any{"query": "authentication token"})
	if res.IsError {
		t.Fatalf("kb_search_with_context reported a tool error: %s", resultText(t, res))
	}
	text := resultText(t, res)
	if !strings.Contains(text, "## Relevant Context") || !strings.Contains(text, "*Found ") {
		t.Errorf("kb_search_with_context output changed shape:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// Error shapes
// ---------------------------------------------------------------------------

// TestCallUnknownToolIsProtocolError documents a deliberate change from the
// hand-rolled server, which answered every failure with a generic -32000. Per
// the MCP spec, "errors in finding the tool" are protocol errors; the SDK
// returns InvalidParams (-32602).
func TestCallUnknownToolIsProtocolError(t *testing.T) {
	cs := connect(t, testServer(t))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "kb_does_not_exist"})
	if err == nil {
		t.Fatalf("expected a protocol error, got result %+v", res)
	}

	var wireErr *jsonrpc.Error
	if !errors.As(err, &wireErr) {
		t.Fatalf("error is %T (%v), want *jsonrpc.Error", err, err)
	}
	if wireErr.Code != -32602 {
		t.Errorf("error code = %d, want -32602 (invalid params)", wireErr.Code)
	}
	if !strings.Contains(wireErr.Message, "kb_does_not_exist") {
		t.Errorf("error message %q does not name the unknown tool", wireErr.Message)
	}
}

// TestCallToolInvalidArgs covers schema validation. Per the MCP spec, tool-level
// failures come back as a normal result with isError set so the model can see
// and correct them, rather than as a JSON-RPC error.
func TestCallToolInvalidArgs(t *testing.T) {
	cs := connect(t, testServer(t))

	tests := []struct {
		name string
		tool string
		args map[string]any
	}{
		{"missing required query", ToolSearch, map[string]any{}},
		{"wrong type for limit", ToolSearch, map[string]any{"query": "auth", "limit": "ten"}},
		{"value outside enum", ToolSearch, map[string]any{"query": "auth", "mode": "telepathy"}},
		// #91 moved this one from schema validation to the handler: with two
		// alternative keys neither is individually required, so an empty call is
		// now a handler-level isError. TestGetDocumentRequiresExactlyOneKey
		// pins the message.
		{"no lookup key at all", ToolGetDocument, map[string]any{}},
		{"missing required query on lexical", ToolLexicalSearch, map[string]any{}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
			if err != nil {
				t.Fatalf("expected an isError result, got protocol error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError, got %s", resultText(t, res))
			}
			if text := resultText(t, res); strings.TrimSpace(text) == "" {
				t.Error("isError result carried no explanatory text")
			}
		})
	}
}

// TestCallToolHandlerErrorIsToolError checks that a handler-level failure (here
// an unknown document ID) is reported as isError rather than as a protocol
// error, preserving the original message text.
func TestCallToolHandlerErrorIsToolError(t *testing.T) {
	cs := connect(t, testServer(t))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      ToolGetDocument,
		Arguments: map[string]any{"document_id": "no-such-doc"},
	})
	if err != nil {
		t.Fatalf("expected an isError result, got protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError, got %s", resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, "get document") {
		t.Errorf("lost the original error message:\n%s", text)
	}
}

// ---------------------------------------------------------------------------
// Resources and prompts
// ---------------------------------------------------------------------------

func TestResourcesListAndRead(t *testing.T) {
	db, sourceID := testDB(t)
	cs := connect(t, New(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil)))

	list, err := cs.ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("list resources: %v", err)
	}
	wantURI := "kb://source/" + sourceID
	var found bool
	for _, r := range list.Resources {
		if r.URI == wantURI {
			found = true
			if r.MIMEType != "application/json" {
				t.Errorf("resource mime type = %q, want application/json", r.MIMEType)
			}
		}
	}
	if !found {
		t.Fatalf("resources/list did not include %q (got %+v)", wantURI, list.Resources)
	}

	read, err := cs.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: wantURI})
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("got %d resource contents, want 1", len(read.Contents))
	}
	if !strings.Contains(read.Contents[0].Text, sourceID) {
		t.Errorf("resource body does not mention the source id:\n%s", read.Contents[0].Text)
	}
}

func TestPromptsListAndGet(t *testing.T) {
	cs := connect(t, testServer(t))

	list, err := cs.ListPrompts(context.Background(), nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(list.Prompts) != 1 || list.Prompts[0].Name != PromptContext {
		t.Fatalf("unexpected prompt list: %+v", list.Prompts)
	}
	if list.Prompts[0].Description != "Get relevant context from knowledge base for a topic" {
		t.Errorf("prompt description drift: %q", list.Prompts[0].Description)
	}

	got, err := cs.GetPrompt(context.Background(), &mcp.GetPromptParams{
		Name:      PromptContext,
		Arguments: map[string]string{"topic": "authentication token"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	if len(got.Messages) != 1 {
		t.Fatalf("got %d prompt messages, want 1", len(got.Messages))
	}
	tc, ok := got.Messages[0].Content.(*mcp.TextContent)
	if !ok {
		t.Fatalf("prompt content is %T, want *mcp.TextContent", got.Messages[0].Content)
	}
	if !strings.Contains(tc.Text, "Relevant Context") {
		t.Errorf("prompt body does not carry KB context:\n%s", tc.Text)
	}
}

// ---------------------------------------------------------------------------
// Unit coverage for chunk reassembly, ported alongside the handler.
// ---------------------------------------------------------------------------

// TestDegradedBannerIsSurfaced covers the banner the previous server computed
// and discarded (WP-3.4).
//
// The old code built a "<mode> (degraded - semantic unavailable)" string in
// toolSearch and never emitted it, so a client receiving hits from a
// half-working engine was told nothing. The only degraded signal that reached
// anyone was result.Note on the empty-result path.
func TestDegradedBannerIsSurfaced(t *testing.T) {
	t.Run("healthy searches carry no banner", func(t *testing.T) {
		cs := connect(t, testServer(t))
		text := resultText(t, callText(t, cs, ToolSearch, map[string]any{"query": "authentication token"}))
		if strings.Contains(text, "retrieval: degraded") {
			t.Errorf("a healthy search emitted a degraded banner:\n%s", text)
		}
	})

	t.Run("a failed strategy is announced", func(t *testing.T) {
		db, _ := testDB(t)
		// Dropping the FTS table makes the lexical half fail outright, which is
		// a failure rather than a miss and must be reported as such.
		if _, err := db.ExecContext(context.Background(), `DROP TABLE kb_fts`); err != nil {
			t.Fatalf("drop kb_fts: %v", err)
		}
		cs := connect(t, New(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil)))

		text := resultText(t, callText(t, cs, ToolSearch, map[string]any{"query": "authentication token"}))
		if !strings.Contains(text, "retrieval: degraded") {
			t.Errorf("kb_search did not announce degraded mode:\n%s", text)
		}
		if !strings.Contains(text, "Lexical search failed") {
			t.Errorf("the banner does not name the failure:\n%s", text)
		}

		ctxText := resultText(t, callText(t, cs, ToolSearchWithContext, map[string]any{"query": "authentication token"}))
		if !strings.Contains(ctxText, "retrieval: degraded") {
			t.Errorf("kb_search_with_context did not announce degraded mode:\n%s", ctxText)
		}
	})
}

// TestToolSchemasAreFrozen pins the wire contract for every tool: argument
// names, types and required-ness. Descriptions are pinned separately by
// TestToolDescriptionsCarriedOverVerbatim.
//
// AI clients are prompt-tuned against these. WP-3.4 reshaped
// kb.HybridSearchOptions underneath the handlers; this test is what makes that
// safe, by proving none of it reached the wire.
func TestToolSchemasAreFrozen(t *testing.T) {
	cs := connect(t, testServer(t))

	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := map[string][]string{
		ToolSearch:            {"query", "limit", "source_id", "mode", "recall_mode"},
		ToolLexicalSearch:     {"query", "limit", "source_id"},
		ToolSearchWithContext: {"query", "source_id", "limit", "mode", "recall_mode"},
		ToolListSources: {},
		// #91: `path` is a deliberate addition -- kb_get_document accepts either
		// key, exactly one of them, and the one-of constraint is enforced by the
		// handler rather than by the schema. `document_id` is consequently no
		// longer schema-required; TestGetDocumentRequiresExactlyOneKey covers
		// the constraint that replaced it.
		ToolGetDocument: {"document_id", "path"},
		ToolStats:       {"source_id"},
		ToolKAGQuery:          {"query", "entities", "include_relations", "max_hops", "limit", "source_id"},
	}

	for _, tool := range res.Tools {
		wantProps, ok := want[tool.Name]
		if !ok {
			t.Errorf("unexpected tool %q on the wire", tool.Name)
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Errorf("tool %q input schema is %T, want a JSON object", tool.Name, tool.InputSchema)
			continue
		}
		props, _ := schema["properties"].(map[string]any)
		var got []string
		for name := range props {
			got = append(got, name)
		}
		sort.Strings(got)
		wantSorted := append([]string(nil), wantProps...)
		sort.Strings(wantSorted)
		if len(got) != len(wantSorted) {
			t.Errorf("tool %q properties: got %v, want %v", tool.Name, got, wantSorted)
			continue
		}
		for i := range got {
			if got[i] != wantSorted[i] {
				t.Errorf("tool %q properties: got %v, want %v", tool.Name, got, wantSorted)
				break
			}
		}
	}
}

func TestRemoveOverlaps(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		want  string
	}{
		{"empty", nil, ""},
		{"single", []string{"abc"}, "abc"},
		{"overlapping", []string{"hello world", "world peace"}, "hello world peace"},
		{"disjoint", []string{"abc", "def"}, "abcdef"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := removeOverlaps(tc.parts); got != tc.want {
				t.Errorf("removeOverlaps(%q) = %q, want %q", tc.parts, got, tc.want)
			}
		})
	}
}
