package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Name(t *testing.T) {
	provider := NewOllamaProvider(DefaultProviderConfig())
	if provider.Name() != "ollama" {
		t.Errorf("expected name ollama, got %s", provider.Name())
	}
}

func TestOllamaProvider_IsAvailable_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]string{
					{"name": "qwen2.5-coder:7b"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := DefaultProviderConfig()
	config.Endpoint = server.URL
	provider := NewOllamaProvider(config)

	available, err := provider.IsAvailable(context.Background())
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !available {
		t.Error("expected provider to be available")
	}
}

func TestOllamaProvider_IsAvailable_ModelNotFound(t *testing.T) {
	// Create mock server with no matching model
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]string{
					{"name": "llama3:latest"},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := DefaultProviderConfig()
	config.Endpoint = server.URL
	provider := NewOllamaProvider(config)

	available, err := provider.IsAvailable(context.Background())
	if err == nil {
		t.Error("expected error for missing model")
	}
	if available {
		t.Error("expected provider to not be available")
	}

	// Check error type
	if _, ok := err.(*ErrProviderUnavailable); !ok {
		t.Errorf("expected ErrProviderUnavailable, got %T", err)
	}
}

func TestOllamaProvider_IsAvailable_ConnectionRefused(t *testing.T) {
	config := DefaultProviderConfig()
	config.Endpoint = "http://localhost:99999" // Invalid port
	config.TimeoutSeconds = 1
	provider := NewOllamaProvider(config)

	available, err := provider.IsAvailable(context.Background())
	if err == nil {
		t.Error("expected connection error")
	}
	if available {
		t.Error("expected provider to not be available")
	}
}

func TestOllamaProvider_Analyze_Success(t *testing.T) {
	// Create mock server that returns a valid analysis
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			response := map[string]interface{}{
				"model":      "qwen2.5-coder:7b",
				"created_at": "2024-01-01T00:00:00Z",
				"message": map[string]string{
					"role": "assistant",
					"content": `{
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
						"description": "Test MCP server",
						"warnings": []
					}`,
				},
				"done": true,
			}
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := DefaultProviderConfig()
	config.Endpoint = server.URL
	provider := NewOllamaProvider(config)

	req := AnalysisRequest{
		RepoURL:     "https://github.com/test/repo",
		README:      "# Test MCP Server",
		PackageJSON: `{"name": "test", "scripts": {"build": "tsc"}}`,
	}

	analysis, err := provider.Analyze(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.Confidence != 0.85 {
		t.Errorf("expected confidence 0.85, got %f", analysis.Confidence)
	}

	if analysis.Runtime != "nodejs" {
		t.Errorf("expected runtime nodejs, got %s", analysis.Runtime)
	}

	if analysis.Transport != "stdio" {
		t.Errorf("expected transport stdio, got %s", analysis.Transport)
	}

	if len(analysis.BuildCommands) != 2 {
		t.Errorf("expected 2 build commands, got %d", len(analysis.BuildCommands))
	}
}

func TestOllamaProvider_GenerateDockerfile_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/chat" {
			response := map[string]interface{}{
				"model":      "qwen2.5-coder:7b",
				"created_at": "2024-01-01T00:00:00Z",
				"message": map[string]string{
					"role": "assistant",
					"content": `{
						"confidence": 0.80,
						"dockerfile": "FROM node:20-slim\nWORKDIR /app\nCOPY . .\nRUN npm install && npm run build\nENTRYPOINT [\"node\", \"build/index.js\"]",
						"volumes": [],
						"ports": [],
						"environment": {"NODE_ENV": "production"},
						"mcp_config": {
							"transport": "stdio",
							"command": "docker",
							"args": ["run", "-i", "--rm", "test-image"],
							"env": {}
						},
						"warnings": []
					}`,
				},
				"done": true,
			}
			json.NewEncoder(w).Encode(response)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := DefaultProviderConfig()
	config.Endpoint = server.URL
	provider := NewOllamaProvider(config)

	analysis := &AnalysisResponse{
		Runtime:        "nodejs",
		RuntimeVersion: "20",
		BuildCommands:  []string{"npm install", "npm run build"},
		RunCommand:     "node",
		RunArgs:        []string{"build/index.js"},
		Transport:      "stdio",
	}

	req := DockerfileRequest{
		Analysis: analysis,
		RepoURL:  "https://github.com/test/repo",
	}

	dockerConfig, err := provider.GenerateDockerfile(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if dockerConfig.Confidence != 0.80 {
		t.Errorf("expected confidence 0.80, got %f", dockerConfig.Confidence)
	}

	if dockerConfig.Dockerfile == "" {
		t.Error("expected non-empty dockerfile")
	}

	if dockerConfig.MCPConfig.Transport != "stdio" {
		t.Errorf("expected transport stdio, got %s", dockerConfig.MCPConfig.Transport)
	}
}

func TestOllamaProvider_ParseAnalysisResponse_InvalidJSON(t *testing.T) {
	provider := NewOllamaProvider(DefaultProviderConfig())

	_, err := provider.parseAnalysisResponse("not valid json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestOllamaProvider_ParseAnalysisResponse_ExtractFromText(t *testing.T) {
	provider := NewOllamaProvider(DefaultProviderConfig())

	response := `Here is my analysis:
	{"confidence": 0.75, "runtime": "python", "runtime_version": "3.11", "build_commands": ["pip install -r requirements.txt"], "run_command": "python", "run_args": ["main.py"], "transport": "stdio", "dependencies": [], "environment_variables": {}, "requires_network": false, "requires_filesystem": [], "description": "Test", "warnings": []}
	End of analysis.`

	analysis, err := provider.parseAnalysisResponse(response)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if analysis.Runtime != "python" {
		t.Errorf("expected runtime python, got %s", analysis.Runtime)
	}

	if analysis.Confidence != 0.75 {
		t.Errorf("expected confidence 0.75, got %f", analysis.Confidence)
	}
}
