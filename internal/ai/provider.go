// Package ai provides AI provider interfaces and implementations for Conduit's
// intelligent MCP server analysis and configuration generation.
package ai

import (
	"context"
	"fmt"
)

// Provider defines the interface for AI providers.
type Provider interface {
	// Name returns the provider name (e.g., "ollama", "anthropic").
	Name() string

	// IsAvailable checks if the provider is configured and accessible.
	IsAvailable(ctx context.Context) (bool, error)

	// Analyze sends a prompt to the AI and returns the response.
	Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResponse, error)

	// GenerateDockerfile generates a Dockerfile based on the analysis.
	GenerateDockerfile(ctx context.Context, req DockerfileRequest) (*DockerfileResponse, error)

	// Troubleshoot analyzes an error and suggests fixes.
	Troubleshoot(ctx context.Context, req TroubleshootRequest) (*TroubleshootResponse, error)
}

// AnalysisRequest contains the data needed to analyze an MCP server repository.
type AnalysisRequest struct {
	// RepoURL is the GitHub/GitLab URL of the MCP server.
	RepoURL string

	// README content from the repository.
	README string

	// PackageJSON content (for Node.js projects).
	PackageJSON string

	// RequirementsTxt content (for Python projects).
	RequirementsTxt string

	// GoMod content (for Go projects).
	GoMod string

	// CargoToml content (for Rust projects).
	CargoToml string

	// OtherFiles contains any other relevant files (filename -> content).
	OtherFiles map[string]string
}

// AnalysisResponse contains the AI's analysis of an MCP server.
type AnalysisResponse struct {
	// Confidence is the AI's confidence in this analysis (0.0 - 1.0).
	Confidence float64

	// Runtime detected (nodejs, python, go, rust, binary).
	Runtime string

	// RuntimeVersion is the recommended version (e.g., "20", "3.11", "1.21").
	RuntimeVersion string

	// BuildCommands are the commands needed to build the project.
	BuildCommands []string

	// RunCommand is the command to start the MCP server.
	RunCommand string

	// RunArgs are arguments for the run command.
	RunArgs []string

	// Transport is the MCP transport type (stdio, sse, http).
	Transport string

	// Dependencies are system dependencies needed.
	Dependencies []string

	// EnvironmentVariables are env vars the server might need.
	EnvironmentVariables map[string]string

	// RequiresNetwork indicates if the server needs network access.
	RequiresNetwork bool

	// RequiresFilesystem indicates paths the server needs to access.
	RequiresFilesystem []string

	// Description is a human-readable summary of what this MCP server does.
	Description string

	// Warnings are any concerns or issues found during analysis.
	Warnings []string

	// RawResponse is the raw AI response for debugging.
	RawResponse string
}

// DockerfileRequest contains data for generating a Dockerfile.
type DockerfileRequest struct {
	// Analysis is the result from Analyze().
	Analysis *AnalysisResponse

	// RepoURL for cloning in the Dockerfile.
	RepoURL string

	// AdditionalContext is any extra information from the user.
	AdditionalContext string
}

// DockerfileResponse contains the generated Dockerfile and related configs.
type DockerfileResponse struct {
	// Confidence in this Dockerfile working correctly.
	Confidence float64

	// Dockerfile content.
	Dockerfile string

	// DockerCompose content (if needed for complex setups).
	DockerCompose string

	// Volumes needed for the container.
	Volumes []VolumeMount

	// Ports to expose.
	Ports []int

	// Environment variables for runtime.
	Environment map[string]string

	// MCPConfig is the configuration to add to Claude Code.
	MCPConfig MCPServerConfig

	// Warnings about the generated config.
	Warnings []string

	// RawResponse for debugging.
	RawResponse string
}

// VolumeMount describes a volume mount for the container.
type VolumeMount struct {
	// HostPath on the user's machine.
	HostPath string

	// ContainerPath inside the container.
	ContainerPath string

	// ReadOnly if the mount should be read-only.
	ReadOnly bool

	// Description of why this mount is needed.
	Description string
}

// MCPServerConfig is the configuration for Claude Code's MCP server entry.
type MCPServerConfig struct {
	// Transport type (stdio, sse).
	Transport string

	// Command to run (for stdio transport).
	Command string

	// Args for the command.
	Args []string

	// Env variables.
	Env map[string]string

	// URL for SSE/HTTP transport.
	URL string
}

// TroubleshootRequest contains error information for troubleshooting.
type TroubleshootRequest struct {
	// Analysis is the original analysis.
	Analysis *AnalysisResponse

	// Dockerfile that was used.
	Dockerfile string

	// ErrorOutput from the failed operation.
	ErrorOutput string

	// Stage where the error occurred (build, start, connect).
	Stage string

	// PreviousAttempts if this isn't the first troubleshooting attempt.
	PreviousAttempts []string
}

// TroubleshootResponse contains suggestions for fixing the issue.
type TroubleshootResponse struct {
	// Confidence in the diagnosis.
	Confidence float64

	// Diagnosis of what went wrong.
	Diagnosis string

	// SuggestedFixes are potential fixes in order of likelihood.
	SuggestedFixes []SuggestedFix

	// UpdatedDockerfile if the Dockerfile needs changes.
	UpdatedDockerfile string

	// NeedsHumanIntervention if the AI can't fix this.
	NeedsHumanIntervention bool

	// RawResponse for debugging.
	RawResponse string
}

// SuggestedFix describes a potential fix for an issue.
type SuggestedFix struct {
	// Description of the fix.
	Description string

	// Commands to run to apply the fix.
	Commands []string

	// FileChanges describes files to modify.
	FileChanges map[string]string

	// Likelihood of this fix working (0.0 - 1.0).
	Likelihood float64
}

// ProviderConfig holds configuration for AI providers.
type ProviderConfig struct {
	// Provider name: "ollama", "anthropic", "openai".
	Provider string `mapstructure:"provider"`

	// Model to use (e.g., "qwen2.5-coder:7b", "claude-sonnet-4-20250514").
	Model string `mapstructure:"model"`

	// Endpoint for the API (mainly for Ollama).
	Endpoint string `mapstructure:"endpoint"`

	// APIKey for cloud providers (from env var, not stored in config).
	APIKey string `mapstructure:"-"`

	// Timeout for API calls.
	TimeoutSeconds int `mapstructure:"timeout_seconds"`

	// MaxRetries for failed requests.
	MaxRetries int `mapstructure:"max_retries"`

	// ConfidenceThreshold below which to warn user.
	ConfidenceThreshold float64 `mapstructure:"confidence_threshold"`
}

// DefaultProviderConfig returns sensible defaults.
func DefaultProviderConfig() ProviderConfig {
	return ProviderConfig{
		Provider:            "ollama",
		Model:               "qwen2.5-coder:7b",
		Endpoint:            "http://localhost:11434",
		TimeoutSeconds:      120,
		MaxRetries:          2,
		ConfidenceThreshold: 0.6,
	}
}

// ErrLowConfidence is returned when AI confidence is below threshold.
type ErrLowConfidence struct {
	Confidence float64
	Threshold  float64
	Message    string
}

func (e *ErrLowConfidence) Error() string {
	return fmt.Sprintf("AI confidence %.0f%% is below threshold %.0f%%: %s",
		e.Confidence*100, e.Threshold*100, e.Message)
}

// ErrProviderUnavailable is returned when the AI provider is not accessible.
type ErrProviderUnavailable struct {
	Provider string
	Reason   string
}

func (e *ErrProviderUnavailable) Error() string {
	return fmt.Sprintf("AI provider %s is unavailable: %s", e.Provider, e.Reason)
}
