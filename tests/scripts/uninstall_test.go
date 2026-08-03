package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// A1 -- the catastrophic-directory guard
// ---------------------------------------------------------------------------

// The guard has to run on the path a deletion would actually see, not on the
// string the user typed. Every entry below names a directory no Conduit
// installation could own, spelled in a way that a plain string comparison
// against a deny list waves straight through. The trailing-slash cases are the
// ones that matter most in practice: tab completion appends one.
func TestUninstallRejectsCatastrophicDataDirs(t *testing.T) {
	cases := []struct {
		name string
		dir  string
	}{
		{"root", "/"},
		{"root with trailing slash artifacts", "//"},
		{"root via dot", "/."},
		{"root via dot-dot", "/usr/.."},
		{"home via tilde", "~"},
		{"home via tilde slash", "~/"},
		{"users container", "/Users"},
		{"users container trailing slash", "/Users/"},
		{"etc", "/etc"},
		{"etc via redundant separators", "//etc//"},
		{"var", "/var/"},
		{"tmp", "/tmp"},
		{"empty", ""},
		{"whitespace only", "   "},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.installStubOnPath(t, v2Stub())

			r := e.run(t, "uninstall.sh", "--data-dir", c.dir, "--dry-run")

			if r.code == 0 {
				t.Fatalf("accepted %q as a data directory\n%s", c.dir, r.combined)
			}
			if !r.contains("refusing") && !r.contains("cannot be empty") {
				t.Errorf("rejected %q but not with a guard message:\n%s", c.dir, r.combined)
			}
			// Nothing may run before the guard: no delegation, no removal.
			if len(e.realInvocations(t)) != 0 {
				t.Errorf("the binary was invoked despite an unsafe --data-dir: %v", e.realInvocations(t))
			}
		})
	}
}

// The same table against remove-v1.sh, which builds <data-dir>/qdrant and
// <data-dir>/falkordb and would therefore aim rm -rf at /qdrant given "/".
func TestRemoveV1RejectsCatastrophicDataDirs(t *testing.T) {
	for _, dir := range []string{"/", "//", "/.", "~/", "/Users/", "/etc", ""} {
		t.Run(dir, func(t *testing.T) {
			e := newEnv(t)
			r := e.run(t, "remove-v1.sh", "--data-dir", dir, "--dry-run")
			if r.code == 0 {
				t.Fatalf("accepted %q\n%s", dir, r.combined)
			}
		})
	}
}

// A directory that really is a plausible data directory must still be accepted,
// or the guard has broken the tool it was meant to protect.
func TestUninstallAcceptsRealDataDir(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	// Including the trailing-slash spelling, which canonicalisation handles.
	for _, dir := range []string{e.dataDir, e.dataDir + "/", e.dataDir + "/."} {
		r := e.run(t, "uninstall.sh", "--data-dir", dir, "--dry-run")
		if r.code != 0 {
			t.Fatalf("rejected a legitimate data directory %q:\n%s", dir, r.combined)
		}
	}
}

// ---------------------------------------------------------------------------
// A6 -- symlinked data directory
// ---------------------------------------------------------------------------

// `rm -rf link/` on macOS empties the TARGET and keeps the link; os.RemoveAll
// removes the link and keeps the data. The two disagree, and both lose. The
// script refuses instead, which is the only answer that matches the binary.
func TestUninstallRefusesSymlinkedDataDir(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	realDir := filepath.Join(e.home, "elsewhere")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	precious := filepath.Join(realDir, "conduit.db")
	e.writeFile(t, precious, "the real knowledge base")

	link := filepath.Join(e.home, "linked-conduit")
	if err := os.Symlink(realDir, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// Both spellings, because the trailing slash is what changes rm's meaning.
	for _, spelling := range []string{link, link + "/"} {
		r := e.run(t, "uninstall.sh", "--data-dir", spelling, "--remove-data", "--force")
		if r.code == 0 {
			t.Fatalf("accepted a symlinked data directory %q\n%s", spelling, r.combined)
		}
		if !r.contains("symlink") {
			t.Errorf("refusal does not explain the symlink:\n%s", r.combined)
		}
	}

	if !exists(precious) {
		t.Fatal("the symlink target's contents were destroyed")
	}
	if !exists(link) {
		t.Error("the symlink itself was removed")
	}
}

// ---------------------------------------------------------------------------
// A2 -- delegation capability probe
// ---------------------------------------------------------------------------

// A v1 binary has no --data-dir. Passing it one makes cobra exit non-zero
// without doing anything, which used to be indistinguishable from a crash and
// dropped the run into the manual path -- the less careful one -- on every v1
// machine.
func TestDelegationRefusesV1BinaryWhenDataDirRequested(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v1Stub())

	r := e.run(t, "uninstall.sh", "--data-dir", e.dataDir, "--dry-run")

	if r.code == 0 {
		t.Fatalf("delegated --data-dir to a binary that does not support it\n%s", r.combined)
	}
	if !r.contains("does not support --data-dir") {
		t.Errorf("error does not name the missing capability:\n%s", r.combined)
	}
	// It must stop, not quietly proceed down the manual path.
	if r.contains("Shell profiles") {
		t.Errorf("fell through to the manual path after a failed probe:\n%s", r.combined)
	}
	if len(e.realInvocations(t)) != 0 {
		t.Errorf("ran the binary anyway: %v", e.realInvocations(t))
	}
}

// The mirror case: a v2 binary must actually receive the flag.
func TestDelegationForwardsDataDirToV2Binary(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	r := e.run(t, "uninstall.sh", "--data-dir", e.dataDir, "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	invs := e.realInvocations(t)
	if len(invs) == 0 {
		t.Fatal("the binary was never invoked")
	}
	if !strings.Contains(invs[0], "--data-dir "+e.dataDir) {
		t.Errorf("--data-dir was not forwarded: %q", invs[0])
	}
}

// A default data directory must NOT be forwarded: doing so overrides a data_dir
// the user configured in conduit.yaml, which is the mirror image of the bug
// where it was never forwarded at all.
func TestDelegationDoesNotForwardDefaultDataDir(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	r := e.run(t, "uninstall.sh", "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	invs := e.realInvocations(t)
	if len(invs) == 0 {
		t.Fatal("the binary was never invoked")
	}
	if strings.Contains(invs[0], "--data-dir") {
		t.Errorf("forwarded a data directory the user never named: %q", invs[0])
	}
}

// --prefix has the same problem: a v1 binary would remove ~/.local/bin instead
// of the prefix it was asked about.
func TestDelegationRefusesV1BinaryWhenPrefixRequested(t *testing.T) {
	e := newEnv(t)

	// --prefix makes find_binary look only inside the prefix, so the v1 stub
	// has to be the one living there.
	scratch := filepath.Join(e.home, "scratch")
	e.installStub(t, filepath.Join(scratch, "conduit"), v1Stub())

	r := e.run(t, "uninstall.sh", "--prefix", scratch, "--dry-run")
	if r.code == 0 {
		t.Fatalf("delegated --prefix to a v1 binary\n%s", r.combined)
	}
	if !r.contains("does not support --prefix") {
		t.Errorf("error does not name the missing capability:\n%s", r.combined)
	}
}

// A delegation failure is a blocking error. The manual path knows less and is
// less careful, so silently downgrading to it is how a wrapper does damage the
// tool it wrapped had refused to do.
func TestDelegationFailureIsFatalNotAFallback(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, stubOptions{SupportsDataDir: true, SupportsPrefix: true, ExitCode: 1})

	r := e.run(t, "uninstall.sh", "--dry-run")

	if r.code == 0 {
		t.Fatalf("a failed delegation reported success\n%s", r.combined)
	}
	if !r.contains("Refusing to fall back") {
		t.Errorf("did not refuse the fallback:\n%s", r.combined)
	}
	if r.contains("Shell profiles") {
		t.Errorf("ran the manual path after a failure:\n%s", r.combined)
	}
}

// ---------------------------------------------------------------------------
// A5 -- user-cancel propagation
// ---------------------------------------------------------------------------

// Exit code 3 means "the user said no". The script must stop, and above all
// must not go on to delete the binary the user just declined to remove and then
// ask the same question a second time.
func TestUserCancelStopsTheWholeRun(t *testing.T) {
	e := newEnv(t)
	binary := e.installStubOnPath(t, stubOptions{
		SupportsDataDir: true, SupportsPrefix: true, ExitCode: 3,
	})

	r := e.run(t, "uninstall.sh", "--remove-data")

	if r.code != 3 {
		t.Errorf("exit %d, want 3 (user cancelled)\n%s", r.code, r.combined)
	}
	if !exists(binary) {
		t.Error("the binary was removed after the user cancelled")
	}
	if !exists(filepath.Join(e.dataDir, "conduit.db")) {
		t.Error("the knowledge base was removed after the user cancelled")
	}
	if r.contains("Type UNINSTALL") {
		t.Error("re-prompted after the user had already declined")
	}
}

// ---------------------------------------------------------------------------
// A3 -- shell profile surgery
// ---------------------------------------------------------------------------

// The fallback used to delete any PATH line mentioning .local/bin. pipx, uv,
// poetry and pip --user all write that exact line, so an uninstall would remove
// other tools from the user's PATH -- surfacing later as "command not found"
// for something that has nothing to do with Conduit.
func TestFallbackProfileSurgeryIsPrecise(t *testing.T) {
	e := newEnv(t)
	// No binary anywhere: this is the path that only runs when there is none.

	zshrc := filepath.Join(e.home, ".zshrc")
	original := strings.Join([]string{
		`# created by pipx`,
		`export PATH="$HOME/.local/bin:$PATH"`,
		``,
		`# Conduit`,
		`export PATH="$HOME/.local/bin:$PATH"`,
		``,
		`alias notes='conduit kb search'`,
		`# a note about conduit being useful`,
		`export EDITOR=vim`,
		``,
	}, "\n")
	e.writeFile(t, zshrc, original)

	r := e.run(t, "uninstall.sh")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	after := readFile(t, zshrc)

	// The pipx line and everything else the user owns must survive.
	for _, keep := range []string{
		"# created by pipx",
		"alias notes='conduit kb search'",
		"# a note about conduit being useful",
		"export EDITOR=vim",
	} {
		if !strings.Contains(after, keep) {
			t.Errorf("deleted a line Conduit did not write: %q\n--- after ---\n%s", keep, after)
		}
	}

	// The marker and its one following line must be gone.
	if strings.Contains(after, "# Conduit") {
		t.Errorf("Conduit marker survived:\n%s", after)
	}
	// pipx's PATH line is identical to Conduit's, so exactly one must remain.
	if got := strings.Count(after, `export PATH="$HOME/.local/bin:$PATH"`); got != 1 {
		t.Errorf("PATH export count = %d, want 1 (pipx's, not Conduit's)\n%s", got, after)
	}

	backup := zshrc + ".conduit-uninstall.bak"
	if !exists(backup) {
		t.Fatal("no backup was written before editing the profile")
	}
	if readFile(t, backup) != original {
		t.Error("the backup does not match the original file")
	}
}

// A profile with no Conduit marker must not be touched at all -- not rewritten,
// not backed up, not reported.
func TestFallbackLeavesUnmarkedProfileAlone(t *testing.T) {
	e := newEnv(t)

	zshrc := filepath.Join(e.home, ".zshrc")
	original := "export PATH=\"$HOME/.local/bin:$PATH\"\nalias c='conduit'\n"
	e.writeFile(t, zshrc, original)

	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	if after := readFile(t, zshrc); after != original {
		t.Errorf("an unmarked profile was modified:\n--- want ---\n%s\n--- got ---\n%s", original, after)
	}
	if exists(zshrc + ".conduit-uninstall.bak") {
		t.Error("backed up a file it had no reason to touch")
	}
}

// The rewrite must not change who can read the profile. mktemp creates 0600, so
// moving it into place unchanged would silently tighten permissions.
func TestFallbackPreservesProfilePermissions(t *testing.T) {
	e := newEnv(t)

	zshrc := filepath.Join(e.home, ".zshrc")
	e.writeFile(t, zshrc, "# Conduit\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport EDITOR=vim\n")
	if err := os.Chmod(zshrc, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	info, err := os.Stat(zshrc)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("profile mode = %o, want 644 (mktemp's 600 leaked through)", got)
	}
}

// ---------------------------------------------------------------------------
// A10 -- dry-run honesty
// ---------------------------------------------------------------------------

// A dry run that previews only the delegated path is not a preview of the run
// the user is about to make: if the binary is unavailable on the day, the real
// run takes the fallback, which edits shell profiles.
func TestDryRunPreviewsFallbackToo(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	zshrc := filepath.Join(e.home, ".zshrc")
	e.writeFile(t, zshrc, "# Conduit\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")

	r := e.run(t, "uninstall.sh", "--dry-run")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	if !r.contains("fallback would also") {
		t.Errorf("dry run does not mention the fallback path:\n%s", r.combined)
	}
	if !r.contains(".zshrc") {
		t.Errorf("dry run does not preview the profile edit:\n%s", r.combined)
	}
	// And it must remain a preview.
	if !exists(zshrc) || !strings.Contains(readFile(t, zshrc), "# Conduit") {
		t.Error("the dry run edited the profile")
	}
}

// ---------------------------------------------------------------------------
// Data safety end to end
// ---------------------------------------------------------------------------

// The default is to keep data. This is the single most important property of
// the whole script.
func TestUninstallKeepsDataByDefault(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	r := e.run(t, "uninstall.sh")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}
	if !exists(filepath.Join(e.dataDir, "conduit.db")) {
		t.Fatal("the knowledge base was deleted without --remove-data")
	}
	invs := e.realInvocations(t)
	if len(invs) == 0 || !strings.Contains(invs[0], "--keep-data") {
		t.Errorf("did not ask the binary to keep data: %v", invs)
	}
}

// --force must be forwarded only when the user gave it. Hardcoding it skipped
// the binary's confirmation gate entirely.
func TestForceIsForwardedOnlyWhenGiven(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	e.run(t, "uninstall.sh", "--dry-run")
	invs := e.realInvocations(t)
	if len(invs) == 0 {
		t.Fatal("no invocation recorded")
	}
	if strings.Contains(invs[0], "--force") {
		t.Errorf("--force was passed although the user never gave it: %q", invs[0])
	}

	e2 := newEnv(t)
	e2.installStubOnPath(t, v2Stub())
	e2.run(t, "uninstall.sh", "--dry-run", "--force")
	invs2 := e2.realInvocations(t)
	if len(invs2) == 0 || !strings.Contains(invs2[0], "--force") {
		t.Errorf("--force was not forwarded when the user gave it: %v", invs2)
	}
}

// --prefix and --remove-data are contradictory: the data directory is shared
// and lives outside every prefix.
func TestPrefixWithRemoveDataIsRejected(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	r := e.run(t, "uninstall.sh", "--prefix", filepath.Join(e.home, "scratch"), "--remove-data")
	if r.code == 0 {
		t.Fatalf("accepted contradictory flags\n%s", r.combined)
	}
	if !r.contains("mutually exclusive") {
		t.Errorf("error does not explain the contradiction:\n%s", r.combined)
	}
}

// An empty --prefix used to slip the contradiction check and then mean "no
// prefix" downstream, turning a scoped removal into a full-scope one.
func TestEmptyPrefixIsRejected(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, v2Stub())

	r := e.run(t, "uninstall.sh", "--prefix", "", "--dry-run")
	if r.code == 0 {
		t.Fatalf("accepted an empty --prefix\n%s", r.combined)
	}
}
