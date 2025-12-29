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

// ClaudeCodeAdapter implements the Adapter interface for Claude Code.
type ClaudeCodeAdapter struct {
	baseAdapter
}

// NewClaudeCodeAdapter creates a new Claude Code adapter.
func NewClaudeCodeAdapter(db *sql.DB) *ClaudeCodeAdapter {
	return &ClaudeCodeAdapter{
		baseAdapter: baseAdapter{db: db},
	}
}

// ID returns the adapter identifier.
func (a *ClaudeCodeAdapter) ID() string {
	return "claude-code"
}

// DisplayName returns the human-readable name.
func (a *ClaudeCodeAdapter) DisplayName() string {
	return "Claude Code"
}

// Version returns the adapter version.
func (a *ClaudeCodeAdapter) Version() string {
	return "1.0.0"
}

// Detect checks if Claude Code is installed and finds config locations.
func (a *ClaudeCodeAdapter) Detect(ctx context.Context) (*DetectResult, error) {
	result := &DetectResult{
		ConfigRoots: []ConfigRoot{},
		Writable:    true,
	}

	// Check if claude CLI exists
	path, err := exec.LookPath("claude")
	if err != nil {
		result.Installed = false
		result.Notes = "Claude Code CLI not found in PATH"
	} else {
		result.Installed = true

		// Get version
		cmd := exec.CommandContext(ctx, path, "--version")
		output, _ := cmd.Output()
		result.Version = strings.TrimSpace(string(output))
	}

	// User-level config
	userConfig := filepath.Join(homeDir(), ".claude.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   userConfig,
		Scope:  "user",
		Exists: fileExists(userConfig),
	})

	// Project-level config (check CWD)
	cwd, _ := os.Getwd()
	projectConfig := filepath.Join(cwd, ".mcp.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   projectConfig,
		Scope:  "project",
		Exists: fileExists(projectConfig),
	})

	return result, nil
}

// PlanInjection creates an injection plan for Claude Code.
func (a *ClaudeCodeAdapter) PlanInjection(ctx context.Context, req PlanRequest) (*InjectionPlan, error) {
	plan := &InjectionPlan{
		ChangeSetID: fmt.Sprintf("cs_%s_%s", time.Now().Format("20060102_150405"), uuid.New().String()[:8]),
		ClientID:    a.ID(),
		InstanceID:  req.InstanceID,
		Operations:  []InjectionOp{},
	}

	// Determine config path based on scope
	var configPath string
	switch req.Scope {
	case "user":
		configPath = filepath.Join(homeDir(), ".claude.json")
	case "project":
		if req.ProjectPath != "" {
			configPath = filepath.Join(req.ProjectPath, ".mcp.json")
		} else {
			cwd, _ := os.Getwd()
			configPath = filepath.Join(cwd, ".mcp.json")
		}
	default:
		if req.ProjectPath != "" {
			configPath = filepath.Join(req.ProjectPath, ".mcp.json")
		} else {
			cwd, _ := os.Getwd()
			configPath = filepath.Join(cwd, ".mcp.json")
		}
	}

	// Check if file exists (for backup)
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
func (a *ClaudeCodeAdapter) ApplyInjection(ctx context.Context, plan *InjectionPlan) (*ApplyResult, error) {
	result := &ApplyResult{
		FilesChanged: []string{},
	}

	for _, op := range plan.Operations {
		switch op.Type {
		case "backup_file":
			// Create backup directory
			if err := ensureDir(filepath.Dir(op.BackupPath)); err != nil {
				return nil, fmt.Errorf("create backup dir: %w", err)
			}
			// Copy file
			if err := copyFile(op.Path, op.BackupPath); err != nil {
				return nil, fmt.Errorf("backup file: %w", err)
			}
			// Store backup record in DB
			a.storeBackup(plan.ChangeSetID, a.ID(), op.Path, op.BackupPath, true)

		case "create_file", "update_file":
			// Read existing config or create new
			var config map[string]interface{}
			if fileExists(op.Path) {
				data, _ := os.ReadFile(op.Path)
				json.Unmarshal(data, &config)
			}
			if config == nil {
				config = make(map[string]interface{})
			}

			// Ensure mcpServers exists
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

			// Write config
			data, _ := json.MarshalIndent(config, "", "  ")
			if err := os.WriteFile(op.Path, data, 0644); err != nil {
				return nil, fmt.Errorf("write config: %w", err)
			}

			result.ConfigPath = op.Path
			result.FilesChanged = append(result.FilesChanged, op.Path)
		}
	}

	result.Success = true
	result.ConfigScope = "project" // Or derive from plan
	return result, nil
}

// Validate validates a binding.
func (a *ClaudeCodeAdapter) Validate(ctx context.Context, bindingID string) (*ValidationResult, error) {
	result := &ValidationResult{
		Observations: make(map[string]interface{}),
	}

	start := time.Now()

	// Get binding info from DB
	binding := a.getBinding(bindingID)
	if binding == nil {
		result.Status = "fail"
		result.Errors = append(result.Errors, "Binding not found")
		return result, nil
	}

	// Check config file exists and parses
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

	// Check Conduit entry exists
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

// Rollback rolls back a change set.
func (a *ClaudeCodeAdapter) Rollback(ctx context.Context, changeSetID string) (*RollbackResult, error) {
	result := &RollbackResult{
		FilesRestored: []string{},
	}

	// Get backups for this change set
	backups := a.getBackups(changeSetID)

	for _, backup := range backups {
		if backup.FileExisted {
			// Restore from backup
			if err := copyFile(backup.BackupPath, backup.Path); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("Failed to restore %s: %v", backup.Path, err))
				continue
			}
		} else {
			// File didn't exist before, delete it
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

// Doctor checks for issues with Claude Code configuration.
func (a *ClaudeCodeAdapter) Doctor(ctx context.Context) ([]DoctorIssue, error) {
	issues := []DoctorIssue{}

	// Check CLI installation
	detect, _ := a.Detect(ctx)
	if !detect.Installed {
		issues = append(issues, DoctorIssue{
			Severity:    "error",
			Component:   "claude-code",
			Description: "Claude Code CLI not installed",
			Suggestion:  "Install Claude Code: npm install -g @anthropic-ai/claude-code",
			AutoFix:     false,
		})
	}

	// Check config file validity
	for _, root := range detect.ConfigRoots {
		if root.Exists {
			data, err := os.ReadFile(root.Path)
			if err != nil {
				issues = append(issues, DoctorIssue{
					Severity:    "warning",
					Component:   "claude-code",
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
					Component:   "claude-code",
					Description: fmt.Sprintf("Invalid JSON in config: %s", root.Path),
					Suggestion:  "Fix JSON syntax errors",
					AutoFix:     false,
				})
			}
		}
	}

	return issues, nil
}
