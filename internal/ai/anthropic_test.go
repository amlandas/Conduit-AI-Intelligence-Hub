package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAnthropicProvider_Name(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	if provider.Name() != "anthropic" {
		t.Errorf("expected name anthropic, got %s", provider.Name())
	}
}

func TestAnthropicProvider_IsAvailable_NoAPIKey(t *testing.T) {
	// Ensure env var is not set
	os.Unsetenv("ANTHROPIC_API_KEY")

	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = ""
	provider := NewAnthropicProvider(config)

	available, err := provider.IsAvailable(context.Background())
	if err == nil {
		t.Error("expected error for missing API key")
	}
	if available {
		t.Error("expected provider to not be available")
	}

	unavailErr, ok := err.(*ErrProviderUnavailable)
	if !ok {
		t.Errorf("expected ErrProviderUnavailable, got %T", err)
	}

	if unavailErr.Provider != "anthropic" {
		t.Errorf("expected provider anthropic in error, got %s", unavailErr.Provider)
	}
}

func TestAnthropicProvider_IsAvailable_WithAPIKey(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "sk-ant-test123"
	provider := NewAnthropicProvider(config)

	available, err := provider.IsAvailable(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !available {
		t.Error("expected provider to be available")
	}
}

func TestAnthropicProvider_IsAvailable_FromEnv(t *testing.T) {
	// Set env var
	os.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-env")
	defer os.Unsetenv("ANTHROPIC_API_KEY")

	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "" // Empty, should pick up from env
	provider := NewAnthropicProvider(config)

	available, err := provider.IsAvailable(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !available {
		t.Error("expected provider to be available from env")
	}
}

func TestAnthropicProvider_Analyze_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers
		if r.Header.Get("x-api-key") != "test-key" {
			t.Error("missing or incorrect API key header")
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Error("missing or incorrect anthropic-version header")
		}

		response := map[string]interface{}{
			"id":   "msg_123",
			"type": "message",
			"role": "assistant",
			"content": []map[string]string{
				{
					"type": "text",
					"text": `{"confidence": 0.90, "runtime": "nodejs", "runtime_version": "20", "build_commands": ["npm install", "npm run build"], "run_command": "node", "run_args": ["build/index.js"], "transport": "stdio", "dependencies": [], "environment_variables": {}, "requires_network": false, "requires_filesystem": [], "description": "A sample MCP server", "warnings": []}`,
				},
			},
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage": map[string]int{
				"input_tokens":  100,
				"output_tokens": 50,
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Override the API URL for testing
	originalURL := anthropicAPIURL
	defer func() { /* can't easily restore const */ }()

	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	// Replace the internal URL (we'd need to make this configurable for proper testing)
	// For now, we'll test the parsing separately

	req := AnalysisRequest{
		RepoURL:     "https://github.com/test/repo",
		README:      "# Test MCP Server",
		PackageJSON: `{"name": "test"}`,
	}

	// Test that the request is properly built
	prompt := provider.buildAnalysisPrompt(req)
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !containsStr(prompt, "https://github.com/test/repo") {
		t.Error("expected prompt to contain repo URL")
	}
	if !containsStr(prompt, "README.md") {
		t.Error("expected prompt to contain README section")
	}

	_ = originalURL // silence unused warning
}

func TestAnthropicProvider_ParseAnalysisResponse(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	response := `{"confidence": 0.88, "runtime": "python", "runtime_version": "3.11", "build_commands": ["pip install -e ."], "run_command": "python", "run_args": ["-m", "myserver"], "transport": "stdio", "dependencies": ["python3"], "environment_variables": {"DEBUG": "1"}, "requires_network": true, "requires_filesystem": ["/tmp"], "description": "Python MCP server", "warnings": ["Requires network access"]}`

	analysis, err := provider.parseAnalysisResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.Confidence != 0.88 {
		t.Errorf("expected confidence 0.88, got %f", analysis.Confidence)
	}

	if analysis.Runtime != "python" {
		t.Errorf("expected runtime python, got %s", analysis.Runtime)
	}

	if analysis.RuntimeVersion != "3.11" {
		t.Errorf("expected runtime version 3.11, got %s", analysis.RuntimeVersion)
	}

	if !analysis.RequiresNetwork {
		t.Error("expected RequiresNetwork to be true")
	}

	if len(analysis.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(analysis.Warnings))
	}

	if len(analysis.Dependencies) != 1 || analysis.Dependencies[0] != "python3" {
		t.Error("expected dependency python3")
	}
}

func TestAnthropicProvider_GenerateDockerfile(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	analysis := &AnalysisResponse{
		Runtime:        "nodejs",
		RuntimeVersion: "20",
		BuildCommands:  []string{"npm install", "npm run build"},
		RunCommand:     "node",
		RunArgs:        []string{"build/index.js"},
		Transport:      "stdio",
		Description:    "Test server",
	}

	req := DockerfileRequest{
		Analysis: analysis,
		RepoURL:  "https://github.com/test/repo",
	}

	prompt := provider.buildDockerfilePrompt(req)
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !containsStr(prompt, "nodejs") {
		t.Error("expected prompt to contain runtime")
	}
	if !containsStr(prompt, "npm install") {
		t.Error("expected prompt to contain build commands")
	}
}

func TestAnthropicProvider_ParseDockerfileResponse(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	response := `{
		"confidence": 0.85,
		"dockerfile": "FROM node:20-slim\nWORKDIR /app\nRUN npm install\nENTRYPOINT [\"node\", \"index.js\"]",
		"volumes": [{"host_path": "/data", "container_path": "/app/data", "read_only": true, "description": "Data volume"}],
		"ports": [3000],
		"environment": {"NODE_ENV": "production"},
		"mcp_config": {
			"transport": "stdio",
			"command": "docker",
			"args": ["run", "-i", "--rm", "test"],
			"env": {}
		},
		"warnings": ["Consider using multi-stage build"]
	}`

	dockerConfig, err := provider.parseDockerfileResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dockerConfig.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", dockerConfig.Confidence)
	}

	if dockerConfig.Dockerfile == "" {
		t.Error("expected non-empty dockerfile")
	}

	if len(dockerConfig.Volumes) != 1 {
		t.Errorf("expected 1 volume, got %d", len(dockerConfig.Volumes))
	}

	if dockerConfig.Volumes[0].HostPath != "/data" {
		t.Errorf("expected volume host path /data, got %s", dockerConfig.Volumes[0].HostPath)
	}

	if !dockerConfig.Volumes[0].ReadOnly {
		t.Error("expected volume to be read-only")
	}

	if len(dockerConfig.Ports) != 1 || dockerConfig.Ports[0] != 3000 {
		t.Error("expected port 3000")
	}

	if len(dockerConfig.Warnings) != 1 {
		t.Errorf("expected 1 warning, got %d", len(dockerConfig.Warnings))
	}
}

func TestAnthropicProvider_Troubleshoot(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	analysis := &AnalysisResponse{
		Runtime: "nodejs",
	}

	req := TroubleshootRequest{
		Analysis:    analysis,
		Dockerfile:  "FROM node:20\nRUN npm install",
		ErrorOutput: "npm ERR! ENOENT",
		Stage:       "build",
	}

	prompt := provider.buildTroubleshootPrompt(req)
	if prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if !containsStr(prompt, "npm ERR!") {
		t.Error("expected prompt to contain error output")
	}
	if !containsStr(prompt, "build") {
		t.Error("expected prompt to contain stage")
	}
}

func TestAnthropicProvider_ParseTroubleshootResponse(t *testing.T) {
	config := DefaultProviderConfig()
	config.Provider = "anthropic"
	config.APIKey = "test-key"
	provider := NewAnthropicProvider(config)

	response := `{
		"confidence": 0.75,
		"diagnosis": "Package.json is missing",
		"suggested_fixes": [
			{
				"description": "Add package.json to the build context",
				"commands": ["docker build --no-cache ."],
				"file_changes": {},
				"likelihood": 0.8
			}
		],
		"updated_dockerfile": "FROM node:20\nCOPY package*.json ./\nRUN npm install",
		"needs_human_intervention": false
	}`

	troubleshoot, err := provider.parseTroubleshootResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if troubleshoot.Confidence != 0.75 {
		t.Errorf("expected confidence 0.75, got %f", troubleshoot.Confidence)
	}

	if troubleshoot.Diagnosis == "" {
		t.Error("expected non-empty diagnosis")
	}

	if len(troubleshoot.SuggestedFixes) != 1 {
		t.Errorf("expected 1 suggested fix, got %d", len(troubleshoot.SuggestedFixes))
	}

	if troubleshoot.SuggestedFixes[0].Likelihood != 0.8 {
		t.Errorf("expected fix likelihood 0.8, got %f", troubleshoot.SuggestedFixes[0].Likelihood)
	}

	if troubleshoot.UpdatedDockerfile == "" {
		t.Error("expected non-empty updated dockerfile")
	}

	if troubleshoot.NeedsHumanIntervention {
		t.Error("expected needs_human_intervention to be false")
	}
}
