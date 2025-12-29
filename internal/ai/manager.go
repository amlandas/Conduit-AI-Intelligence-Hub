package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rs/zerolog/log"
)

// Manager orchestrates AI-powered analysis and configuration generation.
type Manager struct {
	config    ProviderConfig
	provider  Provider
	fetcher   *RepoFetcher
	cacheDir  string
}

// NewManager creates a new AI manager.
func NewManager(config ProviderConfig, dataDir string) (*Manager, error) {
	cacheDir := filepath.Join(dataDir, "ai-cache")

	m := &Manager{
		config:   config,
		cacheDir: cacheDir,
		fetcher:  NewRepoFetcher(cacheDir),
	}

	// Initialize provider based on config
	if err := m.initProvider(); err != nil {
		return nil, err
	}

	return m, nil
}

// initProvider initializes the appropriate AI provider.
func (m *Manager) initProvider() error {
	switch m.config.Provider {
	case "ollama", "":
		m.provider = NewOllamaProvider(m.config)
	case "anthropic":
		m.provider = NewAnthropicProvider(m.config)
	default:
		return fmt.Errorf("unknown AI provider: %s", m.config.Provider)
	}
	return nil
}

// SetProvider allows switching providers at runtime.
func (m *Manager) SetProvider(provider string) error {
	m.config.Provider = provider
	return m.initProvider()
}

// CheckAvailability checks if the current provider is available.
func (m *Manager) CheckAvailability(ctx context.Context) (bool, error) {
	return m.provider.IsAvailable(ctx)
}

// ProviderName returns the name of the current provider.
func (m *Manager) ProviderName() string {
	return m.provider.Name()
}

// InstallResult contains the result of an intelligent installation.
type InstallResult struct {
	// RepoURL is the repository that was installed.
	RepoURL string

	// RepoName is the name of the repository.
	RepoName string

	// Analysis is the AI's analysis of the repository.
	Analysis *AnalysisResponse

	// DockerConfig contains the generated Docker configuration.
	DockerConfig *DockerfileResponse

	// ImageName is the name of the built Docker image.
	ImageName string

	// ContainerID is the ID of the running container (if started).
	ContainerID string

	// MCPConfig is the configuration to add to Claude Code.
	MCPConfig MCPServerConfig

	// Warnings collected during installation.
	Warnings []string
}

// AnalyzeRepository fetches and analyzes an MCP server repository.
func (m *Manager) AnalyzeRepository(ctx context.Context, repoURL string) (*FetchResult, *AnalysisResponse, error) {
	log.Info().Str("url", repoURL).Msg("Fetching repository")

	// Fetch the repository
	fetchResult, err := m.fetcher.Fetch(ctx, repoURL)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch repository: %w", err)
	}

	log.Info().
		Str("name", fetchResult.RepoName).
		Str("owner", fetchResult.Owner).
		Msg("Repository fetched, analyzing with AI")

	// Analyze with AI
	analysisReq := fetchResult.ToAnalysisRequest()
	analysis, err := m.provider.Analyze(ctx, analysisReq)
	if err != nil {
		m.fetcher.Cleanup(fetchResult)
		return nil, nil, fmt.Errorf("analyze repository: %w", err)
	}

	log.Info().
		Float64("confidence", analysis.Confidence).
		Str("runtime", analysis.Runtime).
		Str("transport", analysis.Transport).
		Msg("Analysis complete")

	// Check confidence threshold
	if analysis.Confidence < m.config.ConfidenceThreshold {
		log.Warn().
			Float64("confidence", analysis.Confidence).
			Float64("threshold", m.config.ConfidenceThreshold).
			Msg("AI confidence is below threshold")
	}

	return fetchResult, analysis, nil
}

// GenerateContainerConfig generates a Dockerfile and container configuration.
func (m *Manager) GenerateContainerConfig(ctx context.Context, fetchResult *FetchResult, analysis *AnalysisResponse) (*DockerfileResponse, error) {
	log.Info().Msg("Generating Dockerfile and container configuration")

	dockerReq := DockerfileRequest{
		Analysis: analysis,
		RepoURL:  fetchResult.RepoURL,
	}

	dockerConfig, err := m.provider.GenerateDockerfile(ctx, dockerReq)
	if err != nil {
		return nil, fmt.Errorf("generate dockerfile: %w", err)
	}

	log.Info().
		Float64("confidence", dockerConfig.Confidence).
		Int("volumes", len(dockerConfig.Volumes)).
		Msg("Container configuration generated")

	return dockerConfig, nil
}

// WriteDockerfile writes the Dockerfile to disk.
func (m *Manager) WriteDockerfile(fetchResult *FetchResult, dockerfile string) (string, error) {
	dockerfilePath := filepath.Join(fetchResult.LocalPath, "Dockerfile.conduit")

	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return "", fmt.Errorf("write dockerfile: %w", err)
	}

	return dockerfilePath, nil
}

// Troubleshoot attempts to diagnose and fix an installation error.
func (m *Manager) Troubleshoot(ctx context.Context, analysis *AnalysisResponse, dockerfile, errorOutput, stage string, previousAttempts []string) (*TroubleshootResponse, error) {
	log.Info().
		Str("stage", stage).
		Int("previous_attempts", len(previousAttempts)).
		Msg("Troubleshooting installation error")

	troubleshootReq := TroubleshootRequest{
		Analysis:         analysis,
		Dockerfile:       dockerfile,
		ErrorOutput:      errorOutput,
		Stage:            stage,
		PreviousAttempts: previousAttempts,
	}

	result, err := m.provider.Troubleshoot(ctx, troubleshootReq)
	if err != nil {
		return nil, fmt.Errorf("troubleshoot: %w", err)
	}

	log.Info().
		Float64("confidence", result.Confidence).
		Bool("needs_human", result.NeedsHumanIntervention).
		Int("fixes", len(result.SuggestedFixes)).
		Msg("Troubleshooting complete")

	return result, nil
}

// Cleanup removes temporary files.
func (m *Manager) Cleanup(fetchResult *FetchResult) error {
	return m.fetcher.Cleanup(fetchResult)
}

// EnsureOllamaModel checks if the Ollama model is installed and pulls it if not.
func (m *Manager) EnsureOllamaModel(ctx context.Context) error {
	if m.config.Provider != "ollama" {
		return nil
	}

	available, err := m.provider.IsAvailable(ctx)
	if err != nil {
		// Check if it's a "model not found" error
		if unavailErr, ok := err.(*ErrProviderUnavailable); ok {
			if contains(unavailErr.Reason, "model") && contains(unavailErr.Reason, "not found") {
				log.Info().Str("model", m.config.Model).Msg("Pulling Ollama model")
				return m.pullOllamaModel(ctx)
			}
		}
		return err
	}

	if !available {
		return &ErrProviderUnavailable{
			Provider: "ollama",
			Reason:   "Ollama is not running",
		}
	}

	return nil
}

// pullOllamaModel pulls the model using ollama CLI.
func (m *Manager) pullOllamaModel(ctx context.Context) error {
	// This would typically be done via the Ollama API or CLI
	// For now, return an error with instructions
	return fmt.Errorf("please run: ollama pull %s", m.config.Model)
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
