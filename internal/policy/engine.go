package policy

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
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
)

// Engine evaluates permission requests against policy rules.
type Engine struct {
	db           *sql.DB
	logger       zerolog.Logger
	builtinRules []Rule
	homeDir      string
}

// New creates a new Policy Engine.
func New(db *sql.DB) *Engine {
	homeDir, _ := os.UserHomeDir()

	e := &Engine{
		db:      db,
		logger:  observability.Logger("policy"),
		homeDir: homeDir,
	}

	e.builtinRules = e.initBuiltinRules()

	return e
}

// Evaluate evaluates a permission request and returns a decision.
func (e *Engine) Evaluate(ctx context.Context, req Request) (*Decision, error) {
	decision := &Decision{
		DecisionID: uuid.New().String(),
		Requested:  req.Requested,
		Timestamp:  time.Now(),
		Actor:      req.Actor,
	}

	e.logger.Debug().
		Str("scope", string(req.Scope)).
		Str("instance_id", req.InstanceID).
		Str("package_id", req.PackageID).
		Msg("evaluating policy request")

	// Step 1: Check built-in blocklist rules (highest priority, priority=0)
	for _, rule := range e.builtinRules {
		if rule.Priority == 0 && rule.Matches(req) {
			if rule.Decision == Deny {
				decision.Decision = Deny
				decision.BlockReasons = append(decision.BlockReasons, rule.Reason)
				decision.Reason = rule.Reason
				e.recordDecision(ctx, decision)
				return decision, nil
			}
		}
	}

	// Step 2: Check for forbidden paths
	if violations := e.checkForbiddenPaths(req.Requested.Filesystem); len(violations) > 0 {
		decision.Decision = Deny
		decision.BlockReasons = violations
		decision.Reason = "Forbidden filesystem access requested"
		e.recordDecision(ctx, decision)
		return decision, nil
	}

	// Step 3: Get user grants for this instance (V0: no grants stored yet)
	var userGrants PermissionSet
	if req.InstanceID != "" {
		grants, _ := e.GetUserGrants(ctx, req.InstanceID)
		if grants != nil {
			userGrants = *grants
		}
	}

	// Step 4: Evaluate each permission category
	warnings := []string{}

	// Filesystem - apply user grants
	effectiveFS := e.evaluateFilesystem(req.Requested.Filesystem, userGrants.Filesystem, &warnings)

	// Network - check for egress and apply grants
	effectiveNet := e.evaluateNetwork(req.Requested.Network, userGrants.Network, &warnings)

	// Secrets - require explicit grants
	effectiveSecrets := e.evaluateSecrets(req.Requested.Secrets, userGrants.Secrets, &warnings)

	// Exposure - check secure link
	effectiveExposure := e.evaluateExposure(req.Requested.Exposure, userGrants.Exposure, &warnings)

	// Step 5: Determine final decision
	decision.Effective = PermissionSet{
		Filesystem: effectiveFS,
		Network:    effectiveNet,
		Secrets:    effectiveSecrets,
		Exposure:   effectiveExposure,
	}
	decision.Warnings = warnings

	if len(warnings) > 0 {
		decision.Decision = Warn
		decision.Reason = "Request allowed with warnings"
	} else {
		decision.Decision = Allow
		decision.Reason = "No policy violations"
	}

	// Step 6: Record decision
	e.recordDecision(ctx, decision)

	return decision, nil
}

// GrantPermission records user's explicit permission grant.
func (e *Engine) GrantPermission(ctx context.Context, instanceID string, perms PermissionSet) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Store filesystem permissions
	if len(perms.Filesystem.ReadonlyPaths) > 0 || len(perms.Filesystem.ReadwritePaths) > 0 {
		data, _ := json.Marshal(perms.Filesystem)
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO user_grants (instance_id, permission_type, grant_data, granted_at)
			VALUES (?, 'filesystem', ?, datetime('now'))
		`, instanceID, string(data))
		if err != nil {
			return fmt.Errorf("store filesystem grant: %w", err)
		}
	}

	// Store network permissions
	if perms.Network.Mode != "" && perms.Network.Mode != "none" {
		data, _ := json.Marshal(perms.Network)
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO user_grants (instance_id, permission_type, grant_data, granted_at)
			VALUES (?, 'network', ?, datetime('now'))
		`, instanceID, string(data))
		if err != nil {
			return fmt.Errorf("store network grant: %w", err)
		}
	}

	// Store secrets permissions
	if len(perms.Secrets) > 0 {
		data, _ := json.Marshal(perms.Secrets)
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO user_grants (instance_id, permission_type, grant_data, granted_at)
			VALUES (?, 'secrets', ?, datetime('now'))
		`, instanceID, string(data))
		if err != nil {
			return fmt.Errorf("store secrets grant: %w", err)
		}
	}

	// Store exposure permissions
	if perms.Exposure.SecureLink {
		data, _ := json.Marshal(perms.Exposure)
		_, err = tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO user_grants (instance_id, permission_type, grant_data, granted_at)
			VALUES (?, 'exposure', ?, datetime('now'))
		`, instanceID, string(data))
		if err != nil {
			return fmt.Errorf("store exposure grant: %w", err)
		}
	}

	e.logger.Info().
		Str("instance_id", instanceID).
		Msg("granted permissions")

	return tx.Commit()
}

// RevokePermission removes a permission grant.
func (e *Engine) RevokePermission(ctx context.Context, instanceID string, permType string) error {
	_, err := e.db.ExecContext(ctx, `
		DELETE FROM user_grants WHERE instance_id = ? AND permission_type = ?
	`, instanceID, permType)
	if err != nil {
		return fmt.Errorf("revoke permission: %w", err)
	}

	e.logger.Info().
		Str("instance_id", instanceID).
		Str("permission_type", permType).
		Msg("revoked permission")

	return nil
}

// GetEffectivePermissions returns computed permissions for an instance.
func (e *Engine) GetEffectivePermissions(ctx context.Context, instanceID string) (*PermissionSet, error) {
	grants, err := e.GetUserGrants(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	// For V0, effective = user grants
	// In V1, this would merge package-declared with user grants
	return grants, nil
}

// GetUserGrants returns all user-granted permissions for an instance.
func (e *Engine) GetUserGrants(ctx context.Context, instanceID string) (*PermissionSet, error) {
	rows, err := e.db.QueryContext(ctx, `
		SELECT permission_type, grant_data FROM user_grants WHERE instance_id = ?
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("query grants: %w", err)
	}
	defer rows.Close()

	perms := &PermissionSet{}

	for rows.Next() {
		var permType, grantData string
		if err := rows.Scan(&permType, &grantData); err != nil {
			return nil, fmt.Errorf("scan grant: %w", err)
		}

		switch permType {
		case "filesystem":
			var fs FilesystemPerms
			if err := json.Unmarshal([]byte(grantData), &fs); err == nil {
				perms.Filesystem = fs
			}
		case "network":
			var net NetworkPerms
			if err := json.Unmarshal([]byte(grantData), &net); err == nil {
				perms.Network = net
			}
		case "secrets":
			var secrets []SecretRef
			if err := json.Unmarshal([]byte(grantData), &secrets); err == nil {
				perms.Secrets = secrets
			}
		case "exposure":
			var exp ExposurePerms
			if err := json.Unmarshal([]byte(grantData), &exp); err == nil {
				perms.Exposure = exp
			}
		}
	}

	return perms, rows.Err()
}

// evaluateFilesystem evaluates filesystem permission requests.
func (e *Engine) evaluateFilesystem(requested, granted FilesystemPerms, warnings *[]string) FilesystemPerms {
	effective := FilesystemPerms{}

	// Check readonly paths
	grantedRO := make(map[string]bool)
	for _, p := range granted.ReadonlyPaths {
		grantedRO[p] = true
	}

	for _, path := range requested.ReadonlyPaths {
		if grantedRO[path] || e.isPathGranted(path, granted.ReadonlyPaths) {
			effective.ReadonlyPaths = append(effective.ReadonlyPaths, path)
		} else {
			*warnings = append(*warnings, fmt.Sprintf("Filesystem readonly access to %s requires user approval", path))
		}
	}

	// Check readwrite paths
	grantedRW := make(map[string]bool)
	for _, p := range granted.ReadwritePaths {
		grantedRW[p] = true
	}

	for _, path := range requested.ReadwritePaths {
		if grantedRW[path] || e.isPathGranted(path, granted.ReadwritePaths) {
			effective.ReadwritePaths = append(effective.ReadwritePaths, path)
		} else {
			*warnings = append(*warnings, fmt.Sprintf("Filesystem read-write access to %s requires user approval", path))
		}
	}

	return effective
}

// isPathGranted checks if a path is covered by any granted path (parent directory).
func (e *Engine) isPathGranted(path string, grantedPaths []string) bool {
	normalizedPath := e.normalizePath(path)

	for _, granted := range grantedPaths {
		normalizedGranted := e.normalizePath(granted)
		if normalizedPath == normalizedGranted {
			return true
		}
		// Check if granted is a parent directory
		if strings.HasPrefix(normalizedPath, normalizedGranted+"/") {
			return true
		}
	}

	return false
}

// evaluateNetwork evaluates network permission requests.
func (e *Engine) evaluateNetwork(requested, granted NetworkPerms, warnings *[]string) NetworkPerms {
	if requested.Mode == "" || requested.Mode == "none" {
		return NetworkPerms{Mode: "none"}
	}

	if requested.Mode == "egress" {
		if granted.Mode == "egress" {
			// Check if domains are covered
			effective := NetworkPerms{Mode: "egress"}
			for _, domain := range requested.EgressDomains {
				if e.isDomainGranted(domain, granted.EgressDomains) {
					effective.EgressDomains = append(effective.EgressDomains, domain)
				} else {
					*warnings = append(*warnings, fmt.Sprintf("Network egress to %s requires user approval", domain))
				}
			}
			return effective
		}

		*warnings = append(*warnings, "Connector requests network egress access")
		return NetworkPerms{Mode: "none"}
	}

	return NetworkPerms{Mode: "none"}
}

// isDomainGranted checks if a domain is covered by granted domains.
func (e *Engine) isDomainGranted(domain string, grantedDomains []string) bool {
	for _, granted := range grantedDomains {
		if domain == granted {
			return true
		}
		// Wildcard matching: *.example.com matches api.example.com
		if strings.HasPrefix(granted, "*.") {
			suffix := granted[1:] // Remove "*"
			if strings.HasSuffix(domain, suffix) {
				return true
			}
		}
	}
	return false
}

// evaluateSecrets evaluates secret binding requests.
func (e *Engine) evaluateSecrets(requested, granted []SecretRef, warnings *[]string) []SecretRef {
	grantedSet := make(map[string]bool)
	for _, s := range granted {
		grantedSet[s.SecretID] = true
	}

	var effective []SecretRef
	for _, secret := range requested {
		if grantedSet[secret.SecretID] {
			effective = append(effective, secret)
		} else {
			*warnings = append(*warnings, fmt.Sprintf("Secret binding for %s requires user approval", secret.SecretID))
		}
	}

	return effective
}

// evaluateExposure evaluates exposure permission requests.
func (e *Engine) evaluateExposure(requested, granted ExposurePerms, warnings *[]string) ExposurePerms {
	if requested.SecureLink {
		if granted.SecureLink {
			return ExposurePerms{SecureLink: true}
		}
		*warnings = append(*warnings, "Exposing via Secure Link creates a public endpoint and requires user approval")
		return ExposurePerms{SecureLink: false}
	}

	return ExposurePerms{}
}

// recordDecision logs the decision for audit purposes.
func (e *Engine) recordDecision(ctx context.Context, decision *Decision) {
	e.logger.Info().
		Str("decision_id", decision.DecisionID).
		Str("decision", string(decision.Decision)).
		Str("reason", decision.Reason).
		Int("warning_count", len(decision.Warnings)).
		Int("block_count", len(decision.BlockReasons)).
		Msg("policy decision")

	// V1: Record to consent ledger
}

// initBuiltinRules initializes the built-in security rules.
func (e *Engine) initBuiltinRules() []Rule {
	return []Rule{
		{
			ID:       "deny_root_mount",
			Priority: 0,
			Condition: func(req Request) bool {
				for _, p := range req.Requested.Filesystem.ReadonlyPaths {
					if p == "/" {
						return true
					}
				}
				for _, p := range req.Requested.Filesystem.ReadwritePaths {
					if p == "/" {
						return true
					}
				}
				return false
			},
			Decision: Deny,
			Reason:   "Mounting root filesystem is forbidden",
		},
		{
			ID:       "deny_home_mount",
			Priority: 0,
			Condition: func(req Request) bool {
				return e.matchesHomeDirectory(req.Requested.Filesystem)
			},
			Decision: Deny,
			Reason:   "Mounting entire home directory is forbidden",
		},
		{
			ID:       "deny_credentials_mount",
			Priority: 0,
			Condition: func(req Request) bool {
				return e.matchesCredentialPaths(req.Requested.Filesystem)
			},
			Decision: Deny,
			Reason:   "Mounting credential directories is forbidden",
		},
		{
			ID:       "deny_system_paths",
			Priority: 0,
			Condition: func(req Request) bool {
				return e.matchesSystemPaths(req.Requested.Filesystem)
			},
			Decision: Deny,
			Reason:   "Mounting system directories is forbidden",
		},
		{
			ID:       "warn_network_egress",
			Priority: 10,
			Condition: func(req Request) bool {
				return req.Requested.Network.Mode == "egress"
			},
			Decision: Warn,
			Reason:   "Connector requests network access",
		},
		{
			ID:       "warn_secure_link",
			Priority: 10,
			Condition: func(req Request) bool {
				return req.Requested.Exposure.SecureLink
			},
			Decision: Warn,
			Reason:   "Exposing via Secure Link creates public endpoint",
		},
	}
}

// Forbidden paths - NEVER allow
var forbiddenPaths = []string{
	"/",
	"/etc",
	"/var",
	"/root",
	"/System",          // macOS
	"/Library",         // macOS
	"/private",         // macOS
	"C:\\Windows",      // Windows
	"C:\\Program Files", // Windows
	"C:\\ProgramData",  // Windows
}

// Allowed paths that override forbidden paths (temp directories are safe)
var allowedPaths = []string{
	"/tmp",
	"/var/folders", // macOS per-user temp directory
	"/private/var/folders", // macOS resolved symlink
	"/var/tmp",
}

// Forbidden patterns (relative to home)
var forbiddenPatterns = []string{
	".ssh",
	".gnupg",
	".aws",
	".config/gcloud",
	".azure",
	".kube",
	".docker",
	"Library/Keychains",   // macOS
	"AppData/Roaming",     // Windows
}

// checkForbiddenPaths checks for forbidden filesystem access.
func (e *Engine) checkForbiddenPaths(fs FilesystemPerms) []string {
	violations := []string{}
	allPaths := append(fs.ReadonlyPaths, fs.ReadwritePaths...)

	for _, path := range allPaths {
		normalizedPath := e.normalizePath(path)

		// First check if this path is explicitly allowed (e.g., temp directories)
		if e.isPathAllowed(normalizedPath) {
			continue
		}

		// Check exact forbidden paths
		for _, forbidden := range forbiddenPaths {
			normalizedForbidden := e.normalizePath(forbidden)
			if normalizedPath == normalizedForbidden || strings.HasPrefix(normalizedPath, normalizedForbidden+"/") {
				violations = append(violations, fmt.Sprintf("Path %s is forbidden", path))
			}
		}

		// Check forbidden patterns relative to home
		if e.homeDir != "" {
			for _, pattern := range forbiddenPatterns {
				forbiddenPath := filepath.Join(e.homeDir, pattern)
				normalizedForbidden := e.normalizePath(forbiddenPath)
				if normalizedPath == normalizedForbidden || strings.HasPrefix(normalizedPath, normalizedForbidden+"/") {
					violations = append(violations, fmt.Sprintf("Path %s matches forbidden pattern ~/%s", path, pattern))
				}
			}
		}
	}

	return violations
}

// isPathAllowed checks if a path is in the allowed list (e.g., temp directories).
func (e *Engine) isPathAllowed(normalizedPath string) bool {
	for _, allowed := range allowedPaths {
		normalizedAllowed := e.normalizePath(allowed)
		if normalizedPath == normalizedAllowed || strings.HasPrefix(normalizedPath, normalizedAllowed+"/") {
			return true
		}
	}
	return false
}

// matchesHomeDirectory checks if request mounts the entire home directory.
func (e *Engine) matchesHomeDirectory(fs FilesystemPerms) bool {
	if e.homeDir == "" {
		return false
	}

	allPaths := append(fs.ReadonlyPaths, fs.ReadwritePaths...)
	normalizedHome := e.normalizePath(e.homeDir)

	for _, path := range allPaths {
		normalizedPath := e.normalizePath(path)
		if normalizedPath == normalizedHome {
			return true
		}
	}

	return false
}

// matchesCredentialPaths checks for credential directory access.
func (e *Engine) matchesCredentialPaths(fs FilesystemPerms) bool {
	credentialDirs := []string{".ssh", ".gnupg", ".aws", ".config/gcloud", ".azure", ".kube"}
	allPaths := append(fs.ReadonlyPaths, fs.ReadwritePaths...)

	for _, path := range allPaths {
		normalizedPath := e.normalizePath(path)

		for _, cred := range credentialDirs {
			if e.homeDir != "" {
				credPath := e.normalizePath(filepath.Join(e.homeDir, cred))
				if normalizedPath == credPath || strings.HasPrefix(normalizedPath, credPath+"/") {
					return true
				}
			}
		}
	}

	return false
}

// matchesSystemPaths checks for system directory access.
func (e *Engine) matchesSystemPaths(fs FilesystemPerms) bool {
	var systemPaths []string

	switch runtime.GOOS {
	case "darwin":
		systemPaths = []string{"/System", "/Library", "/private"}
	case "windows":
		systemPaths = []string{"C:\\Windows", "C:\\Program Files", "C:\\ProgramData"}
	default:
		systemPaths = []string{"/etc", "/var", "/root"}
	}

	allPaths := append(fs.ReadonlyPaths, fs.ReadwritePaths...)

	for _, path := range allPaths {
		normalizedPath := e.normalizePath(path)

		for _, sysPath := range systemPaths {
			normalizedSys := e.normalizePath(sysPath)
			if normalizedPath == normalizedSys || strings.HasPrefix(normalizedPath, normalizedSys+"/") {
				return true
			}
		}
	}

	return false
}

// normalizePath normalizes a path for comparison.
func (e *Engine) normalizePath(path string) string {
	// Expand home directory
	if strings.HasPrefix(path, "~") {
		if e.homeDir != "" {
			path = filepath.Join(e.homeDir, path[1:])
		}
	}

	// Clean the path
	path = filepath.Clean(path)

	// On Windows, normalize drive letter to uppercase
	if runtime.GOOS == "windows" && len(path) >= 2 && path[1] == ':' {
		path = strings.ToUpper(path[:1]) + path[1:]
	}

	return path
}
