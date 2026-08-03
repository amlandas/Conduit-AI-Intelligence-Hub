package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveMCPClient_DeletesOnlyConduit(t *testing.T) {
	home := isolate(t)

	// A realistic config: Conduit alongside somebody else's server, plus
	// unrelated top-level settings that an uninstall must not touch.
	path := filepath.Join(home, ".claude.json")
	original := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			ServerName: map[string]interface{}{"command": "conduit"},
			"other":    map[string]interface{}{"command": "somethingelse"},
		},
		"theme":       "dark",
		"otherConfig": map[string]interface{}{"nested": true},
	}
	data, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	res, err := RemoveMCPClient("claude-code")
	if err != nil {
		t.Fatalf("RemoveMCPClient: %v", err)
	}
	if !res.Removed {
		t.Error("Removed = false, want true")
	}

	cfg := readJSON(t, path)
	servers, ok := cfg["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers disappeared: %v", cfg)
	}
	if _, exists := servers[ServerName]; exists {
		t.Error("Conduit entry survived removal")
	}
	if _, exists := servers["other"]; !exists {
		t.Error("an unrelated MCP server was removed")
	}
	if cfg["theme"] != "dark" {
		t.Errorf("unrelated setting lost: theme = %v", cfg["theme"])
	}
	if _, exists := cfg["otherConfig"]; !exists {
		t.Error("unrelated nested settings were lost")
	}
}

// Uninstall must be safe to run twice, so removing an absent entry is a
// success with Removed=false, not an error.
func TestRemoveMCPClient_Idempotent(t *testing.T) {
	isolate(t)

	res, err := RemoveMCPClient("claude-code")
	if err != nil {
		t.Fatalf("RemoveMCPClient on a missing file: %v", err)
	}
	if !res.Missing || res.Removed {
		t.Errorf("missing file: Missing=%v Removed=%v, want true/false", res.Missing, res.Removed)
	}

	if _, err := ConfigureMCPClient("claude-code", false); err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}

	first, err := RemoveMCPClient("claude-code")
	if err != nil {
		t.Fatalf("first removal: %v", err)
	}
	if !first.Removed {
		t.Fatal("first removal did not remove the entry")
	}

	second, err := RemoveMCPClient("claude-code")
	if err != nil {
		t.Fatalf("second removal: %v", err)
	}
	if second.Removed {
		t.Error("second removal reported Removed=true")
	}
}

// VS Code nests its servers, so removal has to walk the same dotted path that
// configuration writes.
func TestRemoveMCPClient_NestedServersKey(t *testing.T) {
	isolate(t)

	if _, err := ConfigureMCPClient("vscode", false); err != nil {
		t.Fatalf("ConfigureMCPClient: %v", err)
	}
	client, err := LookupMCPClient("vscode")
	if err != nil {
		t.Fatalf("LookupMCPClient: %v", err)
	}
	if configured, _ := IsMCPClientConfigured(client.ConfigPath); !configured {
		t.Fatal("vscode config was not written")
	}

	if _, err := RemoveMCPClient("vscode"); err != nil {
		t.Fatalf("RemoveMCPClient: %v", err)
	}
	if configured, _ := IsMCPClientConfigured(client.ConfigPath); configured {
		t.Error("vscode entry survived removal")
	}
}

// A config we cannot parse is somebody's hand-edited file. Report it; never
// overwrite it.
func TestRemoveMCPClient_LeavesUnparseableConfigAlone(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".claude.json")

	junk := []byte("{ this is not json")
	if err := os.WriteFile(path, junk, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := RemoveMCPClient("claude-code"); err == nil {
		t.Error("an unparseable config should be reported as an error")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(after) != string(junk) {
		t.Errorf("unparseable config was modified: %q", after)
	}
}

func TestRemoveAllMCPClients(t *testing.T) {
	isolate(t)

	for _, id := range []string{"claude-code", "vscode"} {
		if _, err := ConfigureMCPClient(id, false); err != nil {
			t.Fatalf("ConfigureMCPClient(%s): %v", id, err)
		}
	}

	results, errs := RemoveAllMCPClients()
	if len(errs) != 0 {
		t.Fatalf("errors: %v", errs)
	}

	removed := map[string]bool{}
	for _, r := range results {
		if r.Removed {
			removed[r.ClientID] = true
		}
	}
	for _, id := range []string{"claude-code", "vscode"} {
		if !removed[id] {
			t.Errorf("%s was not removed", id)
		}
	}
	// Cursor was never configured, so it must be reported as untouched rather
	// than as a failure.
	if removed["cursor"] {
		t.Error("cursor reported as removed although it was never configured")
	}
}
