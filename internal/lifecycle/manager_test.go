package lifecycle

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/policy"
	"github.com/simpleflo/conduit/internal/store"
)

func TestNew(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)

	if m == nil {
		t.Error("New returned nil")
	}
	if m.operations == nil {
		t.Error("operations map not initialized")
	}
}

func TestManager_CreateInstance(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	req := CreateInstanceRequest{
		PackageID:   "test/connector",
		Version:     "1.0.0",
		DisplayName: "Test Connector",
		ImageRef:    "ghcr.io/test/connector:1.0.0",
		Config: map[string]string{
			"key": "value",
		},
	}

	instance, err := m.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	if instance.InstanceID == "" {
		t.Error("expected non-empty InstanceID")
	}
	if instance.PackageID != req.PackageID {
		t.Errorf("PackageID mismatch: got %s, want %s", instance.PackageID, req.PackageID)
	}
	if instance.Status != StatusCreated {
		t.Errorf("Status mismatch: got %s, want %s", instance.Status, StatusCreated)
	}
}

func TestManager_GetInstance(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create an instance first
	req := CreateInstanceRequest{
		PackageID:   "test/connector",
		Version:     "1.0.0",
		DisplayName: "Test Connector",
		ImageRef:    "ghcr.io/test/connector:1.0.0",
	}

	created, err := m.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Get the instance
	got, err := m.GetInstance(ctx, created.InstanceID)
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if got.InstanceID != created.InstanceID {
		t.Errorf("InstanceID mismatch: got %s, want %s", got.InstanceID, created.InstanceID)
	}
}

func TestManager_GetInstanceNotFound(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	_, err := m.GetInstance(ctx, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent instance")
	}
}

func TestManager_ListInstances(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create multiple instances
	for i := 0; i < 3; i++ {
		req := CreateInstanceRequest{
			PackageID:   "test/connector",
			Version:     "1.0.0",
			DisplayName: "Test Connector",
			ImageRef:    "ghcr.io/test/connector:1.0.0",
		}
		if _, err := m.CreateInstance(ctx, req); err != nil {
			t.Fatalf("CreateInstance %d failed: %v", i, err)
		}
	}

	instances, err := m.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) != 3 {
		t.Errorf("expected 3 instances, got %d", len(instances))
	}
}

func TestManager_RemoveInstance(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create an instance
	req := CreateInstanceRequest{
		PackageID:   "test/connector",
		Version:     "1.0.0",
		DisplayName: "Test Connector",
		ImageRef:    "ghcr.io/test/connector:1.0.0",
	}

	instance, err := m.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Remove the instance
	if err := m.RemoveInstance(ctx, instance.InstanceID); err != nil {
		t.Fatalf("RemoveInstance failed: %v", err)
	}

	// Verify it's gone
	_, err = m.GetInstance(ctx, instance.InstanceID)
	if err == nil {
		t.Error("expected error getting removed instance")
	}
}

func TestManager_SetHealthInterval(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)

	// Default should be 30 seconds
	if m.healthInterval.Seconds() != 30 {
		t.Errorf("default health interval: got %v, want 30s", m.healthInterval)
	}

	// Change it
	m.SetHealthInterval(60 * 1e9) // 60 seconds in nanoseconds
	if m.healthInterval.Seconds() != 60 {
		t.Errorf("updated health interval: got %v, want 60s", m.healthInterval)
	}
}

func TestManager_InvalidTransitions(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create an instance (starts in CREATED)
	req := CreateInstanceRequest{
		PackageID:   "test/connector",
		Version:     "1.0.0",
		DisplayName: "Test Connector",
		ImageRef:    "ghcr.io/test/connector:1.0.0",
	}

	instance, err := m.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Cannot start from CREATED (must go through install first)
	err = m.StartInstance(ctx, instance.InstanceID)
	if err == nil {
		t.Error("expected error starting from CREATED status")
	}

	// Cannot stop from CREATED
	err = m.StopInstance(ctx, instance.InstanceID)
	if err == nil {
		t.Error("expected error stopping from CREATED status")
	}
}

func TestManager_CheckHealthNoContainer(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	m := New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create an instance
	req := CreateInstanceRequest{
		PackageID:   "test/connector",
		Version:     "1.0.0",
		DisplayName: "Test Connector",
		ImageRef:    "ghcr.io/test/connector:1.0.0",
	}

	instance, err := m.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Check health (no container running)
	health, err := m.CheckHealth(ctx, instance.InstanceID)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}

	if health.Status != "unknown" {
		t.Errorf("expected 'unknown' status, got %s", health.Status)
	}
}

func TestIsValidTransition(t *testing.T) {
	tests := []struct {
		from  InstanceStatus
		to    InstanceStatus
		valid bool
	}{
		{StatusCreated, StatusAuditing, true},
		{StatusCreated, StatusRunning, false},
		{StatusInstalled, StatusStarting, true},
		{StatusRunning, StatusStopping, true},
		{StatusStopped, StatusStarting, true},
		{StatusStopped, StatusDisabled, false}, // STOPPED cannot go directly to DISABLED
		{StatusRunning, StatusDisabled, true},  // RUNNING can go to DISABLED
		{StatusDisabled, StatusInstalled, true},
	}

	for _, tt := range tests {
		got := IsValidTransition(tt.from, tt.to)
		if got != tt.valid {
			t.Errorf("IsValidTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.valid)
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
			return nil
		}
		t.Fatalf("failed to create store: %v", err)
	}

	return st
}
