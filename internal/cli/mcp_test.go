package cli

import (
	"context"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpToolsList starts the KB MCP server over an in-memory transport -- the same
// wiring `conduit mcp kb` uses, minus the stdio pipe -- and returns the tool
// names it advertises.
//
// Going through newKBMCPServer rather than constructing mcpserver directly is
// the point: this covers the CLI's composition (config load, kbservice.Open,
// searcher and graph wiring), which is what WP-3.2 changed. The protocol
// itself is already covered by internal/mcpserver's own tests.
func mcpToolsList(t *testing.T, env *testEnv) []string {
	t.Helper()

	cfg, err := testConfig(env)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	server, svc, err := newKBMCPServer(cfg)
	if err != nil {
		t.Fatalf("build MCP server: %v", err)
	}
	defer svc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "conduit-cli-test", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}

	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestMCPServer_SearchesTheSameKnowledgeBase proves `mcp kb` serves the corpus
// that `kb sync` indexed, which is the whole contract between the two commands.
func TestMCPServer_SearchesTheSameKnowledgeBase(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")
	env.mustRun(t, "kb", "sync")

	cfg, err := testConfig(env)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	server, svc, err := newKBMCPServer(cfg)
	if err != nil {
		t.Fatalf("build MCP server: %v", err)
	}
	defer svc.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer serverSession.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "conduit-cli-test", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer clientSession.Close()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "kb_search",
		Arguments: map[string]any{"query": "authentication"},
	})
	if err != nil {
		t.Fatalf("kb_search: %v", err)
	}
	if res.IsError {
		t.Fatalf("kb_search returned an error result: %+v", res.Content)
	}
	if len(res.Content) == 0 {
		t.Fatal("kb_search returned no content")
	}
}
