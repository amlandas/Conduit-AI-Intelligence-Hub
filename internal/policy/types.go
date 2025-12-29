// Package policy implements the Conduit policy engine for security decisions.
package policy

import (
	"time"
)

// Scope defines the context of a permission request.
type Scope string

const (
	ScopeInstall          Scope = "install"
	ScopePermissionChange Scope = "permission_change"
	ScopeSecureLink       Scope = "secure_link"
	ScopeSecretBind       Scope = "secret_bind"
)

// DecisionType represents the outcome of policy evaluation.
type DecisionType string

const (
	Allow DecisionType = "ALLOW"
	Warn  DecisionType = "WARN"
	Deny  DecisionType = "DENY"
)

// Request represents a permission request to be evaluated.
type Request struct {
	Scope      Scope         `json:"scope"`
	InstanceID string        `json:"instance_id,omitempty"`
	PackageID  string        `json:"package_id,omitempty"`
	Actor      string        `json:"actor"` // "user" or "system"
	Requested  PermissionSet `json:"requested"`
}

// Decision is the result of policy evaluation.
type Decision struct {
	DecisionID   string        `json:"decision_id"`
	Decision     DecisionType  `json:"decision"`
	Reason       string        `json:"reason"`
	Requested    PermissionSet `json:"requested_permissions"`
	Effective    PermissionSet `json:"effective_permissions"`
	Warnings     []string      `json:"warnings,omitempty"`
	BlockReasons []string      `json:"block_reasons,omitempty"`
	Timestamp    time.Time     `json:"timestamp"`
	Actor        string        `json:"actor"`
}

// PermissionSet represents all permission categories.
type PermissionSet struct {
	Filesystem FilesystemPerms `json:"filesystem"`
	Network    NetworkPerms    `json:"network"`
	Secrets    []SecretRef     `json:"secrets"`
	Exposure   ExposurePerms   `json:"exposure"`
}

// FilesystemPerms defines filesystem access permissions.
type FilesystemPerms struct {
	ReadonlyPaths  []string `json:"readonly_paths"`
	ReadwritePaths []string `json:"readwrite_paths"`
}

// NetworkPerms defines network access permissions.
type NetworkPerms struct {
	Mode          string   `json:"mode"` // "none", "egress"
	EgressDomains []string `json:"egress_domains,omitempty"`
}

// SecretRef references a secret to be bound.
type SecretRef struct {
	SecretID string `json:"secret_id"`
	EnvKey   string `json:"env_key"`
}

// ExposurePerms defines exposure permissions.
type ExposurePerms struct {
	SecureLink bool `json:"secure_link"`
}

// IsEmpty returns true if no permissions are requested.
func (p PermissionSet) IsEmpty() bool {
	return len(p.Filesystem.ReadonlyPaths) == 0 &&
		len(p.Filesystem.ReadwritePaths) == 0 &&
		p.Network.Mode == "" || p.Network.Mode == "none" &&
		len(p.Secrets) == 0 &&
		!p.Exposure.SecureLink
}

// Merge combines two permission sets (union).
func (p PermissionSet) Merge(other PermissionSet) PermissionSet {
	result := PermissionSet{
		Filesystem: FilesystemPerms{
			ReadonlyPaths:  mergeStringSlices(p.Filesystem.ReadonlyPaths, other.Filesystem.ReadonlyPaths),
			ReadwritePaths: mergeStringSlices(p.Filesystem.ReadwritePaths, other.Filesystem.ReadwritePaths),
		},
		Network: p.Network,
		Secrets: mergeSecretRefs(p.Secrets, other.Secrets),
		Exposure: ExposurePerms{
			SecureLink: p.Exposure.SecureLink || other.Exposure.SecureLink,
		},
	}

	// Network: take the more permissive mode
	if other.Network.Mode == "egress" {
		result.Network.Mode = "egress"
		result.Network.EgressDomains = mergeStringSlices(p.Network.EgressDomains, other.Network.EgressDomains)
	}

	return result
}

// mergeStringSlices merges two string slices, removing duplicates.
func mergeStringSlices(a, b []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(a)+len(b))

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// mergeSecretRefs merges two secret ref slices, removing duplicates by SecretID.
func mergeSecretRefs(a, b []SecretRef) []SecretRef {
	seen := make(map[string]bool)
	result := make([]SecretRef, 0, len(a)+len(b))

	for _, s := range a {
		if !seen[s.SecretID] {
			seen[s.SecretID] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s.SecretID] {
			seen[s.SecretID] = true
			result = append(result, s)
		}
	}

	return result
}

// Rule defines a policy rule.
type Rule struct {
	ID        string
	Priority  int // 0 = highest (blocklist), higher = lower priority
	Condition func(Request) bool
	Decision  DecisionType
	Reason    string
}

// Matches checks if the rule matches the request.
func (r Rule) Matches(req Request) bool {
	if r.Condition == nil {
		return false
	}
	return r.Condition(req)
}
