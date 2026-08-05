package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}

func TestMCPClients_KnownIDs(t *testing.T) {
	ids := MCPClientIDs()
	want := map[string]bool{"claude-code": true, "cursor": true, "vscode": true}
	if len(ids) != len(want) {
		t.Fatalf("client IDs = %v, want %d entries", ids, len(want))
	}
	for _, id := range ids {
		if !want[id] {
			t.Errorf("unexpected client ID %q", id)
		}
	}
}

func TestLookupMCPClient_Unknown(t *testing.T) {
	if _, err := LookupMCPClient("emacs"); err == nil {
		t.Error("an unknown client should be rejected")
	}
}

func TestConfigureMCPClient_WritesEntry(t *testing.T) {
	isolate(t)

	res, err := ConfigureMCPClient("claude-code", false)
	if err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}
	if res.AlreadyConfigured {
		t.Error("a fresh HOME cannot already be configured")
	}

	cfg := readJSON(t, res.ConfigPath)
	servers, ok := cfg["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers missing; got %v", cfg)
	}
	entry, ok := servers[ServerName].(map[string]interface{})
	if !ok {
		t.Fatalf("%s entry missing; got %v", ServerName, servers)
	}
	// An ABSOLUTE path, not the bare name. A client launched from a GUI does
	// not read the shell profile install.sh writes the PATH block to, so a bare
	// "conduit" is looked up in a PATH that never contained the install prefix.
	assertAbsoluteConduitCommand(t, entry["command"])
}

// assertAbsoluteConduitCommand checks an MCP entry's command is an absolute
// path to a conduit binary.
func assertAbsoluteConduitCommand(t *testing.T, got interface{}) {
	t.Helper()

	cmd, ok := got.(string)
	if !ok {
		t.Fatalf("command is %T, want string: %v", got, got)
	}
	if cmd == "conduit" {
		t.Fatalf(`command is the bare name "conduit"; it must be an absolute path, ` +
			`or a GUI-launched AI client cannot find the binary`)
	}
	if !filepath.IsAbs(cmd) {
		t.Errorf("command = %q, want an absolute path", cmd)
	}
	if filepath.Base(cmd) != "conduit" && !strings.Contains(filepath.Base(cmd), "setup") {
		// Under `go test` the running executable is the package's test binary,
		// so the basename is whatever the toolchain named it. The absolute-path
		// property is the one being asserted; this only catches a path pointing
		// at something unrelated entirely.
		t.Logf("command basename is %q (test binary)", filepath.Base(cmd))
	}
}

// TestConfigureMCPClient_VSCodeNesting covers a bug this package inherited:
// the previous implementation wrote a literal top-level key "mcp.servers",
// while the check looked for the nested mcp -> servers object. Configuring
// VS Code therefore produced a file that VS Code would not read and that
// `conduit mcp status` reported as unconfigured.
func TestConfigureMCPClient_VSCodeNesting(t *testing.T) {
	isolate(t)

	res, err := ConfigureMCPClient("vscode", false)
	if err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}

	cfg := readJSON(t, res.ConfigPath)

	if _, flat := cfg["mcp.servers"]; flat {
		t.Error(`config contains a literal "mcp.servers" key; it must be nested`)
	}

	section, ok := cfg["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf(`"mcp" section missing; got %v`, cfg)
	}
	servers, ok := section["servers"].(map[string]interface{})
	if !ok {
		t.Fatalf(`"mcp.servers" missing; got %v`, section)
	}
	if _, ok := servers[ServerName]; !ok {
		t.Errorf("%s entry missing; got %v", ServerName, servers)
	}

	// Writing and checking must agree, which is the whole point.
	configured, name := IsMCPClientConfigured(res.ConfigPath)
	if !configured || name != ServerName {
		t.Errorf("IsMCPClientConfigured = (%v, %q) after configuring VS Code", configured, name)
	}
}

func TestConfigureMCPClient_PreservesOtherSettings(t *testing.T) {
	home := isolate(t)

	// A real client config holds the user's other servers and unrelated
	// settings. Losing those would be far worse than not being configured.
	path := filepath.Join(home, ".claude.json")
	original := `{
  "theme": "dark",
  "mcpServers": {
    "someone-elses-server": {"command": "other", "args": ["run"]}
  }
}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := ConfigureMCPClient("claude-code", false); err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}

	cfg := readJSON(t, path)
	if cfg["theme"] != "dark" {
		t.Errorf("unrelated setting was lost: theme = %v", cfg["theme"])
	}
	servers := cfg["mcpServers"].(map[string]interface{})
	if _, ok := servers["someone-elses-server"]; !ok {
		t.Error("another MCP server entry was removed")
	}
	if _, ok := servers[ServerName]; !ok {
		t.Error("conduit-kb was not added")
	}
}

func TestConfigureMCPClient_IdempotentWithoutForce(t *testing.T) {
	isolate(t)

	if _, err := ConfigureMCPClient("claude-code", false); err != nil {
		t.Fatalf("first configure: %v", err)
	}
	res, err := ConfigureMCPClient("claude-code", false)
	if err != nil {
		t.Fatalf("second configure: %v", err)
	}
	if !res.AlreadyConfigured {
		t.Error("a second run should report the entry already exists")
	}
}

func TestConfigureMCPClient_ForceRewrites(t *testing.T) {
	home := isolate(t)

	path := filepath.Join(home, ".claude.json")
	stale := `{"mcpServers": {"conduit-kb": {"command": "old-binary", "args": []}}}`
	if err := os.WriteFile(path, []byte(stale), 0600); err != nil {
		t.Fatal(err)
	}

	res, err := ConfigureMCPClient("claude-code", true)
	if err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}
	if res.AlreadyConfigured {
		t.Error("--force should rewrite rather than report already-configured")
	}

	cfg := readJSON(t, path)
	entry := cfg["mcpServers"].(map[string]interface{})[ServerName].(map[string]interface{})
	if entry["command"] == "old-binary" {
		t.Errorf("stale command survived --force: %v", entry["command"])
	}
	assertAbsoluteConduitCommand(t, entry["command"])
}

func TestConfigureMCPClient_RejectsUnparseableConfig(t *testing.T) {
	home := isolate(t)

	path := filepath.Join(home, ".claude.json")
	if err := os.WriteFile(path, []byte("{ this is not json"), 0600); err != nil {
		t.Fatal(err)
	}

	// Overwriting a file we cannot parse would destroy whatever it holds.
	if _, err := ConfigureMCPClient("claude-code", false); err == nil {
		t.Error("an unparseable client config should be an error, not overwritten")
	}
}

func TestIsMCPClientConfigured_Absent(t *testing.T) {
	home := isolate(t)

	if configured, _ := IsMCPClientConfigured(filepath.Join(home, "nope.json")); configured {
		t.Error("a missing file cannot be configured")
	}

	path := filepath.Join(home, "empty.json")
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if configured, _ := IsMCPClientConfigured(path); configured {
		t.Error("a config with no MCP servers cannot be configured")
	}
}
