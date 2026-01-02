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

// ensureStorageDir creates the Qdrant storage directory if it doesn't exist.
func (m *QdrantManager) ensureStorageDir() error {
	// Create the main storage directory
	if err := os.MkdirAll(m.storageDir, 0755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}

	m.logger.Debug().Str("path", m.storageDir).Msg("storage directory ready")
	return nil
}

// detectContainerRuntime finds an available container runtime.
func (m *QdrantManager) detectContainerRuntime() error {
	// Check for podman first (preferred on macOS with Podman machine)
	if m.commandExists("podman") {
		// On macOS, check if podman machine is running
		if runtime.GOOS == "darwin" {
			out, err := exec.Command("podman", "machine", "list", "--format", "{{.Running}}").Output()
			if err == nil && strings.Contains(string(out), "true") {
				m.containerCmd = "podman"
				m.logger.Debug().Str("runtime", "podman").Msg("using container runtime")
				return nil
			}
		} else {
			m.containerCmd = "podman"
			m.logger.Debug().Str("runtime", "podman").Msg("using container runtime")
			return nil
		}
	}

	// Check for docker
	if m.commandExists("docker") {
		// Verify Docker daemon is running
		if err := exec.Command("docker", "info").Run(); err == nil {
			m.containerCmd = "docker"
			m.logger.Debug().Str("runtime", "docker").Msg("using container runtime")
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
	args := []string{
		"run", "-d",
		"--name", m.containerName,
		"-p", fmt.Sprintf("%d:%d", m.httpPort, 6333),
		"-p", fmt.Sprintf("%d:%d", m.grpcPort, 6334),
		"-v", fmt.Sprintf("%s:/qdrant/storage:Z", m.storageDir),
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

	// Parse collection info
	var result struct {
		Result struct {
			Status               string `json:"status"`
			IndexedVectorsCount  int64  `json:"indexed_vectors_count"`
			PointsCount          int64  `json:"points_count"`
			OptimizerStatus      struct {
				Error string `json:"error"`
			} `json:"optimizer_status"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		health.Error = fmt.Sprintf("parse error: %v", err)
		return health
	}

	health.CollectionStatus = strings.ToLower(result.Result.Status)
	health.IndexedVectors = result.Result.IndexedVectorsCount
	health.TotalPoints = result.Result.PointsCount

	// Check for errors in optimizer status
	if result.Result.OptimizerStatus.Error != "" {
		health.Error = result.Result.OptimizerStatus.Error
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
func (m *QdrantManager) commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
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
