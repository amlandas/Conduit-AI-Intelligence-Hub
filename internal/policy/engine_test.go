package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/simpleflo/conduit/internal/store"
)

func TestEngine_EvaluateDenyRoot(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()
	req := Request{
		InstanceID: "inst_test",
		PackageID:  "test/connector",
		Requested: PermissionSet{
			Filesystem: FilesystemPerms{
				ReadonlyPaths: []string{"/"},
			},
		},
	}

	decision, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if decision.Decision != Deny {
		t.Errorf("expected DENY for root mount, got %s", decision.Decision)
	}

	if len(decision.BlockReasons) == 0 {
		t.Error("expected block reasons for denied request")
	}
}

func TestEngine_EvaluateDenyHomeSensitive(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()
	homeDir, _ := os.UserHomeDir()

	sensitiveSpots := []string{
		filepath.Join(homeDir, ".ssh"),
		filepath.Join(homeDir, ".aws"),
		filepath.Join(homeDir, ".gnupg"),
	}

	for _, path := range sensitiveSpots {
		req := Request{
			InstanceID: "inst_test",
			PackageID:  "test/connector",
			Requested: PermissionSet{
				Filesystem: FilesystemPerms{
					ReadonlyPaths: []string{path},
				},
			},
		}

		decision, err := engine.Evaluate(ctx, req)
		if err != nil {
			t.Fatalf("Evaluate failed for %s: %v", path, err)
		}

		if decision.Decision != Deny {
			t.Errorf("expected DENY for %s mount, got %s", path, decision.Decision)
		}
	}
}

func TestEngine_EvaluateAllowSafeMount(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	req := Request{
		InstanceID: "inst_test",
		PackageID:  "test/connector",
		Requested: PermissionSet{
			Filesystem: FilesystemPerms{
				ReadonlyPaths: []string{tmpDir},
			},
		},
	}

	decision, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Safe paths should NOT be denied. They may return ALLOW or WARN
	// (WARN means allowed but user hasn't explicitly granted the path)
	if decision.Decision == Deny {
		t.Errorf("expected safe path to not be denied, got DENY. Reasons: %v", decision.BlockReasons)
	}
}

func TestEngine_EvaluateNetworkEgress(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()
	req := Request{
		InstanceID: "inst_test",
		PackageID:  "test/connector",
		Requested: PermissionSet{
			Network: NetworkPerms{
				Mode:          "egress",
				EgressDomains: []string{"api.example.com"},
			},
		},
	}

	decision, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	// Network egress should be evaluated
	if decision.Decision == "" {
		t.Error("expected a decision")
	}
}

func TestEngine_GrantAndCheckPermission(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()
	instanceID := "inst_grant_test"
	tmpDir := t.TempDir()

	// Grant permissions
	perms := PermissionSet{
		Filesystem: FilesystemPerms{
			ReadwritePaths: []string{tmpDir},
		},
	}

	if err := engine.GrantPermission(ctx, instanceID, perms); err != nil {
		t.Fatalf("GrantPermission failed: %v", err)
	}

	// Now evaluate
	req := Request{
		InstanceID: instanceID,
		PackageID:  "test/connector",
		Requested: PermissionSet{
			Filesystem: FilesystemPerms{
				ReadwritePaths: []string{tmpDir},
			},
		},
	}

	decision, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if decision.Decision == Deny {
		t.Errorf("expected granted permission to allow, got %s", decision.Decision)
	}
}

func TestEngine_DecisionID(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()
	req := Request{
		InstanceID: "inst_test",
		PackageID:  "test/connector",
		Requested:  PermissionSet{},
	}

	d1, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	d2, err := engine.Evaluate(ctx, req)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if d1.DecisionID == "" {
		t.Error("expected non-empty DecisionID")
	}

	if d1.DecisionID == d2.DecisionID {
		t.Error("expected unique DecisionIDs")
	}
}

func TestEngine_DenySystemPaths(t *testing.T) {
	engine := testEngine(t)
	if engine == nil {
		t.Skip("FTS5 not available, skipping test")
	}

	ctx := context.Background()

	// Test paths that should be DENIED (in forbiddenPaths list)
	deniedPaths := []string{"/etc", "/var", "/root"}

	for _, path := range deniedPaths {
		req := Request{
			InstanceID: "inst_test",
			PackageID:  "test/connector",
			Requested: PermissionSet{
				Filesystem: FilesystemPerms{
					ReadonlyPaths: []string{path},
				},
			},
		}

		decision, err := engine.Evaluate(ctx, req)
		if err != nil {
			t.Fatalf("Evaluate failed for %s: %v", path, err)
		}

		if decision.Decision != Deny {
			t.Errorf("expected DENY for %s mount, got %s", path, decision.Decision)
		}
	}

	// Test paths that should NOT be denied (not in forbiddenPaths list)
	// These paths may return WARN since they're not explicitly granted
	allowedPaths := []string{"/usr", "/opt"}

	for _, path := range allowedPaths {
		req := Request{
			InstanceID: "inst_test",
			PackageID:  "test/connector",
			Requested: PermissionSet{
				Filesystem: FilesystemPerms{
					ReadonlyPaths: []string{path},
				},
			},
		}

		decision, err := engine.Evaluate(ctx, req)
		if err != nil {
			t.Fatalf("Evaluate failed for %s: %v", path, err)
		}

		if decision.Decision == Deny {
			t.Errorf("expected %s mount to not be denied, got DENY. Reasons: %v", path, decision.BlockReasons)
		}
	}
}

// testEngine creates a test policy engine.
func testEngine(t *testing.T) *Engine {
	t.Helper()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.New(dbPath)
	if err != nil {
		// FTS5 not available, return nil to skip test
		if err.Error() == "migrate database: run migration 001: no such module: fts5" ||
			err.Error() == "migrate database: run migration 003: no such module: fts5" {
			return nil
		}
		t.Fatalf("failed to create store: %v", err)
	}

	t.Cleanup(func() {
		st.Close()
	})

	return New(st.DB())
}
