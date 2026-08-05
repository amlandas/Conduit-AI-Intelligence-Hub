package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// #85 -- the PATH block has to land in a file the user's shell actually reads
// ---------------------------------------------------------------------------

// install.sh sent every bash user to ~/.bashrc. On Linux that is right; on
// macOS it is not read by anything the user opens.
//
// Terminal.app and iTerm start bash as a LOGIN shell, and a login bash reads
// ~/.bash_profile (then ~/.bash_login, then ~/.profile) and never ~/.bashrc.
// So the installer printed "PATH updated in ~/.bashrc", the user opened a new
// terminal exactly as instructed, and `conduit` was still not found. A false
// success is worse than a refusal: it sends the user looking for the fault
// somewhere else.
//
// The assertion is behavioural rather than a filename comparison -- it starts
// a real bash the way this platform's terminal does and asks what PATH it got.
// A test that checked for ".bash_profile" would only be a copy of the fix.
func TestInstalledPathIsVisibleToAFreshShell(t *testing.T) {
	e := newEnv(t)

	prefix := filepath.Join(e.home, "opt", "bin")
	if r := e.runInstall(t, prefix, "--from-source", "--no-setup"); r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	got := e.startupPATH(t)
	if !pathContains(got, prefix) {
		t.Fatalf("a new terminal does not see %s on PATH.\nPATH was: %s\nprofiles present: %v",
			prefix, got, profilesPresent(t, e.home))
	}
}

// The installer must also say where it wrote, and that has to be the file it
// really wrote to -- the misdirection in #85 was as much the message as the
// write.
func TestInstallReportsTheProfileItWrote(t *testing.T) {
	e := newEnv(t)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runInstall(t, prefix, "--from-source", "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	written := e.profile()
	if !strings.Contains(readFile(t, written), "# Conduit") {
		t.Fatalf("no marker in %s\nprofiles present: %v", written, profilesPresent(t, e.home))
	}
	if !r.contains(written) {
		t.Errorf("install.sh did not name %s in its output:\n%s", written, r.combined)
	}
}

// profilesPresent lists which rc files exist, to make a failure diagnosable
// without a second run.
func profilesPresent(t *testing.T, home string) []string {
	t.Helper()
	var found []string
	for _, name := range []string{".bashrc", ".bash_profile", ".bash_login", ".profile", ".zshrc"} {
		if exists(filepath.Join(home, name)) {
			found = append(found, name)
		}
	}
	return found
}

// The idempotency check grepped for "# Conduit" as a plain substring, anywhere
// in the file. Any line that happens to contain those characters -- a trailing
// comment on an unrelated alias is the obvious one -- convinced the installer
// that its PATH block was already there, so it wrote nothing and reported
// "PATH entry already present". The user's PATH was never updated.
//
// The uninstaller has always matched the marker anchored to the start of a
// line. The installer must use the same rule, or the pair disagrees about what
// a marker is.
func TestMarkerInAnUnrelatedCommentDoesNotSuppressTheWrite(t *testing.T) {
	decoys := []string{
		"alias k=kubectl # Conduit helper\n",
		"echo 'see the # Conduit docs for details'\n",
		"export EDITOR=vim  # Conduit was here once\n",
	}

	for _, decoy := range decoys {
		t.Run(strings.TrimSpace(decoy), func(t *testing.T) {
			e := newEnv(t)
			e.writeFile(t, e.profile(), decoy)

			prefix := filepath.Join(e.home, "opt", "bin")
			r := e.runInstall(t, prefix, "--from-source", "--no-setup")
			if r.code != 0 {
				t.Fatalf("install exited %d\n%s", r.code, r.combined)
			}

			after := readFile(t, e.profile())
			// Deliberately not matched against a particular quoting style.
			// The subject here is whether the block was written at all, and
			// pinning the exact characters made this test fail when the write
			// was correctly changed from double to single quotes -- the fix for
			// a prefix containing $( ) executing at every login. What the line
			// has to do is put the prefix on PATH, which startupPATH below asks
			// a real shell about.
			if !strings.Contains(after, "export PATH=") || !strings.Contains(after, prefix) {
				t.Fatalf("the PATH line was never written; the decoy line suppressed it:\n%s\n--- installer said ---\n%s",
					after, r.combined)
			}
			// The user's own line is theirs and stays exactly as it was.
			if !strings.Contains(after, strings.TrimRight(decoy, "\n")) {
				t.Errorf("the installer altered a line it did not write:\n%s", after)
			}

			got := e.startupPATH(t)
			if !pathContains(got, prefix) {
				t.Errorf("a new shell still does not see %s\nPATH: %s", prefix, got)
			}
		})
	}
}

// The converse: a real marker, written by a previous run, must still suppress a
// second block. Anchoring the match must not cost idempotency.
func TestAnchoredMarkerStillSuppressesADuplicate(t *testing.T) {
	e := newEnv(t)

	prefix := filepath.Join(e.home, "opt", "bin")
	for i := 0; i < 3; i++ {
		if r := e.runInstall(t, prefix, "--from-source", "--no-setup"); r.code != 0 {
			t.Fatalf("install run %d exited %d\n%s", i, r.code, r.combined)
		}
	}

	if n := strings.Count(readFile(t, e.profile()), "# Conduit"); n != 1 {
		t.Errorf("marker appears %d times, want 1", n)
	}
}

// An indented marker is still a marker: uninstall.sh matches
// '^[[:space:]]*# Conduit', so the installer must treat that as "already
// present" or the two write and remove different sets of blocks.
func TestIndentedMarkerCountsAsPresent(t *testing.T) {
	e := newEnv(t)
	e.writeFile(t, e.profile(), "  # Conduit\n  export PATH=\"/somewhere:$PATH\"\n")

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runInstall(t, prefix, "--from-source", "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	if n := strings.Count(readFile(t, e.profile()), "# Conduit"); n != 1 {
		t.Errorf("an indented marker was not recognised; marker count = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// #86 -- a symlinked profile is a real pattern, not an edge case
// ---------------------------------------------------------------------------

// Keeping dotfiles in a git repo and symlinking them into $HOME is how a large
// share of developers manage their shell config. ~/.zshrc is then a symlink to
// ~/dotfiles/zshrc.
//
// install.sh appends with `>>`, which follows the link and writes the repo
// file. uninstall.sh rewrote with `mv -f tmp file`, which REPLACES the link
// with a regular file. So the two halves acted on different files: the block
// stayed in the dotfiles repo, the link was destroyed, and the user's profile
// silently stopped tracking their own repo.
//
// The rewrite must resolve the link and edit the file it points at.
func TestUninstallDoesNotReplaceASymlinkedProfile(t *testing.T) {
	e := newEnv(t)

	// The dotfiles repo and the link into $HOME, exactly as `stow` would leave
	// them.
	repo := filepath.Join(e.home, "dotfiles")
	real := filepath.Join(repo, "bashrc")
	e.writeFile(t, real, "# from my dotfiles repo\nexport EDITOR=vim\n")

	link := e.profile()
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	prefix := filepath.Join(e.home, "opt", "bin")
	if r := e.runInstall(t, prefix, "--from-source", "--no-setup"); r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}
	// Appending through the link is correct and already worked.
	if !strings.Contains(readFile(t, real), "# Conduit") {
		t.Fatalf("install did not write through the symlink:\n%s", readFile(t, real))
	}

	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("uninstall exited %d\n%s", r.code, r.combined)
	}

	// The link must still be a link.
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat %s: %v", link, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is no longer a symlink; the uninstaller replaced it with a regular file", link)
	}
	target, err := os.Readlink(link)
	if err != nil || target != real {
		t.Fatalf("symlink now points at %q (err %v), want %q", target, err, real)
	}

	// And the edit must have reached the file the link points at.
	after := readFile(t, real)
	if strings.Contains(after, "# Conduit") {
		t.Errorf("the Conduit block survived in the real file:\n%s", after)
	}
	if !strings.Contains(after, "# from my dotfiles repo") {
		t.Errorf("the user's own content was lost:\n%s", after)
	}
}

// The backup must be taken of the file that is actually rewritten. A backup
// sitting next to the link, named after the link, is not where anyone whose
// dotfiles repo just changed would look -- and on a repo checkout it is also an
// untracked file appearing in `git status`.
func TestUninstallBacksUpTheResolvedProfile(t *testing.T) {
	e := newEnv(t)

	repo := filepath.Join(e.home, "dotfiles")
	real := filepath.Join(repo, "bashrc")
	e.writeFile(t, real, "# from my dotfiles repo\n\n# Conduit\nexport PATH=\"/x:$PATH\"\n")

	link := e.profile()
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("uninstall exited %d\n%s", r.code, r.combined)
	}

	resolvedBackup := real + ".conduit-uninstall.bak"
	if !exists(resolvedBackup) {
		t.Errorf("no backup beside the resolved file at %s", resolvedBackup)
	}
	if !strings.Contains(readFile(t, resolvedBackup), "# Conduit") {
		t.Errorf("the backup does not hold the pre-edit contents:\n%s", readFile(t, resolvedBackup))
	}
}

// A symlink chain -- ~/.bashrc -> ~/dotfiles/bashrc -> ~/dotfiles/shared/rc --
// must resolve all the way down. Resolving one hop would rewrite the middle
// link and break the rest of the chain.
func TestUninstallResolvesASymlinkChain(t *testing.T) {
	e := newEnv(t)

	final := filepath.Join(e.home, "dotfiles", "shared", "rc")
	e.writeFile(t, final, "# shared\n\n# Conduit\nexport PATH=\"/x:$PATH\"\n")

	middle := filepath.Join(e.home, "dotfiles", "bashrc")
	if err := os.Symlink(final, middle); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	link := e.profile()
	if err := os.Symlink(middle, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	if r := e.run(t, "uninstall.sh"); r.code != 0 {
		t.Fatalf("uninstall exited %d\n%s", r.code, r.combined)
	}

	for _, hop := range []string{link, middle} {
		info, err := os.Lstat(hop)
		if err != nil {
			t.Fatalf("lstat %s: %v", hop, err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("%s is no longer a symlink", hop)
		}
	}
	if strings.Contains(readFile(t, final), "# Conduit") {
		t.Errorf("the block survived at the end of the chain:\n%s", readFile(t, final))
	}
}
