// Package installer handles automated installation and uninstallation of Conduit.
package installer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// UninstallOptions configures what components to remove during uninstallation.
type UninstallOptions struct {
	// Core removal
	RemoveDaemonService bool // Stop daemon, remove launchd/systemd service
	RemoveBinaries      bool // Remove ~/.local/bin/conduit and conduit-daemon
	RemoveShellConfig   bool // Clean PATH from .zshrc, .bashrc
	RemoveSymlinks      bool // Remove /usr/local/bin symlinks

	// Data removal
	RemoveDataDir    bool // Remove ~/.conduit/ entirely
	RemoveConfigOnly bool // Remove only ~/.conduit/conduit.yaml (keep data)

	// Container removal (selective)
	RemoveQdrantContainer   bool // Stop and rm conduit-qdrant container
	RemoveFalkorDBContainer bool // Stop and rm conduit-falkordb container

	// Dependency removal
	RemoveOllama bool // Remove Ollama binary and ~/.ollama models

	// Safety flags
	Force  bool // Skip confirmations
	DryRun bool // Show what would be removed without doing it
	JSON   bool // Output JSON for programmatic consumption
}

// UninstallInfo provides information about what's installed for UI display.
type UninstallInfo struct {
	// Daemon
	HasDaemonService bool   `json:"hasDaemonService"`
	DaemonRunning    bool   `json:"daemonRunning"`
	DaemonPID        int    `json:"daemonPid,omitempty"`
	ServicePath      string `json:"servicePath,omitempty"`

	// Binaries
	HasBinaries    bool   `json:"hasBinaries"`
	ConduitPath    string `json:"conduitPath,omitempty"`
	DaemonPath     string `json:"daemonPath,omitempty"`
	ConduitVersion string `json:"conduitVersion,omitempty"`

	// Data
	HasDataDir     bool   `json:"hasDataDir"`
	DataDirPath    string `json:"dataDirPath,omitempty"`
	DataDirSize    string `json:"dataDirSize,omitempty"`
	DataDirSizeRaw int64  `json:"dataDirSizeRaw,omitempty"`
	HasConfig      bool   `json:"hasConfig"`
	HasSQLite      bool   `json:"hasSqlite"`
	DocumentCount  int    `json:"documentCount,omitempty"`
	SourceCount    int    `json:"sourceCount,omitempty"`

	// Containers
	ContainerRuntime        string `json:"containerRuntime,omitempty"` // "docker", "podman", or ""
	HasQdrantContainer      bool   `json:"hasQdrantContainer"`
	QdrantContainerRunning  bool   `json:"qdrantContainerRunning"`
	QdrantVectorCount       int64  `json:"qdrantVectorCount,omitempty"`
	HasFalkorDBContainer    bool   `json:"hasFalkorDBContainer"`
	FalkorDBContainerRunning bool  `json:"falkordbContainerRunning"`
	FalkorDBEntityCount     int64  `json:"falkordbEntityCount,omitempty"`

	// Ollama
	HasOllama      bool     `json:"hasOllama"`
	OllamaRunning  bool     `json:"ollamaRunning"`
	OllamaModels   []string `json:"ollamaModels,omitempty"`
	OllamaSize     string   `json:"ollamaSize,omitempty"`
	OllamaSizeRaw  int64    `json:"ollamaSizeRaw,omitempty"`

	// Shell config
	HasShellConfig   bool     `json:"hasShellConfig"`
	ShellConfigFiles []string `json:"shellConfigFiles,omitempty"`

	// Symlinks
	HasSymlinks bool     `json:"hasSymlinks"`
	Symlinks    []string `json:"symlinks,omitempty"`
}

// UninstallResult tracks what was actually removed.
type UninstallResult struct {
	Success      bool     `json:"success"`
	ItemsRemoved []string `json:"itemsRemoved"`
	ItemsFailed  []string `json:"itemsFailed"`
	Errors       []string `json:"errors"`
}

// NewUninstallOptionsKeepData returns options for Tier 1: Uninstall only (keep data).
func NewUninstallOptionsKeepData() UninstallOptions {
	return UninstallOptions{
		RemoveDaemonService: true,
		RemoveBinaries:      true,
		RemoveShellConfig:   true,
		RemoveSymlinks:      true,
		RemoveDataDir:       false,
		RemoveQdrantContainer:   false,
		RemoveFalkorDBContainer: false,
		RemoveOllama:        false,
	}
}

// NewUninstallOptionsAll returns options for Tier 2: Uninstall with data.
func NewUninstallOptionsAll() UninstallOptions {
	return UninstallOptions{
		RemoveDaemonService:     true,
		RemoveBinaries:          true,
		RemoveShellConfig:       true,
		RemoveSymlinks:          true,
		RemoveDataDir:           true,
		RemoveQdrantContainer:   true,
		RemoveFalkorDBContainer: true,
		RemoveOllama:            false,
	}
}

// NewUninstallOptionsFull returns options for Tier 3: Full cleanup.
func NewUninstallOptionsFull() UninstallOptions {
	opts := NewUninstallOptionsAll()
	opts.RemoveOllama = true
	return opts
}

// GetUninstallInfo gathers information about what's installed for UI display.
func (i *Installer) GetUninstallInfo(ctx context.Context) (*UninstallInfo, error) {
	info := &UninstallInfo{}
	homeDir, _ := os.UserHomeDir()

	// Check daemon service
	info.HasDaemonService, info.ServicePath = i.checkDaemonServiceExists(homeDir)
	info.DaemonRunning = i.checkDaemonRunning()

	// Check binaries
	localBin := filepath.Join(homeDir, ".local", "bin")
	conduitPath := filepath.Join(localBin, "conduit")
	daemonPath := filepath.Join(localBin, "conduit-daemon")

	if _, err := os.Stat(conduitPath); err == nil {
		info.HasBinaries = true
		info.ConduitPath = conduitPath
		// Get version
		if out, err := exec.Command(conduitPath, "--version").Output(); err == nil {
			info.ConduitVersion = strings.TrimSpace(string(out))
		}
	}
	if _, err := os.Stat(daemonPath); err == nil {
		info.HasBinaries = true
		info.DaemonPath = daemonPath
	}

	// Check data directory
	conduitHome := filepath.Join(homeDir, ".conduit")
	if stat, err := os.Stat(conduitHome); err == nil && stat.IsDir() {
		info.HasDataDir = true
		info.DataDirPath = conduitHome
		info.DataDirSizeRaw = i.getDirSize(conduitHome)
		info.DataDirSize = formatSize(info.DataDirSizeRaw)

		// Check for config
		if _, err := os.Stat(filepath.Join(conduitHome, "conduit.yaml")); err == nil {
			info.HasConfig = true
		}

		// Check for SQLite
		if _, err := os.Stat(filepath.Join(conduitHome, "conduit.db")); err == nil {
			info.HasSQLite = true
			// Could query counts but keep it simple for now
		}
	}

	// Check container runtime and containers
	info.ContainerRuntime = i.detectContainerRuntime()
	if info.ContainerRuntime != "" {
		info.HasQdrantContainer, info.QdrantContainerRunning = i.checkContainer(info.ContainerRuntime, "conduit-qdrant")
		info.HasFalkorDBContainer, info.FalkorDBContainerRunning = i.checkContainer(info.ContainerRuntime, "conduit-falkordb")

		// Get Qdrant vector count if running
		if info.QdrantContainerRunning {
			info.QdrantVectorCount = i.getQdrantVectorCount()
		}
	}

	// Check Ollama
	if i.commandExists("ollama") {
		info.HasOllama = true
		info.OllamaRunning = i.checkOllamaRunning()
		info.OllamaModels = i.getOllamaModels()

		// Get ~/.ollama size
		ollamaDir := filepath.Join(homeDir, ".ollama")
		if stat, err := os.Stat(ollamaDir); err == nil && stat.IsDir() {
			info.OllamaSizeRaw = i.getDirSize(ollamaDir)
			info.OllamaSize = formatSize(info.OllamaSizeRaw)
		}
	}

	// Check shell config
	info.ShellConfigFiles = i.findShellConfigsWithConduit(homeDir)
	info.HasShellConfig = len(info.ShellConfigFiles) > 0

	// Check symlinks
	info.Symlinks = i.findConduitSymlinks()
	info.HasSymlinks = len(info.Symlinks) > 0

	return info, nil
}

// UninstallWithOptions performs uninstallation based on provided options.
func (i *Installer) UninstallWithOptions(ctx context.Context, opts UninstallOptions) (*UninstallResult, error) {
	result := &UninstallResult{Success: true}
	homeDir, _ := os.UserHomeDir()

	if opts.DryRun {
		return i.dryRunUninstall(ctx, opts)
	}

	// 1. Stop and remove daemon service
	if opts.RemoveDaemonService {
		if err := i.StopDaemonService(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to stop daemon: %v", err))
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, "Daemon service stopped")
		}

		if err := i.RemoveDaemonService(); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove daemon service: %v", err))
			result.ItemsFailed = append(result.ItemsFailed, "Daemon service")
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, "Daemon service removed")
		}
	}

	// 2. Remove containers before data (they may write to data dir)
	containerRuntime := i.detectContainerRuntime()
	if opts.RemoveQdrantContainer && containerRuntime != "" {
		if err := i.removeContainer(containerRuntime, "conduit-qdrant"); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove Qdrant container: %v", err))
			result.ItemsFailed = append(result.ItemsFailed, "Qdrant container")
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, "Qdrant container removed")
		}
	}

	if opts.RemoveFalkorDBContainer && containerRuntime != "" {
		if err := i.removeContainer(containerRuntime, "conduit-falkordb"); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove FalkorDB container: %v", err))
			result.ItemsFailed = append(result.ItemsFailed, "FalkorDB container")
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, "FalkorDB container removed")
		}
	}

	// 3. Remove data directory
	if opts.RemoveDataDir {
		conduitHome := filepath.Join(homeDir, ".conduit")
		if err := os.RemoveAll(conduitHome); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove data dir: %v", err))
			result.ItemsFailed = append(result.ItemsFailed, conduitHome)
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("Data directory: %s", conduitHome))
		}
	} else if opts.RemoveConfigOnly {
		configPath := filepath.Join(homeDir, ".conduit", "conduit.yaml")
		if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove config: %v", err))
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, "Config file removed")
		}
	}

	// 4. Remove binaries
	if opts.RemoveBinaries {
		localBin := filepath.Join(homeDir, ".local", "bin")
		binaries := []string{
			filepath.Join(localBin, "conduit"),
			filepath.Join(localBin, "conduit-daemon"),
		}
		for _, bin := range binaries {
			if _, err := os.Stat(bin); err == nil {
				if err := os.Remove(bin); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove %s: %v", bin, err))
					result.ItemsFailed = append(result.ItemsFailed, bin)
				} else {
					result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("Binary: %s", bin))
				}
			}
		}
	}

	// 5. Clean shell config
	if opts.RemoveShellConfig {
		removed := i.cleanupShellConfigSilent(homeDir)
		for _, f := range removed {
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("PATH removed from: %s", f))
		}
	}

	// 6. Remove symlinks
	if opts.RemoveSymlinks {
		removed := i.cleanupSymlinksSilent()
		for _, s := range removed {
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("Symlink: %s", s))
		}
	}

	// 7. Remove Ollama
	if opts.RemoveOllama {
		if err := i.removeOllama(homeDir); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove Ollama: %v", err))
			result.ItemsFailed = append(result.ItemsFailed, "Ollama")
		} else {
			result.ItemsRemoved = append(result.ItemsRemoved, "Ollama and models removed")
		}
	}

	// Set success based on failures
	result.Success = len(result.ItemsFailed) == 0

	return result, nil
}

// dryRunUninstall returns what would be removed without actually removing anything.
func (i *Installer) dryRunUninstall(ctx context.Context, opts UninstallOptions) (*UninstallResult, error) {
	result := &UninstallResult{Success: true}
	homeDir, _ := os.UserHomeDir()

	if opts.RemoveDaemonService {
		if exists, path := i.checkDaemonServiceExists(homeDir); exists {
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove daemon service: %s", path))
		}
	}

	containerRuntime := i.detectContainerRuntime()
	if opts.RemoveQdrantContainer && containerRuntime != "" {
		if exists, _ := i.checkContainer(containerRuntime, "conduit-qdrant"); exists {
			result.ItemsRemoved = append(result.ItemsRemoved, "[DRY RUN] Would remove container: conduit-qdrant")
		}
	}

	if opts.RemoveFalkorDBContainer && containerRuntime != "" {
		if exists, _ := i.checkContainer(containerRuntime, "conduit-falkordb"); exists {
			result.ItemsRemoved = append(result.ItemsRemoved, "[DRY RUN] Would remove container: conduit-falkordb")
		}
	}

	if opts.RemoveDataDir {
		conduitHome := filepath.Join(homeDir, ".conduit")
		if stat, err := os.Stat(conduitHome); err == nil && stat.IsDir() {
			size := formatSize(i.getDirSize(conduitHome))
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove data dir: %s (%s)", conduitHome, size))
		}
	}

	if opts.RemoveBinaries {
		localBin := filepath.Join(homeDir, ".local", "bin")
		for _, name := range []string{"conduit", "conduit-daemon"} {
			path := filepath.Join(localBin, name)
			if _, err := os.Stat(path); err == nil {
				result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove binary: %s", path))
			}
		}
	}

	if opts.RemoveShellConfig {
		files := i.findShellConfigsWithConduit(homeDir)
		for _, f := range files {
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would clean PATH from: %s", f))
		}
	}

	if opts.RemoveSymlinks {
		symlinks := i.findConduitSymlinks()
		for _, s := range symlinks {
			result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove symlink: %s", s))
		}
	}

	if opts.RemoveOllama && i.commandExists("ollama") {
		ollamaDir := filepath.Join(homeDir, ".ollama")
		size := formatSize(i.getDirSize(ollamaDir))
		result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("[DRY RUN] Would remove Ollama and ~/.ollama (%s)", size))
	}

	return result, nil
}

// Helper methods

func (i *Installer) checkDaemonServiceExists(homeDir string) (bool, string) {
	switch runtime.GOOS {
	case "darwin":
		plistPath := filepath.Join(homeDir, "Library", "LaunchAgents", "dev.simpleflo.conduit.plist")
		if _, err := os.Stat(plistPath); err == nil {
			return true, plistPath
		}
	case "linux":
		servicePath := filepath.Join(homeDir, ".config", "systemd", "user", "conduit.service")
		if _, err := os.Stat(servicePath); err == nil {
			return true, servicePath
		}
	}
	return false, ""
}

func (i *Installer) checkDaemonRunning() bool {
	// Try health endpoint
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:9090/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (i *Installer) detectContainerRuntime() string {
	if i.commandExists("podman") {
		return "podman"
	}
	if i.commandExists("docker") {
		return "docker"
	}
	return ""
}

func (i *Installer) checkContainer(runtime, name string) (exists bool, running bool) {
	// Check if container exists
	out, err := exec.Command(runtime, "ps", "-a", "--format", "{{.Names}}").Output()
	if err != nil {
		return false, false
	}
	if !strings.Contains(string(out), name) {
		return false, false
	}

	// Check if running
	out, err = exec.Command(runtime, "ps", "--format", "{{.Names}}").Output()
	if err != nil {
		return true, false
	}
	return true, strings.Contains(string(out), name)
}

func (i *Installer) removeContainer(runtime, name string) error {
	// Stop container
	_ = exec.Command(runtime, "stop", name).Run()

	// Remove container
	return exec.Command(runtime, "rm", name).Run()
}

func (i *Installer) checkOllamaRunning() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (i *Installer) getOllamaModels() []string {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	var models []string
	for _, m := range result.Models {
		models = append(models, m.Name)
	}
	return models
}

func (i *Installer) removeOllama(homeDir string) error {
	// Stop Ollama service
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("pkill", "-f", "ollama serve").Run()
	case "linux":
		_ = exec.Command("systemctl", "--user", "stop", "ollama").Run()
		_ = exec.Command("sudo", "systemctl", "stop", "ollama").Run()
	}

	// Wait for it to stop
	time.Sleep(time.Second)

	// Remove Ollama binary
	binaryPaths := []string{
		"/usr/local/bin/ollama",
		"/usr/bin/ollama",
	}
	for _, path := range binaryPaths {
		if _, err := os.Stat(path); err == nil {
			// Need sudo to remove from these paths
			if err := exec.Command("sudo", "rm", "-f", path).Run(); err != nil {
				// Try without sudo (might work if user owns it)
				_ = os.Remove(path)
			}
		}
	}

	// Remove ~/.ollama directory (models and data)
	ollamaDir := filepath.Join(homeDir, ".ollama")
	if err := os.RemoveAll(ollamaDir); err != nil {
		return fmt.Errorf("failed to remove ~/.ollama: %w", err)
	}

	return nil
}

func (i *Installer) findShellConfigsWithConduit(homeDir string) []string {
	var found []string
	localBin := filepath.Join(homeDir, ".local", "bin")

	configs := []string{
		filepath.Join(homeDir, ".zshrc"),
		filepath.Join(homeDir, ".bashrc"),
		filepath.Join(homeDir, ".bash_profile"),
		filepath.Join(homeDir, ".config", "fish", "config.fish"),
	}

	for _, config := range configs {
		content, err := os.ReadFile(config)
		if err != nil {
			continue
		}
		if strings.Contains(string(content), localBin) || strings.Contains(string(content), "# Conduit") {
			found = append(found, config)
		}
	}

	return found
}

// cleanupShellConfigSilent removes PATH entries silently and returns files modified.
func (i *Installer) cleanupShellConfigSilent(homeDir string) []string {
	var modified []string
	localBin := filepath.Join(homeDir, ".local", "bin")

	configs := []string{
		filepath.Join(homeDir, ".zshrc"),
		filepath.Join(homeDir, ".bashrc"),
		filepath.Join(homeDir, ".bash_profile"),
	}

	for _, configPath := range configs {
		content, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		lines := strings.Split(string(content), "\n")
		var newLines []string
		changed := false

		for j := 0; j < len(lines); j++ {
			line := lines[j]
			// Skip the Conduit comment and PATH export lines
			if strings.Contains(line, "# Conduit") {
				if j+1 < len(lines) && strings.Contains(lines[j+1], localBin) {
					j++
					changed = true
					continue
				}
			}
			if strings.Contains(line, localBin) && strings.Contains(line, "export PATH") {
				changed = true
				continue
			}
			newLines = append(newLines, line)
		}

		if changed {
			// Clean trailing empty lines
			for len(newLines) > 0 && newLines[len(newLines)-1] == "" {
				newLines = newLines[:len(newLines)-1]
			}
			newLines = append(newLines, "")

			if err := os.WriteFile(configPath, []byte(strings.Join(newLines, "\n")), 0644); err == nil {
				modified = append(modified, configPath)
			}
		}
	}

	return modified
}

func (i *Installer) findConduitSymlinks() []string {
	var found []string
	symlinks := []string{
		"/usr/local/bin/conduit",
		"/usr/local/bin/conduit-daemon",
	}

	for _, link := range symlinks {
		target, err := os.Readlink(link)
		if err != nil {
			continue
		}
		if strings.Contains(target, ".local/bin") {
			found = append(found, link)
		}
	}

	return found
}

// cleanupSymlinksSilent removes symlinks silently and returns those removed.
func (i *Installer) cleanupSymlinksSilent() []string {
	var removed []string
	symlinks := i.findConduitSymlinks()

	for _, link := range symlinks {
		if err := os.Remove(link); err == nil {
			removed = append(removed, link)
		}
	}

	return removed
}

// PrintUninstallInfo prints uninstall info in a formatted way for CLI.
func (i *Installer) PrintUninstallInfo(info *UninstallInfo) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                   Conduit Installation Status                 ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// Daemon
	fmt.Println("Daemon Service:")
	if info.HasDaemonService {
		status := "stopped"
		if info.DaemonRunning {
			status = "running"
		}
		fmt.Printf("  ✓ Installed at %s (%s)\n", info.ServicePath, status)
	} else {
		fmt.Println("  ✗ Not installed")
	}

	// Binaries
	fmt.Println("\nBinaries:")
	if info.HasBinaries {
		if info.ConduitPath != "" {
			fmt.Printf("  ✓ %s", info.ConduitPath)
			if info.ConduitVersion != "" {
				fmt.Printf(" (%s)", info.ConduitVersion)
			}
			fmt.Println()
		}
		if info.DaemonPath != "" {
			fmt.Printf("  ✓ %s\n", info.DaemonPath)
		}
	} else {
		fmt.Println("  ✗ Not installed")
	}

	// Data
	fmt.Println("\nData Directory:")
	if info.HasDataDir {
		fmt.Printf("  ✓ %s (%s)\n", info.DataDirPath, info.DataDirSize)
		if info.HasConfig {
			fmt.Println("    ├── conduit.yaml")
		}
		if info.HasSQLite {
			fmt.Println("    └── conduit.db")
		}
	} else {
		fmt.Println("  ✗ Not found")
	}

	// Containers
	fmt.Println("\nContainers:")
	if info.ContainerRuntime != "" {
		fmt.Printf("  Runtime: %s\n", info.ContainerRuntime)
		if info.HasQdrantContainer {
			status := "stopped"
			if info.QdrantContainerRunning {
				status = fmt.Sprintf("running, %d vectors", info.QdrantVectorCount)
			}
			fmt.Printf("  ✓ conduit-qdrant (%s)\n", status)
		} else {
			fmt.Println("  ✗ conduit-qdrant (not found)")
		}
		if info.HasFalkorDBContainer {
			status := "stopped"
			if info.FalkorDBContainerRunning {
				status = "running"
			}
			fmt.Printf("  ✓ conduit-falkordb (%s)\n", status)
		} else {
			fmt.Println("  ✗ conduit-falkordb (not found)")
		}
	} else {
		fmt.Println("  ✗ No container runtime found")
	}

	// Ollama
	fmt.Println("\nOllama:")
	if info.HasOllama {
		status := "stopped"
		if info.OllamaRunning {
			status = "running"
		}
		fmt.Printf("  ✓ Installed (%s)\n", status)
		if len(info.OllamaModels) > 0 {
			fmt.Printf("    Models: %s\n", strings.Join(info.OllamaModels, ", "))
		}
		if info.OllamaSize != "" {
			fmt.Printf("    Size: %s\n", info.OllamaSize)
		}
	} else {
		fmt.Println("  ✗ Not installed")
	}

	// Shell config
	fmt.Println("\nShell Configuration:")
	if info.HasShellConfig {
		for _, f := range info.ShellConfigFiles {
			fmt.Printf("  ✓ PATH export in %s\n", f)
		}
	} else {
		fmt.Println("  ✗ No PATH exports found")
	}

	// Symlinks
	fmt.Println("\nSymlinks:")
	if info.HasSymlinks {
		for _, s := range info.Symlinks {
			fmt.Printf("  ✓ %s\n", s)
		}
	} else {
		fmt.Println("  ✗ None found")
	}

	fmt.Println()
}
