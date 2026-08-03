package embed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
)

// Sidecar defaults.
const (
	// LlamaServerBinary is the executable name looked up on PATH.
	LlamaServerBinary = "llama-server"

	// DefaultIdleTimeout is how long a sidecar may sit unused before it is
	// shut down to reclaim RAM. Every conduit process refreshes a shared
	// last-used timestamp, so this is a global idle window, not a per-process one.
	DefaultIdleTimeout = 5 * time.Minute

	// DefaultStartupTimeout bounds waiting for the sidecar to report healthy.
	// Model load dominates this; the binary itself starts in well under a second.
	DefaultStartupTimeout = 90 * time.Second

	// DefaultContextSize is the sidecar context window in tokens.
	DefaultContextSize = 2048

	// healthProbeTimeout bounds a single /health request.
	healthProbeTimeout = 3 * time.Second

	// healthCacheTTL is how long a successful health probe is trusted before
	// being repeated. Keeps a tight embed loop from probing on every batch.
	healthCacheTTL = 5 * time.Second

	// maxTouchInterval caps how long the shared last-used timestamp may go
	// unrefreshed while the sidecar is in active use. The effective interval is
	// also bounded by IdleTimeout/4 (see NewManager) so that an actively-used
	// sidecar always refreshes several times per idle window; a fixed throttle
	// larger than IdleTimeout would let the reaper kill a sidecar mid-use.
	maxTouchInterval = 5 * time.Second

	// terminateGrace is how long a sidecar gets to exit after SIGTERM.
	terminateGrace = 5 * time.Second

	// lockWaitTimeout bounds waiting for the spawn lock.
	lockWaitTimeout = 2 * time.Minute

	// stderrTailBytes caps how much sidecar stderr is retained for diagnostics.
	stderrTailBytes = 8 << 10
)

// wellKnownBinaryDirs are searched when llama-server is not on PATH. These are
// the standard Homebrew (Apple Silicon and Intel) and system locations.
var wellKnownBinaryDirs = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"/usr/bin",
}

// installHint is the actionable remedy attached to ErrBinaryNotFound.
const installHint = "install it with `brew install llama.cpp` (macOS) or build llama.cpp from source, " +
	"then re-run; alternatively set kb.embed.binary_path in ~/.conduit/conduit.yaml to an explicit llama-server path"

// ManagerConfig configures the llama-server sidecar manager.
type ManagerConfig struct {
	// DataDir is the conduit data directory. Sidecar lock and state files live
	// under <DataDir>/embed. Required.
	DataDir string

	// BinaryPath optionally pins the llama-server executable. When empty the
	// manager searches PATH and then wellKnownBinaryDirs.
	BinaryPath string

	// ModelPath is the absolute path to the GGUF model file. Required.
	ModelPath string

	// ModelID labels the model in state and logs, e.g. "nomic-embed-text-v1.5".
	ModelID string

	// Dimensions is the expected vector width, used to validate responses.
	Dimensions int

	// ContextSize is the sidecar context window. Defaults to DefaultContextSize.
	ContextSize int

	// Pooling selects the llama.cpp pooling mode ("mean", "cls", "last").
	// Getting this wrong silently degrades retrieval quality, so it is always
	// passed explicitly rather than left to llama-server's default.
	Pooling string

	// QueryPrefix and DocPrefix are model-specific instruction prefixes.
	QueryPrefix string
	DocPrefix   string

	// InputSuffix is appended to every input. See ModelSpec.InputSuffix.
	InputSuffix string

	// IdleTimeout is the global idle window before shutdown. Defaults to
	// DefaultIdleTimeout. A negative value disables idle shutdown.
	IdleTimeout time.Duration

	// StartupTimeout bounds waiting for health after spawn.
	StartupTimeout time.Duration

	// Timeout bounds a single embedding call.
	Timeout time.Duration

	// BatchSize caps texts per HTTP request.
	BatchSize int

	// ExtraArgs are appended to the llama-server command line.
	ExtraArgs []string

	// Logger overrides the component logger.
	Logger *zerolog.Logger
}

// sidecarState is the on-disk record shared by all conduit processes.
//
// It lives at <DataDir>/embed/sidecar.json and is always written atomically
// (temp file + rename) while holding the spawn lock.
type sidecarState struct {
	PID        int       `json:"pid"`
	Port       int       `json:"port"`
	ModelID    string    `json:"model_id"`
	ModelPath  string    `json:"model_path"`
	BinaryPath string    `json:"binary_path"`
	Dimensions int       `json:"dimensions"`
	StartedAt  time.Time `json:"started_at"`
	LastUsed   time.Time `json:"last_used"`
}

// Manager owns the shared llama-server sidecar.
//
// A single sidecar is shared by every conduit process on the machine. The
// manager coordinates via a lock file and a state file in the data directory:
// a process that finds a healthy running instance reuses it instead of
// spawning a duplicate.
//
// Manager is safe for concurrent use.
type Manager struct {
	cfg    ManagerConfig
	logger zerolog.Logger

	stateDir  string
	lockPath  string
	statePath string

	// touchEvery throttles refreshes of the shared last-used timestamp. It is
	// always well under IdleTimeout so active use cannot be mistaken for idle.
	touchEvery time.Duration

	mu          sync.Mutex
	endpoint    string
	lastHealthy time.Time
	lastTouch   time.Time
	provider    *LlamaServerProvider

	reaperOnce sync.Once
	reaperStop chan struct{}
	reaperDone chan struct{}

	closeOnce sync.Once
}

// NewManager constructs a sidecar manager. It performs no I/O beyond creating
// the state directory; the sidecar is spawned lazily on first use.
func NewManager(cfg ManagerConfig) (*Manager, error) {
	if cfg.DataDir == "" {
		return nil, fmt.Errorf("embed: DataDir is required")
	}
	if cfg.ContextSize <= 0 {
		cfg.ContextSize = DefaultContextSize
	}
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = DefaultIdleTimeout
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = DefaultStartupTimeout
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.Pooling == "" {
		cfg.Pooling = "mean"
	}
	if cfg.ModelID == "" {
		cfg.ModelID = "unknown"
	}

	stateDir := filepath.Join(cfg.DataDir, "embed")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("embed: create state dir %s: %w", stateDir, err)
	}

	logger := observability.Logger("embed.sidecar")
	if cfg.Logger != nil {
		logger = *cfg.Logger
	}

	// Refresh the shared timestamp at least four times per idle window so an
	// actively-used sidecar is never reaped as idle.
	touchEvery := maxTouchInterval
	if cfg.IdleTimeout > 0 && cfg.IdleTimeout/4 < touchEvery {
		touchEvery = cfg.IdleTimeout / 4
	}
	if touchEvery < 10*time.Millisecond {
		touchEvery = 10 * time.Millisecond
	}

	return &Manager{
		cfg:        cfg,
		logger:     logger,
		stateDir:   stateDir,
		lockPath:   filepath.Join(stateDir, "sidecar.lock"),
		statePath:  filepath.Join(stateDir, "sidecar.json"),
		touchEvery: touchEvery,
		reaperStop: make(chan struct{}),
		reaperDone: make(chan struct{}),
	}, nil
}

// FindLlamaServer locates the llama-server executable.
//
// Search order: explicit override, PATH, then well-known install locations.
// The returned error carries an actionable install hint.
func FindLlamaServer(override string) (string, error) {
	if override != "" {
		info, err := os.Stat(override)
		if err != nil {
			return "", fmt.Errorf("%w: configured binary_path %q does not exist; %s", ErrBinaryNotFound, override, installHint)
		}
		if info.IsDir() || !isExecutable(info) {
			return "", fmt.Errorf("%w: configured binary_path %q is not an executable file; %s", ErrBinaryNotFound, override, installHint)
		}
		return override, nil
	}

	name := LlamaServerBinary
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	for _, dir := range wellKnownBinaryDirs {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || !isExecutable(info) {
			continue
		}
		return candidate, nil
	}

	return "", fmt.Errorf("%w: searched PATH and %v; %s", ErrBinaryNotFound, wellKnownBinaryDirs, installHint)
}

// isExecutable reports whether the file mode carries any execute bit.
func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

// resolveModel validates the configured GGUF path.
//
// Downloading is deliberately out of scope for this work package; the error
// names the expected path and points at the first-run download command that
// WP-3.3 will implement.
func (m *Manager) resolveModel() (string, error) {
	path := m.cfg.ModelPath
	if path == "" {
		return "", fmt.Errorf("%w: no model path configured; set kb.embed.model_path or run `conduit embed download %s` (first-run download lands in WP-3.3)",
			ErrModelNotFound, m.cfg.ModelID)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%w: expected GGUF at %q; run `conduit embed download %s` to fetch it (first-run download lands in WP-3.3)",
			ErrModelNotFound, path, m.cfg.ModelID)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %q is a directory, expected a GGUF file", ErrModelNotFound, path)
	}
	return path, nil
}

// readState loads the shared state file. A missing file yields (nil, nil).
func (m *Manager) readState() (*sidecarState, error) {
	raw, err := os.ReadFile(m.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("embed: read state: %w", err)
	}
	var st sidecarState
	if err := json.Unmarshal(raw, &st); err != nil {
		// A corrupt state file is treated as absent; the caller will reap it.
		m.logger.Warn().Err(err).Str("path", m.statePath).Msg("corrupt sidecar state, ignoring")
		return nil, nil
	}
	return &st, nil
}

// writeState persists state atomically. Callers must hold the spawn lock.
func (m *Manager) writeState(st *sidecarState) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("embed: marshal state: %w", err)
	}
	tmp, err := os.CreateTemp(m.stateDir, "sidecar-*.json.tmp")
	if err != nil {
		return fmt.Errorf("embed: create temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("embed: write temp state: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("embed: chmod temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("embed: close temp state: %w", err)
	}
	if err := os.Rename(tmpName, m.statePath); err != nil {
		return fmt.Errorf("embed: install state: %w", err)
	}
	return nil
}

// endpointFor builds the loopback base URL for a port. The sidecar is bound to
// 127.0.0.1 exclusively; no other interface is ever addressed.
func endpointFor(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// probeHealth checks whether a sidecar on port answers /health.
func probeHealth(ctx context.Context, port int) error {
	p, err := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL: endpointFor(port),
		Timeout: healthProbeTimeout,
	})
	if err != nil {
		return err
	}
	defer func() { _ = p.Close() }()
	return p.Health(ctx)
}

// stateUsable reports whether st describes a live, healthy sidecar running the
// model this manager wants.
func (m *Manager) stateUsable(ctx context.Context, st *sidecarState) bool {
	if st == nil || st.PID <= 0 || st.Port <= 0 {
		return false
	}
	if !processAlive(st.PID) {
		return false
	}
	// A sidecar serving a different model is not interchangeable.
	if st.ModelPath != "" && m.cfg.ModelPath != "" && st.ModelPath != m.cfg.ModelPath {
		return false
	}
	return probeHealth(ctx, st.Port) == nil
}

// reapLocked cleans up a dead or unusable sidecar. Callers must hold the lock.
func (m *Manager) reapLocked(st *sidecarState) {
	if st == nil {
		return
	}
	if st.PID > 0 && processAlive(st.PID) {
		m.logger.Info().Int("pid", st.PID).Int("port", st.Port).Msg("terminating stale sidecar")
		if err := terminateProcess(st.PID, terminateGrace); err != nil {
			m.logger.Warn().Err(err).Int("pid", st.PID).Msg("failed to terminate stale sidecar")
		}
	}
	if err := os.Remove(m.statePath); err != nil && !os.IsNotExist(err) {
		m.logger.Warn().Err(err).Msg("failed to remove sidecar state")
	}
}

// Ensure guarantees a healthy sidecar is running and returns its loopback
// endpoint. It is safe to call concurrently and from multiple processes: at
// most one sidecar is spawned per machine per model.
func (m *Manager) Ensure(ctx context.Context) (string, error) {
	m.mu.Lock()
	if m.endpoint != "" && time.Since(m.lastHealthy) < healthCacheTTL {
		ep := m.endpoint
		m.mu.Unlock()
		return ep, nil
	}
	m.mu.Unlock()

	// Fast path: an instance started by any process may already be healthy.
	if st, err := m.readState(); err == nil && m.stateUsable(ctx, st) {
		m.markHealthy(st.Port)
		m.startReaper()
		return endpointFor(st.Port), nil
	}

	lockCtx, cancel := context.WithTimeout(ctx, lockWaitTimeout)
	defer cancel()
	lock, err := acquireLock(lockCtx, m.lockPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = lock.release() }()

	// Re-check under the lock: another process may have won the race.
	st, _ := m.readState()
	if m.stateUsable(ctx, st) {
		m.markHealthy(st.Port)
		m.startReaper()
		return endpointFor(st.Port), nil
	}

	// Stale-state recovery: the recorded pid is dead, or the process is alive
	// but not answering. Either way, clean up before respawning.
	m.reapLocked(st)

	port, err := m.spawnLocked(ctx)
	if err != nil {
		return "", err
	}

	m.markHealthy(port)
	m.startReaper()
	return endpointFor(port), nil
}

// markHealthy records a successful health observation.
func (m *Manager) markHealthy(port int) {
	m.mu.Lock()
	m.endpoint = endpointFor(port)
	m.lastHealthy = time.Now()
	m.mu.Unlock()
}

// allocatePort reserves an ephemeral loopback port.
//
// The listener is closed before the child binds, so there is a small race
// window; callers retry on spawn failure.
func allocatePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("embed: allocate port: %w", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	if err := l.Close(); err != nil {
		return 0, fmt.Errorf("embed: release probe listener: %w", err)
	}
	return port, nil
}

// buildArgs constructs the llama-server command line.
//
// Notes on the embedding-specific flags:
//   - --embedding puts the server in embedding mode and enables /v1/embeddings.
//   - --pooling is passed explicitly; the wrong pooling mode is the single
//     most common cause of silently degraded embedding quality in llama.cpp.
//   - -b and -ub are raised to the context size. For non-causal pooling the
//     physical batch must be able to hold the whole sequence, otherwise
//     llama-server rejects longer inputs.
//   - --host is pinned to 127.0.0.1: the sidecar must never be reachable off
//     the loopback interface.
func (m *Manager) buildArgs(port int, modelPath string) []string {
	ctxSize := strconv.Itoa(m.cfg.ContextSize)
	args := []string{
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"-m", modelPath,
		"--embedding",
		"--pooling", m.cfg.Pooling,
		"-c", ctxSize,
		"-b", ctxSize,
		"-ub", ctxSize,
	}
	return append(args, m.cfg.ExtraArgs...)
}

// spawnLocked starts a new sidecar. Callers must hold the spawn lock.
func (m *Manager) spawnLocked(ctx context.Context) (int, error) {
	binary, err := FindLlamaServer(m.cfg.BinaryPath)
	if err != nil {
		return 0, err
	}
	modelPath, err := m.resolveModel()
	if err != nil {
		return 0, err
	}

	port, err := allocatePort()
	if err != nil {
		return 0, err
	}

	args := m.buildArgs(port, modelPath)

	// The sidecar deliberately outlives this context: it is a shared singleton
	// owned by the machine, not by the caller that happened to trigger the
	// spawn. Lifetime is bounded by idle shutdown and explicit Shutdown.
	cmd := exec.Command(binary, args...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	setProcessGroup(cmd)

	tail := newTailBuffer(stderrTailBytes)
	cmd.Stderr = tail

	if err := cmd.Start(); err != nil {
		return 0, fmt.Errorf("embed: start %s: %w", binary, err)
	}
	pid := cmd.Process.Pid

	m.logger.Info().
		Str("binary", binary).
		Str("model", m.cfg.ModelID).
		Int("pid", pid).
		Int("port", port).
		Msg("spawned embedding sidecar")

	// Reap the child so it never becomes a zombie. The sidecar is expected to
	// outlive this call, so the result is only used for early-exit detection.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	if err := m.waitHealthy(ctx, port, pid, exited, tail); err != nil {
		if terr := terminateProcess(pid, terminateGrace); terr != nil {
			m.logger.Warn().Err(terr).Int("pid", pid).Msg("failed to clean up failed sidecar")
		}
		return 0, err
	}

	st := &sidecarState{
		PID:        pid,
		Port:       port,
		ModelID:    m.cfg.ModelID,
		ModelPath:  modelPath,
		BinaryPath: binary,
		Dimensions: m.cfg.Dimensions,
		StartedAt:  time.Now(),
		LastUsed:   time.Now(),
	}
	if err := m.writeState(st); err != nil {
		if terr := terminateProcess(pid, terminateGrace); terr != nil {
			m.logger.Warn().Err(terr).Int("pid", pid).Msg("failed to clean up sidecar after state write failure")
		}
		return 0, err
	}

	return port, nil
}

// waitHealthy polls /health until the sidecar is ready, the process exits, or
// the startup budget is exhausted.
func (m *Manager) waitHealthy(ctx context.Context, port, pid int, exited <-chan error, tail *tailBuffer) error {
	deadline := time.Now().Add(m.cfg.StartupTimeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case werr := <-exited:
			return fmt.Errorf("embed: sidecar (pid %d) exited during startup: %v; stderr tail:\n%s",
				pid, werr, tail.String())
		case <-ctx.Done():
			return fmt.Errorf("embed: cancelled while waiting for sidecar: %w", ctx.Err())
		case <-ticker.C:
		}

		if probeHealth(ctx, port) == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("embed: sidecar did not become healthy within %s; stderr tail:\n%s",
				m.cfg.StartupTimeout, tail.String())
		}
	}
}

// Touch refreshes the shared last-used timestamp so the idle reaper does not
// shut down a sidecar another process is actively using. Writes are throttled.
func (m *Manager) Touch() {
	m.mu.Lock()
	if time.Since(m.lastTouch) < m.touchEvery {
		m.mu.Unlock()
		return
	}
	m.lastTouch = time.Now()
	m.mu.Unlock()

	st, err := m.readState()
	if err != nil || st == nil {
		return
	}
	st.LastUsed = time.Now()
	if err := m.writeState(st); err != nil {
		m.logger.Debug().Err(err).Msg("failed to refresh sidecar last-used timestamp")
	}
}

// startReaper launches the idle-shutdown goroutine once per manager.
func (m *Manager) startReaper() {
	if m.cfg.IdleTimeout < 0 {
		return
	}
	m.reaperOnce.Do(func() {
		interval := m.cfg.IdleTimeout / 4
		if interval > 30*time.Second {
			interval = 30 * time.Second
		}
		if interval < 100*time.Millisecond {
			interval = 100 * time.Millisecond
		}
		go m.reapLoop(interval)
	})
}

// reapLoop periodically shuts down a sidecar that has gone globally idle.
func (m *Manager) reapLoop(interval time.Duration) {
	defer close(m.reaperDone)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.reaperStop:
			return
		case <-ticker.C:
			m.reapIfIdle()
		}
	}
}

// reapIfIdle shuts down the sidecar when the shared last-used timestamp is
// older than the idle window.
func (m *Manager) reapIfIdle() {
	st, err := m.readState()
	if err != nil || st == nil {
		return
	}
	if time.Since(st.LastUsed) < m.cfg.IdleTimeout {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lock, err := acquireLock(ctx, m.lockPath)
	if err != nil {
		return
	}
	defer func() { _ = lock.release() }()

	// Re-read under the lock: another process may have used it since.
	st, err = m.readState()
	if err != nil || st == nil {
		return
	}
	if time.Since(st.LastUsed) < m.cfg.IdleTimeout {
		return
	}

	m.logger.Info().
		Int("pid", st.PID).
		Dur("idle_for", time.Since(st.LastUsed)).
		Msg("shutting down idle embedding sidecar")

	m.reapLocked(st)

	m.mu.Lock()
	m.endpoint = ""
	m.lastHealthy = time.Time{}
	if m.provider != nil {
		_ = m.provider.Close()
		m.provider = nil
	}
	m.mu.Unlock()
}

// Shutdown stops the shared sidecar and removes its state. It affects every
// conduit process on the machine, so it is intended for explicit teardown
// (`conduit stop`, uninstall, tests) rather than routine cleanup.
func (m *Manager) Shutdown(ctx context.Context) error {
	lockCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	lock, err := acquireLock(lockCtx, m.lockPath)
	if err != nil {
		return err
	}
	defer func() { _ = lock.release() }()

	st, err := m.readState()
	if err != nil {
		return err
	}
	m.reapLocked(st)

	m.mu.Lock()
	m.endpoint = ""
	m.lastHealthy = time.Time{}
	if m.provider != nil {
		_ = m.provider.Close()
		m.provider = nil
	}
	m.mu.Unlock()

	if st != nil && st.PID > 0 && processAlive(st.PID) {
		return fmt.Errorf("embed: sidecar pid %d still running after shutdown", st.PID)
	}
	return nil
}

// Close stops this manager's background goroutines. It deliberately leaves the
// shared sidecar running for other processes; use Shutdown to stop it.
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		close(m.reaperStop)
	})
	m.mu.Lock()
	if m.provider != nil {
		_ = m.provider.Close()
		m.provider = nil
	}
	m.mu.Unlock()
	return nil
}

// StatePath returns the path of the shared state file (useful for diagnostics).
func (m *Manager) StatePath() string { return m.statePath }

// Provider returns a Provider backed by the managed sidecar. The sidecar is
// started on first use and restarted automatically if it has gone away.
func (m *Manager) Provider(ctx context.Context) (Provider, error) {
	if _, err := m.Ensure(ctx); err != nil {
		return nil, err
	}
	return &managedProvider{mgr: m}, nil
}

// managedProvider binds a LlamaServerProvider to the sidecar lifecycle: each
// call re-Ensures (cheaply, via the health cache) so an idle-shutdown sidecar
// is transparently respawned.
type managedProvider struct {
	mgr *Manager
}

var _ Provider = (*managedProvider)(nil)

// current returns a provider pointed at the live sidecar endpoint.
func (mp *managedProvider) current(ctx context.Context) (*LlamaServerProvider, error) {
	endpoint, err := mp.mgr.Ensure(ctx)
	if err != nil {
		return nil, err
	}

	mp.mgr.mu.Lock()
	defer mp.mgr.mu.Unlock()
	if mp.mgr.provider != nil && mp.mgr.provider.baseURL == endpoint {
		return mp.mgr.provider, nil
	}
	if mp.mgr.provider != nil {
		_ = mp.mgr.provider.Close()
	}
	p, err := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL:     endpoint,
		Model:       mp.mgr.cfg.ModelID,
		Dimensions:  mp.mgr.cfg.Dimensions,
		Timeout:     mp.mgr.cfg.Timeout,
		BatchSize:   mp.mgr.cfg.BatchSize,
		QueryPrefix: mp.mgr.cfg.QueryPrefix,
		DocPrefix:   mp.mgr.cfg.DocPrefix,
		InputSuffix: mp.mgr.cfg.InputSuffix,
		Logger:      &mp.mgr.logger,
	})
	if err != nil {
		return nil, err
	}
	mp.mgr.provider = p
	return p, nil
}

// Embed implements Provider.
func (mp *managedProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	p, err := mp.current(ctx)
	if err != nil {
		return nil, err
	}
	mp.mgr.Touch()
	return p.Embed(ctx, texts)
}

// EmbedQuery embeds queries with the model's query prefix applied.
func (mp *managedProvider) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	p, err := mp.current(ctx)
	if err != nil {
		return nil, err
	}
	mp.mgr.Touch()
	return p.EmbedQuery(ctx, texts)
}

// Dimensions implements Provider.
func (mp *managedProvider) Dimensions() int {
	mp.mgr.mu.Lock()
	p := mp.mgr.provider
	mp.mgr.mu.Unlock()
	if p != nil {
		if d := p.Dimensions(); d > 0 {
			return d
		}
	}
	return mp.mgr.cfg.Dimensions
}

// ModelID implements Provider.
func (mp *managedProvider) ModelID() string { return mp.mgr.cfg.ModelID }

// Health implements Provider.
func (mp *managedProvider) Health(ctx context.Context) error {
	p, err := mp.current(ctx)
	if err != nil {
		return err
	}
	return p.Health(ctx)
}

// Close implements Provider. It releases the HTTP client but leaves the shared
// sidecar running; use Manager.Shutdown to stop the process.
func (mp *managedProvider) Close() error { return nil }

// tailBuffer retains the last n bytes written to it. Used to surface the tail
// of a failed sidecar's stderr without buffering unbounded log output.
type tailBuffer struct {
	mu   sync.Mutex
	buf  []byte
	size int
}

func newTailBuffer(size int) *tailBuffer {
	return &tailBuffer{size: size}
}

// Write implements io.Writer, discarding all but the trailing size bytes.
func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > t.size {
		t.buf = t.buf[len(t.buf)-t.size:]
	}
	return len(p), nil
}

// String returns the retained tail.
func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// IsBinaryMissing reports whether err indicates a missing llama-server binary.
func IsBinaryMissing(err error) bool { return errors.Is(err, ErrBinaryNotFound) }

// IsModelMissing reports whether err indicates a missing GGUF model file.
func IsModelMissing(err error) bool { return errors.Is(err, ErrModelNotFound) }
