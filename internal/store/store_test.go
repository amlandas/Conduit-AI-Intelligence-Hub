package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simpleflo/conduit/pkg/models"
)

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

func TestStore_Health(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	if err := store.Health(ctx); err != nil {
		t.Errorf("health check failed: %v", err)
	}
}

func TestStore_CreateAndGetInstance(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	instance := &models.ConnectorInstance{
		InstanceID:     "inst_test123",
		PackageID:      "test/connector",
		PackageVersion: "1.0.0",
		DisplayName:    "Test Connector",
		ImageRef:       "ghcr.io/test/connector:1.0.0",
		Status:         models.StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Create
	if err := store.CreateInstance(ctx, instance); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Get
	got, err := store.GetInstance(ctx, instance.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.InstanceID != instance.InstanceID {
		t.Errorf("InstanceID mismatch: got %s, want %s", got.InstanceID, instance.InstanceID)
	}
	if got.PackageID != instance.PackageID {
		t.Errorf("PackageID mismatch: got %s, want %s", got.PackageID, instance.PackageID)
	}
	if got.Status != instance.Status {
		t.Errorf("Status mismatch: got %s, want %s", got.Status, instance.Status)
	}
}

func TestStore_ListInstances(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create multiple instances
	for i := 0; i < 3; i++ {
		instance := &models.ConnectorInstance{
			InstanceID:     "inst_" + string(rune('a'+i)),
			PackageID:      "test/connector",
			PackageVersion: "1.0.0",
			DisplayName:    "Test Connector",
			ImageRef:       "ghcr.io/test/connector:1.0.0",
			Status:         models.StatusCreated,
			CreatedAt:      time.Now(),
			UpdatedAt:      time.Now(),
		}
		if err := store.CreateInstance(ctx, instance); err != nil {
			t.Fatalf("CreateInstance %d failed: %v", i, err)
		}
	}

	instances, err := store.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 3 {
		t.Errorf("expected 3 instances, got %d", len(instances))
	}
}

func TestStore_UpdateInstanceStatus(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	instance := &models.ConnectorInstance{
		InstanceID:     "inst_status_test",
		PackageID:      "test/connector",
		PackageVersion: "1.0.0",
		DisplayName:    "Test Connector",
		ImageRef:       "ghcr.io/test/connector:1.0.0",
		Status:         models.StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := store.CreateInstance(ctx, instance); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Update status
	if err := store.UpdateInstanceStatus(ctx, instance.InstanceID, models.StatusRunning, ""); err != nil {
		t.Fatalf("UpdateInstanceStatus failed: %v", err)
	}

	got, err := store.GetInstance(ctx, instance.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.Status != models.StatusRunning {
		t.Errorf("Status not updated: got %s, want %s", got.Status, models.StatusRunning)
	}
}

func TestStore_DeleteInstance(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	instance := &models.ConnectorInstance{
		InstanceID:     "inst_delete_test",
		PackageID:      "test/connector",
		PackageVersion: "1.0.0",
		DisplayName:    "Test Connector",
		ImageRef:       "ghcr.io/test/connector:1.0.0",
		Status:         models.StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := store.CreateInstance(ctx, instance); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	if err := store.DeleteInstance(ctx, instance.InstanceID); err != nil {
		t.Fatalf("DeleteInstance failed: %v", err)
	}

	_, err := store.GetInstance(ctx, instance.InstanceID)
	if err == nil {
		t.Error("expected error getting deleted instance")
	}
}

func TestStore_CreateAndGetBinding(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create instance first (for foreign key)
	instance := &models.ConnectorInstance{
		InstanceID:     "inst_binding_test",
		PackageID:      "test/connector",
		PackageVersion: "1.0.0",
		DisplayName:    "Test Connector",
		ImageRef:       "ghcr.io/test/connector:1.0.0",
		Status:         models.StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := store.CreateInstance(ctx, instance); err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	binding := &models.ClientBinding{
		BindingID:   "bind_test123",
		InstanceID:  instance.InstanceID,
		ClientID:    "claude-code",
		Scope:       "project",
		ConfigPath:  "/path/to/config",
		ChangeSetID: "cs_test",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := store.CreateBinding(ctx, binding); err != nil {
		t.Fatalf("CreateBinding failed: %v", err)
	}

	got, err := store.GetBinding(ctx, binding.BindingID)
	if err != nil {
		t.Fatalf("GetBinding failed: %v", err)
	}

	if got.BindingID != binding.BindingID {
		t.Errorf("BindingID mismatch: got %s, want %s", got.BindingID, binding.BindingID)
	}
	if got.ClientID != binding.ClientID {
		t.Errorf("ClientID mismatch: got %s, want %s", got.ClientID, binding.ClientID)
	}
}

func TestStore_GetInstanceNotFound(t *testing.T) {
	store := testStore(t)
	defer store.Close()

	ctx := context.Background()
	_, err := store.GetInstance(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent instance")
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

// Cleanup helper
func cleanup(t *testing.T, path string) {
	t.Helper()
	os.RemoveAll(path)
}
