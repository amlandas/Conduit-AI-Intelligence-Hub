// Package main is the entry point for the Conduit CLI.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/simpleflo/conduit/internal/ai"
	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/installer"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/store"
)

var (
	// Version is set at build time
	Version = "dev"
	// BuildTime is set at build time
	BuildTime = "unknown"
)

// Client for daemon communication
type client struct {
	httpClient *http.Client
	baseURL    string
}

func newClient(socketPath string) *client {
	return &client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
			Timeout: 30 * time.Second,
		},
		baseURL: "http://localhost",
	}
}

func (c *client) get(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *client) post(path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(data)
	}
	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", reqBody)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (c *client) delete(path string) error {
	req, _ := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

var socketPath string

func main() {
	rootCmd := &cobra.Command{
		Use:   "conduit",
		Short: "Conduit - AI Intelligence Hub CLI",
		Long: `Conduit is a local-first AI Intelligence Hub that connects
AI clients to MCP servers and your private knowledge base.

Configure once, works everywhere - across Claude Code, Cursor,
VS Code, Gemini CLI, and more.`,
		Version: fmt.Sprintf("%s (built %s)", Version, BuildTime),
	}

	// Global flags
	defaultSocket := getDefaultSocketPath()
	rootCmd.PersistentFlags().StringVar(&socketPath, "socket", defaultSocket,
		"Unix socket path for daemon communication")

	// Add subcommands
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(installDepsCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(removeCmd())
	rootCmd.AddCommand(bindCmd())
	rootCmd.AddCommand(unbindCmd())
	rootCmd.AddCommand(clientsCmd())
	rootCmd.AddCommand(kbCmd())
	rootCmd.AddCommand(mcpCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(uninstallCmd())
	rootCmd.AddCommand(serviceCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func getDefaultSocketPath() string {
	homeDir, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return `\\.\pipe\conduit`
	}
	return filepath.Join(homeDir, ".conduit", "conduit.sock")
}

// setupCmd runs the initial setup wizard
func setupCmd() *cobra.Command {
	var skipDeps bool

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Run the Conduit setup wizard",
		Long: `Configure Conduit for first-time use.

This wizard will help you:
1. Install required dependencies (Docker/Podman, Ollama)
2. Choose an AI provider for intelligent MCP server installation
3. Configure necessary settings
4. Verify everything is working`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetup(skipDeps)
		},
	}

	cmd.Flags().BoolVar(&skipDeps, "skip-deps", false, "Skip dependency installation")

	return cmd
}

// installDepsCmd installs Conduit dependencies
func installDepsCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "install-deps",
		Short: "Install Conduit dependencies",
		Long: `Install the software dependencies required by Conduit:

- Container Runtime (Docker or Podman)
- Ollama (local AI runtime)
- AI model (qwen2.5-coder:7b)

This command will prompt for confirmation before installing each component.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := installer.New(verbose)
			_, err := inst.CheckAndInstallAll(cmd.Context())
			return err
		},
	}

	cmd.Flags().BoolVar(&verbose, "verbose", false, "Show verbose output")

	return cmd
}

func runSetup(skipDeps bool) error {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    Conduit Setup Wizard                      ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Welcome to Conduit! This wizard will help you configure the")
	fmt.Println("intelligent MCP server installer.")
	fmt.Println()

	// Step 0: Install dependencies (optional)
	if !skipDeps {
		fmt.Println("Step 0: Check Dependencies")
		fmt.Println("──────────────────────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Conduit requires the following software:")
		fmt.Println("  • Container runtime (Docker or Podman)")
		fmt.Println("  • Ollama (for local AI) or Anthropic API key")
		fmt.Println()

		if confirmAction("Check and install dependencies now?") {
			inst := installer.New(false)
			ctx := context.Background()
			results, _ := inst.CheckAndInstallAll(ctx)

			// Check if all required deps are installed
			allInstalled := true
			for _, r := range results {
				if r.Error != nil || (!r.Installed && !r.AlreadyExists && !r.Skipped) {
					allInstalled = false
				}
			}

			if !allInstalled {
				fmt.Println()
				fmt.Println("⚠️  Some dependencies were not installed.")
				if !confirmAction("Continue with setup anyway?") {
					return fmt.Errorf("setup cancelled")
				}
			}
			fmt.Println()
		} else {
			fmt.Println("Skipping dependency installation.")
			fmt.Println("You can install dependencies later with: conduit install-deps")
			fmt.Println()
		}
	}

	// Step 1: Choose AI provider
	fmt.Println("Step 1: Choose AI Provider")
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("Conduit uses AI to analyze MCP server repositories and")
	fmt.Println("automatically generate the configuration needed to run them.")
	fmt.Println()
	fmt.Println("  [1] Local AI (Ollama) - Recommended")
	fmt.Println("      • Runs on your machine")
	fmt.Println("      • Free, private, no API key needed")
	fmt.Println("      • Requires ~8GB RAM")
	fmt.Println("      • May struggle with complex MCP servers")
	fmt.Println()
	fmt.Println("  [2] Cloud AI (Anthropic) - Bring Your Own Key")
	fmt.Println("      • Uses Claude via your API key")
	fmt.Println("      • Most capable, handles edge cases well")
	fmt.Println("      • Costs ~$0.01-0.05 per analysis")
	fmt.Println("      • Requires ANTHROPIC_API_KEY environment variable")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Choice [1/2]: ")
	choice, _ := reader.ReadString('\n')
	choice = strings.TrimSpace(choice)

	var provider, model string
	switch choice {
	case "1", "":
		provider = "ollama"
		model = "qwen2.5-coder:7b"
		fmt.Println()
		fmt.Println("✓ Selected: Local AI (Ollama)")
		fmt.Println()
		fmt.Println("Make sure Ollama is installed and running:")
		fmt.Println("  1. Install: https://ollama.ai")
		fmt.Println("  2. Start:   ollama serve")
		fmt.Printf("  3. Pull:    ollama pull %s\n", model)
	case "2":
		provider = "anthropic"
		model = "claude-sonnet-4-20250514"
		fmt.Println()
		fmt.Println("✓ Selected: Cloud AI (Anthropic)")
		fmt.Println()
		fmt.Println("Make sure to set your API key:")
		fmt.Println("  export ANTHROPIC_API_KEY=sk-ant-...")
	default:
		fmt.Println("Invalid choice. Using default (Ollama).")
		provider = "ollama"
		model = "qwen2.5-coder:7b"
	}

	// Create config directory
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".conduit")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	// Write config file
	configPath := filepath.Join(configDir, "conduit.yaml")

	// Check if config already exists
	if _, err := os.Stat(configPath); err == nil {
		fmt.Println()
		fmt.Printf("⚠️  Config file already exists: %s\n", configPath)
		if !confirmAction("Overwrite?") {
			fmt.Println("Setup cancelled. Existing config preserved.")
			return nil
		}
	}

	configContent := fmt.Sprintf(`# Conduit Configuration
# Generated by conduit setup

# Data directory
data_dir: ~/.conduit

# Unix socket path
socket: ~/.conduit/conduit.sock

# Logging
log_level: info

# AI Configuration
ai:
  provider: %s
  model: %s
  endpoint: http://localhost:11434
  timeout_seconds: 120
  max_retries: 2
  confidence_threshold: 0.6

# Container runtime
runtime:
  preferred: auto

# Policy settings
policy:
  allow_network_egress: false
`, provider, model)

	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Println()
	fmt.Printf("✓ Configuration written to: %s\n", configPath)

	// Step 2: Daemon Service Setup
	fmt.Println()
	fmt.Println("Step 2: Daemon Service")
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("The Conduit daemon runs in the background to manage MCP servers.")
	fmt.Println("It can be set up as a system service that starts automatically.")
	fmt.Println()

	inst := installer.New(false)

	if confirmAction("Install daemon as a system service?") {
		// Find the daemon binary
		daemonPath, err := exec.LookPath("conduit-daemon")
		if err != nil {
			// Try relative to conduit binary
			conduitPath, err := os.Executable()
			if err == nil {
				daemonPath = filepath.Join(filepath.Dir(conduitPath), "conduit-daemon")
			}
		}

		if daemonPath != "" {
			result := inst.SetupDaemonService(context.Background(), daemonPath)
			if result.Error != nil {
				fmt.Printf("⚠️  Could not set up service: %v\n", result.Error)
				fmt.Println("   You can start the daemon manually: conduit-daemon --foreground")
			}
		} else {
			fmt.Println("⚠️  Could not find conduit-daemon binary")
			fmt.Println("   Run 'conduit service install' after adding binaries to PATH")
		}
	} else {
		fmt.Println("Skipping service installation.")
		fmt.Println("Start the daemon manually with: conduit-daemon --foreground")
		fmt.Println("Or install later with: conduit service install")
	}

	// Step 3: Final Verification
	fmt.Println()
	fmt.Println("Step 3: Final Verification")
	fmt.Println("──────────────────────────────────────────────────────────────")

	allGood := true

	// Check for Docker/Podman
	dockerAvailable := checkCommand("docker", "version")
	podmanAvailable := checkCommand("podman", "version")

	if dockerAvailable {
		fmt.Println("✓ Docker is available")
	} else if podmanAvailable {
		fmt.Println("✓ Podman is available")
	} else {
		fmt.Println("⚠️  No container runtime found")
		allGood = false
	}

	// Check for git
	if checkCommand("git", "--version") {
		fmt.Println("✓ Git is available")
	} else {
		fmt.Println("⚠️  Git not found")
		allGood = false
	}

	// Check AI provider
	if provider == "ollama" {
		if checkCommand("ollama", "--version") {
			fmt.Println("✓ Ollama is installed")
			// Check if running
			if inst.IsDaemonRunning() || checkOllamaRunning() {
				fmt.Println("✓ Ollama is running")
			} else {
				fmt.Println("⚠️  Ollama is installed but not running")
				fmt.Println("   Start with: ollama serve")
			}
		} else {
			fmt.Println("⚠️  Ollama not found")
			fmt.Println("   Install with: conduit install-deps")
			allGood = false
		}
	} else {
		if os.Getenv("ANTHROPIC_API_KEY") != "" {
			fmt.Println("✓ ANTHROPIC_API_KEY is set")
		} else {
			fmt.Println("⚠️  ANTHROPIC_API_KEY not set")
			fmt.Println("   Set with: export ANTHROPIC_API_KEY=sk-ant-...")
			allGood = false
		}
	}

	// Check daemon
	if inst.IsDaemonRunning() {
		fmt.Println("✓ Conduit daemon is running")
	} else {
		fmt.Println("○ Conduit daemon is not running")
	}

	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	if allGood {
		fmt.Println("                     Setup Complete!                          ")
	} else {
		fmt.Println("              Setup Complete (with warnings)                  ")
	}
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()

	if allGood {
		fmt.Println("You're all set! Install your first MCP server:")
		fmt.Println()
		fmt.Println("  conduit install https://github.com/7nohe/local-mcp-server-sample")
	} else {
		fmt.Println("Some components need attention. Fix the warnings above, then:")
		fmt.Println()
		fmt.Println("  conduit install https://github.com/7nohe/local-mcp-server-sample")
	}
	fmt.Println()
	fmt.Println("View all commands: conduit --help")
	fmt.Println()

	return nil
}

// checkOllamaRunning checks if Ollama is running
func checkOllamaRunning() bool {
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:11434/api/tags")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "200"
}

// checkCommand checks if a command is available
func checkCommand(name string, args ...string) bool {
	cmd := exec.Command(name, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// statusCmd shows the overall status
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Conduit status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/status")
			if err != nil {
				return fmt.Errorf("daemon not running or unreachable: %w", err)
			}

			var status map[string]interface{}
			json.Unmarshal(data, &status)

			fmt.Println("Conduit Status")
			fmt.Println("==============")
			if daemon, ok := status["daemon"].(map[string]interface{}); ok {
				fmt.Printf("Version: %s\n", daemon["version"])
				fmt.Printf("Uptime:  %s\n", daemon["uptime"])
				fmt.Printf("Ready:   %v\n", daemon["ready"])
			}
			if instances, ok := status["instances"].(map[string]interface{}); ok {
				fmt.Printf("\nInstances: %v total\n", instances["total"])
			}
			if bindings, ok := status["bindings"].(map[string]interface{}); ok {
				fmt.Printf("Bindings:  %v total\n", bindings["total"])
			}

			return nil
		},
	}
}

// installCmd installs a connector from a URL
func installCmd() *cobra.Command {
	var name string
	var provider string
	var skipBuild bool
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "install <url>",
		Short: "Install an MCP server from a GitHub URL",
		Long: `Install an MCP server by providing a GitHub repository URL.

Conduit will:
1. Clone the repository
2. Analyze the code using AI to understand how to build and run it
3. Generate a Docker container configuration
4. Build the container
5. Optionally add it to your AI clients (Claude Code, etc.)

Examples:
  conduit install https://github.com/7nohe/local-mcp-server-sample
  conduit install github.com/modelcontextprotocol/servers/src/filesystem
  conduit install https://github.com/user/mcp-server --name "My Server"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repoURL := args[0]
			return runInstall(cmd.Context(), repoURL, name, provider, skipBuild, dryRun)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Custom name for the MCP server")
	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to use: ollama (default) or anthropic")
	cmd.Flags().BoolVar(&skipBuild, "skip-build", false, "Skip Docker build (just analyze)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without doing it")

	return cmd
}

// runInstall performs the intelligent installation
func runInstall(ctx context.Context, repoURL, customName, providerOverride string, skipBuild, dryRun bool) error {
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Conduit Intelligent MCP Installer               ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Override provider if specified
	aiConfig := ai.ProviderConfig{
		Provider:            cfg.AI.Provider,
		Model:               cfg.AI.Model,
		Endpoint:            cfg.AI.Endpoint,
		TimeoutSeconds:      cfg.AI.TimeoutSeconds,
		MaxRetries:          cfg.AI.MaxRetries,
		ConfidenceThreshold: cfg.AI.ConfidenceThreshold,
	}

	if providerOverride != "" {
		aiConfig.Provider = providerOverride
	}

	// Create AI manager
	aiManager, err := ai.NewManager(aiConfig, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("create AI manager: %w", err)
	}

	// Check AI provider availability
	fmt.Printf("🤖 AI Provider: %s\n", aiManager.ProviderName())
	available, err := aiManager.CheckAvailability(ctx)
	if err != nil {
		fmt.Printf("⚠️  AI provider warning: %v\n", err)
		if aiManager.ProviderName() == "ollama" {
			fmt.Println("\nTo use local AI, ensure Ollama is running:")
			fmt.Println("  1. Install Ollama: https://ollama.ai")
			fmt.Println("  2. Start Ollama: ollama serve")
			fmt.Printf("  3. Pull model: ollama pull %s\n", cfg.AI.Model)
			fmt.Println("\nOr use Claude API instead:")
			fmt.Println("  export ANTHROPIC_API_KEY=sk-ant-...")
			fmt.Println("  conduit install <url> --provider anthropic")
		}
		return err
	}
	if !available {
		return fmt.Errorf("AI provider not available")
	}
	fmt.Println("✓ AI provider ready")
	fmt.Println()

	// Step 1: Fetch and analyze repository
	fmt.Printf("📥 Fetching repository: %s\n", repoURL)
	fetchResult, analysis, err := aiManager.AnalyzeRepository(ctx, repoURL)
	if err != nil {
		return fmt.Errorf("analyze repository: %w", err)
	}
	defer aiManager.Cleanup(fetchResult)

	// Display analysis results
	fmt.Println()
	fmt.Println("📊 Analysis Results")
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Printf("   Repository: %s/%s\n", fetchResult.Owner, fetchResult.RepoName)
	fmt.Printf("   Runtime:    %s %s\n", analysis.Runtime, analysis.RuntimeVersion)
	fmt.Printf("   Transport:  %s\n", analysis.Transport)
	fmt.Printf("   Confidence: %.0f%%\n", analysis.Confidence*100)
	if analysis.Description != "" {
		fmt.Printf("   Description: %s\n", analysis.Description)
	}
	if len(analysis.BuildCommands) > 0 {
		fmt.Printf("   Build:      %s\n", strings.Join(analysis.BuildCommands, " && "))
	}
	fmt.Printf("   Run:        %s %s\n", analysis.RunCommand, strings.Join(analysis.RunArgs, " "))

	if len(analysis.Warnings) > 0 {
		fmt.Println()
		fmt.Println("⚠️  Warnings:")
		for _, w := range analysis.Warnings {
			fmt.Printf("   • %s\n", w)
		}
	}

	// Check confidence threshold
	if analysis.Confidence < cfg.AI.ConfidenceThreshold {
		fmt.Println()
		fmt.Printf("⚠️  AI confidence (%.0f%%) is below threshold (%.0f%%)\n",
			analysis.Confidence*100, cfg.AI.ConfidenceThreshold*100)
		fmt.Println()
		if !confirmAction("Continue anyway?") {
			fmt.Println("Installation cancelled.")
			return nil
		}
	}

	// Step 2: Generate Dockerfile
	fmt.Println()
	fmt.Println("🐳 Generating Docker configuration...")
	dockerConfig, err := aiManager.GenerateContainerConfig(ctx, fetchResult, analysis)
	if err != nil {
		return fmt.Errorf("generate docker config: %w", err)
	}

	fmt.Printf("   Confidence: %.0f%%\n", dockerConfig.Confidence*100)
	if len(dockerConfig.Volumes) > 0 {
		fmt.Println("   Volumes:")
		for _, v := range dockerConfig.Volumes {
			mode := "rw"
			if v.ReadOnly {
				mode = "ro"
			}
			fmt.Printf("     • %s → %s (%s)\n", v.HostPath, v.ContainerPath, mode)
		}
	}

	if dryRun {
		fmt.Println()
		fmt.Println("📄 Generated Dockerfile:")
		fmt.Println("──────────────────────────────────────────────────────────────")
		fmt.Println(dockerConfig.Dockerfile)
		fmt.Println("──────────────────────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("🏷️  MCP Configuration for Claude Code:")
		mcpJSON, _ := json.MarshalIndent(map[string]interface{}{
			"command": dockerConfig.MCPConfig.Command,
			"args":    dockerConfig.MCPConfig.Args,
			"env":     dockerConfig.MCPConfig.Env,
		}, "   ", "  ")
		fmt.Printf("   %s\n", mcpJSON)
		fmt.Println()
		fmt.Println("(Dry run - no changes made)")
		return nil
	}

	if skipBuild {
		fmt.Println()
		fmt.Println("(Skipping build as requested)")
		return nil
	}

	// Step 3: Write Dockerfile
	dockerfilePath, err := aiManager.WriteDockerfile(fetchResult, dockerConfig.Dockerfile)
	if err != nil {
		return fmt.Errorf("write dockerfile: %w", err)
	}
	fmt.Printf("   Dockerfile written to: %s\n", dockerfilePath)

	// Step 4: Build container
	fmt.Println()
	fmt.Println("🔨 Building container...")
	imageName := fmt.Sprintf("conduit-mcp-%s", fetchResult.RepoName)
	fmt.Printf("   Image name: %s\n", imageName)

	// TODO: Implement actual Docker build
	fmt.Println("   ⏳ Docker build not yet implemented")
	fmt.Println()

	// Step 5: Show next steps
	fmt.Println("📋 Next Steps")
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Println("1. Build the container manually:")
	fmt.Printf("   cd %s && docker build -f Dockerfile.conduit -t %s .\n", fetchResult.LocalPath, imageName)
	fmt.Println()
	fmt.Println("2. Add to Claude Code (~/.claude.json or claude_desktop_config.json):")
	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			fetchResult.RepoName: map[string]interface{}{
				"command": "docker",
				"args":    []string{"run", "-i", "--rm", imageName},
			},
		},
	}
	mcpJSON, _ := json.MarshalIndent(mcpConfig, "   ", "  ")
	fmt.Printf("   %s\n", mcpJSON)
	fmt.Println()
	fmt.Println("3. Restart Claude Code and run /mcp to verify")

	return nil
}

// confirmAction prompts the user for confirmation
func confirmAction(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("%s [y/N]: ", prompt)
	response, _ := reader.ReadString('\n')
	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// listCmd lists all instances
func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List connector instances",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/instances")
			if err != nil {
				return fmt.Errorf("failed to list instances: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			instances, _ := resp["instances"].([]interface{})
			if len(instances) == 0 {
				fmt.Println("No connector instances found")
				return nil
			}

			fmt.Printf("%-12s %-20s %-12s %-10s\n", "INSTANCE", "NAME", "STATUS", "VERSION")
			for _, inst := range instances {
				i := inst.(map[string]interface{})
				fmt.Printf("%-12s %-20s %-12s %-10s\n",
					truncate(i["instance_id"].(string), 12),
					truncate(i["display_name"].(string), 20),
					i["status"],
					i["package_version"],
				)
			}

			return nil
		},
	}
}

// startCmd starts an instance
func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <instance-id>",
		Short: "Start a connector instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			c := newClient(socketPath)

			_, err := c.post("/api/v1/instances/"+instanceID+"/start", nil)
			if err != nil {
				return fmt.Errorf("failed to start instance: %w", err)
			}

			fmt.Printf("Started instance %s\n", instanceID)
			return nil
		},
	}
}

// stopCmd stops an instance
func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <instance-id>",
		Short: "Stop a connector instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			c := newClient(socketPath)

			_, err := c.post("/api/v1/instances/"+instanceID+"/stop", nil)
			if err != nil {
				return fmt.Errorf("failed to stop instance: %w", err)
			}

			fmt.Printf("Stopped instance %s\n", instanceID)
			return nil
		},
	}
}

// removeCmd removes an instance
func removeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <instance-id>",
		Short: "Remove a connector instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			c := newClient(socketPath)

			err := c.delete("/api/v1/instances/" + instanceID)
			if err != nil {
				return fmt.Errorf("failed to remove instance: %w", err)
			}

			fmt.Printf("Removed instance %s\n", instanceID)
			return nil
		},
	}
}

// bindCmd binds an instance to a client
func bindCmd() *cobra.Command {
	var clients string
	var scope string

	cmd := &cobra.Command{
		Use:   "bind <instance-id>",
		Short: "Bind a connector to AI clients",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			fmt.Printf("Binding %s to clients: %s (scope: %s)\n", instanceID, clients, scope)

			// TODO: Parse client list and create bindings
			fmt.Println("Binding not yet implemented")
			return nil
		},
	}

	cmd.Flags().StringVar(&clients, "clients", "claude-code", "Comma-separated list of clients")
	cmd.Flags().StringVar(&scope, "scope", "project", "Binding scope: project, user, workspace")

	return cmd
}

// unbindCmd removes a binding
func unbindCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unbind <instance-id> <client-id>",
		Short: "Unbind a connector from a client",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			clientID := args[1]

			fmt.Printf("Unbinding %s from %s\n", instanceID, clientID)
			// TODO: Implement unbinding
			fmt.Println("Unbinding not yet implemented")
			return nil
		},
	}
}

// clientsCmd lists available clients
func clientsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clients",
		Short: "List available AI clients",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/clients")
			if err != nil {
				return fmt.Errorf("failed to list clients: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			clients, _ := resp["clients"].([]interface{})
			fmt.Printf("%-15s %-20s %-10s %s\n", "CLIENT", "NAME", "INSTALLED", "NOTES")
			for _, cl := range clients {
				c := cl.(map[string]interface{})
				installed := "No"
				if b, ok := c["installed"].(bool); ok && b {
					installed = "Yes"
				}
				fmt.Printf("%-15s %-20s %-10s %s\n",
					c["client_id"],
					c["display_name"],
					installed,
					c["notes"],
				)
			}

			return nil
		},
	}
}

// kbCmd is the parent command for knowledge base operations
func kbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Knowledge base operations",
	}

	cmd.AddCommand(kbAddCmd())
	cmd.AddCommand(kbListCmd())
	cmd.AddCommand(kbRemoveCmd())
	cmd.AddCommand(kbSearchCmd())
	cmd.AddCommand(kbSyncCmd())

	return cmd
}

func kbAddCmd() *cobra.Command {
	var name string
	var patterns string

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a folder to the knowledge base",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			fmt.Printf("Adding %s to knowledge base as '%s'\n", path, name)
			// TODO: Implement KB add
			fmt.Println("KB add not yet implemented")
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Display name for the source")
	cmd.Flags().StringVar(&patterns, "patterns", "*.md,*.txt", "File patterns to index")

	return cmd
}

func kbListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List knowledge base sources",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/kb/sources")
			if err != nil {
				return fmt.Errorf("failed to list KB sources: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			sources, _ := resp["sources"].([]interface{})
			if len(sources) == 0 {
				fmt.Println("No knowledge base sources configured")
				return nil
			}

			fmt.Printf("%-20s %-40s %-10s\n", "NAME", "PATH", "DOCS")
			for _, src := range sources {
				s := src.(map[string]interface{})
				fmt.Printf("%-20s %-40s %-10v\n",
					s["name"],
					s["path"],
					s["doc_count"],
				)
			}

			return nil
		},
	}
}

func kbRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <source-name>",
		Short: "Remove a knowledge base source",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourceName := args[0]
			fmt.Printf("Removing KB source: %s\n", sourceName)
			// TODO: Implement KB remove
			fmt.Println("KB remove not yet implemented")
			return nil
		},
	}
}

func kbSearchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "search <query>",
		Short: "Search the knowledge base",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			c := newClient(socketPath)

			data, err := c.get("/api/v1/kb/search?q=" + query)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			results, _ := resp["results"].([]interface{})
			if len(results) == 0 {
				fmt.Printf("No results found for: %s\n", query)
				return nil
			}

			fmt.Printf("Found %v results for: %s\n\n", resp["total_hits"], query)
			for _, r := range results {
				result := r.(map[string]interface{})
				fmt.Printf("• %s\n  %s\n\n", result["path"], result["snippet"])
			}

			return nil
		},
	}
}

func kbSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [source-name]",
		Short: "Sync knowledge base sources",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				fmt.Printf("Syncing KB source: %s\n", args[0])
			} else {
				fmt.Println("Syncing all KB sources")
			}
			// TODO: Implement KB sync
			fmt.Println("KB sync not yet implemented")
			return nil
		},
	}
}

// doctorCmd diagnoses issues
func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Conduit issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Running Conduit diagnostics...")

			// Check daemon connectivity
			c := newClient(socketPath)
			_, err := c.get("/api/v1/health")
			if err != nil {
				fmt.Println("❌ Daemon not running or unreachable")
				fmt.Printf("   Socket: %s\n", socketPath)
				fmt.Println("   Try: conduit-daemon --foreground")
			} else {
				fmt.Println("✓ Daemon is running")
			}

			// Check runtime (placeholder)
			fmt.Println("⏳ Container runtime check not yet implemented")

			// Check clients (placeholder)
			fmt.Println("⏳ Client detection not yet implemented")

			fmt.Println("\nDiagnostics complete")
			return nil
		},
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// mcpCmd is the parent command for MCP server operations
func mcpCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server operations",
		Long:  "Run MCP servers for AI client integration",
	}

	cmd.AddCommand(mcpStdioCmd())
	cmd.AddCommand(mcpKBCmd())

	return cmd
}

// mcpStdioCmd runs an MCP server over stdio (for connector instances)
func mcpStdioCmd() *cobra.Command {
	var instanceID string

	cmd := &cobra.Command{
		Use:   "stdio",
		Short: "Run MCP server over stdio",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintf(os.Stderr, "MCP stdio server for instance %s\n", instanceID)
			fmt.Fprintf(os.Stderr, "Not yet implemented - connector proxy coming soon\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&instanceID, "instance", "", "Connector instance ID")
	cmd.MarkFlagRequired("instance")

	return cmd
}

// mcpKBCmd runs the KB MCP server
func mcpKBCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kb",
		Short: "Run Knowledge Base MCP server",
		Long: `Run the Knowledge Base MCP server over stdio.

This server provides search and document retrieval tools for AI clients
to access your private knowledge base.

Example MCP client configuration:
{
  "mcpServers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")
			dbPath := filepath.Join(dataDir, "conduit.db")

			st, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer st.Close()

			// Create and run MCP server
			server := kb.NewMCPServer(st.DB())

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			// Handle shutdown signals
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigCh
				cancel()
			}()

			return server.Run(ctx)
		},
	}
}

// uninstallCmd removes Conduit
func uninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Conduit",
		Long: `Remove Conduit daemon service and configuration.

This command will:
1. Stop the Conduit daemon
2. Remove the daemon service
3. Optionally remove configuration and data

Note: This does NOT remove the Conduit binaries or dependencies
(Docker, Podman, Ollama).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := installer.New(false)
			return inst.Uninstall(cmd.Context())
		},
	}
}

// serviceCmd manages the daemon service
func serviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage Conduit daemon service",
		Long:  "Install, start, stop, or remove the Conduit daemon service",
	}

	cmd.AddCommand(serviceInstallCmd())
	cmd.AddCommand(serviceStartCmd())
	cmd.AddCommand(serviceStopCmd())
	cmd.AddCommand(serviceStatusCmd())
	cmd.AddCommand(serviceRemoveCmd())

	return cmd
}

func serviceInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install daemon as a system service",
		Long: `Install the Conduit daemon as a system service.

On macOS: Creates a launchd agent that starts on login
On Linux: Creates a systemd user service that starts on login`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Find the daemon binary
			daemonPath, err := exec.LookPath("conduit-daemon")
			if err != nil {
				// Try relative to conduit binary
				conduitPath, err := os.Executable()
				if err != nil {
					return fmt.Errorf("could not find conduit-daemon binary")
				}
				daemonPath = filepath.Join(filepath.Dir(conduitPath), "conduit-daemon")
				if _, err := os.Stat(daemonPath); err != nil {
					return fmt.Errorf("could not find conduit-daemon binary")
				}
			}

			inst := installer.New(false)
			result := inst.SetupDaemonService(cmd.Context(), daemonPath)
			if result.Error != nil {
				return result.Error
			}
			return nil
		},
	}
}

func serviceStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the daemon service",
		RunE: func(cmd *cobra.Command, args []string) error {
			switch runtime.GOOS {
			case "darwin":
				return exec.Command("launchctl", "start", "com.simpleflo.conduit").Run()
			case "linux":
				return exec.Command("systemctl", "--user", "start", "conduit").Run()
			default:
				fmt.Println("Start the daemon manually: conduit-daemon --foreground")
				return nil
			}
		},
	}
}

func serviceStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon service",
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := installer.New(false)
			return inst.StopDaemonService()
		},
	}
}

func serviceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show daemon service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := installer.New(false)

			if inst.IsDaemonRunning() {
				fmt.Println("✓ Conduit daemon is running")
			} else {
				fmt.Println("○ Conduit daemon is not running")
			}

			// Check service status
			switch runtime.GOOS {
			case "darwin":
				homeDir, _ := os.UserHomeDir()
				plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.simpleflo.conduit.plist")
				if _, err := os.Stat(plistPath); err == nil {
					fmt.Println("✓ Daemon service is installed (launchd)")
				} else {
					fmt.Println("○ Daemon service is not installed")
				}
			case "linux":
				out, _ := exec.Command("systemctl", "--user", "is-enabled", "conduit").Output()
				if strings.TrimSpace(string(out)) == "enabled" {
					fmt.Println("✓ Daemon service is installed and enabled (systemd)")
				} else {
					fmt.Println("○ Daemon service is not installed")
				}
			}

			return nil
		},
	}
}

func serviceRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove",
		Short: "Remove the daemon service",
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := installer.New(false)
			if err := inst.RemoveDaemonService(); err != nil {
				return err
			}
			fmt.Println("✓ Daemon service removed")
			return nil
		},
	}
}
