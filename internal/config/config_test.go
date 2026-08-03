package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if cfg.DataDir == "" {
		t.Error("DataDir should not be empty")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel should be 'info', got %s", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat should be 'json', got %s", cfg.LogFormat)
	}
}

func TestDefaultConfig_KBDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.KB.Workers != 4 {
		t.Errorf("Workers should be 4, got %d", cfg.KB.Workers)
	}
	if cfg.KB.MaxFileSize != 100*1024*1024 {
		t.Errorf("MaxFileSize should be 100MB, got %d", cfg.KB.MaxFileSize)
	}
	if cfg.KB.ChunkSize != 1000 {
		t.Errorf("ChunkSize should be 1000, got %d", cfg.KB.ChunkSize)
	}
	if cfg.KB.ChunkOverlap != 100 {
		t.Errorf("ChunkOverlap should be 100, got %d", cfg.KB.ChunkOverlap)
	}
}

func TestDefaultConfig_EmbedDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Embed.Provider != EmbedProviderLlamaServer {
		t.Errorf("Embed.Provider should default to %q, got %q",
			EmbedProviderLlamaServer, cfg.Embed.Provider)
	}
	// An empty model means "whatever the pinned registry says", which is the
	// only value that cannot disagree with the vector dimensions.
	if cfg.Embed.Model != "" {
		t.Errorf("Embed.Model should default to empty (registry default), got %q", cfg.Embed.Model)
	}
	if cfg.Embed.Dimensions != 0 {
		t.Errorf("Embed.Dimensions should default to 0 (derive from model), got %d", cfg.Embed.Dimensions)
	}
	if cfg.Embed.TimeoutSeconds <= 0 {
		t.Error("Embed.TimeoutSeconds should have a positive default")
	}
	if cfg.Embed.LlamaServer.Port != 0 {
		t.Errorf("llama-server port should default to 0 (pick free port), got %d", cfg.Embed.LlamaServer.Port)
	}
}

func TestDefaultConfig_KAGOffByDefault(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.KB.KAG.Enabled {
		t.Error("the knowledge graph must be opt-in: no graph tables until enabled")
	}
	if cfg.KB.KAG.Graph.Backend != "sqlite" {
		t.Errorf("graph backend should be sqlite, got %q", cfg.KB.KAG.Graph.Backend)
	}
}

func TestDefaultConfig_PolicyDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if len(cfg.Policy.ForbiddenPaths) == 0 {
		t.Error("ForbiddenPaths should not be empty")
	}

	forbidden := make(map[string]bool)
	for _, p := range cfg.Policy.ForbiddenPaths {
		forbidden[p] = true
	}

	for _, p := range []string{"/", "/etc", "~/.ssh", "~/.aws"} {
		if !forbidden[p] {
			t.Errorf("Expected %s to be in ForbiddenPaths", p)
		}
	}
}

func TestConfig_DatabasePath(t *testing.T) {
	cfg := DefaultConfig()

	dbPath := cfg.DatabasePath()
	if !strings.HasSuffix(dbPath, "conduit.db") {
		t.Errorf("DatabasePath should end with 'conduit.db', got %s", dbPath)
	}
	if !strings.Contains(dbPath, cfg.DataDir) {
		t.Errorf("DatabasePath should be within DataDir")
	}
}

func TestConfig_DatabasePath_DBPathOverrides(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DBPath = "/tmp/workspace/project.db"

	// This is the workspace-isolation contract: db_path wins outright, and is
	// not reinterpreted relative to the data directory.
	if got := cfg.DatabasePath(); got != "/tmp/workspace/project.db" {
		t.Errorf("DatabasePath() = %q, want the explicit db_path", got)
	}
}

func TestConfig_BackupsDir(t *testing.T) {
	cfg := DefaultConfig()

	backupsDir := cfg.BackupsDir()
	if !strings.HasSuffix(backupsDir, "backups") {
		t.Errorf("BackupsDir should end with 'backups', got %s", backupsDir)
	}
	if !strings.Contains(backupsDir, cfg.DataDir) {
		t.Errorf("BackupsDir should be within DataDir")
	}
}

func TestConfig_LogPath(t *testing.T) {
	cfg := DefaultConfig()

	logPath := cfg.LogPath()
	if !strings.HasSuffix(logPath, "conduit.log") {
		t.Errorf("LogPath should end with 'conduit.log', got %s", logPath)
	}
	if !strings.Contains(logPath, cfg.DataDir) {
		t.Errorf("LogPath should be within DataDir")
	}
}

func TestConfig_EnsureDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{DataDir: tmpDir}
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	expectedDirs := []string{
		tmpDir,
		cfg.BackupsDir(),
		cfg.ModelsDir(),
		filepath.Join(tmpDir, "kb"),
	}

	for _, dir := range expectedDirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("Directory %s not created: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s is not a directory", dir)
		}
	}
}

func TestConfig_EnsureDirectories_CreatesDBParent(t *testing.T) {
	tmpDir := t.TempDir()

	// --db may point outside the data directory; its parent must still exist
	// or every command that opens the knowledge base fails.
	cfg := &Config{
		DataDir: tmpDir,
		DBPath:  filepath.Join(tmpDir, "elsewhere", "nested", "kb.db"),
	}
	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	if info, err := os.Stat(filepath.Join(tmpDir, "elsewhere", "nested")); err != nil || !info.IsDir() {
		t.Errorf("parent of db_path was not created: %v", err)
	}
}

func TestConfig_EnsureDirectories_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Permission test not applicable on Windows")
	}

	tmpDir := t.TempDir()
	cfg := &Config{DataDir: tmpDir}

	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	info, err := os.Stat(cfg.BackupsDir())
	if err != nil {
		t.Fatalf("Failed to stat BackupsDir: %v", err)
	}

	perm := info.Mode().Perm()
	if perm&0077 != 0 {
		t.Errorf("Backup directory should not be world-readable, got %o", perm)
	}
}

func TestLoad_DefaultsWhenNoConfig(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
	if cfg.LogLevel == "" {
		t.Error("LogLevel should have default value")
	}
}

func TestExpandPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory")
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"~/.conduit", filepath.Join(homeDir, ".conduit")},
		{"~/", homeDir},
		{"~", homeDir},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
	}

	for _, tt := range tests {
		result := expandPath(tt.input)
		if result != tt.expected {
			t.Errorf("expandPath(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Schema and precedence
// ---------------------------------------------------------------------------

func TestKnownKeys_CoversNestedSchema(t *testing.T) {
	keys := KnownKeys()

	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}

	// A representative slice: top level, one level down, two levels down.
	for _, want := range []string{
		"data_dir", "db_path", "log_level",
		"kb", "kb.chunk_size", "kb.rag", "kb.rag.min_score",
		"kb.kag.enabled", "kb.kag.graph.max_hops",
		"embed", "embed.provider", "embed.llama_server.port",
		"mcp.kb.search.default_mode",
		"telemetry.local_query_log",
	} {
		if !set[want] {
			t.Errorf("KnownKeys() is missing %q", want)
		}
	}
}

func TestKnownKeys_ExcludesRemovedSubsystems(t *testing.T) {
	keys := KnownKeys()

	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}

	// These belonged to the daemon, the container runtime and the external
	// vector/graph servers. If one reappears, a dead subsystem came back.
	for _, gone := range []string{
		"socket", "api", "api.read_timeout",
		"runtime", "runtime.preferred",
		"ai", "ai.provider", "ai.model",
		"kb.kag.graph.host", "kb.kag.graph.port",
	} {
		if set[gone] {
			t.Errorf("KnownKeys() still contains removed key %q", gone)
		}
	}
}

func TestUnknownKeys(t *testing.T) {
	settings := map[string]interface{}{
		"data_dir": "/x",
		"socket":   "/x/conduit.sock", // removed with the daemon
		"runtime": map[string]interface{}{ // removed with containers
			"preferred": "podman",
		},
		"kb": map[string]interface{}{
			"chunk_size": 1000,
			"qdrant_url": "http://localhost:6333", // removed with Qdrant
		},
	}

	got := unknownKeys(settings)

	want := map[string]bool{"socket": true, "runtime": true, "kb.qdrant_url": true}
	if len(got) != len(want) {
		t.Fatalf("unknownKeys() = %v, want exactly %v", got, want)
	}
	for _, k := range got {
		if !want[k] {
			t.Errorf("unexpected unknown key %q", k)
		}
	}
}

func TestUnknownKeys_ReportsOutermostOnly(t *testing.T) {
	// A whole removed section should produce one warning, not one per leaf.
	settings := map[string]interface{}{
		"runtime": map[string]interface{}{
			"preferred":     "podman",
			"pull_timeout":  "10m",
			"start_timeout": "30s",
		},
	}

	got := unknownKeys(settings)
	if len(got) != 1 || got[0] != "runtime" {
		t.Errorf("unknownKeys() = %v, want [runtime]", got)
	}
}

func TestFormatUnknownKeys_EmptyWhenNothingUnknown(t *testing.T) {
	if msg := FormatUnknownKeys("/x/conduit.yaml", nil); msg != "" {
		t.Errorf("expected no warning, got %q", msg)
	}
}

func TestNormalize_UnknownProviderFallsBackToLexical(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Embed.Provider = "gpt-embeddings-9000"
	cfg.normalize()

	// Guessing a provider would index with the wrong model and silently
	// poison the vector index; lexical-only is always correct.
	if cfg.Embed.Provider != EmbedProviderNone {
		t.Errorf("unknown provider should fall back to %q, got %q",
			EmbedProviderNone, cfg.Embed.Provider)
	}
}

func TestNormalize_EmptyProviderTakesDefault(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Embed.Provider = ""
	cfg.normalize()

	if cfg.Embed.Provider != EmbedProviderLlamaServer {
		t.Errorf("empty provider should become %q, got %q",
			EmbedProviderLlamaServer, cfg.Embed.Provider)
	}
}

func TestLoadWithFlags_FlagBeatsFileAndDefault(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "data_dir: /from/file\ndb_path: /from/file/kb.db\n")

	isolate(t, dir)

	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("db", "", "")
	if err := flags.Parse([]string{"--db", "/from/flag/kb.db"}); err != nil {
		t.Fatalf("parse flags: %v", err)
	}

	res, err := LoadWithFlags(flags)
	if err != nil {
		t.Fatalf("LoadWithFlags: %v", err)
	}

	if res.Config.DatabasePath() != "/from/flag/kb.db" {
		t.Errorf("flag should outrank the file: got %q", res.Config.DatabasePath())
	}
}

func TestLoadWithFlags_UnsetFlagDoesNotClobberFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "db_path: /from/file/kb.db\n")

	isolate(t, dir)

	// A registered but unparsed flag carries its own zero default. If binding
	// ignored "was it actually set?", that default would erase the file value.
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.String("db", "", "")

	res, err := LoadWithFlags(flags)
	if err != nil {
		t.Fatalf("LoadWithFlags: %v", err)
	}

	if res.Config.DatabasePath() != "/from/file/kb.db" {
		t.Errorf("unset flag clobbered the file value: got %q", res.Config.DatabasePath())
	}
}

func TestLoadWithFlags_ReportsUnknownKeysFromFile(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "data_dir: /x\nsocket: /x/conduit.sock\n")

	isolate(t, dir)

	res, err := LoadWithFlags(nil)
	if err != nil {
		t.Fatalf("LoadWithFlags: %v", err)
	}

	if len(res.UnknownKeys) != 1 || res.UnknownKeys[0] != "socket" {
		t.Errorf("UnknownKeys = %v, want [socket]", res.UnknownKeys)
	}
	if res.File == "" {
		t.Error("File should name the config that was read")
	}
	// A stale key must not stop Conduit from starting.
	if res.Config.DataDir != "/x" {
		t.Errorf("config should still load; DataDir = %q", res.Config.DataDir)
	}
}

func TestLoadWithFlags_ParsesDurations(t *testing.T) {
	dir := t.TempDir()
	writeConfig(t, dir, "embed:\n  llama_server:\n    idle_timeout: 2m\n")

	isolate(t, dir)

	res, err := LoadWithFlags(nil)
	if err != nil {
		t.Fatalf("LoadWithFlags: %v", err)
	}

	if got := res.Config.Embed.LlamaServer.IdleTimeout; got != 2*time.Minute {
		t.Errorf("idle_timeout = %v, want 2m", got)
	}
}

// writeConfig drops a conduit.yaml into dir.
func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "conduit.yaml"), []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// isolate points the loader at dir and at an empty HOME.
//
// Without the HOME override these tests read the developer's real
// ~/.conduit/conduit.yaml and fail (or, worse, pass for the wrong reason) on
// one machine and not another.
func isolate(t *testing.T, dir string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows
	t.Chdir(dir)
}
