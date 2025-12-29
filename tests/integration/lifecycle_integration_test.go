// Package integration contains integration tests for Conduit components.
package integration

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/lifecycle"
	"github.com/simpleflo/conduit/internal/policy"
	"github.com/simpleflo/conduit/internal/store"
)

// TestLifecycleWithPolicyIntegration tests the lifecycle manager with policy engine.
func TestLifecycleWithPolicyIntegration(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping integration test")
	}
	defer st.Close()

	// Create policy engine
	pol := policy.New(st.DB())

	// Create lifecycle manager
	mgr := lifecycle.New(st.DB(), nil, pol)

	ctx := context.Background()

	// Test 1: Create an instance
	t.Run("CreateInstance", func(t *testing.T) {
		req := lifecycle.CreateInstanceRequest{
			PackageID:   "test/integration-connector",
			Version:     "1.0.0",
			DisplayName: "Integration Test Connector",
			ImageRef:    "ghcr.io/test/connector:1.0.0",
		}

		instance, err := mgr.CreateInstance(ctx, req)
		if err != nil {
			t.Fatalf("CreateInstance failed: %v", err)
		}

		if instance.Status != lifecycle.StatusCreated {
			t.Errorf("Expected status CREATED, got %s", instance.Status)
		}

		// Verify we can retrieve it
		retrieved, err := mgr.GetInstance(ctx, instance.InstanceID)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}

		if retrieved.InstanceID != instance.InstanceID {
			t.Error("Instance ID mismatch after retrieval")
		}
	})

	// Test 2: List instances includes our new instance
	t.Run("ListInstances", func(t *testing.T) {
		instances, err := mgr.ListInstances(ctx)
		if err != nil {
			t.Fatalf("ListInstances failed: %v", err)
		}

		if len(instances) < 1 {
			t.Error("Expected at least one instance")
		}
	})

	// Test 3: Install triggers policy evaluation
	t.Run("InstallTriggersPolicyEvaluation", func(t *testing.T) {
		req := lifecycle.CreateInstanceRequest{
			PackageID:   "test/install-test-connector",
			Version:     "1.0.0",
			DisplayName: "Install Test Connector",
			ImageRef:    "ghcr.io/test/connector:1.0.0",
		}

		instance, err := mgr.CreateInstance(ctx, req)
		if err != nil {
			t.Fatalf("CreateInstance failed: %v", err)
		}

		// Start install (runs asynchronously)
		op, err := mgr.InstallInstance(ctx, instance.InstanceID)
		if err != nil {
			t.Fatalf("InstallInstance failed: %v", err)
		}

		if op.OperationID == "" {
			t.Error("Expected non-empty operation ID")
		}

		// Wait for operation to complete (or timeout)
		mgr.WaitForOperations()

		// Check operation result
		finalOp, err := mgr.GetOperation(ctx, op.OperationID)
		if err != nil {
			t.Fatalf("GetOperation failed: %v", err)
		}

		// Operation should be completed or failed (not pending)
		if finalOp.Status == "pending" {
			t.Error("Operation should not be pending after wait")
		}
	})
}

// TestStoreAndLifecycleIntegration tests store and lifecycle manager together.
func TestStoreAndLifecycleIntegration(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping integration test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	mgr := lifecycle.New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create multiple instances
	var instanceIDs []string
	for i := 0; i < 5; i++ {
		req := lifecycle.CreateInstanceRequest{
			PackageID:   "test/bulk-connector",
			Version:     "1.0.0",
			DisplayName: "Bulk Test Connector",
			ImageRef:    "ghcr.io/test/connector:1.0.0",
		}

		instance, err := mgr.CreateInstance(ctx, req)
		if err != nil {
			t.Fatalf("CreateInstance %d failed: %v", i, err)
		}
		instanceIDs = append(instanceIDs, instance.InstanceID)
	}

	// List should return all instances
	instances, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) < 5 {
		t.Errorf("Expected at least 5 instances, got %d", len(instances))
	}

	// Remove instances
	for _, id := range instanceIDs {
		if err := mgr.RemoveInstance(ctx, id); err != nil {
			t.Errorf("RemoveInstance failed for %s: %v", id, err)
		}
	}

	// Verify removal
	for _, id := range instanceIDs {
		_, err := mgr.GetInstance(ctx, id)
		if err == nil {
			t.Errorf("Instance %s should have been removed", id)
		}
	}
}

// TestPolicyEvaluationIntegration tests policy evaluation in context.
func TestPolicyEvaluationIntegration(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping integration test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	ctx := context.Background()

	// Test 1: Safe path should be allowed
	t.Run("SafePathAllowed", func(t *testing.T) {
		tmpDir := t.TempDir()

		req := policy.Request{
			Scope:      policy.ScopeInstall,
			InstanceID: "inst_test_safe",
			PackageID:  "test/connector",
			Actor:      "user",
			Requested: policy.PermissionSet{
				Filesystem: policy.FilesystemPerms{
					ReadonlyPaths: []string{tmpDir},
				},
			},
		}

		decision, err := pol.Evaluate(ctx, req)
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}

		// Safe paths should NOT be denied. They may return ALLOW or WARN
		// (WARN means allowed but user hasn't explicitly granted the path)
		if decision.Decision == policy.Deny {
			t.Errorf("Expected safe path to not be denied, got DENY. Reasons: %v", decision.BlockReasons)
		}
	})

	// Test 2: Sensitive path should be denied
	t.Run("SensitivePathDenied", func(t *testing.T) {
		req := policy.Request{
			Scope:      policy.ScopeInstall,
			InstanceID: "inst_test_sensitive",
			PackageID:  "test/connector",
			Actor:      "user",
			Requested: policy.PermissionSet{
				Filesystem: policy.FilesystemPerms{
					ReadonlyPaths: []string{"/etc"},
				},
			},
		}

		decision, err := pol.Evaluate(ctx, req)
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}

		if decision.Decision != policy.Deny {
			t.Errorf("Expected DENY for /etc, got %s", decision.Decision)
		}
	})
}

// TestConcurrentLifecycleOperations tests concurrent instance operations.
func TestConcurrentLifecycleOperations(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping integration test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	mgr := lifecycle.New(st.DB(), nil, pol)
	ctx := context.Background()

	// Create instances concurrently
	const numInstances = 10
	results := make(chan error, numInstances)

	for i := 0; i < numInstances; i++ {
		go func(idx int) {
			req := lifecycle.CreateInstanceRequest{
				PackageID:   "test/concurrent-connector",
				Version:     "1.0.0",
				DisplayName: "Concurrent Test",
				ImageRef:    "ghcr.io/test/connector:1.0.0",
			}

			_, err := mgr.CreateInstance(ctx, req)
			results <- err
		}(i)
	}

	// Collect results
	successCount := 0
	for i := 0; i < numInstances; i++ {
		if err := <-results; err == nil {
			successCount++
		}
	}

	if successCount != numInstances {
		t.Errorf("Expected %d successful creates, got %d", numInstances, successCount)
	}

	// Verify all instances exist
	instances, err := mgr.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(instances) < numInstances {
		t.Errorf("Expected at least %d instances, got %d", numInstances, len(instances))
	}
}

// TestHealthCheckIntegration tests health checking for instances.
func TestHealthCheckIntegration(t *testing.T) {
	st := testStore(t)
	if st == nil {
		t.Skip("FTS5 not available, skipping integration test")
	}
	defer st.Close()

	pol := policy.New(st.DB())
	mgr := lifecycle.New(st.DB(), nil, pol)
	mgr.SetHealthInterval(100 * time.Millisecond)

	ctx := context.Background()

	// Create an instance
	req := lifecycle.CreateInstanceRequest{
		PackageID:   "test/health-connector",
		Version:     "1.0.0",
		DisplayName: "Health Test Connector",
		ImageRef:    "ghcr.io/test/connector:1.0.0",
	}

	instance, err := mgr.CreateInstance(ctx, req)
	if err != nil {
		t.Fatalf("CreateInstance failed: %v", err)
	}

	// Check health (no container running)
	health, err := mgr.CheckHealth(ctx, instance.InstanceID)
	if err != nil {
		t.Fatalf("CheckHealth failed: %v", err)
	}

	if health.Status != "unknown" {
		t.Errorf("Expected 'unknown' status without container, got %s", health.Status)
	}
}

// testStore creates a temporary store for testing.
func testStore(t *testing.T) *store.Store {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "integration-test.db")

	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(err.Error(), "fts5") {
			return nil
		}
		t.Fatalf("Failed to create store: %v", err)
	}

	return st
}
