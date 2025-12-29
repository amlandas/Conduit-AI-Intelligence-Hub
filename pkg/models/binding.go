package models

import "time"

// ClientBinding represents a connection between a connector and an AI client.
type ClientBinding struct {
	BindingID     string    `json:"binding_id"`
	InstanceID    string    `json:"instance_id"`
	ClientID      string    `json:"client_id"`
	Scope         string    `json:"scope"` // "project", "user", "workspace"
	ConfigPath    string    `json:"config_path"`
	ChangeSetID   string    `json:"change_set_id"`
	Status        string    `json:"status"` // "active", "invalid", "removed"
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ValidatedAt   *time.Time `json:"validated_at,omitempty"`
}

// ConfigBackup stores backup information for rollback.
type ConfigBackup struct {
	BackupID      string    `json:"backup_id"`
	ChangeSetID   string    `json:"change_set_id"`
	ClientID      string    `json:"client_id"`
	OriginalPath  string    `json:"original_path"`
	BackupPath    string    `json:"backup_path"`
	CreatedAt     time.Time `json:"created_at"`
	FileExisted   bool      `json:"file_existed"`
}

// ClientInfo contains information about a detected AI client.
type ClientInfo struct {
	ClientID    string       `json:"client_id"`
	DisplayName string       `json:"display_name"`
	Installed   bool         `json:"installed"`
	Version     string       `json:"version,omitempty"`
	ConfigRoots []ConfigRoot `json:"config_roots"`
	Writable    bool         `json:"writable"`
	Notes       string       `json:"notes,omitempty"`
}

// ConfigRoot represents a configuration location for a client.
type ConfigRoot struct {
	Path   string `json:"path"`
	Scope  string `json:"scope"` // "project", "user", "workspace"
	Exists bool   `json:"exists"`
}

// InjectionPlan describes changes to be made to client configuration.
type InjectionPlan struct {
	ChangeSetID       string          `json:"change_set_id"`
	ClientID          string          `json:"client_id"`
	InstanceID        string          `json:"instance_id"`
	Operations        []InjectionOp   `json:"operations"`
	ExpectedPostState ExpectedState   `json:"expected_post_state"`
}

// InjectionOp represents a single configuration operation.
type InjectionOp struct {
	Type           string `json:"type"` // "create_file", "update_file", "backup_file"
	Path           string `json:"path"`
	BackupPath     string `json:"backup_path,omitempty"`
	ContentHash    string `json:"content_hash,omitempty"`
	ContentPreview string `json:"content_preview,omitempty"`
}

// ExpectedState describes the expected state after injection.
type ExpectedState struct {
	MCPServers []string `json:"mcp_servers"`
	Transport  string   `json:"transport"`
}

// ApplyResult contains the result of applying an injection plan.
type ApplyResult struct {
	Success      bool     `json:"success"`
	ChangeSetID  string   `json:"change_set_id"`
	ConfigPath   string   `json:"config_path"`
	ConfigScope  string   `json:"config_scope"`
	FilesChanged []string `json:"files_changed"`
	Error        string   `json:"error,omitempty"`
}

// ValidationResult contains the result of validating a binding.
type ValidationResult struct {
	Status       string                 `json:"status"` // "pass", "fail"
	LatencyMS    int                    `json:"latency_ms"`
	ToolTested   string                 `json:"tool_tested,omitempty"`
	ToolsFound   []string               `json:"tools_found"`
	Errors       []string               `json:"errors"`
	Observations map[string]interface{} `json:"observations"`
}

// RollbackResult contains the result of a rollback operation.
type RollbackResult struct {
	Success       bool     `json:"success"`
	FilesRestored []string `json:"files_restored"`
	Errors        []string `json:"errors"`
}

// DoctorIssue represents a detected issue during diagnostics.
type DoctorIssue struct {
	Severity    string `json:"severity"` // "error", "warning", "info"
	Component   string `json:"component"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
	AutoFix     bool   `json:"auto_fix"`
}
