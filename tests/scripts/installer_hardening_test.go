package scripts

// These tests cover install.sh's input handling, its failure diagnosis and the
// two places its output is consumed by something other than a human: the shell
// profile it writes and the MCP configuration `conduit setup` leaves behind.
//
// Every one of them is hermetic in the same way as the rest of this package --
// HOME is a temporary directory and PATH holds only the fake machine's bin --
// with one addition: the tests that install a prefix use a prefix INSIDE the
// fake home, so a bug that ignored --prefix would write into the sandbox rather
// than into the developer's real ~/.local/bin.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Piped invocation
// ---------------------------------------------------------------------------

// runPiped feeds a script to bash on STDIN, the way `curl ... | bash` does.
//
// The distinction matters and is not simulated by running the file: bash sets
// no BASH_SOURCE at all for a script read from standard input, which is a
// different environment from a script executed by path. That difference is the
// whole subject of the two tests below.
func (e *env) runPiped(t *testing.T, script string, args ...string) result {
	t.Helper()

	body, err := os.ReadFile(scriptPath(t, script))
	if err != nil {
		t.Fatalf("read %s: %v", script, err)
	}

	cmd := exec.Command("bash", append([]string{"-s", "--"}, args...)...)
	cmd.Env = []string{
		"HOME=" + e.home,
		"PATH=" + e.binDir + ":/usr/bin:/bin",
		"SHELL=/bin/bash",
		"TERM=dumb",
	}
	cmd.Dir = e.home
	cmd.Stdin = strings.NewReader(string(body))

	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	code := 0
	if runErr := cmd.Run(); runErr != nil {
		var ee *exec.ExitError
		if asExitError(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run piped %s: %v", script, runErr)
		}
	}

	return result{
		stdout:   out.String(),
		stderr:   errOut.String(),
		combined: out.String() + errOut.String(),
		code:     code,
	}
}

// --from-source piped from curl has no repository to build.
//
// It used to die with "BASH_SOURCE[0]: unbound variable" -- a message about a
// shell array, produced by `set -u` on bash 3.2, which is still /bin/bash on
// macOS. The user's actual problem is that there is no checkout, and the fix is
// to clone one; neither of those was anywhere in the output.
func TestPipedFromSourceExplainsTheMissingCheckout(t *testing.T) {
	e := newEnv(t)
	e.installToolStubs(t)

	r := e.runPiped(t, "install.sh", "--from-source", "--no-setup")

	if r.code == 0 {
		t.Fatalf("a piped --from-source reported success with no checkout to build\n%s", r.combined)
	}
	if r.contains("unbound variable") {
		t.Errorf("the failure is still reported as a shell variable error:\n%s", r.combined)
	}
	if !r.contains("git clone") {
		t.Errorf("the error does not tell the user how to get a checkout:\n%s", r.combined)
	}
	// The instruction it used to print names a file that does not exist on a
	// machine that only ever piped this script.
	if strings.Contains(r.combined, "./scripts/install.sh --from-source") &&
		!strings.Contains(r.combined, "git clone") {
		t.Errorf("the error points at ./scripts/install.sh, which is not on this machine:\n%s", r.combined)
	}
}

// The release path is the one that is meant to work piped, and it must.
func TestPipedReleaseInstallWorks(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "opt", "bin")

	// runPiped replaces the environment, so the stub server is passed the same
	// way the harness does it elsewhere: through the process environment.
	body, err := os.ReadFile(scriptPath(t, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	cmd := exec.Command("bash", "-s", "--", "--prefix", prefix, "--no-setup")
	cmd.Env = append([]string{
		"HOME=" + e.home,
		"PATH=" + e.binDir + ":/usr/bin:/bin",
		"SHELL=/bin/bash",
		"TERM=dumb",
	}, releaseEnv(srv)...)
	cmd.Dir = e.home
	cmd.Stdin = strings.NewReader(string(body))

	out, runErr := cmd.CombinedOutput()
	if runErr != nil {
		t.Fatalf("a piped release install failed: %v\n%s", runErr, out)
	}
	if !exists(filepath.Join(prefix, "conduit")) {
		t.Fatalf("a piped release install produced no binary\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Diagnosing a failed "latest" lookup
// ---------------------------------------------------------------------------

// newAPIServer stands in for the releases API with a fixed status and body.
func newAPIServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// GitHub rate-limits unauthenticated API calls to 60 an hour per IP and answers
// 403 once that is spent. It is one of the likeliest things a real user hits.
//
// `die` inside $( ) exits only the subshell, so the old code printed the
// correct rate-limit message from inside resolve_release_tag and then carried
// on to print "no Conduit 2.0 release has been published yet" -- two
// contradictory explanations, the second of which is flatly false.
func TestRateLimitedAPIIsNotReportedAsNoRelease(t *testing.T) {
	e := newEnv(t)
	srv := newAPIServer(t, http.StatusForbidden,
		`{"message":"API rate limit exceeded for 203.0.113.7.","documentation_url":"https://docs.github.com/rest"}`)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, []string{"CONDUIT_RELEASE_API_URL=" + srv.URL},
		"", "install.sh", "--prefix", prefix, "--no-setup")

	if r.code == 0 {
		t.Fatalf("a rate-limited lookup reported success\n%s", r.combined)
	}
	if r.contains("no Conduit 2.0 release has been published") {
		t.Errorf("a 403 was reported as there being no release:\n%s", r.combined)
	}
	if !r.contains("rate") {
		t.Errorf("the error does not identify rate limiting:\n%s", r.combined)
	}
	// The remedy that actually works: --version skips the API entirely.
	if !r.contains("--version") {
		t.Errorf("the error does not name --version as the way around it:\n%s", r.combined)
	}
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("something was installed despite the lookup failing")
	}
}

// A captive portal, a corporate proxy or an API error delivered with a 200 all
// produce a body that is not a releases array. None of them is evidence that no
// release exists, and the script must not say so.
func TestUnparseable200IsNotReportedAsNoRelease(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"captive portal", `<!DOCTYPE html><html><body>Sign in to use this network</body></html>`},
		{"api error object", `{"message":"Bad credentials","status":"401"}`},
		{"empty body", ``},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			srv := newAPIServer(t, http.StatusOK, tc.body)

			prefix := filepath.Join(e.home, "opt", "bin")
			r := e.runWithEnv(t, []string{"CONDUIT_RELEASE_API_URL=" + srv.URL},
				"", "install.sh", "--prefix", prefix, "--no-setup")

			if r.code == 0 {
				t.Fatalf("an unparseable response reported success\n%s", r.combined)
			}
			if r.contains("no Conduit 2.0 release has been published") {
				t.Errorf("a %s was reported as there being no release:\n%s", tc.name, r.combined)
			}
			if exists(filepath.Join(prefix, "conduit")) {
				t.Error("something was installed")
			}
		})
	}
}

// A genuinely empty list still has to produce the message that names it, or the
// distinction drawn above buys nothing.
func TestEmptyReleaseListStillSaysNoReleaseYet(t *testing.T) {
	e := newEnv(t)
	srv := newAPIServer(t, http.StatusOK, `[]`)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, []string{"CONDUIT_RELEASE_API_URL=" + srv.URL},
		"", "install.sh", "--prefix", prefix, "--no-setup")

	if r.code == 0 {
		t.Fatalf("an empty release list reported success\n%s", r.combined)
	}
	if !r.contains("no Conduit 2.0 release has been published") {
		t.Errorf("a real empty list did not produce the no-release message:\n%s", r.combined)
	}
}

// ---------------------------------------------------------------------------
// --prefix
// ---------------------------------------------------------------------------

// The PATH block is written into a file the shell EVALUATES. A prefix carrying
// command substitution used to be interpolated into a double-quoted line, so it
// became code that ran in every login shell from then on.
//
// Two properties are asserted: the prefix is refused, and -- for the case where
// a future edit relaxes the refusal -- nothing the prefix contained was ever
// executed.
func TestMetacharacterPrefixIsRefused(t *testing.T) {
	canary := filepath.Join(t.TempDir(), "executed")

	prefixes := []string{
		fmt.Sprintf("/tmp/$(touch %s)", canary),
		fmt.Sprintf("/tmp/`touch %s`", canary),
		"/tmp/a;b",
		"/tmp/a|b",
		"/tmp/a\"b",
		"/tmp/a'b",
		"/tmp/a\\b",
		"/tmp/a$b",
		"/tmp/a\nb",
	}

	for _, prefix := range prefixes {
		t.Run(strings.ReplaceAll(prefix, "\n", "\\n"), func(t *testing.T) {
			e := newEnv(t)

			r := e.run(t, "install.sh", "--prefix", prefix, "--no-setup")
			if r.code == 0 {
				t.Fatalf("prefix %q was accepted\n%s", prefix, r.combined)
			}
			if exists(canary) {
				t.Fatalf("prefix %q was EXECUTED, not just accepted", prefix)
			}

			// And nothing may have been written into any shell profile.
			if exists(e.profile()) {
				body := readFile(t, e.profile())
				if strings.Contains(body, "Conduit") {
					t.Errorf("a refused prefix still reached %s:\n%s", e.profile(), body)
				}
			}
		})
	}
}

// A relative prefix produces a PATH entry that names a different directory in
// every shell that reads it, which is the same defect uninstall.sh already
// refuses for --data-dir.
func TestRelativePrefixIsRefused(t *testing.T) {
	for _, prefix := range []string{"bin", "./bin", "../bin", "some/where"} {
		t.Run(prefix, func(t *testing.T) {
			e := newEnv(t)

			r := e.run(t, "install.sh", "--prefix", prefix, "--no-setup")
			if r.code == 0 {
				t.Fatalf("relative prefix %q was accepted\n%s", prefix, r.combined)
			}
			if !r.contains("absolute") {
				t.Errorf("the refusal does not explain itself:\n%s", r.combined)
			}
			if exists(filepath.Join(e.home, prefix, "conduit")) {
				t.Error("something was installed at the relative path")
			}
		})
	}
}

// CONDUIT_PREFIX is held to the same rules as the flag: it ends up in the same
// place, in the same line, in the same file.
func TestEnvPrefixIsHeldToTheSameRules(t *testing.T) {
	e := newEnv(t)

	r := e.runWithEnv(t, []string{"CONDUIT_PREFIX=relative/bin"},
		"", "install.sh", "--no-setup")
	if r.code == 0 {
		t.Fatalf("a relative CONDUIT_PREFIX was accepted\n%s", r.combined)
	}
	if !r.contains("CONDUIT_PREFIX") {
		t.Errorf("the refusal does not name the variable that supplied the value:\n%s", r.combined)
	}
}

// The documented environment variable has to actually work, in both scripts:
// install.sh puts the binary there, and uninstall.sh has to find it.
func TestConduitPrefixEnvIsHonouredByBothScripts(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "custom", "bin")

	r := e.runWithEnv(t, append(releaseEnv(srv), "CONDUIT_PREFIX="+prefix),
		"", "install.sh", "--no-setup")
	if r.code != 0 {
		t.Fatalf("install with CONDUIT_PREFIX exited %d\n%s", r.code, r.combined)
	}

	installed := filepath.Join(prefix, "conduit")
	if !exists(installed) {
		t.Fatalf("CONDUIT_PREFIX was ignored; nothing at %s\n%s", installed, r.combined)
	}
	if exists(filepath.Join(e.home, ".local", "bin", "conduit")) {
		t.Error("CONDUIT_PREFIX was set but the default location was used as well")
	}

	// And the uninstaller must know to look there, or the install it just made
	// is one the matching uninstall cannot remove.
	u := e.runWithEnv(t, []string{"CONDUIT_PREFIX=" + prefix},
		"", "uninstall.sh", "--force")
	if exists(installed) {
		t.Errorf("uninstall.sh left the CONDUIT_PREFIX install in place\n%s", u.combined)
	}
}

// The prefix is checked before the network, not after the download.
func TestUnwritablePrefixFailsBeforeDownloading(t *testing.T) {
	e := newEnv(t)

	// A server that records whether it was asked for anything at all.
	var asked bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = true
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	// A directory this user cannot write into.
	locked := filepath.Join(e.home, "locked")
	if err := os.MkdirAll(locked, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	prefix := filepath.Join(locked, "bin")
	r := e.runWithEnv(t, []string{
		"CONDUIT_RELEASE_API_URL=" + srv.URL,
		"CONDUIT_RELEASE_BASE_URL=" + srv.URL + "/download",
	}, "", "install.sh", "--prefix", prefix, "--no-setup")

	if r.code == 0 {
		t.Fatalf("an unwritable prefix reported success\n%s", r.combined)
	}
	if asked {
		t.Errorf("the network was used before the destination was checked:\n%s", r.combined)
	}
}

// A DIRECTORY where the binary belongs is refused.
//
// `mv -f staged dest` does not fail when dest is a directory: it moves the file
// INSIDE it. The install therefore reported "OK installed <dest>" and exited 0
// with the binary at <dest>/conduit.new.<pid>, a name nothing will ever run.
func TestDirectoryAtTheDestinationIsRefused(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "opt", "bin")
	if err := os.MkdirAll(filepath.Join(prefix, "conduit"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")

	if r.code == 0 {
		t.Fatalf("a directory at the destination reported a successful install\n%s", r.combined)
	}
	if r.contains("OK installed") {
		t.Errorf("the install claimed success over a directory destination:\n%s", r.combined)
	}

	// Nothing may have been stashed inside that directory either.
	entries, err := os.ReadDir(filepath.Join(prefix, "conduit"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("the binary was written inside the directory: %v", names)
	}
}

// ---------------------------------------------------------------------------
// --version
// ---------------------------------------------------------------------------

// The tag becomes a path segment in the download URL, and every message the
// script prints calls it the version being installed. Both break if it is not
// shaped like a tag.
func TestTagShapeIsValidated(t *testing.T) {
	bad := []string{
		"../download/v2.0.0-beta.3",
		"v2.0.0/../../etc",
		"v1.0.0",
		"latest-ish",
		"v2.0.0 beta",
		"v2.0.0;id",
		"v2.0.0/../v0.1.10",
		"v2",
	}

	for _, tag := range bad {
		t.Run(tag, func(t *testing.T) {
			e := newEnv(t)
			srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

			prefix := filepath.Join(e.home, "opt", "bin")
			r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh",
				"--prefix", prefix, "--no-setup", "--version", tag)

			if r.code == 0 {
				t.Fatalf("tag %q was accepted\n%s", tag, r.combined)
			}
			if exists(filepath.Join(prefix, "conduit")) {
				t.Errorf("tag %q installed something", tag)
			}

			// Exiting non-zero is not enough, and asserting only that is how
			// this test would pass against the bug it exists for. The old code
			// DID fail on these -- but at the download, having built a URL out
			// of the tag and then reported "release <tag> has no SHA256SUMS".
			// That names the release as broken when the tag was never a tag,
			// and for '../download/...' the URL it fetched was not the one the
			// message quoted. The refusal has to come from the tag itself.
			if r.contains("has no SHA256SUMS") {
				t.Errorf("tag %q was rejected as a broken release rather than as a bad tag:\n%s",
					tag, r.combined)
			}
			if !r.contains("--version") {
				t.Errorf("the error for %q does not point at the flag that supplied it:\n%s",
					tag, r.combined)
			}
		})
	}
}

// And a real tag must still be accepted, or the check above is just a way of
// breaking --version.
func TestValidTagsAreStillAccepted(t *testing.T) {
	for _, tag := range []string{"v2.0.0-beta.1", "v2.0.0", "v2.1.0-rc.2"} {
		t.Run(tag, func(t *testing.T) {
			e := newEnv(t)
			srv := newReleaseServer(t, newRelease(t, tag, fakeBinary(tag)))

			prefix := filepath.Join(e.home, "opt", "bin")
			r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh",
				"--prefix", prefix, "--no-setup", "--version", tag)

			if r.code != 0 {
				t.Fatalf("valid tag %q was refused\n%s", tag, r.combined)
			}
			if !exists(filepath.Join(prefix, "conduit")) {
				t.Errorf("valid tag %q installed nothing", tag)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// --client
// ---------------------------------------------------------------------------

// A mistyped client used to produce a complete install, a cheerful "Done." and
// no configured client anywhere.
func TestUnknownClientFailsLoudly(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh",
		"--prefix", prefix, "--client", "cursr")

	if r.code == 0 {
		t.Fatalf("an unknown client exited 0\n%s", r.combined)
	}
	if r.contains("Done.") {
		t.Errorf("an unknown client still printed the success banner:\n%s", r.combined)
	}
	if !r.contains("cursr") {
		t.Errorf("the error does not quote the name that was rejected:\n%s", r.combined)
	}
	// It has to name the alternatives, or the user's next guess is another typo.
	for _, known := range []string{"claude-code", "cursor", "vscode"} {
		if !r.contains(known) {
			t.Errorf("the error does not list %q as a supported client:\n%s", known, r.combined)
		}
	}
	// And it must fail BEFORE installing: an install that cannot be configured
	// should not leave a half-configured machine behind.
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("the binary was installed despite the client being unusable")
	}
}

// The supported list in install.sh is a copy of MCPClients() in
// internal/setup/mcpclient.go. Copies drift; this is what notices.
func TestSupportedClientsMatchTheBinary(t *testing.T) {
	body, err := os.ReadFile(scriptPath(t, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}

	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "setup", "mcpclient.go"))
	if err != nil {
		t.Fatalf("read mcpclient.go: %v", err)
	}

	for _, id := range []string{"claude-code", "cursor", "vscode"} {
		if !strings.Contains(string(src), `"`+id+`"`) {
			t.Errorf("mcpclient.go no longer defines %q; update SUPPORTED_CLIENTS in install.sh", id)
		}
		if !strings.Contains(string(body), id) {
			t.Errorf("install.sh does not accept %q, which the binary supports", id)
		}
	}
}

// ---------------------------------------------------------------------------
// Temporary state
// ---------------------------------------------------------------------------

// bash keeps ONE handler per signal, so a second `trap ... EXIT` replaces the
// first rather than adding to it. install.sh installed a second one for its
// staging file, which silently displaced the handler that removed the download
// directory -- and the run that reinstated it displaced the staging cleanup in
// turn. Neither leak showed on a path that succeeded.
//
// Asserted structurally because that is where the defect lives: the shape of
// the script, not the outcome of any one run.
func TestInstallHasExactlyOneExitTrap(t *testing.T) {
	body, err := os.ReadFile(scriptPath(t, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}

	var traps []string
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "trap ") && strings.Contains(trimmed, "EXIT") {
			traps = append(traps, trimmed)
		}
	}

	if len(traps) != 1 {
		t.Errorf("install.sh installs %d EXIT traps; each one REPLACES the last, "+
			"so all temporary state must be owned by a single handler:\n  %s",
			len(traps), strings.Join(traps, "\n  "))
	}
}

// A failed install must leave nothing behind: no scratch directory, and no
// full-size staging copy of the binary sitting in a directory on the user's
// PATH.
func TestFailedInstallLeavesNoTemporaryState(t *testing.T) {
	e := newEnv(t)

	// A private TMPDIR, so the scratch directory is observable.
	tmp := filepath.Join(e.home, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// A release whose tarball does not match its manifest: the failure lands
	// after the scratch directory has been created and filled.
	rel := newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1"))
	rel.files[artifactName()] = tarGzBinary(t, fakeBinary("TAMPERED"))
	srv := newReleaseServer(t, rel)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, append(releaseEnv(srv), "TMPDIR="+tmp),
		"", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code == 0 {
		t.Fatalf("the tampered artifact was installed\n%s", r.combined)
	}

	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("a failed install left scratch state in TMPDIR: %v", names)
	}

	// And no staging file in the prefix.
	if matches, _ := filepath.Glob(filepath.Join(prefix, "conduit.new.*")); len(matches) != 0 {
		t.Errorf("a failed install left staging files on PATH: %v", matches)
	}
}

// ---------------------------------------------------------------------------
// The PATH block is shell source
// ---------------------------------------------------------------------------

// Whatever install.sh appends, a real shell has to be able to read it and end
// up with the prefix on PATH -- and nothing else may have happened.
//
// A prefix containing a space is legitimate (it is what a home directory named
// "My Name" produces) and is exactly the case an unquoted write gets wrong in
// the other direction: it splits into two PATH entries, neither of which is the
// install directory.
func TestPathBlockSurvivesAPrefixWithSpaces(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "My Tools", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code != 0 {
		t.Fatalf("a prefix containing a space was refused\n%s", r.combined)
	}
	if !exists(filepath.Join(prefix, "conduit")) {
		t.Fatalf("nothing installed at %q\n%s", prefix, r.combined)
	}

	// Ask a real shell what the block did, rather than restating the rule.
	got := e.startupPATH(t)
	if !pathContains(got, prefix) {
		t.Errorf("a fresh shell does not have %q on PATH:\n%s\nprofile:\n%s",
			prefix, got, readFile(t, e.profile()))
	}
}

// ---------------------------------------------------------------------------
// End to end: the MCP configuration setup leaves behind
// ---------------------------------------------------------------------------

// The whole point of the install is a working MCP registration, and the entry
// it writes has to name a binary an AI client can actually spawn.
//
// A bare "conduit" cannot be: a client launched from a GUI inherits no shell
// PATH, so it never reads the block install.sh just appended to the profile.
// With --prefix the directory may be on no PATH in any process at all, which is
// exactly the situation this test creates.
func TestInstallWritesAnMCPEntryPointingAtTheInstalledBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the real binary; skipped under -short")
	}

	binary, err := os.ReadFile(buildRealBinary(t))
	if err != nil {
		t.Fatalf("read built binary: %v", err)
	}

	e := newEnv(t)

	// newEnv seeds a data directory whose conduit.db is the string
	// "pretend sqlite" -- enough to satisfy uninstall.sh's "does this look like
	// a data directory" guard, and not a database. The REAL binary is about to
	// open it, so it has to go: this test is about what the install writes, not
	// about how the binary reports a corrupt file.
	if err := os.RemoveAll(e.dataDir); err != nil {
		t.Fatalf("clear seeded data dir: %v", err)
	}

	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", binary))

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix)
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}
	if r.contains("setup reported problems") {
		t.Fatalf("setup did not complete\n%s", r.combined)
	}

	installed := filepath.Join(prefix, "conduit")

	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	data, err := os.ReadFile(filepath.Join(e.home, ".claude.json"))
	if err != nil {
		t.Fatalf("the install configured no MCP client: %v\n%s", err, r.combined)
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("the client config is not valid JSON: %v", err)
	}

	entry, ok := cfg.MCPServers["conduit-kb"]
	if !ok {
		t.Fatalf("no conduit-kb entry; got %v", cfg.MCPServers)
	}
	if entry.Command == "conduit" {
		t.Fatalf(`the MCP entry names the bare command "conduit"; ` +
			`a GUI-launched client has no PATH that finds it`)
	}
	if !filepath.IsAbs(entry.Command) {
		t.Fatalf("the MCP command %q is not an absolute path", entry.Command)
	}
	if entry.Command != installed {
		t.Errorf("the MCP entry names %q, but the binary was installed at %q",
			entry.Command, installed)
	}

	// The recorded command has to actually run, with the PATH a GUI-launched
	// client would have: no shell profile, no install prefix.
	probe := exec.Command(entry.Command, "version")
	probe.Env = []string{"HOME=" + e.home, "PATH=/usr/bin:/bin"}
	if out, perr := probe.CombinedOutput(); perr != nil {
		t.Errorf("the command in the MCP entry does not run: %v\n%s", perr, out)
	}

	// The installer's own transcript must be readable: the binary's structured
	// logger has no business in it.
	for _, line := range strings.Split(r.combined, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `{"level":`) {
			t.Errorf("raw log JSON in the installer transcript: %s", trimmed)
		}
	}

	// Diagnostics are printed once, not twice. `conduit setup` prints them and
	// install.sh used to run a full `conduit doctor` straight afterwards.
	if n := strings.Count(r.combined, "indexed content"); n > 1 {
		t.Errorf("the diagnostics were printed %d times; setup already prints them", n)
	}
	if n := strings.Count(r.combined, "# index a folder"); n > 1 {
		t.Errorf("the next steps were printed %d times", n)
	}
}
