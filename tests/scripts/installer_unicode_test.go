package scripts

// Non-ASCII install prefixes, and telling a failed lookup apart from a failed
// server.
//
// Both of these are cases where the previous round's hardening was too eager:
// the prefix whitelist refused characters that are ordinary parts of a name,
// and the status-code plumbing made the no-network branch unreachable.

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runShellScript runs a bash snippet inside the fake machine, with arguments.
//
// The snippet is passed via `bash -c`, and everything variable about the run is
// a positional argument rather than string-interpolated into the source: paths
// here contain spaces and non-ASCII characters, and interpolation would be
// testing the test's own quoting rather than the script's.
func (e *env) runShellScript(t *testing.T, script string, args ...string) result {
	t.Helper()

	cmd := exec.Command("bash", append([]string{"-c", script, "bash"}, args...)...)
	cmd.Env = []string{
		"HOME=" + e.home,
		"PATH=" + e.binDir + ":/usr/bin:/bin",
		"SHELL=/bin/bash",
		"TERM=dumb",
	}
	cmd.Dir = e.home

	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	code := 0
	if runErr := cmd.Run(); runErr != nil {
		var ee *exec.ExitError
		if asExitError(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run shell snippet: %v", runErr)
		}
	}

	return result{
		stdout:   out.String(),
		stderr:   errOut.String(),
		combined: out.String() + errOut.String(),
		code:     code,
	}
}

// ---------------------------------------------------------------------------
// A home directory is allowed to have a name
// ---------------------------------------------------------------------------

// The prefix whitelist deleted the allowed ASCII characters and treated
// whatever survived as forbidden. Every byte of a non-ASCII character survives
// that, so /Users/José and /home/müller were refused -- which made the DEFAULT
// install, with no flags at all, impossible for anyone whose name does not fit
// in ASCII.
//
// The install has to complete, and the PATH block it writes has to work: a
// quoting bug here would be invisible in the exit code and would surface later
// as `conduit: command not found`.
func TestNonASCIIPrefixInstallsAndIsUsable(t *testing.T) {
	prefixes := []struct {
		name string
		dir  string
	}{
		{"latin1 accents", "José/.local/bin"},
		{"umlaut", "müller/.local/bin"},
		{"cjk", "日本語/bin"},
		{"accent and space", "José Núñez/my tools"},
		{"emoji", "tools 🚀/bin"},
	}

	for _, tc := range prefixes {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

			prefix := filepath.Join(e.home, tc.dir)
			r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")

			if r.code != 0 {
				t.Fatalf("a non-ASCII prefix was refused:\n%s", r.combined)
			}
			if !exists(filepath.Join(prefix, "conduit")) {
				t.Fatalf("nothing installed at %q\n%s", prefix, r.combined)
			}

			// The written PATH block has to survive a real shell, byte for byte.
			// Asked of bash rather than asserted about the file, because the
			// question is what the shell does with it.
			got := e.startupPATH(t)
			if !pathContains(got, prefix) {
				t.Errorf("a fresh shell does not have %q on PATH.\nPATH: %s\nprofile:\n%s",
					prefix, got, readFile(t, e.profile()))
			}
		})
	}
}

// The exemption is for non-ASCII bytes only. Every shell metacharacter is
// ASCII, and UTF-8 encodes non-ASCII codepoints entirely in high bytes, so
// nothing about allowing them weakens the check that matters.
func TestNonASCIIExemptionDoesNotAdmitMetacharacters(t *testing.T) {
	canary := filepath.Join(t.TempDir(), "executed")

	// Each of these mixes a legitimate non-ASCII character with something that
	// must still be refused, so a fix that simply skipped the check for any
	// non-ASCII prefix would fail here.
	prefixes := []string{
		"/tmp/José/$(touch " + canary + ")",
		"/tmp/müller/`touch " + canary + "`",
		"/tmp/日本;rm -rf /",
		"/tmp/José'bin",
		"/tmp/José\"bin",
		"/tmp/José|bin",
	}

	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			e := newEnv(t)

			r := e.run(t, "install.sh", "--prefix", prefix, "--no-setup")
			if r.code == 0 {
				t.Fatalf("prefix %q was accepted\n%s", prefix, r.combined)
			}
			if exists(canary) {
				t.Fatalf("prefix %q was EXECUTED", prefix)
			}
		})
	}
}

// A refusal must name the thing the user actually set.
//
// PREFIX_SOURCE defaulted to "--prefix", so a prefix problem originating in
// $HOME -- the only way the default prefix can be bad -- reported an error
// about a flag the user had never passed, with nothing pointing at the cause.
func TestPrefixRefusalNamesTheRealSource(t *testing.T) {
	t.Run("flag", func(t *testing.T) {
		e := newEnv(t)
		r := e.run(t, "install.sh", "--prefix", "/tmp/a;b", "--no-setup")
		if r.code == 0 {
			t.Fatal("accepted")
		}
		if !r.contains("--prefix") {
			t.Errorf("a --prefix failure does not name --prefix:\n%s", r.combined)
		}
	})

	t.Run("env", func(t *testing.T) {
		e := newEnv(t)
		r := e.runWithEnv(t, []string{"CONDUIT_PREFIX=/tmp/a;b"}, "", "install.sh", "--no-setup")
		if r.code == 0 {
			t.Fatal("accepted")
		}
		if !r.contains("CONDUIT_PREFIX") {
			t.Errorf("a CONDUIT_PREFIX failure does not name the variable:\n%s", r.combined)
		}
	})

	// The default prefix is derived entirely from $HOME, so a bad $HOME is the
	// only way to reach a refusal without the user having typed anything.
	t.Run("home directory", func(t *testing.T) {
		e := newEnv(t)

		// A home directory containing a metacharacter. Rare, but it is the case
		// that produced a message blaming a flag nobody passed.
		badHome := filepath.Join(t.TempDir(), "a;b")
		if err := os.MkdirAll(badHome, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		r := e.runWithEnv(t, []string{"HOME=" + badHome}, "", "install.sh", "--no-setup")
		if r.code == 0 {
			t.Fatalf("a metacharacter in the default prefix was accepted\n%s", r.combined)
		}
		if strings.Contains(r.combined, "--prefix must") ||
			strings.Contains(r.combined, "It came from --prefix") {
			t.Errorf("the error blames --prefix, which was never passed:\n%s", r.combined)
		}
		if !strings.Contains(r.combined, "home directory") {
			t.Errorf("the error does not say the value came from the home directory:\n%s", r.combined)
		}
		// And it must suggest something the user can actually do.
		if !r.contains("--prefix /usr/local/bin") {
			t.Errorf("the error offers no way out:\n%s", r.combined)
		}
	})
}

// ---------------------------------------------------------------------------
// "Nothing answered" is not "the server failed"
// ---------------------------------------------------------------------------

// closedLoopbackURL returns a URL on a port nothing is listening on.
//
// A listener is opened to have the kernel allocate a free port, then closed, so
// the address is known-unused rather than guessed. No stub server is involved:
// the subject is what happens when there is nothing to talk to.
func closedLoopbackURL(t *testing.T) string {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return "http://" + addr
}

// curl prints 000 itself via --write-out when no HTTP exchange happened, and
// THEN exits non-zero. The `|| printf '000'` fallback therefore appended a
// second 000 and the function returned "000000", which matched no case and fell
// through to the catch-all: a refused connection was reported as "a failure at
// api.github.com, not on this machine". That is exactly backwards, and it made
// the no-network branch unreachable.
func TestUnreachableAPIIsReportedAsANetworkFailure(t *testing.T) {
	e := newEnv(t)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, []string{"CONDUIT_RELEASE_API_URL=" + closedLoopbackURL(t)},
		"", "install.sh", "--prefix", prefix, "--no-setup")

	if r.code == 0 {
		t.Fatalf("an unreachable API reported success\n%s", r.combined)
	}

	// The wrong message, verbatim from the catch-all branch.
	if r.contains("not on this machine") {
		t.Errorf("a refused connection was blamed on the server:\n%s", r.combined)
	}
	if strings.Contains(r.combined, "HTTP 000") {
		t.Errorf("a non-status was reported as an HTTP status:\n%s", r.combined)
	}

	// The right one.
	if !r.contains("Nothing answered") {
		t.Errorf("the no-network branch did not fire:\n%s", r.combined)
	}
	if !r.contains("--version") {
		t.Errorf("the error does not name the way around it:\n%s", r.combined)
	}
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("something was installed")
	}
}

// The status the function reports has to be a status, not two concatenated.
//
// Asserted directly against fetch_status, so the failure names the cause -- a
// doubled status code -- rather than whichever downstream message it happened
// to produce. fetch_status is extracted from install.sh by name and run on its
// own; nothing else in the script executes.
func TestFetchStatusReturnsOneStatusCode(t *testing.T) {
	e := newEnv(t)

	dest := filepath.Join(e.home, "body")
	script := `set -euo pipefail
have() { command -v "$1" >/dev/null 2>&1; }
die()  { printf 'die: %s\n' "$*" >&2; exit 1; }
eval "$(sed -n '/^fetch_status()/,/^}$/p' "$1")"
fetch_status "$2" "$3"
printf '\n'
`
	r := e.runShellScript(t, script, scriptPath(t, "install.sh"), closedLoopbackURL(t)+"/x", dest)
	got := strings.TrimSpace(r.stdout)

	if got != "000" {
		t.Errorf("fetch_status returned %q for an unreachable host, want exactly \"000\". "+
			"A doubled code matches no case in the caller and falls through to "+
			"the wrong branch.\n%s", got, r.combined)
	}
}
