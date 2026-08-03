package embed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Download errors. Callers match on these rather than on message text.
var (
	// ErrChecksumMismatch means the bytes that arrived are not the bytes the
	// registry pins. The partial file has already been deleted.
	ErrChecksumMismatch = errors.New("embed: model checksum mismatch")

	// ErrDownloadIncomplete means the transfer ended early. The partial file has
	// already been deleted, so a retry starts clean.
	ErrDownloadIncomplete = errors.New("embed: model download incomplete")

	// ErrDownloadUnavailable means the artifact could not be reached at all:
	// no network, DNS failure, or a non-200 response.
	ErrDownloadUnavailable = errors.New("embed: model download unavailable")

	// ErrInsecureURL means a download URL was neither HTTPS nor loopback.
	ErrInsecureURL = errors.New("embed: refusing to download over an insecure URL")
)

// defaultDownloadTimeout bounds a whole transfer. The largest pinned artifact is
// ~640MB, so this is generous even on a slow link, while still guaranteeing that
// a wedged connection eventually fails instead of hanging a CLI command forever.
const defaultDownloadTimeout = 30 * time.Minute

// progressInterval throttles progress callbacks. Hashing and writing happen at
// disk speed; calling back on every 32KB chunk would spend more time formatting
// terminal output than moving bytes.
const progressInterval = 250 * time.Millisecond

// ProgressEvent reports transfer progress for a single model.
type ProgressEvent struct {
	// ModelID is the registry key being downloaded.
	ModelID string

	// Downloaded is the number of bytes written so far.
	Downloaded int64

	// Total is the expected size from the registry. Always > 0.
	Total int64

	// Done is true for the final event of a transfer.
	Done bool
}

// Percent returns transfer completion in the range [0, 100].
func (e ProgressEvent) Percent() float64 {
	if e.Total <= 0 {
		return 0
	}
	p := float64(e.Downloaded) / float64(e.Total) * 100
	if p > 100 {
		return 100
	}
	return p
}

// DownloadResult describes the outcome of a successful download.
type DownloadResult struct {
	// ModelID is the registry key.
	ModelID string

	// Path is the verified GGUF file, exactly where the sidecar looks for it.
	Path string

	// Bytes is the file size.
	Bytes int64

	// SHA256 is the verified hash, which equals the registry's pin.
	SHA256 string

	// AlreadyPresent is true when a correct file was already on disk and
	// nothing was transferred.
	AlreadyPresent bool
}

// Downloader fetches pinned embedding models and verifies them.
//
// Every download is checked against the SHA-256 in the model registry before it
// is allowed to take its final name. That check is not optional and there is no
// flag to skip it: an embedding model is executable input to llama.cpp, it
// arrives from a third-party CDN, and a silently corrupted or substituted GGUF
// would degrade retrieval in ways no test would catch.
//
// The zero value is ready to use.
type Downloader struct {
	// Client performs the HTTP request. nil means a client with sane timeouts.
	Client *http.Client

	// ResolveURL maps a pinned spec to the URL to fetch.
	//
	// nil -- the production setting -- means ModelSpec.DownloadURL, which is
	// built from the registry entry. The registry is the only place download
	// URLs come from; this hook exists so tests can serve a fake GGUF from an
	// httptest server without a network.
	ResolveURL func(ModelSpec) string

	// Progress, if non-nil, receives throttled transfer updates.
	Progress func(ProgressEvent)
}

// client returns the HTTP client to use.
func (d *Downloader) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: defaultDownloadTimeout}
}

// urlFor returns the download URL for spec.
func (d *Downloader) urlFor(spec ModelSpec) string {
	if d.ResolveURL != nil {
		return d.ResolveURL(spec)
	}
	return spec.DownloadURL()
}

// emit delivers a progress event if a callback is registered.
func (d *Downloader) emit(ev ProgressEvent) {
	if d.Progress != nil {
		d.Progress(ev)
	}
}

// DownloadModel fetches the model with the given registry id into dataDir.
//
// It is a convenience wrapper over Download for callers that have an id rather
// than a spec.
func (d *Downloader) DownloadModel(ctx context.Context, modelID, dataDir string) (*DownloadResult, error) {
	spec, err := LookupModel(modelID)
	if err != nil {
		return nil, err
	}
	return d.Download(ctx, spec, dataDir)
}

// Download fetches spec's artifact into dataDir and verifies it.
//
// The transfer goes to a temporary file in the destination directory and is
// renamed into place only after the hash matches. That ordering is what makes
// the operation safe to interrupt: a killed download leaves a .part file that
// the next run deletes, never a truncated file at the real path that would look
// valid to a later os.Stat.
//
// A correct file already at the destination is a no-op. A file at the
// destination whose hash does not match the registry is treated as corrupt and
// replaced, because that exact path is Conduit's own and the registry pin is the
// definition of what belongs there.
func (d *Downloader) Download(ctx context.Context, spec ModelSpec, dataDir string) (*DownloadResult, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if dataDir == "" {
		return nil, fmt.Errorf("embed: download requires a data directory")
	}

	dest := spec.LocalPath(dataDir)
	destDir := filepath.Dir(dest)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("embed: create model directory %s: %w", destDir, err)
	}

	// Already there and correct? Nothing to do. This is what makes `conduit
	// model download` safe to put in an install script.
	if sum, err := hashFile(dest); err == nil {
		if strings.EqualFold(sum, spec.SHA256) {
			info, statErr := os.Stat(dest)
			var size int64
			if statErr == nil {
				size = info.Size()
			}
			d.emit(ProgressEvent{ModelID: spec.ID, Downloaded: size, Total: spec.SizeBytes, Done: true})
			return &DownloadResult{
				ModelID:        spec.ID,
				Path:           dest,
				Bytes:          size,
				SHA256:         sum,
				AlreadyPresent: true,
			}, nil
		}
		// Wrong bytes at our own path: unusable, and keeping it would make the
		// sidecar fail later with a much worse error than this one.
		if err := os.Remove(dest); err != nil {
			return nil, fmt.Errorf("embed: remove corrupt model at %s: %w", dest, err)
		}
	}

	// Sweep stale .part files from earlier interrupted runs before starting, so
	// a repeatedly cancelled download cannot fill the disk.
	cleanPartFiles(destDir, spec.HFFile)

	rawURL := d.urlFor(spec)
	if err := checkURL(rawURL); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("embed: build request for %s: %w", spec.ID, err)
	}
	req.Header.Set("User-Agent", "conduit-model-downloader")

	resp, err := d.client().Do(req)
	if err != nil {
		return nil, downloadNetError(spec, rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: %s returned HTTP %d for %s",
			ErrDownloadUnavailable, hostOf(rawURL), resp.StatusCode, spec.HFFile)
	}

	// The registry pins the exact size. A different Content-Length means the
	// artifact upstream is not the one that was pinned, and there is no point
	// spending bandwidth to prove it with a hash.
	if resp.ContentLength > 0 && resp.ContentLength != spec.SizeBytes {
		return nil, fmt.Errorf("%w: %s advertises %d bytes, registry pins %d; the upstream artifact changed",
			ErrChecksumMismatch, spec.HFFile, resp.ContentLength, spec.SizeBytes)
	}

	tmp, err := os.CreateTemp(destDir, spec.HFFile+".part-*")
	if err != nil {
		return nil, fmt.Errorf("embed: create temp file in %s: %w", destDir, err)
	}
	tmpName := tmp.Name()
	// Removed on every path except the successful rename, which clears the name.
	defer func() {
		if tmpName != "" {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	hasher := sha256.New()
	counter := &progressWriter{
		total:   spec.SizeBytes,
		modelID: spec.ID,
		emit:    d.emit,
	}

	written, err := io.Copy(io.MultiWriter(tmp, hasher, counter), resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: %s after %s: %w",
			ErrDownloadIncomplete, spec.HFFile, humanSize(written), err)
	}

	if written != spec.SizeBytes {
		return nil, fmt.Errorf("%w: %s ended at %d bytes, registry pins %d; retry the download",
			ErrDownloadIncomplete, spec.HFFile, written, spec.SizeBytes)
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	if !strings.EqualFold(sum, spec.SHA256) {
		return nil, fmt.Errorf("%w: %s\n  expected %s\n  got      %s\nThe partial download was deleted. This means the file was corrupted in transit or the upstream artifact was replaced; it is not safe to use.",
			ErrChecksumMismatch, spec.HFFile, strings.ToLower(spec.SHA256), sum)
	}

	if err := tmp.Chmod(0o644); err != nil {
		return nil, fmt.Errorf("embed: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		return nil, fmt.Errorf("embed: sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("embed: close %s: %w", tmpName, err)
	}

	// Verified bytes, and only now does the file take the name the sidecar
	// looks for.
	if err := os.Rename(tmpName, dest); err != nil {
		return nil, fmt.Errorf("embed: install model to %s: %w", dest, err)
	}
	tmpName = ""

	d.emit(ProgressEvent{ModelID: spec.ID, Downloaded: written, Total: spec.SizeBytes, Done: true})

	return &DownloadResult{
		ModelID: spec.ID,
		Path:    dest,
		Bytes:   written,
		SHA256:  sum,
	}, nil
}

// ModelStatus reports whether a pinned model is present and valid on disk.
type ModelStatus struct {
	// Spec is the registry entry.
	Spec ModelSpec

	// Path is where the artifact belongs.
	Path string

	// Present is true when a file exists at Path.
	Present bool

	// Verified is true when the file's hash matches the registry pin. Computing
	// it reads the whole file, so callers that only need presence should not
	// ask for verification.
	Verified bool

	// SizeBytes is the on-disk size, 0 when absent.
	SizeBytes int64
}

// StatModel reports whether spec's artifact is on disk under dataDir.
//
// When verify is false the hash is not computed, which keeps `conduit model
// list` instant on a machine holding several hundred megabytes of GGUF.
func StatModel(spec ModelSpec, dataDir string, verify bool) ModelStatus {
	st := ModelStatus{Spec: spec, Path: spec.LocalPath(dataDir)}

	info, err := os.Stat(st.Path)
	if err != nil || info.IsDir() {
		return st
	}
	st.Present = true
	st.SizeBytes = info.Size()

	if verify {
		if sum, herr := hashFile(st.Path); herr == nil && strings.EqualFold(sum, spec.SHA256) {
			st.Verified = true
		}
	}
	return st
}

// VerifyModel checks an on-disk artifact against its registry pin.
//
// It returns ErrModelNotFound when the file is absent and ErrChecksumMismatch
// when it is present but wrong.
func VerifyModel(spec ModelSpec, dataDir string) error {
	path := spec.LocalPath(dataDir)
	sum, err := hashFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: no GGUF at %s; run `conduit model download %s`",
				ErrModelNotFound, path, spec.ID)
		}
		return fmt.Errorf("embed: read %s: %w", path, err)
	}
	if !strings.EqualFold(sum, spec.SHA256) {
		return fmt.Errorf("%w: %s\n  expected %s\n  got      %s",
			ErrChecksumMismatch, path, strings.ToLower(spec.SHA256), sum)
	}
	return nil
}

// progressWriter counts bytes and emits throttled progress events.
type progressWriter struct {
	total   int64
	written int64
	modelID string
	emit    func(ProgressEvent)
	last    time.Time
}

func (w *progressWriter) Write(p []byte) (int, error) {
	w.written += int64(len(p))
	now := time.Now()
	if now.Sub(w.last) >= progressInterval {
		w.last = now
		w.emit(ProgressEvent{
			ModelID:    w.modelID,
			Downloaded: w.written,
			Total:      w.total,
		})
	}
	return len(p), nil
}

// hashFile returns the lowercase hex SHA-256 of a file.
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// stalePartAge is how long a .part file must have gone untouched before it is
// considered abandoned.
//
// An in-progress download rewrites its temp file continuously, so its mtime is
// never this stale unless the writing process is gone or wedged. It is bounded
// by defaultDownloadTimeout for that reason: a transfer that has made no
// progress for longer than a whole download is allowed to take is not running.
const stalePartAge = defaultDownloadTimeout

// cleanPartFiles removes abandoned temp files from interrupted downloads.
//
// Age matters here. A blanket sweep would delete the temp file of a *concurrent*
// download that is still writing to it: on Unix the other process keeps its
// open handle and carries on to completion, then fails at the rename because
// the path it was promised no longer exists. That is a confusing failure for
// the process that did nothing wrong, so only files that nothing can plausibly
// still be writing are removed.
func cleanPartFiles(dir, base string) {
	matches, err := filepath.Glob(filepath.Join(dir, base+".part-*"))
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-stalePartAge)
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			_ = os.Remove(m)
		}
	}
}

// checkURL rejects anything that is not HTTPS.
//
// Plain HTTP is permitted only to loopback, which is what lets the test suite
// serve a fake GGUF from httptest without weakening the rule that real model
// downloads are encrypted.
func checkURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: cannot parse %q: %w", ErrInsecureURL, raw, err)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("%w: %s is plain HTTP", ErrInsecureURL, raw)
	default:
		return fmt.Errorf("%w: unsupported scheme %q in %s", ErrInsecureURL, u.Scheme, raw)
	}
}

// isLoopbackHost reports whether host names the local machine.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// downloadNetError turns a transport failure into advice.
//
// The common case by a wide margin is "this machine is offline", and saying so
// is more useful than surfacing a wrapped dial error.
func downloadNetError(spec ModelSpec, rawURL string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: download of %s was cancelled: %w", ErrDownloadIncomplete, spec.HFFile, err)
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return fmt.Errorf("%w: cannot resolve %s. This machine appears to be offline; the model is a %s download and must be fetched once before embeddings work: %w",
			ErrDownloadUnavailable, hostOf(rawURL), humanSize(spec.SizeBytes), err)
	}

	return fmt.Errorf("%w: cannot reach %s. Check the network connection, or set embed.provider to \"none\" to run keyword-only search: %w",
		ErrDownloadUnavailable, hostOf(rawURL), err)
}

// hostOf extracts a host for error messages, falling back to the raw string.
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// humanSize renders a byte count for humans.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
