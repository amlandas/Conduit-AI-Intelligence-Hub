// Package adapters provides client adapter implementations for AI clients.
package adapters

import (
	"context"
)

// Adapter is the interface all client adapters must implement.
type Adapter interface {
	// Identity
	ID() string
	DisplayName() string
	Version() string

	// Detection
	Detect(ctx context.Context) (*DetectResult, error)

	// Planning
	PlanInjection(ctx context.Context, req PlanRequest) (*InjectionPlan, error)

	// Execution
	ApplyInjection(ctx context.Context, plan *InjectionPlan) (*ApplyResult, error)
	Validate(ctx context.Context, bindingID string) (*ValidationResult, error)
	Rollback(ctx context.Context, changeSetID string) (*RollbackResult, error)

	// Doctor
	Doctor(ctx context.Context) ([]DoctorIssue, error)
}

// Registry manages all available adapters.
type Registry interface {
	Register(adapter Adapter)
	Get(clientID string) (Adapter, error)
	List() []Adapter
	DetectAll(ctx context.Context) (map[string]*DetectResult, error)
}

// DetectResult contains client detection information.
type DetectResult struct {
	Installed   bool         `json:"installed"`
	Version     string       `json:"version,omitempty"`
	ConfigRoots []ConfigRoot `json:"config_roots"`
	Writable    bool         `json:"writable"`
	Notes       string       `json:"notes,omitempty"`
}

// ConfigRoot represents a configuration file location.
type ConfigRoot struct {
	Path   string `json:"path"`
	Scope  string `json:"scope"` // "project", "user", "workspace"
	Exists bool   `json:"exists"`
}

// PlanRequest contains information needed to plan an injection.
type PlanRequest struct {
	InstanceID  string            `json:"instance_id"`
	DisplayName string            `json:"display_name"`
	Command     []string          `json:"command,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Scope       string            `json:"scope"`       // "project", "user", "workspace"
	ProjectPath string            `json:"project_path,omitempty"`
}

// InjectionPlan describes exactly what will change.
type InjectionPlan struct {
	ChangeSetID       string        `json:"change_set_id"`
	ClientID          string        `json:"client_id"`
	InstanceID        string        `json:"instance_id"`
	Operations        []InjectionOp `json:"operations"`
	ExpectedPostState ExpectedState `json:"expected_post_state"`
}

// InjectionOp describes a single operation in the injection plan.
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

// ApplyResult contains the result of applying an injection.
type ApplyResult struct {
	Success      bool     `json:"success"`
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

// RollbackResult contains the result of a rollback.
type RollbackResult struct {
	Success       bool     `json:"success"`
	FilesRestored []string `json:"files_restored"`
	Errors        []string `json:"errors"`
}

// DoctorIssue represents a detected issue.
type DoctorIssue struct {
	Severity    string `json:"severity"` // "error", "warning", "info"
	Component   string `json:"component"`
	Description string `json:"description"`
	Suggestion  string `json:"suggestion"`
	AutoFix     bool   `json:"auto_fix"`
}

// BackupRecord stores information about a backup for rollback.
type BackupRecord struct {
	ChangeSetID     string `json:"change_set_id"`
	ClientID        string `json:"client_id"`
	Path            string `json:"path"`
	BackupPath      string `json:"backup_path"`
	OriginalContent string `json:"original_content,omitempty"`
	FileExisted     bool   `json:"file_existed"`
}

// BindingInfo contains binding information for validation.
type BindingInfo struct {
	BindingID  string `json:"binding_id"`
	InstanceID string `json:"instance_id"`
	ClientID   string `json:"client_id"`
	ConfigPath string `json:"config_path"`
	Scope      string `json:"scope"`
}
