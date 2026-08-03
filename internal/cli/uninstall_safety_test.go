package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A4: the prompt has to name the directory it is about to delete.
//
// dataDir comes from config, and config precedence includes a conduit.yaml in
// the working directory -- so the path about to go is not necessarily the one
// the user has in mind. The old prompt said "this will permanently delete all
// Conduit data" and never said which directory that was, omitting the single
// fact needed to catch the mistake.
func TestUninstallPromptNamesTheDirectory(t *testing.T) {
	env := newTestEnv(t)
	dataDir := filepath.Join(env.home, ".conduit")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "conduit.db"), []byte("db"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// No --force, and stdin is empty, so the prompt is printed and then
	// declined by EOF.
	out, code := env.run(t, "uninstall", "--all")

	if !strings.Contains(out, dataDir) {
		t.Errorf("the confirmation does not name the directory:\n%s", out)
	}
	if !strings.Contains(out, "Type 'UNINSTALL'") {
		t.Errorf("no confirmation was shown:\n%s", out)
	}
	// A5: declining must be distinguishable from success.
	if code != exitCodeUserCancelled {
		t.Errorf("exit %d, want %d (user cancelled)", code, exitCodeUserCancelled)
	}
	if _, err := os.Stat(filepath.Join(dataDir, "conduit.db")); err != nil {
		t.Error("the knowledge base was deleted despite the cancellation")
	}
}

// A4: a directory holding none of Conduit's own files is almost certainly a
// mistyped --data-dir, and recursively deleting it on the strength of a typo is
// not a risk worth taking silently.
func TestUninstallRefusesDirectoryThatIsNotConduits(t *testing.T) {
	env := newTestEnv(t)

	stranger := filepath.Join(env.home, "Documents")
	if err := os.MkdirAll(stranger, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	thesis := filepath.Join(stranger, "thesis.txt")
	if err := os.WriteFile(thesis, []byte("years of work"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, code := env.run(t, "--data-dir", stranger, "uninstall", "--all")

	if code == 0 {
		t.Fatalf("deleted a directory with no Conduit files in it\n%s", out)
	}
	if !strings.Contains(out, "does not look like a Conduit data directory") {
		t.Errorf("error does not explain the refusal:\n%s", out)
	}
	if _, err := os.Stat(thesis); err != nil {
		t.Fatalf("the stranger's files were deleted: %v", err)
	}
}

// A1: the catastrophic-directory guard applies to the binary too, not just the
// wrapper script.
func TestUninstallRejectsCatastrophicDataDir(t *testing.T) {
	for _, dir := range []string{"/", "//", "/etc/", "/Users/", "~/"} {
		t.Run(dir, func(t *testing.T) {
			env := newTestEnv(t)
			out, code := env.run(t, "--data-dir", dir, "uninstall", "--all", "--force")
			if code == 0 {
				t.Fatalf("accepted %q as a data directory\n%s", dir, out)
			}
			if !strings.Contains(out, "refusing") {
				t.Errorf("no guard message for %q:\n%s", dir, out)
			}
		})
	}
}

// A6: a symlinked data directory is refused rather than followed, because
// os.RemoveAll would remove the link and leave the data behind while the user
// believed it was gone.
func TestUninstallRefusesSymlinkedDataDir(t *testing.T) {
	env := newTestEnv(t)

	real := filepath.Join(env.home, "real-data")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db := filepath.Join(real, "conduit.db")
	if err := os.WriteFile(db, []byte("knowledge base"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	link := filepath.Join(env.home, "linked-data")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	out, code := env.run(t, "--data-dir", link, "uninstall", "--all", "--force")
	if code == 0 {
		t.Fatalf("accepted a symlinked data directory\n%s", out)
	}
	if _, err := os.Stat(db); err != nil {
		t.Fatal("the symlink target's contents were destroyed")
	}
}

// An empty --prefix used to slip the mutual-exclusion check and then mean
// "no prefix" downstream: the user asked for a scoped removal and got a
// full-scope one.
func TestUninstallRejectsEmptyPrefix(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "uninstall", "--prefix", "", "--keep-data", "--force")
	if code == 0 {
		t.Fatalf("accepted an empty --prefix\n%s", out)
	}
	if !strings.Contains(out, "empty value") {
		t.Errorf("error does not explain the problem:\n%s", out)
	}

	// And it must still be caught when combined with --all.
	out, code = env.run(t, "uninstall", "--prefix", "", "--all", "--force")
	if code == 0 {
		t.Fatalf("accepted an empty --prefix with --all\n%s", out)
	}
}

// --force skips the prompt, which is the whole point of it, but the safety
// guards on the path itself still apply.
func TestUninstallForceStillHonoursPathGuards(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "--data-dir", "/", "uninstall", "--all", "--force")
	if code == 0 {
		t.Fatalf("--force bypassed the catastrophic-path guard\n%s", out)
	}
	_ = out
}

// A dry run must never prompt and never delete.
func TestUninstallDryRunNeitherPromptsNorDeletes(t *testing.T) {
	env := newTestEnv(t)
	dataDir := filepath.Join(env.home, ".conduit")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db := filepath.Join(dataDir, "conduit.db")
	if err := os.WriteFile(db, []byte("db"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	out, code := env.run(t, "uninstall", "--all", "--dry-run")
	if code != 0 {
		t.Fatalf("dry run exited %d\n%s", code, out)
	}
	if strings.Contains(out, "Type 'UNINSTALL'") {
		t.Errorf("dry run prompted for confirmation:\n%s", out)
	}
	if _, err := os.Stat(db); err != nil {
		t.Error("dry run deleted the knowledge base")
	}
}
