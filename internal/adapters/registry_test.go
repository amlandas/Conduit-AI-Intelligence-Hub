package adapters

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/store"
)

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Error("NewRegistry returned nil")
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	// Create a test adapter
	st := testStore(t)
	defer st.Close()

	adapter := NewClaudeCodeAdapter(st.DB())
	r.Register(adapter)

	got, err := r.Get(adapter.ID())
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.ID() != adapter.ID() {
		t.Errorf("ID mismatch: got %s, want %s", got.ID(), adapter.ID())
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent adapter")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	st := testStore(t)
	defer st.Close()

	adapters := []Adapter{
		NewClaudeCodeAdapter(st.DB()),
		NewCursorAdapter(st.DB()),
		NewVSCodeAdapter(st.DB()),
	}

	for _, a := range adapters {
		r.Register(a)
	}

	list := r.List()
	if len(list) != 3 {
		t.Errorf("expected 3 adapters, got %d", len(list))
	}
}

func TestDefaultRegistry(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	r := DefaultRegistry(st.DB())

	// Should have all built-in adapters
	expectedIDs := []string{"claude-code", "cursor", "vscode", "gemini-cli"}

	for _, id := range expectedIDs {
		_, err := r.Get(id)
		if err != nil {
			t.Errorf("DefaultRegistry missing adapter: %s", id)
		}
	}
}

func TestRegistry_DetectAll(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	r := DefaultRegistry(st.DB())
	ctx := context.Background()

	results, err := r.DetectAll(ctx)
	if err != nil {
		t.Fatalf("DetectAll failed: %v", err)
	}

	// Should have results for all registered adapters
	if len(results) == 0 {
		t.Error("expected non-empty results from DetectAll")
	}

	// Each result should have basic info
	for id, result := range results {
		if id == "" {
			t.Error("empty adapter ID in results")
		}
		_ = result // Result is valid even if not installed
	}
}

func TestClaudeCodeAdapter_ID(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	a := NewClaudeCodeAdapter(st.DB())
	if a.ID() != "claude-code" {
		t.Errorf("expected ID 'claude-code', got %q", a.ID())
	}
}

func TestClaudeCodeAdapter_DisplayName(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	a := NewClaudeCodeAdapter(st.DB())
	if a.DisplayName() != "Claude Code" {
		t.Errorf("expected DisplayName 'Claude Code', got %q", a.DisplayName())
	}
}

func TestCursorAdapter_ID(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	a := NewCursorAdapter(st.DB())
	if a.ID() != "cursor" {
		t.Errorf("expected ID 'cursor', got %q", a.ID())
	}
}

func TestVSCodeAdapter_ID(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	a := NewVSCodeAdapter(st.DB())
	if a.ID() != "vscode" {
		t.Errorf("expected ID 'vscode', got %q", a.ID())
	}
}

func TestGeminiCLIAdapter_ID(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	a := NewGeminiCLIAdapter(st.DB())
	if a.ID() != "gemini-cli" {
		t.Errorf("expected ID 'gemini-cli', got %q", a.ID())
	}
}

func TestAdapter_PlanInjection(t *testing.T) {
	st := testStore(t)
	defer st.Close()

	tmpDir := t.TempDir()

	adapters := []Adapter{
		NewClaudeCodeAdapter(st.DB()),
		NewCursorAdapter(st.DB()),
		NewVSCodeAdapter(st.DB()),
		NewGeminiCLIAdapter(st.DB()),
	}

	ctx := context.Background()
	for _, a := range adapters {
		req := PlanRequest{
			InstanceID:  "inst_test",
			DisplayName: "Test Connector",
			Scope:       "project",
			ProjectPath: tmpDir,
		}

		plan, err := a.PlanInjection(ctx, req)
		if err != nil {
			t.Errorf("%s.PlanInjection failed: %v", a.ID(), err)
			continue
		}

		if plan.ChangeSetID == "" {
			t.Errorf("%s: expected non-empty ChangeSetID", a.ID())
		}
		if plan.ClientID != a.ID() {
			t.Errorf("%s: ClientID mismatch: got %s", a.ID(), plan.ClientID)
		}
		if len(plan.Operations) == 0 {
			t.Errorf("%s: expected at least one operation", a.ID())
		}
	}
}

// testStore creates a temporary store for testing.
func testStore(t *testing.T) *store.Store {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.New(dbPath)
	if err != nil {
		// Skip if FTS5 not available
		if strings.Contains(err.Error(), "fts5") {
			t.Skip("FTS5 not available, skipping test")
		}
		t.Fatalf("failed to create store: %v", err)
	}

	return st
}
