package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// GeminiCLIAdapter implements the Adapter interface for Gemini CLI.
type GeminiCLIAdapter struct {
	baseAdapter
}

// NewGeminiCLIAdapter creates a new Gemini CLI adapter.
func NewGeminiCLIAdapter(db *sql.DB) *GeminiCLIAdapter {
	return &GeminiCLIAdapter{
		baseAdapter: baseAdapter{db: db},
	}
}

// ID returns the adapter identifier.
func (a *GeminiCLIAdapter) ID() string {
	return "gemini-cli"
}

// DisplayName returns the human-readable name.
func (a *GeminiCLIAdapter) DisplayName() string {
	return "Gemini CLI"
}

// Version returns the adapter version.
func (a *GeminiCLIAdapter) Version() string {
	return "1.0.0"
}

// Detect checks if Gemini CLI is installed and finds config locations.
func (a *GeminiCLIAdapter) Detect(ctx context.Context) (*DetectResult, error) {
	result := &DetectResult{
		ConfigRoots: []ConfigRoot{},
		Writable:    true,
	}

	// Check if gemini CLI exists
	path, err := exec.LookPath("gemini")
	if err != nil {
		result.Installed = false
		result.Notes = "Gemini CLI not found in PATH"
	} else {
		result.Installed = true

		// Get version
		cmd := exec.CommandContext(ctx, path, "--version")
		output, _ := cmd.Output()
		result.Version = strings.TrimSpace(string(output))
	}

	// User-level config (~/.gemini/settings.json)
	userConfig := filepath.Join(homeDir(), ".gemini", "settings.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   userConfig,
		Scope:  "user",
		Exists: fileExists(userConfig),
	})

	// Project-level config (.gemini/settings.json)
	cwd, _ := os.Getwd()
	projectConfig := filepath.Join(cwd, ".gemini", "settings.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   projectConfig,
		Scope:  "project",
		Exists: fileExists(projectConfig),
	})

	return result, nil
}

// PlanInjection creates an injection plan for Gemini CLI.
func (a *GeminiCLIAdapter) PlanInjection(ctx context.Context, req PlanRequest) (*InjectionPlan, error) {
	plan := &InjectionPlan{
		ChangeSetID: fmt.Sprintf("cs_%s_%s", time.Now().Format("20060102_150405"), uuid.New().String()[:8]),
		ClientID:    a.ID(),
		InstanceID:  req.InstanceID,
		Operations:  []InjectionOp{},
	}

	// Determine config path based on scope
	var configPath string
	switch req.Scope {
	case "project":
		if req.ProjectPath != "" {
			configPath = filepath.Join(req.ProjectPath, ".gemini", "settings.json")
		} else {
			cwd, _ := os.Getwd()
			configPath = filepath.Join(cwd, ".gemini", "settings.json")
		}
	default: // "user" is default for Gemini CLI
		configPath = filepath.Join(homeDir(), ".gemini", "settings.json")
	}

	if fileExists(configPath) {
		backupPath := filepath.Join(backupDir(plan.ChangeSetID), filepath.Base(configPath))

		plan.Operations = append(plan.Operations, InjectionOp{
			Type:       "backup_file",
			Path:       configPath,
			BackupPath: backupPath,
		})
		plan.Operations = append(plan.Operations, InjectionOp{
			Type: "update_file",
			Path: configPath,
		})
	} else {
		plan.Operations = append(plan.Operations, InjectionOp{
			Type: "create_file",
			Path: configPath,
		})
	}

	// Build server entry name
	serverName := fmt.Sprintf("conduit-%s", req.DisplayName)
	serverName = strings.ToLower(strings.ReplaceAll(serverName, " ", "-"))

	plan.ExpectedPostState = ExpectedState{
		MCPServers: []string{serverName},
		Transport:  "stdio",
	}

	return plan, nil
}

// ApplyInjection applies the injection plan.
// Gemini CLI supports both CLI commands and direct file manipulation.
func (a *GeminiCLIAdapter) ApplyInjection(ctx context.Context, plan *InjectionPlan) (*ApplyResult, error) {
	result := &ApplyResult{
		FilesChanged: []string{},
	}

	// Try using gemini CLI first (preferred for consistency)
	if a.tryGeminiCLI(ctx, plan) {
		result.Success = true
		result.ConfigScope = "user"
		return result, nil
	}

	// Fallback to direct file manipulation
	for _, op := range plan.Operations {
		switch op.Type {
		case "backup_file":
			if err := ensureDir(filepath.Dir(op.BackupPath)); err != nil {
				return nil, fmt.Errorf("create backup dir: %w", err)
			}
			if err := copyFile(op.Path, op.BackupPath); err != nil {
				return nil, fmt.Errorf("backup file: %w", err)
			}
			a.storeBackup(plan.ChangeSetID, a.ID(), op.Path, op.BackupPath, true)

		case "create_file", "update_file":
			var config map[string]interface{}
			if fileExists(op.Path) {
				data, _ := os.ReadFile(op.Path)
				json.Unmarshal(data, &config)
			}
			if config == nil {
				config = make(map[string]interface{})
			}

			// Gemini CLI uses "mcpServers" like Claude Code
			if config["mcpServers"] == nil {
				config["mcpServers"] = make(map[string]interface{})
			}
			servers, ok := config["mcpServers"].(map[string]interface{})
			if !ok {
				servers = make(map[string]interface{})
				config["mcpServers"] = servers
			}

			// Add Conduit server entry
			serverName := plan.ExpectedPostState.MCPServers[0]
			servers[serverName] = map[string]interface{}{
				"command": "conduit",
				"args":    []string{"mcp", "stdio", "--instance", plan.InstanceID},
				"env": map[string]string{
					"CONDUIT_SOCKET": filepath.Join(conduitDir(), "conduit.sock"),
				},
				"_managed_by":  "conduit",
				"_instance_id": plan.InstanceID,
			}

			// Create parent directory if needed
			if err := ensureDir(filepath.Dir(op.Path)); err != nil {
				return nil, fmt.Errorf("create config dir: %w", err)
			}

			data, _ := json.MarshalIndent(config, "", "  ")
			if err := os.WriteFile(op.Path, data, 0644); err != nil {
				return nil, fmt.Errorf("write config: %w", err)
			}

			result.ConfigPath = op.Path
			result.FilesChanged = append(result.FilesChanged, op.Path)
		}
	}

	result.Success = true
	result.ConfigScope = "user"
	return result, nil
}

// tryGeminiCLI attempts to use gemini mcp add command.
func (a *GeminiCLIAdapter) tryGeminiCLI(ctx context.Context, plan *InjectionPlan) bool {
	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		return false
	}

	serverName := plan.ExpectedPostState.MCPServers[0]
	args := []string{
		"mcp", "add", serverName,
		"--command", "conduit",
		"--args", strings.Join([]string{"mcp", "stdio", "--instance", plan.InstanceID}, ","),
	}

	cmd := exec.CommandContext(ctx, geminiPath, args...)
	if err := cmd.Run(); err != nil {
		return false
	}

	return true
}

// Validate validates a binding.
func (a *GeminiCLIAdapter) Validate(ctx context.Context, bindingID string) (*ValidationResult, error) {
	result := &ValidationResult{
		Observations: make(map[string]interface{}),
	}

	start := time.Now()

	binding := a.getBinding(bindingID)
	if binding == nil {
		result.Status = "fail"
		result.Errors = append(result.Errors, "Binding not found")
		return result, nil
	}

	// Try gemini mcp list command first
	if tools := a.tryGeminiMCPList(ctx); len(tools) > 0 {
		result.Status = "pass"
		result.ToolsFound = tools
		result.Observations["detected_via"] = "gemini_cli"
		result.LatencyMS = int(time.Since(start).Milliseconds())
		return result, nil
	}

	// Fallback to config file check
	if !fileExists(binding.ConfigPath) {
		result.Status = "fail"
		result.Errors = append(result.Errors, "Config file not found")
		return result, nil
	}

	data, err := os.ReadFile(binding.ConfigPath)
	if err != nil {
		result.Status = "fail"
		result.Errors = append(result.Errors, fmt.Sprintf("Cannot read config: %v", err))
		return result, nil
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		result.Status = "fail"
		result.Errors = append(result.Errors, fmt.Sprintf("Invalid JSON: %v", err))
		return result, nil
	}

	result.Observations["config_loaded"] = true

	servers, ok := config["mcpServers"].(map[string]interface{})
	if !ok {
		result.Status = "fail"
		result.Errors = append(result.Errors, "No mcpServers in config")
		return result, nil
	}

	for name := range servers {
		result.ToolsFound = append(result.ToolsFound, name)
	}

	result.Status = "pass"
	result.LatencyMS = int(time.Since(start).Milliseconds())
	return result, nil
}

// tryGeminiMCPList attempts to use gemini mcp list command.
func (a *GeminiCLIAdapter) tryGeminiMCPList(ctx context.Context) []string {
	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		return nil
	}

	cmd := exec.CommandContext(ctx, geminiPath, "mcp", "list", "--json")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result struct {
		Servers []string `json:"servers"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return nil
	}

	return result.Servers
}

// Rollback rolls back a change set.
func (a *GeminiCLIAdapter) Rollback(ctx context.Context, changeSetID string) (*RollbackResult, error) {
	result := &RollbackResult{
		FilesRestored: []string{},
	}

	backups := a.getBackups(changeSetID)

	for _, backup := range backups {
		if backup.FileExisted {
			if err := copyFile(backup.BackupPath, backup.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to restore %s: %v", backup.Path, err))
				continue
			}
		} else {
			if err := os.Remove(backup.Path); err != nil && !os.IsNotExist(err) {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to remove %s: %v", backup.Path, err))
				continue
			}
		}
		result.FilesRestored = append(result.FilesRestored, backup.Path)
	}

	result.Success = len(result.Errors) == 0
	return result, nil
}

// Doctor checks for issues with Gemini CLI configuration.
func (a *GeminiCLIAdapter) Doctor(ctx context.Context) ([]DoctorIssue, error) {
	issues := []DoctorIssue{}

	detect, _ := a.Detect(ctx)
	if !detect.Installed {
		issues = append(issues, DoctorIssue{
			Severity:    "error",
			Component:   "gemini-cli",
			Description: "Gemini CLI not installed",
			Suggestion:  "Install Gemini CLI: npm install -g @google/gemini-cli",
			AutoFix:     false,
		})
	}

	for _, root := range detect.ConfigRoots {
		if root.Exists {
			data, err := os.ReadFile(root.Path)
			if err != nil {
				issues = append(issues, DoctorIssue{
					Severity:    "warning",
					Component:   "gemini-cli",
					Description: fmt.Sprintf("Cannot read config: %s", root.Path),
					Suggestion:  "Check file permissions",
					AutoFix:     false,
				})
				continue
			}

			var config map[string]interface{}
			if err := json.Unmarshal(data, &config); err != nil {
				issues = append(issues, DoctorIssue{
					Severity:    "error",
					Component:   "gemini-cli",
					Description: fmt.Sprintf("Invalid JSON in config: %s", root.Path),
					Suggestion:  "Fix JSON syntax errors",
					AutoFix:     false,
				})
			}
		}
	}

	return issues, nil
}
