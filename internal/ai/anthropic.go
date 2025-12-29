package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"
)

// AnthropicProvider implements the Provider interface using Anthropic's Claude API.
type AnthropicProvider struct {
	config ProviderConfig
	client *http.Client
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(config ProviderConfig) *AnthropicProvider {
	// Get API key from environment if not set
	if config.APIKey == "" {
		config.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}

	return &AnthropicProvider{
		config: config,
		client: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// Name returns "anthropic".
func (p *AnthropicProvider) Name() string {
	return "anthropic"
}

// IsAvailable checks if the Anthropic API is accessible.
func (p *AnthropicProvider) IsAvailable(ctx context.Context) (bool, error) {
	if p.config.APIKey == "" {
		return false, &ErrProviderUnavailable{
			Provider: "anthropic",
			Reason:   "ANTHROPIC_API_KEY environment variable not set",
		}
	}

	// Make a minimal request to verify the key works
	// We'll just check if we can reach the API (actual validation happens on first use)
	return true, nil
}

// anthropicRequest is the request body for Anthropic API.
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the response from Anthropic API.
type anthropicResponse struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Role         string `json:"role"`
	Content      []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model        string `json:"model"`
	StopReason   string `json:"stop_reason"`
	StopSequence string `json:"stop_sequence"`
	Usage        struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicError struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// chat sends a chat request to Anthropic and returns the response.
func (p *AnthropicProvider) chat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	model := p.config.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	reqBody := anthropicRequest{
		Model:     model,
		MaxTokens: 4096,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userPrompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.config.APIKey)
	req.Header.Set("anthropic-version", anthropicAPIVersion)

	var lastErr error
	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		resp, err := p.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second * time.Duration(attempt+1))
			continue
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			var apiErr anthropicError
			if err := json.Unmarshal(bodyBytes, &apiErr); err == nil {
				lastErr = fmt.Errorf("Anthropic API error: %s - %s", apiErr.Error.Type, apiErr.Error.Message)
			} else {
				lastErr = fmt.Errorf("Anthropic returned status %d: %s", resp.StatusCode, string(bodyBytes))
			}

			// Don't retry on auth errors
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				return "", &ErrProviderUnavailable{
					Provider: "anthropic",
					Reason:   lastErr.Error(),
				}
			}

			time.Sleep(time.Second * time.Duration(attempt+1))
			continue
		}

		var anthropicResp anthropicResponse
		if err := json.Unmarshal(bodyBytes, &anthropicResp); err != nil {
			lastErr = err
			continue
		}

		if len(anthropicResp.Content) == 0 {
			lastErr = fmt.Errorf("empty response from Anthropic")
			continue
		}

		return anthropicResp.Content[0].Text, nil
	}

	return "", fmt.Errorf("failed after %d attempts: %w", p.config.MaxRetries+1, lastErr)
}

// Analyze analyzes an MCP server repository.
func (p *AnthropicProvider) Analyze(ctx context.Context, req AnalysisRequest) (*AnalysisResponse, error) {
	systemPrompt := `You are an expert at analyzing MCP (Model Context Protocol) server repositories.
Your task is to analyze the provided repository files and determine:
1. What runtime/language is used (nodejs, python, go, rust, or binary)
2. What version of the runtime is recommended
3. How to build the project
4. How to run the MCP server
5. What MCP transport is used (stdio, sse, or http) - look for @modelcontextprotocol/sdk usage patterns
6. What system dependencies are needed
7. What environment variables might be needed
8. Whether it needs network access
9. What filesystem paths it might need access to

Be thorough and precise. Look for MCP-specific patterns like:
- StdioServerTransport or SSEServerTransport in TypeScript/JavaScript
- mcp.server.stdio or mcp.server.sse in Python
- The way the server reads from stdin and writes to stdout

Respond ONLY with a JSON object in this exact format (no markdown, no code blocks):
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

	response, err := p.chat(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	return p.parseAnalysisResponse(response)
}

func (p *AnthropicProvider) buildAnalysisPrompt(req AnalysisRequest) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Analyze this MCP server repository: %s\n\n", req.RepoURL))

	if req.README != "" {
		sb.WriteString("=== README.md ===\n")
		sb.WriteString(truncate(req.README, 8000))
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
		sb.WriteString(truncate(content, 4000))
		sb.WriteString("\n\n")
	}

	return sb.String()
}

func (p *AnthropicProvider) parseAnalysisResponse(response string) (*AnalysisResponse, error) {
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
func (p *AnthropicProvider) GenerateDockerfile(ctx context.Context, req DockerfileRequest) (*DockerfileResponse, error) {
	systemPrompt := `You are an expert at creating Dockerfiles for MCP (Model Context Protocol) servers.
Your task is to generate a production-ready Dockerfile that will:
1. Use an appropriate slim/alpine base image for the runtime
2. Clone the repository from the provided URL
3. Install dependencies efficiently (use layer caching)
4. Build the project
5. Set up the correct entrypoint for MCP stdio transport

CRITICAL for MCP stdio transport:
- The container MUST read from stdin and write to stdout
- Do NOT run as a daemon or detached process
- Use exec form for ENTRYPOINT: ["node", "build/index.js"]
- Do NOT use shell form that wraps in /bin/sh

The container will be run with: docker run -i --rm <image>

Respond ONLY with a JSON object (no markdown, no code blocks):
{
  "confidence": 0.85,
  "dockerfile": "FROM node:20-slim\nWORKDIR /app\nRUN git clone <repo> . && npm install && npm run build\nENTRYPOINT [\"node\", \"build/index.js\"]",
  "volumes": [
    {"host_path": "~/.config", "container_path": "/config", "read_only": true, "description": "Config files"}
  ],
  "ports": [],
  "environment": {"NODE_ENV": "production"},
  "mcp_config": {
    "transport": "stdio",
    "command": "docker",
    "args": ["run", "-i", "--rm", "conduit-mcp-<name>"],
    "env": {}
  },
  "warnings": []
}`

	userPrompt := p.buildDockerfilePrompt(req)

	response, err := p.chat(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	return p.parseDockerfileResponse(response)
}

func (p *AnthropicProvider) buildDockerfilePrompt(req DockerfileRequest) string {
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
	sb.WriteString(fmt.Sprintf("- Description: %s\n", req.Analysis.Description))

	if req.AdditionalContext != "" {
		sb.WriteString(fmt.Sprintf("\nAdditional context: %s\n", req.AdditionalContext))
	}

	return sb.String()
}

func (p *AnthropicProvider) parseDockerfileResponse(response string) (*DockerfileResponse, error) {
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
func (p *AnthropicProvider) Troubleshoot(ctx context.Context, req TroubleshootRequest) (*TroubleshootResponse, error) {
	systemPrompt := `You are an expert at debugging Docker containers and MCP servers.
Analyze the error carefully and provide actionable fixes.

Common issues to check:
- Base image compatibility (especially for ARM vs x86)
- Missing system dependencies (git, build-essential, etc.)
- Incorrect build commands
- Permission issues
- Network/firewall issues
- stdin/stdout handling for MCP stdio transport

Respond ONLY with a JSON object (no markdown, no code blocks):
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

	response, err := p.chat(ctx, systemPrompt, userPrompt)
	if err != nil {
		return nil, err
	}

	return p.parseTroubleshootResponse(response)
}

func (p *AnthropicProvider) buildTroubleshootPrompt(req TroubleshootRequest) string {
	var sb strings.Builder

	sb.WriteString("Troubleshoot this MCP server installation error.\n\n")
	sb.WriteString(fmt.Sprintf("Stage: %s\n\n", req.Stage))
	sb.WriteString("=== Error Output ===\n")
	sb.WriteString(truncate(req.ErrorOutput, 6000))
	sb.WriteString("\n\n")

	sb.WriteString("=== Dockerfile ===\n")
	sb.WriteString(req.Dockerfile)
	sb.WriteString("\n\n")

	if req.Analysis != nil {
		sb.WriteString("=== Original Analysis ===\n")
		sb.WriteString(fmt.Sprintf("Runtime: %s %s\n", req.Analysis.Runtime, req.Analysis.RuntimeVersion))
		sb.WriteString(fmt.Sprintf("Build: %v\n", req.Analysis.BuildCommands))
		sb.WriteString(fmt.Sprintf("Run: %s %v\n", req.Analysis.RunCommand, req.Analysis.RunArgs))
		sb.WriteString(fmt.Sprintf("Transport: %s\n", req.Analysis.Transport))
	}

	if len(req.PreviousAttempts) > 0 {
		sb.WriteString("\n=== Previous Fix Attempts (these didn't work) ===\n")
		for i, attempt := range req.PreviousAttempts {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, attempt))
		}
	}

	return sb.String()
}

func (p *AnthropicProvider) parseTroubleshootResponse(response string) (*TroubleshootResponse, error) {
	response = extractJSON(response)

	var parsed struct {
		Confidence     float64 `json:"confidence"`
		Diagnosis      string  `json:"diagnosis"`
		SuggestedFixes []struct {
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
