package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolate points HOME at a temporary directory so nothing in this package can
// touch the developer's real shell configuration. Everything here edits files
// in $HOME, so this is not optional.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

func TestCheckDocumentTools_ReportsNativeExtractors(t *testing.T) {
	tools := CheckDocumentTools()
	if len(tools) == 0 {
		t.Fatal("no document tools reported")
	}

	// DOCX and ODT are pure Go, so they are available on every machine and
	// must never be reported as something to install.
	byName := make(map[string]bool)
	for _, tool := range tools {
		byName[tool.Name] = tool.Available
	}
	for _, native := range []string{"docx (native)", "odt (native)"} {
		if !byName[native] {
			t.Errorf("%s should always be available", native)
		}
	}
}

func TestInstallDocumentTools_SkipsWhatIsPresent(t *testing.T) {
	isolate(t)

	// This must not fail on a machine with no package manager, and must not
	// report anything for tools that are already installed.
	results, err := InstallDocumentTools(context.Background(), false)
	if err != nil {
		t.Fatalf("InstallDocumentTools: %v", err)
	}
	for _, r := range results {
		if !r.Success && r.Message == "" {
			t.Errorf("failed result %q carries no explanation", r.Name)
		}
	}
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

func TestGetUninstallInfo_EmptyMachine(t *testing.T) {
	home := isolate(t)
	dataDir := filepath.Join(home, ".conduit")

	info, err := GetUninstallInfo(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("GetUninstallInfo: %v", err)
	}
	if info.HasBinaries {
		t.Error("reported binaries on a machine with none")
	}
	if info.HasDataDir {
		t.Error("reported a data directory that does not exist")
	}
}

func TestGetUninstallInfo_FindsDataDir(t *testing.T) {
	home := isolate(t)
	dataDir := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "conduit.db"), []byte("xxxxx"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "conduit.yaml"), []byte("x: 1\n"), 0600); err != nil {
		t.Fatal(err)
	}

	info, err := GetUninstallInfo(context.Background(), dataDir)
	if err != nil {
		t.Fatalf("GetUninstallInfo: %v", err)
	}
	if !info.HasDataDir || !info.HasSQLite || !info.HasConfig {
		t.Errorf("data directory not fully detected: %+v", info)
	}
	if info.DataDirSizeRaw == 0 {
		t.Error("data directory size was not measured")
	}
}

func TestUninstall_DryRunChangesNothing(t *testing.T) {
	home := isolate(t)
	dataDir := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	dbFile := filepath.Join(dataDir, "conduit.db")
	if err := os.WriteFile(dbFile, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	opts := NewUninstallOptionsAll()
	opts.DryRun = true

	result, err := Uninstall(context.Background(), dataDir, opts)
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if !result.Success {
		t.Errorf("dry run reported failure: %+v", result)
	}
	if _, err := os.Stat(dbFile); err != nil {
		t.Errorf("dry run deleted data: %v", err)
	}
	if len(result.ItemsRemoved) == 0 {
		t.Error("dry run should still report what it would remove")
	}
	for _, item := range result.ItemsRemoved {
		if !strings.Contains(item, "DRY RUN") {
			t.Errorf("dry run item not labelled: %q", item)
		}
	}
}

func TestUninstall_KeepDataPreservesTheKnowledgeBase(t *testing.T) {
	home := isolate(t)
	dataDir := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	dbFile := filepath.Join(dataDir, "conduit.db")
	if err := os.WriteFile(dbFile, []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall(context.Background(), dataDir, NewUninstallOptionsKeepData()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(dbFile); err != nil {
		t.Errorf("--keep-data removed the knowledge base: %v", err)
	}
}

func TestUninstall_AllRemovesTheDataDirectory(t *testing.T) {
	home := isolate(t)
	dataDir := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "conduit.db"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := Uninstall(context.Background(), dataDir, NewUninstallOptionsAll()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Errorf("--all left the data directory behind: %v", err)
	}
}

// TestStripShellConfig_RemovesOnlyConduitsLine is the important one: this code
// rewrites a file the user owns, and deleting the wrong line breaks their shell.
func TestStripShellConfig_RemovesOnlyConduitsLine(t *testing.T) {
	home := isolate(t)
	localBin := filepath.Join(home, ".local", "bin")

	rc := filepath.Join(home, ".zshrc")
	original := strings.Join([]string{
		"export EDITOR=vim",
		`export PATH="$HOME/my-own-tools:$PATH"`,
		"",
		"# Conduit",
		`export PATH="` + localBin + `:$PATH"`,
		"",
		"alias ll='ls -la'",
	}, "\n")
	if err := os.WriteFile(rc, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	result := &UninstallResult{}
	if err := stripShellConfig(rc, false, result); err != nil {
		t.Fatalf("stripShellConfig: %v", err)
	}

	data, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)

	if strings.Contains(got, "# Conduit") || strings.Contains(got, localBin) {
		t.Errorf("Conduit's PATH entry survived:\n%s", got)
	}
	for _, keep := range []string{"export EDITOR=vim", "my-own-tools", "alias ll='ls -la'"} {
		if !strings.Contains(got, keep) {
			t.Errorf("stripShellConfig deleted the user's own line %q:\n%s", keep, got)
		}
	}
	if len(result.ItemsRemoved) != 1 {
		t.Errorf("expected one reported removal, got %v", result.ItemsRemoved)
	}
}

func TestStripShellConfig_NoOpWhenAbsent(t *testing.T) {
	home := isolate(t)

	rc := filepath.Join(home, ".bashrc")
	original := "export EDITOR=vim\nalias ll='ls -la'\n"
	if err := os.WriteFile(rc, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}

	result := &UninstallResult{}
	if err := stripShellConfig(rc, false, result); err != nil {
		t.Fatalf("stripShellConfig: %v", err)
	}

	data, _ := os.ReadFile(rc)
	if string(data) != original {
		t.Errorf("an untouched rc file was rewritten:\ngot  %q\nwant %q", string(data), original)
	}
	if len(result.ItemsRemoved) != 0 {
		t.Errorf("reported a removal that did not happen: %v", result.ItemsRemoved)
	}
}

func TestStripShellConfig_MissingFileIsNotAnError(t *testing.T) {
	home := isolate(t)

	result := &UninstallResult{}
	if err := stripShellConfig(filepath.Join(home, ".no-such-rc"), false, result); err != nil {
		t.Errorf("a missing rc file should not be an error: %v", err)
	}
}
