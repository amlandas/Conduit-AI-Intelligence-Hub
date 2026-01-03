// Package installer handles automated installation of Conduit dependencies.
package installer

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Dependency represents a software dependency.
type Dependency struct {
	Name        string
	Description string
	CheckCmd    []string // Command to check if installed
	Required    bool     // If false, it's optional
}

// InstallResult holds the result of an installation attempt.
type InstallResult struct {
	Dependency    string
	Installed     bool
	AlreadyExists bool
	Skipped       bool
	Error         error
	Message       string
}

// Installer handles dependency installation.
type Installer struct {
	reader  *bufio.Reader
	verbose bool
}

// New creates a new Installer.
func New(verbose bool) *Installer {
	return &Installer{
		reader:  bufio.NewReader(os.Stdin),
		verbose: verbose,
	}
}

// CheckAndInstallAll checks and installs all Conduit dependencies.
func (i *Installer) CheckAndInstallAll(ctx context.Context) ([]InstallResult, error) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║              Conduit Dependency Installer                    ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	results := []InstallResult{}

	// Step 1: Container Runtime (Docker or Podman)
	fmt.Println("Step 1: Container Runtime")
	fmt.Println("──────────────────────────────────────────────────────────────")
	containerResult := i.installContainerRuntime(ctx)
	results = append(results, containerResult)
	fmt.Println()

	// Step 2: Ollama
	fmt.Println("Step 2: Ollama (Local AI Runtime)")
	fmt.Println("──────────────────────────────────────────────────────────────")
	ollamaResult := i.installOllama(ctx)
	results = append(results, ollamaResult)
	fmt.Println()

	// Step 3: Pull AI Model
	if ollamaResult.Installed || ollamaResult.AlreadyExists {
		fmt.Println("Step 3: AI Model (qwen2.5-coder:7b)")
		fmt.Println("──────────────────────────────────────────────────────────────")
		modelResult := i.pullOllamaModel(ctx, "qwen2.5-coder:7b")
		results = append(results, modelResult)
		fmt.Println()
	}

	// Step 4: Document Extraction Tools
	fmt.Println("Step 4: Document Extraction Tools")
	fmt.Println("──────────────────────────────────────────────────────────────")
	docToolResults := i.installDocumentTools(ctx)
	results = append(results, docToolResults...)
	fmt.Println()

	// Summary
	i.printSummary(results)

	return results, nil
}

// installContainerRuntime installs Docker or Podman.
func (i *Installer) installContainerRuntime(ctx context.Context) InstallResult {
	// Check if Docker is already installed
	if i.commandExists("docker") {
		version := i.getCommandOutput("docker", "--version")
		fmt.Printf("✓ Docker is already installed: %s\n", strings.TrimSpace(version))
		return InstallResult{
			Dependency:    "Container Runtime",
			AlreadyExists: true,
			Message:       "Docker already installed",
		}
	}

	// Check if Podman is already installed
	if i.commandExists("podman") {
		version := i.getCommandOutput("podman", "--version")
		fmt.Printf("✓ Podman is already installed: %s\n", strings.TrimSpace(version))
		return InstallResult{
			Dependency:    "Container Runtime",
			AlreadyExists: true,
			Message:       "Podman already installed",
		}
	}

	fmt.Println("No container runtime found. Conduit needs Docker or Podman to run")
	fmt.Println("MCP servers in isolated containers.")
	fmt.Println()

	// Determine which to recommend based on OS
	recommended := "Docker"
	if runtime.GOOS == "linux" {
		recommended = "Podman"
	}

	fmt.Printf("  [1] Install %s (Recommended for %s)\n", recommended, runtime.GOOS)
	if recommended == "Docker" {
		fmt.Println("  [2] Install Podman")
	} else {
		fmt.Println("  [2] Install Docker")
	}
	fmt.Println("  [3] Skip (install manually later)")
	fmt.Println()

	choice := i.prompt("Choice [1/2/3]: ")

	switch choice {
	case "1":
		if recommended == "Docker" {
			return i.installDocker(ctx)
		}
		return i.installPodman(ctx)
	case "2":
		if recommended == "Docker" {
			return i.installPodman(ctx)
		}
		return i.installDocker(ctx)
	default:
		fmt.Println("Skipping container runtime installation.")
		return InstallResult{
			Dependency: "Container Runtime",
			Skipped:    true,
			Message:    "User skipped installation",
		}
	}
}

// installDocker installs Docker based on the OS.
func (i *Installer) installDocker(ctx context.Context) InstallResult {
	fmt.Println()
	fmt.Println("Installing Docker...")

	var installCmd []string
	var postInstall []string

	switch runtime.GOOS {
	case "darwin":
		// macOS - use Homebrew
		if !i.commandExists("brew") {
			fmt.Println("Homebrew is required to install Docker on macOS.")
			fmt.Println("Install Homebrew first: https://brew.sh")
			return InstallResult{
				Dependency: "Docker",
				Error:      fmt.Errorf("homebrew not installed"),
				Message:    "Homebrew required but not installed",
			}
		}
		installCmd = []string{"brew", "install", "--cask", "docker"}
		postInstall = []string{"open", "-a", "Docker"}

	case "linux":
		// Linux - detect distro and use appropriate package manager
		distro := i.detectLinuxDistro()
		switch distro {
		case "ubuntu", "debian":
			// Use Docker's official repository
			fmt.Println("Installing Docker using apt...")
			return i.installDockerUbuntu(ctx)
		case "fedora", "rhel", "centos":
			fmt.Println("Installing Docker using dnf...")
			return i.installDockerFedora(ctx)
		case "arch":
			installCmd = []string{"sudo", "pacman", "-S", "--noconfirm", "docker"}
			postInstall = []string{"sudo", "systemctl", "enable", "--now", "docker"}
		default:
			fmt.Printf("Unsupported Linux distribution: %s\n", distro)
			fmt.Println("Please install Docker manually: https://docs.docker.com/engine/install/")
			return InstallResult{
				Dependency: "Docker",
				Error:      fmt.Errorf("unsupported distro: %s", distro),
				Message:    "Manual installation required",
			}
		}

	case "windows":
		fmt.Println("On Windows, please install Docker Desktop manually:")
		fmt.Println("  https://docs.docker.com/desktop/install/windows-install/")
		fmt.Println()
		fmt.Println("After installation, ensure WSL 2 backend is enabled.")
		return InstallResult{
			Dependency: "Docker",
			Skipped:    true,
			Message:    "Manual installation required on Windows",
		}

	default:
		return InstallResult{
			Dependency: "Docker",
			Error:      fmt.Errorf("unsupported OS: %s", runtime.GOOS),
		}
	}

	// Run installation command
	if len(installCmd) > 0 {
		fmt.Printf("Running: %s\n", strings.Join(installCmd, " "))
		if !i.confirmAction("Proceed with installation?") {
			return InstallResult{
				Dependency: "Docker",
				Skipped:    true,
				Message:    "User cancelled installation",
			}
		}

		if err := i.runCommand(ctx, installCmd[0], installCmd[1:]...); err != nil {
			return InstallResult{
				Dependency: "Docker",
				Error:      err,
				Message:    "Installation command failed",
			}
		}

		// Run post-install commands
		for _, cmd := range postInstall {
			parts := strings.Fields(cmd)
			_ = i.runCommand(ctx, parts[0], parts[1:]...)
		}

		fmt.Println("✓ Docker installed successfully")
		if runtime.GOOS == "darwin" {
			fmt.Println("  Note: Docker Desktop is starting. Please complete the setup in the app.")
		}

		return InstallResult{
			Dependency: "Docker",
			Installed:  true,
			Message:    "Docker installed successfully",
		}
	}

	return InstallResult{
		Dependency: "Docker",
		Skipped:    true,
	}
}

// installDockerUbuntu installs Docker on Ubuntu/Debian.
func (i *Installer) installDockerUbuntu(ctx context.Context) InstallResult {
	commands := [][]string{
		{"sudo", "apt-get", "update"},
		{"sudo", "apt-get", "install", "-y", "ca-certificates", "curl", "gnupg"},
		{"sudo", "install", "-m", "0755", "-d", "/etc/apt/keyrings"},
	}

	// Add Docker's GPG key and repository
	gpgCmd := `curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg`
	repoCmd := `echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null`

	fmt.Println("This will run the following commands:")
	for _, cmd := range commands {
		fmt.Printf("  %s\n", strings.Join(cmd, " "))
	}
	fmt.Printf("  %s\n", gpgCmd)
	fmt.Printf("  %s\n", repoCmd)
	fmt.Println("  sudo apt-get update")
	fmt.Println("  sudo apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin")
	fmt.Println()

	if !i.confirmAction("Proceed with installation?") {
		return InstallResult{
			Dependency: "Docker",
			Skipped:    true,
			Message:    "User cancelled installation",
		}
	}

	for _, cmd := range commands {
		if err := i.runCommand(ctx, cmd[0], cmd[1:]...); err != nil {
			return InstallResult{Dependency: "Docker", Error: err}
		}
	}

	// Run shell commands for GPG and repo
	if err := i.runShellCommand(ctx, gpgCmd); err != nil {
		return InstallResult{Dependency: "Docker", Error: err}
	}
	if err := i.runShellCommand(ctx, repoCmd); err != nil {
		return InstallResult{Dependency: "Docker", Error: err}
	}

	// Install Docker
	if err := i.runCommand(ctx, "sudo", "apt-get", "update"); err != nil {
		return InstallResult{Dependency: "Docker", Error: err}
	}
	if err := i.runCommand(ctx, "sudo", "apt-get", "install", "-y",
		"docker-ce", "docker-ce-cli", "containerd.io",
		"docker-buildx-plugin", "docker-compose-plugin"); err != nil {
		return InstallResult{Dependency: "Docker", Error: err}
	}

	// Add user to docker group
	user := os.Getenv("USER")
	if user != "" {
		_ = i.runCommand(ctx, "sudo", "usermod", "-aG", "docker", user)
		fmt.Printf("Added user '%s' to docker group. You may need to log out and back in.\n", user)
	}

	return InstallResult{
		Dependency: "Docker",
		Installed:  true,
		Message:    "Docker installed successfully",
	}
}

// installDockerFedora installs Docker on Fedora/RHEL.
func (i *Installer) installDockerFedora(ctx context.Context) InstallResult {
	commands := [][]string{
		{"sudo", "dnf", "-y", "install", "dnf-plugins-core"},
		{"sudo", "dnf", "config-manager", "--add-repo", "https://download.docker.com/linux/fedora/docker-ce.repo"},
		{"sudo", "dnf", "install", "-y", "docker-ce", "docker-ce-cli", "containerd.io", "docker-buildx-plugin", "docker-compose-plugin"},
		{"sudo", "systemctl", "enable", "--now", "docker"},
	}

	fmt.Println("This will run the following commands:")
	for _, cmd := range commands {
		fmt.Printf("  %s\n", strings.Join(cmd, " "))
	}
	fmt.Println()

	if !i.confirmAction("Proceed with installation?") {
		return InstallResult{
			Dependency: "Docker",
			Skipped:    true,
			Message:    "User cancelled installation",
		}
	}

	for _, cmd := range commands {
		if err := i.runCommand(ctx, cmd[0], cmd[1:]...); err != nil {
			return InstallResult{Dependency: "Docker", Error: err}
		}
	}

	// Add user to docker group
	user := os.Getenv("USER")
	if user != "" {
		_ = i.runCommand(ctx, "sudo", "usermod", "-aG", "docker", user)
		fmt.Printf("Added user '%s' to docker group. You may need to log out and back in.\n", user)
	}

	return InstallResult{
		Dependency: "Docker",
		Installed:  true,
		Message:    "Docker installed successfully",
	}
}

// installPodman installs Podman based on the OS.
func (i *Installer) installPodman(ctx context.Context) InstallResult {
	fmt.Println()
	fmt.Println("Installing Podman...")

	var installCmd []string

	switch runtime.GOOS {
	case "darwin":
		if !i.commandExists("brew") {
			fmt.Println("Homebrew is required to install Podman on macOS.")
			fmt.Println("Install Homebrew first: https://brew.sh")
			return InstallResult{
				Dependency: "Podman",
				Error:      fmt.Errorf("homebrew not installed"),
			}
		}
		installCmd = []string{"brew", "install", "podman"}

	case "linux":
		distro := i.detectLinuxDistro()
		switch distro {
		case "ubuntu", "debian":
			installCmd = []string{"sudo", "apt-get", "install", "-y", "podman"}
		case "fedora", "rhel", "centos":
			installCmd = []string{"sudo", "dnf", "install", "-y", "podman"}
		case "arch":
			installCmd = []string{"sudo", "pacman", "-S", "--noconfirm", "podman"}
		default:
			fmt.Printf("Unsupported Linux distribution: %s\n", distro)
			fmt.Println("Please install Podman manually: https://podman.io/docs/installation")
			return InstallResult{
				Dependency: "Podman",
				Skipped:    true,
				Message:    "Manual installation required",
			}
		}

	case "windows":
		fmt.Println("On Windows, please install Podman Desktop manually:")
		fmt.Println("  https://podman-desktop.io/")
		return InstallResult{
			Dependency: "Podman",
			Skipped:    true,
			Message:    "Manual installation required on Windows",
		}

	default:
		return InstallResult{
			Dependency: "Podman",
			Error:      fmt.Errorf("unsupported OS: %s", runtime.GOOS),
		}
	}

	fmt.Printf("Running: %s\n", strings.Join(installCmd, " "))
	if !i.confirmAction("Proceed with installation?") {
		return InstallResult{
			Dependency: "Podman",
			Skipped:    true,
			Message:    "User cancelled installation",
		}
	}

	if err := i.runCommand(ctx, installCmd[0], installCmd[1:]...); err != nil {
		return InstallResult{
			Dependency: "Podman",
			Error:      err,
			Message:    "Installation command failed",
		}
	}

	// Initialize Podman machine on macOS
	if runtime.GOOS == "darwin" {
		fmt.Println("Initializing Podman machine...")
		_ = i.runCommand(ctx, "podman", "machine", "init")
		fmt.Println("Starting Podman machine...")
		_ = i.runCommand(ctx, "podman", "machine", "start")
	}

	fmt.Println("✓ Podman installed successfully")
	return InstallResult{
		Dependency: "Podman",
		Installed:  true,
		Message:    "Podman installed successfully",
	}
}

// installOllama installs Ollama.
func (i *Installer) installOllama(ctx context.Context) InstallResult {
	// Check if already installed
	if i.commandExists("ollama") {
		version := i.getCommandOutput("ollama", "--version")
		fmt.Printf("✓ Ollama is already installed: %s\n", strings.TrimSpace(version))

		// Check if Ollama is running
		if i.isOllamaRunning() {
			fmt.Println("✓ Ollama service is running")
		} else {
			fmt.Println("⚠ Ollama is installed but not running")
			if i.confirmAction("Start Ollama service?") {
				i.startOllama(ctx)
			}
		}

		return InstallResult{
			Dependency:    "Ollama",
			AlreadyExists: true,
			Message:       "Ollama already installed",
		}
	}

	fmt.Println("Ollama is not installed. Ollama provides local AI capabilities for")
	fmt.Println("analyzing MCP server repositories without needing a cloud API key.")
	fmt.Println()
	fmt.Println("  [1] Install Ollama (Recommended)")
	fmt.Println("  [2] Skip (use Anthropic API instead)")
	fmt.Println()

	choice := i.prompt("Choice [1/2]: ")

	if choice != "1" {
		fmt.Println("Skipping Ollama installation.")
		fmt.Println("You'll need to set ANTHROPIC_API_KEY to use Conduit's AI features.")
		return InstallResult{
			Dependency: "Ollama",
			Skipped:    true,
			Message:    "User chose to skip",
		}
	}

	fmt.Println()
	fmt.Println("Installing Ollama...")

	switch runtime.GOOS {
	case "darwin", "linux":
		// Use the official install script
		fmt.Println("Running official Ollama install script...")
		fmt.Println("Command: curl -fsSL https://ollama.com/install.sh | sh")
		fmt.Println()

		if !i.confirmAction("Proceed with installation?") {
			return InstallResult{
				Dependency: "Ollama",
				Skipped:    true,
				Message:    "User cancelled installation",
			}
		}

		if err := i.runShellCommand(ctx, "curl -fsSL https://ollama.com/install.sh | sh"); err != nil {
			return InstallResult{
				Dependency: "Ollama",
				Error:      err,
				Message:    "Installation script failed",
			}
		}

	case "windows":
		fmt.Println("On Windows, please download Ollama from:")
		fmt.Println("  https://ollama.com/download/windows")
		fmt.Println()
		fmt.Println("After installation, run 'ollama serve' to start the service.")
		return InstallResult{
			Dependency: "Ollama",
			Skipped:    true,
			Message:    "Manual installation required on Windows",
		}

	default:
		return InstallResult{
			Dependency: "Ollama",
			Error:      fmt.Errorf("unsupported OS: %s", runtime.GOOS),
		}
	}

	// Verify installation
	if !i.commandExists("ollama") {
		return InstallResult{
			Dependency: "Ollama",
			Error:      fmt.Errorf("ollama command not found after installation"),
		}
	}

	fmt.Println("✓ Ollama installed successfully")

	// Start Ollama service
	fmt.Println()
	if i.confirmAction("Start Ollama service now?") {
		i.startOllama(ctx)
	}

	return InstallResult{
		Dependency: "Ollama",
		Installed:  true,
		Message:    "Ollama installed successfully",
	}
}

// pullOllamaModel pulls an Ollama model.
func (i *Installer) pullOllamaModel(ctx context.Context, model string) InstallResult {
	// Check if model exists
	if i.ollamaModelExists(model) {
		fmt.Printf("✓ Model '%s' is already downloaded\n", model)
		return InstallResult{
			Dependency:    "AI Model",
			AlreadyExists: true,
			Message:       fmt.Sprintf("Model %s already exists", model),
		}
	}

	fmt.Printf("The AI model '%s' needs to be downloaded (~4.7GB).\n", model)
	fmt.Println("This model is optimized for code analysis tasks.")
	fmt.Println()

	if !i.confirmAction(fmt.Sprintf("Download model '%s'?", model)) {
		return InstallResult{
			Dependency: "AI Model",
			Skipped:    true,
			Message:    "User skipped model download",
		}
	}

	// Ensure Ollama is running
	if !i.isOllamaRunning() {
		fmt.Println("Starting Ollama service...")
		i.startOllama(ctx)
		time.Sleep(2 * time.Second)
	}

	fmt.Printf("Downloading model '%s'...\n", model)
	fmt.Println("This may take several minutes depending on your internet connection.")
	fmt.Println()

	cmd := exec.CommandContext(ctx, "ollama", "pull", model)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return InstallResult{
			Dependency: "AI Model",
			Error:      err,
			Message:    "Failed to pull model",
		}
	}

	fmt.Println()
	fmt.Printf("✓ Model '%s' downloaded successfully\n", model)

	return InstallResult{
		Dependency: "AI Model",
		Installed:  true,
		Message:    fmt.Sprintf("Model %s installed", model),
	}
}

// installDocumentTools installs document extraction tools.
func (i *Installer) installDocumentTools(ctx context.Context) []InstallResult {
	var results []InstallResult

	fmt.Println("Document extraction tools enable indexing of PDF, DOC, and RTF files")
	fmt.Println("in the knowledge base. DOCX and ODT are supported natively.")
	fmt.Println()

	// Check existing tools
	tools := i.checkDocumentTools()

	allInstalled := true
	for _, tool := range tools {
		if tool.Available {
			fmt.Printf("✓ %s is already installed\n", tool.Name)
		} else {
			fmt.Printf("○ %s is not installed\n", tool.Name)
			allInstalled = false
		}
	}

	if allInstalled {
		fmt.Println()
		fmt.Println("All document extraction tools are already installed.")
		for _, tool := range tools {
			results = append(results, InstallResult{
				Dependency:    tool.Name,
				AlreadyExists: true,
				Message:       "Already installed",
			})
		}
		return results
	}

	fmt.Println()
	fmt.Println("  [1] Install missing document tools (Recommended)")
	fmt.Println("  [2] Skip (some document formats won't be indexed)")
	fmt.Println()

	choice := i.prompt("Choice [1/2]: ")

	if choice != "1" {
		fmt.Println("Skipping document tools installation.")
		for _, tool := range tools {
			if !tool.Available {
				results = append(results, InstallResult{
					Dependency: tool.Name,
					Skipped:    true,
					Message:    "User skipped installation",
				})
			}
		}
		return results
	}

	// Install based on OS
	switch runtime.GOOS {
	case "darwin":
		return i.installDocumentToolsMacOS(ctx)
	case "linux":
		return i.installDocumentToolsLinux(ctx)
	case "windows":
		return i.installDocumentToolsWindows(ctx)
	default:
		fmt.Printf("Document tool installation not supported on %s.\n", runtime.GOOS)
		return []InstallResult{{
			Dependency: "Document Tools",
			Skipped:    true,
			Message:    fmt.Sprintf("Not supported on %s", runtime.GOOS),
		}}
	}
}

// DocumentTool represents a document extraction tool's status.
type DocumentTool struct {
	Name      string
	Available bool
}

// checkDocumentTools checks which document tools are available.
func (i *Installer) checkDocumentTools() []DocumentTool {
	var tools []DocumentTool

	// pdftotext (for PDF)
	tools = append(tools, DocumentTool{
		Name:      "pdftotext (PDF)",
		Available: i.commandExists("pdftotext"),
	})

	// DOC extraction
	if runtime.GOOS == "darwin" {
		// textutil is built-in on macOS
		tools = append(tools, DocumentTool{
			Name:      "textutil (DOC/RTF)",
			Available: true, // Always available on macOS
		})
	} else {
		// antiword for other platforms
		tools = append(tools, DocumentTool{
			Name:      "antiword (DOC)",
			Available: i.commandExists("antiword"),
		})
		// unrtf for RTF
		tools = append(tools, DocumentTool{
			Name:      "unrtf (RTF)",
			Available: i.commandExists("unrtf"),
		})
	}

	return tools
}

// installDocumentToolsMacOS installs document tools on macOS using Homebrew.
func (i *Installer) installDocumentToolsMacOS(ctx context.Context) []InstallResult {
	var results []InstallResult

	// Check Homebrew
	if !i.commandExists("brew") {
		fmt.Println("Homebrew is required to install document tools on macOS.")
		fmt.Println("Install Homebrew first: https://brew.sh")

		if i.confirmAction("Install Homebrew now?") {
			if err := i.runShellCommand(ctx, `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`); err != nil {
				return []InstallResult{{
					Dependency: "Document Tools",
					Error:      fmt.Errorf("failed to install Homebrew: %w", err),
				}}
			}
		} else {
			return []InstallResult{{
				Dependency: "Document Tools",
				Skipped:    true,
				Message:    "Homebrew required but not installed",
			}}
		}
	}

	// Install poppler (provides pdftotext)
	if !i.commandExists("pdftotext") {
		fmt.Println()
		fmt.Println("Installing poppler (pdftotext)...")
		if err := i.runCommand(ctx, "brew", "install", "poppler"); err != nil {
			results = append(results, InstallResult{
				Dependency: "pdftotext",
				Error:      err,
			})
		} else {
			fmt.Println("✓ pdftotext installed")
			results = append(results, InstallResult{
				Dependency: "pdftotext",
				Installed:  true,
			})
		}
	} else {
		results = append(results, InstallResult{
			Dependency:    "pdftotext",
			AlreadyExists: true,
		})
	}

	// textutil is built-in on macOS
	fmt.Println("✓ textutil (DOC/RTF) is built-in on macOS")
	results = append(results, InstallResult{
		Dependency:    "textutil",
		AlreadyExists: true,
		Message:       "Built-in on macOS",
	})

	return results
}

// installDocumentToolsLinux installs document tools on Linux.
func (i *Installer) installDocumentToolsLinux(ctx context.Context) []InstallResult {
	var results []InstallResult

	distro := i.detectLinuxDistro()

	var packages []string
	var installCmd []string

	switch distro {
	case "ubuntu", "debian":
		packages = []string{"poppler-utils", "antiword", "unrtf"}
		installCmd = []string{"sudo", "apt-get", "install", "-y"}
	case "fedora", "rhel", "centos":
		packages = []string{"poppler-utils", "antiword", "unrtf"}
		installCmd = []string{"sudo", "dnf", "install", "-y"}
	case "arch":
		packages = []string{"poppler", "antiword", "unrtf"}
		installCmd = []string{"sudo", "pacman", "-S", "--noconfirm"}
	default:
		fmt.Printf("Unsupported Linux distribution: %s\n", distro)
		fmt.Println("Please install manually: poppler-utils antiword unrtf")
		return []InstallResult{{
			Dependency: "Document Tools",
			Skipped:    true,
			Message:    "Manual installation required",
		}}
	}

	// Build list of packages to install
	var toInstall []string
	if !i.commandExists("pdftotext") {
		toInstall = append(toInstall, packages[0])
	}
	if !i.commandExists("antiword") {
		toInstall = append(toInstall, packages[1])
	}
	if !i.commandExists("unrtf") {
		toInstall = append(toInstall, packages[2])
	}

	if len(toInstall) == 0 {
		fmt.Println("All document tools are already installed.")
		return results
	}

	fmt.Printf("Installing: %s\n", strings.Join(toInstall, " "))
	fullCmd := append(installCmd, toInstall...)

	if !i.confirmAction("Proceed with installation?") {
		return []InstallResult{{
			Dependency: "Document Tools",
			Skipped:    true,
			Message:    "User cancelled",
		}}
	}

	if err := i.runCommand(ctx, fullCmd[0], fullCmd[1:]...); err != nil {
		return []InstallResult{{
			Dependency: "Document Tools",
			Error:      err,
		}}
	}

	// Verify and report
	for _, pkg := range toInstall {
		var toolName string
		switch pkg {
		case "poppler-utils", "poppler":
			toolName = "pdftotext"
		case "antiword":
			toolName = "antiword"
		case "unrtf":
			toolName = "unrtf"
		}

		if i.commandExists(toolName) {
			fmt.Printf("✓ %s installed\n", toolName)
			results = append(results, InstallResult{
				Dependency: toolName,
				Installed:  true,
			})
		} else {
			results = append(results, InstallResult{
				Dependency: toolName,
				Error:      fmt.Errorf("command not found after installation"),
			})
		}
	}

	return results
}

// installDocumentToolsWindows provides guidance for Windows users.
func (i *Installer) installDocumentToolsWindows(ctx context.Context) []InstallResult {
	var results []InstallResult

	fmt.Println()
	fmt.Println("On Windows, document tools can be installed using Chocolatey.")
	fmt.Println()

	// Check if Chocolatey is available
	if i.commandExists("choco") {
		fmt.Println("Chocolatey is installed. Installing document tools...")

		// Install poppler (pdftotext) and antiword
		packages := []string{"poppler", "antiword"}
		for _, pkg := range packages {
			fmt.Printf("Installing %s...\n", pkg)
			if err := i.runCommand(ctx, "choco", "install", "-y", pkg); err != nil {
				results = append(results, InstallResult{
					Dependency: pkg,
					Error:      err,
				})
			} else {
				results = append(results, InstallResult{
					Dependency: pkg,
					Installed:  true,
				})
			}
		}
		return results
	}

	// Chocolatey not installed - provide manual instructions
	fmt.Println("Chocolatey is not installed.")
	fmt.Println()
	fmt.Println("Option 1: Install Chocolatey, then run:")
	fmt.Println("  choco install poppler antiword")
	fmt.Println()
	fmt.Println("Option 2: Manual installation:")
	fmt.Println("  • pdftotext: https://github.com/oschwartz10612/poppler-windows/releases")
	fmt.Println("  • antiword:  https://antiword.sourceforge.net/")
	fmt.Println()

	if i.confirmAction("Install Chocolatey now?") {
		fmt.Println("Installing Chocolatey...")
		chocoInstall := `Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iex ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))`

		cmd := exec.CommandContext(ctx, "powershell", "-Command", chocoInstall)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return []InstallResult{{
				Dependency: "Chocolatey",
				Error:      err,
			}}
		}

		fmt.Println("✓ Chocolatey installed. Installing document tools...")
		return i.installDocumentToolsWindows(ctx)
	}

	return []InstallResult{{
		Dependency: "Document Tools",
		Skipped:    true,
		Message:    "Manual installation required",
	}}
}

// InstallDocumentToolsOnly installs only document extraction tools.
// This is used by the --document-tools flag.
func (i *Installer) InstallDocumentToolsOnly(ctx context.Context) ([]InstallResult, error) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║           Document Extraction Tools Installer                ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	results := i.installDocumentTools(ctx)
	fmt.Println()

	// Print summary
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("                    Installation Summary                       ")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()

	for _, r := range results {
		status := "✓"
		statusText := "Installed"

		if r.AlreadyExists {
			statusText = "Already installed"
		} else if r.Skipped {
			status = "○"
			statusText = "Skipped"
		} else if r.Error != nil {
			status = "✗"
			statusText = fmt.Sprintf("Failed: %v", r.Error)
		}

		fmt.Printf("  %s %s: %s\n", status, r.Dependency, statusText)
	}

	fmt.Println()
	fmt.Println("Run 'conduit doctor' to verify all tools are working correctly.")
	fmt.Println()

	return results, nil
}

// Helper methods

func (i *Installer) commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

func (i *Installer) getCommandOutput(cmd string, args ...string) string {
	out, err := exec.Command(cmd, args...).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func (i *Installer) runCommand(ctx context.Context, cmd string, args ...string) error {
	c := exec.CommandContext(ctx, cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}

func (i *Installer) runShellCommand(ctx context.Context, cmd string) error {
	shell := "/bin/sh"
	if runtime.GOOS == "windows" {
		shell = "cmd"
	}

	var c *exec.Cmd
	if runtime.GOOS == "windows" {
		c = exec.CommandContext(ctx, shell, "/c", cmd)
	} else {
		c = exec.CommandContext(ctx, shell, "-c", cmd)
	}

	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Stdin = os.Stdin
	return c.Run()
}

func (i *Installer) detectLinuxDistro() string {
	// Try /etc/os-release first
	data, err := os.ReadFile("/etc/os-release")
	if err == nil {
		content := string(data)
		if strings.Contains(strings.ToLower(content), "ubuntu") {
			return "ubuntu"
		}
		if strings.Contains(strings.ToLower(content), "debian") {
			return "debian"
		}
		if strings.Contains(strings.ToLower(content), "fedora") {
			return "fedora"
		}
		if strings.Contains(strings.ToLower(content), "rhel") || strings.Contains(strings.ToLower(content), "red hat") {
			return "rhel"
		}
		if strings.Contains(strings.ToLower(content), "centos") {
			return "centos"
		}
		if strings.Contains(strings.ToLower(content), "arch") {
			return "arch"
		}
	}

	return "unknown"
}

func (i *Installer) isOllamaRunning() bool {
	// Try to connect to Ollama API
	cmd := exec.Command("curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://localhost:11434/api/tags")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "200"
}

func (i *Installer) startOllama(ctx context.Context) {
	// Start Ollama in background
	fmt.Println("Starting Ollama service...")

	if runtime.GOOS == "darwin" {
		// On macOS, use the launchd service
		_ = exec.Command("ollama", "serve").Start()
	} else if runtime.GOOS == "linux" {
		// On Linux, try systemd first
		if err := exec.Command("systemctl", "is-enabled", "ollama").Run(); err == nil {
			_ = exec.Command("sudo", "systemctl", "start", "ollama").Run()
		} else {
			// Fall back to running directly
			_ = exec.Command("ollama", "serve").Start()
		}
	}

	// Wait for Ollama to start
	for j := 0; j < 10; j++ {
		if i.isOllamaRunning() {
			fmt.Println("✓ Ollama service is running")
			return
		}
		time.Sleep(time.Second)
	}

	fmt.Println("⚠ Ollama service may not have started. Try 'ollama serve' manually.")
}

func (i *Installer) ollamaModelExists(model string) bool {
	out, err := exec.Command("ollama", "list").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), strings.Split(model, ":")[0])
}

func (i *Installer) prompt(message string) string {
	fmt.Print(message)
	response, _ := i.reader.ReadString('\n')
	return strings.TrimSpace(response)
}

func (i *Installer) confirmAction(message string) bool {
	response := i.prompt(fmt.Sprintf("%s [y/N]: ", message))
	return strings.ToLower(response) == "y" || strings.ToLower(response) == "yes"
}

func (i *Installer) printSummary(results []InstallResult) {
	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("                    Installation Summary                       ")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()

	allSuccess := true
	for _, r := range results {
		status := "✓"
		statusText := "Installed"

		if r.AlreadyExists {
			status = "✓"
			statusText = "Already installed"
		} else if r.Skipped {
			status = "○"
			statusText = "Skipped"
		} else if r.Error != nil {
			status = "✗"
			statusText = fmt.Sprintf("Failed: %v", r.Error)
			allSuccess = false
		} else if !r.Installed {
			status = "○"
			statusText = "Not installed"
			allSuccess = false
		}

		fmt.Printf("  %s %s: %s\n", status, r.Dependency, statusText)
	}

	fmt.Println()

	if allSuccess {
		fmt.Println("All dependencies are installed! You're ready to use Conduit.")
		fmt.Println()
		fmt.Println("Next steps:")
		fmt.Println("  1. Run: conduit setup")
		fmt.Println("  2. Install your first MCP server:")
		fmt.Println("     conduit install https://github.com/7nohe/local-mcp-server-sample")
	} else {
		fmt.Println("Some dependencies were not installed. Please install them manually")
		fmt.Println("or re-run this installer.")
	}
	fmt.Println()
}

// SetupDaemonService sets up the Conduit daemon as a system service.
func (i *Installer) SetupDaemonService(ctx context.Context, binaryPath string) InstallResult {
	fmt.Println()
	fmt.Println("Setting up Conduit daemon service...")

	switch runtime.GOOS {
	case "darwin":
		return i.setupLaunchdService(binaryPath)
	case "linux":
		return i.setupSystemdService(binaryPath)
	default:
		return InstallResult{
			Dependency: "Daemon Service",
			Skipped:    true,
			Message:    fmt.Sprintf("Service setup not supported on %s", runtime.GOOS),
		}
	}
}

func (i *Installer) setupLaunchdService(binaryPath string) InstallResult {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	plistDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	plistPath := filepath.Join(plistDir, "com.simpleflo.conduit.plist")
	conduitHome := filepath.Join(homeDir, ".conduit")

	// Create directory
	if err := os.MkdirAll(plistDir, 0755); err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	plistContent := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.simpleflo.conduit</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>%s/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>%s/daemon.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>%s</string>
    </dict>
</dict>
</plist>`, binaryPath, conduitHome, conduitHome, homeDir)

	if err := os.WriteFile(plistPath, []byte(plistContent), 0644); err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	// Unload if already loaded, then load
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := exec.Command("launchctl", "load", plistPath).Run(); err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	// Start the service
	_ = exec.Command("launchctl", "start", "com.simpleflo.conduit").Run()

	fmt.Println("✓ Daemon service installed (launchd)")
	fmt.Println("  The daemon will start automatically on login.")

	return InstallResult{
		Dependency: "Daemon Service",
		Installed:  true,
		Message:    "launchd service installed",
	}
}

func (i *Installer) setupSystemdService(binaryPath string) InstallResult {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	serviceDir := filepath.Join(homeDir, ".config", "systemd", "user")
	servicePath := filepath.Join(serviceDir, "conduit.service")

	// Create directory
	if err := os.MkdirAll(serviceDir, 0755); err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	serviceContent := fmt.Sprintf(`[Unit]
Description=Conduit AI Intelligence Hub Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s --foreground
Restart=always
RestartSec=10
Environment=HOME=%s

[Install]
WantedBy=default.target
`, binaryPath, homeDir)

	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		return InstallResult{Dependency: "Daemon Service", Error: err}
	}

	// Reload, enable, and start
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	_ = exec.Command("systemctl", "--user", "enable", "conduit").Run()
	if err := exec.Command("systemctl", "--user", "start", "conduit").Run(); err != nil {
		// Non-fatal - service might already be running
		fmt.Printf("  Note: Could not start service: %v\n", err)
	}

	// Enable lingering so service runs without login
	_ = exec.Command("sudo", "loginctl", "enable-linger", os.Getenv("USER")).Run()

	fmt.Println("✓ Daemon service installed (systemd)")
	fmt.Println("  The daemon will start automatically on login.")

	return InstallResult{
		Dependency: "Daemon Service",
		Installed:  true,
		Message:    "systemd user service installed",
	}
}

// StopDaemonService stops the daemon service.
func (i *Installer) StopDaemonService() error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("launchctl", "stop", "com.simpleflo.conduit").Run()
	case "linux":
		return exec.Command("systemctl", "--user", "stop", "conduit").Run()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// RemoveDaemonService removes the daemon service.
func (i *Installer) RemoveDaemonService() error {
	homeDir, _ := os.UserHomeDir()

	switch runtime.GOOS {
	case "darwin":
		plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "com.simpleflo.conduit.plist")
		_ = exec.Command("launchctl", "unload", plistPath).Run()
		return os.Remove(plistPath)
	case "linux":
		servicePath := filepath.Join(homeDir, ".config", "systemd", "user", "conduit.service")
		_ = exec.Command("systemctl", "--user", "stop", "conduit").Run()
		_ = exec.Command("systemctl", "--user", "disable", "conduit").Run()
		return os.Remove(servicePath)
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}

// IsDaemonRunning checks if the daemon is running.
func (i *Installer) IsDaemonRunning() bool {
	homeDir, _ := os.UserHomeDir()
	socketPath := filepath.Join(homeDir, ".conduit", "conduit.sock")

	// Try to connect to the socket
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// StartDaemon starts the daemon in the background.
func (i *Installer) StartDaemon(ctx context.Context, binaryPath string) error {
	if i.IsDaemonRunning() {
		fmt.Println("✓ Daemon is already running")
		return nil
	}

	fmt.Println("Starting Conduit daemon...")

	cmd := exec.Command(binaryPath, "--foreground")
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait for daemon to be ready
	for j := 0; j < 10; j++ {
		if i.IsDaemonRunning() {
			fmt.Println("✓ Daemon started successfully")
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}

	return fmt.Errorf("daemon did not start within timeout")
}

// Uninstall removes Conduit completely.
func (i *Installer) Uninstall(ctx context.Context) error {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   Conduit Uninstaller                        ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	homeDir, _ := os.UserHomeDir()
	conduitHome := filepath.Join(homeDir, ".conduit")

	fmt.Println("This will remove:")
	fmt.Println("  • Conduit daemon service")
	fmt.Println("  • PATH export from shell config (~/.zshrc or ~/.bashrc)")
	fmt.Println("  • Symlinks in /usr/local/bin (if any)")
	fmt.Println()
	fmt.Println("This will NOT remove:")
	fmt.Println("  • Conduit binaries (conduit, conduit-daemon in ~/.local/bin)")
	fmt.Println("  • Docker/Podman")
	fmt.Println("  • Ollama")
	fmt.Println()

	if !i.confirmAction("Proceed with uninstallation?") {
		fmt.Println("Uninstallation cancelled.")
		return nil
	}

	// Step 1: Stop and remove daemon service
	fmt.Println()
	fmt.Println("Step 1: Stop Daemon Service")
	fmt.Println("──────────────────────────────────────────────────────────────")
	fmt.Println("Stopping daemon service...")
	_ = i.StopDaemonService()
	_ = i.RemoveDaemonService()
	fmt.Println("✓ Daemon service removed")

	// Step 1.5: Clean up PATH from shell configs and symlinks
	fmt.Println()
	fmt.Println("Step 1.5: Clean Up Shell Config")
	fmt.Println("──────────────────────────────────────────────────────────────")
	i.cleanupShellConfig(homeDir)
	i.cleanupSymlinks()

	// Step 2: Data removal decision
	fmt.Println()
	fmt.Println("Step 2: Data Removal")
	fmt.Println("──────────────────────────────────────────────────────────────")

	// Check what exists
	hasDataDir := false
	hasQdrantContainer := false
	var containerCmd string
	var qdrantVectors int64

	if _, err := os.Stat(conduitHome); err == nil {
		hasDataDir = true
	}

	// Detect container runtime and check for Qdrant
	if i.commandExists("podman") {
		containerCmd = "podman"
	} else if i.commandExists("docker") {
		containerCmd = "docker"
	}

	if containerCmd != "" {
		out, err := exec.Command(containerCmd, "ps", "-a", "--format", "{{.Names}}").Output()
		if err == nil && strings.Contains(string(out), "conduit-qdrant") {
			hasQdrantContainer = true
			// Try to get vector count
			qdrantVectors = i.getQdrantVectorCount()
		}
	}

	if !hasDataDir && !hasQdrantContainer {
		fmt.Println("No Conduit data found.")
	} else {
		// Show data summary
		fmt.Println()
		fmt.Println("════════════════════════════════════════════════════════════════")
		fmt.Println("                    DATA REMOVAL DECISION                        ")
		fmt.Println("════════════════════════════════════════════════════════════════")
		fmt.Println()
		fmt.Println("Conduit stores the following data:")
		fmt.Println()

		if hasDataDir {
			dataSize := i.getDirSize(conduitHome)
			sqliteSize := i.getFileSize(filepath.Join(conduitHome, "conduit.db"))
			qdrantDataSize := i.getDirSize(filepath.Join(conduitHome, "qdrant"))

			fmt.Printf("  📁 Data Directory: %s\n", conduitHome)
			fmt.Printf("     ├── SQLite Database:     %s (documents, sources, config)\n", formatSize(sqliteSize))
			fmt.Printf("     ├── Qdrant Vector Data:  %s (%d vectors)\n", formatSize(qdrantDataSize), qdrantVectors)
			fmt.Println("     ├── Daemon logs")
			fmt.Println("     └── Configuration files")
			fmt.Printf("     Total size: %s\n", formatSize(dataSize))
		}

		if hasQdrantContainer {
			fmt.Println()
			fmt.Println("  🐳 Qdrant Container: conduit-qdrant")
			fmt.Println("     Running on ports 6333 (HTTP) and 6334 (gRPC)")
		}

		fmt.Println()
		fmt.Println("────────────────────────────────────────────────────────────────")
		fmt.Println()
		fmt.Println("Choose an option:")
		fmt.Println()
		fmt.Println("  [1] DELETE ALL DATA - Remove everything (recommended for clean uninstall)")
		fmt.Println("      • Stops and removes Qdrant container")
		fmt.Println("      • Deletes all vectors, documents, and configuration")
		fmt.Println("      • Fresh start if you reinstall")
		fmt.Println()
		fmt.Println("  [2] KEEP DATA - Preserve data for potential reinstall")
		fmt.Println("      • Qdrant container and vectors preserved")
		fmt.Println("      • SQLite database and config preserved")
		fmt.Println("      • If reinstalling, run 'conduit qdrant purge' to clear stale vectors")
		fmt.Println()

		choice := i.prompt("Enter choice [1/2]: ")

		if choice == "1" {
			fmt.Println()
			fmt.Println("⚠ This will permanently delete all Conduit data!")
			fmt.Println()

			confirmDelete := i.prompt("Type 'DELETE' to confirm: ")
			if confirmDelete != "DELETE" {
				fmt.Println("Deletion cancelled. Data preserved.")
			} else {
				// Remove Qdrant container first
				if hasQdrantContainer && containerCmd != "" {
					fmt.Println()
					fmt.Println("Stopping and removing Qdrant container...")
					_ = exec.Command(containerCmd, "stop", "conduit-qdrant").Run()
					_ = exec.Command(containerCmd, "rm", "conduit-qdrant").Run()
					fmt.Println("✓ Qdrant container removed")
				}

				// Remove data directory
				if hasDataDir {
					fmt.Println("Removing data directory...")
					if err := os.RemoveAll(conduitHome); err != nil {
						fmt.Printf("⚠ Could not remove %s: %v\n", conduitHome, err)
					} else {
						fmt.Println("✓ Data directory removed")
					}
				}

				fmt.Println()
				fmt.Println("✓ All Conduit data has been removed")
			}
		} else {
			fmt.Println()
			fmt.Println("⚠ Data preserved")
			fmt.Println()
			fmt.Printf("  Your data remains at: %s\n", conduitHome)
			if hasQdrantContainer {
				fmt.Println("  Qdrant container: conduit-qdrant (still running)")
			}
			fmt.Println()
			fmt.Println("  If you reinstall Conduit later:")
			fmt.Println("    • Your KB sources and documents will be restored")
			fmt.Println("    • Run 'conduit qdrant purge' if vectors seem stale")
			fmt.Println("    • Run 'conduit kb sync' to re-index documents")
		}
	}

	fmt.Println()
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println("                  Uninstallation Complete                      ")
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Println("To complete removal, delete the binaries manually:")
	fmt.Println("  rm $(which conduit) $(which conduit-daemon)")
	fmt.Println()

	return nil
}

// getQdrantVectorCount returns the number of vectors in Qdrant.
func (i *Installer) getQdrantVectorCount() int64 {
	cmd := exec.Command("curl", "-s", "http://localhost:6333/collections/conduit_kb")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Parse points_count from JSON response
	outStr := string(out)
	if idx := strings.Index(outStr, `"points_count":`); idx != -1 {
		start := idx + len(`"points_count":`)
		end := start
		for end < len(outStr) && (outStr[end] >= '0' && outStr[end] <= '9') {
			end++
		}
		if end > start {
			var count int64
			fmt.Sscanf(outStr[start:end], "%d", &count)
			return count
		}
	}
	return 0
}

// getDirSize returns the size of a directory in bytes.
func (i *Installer) getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// getFileSize returns the size of a file in bytes.
func (i *Installer) getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// formatSize formats bytes as human-readable string.
func formatSize(bytes int64) string {
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

// cleanupShellConfig removes Conduit PATH export from shell config files.
func (i *Installer) cleanupShellConfig(homeDir string) {
	shellConfigs := []string{
		filepath.Join(homeDir, ".zshrc"),
		filepath.Join(homeDir, ".bashrc"),
		filepath.Join(homeDir, ".bash_profile"),
	}

	localBinPath := filepath.Join(homeDir, ".local", "bin")

	for _, configPath := range shellConfigs {
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			continue
		}

		content, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		var newLines []string
		removed := false

		for j := 0; j < len(lines); j++ {
			line := lines[j]
			// Skip the Conduit comment and PATH export lines
			if strings.Contains(line, "# Conduit") {
				// Skip this line and the next if it's the PATH export
				if j+1 < len(lines) && strings.Contains(lines[j+1], localBinPath) {
					j++ // Skip the PATH line too
					removed = true
					continue
				}
			}
			// Also remove standalone PATH lines that reference .local/bin with Conduit
			if strings.Contains(line, localBinPath) && strings.Contains(line, "export PATH") {
				removed = true
				continue
			}
			newLines = append(newLines, line)
		}

		if removed {
			// Remove any trailing empty lines caused by removal
			for len(newLines) > 0 && newLines[len(newLines)-1] == "" {
				newLines = newLines[:len(newLines)-1]
			}
			newLines = append(newLines, "") // Ensure single trailing newline

			if err := os.WriteFile(configPath, []byte(strings.Join(newLines, "\n")), 0644); err != nil {
				fmt.Printf("⚠ Could not update %s: %v\n", configPath, err)
			} else {
				fmt.Printf("✓ Removed PATH from %s\n", configPath)
			}
		}
	}
}

// cleanupSymlinks removes Conduit symlinks from /usr/local/bin.
func (i *Installer) cleanupSymlinks() {
	symlinks := []string{
		"/usr/local/bin/conduit",
		"/usr/local/bin/conduit-daemon",
	}

	for _, link := range symlinks {
		// Check if it's a symlink pointing to our binary
		target, err := os.Readlink(link)
		if err != nil {
			continue // Not a symlink or doesn't exist
		}

		if strings.Contains(target, ".local/bin") {
			if err := os.Remove(link); err != nil {
				fmt.Printf("⚠ Could not remove symlink %s: %v\n", link, err)
			} else {
				fmt.Printf("✓ Removed symlink %s\n", link)
			}
		}
	}
}
