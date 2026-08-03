package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A1: the guard has to live in the library, not only in the wrapper scripts.
// The binary is invoked directly by users, by the desktop GUI and by other
// scripts; a check that only exists in uninstall.sh protects none of them.
func TestAssertSafeDataDir_RejectsCatastrophicPaths(t *testing.T) {
	home := isolate(t)

	cases := []struct {
		name string
		dir  string
	}{
		{"root", "/"},
		{"root doubled separator", "//"},
		{"root via dot", "/."},
		{"root via dot-dot", "/usr/.."},
		{"home", home},
		{"home trailing separator", home + "/"},
		{"home via tilde", "~"},
		{"home via tilde slash", "~/"},
		{"home via dot", home + "/."},
		{"home via dot-dot", home + "/sub/.."},
		{"users container", "/Users"},
		{"users container trailing separator", "/Users/"},
		{"etc", "/etc"},
		{"etc via redundant separators", "//etc//"},
		{"var", "/var/"},
		{"tmp", "/tmp"},
		{"empty", ""},
		{"whitespace", "   "},

		// N3: mount points. An external disk, a network share or a container
		// bind mount is somebody's whole filesystem, and "/Volumes" differs
		// from a real data directory by one missing path component.
		{"volumes", "/Volumes"},
		{"volumes trailing separator", "/Volumes/"},
		{"mnt", "/mnt"},
		{"media", "/media"},
		{"net", "/net"},
		{"srv", "/srv"},

		// #87: on macOS Catalina and later the system volume is read-only and
		// everything writable lives on a second volume mounted here -- every
		// user's home included. "/System" was already listed, but this list is
		// matched by exact path and by device:inode, and neither catches a
		// child. The data volume is its own mount with its own inode, distinct
		// from both "/" and "/Users", so nothing here resembled it.
		{"macos data volume", "/System/Volumes/Data"},
		{"macos data volume trailing separator", "/System/Volumes/Data/"},
		{"macos data volume via dot", "/System/Volumes/Data/."},
		{"macos data volume via dot-dot", "/System/Volumes/Data/Users/.."},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := AssertSafeDataDir(c.dir)
			if err == nil {
				t.Fatalf("accepted %q (resolved to %q)", c.dir, got)
			}
			if !errors.Is(err, ErrUnsafeDataDir) {
				t.Errorf("error = %v, want ErrUnsafeDataDir", err)
			}
		})
	}
}

// The guard must not have broken the ordinary case.
func TestAssertSafeDataDir_AcceptsRealDataDirs(t *testing.T) {
	home := isolate(t)

	dataDir := filepath.Join(home, ".conduit")
	for _, spelling := range []string{dataDir, dataDir + "/", dataDir + "/.", "~/.conduit"} {
		got, err := AssertSafeDataDir(spelling)
		if err != nil {
			t.Fatalf("rejected %q: %v", spelling, err)
		}
		if got != dataDir {
			t.Errorf("AssertSafeDataDir(%q) = %q, want %q", spelling, got, dataDir)
		}
	}
}

// Canonicalisation is what makes the deny list meaningful; every evasion above
// is a spelling difference, not a different directory.
func TestCanonicalDataDir_ResolvesSpellings(t *testing.T) {
	home := isolate(t)

	cases := map[string]string{
		"/":                    "/",
		"//":                   "/",
		"/.":                   "/",
		"/usr/..":              "/",
		"/etc/":                "/etc",
		"//etc//":              "/etc",
		"/a/b/../c":            "/a/c",
		"~":                    home,
		"~/":                   home,
		"~/.conduit":           filepath.Join(home, ".conduit"),
		home + "/x/../.conduit": filepath.Join(home, ".conduit"),
	}

	for in, want := range cases {
		got, err := CanonicalDataDir(in)
		if err != nil {
			t.Errorf("CanonicalDataDir(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("CanonicalDataDir(%q) = %q, want %q", in, got, want)
		}
	}
}

// A6: os.RemoveAll on a symlinked data directory removes the link and leaves
// the data, so the user believes their knowledge base is gone when it is not.
// `rm -rf dir/` in the script makes the opposite mistake. Refusing is the only
// answer that is the same in both.
func TestAssertRemovableDataDir_RefusesSymlink(t *testing.T) {
	home := isolate(t)

	realDir := filepath.Join(home, "real-data")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	link := filepath.Join(home, "linked-data")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	for _, spelling := range []string{link, link + "/"} {
		_, err := AssertRemovableDataDir(spelling)
		if err == nil {
			t.Fatalf("accepted symlinked data directory %q", spelling)
		}
		if !errors.Is(err, ErrSymlinkedDataDir) {
			t.Errorf("error = %v, want ErrSymlinkedDataDir", err)
		}
	}
}

// Uninstall must apply the guard itself, so that a caller which never went
// through the CLI is still protected.
func TestUninstall_RefusesSymlinkedDataDir(t *testing.T) {
	home := isolate(t)

	realDir := filepath.Join(home, "real-data")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	precious := filepath.Join(realDir, "conduit.db")
	if err := os.WriteFile(precious, []byte("knowledge base"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	link := filepath.Join(home, "linked-data")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	opts := NewUninstallOptionsAll()
	opts.Force = true

	if _, err := Uninstall(context.Background(), link, opts); err == nil {
		t.Fatal("Uninstall accepted a symlinked data directory")
	}
	if !exists(precious) {
		t.Fatal("the symlink target's contents were destroyed")
	}
	if !exists(link) {
		t.Error("the symlink was removed, leaving the user unsure where their data went")
	}
}

// The library guard must fire even when the caller is not the CLI.
func TestUninstall_RefusesCatastrophicDataDir(t *testing.T) {
	isolate(t)

	opts := NewUninstallOptionsAll()
	opts.Force = true

	for _, dir := range []string{"/", "//", "/etc/", "~/"} {
		if _, err := Uninstall(context.Background(), dir, opts); err == nil {
			t.Errorf("Uninstall accepted %q as a data directory", dir)
		}
	}
}

func TestSummarizeDataDir(t *testing.T) {
	home := isolate(t)

	missing := SummarizeDataDir(filepath.Join(home, "nope"))
	if missing.Exists || missing.LooksLikeConduit {
		t.Errorf("absent dir: Exists=%v LooksLikeConduit=%v", missing.Exists, missing.LooksLikeConduit)
	}

	// A directory full of somebody else's files is not a Conduit data
	// directory, and saying so is what stops a mistyped --data-dir becoming a
	// recursive delete.
	stranger := filepath.Join(home, "documents")
	if err := os.MkdirAll(stranger, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stranger, "thesis.txt"), []byte("years of work"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := SummarizeDataDir(stranger)
	if !s.Exists {
		t.Error("Exists = false for a directory that exists")
	}
	if s.LooksLikeConduit {
		t.Error("a directory with no Conduit files was reported as a Conduit data directory")
	}

	real := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(real, "conduit.db"), []byte("db"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s = SummarizeDataDir(real)
	if !s.Exists || !s.LooksLikeConduit {
		t.Errorf("real data dir: Exists=%v LooksLikeConduit=%v", s.Exists, s.LooksLikeConduit)
	}
	if s.Size == "" {
		t.Error("no size reported for the confirmation prompt")
	}
}

// exists is a small helper shared by the tests in this file.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}
