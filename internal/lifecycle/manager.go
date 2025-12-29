package lifecycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
	"github.com/simpleflo/conduit/internal/policy"
	"github.com/simpleflo/conduit/internal/runtime"
)

// Manager handles connector instance lifecycle operations.
type Manager struct {
	db      *sql.DB
	runtime runtime.Provider
	policy  *policy.Engine
	logger  zerolog.Logger

	// Operation tracking
	opsMu      sync.RWMutex
	operations map[string]*Operation

	// Health monitoring
	healthInterval time.Duration
	healthCh       chan struct{}
	wg             sync.WaitGroup
}

// New creates a new Lifecycle Manager.
func New(db *sql.DB, rt runtime.Provider, pol *policy.Engine) *Manager {
	return &Manager{
		db:             db,
		runtime:        rt,
		policy:         pol,
		logger:         observability.Logger("lifecycle"),
		operations:     make(map[string]*Operation),
		healthInterval: 30 * time.Second,
		healthCh:       make(chan struct{}),
	}
}

// SetHealthInterval configures the health check interval.
func (m *Manager) SetHealthInterval(interval time.Duration) {
	m.healthInterval = interval
}

// Start begins background health monitoring.
func (m *Manager) Start(ctx context.Context) {
	m.wg.Add(1)
	go m.healthMonitorLoop(ctx)
}

// Stop stops background operations.
func (m *Manager) Stop() {
	close(m.healthCh)
	m.wg.Wait()
}

// WaitForOperations blocks until all operations complete.
func (m *Manager) WaitForOperations() {
	m.wg.Wait()
}

// CreateInstance creates a new connector instance.
func (m *Manager) CreateInstance(ctx context.Context, req CreateInstanceRequest) (*Instance, error) {
	instanceID := uuid.New().String()

	config, _ := json.Marshal(req.Config)

	_, err := m.db.ExecContext(ctx, `
		INSERT INTO connector_instances
		(instance_id, package_id, package_version, display_name, image_ref, config, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
	`, instanceID, req.PackageID, req.Version, req.DisplayName, req.ImageRef, string(config), string(StatusCreated))

	if err != nil {
		return nil, fmt.Errorf("create instance: %w", err)
	}

	m.logger.Info().
		Str("instance_id", instanceID).
		Str("package_id", req.PackageID).
		Msg("created instance")

	return m.GetInstance(ctx, instanceID)
}

// InstallInstance runs the installation flow for an instance.
func (m *Manager) InstallInstance(ctx context.Context, instanceID string) (*Operation, error) {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	if !IsValidTransition(instance.Status, StatusAuditing) {
		return nil, fmt.Errorf("cannot install instance in status %s", instance.Status)
	}

	// Create operation
	op := m.createOperation("install", instanceID)

	// Run installation in background
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runInstall(context.Background(), instanceID, op.OperationID)
	}()

	return op, nil
}

// runInstall performs the installation steps.
func (m *Manager) runInstall(ctx context.Context, instanceID, operationID string) {
	// Stage 1: Audit
	m.updateOperation(operationID, "running", "auditing", 10, "")

	if err := m.transitionTo(ctx, instanceID, StatusAuditing); err != nil {
		m.failOperation(operationID, fmt.Sprintf("transition failed: %v", err))
		return
	}

	// For V0, we skip actual audit and just proceed
	// In V1, we would call the auditor here

	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		m.failOperation(operationID, fmt.Sprintf("get instance: %v", err))
		return
	}

	// Stage 2: Policy evaluation
	m.updateOperation(operationID, "running", "policy_check", 25, "")

	decision, err := m.policy.Evaluate(ctx, policy.Request{
		Scope:      policy.ScopeInstall,
		InstanceID: instanceID,
		PackageID:  instance.PackageID,
		Actor:      "system",
		// For V0, we don't have declared permissions in the package
		// So we just evaluate with empty permissions
		Requested: policy.PermissionSet{},
	})
	if err != nil {
		m.failOperation(operationID, fmt.Sprintf("policy evaluation: %v", err))
		return
	}

	if decision.Decision == policy.Deny {
		m.transitionTo(ctx, instanceID, StatusBlocked)
		m.failOperation(operationID, fmt.Sprintf("policy denied: %s", decision.Reason))
		return
	}

	// Stage 3: Pull image
	m.updateOperation(operationID, "running", "pulling", 40, "")

	if instance.ImageRef != "" && m.runtime != nil {
		if err := m.runtime.Pull(ctx, instance.ImageRef, runtime.PullOptions{}); err != nil {
			m.failOperation(operationID, fmt.Sprintf("image pull: %v", err))
			return
		}
	}

	// Stage 4: Mark as installed
	m.updateOperation(operationID, "running", "finalizing", 90, "")

	if err := m.transitionTo(ctx, instanceID, StatusInstalled); err != nil {
		m.failOperation(operationID, fmt.Sprintf("transition failed: %v", err))
		return
	}

	// Complete operation
	m.completeOperation(operationID, map[string]string{"status": "installed"})

	m.logger.Info().
		Str("instance_id", instanceID).
		Msg("instance installed")
}

// StartInstance starts a connector instance.
func (m *Manager) StartInstance(ctx context.Context, instanceID string) error {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	if !IsValidTransition(instance.Status, StatusStarting) {
		return fmt.Errorf("cannot start instance in status %s", instance.Status)
	}

	if m.runtime == nil {
		return fmt.Errorf("no runtime provider available")
	}

	// Transition to STARTING
	if err := m.transitionTo(ctx, instanceID, StatusStarting); err != nil {
		return err
	}

	// Build container spec
	spec := runtime.ContainerSpec{
		Name:  fmt.Sprintf("conduit-%s", instanceID[:8]),
		Image: instance.ImageRef,
		Labels: map[string]string{
			"conduit.instance_id": instanceID,
			"conduit.managed":     "true",
		},
		Security: runtime.SecuritySpec{
			ReadOnlyRootfs:  true,
			NoNewPrivileges: true,
			DropCapabilities: []string{"ALL"},
		},
		Network: runtime.NetworkSpec{
			Mode: "none", // Default to no network for security
		},
		Stdin: true, // MCP servers need stdin
	}

	// Add config as environment variables
	if instance.Config != nil {
		spec.Env = make(map[string]string)
		for k, v := range instance.Config {
			spec.Env[k] = v
		}
	}

	// Start container
	containerID, err := m.runtime.Run(ctx, spec)
	if err != nil {
		m.transitionTo(ctx, instanceID, StatusDegraded)
		m.updateInstanceError(ctx, instanceID, fmt.Sprintf("container start failed: %v", err))
		return fmt.Errorf("container start: %w", err)
	}

	// Update instance with container ID
	_, err = m.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET container_id = ?, started_at = datetime('now'), updated_at = datetime('now'), error_message = NULL
		WHERE instance_id = ?
	`, containerID, instanceID)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}

	// Check initial health
	health, err := m.CheckHealth(ctx, instanceID)
	if err != nil || health.Status == "unhealthy" {
		m.transitionTo(ctx, instanceID, StatusDegraded)
		return nil // Started but degraded
	}

	// Transition to RUNNING
	if err := m.transitionTo(ctx, instanceID, StatusRunning); err != nil {
		return err
	}

	m.logger.Info().
		Str("instance_id", instanceID).
		Str("container_id", containerID).
		Msg("instance started")

	return nil
}

// StopInstance stops a running connector instance.
func (m *Manager) StopInstance(ctx context.Context, instanceID string) error {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	if !IsValidTransition(instance.Status, StatusStopping) {
		return fmt.Errorf("cannot stop instance in status %s", instance.Status)
	}

	if m.runtime == nil {
		return fmt.Errorf("no runtime provider available")
	}

	// Transition to STOPPING
	if err := m.transitionTo(ctx, instanceID, StatusStopping); err != nil {
		return err
	}

	// Stop container
	if instance.ContainerID != "" {
		if err := m.runtime.Stop(ctx, instance.ContainerID, 30*time.Second); err != nil {
			m.logger.Warn().
				Err(err).
				Str("instance_id", instanceID).
				Str("container_id", instance.ContainerID).
				Msg("container stop failed")
		}
	}

	// Update stopped time
	_, err = m.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET stopped_at = datetime('now'), updated_at = datetime('now')
		WHERE instance_id = ?
	`, instanceID)
	if err != nil {
		return fmt.Errorf("update instance: %w", err)
	}

	// Transition to STOPPED
	if err := m.transitionTo(ctx, instanceID, StatusStopped); err != nil {
		return err
	}

	m.logger.Info().
		Str("instance_id", instanceID).
		Msg("instance stopped")

	return nil
}

// DisableInstance disables a connector instance.
func (m *Manager) DisableInstance(ctx context.Context, instanceID string) error {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	if !IsValidTransition(instance.Status, StatusDisabled) {
		return fmt.Errorf("cannot disable instance in status %s", instance.Status)
	}

	// Stop if running
	if instance.Status == StatusRunning || instance.Status == StatusDegraded {
		if err := m.StopInstance(ctx, instanceID); err != nil {
			return err
		}
	}

	// Transition to DISABLED
	return m.transitionTo(ctx, instanceID, StatusDisabled)
}

// EnableInstance enables a disabled connector instance.
func (m *Manager) EnableInstance(ctx context.Context, instanceID string) error {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	if instance.Status != StatusDisabled {
		return fmt.Errorf("instance is not disabled")
	}

	// Transition to INSTALLED (can then be started)
	return m.transitionTo(ctx, instanceID, StatusInstalled)
}

// RemoveInstance removes a connector instance.
func (m *Manager) RemoveInstance(ctx context.Context, instanceID string) error {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	// Stop if running
	if instance.Status == StatusRunning || instance.Status == StatusDegraded {
		if err := m.StopInstance(ctx, instanceID); err != nil {
			m.logger.Warn().Err(err).Msg("failed to stop instance before removal")
		}
	}

	// Remove container if exists
	if instance.ContainerID != "" && m.runtime != nil {
		if err := m.runtime.Remove(ctx, instance.ContainerID, true); err != nil {
			m.logger.Warn().Err(err).Msg("failed to remove container")
		}
	}

	// Transition to REMOVING then REMOVED
	m.transitionTo(ctx, instanceID, StatusRemoving)

	// Delete from database
	_, err = m.db.ExecContext(ctx, `DELETE FROM connector_instances WHERE instance_id = ?`, instanceID)
	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}

	m.logger.Info().
		Str("instance_id", instanceID).
		Msg("instance removed")

	return nil
}

// GetInstance retrieves an instance by ID.
func (m *Manager) GetInstance(ctx context.Context, instanceID string) (*Instance, error) {
	row := m.db.QueryRowContext(ctx, `
		SELECT instance_id, package_id, package_version, display_name, image_ref, status,
		       container_id, socket_path, config, error_message, created_at, updated_at,
		       started_at, stopped_at
		FROM connector_instances
		WHERE instance_id = ?
	`, instanceID)

	var inst Instance
	var containerID, socketPath, config, errorMsg sql.NullString
	var createdAt, updatedAt string
	var startedAt, stoppedAt sql.NullString

	err := row.Scan(
		&inst.InstanceID, &inst.PackageID, &inst.PackageVersion,
		&inst.DisplayName, &inst.ImageRef, &inst.Status,
		&containerID, &socketPath, &config, &errorMsg,
		&createdAt, &updatedAt, &startedAt, &stoppedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}
	if err != nil {
		return nil, fmt.Errorf("scan instance: %w", err)
	}

	// Parse datetime strings from SQLite
	inst.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	inst.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

	if containerID.Valid {
		inst.ContainerID = containerID.String
	}
	if socketPath.Valid {
		inst.SocketPath = socketPath.String
	}
	if config.Valid && config.String != "" {
		json.Unmarshal([]byte(config.String), &inst.Config)
	}
	if errorMsg.Valid {
		inst.ErrorMessage = errorMsg.String
	}
	if startedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", startedAt.String)
		inst.StartedAt = &t
	}
	if stoppedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", stoppedAt.String)
		inst.StoppedAt = &t
	}

	return &inst, nil
}

// ListInstances retrieves all instances.
func (m *Manager) ListInstances(ctx context.Context) ([]*Instance, error) {
	rows, err := m.db.QueryContext(ctx, `
		SELECT instance_id, package_id, package_version, display_name, image_ref, status,
		       container_id, socket_path, config, error_message, created_at, updated_at,
		       started_at, stopped_at
		FROM connector_instances
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query instances: %w", err)
	}
	defer rows.Close()

	var instances []*Instance
	for rows.Next() {
		var inst Instance
		var containerID, socketPath, config, errorMsg sql.NullString
		var createdAt, updatedAt string
		var startedAt, stoppedAt sql.NullString

		err := rows.Scan(
			&inst.InstanceID, &inst.PackageID, &inst.PackageVersion,
			&inst.DisplayName, &inst.ImageRef, &inst.Status,
			&containerID, &socketPath, &config, &errorMsg,
			&createdAt, &updatedAt, &startedAt, &stoppedAt,
		)
		if err != nil {
			continue
		}

		// Parse datetime strings from SQLite
		inst.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		inst.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)

		if containerID.Valid {
			inst.ContainerID = containerID.String
		}
		if socketPath.Valid {
			inst.SocketPath = socketPath.String
		}
		if config.Valid && config.String != "" {
			json.Unmarshal([]byte(config.String), &inst.Config)
		}
		if errorMsg.Valid {
			inst.ErrorMessage = errorMsg.String
		}
		if startedAt.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", startedAt.String)
			inst.StartedAt = &t
		}
		if stoppedAt.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", stoppedAt.String)
			inst.StoppedAt = &t
		}

		instances = append(instances, &inst)
	}

	return instances, rows.Err()
}

// CheckHealth checks the health of an instance.
func (m *Manager) CheckHealth(ctx context.Context, instanceID string) (*HealthStatus, error) {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return nil, err
	}

	health := &HealthStatus{
		LastCheck: time.Now(),
		Status:    "unknown",
	}

	if instance.ContainerID == "" || m.runtime == nil {
		health.Status = "unknown"
		health.Message = "No container running"
		return health, nil
	}

	// Check container status
	status, err := m.runtime.Status(ctx, instance.ContainerID)
	if err != nil {
		health.Status = "unhealthy"
		health.Message = fmt.Sprintf("Container check failed: %v", err)
	} else if status == "running" {
		health.Status = "healthy"
		health.Message = "Container is running"
	} else {
		health.Status = "unhealthy"
		health.Message = fmt.Sprintf("Container status: %s", status)
	}

	// Update health status in database
	_, err = m.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET health_status = ?, last_health_check = datetime('now')
		WHERE instance_id = ?
	`, health.Status, instanceID)

	return health, nil
}

// RunHealthChecks checks health of all running instances.
func (m *Manager) RunHealthChecks(ctx context.Context) error {
	rows, err := m.db.QueryContext(ctx, `
		SELECT instance_id FROM connector_instances
		WHERE status IN ('RUNNING', 'DEGRADED')
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var instanceID string
		rows.Scan(&instanceID)

		health, err := m.CheckHealth(ctx, instanceID)
		if err != nil {
			continue
		}

		instance, _ := m.GetInstance(ctx, instanceID)
		if instance == nil {
			continue
		}

		// Update status based on health
		if health.Status == "healthy" && instance.Status == StatusDegraded {
			m.transitionTo(ctx, instanceID, StatusRunning)
		} else if health.Status == "unhealthy" && instance.Status == StatusRunning {
			m.transitionTo(ctx, instanceID, StatusDegraded)
		}
	}

	return nil
}

// GetOperation retrieves an operation by ID.
func (m *Manager) GetOperation(ctx context.Context, operationID string) (*Operation, error) {
	m.opsMu.RLock()
	defer m.opsMu.RUnlock()

	op, exists := m.operations[operationID]
	if !exists {
		return nil, fmt.Errorf("operation not found: %s", operationID)
	}

	return op, nil
}

// transitionTo changes instance status.
func (m *Manager) transitionTo(ctx context.Context, instanceID string, newStatus InstanceStatus) error {
	instance, err := m.GetInstance(ctx, instanceID)
	if err != nil {
		return err
	}

	if !IsValidTransition(instance.Status, newStatus) {
		return fmt.Errorf("invalid transition from %s to %s", instance.Status, newStatus)
	}

	_, err = m.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET status = ?, updated_at = datetime('now')
		WHERE instance_id = ?
	`, string(newStatus), instanceID)

	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	m.logger.Debug().
		Str("instance_id", instanceID).
		Str("from", string(instance.Status)).
		Str("to", string(newStatus)).
		Msg("instance status transition")

	return nil
}

// updateInstanceError updates the error message for an instance.
func (m *Manager) updateInstanceError(ctx context.Context, instanceID, errorMsg string) {
	m.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET error_message = ?, updated_at = datetime('now')
		WHERE instance_id = ?
	`, errorMsg, instanceID)
}

// createOperation creates a new operation.
func (m *Manager) createOperation(opType, instanceID string) *Operation {
	op := &Operation{
		OperationID: uuid.New().String(),
		Type:        opType,
		InstanceID:  instanceID,
		Status:      "pending",
		Progress:    0,
		CreatedAt:   time.Now(),
	}

	m.opsMu.Lock()
	m.operations[op.OperationID] = op
	m.opsMu.Unlock()

	return op
}

// updateOperation updates operation progress.
func (m *Manager) updateOperation(operationID, status, stage string, progress int, errorMsg string) {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	if op, exists := m.operations[operationID]; exists {
		op.Status = status
		op.CurrentStage = stage
		op.Progress = progress
		op.Error = errorMsg
	}
}

// completeOperation marks an operation as complete.
func (m *Manager) completeOperation(operationID string, result interface{}) {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	if op, exists := m.operations[operationID]; exists {
		op.Status = "completed"
		op.Progress = 100
		op.Result = result
		now := time.Now()
		op.CompletedAt = &now
	}
}

// failOperation marks an operation as failed.
func (m *Manager) failOperation(operationID, errorMsg string) {
	m.opsMu.Lock()
	defer m.opsMu.Unlock()

	if op, exists := m.operations[operationID]; exists {
		op.Status = "failed"
		op.Error = errorMsg
		now := time.Now()
		op.CompletedAt = &now
	}
}

// healthMonitorLoop runs periodic health checks.
func (m *Manager) healthMonitorLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.healthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.healthCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.RunHealthChecks(ctx)
		}
	}
}
