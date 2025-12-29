package adapters

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CursorAdapter implements the Adapter interface for Cursor.
type CursorAdapter struct {
	baseAdapter
}

// NewCursorAdapter creates a new Cursor adapter.
func NewCursorAdapter(db *sql.DB) *CursorAdapter {
	return &CursorAdapter{
		baseAdapter: baseAdapter{db: db},
	}
}

// ID returns the adapter identifier.
func (a *CursorAdapter) ID() string {
	return "cursor"
}

// DisplayName returns the human-readable name.
func (a *CursorAdapter) DisplayName() string {
	return "Cursor"
}

// Version returns the adapter version.
func (a *CursorAdapter) Version() string {
	return "1.0.0"
}

// Detect checks if Cursor is installed and finds config locations.
func (a *CursorAdapter) Detect(ctx context.Context) (*DetectResult, error) {
	result := &DetectResult{
		ConfigRoots: []ConfigRoot{},
		Writable:    true,
	}

	// Check Cursor app installation
	var appPath string
	switch runtime.GOOS {
	case "darwin":
		appPath = "/Applications/Cursor.app"
	case "windows":
		appPath = filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "cursor", "Cursor.exe")
	case "linux":
		// Try common locations
		candidates := []string{
			"/usr/bin/cursor",
			"/usr/local/bin/cursor",
			filepath.Join(homeDir(), ".local/bin/cursor"),
		}
		for _, c := range candidates {
			if fileExists(c) {
				appPath = c
				break
			}
		}
		if appPath == "" {
			appPath = "/usr/bin/cursor"
		}
	}

	if fileExists(appPath) || dirExists(appPath) {
		result.Installed = true
	} else {
		result.Notes = "Cursor application not found"
	}

	// Global config (~/.cursor/mcp.json)
	globalConfig := filepath.Join(homeDir(), ".cursor", "mcp.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   globalConfig,
		Scope:  "user",
		Exists: fileExists(globalConfig),
	})

	// Project config (.cursor/mcp.json)
	cwd, _ := os.Getwd()
	projectConfig := filepath.Join(cwd, ".cursor", "mcp.json")
	result.ConfigRoots = append(result.ConfigRoots, ConfigRoot{
		Path:   projectConfig,
		Scope:  "project",
		Exists: fileExists(projectConfig),
	})

	return result, nil
}

// PlanInjection creates an injection plan for Cursor.
func (a *CursorAdapter) PlanInjection(ctx context.Context, req PlanRequest) (*InjectionPlan, error) {
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
			configPath = filepath.Join(req.ProjectPath, ".cursor", "mcp.json")
		} else {
			cwd, _ := os.Getwd()
			configPath = filepath.Join(cwd, ".cursor", "mcp.json")
		}
	default: // "user" is default for Cursor
		configPath = filepath.Join(homeDir(), ".cursor", "mcp.json")
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
func (a *CursorAdapter) ApplyInjection(ctx context.Context, plan *InjectionPlan) (*ApplyResult, error) {
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

			// Ensure mcpServers exists (Cursor uses same format as Claude Code)
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

// Validate validates a binding.
func (a *CursorAdapter) Validate(ctx context.Context, bindingID string) (*ValidationResult, error) {
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
func (a *CursorAdapter) Rollback(ctx context.Context, changeSetID string) (*RollbackResult, error) {
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

// Doctor checks for issues with Cursor configuration.
func (a *CursorAdapter) Doctor(ctx context.Context) ([]DoctorIssue, error) {
	issues := []DoctorIssue{}

	detect, _ := a.Detect(ctx)
	if !detect.Installed {
		issues = append(issues, DoctorIssue{
			Severity:    "error",
			Component:   "cursor",
			Description: "Cursor application not installed",
			Suggestion:  "Download Cursor from https://cursor.sh",
			AutoFix:     false,
		})
	}

	for _, root := range detect.ConfigRoots {
		if root.Exists {
			data, err := os.ReadFile(root.Path)
			if err != nil {
				issues = append(issues, DoctorIssue{
					Severity:    "warning",
					Component:   "cursor",
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
					Component:   "cursor",
					Description: fmt.Sprintf("Invalid JSON in config: %s", root.Path),
					Suggestion:  "Fix JSON syntax errors",
					AutoFix:     false,
				})
			}
		}
	}

	return issues, nil
}
