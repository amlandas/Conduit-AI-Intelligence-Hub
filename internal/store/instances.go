package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/simpleflo/conduit/pkg/models"
)

// CreateInstance creates a new connector instance.
func (s *Store) CreateInstance(ctx context.Context, instance *models.ConnectorInstance) error {
	config, _ := json.Marshal(instance.Config)
	grantedPerms, _ := json.Marshal(instance.GrantedPerms)
	auditResult, _ := json.Marshal(instance.AuditResult)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO connector_instances (
			instance_id, package_id, package_version, display_name, status,
			container_id, socket_path, image_ref, config, granted_perms,
			audit_result, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		instance.InstanceID,
		instance.PackageID,
		instance.PackageVersion,
		instance.DisplayName,
		instance.Status,
		nullString(instance.ContainerID),
		nullString(instance.SocketPath),
		instance.ImageRef,
		string(config),
		string(grantedPerms),
		string(auditResult),
		instance.CreatedAt.Format(time.RFC3339),
		instance.UpdatedAt.Format(time.RFC3339),
	)

	if err != nil {
		return fmt.Errorf("create instance: %w", err)
	}

	return nil
}

// GetInstance retrieves an instance by ID.
func (s *Store) GetInstance(ctx context.Context, instanceID string) (*models.ConnectorInstance, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			instance_id, package_id, package_version, display_name, status,
			container_id, socket_path, image_ref, config, granted_perms,
			audit_result, created_at, updated_at, started_at, stopped_at,
			last_health_check, health_status, error_message
		FROM connector_instances
		WHERE instance_id = ?
	`, instanceID)

	return scanInstance(row)
}

// ListInstances returns all connector instances.
func (s *Store) ListInstances(ctx context.Context) ([]*models.ConnectorInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			instance_id, package_id, package_version, display_name, status,
			container_id, socket_path, image_ref, config, granted_perms,
			audit_result, created_at, updated_at, started_at, stopped_at,
			last_health_check, health_status, error_message
		FROM connector_instances
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	defer rows.Close()

	var instances []*models.ConnectorInstance
	for rows.Next() {
		instance, err := scanInstanceRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

// ListInstancesByStatus returns instances with the given status.
func (s *Store) ListInstancesByStatus(ctx context.Context, status models.InstanceStatus) ([]*models.ConnectorInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			instance_id, package_id, package_version, display_name, status,
			container_id, socket_path, image_ref, config, granted_perms,
			audit_result, created_at, updated_at, started_at, stopped_at,
			last_health_check, health_status, error_message
		FROM connector_instances
		WHERE status = ?
		ORDER BY created_at DESC
	`, status)
	if err != nil {
		return nil, fmt.Errorf("list instances by status: %w", err)
	}
	defer rows.Close()

	var instances []*models.ConnectorInstance
	for rows.Next() {
		instance, err := scanInstanceRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		instances = append(instances, instance)
	}

	return instances, rows.Err()
}

// UpdateInstanceStatus updates the status of an instance.
func (s *Store) UpdateInstanceStatus(ctx context.Context, instanceID string, status models.InstanceStatus, errorMsg string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET status = ?, error_message = ?, updated_at = ?
		WHERE instance_id = ?
	`, status, nullString(errorMsg), time.Now().Format(time.RFC3339), instanceID)

	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return models.NewError(models.ErrInstanceNotFound, "instance not found").WithDetails("instance_id", instanceID)
	}

	return nil
}

// UpdateInstanceContainer updates container-related fields.
func (s *Store) UpdateInstanceContainer(ctx context.Context, instanceID, containerID, socketPath string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET container_id = ?, socket_path = ?, updated_at = ?
		WHERE instance_id = ?
	`, nullString(containerID), nullString(socketPath), time.Now().Format(time.RFC3339), instanceID)

	return err
}

// UpdateInstanceStarted marks an instance as started.
func (s *Store) UpdateInstanceStarted(ctx context.Context, instanceID string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET status = 'RUNNING', started_at = ?, updated_at = ?
		WHERE instance_id = ?
	`, now, now, instanceID)

	return err
}

// UpdateInstanceStopped marks an instance as stopped.
func (s *Store) UpdateInstanceStopped(ctx context.Context, instanceID string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET status = 'STOPPED', stopped_at = ?, container_id = NULL, updated_at = ?
		WHERE instance_id = ?
	`, now, now, instanceID)

	return err
}

// UpdateInstanceHealth updates health check information.
func (s *Store) UpdateInstanceHealth(ctx context.Context, instanceID, healthStatus string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE connector_instances
		SET last_health_check = ?, health_status = ?, updated_at = ?
		WHERE instance_id = ?
	`, now, healthStatus, now, instanceID)

	return err
}

// DeleteInstance removes an instance from the database.
func (s *Store) DeleteInstance(ctx context.Context, instanceID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM connector_instances WHERE instance_id = ?
	`, instanceID)

	if err != nil {
		return fmt.Errorf("delete instance: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return models.NewError(models.ErrInstanceNotFound, "instance not found")
	}

	return nil
}

// Helper functions

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func scanInstance(row *sql.Row) (*models.ConnectorInstance, error) {
	var (
		instance                                               models.ConnectorInstance
		containerID, socketPath, errorMsg, healthStatus        sql.NullString
		config, grantedPerms, auditResult                      sql.NullString
		createdAt, updatedAt                                   string
		startedAt, stoppedAt, lastHealthCheck                  sql.NullString
	)

	err := row.Scan(
		&instance.InstanceID,
		&instance.PackageID,
		&instance.PackageVersion,
		&instance.DisplayName,
		&instance.Status,
		&containerID,
		&socketPath,
		&instance.ImageRef,
		&config,
		&grantedPerms,
		&auditResult,
		&createdAt,
		&updatedAt,
		&startedAt,
		&stoppedAt,
		&lastHealthCheck,
		&healthStatus,
		&errorMsg,
	)

	if err == sql.ErrNoRows {
		return nil, models.NewError(models.ErrInstanceNotFound, "instance not found")
	}
	if err != nil {
		return nil, err
	}

	// Parse timestamps
	instance.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	instance.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339, startedAt.String)
		instance.StartedAt = &t
	}
	if stoppedAt.Valid {
		t, _ := time.Parse(time.RFC3339, stoppedAt.String)
		instance.StoppedAt = &t
	}
	if lastHealthCheck.Valid {
		t, _ := time.Parse(time.RFC3339, lastHealthCheck.String)
		instance.LastHealthCheck = &t
	}

	// Parse JSON fields
	if config.Valid {
		json.Unmarshal([]byte(config.String), &instance.Config)
	}
	if grantedPerms.Valid {
		json.Unmarshal([]byte(grantedPerms.String), &instance.GrantedPerms)
	}
	if auditResult.Valid {
		json.Unmarshal([]byte(auditResult.String), &instance.AuditResult)
	}

	instance.ContainerID = containerID.String
	instance.SocketPath = socketPath.String
	instance.HealthStatus = healthStatus.String
	instance.ErrorMessage = errorMsg.String

	return &instance, nil
}

func scanInstanceRows(rows *sql.Rows) (*models.ConnectorInstance, error) {
	var (
		instance                                               models.ConnectorInstance
		containerID, socketPath, errorMsg, healthStatus        sql.NullString
		config, grantedPerms, auditResult                      sql.NullString
		createdAt, updatedAt                                   string
		startedAt, stoppedAt, lastHealthCheck                  sql.NullString
	)

	err := rows.Scan(
		&instance.InstanceID,
		&instance.PackageID,
		&instance.PackageVersion,
		&instance.DisplayName,
		&instance.Status,
		&containerID,
		&socketPath,
		&instance.ImageRef,
		&config,
		&grantedPerms,
		&auditResult,
		&createdAt,
		&updatedAt,
		&startedAt,
		&stoppedAt,
		&lastHealthCheck,
		&healthStatus,
		&errorMsg,
	)

	if err != nil {
		return nil, err
	}

	// Parse timestamps
	instance.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	instance.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339, startedAt.String)
		instance.StartedAt = &t
	}
	if stoppedAt.Valid {
		t, _ := time.Parse(time.RFC3339, stoppedAt.String)
		instance.StoppedAt = &t
	}
	if lastHealthCheck.Valid {
		t, _ := time.Parse(time.RFC3339, lastHealthCheck.String)
		instance.LastHealthCheck = &t
	}

	// Parse JSON fields
	if config.Valid {
		json.Unmarshal([]byte(config.String), &instance.Config)
	}
	if grantedPerms.Valid {
		json.Unmarshal([]byte(grantedPerms.String), &instance.GrantedPerms)
	}
	if auditResult.Valid {
		json.Unmarshal([]byte(auditResult.String), &instance.AuditResult)
	}

	instance.ContainerID = containerID.String
	instance.SocketPath = socketPath.String
	instance.HealthStatus = healthStatus.String
	instance.ErrorMessage = errorMsg.String

	return &instance, nil
}
