package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A3: the old rule deleted any `export PATH` line mentioning ~/.local/bin.
// pipx, uv, poetry and pip --user all write that exact line, so an uninstall
// removed other tools from the user's PATH -- and the failure surfaced much
// later, as "command not found" for something with no connection to Conduit.
func TestStripShellConfig_LeavesForeignPathLines(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".zshrc")

	// Both spellings a real profile uses. The expanded one is the case that
	// actually fired on a live machine: the old rule compared each line against
	// localBinDir(), which is the resolved path, so an unexpanded "$HOME/..."
	// line slipped past while a resolved one was deleted.
	expandedBin := filepath.Join(home, ".local", "bin")

	original := strings.Join([]string{
		`# created by pipx`,
		`export PATH="` + expandedBin + `:$PATH"`,
		``,
		`# added by uv`,
		`export PATH="$HOME/.local/bin:$PATH"`,
		``,
		`# Conduit`,
		`export PATH="` + expandedBin + `:$PATH"`,
		``,
		`alias c='conduit kb search'`,
		`export EDITOR=vim`,
		``,
	}, "\n")
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := &UninstallResult{}
	if err := stripShellConfig(path, false, result); err != nil {
		t.Fatalf("stripShellConfig: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	got := string(after)

	for _, keep := range []string{
		"# created by pipx",
		"# added by uv",
		`export PATH="$HOME/.local/bin:$PATH"`,
		"alias c='conduit kb search'",
		"export EDITOR=vim",
	} {
		if !strings.Contains(got, keep) {
			t.Errorf("removed a line Conduit never wrote: %q\n--- after ---\n%s", keep, got)
		}
	}
	if strings.Contains(got, "# Conduit") {
		t.Errorf("Conduit's marker survived:\n%s", got)
	}
	// pipx's line is byte-identical to the one Conduit wrote, so exactly one of
	// the resolved-path exports must remain.
	if n := strings.Count(got, `export PATH="`+expandedBin+`:$PATH"`); n != 1 {
		t.Errorf("resolved-path PATH export count = %d, want 1 (pipx's)\n%s", n, got)
	}
}

// Detection has to use the same rule as removal, or `--info` reports shell
// configuration on machines where only pipx ever wrote that line.
func TestHasConduitPathLine_RequiresTheMarker(t *testing.T) {
	if hasConduitPathLine("export PATH=\"$HOME/.local/bin:$PATH\"\n") {
		t.Error("a bare .local/bin PATH line was detected as Conduit's")
	}
	if !hasConduitPathLine("# Conduit\nexport PATH=\"$HOME/.local/bin:$PATH\"\n") {
		t.Error("the marked block was not detected")
	}
	// Prose mentioning the marker mid-line must not count, or an unrelated
	// comment costs the user the line beneath it.
	if hasConduitPathLine("echo 'see # Conduit docs'\n") {
		t.Error("a mid-line mention was treated as the marker")
	}
}

// A7: a profile is copied aside before it is rewritten. Atomicity guarantees
// the file is never truncated; it does not guarantee the edit was right, and a
// wrong edit to .zshrc breaks the user's next login shell.
func TestStripShellConfig_BacksUpBeforeRewriting(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".zshrc")

	original := "# Conduit\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport EDITOR=vim\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := &UninstallResult{}
	if err := stripShellConfig(path, false, result); err != nil {
		t.Fatalf("stripShellConfig: %v", err)
	}

	backup := path + backupSuffix
	data, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("no backup written: %v", err)
	}
	if string(data) != original {
		t.Errorf("backup does not match the original:\n--- want ---\n%s\n--- got ---\n%s", original, data)
	}
}

// Rewriting must not change who can read the profile.
func TestStripShellConfig_PreservesMode(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".zshrc")

	if err := os.WriteFile(path, []byte("# Conduit\nexport PATH=\"x\"\nexport EDITOR=vim\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	result := &UninstallResult{}
	if err := stripShellConfig(path, false, result); err != nil {
		t.Fatalf("stripShellConfig: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode = %o, want 644 (the temp file's 600 leaked through)", got)
	}
}

// A dry run must not write anything at all -- not the profile, not a backup.
func TestStripShellConfig_DryRunWritesNothing(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".zshrc")

	original := "# Conduit\nexport PATH=\"x\"\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	result := &UninstallResult{}
	if err := stripShellConfig(path, true, result); err != nil {
		t.Fatalf("stripShellConfig: %v", err)
	}

	if data, _ := os.ReadFile(path); string(data) != original {
		t.Error("dry run modified the profile")
	}
	if _, err := os.Stat(path + backupSuffix); err == nil {
		t.Error("dry run wrote a backup")
	}
	if len(result.ItemsRemoved) == 0 {
		t.Error("dry run reported nothing")
	}
}

// ---------------------------------------------------------------------------
// A7 -- atomic replacement
// ---------------------------------------------------------------------------

func TestWriteFileAtomic_ReplacesAndPreservesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := writeFileAtomic(path, []byte("new contents"), 0o600); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "new contents" {
		t.Errorf("contents = %q", data)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600", got)
	}

	// No temp files may be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only config.json", names)
	}
}

// ~/.claude.json holds every MCP server the user has plus Claude Code's own
// state. Removing Conduit's entry must not put any of that at risk, and the
// file must never be observable in a truncated state.
func TestRemoveMCPClient_PreservesModeAndOtherServers(t *testing.T) {
	home := isolate(t)
	path := filepath.Join(home, ".claude.json")

	original := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			ServerName: map[string]interface{}{"command": "conduit"},
			"github":   map[string]interface{}{"command": "gh-mcp"},
			"postgres": map[string]interface{}{"command": "pg-mcp"},
		},
		"theme":        "dark",
		"numStartups":  42,
		"projectState": map[string]interface{}{"a": "b"},
	}
	raw, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := RemoveMCPClient("claude-code"); err != nil {
		t.Fatalf("RemoveMCPClient: %v", err)
	}

	var after map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := json.Unmarshal(data, &after); err != nil {
		t.Fatalf("the config is no longer valid JSON: %v\n%s", err, data)
	}

	servers, ok := after["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcpServers vanished: %v", after)
	}
	if _, gone := servers[ServerName]; gone {
		t.Error("Conduit's entry survived")
	}
	for _, keep := range []string{"github", "postgres"} {
		if _, ok := servers[keep]; !ok {
			t.Errorf("lost an unrelated MCP server: %s", keep)
		}
	}
	for _, key := range []string{"theme", "numStartups", "projectState"} {
		if _, ok := after[key]; !ok {
			t.Errorf("lost unrelated state: %s", key)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode = %o, want 600 preserved", got)
	}

	// The atomic write must not have left its temp file in the home directory.
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("left a temp file behind: %s", e.Name())
		}
	}
}
