package ai

import (
	"testing"
)

func TestParseRepoURL_HTTPS(t *testing.T) {
	tests := []struct {
		input         string
		expectedURL   string
		expectedOwner string
		expectedName  string
		shouldError   bool
	}{
		{
			input:         "https://github.com/7nohe/local-mcp-server-sample",
			expectedURL:   "https://github.com/7nohe/local-mcp-server-sample.git",
			expectedOwner: "7nohe",
			expectedName:  "local-mcp-server-sample",
		},
		{
			input:         "https://github.com/modelcontextprotocol/servers.git",
			expectedURL:   "https://github.com/modelcontextprotocol/servers.git",
			expectedOwner: "modelcontextprotocol",
			expectedName:  "servers",
		},
		{
			input:         "github.com/owner/repo",
			expectedURL:   "https://github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedName:  "repo",
		},
		{
			input:         "https://github.com/owner/repo/",
			expectedURL:   "https://github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedName:  "repo",
		},
		{
			input:       "invalid-url",
			shouldError: true,
		},
		{
			input:       "https://github.com/owner",
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			normalizedURL, owner, name, err := parseRepoURL(tt.input)

			if tt.shouldError {
				if err == nil {
					t.Errorf("expected error for input %s", tt.input)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if normalizedURL != tt.expectedURL {
				t.Errorf("expected URL %s, got %s", tt.expectedURL, normalizedURL)
			}

			if owner != tt.expectedOwner {
				t.Errorf("expected owner %s, got %s", tt.expectedOwner, owner)
			}

			if name != tt.expectedName {
				t.Errorf("expected name %s, got %s", tt.expectedName, name)
			}
		})
	}
}

func TestParseRepoURL_SSH(t *testing.T) {
	tests := []struct {
		input         string
		expectedURL   string
		expectedOwner string
		expectedName  string
	}{
		{
			input:         "git@github.com:owner/repo.git",
			expectedURL:   "https://github.com/owner/repo.git",
			expectedOwner: "owner",
			expectedName:  "repo",
		},
		{
			input:         "git@github.com:modelcontextprotocol/servers",
			expectedURL:   "https://github.com/modelcontextprotocol/servers.git",
			expectedOwner: "modelcontextprotocol",
			expectedName:  "servers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			normalizedURL, owner, name, err := parseRepoURL(tt.input)

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if normalizedURL != tt.expectedURL {
				t.Errorf("expected URL %s, got %s", tt.expectedURL, normalizedURL)
			}

			if owner != tt.expectedOwner {
				t.Errorf("expected owner %s, got %s", tt.expectedOwner, owner)
			}

			if name != tt.expectedName {
				t.Errorf("expected name %s, got %s", tt.expectedName, name)
			}
		})
	}
}

func TestContainsMCPPatterns(t *testing.T) {
	tests := []struct {
		content  string
		expected bool
	}{
		{
			content:  `import { StdioServerTransport } from "@modelcontextprotocol/sdk"`,
			expected: true,
		},
		{
			content:  `from mcp.server import Server`,
			expected: true,
		},
		{
			content:  `const server = new McpServer()`,
			expected: true,
		},
		{
			content:  `console.log("Hello World")`,
			expected: false,
		},
		{
			content:  `def main(): print("test")`,
			expected: false,
		},
		{
			content:  `server.tool("my_tool", ...)`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.content[:min(30, len(tt.content))], func(t *testing.T) {
			result := containsMCPPatterns(tt.content)
			if result != tt.expected {
				t.Errorf("expected %v for content containing MCP patterns, got %v", tt.expected, result)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{
			input:    "short",
			maxLen:   10,
			expected: "short",
		},
		{
			input:    "this is a very long string that should be truncated",
			maxLen:   20,
			expected: "this is a very long ",
		},
		{
			input:    "",
			maxLen:   10,
			expected: "",
		},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if len(result) > tt.maxLen+len("\n... (truncated)") {
			t.Errorf("truncate did not limit string properly")
		}
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			input:    `Here is the JSON: {"key": "value"} and some more text`,
			expected: `{"key": "value"}`,
		},
		{
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			input:    "no json here",
			expected: "no json here",
		},
	}

	for _, tt := range tests {
		result := extractJSON(tt.input)
		if result != tt.expected {
			t.Errorf("expected %s, got %s", tt.expected, result)
		}
	}
}

func TestFetchResultToAnalysisRequest(t *testing.T) {
	fetchResult := &FetchResult{
		LocalPath: "/tmp/test",
		RepoURL:   "https://github.com/test/repo.git",
		RepoName:  "repo",
		Owner:     "test",
		Files: ExtractedFiles{
			README:      "# Test README",
			PackageJSON: `{"name": "test"}`,
		},
	}

	req := fetchResult.ToAnalysisRequest()

	if req.RepoURL != fetchResult.RepoURL {
		t.Errorf("expected RepoURL %s, got %s", fetchResult.RepoURL, req.RepoURL)
	}

	if req.README != fetchResult.Files.README {
		t.Errorf("expected README %s, got %s", fetchResult.Files.README, req.README)
	}

	if req.PackageJSON != fetchResult.Files.PackageJSON {
		t.Errorf("expected PackageJSON %s, got %s", fetchResult.Files.PackageJSON, req.PackageJSON)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
