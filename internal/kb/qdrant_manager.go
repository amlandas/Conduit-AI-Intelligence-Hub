// Package kb provides knowledge base functionality.
package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// QdrantManager handles Qdrant container lifecycle and health management.
// It provides a managed experience ensuring storage directories exist,
// the container is healthy, and collections are in good state.
type QdrantManager struct {
	dataDir        string
	storageDir     string
	containerName  string
	httpPort       int
	grpcPort       int
	logger         zerolog.Logger
	containerCmd   string // "docker" or "podman"
	collectionName string
}

// QdrantConfig configures the Qdrant manager.
type QdrantConfig struct {
	DataDir        string // Base data directory (default: ~/.conduit)
	ContainerName  string // Container name (default: conduit-qdrant)
	HTTPPort       int    // HTTP port (default: 6333)
	GRPCPort       int    // gRPC port (default: 6334)
	CollectionName string // Collection name (default: conduit_kb)
}

// QdrantHealth represents the health status of Qdrant.
type QdrantHealth struct {
	ContainerRunning bool   `json:"container_running"`
	APIReachable     bool   `json:"api_reachable"`
	CollectionStatus string `json:"collection_status"` // "green", "yellow", "red", "missing"
	IndexedVectors   int64  `json:"indexed_vectors"`
	TotalPoints      int64  `json:"total_points"`
	Error            string `json:"error,omitempty"`
	NeedsRecovery    bool   `json:"needs_recovery"`
}

// NewQdrantManager creates a new Qdrant manager.
func NewQdrantManager(cfg QdrantConfig) *QdrantManager {
	if cfg.DataDir == "" {
		homeDir, _ := os.UserHomeDir()
		cfg.DataDir = filepath.Join(homeDir, ".conduit")
	}
	if cfg.ContainerName == "" {
		cfg.ContainerName = "conduit-qdrant"
	}
	if cfg.HTTPPort == 0 {
		cfg.HTTPPort = 6333
	}
	if cfg.GRPCPort == 0 {
		cfg.GRPCPort = 6334
	}
	if cfg.CollectionName == "" {
		cfg.CollectionName = DefaultCollectionName
	}

	return &QdrantManager{
		dataDir:        cfg.DataDir,
		storageDir:     filepath.Join(cfg.DataDir, "qdrant"),
		containerName:  cfg.ContainerName,
		httpPort:       cfg.HTTPPort,
		grpcPort:       cfg.GRPCPort,
		collectionName: cfg.CollectionName,
		logger:         observability.Logger("kb.qdrant"),
	}
}

// EnsureReady ensures Qdrant is ready for use.
// This is the main entry point that handles all aspects of Qdrant readiness:
// 1. Ensures storage directory exists
// 2. Detects container runtime (Docker/Podman)
// 3. Ensures container is running
// 4. Checks API reachability
// 5. Validates collection health
// 6. Attempts recovery if needed
func (m *QdrantManager) EnsureReady(ctx context.Context) error {
	m.logger.Info().Msg("ensuring Qdrant is ready")

	// Step 1: Ensure storage directory exists
	if err := m.ensureStorageDir(); err != nil {
		return fmt.Errorf("ensure storage directory: %w", err)
	}

	// Step 2: Detect container runtime
	if err := m.detectContainerRuntime(); err != nil {
		m.logger.Warn().Err(err).Msg("no container runtime found, semantic search disabled")
		return nil // Not an error - semantic search is optional
	}

	// Step 3: Ensure container is running
	if err := m.ensureContainerRunning(ctx); err != nil {
		return fmt.Errorf("ensure container running: %w", err)
	}

	// Step 4: Wait for API to be reachable
	if err := m.waitForAPI(ctx, 30*time.Second); err != nil {
		return fmt.Errorf("wait for Qdrant API: %w", err)
	}

	// Step 5: Check and recover collection if needed
	health := m.CheckHealth(ctx)
	if health.NeedsRecovery {
		m.logger.Warn().
			Str("status", health.CollectionStatus).
			Str("error", health.Error).
			Msg("collection needs recovery")

		if err := m.recoverCollection(ctx); err != nil {
			return fmt.Errorf("recover collection: %w", err)
		}
	}

	m.logger.Info().Msg("Qdrant is ready")
	return nil
}

// ensureStorageDir creates the Qdrant storage directory structure if it doesn't exist.
func (m *QdrantManager) ensureStorageDir() error {
	// Create the main storage directory and subdirectories that Qdrant expects
	// This prevents "No such file or directory" errors when Qdrant tries to create collections
	dirs := []string{
		m.storageDir,
		filepath.Join(m.storageDir, "collections"),
		filepath.Join(m.storageDir, "snapshots"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	m.logger.Debug().Str("path", m.storageDir).Msg("storage directory structure ready")
	return nil
}

// detectContainerRuntime finds an available container runtime.
// Uses findBinaryPath to locate binaries in known paths (not just PATH).
func (m *QdrantManager) detectContainerRuntime() error {
	// Check for podman first (preferred on macOS with Podman machine)
	if podmanPath := findBinaryPath("podman"); podmanPath != "" {
		// On macOS, check if podman machine is running
		if runtime.GOOS == "darwin" {
			out, err := exec.Command(podmanPath, "machine", "list", "--format", "{{.Running}}").Output()
			if err == nil && strings.Contains(string(out), "true") {
				m.containerCmd = podmanPath
				m.logger.Debug().Str("runtime", "podman").Str("path", podmanPath).Msg("using container runtime")
				return nil
			}
		} else {
			m.containerCmd = podmanPath
			m.logger.Debug().Str("runtime", "podman").Str("path", podmanPath).Msg("using container runtime")
			return nil
		}
	}

	// Check for docker
	if dockerPath := findBinaryPath("docker"); dockerPath != "" {
		// Verify Docker daemon is running
		if err := exec.Command(dockerPath, "info").Run(); err == nil {
			m.containerCmd = dockerPath
			m.logger.Debug().Str("runtime", "docker").Str("path", dockerPath).Msg("using container runtime")
			return nil
		}
	}

	return fmt.Errorf("no container runtime available (need Docker or Podman)")
}

// ensureContainerRunning ensures the Qdrant container is running.
func (m *QdrantManager) ensureContainerRunning(ctx context.Context) error {
	// First, check if Qdrant API is already reachable (e.g., from another container or external instance)
	if m.isAPIReachable() {
		m.logger.Info().Int("port", m.httpPort).Msg("Qdrant API already reachable, using existing instance")
		return nil
	}

	// Check if our managed container exists and is running
	running, err := m.isContainerRunning(ctx)
	if err != nil {
		m.logger.Debug().Err(err).Msg("error checking container status")
	}

	if running {
		m.logger.Debug().Str("container", m.containerName).Msg("container already running")
		return nil
	}

	// Check if our managed container exists but is stopped
	exists, err := m.containerExists(ctx)
	if err != nil {
		m.logger.Debug().Err(err).Msg("error checking if container exists")
	}

	if exists {
		// Start the existing container
		m.logger.Info().Str("container", m.containerName).Msg("starting existing container")
		if err := m.runContainerCmd(ctx, "start", m.containerName); err != nil {
			return fmt.Errorf("start container: %w", err)
		}
		return nil
	}

	// Check if port is already in use by something else
	if m.isPortInUse() {
		m.logger.Warn().Int("port", m.httpPort).Msg("port already in use but API not reachable, cannot start Qdrant")
		return fmt.Errorf("port %d already in use by another process", m.httpPort)
	}

	// Create and start new container
	m.logger.Info().Str("container", m.containerName).Msg("creating new Qdrant container")
	return m.createContainer(ctx)
}

// isAPIReachable checks if the Qdrant API is reachable without waiting.
func (m *QdrantManager) isAPIReachable() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/collections", m.httpPort))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// isPortInUse checks if the HTTP port is already bound.
func (m *QdrantManager) isPortInUse() bool {
	client := &http.Client{Timeout: 1 * time.Second}
	_, err := client.Get(fmt.Sprintf("http://localhost:%d/", m.httpPort))
	// If we get any response (even error), port is in use
	// Connection refused means port is free
	return err == nil || !strings.Contains(err.Error(), "connection refused")
}

// createContainer creates a new Qdrant container.
func (m *QdrantManager) createContainer(ctx context.Context) error {
	// Ensure storage directory structure exists before mounting
	// Qdrant needs the collections subdirectory to exist
	collectionsDir := filepath.Join(m.storageDir, "collections")
	if err := os.MkdirAll(collectionsDir, 0755); err != nil {
		return fmt.Errorf("create collections directory: %w", err)
	}
	m.logger.Debug().Str("path", collectionsDir).Msg("ensured collections directory exists")

	// Build volume mount - use :Z for SELinux on Linux only
	// macOS and Windows don't use SELinux and :Z can cause issues with Podman
	volumeMount := fmt.Sprintf("%s:/qdrant/storage", m.storageDir)
	if runtime.GOOS == "linux" {
		volumeMount += ":Z"
	}

	args := []string{
		"run", "-d",
		"--name", m.containerName,
		"-p", fmt.Sprintf("%d:%d", m.httpPort, 6333),
		"-p", fmt.Sprintf("%d:%d", m.grpcPort, 6334),
		"-v", volumeMount,
		"docker.io/qdrant/qdrant:latest",
	}

	m.logger.Debug().
		Str("cmd", m.containerCmd).
		Strs("args", args).
		Msg("creating container")

	if err := m.runContainerCmd(ctx, args...); err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	return nil
}

// isContainerRunning checks if the Qdrant container is running.
func (m *QdrantManager) isContainerRunning(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, m.containerCmd, "ps", "-q", "-f", fmt.Sprintf("name=%s", m.containerName)).Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// containerExists checks if the Qdrant container exists (running or stopped).
func (m *QdrantManager) containerExists(ctx context.Context) (bool, error) {
	out, err := exec.CommandContext(ctx, m.containerCmd, "ps", "-a", "-q", "-f", fmt.Sprintf("name=%s", m.containerName)).Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// waitForAPI waits for the Qdrant API to become reachable.
func (m *QdrantManager) waitForAPI(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(fmt.Sprintf("http://localhost:%d/collections", m.httpPort))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				m.logger.Debug().Msg("Qdrant API is reachable")
				return nil
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("Qdrant API not reachable after %v", timeout)
}

// CheckHealth checks the health of Qdrant and its collection.
func (m *QdrantManager) CheckHealth(ctx context.Context) QdrantHealth {
	health := QdrantHealth{}

	// First check if API is reachable (works regardless of container name)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/collections/%s", m.httpPort, m.collectionName))
	if err != nil {
		// API not reachable - check if our managed container is running
		running, _ := m.isContainerRunning(ctx)
		health.ContainerRunning = running
		if running {
			health.Error = fmt.Sprintf("API error: %v", err)
		} else {
			health.Error = "Qdrant not running"
		}
		return health
	}
	defer resp.Body.Close()

	// API is reachable
	health.ContainerRunning = true // Qdrant is running (may not be our container)
	health.APIReachable = true

	if resp.StatusCode == http.StatusNotFound {
		health.CollectionStatus = "missing"
		return health
	}

	// Parse collection info - optimizer_status can be "ok" (string) or {error: "..."} (object)
	var result struct {
		Result struct {
			Status              string          `json:"status"`
			IndexedVectorsCount int64           `json:"indexed_vectors_count"`
			PointsCount         int64           `json:"points_count"`
			OptimizerStatus     json.RawMessage `json:"optimizer_status"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		health.Error = fmt.Sprintf("parse error: %v", err)
		return health
	}

	health.CollectionStatus = strings.ToLower(result.Result.Status)
	health.IndexedVectors = result.Result.IndexedVectorsCount
	health.TotalPoints = result.Result.PointsCount

	// Check for errors in optimizer status (can be string "ok" or object {error: "..."})
	if len(result.Result.OptimizerStatus) > 0 {
		// Try to parse as object first
		var optimizerObj struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(result.Result.OptimizerStatus, &optimizerObj); err == nil && optimizerObj.Error != "" {
			health.Error = optimizerObj.Error
		}
		// If it's a string like "ok", no error to report
	}

	// Determine if recovery is needed
	health.NeedsRecovery = health.CollectionStatus == "red" ||
		(health.TotalPoints > 0 && health.IndexedVectors == 0 && health.Error != "")

	return health
}

// recoverCollection attempts to recover a corrupted collection.
func (m *QdrantManager) recoverCollection(ctx context.Context) error {
	m.logger.Warn().Str("collection", m.collectionName).Msg("attempting collection recovery")

	// Strategy 1: Try to delete and recreate the collection
	client := &http.Client{Timeout: 30 * time.Second}

	// Delete the corrupted collection
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("http://localhost:%d/collections/%s", m.httpPort, m.collectionName), nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		m.logger.Warn().Err(err).Msg("failed to delete collection, trying container restart")
		return m.restartContainer(ctx)
	}
	resp.Body.Close()

	m.logger.Info().Str("collection", m.collectionName).Msg("deleted corrupted collection")

	// Remove corrupted storage files
	collectionDir := filepath.Join(m.storageDir, "collections", m.collectionName)
	if err := os.RemoveAll(collectionDir); err != nil {
		m.logger.Warn().Err(err).Str("path", collectionDir).Msg("failed to remove collection directory")
	}

	m.logger.Info().Msg("collection recovery complete - run 'conduit kb sync' to re-index documents")
	return nil
}

// restartContainer restarts the Qdrant container.
func (m *QdrantManager) restartContainer(ctx context.Context) error {
	m.logger.Info().Str("container", m.containerName).Msg("restarting container")

	if err := m.runContainerCmd(ctx, "restart", m.containerName); err != nil {
		return fmt.Errorf("restart container: %w", err)
	}

	return m.waitForAPI(ctx, 30*time.Second)
}

// RecreateContainer removes and recreates the Qdrant container.
// This is a more aggressive recovery that clears all data.
func (m *QdrantManager) RecreateContainer(ctx context.Context) error {
	m.logger.Warn().Msg("recreating Qdrant container (all vector data will be lost)")

	// Stop and remove existing container
	_ = m.runContainerCmd(ctx, "stop", m.containerName)
	_ = m.runContainerCmd(ctx, "rm", m.containerName)

	// Clear storage directory
	if err := os.RemoveAll(m.storageDir); err != nil {
		m.logger.Warn().Err(err).Msg("failed to remove storage directory")
	}

	// Recreate storage directory
	if err := m.ensureStorageDir(); err != nil {
		return err
	}

	// Create new container
	return m.createContainer(ctx)
}

// runContainerCmd runs a container command.
func (m *QdrantManager) runContainerCmd(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, m.containerCmd, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %w (output: %s)", m.containerCmd, args, err, string(output))
	}
	return nil
}

// commandExists checks if a command is available.
// knownBinaryPaths maps binary names to common installation locations.
// This is needed because Electron apps don't inherit shell PATH.
var knownBinaryPaths = map[string][]string{
	"podman": {
		"/opt/homebrew/bin/podman",     // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/podman",        // macOS Homebrew (Intel) / Linux
		"/usr/bin/podman",              // System package
	},
	"docker": {
		"/opt/homebrew/bin/docker",                                    // Homebrew
		"/usr/local/bin/docker",                                       // Docker Desktop symlink
		"/usr/bin/docker",                                             // System package
		"/Applications/Docker.app/Contents/Resources/bin/docker",      // App bundle
	},
}

// findBinaryPath finds a binary by checking PATH first, then known installation locations.
// Returns the full path if found, empty string otherwise.
func findBinaryPath(cmd string) string {
	// Check PATH first
	if path, err := exec.LookPath(cmd); err == nil {
		return path
	}

	// Check known installation paths
	if paths, ok := knownBinaryPaths[cmd]; ok {
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	return ""
}

func (m *QdrantManager) commandExists(cmd string) bool {
	return findBinaryPath(cmd) != ""
}

// GetStorageDir returns the Qdrant storage directory path.
func (m *QdrantManager) GetStorageDir() string {
	return m.storageDir
}

// IsAvailable returns true if Qdrant is available (container runtime exists).
func (m *QdrantManager) IsAvailable() bool {
	return m.containerCmd != ""
}

// GetContainerRuntime returns the detected container runtime name (e.g., "docker" or "podman").
// Returns empty string if no runtime is available.
func (m *QdrantManager) GetContainerRuntime() string {
	return m.containerCmd
}

// GetContainerName returns the Qdrant container name.
func (m *QdrantManager) GetContainerName() string {
	return m.containerName
}

// GetPorts returns the HTTP and gRPC ports configured for Qdrant.
func (m *QdrantManager) GetPorts() (httpPort, grpcPort int) {
	return m.httpPort, m.grpcPort
}

// Stop stops the Qdrant container (preserves data).
func (m *QdrantManager) Stop(ctx context.Context) error {
	if m.containerCmd == "" {
		return fmt.Errorf("no container runtime available")
	}

	running, err := m.isContainerRunning(ctx)
	if err != nil {
		return fmt.Errorf("check container status: %w", err)
	}

	if !running {
		m.logger.Info().Str("container", m.containerName).Msg("container is not running")
		return nil
	}

	m.logger.Info().Str("container", m.containerName).Msg("stopping container")
	if err := m.runContainerCmd(ctx, "stop", m.containerName); err != nil {
		return fmt.Errorf("stop container: %w", err)
	}

	m.logger.Info().Str("container", m.containerName).Msg("container stopped")
	return nil
}

// Remove removes the Qdrant container (preserves storage data).
func (m *QdrantManager) Remove(ctx context.Context) error {
	if m.containerCmd == "" {
		return fmt.Errorf("no container runtime available")
	}

	exists, err := m.containerExists(ctx)
	if err != nil {
		return fmt.Errorf("check container exists: %w", err)
	}

	if !exists {
		m.logger.Info().Str("container", m.containerName).Msg("container does not exist")
		return nil
	}

	// Stop first if running
	running, _ := m.isContainerRunning(ctx)
	if running {
		m.logger.Info().Str("container", m.containerName).Msg("stopping container before removal")
		_ = m.runContainerCmd(ctx, "stop", m.containerName)
	}

	m.logger.Info().Str("container", m.containerName).Msg("removing container")
	if err := m.runContainerCmd(ctx, "rm", m.containerName); err != nil {
		return fmt.Errorf("remove container: %w", err)
	}

	m.logger.Info().Str("container", m.containerName).Msg("container removed (storage data preserved)")
	return nil
}

// Install installs and starts Qdrant (pulls image, creates and starts container).
// If a container runtime is provided, it will be used; otherwise auto-detection occurs.
func (m *QdrantManager) Install(ctx context.Context) error {
	// Detect container runtime if not already set
	if m.containerCmd == "" {
		if err := m.detectContainerRuntime(); err != nil {
			return fmt.Errorf("no container runtime: %w", err)
		}
	}

	// Check if already running
	if m.isAPIReachable() {
		m.logger.Info().Int("port", m.httpPort).Msg("Qdrant is already running")
		return nil
	}

	// Ensure storage directory exists
	if err := m.ensureStorageDir(); err != nil {
		return fmt.Errorf("ensure storage directory: %w", err)
	}

	// Remove existing container if any (to ensure clean state)
	exists, _ := m.containerExists(ctx)
	if exists {
		m.logger.Info().Str("container", m.containerName).Msg("removing existing container")
		_ = m.runContainerCmd(ctx, "stop", m.containerName)
		_ = m.runContainerCmd(ctx, "rm", m.containerName)
	}

	// Pull the image first
	m.logger.Info().Msg("pulling Qdrant image...")
	if err := m.runContainerCmd(ctx, "pull", "docker.io/qdrant/qdrant:latest"); err != nil {
		m.logger.Warn().Err(err).Msg("failed to pull image, trying with existing")
	}

	// Create and start container
	m.logger.Info().Str("container", m.containerName).Msg("creating Qdrant container")
	if err := m.createContainer(ctx); err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	// Wait for API to be ready
	m.logger.Info().Msg("waiting for Qdrant to be ready...")
	if err := m.waitForAPI(ctx, 60*time.Second); err != nil {
		return fmt.Errorf("Qdrant failed to start: %w", err)
	}

	m.logger.Info().Msg("Qdrant installed and running")
	return nil
}

// SetContainerRuntime explicitly sets the container runtime to use.
// This is useful when the caller wants to override auto-detection.
func (m *QdrantManager) SetContainerRuntime(runtime string) {
	m.containerCmd = runtime
}

// DetectContainerRuntime detects and sets the container runtime.
// Returns the detected runtime name ("docker" or "podman") or error if none available.
func (m *QdrantManager) DetectContainerRuntime() (string, error) {
	if err := m.detectContainerRuntime(); err != nil {
		return "", err
	}
	return m.containerCmd, nil
}

// StartPodmanMachine attempts to start the Podman machine on macOS.
// Returns true if started successfully or already running, false otherwise.
func (m *QdrantManager) StartPodmanMachine(ctx context.Context) (bool, error) {
	if runtime.GOOS != "darwin" {
		return false, fmt.Errorf("Podman machine is only used on macOS")
	}

	if !m.commandExists("podman") {
		return false, fmt.Errorf("Podman is not installed")
	}

	// Check if machine exists
	out, err := exec.CommandContext(ctx, "podman", "machine", "list", "--format", "{{.Name}}").Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return false, fmt.Errorf("no Podman machine found, run: podman machine init")
	}

	// Check if already running
	out, err = exec.CommandContext(ctx, "podman", "machine", "list", "--format", "{{.Running}}").Output()
	if err == nil && strings.Contains(string(out), "true") {
		m.logger.Info().Msg("Podman machine is already running")
		m.containerCmd = "podman"
		return true, nil
	}

	// Start the machine
	m.logger.Info().Msg("starting Podman machine...")
	cmd := exec.CommandContext(ctx, "podman", "machine", "start")
	if output, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to start Podman machine: %w (output: %s)", err, string(output))
	}

	m.containerCmd = "podman"
	m.logger.Info().Msg("Podman machine started")
	return true, nil
}
