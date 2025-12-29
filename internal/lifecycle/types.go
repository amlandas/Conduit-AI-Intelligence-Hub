// Package lifecycle manages connector instance lifecycle and state transitions.
package lifecycle

import (
	"time"
)

// InstanceStatus represents the status of a connector instance.
type InstanceStatus string

const (
	StatusCreated   InstanceStatus = "CREATED"
	StatusAuditing  InstanceStatus = "AUDITING"
	StatusBlocked   InstanceStatus = "BLOCKED"
	StatusInstalled InstanceStatus = "INSTALLED"
	StatusStarting  InstanceStatus = "STARTING"
	StatusRunning   InstanceStatus = "RUNNING"
	StatusDegraded  InstanceStatus = "DEGRADED"
	StatusStopping  InstanceStatus = "STOPPING"
	StatusStopped   InstanceStatus = "STOPPED"
	StatusUpdating  InstanceStatus = "UPDATING"
	StatusDisabled  InstanceStatus = "DISABLED"
	StatusRemoving  InstanceStatus = "REMOVING"
	StatusRemoved   InstanceStatus = "REMOVED"
)

// BindingStatus represents the status of a client binding.
type BindingStatus string

const (
	BindingUnbound   BindingStatus = "UNBOUND"
	BindingBinding   BindingStatus = "BINDING"
	BindingBound     BindingStatus = "BOUND"
	BindingDegraded  BindingStatus = "DEGRADED"
	BindingUnbinding BindingStatus = "UNBINDING"
	BindingFailed    BindingStatus = "FAILED"
)

// ValidTransitions defines the allowed state transitions.
var ValidTransitions = map[InstanceStatus][]InstanceStatus{
	StatusCreated:   {StatusAuditing},
	StatusAuditing:  {StatusBlocked, StatusInstalled},
	StatusBlocked:   {StatusRemoving}, // Can only be removed
	StatusInstalled: {StatusStarting, StatusDisabled, StatusRemoving, StatusUpdating},
	StatusStarting:  {StatusRunning, StatusDegraded, StatusStopped},
	StatusRunning:   {StatusDegraded, StatusStopping, StatusDisabled},
	StatusDegraded:  {StatusRunning, StatusStopping, StatusDisabled},
	StatusStopping:  {StatusStopped},
	StatusStopped:   {StatusStarting, StatusRemoving, StatusUpdating},
	StatusUpdating:  {StatusInstalled, StatusBlocked},
	StatusDisabled:  {StatusInstalled, StatusRemoving},
	StatusRemoving:  {StatusRemoved},
	StatusRemoved:   {}, // Terminal state
}

// IsValidTransition checks if a state transition is allowed.
func IsValidTransition(from, to InstanceStatus) bool {
	allowed, exists := ValidTransitions[from]
	if !exists {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// CreateInstanceRequest contains parameters for creating an instance.
type CreateInstanceRequest struct {
	PackageID      string            `json:"package_id"`
	Version        string            `json:"version"`
	DisplayName    string            `json:"display_name"`
	ImageRef       string            `json:"image_ref"`
	Config         map[string]string `json:"config,omitempty"`
}

// Instance represents a connector instance with full details.
type Instance struct {
	InstanceID      string         `json:"instance_id"`
	PackageID       string         `json:"package_id"`
	PackageVersion  string         `json:"package_version"`
	DisplayName     string         `json:"display_name"`
	ImageRef        string         `json:"image_ref"`
	Status          InstanceStatus `json:"status"`
	ContainerID     string         `json:"container_id,omitempty"`
	SocketPath      string         `json:"socket_path,omitempty"`
	RuntimeProvider string         `json:"runtime_provider,omitempty"`
	Config          map[string]string `json:"config,omitempty"`
	Health          *HealthStatus  `json:"health,omitempty"`
	Bindings        []*Binding     `json:"bindings,omitempty"`
	ErrorMessage    string         `json:"error_message,omitempty"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	StartedAt       *time.Time     `json:"started_at,omitempty"`
	StoppedAt       *time.Time     `json:"stopped_at,omitempty"`
}

// Binding represents a client binding to an instance.
type Binding struct {
	BindingID      string         `json:"binding_id"`
	InstanceID     string         `json:"instance_id"`
	ClientID       string         `json:"client_id"`
	AdapterVersion string         `json:"adapter_version,omitempty"`
	ChangeSetID    string         `json:"change_set_id"`
	Scope          string         `json:"scope"`
	ConfigPath     string         `json:"config_path"`
	Status         BindingStatus  `json:"status"`
	LastValidation *ValidationResult `json:"last_validation,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// BindingOptions configures binding creation.
type BindingOptions struct {
	Scope string `json:"scope"` // "project", "user", "workspace"
}

// InjectionPlan describes what will be injected into a client config.
type InjectionPlan struct {
	PlanID      string            `json:"plan_id"`
	InstanceID  string            `json:"instance_id"`
	ClientID    string            `json:"client_id"`
	ConfigPath  string            `json:"config_path"`
	ConfigScope string            `json:"config_scope"`
	ChangeSetID string            `json:"change_set_id"`
	Additions   []InjectionEntry  `json:"additions"`
	Modifications []InjectionEntry `json:"modifications,omitempty"`
}

// InjectionEntry represents a single config entry to inject.
type InjectionEntry struct {
	ServerName  string            `json:"server_name"`
	Transport   string            `json:"transport"`
	Command     []string          `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
}

// ValidationResult contains binding validation details.
type ValidationResult struct {
	Status    string    `json:"status"` // "pass", "fail", "warn"
	Checks    []Check   `json:"checks"`
	Timestamp time.Time `json:"timestamp"`
}

// Check represents a single validation check.
type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "pass", "fail", "skip"
	Message string `json:"message,omitempty"`
}

// HealthStatus represents instance health.
type HealthStatus struct {
	Status      string    `json:"status"` // "healthy", "unhealthy", "unknown"
	LastCheck   time.Time `json:"last_check"`
	Message     string    `json:"message,omitempty"`
	Consecutive int       `json:"consecutive"` // Consecutive failures/successes
}

// Operation tracks a long-running operation.
type Operation struct {
	OperationID   string      `json:"operation_id"`
	Type          string      `json:"type"` // "install", "update", "remove"
	InstanceID    string      `json:"instance_id"`
	Status        string      `json:"status"` // "pending", "running", "completed", "failed", "cancelled"
	CurrentStage  string      `json:"current_stage,omitempty"`
	Progress      int         `json:"progress"` // 0-100
	Error         string      `json:"error,omitempty"`
	Result        interface{} `json:"result,omitempty"`
	CreatedAt     time.Time   `json:"created_at"`
	CompletedAt   *time.Time  `json:"completed_at,omitempty"`
}

// ContainerSpec describes a container to be started.
// This mirrors the runtime.ContainerSpec but is defined here to avoid circular dependencies.
type ContainerSpec struct {
	Name        string
	Image       string
	Command     []string
	Entrypoint  []string
	Env         map[string]string
	Mounts      []Mount
	Ports       []Port
	Network     NetworkSpec
	Security    SecuritySpec
	Resources   ResourceSpec
	Labels      map[string]string
	WorkingDir  string
	Stdin       bool
}

// Mount defines a bind mount.
type Mount struct {
	Source   string
	Target   string
	ReadOnly bool
}

// Port defines a port mapping.
type Port struct {
	Host      int
	Container int
	Protocol  string
}

// NetworkSpec defines network configuration.
type NetworkSpec struct {
	Mode string // "none", "bridge", "host"
}

// SecuritySpec defines security options.
type SecuritySpec struct {
	ReadOnlyRootfs   bool
	NoNewPrivileges  bool
	DropCapabilities []string
	User             string
	SeccompProfile   string
}

// ResourceSpec defines resource limits.
type ResourceSpec struct {
	MemoryMB int64
	CPUs     float64
}
