package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OllamaProvider implements the Provider interface using local Ollama.
type OllamaProvider struct {
	config ProviderConfig
	client *http.Client
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(config ProviderConfig) *OllamaProvider {
	return &OllamaProvider{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// Name returns "ollama".
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// IsAvailable checks if Ollama is running and the model is available.
func (p *OllamaProvider) IsAvailable(ctx context.Context) (bool, error) {
	// Check if Ollama is running
	req, err := http.NewRequestWithContext(ctx, "GET", p.config.Endpoint+"/api/tags", nil)
	if err != nil {
		return false, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return false, &ErrProviderUnavailable{
			Provider: "ollama",
			Reason:   fmt.Sprintf("cannot connect to Ollama at %s: %v", p.config.Endpoint, err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, &ErrProviderUnavailable{
			Provider: "ollama",
			Reason:   fmt.Sprintf("Ollama returned status %d", resp.StatusCode),
		}
	}

	// Check if the model exists
	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return false, err
	}

	modelBase := strings.Split(p.config.Model, ":")[0]
	for _, m := range tagsResp.Models {
		if strings.HasPrefix(m.Name, modelBase) {
			return true, nil
		}
	}

	return false, &ErrProviderUnavailable{
		Provider: "ollama",
		Reason:   fmt.Sprintf("model %s not found, run: ollama pull %s", p.config.Model, p.config.Model),
	}
}

// ollamaRequest is the request body for Ollama API.
type ollamaRequest struct {
	Model    string                   `json:"model"`
	Messages []ollamaMessage          `json:"messages"`
	Stream   bool                     `json:"stream"`
	Format   string                   `json:"format,omitempty"`
	Options  map[string]interface{}   `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ollamaResponse is the response from Ollama API.
type ollamaResponse struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Done bool `json:"done"`
}

// chat sends a chat request to Ollama and returns the response.
func (p *OllamaProvider) chat(ctx context.Context, systemPrompt, userPrompt string, jsonFormat bool) (string, error) {
	messages := []ollamaMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	reqBody := ollamaRequest{
		Model:    p.config.Model,
		Messages: messages,
		Stream:   false,
		Options: map[string]interface{}{
			"temperature": 0.1, // Low temperature for more deterministic output
		},
	}

	if jsonFormat {
		reqBody.Format = "json"
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.config.Endpoint+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var lastErr error
	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			bodyBytes, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("Ollama returned status %d: %s", resp.StatusCode, string(bodyBytes))
			continue
		}

		var ollamaResp ollamaResponse
		if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
			lastErr = err
			continue
		}

		return ollamaResp.Message.Content, nil
	}

	return "", fmt.Errorf("failed after %d attempts: %w", p.config.MaxRetries+1, lastErr)
}

// Analyze analyzes an MCP server repository.
func (p *OllamaProvider) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResponse, error) {
	systemPrompt := `You are an expert at analyzing MCP (Model Context Protocol) server repositories.
Your task is to analyze the provided repository files and determine:
1. What runtime/language is used (nodejs, python, go, rust, or binary)
2. What version of the runtime is recommended
3. How to build the project
4. How to run the MCP server
5. What MCP transport is used (stdio, sse, or http)
6. What system dependencies are needed
7. What environment variables might be needed
8. Whether it needs network access
9. What filesystem paths it might need access to

Respond ONLY with a JSON object in this exact format:
{
  "confidence": 0.85,
  "runtime": "nodejs",
  "runtime_version": "20",
  "build_commands": ["npm install", "npm run build"],
  "run_command": "node",
  "run_args": ["build/index.js"],
  "transport": "stdio",
  "dependencies": [],
  "environment_variables": {},
  "requires_network": false,
  "requires_filesystem": [],
  "description": "A brief description of what this MCP server does",
  "warnings": []
}`

	userPrompt := p.buildAnalysisPrompt(req)

	response, err := p.chat(ctx, systemPrompt, userPrompt, true)
	if err != nil {
		return nil, err
	}

	return p.parseAnalysisResponse(response)
}

func (p *OllamaProvider) buildAnalysisPrompt(req AnalysisRequest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Analyze this MCP server repository: %s\n\n", req.RepoURL))

	if req.README != "" {
		sb.WriteString("=== README.md ===\n")
		sb.WriteString(truncate(req.README, 4000))
		sb.WriteString("\n\n")
	}

	if req.PackageJSON != "" {
		sb.WriteString("=== package.json ===\n")
		sb.WriteString(req.PackageJSON)
		sb.WriteString("\n\n")
	}

	if req.RequirementsTxt != "" {
		sb.WriteString("=== requirements.txt ===\n")
		sb.WriteString(req.RequirementsTxt)
		sb.WriteString("\n\n")
	}

	if req.GoMod != "" {
		sb.WriteString("=== go.mod ===\n")
		sb.WriteString(req.GoMod)
		sb.WriteString("\n\n")
	}

	if req.CargoToml != "" {
		sb.WriteString("=== Cargo.toml ===\n")
		sb.WriteString(req.CargoToml)
		sb.WriteString("\n\n")
	}

	for name, content := range req.OtherFiles {
		sb.WriteString(fmt.Sprintf("=== %s ===\n", name))
		sb.WriteString(truncate(content, 2000))
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (p *OllamaProvider) parseAnalysisResponse(response string) (*AnalysisResponse, error) {
	// Try to extract JSON from the response
	response = extractJSON(response)

	var parsed struct {
		Confidence           float64           `json:"confidence"`
		Runtime              string            `json:"runtime"`
		RuntimeVersion       string            `json:"runtime_version"`
		BuildCommands        []string          `json:"build_commands"`
		RunCommand           string            `json:"run_command"`
		RunArgs              []string          `json:"run_args"`
		Transport            string            `json:"transport"`
		Dependencies         []string          `json:"dependencies"`
		EnvironmentVariables map[string]string `json:"environment_variables"`
		RequiresNetwork      bool              `json:"requires_network"`
		RequiresFilesystem   []string          `json:"requires_filesystem"`
		Description          string            `json:"description"`
		Warnings             []string          `json:"warnings"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w\nResponse: %s", err, response)
	}

	return &AnalysisResponse{
		Confidence:           parsed.Confidence,
		Runtime:              parsed.Runtime,
		RuntimeVersion:       parsed.RuntimeVersion,
		BuildCommands:        parsed.BuildCommands,
		RunCommand:           parsed.RunCommand,
		RunArgs:              parsed.RunArgs,
		Transport:            parsed.Transport,
		Dependencies:         parsed.Dependencies,
		EnvironmentVariables: parsed.EnvironmentVariables,
		RequiresNetwork:      parsed.RequiresNetwork,
		RequiresFilesystem:   parsed.RequiresFilesystem,
		Description:          parsed.Description,
		Warnings:             parsed.Warnings,
		RawResponse:          response,
	}, nil
}

// GenerateDockerfile generates a Dockerfile for the MCP server.
func (p *OllamaProvider) GenerateDockerfile(ctx context.Context, req DockerfileRequest) (*DockerfileResponse, error) {
	systemPrompt := `You are an expert at creating Dockerfiles for MCP (Model Context Protocol) servers.
Your task is to generate a Dockerfile that will:
1. Use an appropriate base image for the runtime
2. Clone the repository
3. Install dependencies
4. Build the project
5. Set up the correct entrypoint for the MCP transport

The container will communicate via stdio transport with the host, so it needs to:
- NOT run as a daemon
- Read from stdin and write to stdout
- Have proper signal handling

Respond ONLY with a JSON object in this exact format:
{
  "confidence": 0.85,
  "dockerfile": "FROM node:20-slim\n...",
  "volumes": [
    {"host_path": "~/.config", "container_path": "/config", "read_only": true, "description": "Config files"}
  ],
  "ports": [],
  "environment": {"NODE_ENV": "production"},
  "mcp_config": {
    "transport": "stdio",
    "command": "docker",
    "args": ["run", "-i", "--rm", "image-name"],
    "env": {}
  },
  "warnings": []
}`

	userPrompt := p.buildDockerfilePrompt(req)

	response, err := p.chat(ctx, systemPrompt, userPrompt, true)
	if err != nil {
		return nil, err
	}

	return p.parseDockerfileResponse(response)
}

func (p *OllamaProvider) buildDockerfilePrompt(req DockerfileRequest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Generate a Dockerfile for this MCP server: %s\n\n", req.RepoURL))
	sb.WriteString("Analysis results:\n")
	sb.WriteString(fmt.Sprintf("- Runtime: %s %s\n", req.Analysis.Runtime, req.Analysis.RuntimeVersion))
	sb.WriteString(fmt.Sprintf("- Build commands: %v\n", req.Analysis.BuildCommands))
	sb.WriteString(fmt.Sprintf("- Run command: %s %v\n", req.Analysis.RunCommand, req.Analysis.RunArgs))
	sb.WriteString(fmt.Sprintf("- Transport: %s\n", req.Analysis.Transport))
	sb.WriteString(fmt.Sprintf("- Dependencies: %v\n", req.Analysis.Dependencies))
	sb.WriteString(fmt.Sprintf("- Requires network: %v\n", req.Analysis.RequiresNetwork))
	sb.WriteString(fmt.Sprintf("- Requires filesystem: %v\n", req.Analysis.RequiresFilesystem))

	if req.AdditionalContext != "" {
		sb.WriteString(fmt.Sprintf("\nAdditional context: %s\n", req.AdditionalContext))
	}

	return sb.String()
}

func (p *OllamaProvider) parseDockerfileResponse(response string) (*DockerfileResponse, error) {
	response = extractJSON(response)

	var parsed struct {
		Confidence  float64 `json:"confidence"`
		Dockerfile  string  `json:"dockerfile"`
		Volumes     []struct {
			HostPath      string `json:"host_path"`
			ContainerPath string `json:"container_path"`
			ReadOnly      bool   `json:"read_only"`
			Description   string `json:"description"`
		} `json:"volumes"`
		Ports       []int             `json:"ports"`
		Environment map[string]string `json:"environment"`
		MCPConfig   struct {
			Transport string            `json:"transport"`
			Command   string            `json:"command"`
			Args      []string          `json:"args"`
			Env       map[string]string `json:"env"`
			URL       string            `json:"url"`
		} `json:"mcp_config"`
		Warnings []string `json:"warnings"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w\nResponse: %s", err, response)
	}

	volumes := make([]VolumeMount, len(parsed.Volumes))
	for i, v := range parsed.Volumes {
		volumes[i] = VolumeMount{
			HostPath:      v.HostPath,
			ContainerPath: v.ContainerPath,
			ReadOnly:      v.ReadOnly,
			Description:   v.Description,
		}
	}

	return &DockerfileResponse{
		Confidence:  parsed.Confidence,
		Dockerfile:  parsed.Dockerfile,
		Volumes:     volumes,
		Ports:       parsed.Ports,
		Environment: parsed.Environment,
		MCPConfig: MCPServerConfig{
			Transport: parsed.MCPConfig.Transport,
			Command:   parsed.MCPConfig.Command,
			Args:      parsed.MCPConfig.Args,
			Env:       parsed.MCPConfig.Env,
			URL:       parsed.MCPConfig.URL,
		},
		Warnings:    parsed.Warnings,
		RawResponse: response,
	}, nil
}

// Troubleshoot analyzes an error and suggests fixes.
func (p *OllamaProvider) Troubleshoot(ctx context.Context, req TroubleshootRequest) (*TroubleshootResponse, error) {
	systemPrompt := `You are an expert at debugging Docker containers and MCP servers.
Analyze the error and suggest fixes.

Respond ONLY with a JSON object in this exact format:
{
  "confidence": 0.75,
  "diagnosis": "The error occurs because...",
  "suggested_fixes": [
    {
      "description": "Fix the Dockerfile base image",
      "commands": ["docker build -t fixed ."],
      "file_changes": {"Dockerfile": "FROM node:20-alpine\n..."},
      "likelihood": 0.8
    }
  ],
  "updated_dockerfile": "FROM node:20-alpine\n...",
  "needs_human_intervention": false
}`

	userPrompt := p.buildTroubleshootPrompt(req)

	response, err := p.chat(ctx, systemPrompt, userPrompt, true)
	if err != nil {
		return nil, err
	}

	return p.parseTroubleshootResponse(response)
}

func (p *OllamaProvider) buildTroubleshootPrompt(req TroubleshootRequest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Troubleshoot this MCP server installation error.\n\n"))
	sb.WriteString(fmt.Sprintf("Stage: %s\n\n", req.Stage))
	sb.WriteString("=== Error Output ===\n")
	sb.WriteString(truncate(req.ErrorOutput, 3000))
	sb.WriteString("\n\n")

	sb.WriteString("=== Dockerfile ===\n")
	sb.WriteString(req.Dockerfile)
	sb.WriteString("\n\n")

	if req.Analysis != nil {
		sb.WriteString("=== Analysis ===\n")
		sb.WriteString(fmt.Sprintf("Runtime: %s %s\n", req.Analysis.Runtime, req.Analysis.RuntimeVersion))
		sb.WriteString(fmt.Sprintf("Build: %v\n", req.Analysis.BuildCommands))
		sb.WriteString(fmt.Sprintf("Run: %s %v\n", req.Analysis.RunCommand, req.Analysis.RunArgs))
	}

	if len(req.PreviousAttempts) > 0 {
		sb.WriteString("\n=== Previous Fix Attempts ===\n")
		for i, attempt := range req.PreviousAttempts {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, attempt))
		}
	}

	return sb.String()
}

func (p *OllamaProvider) parseTroubleshootResponse(response string) (*TroubleshootResponse, error) {
	response = extractJSON(response)

	var parsed struct {
		Confidence      float64 `json:"confidence"`
		Diagnosis       string  `json:"diagnosis"`
		SuggestedFixes  []struct {
			Description string            `json:"description"`
			Commands    []string          `json:"commands"`
			FileChanges map[string]string `json:"file_changes"`
			Likelihood  float64           `json:"likelihood"`
		} `json:"suggested_fixes"`
		UpdatedDockerfile      string `json:"updated_dockerfile"`
		NeedsHumanIntervention bool   `json:"needs_human_intervention"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse AI response: %w\nResponse: %s", err, response)
	}

	fixes := make([]SuggestedFix, len(parsed.SuggestedFixes))
	for i, f := range parsed.SuggestedFixes {
		fixes[i] = SuggestedFix{
			Description: f.Description,
			Commands:    f.Commands,
			FileChanges: f.FileChanges,
			Likelihood:  f.Likelihood,
		}
	}

	return &TroubleshootResponse{
		Confidence:             parsed.Confidence,
		Diagnosis:              parsed.Diagnosis,
		SuggestedFixes:         fixes,
		UpdatedDockerfile:      parsed.UpdatedDockerfile,
		NeedsHumanIntervention: parsed.NeedsHumanIntervention,
		RawResponse:            response,
	}, nil
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "\n... (truncated)"
}

func extractJSON(s string) string {
	// Try to find JSON in the response
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")

	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}
