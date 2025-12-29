// Package models contains shared data structures used across Conduit modules.
package models

import "time"

// InstanceStatus represents the lifecycle state of a connector instance.
type InstanceStatus string

const (
	StatusCreated    InstanceStatus = "CREATED"
	StatusAuditing   InstanceStatus = "AUDITING"
	StatusBlocked    InstanceStatus = "BLOCKED"
	StatusInstalled  InstanceStatus = "INSTALLED"
	StatusStarting   InstanceStatus = "STARTING"
	StatusRunning    InstanceStatus = "RUNNING"
	StatusDegraded   InstanceStatus = "DEGRADED"
	StatusStopping   InstanceStatus = "STOPPING"
	StatusStopped    InstanceStatus = "STOPPED"
	StatusRemoving   InstanceStatus = "REMOVING"
	StatusRemoved    InstanceStatus = "REMOVED"
	StatusFailed     InstanceStatus = "FAILED"
	StatusUpdating   InstanceStatus = "UPDATING"
	StatusRestarting InstanceStatus = "RESTARTING"
)

// ConnectorInstance represents a running or installed connector.
type ConnectorInstance struct {
	InstanceID      string            `json:"instance_id"`
	PackageID       string            `json:"package_id"`
	PackageVersion  string            `json:"package_version"`
	DisplayName     string            `json:"display_name"`
	Status          InstanceStatus    `json:"status"`
	ContainerID     string            `json:"container_id,omitempty"`
	SocketPath      string            `json:"socket_path,omitempty"`
	ImageRef        string            `json:"image_ref"`
	Config          map[string]string `json:"config,omitempty"`
	GrantedPerms    *PermissionSet    `json:"granted_perms,omitempty"`
	AuditResult     *AuditResult      `json:"audit_result,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	StoppedAt       *time.Time        `json:"stopped_at,omitempty"`
	LastHealthCheck *time.Time        `json:"last_health_check,omitempty"`
	HealthStatus    string            `json:"health_status,omitempty"`
	ErrorMessage    string            `json:"error_message,omitempty"`
}

// ConnectorPackage represents a connector package definition.
type ConnectorPackage struct {
	SchemaVersion    string          `json:"schema_version"`
	PackageID        string          `json:"package_id"`
	Version          string          `json:"version"`
	MinConduitVersion string         `json:"min_conduit_version,omitempty"`
	Metadata         PackageMetadata `json:"metadata"`
	Distribution     Distribution    `json:"distribution"`
	Permissions      Permissions     `json:"permissions"`
	MCP              MCPConfig       `json:"mcp"`
	Config           *ConfigSchema   `json:"config,omitempty"`
	Audit            *AuditConfig    `json:"audit,omitempty"`
}

// PackageMetadata contains connector metadata for display and discovery.
type PackageMetadata struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	License     string   `json:"license"`
	Homepage    string   `json:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	Icon        string   `json:"icon,omitempty"`
}

// Distribution defines how the connector is packaged and run.
type Distribution struct {
	Type         string   `json:"type"` // "container" or "binary"
	Image        string   `json:"image,omitempty"`
	Tag          string   `json:"tag,omitempty"`
	Digest       string   `json:"digest,omitempty"`
	Entrypoint   []string `json:"entrypoint,omitempty"`
	Command      []string `json:"command,omitempty"`
	WorkingDir   string   `json:"working_dir,omitempty"`
	Platforms    []string `json:"platforms,omitempty"`
}

// Permissions defines what resources the connector needs access to.
type Permissions struct {
	Filesystem  FilesystemPerms  `json:"filesystem,omitempty"`
	Network     NetworkPerms     `json:"network,omitempty"`
	Environment EnvironmentPerms `json:"environment,omitempty"`
	Secrets     SecretsPerms     `json:"secrets,omitempty"`
}

// FilesystemPerms defines filesystem access permissions.
type FilesystemPerms struct {
	ReadonlyPaths  []string `json:"readonly_paths,omitempty"`
	WritablePaths  []string `json:"writable_paths,omitempty"`
	TempDir        bool     `json:"temp_dir,omitempty"`
}

// NetworkPerms defines network access permissions.
type NetworkPerms struct {
	Mode       string   `json:"mode"` // "none", "egress", "ingress", "full"
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
	AllowedPorts []int    `json:"allowed_ports,omitempty"`
}

// EnvironmentPerms defines environment variable access.
type EnvironmentPerms struct {
	Passthrough []string `json:"passthrough,omitempty"`
	Inject      []string `json:"inject,omitempty"`
}

// SecretsPerms defines secrets the connector needs.
type SecretsPerms struct {
	Required []SecretRef `json:"required,omitempty"`
	Optional []SecretRef `json:"optional,omitempty"`
}

// SecretRef references a secret by name.
type SecretRef struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	EnvVar      string `json:"env_var,omitempty"`
}

// MCPConfig defines MCP server configuration.
type MCPConfig struct {
	Transport   string   `json:"transport"` // "stdio" or "sse"
	Entrypoint  string   `json:"entrypoint,omitempty"`
	HealthCheck string   `json:"health_check,omitempty"`
	Tools       []string `json:"tools,omitempty"`
	Resources   []string `json:"resources,omitempty"`
}

// ConfigSchema defines user-configurable options.
type ConfigSchema struct {
	Properties map[string]ConfigProperty `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// ConfigProperty defines a single configuration property.
type ConfigProperty struct {
	Type        string      `json:"type"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Secret      bool        `json:"secret,omitempty"`
}

// AuditConfig defines audit requirements.
type AuditConfig struct {
	MinScore   int      `json:"min_score,omitempty"`
	Required   []string `json:"required,omitempty"`
}

// PermissionSet represents a granted set of permissions.
type PermissionSet struct {
	Filesystem  *FilesystemPerms  `json:"filesystem,omitempty"`
	Network     *NetworkPerms     `json:"network,omitempty"`
	Environment *EnvironmentPerms `json:"environment,omitempty"`
	Secrets     []string          `json:"secrets,omitempty"`
}

// AuditResult contains the result of a security audit.
type AuditResult struct {
	Status      AuditStatus   `json:"status"`
	Score       int           `json:"score"`
	Checks      []AuditCheck  `json:"checks"`
	PerformedAt time.Time     `json:"performed_at"`
	Version     string        `json:"version"`
}

// AuditStatus represents the overall audit outcome.
type AuditStatus string

const (
	AuditPass  AuditStatus = "PASS"
	AuditWarn  AuditStatus = "WARN"
	AuditBlock AuditStatus = "BLOCK"
)

// AuditCheck represents a single audit check result.
type AuditCheck struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Status      AuditStatus  `json:"status"`
	Message     string       `json:"message,omitempty"`
	Severity    string       `json:"severity"`
	Details     string       `json:"details,omitempty"`
}

// ValidTransitions defines valid state transitions for connector instances.
var ValidTransitions = map[InstanceStatus][]InstanceStatus{
	StatusCreated:    {StatusAuditing, StatusRemoved},
	StatusAuditing:   {StatusInstalled, StatusBlocked, StatusFailed},
	StatusBlocked:    {StatusRemoved},
	StatusInstalled:  {StatusStarting, StatusRemoving},
	StatusStarting:   {StatusRunning, StatusFailed, StatusStopped},
	StatusRunning:    {StatusDegraded, StatusStopping, StatusFailed, StatusRestarting},
	StatusDegraded:   {StatusRunning, StatusStopping, StatusFailed},
	StatusStopping:   {StatusStopped, StatusFailed},
	StatusStopped:    {StatusStarting, StatusRemoving, StatusUpdating},
	StatusRemoving:   {StatusRemoved, StatusFailed},
	StatusRemoved:    {},
	StatusFailed:     {StatusStarting, StatusRemoving},
	StatusUpdating:   {StatusInstalled, StatusFailed},
	StatusRestarting: {StatusRunning, StatusFailed},
}

// IsValidTransition checks if a state transition is allowed.
func IsValidTransition(from, to InstanceStatus) bool {
	allowed, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}
