package adapters

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
)

// baseAdapter provides common functionality for all adapters.
type baseAdapter struct {
	db *sql.DB
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// dirExists checks if a directory exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	// Create destination directory if needed
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// ensureDir creates a directory if it doesn't exist.
func ensureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// storeBackup stores a backup record in the database.
func (a *baseAdapter) storeBackup(changeSetID, clientID, originalPath, backupPath string, fileExisted bool) error {
	_, err := a.db.Exec(`
		INSERT INTO config_backups (backup_id, change_set_id, client_id, original_path, backup_path, file_existed, created_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
	`, generateBackupID(), changeSetID, clientID, originalPath, backupPath, fileExisted)
	return err
}

// getBackups retrieves backup records for a change set.
func (a *baseAdapter) getBackups(changeSetID string) []BackupRecord {
	rows, err := a.db.Query(`
		SELECT change_set_id, client_id, original_path, backup_path, file_existed
		FROM config_backups
		WHERE change_set_id = ?
	`, changeSetID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var backups []BackupRecord
	for rows.Next() {
		var b BackupRecord
		if err := rows.Scan(&b.ChangeSetID, &b.ClientID, &b.Path, &b.BackupPath, &b.FileExisted); err != nil {
			continue
		}
		backups = append(backups, b)
	}

	return backups
}

// getBinding retrieves binding information.
func (a *baseAdapter) getBinding(bindingID string) *BindingInfo {
	row := a.db.QueryRow(`
		SELECT binding_id, instance_id, client_id, config_path, scope
		FROM client_bindings
		WHERE binding_id = ?
	`, bindingID)

	var b BindingInfo
	if err := row.Scan(&b.BindingID, &b.InstanceID, &b.ClientID, &b.ConfigPath, &b.Scope); err != nil {
		return nil
	}

	return &b
}

// generateBackupID generates a unique backup ID.
func generateBackupID() string {
	return "bak_" + randomString(12)
}

// randomString generates a random string of length n.
func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		// Simple random for ID generation
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

// homeDir returns the user's home directory.
func homeDir() string {
	dir, _ := os.UserHomeDir()
	return dir
}

// conduitDir returns the Conduit data directory.
func conduitDir() string {
	return filepath.Join(homeDir(), ".conduit")
}

// backupDir returns the backup directory for a change set.
func backupDir(changeSetID string) string {
	return filepath.Join(conduitDir(), "backups", changeSetID)
}
