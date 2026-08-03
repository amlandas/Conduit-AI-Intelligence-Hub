package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// N2: the deny list was a byte comparison, and on macOS that is not a guard at
// all. APFS is case-insensitive by default, so "/USERS/<you>" opens exactly the
// same directory as "/Users/<you>" while matching nothing in the list -- and
// with --force skipping the "does this look like Conduit's?" backstop, the next
// step was a recursive delete of the user's home.
//
// The fix compares device and inode, which is what the kernel uses to decide
// whether two names are one directory.
func TestAssertSafeDataDir_CaseInsensitiveEvasion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home directory")
	}

	// Only meaningful where the filesystem folds case. On a case-sensitive
	// filesystem the upper-cased path simply does not exist, and the lexical
	// check plus a failed Stat already refuse it.
	upper := strings.ToUpper(home)
	if upper == home {
		t.Skip("home directory has no case to fold")
	}
	if !sameDir(home, upper) {
		t.Skipf("filesystem is case-sensitive; %s is not %s", upper, home)
	}

	if _, err := AssertSafeDataDir(upper); err == nil {
		t.Fatalf("accepted %q, which IS the home directory on this filesystem", upper)
	} else if !errors.Is(err, ErrUnsafeDataDir) {
		t.Errorf("error = %v, want ErrUnsafeDataDir", err)
	}
}

// The same hole, reached through a mixed-case system directory.
func TestAssertSafeDataDir_MixedCaseSystemDirs(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("case-folding behaviour is filesystem-specific; exercised on macOS")
	}

	for _, spelling := range []string{"/ETC", "/etc/", "/Etc", "/USERS", "/users"} {
		if !sameDir(spelling, filepath.Clean(strings.ToLower(spelling))) {
			// Not case-folding here, so there is nothing to evade.
			continue
		}
		if _, err := AssertSafeDataDir(spelling); err == nil {
			t.Errorf("accepted %q, which resolves to a protected directory", spelling)
		}
	}
}

// The identity check must also catch a hard link or bind mount to a protected
// directory, which no amount of string comparison can see.
func TestSameDir_ComparesIdentityNotSpelling(t *testing.T) {
	dir := t.TempDir()

	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// The same directory named two ways.
	viaDot := filepath.Join(dir, ".", "real")
	viaParent := filepath.Join(dir, "real", "..", "real")

	for _, spelling := range []string{viaDot, viaParent} {
		if !sameDir(real, spelling) {
			t.Errorf("sameDir(%q, %q) = false, want true", real, spelling)
		}
	}

	other := filepath.Join(dir, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if sameDir(real, other) {
		t.Error("two genuinely different directories compared equal")
	}

	// A path that does not exist must not compare equal to anything.
	if sameDir(real, filepath.Join(dir, "absent")) {
		t.Error("an absent path compared equal to a real one")
	}
}

// N9: the "does this look like Conduit's?" backstop belongs in the library, so
// the desktop GUI and any other caller are covered, not only the CLI.
func TestUninstall_RefusesDirectoryThatIsNotConduits(t *testing.T) {
	home := isolate(t)

	stranger := filepath.Join(home, "Documents")
	if err := os.MkdirAll(stranger, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	thesis := filepath.Join(stranger, "thesis.txt")
	if err := os.WriteFile(thesis, []byte("years of work"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := NewUninstallOptionsAll() // RemoveDataDir, Force unset

	_, err := Uninstall(context.Background(), stranger, opts)
	if err == nil {
		t.Fatal("Uninstall deleted a directory holding none of Conduit's files")
	}
	if !errors.Is(err, ErrNotConduitDataDir) {
		t.Errorf("error = %v, want ErrNotConduitDataDir", err)
	}
	if _, serr := os.Stat(thesis); serr != nil {
		t.Fatalf("the stranger's files were deleted: %v", serr)
	}
}

// Force is the deliberate override: a data directory whose contents are already
// gone is a legitimate thing to remove.
func TestUninstall_ForceOverridesTheConduitDirBackstop(t *testing.T) {
	home := isolate(t)

	empty := filepath.Join(home, "empty-data")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	opts := NewUninstallOptionsAll()
	opts.Force = true

	if _, err := Uninstall(context.Background(), empty, opts); err != nil {
		t.Fatalf("Force did not override the backstop: %v", err)
	}
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Error("the directory was not removed despite Force")
	}
}

// A real data directory goes through without Force.
func TestUninstall_RealDataDirNeedsNoForce(t *testing.T) {
	home := isolate(t)

	dataDir := filepath.Join(home, ".conduit")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "conduit.db"), []byte("db"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if _, err := Uninstall(context.Background(), dataDir, NewUninstallOptionsAll()); err != nil {
		t.Fatalf("a genuine data directory was refused: %v", err)
	}
	if _, err := os.Stat(dataDir); !os.IsNotExist(err) {
		t.Error("the data directory was not removed")
	}
}

// Force must not be a skeleton key: the path guards still apply.
func TestUninstall_ForceStillHonoursPathGuards(t *testing.T) {
	isolate(t)

	opts := NewUninstallOptionsAll()
	opts.Force = true

	for _, dir := range []string{"/", "/etc/", "~/", "/Volumes"} {
		if _, err := Uninstall(context.Background(), dir, opts); err == nil {
			t.Errorf("Force bypassed the path guard for %q", dir)
		}
	}
}
