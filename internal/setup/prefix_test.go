package setup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// --prefix exists so a scratch install can be removed without disturbing the
// real one. If it ever reaches outside the prefix it is worse than not existing,
// because the caller was told it was safe.
func TestUninstall_PrefixDoesNotReachOutsideIt(t *testing.T) {
	home := isolate(t)

	// The install the user actually depends on.
	realBin := filepath.Join(home, ".local", "bin", "conduit")
	if err := os.MkdirAll(filepath.Dir(realBin), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(realBin, []byte("real binary"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A shell profile with a Conduit PATH line, which belongs to that install.
	zshrc := filepath.Join(home, ".zshrc")
	profile := "export PATH=\"$HOME/.local/bin:$PATH\"  # conduit\n"
	if err := os.WriteFile(zshrc, []byte(profile), 0o644); err != nil {
		t.Fatalf("write zshrc: %v", err)
	}

	// A throwaway install somewhere else.
	scratch := t.TempDir()
	scratchBin := filepath.Join(scratch, "conduit")
	if err := os.WriteFile(scratchBin, []byte("scratch binary"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	dataDir := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir data: %v", err)
	}

	opts := NewUninstallOptionsKeepData()
	opts.Force = true
	opts.Prefix = scratch

	if _, err := Uninstall(context.Background(), dataDir, opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	if _, err := os.Stat(scratchBin); !os.IsNotExist(err) {
		t.Error("the prefixed binary was not removed")
	}
	if _, err := os.Stat(realBin); err != nil {
		t.Errorf("--prefix removed the binary outside the prefix: %v", err)
	}

	after, err := os.ReadFile(zshrc)
	if err != nil {
		t.Fatalf("read zshrc: %v", err)
	}
	if string(after) != profile {
		t.Errorf("--prefix edited the shell profile:\n got %q\nwant %q", after, profile)
	}
	if _, err := os.Stat(dataDir); err != nil {
		t.Errorf("--prefix removed the data directory: %v", err)
	}
}

// Without a prefix the default locations are still swept, or the flag's
// existence would have quietly changed normal uninstall behaviour.
func TestUninstall_NoPrefixRemovesDefaultLocations(t *testing.T) {
	home := isolate(t)

	realBin := filepath.Join(home, ".local", "bin", "conduit")
	if err := os.MkdirAll(filepath.Dir(realBin), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(realBin, []byte("real binary"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := NewUninstallOptionsKeepData()
	opts.Force = true

	if _, err := Uninstall(context.Background(), filepath.Join(home, ".conduit"), opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(realBin); !os.IsNotExist(err) {
		t.Error("the default-location binary survived an unprefixed uninstall")
	}
}

// A downloaded model is user data as much as the knowledge base is: it costs
// hundreds of megabytes to replace. --keep-data must not touch it.
func TestUninstall_KeepDataPreservesDownloadedModels(t *testing.T) {
	home := isolate(t)
	dataDir := filepath.Join(home, ".conduit")

	if err := os.MkdirAll(filepath.Join(dataDir, "models"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	model := filepath.Join(dataDir, "models", "some-model.gguf")
	if err := os.WriteFile(model, []byte("pretend gguf"), 0o644); err != nil {
		t.Fatalf("write model: %v", err)
	}

	opts := NewUninstallOptionsKeepData()
	opts.Force = true

	if _, err := Uninstall(context.Background(), dataDir, opts); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(model); err != nil {
		t.Errorf("--keep-data deleted a downloaded model: %v", err)
	}
}

func TestTargetBinaryPaths(t *testing.T) {
	isolate(t)

	if got := targetBinaryPaths(""); len(got) != len(binaryPaths()) {
		t.Errorf("empty prefix returned %v, want the default list", got)
	}

	got := targetBinaryPaths("/opt/conduit/bin")
	if len(got) != 1 || got[0] != filepath.Join("/opt/conduit/bin", "conduit") {
		t.Errorf("targetBinaryPaths with a prefix = %v, want exactly one path inside it", got)
	}
}
