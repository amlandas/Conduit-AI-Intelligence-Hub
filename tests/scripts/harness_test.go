// Package scripts exercises the shell installers and uninstallers.
//
// These scripts delete files for a living, and until now nothing tested them at
// all: every bug found in review was a bug no test could have caught, because
// the only way anyone had ever run them was by hand, on a real machine, with
// real data. That is not a safe way to develop a teardown tool.
//
// Every test here is hermetic. HOME is a temporary directory, PATH contains a
// stub `conduit` the test writes itself, and no test touches anything outside
// its own t.TempDir(). Nothing here may ever run against a developer's real
// installation -- which is precisely the accident these tests exist to make
// impossible.
package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// scriptPath resolves a script in the repository's scripts/ directory.
func scriptPath(t *testing.T, name string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "scripts", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("script not found: %v", err)
	}
	return abs
}

// env is one hermetic machine for a script to act on.
type env struct {
	home    string // stands in for the user's home directory
	dataDir string // the Conduit data directory
	binDir  string // the only directory on PATH besides the system minimum
	log     string // where the stub conduit records its invocations
}

// newEnv builds an isolated fake machine.
func newEnv(t *testing.T) *env {
	t.Helper()

	root := t.TempDir()
	e := &env{
		home:    filepath.Join(root, "home"),
		dataDir: filepath.Join(root, "home", ".conduit"),
		binDir:  filepath.Join(root, "bin"),
		log:     filepath.Join(root, "conduit-invocations.log"),
	}
	for _, dir := range []string{e.home, e.dataDir, e.binDir, filepath.Join(e.home, ".local", "bin")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// Files that make the directory look like a real Conduit data directory,
	// so the binary's "this does not look like a data directory" guard does not
	// fire in tests that are not about that guard.
	e.writeFile(t, filepath.Join(e.dataDir, "conduit.db"), "pretend sqlite")
	e.writeFile(t, filepath.Join(e.dataDir, "conduit.yaml"), "kb: {}\n")

	return e
}

// profile returns the shell rc file install.sh will write its PATH block to.
//
// The harness pins SHELL=/bin/bash, so the answer is bash's -- but which bash
// file depends on the platform, because the two operating systems start a
// terminal's bash differently:
//
//   - macOS terminals start a LOGIN shell, which reads ~/.bash_profile and
//     never ~/.bashrc.
//   - Linux terminals start an interactive non-login shell, which reads
//     ~/.bashrc and not ~/.bash_profile.
//
// Writing to the wrong one of those is issue #85: the installer reported
// success and the PATH entry was never read by any shell the user opened.
//
// uninstall.sh scans all four candidate profiles, so it finds whichever this
// returns; only the install side is platform-dependent.
func (e *env) profile() string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(e.home, ".bash_profile")
	}
	return filepath.Join(e.home, ".bashrc")
}

// startupPATH returns the PATH a freshly opened terminal would have on this
// machine, given the fake home's profiles.
//
// This asks the real bash rather than restating the rule the installer is
// supposed to follow, which is the only way for the test to be evidence about
// behaviour instead of a copy of the implementation. -l on macOS and -i on
// Linux is exactly how each platform's terminal starts bash.
func (e *env) startupPATH(t *testing.T) string {
	t.Helper()

	mode := "-i"
	if runtime.GOOS == "darwin" {
		mode = "-l"
	}

	// The value is fenced. A startup file is allowed to print things -- and one
	// of the fixtures below deliberately does -- so the shell's whole stdout is
	// not the answer, only the part between the sentinels is.
	const openTag, closeTag = "<<<CONDUIT_PATH[", "]CONDUIT_PATH>>>"
	cmd := exec.Command("bash", mode, "-c", `printf '%s' "`+openTag+`$PATH`+closeTag+`"`)
	cmd.Env = []string{
		"HOME=" + e.home,
		"PATH=/usr/bin:/bin",
		"SHELL=/bin/bash",
		"TERM=dumb",
	}
	cmd.Dir = e.home

	var out strings.Builder
	cmd.Stdout = &out
	// An interactive bash with no controlling terminal warns about job control
	// on stderr. That is noise, not failure, so stderr is discarded and only
	// the exit status is consulted.
	if err := cmd.Run(); err != nil {
		t.Fatalf("could not read the startup PATH from bash %s: %v", mode, err)
	}

	text := out.String()
	start := strings.Index(text, openTag)
	end := strings.Index(text, closeTag)
	if start < 0 || end < start {
		t.Fatalf("bash %s produced no PATH marker; output was:\n%s", mode, text)
	}
	return text[start+len(openTag) : end]
}

// pathContains reports whether a PATH string has dir as one of its entries.
//
// Substring matching would pass on a prefix of a longer directory name, which
// is the one way this assertion could be wrong in the installer's favour.
func pathContains(pathVar, dir string) bool {
	for _, entry := range strings.Split(pathVar, ":") {
		if entry == dir {
			return true
		}
	}
	return false
}

// writeFile creates a file under the fake machine.
func (e *env) writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// stubOptions configures the fake conduit binary.
type stubOptions struct {
	// SupportsDataDir makes `uninstall --help` advertise --data-dir.
	SupportsDataDir bool

	// SupportsPrefix makes `uninstall --help` advertise --prefix.
	SupportsPrefix bool

	// ExitCode is what the stub returns for a real (non --help) invocation.
	ExitCode int
}

// v2Stub behaves like a current binary.
func v2Stub() stubOptions {
	return stubOptions{SupportsDataDir: true, SupportsPrefix: true}
}

// v1Stub behaves like a Conduit 1.x binary: neither flag exists, and cobra
// exits non-zero when handed one.
func v1Stub() stubOptions {
	return stubOptions{}
}

// installStub writes a fake `conduit` at path.
//
// It records every invocation so a test can assert on the exact argv the script
// built, which is the only way to prove a flag was or was not forwarded.
func (e *env) installStub(t *testing.T, path string, opts stubOptions) {
	t.Helper()

	var helpFlags []string
	if opts.SupportsDataDir {
		helpFlags = append(helpFlags, `      --data-dir string   Conduit data directory`)
	}
	if opts.SupportsPrefix {
		helpFlags = append(helpFlags, `      --prefix string     Remove only the install in this directory`)
	}

	// The stub rejects flags it does not advertise, exactly as cobra does. That
	// is the behaviour that made an un-probed --data-dir look like a crash.
	var rejects []string
	if !opts.SupportsDataDir {
		rejects = append(rejects, `--data-dir`)
	}
	if !opts.SupportsPrefix {
		rejects = append(rejects, `--prefix`)
	}

	script := fmt.Sprintf(`#!/usr/bin/env bash
printf '%%s\n' "$*" >> %q

if [[ "$*" == *"--help"* ]]; then
    echo "Usage: conduit uninstall [flags]"
    echo "Flags:"
%s
    exit 0
fi

for arg in "$@"; do
    for bad in %s; do
        if [[ "$arg" == "$bad" ]]; then
            echo "Error: unknown flag: $arg" >&2
            exit 1
        fi
    done
done

exit %d
`,
		e.log,
		indentEcho(helpFlags),
		shellList(rejects),
		opts.ExitCode,
	)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
}

// installStubOnPath puts the stub where find_binary looks first.
func (e *env) installStubOnPath(t *testing.T, opts stubOptions) string {
	t.Helper()
	path := filepath.Join(e.home, ".local", "bin", "conduit")
	e.installStub(t, path, opts)
	return path
}

// indentEcho renders help lines as echo statements.
func indentEcho(lines []string) string {
	if len(lines) == 0 {
		return `    echo "  (no flags)"`
	}
	var b strings.Builder
	for _, l := range lines {
		fmt.Fprintf(&b, "    echo %q\n", l)
	}
	return strings.TrimRight(b.String(), "\n")
}

// shellList renders a list for a bash for-loop, or a token that matches nothing.
func shellList(items []string) string {
	if len(items) == 0 {
		return `"__none__"`
	}
	quoted := make([]string, 0, len(items))
	for _, i := range items {
		quoted = append(quoted, fmt.Sprintf("%q", i))
	}
	return strings.Join(quoted, " ")
}

// result is the outcome of running a script.
type result struct {
	stdout   string
	stderr   string
	combined string
	code     int
}

// contains reports whether the combined output mentions s.
func (r result) contains(s string) bool {
	return strings.Contains(r.combined, s)
}

// run executes a script inside the fake machine.
//
// The environment is replaced rather than extended: HOME points at the fake
// home and PATH holds only the stub directory plus the system minimum, so a
// script cannot reach the developer's real conduit, docker, podman or anything
// else that would make the test non-hermetic.
func (e *env) run(t *testing.T, script string, args ...string) result {
	t.Helper()
	return e.runWithStdin(t, "", script, args...)
}

// runWithStdin is run with something on standard input.
func (e *env) runWithStdin(t *testing.T, stdin, script string, args ...string) result {
	t.Helper()

	cmd := exec.Command("bash", append([]string{scriptPath(t, script)}, args...)...)
	cmd.Env = []string{
		"HOME=" + e.home,
		"PATH=" + e.binDir + ":/usr/bin:/bin",
		"SHELL=/bin/bash",
		"TERM=dumb",
	}
	cmd.Dir = e.home
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}

	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	err := cmd.Run()

	code := 0
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %s: %v", script, err)
		}
	}

	return result{
		stdout:   out.String(),
		stderr:   errOut.String(),
		combined: out.String() + errOut.String(),
		code:     code,
	}
}

// asExitError unwraps an *exec.ExitError.
func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// withGNUStat puts a `stat` on the fake machine's PATH that forwards to GNU
// coreutils stat, and reports whether it managed to.
//
// The BSD/GNU divergence in stat has now broken this suite twice, and only on
// Linux, because macOS developers and macOS CI never execute the GNU branch.
// Where GNU stat is installed alongside BSD stat -- `brew install coreutils`
// provides it as gstat -- this lets a Mac exercise the branch that only Linux
// would otherwise reach.
//
// The shim must invoke gstat by ABSOLUTE path. Scripts run here under a
// scrubbed PATH, so a bare `exec gstat` is unresolvable, the shim exits 127,
// every stat fails, and the test silently proves nothing.
func (e *env) withGNUStat(t *testing.T) bool {
	t.Helper()

	gnu, err := exec.LookPath("gstat")
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(gnu)
	if err != nil {
		return false
	}

	shim := "#!/usr/bin/env bash\nexec " + abs + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(e.binDir, "stat"), []byte(shim), 0o755); err != nil {
		t.Fatalf("write stat shim: %v", err)
	}

	// Prove the shim resolves before any caller relies on it.
	probe := exec.Command(filepath.Join(e.binDir, "stat"), "--version")
	probe.Env = []string{"PATH=" + e.binDir + ":/usr/bin:/bin"}
	out, perr := probe.Output()
	if perr != nil || !strings.Contains(string(out), "GNU coreutils") {
		t.Fatalf("GNU stat shim does not work: err=%v out=%q", perr, out)
	}
	return true
}

// runWithRealHome runs a script with the developer's actual HOME.
//
// Used only by the case-folding guard test, where the property under test is
// how the real filesystem treats the real home directory's name. It is
// restricted to --dry-run invocations for that reason: the whole point is that
// the script must refuse before doing anything.
func (e *env) runWithRealHome(t *testing.T, script string, args ...string) result {
	t.Helper()

	dryRun := false
	for _, a := range args {
		if a == "--dry-run" {
			dryRun = true
		}
	}
	if !dryRun {
		t.Fatalf("runWithRealHome is only safe for --dry-run invocations; got %v", args)
	}

	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}

	cmd := exec.Command("bash", append([]string{scriptPath(t, script)}, args...)...)
	cmd.Env = []string{
		"HOME=" + realHome,
		"PATH=" + e.binDir + ":/usr/bin:/bin",
		"SHELL=/bin/bash",
		"TERM=dumb",
	}
	cmd.Dir = e.home

	var out, errOut strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errOut

	runErr := cmd.Run()

	code := 0
	if runErr != nil {
		var ee *exec.ExitError
		if asExitError(runErr, &ee) {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run %s: %v", script, runErr)
		}
	}

	return result{
		stdout:   out.String(),
		stderr:   errOut.String(),
		combined: out.String() + errOut.String(),
		code:     code,
	}
}

// installToolStubs puts a fake Go toolchain on the fake machine's PATH.
//
// install.sh --from-source is otherwise the only way to reach the
// profile-writing code, and compiling the real binary for a test about a
// two-line shell function is both slow and a dependency on the developer's
// toolchain. The stub `go build` honours -o and writes a file there, which is
// all the script needs from it, and every other step -- staging, the atomic
// rename, PATH detection, the profile append -- is the real thing.
func (e *env) installToolStubs(t *testing.T) {
	t.Helper()

	// `go build ... -o <path> ...` writes an executable at <path>.
	goStub := `#!/usr/bin/env bash
if [[ "$1" == "build" ]]; then
    out=""
    while [[ $# -gt 0 ]]; do
        if [[ "$1" == "-o" ]]; then out="$2"; shift 2; continue; fi
        shift
    done
    if [[ -n "$out" ]]; then
        mkdir -p "$(dirname "$out")"
        printf '#!/bin/sh\necho conduit stub\n' > "$out"
        chmod 755 "$out"
    fi
    exit 0
fi
exit 0
`
	if err := os.WriteFile(filepath.Join(e.binDir, "go"), []byte(goStub), 0o755); err != nil {
		t.Fatalf("write go stub: %v", err)
	}

	// The script refuses to build without a C compiler, since cgo is required
	// for FTS5. Its presence is all that is checked.
	ccStub := "#!/usr/bin/env bash\nexit 0\n"
	if err := os.WriteFile(filepath.Join(e.binDir, "cc"), []byte(ccStub), 0o755); err != nil {
		t.Fatalf("write cc stub: %v", err)
	}
}

// runInstall runs install.sh against the fake machine.
func (e *env) runInstall(t *testing.T, prefix string, args ...string) result {
	t.Helper()
	e.installToolStubs(t)
	full := append([]string{"--prefix", prefix}, args...)
	return e.run(t, "install.sh", full...)
}

// sameDirOnDisk reports whether two names refer to one directory.
//
// The test-side counterpart of the guard's identity check: it decides whether a
// case-folding test is meaningful on this filesystem.
func sameDirOnDisk(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// fsFoldsCase reports whether the filesystem holding dir treats "abc" and "ABC"
// as the same name.
//
// Probed rather than inferred from GOOS. Whether "/USERS" names the same
// directory as "/Users" is a fact about the filesystem, not about the operating
// system: macOS formats APFS case-insensitively by default but can be formatted
// case-sensitively, ext4 is case-sensitive, and a single machine can mount both
// at once. A GOOS switch would assert the common case and be wrong on the
// others -- including, silently, on a developer's case-sensitive Mac.
func fsFoldsCase(t *testing.T, dir string) bool {
	t.Helper()

	lower := filepath.Join(dir, "caseprobe")
	if err := os.MkdirAll(lower, 0o755); err != nil {
		t.Fatalf("case probe: mkdir: %v", err)
	}
	defer func() { _ = os.RemoveAll(lower) }()

	upper := filepath.Join(dir, "CASEPROBE")
	ui, err := os.Stat(upper)
	if err != nil {
		// The upper-cased name does not resolve, so names are distinct here.
		return false
	}
	li, err := os.Stat(lower)
	if err != nil {
		t.Fatalf("case probe: stat: %v", err)
	}
	return os.SameFile(li, ui)
}

// invocations returns the argv lines the stub recorded.
func (e *env) invocations(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read invocation log: %v", err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// realInvocations returns the stub's invocations excluding capability probes.
func (e *env) realInvocations(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, inv := range e.invocations(t) {
		if !strings.Contains(inv, "--help") {
			out = append(out, inv)
		}
	}
	return out
}

// exists reports whether a path is present in the fake machine.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// readFile returns a file's contents.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
