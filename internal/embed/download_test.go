package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGGUF builds deterministic bytes standing in for a model artifact, plus
// the spec that pins them. Nothing here touches the network or the real
// registry: the whole download path is exercised against an httptest server.
func fakeGGUF(t *testing.T, size int) ([]byte, ModelSpec) {
	t.Helper()

	body := make([]byte, size)
	for i := range body {
		body[i] = byte(i % 251)
	}
	sum := sha256.Sum256(body)

	return body, ModelSpec{
		ID:            "test-model",
		Dimensions:    8,
		ContextTokens: 512,
		Pooling:       "mean",
		HFRepo:        "example/test-model-GGUF",
		HFFile:        "test-model.f16.gguf",
		SHA256:        hex.EncodeToString(sum[:]),
		SizeBytes:     int64(size),
		Quantization:  "F16",
		License:       "Apache-2.0",
	}
}

// serveBytes starts an httptest server returning body for any path.
func serveBytes(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// downloaderFor builds a Downloader pointed at srv instead of HuggingFace.
func downloaderFor(srv *httptest.Server) *Downloader {
	return &Downloader{
		Client:     srv.Client(),
		ResolveURL: func(ModelSpec) string { return srv.URL + "/model.gguf" },
	}
}

func TestDownloadHappyPath(t *testing.T) {
	body, spec := fakeGGUF(t, 64*1024)
	srv := serveBytes(t, body)
	dataDir := t.TempDir()

	var events []ProgressEvent
	d := downloaderFor(srv)
	d.Progress = func(ev ProgressEvent) { events = append(events, ev) }

	res, err := d.Download(context.Background(), spec, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	if res.AlreadyPresent {
		t.Error("first download reported AlreadyPresent")
	}
	if res.Bytes != spec.SizeBytes {
		t.Errorf("Bytes = %d, want %d", res.Bytes, spec.SizeBytes)
	}
	if !strings.EqualFold(res.SHA256, spec.SHA256) {
		t.Errorf("SHA256 = %s, want %s", res.SHA256, spec.SHA256)
	}

	// The file must land exactly where the sidecar's resolveModel looks.
	want := filepath.Join(dataDir, "models", spec.HFFile)
	if res.Path != want {
		t.Errorf("Path = %s, want %s", res.Path, want)
	}
	onDisk, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read downloaded model: %v", err)
	}
	if len(onDisk) != len(body) {
		t.Fatalf("on-disk size = %d, want %d", len(onDisk), len(body))
	}

	if len(events) == 0 {
		t.Error("no progress events emitted")
	}
	last := events[len(events)-1]
	if !last.Done {
		t.Error("final progress event not marked Done")
	}
	if last.Percent() != 100 {
		t.Errorf("final Percent() = %v, want 100", last.Percent())
	}
}

// A download must leave nothing behind but the finished file: a stray .part
// would be re-verified as a model by nothing, but it would waste hundreds of
// megabytes per interrupted attempt.
func TestDownloadLeavesNoTempFiles(t *testing.T) {
	body, spec := fakeGGUF(t, 32*1024)
	srv := serveBytes(t, body)
	dataDir := t.TempDir()

	if _, err := downloaderFor(srv).Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("Download: %v", err)
	}

	entries, err := os.ReadDir(filepath.Join(dataDir, "models"))
	if err != nil {
		t.Fatalf("read models dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != spec.HFFile {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("models dir = %v, want only %s", names, spec.HFFile)
	}
}

// Re-running a completed download must be a cheap no-op, because install
// scripts call it unconditionally.
func TestDownloadIsIdempotent(t *testing.T) {
	body, spec := fakeGGUF(t, 16*1024)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	dataDir := t.TempDir()
	d := downloaderFor(srv)

	if _, err := d.Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("first Download: %v", err)
	}
	res, err := d.Download(context.Background(), spec, dataDir)
	if err != nil {
		t.Fatalf("second Download: %v", err)
	}

	if !res.AlreadyPresent {
		t.Error("second Download did not report AlreadyPresent")
	}
	if hits != 1 {
		t.Errorf("server hit %d times, want 1 (second call should not transfer)", hits)
	}
}

// The whole point of the pin: bytes that do not hash to the registry value are
// refused and deleted, never installed.
func TestDownloadRejectsCorruptBody(t *testing.T) {
	body, spec := fakeGGUF(t, 8*1024)

	corrupt := make([]byte, len(body))
	copy(corrupt, body)
	corrupt[len(corrupt)/2] ^= 0xFF // same length, different content

	srv := serveBytes(t, corrupt)
	dataDir := t.TempDir()

	_, err := downloaderFor(srv).Download(context.Background(), spec, dataDir)
	if err == nil {
		t.Fatal("Download accepted a corrupt body")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), spec.SHA256) {
		t.Errorf("error does not name the expected hash: %v", err)
	}

	// Nothing at the destination, and no temp file left to be mistaken for one.
	if _, statErr := os.Stat(spec.LocalPath(dataDir)); !os.IsNotExist(statErr) {
		t.Error("corrupt download was installed at the destination path")
	}
	assertNoPartFiles(t, filepath.Join(dataDir, "models"))
}

// A short body is the interrupted-transfer case: the server closes early.
func TestDownloadRejectsTruncatedBody(t *testing.T) {
	body, spec := fakeGGUF(t, 64*1024)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No Content-Length: the transfer simply ends early, which is what a
		// dropped connection looks like to the client.
		_, _ = w.Write(body[:len(body)/3])
	}))
	t.Cleanup(srv.Close)

	dataDir := t.TempDir()
	_, err := downloaderFor(srv).Download(context.Background(), spec, dataDir)
	if err == nil {
		t.Fatal("Download accepted a truncated body")
	}
	if !errors.Is(err, ErrDownloadIncomplete) {
		t.Fatalf("error = %v, want ErrDownloadIncomplete", err)
	}

	if _, statErr := os.Stat(spec.LocalPath(dataDir)); !os.IsNotExist(statErr) {
		t.Error("truncated download was installed at the destination path")
	}
	assertNoPartFiles(t, filepath.Join(dataDir, "models"))
}

// A mid-flight cancellation must clean up after itself so the retry is clean.
func TestDownloadCancelledMidFlightRetriesClean(t *testing.T) {
	body, spec := fakeGGUF(t, 512*1024)

	stall := make(chan struct{})
	var serveFull bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if serveFull {
			_, _ = w.Write(body)
			return
		}
		// Send a prefix, flush it, then block until the client gives up.
		_, _ = w.Write(body[:1024])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-stall
	}))
	t.Cleanup(func() {
		close(stall)
		srv.Close()
	})

	dataDir := t.TempDir()
	d := downloaderFor(srv)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := d.Download(ctx, spec, dataDir)
	if err == nil {
		t.Fatal("cancelled Download returned no error")
	}

	// The interrupted attempt must not have left a partial file behind.
	assertNoPartFiles(t, filepath.Join(dataDir, "models"))
	if _, statErr := os.Stat(spec.LocalPath(dataDir)); !os.IsNotExist(statErr) {
		t.Fatal("cancelled download installed a file at the destination")
	}

	// The retry, against a now-healthy server, must succeed.
	serveFull = true
	res, err := d.Download(context.Background(), spec, dataDir)
	if err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
	if res.Bytes != spec.SizeBytes {
		t.Errorf("retry Bytes = %d, want %d", res.Bytes, spec.SizeBytes)
	}
}

// A stale .part from a killed process must not survive the next attempt.
func TestDownloadSweepsStalePartFiles(t *testing.T) {
	body, spec := fakeGGUF(t, 16*1024)
	srv := serveBytes(t, body)
	dataDir := t.TempDir()

	modelsDir := filepath.Join(dataDir, "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	stale := filepath.Join(modelsDir, spec.HFFile+".part-123456")
	if err := os.WriteFile(stale, []byte("junk from a killed process"), 0o644); err != nil {
		t.Fatalf("write stale part: %v", err)
	}

	if _, err := downloaderFor(srv).Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale .part file survived a successful download")
	}
}

// A wrong-but-present file at the destination is corrupt by definition, since
// the registry pins what belongs at that path. It gets replaced.
func TestDownloadReplacesCorruptDestination(t *testing.T) {
	body, spec := fakeGGUF(t, 16*1024)
	srv := serveBytes(t, body)
	dataDir := t.TempDir()

	dest := spec.LocalPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dest, []byte("not a gguf"), 0o644); err != nil {
		t.Fatalf("write bad model: %v", err)
	}

	res, err := downloaderFor(srv).Download(context.Background(), spec, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if res.AlreadyPresent {
		t.Error("a corrupt destination was reported AlreadyPresent")
	}
	if err := VerifyModel(spec, dataDir); err != nil {
		t.Errorf("model not valid after replacement: %v", err)
	}
}

// A Content-Length that disagrees with the pin means the upstream artifact
// changed; fail before spending the bandwidth.
func TestDownloadRejectsSizeMismatchEarly(t *testing.T) {
	body, spec := fakeGGUF(t, 8*1024)
	spec.SizeBytes = int64(len(body)) + 4096 // pin a different size

	srv := serveBytes(t, body)
	dataDir := t.TempDir()

	_, err := downloaderFor(srv).Download(context.Background(), spec, dataDir)
	if err == nil {
		t.Fatal("Download accepted a size that disagrees with the pin")
	}
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want ErrChecksumMismatch", err)
	}
}

func TestDownloadRejectsNon200(t *testing.T) {
	_, spec := fakeGGUF(t, 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	_, err := downloaderFor(srv).Download(context.Background(), spec, t.TempDir())
	if !errors.Is(err, ErrDownloadUnavailable) {
		t.Fatalf("error = %v, want ErrDownloadUnavailable", err)
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should report the status code: %v", err)
	}
}

// An unreachable host must produce advice, not a raw dial error.
//
// The unreachable endpoint is a loopback port that was just closed, so the test
// needs neither DNS nor a network: the kernel refuses the connection locally.
func TestDownloadUnreachableIsGraceful(t *testing.T) {
	_, spec := fakeGGUF(t, 1024)

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close() // nothing is listening on that port any more

	d := &Downloader{
		ResolveURL: func(ModelSpec) string { return deadURL + "/model.gguf" },
		Client:     &http.Client{Timeout: 5 * time.Second},
	}

	_, err := d.Download(context.Background(), spec, t.TempDir())
	if err == nil {
		t.Fatal("Download against a dead endpoint returned no error")
	}
	if !errors.Is(err, ErrDownloadUnavailable) {
		t.Fatalf("error = %v, want ErrDownloadUnavailable", err)
	}
	// The advice matters as much as the error class: a user who cannot reach
	// the CDN needs to know the keyword-only escape hatch exists.
	if !strings.Contains(err.Error(), "embed.provider") {
		t.Errorf("error should mention the embed.provider escape hatch: %v", err)
	}
}

// The offline branch is selected by a DNS failure, which cannot be provoked
// hermetically, so the classifier is tested directly.
func TestDownloadNetErrorClassifiesDNSFailure(t *testing.T) {
	_, spec := fakeGGUF(t, 1024)
	dnsErr := &net.DNSError{Err: "no such host", Name: "huggingface.co", IsNotFound: true}

	err := downloadNetError(spec, "https://huggingface.co/x/y.gguf", &url.Error{
		Op: "Get", URL: "https://huggingface.co/x/y.gguf", Err: dnsErr,
	})

	if !errors.Is(err, ErrDownloadUnavailable) {
		t.Fatalf("error = %v, want ErrDownloadUnavailable", err)
	}
	if !strings.Contains(err.Error(), "offline") {
		t.Errorf("DNS failure should be reported as offline: %v", err)
	}
	if !strings.Contains(err.Error(), "huggingface.co") {
		t.Errorf("error should name the host: %v", err)
	}
}

// A cancelled transfer is an incomplete download, not an unreachable host.
func TestDownloadNetErrorClassifiesCancellation(t *testing.T) {
	_, spec := fakeGGUF(t, 1024)
	err := downloadNetError(spec, "https://huggingface.co/x/y.gguf", context.Canceled)
	if !errors.Is(err, ErrDownloadIncomplete) {
		t.Fatalf("error = %v, want ErrDownloadIncomplete", err)
	}
}

func TestDownloadRejectsInsecureURL(t *testing.T) {
	_, spec := fakeGGUF(t, 1024)

	d := &Downloader{ResolveURL: func(ModelSpec) string { return "http://models.example.com/m.gguf" }}
	_, err := d.Download(context.Background(), spec, t.TempDir())
	if !errors.Is(err, ErrInsecureURL) {
		t.Fatalf("plain HTTP to a remote host: error = %v, want ErrInsecureURL", err)
	}

	d = &Downloader{ResolveURL: func(ModelSpec) string { return "file:///etc/passwd" }}
	_, err = d.Download(context.Background(), spec, t.TempDir())
	if !errors.Is(err, ErrInsecureURL) {
		t.Fatalf("file scheme: error = %v, want ErrInsecureURL", err)
	}
}

func TestCheckURL(t *testing.T) {
	ok := []string{
		"https://huggingface.co/x/y/resolve/main/z.gguf",
		"http://127.0.0.1:5000/model.gguf",
		"http://localhost:8080/model.gguf",
		"http://[::1]:8080/model.gguf",
	}
	for _, u := range ok {
		if err := checkURL(u); err != nil {
			t.Errorf("checkURL(%q) = %v, want nil", u, err)
		}
	}

	bad := []string{
		"http://huggingface.co/x.gguf",
		"ftp://example.com/x.gguf",
		"file:///tmp/x.gguf",
		"http://127.0.0.1.evil.com/x.gguf",
	}
	for _, u := range bad {
		if err := checkURL(u); err == nil {
			t.Errorf("checkURL(%q) = nil, want an error", u)
		}
	}
}

// Every real registry URL must be HTTPS and must come from the registry, not
// from a literal anywhere else in the tree.
func TestRegistryURLsAreSecure(t *testing.T) {
	for _, spec := range Models() {
		u := spec.DownloadURL()
		if !strings.HasPrefix(u, "https://huggingface.co/") {
			t.Errorf("%s: DownloadURL = %s, want an https huggingface.co URL", spec.ID, u)
		}
		if err := checkURL(u); err != nil {
			t.Errorf("%s: checkURL(%s) = %v", spec.ID, u, err)
		}
	}
}

// The downloader with no ResolveURL hook must use the registry's own URL. This
// is the guard that keeps the registry the single source of truth.
func TestDownloaderDefaultsToRegistryURL(t *testing.T) {
	spec, err := LookupModel(DefaultModelID)
	if err != nil {
		t.Fatalf("LookupModel: %v", err)
	}
	d := &Downloader{}
	if got := d.urlFor(spec); got != spec.DownloadURL() {
		t.Errorf("urlFor = %s, want %s", got, spec.DownloadURL())
	}
}

func TestStatModel(t *testing.T) {
	body, spec := fakeGGUF(t, 4096)
	dataDir := t.TempDir()

	st := StatModel(spec, dataDir, true)
	if st.Present || st.Verified {
		t.Errorf("absent model reported Present=%v Verified=%v", st.Present, st.Verified)
	}
	if st.Path != filepath.Join(dataDir, "models", spec.HFFile) {
		t.Errorf("Path = %s", st.Path)
	}

	srv := serveBytes(t, body)
	if _, err := downloaderFor(srv).Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("Download: %v", err)
	}

	st = StatModel(spec, dataDir, true)
	if !st.Present || !st.Verified {
		t.Errorf("downloaded model reported Present=%v Verified=%v", st.Present, st.Verified)
	}
	if st.SizeBytes != spec.SizeBytes {
		t.Errorf("SizeBytes = %d, want %d", st.SizeBytes, spec.SizeBytes)
	}

	// Presence without verification must not read the file's contents, and must
	// still report a corrupted file as present.
	if err := os.WriteFile(st.Path, []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	st = StatModel(spec, dataDir, false)
	if !st.Present {
		t.Error("tampered file should still be Present")
	}
	if st.Verified {
		t.Error("Verified set when verification was not requested")
	}
	st = StatModel(spec, dataDir, true)
	if st.Verified {
		t.Error("tampered file passed verification")
	}
}

func TestVerifyModel(t *testing.T) {
	body, spec := fakeGGUF(t, 4096)
	dataDir := t.TempDir()

	if err := VerifyModel(spec, dataDir); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("absent model: error = %v, want ErrModelNotFound", err)
	}

	srv := serveBytes(t, body)
	if _, err := downloaderFor(srv).Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if err := VerifyModel(spec, dataDir); err != nil {
		t.Errorf("VerifyModel after download: %v", err)
	}

	if err := os.WriteFile(spec.LocalPath(dataDir), []byte("tampered"), 0o644); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if err := VerifyModel(spec, dataDir); !errors.Is(err, ErrChecksumMismatch) {
		t.Errorf("tampered model: error = %v, want ErrChecksumMismatch", err)
	}
}

// The downloaded artifact must satisfy the very check the sidecar performs, or
// the download is useless. This closes the loop between WP-3.3 and WP-2.2.
func TestDownloadedModelSatisfiesSidecarResolution(t *testing.T) {
	body, spec := fakeGGUF(t, 8*1024)
	srv := serveBytes(t, body)
	dataDir := t.TempDir()

	// Register the fake spec so ManagerConfigForModel can find it, exactly as
	// the sidecar would for a real model.
	registry[spec.ID] = spec
	t.Cleanup(func() { delete(registry, spec.ID) })

	mcfg, err := ManagerConfigForModel(dataDir, spec.ID, "")
	if err != nil {
		t.Fatalf("ManagerConfigForModel: %v", err)
	}

	mgr, err := NewManager(mcfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// Before the download, resolution must fail and say so.
	if _, err := mgr.resolveModel(); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("resolveModel before download: %v, want ErrModelNotFound", err)
	}

	res, err := downloaderFor(srv).Download(context.Background(), spec, dataDir)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}

	// After it, resolution must return the very file that was downloaded.
	got, err := mgr.resolveModel()
	if err != nil {
		t.Fatalf("resolveModel after download: %v", err)
	}
	if got != res.Path {
		t.Errorf("resolveModel = %s, download wrote %s", got, res.Path)
	}
	if got != mcfg.ModelPath {
		t.Errorf("resolveModel = %s, ManagerConfig.ModelPath = %s", got, mcfg.ModelPath)
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{274290560, "261.6 MB"},
	}
	for _, c := range cases {
		if got := humanSize(c.in); got != c.want {
			t.Errorf("humanSize(%d) = %s, want %s", c.in, got, c.want)
		}
	}
}

// assertNoPartFiles fails if any interrupted-download temp file remains.
func assertNoPartFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.part-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Errorf("leftover temp files: %v", matches)
	}
}
