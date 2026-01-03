package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	// Check data directory is set
	if cfg.DataDir == "" {
		t.Error("DataDir should not be empty")
	}

	// Check socket path
	if cfg.SocketPath == "" {
		t.Error("SocketPath should not be empty")
	}

	// Check log defaults
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel should be 'info', got %s", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat should be 'json', got %s", cfg.LogFormat)
	}
}

func TestDefaultConfig_WindowsSocketPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Only run on Windows")
	}

	cfg := DefaultConfig()
	if !strings.HasPrefix(cfg.SocketPath, `\\.\pipe\`) {
		t.Errorf("Windows socket path should use named pipes, got %s", cfg.SocketPath)
	}
}

func TestDefaultConfig_UnixSocketPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skip on Windows")
	}

	cfg := DefaultConfig()
	if !strings.HasSuffix(cfg.SocketPath, ".sock") {
		t.Errorf("Unix socket path should end with .sock, got %s", cfg.SocketPath)
	}
}

func TestDefaultConfig_APIDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.API.ReadTimeout != 30*time.Second {
		t.Errorf("ReadTimeout should be 30s, got %v", cfg.API.ReadTimeout)
	}
	if cfg.API.WriteTimeout != 10*time.Minute {
		t.Errorf("WriteTimeout should be 10m, got %v", cfg.API.WriteTimeout)
	}
	if cfg.API.IdleTimeout != 120*time.Second {
		t.Errorf("IdleTimeout should be 120s, got %v", cfg.API.IdleTimeout)
	}
}

func TestDefaultConfig_RuntimeDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Runtime.Preferred != "auto" {
		t.Errorf("Preferred runtime should be 'auto', got %s", cfg.Runtime.Preferred)
	}
	if cfg.Runtime.PullTimeout != 10*time.Minute {
		t.Errorf("PullTimeout should be 10m, got %v", cfg.Runtime.PullTimeout)
	}
	if cfg.Runtime.HealthInterval != 30*time.Second {
		t.Errorf("HealthInterval should be 30s, got %v", cfg.Runtime.HealthInterval)
	}
}

func TestDefaultConfig_KBDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.KB.Workers != 4 {
		t.Errorf("Workers should be 4, got %d", cfg.KB.Workers)
	}
	if cfg.KB.MaxFileSize != 10*1024*1024 {
		t.Errorf("MaxFileSize should be 10MB, got %d", cfg.KB.MaxFileSize)
	}
	if cfg.KB.ChunkSize != 1000 {
		t.Errorf("ChunkSize should be 1000, got %d", cfg.KB.ChunkSize)
	}
	if cfg.KB.ChunkOverlap != 100 {
		t.Errorf("ChunkOverlap should be 100, got %d", cfg.KB.ChunkOverlap)
	}
}

func TestDefaultConfig_PolicyDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Policy.AllowNetworkEgress {
		t.Error("AllowNetworkEgress should be false by default")
	}
	if len(cfg.Policy.ForbiddenPaths) == 0 {
		t.Error("ForbiddenPaths should not be empty")
	}

	// Check that sensitive paths are forbidden
	forbidden := make(map[string]bool)
	for _, p := range cfg.Policy.ForbiddenPaths {
		forbidden[p] = true
	}

	expectedForbidden := []string{"/", "/etc", "~/.ssh", "~/.aws"}
	for _, p := range expectedForbidden {
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
	// Create temp directory for test
	tmpDir := t.TempDir()

	cfg := &Config{
		DataDir: tmpDir,
	}

	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	// Check directories were created
	expectedDirs := []string{
		tmpDir,
		cfg.BackupsDir(),
		filepath.Join(tmpDir, "connectors"),
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

func TestConfig_EnsureDirectories_Permissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Permission test not applicable on Windows")
	}

	tmpDir := t.TempDir()

	cfg := &Config{
		DataDir: tmpDir,
	}

	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	// Check permissions are restrictive (0700)
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

	// Should have defaults applied
	if cfg.LogLevel == "" {
		t.Error("LogLevel should have default value")
	}
}

func TestDefaultConfig_AIDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.AI.Provider != "ollama" {
		t.Errorf("AI.Provider should be 'ollama', got %s", cfg.AI.Provider)
	}
	if cfg.AI.Model != "qwen2.5-coder:7b" {
		t.Errorf("AI.Model should be 'qwen2.5-coder:7b', got %s", cfg.AI.Model)
	}
	if cfg.AI.Endpoint != "http://localhost:11434" {
		t.Errorf("AI.Endpoint should be 'http://localhost:11434', got %s", cfg.AI.Endpoint)
	}
	if cfg.AI.TimeoutSeconds != 120 {
		t.Errorf("AI.TimeoutSeconds should be 120, got %d", cfg.AI.TimeoutSeconds)
	}
	if cfg.AI.MaxRetries != 2 {
		t.Errorf("AI.MaxRetries should be 2, got %d", cfg.AI.MaxRetries)
	}
	if cfg.AI.ConfidenceThreshold != 0.6 {
		t.Errorf("AI.ConfidenceThreshold should be 0.6, got %f", cfg.AI.ConfidenceThreshold)
	}
}

func TestConfig_AICacheDir(t *testing.T) {
	cfg := DefaultConfig()

	cacheDir := cfg.AICacheDir()
	if !strings.HasSuffix(cacheDir, "ai-cache") {
		t.Errorf("AICacheDir should end with 'ai-cache', got %s", cacheDir)
	}
	if !strings.Contains(cacheDir, cfg.DataDir) {
		t.Errorf("AICacheDir should be within DataDir")
	}
}

func TestConfig_ConnectorsDir(t *testing.T) {
	cfg := DefaultConfig()

	connectorsDir := cfg.ConnectorsDir()
	if !strings.HasSuffix(connectorsDir, "connectors") {
		t.Errorf("ConnectorsDir should end with 'connectors', got %s", connectorsDir)
	}
	if !strings.Contains(connectorsDir, cfg.DataDir) {
		t.Errorf("ConnectorsDir should be within DataDir")
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

func TestConfig_EnsureDirectories_IncludesAICache(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &Config{
		DataDir: tmpDir,
	}

	if err := cfg.EnsureDirectories(); err != nil {
		t.Fatalf("EnsureDirectories failed: %v", err)
	}

	// Check AI cache directory was created
	aiCacheDir := cfg.AICacheDir()
	info, err := os.Stat(aiCacheDir)
	if err != nil {
		t.Errorf("AI cache directory %s not created: %v", aiCacheDir, err)
		return
	}
	if !info.IsDir() {
		t.Errorf("%s is not a directory", aiCacheDir)
	}
}
