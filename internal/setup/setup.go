// Package setup handles the small amount of machine preparation Conduit still
// needs, and its removal.
//
// It replaces internal/installer, which existed to install and orchestrate a
// container runtime, container images for Qdrant and FalkorDB, an Ollama
// service, and a launchd/systemd unit for the daemon. WP-3.2 deleted every one
// of those, so what is left is genuinely small: optional document extraction
// tools, and knowing how to take Conduit off a machine again.
//
// Nothing here installs a package manager or edits system state without being
// asked. Bootstrapping a machine from nothing is the install script's job.
package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/simpleflo/conduit/internal/kb"
)

// InstallResult reports the outcome of one setup step.
type InstallResult struct {
	// Name is the thing being set up, e.g. "pdftotext (PDF)".
	Name string `json:"name"`

	// Success is true when the tool is usable after this step.
	Success bool `json:"success"`

	// Message explains the outcome, and for a failure says what to run.
	Message string `json:"message,omitempty"`
}

// documentTool is an optional external extractor.
type documentTool struct {
	// binary is the executable to look for.
	binary string
	// label is what the user sees.
	label string
	// brewPkg and aptPkg name the package providing it, empty when none.
	brewPkg string
	aptPkg  string
}

// documentTools are the extractors Conduit can use but does not require.
//
// DOCX and ODT are handled by pure-Go extractors and need nothing installed;
// they are deliberately absent here. On macOS, DOC and RTF are handled by the
// built-in textutil, so only pdftotext is ever missing.
func documentTools() []documentTool {
	tools := []documentTool{
		{binary: "pdftotext", label: "pdftotext (PDF)", brewPkg: "poppler", aptPkg: "poppler-utils"},
	}
	if runtime.GOOS != "darwin" {
		tools = append(tools,
			documentTool{binary: "antiword", label: "antiword (DOC)", aptPkg: "antiword"},
			documentTool{binary: "unrtf", label: "unrtf (RTF)", aptPkg: "unrtf"},
		)
	}
	return tools
}

// CheckDocumentTools reports the availability of every extraction tool,
// including the ones that need nothing installed.
func CheckDocumentTools() []kb.ToolStatus {
	return kb.GetToolStatus()
}

// InstallDocumentTools installs any missing document extraction tool using the
// platform package manager, when one is present.
//
// It returns one result per tool that needed attention; an empty slice means
// everything was already available. A missing package manager is reported with
// the exact command to run, not treated as a failure of Conduit.
func InstallDocumentTools(ctx context.Context, verbose bool) ([]InstallResult, error) {
	var results []InstallResult

	for _, tool := range documentTools() {
		if _, err := exec.LookPath(tool.binary); err == nil {
			continue // already available; nothing to report
		}

		mgr, pkg := packageManagerFor(tool)
		if mgr == "" {
			results = append(results, InstallResult{
				Name:    tool.label,
				Success: false,
				Message: fmt.Sprintf("not installed; no supported package manager found on %s", runtime.GOOS),
			})
			continue
		}

		if verbose {
			fmt.Printf("  installing %s via %s...\n", tool.label, mgr)
		}

		if err := runInstall(ctx, mgr, pkg, verbose); err != nil {
			results = append(results, InstallResult{
				Name:    tool.label,
				Success: false,
				Message: fmt.Sprintf("not installed; run: %s", installCommand(mgr, pkg)),
			})
			continue
		}

		results = append(results, InstallResult{
			Name:    tool.label,
			Success: true,
			Message: fmt.Sprintf("installed via %s", mgr),
		})
	}

	return results, nil
}

// packageManagerFor picks the package manager to use for a tool, or "" when
// none is usable.
func packageManagerFor(tool documentTool) (mgr string, pkg string) {
	if runtime.GOOS == "darwin" {
		if tool.brewPkg != "" && commandExists("brew") {
			return "brew", tool.brewPkg
		}
		return "", ""
	}
	if tool.aptPkg != "" && commandExists("apt-get") {
		return "apt-get", tool.aptPkg
	}
	if tool.brewPkg != "" && commandExists("brew") {
		return "brew", tool.brewPkg
	}
	return "", ""
}

func installCommand(mgr, pkg string) string {
	if mgr == "apt-get" {
		return "sudo apt-get install -y " + pkg
	}
	return mgr + " install " + pkg
}

// runInstall shells out to the package manager.
//
// apt-get needs root; rather than invoking sudo behind the user's back, the
// command is only attempted when already running as root, and otherwise
// reported so the user can run it themselves.
func runInstall(ctx context.Context, mgr, pkg string, verbose bool) error {
	var cmd *exec.Cmd
	switch mgr {
	case "brew":
		cmd = exec.CommandContext(ctx, "brew", "install", pkg)
	case "apt-get":
		if os.Geteuid() != 0 {
			return fmt.Errorf("apt-get requires root")
		}
		cmd = exec.CommandContext(ctx, "apt-get", "install", "-y", pkg)
	default:
		return fmt.Errorf("unsupported package manager %q", mgr)
	}

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ---------------------------------------------------------------------------
// Uninstall
// ---------------------------------------------------------------------------

// UninstallOptions configures what to remove.
//
// Dependencies that a user may share with other projects -- Ollama, package
// manager installs such as poppler -- are deliberately never removed.
type UninstallOptions struct {
	// RemoveBinaries removes the conduit executable from the usual locations.
	RemoveBinaries bool

	// RemoveShellConfig strips Conduit's PATH lines from shell rc files.
	RemoveShellConfig bool

	// RemoveSymlinks removes /usr/local/bin symlinks.
	RemoveSymlinks bool

	// RemoveDataDir removes ~/.conduit entirely, knowledge base included.
	RemoveDataDir bool

	// Prefix limits binary removal to a single directory.
	//
	// Empty means the conventional locations. A non-empty value is a promise
	// that nothing outside it is touched: `--prefix /tmp/scratch` must never
	// delete the copy in ~/.local/bin, which is exactly what would happen if
	// this only *added* a path to the search list. Symlink cleanup is skipped
	// too, since a symlink in /usr/local/bin is not part of that prefix.
	Prefix string

	// RemoveConfigOnly removes just the config file, keeping indexed data.
	RemoveConfigOnly bool

	// Force skips confirmations (handled by the caller).
	Force bool

	// DryRun reports what would happen without touching anything.
	DryRun bool

	// JSON signals machine-readable output (handled by the caller).
	JSON bool
}

// NewUninstallOptionsKeepData removes the program but preserves indexed data.
func NewUninstallOptionsKeepData() UninstallOptions {
	return UninstallOptions{
		RemoveBinaries:    true,
		RemoveShellConfig: true,
		RemoveSymlinks:    true,
		RemoveDataDir:     false,
	}
}

// NewUninstallOptionsAll removes the program and all data.
func NewUninstallOptionsAll() UninstallOptions {
	return UninstallOptions{
		RemoveBinaries:    true,
		RemoveShellConfig: true,
		RemoveSymlinks:    true,
		RemoveDataDir:     true,
	}
}

// UninstallInfo describes what is currently installed.
//
// The daemon-service and container fields that used to live here are gone with
// the subsystems they described.
type UninstallInfo struct {
	// Binaries
	HasBinaries    bool   `json:"hasBinaries"`
	ConduitPath    string `json:"conduitPath,omitempty"`
	ConduitVersion string `json:"conduitVersion,omitempty"`

	// Data
	HasDataDir     bool   `json:"hasDataDir"`
	DataDirPath    string `json:"dataDirPath,omitempty"`
	DataDirSize    string `json:"dataDirSize,omitempty"`
	DataDirSizeRaw int64  `json:"dataDirSizeRaw,omitempty"`
	HasConfig      bool   `json:"hasConfig"`
	HasSQLite      bool   `json:"hasSqlite"`

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

// binaryPaths are the places Conduit installs itself.
func binaryPaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".local", "bin", "conduit"),
		filepath.Join(home, "bin", "conduit"),
	}
}

// targetBinaryPaths returns the executables an uninstall should remove.
//
// A non-empty prefix replaces the default list rather than extending it. That
// is the whole point of the flag: it names one install, and anything else on
// the machine belongs to somebody else's.
func targetBinaryPaths(prefix string) []string {
	if prefix == "" {
		return binaryPaths()
	}
	return []string{filepath.Join(prefix, "conduit")}
}

// symlinkPaths are the system-wide symlinks an install may create.
func symlinkPaths() []string {
	return []string{"/usr/local/bin/conduit"}
}

// shellConfigPaths are the rc files an install may have edited.
func shellConfigPaths() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".config", "fish", "config.fish"),
	}
}

// localBinDir is the directory the installer adds to PATH.
func localBinDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

// GetUninstallInfo gathers what is present on this machine.
//
// dataDir is the resolved Conduit data directory.
func GetUninstallInfo(ctx context.Context, dataDir string) (*UninstallInfo, error) {
	info := &UninstallInfo{}

	for _, p := range binaryPaths() {
		if _, err := os.Stat(p); err == nil {
			info.HasBinaries = true
			if info.ConduitPath == "" {
				info.ConduitPath = p
				info.ConduitVersion = binaryVersion(ctx, p)
			}
		}
	}

	if st, err := os.Stat(dataDir); err == nil && st.IsDir() {
		info.HasDataDir = true
		info.DataDirPath = dataDir
		size := dirSize(dataDir)
		info.DataDirSizeRaw = size
		info.DataDirSize = humanBytes(size)

		if _, err := os.Stat(filepath.Join(dataDir, "conduit.yaml")); err == nil {
			info.HasConfig = true
		}
		if _, err := os.Stat(filepath.Join(dataDir, "conduit.db")); err == nil {
			info.HasSQLite = true
		}
	}

	for _, p := range shellConfigPaths() {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		if hasConduitPathLine(string(data)) {
			info.HasShellConfig = true
			info.ShellConfigFiles = append(info.ShellConfigFiles, p)
		}
	}

	for _, p := range symlinkPaths() {
		if _, err := os.Lstat(p); err == nil {
			info.HasSymlinks = true
			info.Symlinks = append(info.Symlinks, p)
		}
	}

	return info, nil
}

// conduitPathMarker is the comment the install script writes above the PATH
// line it adds. Detection and removal both key off it, exactly as the v1
// installer did, so an existing install still uninstalls cleanly.
const conduitPathMarker = "# Conduit"

// hasConduitPathLine reports whether a profile carries Conduit's PATH block.
//
// Detection has to use the same rule as removal. It used to return true for any
// file merely mentioning ~/.local/bin, which meant `conduit uninstall --info`
// reported "shell configuration found" on machines where pipx or uv had written
// that line and Conduit never had.
func hasConduitPathLine(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		if isConduitPathMarker(line) {
			return true
		}
	}
	return false
}

func binaryVersion(ctx context.Context, path string) string {
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// PrintUninstallInfo renders the installation status for a human.
func PrintUninstallInfo(info *UninstallInfo) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println("                     Conduit Installation                       ")
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()

	fmt.Println("Binaries:")
	if info.HasBinaries {
		fmt.Printf("  ✓ %s\n", info.ConduitPath)
		if info.ConduitVersion != "" {
			fmt.Printf("    %s\n", info.ConduitVersion)
		}
	} else {
		fmt.Println("  - none found in the usual locations")
	}

	fmt.Println()
	fmt.Println("Data:")
	if info.HasDataDir {
		fmt.Printf("  ✓ %s (%s)\n", info.DataDirPath, info.DataDirSize)
		fmt.Printf("    config:        %v\n", info.HasConfig)
		fmt.Printf("    knowledge base: %v\n", info.HasSQLite)
	} else {
		fmt.Println("  - no data directory")
	}

	if info.HasShellConfig {
		fmt.Println()
		fmt.Println("Shell configuration:")
		for _, f := range info.ShellConfigFiles {
			fmt.Printf("  ✓ %s\n", f)
		}
	}

	if info.HasSymlinks {
		fmt.Println()
		fmt.Println("Symlinks:")
		for _, s := range info.Symlinks {
			fmt.Printf("  ✓ %s\n", s)
		}
	}

	fmt.Println()
}

// Uninstall removes the selected components.
func Uninstall(ctx context.Context, dataDir string, opts UninstallOptions) (*UninstallResult, error) {
	result := &UninstallResult{Success: true}

	remove := func(label, path string, dir bool) {
		if _, err := os.Lstat(path); err != nil {
			return
		}
		if opts.DryRun {
			result.ItemsRemoved = append(result.ItemsRemoved,
				fmt.Sprintf("[DRY RUN] Would remove %s: %s", label, path))
			return
		}
		var err error
		if dir {
			err = os.RemoveAll(path)
		} else {
			err = os.Remove(path)
		}
		if err != nil {
			result.Success = false
			result.ItemsFailed = append(result.ItemsFailed, fmt.Sprintf("%s: %s", label, path))
			result.Errors = append(result.Errors, fmt.Sprintf("failed to remove %s: %v", path, err))
			return
		}
		result.ItemsRemoved = append(result.ItemsRemoved, fmt.Sprintf("%s: %s", label, path))
	}

	if opts.RemoveBinaries {
		for _, p := range targetBinaryPaths(opts.Prefix) {
			remove("binary", p, false)
		}
	}

	// A prefix-scoped uninstall must not reach outside that prefix, and
	// /usr/local/bin is outside every prefix but its own.
	if opts.RemoveSymlinks && opts.Prefix == "" {
		for _, p := range symlinkPaths() {
			remove("symlink", p, false)
		}
	}

	// The PATH line in a shell profile names the default prefix, so it belongs
	// to the default install, not to whichever one --prefix selected.
	if opts.RemoveShellConfig && opts.Prefix == "" {
		for _, p := range shellConfigPaths() {
			if err := stripShellConfig(p, opts.DryRun, result); err != nil {
				result.Success = false
				result.Errors = append(result.Errors, err.Error())
			}
		}
	}

	// The data directory has nothing to do with the prefix a binary was
	// installed under, so a prefix-scoped uninstall must not delete it. Without
	// this gate, `--all --prefix /tmp/scratch` wiped the real ~/.conduit while
	// reporting that it had touched only the scratch directory -- the exact
	// opposite of what Prefix promises. Callers should reject that flag
	// combination outright; this is the backstop that makes the promise true
	// for every caller of the library.
	if opts.Prefix == "" && (opts.RemoveDataDir || opts.RemoveConfigOnly) {
		// Canonicalise and vet the path here, not only in the wrapper scripts.
		// This function is called directly by the CLI, by the desktop GUI and
		// by anything else that links the package, and a guard living only in
		// uninstall.sh protects none of them.
		safe, err := AssertRemovableDataDir(dataDir)
		if err != nil {
			result.Success = false
			result.Errors = append(result.Errors, err.Error())
			return result, err
		}

		// A directory holding none of Conduit's files is far more likely to be
		// a mistyped path than a real request. Checked here rather than only in
		// the CLI so that the desktop GUI and any other caller are covered by
		// the same backstop.
		if opts.RemoveDataDir {
			if cerr := assertConduitDataDir(safe, opts.Force); cerr != nil {
				result.Success = false
				result.Errors = append(result.Errors, cerr.Error())
				return result, cerr
			}
		}

		switch {
		case opts.RemoveDataDir:
			remove("data directory", safe, true)
		case opts.RemoveConfigOnly:
			remove("config", filepath.Join(safe, "conduit.yaml"), false)
		}
	}

	return result, nil
}

// stripShellConfig removes the PATH block an install added to an rc file.
//
// Matching is strictly the marker comment plus the single line immediately
// after it. The previous version also deleted any `export PATH` line mentioning
// ~/.local/bin, which is not a Conduit signature at all: pipx, uv, poetry, pip
// --user and countless hand-written profiles put that exact directory on PATH,
// and a dry run against a real machine flagged a line Conduit had never
// written. Deleting it would silently remove other tools from the user's PATH,
// and the failure would surface much later as "command not found" for something
// unrelated to Conduit.
//
// The cost of being strict is that a profile edited by hand, without the
// marker, is left alone. That is the right way round: a leftover PATH entry
// pointing at a directory that no longer exists is harmless, and a deleted one
// that other tools needed is not.
func stripShellConfig(path string, dryRun bool, result *UninstallResult) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // absent file is not an error
	}
	content := string(data)

	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); i++ {
		if isConduitPathMarker(lines[i]) {
			changed = true
			// The marker introduces exactly one line. Drop both, and drop the
			// marker alone if it happens to be the last line in the file.
			if i+1 < len(lines) {
				i++
			}
			continue
		}
		out = append(out, lines[i])
	}
	if !changed {
		return nil
	}

	if dryRun {
		result.ItemsRemoved = append(result.ItemsRemoved,
			fmt.Sprintf("[DRY RUN] Would clean PATH entry from %s", path))
		return nil
	}

	// Collapse the trailing blank lines the removal leaves behind, then end
	// the file with exactly one newline.
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	out = append(out, "")

	mode := os.FileMode(0644)
	if info, serr := os.Stat(path); serr == nil {
		mode = info.Mode().Perm()
	}

	// Copy aside before rewriting. Atomic replacement guarantees the file is
	// never truncated; it does not guarantee the edit was the one the user
	// wanted, and a wrong edit to .zshrc breaks their next login shell.
	backup, err := backupFile(path)
	if err != nil {
		return fmt.Errorf("failed to back up %s: %w", path, err)
	}

	if err := writeFileAtomic(path, []byte(strings.Join(out, "\n")), mode); err != nil {
		return fmt.Errorf("failed to clean %s: %w", path, err)
	}
	result.ItemsRemoved = append(result.ItemsRemoved,
		fmt.Sprintf("PATH entry: %s (backup: %s)", path, backup))
	return nil
}

// isConduitPathMarker reports whether a line is the comment an installer wrote
// above the PATH line it added.
//
// Anchored to the start of the line so that a user's own prose mentioning
// "# Conduit" mid-sentence does not trigger a two-line deletion.
func isConduitPathMarker(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, conduitPathMarker)
}
