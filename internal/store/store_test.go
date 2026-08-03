package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// WP-3.2 removed the connector-instance and client-binding subsystem along
// with the daemon and container runtime that gave it meaning, so the tests
// that exercised CreateInstance/ListInstances/CreateBinding went with it. What
// the store still owes its callers is what is tested here: it opens a database
// at a path, applies migrations, and reports its own health.

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := New(dbPath)
	if err != nil {
		// Skip if FTS5 not available
		if strings.Contains(err.Error(), "fts5") {
			t.Skip("FTS5 not available, skipping test")
		}
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	if store.DB() == nil {
		t.Error("expected non-nil DB")
	}
}

func TestNew_CreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "created.db")

	store, err := New(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			t.Skip("FTS5 not available, skipping test")
		}
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// The knowledge base is created on open; there is no separate init step.
	var name string
	err = store.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='migrations'`).Scan(&name)
	if err != nil {
		t.Fatalf("migrations table missing after New: %v", err)
	}
}

func TestNew_WALMode(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	// WAL is the whole concurrency story now that there is no daemon
	// serialising access: two conduit processes coordinate here or nowhere.
	var mode string
	if err := store.DB().QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("read journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

func TestStore_Health(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	if err := store.Health(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

// testStore creates a temporary store for testing.
func testStore(t *testing.T) *Store {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := New(dbPath)
	if err != nil {
		// Skip if FTS5 not available
		if strings.Contains(err.Error(), "fts5") {
			t.Skip("FTS5 not available, skipping test")
		}
		t.Fatalf("failed to create test store: %v", err)
	}

	return store
}
