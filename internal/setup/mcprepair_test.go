package setup

// Repairing an MCP entry that already exists but names the wrong command.
//
// Writing the absolute path fixed new installs and reached nobody else.
// ConfigureMCPClient short-circuited on the mere PRESENCE of the conduit-kb
// key, so every user configured before that change kept an entry naming the
// bare command "conduit" -- and re-running `conduit setup` or
// `conduit mcp configure`, the obvious repair, reported "already configured"
// and changed nothing.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeClaudeConfig seeds ~/.claude.json with the given JSON.
func writeClaudeConfig(t *testing.T, home, body string) string {
	t.Helper()
	path := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return path
}

// serverEntry returns one MCP server's object from a config file.
func serverEntry(t *testing.T, path, name string) map[string]interface{} {
	t.Helper()
	cfg := readJSON(t, path)
	servers, ok := cfg["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers missing in %s: %v", path, cfg)
	}
	entry, ok := servers[name].(map[string]interface{})
	if !ok {
		t.Fatalf("server %q missing in %s: %v", name, path, servers)
	}
	return entry
}

// The exact state every pre-fix install is in.
func TestBareCommandEntryIsRepaired(t *testing.T) {
	home := isolate(t)
	path := writeClaudeConfig(t, home,
		`{"mcpServers":{"conduit-kb":{"command":"conduit","args":["mcp","kb"]}}}`)

	res, err := ConfigureMCPClient("claude-code", false)
	if err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}

	if res.AlreadyConfigured {
		t.Fatal("a bare-name entry was reported as already configured; " +
			"re-running the tool is how a user repairs this, and it did nothing")
	}
	if !res.Repaired {
		t.Error("the result does not report that an existing entry was repaired")
	}
	if res.PreviousCommand != "conduit" {
		t.Errorf("PreviousCommand = %q, want \"conduit\"", res.PreviousCommand)
	}

	entry := serverEntry(t, path, ServerName)
	if entry["command"] == "conduit" {
		t.Fatal(`the entry still names the bare command "conduit"`)
	}
	assertAbsoluteConduitCommand(t, entry["command"])
}

// Moving an install with --prefix leaves the old absolute path behind, which is
// the same defect one step along: the entry points at a binary that may no
// longer exist.
func TestStalePrefixEntryIsRepaired(t *testing.T) {
	home := isolate(t)
	stale := filepath.Join(home, "old", "prefix", "conduit")
	body, err := json.Marshal(map[string]interface{}{
		"mcpServers": map[string]interface{}{
			ServerName: map[string]interface{}{
				"command": stale,
				"args":    []string{"mcp", "kb"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := writeClaudeConfig(t, home, string(body))

	res, cerr := ConfigureMCPClient("claude-code", false)
	if cerr != nil {
		t.Fatalf("ConfigureMCPClient: %v", cerr)
	}
	if res.AlreadyConfigured {
		t.Fatal("an entry pointing at a different prefix was left in place")
	}
	if !res.Repaired || res.PreviousCommand != stale {
		t.Errorf("Repaired=%v PreviousCommand=%q, want true and %q",
			res.Repaired, res.PreviousCommand, stale)
	}

	entry := serverEntry(t, path, ServerName)
	if entry["command"] == stale {
		t.Error("the stale path survived")
	}
	if entry["command"] != ConduitCommand() {
		t.Errorf("command = %v, want %q", entry["command"], ConduitCommand())
	}
}

// The repair rewrites ONE key. A config file holds the user's other MCP servers
// and, for ~/.claude.json, Claude Code's own state; losing any of that would be
// a far worse failure than a stale command.
func TestRepairLeavesEverythingElseAlone(t *testing.T) {
	home := isolate(t)
	path := writeClaudeConfig(t, home, `{
  "numStartups": 42,
  "theme": "dark",
  "mcpServers": {
    "conduit-kb": {"command": "conduit", "args": ["mcp", "kb"]},
    "github":   {"command": "gh-mcp", "args": ["serve"], "env": {"TOKEN": "x"}},
    "postgres": {"command": "pg-mcp", "args": []}
  },
  "projects": {"/some/path": {"allowedTools": ["Read"]}}
}`)

	if _, err := ConfigureMCPClient("claude-code", false); err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}

	cfg := readJSON(t, path)

	// Unrelated top-level settings.
	if cfg["theme"] != "dark" {
		t.Errorf("theme = %v, want dark", cfg["theme"])
	}
	if cfg["numStartups"] != float64(42) {
		t.Errorf("numStartups = %v, want 42", cfg["numStartups"])
	}
	if _, ok := cfg["projects"].(map[string]interface{}); !ok {
		t.Error("the projects section was lost")
	}

	// Other people's servers, including nested values.
	gh := serverEntry(t, path, "github")
	if gh["command"] != "gh-mcp" {
		t.Errorf("another server's command was rewritten: %v", gh["command"])
	}
	env, ok := gh["env"].(map[string]interface{})
	if !ok || env["TOKEN"] != "x" {
		t.Errorf("another server's env was lost: %v", gh["env"])
	}
	if pg := serverEntry(t, path, "postgres"); pg["command"] != "pg-mcp" {
		t.Errorf("another server's command was rewritten: %v", pg["command"])
	}

	// And ours is fixed.
	assertAbsoluteConduitCommand(t, serverEntry(t, path, ServerName)["command"])
}

// A correct entry must still be a no-op, or every run rewrites the file and
// "already configured" stops meaning anything.
func TestCorrectEntryIsStillANoOp(t *testing.T) {
	home := isolate(t)

	// Configure once, from scratch.
	if _, err := ConfigureMCPClient("claude-code", false); err != nil {
		t.Fatalf("first configure: %v", err)
	}
	path := filepath.Join(home, ".claude.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	res, err := ConfigureMCPClient("claude-code", false)
	if err != nil {
		t.Fatalf("second configure: %v", err)
	}
	if !res.AlreadyConfigured {
		t.Error("a correct entry was not reported as already configured")
	}
	if res.Repaired {
		t.Error("a correct entry was reported as repaired")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("the file was rewritten though nothing needed changing:\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
}

// An entry whose command is missing or not a string is malformed rather than
// merely stale. It must be replaced, not crash.
func TestMalformedEntryIsReplaced(t *testing.T) {
	for name, body := range map[string]string{
		"no command":          `{"mcpServers":{"conduit-kb":{"args":["mcp","kb"]}}}`,
		"command is null":     `{"mcpServers":{"conduit-kb":{"command":null}}}`,
		"command is a number": `{"mcpServers":{"conduit-kb":{"command":7}}}`,
		"entry is a string":   `{"mcpServers":{"conduit-kb":"nonsense"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			home := isolate(t)
			path := writeClaudeConfig(t, home, body)

			if _, err := ConfigureMCPClient("claude-code", false); err != nil {
				t.Fatalf("ConfigureMCPClient: %v", err)
			}
			assertAbsoluteConduitCommand(t, serverEntry(t, path, ServerName)["command"])
		})
	}
}
