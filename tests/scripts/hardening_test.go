package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// N2 -- identity, not spelling
// ---------------------------------------------------------------------------

// The deny list was a byte comparison, which on macOS is not a guard at all:
// APFS folds case, so "/USERS/<you>" opens exactly the same directory as
// "/Users/<you>" while matching nothing in the list.
//
// This test uses the developer's REAL home directory name, because that is the
// only thing whose case-folding behaviour is the property under test. It only
// ever passes that path to --dry-run, and asserts the script refuses before
// doing anything.
func TestGuardCatchesCaseFoldedRealHome(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil || realHome == "" {
		t.Skip("no home directory")
	}
	upper := strings.ToUpper(realHome)
	if upper == realHome {
		t.Skip("home directory has no case to fold")
	}
	if !sameDirOnDisk(realHome, upper) {
		t.Skipf("filesystem is case-sensitive; %s is not %s", upper, realHome)
	}

	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			// Deliberately NOT overriding HOME here: the point is that the real
			// home is reachable under a folded spelling.
			r := e.runWithRealHome(t, script, "--data-dir", upper, "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted %q, which IS the home directory\n%s", script, upper, r.combined)
			}
			if !r.contains("refusing") {
				t.Errorf("no guard message:\n%s", r.combined)
			}
		})
	}
}

// The reviewer's evasion table, run against the fake machine's own home.
//
// The rows split into two kinds, and conflating them is what broke CI on
// ubuntu-latest. "/Users" names a protected directory on every platform,
// because the guard lists that exact string. "/USERS" only names one where the
// filesystem folds case: on ext4 it is simply a path that does not exist and is
// genuinely NOT /Users, so a guard that accepts it is behaving correctly.
//
// Whether a row is testable is therefore a question about the filesystem under
// the test, which is probed rather than assumed.
func TestGuardEvasionTable(t *testing.T) {
	cases := []struct {
		dir string
		// aliasOf, when set, names the protected directory this spelling is
		// only equivalent to on a case-insensitive filesystem. The row is
		// tested when the two really are one directory on this machine.
		//
		// Checked per row rather than by probing a temp directory, because
		// TMPDIR is not necessarily on the same filesystem as / -- tmpfs on
		// Linux, a separate volume on a partitioned Mac -- and the question the
		// row asks is specifically about /Users and /etc.
		aliasOf string
	}{
		// Literal deny-list entries and path-arithmetic tricks: protected
		// everywhere, whatever the filesystem does with case.
		{dir: "/"},
		{dir: "//"},
		{dir: "/."},
		{dir: "/usr/.."},
		{dir: "~"},
		{dir: "~/"},
		{dir: "/Users"},
		{dir: "/etc"},
		{dir: "/Volumes"},
		{dir: "/Volumes/"},
		{dir: "/mnt"},
		{dir: "/media"},
		{dir: "/net"},
		{dir: "/srv"},

		// Case-folded spellings: the same directory only where names fold.
		{dir: "/USERS", aliasOf: "/Users"},
		{dir: "/users", aliasOf: "/Users"},
		{dir: "/ETC", aliasOf: "/etc"},
		{dir: "/Etc", aliasOf: "/etc"},
	}

	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		for _, c := range cases {
			t.Run(script+" "+c.dir, func(t *testing.T) {
				if c.aliasOf != "" && !sameDirOnDisk(c.dir, c.aliasOf) {
					t.Skipf("%s is not %s on this filesystem, so it is a genuinely different path and accepting it is correct",
						c.dir, c.aliasOf)
				}

				e := newEnv(t)
				r := e.run(t, script, "--data-dir", c.dir, "--dry-run")
				if r.code == 0 {
					t.Fatalf("%s accepted %q\n%s", script, c.dir, r.combined)
				}
			})
		}
	}
}

// The evasion table above reasons about /Users and /etc, which live on the root
// filesystem. This one exercises the same property against the fake machine's
// own home directory on whatever filesystem the test's temp dir lives on --
// which on a CI runner is frequently not the root one.
//
// It is also the case that matters most in practice: HOME is the protected path
// a mistyped --data-dir is most likely to land on.
func TestGuardCatchesCaseFoldedFakeHome(t *testing.T) {
	probeRoot := t.TempDir()
	if !fsFoldsCase(t, probeRoot) {
		t.Skip("temp filesystem is case-sensitive; a folded spelling names a different directory here")
	}

	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)

			folded := strings.ToUpper(e.home)
			if !sameDirOnDisk(e.home, folded) {
				t.Skipf("%s is not %s here", folded, e.home)
			}

			r := e.run(t, script, "--data-dir", folded, "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted %q, which IS the home directory\n%s", script, folded, r.combined)
			}
			if !r.contains("refusing") {
				t.Errorf("no guard message:\n%s", r.combined)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// BSD / GNU stat divergence
// ---------------------------------------------------------------------------

// GNU's -f is --file-system and takes no format argument, so `stat -f '%Lp'
// FILE` is parsed as "show filesystem status for '%Lp' and FILE": the first
// fails, the second prints a six-line status block, and the exit status is
// non-zero. A `stat -f ... || stat -c ...` chain therefore ran its fallback with
// that block already on stdout, and the caller captured 237 characters of
// filesystem statistics as a file mode. chmod rejected it, `|| true` swallowed
// the failure, and the rewritten profile kept mktemp's 0600.
//
// This test exercises that branch on a Mac, where BSD stat would otherwise hide
// it -- the reason it reached CI twice.
func TestProfilePermissionsUnderGNUStat(t *testing.T) {
	e := newEnv(t)
	if !e.withGNUStat(t) {
		t.Skip("GNU coreutils stat (gstat) not installed; the GNU branch is covered natively on Linux")
	}

	profile := e.profile()
	e.writeFile(t, profile, "# created by pipx\nexport PATH=\"$HOME/.local/bin:$PATH\"\n\n# Conduit\nexport PATH=\"$HOME/.local/bin:$PATH\"\n\nexport EDITOR=vim\n")
	if err := os.Chmod(profile, 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.combined)
	}

	info, err := os.Stat(profile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("profile mode = %o, want 644 (mktemp's 600 leaked through under GNU stat)", got)
	}

	// The surgery itself must still be correct under GNU.
	after := readFile(t, profile)
	if strings.Contains(after, "# Conduit") {
		t.Errorf("Conduit block survived:\n%s", after)
	}
	if !strings.Contains(after, "# created by pipx") {
		t.Errorf("pipx line was removed:\n%s", after)
	}
}

// The identity guard must work under GNU stat too: it is what stops a
// case-folded or symlinked spelling of a protected directory getting through.
func TestPathIdentityGuardUnderGNUStat(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			if !e.withGNUStat(t) {
				t.Skip("GNU coreutils stat (gstat) not installed")
			}

			// A symlink to the home directory: refused only if the identity
			// check resolves it, which is what the polluted-stdout chain broke.
			link := filepath.Join(e.home, "link-to-home")
			if err := os.Symlink(e.home, link); err != nil {
				t.Skipf("cannot create symlink: %v", err)
			}

			r := e.run(t, script, "--data-dir", link, "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted a symlink to the home directory under GNU stat\n%s", script, r.combined)
			}

			// And a legitimate data directory must still be accepted, so the
			// guard is not simply refusing everything.
			if script == "uninstall.sh" {
				e2 := newEnv(t)
				e2.withGNUStat(t)
				e2.installStubOnPath(t, v2Stub())
				if r := e2.run(t, script, "--data-dir", e2.dataDir, "--dry-run"); r.code != 0 {
					t.Errorf("a real data directory was refused under GNU stat\n%s", r.combined)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// N4 -- relative paths
// ---------------------------------------------------------------------------

// A relative --data-dir resolves against whatever directory the script happened
// to be run from. The A1 canonicalisation rewrite silently made such a path
// absolute, which turned an obvious mistake into a plausible-looking one.
func TestGuardRejectsRelativeDataDir(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		for _, dir := range []string{"relative/path", "./conduit", "../conduit", "conduit"} {
			t.Run(script+" "+dir, func(t *testing.T) {
				e := newEnv(t)
				r := e.run(t, script, "--data-dir", dir, "--dry-run")
				if r.code == 0 {
					t.Fatalf("%s accepted relative path %q\n%s", script, dir, r.combined)
				}
				if !r.contains("absolute") {
					t.Errorf("error does not explain the rule:\n%s", r.combined)
				}
			})
		}
	}
}

// The default data directory is not user-supplied and must not be caught by the
// absolute-path rule.
func TestGuardAllowsDefaultDataDir(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			if script == "uninstall.sh" {
				e.installStubOnPath(t, v2Stub())
			}
			r := e.run(t, script, "--dry-run")
			if r.code != 0 {
				t.Fatalf("%s rejected its own default data directory\n%s", script, r.combined)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// N5 -- glob expansion in canonicalisation
// ---------------------------------------------------------------------------

// canonicalize_path split the path with unquoted word splitting, which also
// performs pathname expansion. A directory legitimately containing * or ? was
// rewritten into whatever happened to match in the working directory, so the
// guard's input depended on where the script was run from.
func TestCanonicalizeDoesNotGlob(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)

			// Decoys that a glob would expand to.
			for _, decoy := range []string{"aaa", "bbb", "ccc"} {
				if err := os.MkdirAll(filepath.Join(e.home, decoy), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			}

			globby := filepath.Join(e.home, "*")
			r := e.run(t, script, "--data-dir", globby, "--dry-run")

			// Whatever it decides, it must have reasoned about the literal path
			// and must never report one of the decoys.
			for _, decoy := range []string{"/aaa", "/bbb", "/ccc"} {
				if strings.Contains(r.combined, e.home+decoy) {
					t.Fatalf("the '*' was glob-expanded to %s:\n%s", decoy, r.combined)
				}
			}
			if r.code == 0 && !strings.Contains(r.combined, "*") {
				t.Errorf("the literal path never appears in the output:\n%s", r.combined)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// N7 -- parent of a non-standard home
// ---------------------------------------------------------------------------

// /Users and /home cover the usual layouts. A home at /export/people/<user>
// has a parent that neither covers, and deleting it takes out every colleague
// on that machine.
func TestGuardProtectsParentOfNonStandardHome(t *testing.T) {
	for _, script := range []string{"uninstall.sh", "remove-v1.sh"} {
		t.Run(script, func(t *testing.T) {
			e := newEnv(t)
			// e.home is <tmp>/home, so its parent is <tmp> -- a stand-in for
			// the /export/people of a real site layout.
			parent := filepath.Dir(e.home)

			r := e.run(t, script, "--data-dir", parent, "--dry-run")
			if r.code == 0 {
				t.Fatalf("%s accepted the parent of the home directory (%s)\n%s", script, parent, r.combined)
			}
			if !r.contains("refusing") {
				t.Errorf("no guard message:\n%s", r.combined)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// N1 -- could-not-execute vs ran-and-failed
// ---------------------------------------------------------------------------

// 126 (not executable) and 127 (not found) mean the binary never ran, so it
// expressed no opinion and there is nothing to respect. This is precisely the
// case the manual path exists for: a wrong-architecture download or a missing
// shared library would otherwise leave the user with a broken binary they
// cannot remove using the tool whose whole job is removing it.
func TestCannotExecuteFallsBackLoudly(t *testing.T) {
	for _, code := range []int{126, 127} {
		t.Run(string(rune('0'+code/100))+"xx", func(t *testing.T) {
			e := newEnv(t)
			binary := e.installStubOnPath(t, stubOptions{
				SupportsDataDir: true, SupportsPrefix: true, ExitCode: code,
			})

			r := e.run(t, "uninstall.sh")

			// It must fall back, not block.
			if !r.contains("could not be executed") {
				t.Errorf("exit %d was not reported as a failure to execute:\n%s", code, r.combined)
			}
			if r.contains("Refusing to fall back") {
				t.Errorf("exit %d blocked the uninstall instead of falling back:\n%s", code, r.combined)
			}
			// And the fallback must actually run.
			if !r.contains("Shell profiles") && !r.contains("MCP client entries") {
				t.Errorf("the manual path did not run:\n%s", r.combined)
			}
			// The broken binary is what the user is trying to get rid of.
			if exists(binary) {
				t.Error("the unexecutable binary was left in place")
			}
		})
	}
}

// A binary that RAN and returned a plain failure made a decision about this
// machine, and the less careful manual path is not entitled to overrule it.
func TestRanAndFailedStillBlocks(t *testing.T) {
	e := newEnv(t)
	e.installStubOnPath(t, stubOptions{
		SupportsDataDir: true, SupportsPrefix: true, ExitCode: 1,
	})

	r := e.run(t, "uninstall.sh")
	if r.code == 0 {
		t.Fatalf("a failed delegation reported success\n%s", r.combined)
	}
	if !r.contains("Refusing to fall back") {
		t.Errorf("did not block:\n%s", r.combined)
	}
}

// --manual is the documented escape hatch from that block.
func TestManualFlagSkipsDelegation(t *testing.T) {
	e := newEnv(t)
	binary := e.installStubOnPath(t, stubOptions{
		SupportsDataDir: true, SupportsPrefix: true, ExitCode: 1,
	})

	r := e.run(t, "uninstall.sh", "--manual")
	if r.code != 0 {
		t.Fatalf("--manual exited %d\n%s", r.code, r.combined)
	}
	if len(e.realInvocations(t)) != 0 {
		t.Errorf("--manual still ran the binary: %v", e.realInvocations(t))
	}
	if !r.contains("Skipping delegation") {
		t.Errorf("--manual was not announced:\n%s", r.combined)
	}
	if exists(binary) {
		t.Error("--manual did not remove the binary")
	}
	// It must say it is the less careful path.
	if !r.contains("less thorough") {
		t.Errorf("--manual does not warn about its limitations:\n%s", r.combined)
	}
}

// ---------------------------------------------------------------------------
// N6 -- the marker is actually written
// ---------------------------------------------------------------------------

// Nothing ever wrote the "# Conduit" marker, so the uninstaller's profile
// cleanup searched for a signature that by construction did not exist -- dead
// code, while the help promised to remove PATH entries.
func TestInstallWritesThePathMarker(t *testing.T) {
	e := newEnv(t)

	zshrc := e.profile()
	e.writeFile(t, zshrc, "export EDITOR=vim\n")

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runInstall(t, prefix, "--from-source", "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	after := readFile(t, zshrc)
	if !strings.Contains(after, "# Conduit") {
		t.Fatalf("install.sh did not write the marker:\n%s", after)
	}
	if !strings.Contains(after, prefix) {
		t.Errorf("install.sh did not write the PATH line:\n%s", after)
	}
	if !strings.Contains(after, "export EDITOR=vim") {
		t.Errorf("install.sh clobbered the existing profile:\n%s", after)
	}
}

// What install.sh writes must be exactly what uninstall.sh removes. If these
// ever drift, the cleanup goes back to being dead code.
func TestInstallAndUninstallAgreeOnTheMarker(t *testing.T) {
	e := newEnv(t)

	zshrc := e.profile()
	e.writeFile(t, zshrc, "# created by pipx\nexport PATH=\"$HOME/.local/bin:$PATH\"\nexport EDITOR=vim\n")

	prefix := filepath.Join(e.home, "opt", "bin")
	if r := e.runInstall(t, prefix, "--from-source", "--no-setup"); r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}
	if !strings.Contains(readFile(t, zshrc), "# Conduit") {
		t.Fatal("nothing to remove: install wrote no marker")
	}

	// No binary on PATH, so the manual profile surgery runs.
	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("uninstall exited %d\n%s", r.code, r.combined)
	}

	after := readFile(t, zshrc)
	if strings.Contains(after, "# Conduit") {
		t.Errorf("uninstall did not remove the block install.sh wrote:\n%s", after)
	}
	if strings.Contains(after, prefix) {
		t.Errorf("the PATH line survived:\n%s", after)
	}
	// The user's own lines are still theirs.
	for _, keep := range []string{"# created by pipx", `export PATH="$HOME/.local/bin:$PATH"`, "export EDITOR=vim"} {
		if !strings.Contains(after, keep) {
			t.Errorf("removed a line Conduit never wrote: %q\n%s", keep, after)
		}
	}
}

// Re-running the installer must not stack up duplicate PATH blocks.
func TestInstallPathBlockIsIdempotent(t *testing.T) {
	e := newEnv(t)

	zshrc := e.profile()
	e.writeFile(t, zshrc, "export EDITOR=vim\n")

	prefix := filepath.Join(e.home, "opt", "bin")
	for i := 0; i < 3; i++ {
		if r := e.runInstall(t, prefix, "--from-source", "--no-setup"); r.code != 0 {
			t.Fatalf("install run %d exited %d\n%s", i, r.code, r.combined)
		}
	}

	if n := strings.Count(readFile(t, zshrc), "# Conduit"); n != 1 {
		t.Errorf("marker appears %d times, want 1", n)
	}
}
