package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager_DefaultOllama(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if manager.ProviderName() != "ollama" {
		t.Errorf("expected provider ollama, got %s", manager.ProviderName())
	}
}

func TestNewManager_Anthropic(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"

	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if manager.ProviderName() != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", manager.ProviderName())
	}
}

func TestNewManager_InvalidProvider(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	config.Provider = "invalid-provider"

	_, err = NewManager(config, tempDir)
	if err == nil {
		t.Error("expected error for invalid provider")
	}
}

func TestManager_SetProvider(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initially ollama
	if manager.ProviderName() != "ollama" {
		t.Errorf("expected initial provider ollama, got %s", manager.ProviderName())
	}

	// Switch to anthropic
	err = manager.SetProvider("anthropic")
	if err != nil {
		t.Fatalf("unexpected error switching provider: %v", err)
	}

	if manager.ProviderName() != "anthropic" {
		t.Errorf("expected provider anthropic after switch, got %s", manager.ProviderName())
	}

	// Switch back to ollama
	err = manager.SetProvider("ollama")
	if err != nil {
		t.Fatalf("unexpected error switching provider: %v", err)
	}

	if manager.ProviderName() != "ollama" {
		t.Errorf("expected provider ollama after switch, got %s", manager.ProviderName())
	}
}

func TestManager_SetProvider_Invalid(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	err = manager.SetProvider("invalid")
	if err == nil {
		t.Error("expected error for invalid provider")
	}
}

func TestManager_WriteDockerfile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a mock fetch result
	repoDir := filepath.Join(tempDir, "test-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	fetchResult := &FetchResult{
		LocalPath: repoDir,
		RepoURL:   "https://github.com/test/repo.git",
		RepoName:  "repo",
		Owner:     "test",
	}

	dockerfile := "FROM node:20\nWORKDIR /app\nCOPY . .\nRUN npm install\nENTRYPOINT [\"node\", \"index.js\"]"

	dockerfilePath, err := manager.WriteDockerfile(fetchResult, dockerfile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify file was written
	expectedPath := filepath.Join(repoDir, "Dockerfile.conduit")
	if dockerfilePath != expectedPath {
		t.Errorf("expected path %s, got %s", expectedPath, dockerfilePath)
	}

	// Verify content
	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		t.Fatalf("failed to read dockerfile: %v", err)
	}

	if string(content) != dockerfile {
		t.Errorf("dockerfile content mismatch")
	}
}

func TestManager_Cleanup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Create a mock repo directory
	repoDir := filepath.Join(tempDir, "test-repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}

	// Create a file in it
	testFile := filepath.Join(repoDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	fetchResult := &FetchResult{
		LocalPath: repoDir,
	}

	// Cleanup
	err = manager.Cleanup(fetchResult)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify directory was removed
	if _, err := os.Stat(repoDir); !os.IsNotExist(err) {
		t.Error("expected repo directory to be removed")
	}
}

func TestManager_Cleanup_NilResult(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "conduit-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := DefaultProviderConfig()
	manager, err := NewManager(config, tempDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Cleanup with nil should not error
	err = manager.Cleanup(nil)
	if err != nil {
		t.Errorf("unexpected error for nil cleanup: %v", err)
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s      string
		substr string
		want   bool
	}{
		{"hello world", "world", true},
		{"hello world", "foo", false},
		{"", "foo", false},
		{"foo", "", true},
		{"foo", "foo", true},
		{"foo", "foobar", false},
	}

	for _, tt := range tests {
		got := contains(tt.s, tt.substr)
		if got != tt.want {
			t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, got, tt.want)
		}
	}
}
