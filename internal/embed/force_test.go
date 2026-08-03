package embed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A8: --force used to delete the existing model before the network attempt.
// Offline, interrupted, or against a changed upstream, that left the machine
// with no model at all -- having destroyed a perfectly good one in order to go
// looking for an identical copy.
func TestDownloadForce_KeepsExistingModelWhenTheFetchFails(t *testing.T) {
	body, spec := fakeGGUF(t, 8*1024)
	dataDir := t.TempDir()

	// Start with a valid, verified model on disk.
	srv := serveBytes(t, body)
	if _, err := downloaderFor(srv).Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("seed download: %v", err)
	}
	if err := VerifyModel(spec, dataDir); err != nil {
		t.Fatalf("seed model is not valid: %v", err)
	}

	// Now force a re-download against an endpoint that is not there.
	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	d := &Downloader{
		Force:      true,
		Client:     dead.Client(),
		ResolveURL: func(ModelSpec) string { return deadURL + "/model.gguf" },
	}

	if _, err := d.Download(context.Background(), spec, dataDir); err == nil {
		t.Fatal("a download against a dead endpoint reported success")
	}

	// The working model must still be there and still be valid.
	if err := VerifyModel(spec, dataDir); err != nil {
		t.Fatalf("--force destroyed a working model when the fetch failed: %v", err)
	}
}

// The same property for a mid-flight cancellation, which is what Ctrl-C looks
// like to the downloader.
func TestDownloadForce_KeepsExistingModelWhenCancelled(t *testing.T) {
	body, spec := fakeGGUF(t, 256*1024)
	dataDir := t.TempDir()

	srv := serveBytes(t, body)
	if _, err := downloaderFor(srv).Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("seed download: %v", err)
	}

	stall := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body[:1024])
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-stall
	}))
	t.Cleanup(func() {
		close(stall)
		slow.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-time.After(50 * time.Millisecond)
		cancel()
	}()

	d := &Downloader{
		Force:      true,
		Client:     slow.Client(),
		ResolveURL: func(ModelSpec) string { return slow.URL + "/model.gguf" },
	}
	if _, err := d.Download(ctx, spec, dataDir); err == nil {
		t.Fatal("a cancelled download reported success")
	}

	if err := VerifyModel(spec, dataDir); err != nil {
		t.Fatalf("--force destroyed a working model when cancelled: %v", err)
	}
}

// Force must still do its job: fetch again and replace.
func TestDownloadForce_RefetchesAndReplaces(t *testing.T) {
	body, spec := fakeGGUF(t, 8*1024)
	dataDir := t.TempDir()

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	d := &Downloader{
		Client:     srv.Client(),
		ResolveURL: func(ModelSpec) string { return srv.URL + "/model.gguf" },
	}
	if _, err := d.Download(context.Background(), spec, dataDir); err != nil {
		t.Fatalf("first download: %v", err)
	}

	d.Force = true
	res, err := d.Download(context.Background(), spec, dataDir)
	if err != nil {
		t.Fatalf("forced download: %v", err)
	}
	if res.AlreadyPresent {
		t.Error("--force took the already-present shortcut")
	}
	if hits != 2 {
		t.Errorf("server hit %d times, want 2 (--force must actually re-fetch)", hits)
	}
	if err := VerifyModel(spec, dataDir); err != nil {
		t.Errorf("model invalid after a forced re-download: %v", err)
	}
}

// A corrupt file at the destination is likewise not deleted up front: if the
// replacement download fails, the user is no worse off than before.
func TestDownload_CorruptDestinationSurvivesAFailedFetch(t *testing.T) {
	_, spec := fakeGGUF(t, 8*1024)
	dataDir := t.TempDir()

	dest := spec.LocalPath(dataDir)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dest, []byte("corrupt but present"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	d := &Downloader{
		Client:     dead.Client(),
		ResolveURL: func(ModelSpec) string { return deadURL + "/model.gguf" },
	}
	if _, err := d.Download(context.Background(), spec, dataDir); err == nil {
		t.Fatal("expected the download to fail")
	}

	// Still there. It is useless, but deleting it bought nothing and the error
	// message is what tells the user to re-fetch.
	if _, err := os.Stat(dest); err != nil {
		t.Errorf("a failed download deleted the pre-existing file: %v", err)
	}
}

// A12: HFFile is joined onto the data directory, so a separator or a parent
// reference in it would write outside <data-dir>/models.
func TestModelSpecValidate_RejectsUnsafeFilenames(t *testing.T) {
	base := func() ModelSpec {
		_, spec := fakeGGUF(t, 1024)
		return spec
	}

	for _, name := range []string{
		"../escape.gguf",
		"../../etc/cron.d/payload",
		"sub/dir/model.gguf",
		"/absolute/model.gguf",
		"..",
		".",
		`windows\path.gguf`,
	} {
		spec := base()
		spec.HFFile = name
		if err := spec.Validate(); err == nil {
			t.Errorf("Validate accepted unsafe HFFile %q", name)
		}
	}

	// A plain filename must still be fine.
	spec := base()
	spec.HFFile = "model.f16.gguf"
	if err := spec.Validate(); err != nil {
		t.Errorf("Validate rejected a legitimate filename: %v", err)
	}
}

// Every pinned entry must satisfy the rule, or the registry itself is the hole.
func TestRegistryFilenamesAreSafe(t *testing.T) {
	for _, spec := range Models() {
		if err := spec.Validate(); err != nil {
			t.Errorf("%s: %v", spec.ID, err)
		}
	}
}
