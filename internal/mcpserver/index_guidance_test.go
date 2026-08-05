package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/kb"
)

// Regression tests for GitHub issue #97 at the MCP protocol level.
//
// A user added two sources, skipped `conduit kb sync`, and searched. The CLI
// said "No results found" and stopped there. The MCP tools said the same, which
// is worse: a human might wonder whether they missed a step, but an AI client
// takes an empty tool result at face value and tells the user, confidently,
// that their documents do not mention the thing they asked about.
//
// Everything asserted here is RESULT TEXT. Tool names, descriptions and input
// schemas are frozen -- see TestToolDescriptionsCarriedOverVerbatim and
// TestToolSchemasAreFrozen, neither of which is touched by this change.

// unsyncedServer returns a server whose knowledge base has a source registered
// and nothing indexed: `kb add` without `kb sync`.
func unsyncedServer(t *testing.T) *Server {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "unsynced.db")
	db, err := openDB(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping (build with CGO_ENABLED=1 -tags fts5)")
		}
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	sources := kb.NewSourceManager(db)
	for _, name := range []string{"project-docs", "meeting-notes"} {
		if _, err := sources.Add(context.Background(), kb.AddSourceRequest{
			Path: t.TempDir(), Name: name, SyncMode: "manual",
		}); err != nil {
			t.Fatalf("add source %s: %v", name, err)
		}
	}

	return New(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil))
}

// TestIssue97_UnsyncedKBTellsTheClientToSync is the reported case, over the
// wire, for every tool that can return an empty result.
func TestIssue97_UnsyncedKBTellsTheClientToSync(t *testing.T) {
	cs := connect(t, unsyncedServer(t))

	cases := []struct {
		tool string
		args map[string]any
	}{
		{ToolSearch, map[string]any{"query": "authentication"}},
		{ToolLexicalSearch, map[string]any{"query": "authentication"}},
		{ToolSearchWithContext, map[string]any{"query": "authentication"}},
		{ToolKAGQuery, map[string]any{"query": "authentication"}},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			res := callText(t, cs, tc.tool, tc.args)
			if res.IsError {
				t.Fatalf("%s reported a tool error: %s", tc.tool, resultText(t, res))
			}
			text := resultText(t, res)

			if !strings.Contains(text, "conduit kb sync") {
				t.Errorf("%s returned an empty result without naming `conduit kb sync`:\n%s",
					tc.tool, text)
			}
			if !strings.Contains(text, "never been synced") {
				t.Errorf("%s does not say the sources were never indexed:\n%s", tc.tool, text)
			}
			// The client has to be able to tell "we have no such content" from
			// "we have not looked yet".
			if !strings.Contains(text, "says nothing about whether the content exists") {
				t.Errorf("%s does not warn against reading this as absence of content:\n%s",
					tc.tool, text)
			}
			// The sources are named so the user knows which ones.
			if !strings.Contains(text, "project-docs") || !strings.Contains(text, "meeting-notes") {
				t.Errorf("%s does not name the unsynced sources:\n%s", tc.tool, text)
			}
		})
	}
}

// TestIssue97_SyncedButEmptyKeepsThePlainMessage is the other half.
//
// A populated knowledge base that simply does not contain the query must NOT be
// dressed up as a configuration problem. A note on every empty search is noise,
// and noise is how a real warning gets ignored.
func TestIssue97_SyncedButEmptyKeepsThePlainMessage(t *testing.T) {
	cs := connect(t, testServer(t)) // fixture source is marked synced

	for _, tool := range []string{ToolSearch, ToolLexicalSearch, ToolSearchWithContext} {
		t.Run(tool, func(t *testing.T) {
			res := callText(t, cs, tool, map[string]any{"query": "zzzznonexistentterm"})
			text := resultText(t, res)

			if !strings.Contains(text, "zzzznonexistentterm") {
				t.Fatalf("%s did not echo the query:\n%s", tool, text)
			}
			if strings.Contains(text, "conduit kb sync") {
				t.Errorf("%s advised syncing a knowledge base that is already indexed:\n%s",
					tool, text)
			}
			if strings.Contains(text, "index: incomplete") {
				t.Errorf("%s flagged an incomplete index on a synced knowledge base:\n%s",
					tool, text)
			}
		})
	}
}

// TestIssue97_GuidanceDoesNotAppearOnSuccessfulSearches: the note belongs on
// the empty path only.
func TestIssue97_GuidanceDoesNotAppearOnSuccessfulSearches(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolSearch, map[string]any{"query": "authentication"})
	text := resultText(t, res)
	if strings.Contains(text, "index: incomplete") {
		t.Errorf("guidance leaked into a result that found documents:\n%s", text)
	}
}

// TestIssue97_ToolSchemasUnchanged states the constraint explicitly, next to
// the change that could have violated it.
//
// The guidance is appended to RESULT TEXT. Nothing about the wire contract --
// tool names, descriptions, argument names, types or required-ness -- moves,
// because AI clients are prompt-tuned against those. The authoritative
// assertions are TestToolDescriptionsCarriedOverVerbatim and
// TestToolSchemasAreFrozen; this checks that the empty-result path did not
// smuggle in a new tool.
func TestIssue97_ToolSchemasUnchanged(t *testing.T) {
	cs := connect(t, unsyncedServer(t))

	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	want := map[string]bool{
		ToolSearch: true, ToolLexicalSearch: true, ToolSearchWithContext: true,
		ToolListSources: true, ToolGetDocument: true, ToolStats: true,
		ToolKAGQuery: true,
	}
	for _, tool := range tools.Tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool on the wire: %q", tool.Name)
		}
		delete(want, tool.Name)
	}
	for name := range want {
		t.Errorf("tool disappeared from the wire: %q", name)
	}
}
