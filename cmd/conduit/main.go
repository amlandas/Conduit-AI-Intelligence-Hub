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
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"

	"github.com/simpleflo/conduit/internal/ai"
	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/installer"
	"github.com/simpleflo/conduit/internal/kb"
	containerRuntime "github.com/simpleflo/conduit/internal/runtime"
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
	return newClientWithTimeout(socketPath, 30*time.Second)
}

func newClientWithTimeout(socketPath string, timeout time.Duration) *client {
	return &client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.Dial("unix", socketPath)
				},
			},
			Timeout: timeout,
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

func (c *client) deleteWithResponse(path string) ([]byte, error) {
	req, _ := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
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
	rootCmd.AddCommand(depsCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(listCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(stopCmd())
	rootCmd.AddCommand(removeCmd())
	rootCmd.AddCommand(createCmd())
	rootCmd.AddCommand(statsCmd())
	rootCmd.AddCommand(permissionsCmd())
	rootCmd.AddCommand(auditCmd())
	rootCmd.AddCommand(logsCmd())
	rootCmd.AddCommand(clientCmd())
	rootCmd.AddCommand(kbCmd())
	rootCmd.AddCommand(mcpCmd())
	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(uninstallCmd())
	rootCmd.AddCommand(serviceCmd())
	rootCmd.AddCommand(configCmd())
	rootCmd.AddCommand(backupCmd())
	rootCmd.AddCommand(qdrantCmd())
	rootCmd.AddCommand(falkordbCmd())
	rootCmd.AddCommand(ollamaCmd())
	rootCmd.AddCommand(eventsCmd())

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

// depsCmd manages Conduit dependencies (status, install, validate)
func depsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deps",
		Short: "Manage Conduit dependencies",
		Long: `Check, install, and validate Conduit dependencies.

This command provides programmatic access to dependency management
for use by the GUI and automation tools.

Available subcommands:
  status    - Check status of all dependencies
  install   - Install a dependency
  validate  - Validate a custom binary path`,
	}

	cmd.AddCommand(depsStatusCmd())
	cmd.AddCommand(depsInstallCmd())
	cmd.AddCommand(depsValidateCmd())

	return cmd
}

// DependencyInfo holds information about a dependency
type DependencyInfo struct {
	Installed bool   `json:"installed"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Required  bool   `json:"required"`
}

// depsStatusCmd checks status of all dependencies
func depsStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check status of all dependencies",
		Long: `Check the installation status of all Conduit dependencies.

Dependencies checked:
  - Homebrew (package manager, macOS/Linux)
  - Ollama (local AI runtime)
  - Podman (container runtime, preferred)
  - Docker (container runtime, alternative)

Examples:
  conduit deps status
  conduit deps status --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps := map[string]*DependencyInfo{
				"homebrew": {Required: false},
				"ollama":   {Required: true},
				"podman":   {Required: false},
				"docker":   {Required: false},
			}

			// Map dependency names to binary names
			binaryNames := map[string]string{
				"homebrew": "brew",
				"ollama":   "ollama",
				"podman":   "podman",
				"docker":   "docker",
			}

			// Check each dependency
			for name := range deps {
				binName := binaryNames[name]
				binPath := findBinaryPath(binName)
				if binPath != "" {
					deps[name].Installed = true
					deps[name].Path = binPath

					// Get version
					var versionCmd *exec.Cmd
					if name == "homebrew" {
						versionCmd = exec.Command(binPath, "--version")
					} else {
						versionCmd = exec.Command(binPath, "--version")
					}
					if output, err := versionCmd.Output(); err == nil {
						version := strings.TrimSpace(string(output))
						// Extract first line only
						if idx := strings.Index(version, "\n"); idx > 0 {
							version = version[:idx]
						}
						deps[name].Version = version
					}
				}
			}

			// Check if we have at least one container runtime
			hasContainerRuntime := deps["podman"].Installed || deps["docker"].Installed

			if jsonOutput {
				// JSON output for GUI
				output, _ := json.Marshal(deps)
				fmt.Println(string(output))
			} else {
				// Human-readable output
				fmt.Println("Dependency Status:")
				fmt.Println()

				for _, name := range []string{"homebrew", "ollama", "podman", "docker"} {
					info := deps[name]
					displayName := strings.Title(name)
					if info.Installed {
						fmt.Printf("  ✓ %-12s %s\n", displayName, info.Path)
						if info.Version != "" {
							fmt.Printf("    %-12s %s\n", "", info.Version)
						}
					} else {
						status := "not installed"
						if name == "docker" && deps["podman"].Installed {
							status = "not installed (using Podman)"
						} else if name == "podman" && deps["docker"].Installed {
							status = "not installed (using Docker)"
						}
						fmt.Printf("  ✗ %-12s %s\n", displayName, status)
					}
				}

				fmt.Println()
				if !deps["ollama"].Installed {
					fmt.Println("⚠ Ollama is required for AI features")
					fmt.Println("  Install with: conduit deps install ollama")
				}
				if !hasContainerRuntime {
					fmt.Println("⚠ A container runtime (Podman or Docker) is required")
					fmt.Println("  Install with: conduit deps install podman")
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

// depsInstallCmd installs a dependency
func depsInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <dependency>",
		Short: "Install a dependency",
		Long: `Install a Conduit dependency using the appropriate method for your platform.

Supported dependencies:
  ollama   - Local AI runtime
  podman   - Container runtime (recommended)
  docker   - Container runtime (alternative)
  homebrew - Package manager (macOS/Linux)

Installation methods by platform:
  macOS:
    ollama  → brew install ollama && brew services start ollama
    podman  → brew install podman && podman machine init && podman machine start
    docker  → Opens Docker Desktop download page
    homebrew → Official Homebrew installer

  Linux:
    ollama  → Official Ollama installer script
    podman  → System package manager (apt/dnf)
    docker  → Official Docker installer
    homebrew → Official Homebrew installer

Progress output format (for GUI):
  PROGRESS:<percent>:<message>

Examples:
  conduit deps install ollama
  conduit deps install podman`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dep := strings.ToLower(args[0])
			return installDependency(dep)
		},
	}

	return cmd
}

// installDependency installs a specific dependency
func installDependency(dep string) error {
	// Progress helper
	progress := func(percent int, msg string) {
		fmt.Printf("PROGRESS:%d:%s\n", percent, msg)
	}

	switch dep {
	case "ollama":
		return installOllama(progress)
	case "podman":
		return installPodman(progress)
	case "docker":
		return installDocker(progress)
	case "homebrew", "brew":
		return installHomebrew(progress)
	default:
		return fmt.Errorf("unknown dependency: %s (supported: ollama, podman, docker, homebrew)", dep)
	}
}

// installOllama installs Ollama using platform-appropriate method
func installOllama(progress func(int, string)) error {
	// Check if already installed
	if path := findBinaryPath("ollama"); path != "" {
		progress(100, "Ollama is already installed at "+path)
		return nil
	}

	progress(10, "Checking prerequisites...")

	if runtime.GOOS == "darwin" {
		// macOS: Use Homebrew
		brewPath := findBinaryPath("brew")
		if brewPath == "" {
			return fmt.Errorf("Homebrew is required for Ollama installation on macOS. Install with: conduit deps install homebrew")
		}

		progress(20, "Installing Ollama via Homebrew...")
		installCmd := exec.Command(brewPath, "install", "ollama")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("brew install ollama failed: %w", err)
		}

		progress(70, "Starting Ollama service...")
		startCmd := exec.Command(brewPath, "services", "start", "ollama")
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr
		if err := startCmd.Run(); err != nil {
			// Service start is not critical - user can start manually
			fmt.Printf("Warning: Failed to start Ollama service: %v\n", err)
		}

		// Wait for service to initialize (generates keys)
		progress(85, "Waiting for Ollama to initialize...")
		time.Sleep(3 * time.Second)

		progress(100, "Ollama installed successfully")
		return nil
	}

	// Linux: Use official installer script
	progress(20, "Downloading Ollama installer...")
	installCmd := exec.Command("sh", "-c", "curl -fsSL https://ollama.com/install.sh | sh")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("Ollama installer failed: %w", err)
	}

	progress(100, "Ollama installed successfully")
	return nil
}

// installPodman installs Podman using platform-appropriate method
func installPodman(progress func(int, string)) error {
	// Check if already installed
	if path := findBinaryPath("podman"); path != "" {
		progress(100, "Podman is already installed at "+path)
		return nil
	}

	progress(10, "Checking prerequisites...")

	if runtime.GOOS == "darwin" {
		// macOS: Use Homebrew
		brewPath := findBinaryPath("brew")
		if brewPath == "" {
			return fmt.Errorf("Homebrew is required for Podman installation on macOS. Install with: conduit deps install homebrew")
		}

		progress(20, "Installing Podman via Homebrew...")
		installCmd := exec.Command(brewPath, "install", "podman")
		installCmd.Stdout = os.Stdout
		installCmd.Stderr = os.Stderr
		if err := installCmd.Run(); err != nil {
			return fmt.Errorf("brew install podman failed: %w", err)
		}

		// Initialize Podman machine
		progress(60, "Initializing Podman machine...")
		podmanPath := findBinaryPath("podman")
		if podmanPath == "" {
			podmanPath = "/opt/homebrew/bin/podman"
		}

		initCmd := exec.Command(podmanPath, "machine", "init")
		initCmd.Stdout = os.Stdout
		initCmd.Stderr = os.Stderr
		// Ignore error if machine already exists
		initCmd.Run()

		progress(80, "Starting Podman machine...")
		startCmd := exec.Command(podmanPath, "machine", "start")
		startCmd.Stdout = os.Stdout
		startCmd.Stderr = os.Stderr
		if err := startCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to start Podman machine: %v\n", err)
			fmt.Println("You may need to start it manually with: podman machine start")
		}

		progress(100, "Podman installed successfully")
		return nil
	}

	// Linux: Use package manager
	progress(20, "Installing Podman...")
	var installCmd *exec.Cmd
	if _, err := exec.LookPath("apt"); err == nil {
		installCmd = exec.Command("sudo", "apt", "install", "-y", "podman")
	} else if _, err := exec.LookPath("dnf"); err == nil {
		installCmd = exec.Command("sudo", "dnf", "install", "-y", "podman")
	} else {
		return fmt.Errorf("no supported package manager found (apt or dnf required)")
	}

	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("package manager install failed: %w", err)
	}

	progress(100, "Podman installed successfully")
	return nil
}

// installDocker installs Docker
func installDocker(progress func(int, string)) error {
	// Check if already installed
	if path := findBinaryPath("docker"); path != "" {
		progress(100, "Docker is already installed at "+path)
		return nil
	}

	progress(10, "Checking prerequisites...")

	if runtime.GOOS == "darwin" {
		// macOS: Direct users to Docker Desktop
		progress(50, "Docker Desktop required for macOS")
		fmt.Println()
		fmt.Println("Docker on macOS requires Docker Desktop.")
		fmt.Println("Download from: https://www.docker.com/products/docker-desktop/")
		fmt.Println()
		fmt.Println("Alternatively, use Podman (recommended):")
		fmt.Println("  conduit deps install podman")
		return nil
	}

	// Linux: Use official installer
	progress(20, "Installing Docker...")
	installCmd := exec.Command("sh", "-c", "curl -fsSL https://get.docker.com | sh")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("Docker installer failed: %w", err)
	}

	progress(100, "Docker installed successfully")
	return nil
}

// installHomebrew installs Homebrew package manager
func installHomebrew(progress func(int, string)) error {
	// Check if already installed
	if path := findBinaryPath("brew"); path != "" {
		progress(100, "Homebrew is already installed at "+path)
		return nil
	}

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("Homebrew is only available on macOS and Linux")
	}

	progress(10, "Downloading Homebrew installer...")
	progress(30, "Installing Homebrew (this may take a few minutes)...")

	// Run the official installer
	installCmd := exec.Command("/bin/bash", "-c", "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)")
	installCmd.Stdout = os.Stdout
	installCmd.Stderr = os.Stderr
	installCmd.Stdin = os.Stdin // Allow interactive prompts

	if err := installCmd.Run(); err != nil {
		return fmt.Errorf("Homebrew installer failed: %w", err)
	}

	progress(100, "Homebrew installed successfully")
	fmt.Println()
	fmt.Println("Note: You may need to restart your terminal or run:")
	if runtime.GOOS == "darwin" {
		fmt.Println("  eval \"$(/opt/homebrew/bin/brew shellenv)\"")
	} else {
		fmt.Println("  eval \"$(/home/linuxbrew/.linuxbrew/bin/brew shellenv)\"")
	}

	return nil
}

// depsValidateCmd validates a custom binary path
func depsValidateCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a custom binary path",
		Long: `Validate that a custom binary path is valid and executable.

Checks:
  - File exists
  - File is executable
  - Can run --version

Examples:
  conduit deps validate /custom/path/to/ollama
  conduit deps validate /custom/path/to/podman --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			binaryPath := args[0]

			result := struct {
				Valid   bool   `json:"valid"`
				Path    string `json:"path"`
				Version string `json:"version,omitempty"`
				Error   string `json:"error,omitempty"`
			}{
				Path: binaryPath,
			}

			// Check if file exists
			info, err := os.Stat(binaryPath)
			if os.IsNotExist(err) {
				result.Error = "File not found"
				if jsonOutput {
					output, _ := json.Marshal(result)
					fmt.Println(string(output))
					return nil
				}
				return fmt.Errorf("file not found: %s", binaryPath)
			}
			if err != nil {
				result.Error = err.Error()
				if jsonOutput {
					output, _ := json.Marshal(result)
					fmt.Println(string(output))
					return nil
				}
				return err
			}

			// Check if executable
			if info.Mode()&0111 == 0 {
				result.Error = "File is not executable"
				if jsonOutput {
					output, _ := json.Marshal(result)
					fmt.Println(string(output))
					return nil
				}
				return fmt.Errorf("file is not executable: %s", binaryPath)
			}

			// Try to get version
			versionCmd := exec.Command(binaryPath, "--version")
			if output, err := versionCmd.Output(); err == nil {
				version := strings.TrimSpace(string(output))
				if idx := strings.Index(version, "\n"); idx > 0 {
					version = version[:idx]
				}
				result.Version = version
			} else {
				result.Error = fmt.Sprintf("Failed to run --version: %v", err)
				if jsonOutput {
					output, _ := json.Marshal(result)
					fmt.Println(string(output))
					return nil
				}
				return fmt.Errorf("failed to run %s --version: %w", binaryPath, err)
			}

			result.Valid = true

			if jsonOutput {
				output, _ := json.Marshal(result)
				fmt.Println(string(output))
			} else {
				fmt.Printf("✓ Valid: %s\n", binaryPath)
				if result.Version != "" {
					fmt.Printf("  Version: %s\n", result.Version)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

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

// checkQdrantRunning checks if Qdrant vector database is running
func checkQdrantRunning() bool {
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:6333/collections")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "200"
}

// checkFalkorDBRunning checks if FalkorDB is accessible on localhost:6379
func checkFalkorDBRunning() bool {
	conn, err := net.DialTimeout("tcp", "localhost:6379", 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// getQdrantVectorCount returns the number of vectors in the conduit_kb collection
func getQdrantVectorCount() (int64, error) {
	cmd := exec.Command("curl", "-s", "http://localhost:6333/collections/conduit_kb")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, err
	}

	if res, ok := result["result"].(map[string]interface{}); ok {
		if count, ok := res["points_count"].(float64); ok {
			return int64(count), nil
		}
	}
	return 0, fmt.Errorf("collection not found")
}

// getOllamaModels returns a list of installed Ollama models
func getOllamaModels() ([]string, error) {
	cmd := exec.Command("curl", "-s", "http://localhost:11434/api/tags")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}

	var models []string
	if modelsData, ok := result["models"].([]interface{}); ok {
		for _, m := range modelsData {
			if model, ok := m.(map[string]interface{}); ok {
				if name, ok := model["name"].(string); ok {
					models = append(models, name)
				}
			}
		}
	}
	return models, nil
}

// getActiveContainerRuntime returns the name and version of the preferred container runtime
func getActiveContainerRuntime(ctx context.Context) (name string, version string) {
	selector := containerRuntime.NewSelector("")
	runtimes := selector.DetectAll(ctx)

	for _, rt := range runtimes {
		if rt.Available && rt.Preferred {
			return rt.Name, rt.Version
		}
	}
	// Return first available if no preferred
	for _, rt := range runtimes {
		if rt.Available {
			return rt.Name, rt.Version
		}
	}
	return "none", ""
}

// statusCmd shows the overall status
func statusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show Conduit status",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/status")
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"daemon not running or unreachable: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("daemon not running or unreachable: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
			}

			var status map[string]interface{}
			json.Unmarshal(data, &status)

			fmt.Println("╔══════════════════════════════════════════════════════════════╗")
			fmt.Println("║                     Conduit Status                           ║")
			fmt.Println("╚══════════════════════════════════════════════════════════════╝")
			fmt.Println()

			// Daemon Info
			fmt.Println("📡 Daemon")
			fmt.Println("────────────────────────────────────────────────────────")
			if daemon, ok := status["daemon"].(map[string]interface{}); ok {
				fmt.Printf("   Version: %s\n", daemon["version"])
				fmt.Printf("   Uptime:  %s\n", daemon["uptime"])
				ready := daemon["ready"].(bool)
				if ready {
					fmt.Println("   Status:  ✓ Ready")
				} else {
					fmt.Println("   Status:  ⚠️  Not Ready")
				}
			}
			if instances, ok := status["instances"].(map[string]interface{}); ok {
				fmt.Printf("   Instances: %v\n", instances["total"])
			}
			if bindings, ok := status["bindings"].(map[string]interface{}); ok {
				fmt.Printf("   Bindings:  %v\n", bindings["total"])
			}

			// Dependencies section - from daemon
			fmt.Println()
			fmt.Println("🔧 Managed Dependencies")
			fmt.Println("────────────────────────────────────────────────────────")

			if deps, ok := status["dependencies"].(map[string]interface{}); ok {
				// Container Runtime
				if container, ok := deps["container_runtime"].(map[string]interface{}); ok {
					available := container["available"].(bool)
					if available {
						runtime := container["runtime"].(string)
						containerName := ""
						if cn, ok := container["container"].(string); ok {
							containerName = cn
						}
						fmt.Printf("   Container Runtime: ✓ %s (managed)\n", strings.Title(runtime))
						if containerName != "" {
							fmt.Printf("                      Container: %s\n", containerName)
						}
					} else {
						fmt.Println("   Container Runtime: ○ Not available")
					}
				}

				// Qdrant (Vector DB)
				if qdrant, ok := deps["qdrant"].(map[string]interface{}); ok {
					available := qdrant["available"].(bool)
					qdrantStatus := qdrant["status"].(string)
					if available {
						vectors := int64(0)
						if v, ok := qdrant["vectors"].(float64); ok {
							vectors = int64(v)
						}
						fmt.Printf("   Vector Database:   ✓ Qdrant (%s, %d vectors)\n", qdrantStatus, vectors)
					} else {
						fmt.Printf("   Vector Database:   ○ Qdrant (%s)\n", qdrantStatus)
					}
				}

				// Semantic Search
				if semantic, ok := deps["semantic_search"].(map[string]interface{}); ok {
					enabled := semantic["enabled"].(bool)
					model := semantic["embedding_model"].(string)
					if enabled {
						fmt.Printf("   Semantic Search:   ✓ Enabled (%s)\n", model)
					} else {
						fmt.Println("   Semantic Search:   ○ Disabled")
					}
				}

				// FTS5
				if fts5, ok := deps["full_text_search"].(map[string]interface{}); ok {
					available := fts5["available"].(bool)
					if available {
						fmt.Println("   Full-Text Search:  ✓ SQLite FTS5")
					}
				}
			}

			// AI Provider section
			fmt.Println()
			fmt.Println("🤖 AI Provider")
			fmt.Println("────────────────────────────────────────────────────────")

			cfg, cfgErr := config.Load()
			if cfgErr == nil {
				if cfg.AI.Provider == "ollama" {
					if checkOllamaRunning() {
						fmt.Printf("   Provider: ✓ Ollama (local)\n")
						fmt.Printf("   Model:    %s\n", cfg.AI.Model)
						// List installed models
						if models, err := getOllamaModels(); err == nil && len(models) > 0 {
							fmt.Printf("   Available: %s\n", strings.Join(models, ", "))
						}
					} else {
						fmt.Printf("   Provider: ⚠️  Ollama (not running)\n")
						fmt.Println("   Hint:     Start with 'ollama serve'")
					}
				} else if cfg.AI.Provider == "anthropic" {
					if os.Getenv("ANTHROPIC_API_KEY") != "" {
						fmt.Printf("   Provider: ✓ Anthropic (cloud)\n")
						fmt.Printf("   Model:    %s\n", cfg.AI.Model)
					} else {
						fmt.Printf("   Provider: ❌ Anthropic (API key not set)\n")
					}
				} else if cfg.AI.Provider == "openai" {
					if os.Getenv("OPENAI_API_KEY") != "" {
						fmt.Printf("   Provider: ✓ OpenAI (cloud)\n")
						fmt.Printf("   Model:    %s\n", cfg.AI.Model)
					} else {
						fmt.Printf("   Provider: ❌ OpenAI (API key not set)\n")
					}
				} else {
					fmt.Printf("   Provider: %s\n", cfg.AI.Provider)
					fmt.Printf("   Model:    %s\n", cfg.AI.Model)
				}
			} else {
				fmt.Println("   Provider: ○ Not configured")
			}

			// KAG (Knowledge Graph) section
			fmt.Println()
			fmt.Println("🔮 Knowledge Graph (KAG)")
			fmt.Println("────────────────────────────────────────────────────────")

			if cfg != nil && cfg.KB.KAG.Enabled {
				fmt.Println("   Status:   ✓ Enabled")
				fmt.Printf("   Provider: %s\n", cfg.KB.KAG.Provider)
				if cfg.KB.KAG.Provider == "ollama" {
					fmt.Printf("   Model:    %s\n", cfg.KB.KAG.Ollama.Model)
				}
				if cfg.KB.KAG.PreloadModel {
					fmt.Println("   Preload:  ✓ Model loaded on startup")
				} else {
					fmt.Println("   Preload:  ○ Load on first use")
				}

				// Check FalkorDB status
				if checkFalkorDBRunning() {
					fmt.Println("   FalkorDB: ✓ Running")
				} else {
					fmt.Println("   FalkorDB: ○ Not running")
				}

				// Get KAG stats from database
				homeDir, _ := os.UserHomeDir()
				dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
				if db, err := store.New(dbPath); err == nil {
					defer db.Close()

					var entityCount, relationCount int
					db.DB().QueryRow("SELECT COUNT(*) FROM kb_entities").Scan(&entityCount)
					db.DB().QueryRow("SELECT COUNT(*) FROM kb_relations").Scan(&relationCount)

					fmt.Printf("   Entities:  %d\n", entityCount)
					fmt.Printf("   Relations: %d\n", relationCount)

					// Get extraction status
					var completed, pending, errors int
					db.DB().QueryRow(`
						SELECT COUNT(*) FROM kb_extraction_status WHERE status = 'completed'
					`).Scan(&completed)
					db.DB().QueryRow(`
						SELECT COUNT(*) FROM kb_chunks c
						LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
						WHERE s.status IS NULL
					`).Scan(&pending)
					db.DB().QueryRow(`
						SELECT COUNT(*) FROM kb_extraction_status WHERE status = 'error'
					`).Scan(&errors)

					if pending > 0 || completed > 0 {
						fmt.Printf("   Progress:  %d/%d chunks extracted", completed, completed+pending)
						if errors > 0 {
							fmt.Printf(" (%d errors)", errors)
						}
						fmt.Println()
					}
				}
			} else {
				fmt.Println("   Status:   ○ Disabled")
				fmt.Println("   Enable:   Set kb.kag.enabled=true in config")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// installCmd installs a connector from a URL
func installCmd() *cobra.Command {
	var name string
	var provider string
	var skipBuild bool
	var dryRun bool
	var documentTools bool

	cmd := &cobra.Command{
		Use:   "install [url]",
		Short: "Install an MCP server or document extraction tools",
		Long: `Install an MCP server by providing a GitHub repository URL,
or install document extraction tools using the --document-tools flag.

For MCP server installation, Conduit will:
1. Clone the repository
2. Analyze the code using AI to understand how to build and run it
3. Generate a Docker container configuration
4. Build the container
5. Optionally add it to your AI clients (Claude Code, etc.)

For document tools installation (--document-tools):
Installs pdftotext, antiword, unrtf for indexing PDF, DOC, and RTF files.

Examples:
  conduit install https://github.com/7nohe/local-mcp-server-sample
  conduit install github.com/modelcontextprotocol/servers/src/filesystem
  conduit install https://github.com/user/mcp-server --name "My Server"
  conduit install --document-tools`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Handle --document-tools flag
			if documentTools {
				inst := installer.New(false)
				_, err := inst.InstallDocumentToolsOnly(cmd.Context())
				return err
			}

			// Require URL for MCP server installation
			if len(args) == 0 {
				return fmt.Errorf("URL required for MCP server installation. Use --document-tools to install document extraction tools")
			}

			repoURL := args[0]
			return runInstall(cmd.Context(), repoURL, name, provider, skipBuild, dryRun)
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Custom name for the MCP server")
	cmd.Flags().StringVar(&provider, "provider", "", "AI provider to use: ollama (default) or anthropic")
	cmd.Flags().BoolVar(&skipBuild, "skip-build", false, "Skip Docker build (just analyze)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be done without doing it")
	cmd.Flags().BoolVar(&documentTools, "document-tools", false, "Install document extraction tools (pdftotext, antiword, unrtf)")

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

	// Select container runtime (prefer Podman)
	selector := containerRuntime.NewSelector(cfg.Runtime.Preferred)
	provider, err := selector.Select(ctx)
	if err != nil {
		fmt.Println()
		fmt.Println("⚠️  No container runtime available")
		fmt.Println("   Install Podman or Docker to build containers automatically.")
		fmt.Println()
		fmt.Println("📋 Manual Build Steps")
		fmt.Println("──────────────────────────────────────────────────────────────")
		fmt.Printf("1. Build: cd %s && docker build -f Dockerfile.conduit -t %s .\n", fetchResult.LocalPath, imageName)
		return nil
	}

	fmt.Printf("   Runtime: %s\n", provider.Name())
	fmt.Println()

	// Build the container
	buildOpts := containerRuntime.BuildOptions{
		ContextDir:     fetchResult.LocalPath,
		DockerfilePath: dockerfilePath,
		ImageName:      imageName,
		NoCache:        false,
		Progress: func(line string) {
			// Show build progress
			if line != "" {
				fmt.Printf("   %s\n", line)
			}
		},
	}

	if err := provider.Build(ctx, buildOpts); err != nil {
		fmt.Printf("   ❌ Build failed: %v\n", err)
		fmt.Println()
		fmt.Println("📋 Try building manually:")
		fmt.Printf("   cd %s && %s build -f Dockerfile.conduit -t %s .\n",
			fetchResult.LocalPath, provider.Name(), imageName)
		return fmt.Errorf("container build failed: %w", err)
	}

	fmt.Println()
	fmt.Println("✓ Container built successfully!")
	fmt.Println()

	// Step 5: Create instance in daemon (if daemon is running)
	c := newClient(socketPath)
	instanceReq := map[string]interface{}{
		"package_id":      fmt.Sprintf("github.com/%s/%s", fetchResult.Owner, fetchResult.RepoName),
		"package_version": "latest",
		"display_name":    customName,
		"image_ref":       imageName,
		"config":          map[string]string{},
	}
	if customName == "" {
		instanceReq["display_name"] = fetchResult.RepoName
	}

	data, err := c.post("/api/v1/instances", instanceReq)
	if err != nil {
		fmt.Println("⚠️  Could not register with daemon (is it running?)")
		fmt.Println("   Run 'conduit service start' to start the daemon")
	} else {
		var resp map[string]interface{}
		json.Unmarshal(data, &resp)
		if instanceID, ok := resp["instance_id"].(string); ok {
			fmt.Printf("✓ Instance registered: %s\n", instanceID)
			fmt.Println()
			fmt.Println("📋 Next Steps")
			fmt.Println("──────────────────────────────────────────────────────────────")
			fmt.Printf("1. Bind to Claude Code: conduit client bind %s --client claude-code\n", instanceID)
			fmt.Println("2. Restart Claude Code")
			fmt.Println("3. Run /mcp in Claude Code to verify")
			return nil
		}
	}

	// Fallback: Show manual configuration steps
	fmt.Println()
	fmt.Println("📋 Add to Claude Code (~/.claude.json or claude_desktop_config.json):")
	runtimeName := provider.Name()
	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			fetchResult.RepoName: map[string]interface{}{
				"command": runtimeName,
				"args":    []string{"run", "-i", "--rm", imageName},
			},
		},
	}
	mcpJSON, _ := json.MarshalIndent(mcpConfig, "   ", "  ")
	fmt.Printf("   %s\n", mcpJSON)
	fmt.Println()
	fmt.Println("Then restart Claude Code and run /mcp to verify")

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
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List connector instances",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/instances")
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"failed to list instances: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("failed to list instances: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
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

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// startCmd starts an instance
func startCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "start <instance-id>",
		Short: "Start a connector instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			c := newClient(socketPath)

			data, err := c.post("/api/v1/instances/"+instanceID+"/start", nil)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"instance_id":"%s","error":"failed to start instance: %s"}`, instanceID, err.Error())
					return nil
				}
				return fmt.Errorf("failed to start instance: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				// Return the response from the API, or construct a success response
				if len(data) > 0 {
					fmt.Println(string(data))
				} else {
					fmt.Printf(`{"success":true,"instance_id":"%s","message":"Started instance"}`, instanceID)
				}
				return nil
			}

			fmt.Printf("Started instance %s\n", instanceID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// stopCmd stops an instance
func stopCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "stop <instance-id>",
		Short: "Stop a connector instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			c := newClient(socketPath)

			data, err := c.post("/api/v1/instances/"+instanceID+"/stop", nil)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"instance_id":"%s","error":"failed to stop instance: %s"}`, instanceID, err.Error())
					return nil
				}
				return fmt.Errorf("failed to stop instance: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				if len(data) > 0 {
					fmt.Println(string(data))
				} else {
					fmt.Printf(`{"success":true,"instance_id":"%s","message":"Stopped instance"}`, instanceID)
				}
				return nil
			}

			fmt.Printf("Stopped instance %s\n", instanceID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// removeCmd removes an instance
func removeCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "remove <instance-id>",
		Short: "Remove a connector instance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]
			c := newClient(socketPath)

			err := c.delete("/api/v1/instances/" + instanceID)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"instance_id":"%s","error":"failed to remove instance: %s"}`, instanceID, err.Error())
					return nil
				}
				return fmt.Errorf("failed to remove instance: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Printf(`{"success":true,"instance_id":"%s","message":"Removed instance"}`, instanceID)
				return nil
			}

			fmt.Printf("Removed instance %s\n", instanceID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// statsCmd shows daemon statistics
func statsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show daemon statistics",
		Long: `Show statistics about the Conduit daemon including instance counts,
knowledge base stats, and resource usage.

Examples:
  conduit stats
  conduit stats --json   # JSON output for GUI`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)

			// Get status data (includes instances, bindings count)
			statusData, err := c.get("/api/v1/status")
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"daemon not running: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("daemon not running: %w", err)
			}

			// Get KB sources
			kbData, _ := c.get("/api/v1/kb/sources")

			var status map[string]interface{}
			json.Unmarshal(statusData, &status)

			var kbResp struct {
				Sources []map[string]interface{} `json:"sources"`
			}
			json.Unmarshal(kbData, &kbResp)

			// Build stats
			stats := make(map[string]interface{})

			// Instance stats
			instanceTotal := 0
			instanceRunning := 0
			if instances, ok := status["instances"].(map[string]interface{}); ok {
				if total, ok := instances["total"].(float64); ok {
					instanceTotal = int(total)
				}
				if running, ok := instances["running"].(float64); ok {
					instanceRunning = int(running)
				}
			}
			stats["instances"] = map[string]int{
				"total":   instanceTotal,
				"running": instanceRunning,
			}

			// Binding stats
			bindingTotal := 0
			if bindings, ok := status["bindings"].(map[string]interface{}); ok {
				if total, ok := bindings["total"].(float64); ok {
					bindingTotal = int(total)
				}
			}
			stats["bindings"] = map[string]int{
				"total": bindingTotal,
			}

			// KB stats
			kbSources := len(kbResp.Sources)
			kbDocs := 0
			kbChunks := 0
			for _, src := range kbResp.Sources {
				if dc, ok := src["doc_count"].(float64); ok {
					kbDocs += int(dc)
				}
				if cc, ok := src["chunk_count"].(float64); ok {
					kbChunks += int(cc)
				}
			}
			stats["knowledge_base"] = map[string]int{
				"sources":   kbSources,
				"documents": kbDocs,
				"chunks":    kbChunks,
			}

			// Daemon info
			if daemon, ok := status["daemon"].(map[string]interface{}); ok {
				stats["daemon"] = daemon
			}

			// JSON output for GUI consumption
			if jsonOutput {
				jsonBytes, _ := json.MarshalIndent(stats, "", "  ")
				fmt.Println(string(jsonBytes))
				return nil
			}

			// Human-readable output
			fmt.Println("Conduit Statistics")
			fmt.Println("══════════════════════════════════════")
			fmt.Println()
			fmt.Println("📊 Instances")
			fmt.Printf("   Total:   %d\n", instanceTotal)
			fmt.Printf("   Running: %d\n", instanceRunning)
			fmt.Println()
			fmt.Println("🔗 Bindings")
			fmt.Printf("   Total:   %d\n", bindingTotal)
			fmt.Println()
			fmt.Println("📚 Knowledge Base")
			fmt.Printf("   Sources:   %d\n", kbSources)
			fmt.Printf("   Documents: %d\n", kbDocs)
			fmt.Printf("   Chunks:    %d\n", kbChunks)

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// permissionsCmd manages instance permissions (Advanced Mode)
func permissionsCmd() *cobra.Command {
	var setPermission string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "permissions <instance-id>",
		Short: "Get or set instance permissions (Advanced Mode)",
		Long: `View or modify permissions for a connector instance.

Permissions control what operations an instance can perform.
This is an Advanced Mode feature for fine-grained access control.

Examples:
  conduit permissions abc123
  conduit permissions abc123 --set "filesystem.read=true"
  conduit permissions abc123 --json   # JSON output for GUI

NOTE: This feature requires daemon API support (coming in future release).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]

			// NOTE: Daemon API for permissions doesn't exist yet
			// Return a structured response indicating feature is planned
			if setPermission != "" {
				// Setting permissions
				if jsonOutput {
					result := map[string]interface{}{
						"success":      false,
						"instance_id":  instanceID,
						"error":        "Permission management API not yet implemented in daemon",
						"feature_note": "This feature is planned for a future release",
					}
					jsonBytes, _ := json.MarshalIndent(result, "", "  ")
					fmt.Println(string(jsonBytes))
					return nil
				}
				fmt.Printf("⚠️  Permission management is not yet implemented\n")
				fmt.Printf("    Instance: %s\n", instanceID)
				fmt.Printf("    This feature is planned for a future release.\n")
				return nil
			}

			// Getting permissions
			if jsonOutput {
				result := map[string]interface{}{
					"success":      false,
					"instance_id":  instanceID,
					"permissions":  []interface{}{},
					"error":        "Permission management API not yet implemented in daemon",
					"feature_note": "This feature is planned for a future release",
				}
				jsonBytes, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(jsonBytes))
				return nil
			}

			fmt.Printf("⚠️  Permission management is not yet implemented\n")
			fmt.Printf("    Instance: %s\n", instanceID)
			fmt.Printf("    This feature is planned for a future release.\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&setPermission, "set", "", "Set permission (format: permission.name=true/false)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// auditCmd shows instance access audit logs (Advanced Mode)
func auditCmd() *cobra.Command {
	var limit int
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "audit <instance-id>",
		Short: "Show instance audit logs (Advanced Mode)",
		Long: `View audit logs for a connector instance.

Audit logs track all access and operations performed by the instance.
This is an Advanced Mode feature for security monitoring.

Examples:
  conduit audit abc123
  conduit audit abc123 --limit 50
  conduit audit abc123 --json   # JSON output for GUI

NOTE: This feature requires daemon API support (coming in future release).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]

			// NOTE: Daemon API for audit doesn't exist yet
			// Return a structured response indicating feature is planned
			if jsonOutput {
				result := map[string]interface{}{
					"success":      false,
					"instance_id":  instanceID,
					"audit_logs":   []interface{}{},
					"error":        "Audit log API not yet implemented in daemon",
					"feature_note": "This feature is planned for a future release",
				}
				jsonBytes, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(jsonBytes))
				return nil
			}

			fmt.Printf("⚠️  Audit logging is not yet implemented\n")
			fmt.Printf("    Instance: %s\n", instanceID)
			fmt.Printf("    Limit: %d entries\n", limit)
			fmt.Printf("    This feature is planned for a future release.\n")
			return nil
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum number of audit entries to show")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// createCmd creates a new connector instance
func createCmd() *cobra.Command {
	var name string
	var version string
	var imageRef string
	var configStr string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "create <package-id>",
		Short: "Create a new connector instance",
		Long: `Create a new connector instance from a package.

The package ID identifies the connector type (e.g., filesystem, github).
The instance will be created but not started. Use 'conduit start' to run it.

Examples:
  conduit create filesystem --name "My Files"
  conduit create github --name "GitHub Repos" --config "token=ghp_xxx"
  conduit create filesystem --json   # JSON output for GUI`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			packageID := args[0]

			// Build request
			req := map[string]interface{}{
				"package_id": packageID,
			}
			if name != "" {
				req["display_name"] = name
			} else {
				req["display_name"] = packageID + "-instance"
			}
			if version != "" {
				req["package_version"] = version
			} else {
				req["package_version"] = "1.0.0"
			}
			if imageRef != "" {
				req["image_ref"] = imageRef
			}
			if configStr != "" {
				// Parse config string (format: key1=value1,key2=value2)
				config := make(map[string]string)
				pairs := strings.Split(configStr, ",")
				for _, pair := range pairs {
					kv := strings.SplitN(pair, "=", 2)
					if len(kv) == 2 {
						config[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
					}
				}
				req["config"] = config
			}

			c := newClient(socketPath)
			data, err := c.post("/api/v1/instances", req)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"failed to create instance: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("failed to create instance: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			instanceID := resp["instance_id"].(string)
			displayName := resp["display_name"].(string)

			fmt.Printf("✓ Created instance: %s\n", displayName)
			fmt.Printf("  ID: %s\n", instanceID)
			fmt.Println()
			fmt.Println("Run 'conduit start " + instanceID + "' to start the instance")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Display name for the instance")
	cmd.Flags().StringVar(&version, "version", "", "Package version (default: 1.0.0)")
	cmd.Flags().StringVar(&imageRef, "image", "", "Docker image reference")
	cmd.Flags().StringVar(&configStr, "config", "", "Instance config (format: key1=value1,key2=value2)")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

// clientCmd is the parent command for AI client operations
func clientCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Manage AI client connections",
		Long: `Manage connections between Conduit and AI clients.

Supported clients:
  - claude-code: Claude Code CLI
  - cursor: Cursor IDE
  - vscode: VS Code with MCP extension
  - gemini-cli: Gemini CLI

Examples:
  conduit client list
  conduit client bind my-server --client claude-code
  conduit client unbind my-server --client claude-code`,
	}

	cmd.AddCommand(clientListCmd())
	cmd.AddCommand(clientBindCmd())
	cmd.AddCommand(clientUnbindCmd())
	cmd.AddCommand(clientBindingsCmd())

	return cmd
}

func clientListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
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

			fmt.Println("Detected AI Clients")
			fmt.Println("═══════════════════════════════════════════════════════")
			fmt.Printf("%-15s %-20s %-10s %s\n", "CLIENT", "NAME", "INSTALLED", "NOTES")
			fmt.Println("───────────────────────────────────────────────────────")

			for _, cl := range clients {
				client := cl.(map[string]interface{})
				installed := "No"
				if b, ok := client["installed"].(bool); ok && b {
					installed = "Yes"
				}
				notes := ""
				if n, ok := client["notes"].(string); ok {
					notes = n
				}
				fmt.Printf("%-15s %-20s %-10s %s\n",
					client["client_id"],
					client["display_name"],
					installed,
					notes,
				)
			}

			return nil
		},
	}
}

func clientBindCmd() *cobra.Command {
	var clientID string
	var scope string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "bind <instance-id>",
		Short: "Bind a connector instance to an AI client",
		Long: `Bind a connector instance to an AI client.

This injects the MCP server configuration into the client's config file,
allowing the AI client to access the connector.

Examples:
  conduit client bind my-server --client claude-code
  conduit client bind abc123 --client cursor --scope user
  conduit client bind my-server --json   # JSON output for GUI`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]

			if clientID == "" {
				clientID = "claude-code" // Default
			}

			c := newClient(socketPath)

			// Create binding request
			req := map[string]interface{}{
				"instance_id": instanceID,
				"client_id":   clientID,
				"scope":       scope,
			}

			data, err := c.post("/api/v1/bindings", req)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"%s"}`, err.Error())
					fmt.Println()
					return nil
				}
				return fmt.Errorf("bind failed: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			if errData, ok := resp["error"]; ok {
				errMap := errData.(map[string]interface{})
				errMsg := fmt.Sprintf("%s", errMap["message"])
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"%s"}`, errMsg)
					fmt.Println()
					return nil
				}
				return fmt.Errorf("%s", errMsg)
			}

			bindingID := resp["binding_id"].(string)

			if jsonOutput {
				result := map[string]interface{}{
					"success":     true,
					"binding_id":  bindingID,
					"instance_id": instanceID,
					"client_id":   clientID,
					"scope":       scope,
				}
				jsonBytes, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(jsonBytes))
				return nil
			}

			fmt.Printf("✓ Bound instance %s to %s\n", instanceID, clientID)
			fmt.Printf("  Binding ID: %s\n", bindingID)
			fmt.Printf("  Scope: %s\n", scope)
			fmt.Println()
			fmt.Printf("Restart %s for the binding to take effect.\n", clientID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&clientID, "client", "c", "claude-code", "Client to bind to")
	cmd.Flags().StringVarP(&scope, "scope", "s", "project", "Binding scope: project, user, workspace")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

func clientUnbindCmd() *cobra.Command {
	var clientID string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "unbind <instance-id>",
		Short: "Unbind a connector instance from an AI client",
		Long: `Unbind a connector instance from an AI client.

This removes the MCP server configuration from the client's config file.

Examples:
  conduit client unbind my-server --client claude-code
  conduit client unbind abc123 --client cursor
  conduit client unbind my-server --json   # JSON output for GUI`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]

			if clientID == "" {
				clientID = "claude-code"
			}

			c := newClient(socketPath)

			// Find the binding
			data, err := c.get("/api/v1/bindings")
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"%s"}`, err.Error())
					fmt.Println()
					return nil
				}
				return fmt.Errorf("list bindings: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			bindings, _ := resp["bindings"].([]interface{})
			var bindingID string

			for _, b := range bindings {
				binding := b.(map[string]interface{})
				if binding["instance_id"] == instanceID && binding["client_id"] == clientID {
					bindingID = binding["binding_id"].(string)
					break
				}
			}

			if bindingID == "" {
				errMsg := fmt.Sprintf("no binding found for instance %s and client %s", instanceID, clientID)
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"%s"}`, errMsg)
					fmt.Println()
					return nil
				}
				return fmt.Errorf("%s", errMsg)
			}

			// Delete the binding
			if err := c.delete("/api/v1/bindings/" + bindingID); err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"%s"}`, err.Error())
					fmt.Println()
					return nil
				}
				return fmt.Errorf("unbind failed: %w", err)
			}

			if jsonOutput {
				result := map[string]interface{}{
					"success":     true,
					"binding_id":  bindingID,
					"instance_id": instanceID,
					"client_id":   clientID,
				}
				jsonBytes, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(jsonBytes))
				return nil
			}

			fmt.Printf("✓ Unbound instance %s from %s\n", instanceID, clientID)
			fmt.Println()
			fmt.Printf("Restart %s for the change to take effect.\n", clientID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&clientID, "client", "c", "claude-code", "Client to unbind from")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

func clientBindingsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "bindings [instance-id]",
		Short: "List bindings for an instance or all bindings",
		Long: `List all client bindings or filter by instance.

Examples:
  conduit client bindings
  conduit client bindings abc123
  conduit client bindings --json   # JSON output for GUI`,
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)

			data, err := c.get("/api/v1/bindings")
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"%s","bindings":[]}`, err.Error())
					fmt.Println()
					return nil
				}
				return fmt.Errorf("list bindings: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			bindings, _ := resp["bindings"].([]interface{})

			// Filter by instance if provided
			var filterInstance string
			if len(args) > 0 {
				filterInstance = args[0]
			}

			var filteredBindings []interface{}
			for _, b := range bindings {
				binding := b.(map[string]interface{})
				instanceID, _ := binding["instance_id"].(string)
				if filterInstance == "" || strings.HasPrefix(instanceID, filterInstance) {
					filteredBindings = append(filteredBindings, binding)
				}
			}

			if jsonOutput {
				result := map[string]interface{}{
					"success":  true,
					"bindings": filteredBindings,
				}
				jsonBytes, _ := json.MarshalIndent(result, "", "  ")
				fmt.Println(string(jsonBytes))
				return nil
			}

			if len(filteredBindings) == 0 {
				fmt.Println("No bindings configured")
				return nil
			}

			fmt.Println("Client Bindings")
			fmt.Println("═══════════════════════════════════════════════════════")
			fmt.Printf("%-12s %-15s %-12s %-10s\n", "BINDING", "CLIENT", "INSTANCE", "SCOPE")
			fmt.Println("───────────────────────────────────────────────────────")

			for _, b := range filteredBindings {
				binding := b.(map[string]interface{})
				bindingID := binding["binding_id"].(string)
				clientID := binding["client_id"].(string)
				instanceID := binding["instance_id"].(string)
				scope := binding["scope"].(string)

				fmt.Printf("%-12s %-15s %-12s %-10s\n",
					truncate(bindingID, 12),
					clientID,
					truncate(instanceID, 12),
					scope,
				)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

// kbCmd is the parent command for knowledge base operations
func kbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "kb",
		Short: "Knowledge base operations",
		Long: `Manage the Conduit knowledge base.

The knowledge base indexes your documents for AI-powered search,
allowing AI clients to find relevant information quickly.

Examples:
  conduit kb add ./docs --name "My Docs"
  conduit kb list
  conduit kb sync
  conduit kb search "authentication"
  conduit kb stats`,
	}

	cmd.AddCommand(kbAddCmd())
	cmd.AddCommand(kbListCmd())
	cmd.AddCommand(kbRemoveCmd())
	cmd.AddCommand(kbSearchCmd())
	cmd.AddCommand(kbSyncCmd())
	cmd.AddCommand(kbStatsCmd())
	cmd.AddCommand(kbMigrateCmd())
	cmd.AddCommand(kbKagSyncCmd())
	cmd.AddCommand(kbKagStatusCmd())
	cmd.AddCommand(kbKagRetryCmd())
	cmd.AddCommand(kbKagDedupeCmd())
	cmd.AddCommand(kbKagVectorizeCmd())
	cmd.AddCommand(kbKagQueryCmd())

	return cmd
}

func kbStatsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stats",
		Short: "Show knowledge base statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)

			// Get all sources
			data, err := c.get("/api/v1/kb/sources")
			if err != nil {
				return fmt.Errorf("get sources: %w", err)
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			sources, _ := resp["sources"].([]interface{})

			totalDocs := 0
			totalChunks := 0
			var totalSize int64 = 0

			fmt.Println("Knowledge Base Statistics")
			fmt.Println("═════════════════════════════════════════")

			if len(sources) == 0 {
				fmt.Println("No sources configured")
				return nil
			}

			for _, src := range sources {
				source := src.(map[string]interface{})
				docCount := int(source["doc_count"].(float64))
				chunkCount := int(source["chunk_count"].(float64))
				sizeBytes := int64(source["size_bytes"].(float64))

				totalDocs += docCount
				totalChunks += chunkCount
				totalSize += sizeBytes
			}

			fmt.Printf("Sources:     %d\n", len(sources))
			fmt.Printf("Documents:   %d\n", totalDocs)
			fmt.Printf("Chunks:      %d\n", totalChunks)
			fmt.Printf("Total Size:  %s\n", formatBytes(totalSize))
			fmt.Println()
			fmt.Println("By Source:")
			fmt.Println("─────────────────────────────────────────")
			fmt.Printf("%-20s %-8s %-8s %s\n", "NAME", "DOCS", "CHUNKS", "SIZE")

			for _, src := range sources {
				source := src.(map[string]interface{})
				name := source["name"].(string)
				docCount := int(source["doc_count"].(float64))
				chunkCount := int(source["chunk_count"].(float64))
				sizeBytes := int64(source["size_bytes"].(float64))

				fmt.Printf("%-20s %-8d %-8d %s\n",
					truncate(name, 20), docCount, chunkCount, formatBytes(sizeBytes))
			}

			return nil
		},
	}
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// formatDuration formats a duration as a human-readable string (e.g., "2h 15m", "45m 30s")
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)

	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func kbAddCmd() *cobra.Command {
	var name string
	var patterns string
	var excludes string
	var syncMode string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "add <path>",
		Short: "Add a folder to the knowledge base",
		Long: `Add a folder to the knowledge base for document indexing.

The folder will be scanned for matching files which are then indexed
for full-text search. By default, common text and code files are indexed.

Examples:
  conduit kb add ./docs --name "Project Docs"
  conduit kb add /path/to/notes --patterns "*.md,*.txt"
  conduit kb add ./src --excludes "node_modules,dist"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourcePath := args[0]

			// Resolve to absolute path
			absPath, err := filepath.Abs(sourcePath)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"resolve path: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("resolve path: %w", err)
			}

			// Check path exists
			info, err := os.Stat(absPath)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"path not accessible: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("path not accessible: %w", err)
			}
			if !info.IsDir() {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"path is not a directory: %s"}`, absPath)
					return nil
				}
				return fmt.Errorf("path is not a directory: %s", absPath)
			}

			// Build request
			req := map[string]interface{}{
				"path": absPath,
			}
			if name != "" {
				req["name"] = name
			} else {
				req["name"] = filepath.Base(absPath)
			}
			if patterns != "" {
				req["patterns"] = strings.Split(patterns, ",")
			}
			if excludes != "" {
				req["excludes"] = strings.Split(excludes, ",")
			}
			if syncMode != "" {
				req["sync_mode"] = syncMode
			}

			c := newClient(socketPath)
			data, err := c.post("/api/v1/kb/sources", req)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"add source: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("add source: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			if errData, ok := resp["error"]; ok {
				errMap := errData.(map[string]interface{})
				return fmt.Errorf("%s", errMap["message"])
			}

			sourceID := resp["source_id"].(string)
			sourceName := resp["name"].(string)

			fmt.Printf("✓ Added source: %s\n", sourceName)
			fmt.Printf("  ID:   %s\n", sourceID)
			fmt.Printf("  Path: %s\n", absPath)
			fmt.Println()
			fmt.Println("Run 'conduit kb sync' to index documents")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Display name for the source")
	cmd.Flags().StringVar(&patterns, "patterns", "", "File patterns to index (comma-separated, e.g., '*.md,*.txt')")
	cmd.Flags().StringVar(&excludes, "excludes", "", "Directories to exclude (comma-separated, e.g., 'node_modules,dist')")
	cmd.Flags().StringVar(&syncMode, "sync", "manual", "Sync mode: manual or auto")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

func kbListCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List knowledge base sources",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			c := newClient(socketPath)
			data, err := c.get("/api/v1/kb/sources")
			if err != nil {
				return fmt.Errorf("failed to list KB sources: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
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

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	return cmd
}

func kbRemoveCmd() *cobra.Command {
	var force bool
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "remove <name-or-id>",
		Short: "Remove a knowledge base source",
		Long: `Remove a knowledge base source and all its indexed documents.

Use 'conduit kb list' to see source names.

Examples:
  conduit kb remove "User Files"
  conduit kb remove test --force`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nameOrID := args[0]

			// Get all sources to find by name or ID
			c := newClient(socketPath)
			data, err := c.get("/api/v1/kb/sources")
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"failed to list sources: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("failed to list sources: %w", err)
			}

			var resp struct {
				Sources []map[string]interface{} `json:"sources"`
			}
			if err := json.Unmarshal(data, &resp); err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"failed to parse sources: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("failed to parse sources: %w", err)
			}
			sources := resp.Sources

			// Find matching source by name, ID, or path
			var matchedSource map[string]interface{}
			for _, src := range sources {
				srcID, _ := src["source_id"].(string)
				srcName, _ := src["name"].(string)
				srcPath, _ := src["path"].(string)
				if srcID == nameOrID || srcName == nameOrID || srcPath == nameOrID {
					matchedSource = src
					break
				}
			}

			if matchedSource == nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"source not found: %s"}`, nameOrID)
					return nil
				}
				return fmt.Errorf("source not found: %s\nUse 'conduit kb list' to see available sources", nameOrID)
			}

			sourceID, _ := matchedSource["source_id"].(string)
			sourceName, _ := matchedSource["name"].(string)
			docCount := 0
			if dc, ok := matchedSource["doc_count"].(float64); ok {
				docCount = int(dc)
			}

			// JSON mode implies force (non-interactive)
			if !jsonOutput && !force && docCount > 0 {
				fmt.Printf("Source '%s' has %d indexed documents.\n", sourceName, docCount)
				if !confirmAction("Remove source and all documents?") {
					fmt.Println("Cancelled")
					return nil
				}
			}

			// Delete the source and get deletion statistics
			respBytes, err := c.deleteWithResponse("/api/v1/kb/sources/" + sourceID)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"remove source failed: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("remove source: %w", err)
			}

			// Parse the response to get deletion statistics
			var deleteResult struct {
				DocumentsDeleted int `json:"documents_deleted"`
				VectorsDeleted   int `json:"vectors_deleted"`
			}
			json.Unmarshal(respBytes, &deleteResult)

			// JSON output for GUI consumption
			if jsonOutput {
				result := map[string]interface{}{
					"success":           true,
					"source_id":         sourceID,
					"source_name":       sourceName,
					"documents_deleted": deleteResult.DocumentsDeleted,
					"vectors_deleted":   deleteResult.VectorsDeleted,
				}
				jsonBytes, _ := json.Marshal(result)
				fmt.Println(string(jsonBytes))
				return nil
			}

			if deleteResult.VectorsDeleted > 0 {
				fmt.Printf("✓ Removed source: %s (%d documents, %d vectors)\n",
					sourceName, deleteResult.DocumentsDeleted, deleteResult.VectorsDeleted)
			} else {
				fmt.Printf("✓ Removed source: %s (%d documents)\n", sourceName, docCount)
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

func kbSearchCmd() *cobra.Command {
	var semantic, fts5, raw, jsonOutput bool
	var contextChunks, limit int
	var minScore, semanticWeight, mmrLambda float64
	var disableMMR, disableRerank bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the knowledge base",
		Long: `Search the knowledge base using hybrid, semantic, or keyword search.

By default, hybrid search uses RRF (Reciprocal Rank Fusion) to combine
results from both semantic (vector) and lexical (FTS5) search. This gives
the best of both worlds: semantic understanding AND exact phrase matching.

The hybrid mode automatically detects:
- Quoted phrases → prioritizes lexical exact matching
- Proper nouns (e.g., "Oak Ridge") → boosts exact matches
- Natural language → balances semantic and lexical

Results are processed by default (merged chunks, filtered boilerplate).
Use --raw to get unprocessed results.

ADVANCED MODE: RAG tuning flags allow fine-grained control over retrieval:
  --min-score         Minimum similarity threshold (0.0-1.0, default 0.0)
  --semantic-weight   Balance between semantic/lexical (0.0-1.0, default 0.5)
  --mmr-lambda        Relevance vs diversity (0.0-1.0, default 0.7)

Examples:
  conduit kb search "how does authentication work"    # Hybrid RRF (default)
  conduit kb search "Oak Ridge laboratories"          # Auto-detects proper noun
  conduit kb search "authentication" --semantic       # Force semantic only
  conduit kb search "class AuthProvider" --fts5       # Force keyword only
  conduit kb search "query" --raw                     # Raw chunks without processing

  # Advanced: Lower threshold for more permissive matching
  conduit kb search "ASL-3 safeguards" --min-score 0.05

  # Advanced: Pure semantic search with low threshold
  conduit kb search "AI safety deployment" --semantic --min-score 0.0

  # Advanced: Higher relevance, less diversity
  conduit kb search "authentication" --mmr-lambda 0.9`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			c := newClient(socketPath)

			// Determine search mode
			mode := "hybrid"
			if semantic && fts5 {
				return fmt.Errorf("cannot use both --semantic and --fts5 flags")
			}
			if semantic {
				mode = "semantic"
			} else if fts5 {
				mode = "fts5"
			}

			// Build API URL with processing options
			apiURL := fmt.Sprintf("/api/v1/kb/search?q=%s&mode=%s", url.QueryEscape(query), mode)
			if raw {
				apiURL += "&raw=true"
			}
			if contextChunks > 0 {
				apiURL += fmt.Sprintf("&context=%d", contextChunks)
			}
			if limit > 0 {
				apiURL += fmt.Sprintf("&limit=%d", limit)
			}

			// Advanced RAG parameters
			if minScore >= 0 {
				apiURL += fmt.Sprintf("&min_score=%.4f", minScore)
			}
			if semanticWeight >= 0 {
				apiURL += fmt.Sprintf("&semantic_weight=%.2f", semanticWeight)
			}
			if mmrLambda >= 0 {
				apiURL += fmt.Sprintf("&mmr_lambda=%.2f", mmrLambda)
			}
			if disableMMR {
				apiURL += "&enable_mmr=false"
			}
			if disableRerank {
				apiURL += "&enable_rerank=false"
			}

			data, err := c.get(apiURL)
			if err != nil {
				if jsonOutput {
					fmt.Printf(`{"success":false,"error":"search failed: %s"}`, err.Error())
					return nil
				}
				return fmt.Errorf("search failed: %w", err)
			}

			// JSON output for GUI consumption
			if jsonOutput {
				fmt.Println(string(data))
				return nil
			}

			var resp map[string]interface{}
			json.Unmarshal(data, &resp)

			results, _ := resp["results"].([]interface{})
			searchMode, _ := resp["search_mode"].(string)

			if len(results) == 0 {
				fmt.Printf("No results found for: %s\n", query)
				return nil
			}

			// Show search mode indicator
			modeLabel := ""
			switch searchMode {
			case "semantic":
				modeLabel = " [semantic]"
			case "fts5", "lexical":
				modeLabel = " [keyword]"
			case "fusion":
				modeLabel = " [hybrid RRF]"
			case "auto":
				modeLabel = " [hybrid]"
			}

			// Check if results are processed (merged)
			isProcessed, _ := resp["processed"].(bool)
			if isProcessed {
				modeLabel += " [processed]"
			}

			fmt.Printf("Found %v results for: %s%s\n\n", resp["total_hits"], query, modeLabel)

			// Display results based on whether they're processed or raw
			if isProcessed {
				// Processed results have merged content
				for _, r := range results {
					result := r.(map[string]interface{})
					path, _ := result["path"].(string)
					content, _ := result["content"].(string)
					chunkCount := 1
					if cc, ok := result["chunk_count"].(float64); ok {
						chunkCount = int(cc)
					}

					// Extract filename for cleaner display
					parts := strings.Split(path, "/")
					filename := path
					if len(parts) > 0 {
						filename = parts[len(parts)-1]
					}

					if chunkCount > 1 {
						fmt.Printf("• %s (%d chunks merged)\n", filename, chunkCount)
					} else {
						fmt.Printf("• %s\n", filename)
					}
					fmt.Printf("  Path: %s\n", path)
					fmt.Printf("  %s\n\n", content)
				}
			} else {
				// Raw results show individual chunks
				for _, r := range results {
					result := r.(map[string]interface{})
					path, _ := result["path"].(string)
					snippet, _ := result["snippet"].(string)

					// Show confidence for semantic results
					confidence, hasConfidence := result["confidence"].(string)
					if hasConfidence && confidence != "" {
						fmt.Printf("• %s [%s]\n  %s\n\n", path, confidence, snippet)
					} else {
						fmt.Printf("• %s\n  %s\n\n", path, snippet)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&semantic, "semantic", false, "Force semantic search (requires Qdrant + Ollama)")
	cmd.Flags().BoolVar(&fts5, "fts5", false, "Force FTS5 keyword search")
	cmd.Flags().BoolVar(&raw, "raw", false, "Return raw chunks without processing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")
	cmd.Flags().IntVar(&contextChunks, "context", 0, "Number of adjacent chunks to include")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum results to return (default: 10)")

	// Advanced RAG tuning flags
	cmd.Flags().Float64Var(&minScore, "min-score", -1, "Minimum similarity threshold (0.0-1.0)")
	cmd.Flags().Float64Var(&semanticWeight, "semantic-weight", -1, "Semantic vs lexical weight (0.0-1.0)")
	cmd.Flags().Float64Var(&mmrLambda, "mmr-lambda", -1, "Relevance vs diversity balance (0.0-1.0)")
	cmd.Flags().BoolVar(&disableMMR, "no-mmr", false, "Disable MMR diversity filtering")
	cmd.Flags().BoolVar(&disableRerank, "no-rerank", false, "Disable semantic reranking")

	return cmd
}

func kbSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync [source-id]",
		Short: "Sync knowledge base sources",
		Long: `Synchronize knowledge base sources to index new and updated documents.

If a source ID is provided, only that source is synced.
If no source ID is provided, all sources are synced.

Examples:
  conduit kb sync                    # Sync all sources
  conduit kb sync abc123-def456      # Sync specific source`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use a longer timeout for sync (10 minutes) - large file embedding can be slow
			c := newClientWithTimeout(socketPath, 10*time.Minute)

			if len(args) > 0 {
				// Sync specific source
				sourceID := args[0]

				fmt.Printf("Syncing source: %s\n", sourceID)
				data, err := c.post("/api/v1/kb/sources/"+sourceID+"/sync", nil)
				if err != nil {
					return fmt.Errorf("sync failed: %w", err)
				}

				var result map[string]interface{}
				json.Unmarshal(data, &result)

				if errData, ok := result["error"]; ok {
					errMap := errData.(map[string]interface{})
					return fmt.Errorf("%s", errMap["message"])
				}

				added := int(result["added"].(float64))
				updated := int(result["updated"].(float64))
				deleted := int(result["deleted"].(float64))

				fmt.Printf("✓ Sync complete\n")
				fmt.Printf("  Added:   %d documents\n", added)
				fmt.Printf("  Updated: %d documents\n", updated)
				fmt.Printf("  Deleted: %d documents\n", deleted)

				// Show semantic search status
				if semanticEnabled, ok := result["semantic_enabled"].(bool); ok {
					if semanticEnabled {
						semanticErrors := 0
						if se, ok := result["semantic_errors"].(float64); ok {
							semanticErrors = int(se)
						}
						if semanticErrors > 0 {
							fmt.Printf("  Vectors: %d documents failed (FTS5 fallback used)\n", semanticErrors)
						} else {
							fmt.Printf("  Vectors: ✓ indexed\n")
						}
					} else {
						fmt.Printf("  Vectors: disabled (Qdrant/Ollama unavailable)\n")
					}
				}

				if errors, ok := result["errors"].([]interface{}); ok && len(errors) > 0 {
					fmt.Printf("  Errors:  %d\n", len(errors))
					for _, e := range errors {
						errInfo := e.(map[string]interface{})
						fmt.Printf("    - %s: %s\n", errInfo["path"], errInfo["message"])
					}
				}
			} else {
				// Sync all sources
				fmt.Println("Syncing all sources...")

				// Get list of sources
				data, err := c.get("/api/v1/kb/sources")
				if err != nil {
					return fmt.Errorf("list sources: %w", err)
				}

				var resp map[string]interface{}
				json.Unmarshal(data, &resp)

				sources, _ := resp["sources"].([]interface{})
				if len(sources) == 0 {
					fmt.Println("No sources to sync")
					return nil
				}

				totalAdded := 0
				totalUpdated := 0
				totalDeleted := 0

				for _, src := range sources {
					source := src.(map[string]interface{})
					sourceID := source["source_id"].(string)
					sourceName := source["name"].(string)

					fmt.Printf("  Syncing: %s... ", sourceName)

					syncData, err := c.post("/api/v1/kb/sources/"+sourceID+"/sync", nil)
					if err != nil {
						fmt.Printf("ERROR: %v\n", err)
						continue
					}

					var result map[string]interface{}
					json.Unmarshal(syncData, &result)

					if errData, ok := result["error"]; ok {
						if errMap, ok := errData.(map[string]interface{}); ok {
							fmt.Printf("ERROR: %s\n", errMap["message"])
						} else {
							fmt.Printf("ERROR: %v\n", errData)
						}
						continue
					}

					// Safely extract numeric fields with nil checks
					var added, updated, deleted, semanticErrors int
					if v, ok := result["added"].(float64); ok {
						added = int(v)
					}
					if v, ok := result["updated"].(float64); ok {
						updated = int(v)
					}
					if v, ok := result["deleted"].(float64); ok {
						deleted = int(v)
					}
					if v, ok := result["semantic_errors"].(float64); ok {
						semanticErrors = int(v)
					}

					totalAdded += added
					totalUpdated += updated
					totalDeleted += deleted

					if semanticErrors > 0 {
						fmt.Printf("done (+%d/~%d/-%d) ⚠️  %d vector indexing errors\n", added, updated, deleted, semanticErrors)
					} else {
						fmt.Printf("done (+%d/~%d/-%d)\n", added, updated, deleted)
					}
				}

				fmt.Println()
				fmt.Printf("✓ Sync complete: %d added, %d updated, %d deleted\n",
					totalAdded, totalUpdated, totalDeleted)
			}

			return nil
		},
	}
}

func kbMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Migrate FTS documents to vector search",
		Long: `Migrate existing FTS5-indexed documents to the vector search index.

This is required to enable semantic search for documents that were indexed
before semantic search was enabled. New documents are automatically indexed
in both FTS5 and vector search.

Requires Qdrant and Ollama to be running.

Examples:
  conduit kb migrate`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use a longer timeout for migration (10 minutes)
			c := newClientWithTimeout(socketPath, 10*time.Minute)

			fmt.Println("Migrating documents to vector search...")
			fmt.Println("This may take a while for large knowledge bases.")
			fmt.Println()

			data, err := c.post("/api/v1/kb/migrate", nil)
			if err != nil {
				return fmt.Errorf("migration failed: %w", err)
			}

			var result map[string]interface{}
			json.Unmarshal(data, &result)

			if errData, ok := result["error"]; ok {
				errMap := errData.(map[string]interface{})
				return fmt.Errorf("%s", errMap["message"])
			}

			migratedVal, ok := result["migrated"]
			if !ok || migratedVal == nil {
				return fmt.Errorf("unexpected response: missing 'migrated' field")
			}
			migrated := int(migratedVal.(float64))
			fmt.Printf("✓ Migration complete: %d documents migrated to vector search\n", migrated)

			return nil
		},
	}
}

// doctorCmd diagnoses issues
func doctorCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose Conduit issues",
		Long: `Run comprehensive diagnostics on the Conduit installation.

Checks:
  - Daemon connectivity and health
  - Container runtime availability (Podman/Docker)
  - Database accessibility
  - AI provider configuration and installed models
  - Semantic search (Qdrant vector database + embeddings)
  - Client configurations
  - Knowledge base status
  - Document extraction tools (PDF, DOC, RTF, DOCX, ODT)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("╔══════════════════════════════════════════════════════════════╗")
			fmt.Println("║                   Conduit Diagnostics                        ║")
			fmt.Println("╚══════════════════════════════════════════════════════════════╝")
			fmt.Println()

			issues := 0
			warnings := 0

			// Load configuration
			cfg, cfgErr := config.Load()
			if cfgErr != nil {
				fmt.Println("❌ Configuration")
				fmt.Printf("   Error loading config: %v\n", cfgErr)
				issues++
			} else {
				fmt.Println("✓ Configuration loaded")
				if verbose {
					fmt.Printf("   Data dir: %s\n", cfg.DataDir)
					fmt.Printf("   Socket:   %s\n", cfg.SocketPath)
				}
			}

			// Check daemon connectivity
			fmt.Println()
			fmt.Println("📡 Daemon Status")
			fmt.Println("────────────────────────────────────────────────────────")

			c := newClient(socketPath)
			healthData, err := c.get("/api/v1/health")
			var daemonStatus map[string]interface{} // Shared across checks
			if err != nil {
				fmt.Println("❌ Daemon not running or unreachable")
				fmt.Printf("   Socket: %s\n", socketPath)
				fmt.Println("   Try: conduit service start")
				issues++
			} else {
				var health map[string]interface{}
				json.Unmarshal(healthData, &health)

				if health["status"] == "healthy" {
					fmt.Println("✓ Daemon is running and healthy")
				} else {
					fmt.Println("⚠️  Daemon is running but unhealthy")
					warnings++
				}

				// Get status info (with dependencies)
				statusData, _ := c.get("/api/v1/status")
				json.Unmarshal(statusData, &daemonStatus)

				if daemon, ok := daemonStatus["daemon"].(map[string]interface{}); ok {
					if verbose {
						fmt.Printf("   Version: %s\n", daemon["version"])
						fmt.Printf("   Uptime:  %s\n", daemon["uptime"])
					}
				}

				if instances, ok := daemonStatus["instances"].(map[string]interface{}); ok {
					total := int(instances["total"].(float64))
					fmt.Printf("   Instances: %d\n", total)
				}
			}

			// Check container runtime
			fmt.Println()
			fmt.Println("🐳 Container Runtime")
			fmt.Println("────────────────────────────────────────────────────────")

			ctx := cmd.Context()
			selector := containerRuntime.NewSelector("")
			runtimes := selector.DetectAll(ctx)

			// Check what daemon is actually using
			daemonRuntime := ""
			daemonContainer := ""
			if daemonStatus != nil {
				if deps, ok := daemonStatus["dependencies"].(map[string]interface{}); ok {
					if container, ok := deps["container_runtime"].(map[string]interface{}); ok {
						if available, ok := container["available"].(bool); ok && available {
							if rt, ok := container["runtime"].(string); ok {
								daemonRuntime = rt
							}
							if cn, ok := container["container"].(string); ok {
								daemonContainer = cn
							}
						}
					}
				}
			}

			anyAvailable := false
			for _, rt := range runtimes {
				if rt.Available {
					anyAvailable = true
					statusMark := "✓"
					extra := ""
					if strings.ToLower(rt.Name) == daemonRuntime {
						statusMark = "★"
						extra = " (used by Conduit)"
					} else if rt.Preferred {
						extra = " (preferred)"
					}
					fmt.Printf("%s %s %s%s\n", statusMark, rt.Name, rt.Version, extra)
				} else {
					fmt.Printf("○ %s (not installed)\n", rt.Name)
				}
			}

			if daemonRuntime != "" && daemonContainer != "" {
				fmt.Printf("   Managed container: %s\n", daemonContainer)
			}

			if !anyAvailable {
				fmt.Println("❌ No container runtime available")
				fmt.Println("   Install Podman or Docker to run MCP servers")
				issues++
			}

			// Check database
			fmt.Println()
			fmt.Println("💾 Database")
			fmt.Println("────────────────────────────────────────────────────────")

			if cfg != nil {
				dbPath := cfg.DatabasePath()
				if info, err := os.Stat(dbPath); err == nil {
					fmt.Println("✓ Database exists")
					if verbose {
						fmt.Printf("   Path: %s\n", dbPath)
						fmt.Printf("   Size: %s\n", formatBytes(info.Size()))
					}
				} else {
					fmt.Println("○ Database not yet created")
					fmt.Println("   Will be created on first use")
				}
			}

			// Check AI provider
			fmt.Println()
			fmt.Println("🤖 AI Provider")
			fmt.Println("────────────────────────────────────────────────────────")

			if cfg != nil {
				fmt.Printf("   Provider: %s\n", cfg.AI.Provider)
				fmt.Printf("   Model:    %s\n", cfg.AI.Model)

				if cfg.AI.Provider == "ollama" {
					// Check if Ollama is running
					if checkOllamaRunning() {
						fmt.Println("✓ Ollama is running")
						// List installed models
						if models, err := getOllamaModels(); err == nil && len(models) > 0 {
							fmt.Println("   Installed models:")
							for _, model := range models {
								marker := "  "
								if model == cfg.AI.Model || strings.HasPrefix(cfg.AI.Model, strings.Split(model, ":")[0]) {
									marker = "→ " // Mark the active model
								}
								fmt.Printf("   %s %s\n", marker, model)
							}
						}
					} else {
						if checkCommand("ollama", "--version") {
							fmt.Println("⚠️  Ollama is installed but not running")
							fmt.Println("   Start with: ollama serve")
							warnings++
						} else {
							fmt.Println("❌ Ollama not installed")
							fmt.Println("   Install from: https://ollama.ai")
							issues++
						}
					}
				} else if cfg.AI.Provider == "anthropic" {
					if os.Getenv("ANTHROPIC_API_KEY") != "" {
						fmt.Println("✓ ANTHROPIC_API_KEY is set")
					} else {
						fmt.Println("❌ ANTHROPIC_API_KEY not set")
						issues++
					}
				} else if cfg.AI.Provider == "openai" {
					if os.Getenv("OPENAI_API_KEY") != "" {
						fmt.Println("✓ OPENAI_API_KEY is set")
					} else {
						fmt.Println("❌ OPENAI_API_KEY not set")
						issues++
					}
				}
			}

			// Check semantic search (Qdrant + embeddings)
			fmt.Println()
			fmt.Println("🔍 Semantic Search")
			fmt.Println("────────────────────────────────────────────────────────")

			// Get daemon's view of semantic search status
			daemonSemanticEnabled := false
			daemonQdrantStatus := ""
			daemonVectorCount := int64(0)
			if daemonStatus != nil {
				if deps, ok := daemonStatus["dependencies"].(map[string]interface{}); ok {
					if semantic, ok := deps["semantic_search"].(map[string]interface{}); ok {
						if enabled, ok := semantic["enabled"].(bool); ok {
							daemonSemanticEnabled = enabled
						}
					}
					if qdrant, ok := deps["qdrant"].(map[string]interface{}); ok {
						if qs, ok := qdrant["status"].(string); ok {
							daemonQdrantStatus = qs
						}
						if vc, ok := qdrant["vectors"].(float64); ok {
							daemonVectorCount = int64(vc)
						}
					}
				}
			}

			qdrantRunning := checkQdrantRunning()
			if qdrantRunning {
				if daemonQdrantStatus != "" && daemonQdrantStatus != "unknown" {
					fmt.Printf("✓ Qdrant vector database: %s\n", daemonQdrantStatus)
				} else {
					fmt.Println("✓ Qdrant vector database is running")
				}
				if daemonVectorCount > 0 {
					fmt.Printf("   Collection: conduit_kb (%d vectors)\n", daemonVectorCount)
				} else if count, err := getQdrantVectorCount(); err == nil {
					fmt.Printf("   Collection: conduit_kb (%d vectors)\n", count)
				} else {
					fmt.Println("   Collection: not yet created (run 'conduit kb sync')")
				}
				if daemonContainer != "" {
					fmt.Println("   Managed by: Conduit (auto-started)")
				}
			} else {
				fmt.Println("⚠️  Qdrant not running")
				fmt.Println("   Semantic search unavailable (using FTS5 fallback)")
				if daemonRuntime != "" {
					fmt.Println("   Conduit will auto-start on daemon restart")
				} else {
					fmt.Println("   Install Docker/Podman for auto-managed Qdrant")
				}
				warnings++
			}

			// Show if daemon has semantic search enabled
			if daemonStatus != nil {
				if daemonSemanticEnabled {
					fmt.Println("   Daemon: Semantic search ENABLED")
				} else {
					fmt.Println("   Daemon: Semantic search DISABLED (FTS5 fallback)")
				}
			}

			// Check for embedding model
			embeddingModel := "nomic-embed-text"
			if models, err := getOllamaModels(); err == nil {
				hasEmbedding := false
				for _, m := range models {
					if strings.Contains(m, "nomic-embed") || strings.Contains(m, "embed") {
						hasEmbedding = true
						embeddingModel = m
						break
					}
				}
				if hasEmbedding {
					fmt.Printf("✓ Embedding model: %s\n", embeddingModel)
				} else {
					fmt.Println("⚠️  No embedding model found")
					fmt.Println("   Pull with: ollama pull nomic-embed-text")
					warnings++
				}
			} else if !checkOllamaRunning() {
				fmt.Println("○ Embedding model check skipped (Ollama not running)")
			}

			// Check KAG (Knowledge Graph)
			fmt.Println()
			fmt.Println("🔮 Knowledge Graph (KAG)")
			fmt.Println("────────────────────────────────────────────────────────")

			if cfg != nil && cfg.KB.KAG.Enabled {
				fmt.Println("✓ KAG is enabled")
				fmt.Printf("   Provider: %s\n", cfg.KB.KAG.Provider)
				if cfg.KB.KAG.PreloadModel {
					fmt.Println("✓ Model preloading is enabled")
					fmt.Println("   Note: Model loads on daemon startup (~4GB RAM)")
				} else {
					fmt.Println("○ Model preloading is disabled")
					fmt.Println("   Model loads on first use (1-2 minute delay)")
				}

				// Check FalkorDB
				if checkFalkorDBRunning() {
					fmt.Println("✓ FalkorDB is running")
				} else {
					fmt.Println("⚠️  FalkorDB not running")
					fmt.Println("   Graph queries will be slower (SQLite fallback)")
					fmt.Println("   Start with: conduit falkordb start")
					warnings++
				}

				// Check KAG extraction model
				if cfg.KB.KAG.Provider == "ollama" {
					kagModel := cfg.KB.KAG.Ollama.Model
					if kagModel == "" {
						kagModel = "mistral:7b-instruct-q4_K_M"
					}
					if models, err := getOllamaModels(); err == nil {
						hasKagModel := false
						for _, m := range models {
							if strings.Contains(m, "mistral") {
								hasKagModel = true
								break
							}
						}
						if hasKagModel {
							fmt.Printf("✓ KAG model available: %s\n", kagModel)
						} else {
							fmt.Printf("⚠️  KAG model not installed: %s\n", kagModel)
							fmt.Println("   Pull with: ollama pull mistral:7b-instruct-q4_K_M")
							warnings++
						}
					} else if !checkOllamaRunning() {
						fmt.Println("○ KAG model check skipped (Ollama not running)")
					}
				}

				// Get KAG extraction stats
				homeDir, _ := os.UserHomeDir()
				dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
				if db, err := store.New(dbPath); err == nil {
					defer db.Close()

					var entityCount, relationCount int
					db.DB().QueryRow("SELECT COUNT(*) FROM kb_entities").Scan(&entityCount)
					db.DB().QueryRow("SELECT COUNT(*) FROM kb_relations").Scan(&relationCount)

					var completed, pending int
					db.DB().QueryRow(`SELECT COUNT(*) FROM kb_extraction_status WHERE status = 'completed'`).Scan(&completed)
					db.DB().QueryRow(`
						SELECT COUNT(*) FROM kb_chunks c
						LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
						WHERE s.status IS NULL
					`).Scan(&pending)

					fmt.Printf("   Entities:  %d\n", entityCount)
					fmt.Printf("   Relations: %d\n", relationCount)
					if pending > 0 {
						fmt.Printf("   Pending:   %d chunks (run 'conduit kb kag-sync')\n", pending)
					} else if completed > 0 {
						fmt.Printf("   Status:    All %d chunks extracted\n", completed)
					}
				}
			} else {
				fmt.Println("○ KAG is disabled")
				fmt.Println("   Enable in config: kb.kag.enabled=true")
			}

			// Check AI clients
			fmt.Println()
			fmt.Println("🔗 AI Clients")
			fmt.Println("────────────────────────────────────────────────────────")

			homeDir, _ := os.UserHomeDir()

			clients := []struct {
				name       string
				configPath string
			}{
				{"Claude Code", filepath.Join(homeDir, ".claude.json")},
				{"Cursor", filepath.Join(homeDir, ".cursor", "mcp.json")},
				{"VS Code", filepath.Join(homeDir, ".vscode", "mcp.json")},
				{"Gemini CLI", filepath.Join(homeDir, ".gemini", "mcp.json")},
			}

			for _, client := range clients {
				if _, err := os.Stat(client.configPath); err == nil {
					fmt.Printf("✓ %s configured\n", client.name)
					if verbose {
						fmt.Printf("   Config: %s\n", client.configPath)
					}
				} else {
					fmt.Printf("○ %s (not configured)\n", client.name)
				}
			}

			// Check knowledge base
			fmt.Println()
			fmt.Println("📚 Knowledge Base")
			fmt.Println("────────────────────────────────────────────────────────")

			if c != nil {
				kbData, err := c.get("/api/v1/kb/sources")
				if err == nil {
					var resp map[string]interface{}
					json.Unmarshal(kbData, &resp)
					sources, _ := resp["sources"].([]interface{})
					if len(sources) > 0 {
						fmt.Printf("✓ %d sources configured\n", len(sources))
					} else {
						fmt.Println("○ No sources configured")
						fmt.Println("   Add with: conduit kb add <path>")
					}
				}
			}

			// Check document extraction tools
			fmt.Println()
			fmt.Println("📄 Document Extraction Tools")
			fmt.Println("────────────────────────────────────────────────────────")

			toolStatus := kb.GetToolStatus()
			missingTools := 0
			for _, tool := range toolStatus {
				if tool.Available {
					if verbose && tool.Path != "" {
						fmt.Printf("✓ %s (%s)\n", tool.Name, tool.Path)
					} else {
						fmt.Printf("✓ %s\n", tool.Name)
					}
				} else {
					fmt.Printf("○ %s (not installed)\n", tool.Name)
					missingTools++
				}
			}

			if missingTools > 0 {
				fmt.Println()
				fmt.Println("   Some document formats may not be indexed.")
				fmt.Println("   Install missing tools: conduit install --document-tools")
				warnings++
			}

			// Summary
			fmt.Println()
			fmt.Println("════════════════════════════════════════════════════════")

			if issues == 0 && warnings == 0 {
				fmt.Println("✓ All checks passed! Conduit is ready to use.")
			} else if issues == 0 {
				fmt.Printf("⚠️  %d warning(s), but Conduit should work.\n", warnings)
			} else {
				fmt.Printf("❌ %d issue(s) found, %d warning(s).\n", issues, warnings)
				fmt.Println("   Fix the issues above and run 'conduit doctor' again.")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information")

	return cmd
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
	cmd.AddCommand(mcpStatusCmd())
	cmd.AddCommand(mcpLogsCmd())
	cmd.AddCommand(mcpConfigureCmd())

	return cmd
}

// mcpConfigureCmd auto-configures the MCP KB server in AI clients
func mcpConfigureCmd() *cobra.Command {
	var clientID string
	var forceOverwrite bool

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Auto-configure MCP KB server in AI clients",
		Long: `Auto-configure the Conduit MCP KB server in AI clients.

This adds the MCP server configuration to the client's config file,
enabling AI-powered document search from your Knowledge Base.

Supported clients:
  - claude-code: Claude Code CLI (~/.claude.json)
  - cursor: Cursor IDE (.cursor/settings/extensions.json)
  - vscode: VS Code (.vscode/settings.json)

Examples:
  conduit mcp configure                    # Configure for Claude Code (default)
  conduit mcp configure --client cursor    # Configure for Cursor IDE
  conduit mcp configure --check            # Check if already configured`,
		RunE: func(cmd *cobra.Command, args []string) error {
			homeDir, _ := os.UserHomeDir()
			var configPath string
			var configKey string

			switch clientID {
			case "claude-code":
				configPath = filepath.Join(homeDir, ".claude.json")
				configKey = "mcpServers"
			case "cursor":
				configPath = filepath.Join(homeDir, ".cursor", "settings", "extensions.json")
				configKey = "mcpServers"
			case "vscode":
				configPath = filepath.Join(homeDir, ".vscode", "settings.json")
				configKey = "mcp.servers"
			default:
				return fmt.Errorf("unsupported client: %s", clientID)
			}

			// Read existing config or create new
			var config map[string]interface{}
			if data, err := os.ReadFile(configPath); err == nil {
				if err := json.Unmarshal(data, &config); err != nil {
					return fmt.Errorf("parse config: %w", err)
				}
			} else {
				config = make(map[string]interface{})
			}

			// Get or create mcpServers section
			var mcpServers map[string]interface{}
			if servers, ok := config[configKey].(map[string]interface{}); ok {
				mcpServers = servers
			} else {
				mcpServers = make(map[string]interface{})
			}

			// Check if already configured
			if _, exists := mcpServers["conduit-kb"]; exists && !forceOverwrite {
				fmt.Println("✓ MCP KB server already configured")
				fmt.Printf("  Client: %s\n", clientID)
				fmt.Printf("  Config: %s\n", configPath)
				return nil
			}

			// Add conduit-kb configuration
			mcpServers["conduit-kb"] = map[string]interface{}{
				"command": "conduit",
				"args":    []string{"mcp", "kb"},
			}
			config[configKey] = mcpServers

			// Ensure directory exists
			if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}

			// Write config with pretty formatting
			data, err := json.MarshalIndent(config, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}

			if err := os.WriteFile(configPath, data, 0644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			fmt.Println("✓ MCP KB server configured")
			fmt.Printf("  Client: %s\n", clientID)
			fmt.Printf("  Config: %s\n", configPath)
			fmt.Println()
			fmt.Printf("Restart %s for the configuration to take effect.\n", clientID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&clientID, "client", "c", "claude-code", "Client to configure (claude-code, cursor, vscode)")
	cmd.Flags().BoolVarP(&forceOverwrite, "force", "f", false, "Overwrite existing configuration")

	return cmd
}

// mcpStdioCmd runs an MCP server over stdio (for connector instances)
func mcpStdioCmd() *cobra.Command {
	var instanceID string

	cmd := &cobra.Command{
		Use:   "stdio",
		Short: "Run MCP server over stdio",
		Long: `Proxy an MCP server over stdio.

This command runs a containerized MCP server with stdin/stdout attached,
allowing AI clients to communicate with it via the MCP protocol.

Example usage in AI client config:
{
  "mcpServers": {
    "my-server": {
      "command": "conduit",
      "args": ["mcp", "stdio", "--instance", "abc123"]
    }
  }
}`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Get instance info from daemon
			c := newClient(socketPath)
			data, err := c.get("/api/v1/instances/" + instanceID)
			if err != nil {
				return fmt.Errorf("instance not found: %w", err)
			}

			var instance map[string]interface{}
			if err := json.Unmarshal(data, &instance); err != nil {
				return fmt.Errorf("parse instance: %w", err)
			}

			// Get image reference
			imageRef, ok := instance["image_ref"].(string)
			if !ok || imageRef == "" {
				return fmt.Errorf("instance has no image reference")
			}

			// Get configuration
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Select runtime
			selector := containerRuntime.NewSelector(cfg.Runtime.Preferred)
			provider, err := selector.Select(ctx)
			if err != nil {
				return fmt.Errorf("no container runtime: %w", err)
			}

			// Build container spec for interactive run
			spec := containerRuntime.ContainerSpec{
				Name:  fmt.Sprintf("conduit-mcp-%s-%d", instanceID[:8], time.Now().Unix()),
				Image: imageRef,
				Stdin: true,
				Security: containerRuntime.SecuritySpec{
					NoNewPrivileges: true,
					DropCapabilities: []string{"ALL"},
				},
				Network: containerRuntime.NetworkSpec{
					Mode: "none", // No network by default for security
				},
			}

			// Apply any instance-specific config
			if envMap, ok := instance["env"].(map[string]interface{}); ok {
				spec.Env = make(map[string]string)
				for k, v := range envMap {
					if str, ok := v.(string); ok {
						spec.Env[k] = str
					}
				}
			}

			// Add instance labels
			spec.Labels = map[string]string{
				"conduit.instance_id": instanceID,
				"conduit.mcp.stdio":   "true",
			}

			// Run the container interactively
			// This will connect stdin/stdout directly to the container
			return provider.RunInteractive(ctx, spec)
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

			// Create FTS5 searcher
			ftsSearcher := kb.NewSearcher(st.DB())

			// Attempt to create semantic searcher (if Qdrant/Ollama available)
			var semanticSearcher *kb.SemanticSearcher
			semanticCfg := kb.SemanticSearchConfig{
				EmbeddingConfig: kb.EmbeddingConfig{
					OllamaHost: "http://localhost:11434",
					Model:      "nomic-embed-text",
					Dimension:  768,
					BatchSize:  10,
				},
				VectorStoreConfig: kb.VectorStoreConfig{
					Host:           "localhost",
					Port:           6334, // gRPC port
					CollectionName: "conduit_kb",
					Dimension:      768,
					BatchSize:      100,
				},
			}

			// Try to create semantic searcher - if it fails, we fall back to FTS5 only
			semanticSearcher, _ = kb.NewSemanticSearcher(st.DB(), semanticCfg)
			// Error is ignored - hybrid searcher works with nil semantic searcher

			// Create hybrid searcher (combines FTS5 + semantic when available)
			hybridSearcher := kb.NewHybridSearcher(ftsSearcher, semanticSearcher)

			// Create and run MCP server with hybrid searcher
			server := kb.NewMCPServer(st.DB(), hybridSearcher)

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

// mcpStatusCmd shows MCP server status and capabilities
func mcpStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show MCP server status and capabilities",
		Long: `Display the status and capabilities of the MCP KB server.

Shows:
- MCP configuration status in AI clients (Claude Code, Cursor, VS Code)
- Search capabilities (FTS5, semantic search availability)
- Qdrant and Ollama connectivity status
- Knowledge base sources and statistics

Use --json for machine-readable output (used by GUI).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()

			// Check MCP configuration in each supported client
			type ClientConfig struct {
				Configured bool   `json:"configured"`
				ConfigPath string `json:"configPath"`
				ServerName string `json:"serverName,omitempty"`
			}

			clients := map[string]ClientConfig{
				"claude-code": {ConfigPath: filepath.Join(homeDir, ".claude.json")},
				"cursor":      {ConfigPath: filepath.Join(homeDir, ".cursor", "settings", "extensions.json")},
				"vscode":      {ConfigPath: filepath.Join(homeDir, ".vscode", "settings.json")},
			}

			// Check each client's configuration
			for name, cfg := range clients {
				configured, serverName := checkMCPClientConfigured(cfg.ConfigPath)
				cfg.Configured = configured
				cfg.ServerName = serverName
				clients[name] = cfg
			}

			// JSON output mode for GUI consumption
			if jsonOutput {
				result := make(map[string]interface{})
				for name, cfg := range clients {
					result[name] = map[string]interface{}{
						"configured": cfg.Configured,
						"configPath": cfg.ConfigPath,
						"serverName": cfg.ServerName,
					}
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			// Human-readable output
			fmt.Println("MCP KB Server Status")
			fmt.Println("════════════════════════════════════════════════════════")

			// Client configuration status
			fmt.Println("\n🔧 Client Configuration:")
			fmt.Println("────────────────────────────────────────────────────────")
			for name, cfg := range clients {
				status := "✗ Not configured"
				if cfg.Configured {
					status = "✓ Configured"
				}
				fmt.Printf("  %-12s %s\n", name+":", status)
				fmt.Printf("    └─ %s\n", cfg.ConfigPath)
			}

			// Open database for capabilities check
			dataDir := filepath.Join(homeDir, ".conduit")
			dbPath := filepath.Join(dataDir, "conduit.db")

			st, err := store.New(dbPath)
			if err != nil {
				// Database not available - skip capabilities section
				fmt.Println("\n⚠️  Knowledge base not initialized. Run 'conduit kb add <path>' first.")
				return nil
			}
			defer st.Close()

			// Detect capabilities
			caps := kb.DetectCapabilities(ctx, st.DB())

			// Capabilities
			fmt.Println("\n📋 Search Capabilities:")
			fmt.Println("────────────────────────────────────────────────────────")
			if caps.FTS5Available {
				fmt.Println("  ✓ FTS5 (keyword search): available")
			} else {
				fmt.Println("  ✗ FTS5 (keyword search): not available")
			}

			if caps.SemanticAvailable {
				fmt.Printf("  ✓ Semantic search: available (model: %s)\n", caps.EmbeddingModel)
			} else {
				fmt.Println("  ✗ Semantic search: not available")
			}

			fmt.Printf("  → Recommended mode: %s\n", caps.SearchMode())

			// Service status
			fmt.Println("\n🔌 Service Connectivity:")
			fmt.Println("────────────────────────────────────────────────────────")
			fmt.Printf("  Qdrant (localhost:6333): %s\n", caps.QdrantStatus)
			fmt.Printf("  Ollama (localhost:11434): %s\n", caps.OllamaStatus)

			// Knowledge base stats
			fmt.Println("\n📚 Knowledge Base:")
			fmt.Println("────────────────────────────────────────────────────────")
			sourceMgr := kb.NewSourceManager(st.DB())
			sources, err := sourceMgr.List(ctx)
			if err != nil {
				fmt.Printf("  Error listing sources: %v\n", err)
			} else if len(sources) == 0 {
				fmt.Println("  No sources indexed. Use 'conduit kb add <path>' to add sources.")
			} else {
				fmt.Printf("  Sources: %d\n", len(sources))
				for _, src := range sources {
					fmt.Printf("    • %s (%d docs, %d chunks)\n", src.Name, src.DocCount, src.ChunkCount)
				}
			}

			// Configuration help if not configured
			anyConfigured := false
			for _, cfg := range clients {
				if cfg.Configured {
					anyConfigured = true
					break
				}
			}

			if !anyConfigured {
				fmt.Println("\n⚙️  To configure MCP in an AI client:")
				fmt.Println("────────────────────────────────────────────────────────")
				fmt.Println("  Run: conduit mcp configure --client <client-name>")
				fmt.Println()
				fmt.Println("  Or add manually to client config:")
				fmt.Println(`  {
    "mcpServers": {
      "conduit-kb": {
        "command": "conduit",
        "args": ["mcp", "kb"]
      }
    }
  }`)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON (for GUI consumption)")

	return cmd
}

// checkMCPClientConfigured checks if conduit-kb MCP server is configured in a client config file
func checkMCPClientConfigured(configPath string) (bool, string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, ""
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return false, ""
	}

	// Check for mcpServers key (Claude Code, Cursor)
	if mcpServers, ok := config["mcpServers"].(map[string]interface{}); ok {
		if _, exists := mcpServers["conduit-kb"]; exists {
			return true, "conduit-kb"
		}
	}

	// Check for mcp.servers key (VS Code style)
	if mcpSection, ok := config["mcp"].(map[string]interface{}); ok {
		if servers, ok := mcpSection["servers"].(map[string]interface{}); ok {
			if _, exists := servers["conduit-kb"]; exists {
				return true, "conduit-kb"
			}
		}
	}

	return false, ""
}

// mcpLogsCmd shows MCP-related logs
func mcpLogsCmd() *cobra.Command {
	var tail int
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show MCP server logs",
		Long: `Display logs from MCP server operations.

Note: The MCP KB server runs synchronously when invoked by an AI client.
This command shows daemon logs related to MCP operations.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// MCP server logs are typically in the daemon log or stderr
			homeDir, _ := os.UserHomeDir()
			logPath := filepath.Join(homeDir, ".conduit", "logs", "mcp.log")

			// Check if log file exists
			if _, err := os.Stat(logPath); os.IsNotExist(err) {
				fmt.Println("MCP Log Status")
				fmt.Println("════════════════════════════════════════════════════════")
				fmt.Println()
				fmt.Println("ℹ️  No MCP logs found.")
				fmt.Println()
				fmt.Println("The MCP KB server runs synchronously when invoked by AI clients.")
				fmt.Println("Logs are written to stderr and captured by the AI client.")
				fmt.Println()
				fmt.Println("To debug MCP issues:")
				fmt.Println("  1. Check your AI client's MCP server logs")
				fmt.Println("  2. Run 'conduit mcp kb' manually and send JSON-RPC requests")
				fmt.Println("  3. Use 'conduit mcp status' to verify capabilities")
				return nil
			}

			// Read and display log file
			file, err := os.Open(logPath)
			if err != nil {
				return fmt.Errorf("open log file: %w", err)
			}
			defer file.Close()

			if follow {
				// Follow mode - tail -f style
				fmt.Printf("Following MCP logs (Ctrl+C to stop)...\n\n")

				// Seek to end minus tail lines
				scanner := bufio.NewScanner(file)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
					if len(lines) > tail {
						lines = lines[1:]
					}
				}

				for _, line := range lines {
					fmt.Println(line)
				}

				// Continue watching for new content
				for {
					select {
					case <-cmd.Context().Done():
						return nil
					default:
						line, err := bufio.NewReader(file).ReadString('\n')
						if err != nil {
							time.Sleep(100 * time.Millisecond)
							continue
						}
						fmt.Print(line)
					}
				}
			} else {
				// Print last N lines
				scanner := bufio.NewScanner(file)
				var lines []string
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
					if tail > 0 && len(lines) > tail {
						lines = lines[1:]
					}
				}

				if len(lines) == 0 {
					fmt.Println("No MCP log entries found.")
				} else {
					for _, line := range lines {
						fmt.Println(line)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&tail, "tail", 50, "Number of lines to show")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")

	return cmd
}

// logsCmd shows instance logs
func logsCmd() *cobra.Command {
	var follow bool
	var tail int
	var since string

	cmd := &cobra.Command{
		Use:   "logs <instance-id>",
		Short: "Show connector instance logs",
		Long: `Display logs from a connector instance.

Shows both container runtime logs and stored application logs.
Use --follow to stream logs in real-time.

Examples:
  conduit logs my-server
  conduit logs abc123 --tail 100
  conduit logs abc123 --follow
  conduit logs abc123 --since 1h`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			instanceID := args[0]

			// Get instance info from daemon
			c := newClient(socketPath)
			data, err := c.get("/api/v1/instances/" + instanceID)
			if err != nil {
				return fmt.Errorf("instance not found: %w", err)
			}

			var instance map[string]interface{}
			json.Unmarshal(data, &instance)

			// Check if instance has a container ID
			containerID, hasContainer := instance["container_id"].(string)
			status := instance["status"].(string)

			fmt.Printf("Logs for instance: %s (status: %s)\n", instanceID, status)
			fmt.Println("═══════════════════════════════════════════════════════")

			// If container is running, get container logs
			if hasContainer && containerID != "" && (status == "running" || status == "stopped") {
				fmt.Println("\n📦 Container Logs:")
				fmt.Println("───────────────────────────────────────────────────────")

				// Build log command
				runtime := detectContainerRuntime()
				if runtime == "" {
					fmt.Println("  (No container runtime detected)")
				} else {
					logArgs := []string{"logs"}
					if tail > 0 {
						logArgs = append(logArgs, "--tail", strconv.Itoa(tail))
					}
					if since != "" {
						logArgs = append(logArgs, "--since", since)
					}
					if follow {
						logArgs = append(logArgs, "-f")
					}
					logArgs = append(logArgs, containerID)

					logCmd := exec.CommandContext(cmd.Context(), runtime, logArgs...)
					logCmd.Stdout = os.Stdout
					logCmd.Stderr = os.Stderr

					if err := logCmd.Run(); err != nil {
						fmt.Printf("  (Container logs unavailable: %v)\n", err)
					}
				}
			} else {
				fmt.Println("\n  (No container running for this instance)")
			}

			// Show stored logs from data directory
			homeDir, _ := os.UserHomeDir()
			logPath := filepath.Join(homeDir, ".conduit", "logs", instanceID+".log")
			if info, err := os.Stat(logPath); err == nil && !info.IsDir() {
				fmt.Println("\n📄 Stored Logs:")
				fmt.Println("───────────────────────────────────────────────────────")

				// Read and display stored logs
				logFile, err := os.Open(logPath)
				if err != nil {
					fmt.Printf("  (Could not read stored logs: %v)\n", err)
				} else {
					defer logFile.Close()

					// If tail is specified, only show last N lines
					if tail > 0 {
						lines := readLastNLines(logFile, tail)
						for _, line := range lines {
							fmt.Println(line)
						}
					} else {
						io.Copy(os.Stdout, logFile)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	cmd.Flags().IntVarP(&tail, "tail", "n", 0, "Number of lines to show from the end")
	cmd.Flags().StringVar(&since, "since", "", "Show logs since timestamp (e.g., '1h', '2024-01-01')")

	return cmd
}

// knownBinaryPaths maps binary names to common installation locations.
// This is needed because Electron apps don't inherit shell PATH.
var knownBinaryPaths = map[string][]string{
	"brew": {
		"/opt/homebrew/bin/brew",            // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/brew",               // macOS Homebrew (Intel)
		"/home/linuxbrew/.linuxbrew/bin/brew", // Linux Homebrew
	},
	"ollama": {
		"/opt/homebrew/bin/ollama",     // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/ollama",        // macOS Homebrew (Intel) / Linux
		"/usr/bin/ollama",              // Linux system install
		"/snap/bin/ollama",             // Linux snap install
	},
	"podman": {
		"/opt/homebrew/bin/podman",     // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/podman",        // macOS Homebrew (Intel) / Linux
		"/usr/bin/podman",              // System package
	},
	"docker": {
		"/opt/homebrew/bin/docker",                                    // Homebrew
		"/usr/local/bin/docker",                                       // Docker Desktop symlink
		"/usr/bin/docker",                                             // System package
		"/Applications/Docker.app/Contents/Resources/bin/docker",      // App bundle
	},
}

// findBinaryPath finds a binary by checking PATH first, then known installation locations.
// Returns the full path if found, empty string otherwise.
func findBinaryPath(cmd string) string {
	// Check PATH first
	if path, err := exec.LookPath(cmd); err == nil {
		return path
	}

	// Check known installation paths
	if paths, ok := knownBinaryPaths[cmd]; ok {
		for _, p := range paths {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}

	return ""
}

// getEmptyAuthFile returns path to an empty auth file for bypassing credential helpers.
// This is needed when running from Electron where credential helpers (like gcloud) aren't in PATH.
func getEmptyAuthFile() string {
	authFile := filepath.Join(os.TempDir(), "conduit-empty-auth.json")
	// Create or verify the file exists with valid JSON content
	if _, err := os.Stat(authFile); os.IsNotExist(err) {
		if err := os.WriteFile(authFile, []byte("{}"), 0600); err != nil {
			return "" // Fall back to no auth file
		}
	}
	return authFile
}

// detectContainerRuntime finds the available container runtime
func detectContainerRuntime() string {
	if findBinaryPath("podman") != "" {
		return "podman"
	}
	if findBinaryPath("docker") != "" {
		return "docker"
	}
	return ""
}

// readLastNLines reads the last N lines from a file
func readLastNLines(f *os.File, n int) []string {
	scanner := bufio.NewScanner(f)
	var lines []string

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}

	return lines
}

// configCmd shows configuration
func configCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show Conduit configuration",
		Long: `Display the current Conduit configuration.

Shows configuration loaded from:
  - ~/.conduit/conduit.yaml
  - /etc/conduit/conduit.yaml
  - Environment variables (CONDUIT_*)

Examples:
  conduit config
  conduit config --all`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			fmt.Println("Conduit Configuration")
			fmt.Println("═══════════════════════════════════════════════════════")

			fmt.Println("\n📁 Paths:")
			fmt.Printf("  Data Directory:  %s\n", cfg.DataDir)
			fmt.Printf("  Socket Path:     %s\n", cfg.SocketPath)
			fmt.Printf("  Database Path:   %s\n", cfg.DatabasePath())
			fmt.Printf("  Log Path:        %s\n", cfg.LogPath())
			fmt.Printf("  Backups Dir:     %s\n", cfg.BackupsDir())

			fmt.Println("\n📝 Logging:")
			fmt.Printf("  Log Level:       %s\n", cfg.LogLevel)
			fmt.Printf("  Log Format:      %s\n", cfg.LogFormat)

			fmt.Println("\n🤖 AI Configuration:")
			fmt.Printf("  Provider:        %s\n", cfg.AI.Provider)
			fmt.Printf("  Model:           %s\n", cfg.AI.Model)
			fmt.Printf("  Endpoint:        %s\n", cfg.AI.Endpoint)
			fmt.Printf("  Timeout:         %d seconds\n", cfg.AI.TimeoutSeconds)
			fmt.Printf("  Confidence:      %.0f%%\n", cfg.AI.ConfidenceThreshold*100)

			fmt.Println("\n🐳 Runtime:")
			fmt.Printf("  Preferred:       %s\n", cfg.Runtime.Preferred)
			fmt.Printf("  Pull Timeout:    %s\n", cfg.Runtime.PullTimeout)
			fmt.Printf("  Start Timeout:   %s\n", cfg.Runtime.StartTimeout)
			fmt.Printf("  Stop Timeout:    %s\n", cfg.Runtime.StopTimeout)

			if showAll {
				fmt.Println("\n📚 Knowledge Base:")
				fmt.Printf("  Workers:         %d\n", cfg.KB.Workers)
				fmt.Printf("  Max File Size:   %s\n", formatBytes(cfg.KB.MaxFileSize))
				fmt.Printf("  Chunk Size:      %d\n", cfg.KB.ChunkSize)
				fmt.Printf("  Chunk Overlap:   %d\n", cfg.KB.ChunkOverlap)

				fmt.Println("\n🔒 Policy:")
				fmt.Printf("  Network Egress:  %v\n", cfg.Policy.AllowNetworkEgress)
				fmt.Println("  Forbidden Paths:")
				for _, p := range cfg.Policy.ForbiddenPaths {
					fmt.Printf("    - %s\n", p)
				}
				fmt.Println("  Warn Paths:")
				for _, p := range cfg.Policy.WarnPaths {
					fmt.Printf("    - %s\n", p)
				}

				fmt.Println("\n⚙️ API:")
				fmt.Printf("  Read Timeout:    %s\n", cfg.API.ReadTimeout)
				fmt.Printf("  Write Timeout:   %s\n", cfg.API.WriteTimeout)
				fmt.Printf("  Idle Timeout:    %s\n", cfg.API.IdleTimeout)
			}

			// Show config file location
			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")
			if _, err := os.Stat(configPath); err == nil {
				fmt.Printf("\n📄 Config File: %s\n", configPath)
			} else {
				fmt.Println("\n📄 Config File: (using defaults, no config file found)")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&showAll, "all", "a", false, "Show all configuration options")

	// Add subcommands
	cmd.AddCommand(configGetCmd())
	cmd.AddCommand(configSetCmd())
	cmd.AddCommand(configUnsetCmd())

	return cmd
}

// configGetCmd retrieves a configuration value
func configGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <key>",
		Short: "Get a configuration value",
		Long: `Get a specific configuration value.

Keys use dot notation to access nested values.

Examples:
  conduit config get ai.model
  conduit config get deps.ollama.path
  conduit config get runtime.preferred`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")

			v := viper.New()
			v.SetConfigFile(configPath)
			v.SetConfigType("yaml")

			if err := v.ReadInConfig(); err != nil {
				// If config file doesn't exist, return empty
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("read config: %w", err)
			}

			value := v.Get(key)
			if value == nil {
				return nil // Key not found, return empty
			}

			// Output the value
			switch v := value.(type) {
			case string:
				fmt.Println(v)
			case bool:
				fmt.Printf("%v\n", v)
			case int, int64, float64:
				fmt.Printf("%v\n", v)
			default:
				// For complex values, output as JSON
				data, _ := json.Marshal(v)
				fmt.Println(string(data))
			}

			return nil
		},
	}
}

// configSetCmd sets a configuration value
func configSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a configuration value",
		Long: `Set a specific configuration value.

Keys use dot notation to access nested values.
Values are stored in ~/.conduit/conduit.yaml.

Examples:
  conduit config set ai.model qwen2.5-coder:7b
  conduit config set deps.ollama.path /custom/path/ollama
  conduit config set runtime.preferred podman`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			value := args[1]

			homeDir, _ := os.UserHomeDir()
			configDir := filepath.Join(homeDir, ".conduit")
			configPath := filepath.Join(configDir, "conduit.yaml")

			// Ensure config directory exists
			if err := os.MkdirAll(configDir, 0700); err != nil {
				return fmt.Errorf("create config directory: %w", err)
			}

			v := viper.New()
			v.SetConfigFile(configPath)
			v.SetConfigType("yaml")

			// Read existing config if it exists
			if err := v.ReadInConfig(); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("read config: %w", err)
				}
				// Config doesn't exist yet, that's fine
			}

			// Set the value
			v.Set(key, value)

			// Write the config
			if err := v.WriteConfig(); err != nil {
				// If the config file doesn't exist, create it
				if os.IsNotExist(err) {
					if err := v.SafeWriteConfig(); err != nil {
						return fmt.Errorf("write config: %w", err)
					}
				} else {
					return fmt.Errorf("write config: %w", err)
				}
			}

			fmt.Printf("Set %s = %s\n", key, value)
			return nil
		},
	}
}

// configUnsetCmd removes a configuration value
func configUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a configuration value",
		Long: `Remove a specific configuration value.

Keys use dot notation to access nested values.
The value will be removed from ~/.conduit/conduit.yaml.

Examples:
  conduit config unset deps.ollama.path
  conduit config unset runtime.preferred`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]

			homeDir, _ := os.UserHomeDir()
			configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")

			// Read current config file directly as YAML
			data, err := os.ReadFile(configPath)
			if err != nil {
				if os.IsNotExist(err) {
					// Config doesn't exist, nothing to unset
					return nil
				}
				return fmt.Errorf("read config: %w", err)
			}

			// Parse the YAML into a map
			var configMap map[string]interface{}
			if err := yaml.Unmarshal(data, &configMap); err != nil {
				return fmt.Errorf("parse config: %w", err)
			}

			// Remove the key using dot notation
			if removeNestedKey(configMap, strings.Split(key, ".")) {
				// Write back
				newData, err := yaml.Marshal(configMap)
				if err != nil {
					return fmt.Errorf("marshal config: %w", err)
				}
				if err := os.WriteFile(configPath, newData, 0600); err != nil {
					return fmt.Errorf("write config: %w", err)
				}
				fmt.Printf("Unset %s\n", key)
			}

			return nil
		},
	}
}

// removeNestedKey removes a nested key from a map using a slice of key parts
func removeNestedKey(m map[string]interface{}, keys []string) bool {
	if len(keys) == 0 {
		return false
	}

	if len(keys) == 1 {
		if _, exists := m[keys[0]]; exists {
			delete(m, keys[0])
			return true
		}
		return false
	}

	// Navigate to nested map
	if nested, ok := m[keys[0]].(map[string]interface{}); ok {
		if removeNestedKey(nested, keys[1:]) {
			// If the nested map is now empty, remove it too
			if len(nested) == 0 {
				delete(m, keys[0])
			}
			return true
		}
	}

	return false
}

// backupCmd creates a backup of Conduit data
func backupCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create a backup of Conduit data",
		Long: `Create a complete backup of the Conduit data directory.

The backup includes:
  - Database (conduit.db)
  - Configuration (conduit.yaml)
  - Knowledge base data
  - Connector configurations

The backup is saved as a compressed tar.gz archive.

Examples:
  conduit backup
  conduit backup --output ~/backups/conduit-backup.tar.gz`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Ensure backups directory exists
			if err := os.MkdirAll(cfg.BackupsDir(), 0700); err != nil {
				return fmt.Errorf("create backups directory: %w", err)
			}

			// Determine output path
			if outputPath == "" {
				timestamp := time.Now().Format("20060102-150405")
				outputPath = filepath.Join(cfg.BackupsDir(), fmt.Sprintf("conduit-backup-%s.tar.gz", timestamp))
			}

			// Resolve to absolute path
			absOutput, err := filepath.Abs(outputPath)
			if err != nil {
				return fmt.Errorf("resolve output path: %w", err)
			}

			fmt.Printf("Creating backup of %s\n", cfg.DataDir)
			fmt.Printf("Output: %s\n", absOutput)
			fmt.Println()

			// Create the backup using tar
			fmt.Println("📦 Backing up data directory...")

			// Create output file
			outFile, err := os.Create(absOutput)
			if err != nil {
				return fmt.Errorf("create backup file: %w", err)
			}
			defer outFile.Close()

			// Use tar command for simplicity and better compatibility
			tarCmd := exec.Command("tar", "-czf", "-", "-C", filepath.Dir(cfg.DataDir), filepath.Base(cfg.DataDir))
			tarCmd.Stdout = outFile

			var stderr bytes.Buffer
			tarCmd.Stderr = &stderr

			if err := tarCmd.Run(); err != nil {
				return fmt.Errorf("create archive: %w (%s)", err, stderr.String())
			}

			// Get file size
			info, _ := os.Stat(absOutput)
			fmt.Printf("\n✓ Backup complete: %s (%s)\n", absOutput, formatBytes(info.Size()))

			// Show what was backed up
			fmt.Println("\nContents:")
			listCmd := exec.Command("tar", "-tzf", absOutput)
			listOut, _ := listCmd.Output()
			lines := strings.Split(string(listOut), "\n")
			shown := 0
			for _, line := range lines {
				if line != "" && shown < 10 {
					fmt.Printf("  %s\n", line)
					shown++
				}
			}
			if len(lines) > 10 {
				fmt.Printf("  ... and %d more files\n", len(lines)-10)
			}

			fmt.Println("\nTo restore, extract the archive to ~/.conduit")

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output path for backup file")

	return cmd
}

// uninstallCmd removes Conduit
func uninstallCmd() *cobra.Command {
	var (
		keepData   bool
		all        bool
		force      bool
		dryRun     bool
		jsonOutput bool
		showInfo   bool
	)

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall Conduit",
		Long: `Remove Conduit daemon service, binaries, and optionally data.

UNINSTALL OPTIONS:
  --keep-data    Remove binaries and service, keep data for reinstall
  --all          Remove everything including data directory

SAFETY FLAGS:
  --force        Skip all confirmations
  --dry-run      Show what would be removed without removing
  --json         Output results as JSON

NOTE: Dependencies (Ollama, container runtimes, containers) are NOT removed.
      These may be shared with other projects. To remove manually:
      - Stop containers: podman stop qdrant falkordb && podman rm qdrant falkordb
      - Remove Ollama: See https://ollama.com/download for uninstall instructions
      - Remove Podman: brew uninstall podman

Examples:
  conduit uninstall                    # Interactive mode
  conduit uninstall --keep-data        # Keep data for reinstall
  conduit uninstall --all --force      # Remove data without prompts
  conduit uninstall --dry-run          # Preview what would be removed
  conduit uninstall --info             # Show what's installed`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst := installer.New(false)
			ctx := cmd.Context()

			// Show info mode
			if showInfo {
				info, err := inst.GetUninstallInfo(ctx)
				if err != nil {
					return fmt.Errorf("failed to get uninstall info: %w", err)
				}
				if jsonOutput {
					enc := json.NewEncoder(os.Stdout)
					enc.SetIndent("", "  ")
					return enc.Encode(info)
				}
				inst.PrintUninstallInfo(info)
				return nil
			}

			// Build options based on flags
			var opts installer.UninstallOptions

			switch {
			case all:
				opts = installer.NewUninstallOptionsAll()
			case keepData:
				opts = installer.NewUninstallOptionsKeepData()
			default:
				// Interactive mode - show current state and ask
				info, err := inst.GetUninstallInfo(ctx)
				if err != nil {
					return fmt.Errorf("failed to get uninstall info: %w", err)
				}
				inst.PrintUninstallInfo(info)

				// If nothing to remove, exit
				if !info.HasDaemonService && !info.HasBinaries && !info.HasDataDir {
					fmt.Println("Nothing to uninstall.")
					return nil
				}

				// Interactive prompts
				fmt.Println("Choose uninstall option:")
				fmt.Println()
				fmt.Println("  [1] Keep Data - Remove service/binaries, keep data for reinstall")
				fmt.Println("  [2] Remove All - Remove service/binaries/data")
				fmt.Println("  [q] Cancel")
				fmt.Println()

				reader := bufio.NewReader(os.Stdin)
				fmt.Print("Enter choice [1/2/q]: ")
				choice, _ := reader.ReadString('\n')
				choice = strings.TrimSpace(choice)

				switch choice {
				case "1":
					opts = installer.NewUninstallOptionsKeepData()
				case "2":
					opts = installer.NewUninstallOptionsAll()
				default:
					fmt.Println("Uninstallation cancelled.")
					return nil
				}
			}

			opts.Force = force
			opts.DryRun = dryRun
			opts.JSON = jsonOutput

			// Confirmation for data removal (unless --force or --dry-run)
			if !force && !dryRun && opts.RemoveDataDir {
				fmt.Println()
				fmt.Println("⚠  WARNING: This will permanently delete all Conduit data!")
				fmt.Println()
				reader := bufio.NewReader(os.Stdin)
				fmt.Print("Type 'UNINSTALL' to confirm: ")
				confirm, _ := reader.ReadString('\n')
				if strings.TrimSpace(confirm) != "UNINSTALL" {
					fmt.Println("Uninstallation cancelled.")
					return nil
				}
			}

			// Execute uninstall
			result, err := inst.UninstallWithOptions(ctx, opts)
			if err != nil {
				return fmt.Errorf("uninstall failed: %w", err)
			}

			// ALWAYS remove GUI state (Electron app userData)
			// This ensures a clean slate on reinstall, regardless of --keep-data flag
			// GUI state should NEVER persist independently of CLI state
			home, _ := os.UserHomeDir()
			electronDataDirs := []string{
				filepath.Join(home, "Library", "Application Support", "conduit-desktop"),  // macOS
				filepath.Join(home, ".config", "conduit-desktop"),                          // Linux
			}

			for _, dir := range electronDataDirs {
				if _, statErr := os.Stat(dir); statErr == nil {
					if dryRun {
						result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove GUI state: %s", dir))
					} else {
						if removeErr := os.RemoveAll(dir); removeErr != nil {
							result.ItemsFailed = append(result.ItemsFailed, fmt.Sprintf("GUI state: %s", dir))
							result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove GUI state %s: %v", dir, removeErr))
						} else {
							result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("GUI state: %s", dir))
						}
					}
				}
			}

			// Output results
			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(result)
			}

			// Print results
			fmt.Println()
			if dryRun {
				fmt.Println("═══════════════════════════════════════════════════════════════")
				fmt.Println("                     DRY RUN - No changes made                  ")
				fmt.Println("═══════════════════════════════════════════════════════════════")
			} else {
				fmt.Println("═══════════════════════════════════════════════════════════════")
				fmt.Println("                     Uninstallation Complete                    ")
				fmt.Println("═══════════════════════════════════════════════════════════════")
			}
			fmt.Println()

			if len(result.ItemsRemoved) > 0 {
				for _, item := range result.ItemsRemoved {
					fmt.Printf("  ✓ %s\n", item)
				}
			}

			if len(result.ItemsFailed) > 0 {
				fmt.Println()
				fmt.Println("Failed to remove:")
				for _, item := range result.ItemsFailed {
					fmt.Printf("  ✗ %s\n", item)
				}
			}

			if len(result.Errors) > 0 {
				fmt.Println()
				fmt.Println("Errors:")
				for _, err := range result.Errors {
					fmt.Printf("  • %s\n", err)
				}
			}

			// Print manual cleanup guidance
			if !dryRun && result.Success {
				fmt.Println()
				fmt.Println("To remove dependencies manually (if no longer needed):")
				fmt.Println("  • Containers: podman stop qdrant falkordb && podman rm qdrant falkordb")
				fmt.Println("  • Ollama: rm -rf ~/.ollama && brew uninstall ollama")
				fmt.Println("  • Podman: podman machine stop && podman machine rm && brew uninstall podman")
			}

			fmt.Println()

			return nil
		},
	}

	// Uninstall options
	cmd.Flags().BoolVar(&keepData, "keep-data", false, "Remove binaries/service, keep data for reinstall")
	cmd.Flags().BoolVar(&all, "all", false, "Remove everything including data directory")

	// Safety flags
	cmd.Flags().BoolVar(&force, "force", false, "Skip all confirmations")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would be removed without removing")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output results as JSON")
	cmd.Flags().BoolVar(&showInfo, "info", false, "Show installation status without uninstalling")

	return cmd
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
			inst := installer.New(false)

			// First, check if the daemon is already running
			if inst.IsDaemonRunning() {
				fmt.Println("✓ Daemon is already running")
				return nil
			}

			// Check if service is installed, if not, install it first
			switch runtime.GOOS {
			case "darwin":
				homeDir, _ := os.UserHomeDir()
				plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "dev.simpleflo.conduit.plist")
				if _, err := os.Stat(plistPath); os.IsNotExist(err) {
					fmt.Println("Service not installed. Installing...")
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
					result := inst.SetupDaemonService(cmd.Context(), daemonPath)
					if result.Error != nil {
						return result.Error
					}
					fmt.Println("✓ Service installed")
				}
				// Now start the service
				if err := exec.Command("launchctl", "start", "dev.simpleflo.conduit").Run(); err != nil {
					return fmt.Errorf("failed to start service: %w", err)
				}
				fmt.Println("✓ Daemon service started")
				return nil

			case "linux":
				homeDir, _ := os.UserHomeDir()
				servicePath := filepath.Join(homeDir, ".config", "systemd", "user", "conduit.service")
				if _, err := os.Stat(servicePath); os.IsNotExist(err) {
					fmt.Println("Service not installed. Installing...")
					// Find the daemon binary
					daemonPath, err := exec.LookPath("conduit-daemon")
					if err != nil {
						conduitPath, err := os.Executable()
						if err != nil {
							return fmt.Errorf("could not find conduit-daemon binary")
						}
						daemonPath = filepath.Join(filepath.Dir(conduitPath), "conduit-daemon")
						if _, err := os.Stat(daemonPath); err != nil {
							return fmt.Errorf("could not find conduit-daemon binary")
						}
					}
					result := inst.SetupDaemonService(cmd.Context(), daemonPath)
					if result.Error != nil {
						return result.Error
					}
					fmt.Println("✓ Service installed")
				}
				if err := exec.Command("systemctl", "--user", "start", "conduit").Run(); err != nil {
					return fmt.Errorf("failed to start service: %w", err)
				}
				fmt.Println("✓ Daemon service started")
				return nil

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
				plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "dev.simpleflo.conduit.plist")
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

// qdrantCmd is the parent command for Qdrant management
func qdrantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "qdrant",
		Short: "Manage Qdrant vector database",
		Long: `Manage the Qdrant vector database for semantic search.

Qdrant enables semantic search - finding documents by meaning,
not just keywords. It runs as a container managed by Conduit.

Examples:
  conduit qdrant install     # Install and start Qdrant
  conduit qdrant status      # Check Qdrant health
  conduit qdrant attach      # Enable semantic search without restart
  conduit qdrant purge       # Clear all vectors (fresh start)`,
	}

	cmd.AddCommand(qdrantInstallCmd())
	cmd.AddCommand(qdrantStartCmd())
	cmd.AddCommand(qdrantStopCmd())
	cmd.AddCommand(qdrantStatusCmd())
	cmd.AddCommand(qdrantAttachCmd())
	cmd.AddCommand(qdrantPurgeCmd())

	return cmd
}

// qdrantInstallCmd installs and starts Qdrant
func qdrantInstallCmd() *cobra.Command {
	var preferRuntime string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and start Qdrant container",
		Long: `Install Qdrant vector database for semantic search.

This command will:
1. Detect available container runtime (Podman preferred, Docker as fallback)
2. On macOS: Start Podman machine if needed
3. Pull the Qdrant image
4. Create and start the conduit-qdrant container
5. Verify Qdrant is healthy

After installation, enable semantic search with:
  conduit qdrant attach`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")

			// Create QdrantManager
			mgr := kb.NewQdrantManager(kb.QdrantConfig{
				DataDir: dataDir,
			})

			// Handle runtime preference
			if preferRuntime != "" {
				mgr.SetContainerRuntime(preferRuntime)
				fmt.Printf("Using %s as container runtime\n", preferRuntime)
			} else {
				// Try Podman first with cascading fallback
				runtime, err := detectContainerRuntimeCascading(ctx, mgr)
				if err != nil {
					return fmt.Errorf("no container runtime available: %w\n\nInstall Podman or Docker first:\n  brew install podman && podman machine init && podman machine start", err)
				}
				fmt.Printf("Using %s as container runtime\n", runtime)
			}

			// Install Qdrant
			fmt.Println("Installing Qdrant...")
			if err := mgr.Install(ctx); err != nil {
				return fmt.Errorf("failed to install Qdrant: %w", err)
			}

			fmt.Println()
			fmt.Println("✓ Qdrant installed and running")
			fmt.Println()
			fmt.Println("Next steps:")
			fmt.Println("  conduit qdrant attach    # Enable semantic search in daemon")
			fmt.Println("  conduit kb sync          # Index documents into vector store")

			return nil
		},
	}

	cmd.Flags().StringVar(&preferRuntime, "runtime", "", "Preferred container runtime (podman or docker)")

	return cmd
}

// detectContainerRuntimeCascading tries Podman first (with machine start on macOS), then Docker
// Uses full binary paths to work correctly when called from Electron (no PATH)
func detectContainerRuntimeCascading(ctx context.Context, mgr *kb.QdrantManager) (string, error) {
	// Get full paths for binaries
	podmanPath := findBinaryPath("podman")
	dockerPath := findBinaryPath("docker")

	// Try Podman first
	if podmanPath != "" {
		if runtime.GOOS == "darwin" {
			// Check if Podman machine is running
			out, err := exec.CommandContext(ctx, podmanPath, "machine", "list", "--format", "{{.Running}}").Output()
			if err == nil && strings.Contains(string(out), "true") {
				mgr.SetContainerRuntime(podmanPath)
				return "podman", nil
			}

			// Machine exists but not running
			out, _ = exec.CommandContext(ctx, podmanPath, "machine", "list", "--format", "{{.Name}}").Output()
			if len(strings.TrimSpace(string(out))) > 0 {
				fmt.Println("Podman machine exists but is not running.")
				fmt.Print("Start Podman machine now? [Y/n]: ")
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(strings.ToLower(input))

				if input == "" || input == "y" || input == "yes" {
					fmt.Println("Starting Podman machine...")
					startCmd := exec.CommandContext(ctx, podmanPath, "machine", "start")
					startCmd.Stdout = os.Stdout
					startCmd.Stderr = os.Stderr
					if err := startCmd.Run(); err != nil {
						fmt.Printf("⚠ Failed to start Podman machine: %v\n", err)
						fmt.Println("Trying Docker as fallback...")
					} else {
						mgr.SetContainerRuntime(podmanPath)
						return "podman", nil
					}
				}
			} else {
				// No machine exists
				fmt.Println("Podman is installed but no machine exists.")
				fmt.Print("Initialize and start Podman machine? [Y/n]: ")
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				input = strings.TrimSpace(strings.ToLower(input))

				if input == "" || input == "y" || input == "yes" {
					fmt.Println("Initializing Podman machine...")
					initCmd := exec.CommandContext(ctx, podmanPath, "machine", "init")
					initCmd.Stdout = os.Stdout
					initCmd.Stderr = os.Stderr
					if err := initCmd.Run(); err != nil {
						fmt.Printf("⚠ Failed to initialize Podman machine: %v\n", err)
					} else {
						fmt.Println("Starting Podman machine...")
						startCmd := exec.CommandContext(ctx, podmanPath, "machine", "start")
						startCmd.Stdout = os.Stdout
						startCmd.Stderr = os.Stderr
						if err := startCmd.Run(); err != nil {
							fmt.Printf("⚠ Failed to start Podman machine: %v\n", err)
						} else {
							mgr.SetContainerRuntime(podmanPath)
							return "podman", nil
						}
					}
					fmt.Println("Trying Docker as fallback...")
				}
			}
		} else {
			// Linux: Podman works natively
			testCmd := exec.CommandContext(ctx, podmanPath, "ps")
			if testCmd.Run() == nil {
				mgr.SetContainerRuntime(podmanPath)
				return "podman", nil
			}
		}
	}

	// Fallback to Docker
	if dockerPath != "" {
		testCmd := exec.CommandContext(ctx, dockerPath, "ps")
		if testCmd.Run() == nil {
			mgr.SetContainerRuntime(dockerPath)
			return "docker", nil
		}
		fmt.Println("Docker is installed but not running.")
	}

	return "", fmt.Errorf("neither Podman nor Docker is available and working")
}

// commandExists checks if a command is available in PATH or known locations
func commandExists(cmd string) bool {
	return findBinaryPath(cmd) != ""
}

// qdrantStartCmd starts an existing Qdrant container
func qdrantStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start existing Qdrant container",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")

			mgr := kb.NewQdrantManager(kb.QdrantConfig{
				DataDir: dataDir,
			})

			// Detect runtime
			if _, err := mgr.DetectContainerRuntime(); err != nil {
				return fmt.Errorf("no container runtime available: %w", err)
			}

			// Check if already running
			health := mgr.CheckHealth(ctx)
			if health.APIReachable {
				fmt.Println("✓ Qdrant is already running")
				return nil
			}

			// Start via EnsureReady which handles starting stopped containers
			fmt.Println("Starting Qdrant...")
			if err := mgr.EnsureReady(ctx); err != nil {
				return fmt.Errorf("failed to start Qdrant: %w", err)
			}

			fmt.Println("✓ Qdrant started")
			return nil
		},
	}
}

// qdrantStopCmd stops the Qdrant container
func qdrantStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop Qdrant container",
		Long: `Stop the Qdrant container (preserves data).

The container can be started again with 'conduit qdrant start'.
All indexed vectors are preserved in ~/.conduit/qdrant.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")

			mgr := kb.NewQdrantManager(kb.QdrantConfig{
				DataDir: dataDir,
			})

			// Detect runtime
			if _, err := mgr.DetectContainerRuntime(); err != nil {
				return fmt.Errorf("no container runtime available: %w", err)
			}

			if err := mgr.Stop(ctx); err != nil {
				return fmt.Errorf("failed to stop Qdrant: %w", err)
			}

			fmt.Println("✓ Qdrant stopped")
			fmt.Println("  Data preserved in ~/.conduit/qdrant")
			fmt.Println("  Restart with: conduit qdrant start")
			return nil
		},
	}
}

// qdrantStatusCmd shows Qdrant status and health
func qdrantStatusCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check Qdrant status and health",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")

			mgr := kb.NewQdrantManager(kb.QdrantConfig{
				DataDir: dataDir,
			})

			// Detect runtime (don't fail if not found)
			runtime, _ := mgr.DetectContainerRuntime()

			fmt.Println("Qdrant Vector Database Status")
			fmt.Println("────────────────────────────────────────────────────────")

			// Container runtime
			if runtime != "" {
				fmt.Printf("Container Runtime: %s\n", runtime)
			} else {
				fmt.Println("Container Runtime: not available")
				fmt.Println("  Install with: brew install podman && podman machine init && podman machine start")
				return nil
			}

			// Health check
			health := mgr.CheckHealth(ctx)

			// API status
			if health.APIReachable {
				fmt.Println("API Status:        ✓ reachable")
			} else {
				fmt.Println("API Status:        ○ not reachable")
				if health.ContainerRunning {
					fmt.Println("  Container is running but API is not responding")
					fmt.Println("  Try: conduit qdrant stop && conduit qdrant start")
				} else {
					fmt.Println("  Start with: conduit qdrant start")
				}
				return nil
			}

			// Collection status
			fmt.Printf("Collection:        %s\n", health.CollectionStatus)
			if health.CollectionStatus == "missing" {
				fmt.Println("  Run 'conduit kb sync' to create collection and index documents")
			}

			// Vector count
			fmt.Printf("Indexed Vectors:   %d\n", health.IndexedVectors)
			fmt.Printf("Total Points:      %d\n", health.TotalPoints)

			// Recovery status
			if health.NeedsRecovery {
				fmt.Println()
				fmt.Println("⚠ Collection needs recovery")
				if health.Error != "" {
					fmt.Printf("  Error: %s\n", health.Error)
				}
				fmt.Println("  Run 'conduit kb sync --force' to rebuild index")
			}

			// Storage path (verbose)
			if verbose {
				fmt.Println()
				fmt.Printf("Storage Path:      %s\n", mgr.GetStorageDir())
				httpPort, grpcPort := mgr.GetPorts()
				fmt.Printf("HTTP Port:         %d\n", httpPort)
				fmt.Printf("gRPC Port:         %d\n", grpcPort)
			}

			// Check daemon semantic search status
			fmt.Println()
			c := newClient(socketPath)
			data, err := c.get("/api/v1/status")
			if err == nil {
				var status map[string]interface{}
				if json.Unmarshal(data, &status) == nil {
					if deps, ok := status["dependencies"].(map[string]interface{}); ok {
						if semantic, ok := deps["semantic_search"].(map[string]interface{}); ok {
							if enabled, ok := semantic["enabled"].(bool); ok {
								if enabled {
									fmt.Println("Daemon Status:     ✓ semantic search enabled")
								} else {
									fmt.Println("Daemon Status:     ○ semantic search not enabled")
									fmt.Println("  Enable with: conduit qdrant attach")
								}
							}
						}
					}
				}
			} else {
				fmt.Println("Daemon Status:     ○ daemon not running")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information")

	return cmd
}

// qdrantAttachCmd enables semantic search in the daemon
func qdrantAttachCmd() *cobra.Command {
	var reindex bool

	cmd := &cobra.Command{
		Use:   "attach",
		Short: "Enable semantic search in daemon",
		Long: `Attach the running daemon to Qdrant and enable semantic search.

This command:
1. Verifies Qdrant is running and healthy
2. Notifies the daemon to initialize semantic search
3. Optionally triggers re-indexing of existing documents

Use this after installing Qdrant to enable semantic search without
restarting the daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")

			// First verify Qdrant is running
			mgr := kb.NewQdrantManager(kb.QdrantConfig{
				DataDir: dataDir,
			})

			health := mgr.CheckHealth(ctx)
			if !health.APIReachable {
				return fmt.Errorf("Qdrant is not running. Start it first with: conduit qdrant start")
			}

			fmt.Println("✓ Qdrant is running")

			// Call daemon API to attach
			c := newClient(socketPath)
			data, err := c.post("/api/v1/qdrant/attach", nil)
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w\n  Is the daemon running? Start with: conduit service start", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal(data, &result); err != nil {
				return fmt.Errorf("invalid response from daemon: %w", err)
			}

			if errMsg, ok := result["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("daemon error: %s", errMsg)
			}

			status := result["status"].(string)
			message := result["message"].(string)

			if status == "already_attached" {
				fmt.Println("✓ Semantic search is already enabled")
			} else {
				fmt.Println("✓", message)
			}

			// Trigger reindex if requested
			if reindex && status == "attached" {
				fmt.Println()
				fmt.Println("Re-indexing documents into vector store...")
				data, err = c.post("/api/v1/qdrant/reindex", nil)
				if err != nil {
					fmt.Printf("⚠ Failed to trigger reindex: %v\n", err)
					fmt.Println("  You can manually reindex with: conduit kb sync")
				} else {
					fmt.Println("✓ Re-indexing started in background")
					fmt.Println("  Check progress with: conduit kb stats")
				}
			} else if status == "attached" {
				fmt.Println()
				fmt.Println("Index existing documents with: conduit kb sync")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&reindex, "reindex", false, "Re-index existing documents after attach")

	return cmd
}

// qdrantPurgeCmd clears all vectors from the Qdrant collection
func qdrantPurgeCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "purge",
		Short: "Clear all vectors from Qdrant",
		Long: `Remove all vectors from the Qdrant collection.

This is useful when:
- You reinstalled Conduit and have orphaned vectors
- You want to start fresh with semantic search
- There's a mismatch between SQLite documents and Qdrant vectors

After purging, run 'conduit kb sync' to re-index all documents.

WARNING: This operation cannot be undone!`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit")

			// Create QdrantManager to check status
			mgr := kb.NewQdrantManager(kb.QdrantConfig{
				DataDir: dataDir,
			})

			// Check if Qdrant is running
			health := mgr.CheckHealth(ctx)
			if !health.APIReachable {
				return fmt.Errorf("Qdrant is not running. Start it first with: conduit qdrant start")
			}

			// Get current vector count
			vectorCount := health.TotalPoints
			if vectorCount == 0 {
				fmt.Println("✓ Qdrant collection is already empty")
				return nil
			}

			// Confirm with user unless force flag is set
			if !force {
				fmt.Printf("This will delete %d vectors from Qdrant.\n", vectorCount)
				fmt.Print("Are you sure you want to continue? [y/N]: ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					fmt.Println("Aborted.")
					return nil
				}
			}

			// Delete all vectors by deleting and recreating the collection
			fmt.Println("Purging Qdrant collection...")

			// Use curl to delete the collection (simpler than importing Qdrant client)
			deleteURL := fmt.Sprintf("http://localhost:%d/collections/conduit_kb", 6333)
			req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
			client := &http.Client{Timeout: 30 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("failed to delete collection: %w", err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
				return fmt.Errorf("failed to delete collection: HTTP %d", resp.StatusCode)
			}

			fmt.Printf("✓ Purged %d vectors from Qdrant\n", vectorCount)
			fmt.Println()
			fmt.Println("The collection will be recreated automatically on next sync.")
			fmt.Println("Re-index documents with: conduit kb sync")

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}

// ============================================================================
// FalkorDB Commands (KAG Graph Database)
// ============================================================================

func falkordbCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "falkordb",
		Short: "Manage FalkorDB graph database for KAG",
		Long: `Manage the FalkorDB graph database for Knowledge-Augmented Generation (KAG).

FalkorDB stores entity-relationship graphs extracted from your documents,
enabling multi-hop reasoning and aggregation queries.

Examples:
  conduit falkordb install     # Install and start FalkorDB
  conduit falkordb status      # Check FalkorDB health
  conduit falkordb stop        # Stop FalkorDB container`,
	}

	cmd.AddCommand(falkordbInstallCmd())
	cmd.AddCommand(falkordbStartCmd())
	cmd.AddCommand(falkordbStopCmd())
	cmd.AddCommand(falkordbStatusCmd())

	return cmd
}

func falkordbInstallCmd() *cobra.Command {
	var preferRuntime string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install and start FalkorDB container",
		Long: `Install FalkorDB graph database for KAG (Knowledge-Augmented Generation).

This command will:
1. Detect available container runtime (Podman preferred, Docker as fallback)
2. Pull the FalkorDB image
3. Create and start the conduit-falkordb container
4. Verify FalkorDB is healthy

After installation, enable KAG with:
  conduit kb kag-sync`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			homeDir, _ := os.UserHomeDir()
			dataDir := filepath.Join(homeDir, ".conduit", "falkordb")

			// Ensure data directory exists
			if err := os.MkdirAll(dataDir, 0755); err != nil {
				return fmt.Errorf("create data directory: %w", err)
			}

			// Detect container runtime - use full path to work from Electron
			var runtimePath string
			if preferRuntime != "" {
				runtimePath = findBinaryPath(preferRuntime)
				if runtimePath == "" {
					return fmt.Errorf("specified runtime %s not found", preferRuntime)
				}
			} else {
				if podmanPath := findBinaryPath("podman"); podmanPath != "" {
					runtimePath = podmanPath
				} else if dockerPath := findBinaryPath("docker"); dockerPath != "" {
					runtimePath = dockerPath
				} else {
					return fmt.Errorf("no container runtime available.\n\nInstall Podman or Docker first:\n  brew install podman && podman machine init && podman machine start")
				}
			}
			fmt.Printf("Using %s as container runtime\n", filepath.Base(runtimePath))

			// Bypass credential helpers (gcloud, docker-credential-desktop, etc.)
			// Podman: use --authfile with empty JSON
			// Docker: use DOCKER_CONFIG env pointing to dir with empty config.json
			isPodman := strings.Contains(filepath.Base(runtimePath), "podman")
			authFile := ""
			dockerConfigDir := ""
			if isPodman {
				authFile = getEmptyAuthFile()
			} else {
				// For Docker, create a temp config directory with empty config.json
				dockerConfigDir = filepath.Join(os.TempDir(), "conduit-docker-config")
				os.MkdirAll(dockerConfigDir, 0700)
				configPath := filepath.Join(dockerConfigDir, "config.json")
				if _, err := os.Stat(configPath); os.IsNotExist(err) {
					os.WriteFile(configPath, []byte("{}"), 0600)
				}
			}

			// Pull FalkorDB image
			fmt.Println("Pulling FalkorDB image...")
			pullArgs := []string{"pull"}
			if authFile != "" {
				pullArgs = append(pullArgs, "--authfile", authFile)
			}
			pullArgs = append(pullArgs, "falkordb/falkordb:latest")
			pullCmd := exec.CommandContext(ctx, runtimePath, pullArgs...)
			if dockerConfigDir != "" {
				pullCmd.Env = append(os.Environ(), "DOCKER_CONFIG="+dockerConfigDir)
			}
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err != nil {
				return fmt.Errorf("pull image: %w", err)
			}

			// Stop and remove existing container if any
			exec.CommandContext(ctx, runtimePath, "stop", "conduit-falkordb").Run()
			exec.CommandContext(ctx, runtimePath, "rm", "conduit-falkordb").Run()

			// Create and start container
			fmt.Println("Starting FalkorDB container...")
			runArgs := []string{"run"}
			if authFile != "" {
				runArgs = append(runArgs, "--authfile", authFile)
			}
			runArgs = append(runArgs, "-d",
				"--name", "conduit-falkordb",
				"-p", "6379:6379",
				"-v", dataDir+":/data",
				"--restart", "unless-stopped",
				"falkordb/falkordb:latest",
			)
			runCmd := exec.CommandContext(ctx, runtimePath, runArgs...)
			if dockerConfigDir != "" {
				runCmd.Env = append(os.Environ(), "DOCKER_CONFIG="+dockerConfigDir)
			}
			if output, err := runCmd.CombinedOutput(); err != nil {
				return fmt.Errorf("start container: %w\n%s", err, string(output))
			}

			// Wait for healthy
			fmt.Println("Waiting for FalkorDB to be ready...")
			for i := 0; i < 30; i++ {
				time.Sleep(time.Second)
				checkCmd := exec.CommandContext(ctx, runtimePath, "exec", "conduit-falkordb", "redis-cli", "PING")
				if output, err := checkCmd.Output(); err == nil && strings.TrimSpace(string(output)) == "PONG" {
					fmt.Println()
					fmt.Println("✓ FalkorDB installed and running")
					fmt.Println()
					fmt.Println("Next steps:")
					fmt.Println("  conduit kb kag-sync    # Extract entities from documents")
					return nil
				}
			}

			return fmt.Errorf("FalkorDB did not become healthy in time")
		},
	}

	cmd.Flags().StringVar(&preferRuntime, "runtime", "", "Preferred container runtime (podman or docker)")

	return cmd
}

func falkordbStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start FalkorDB container",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			runtimePath := findBinaryPath("podman")
			if runtimePath == "" {
				runtimePath = findBinaryPath("docker")
			}
			if runtimePath == "" {
				return fmt.Errorf("no container runtime available")
			}

			startCmd := exec.CommandContext(ctx, runtimePath, "start", "conduit-falkordb")
			if err := startCmd.Run(); err != nil {
				return fmt.Errorf("start container: %w\n\nContainer may not exist. Run: conduit falkordb install", err)
			}

			fmt.Println("✓ FalkorDB started")
			return nil
		},
	}
}

func falkordbStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop FalkorDB container",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			runtimePath := findBinaryPath("podman")
			if runtimePath == "" {
				runtimePath = findBinaryPath("docker")
			}
			if runtimePath == "" {
				return fmt.Errorf("no container runtime available")
			}

			stopCmd := exec.CommandContext(ctx, runtimePath, "stop", "conduit-falkordb")
			if err := stopCmd.Run(); err != nil {
				return fmt.Errorf("stop container: %w", err)
			}

			fmt.Println("✓ FalkorDB stopped")
			return nil
		},
	}
}

func falkordbStatusCmd() *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check FalkorDB status",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			runtimePath := findBinaryPath("podman")
			if runtimePath == "" {
				runtimePath = findBinaryPath("docker")
			}
			if runtimePath == "" {
				fmt.Println("FalkorDB Status")
				fmt.Println("═══════════════════════════════════════")
				fmt.Println("Container:    ✗ no container runtime available")
				return nil
			}

			// Check container status
			inspectCmd := exec.CommandContext(ctx, runtimePath, "inspect", "--format", "{{.State.Status}}", "conduit-falkordb")
			output, err := inspectCmd.Output()
			if err != nil {
				fmt.Println("FalkorDB Status")
				fmt.Println("═══════════════════════════════════════")
				fmt.Println("Container:    ✗ not installed")
				fmt.Println()
				fmt.Println("Install with: conduit falkordb install")
				return nil
			}

			status := strings.TrimSpace(string(output))
			fmt.Println("FalkorDB Status")
			fmt.Println("═══════════════════════════════════════")

			if status == "running" {
				fmt.Println("Container:    ✓ running")

				// Check if FalkorDB responds
				pingCmd := exec.CommandContext(ctx, runtimePath, "exec", "conduit-falkordb", "redis-cli", "PING")
				if pingOutput, err := pingCmd.Output(); err == nil && strings.TrimSpace(string(pingOutput)) == "PONG" {
					fmt.Println("API:          ✓ responding")

					// Get graph stats if verbose
					if verbose {
						// Get list of graphs
						graphCmd := exec.CommandContext(ctx, runtimePath, "exec", "conduit-falkordb", "redis-cli", "GRAPH.LIST")
						if graphOutput, err := graphCmd.Output(); err == nil {
							graphs := strings.TrimSpace(string(graphOutput))
							if graphs != "" {
								fmt.Printf("Graphs:       %s\n", graphs)
							}
						}
					}
				} else {
					fmt.Println("API:          ✗ not responding")
				}
			} else {
				fmt.Printf("Container:    ○ %s\n", status)
				fmt.Println()
				fmt.Println("Start with: conduit falkordb start")
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Show detailed information")

	return cmd
}

// ============================================================================
// KAG (Knowledge-Augmented Generation) Commands
// ============================================================================

func kbKagSyncCmd() *cobra.Command {
	var force bool
	var provider string
	var advanced bool

	cmd := &cobra.Command{
		Use:   "kag-sync",
		Short: "Extract entities from indexed documents",
		Long: `Extract entities and relationships from indexed documents into the knowledge graph.

This command processes chunks from your knowledge base and extracts:
- Named entities (concepts, people, organizations, technologies, etc.)
- Relationships between entities (mentions, defines, relates_to, etc.)

The extracted graph enables multi-hop reasoning queries.

Examples:
  conduit kb kag-sync                    # Extract from all unprocessed chunks
  conduit kb kag-sync --force            # Re-extract from all chunks
  conduit kb kag-sync --advanced         # Show advanced options`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			// Check if KAG is enabled
			if !cfg.KB.KAG.Enabled {
				fmt.Println("KAG is not enabled. Enable it in your config:")
				fmt.Println()
				fmt.Println("  kb:")
				fmt.Println("    kag:")
				fmt.Println("      enabled: true")
				fmt.Println()
				fmt.Println("Or set CONDUIT_KB_KAG_ENABLED=true")
				return nil
			}

			// Open database
			homeDir, _ := os.UserHomeDir()
			dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			// Create KAG config
			kagCfg := kb.DefaultKAGConfig()
			kagCfg.Enabled = true
			kagCfg.Extraction.EnableBackground = false // CLI does synchronous extraction
			if provider != "" {
				kagCfg.Provider = provider
			}

			// Check Ollama is running and model is available
			if kagCfg.Provider == "ollama" {
				if !checkOllamaRunning() {
					return fmt.Errorf("Ollama is not running.\n\nStart with: ollama serve")
				}

				// Check if model is available
				kagModel := kagCfg.Ollama.Model
				if kagModel == "" {
					kagModel = "mistral:7b-instruct-q4_K_M"
				}

				models, err := getOllamaModels()
				if err != nil {
					return fmt.Errorf("cannot list Ollama models: %w", err)
				}

				hasModel := false
				for _, m := range models {
					if strings.Contains(m, "mistral") {
						hasModel = true
						break
					}
				}

				if !hasModel {
					fmt.Printf("KAG extraction model not found: %s\n\n", kagModel)
					fmt.Println("Pull the model first:")
					fmt.Printf("  ollama pull %s\n\n", kagModel)
					fmt.Println("This may take a few minutes to download (~4GB).")
					return nil
				}

				fmt.Printf("Using extraction model: %s\n", kagModel)
			}

			// Create provider
			factory := kb.NewProviderFactory()
			llmProvider, err := factory.CreateProvider(kagCfg)
			if err != nil {
				return fmt.Errorf("create LLM provider: %w", err)
			}
			defer llmProvider.Close()

			// Create graph store (optional - extraction can work without it)
			var graphStore *kb.FalkorDBStore
			graphStore, err = kb.NewFalkorDBStore(kb.FalkorDBStoreConfig{
				Host:      kagCfg.Graph.FalkorDB.Host,
				Port:      kagCfg.Graph.FalkorDB.Port,
				GraphName: kagCfg.Graph.FalkorDB.GraphName,
			})
			if err != nil {
				fmt.Printf("⚠ FalkorDB not available: %v\n", err)
				fmt.Println("  Entities will be stored in SQLite only")
				graphStore = nil
			} else {
				ctx := cmd.Context()
				if err := graphStore.Connect(ctx); err != nil {
					fmt.Printf("⚠ Cannot connect to FalkorDB: %v\n", err)
					fmt.Println("  Entities will be stored in SQLite only")
					graphStore = nil
				} else {
					defer graphStore.Close()
				}
			}

			// Create entity extractor
			extractor, err := kb.NewEntityExtractor(kb.EntityExtractorConfig{
				Provider:   llmProvider,
				DB:         db.DB(),
				GraphStore: graphStore,
				Config:     kagCfg,
				NumWorkers: 2,
			})
			if err != nil {
				return fmt.Errorf("create extractor: %w", err)
			}
			defer extractor.Close()

			// Count total chunks to process FIRST (before opening cursor)
			ctx := cmd.Context()
			var totalChunks int
			if force {
				db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_chunks").Scan(&totalChunks)
			} else {
				db.DB().QueryRowContext(ctx, `
					SELECT COUNT(*) FROM kb_chunks c
					LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
					WHERE s.status IS NULL OR s.status = 'error'
				`).Scan(&totalChunks)
			}

			if totalChunks == 0 {
				fmt.Println("No chunks to process. All documents have been extracted.")
				fmt.Println()
				fmt.Println("Use --force to re-extract all chunks.")
				return nil
			}

			// Now query the actual chunks to process (include title to avoid nested queries)
			var query string
			if force {
				query = `
					SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, '')
					FROM kb_chunks c
					LEFT JOIN kb_documents d ON c.document_id = d.document_id
					ORDER BY c.chunk_id
				`
			} else {
				query = `
					SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, '')
					FROM kb_chunks c
					LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
					LEFT JOIN kb_documents d ON c.document_id = d.document_id
					WHERE s.status IS NULL OR s.status = 'error'
					ORDER BY c.chunk_id
				`
			}

			rows, err := db.DB().QueryContext(ctx, query)
			if err != nil {
				return fmt.Errorf("query chunks: %w", err)
			}

			// Collect all chunks into slice FIRST to avoid SQLite cursor conflicts
			// (storeEntities uses transactions which conflict with open cursors)
			type chunkData struct {
				ChunkID    string
				DocumentID string
				Content    string
				Title      string
			}
			var chunks []chunkData
			for rows.Next() {
				var c chunkData
				if err := rows.Scan(&c.ChunkID, &c.DocumentID, &c.Content, &c.Title); err != nil {
					continue
				}
				chunks = append(chunks, c)
			}
			rows.Close() // Close cursor BEFORE processing

			// Process chunks
			var processed, errors int
			fmt.Printf("Extracting entities from %d chunks...\n", totalChunks)
			fmt.Println()

			// Auto-warmup: Check if model is loaded and warm it up if not
			fmt.Print("Checking model status... ")
			os.Stdout.Sync()

			ollamaBin := findOllamaBinary()
			psOut, psErr := exec.CommandContext(ctx, ollamaBin, "ps").Output()
			modelLoaded := psErr == nil && strings.Contains(string(psOut), "mistral")

			if modelLoaded {
				fmt.Println("✓ Model already loaded")
			} else {
				fmt.Println("model not loaded")
				fmt.Print("Warming up mistral model (this may take 1-2 minutes)... ")
				os.Stdout.Sync()

				warmupStart := time.Now()
				warmupCmd := exec.CommandContext(ctx, ollamaBin, "run", "mistral:7b-instruct-q4_K_M", "hello")
				warmupCmd.Stdin = strings.NewReader("")
				if err := warmupCmd.Run(); err != nil {
					fmt.Printf("✗ warmup failed: %v\n", err)
					fmt.Println("Continuing anyway - first extraction will be slower.")
				} else {
					fmt.Printf("✓ ready (%s)\n", formatDuration(time.Since(warmupStart)))
				}
			}
			fmt.Println()
			os.Stdout.Sync() // Flush output before blocking extraction calls

			// Track timing for ETA calculation
			var totalElapsed time.Duration
			syncStartTime := time.Now()

			for _, chunk := range chunks {
				chunkID := chunk.ChunkID
				documentID := chunk.DocumentID
				content := chunk.Content
				title := chunk.Title

				// Show progress with ETA
				current := processed + errors + 1
				remaining := totalChunks - current + 1

				// Calculate ETA based on average processing time
				var etaStr string
				if current > 1 && totalElapsed > 0 {
					avgPerChunk := totalElapsed / time.Duration(current-1)
					eta := avgPerChunk * time.Duration(remaining)
					etaStr = fmt.Sprintf(" | ETA: %s", formatDuration(eta))
				}

				fmt.Printf("[%d/%d] Processing chunk %s...%s\n", current, totalChunks, chunkID[:16], etaStr)
				os.Stdout.Sync() // Flush before blocking extraction call

				startTime := time.Now()
				result, err := extractor.ExtractFromChunk(ctx, chunkID, documentID, title, content)
				elapsed := time.Since(startTime)
				totalElapsed += elapsed

				if err != nil {
					errors++
					fmt.Printf("        ✗ Error: %v (%.1fs)\n", err, elapsed.Seconds())
					os.Stdout.Sync()
				} else {
					processed++
					fmt.Printf("        ✓ %d entities, %d relations (%.1fs)\n",
						len(result.Entities), len(result.Relations), elapsed.Seconds())
					os.Stdout.Sync()
				}
			}

			// Show completion summary
			totalTime := time.Since(syncStartTime)
			fmt.Println()
			fmt.Println("Extraction Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Processed:   %d chunks in %s\n", processed, formatDuration(totalTime))
			if errors > 0 {
				fmt.Printf("Errors:      %d chunks failed\n", errors)

				// Show error breakdown
				errorRows, err := db.DB().QueryContext(ctx, `
					SELECT error_message FROM kb_extraction_status WHERE status = 'error'
				`)
				if err == nil {
					defer errorRows.Close()
					errorTypes := make(map[string]int)
					for errorRows.Next() {
						var errMsg string
						errorRows.Scan(&errMsg)
						errType := categorizeError(errMsg)
						errorTypes[errType]++
					}
					for errType, count := range errorTypes {
						fmt.Printf("  - %-18s %d\n", errType+":", count)
					}
				}

				fmt.Println()
				fmt.Println("Note: Failed chunks are still searchable via FTS5")
				fmt.Println("Use 'conduit kb kag-retry' to retry failed extractions")
			}

			// Show stats
			stats, _ := extractor.GetExtractionStats(ctx)
			if stats != nil {
				fmt.Println()
				fmt.Println("Knowledge Graph Statistics:")
				fmt.Printf("  Entities:  %d\n", stats["total_entities"])
				fmt.Printf("  Relations: %d\n", stats["total_relations"])
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Re-extract from all chunks, even previously processed")
	cmd.Flags().StringVar(&provider, "provider", "", "LLM provider: ollama, openai, anthropic")
	cmd.Flags().BoolVar(&advanced, "advanced", false, "Show advanced options and verbose output")

	return cmd
}

func kbKagStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kag-status",
		Short: "Show detailed KAG extraction status dashboard",
		Long: `Display a comprehensive dashboard of KAG extraction status including:
- Progress bar with completion percentage
- Entity and relation extraction statistics
- Error breakdown by type
- System resource usage (CPU, RAM, storage)
- Ollama model status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			homeDir, _ := os.UserHomeDir()
			dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			fmt.Println()
			fmt.Println("KAG Extraction Status")
			fmt.Println("═══════════════════════════════════════════════════════════")
			fmt.Println()

			// Get status counts
			statusCounts := make(map[string]int)
			rows, err := db.DB().QueryContext(ctx, `
				SELECT status, COUNT(*) as count
				FROM kb_extraction_status
				GROUP BY status
			`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var status string
					var count int
					rows.Scan(&status, &count)
					statusCounts[status] = count
				}
			}

			// Count pending (no status)
			var pendingCount int
			db.DB().QueryRowContext(ctx, `
				SELECT COUNT(*) FROM kb_chunks c
				LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
				WHERE s.status IS NULL
			`).Scan(&pendingCount)
			if pendingCount > 0 {
				statusCounts["pending"] = pendingCount
			}

			// Calculate totals
			var totalChunks int
			db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_chunks").Scan(&totalChunks)

			completedCount := statusCounts["completed"]
			errorCount := statusCounts["error"]
			pendingTotal := pendingCount

			// Progress bar
			fmt.Println("Progress:")
			progressPercent := 0.0
			if totalChunks > 0 {
				progressPercent = float64(completedCount+errorCount) / float64(totalChunks) * 100
			}

			barWidth := 40
			filledWidth := int(float64(barWidth) * progressPercent / 100)
			bar := strings.Repeat("█", filledWidth) + strings.Repeat("░", barWidth-filledWidth)
			fmt.Printf("  %s %d/%d chunks (%.1f%%)\n", bar, completedCount+errorCount, totalChunks, progressPercent)
			fmt.Println()

			// Status breakdown
			fmt.Printf("  Completed:  %d (%.1f%%)\n", completedCount, float64(completedCount)/float64(totalChunks)*100)
			fmt.Printf("  Errors:     %d (%.1f%%)\n", errorCount, float64(errorCount)/float64(totalChunks)*100)
			fmt.Printf("  Pending:    %d (%.1f%%)\n", pendingTotal, float64(pendingTotal)/float64(totalChunks)*100)
			fmt.Println()

			// Entity and relation counts
			var entityCount, relationCount int
			db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_entities").Scan(&entityCount)
			db.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_relations").Scan(&relationCount)

			fmt.Println("Entities & Relations:")
			fmt.Println("───────────────────────────────────────────────────────────")
			fmt.Printf("  Entities:   %d extracted\n", entityCount)
			fmt.Printf("  Relations:  %d extracted\n", relationCount)
			if completedCount > 0 {
				fmt.Printf("  Avg/chunk:  %.1f entities, %.1f relations\n",
					float64(entityCount)/float64(completedCount),
					float64(relationCount)/float64(completedCount))
			}
			fmt.Println()

			// Error breakdown (if errors exist)
			if errorCount > 0 {
				fmt.Println("Error Breakdown:")
				fmt.Println("───────────────────────────────────────────────────────────")

				errorRows, err := db.DB().QueryContext(ctx, `
					SELECT error_message FROM kb_extraction_status WHERE status = 'error'
				`)
				if err == nil {
					defer errorRows.Close()
					errorTypes := make(map[string]int)
					for errorRows.Next() {
						var errMsg string
						errorRows.Scan(&errMsg)
						errType := categorizeError(errMsg)
						errorTypes[errType]++
					}
					for errType, count := range errorTypes {
						fmt.Printf("  %-20s %d chunks\n", errType+":", count)
					}
				}
				fmt.Println()
			}

			// System Resources
			fmt.Println("System Resources:")
			fmt.Println("───────────────────────────────────────────────────────────")

			// RAM usage (Go runtime)
			var memStats runtime.MemStats
			runtime.ReadMemStats(&memStats)
			ramMB := float64(memStats.Alloc) / 1024 / 1024
			fmt.Printf("  RAM:        %.1f MB (Go process)\n", ramMB)

			// Storage usage (.conduit directory)
			conduitDir := filepath.Join(homeDir, ".conduit")
			var totalSize int64
			filepath.Walk(conduitDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					totalSize += info.Size()
				}
				return nil
			})
			storageMB := float64(totalSize) / 1024 / 1024
			fmt.Printf("  Storage:    %.1f MB (~/.conduit/)\n", storageMB)

			// CPU cores
			fmt.Printf("  CPU Cores:  %d available\n", runtime.NumCPU())
			fmt.Println()

			// Ollama Status
			fmt.Println("Ollama Status:")
			fmt.Println("───────────────────────────────────────────────────────────")

			ollamaBin := findOllamaBinary()
			ollamaOut, err := exec.Command(ollamaBin, "ps").Output()
			if err != nil {
				fmt.Println("  Status:     not running or not accessible")
			} else {
				lines := strings.Split(strings.TrimSpace(string(ollamaOut)), "\n")
				if len(lines) <= 1 {
					fmt.Println("  Status:     running (no models loaded)")
				} else {
					// Parse loaded models
					for i, line := range lines {
						if i == 0 {
							continue // Skip header
						}
						fields := strings.Fields(line)
						if len(fields) >= 4 {
							modelName := fields[0]
							size := fields[2]
							until := strings.Join(fields[4:], " ")
							fmt.Printf("  Model:      %s\n", modelName)
							fmt.Printf("  Size:       %s\n", size)
							fmt.Printf("  Until:      %s\n", until)
						}
					}
				}
			}
			fmt.Println()

			// Suggested commands
			fmt.Println("Commands:")
			fmt.Println("───────────────────────────────────────────────────────────")
			if errorCount > 0 {
				fmt.Println("  conduit kb kag-retry        # Retry failed chunks")
			}
			if pendingTotal > 0 {
				fmt.Println("  conduit kb kag-sync         # Continue extraction")
			}
			fmt.Println("  conduit kb kag-sync --force # Re-extract all chunks")
			fmt.Println()

			return nil
		},
	}
}

func kbKagRetryCmd() *cobra.Command {
	var chunkIDs []string
	var maxRetries int
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "kag-retry",
		Short: "Retry failed KAG extractions",
		Long: `Retry entity extraction for failed chunks.

Without flags, retries all failed chunks. Use --chunk-id to retry specific chunks.

Examples:
  conduit kb kag-retry                    # Retry all failed chunks
  conduit kb kag-retry --chunk-id abc123  # Retry specific chunk
  conduit kb kag-retry --dry-run          # Preview what would be retried
  conduit kb kag-retry --max-retries 3    # Retry with 3 attempts`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			homeDir, _ := os.UserHomeDir()
			dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Build query for failed chunks
			var failedChunks []struct {
				ChunkID    string
				DocumentID string
				Content    string
				Title      string
				Error      string
			}

			if len(chunkIDs) > 0 {
				// Specific chunks
				for _, cid := range chunkIDs {
					var chunk struct {
						ChunkID    string
						DocumentID string
						Content    string
						Title      string
						Error      string
					}
					err := db.DB().QueryRowContext(ctx, `
						SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, ''), COALESCE(s.error_message, '')
						FROM kb_chunks c
						LEFT JOIN kb_documents d ON c.document_id = d.document_id
						LEFT JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
						WHERE c.chunk_id = ? AND s.status = 'error'
					`, cid).Scan(&chunk.ChunkID, &chunk.DocumentID, &chunk.Content, &chunk.Title, &chunk.Error)
					if err == nil {
						failedChunks = append(failedChunks, chunk)
					}
				}
			} else {
				// All failed chunks
				rows, err := db.DB().QueryContext(ctx, `
					SELECT c.chunk_id, c.document_id, c.content, COALESCE(d.title, ''), COALESCE(s.error_message, '')
					FROM kb_chunks c
					JOIN kb_extraction_status s ON c.chunk_id = s.chunk_id
					LEFT JOIN kb_documents d ON c.document_id = d.document_id
					WHERE s.status = 'error'
				`)
				if err != nil {
					return fmt.Errorf("query failed chunks: %w", err)
				}
				defer rows.Close()

				for rows.Next() {
					var chunk struct {
						ChunkID    string
						DocumentID string
						Content    string
						Title      string
						Error      string
					}
					if err := rows.Scan(&chunk.ChunkID, &chunk.DocumentID, &chunk.Content, &chunk.Title, &chunk.Error); err != nil {
						continue
					}
					failedChunks = append(failedChunks, chunk)
				}
			}

			if len(failedChunks) == 0 {
				fmt.Println("No failed chunks to retry")
				return nil
			}

			fmt.Printf("Found %d failed chunks\n", len(failedChunks))

			// Show error breakdown
			errorCounts := make(map[string]int)
			for _, chunk := range failedChunks {
				errType := categorizeError(chunk.Error)
				errorCounts[errType]++
			}
			fmt.Println("\nError breakdown:")
			for errType, count := range errorCounts {
				fmt.Printf("  %-20s %d chunks\n", errType+":", count)
			}
			fmt.Println()

			if dryRun {
				fmt.Println("Dry run mode - no changes made")
				fmt.Println("\nChunks that would be retried:")
				for i, chunk := range failedChunks {
					if i >= 10 {
						fmt.Printf("  ... and %d more\n", len(failedChunks)-10)
						break
					}
					fmt.Printf("  %s: %s\n", chunk.ChunkID[:12], truncateString(chunk.Error, 50))
				}
				return nil
			}

			// Create Ollama provider
			ollamaHost := "http://localhost:11434"
			ollamaModel := "mistral:7b-instruct-q4_K_M"

			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{
				Host:  ollamaHost,
				Model: ollamaModel,
			})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}
			defer provider.Close()

			// Check if provider is available
			if !provider.IsAvailable(ctx) {
				return fmt.Errorf("Ollama is not available at %s", ollamaHost)
			}

			// Warm up model
			fmt.Printf("Warming up %s model...", ollamaModel)
			if err := provider.WarmUp(ctx); err != nil {
				fmt.Println(" failed")
				return fmt.Errorf("warmup failed: %w", err)
			}
			fmt.Println(" ready")

			// Create extractor config
			kagCfg := kb.DefaultKAGConfig()
			if maxRetries > 0 {
				kagCfg.Extraction.RetryAttempts = maxRetries
			}

			extractor, err := kb.NewEntityExtractor(kb.EntityExtractorConfig{
				Provider: provider,
				DB:       db.DB(),
				Config:   kagCfg,
			})
			if err != nil {
				return fmt.Errorf("create extractor: %w", err)
			}
			defer extractor.Close()

			// Process failed chunks
			fmt.Printf("\nRetrying %d chunks (max %d attempts each):\n", len(failedChunks), kagCfg.Extraction.RetryAttempts)

			successCount := 0
			failCount := 0
			startTime := time.Now()

			for i, chunk := range failedChunks {
				fmt.Printf("[%d/%d] %s...", i+1, len(failedChunks), chunk.ChunkID[:12])

				result, err := extractor.ExtractFromChunkWithRetry(
					ctx,
					chunk.ChunkID,
					chunk.DocumentID,
					chunk.Title,
					chunk.Content,
					maxRetries,
				)

				if err != nil {
					fmt.Printf(" failed: %s\n", truncateString(err.Error(), 40))
					failCount++
				} else {
					fmt.Printf(" ✓ %d entities, %d relations\n", len(result.Entities), len(result.Relations))
					successCount++
				}
			}

			elapsed := time.Since(startTime)
			fmt.Println()
			fmt.Println("Retry Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Successful:  %d chunks\n", successCount)
			fmt.Printf("Failed:      %d chunks\n", failCount)
			fmt.Printf("Duration:    %s\n", elapsed.Round(time.Second))

			if failCount > 0 {
				fmt.Println("\nSome chunks still failed. Check 'conduit kb kag-status' for details.")
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&chunkIDs, "chunk-id", nil, "Specific chunk IDs to retry (can repeat)")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 0, "Maximum retry attempts (default: 2, max: 5)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without executing")

	return cmd
}

// categorizeError classifies extraction errors into categories
func categorizeError(errMsg string) string {
	errLower := strings.ToLower(errMsg)

	if strings.Contains(errLower, "incomplete json") || strings.Contains(errLower, "incomplete") {
		return "Incomplete JSON"
	}
	if strings.Contains(errLower, "invalid escape") || strings.Contains(errLower, "\\_") {
		return "Invalid escape"
	}
	if strings.Contains(errLower, "array") || strings.Contains(errLower, "schema") || strings.Contains(errLower, "type mismatch") {
		return "Schema mismatch"
	}
	if strings.Contains(errLower, "timeout") {
		return "Timeout"
	}
	if strings.Contains(errLower, "connection") || strings.Contains(errLower, "unavailable") {
		return "Connection"
	}
	if strings.Contains(errLower, "parse json") || strings.Contains(errLower, "no json found") {
		return "Parse error"
	}

	return "Other"
}

// truncateString truncates a string to the specified length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func kbKagDedupeCmd() *cobra.Command {
	var dryRun bool

	cmd := &cobra.Command{
		Use:   "kag-dedupe",
		Short: "Deduplicate entities in the knowledge graph",
		Long: `Merge duplicate entities that have the same normalized name and type.

This command identifies entities that are semantically the same (e.g., "Threat Model"
and "threat model") and merges them, keeping the highest confidence and best description.

Examples:
  conduit kb kag-dedupe           # Deduplicate all entities
  conduit kb kag-dedupe --dry-run # Preview without making changes`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			homeDir, _ := os.UserHomeDir()
			dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Find duplicate entities (same normalized name + type but different IDs)
			fmt.Println("Analyzing entities for duplicates...")

			rows, err := db.DB().QueryContext(ctx, `
				SELECT entity_id, name, type, description, confidence, source_document_id
				FROM kb_entities
				ORDER BY name COLLATE NOCASE, type, confidence DESC
			`)
			if err != nil {
				return fmt.Errorf("query entities: %w", err)
			}
			defer rows.Close()

			type entityInfo struct {
				ID          string
				Name        string
				Type        string
				Description string
				Confidence  float64
				SourceDocs  string
			}

			// Group entities by normalized name + type
			groups := make(map[string][]entityInfo)
			var totalEntities int

			for rows.Next() {
				var e entityInfo
				if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description, &e.Confidence, &e.SourceDocs); err != nil {
					continue
				}
				totalEntities++

				// Create normalized key
				key := strings.ToLower(strings.TrimSpace(e.Name)) + "|" + e.Type
				groups[key] = append(groups[key], e)
			}

			// Find groups with duplicates
			var duplicateGroups int
			var totalDuplicates int
			for _, entities := range groups {
				if len(entities) > 1 {
					duplicateGroups++
					totalDuplicates += len(entities) - 1 // Count extras as duplicates
				}
			}

			fmt.Printf("\nFound %d entities in %d groups\n", totalEntities, len(groups))
			fmt.Printf("Duplicate groups: %d (containing %d extra entities)\n", duplicateGroups, totalDuplicates)

			if duplicateGroups == 0 {
				fmt.Println("\nNo duplicates found. Knowledge graph is clean.")
				return nil
			}

			if dryRun {
				fmt.Println("\n--dry-run: Showing what would be merged:")
				shown := 0
				for key, entities := range groups {
					if len(entities) > 1 && shown < 10 {
						parts := strings.SplitN(key, "|", 2)
						fmt.Printf("  \"%s\" (%s): %d entities → 1\n", parts[0], parts[1], len(entities))
						shown++
					}
				}
				if duplicateGroups > 10 {
					fmt.Printf("  ... and %d more groups\n", duplicateGroups-10)
				}
				fmt.Println("\nRun without --dry-run to merge duplicates.")
				return nil
			}

			// Perform deduplication
			fmt.Println("\nMerging duplicates...")

			merged := 0
			deleted := 0

			for _, entities := range groups {
				if len(entities) <= 1 {
					continue
				}

				// First entity (highest confidence) becomes the canonical one
				canonical := entities[0]
				canonicalID := kb.GenerateCanonicalEntityID(canonical.Name, kb.EntityType(canonical.Type))

				// Best description is the longest
				bestDesc := canonical.Description
				for _, e := range entities[1:] {
					if len(e.Description) > len(bestDesc) {
						bestDesc = e.Description
					}
				}

				// Combine source documents
				sourceDocs := canonical.SourceDocs
				for _, e := range entities[1:] {
					if e.SourceDocs != "" && !strings.Contains(sourceDocs, e.SourceDocs) {
						if sourceDocs != "" {
							sourceDocs += "," + e.SourceDocs
						} else {
							sourceDocs = e.SourceDocs
						}
					}
				}

				// Update/insert canonical entity
				now := time.Now().Format(time.RFC3339)
				_, err := db.DB().ExecContext(ctx, `
					INSERT OR REPLACE INTO kb_entities
					(entity_id, name, type, description, source_chunk_id, source_document_id,
					 confidence, metadata, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
				`, canonicalID, canonical.Name, canonical.Type, bestDesc,
					"", sourceDocs, canonical.Confidence, now, now)
				if err != nil {
					return fmt.Errorf("upsert canonical entity: %w", err)
				}

				// Delete old entities
				for _, e := range entities {
					if e.ID != canonicalID {
						_, err := db.DB().ExecContext(ctx, `DELETE FROM kb_entities WHERE entity_id = ?`, e.ID)
						if err == nil {
							deleted++
						}
					}
				}
				merged++
			}

			fmt.Println("\nDeduplication Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Groups merged:    %d\n", merged)
			fmt.Printf("Entities deleted: %d\n", deleted)
			fmt.Printf("Entities after:   %d\n", totalEntities-deleted)

			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without making changes")

	return cmd
}

func kbKagVectorizeCmd() *cobra.Command {
	var batchSize int
	var ollamaHost string
	var qdrantHost string
	var qdrantPort int

	cmd := &cobra.Command{
		Use:   "kag-vectorize",
		Short: "Generate vector embeddings for KAG entities",
		Long: `Generate and store vector embeddings for all entities in the knowledge graph.

This enables semantic search over entities using vector similarity.
Embeddings are stored in a Qdrant collection (conduit_entities) separate from chunk vectors.

Requirements:
  - Ollama running with nomic-embed-text model
  - Qdrant running on the specified host/port

Examples:
  conduit kb kag-vectorize
  conduit kb kag-vectorize --batch-size 50
  conduit kb kag-vectorize --ollama-host http://192.168.1.60:11434`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Open database
			homeDir, _ := os.UserHomeDir()
			dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Create embedding service
			fmt.Println("Connecting to Ollama...")
			embeddingSvc, err := kb.NewEmbeddingService(kb.EmbeddingConfig{
				OllamaHost: ollamaHost,
				BatchSize:  batchSize,
			})
			if err != nil {
				return fmt.Errorf("create embedding service: %w", err)
			}

			// Ensure embedding model is available
			if err := embeddingSvc.EnsureModel(ctx); err != nil {
				return fmt.Errorf("ensure embedding model: %w", err)
			}

			// Create vector store
			fmt.Println("Connecting to Qdrant...")
			vectorStore, err := kb.NewVectorStore(kb.VectorStoreConfig{
				Host: qdrantHost,
				Port: qdrantPort,
			})
			if err != nil {
				return fmt.Errorf("create vector store: %w", err)
			}
			defer vectorStore.Close()

			// Ensure entity collection exists
			if err := vectorStore.EnsureEntityCollection(ctx); err != nil {
				return fmt.Errorf("ensure entity collection: %w", err)
			}

			// Query all entities from database
			fmt.Println("Loading entities from database...")
			rows, err := db.DB().QueryContext(ctx, `
				SELECT entity_id, name, type, description, confidence, source_document_id
				FROM kb_entities
				ORDER BY name
			`)
			if err != nil {
				return fmt.Errorf("query entities: %w", err)
			}
			defer rows.Close()

			type entityInfo struct {
				ID          string
				Name        string
				Type        string
				Description string
				Confidence  float64
				SourceDocs  string
			}

			var entities []entityInfo
			for rows.Next() {
				var e entityInfo
				if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description, &e.Confidence, &e.SourceDocs); err != nil {
					continue
				}
				entities = append(entities, e)
			}

			if len(entities) == 0 {
				fmt.Println("No entities found to vectorize.")
				return nil
			}

			fmt.Printf("Found %d entities to vectorize\n", len(entities))

			// Process in batches
			var vectorized, failed int
			for i := 0; i < len(entities); i += batchSize {
				end := i + batchSize
				if end > len(entities) {
					end = len(entities)
				}
				batch := entities[i:end]

				// Generate embeddings for this batch
				texts := make([]string, len(batch))
				for j, e := range batch {
					// Combine name and description for richer embeddings
					texts[j] = e.Name
					if e.Description != "" {
						texts[j] += ": " + e.Description
					}
				}

				embeddings, err := embeddingSvc.EmbedBatch(ctx, texts)
				if err != nil {
					fmt.Printf("  Batch %d-%d: embedding failed: %v\n", i+1, end, err)
					failed += len(batch)
					continue
				}

				// Convert to entity vector points
				points := make([]kb.EntityVectorPoint, len(batch))
				for j, e := range batch {
					points[j] = kb.EntityVectorPoint{
						ID:          e.ID,
						Vector:      embeddings[j],
						Name:        e.Name,
						Type:        e.Type,
						Description: e.Description,
						SourceIDs:   e.SourceDocs,
						Confidence:  e.Confidence,
					}
				}

				// Upsert to Qdrant
				if err := vectorStore.UpsertEntityBatch(ctx, points); err != nil {
					fmt.Printf("  Batch %d-%d: upsert failed: %v\n", i+1, end, err)
					failed += len(batch)
					continue
				}

				vectorized += len(batch)
				fmt.Printf("  Vectorized %d/%d entities\r", vectorized, len(entities))
			}

			fmt.Println() // New line after progress
			fmt.Println("\nVectorization Summary")
			fmt.Println("───────────────────────────────────────")
			fmt.Printf("Total entities:   %d\n", len(entities))
			fmt.Printf("Vectorized:       %d\n", vectorized)
			if failed > 0 {
				fmt.Printf("Failed:           %d\n", failed)
			}

			// Show collection stats
			stats, err := vectorStore.GetEntityStats(ctx)
			if err == nil {
				fmt.Printf("\nEntity Collection: %s\n", stats.CollectionName)
				fmt.Printf("  Vectors: %d\n", stats.VectorCount)
				fmt.Printf("  Status:  %s\n", stats.Status)
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&batchSize, "batch-size", 20, "Number of entities to process at a time")
	cmd.Flags().StringVar(&ollamaHost, "ollama-host", "http://localhost:11434", "Ollama API endpoint")
	cmd.Flags().StringVar(&qdrantHost, "qdrant-host", "localhost", "Qdrant gRPC host")
	cmd.Flags().IntVar(&qdrantPort, "qdrant-port", 6334, "Qdrant gRPC port")

	return cmd
}

func kbKagQueryCmd() *cobra.Command {
	var maxHops int
	var format string
	var hybrid bool
	var ollamaHost string
	var qdrantHost string
	var qdrantPort int

	cmd := &cobra.Command{
		Use:   "kag-query <query>",
		Short: "Query the knowledge graph",
		Long: `Query the knowledge graph for entities and relationships.

The --hybrid flag enables hybrid search (lexical + semantic) for improved recall.
Requires Ollama (nomic-embed-text) and Qdrant running, with entities vectorized via kag-vectorize.

Examples:
  conduit kb kag-query "threat models"
  conduit kb kag-query "authentication" --max-hops 3
  conduit kb kag-query "API security" --format json
  conduit kb kag-query "threat model summary" --hybrid  # Uses semantic search`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			// Open database
			homeDir, _ := os.UserHomeDir()
			dbPath := filepath.Join(homeDir, ".conduit", "conduit.db")
			db, err := store.New(dbPath)
			if err != nil {
				return fmt.Errorf("open database: %w", err)
			}
			defer db.Close()

			ctx := cmd.Context()

			// Create KAGSearcher configuration
			kagCfg := kb.KAGSearcherConfig{
				DB:         db.DB(),
				GraphStore: nil,
			}

			// Set up hybrid search if requested
			if hybrid {
				// Create embedding service
				embeddingSvc, err := kb.NewEmbeddingService(kb.EmbeddingConfig{
					OllamaHost: ollamaHost,
				})
				if err != nil {
					fmt.Printf("Warning: Could not connect to Ollama, falling back to lexical search: %v\n", err)
				} else {
					// Create vector store
					vectorStore, err := kb.NewVectorStore(kb.VectorStoreConfig{
						Host: qdrantHost,
						Port: qdrantPort,
					})
					if err != nil {
						fmt.Printf("Warning: Could not connect to Qdrant, falling back to lexical search: %v\n", err)
					} else {
						kagCfg.VectorStore = vectorStore
						kagCfg.EmbeddingService = embeddingSvc
						defer vectorStore.Close()
					}
				}
			}

			// Use KAGSearcher for improved tokenized search
			kagSearcher := kb.NewKAGSearcherWithConfig(kagCfg)
			result, err := kagSearcher.Search(ctx, &kb.KAGSearchRequest{
				Query:            query,
				MaxHops:          maxHops,
				Limit:            20,
				IncludeRelations: maxHops > 0,
			})
			if err != nil {
				return fmt.Errorf("search entities: %w", err)
			}

			if format == "json" {
				output := map[string]interface{}{
					"query":    query,
					"entities": result.Entities,
				}
				if len(result.Relations) > 0 {
					output["relations"] = result.Relations
				}
				data, _ := json.MarshalIndent(output, "", "  ")
				fmt.Println(string(data))
			} else {
				fmt.Printf("Query: %s\n", query)
				fmt.Println("═══════════════════════════════════════")
				fmt.Println()

				if len(result.Entities) == 0 {
					fmt.Println("No matching entities found.")
					return nil
				}

				for _, e := range result.Entities {
					fmt.Printf("• %s (%s)\n", e.Name, e.Type)
					if e.Description != "" {
						fmt.Printf("  %s\n", truncate(e.Description, 80))
					}
					fmt.Printf("  Confidence: %.0f%%\n", e.Confidence*100)
					fmt.Println()
				}

				// Show relations if any
				if len(result.Relations) > 0 {
					fmt.Println("Relationships:")
					for _, r := range result.Relations {
						fmt.Printf("  %s → %s → %s\n", r.SubjectName, r.Predicate, r.ObjectName)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().IntVar(&maxHops, "max-hops", 2, "Maximum relationship hops to traverse")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.Flags().BoolVar(&hybrid, "hybrid", false, "Enable hybrid search (lexical + semantic)")
	cmd.Flags().StringVar(&ollamaHost, "ollama-host", "http://localhost:11434", "Ollama API endpoint")
	cmd.Flags().StringVar(&qdrantHost, "qdrant-host", "localhost", "Qdrant gRPC host")
	cmd.Flags().IntVar(&qdrantPort, "qdrant-port", 6334, "Qdrant gRPC port")

	return cmd
}

// ollamaCmd returns the ollama parent command
func ollamaCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ollama",
		Short: "Manage Ollama models for AI features",
		Long: `Manage Ollama models used for semantic search and entity extraction.

Conduit uses Ollama for:
  • nomic-embed-text - Embedding model for semantic search
  • mistral:7b-instruct-q4_K_M - Entity extraction for KAG

Examples:
  conduit ollama status     # Show loaded models and Ollama status
  conduit ollama models     # List available models
  conduit ollama pull <model>  # Download a model
  conduit ollama warmup     # Preload required models into memory`,
	}

	cmd.AddCommand(ollamaStatusCmd())
	cmd.AddCommand(ollamaModelsCmd())
	cmd.AddCommand(ollamaPullCmd())
	cmd.AddCommand(ollamaWarmupCmd())

	return cmd
}

// findOllamaBinary searches for the ollama CLI in common locations
func findOllamaBinary() string {
	// Check PATH first
	if path, err := exec.LookPath("ollama"); err == nil {
		return path
	}

	// Check common installation locations
	locations := []string{
		"/opt/homebrew/bin/ollama",     // macOS Homebrew (Apple Silicon)
		"/usr/local/bin/ollama",        // macOS Homebrew (Intel) / Linux
		"/usr/bin/ollama",              // Linux system install
		"/snap/bin/ollama",             // Linux snap install
	}

	for _, loc := range locations {
		if _, err := os.Stat(loc); err == nil {
			return loc
		}
	}

	return "ollama" // Fall back to PATH lookup
}

// ollamaStatusCmd shows Ollama status and loaded models
func ollamaStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show Ollama status and loaded models",
		Long: `Check if Ollama is running and show currently loaded models.

Loaded models are kept in memory for fast inference. Models that
aren't loaded will have a cold-start delay on first use (1-2 minutes).

Use 'conduit ollama warmup' to preload models for faster response times.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check if Ollama is available
			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}

			if !provider.IsAvailable(ctx) {
				fmt.Println("✗ Ollama is not running")
				fmt.Println()
				fmt.Println("Start Ollama with:")
				fmt.Println("  ollama serve")
				fmt.Println()
				fmt.Println("Or install Ollama from: https://ollama.ai")
				return nil
			}

			fmt.Println("✓ Ollama is running")
			fmt.Println()

			// Get loaded models using ollama ps
			ollamaBin := findOllamaBinary()
			out, err := exec.CommandContext(ctx, ollamaBin, "ps").Output()
			if err != nil {
				fmt.Println("Loaded models: (unable to query)")
				return nil
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) <= 1 {
				fmt.Println("Loaded models: none")
				fmt.Println()
				fmt.Println("No models are currently loaded in memory.")
				fmt.Println("Models will be loaded on first use (1-2 minute delay).")
				fmt.Println()
				fmt.Println("To preload models for faster response times:")
				fmt.Println("  conduit ollama warmup")
			} else {
				fmt.Println("Loaded models:")
				for _, line := range lines {
					fmt.Println("  " + line)
				}
			}

			// Show required models status
			fmt.Println()
			fmt.Println("Required models for Conduit:")
			fmt.Println("  • nomic-embed-text       - Semantic search embeddings")
			fmt.Println("  • mistral:7b-instruct-q4_K_M - Entity extraction (KAG)")

			return nil
		},
	}
}

// ollamaModelsCmd lists available Ollama models
func ollamaModelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "List available Ollama models",
		Long: `List all Ollama models installed on the system.

Shows which models are available locally. Missing models will be
automatically downloaded on first use, or you can pull them manually:

  ollama pull nomic-embed-text
  ollama pull mistral:7b-instruct-q4_K_M`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check if Ollama is available
			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}

			if !provider.IsAvailable(ctx) {
				fmt.Println("✗ Ollama is not running")
				fmt.Println()
				fmt.Println("Start Ollama with: ollama serve")
				return nil
			}

			// Get available models using ollama list
			ollamaBin := findOllamaBinary()
			out, err := exec.CommandContext(ctx, ollamaBin, "list").Output()
			if err != nil {
				return fmt.Errorf("list models: %w", err)
			}

			lines := strings.Split(strings.TrimSpace(string(out)), "\n")
			if len(lines) <= 1 {
				fmt.Println("No models installed.")
				fmt.Println()
				fmt.Println("Pull required models with:")
				fmt.Println("  ollama pull nomic-embed-text")
				fmt.Println("  ollama pull mistral:7b-instruct-q4_K_M")
				return nil
			}

			fmt.Println("Available models:")
			for _, line := range lines {
				fmt.Println("  " + line)
			}

			// Check for required models
			modelsStr := string(out)
			fmt.Println()
			fmt.Println("Required models status:")

			if strings.Contains(modelsStr, "nomic-embed-text") {
				fmt.Println("  ✓ nomic-embed-text (installed)")
			} else {
				fmt.Println("  ✗ nomic-embed-text (not installed)")
				fmt.Println("    Pull with: ollama pull nomic-embed-text")
			}

			if strings.Contains(modelsStr, "mistral") {
				fmt.Println("  ✓ mistral (installed)")
			} else {
				fmt.Println("  ✗ mistral (not installed)")
				fmt.Println("    Pull with: ollama pull mistral:7b-instruct-q4_K_M")
			}

			return nil
		},
	}
}

// ensureOllamaRunning starts Ollama server if not running and waits for it to be ready
func ensureOllamaRunning(ctx context.Context) error {
	// Check if already running
	provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	if provider.IsAvailable(ctx) {
		return nil // Already running
	}

	// Try to start Ollama
	ollamaBin := findOllamaBinary()
	if ollamaBin == "" {
		return fmt.Errorf("Ollama binary not found. Install with: brew install ollama")
	}

	fmt.Println("Starting Ollama server...")

	// Start ollama serve in background
	serveCmd := exec.Command(ollamaBin, "serve")
	serveCmd.Stdout = nil
	serveCmd.Stderr = nil
	if err := serveCmd.Start(); err != nil {
		return fmt.Errorf("failed to start Ollama: %w", err)
	}

	// Wait for Ollama to become available (up to 30 seconds)
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if provider.IsAvailable(ctx) {
			fmt.Println("✓ Ollama server started")
			return nil
		}
	}

	return fmt.Errorf("Ollama did not start in time. Try running: %s serve", ollamaBin)
}

// ollamaPullCmd pulls an Ollama model with progress streaming
func ollamaPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull <model>",
		Short: "Pull an Ollama model",
		Long: `Pull (download) an Ollama model from the registry.

Progress is streamed to stdout, making it suitable for GUI integration.
If Ollama is not running, it will be started automatically.

Examples:
  conduit ollama pull nomic-embed-text
  conduit ollama pull mistral:7b-instruct-q4_K_M`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			model := args[0]

			// Ensure Ollama is running (start it if needed)
			if err := ensureOllamaRunning(ctx); err != nil {
				return err
			}

			fmt.Printf("Pulling model: %s\n", model)

			// Run ollama pull with output streaming
			ollamaBin := findOllamaBinary()
			pullCmd := exec.CommandContext(ctx, ollamaBin, "pull", model)

			// Capture both stdout and stderr for progress
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr

			if err := pullCmd.Run(); err != nil {
				return fmt.Errorf("pull failed: %w", err)
			}

			fmt.Printf("\n✓ Model %s pulled successfully\n", model)
			return nil
		},
	}
}

// ollamaWarmupCmd preloads required models into memory
func ollamaWarmupCmd() *cobra.Command {
	var models []string

	cmd := &cobra.Command{
		Use:   "warmup",
		Short: "Preload models into memory for faster inference",
		Long: `Preload Ollama models into memory to eliminate cold-start delays.

By default, warms up both required models:
  • nomic-embed-text - For semantic search
  • mistral:7b-instruct-q4_K_M - For entity extraction

Models stay loaded based on Ollama's keep_alive setting (default: 5 minutes).

Examples:
  conduit ollama warmup                           # Warm up all required models
  conduit ollama warmup --models nomic-embed-text # Warm up specific model`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Check if Ollama is available
			provider, err := kb.NewOllamaProvider(kb.OllamaProviderConfig{})
			if err != nil {
				return fmt.Errorf("create provider: %w", err)
			}

			if !provider.IsAvailable(ctx) {
				fmt.Println("✗ Ollama is not running")
				fmt.Println()
				fmt.Println("Start Ollama with: ollama serve")
				return nil
			}

			// Default models if none specified
			if len(models) == 0 {
				models = []string{"nomic-embed-text", "mistral:7b-instruct-q4_K_M"}
			}

			fmt.Println("Warming up Ollama models...")
			fmt.Println("This may take 1-2 minutes per model on first load.")
			fmt.Println()

			ollamaBin := findOllamaBinary()

			for _, model := range models {
				fmt.Printf("Loading %s... ", model)
				os.Stdout.Sync()

				startTime := time.Now()

				// Send a minimal request to load the model
				// Using ollama run with a simple prompt
				runCmd := exec.CommandContext(ctx, ollamaBin, "run", model, "hello")
				runCmd.Stdin = strings.NewReader("")

				if err := runCmd.Run(); err != nil {
					fmt.Printf("✗ failed\n")
					fmt.Printf("  Error: %v\n", err)
					fmt.Printf("  Try pulling the model: ollama pull %s\n", model)
					continue
				}

				elapsed := time.Since(startTime)
				fmt.Printf("✓ loaded (%s)\n", formatDuration(elapsed))
			}

			fmt.Println()
			fmt.Println("Models are now loaded and ready for fast inference.")
			fmt.Println("They will stay loaded based on Ollama's keep_alive setting.")

			// Show current status
			fmt.Println()
			out, err := exec.CommandContext(ctx, ollamaBin, "ps").Output()
			if err == nil {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				if len(lines) > 1 {
					fmt.Println("Currently loaded:")
					for _, line := range lines {
						fmt.Println("  " + line)
					}
				}
			}

			return nil
		},
	}

	cmd.Flags().StringSliceVar(&models, "models", nil, "Specific models to warm up (default: all required)")

	return cmd
}

// eventsCmd streams real-time events from the daemon via SSE
func eventsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Stream real-time events from the daemon",
		Long: `Connect to the daemon's Server-Sent Events (SSE) endpoint
and stream real-time events to the console.

Events include:
  - Instance status changes (created, started, stopped, deleted)
  - KB sync progress and completion
  - KAG extraction progress
  - Binding changes
  - Daemon heartbeat (every 30s)

Press Ctrl+C to stop streaming.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return streamEvents(socketPath, jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output raw JSON events")

	return cmd
}

// streamEvents connects to the SSE endpoint and streams events
func streamEvents(socketPath string, jsonOutput bool) error {
	// Create a custom HTTP client with Unix socket transport
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", socketPath)
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   0, // No timeout for SSE streaming
	}

	// Make the SSE request
	req, err := http.NewRequest("GET", "http://localhost/api/v1/events", nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// Handle Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\nDisconnecting from event stream...")
		cancel()
	}()

	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect to daemon: %w (is the daemon running?)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	if !jsonOutput {
		fmt.Println("╔══════════════════════════════════════════════════════════════╗")
		fmt.Println("║              Conduit Event Stream (SSE)                      ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════╝")
		fmt.Println()
		fmt.Println("Streaming events... (Press Ctrl+C to stop)")
		fmt.Println()
	}

	// Parse SSE stream
	scanner := bufio.NewScanner(resp.Body)
	var eventType, eventData string

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			eventData = strings.TrimPrefix(line, "data: ")
		} else if line == "" && eventType != "" && eventData != "" {
			// Empty line signals end of event
			if jsonOutput {
				// Output raw JSON
				fmt.Printf("{\"event\":\"%s\",\"data\":%s}\n", eventType, eventData)
			} else {
				// Pretty print event
				printEvent(eventType, eventData)
			}
			eventType = ""
			eventData = ""
		}
	}

	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read stream: %w", err)
	}

	return nil
}

// printEvent pretty-prints an SSE event
func printEvent(eventType, eventData string) {
	timestamp := time.Now().Format("15:04:05")

	// Parse data as JSON for pretty display
	var data map[string]interface{}
	json.Unmarshal([]byte(eventData), &data)

	// Choose icon based on event type
	var icon string
	switch {
	case strings.HasPrefix(eventType, "instance_"):
		icon = "📦"
	case strings.HasPrefix(eventType, "kb_"):
		icon = "📚"
	case strings.HasPrefix(eventType, "kag_"):
		icon = "🔗"
	case strings.HasPrefix(eventType, "binding_"):
		icon = "🔌"
	case eventType == "daemon_status":
		icon = "💓"
	case eventType == "connected":
		icon = "✓"
	case eventType == "shutdown":
		icon = "⚠️"
	default:
		icon = "•"
	}

	fmt.Printf("[%s] %s %s\n", timestamp, icon, eventType)

	// Print relevant data fields
	for key, value := range data {
		if key == "timestamp" {
			continue // Skip timestamp, we show our own
		}
		fmt.Printf("         %s: %v\n", key, value)
	}
	fmt.Println()
}
