package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// These tests exercise install.sh's release path against a stub GitHub serving
// artifacts the test builds itself, in exactly the shape
// .github/workflows/release.yml publishes:
//
//	conduit-<os>-<arch>.tar.gz   one executable named `conduit` at the root
//	SHA256SUMS                   bare basenames, one line per tarball
//
// Nothing here reaches the network. The workflow cannot be run from a
// development machine, so this is the evidence that the two halves of the
// contract agree.

// artifactName is the tarball this machine's install.sh will ask for.
func artifactName() string {
	return fmt.Sprintf("conduit-%s-%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// release is one published release the stub server offers.
type release struct {
	tag   string
	files map[string][]byte // basename -> contents
}

// tarGzBinary packages content as an executable named `conduit` at the root of
// a gzipped tar, which is what the release workflow produces.
func tarGzBinary(t *testing.T, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name: "conduit",
		Mode: 0o755,
		Size: int64(len(content)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("tar write: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// sha256Manifest renders a SHA256SUMS body in sha256sum's format.
func sha256Manifest(files map[string][]byte) string {
	var b strings.Builder
	// Sorted for a stable manifest; the order carries no meaning.
	for _, name := range sortedKeys(files) {
		if name == "SHA256SUMS" {
			continue
		}
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return b.String()
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Small maps; a plain insertion sort keeps this dependency-free.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}

// newReleaseServer starts a stub GitHub.
//
// It answers the two things install.sh asks for: the release list, used to
// resolve "latest" to a tag, and the download directory for a given tag.
// Releases are listed newest first, as GitHub does.
func newReleaseServer(t *testing.T, releases ...release) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()

	// GET /repos/<owner>/<repo>/releases
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/releases") {
			http.NotFound(w, r)
			return
		}
		var b strings.Builder
		b.WriteString("[")
		for i, rel := range releases {
			if i > 0 {
				b.WriteString(",")
			}
			// Shaped like the real payload, prerelease included, so the parser
			// is exercised against something recognisable.
			fmt.Fprintf(&b, `{"id":%d,"tag_name":"%s","name":"%s","draft":false,"prerelease":true}`,
				100+i, rel.tag, rel.tag)
		}
		b.WriteString("]")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(b.String()))
	})

	// GET /download/<tag>/<file>
	mux.HandleFunc("/download/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/download/")
		parts := strings.SplitN(rest, "/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		tag, name := parts[0], parts[1]
		for _, rel := range releases {
			if rel.tag != tag {
				continue
			}
			body, ok := rel.files[name]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// releaseEnv points install.sh at the stub server.
func releaseEnv(srv *httptest.Server) []string {
	return []string{
		"CONDUIT_RELEASE_API_URL=" + srv.URL,
		"CONDUIT_RELEASE_BASE_URL=" + srv.URL + "/download",
	}
}

// newRelease builds a release carrying one artifact for this platform, plus a
// matching checksum manifest.
func newRelease(t *testing.T, tag string, binary []byte) release {
	t.Helper()
	files := map[string][]byte{
		artifactName(): tarGzBinary(t, binary),
	}
	files["SHA256SUMS"] = []byte(sha256Manifest(files))
	return release{tag: tag, files: files}
}

// fakeBinary is a script standing in for the real artifact, for the tests whose
// subject is install.sh rather than the binary. It reports the version it was
// told to, so a test can prove which artifact was installed.
func fakeBinary(version string) []byte {
	return []byte("#!/bin/sh\necho 'conduit " + version + "'\n")
}

// ---------------------------------------------------------------------------
// The happy path
// ---------------------------------------------------------------------------

func TestInstallsFromAReleaseArtifact(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	installed := filepath.Join(prefix, "conduit")
	if !exists(installed) {
		t.Fatalf("nothing was installed at %s\n%s", installed, r.combined)
	}

	info, err := os.Stat(installed)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("the installed binary is not executable (mode %o)", info.Mode().Perm())
	}

	// Verification is the point of this path; it must say it happened.
	if !r.contains("checksum verified") {
		t.Errorf("the install did not report verifying the checksum:\n%s", r.combined)
	}

	out, err := exec.Command(installed, "version").Output()
	if err != nil {
		t.Fatalf("the installed artifact does not run: %v", err)
	}
	if !strings.Contains(string(out), "v2.0.0-beta.1") {
		t.Errorf("installed artifact reports %q", out)
	}
}

// ---------------------------------------------------------------------------
// Resolving "latest"
// ---------------------------------------------------------------------------

// GitHub's /releases/latest endpoint EXCLUDES pre-releases, and every v2
// release is one. An installer using it would either 404 or -- far worse --
// serve v0.1.10: the newest non-pre-release, from the daemon era, a different
// product whose artifacts do not work. The default must resolve to the newest
// v2 entry in the list instead.
func TestDefaultResolvesTheNewestV2Release(t *testing.T) {
	e := newEnv(t)

	// Newest first, as GitHub returns them, with a daemon-era tag underneath.
	srv := newReleaseServer(t,
		newRelease(t, "v2.0.0-beta.3", fakeBinary("v2.0.0-beta.3")),
		newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")),
		newRelease(t, "v0.1.10", fakeBinary("v0.1.10-DAEMON-ERA")),
	)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	out, err := exec.Command(filepath.Join(prefix, "conduit"), "version").Output()
	if err != nil {
		t.Fatalf("installed artifact does not run: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if strings.Contains(got, "DAEMON-ERA") {
		t.Fatalf("installed the daemon-era release: %q\n%s", got, r.combined)
	}
	if !strings.Contains(got, "v2.0.0-beta.3") {
		t.Errorf("installed %q, want the newest v2 release v2.0.0-beta.3\n%s", got, r.combined)
	}
}

// --version pins a specific tag, and the pin must win over the newest release.
func TestVersionFlagPinsTheRelease(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t,
		newRelease(t, "v2.0.0-beta.3", fakeBinary("v2.0.0-beta.3")),
		newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")),
	)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh",
		"--prefix", prefix, "--no-setup", "--version", "v2.0.0-beta.1")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	out, err := exec.Command(filepath.Join(prefix, "conduit"), "version").Output()
	if err != nil {
		t.Fatalf("installed artifact does not run: %v", err)
	}
	if !strings.Contains(string(out), "v2.0.0-beta.1") {
		t.Errorf("--version was ignored; installed %q\n%s", out, r.combined)
	}
}

// Until the first release exists the default path has nothing to install, and
// it has to say so in a way that names the alternative.
func TestNoReleaseYetFailsClearly(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t) // an empty releases list

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code == 0 {
		t.Fatalf("a missing release reported success\n%s", r.combined)
	}
	if !r.contains("--from-source") {
		t.Errorf("the error does not name the alternative:\n%s", r.combined)
	}
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("something was installed despite there being no release")
	}
}

// A v0.1.x release present but no v2 one is the same situation: v1 artifacts
// are a different product, and installing one would be worse than failing.
func TestOnlyV1ReleasesIsTreatedAsNoRelease(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v0.1.10", fakeBinary("v0.1.10")))

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code == 0 {
		t.Fatalf("installed a v1 release as if it were v2\n%s", r.combined)
	}
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("a daemon-era artifact was installed")
	}
}

// ---------------------------------------------------------------------------
// Verification is mandatory
// ---------------------------------------------------------------------------

// A tarball that does not match the manifest must never be installed, and the
// download must not be left on disk for someone to run by hand afterwards.
func TestChecksumMismatchIsFatal(t *testing.T) {
	e := newEnv(t)

	rel := newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1"))
	// Corrupt the tarball while leaving the manifest describing the original --
	// exactly what a tampered or truncated download looks like.
	rel.files[artifactName()] = tarGzBinary(t, fakeBinary("TAMPERED"))
	srv := newReleaseServer(t, rel)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code == 0 {
		t.Fatalf("a checksum mismatch was installed anyway\n%s", r.combined)
	}
	if !r.contains("checksum mismatch") {
		t.Errorf("the failure was not reported as a checksum mismatch:\n%s", r.combined)
	}
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("the mismatched artifact was installed")
	}
}

// No manifest means nothing can be verified, so nothing may be installed. There
// is deliberately no flag that skips this.
func TestMissingChecksumManifestIsFatal(t *testing.T) {
	e := newEnv(t)

	rel := newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1"))
	delete(rel.files, "SHA256SUMS")
	srv := newReleaseServer(t, rel)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code == 0 {
		t.Fatalf("installed without a checksum manifest\n%s", r.combined)
	}
	if !r.contains("SHA256SUMS") {
		t.Errorf("the error does not say what was missing:\n%s", r.combined)
	}
	if exists(filepath.Join(prefix, "conduit")) {
		t.Error("something was installed with nothing to verify it against")
	}
}

// There is no --skip-checksum, --insecure or -k. If one is ever added, this
// fails and whoever added it has to justify it in review.
func TestThereIsNoWayToSkipVerification(t *testing.T) {
	body, err := os.ReadFile(scriptPath(t, "install.sh"))
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	for _, flag := range []string{"--skip-checksum", "--no-verify", "--insecure", "--no-check-certificate"} {
		if strings.Contains(string(body), flag) {
			t.Errorf("install.sh mentions %s; verification must have no escape hatch", flag)
		}
	}
}

// A release with no artifact for this platform is a real situation -- linux
// arm64 and Intel macs have none. It must fail while naming what the release
// does contain, so the user can tell "unsupported platform" from "broken
// release".
func TestMissingPlatformArtifactNamesWhatExists(t *testing.T) {
	e := newEnv(t)

	other := map[string][]byte{
		"conduit-plan9-mips.tar.gz": tarGzBinary(t, fakeBinary("v2.0.0-beta.1")),
	}
	other["SHA256SUMS"] = []byte(sha256Manifest(other))
	srv := newReleaseServer(t, release{tag: "v2.0.0-beta.1", files: other})

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code == 0 {
		t.Fatalf("installed from a release with no artifact for this platform\n%s", r.combined)
	}
	if !r.contains("conduit-plan9-mips.tar.gz") {
		t.Errorf("the error does not list what the release publishes:\n%s", r.combined)
	}
	if !r.contains("--from-source") {
		t.Errorf("the error does not name the alternative:\n%s", r.combined)
	}
}

// ---------------------------------------------------------------------------
// Transport
// ---------------------------------------------------------------------------

// The override exists to point the installer at a local server or a mirror. It
// must not become a way to fetch a binary over plaintext from anywhere on the
// network: the exception is loopback only.
func TestPlaintextHTTPIsRefusedForNonLoopback(t *testing.T) {
	e := newEnv(t)

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, []string{
		"CONDUIT_RELEASE_API_URL=http://example.invalid",
		"CONDUIT_RELEASE_BASE_URL=http://example.invalid/download",
	}, "", "install.sh", "--prefix", prefix, "--no-setup", "--version", "v2.0.0-beta.1")

	if r.code == 0 {
		t.Fatalf("a plaintext remote download was accepted\n%s", r.combined)
	}
	if !r.contains("non-HTTPS") {
		t.Errorf("the refusal does not explain itself:\n%s", r.combined)
	}
}

// Using the override has to be visible. A silent redirect of where somebody's
// binary comes from is precisely what an installer must never do.
func TestOverrideIsAnnounced(t *testing.T) {
	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", fakeBinary("v2.0.0-beta.1")))

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}
	if !r.contains("CONDUIT_RELEASE_BASE_URL") {
		t.Errorf("the installer did not say it was using an override:\n%s", r.combined)
	}
}

// ---------------------------------------------------------------------------
// The real artifact
// ---------------------------------------------------------------------------

var (
	realBinaryOnce sync.Once
	realBinaryPath string
	realBinaryErr  error
)

// buildRealBinary compiles conduit exactly as the release workflow does.
//
// Built once for the whole package: it is the slowest thing here by a wide
// margin, and every caller wants the same bytes.
func buildRealBinary(t *testing.T) string {
	t.Helper()

	realBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "conduit-release-artifact-")
		if err != nil {
			realBinaryErr = err
			return
		}
		out := filepath.Join(dir, "conduit")

		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			realBinaryErr = err
			return
		}

		// The workflow's flags, verbatim.
		cmd := exec.Command("go", "build",
			"-tags", "fts5",
			"-trimpath",
			"-ldflags", "-s -w -X main.Version=v2.0.0-beta.1 -X main.BuildTime=1970-01-01T00:00:00Z",
			"-o", out,
			"./cmd/conduit",
		)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
		if combined, berr := cmd.CombinedOutput(); berr != nil {
			realBinaryErr = fmt.Errorf("go build: %v\n%s", berr, combined)
			return
		}
		realBinaryPath = out
	})

	if realBinaryErr != nil {
		t.Fatalf("could not build the release artifact: %v", realBinaryErr)
	}
	return realBinaryPath
}

// End to end against the genuine article: the real binary, packaged the way the
// workflow packages it, served the way GitHub serves it, installed by
// install.sh, and then run.
//
// The other tests in this file use a shell script as the payload, which proves
// install.sh's logic but says nothing about whether the artifact contract holds
// for a real executable. This is the test that ties the workflow and the
// installer together, and it is the only local evidence that a release will be
// installable -- the workflow itself cannot be run from a development machine.
func TestRealArtifactRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the real binary; skipped under -short")
	}

	binary, err := os.ReadFile(buildRealBinary(t))
	if err != nil {
		t.Fatalf("read built binary: %v", err)
	}

	e := newEnv(t)
	srv := newReleaseServer(t, newRelease(t, "v2.0.0-beta.1", binary))

	prefix := filepath.Join(e.home, "opt", "bin")
	r := e.runWithEnv(t, releaseEnv(srv), "", "install.sh", "--prefix", prefix, "--no-setup")
	if r.code != 0 {
		t.Fatalf("install exited %d\n%s", r.code, r.combined)
	}

	installed := filepath.Join(prefix, "conduit")

	// It runs, and it knows the version the workflow injected. A binary
	// reporting "dev" means the ldflags symbol name is wrong -- Go's linker
	// ignores -X for a symbol that does not exist, silently.
	out, err := exec.Command(installed, "version", "--json").Output()
	if err != nil {
		t.Fatalf("the installed artifact does not run: %v", err)
	}
	if !strings.Contains(string(out), `"version":"v2.0.0-beta.1"`) {
		t.Errorf("version was not injected into the artifact: %s", out)
	}

	// And FTS5 survived the round trip. A CGO_ENABLED=0 or missing-tag build
	// installs and prints its version quite happily, then fails every search
	// with "no such module: fts5".
	doctor := exec.Command(installed, "doctor", "--json",
		"--data-dir", filepath.Join(e.home, "smoke-data"), "--probe-timeout", "1")
	doctor.Env = []string{
		"HOME=" + e.home,
		"PATH=/usr/bin:/bin",
	}
	// doctor exits non-zero when any check fails, and with no embedding model
	// several legitimately do. Its output is the subject, not its exit code.
	doctorOut, _ := doctor.Output()
	if !strings.Contains(string(doctorOut), `"name": "FTS5 lexical search"`) {
		t.Fatalf("doctor did not report on FTS5:\n%s", doctorOut)
	}
	if !fts5Reported0K(string(doctorOut)) {
		t.Errorf("FTS5 is not available in the installed artifact:\n%s", doctorOut)
	}
}

// fts5Reported0K reports whether doctor's JSON says FTS5 is available.
//
// A small scan rather than a JSON decode: the shape is doctor's, not this
// package's, and duplicating the struct here would be one more thing to keep in
// step for no gain.
func fts5Reported0K(doctorJSON string) bool {
	idx := strings.Index(doctorJSON, `"name": "FTS5 lexical search"`)
	if idx < 0 {
		return false
	}
	rest := doctorJSON[idx:]
	end := strings.Index(rest, "}")
	if end < 0 {
		return false
	}
	return strings.Contains(rest[:end], `"status": "ok"`)
}
