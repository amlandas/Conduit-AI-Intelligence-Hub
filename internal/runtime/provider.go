// Package runtime provides container runtime abstraction for Conduit.
package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
	"github.com/simpleflo/conduit/pkg/models"
)

// Provider is the interface for container runtime operations.
type Provider interface {
	// Name returns the runtime name (e.g., "podman", "docker")
	Name() string

	// Version returns the runtime version
	Version(ctx context.Context) (string, error)

	// Available checks if the runtime is available and ready
	Available(ctx context.Context) bool

	// Pull downloads a container image
	Pull(ctx context.Context, image string, opts PullOptions) error

	// Run starts a container and returns the container ID
	Run(ctx context.Context, spec ContainerSpec) (string, error)

	// Stop stops a running container
	Stop(ctx context.Context, containerID string, timeout time.Duration) error

	// Remove removes a container
	Remove(ctx context.Context, containerID string, force bool) error

	// Status returns the status of a container
	Status(ctx context.Context, containerID string) (string, error)

	// Logs returns container logs
	Logs(ctx context.Context, containerID string, opts LogOptions) (string, error)

	// Exec executes a command in a running container
	Exec(ctx context.Context, containerID string, command []string) (string, error)

	// Inspect returns detailed container information
	Inspect(ctx context.Context, containerID string) (*ContainerInfo, error)
}

// PullOptions configures image pull behavior.
type PullOptions struct {
	Timeout  time.Duration
	Progress chan<- string
}

// ContainerSpec defines the container to run.
type ContainerSpec struct {
	Name       string
	Image      string
	Command    []string
	Entrypoint []string
	Env        map[string]string
	Mounts     []Mount
	Ports      []Port
	Network    NetworkSpec
	Security   SecuritySpec
	Resources  ResourceSpec
	Labels     map[string]string
	WorkingDir string
	User       string
	Stdin      bool
	StdinOnce  bool
}

// Mount defines a bind mount.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// Port defines a port mapping.
type Port struct {
	Host      int
	Container int
	Protocol  string // "tcp" or "udp"
}

// NetworkSpec defines network configuration.
type NetworkSpec struct {
	Mode         string   // "none", "bridge", "host"
	AllowedHosts []string // For egress filtering
}

// SecuritySpec defines security options.
type SecuritySpec struct {
	ReadOnlyRootfs   bool
	NoNewPrivileges  bool
	DropCapabilities []string
	User             string
	SeccompProfile   string
	AppArmorProfile  string
}

// ResourceSpec defines resource limits.
type ResourceSpec struct {
	MemoryMB int64
	CPUs     float64
}

// LogOptions configures log retrieval.
type LogOptions struct {
	Since  time.Time
	Until  time.Time
	Tail   int
	Follow bool
}

// ContainerInfo contains detailed container information.
type ContainerInfo struct {
	ID        string
	Name      string
	Image     string
	Status    string
	State     string
	CreatedAt time.Time
	StartedAt *time.Time
	ExitCode  int
	Health    string
	Ports     []Port
}

// Selector helps select and configure the runtime.
type Selector struct {
	logger    zerolog.Logger
	preferred string
}

// NewSelector creates a new runtime selector.
func NewSelector(preferred string) *Selector {
	return &Selector{
		logger:    observability.Logger("runtime"),
		preferred: preferred,
	}
}

// Select returns the best available runtime provider.
func (s *Selector) Select(ctx context.Context) (Provider, error) {
	// Try preferred runtime first
	switch s.preferred {
	case "podman":
		p := NewPodmanProvider()
		if p.Available(ctx) {
			s.logger.Info().Str("runtime", "podman").Msg("using preferred runtime")
			return p, nil
		}
		s.logger.Warn().Msg("preferred runtime podman not available")

	case "docker":
		p := NewDockerProvider()
		if p.Available(ctx) {
			s.logger.Info().Str("runtime", "docker").Msg("using preferred runtime")
			return p, nil
		}
		s.logger.Warn().Msg("preferred runtime docker not available")
	}

	// Auto-detect: try podman first (rootless by default), then docker
	podman := NewPodmanProvider()
	if podman.Available(ctx) {
		s.logger.Info().Str("runtime", "podman").Msg("auto-selected runtime")
		return podman, nil
	}

	docker := NewDockerProvider()
	if docker.Available(ctx) {
		s.logger.Info().Str("runtime", "docker").Msg("auto-selected runtime")
		return docker, nil
	}

	return nil, models.NewError(models.ErrRuntimeNotFound,
		"no container runtime available (install Podman or Docker)")
}

// DetectAll returns information about all available runtimes.
func (s *Selector) DetectAll(ctx context.Context) []RuntimeInfo {
	var runtimes []RuntimeInfo

	podman := NewPodmanProvider()
	if podman.Available(ctx) {
		version, _ := podman.Version(ctx)
		runtimes = append(runtimes, RuntimeInfo{
			Name:      "podman",
			Available: true,
			Version:   version,
			Preferred: s.preferred == "podman" || s.preferred == "" || s.preferred == "auto",
		})
	} else {
		runtimes = append(runtimes, RuntimeInfo{
			Name:      "podman",
			Available: false,
		})
	}

	docker := NewDockerProvider()
	if docker.Available(ctx) {
		version, _ := docker.Version(ctx)
		runtimes = append(runtimes, RuntimeInfo{
			Name:      "docker",
			Available: true,
			Version:   version,
			Preferred: s.preferred == "docker",
		})
	} else {
		runtimes = append(runtimes, RuntimeInfo{
			Name:      "docker",
			Available: false,
		})
	}

	return runtimes
}

// RuntimeInfo contains information about a runtime.
type RuntimeInfo struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Preferred bool   `json:"preferred"`
}

// Helper functions

// findExecutable finds an executable in PATH.
func findExecutable(name string) (string, error) {
	// On macOS, also check common installation paths
	if runtime.GOOS == "darwin" {
		paths := []string{
			"/opt/homebrew/bin/" + name,
			"/usr/local/bin/" + name,
		}
		for _, p := range paths {
			if _, err := exec.LookPath(p); err == nil {
				return p, nil
			}
		}
	}

	return exec.LookPath(name)
}

// parseKeyValue parses key=value format.
func parseKeyValue(kv string) (string, string) {
	parts := strings.SplitN(kv, "=", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return kv, ""
}

// formatEnv formats environment variables for command line.
func formatEnv(env map[string]string) []string {
	var result []string
	for k, v := range env {
		result = append(result, fmt.Sprintf("%s=%s", k, v))
	}
	return result
}
