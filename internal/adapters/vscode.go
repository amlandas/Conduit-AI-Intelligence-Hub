package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// VSCodeAdapter implements the Adapter interface for VS Code.
type VSCodeAdapter struct {
	baseAdapter
}

// NewVSCodeAdapter creates a new VS Code adapter.
func NewVSCodeAdapter(db *sql.DB) *VSCodeAdapter {
	return &VSCodeAdapter{
		baseAdapter: baseAdapter{db: db},
	}
}

// ID returns the adapter identifier.
func (a *VSCodeAdapter) ID() string {
	return "vscode"
}

// DisplayName returns the human-readable name.
func (a *VSCodeAdapter) DisplayName() string {
	return "VS Code"
}

// Version returns the adapter version.
func (a *VSCodeAdapter) Version() string {
	return "1.0.0"
}

// Detect checks if VS Code is installed and finds config locations.
func (a *VSCodeAdapter) Detect(ctx context.Context) (*DetectResult, error) {
	result := &DetectResult{
		ConfigRoots: []ConfigRoot{},
		Writable:    true,
	}

	// Check if code CLI exists
	path, err := exec.LookPath("code")
	if err != nil {
		result.Installed = false
		result.Notes = "VS Code 'code' command not found in PATH"
	} else {
		result.Installed = true

		// Get version
		cmd := exec.CommandContext(ctx, path, "--version")
		output, _ := cmd.Output()
		lines := strings.Split(strings.TrimSpace(string(output)), "\n")
		if len(lines) > 0 {
			result.Version = lines[0]
		}
	}

	// User settings location varies by OS
	var userSettingsDir string
	switch runtime.GOOS {
	case "darwin":
		userSettingsDir = filepath.Join(homeDir(), "Library", "Application Support", "Code", "User")
	case "windows":
		userSettingsDir = filepath.Join(os.Getenv("APPDATA"), "Code", "User")
	case "linux":
		userSettingsDir = filepath.Join(homeDir(), ".config", "Code", "User")
	}

	// User-level MCP config (not typically used for VS Code MCP, but included for completeness)
	userMCPConfig := filepath.Join(userSettingsDir, "mcp.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   userMCPConfig,
		Scope:  "user",
		Exists: fileExists(userMCPConfig),
	})

	// Workspace config (.vscode/mcp.json)
	cwd, _ := os.Getwd()
	workspaceConfig := filepath.Join(cwd, ".vscode", "mcp.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   workspaceConfig,
		Scope:  "workspace",
		Exists: fileExists(workspaceConfig),
	})

	return result, nil
}

// PlanInjection creates an injection plan for VS Code.
func (a *VSCodeAdapter) PlanInjection(ctx context.Context, req PlanRequest) (*InjectionPlan, error) {
	plan := &InjectionPlan{
		ChangeSetID: fmt.Sprintf("cs_%s_%s", time.Now().Format("20060102_150405"), uuid.New().String()[:8]),
		ClientID:    a.ID(),
		InstanceID:  req.InstanceID,
		Operations:  []InjectionOp{},
	}

	// VS Code typically uses workspace scope
	var configPath string
	if req.ProjectPath != "" {
		configPath = filepath.Join(req.ProjectPath, ".vscode", "mcp.json")
	} else {
		cwd, _ := os.Getwd()
		configPath = filepath.Join(cwd, ".vscode", "mcp.json")
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
func (a *VSCodeAdapter) ApplyInjection(ctx context.Context, plan *InjectionPlan) (*ApplyResult, error) {
	result := &ApplyResult{
		FilesChanged: []string{},
	}

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

			// VS Code uses "servers" not "mcpServers"
			if config["servers"] == nil {
				config["servers"] = make(map[string]interface{})
			}
			servers, ok := config["servers"].(map[string]interface{})
			if !ok {
				servers = make(map[string]interface{})
				config["servers"] = servers
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

			// Create .vscode directory if needed
			if err := ensureDir(filepath.Dir(op.Path)); err != nil {
				return nil, fmt.Errorf("create .vscode dir: %w", err)
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
	result.ConfigScope = "workspace"
	return result, nil
}

// Validate validates a binding.
func (a *VSCodeAdapter) Validate(ctx context.Context, bindingID string) (*ValidationResult, error) {
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

	// VS Code uses "servers" key
	servers, ok := config["servers"].(map[string]interface{})
	if !ok {
		result.Status = "fail"
		result.Errors = append(result.Errors, "No servers in config")
		return result, nil
	}

	for name := range servers {
		result.ToolsFound = append(result.ToolsFound, name)
	}

	result.Status = "pass"
	result.LatencyMS = int(time.Since(start).Milliseconds())
	return result, nil
}

// Rollback rolls back a change set.
func (a *VSCodeAdapter) Rollback(ctx context.Context, changeSetID string) (*RollbackResult, error) {
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

// Doctor checks for issues with VS Code configuration.
func (a *VSCodeAdapter) Doctor(ctx context.Context) ([]DoctorIssue, error) {
	issues := []DoctorIssue{}

	detect, _ := a.Detect(ctx)
	if !detect.Installed {
		issues = append(issues, DoctorIssue{
			Severity:    "error",
			Component:   "vscode",
			Description: "VS Code 'code' command not installed",
			Suggestion:  "Install VS Code and run 'Shell Command: Install 'code' command in PATH'",
			AutoFix:     false,
		})
	}

	for _, root := range detect.ConfigRoots {
		if root.Exists {
			data, err := os.ReadFile(root.Path)
			if err != nil {
				issues = append(issues, DoctorIssue{
					Severity:    "warning",
					Component:   "vscode",
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
					Component:   "vscode",
					Description: fmt.Sprintf("Invalid JSON in config: %s", root.Path),
					Suggestion:  "Fix JSON syntax errors",
					AutoFix:     false,
				})
			}
		}
	}

	return issues, nil
}
