package embed

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSidecarOnce guards the one-time compile of the fake sidecar binary.
var (
	fakeSidecarOnce sync.Once
	fakeSidecarPath string
	fakeSidecarErr  error
)

// buildFakeSidecar compiles testdata/fakesidecar into a temp binary shared by
// every test in the package. It replaces llama-server so the whole lifecycle
// can be exercised hermetically, with no llama.cpp installed.
func buildFakeSidecar(t *testing.T) string {
	t.Helper()

	fakeSidecarOnce.Do(func() {
		outDir, err := os.MkdirTemp("", "conduit-fake-sidecar-")
		if err != nil {
			fakeSidecarErr = err
			return
		}
		name := "fakesidecar"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		out := filepath.Join(outDir, name)

		src := filepath.Join("testdata", "fakesidecar", "main.go")
		cmd := exec.Command("go", "build", "-o", out, src)
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if buildOut, err := cmd.CombinedOutput(); err != nil {
			fakeSidecarErr = errors.New("building fake sidecar: " + err.Error() + "\n" + string(buildOut))
			return
		}
		fakeSidecarPath = out
	})

	if fakeSidecarErr != nil {
		t.Fatalf("could not build fake sidecar: %v", fakeSidecarErr)
	}
	return fakeSidecarPath
}

// newTestManager wires a manager onto the fake binary and a dummy model file.
func newTestManager(t *testing.T, mutate func(*ManagerConfig)) *Manager {
	t.Helper()

	dataDir := t.TempDir()
	modelPath := filepath.Join(dataDir, "model.gguf")
	if err := os.WriteFile(modelPath, []byte("not a real gguf"), 0o600); err != nil {
		t.Fatalf("write dummy model: %v", err)
	}

	cfg := ManagerConfig{
		DataDir:        dataDir,
		BinaryPath:     buildFakeSidecar(t),
		ModelPath:      modelPath,
		ModelID:        "fake-model",
		Dimensions:     8,
		StartupTimeout: 20 * time.Second,
		Timeout:        5 * time.Second,
		IdleTimeout:    -1, // reaper off unless a test opts in
	}
	if mutate != nil {
		mutate(&cfg)
	}

	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = m.Shutdown(ctx)
		_ = m.Close()
	})
	return m
}

// statePID reads the pid recorded in the shared state file.
func statePID(t *testing.T, m *Manager) int {
	t.Helper()
	st, err := m.readState()
	if err != nil {
		t.Fatalf("readState: %v", err)
	}
	if st == nil {
		t.Fatal("expected sidecar state to exist")
	}
	return st.PID
}

func TestManager_SpawnsAndServesEmbeddings(t *testing.T) {
	m := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint, err := m.Ensure(ctx)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatalf("endpoint %q is not bound to loopback", endpoint)
	}

	st, err := m.readState()
	if err != nil || st == nil {
		t.Fatalf("state after Ensure: %v %v", st, err)
	}
	if st.PID <= 0 || !processAlive(st.PID) {
		t.Fatalf("sidecar pid %d is not alive", st.PID)
	}
	if st.ModelID != "fake-model" {
		t.Errorf("state ModelID = %q, want fake-model", st.ModelID)
	}

	p, err := m.Provider(ctx)
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	vecs, err := p.Embed(ctx, []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Embed via managed provider: %v", err)
	}
	if len(vecs) != 2 || len(vecs[0]) != 8 {
		t.Fatalf("got %d vectors of width %d, want 2 x 8", len(vecs), len(vecs[0]))
	}
	if p.ModelID() != "fake-model" {
		t.Errorf("ModelID() = %q", p.ModelID())
	}
	if err := p.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}
}

// TestManager_SecondClientReusesRunningSidecar is the singleton guarantee: a
// second manager over the same data dir must discover and reuse the running
// instance rather than spawning a duplicate.
func TestManager_SecondClientReusesRunningSidecar(t *testing.T) {
	first := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	endpoint1, err := first.Ensure(ctx)
	if err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	pid1 := statePID(t, first)

	// A distinct Manager over the same data dir stands in for a second
	// conduit process.
	second, err := NewManager(ManagerConfig{
		DataDir:        first.cfg.DataDir,
		BinaryPath:     first.cfg.BinaryPath,
		ModelPath:      first.cfg.ModelPath,
		ModelID:        first.cfg.ModelID,
		Dimensions:     8,
		StartupTimeout: 20 * time.Second,
		IdleTimeout:    -1,
	})
	if err != nil {
		t.Fatalf("second NewManager: %v", err)
	}
	defer func() { _ = second.Close() }()

	endpoint2, err := second.Ensure(ctx)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}

	if endpoint1 != endpoint2 {
		t.Errorf("second client got endpoint %q, want the shared %q", endpoint2, endpoint1)
	}
	if pid2 := statePID(t, second); pid2 != pid1 {
		t.Errorf("second client spawned a duplicate sidecar (pid %d vs %d)", pid2, pid1)
	}
}

// TestManager_ConcurrentEnsureSpawnsOnce proves the lock serialises spawning
// when many goroutines race to start the sidecar.
func TestManager_ConcurrentEnsureSpawnsOnce(t *testing.T) {
	m := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	const n = 8
	endpoints := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			endpoints[i], errs[i] = m.Ensure(ctx)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("Ensure[%d]: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if endpoints[i] != endpoints[0] {
			t.Fatalf("endpoint[%d] = %q, want %q; concurrent Ensure spawned duplicates",
				i, endpoints[i], endpoints[0])
		}
	}
}

// TestManager_StalePIDRecovery covers the crash case: the state file survives
// but the process it names is gone. The manager must clean up and respawn.
func TestManager_StalePIDRecovery(t *testing.T) {
	m := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("initial Ensure: %v", err)
	}
	originalPID := statePID(t, m)

	// Simulate a crash: kill the sidecar behind the manager's back, leaving
	// the state file pointing at a dead pid.
	if err := terminateProcess(originalPID, terminateGrace); err != nil {
		t.Fatalf("kill sidecar: %v", err)
	}
	if processAlive(originalPID) {
		t.Fatalf("pid %d still alive after kill", originalPID)
	}
	if _, err := os.Stat(m.StatePath()); err != nil {
		t.Fatalf("state file should still exist after a crash: %v", err)
	}

	// Clear the in-memory health cache the way a fresh process would see it.
	m.mu.Lock()
	m.endpoint = ""
	m.lastHealthy = time.Time{}
	m.mu.Unlock()

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure after stale pid: %v", err)
	}
	newPID := statePID(t, m)
	if newPID == originalPID {
		t.Fatal("manager did not respawn after stale pid")
	}
	if !processAlive(newPID) {
		t.Fatalf("respawned pid %d is not alive", newPID)
	}
}

// TestManager_CorruptStateRecovery covers a truncated or garbage state file.
func TestManager_CorruptStateRecovery(t *testing.T) {
	m := newTestManager(t, nil)

	if err := os.WriteFile(m.StatePath(), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure with corrupt state: %v", err)
	}
	if pid := statePID(t, m); !processAlive(pid) {
		t.Fatalf("pid %d not alive after recovery", pid)
	}
}

// TestManager_ShutdownLeavesNoOrphans proves the whole process group dies:
// the sidecar spawns a grandchild, and both must be gone after Shutdown.
func TestManager_ShutdownLeavesNoOrphans(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group semantics differ on Windows")
	}

	childPidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	m := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Point the fake at a pid file so it forks a long-lived grandchild.
	t.Setenv(envSpawnChildTest, childPidFile)

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	sidecarPID := statePID(t, m)

	grandchildPID := waitForPIDFile(t, childPidFile, 10*time.Second)
	if !processAlive(grandchildPID) {
		t.Fatalf("grandchild %d never started", grandchildPID)
	}

	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	if processAlive(sidecarPID) {
		t.Errorf("sidecar pid %d survived Shutdown", sidecarPID)
	}
	// The grandchild is the orphan check: killing only the direct child would
	// leave this process running forever.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && processAlive(grandchildPID) {
		time.Sleep(50 * time.Millisecond)
	}
	if processAlive(grandchildPID) {
		t.Errorf("grandchild pid %d was orphaned by Shutdown", grandchildPID)
	}

	if _, err := os.Stat(m.StatePath()); !os.IsNotExist(err) {
		t.Errorf("state file still present after Shutdown: %v", err)
	}
}

// TestManager_IdleShutdown proves an unused sidecar is reaped and that a
// later call transparently respawns it.
func TestManager_IdleShutdown(t *testing.T) {
	m := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.IdleTimeout = 400 * time.Millisecond
	})

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	pid := statePID(t, m)
	if !processAlive(pid) {
		t.Fatalf("pid %d not alive after Ensure", pid)
	}

	// Wait for the reaper to notice the idle window has passed.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(m.StatePath()); os.IsNotExist(err) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if _, err := os.Stat(m.StatePath()); !os.IsNotExist(err) {
		t.Fatal("idle sidecar was not reaped within 20s")
	}
	if processAlive(pid) {
		t.Errorf("idle sidecar pid %d is still running", pid)
	}

	// A later request must bring it back.
	p, err := m.Provider(ctx)
	if err != nil {
		t.Fatalf("Provider after idle shutdown: %v", err)
	}
	vecs, err := p.Embed(ctx, []string{"wake up"})
	if err != nil {
		t.Fatalf("Embed after idle shutdown: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("got %d vectors, want 1", len(vecs))
	}
	if newPID := statePID(t, m); newPID == pid {
		t.Error("expected a fresh pid after respawn")
	}
}

// TestManager_TouchKeepsSidecarAlive proves an actively-used sidecar is not
// reaped, which is the cross-process safety property of the shared timestamp.
func TestManager_TouchKeepsSidecarAlive(t *testing.T) {
	m := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.IdleTimeout = 2 * time.Second
	})

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	p, err := m.Provider(ctx)
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	pid := statePID(t, m)

	// Keep using it for longer than the idle window.
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := p.Embed(ctx, []string{"still here"}); err != nil {
			t.Fatalf("Embed during activity: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if !processAlive(pid) {
		t.Error("actively used sidecar was reaped as idle")
	}
	if newPID := statePID(t, m); newPID != pid {
		t.Errorf("sidecar was restarted (pid %d -> %d) despite continuous use", pid, newPID)
	}
}

// TestManager_SidecarExitDuringStartup surfaces a clear error, and does not
// leave a state file behind.
func TestManager_SidecarExitDuringStartup(t *testing.T) {
	m := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.StartupTimeout = 5 * time.Second
	})

	t.Setenv(envExitCodeTest, "3")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	_, err := m.Ensure(ctx)
	if err == nil {
		t.Fatal("expected Ensure to fail when the sidecar exits at startup")
	}
	if !strings.Contains(err.Error(), "exited during startup") {
		t.Errorf("error = %v, want it to mention startup exit", err)
	}
	if _, statErr := os.Stat(m.StatePath()); !os.IsNotExist(statErr) {
		t.Error("state file written despite a failed spawn")
	}
}

// TestManager_SlowStartupStillSucceeds covers a model that takes a while to
// load: the binary starts fast, health only arrives later.
func TestManager_SlowStartupStillSucceeds(t *testing.T) {
	m := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.StartupTimeout = 20 * time.Second
	})

	t.Setenv(envStartupDelayTest, "700ms")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure with slow startup: %v", err)
	}
	if pid := statePID(t, m); !processAlive(pid) {
		t.Fatal("sidecar not alive after slow startup")
	}
}

// TestManager_StartupTimeoutCleansUp proves a sidecar that never becomes
// healthy is killed rather than leaked.
func TestManager_StartupTimeoutCleansUp(t *testing.T) {
	m := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.StartupTimeout = 700 * time.Millisecond
	})

	t.Setenv(envStartupDelayTest, "30s")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := m.Ensure(ctx)
	if err == nil {
		t.Fatal("expected Ensure to time out")
	}
	if !strings.Contains(err.Error(), "did not become healthy") {
		t.Errorf("error = %v, want a health-timeout message", err)
	}
	if _, statErr := os.Stat(m.StatePath()); !os.IsNotExist(statErr) {
		t.Error("state file written despite failed startup")
	}
}

func TestManager_BuildArgs(t *testing.T) {
	t.Parallel()

	m := &Manager{cfg: ManagerConfig{ContextSize: 2048, Pooling: "mean", ExtraArgs: []string{"--verbose"}}}
	args := m.buildArgs(9999, "/models/x.gguf")
	joined := strings.Join(args, " ")

	for _, want := range []string{
		"--host 127.0.0.1",
		"--port 9999",
		"-m /models/x.gguf",
		"--embedding",
		"--pooling mean",
		"-c 2048",
		"-b 2048",
		"-ub 2048",
		"--verbose",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q missing %q", joined, want)
		}
	}

	// The sidecar must never be exposed beyond loopback.
	for _, bad := range []string{"0.0.0.0", "::", "--host localhost"} {
		if strings.Contains(joined, bad) {
			t.Errorf("args contain non-loopback binding %q: %s", bad, joined)
		}
	}
}

func TestFindLlamaServer_MissingBinaryHasInstallHint(t *testing.T) {
	t.Parallel()

	_, err := FindLlamaServer(filepath.Join(t.TempDir(), "nope", "llama-server"))
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound", err)
	}
	if !IsBinaryMissing(err) {
		t.Error("IsBinaryMissing did not recognise the error")
	}
	if !strings.Contains(err.Error(), "brew install llama.cpp") {
		t.Errorf("error lacks an actionable install hint: %v", err)
	}
}

func TestFindLlamaServer_RejectsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute bits are not modelled the same way on Windows")
	}
	t.Parallel()

	path := filepath.Join(t.TempDir(), "llama-server")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := FindLlamaServer(path)
	if !errors.Is(err, ErrBinaryNotFound) {
		t.Fatalf("err = %v, want ErrBinaryNotFound for a non-executable file", err)
	}
}

func TestFindLlamaServer_AcceptsOverride(t *testing.T) {
	t.Parallel()

	bin := buildFakeSidecar(t)
	got, err := FindLlamaServer(bin)
	if err != nil {
		t.Fatalf("FindLlamaServer(%q): %v", bin, err)
	}
	if got != bin {
		t.Errorf("got %q, want %q", got, bin)
	}
}

func TestManager_MissingModelHasActionableError(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	m, err := NewManager(ManagerConfig{
		DataDir:    dataDir,
		BinaryPath: buildFakeSidecar(t),
		ModelPath:  filepath.Join(dataDir, "absent.gguf"),
		ModelID:    "nomic-embed-text-v1.5",
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	_, err = m.resolveModel()
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
	if !IsModelMissing(err) {
		t.Error("IsModelMissing did not recognise the error")
	}
	if !strings.Contains(err.Error(), "absent.gguf") {
		t.Errorf("error does not name the expected path: %v", err)
	}
	if !strings.Contains(err.Error(), "conduit model download") {
		t.Errorf("error does not point at the download command: %v", err)
	}
}

func TestManager_EmptyModelPathIsActionable(t *testing.T) {
	t.Parallel()

	m, err := NewManager(ManagerConfig{DataDir: t.TempDir(), ModelID: "nomic-embed-text-v1.5"})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer func() { _ = m.Close() }()

	_, err = m.resolveModel()
	if !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("err = %v, want ErrModelNotFound", err)
	}
}

func TestNewManager_RequiresDataDir(t *testing.T) {
	t.Parallel()
	if _, err := NewManager(ManagerConfig{}); err == nil {
		t.Fatal("expected an error when DataDir is empty")
	}
}

func TestManager_ShutdownIsIdempotent(t *testing.T) {
	m := newTestManager(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := m.Ensure(ctx); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("first Shutdown: %v", err)
	}
	if err := m.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown should be a no-op: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close must be idempotent: %v", err)
	}
}

func TestAllocatePort_ReturnsUsableLoopbackPort(t *testing.T) {
	t.Parallel()

	port, err := allocatePort()
	if err != nil {
		t.Fatalf("allocatePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("port %d out of range", port)
	}
	if got := endpointFor(port); got != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Errorf("endpointFor(%d) = %q", port, got)
	}
}

func TestTailBuffer_RetainsOnlyTail(t *testing.T) {
	t.Parallel()

	tb := newTailBuffer(8)
	if _, err := tb.Write([]byte("0123456789ABCDEF")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := tb.String(); got != "89ABCDEF" {
		t.Errorf("tail = %q, want %q", got, "89ABCDEF")
	}
}

func TestLockfile_IsMutuallyExclusive(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "test.lock")

	lock, err := acquireLock(context.Background(), path)
	if err != nil {
		t.Fatalf("acquireLock: %v", err)
	}

	// A second acquisition must block until the first is released.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if _, err := acquireLock(ctx, path); err == nil {
		t.Fatal("second acquireLock succeeded while the lock was held")
	}

	if err := lock.release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	lock2, err := acquireLock(ctx2, path)
	if err != nil {
		t.Fatalf("acquireLock after release: %v", err)
	}
	if err := lock2.release(); err != nil {
		t.Fatalf("release: %v", err)
	}
}

func TestProcessAlive_DeadPID(t *testing.T) {
	t.Parallel()

	if processAlive(0) || processAlive(-1) {
		t.Error("processAlive should reject non-positive pids")
	}
	// Spawn and reap a short-lived process, then confirm it reads as dead.
	cmd := exec.Command("go", "version")
	if err := cmd.Run(); err != nil {
		t.Skipf("could not run helper process: %v", err)
	}
	if processAlive(cmd.Process.Pid) {
		// The pid could in principle be recycled; only fail if clearly wrong.
		t.Logf("pid %d reported alive after exit (possible pid reuse)", cmd.Process.Pid)
	}
}

// waitForPIDFile polls until the fake sidecar has recorded its grandchild pid.
func waitForPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil {
			if pid, convErr := strconv.Atoi(strings.TrimSpace(string(raw))); convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("pid file %s never appeared", path)
	return 0
}

// Env var names honoured by the fake sidecar, mirrored here so the test and
// the helper cannot drift apart silently.
const (
	envStartupDelayTest = "FAKE_SIDECAR_STARTUP_DELAY"
	envExitCodeTest     = "FAKE_SIDECAR_EXIT_CODE"
	envSpawnChildTest   = "FAKE_SIDECAR_CHILD_PIDFILE"
)
