package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/simpleflo/conduit/pkg/models"
)

// CreateBinding creates a new client binding.
func (s *Store) CreateBinding(ctx context.Context, binding *models.ClientBinding) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO client_bindings (
			binding_id, instance_id, client_id, scope, config_path,
			change_set_id, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		binding.BindingID,
		binding.InstanceID,
		binding.ClientID,
		binding.Scope,
		binding.ConfigPath,
		binding.ChangeSetID,
		binding.Status,
		binding.CreatedAt.Format(time.RFC3339),
		binding.UpdatedAt.Format(time.RFC3339),
	)

	if err != nil {
		return fmt.Errorf("create binding: %w", err)
	}

	return nil
}

// GetBinding retrieves a binding by ID.
func (s *Store) GetBinding(ctx context.Context, bindingID string) (*models.ClientBinding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			binding_id, instance_id, client_id, scope, config_path,
			change_set_id, status, created_at, updated_at, validated_at
		FROM client_bindings
		WHERE binding_id = ?
	`, bindingID)

	return scanBinding(row)
}

// GetBindingByInstanceAndClient retrieves a binding for a specific instance and client.
func (s *Store) GetBindingByInstanceAndClient(ctx context.Context, instanceID, clientID string) (*models.ClientBinding, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT
			binding_id, instance_id, client_id, scope, config_path,
			change_set_id, status, created_at, updated_at, validated_at
		FROM client_bindings
		WHERE instance_id = ? AND client_id = ? AND status = 'active'
	`, instanceID, clientID)

	return scanBinding(row)
}

// ListBindings returns all bindings.
func (s *Store) ListBindings(ctx context.Context) ([]*models.ClientBinding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			binding_id, instance_id, client_id, scope, config_path,
			change_set_id, status, created_at, updated_at, validated_at
		FROM client_bindings
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list bindings: %w", err)
	}
	defer rows.Close()

	var bindings []*models.ClientBinding
	for rows.Next() {
		binding, err := scanBindingRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan binding: %w", err)
		}
		bindings = append(bindings, binding)
	}

	return bindings, rows.Err()
}

// ListBindingsByInstance returns bindings for a specific instance.
func (s *Store) ListBindingsByInstance(ctx context.Context, instanceID string) ([]*models.ClientBinding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			binding_id, instance_id, client_id, scope, config_path,
			change_set_id, status, created_at, updated_at, validated_at
		FROM client_bindings
		WHERE instance_id = ?
		ORDER BY created_at DESC
	`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list bindings by instance: %w", err)
	}
	defer rows.Close()

	var bindings []*models.ClientBinding
	for rows.Next() {
		binding, err := scanBindingRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan binding: %w", err)
		}
		bindings = append(bindings, binding)
	}

	return bindings, rows.Err()
}

// ListBindingsByClient returns bindings for a specific client.
func (s *Store) ListBindingsByClient(ctx context.Context, clientID string) ([]*models.ClientBinding, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			binding_id, instance_id, client_id, scope, config_path,
			change_set_id, status, created_at, updated_at, validated_at
		FROM client_bindings
		WHERE client_id = ? AND status = 'active'
		ORDER BY created_at DESC
	`, clientID)
	if err != nil {
		return nil, fmt.Errorf("list bindings by client: %w", err)
	}
	defer rows.Close()

	var bindings []*models.ClientBinding
	for rows.Next() {
		binding, err := scanBindingRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scan binding: %w", err)
		}
		bindings = append(bindings, binding)
	}

	return bindings, rows.Err()
}

// UpdateBindingStatus updates the status of a binding.
func (s *Store) UpdateBindingStatus(ctx context.Context, bindingID, status string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE client_bindings
		SET status = ?, updated_at = ?
		WHERE binding_id = ?
	`, status, time.Now().Format(time.RFC3339), bindingID)

	return err
}

// UpdateBindingValidated marks a binding as validated.
func (s *Store) UpdateBindingValidated(ctx context.Context, bindingID string) error {
	now := time.Now().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		UPDATE client_bindings
		SET validated_at = ?, updated_at = ?
		WHERE binding_id = ?
	`, now, now, bindingID)

	return err
}

// DeleteBinding removes a binding.
func (s *Store) DeleteBinding(ctx context.Context, bindingID string) error {
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM client_bindings WHERE binding_id = ?
	`, bindingID)

	if err != nil {
		return fmt.Errorf("delete binding: %w", err)
	}

	affected, _ := result.RowsAffected()
	if affected == 0 {
		return models.NewError(models.ErrBindingNotFound, "binding not found")
	}

	return nil
}

// CreateBackup stores a config backup record.
func (s *Store) CreateBackup(ctx context.Context, backup *models.ConfigBackup) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO config_backups (
			backup_id, change_set_id, client_id, original_path, backup_path,
			file_existed, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`,
		backup.BackupID,
		backup.ChangeSetID,
		backup.ClientID,
		backup.OriginalPath,
		backup.BackupPath,
		backup.FileExisted,
		backup.CreatedAt.Format(time.RFC3339),
	)

	return err
}

// GetBackupsByChangeSet retrieves all backups for a change set.
func (s *Store) GetBackupsByChangeSet(ctx context.Context, changeSetID string) ([]*models.ConfigBackup, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			backup_id, change_set_id, client_id, original_path, backup_path,
			file_existed, created_at
		FROM config_backups
		WHERE change_set_id = ?
	`, changeSetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var backups []*models.ConfigBackup
	for rows.Next() {
		var backup models.ConfigBackup
		var createdAt string
		err := rows.Scan(
			&backup.BackupID,
			&backup.ChangeSetID,
			&backup.ClientID,
			&backup.OriginalPath,
			&backup.BackupPath,
			&backup.FileExisted,
			&createdAt,
		)
		if err != nil {
			return nil, err
		}
		backup.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		backups = append(backups, &backup)
	}

	return backups, rows.Err()
}

func scanBinding(row *sql.Row) (*models.ClientBinding, error) {
	var binding models.ClientBinding
	var createdAt, updatedAt string
	var validatedAt sql.NullString

	err := row.Scan(
		&binding.BindingID,
		&binding.InstanceID,
		&binding.ClientID,
		&binding.Scope,
		&binding.ConfigPath,
		&binding.ChangeSetID,
		&binding.Status,
		&createdAt,
		&updatedAt,
		&validatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, models.NewError(models.ErrBindingNotFound, "binding not found")
	}
	if err != nil {
		return nil, err
	}

	binding.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	binding.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if validatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, validatedAt.String)
		binding.ValidatedAt = &t
	}

	return &binding, nil
}

func scanBindingRows(rows *sql.Rows) (*models.ClientBinding, error) {
	var binding models.ClientBinding
	var createdAt, updatedAt string
	var validatedAt sql.NullString

	err := rows.Scan(
		&binding.BindingID,
		&binding.InstanceID,
		&binding.ClientID,
		&binding.Scope,
		&binding.ConfigPath,
		&binding.ChangeSetID,
		&binding.Status,
		&createdAt,
		&updatedAt,
		&validatedAt,
	)

	if err != nil {
		return nil, err
	}

	binding.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	binding.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	if validatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, validatedAt.String)
		binding.ValidatedAt = &t
	}

	return &binding, nil
}
