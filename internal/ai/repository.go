package ai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// RepoFetcher handles cloning and analyzing Git repositories.
type RepoFetcher struct {
	// CacheDir is where repos are cloned temporarily.
	CacheDir string
}

// NewRepoFetcher creates a new repository fetcher.
func NewRepoFetcher(cacheDir string) *RepoFetcher {
	return &RepoFetcher{
		CacheDir: cacheDir,
	}
}

// FetchResult contains the fetched repository data.
type FetchResult struct {
	// LocalPath is the local path to the cloned repo.
	LocalPath string

	// RepoURL is the normalized repository URL.
	RepoURL string

	// RepoName is the name of the repository.
	RepoName string

	// Owner is the repository owner/organization.
	Owner string

	// Files contains extracted file contents.
	Files ExtractedFiles
}

// ExtractedFiles contains relevant files from the repository.
type ExtractedFiles struct {
	README          string
	PackageJSON     string
	RequirementsTxt string
	GoMod           string
	CargoToml       string
	Dockerfile      string
	SourceFiles     map[string]string // Key source files
}

// Fetch clones a repository and extracts relevant files.
func (f *RepoFetcher) Fetch(ctx context.Context, repoURL string) (*FetchResult, error) {
	// Normalize the URL
	normalizedURL, owner, name, err := parseRepoURL(repoURL)
	if err != nil {
		return nil, err
	}

	// Create cache directory
	if err := os.MkdirAll(f.CacheDir, 0755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	// Clone to a temp directory
	localPath := filepath.Join(f.CacheDir, fmt.Sprintf("%s-%s", owner, name))

	// Remove if exists (fresh clone)
	os.RemoveAll(localPath)

	// Clone the repository
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", normalizedURL, localPath)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}

	// Extract relevant files
	files, err := f.extractFiles(localPath)
	if err != nil {
		return nil, fmt.Errorf("extract files: %w", err)
	}

	return &FetchResult{
		LocalPath: localPath,
		RepoURL:   normalizedURL,
		RepoName:  name,
		Owner:     owner,
		Files:     files,
	}, nil
}

// Cleanup removes the cloned repository.
func (f *RepoFetcher) Cleanup(result *FetchResult) error {
	if result != nil && result.LocalPath != "" {
		return os.RemoveAll(result.LocalPath)
	}
	return nil
}

// parseRepoURL parses various Git URL formats.
func parseRepoURL(url string) (normalizedURL, owner, name string, err error) {
	// Handle different URL formats:
	// - https://github.com/owner/repo
	// - https://github.com/owner/repo.git
	// - git@github.com:owner/repo.git
	// - github.com/owner/repo

	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.TrimSuffix(url, ".git")

	// Pattern for HTTPS URLs
	httpsPattern := regexp.MustCompile(`^(?:https?://)?(?:www\.)?([^/]+)/([^/]+)/([^/]+)$`)
	if matches := httpsPattern.FindStringSubmatch(url); len(matches) == 4 {
		host := matches[1]
		owner = matches[2]
		name = matches[3]
		normalizedURL = fmt.Sprintf("https://%s/%s/%s.git", host, owner, name)
		return normalizedURL, owner, name, nil
	}

	// Pattern for SSH URLs
	sshPattern := regexp.MustCompile(`^git@([^:]+):([^/]+)/([^/]+)$`)
	if matches := sshPattern.FindStringSubmatch(url); len(matches) == 4 {
		host := matches[1]
		owner = matches[2]
		name = matches[3]
		// Convert to HTTPS for cloning
		normalizedURL = fmt.Sprintf("https://%s/%s/%s.git", host, owner, name)
		return normalizedURL, owner, name, nil
	}

	return "", "", "", fmt.Errorf("invalid repository URL: %s", url)
}

// extractFiles reads relevant files from the repository.
func (f *RepoFetcher) extractFiles(repoPath string) (ExtractedFiles, error) {
	files := ExtractedFiles{
		SourceFiles: make(map[string]string),
	}

	// README files
	for _, name := range []string{"README.md", "readme.md", "README", "README.txt"} {
		if content, err := readFile(filepath.Join(repoPath, name)); err == nil {
			files.README = content
			break
		}
	}

	// Package files
	if content, err := readFile(filepath.Join(repoPath, "package.json")); err == nil {
		files.PackageJSON = content
	}

	if content, err := readFile(filepath.Join(repoPath, "requirements.txt")); err == nil {
		files.RequirementsTxt = content
	}

	if content, err := readFile(filepath.Join(repoPath, "go.mod")); err == nil {
		files.GoMod = content
	}

	if content, err := readFile(filepath.Join(repoPath, "Cargo.toml")); err == nil {
		files.CargoToml = content
	}

	if content, err := readFile(filepath.Join(repoPath, "Dockerfile")); err == nil {
		files.Dockerfile = content
	}

	// Try to find main source files for MCP servers
	files.SourceFiles = f.findSourceFiles(repoPath)

	return files, nil
}

// findSourceFiles looks for key source files that might contain MCP server code.
func (f *RepoFetcher) findSourceFiles(repoPath string) map[string]string {
	sourceFiles := make(map[string]string)

	// Common entry points for MCP servers
	candidates := []string{
		// Node.js
		"src/index.ts",
		"src/index.js",
		"src/main.ts",
		"src/main.js",
		"src/server.ts",
		"src/server.js",
		"index.ts",
		"index.js",
		"server.ts",
		"server.js",
		"build/index.js",
		// Python
		"src/main.py",
		"src/server.py",
		"src/__main__.py",
		"main.py",
		"server.py",
		"__main__.py",
		// Go
		"main.go",
		"cmd/main.go",
		"cmd/server/main.go",
		// Rust
		"src/main.rs",
		"src/lib.rs",
	}

	for _, candidate := range candidates {
		fullPath := filepath.Join(repoPath, candidate)
		if content, err := readFile(fullPath); err == nil {
			// Only include files that seem to be MCP-related
			if containsMCPPatterns(content) {
				sourceFiles[candidate] = content
			}
		}
	}

	// If no MCP-specific files found, include any entry point we find
	if len(sourceFiles) == 0 {
		for _, candidate := range candidates[:6] { // Just first few candidates
			fullPath := filepath.Join(repoPath, candidate)
			if content, err := readFile(fullPath); err == nil {
				sourceFiles[candidate] = content
				break // Just get one
			}
		}
	}

	return sourceFiles
}

// containsMCPPatterns checks if file content contains MCP-related patterns.
func containsMCPPatterns(content string) bool {
	patterns := []string{
		"@modelcontextprotocol",
		"mcp.server",
		"StdioServerTransport",
		"SSEServerTransport",
		"McpServer",
		"MCP_SERVER",
		"mcp-server",
		"createServer",
		"tool(",
		"resource(",
		"prompt(",
	}

	contentLower := strings.ToLower(content)
	for _, pattern := range patterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// readFile reads a file and returns its content as a string.
func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ToAnalysisRequest converts FetchResult to an AnalysisRequest.
func (r *FetchResult) ToAnalysisRequest() AnalysisRequest {
	return AnalysisRequest{
		RepoURL:         r.RepoURL,
		README:          r.Files.README,
		PackageJSON:     r.Files.PackageJSON,
		RequirementsTxt: r.Files.RequirementsTxt,
		GoMod:           r.Files.GoMod,
		CargoToml:       r.Files.CargoToml,
		OtherFiles:      r.Files.SourceFiles,
	}
}
