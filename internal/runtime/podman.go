package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
)

// PodmanProvider implements the Provider interface using Podman CLI.
type PodmanProvider struct {
	logger     zerolog.Logger
	executable string
}

// NewPodmanProvider creates a new Podman provider.
func NewPodmanProvider() *PodmanProvider {
	executable, _ := findExecutable("podman")
	return &PodmanProvider{
		logger:     observability.Logger("runtime.podman"),
		executable: executable,
	}
}

// Name returns the provider name.
func (p *PodmanProvider) Name() string {
	return "podman"
}

// Version returns the Podman version.
func (p *PodmanProvider) Version(ctx context.Context) (string, error) {
	out, err := p.run(ctx, "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Available checks if Podman is available.
func (p *PodmanProvider) Available(ctx context.Context) bool {
	if p.executable == "" {
		return false
	}

	// Quick check: can we run podman version?
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := p.Version(ctx)
	return err == nil
}

// Pull downloads a container image.
func (p *PodmanProvider) Pull(ctx context.Context, image string, opts PullOptions) error {
	args := []string{"pull", image}

	p.logger.Info().Str("image", image).Msg("pulling image")

	_, err := p.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	p.logger.Info().Str("image", image).Msg("image pulled successfully")
	return nil
}

// Run starts a container.
func (p *PodmanProvider) Run(ctx context.Context, spec ContainerSpec) (string, error) {
	args := p.buildRunArgs(spec)

	p.logger.Info().
		Str("name", spec.Name).
		Str("image", spec.Image).
		Msg("starting container")

	out, err := p.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	containerID := strings.TrimSpace(out)
	p.logger.Info().
		Str("container_id", containerID).
		Str("name", spec.Name).
		Msg("container started")

	return containerID, nil
}

// buildRunArgs constructs podman run arguments from ContainerSpec.
func (p *PodmanProvider) buildRunArgs(spec ContainerSpec) []string {
	args := []string{"run", "-d"} // Detached mode

	// Container name
	if spec.Name != "" {
		args = append(args, "--name", spec.Name)
	}

	// Security options (critical for isolation)
	if spec.Security.ReadOnlyRootfs {
		args = append(args, "--read-only")
	}
	if spec.Security.NoNewPrivileges {
		args = append(args, "--security-opt=no-new-privileges")
	}
	for _, cap := range spec.Security.DropCapabilities {
		args = append(args, "--cap-drop="+cap)
	}
	if spec.Security.User != "" {
		args = append(args, "--user", spec.Security.User)
	}
	if spec.Security.SeccompProfile != "" {
		args = append(args, "--security-opt=seccomp="+spec.Security.SeccompProfile)
	}

	// Network
	switch spec.Network.Mode {
	case "none":
		args = append(args, "--network=none")
	case "bridge":
		args = append(args, "--network=bridge")
	case "host":
		args = append(args, "--network=host")
	default:
		// Default to none for security
		args = append(args, "--network=none")
	}

	// Resource limits
	if spec.Resources.MemoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", spec.Resources.MemoryMB))
	}
	if spec.Resources.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", spec.Resources.CPUs))
	}

	// Mounts
	for _, m := range spec.Mounts {
		opt := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if m.ReadOnly {
			opt += ":ro"
		}
		args = append(args, "-v", opt)
	}

	// Ports
	for _, port := range spec.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		args = append(args, "-p", fmt.Sprintf("%d:%d/%s", port.Host, port.Container, protocol))
	}

	// Environment
	for k, v := range spec.Env {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Labels
	for k, v := range spec.Labels {
		args = append(args, "--label", fmt.Sprintf("%s=%s", k, v))
	}

	// Add Conduit label
	args = append(args, "--label", "conduit.managed=true")

	// Working directory
	if spec.WorkingDir != "" {
		args = append(args, "-w", spec.WorkingDir)
	}

	// Stdin handling for MCP servers
	if spec.Stdin {
		args = append(args, "-i")
	}

	// Entrypoint
	if len(spec.Entrypoint) > 0 {
		args = append(args, "--entrypoint", strings.Join(spec.Entrypoint, " "))
	}

	// Image (must be last before command)
	args = append(args, spec.Image)

	// Command
	args = append(args, spec.Command...)

	return args
}

// Stop stops a container.
func (p *PodmanProvider) Stop(ctx context.Context, containerID string, timeout time.Duration) error {
	args := []string{"stop"}
	if timeout > 0 {
		args = append(args, "-t", strconv.Itoa(int(timeout.Seconds())))
	}
	args = append(args, containerID)

	p.logger.Info().Str("container_id", containerID).Msg("stopping container")

	_, err := p.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("stop container: %w", err)
	}

	p.logger.Info().Str("container_id", containerID).Msg("container stopped")
	return nil
}

// Remove removes a container.
func (p *PodmanProvider) Remove(ctx context.Context, containerID string, force bool) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	args = append(args, containerID)

	p.logger.Info().Str("container_id", containerID).Bool("force", force).Msg("removing container")

	_, err := p.run(ctx, args...)
	if err != nil {
		return fmt.Errorf("remove container: %w", err)
	}

	p.logger.Info().Str("container_id", containerID).Msg("container removed")
	return nil
}

// Status returns the status of a container.
func (p *PodmanProvider) Status(ctx context.Context, containerID string) (string, error) {
	out, err := p.run(ctx, "inspect", "--format", "{{.State.Status}}", containerID)
	if err != nil {
		return "", fmt.Errorf("get status: %w", err)
	}
	return strings.TrimSpace(out), nil
}

// Logs returns container logs.
func (p *PodmanProvider) Logs(ctx context.Context, containerID string, opts LogOptions) (string, error) {
	args := []string{"logs"}
	if opts.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(opts.Tail))
	}
	if !opts.Since.IsZero() {
		args = append(args, "--since", opts.Since.Format(time.RFC3339))
	}
	args = append(args, containerID)

	out, err := p.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("get logs: %w", err)
	}
	return out, nil
}

// Exec executes a command in a container.
func (p *PodmanProvider) Exec(ctx context.Context, containerID string, command []string) (string, error) {
	args := append([]string{"exec", containerID}, command...)
	out, err := p.run(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("exec: %w", err)
	}
	return out, nil
}

// Inspect returns detailed container information.
func (p *PodmanProvider) Inspect(ctx context.Context, containerID string) (*ContainerInfo, error) {
	out, err := p.run(ctx, "inspect", containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}

	var inspectResult []struct {
		ID      string `json:"Id"`
		Name    string `json:"Name"`
		Image   string `json:"Image"`
		Created string `json:"Created"`
		State   struct {
			Status     string `json:"Status"`
			Running    bool   `json:"Running"`
			ExitCode   int    `json:"ExitCode"`
			StartedAt  string `json:"StartedAt"`
			FinishedAt string `json:"FinishedAt"`
		} `json:"State"`
	}

	if err := json.Unmarshal([]byte(out), &inspectResult); err != nil {
		return nil, fmt.Errorf("parse inspect output: %w", err)
	}

	if len(inspectResult) == 0 {
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	r := inspectResult[0]
	info := &ContainerInfo{
		ID:       r.ID,
		Name:     strings.TrimPrefix(r.Name, "/"),
		Image:    r.Image,
		Status:   r.State.Status,
		ExitCode: r.State.ExitCode,
	}

	if t, err := time.Parse(time.RFC3339Nano, r.Created); err == nil {
		info.CreatedAt = t
	}
	if r.State.StartedAt != "" && r.State.StartedAt != "0001-01-01T00:00:00Z" {
		if t, err := time.Parse(time.RFC3339Nano, r.State.StartedAt); err == nil {
			info.StartedAt = &t
		}
	}

	if r.State.Running {
		info.State = "running"
	} else {
		info.State = "stopped"
	}

	return info, nil
}

// run executes a podman command and returns the output.
func (p *PodmanProvider) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, p.executable, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	p.logger.Debug().
		Str("cmd", p.executable).
		Strs("args", args).
		Msg("executing podman command")

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, stderr.String())
	}

	return stdout.String(), nil
}
