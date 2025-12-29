// Package store provides SQLite database operations for Conduit.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store provides database operations for Conduit.
type Store struct {
	db *sql.DB
}

// New creates a new Store with the given database path.
func New(dbPath string) (*Store, error) {
	// Open database with WAL mode for better concurrency
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=ON&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite supports single writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0) // Connections don't expire

	// Verify connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	store := &Store{db: db}

	// Run migrations
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return store, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// DB returns the underlying database connection.
func (s *Store) DB() *sql.DB {
	return s.db
}

// migrate runs all pending database migrations.
func (s *Store) migrate() error {
	// Create migrations table if not exists
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Get current version
	var currentVersion int
	err = s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM migrations").Scan(&currentVersion)
	if err != nil {
		return fmt.Errorf("get current version: %w", err)
	}

	// Run the initial schema migration
	if currentVersion < 1 {
		if err := s.runMigration001(); err != nil {
			return fmt.Errorf("run migration 001: %w", err)
		}
	}

	// Run migration 002 for policy user grants
	if currentVersion < 2 {
		if err := s.runMigration002(); err != nil {
			return fmt.Errorf("run migration 002: %w", err)
		}
	}

	// Run migration 003 for enhanced FTS5 schema
	if currentVersion < 3 {
		if err := s.runMigration003(); err != nil {
			return fmt.Errorf("run migration 003: %w", err)
		}
	}

	return nil
}

// runMigration001 creates the initial schema.
func (s *Store) runMigration001() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Connector instances table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS connector_instances (
			instance_id TEXT PRIMARY KEY,
			package_id TEXT NOT NULL,
			package_version TEXT NOT NULL,
			display_name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'CREATED',
			container_id TEXT,
			socket_path TEXT,
			image_ref TEXT NOT NULL,
			config TEXT,
			granted_perms TEXT,
			audit_result TEXT,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			started_at TEXT,
			stopped_at TEXT,
			last_health_check TEXT,
			health_status TEXT,
			error_message TEXT
		)
	`)
	if err != nil {
		return err
	}

	// Client bindings table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS client_bindings (
			binding_id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL REFERENCES connector_instances(instance_id) ON DELETE CASCADE,
			client_id TEXT NOT NULL,
			scope TEXT NOT NULL DEFAULT 'project',
			config_path TEXT NOT NULL,
			change_set_id TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			validated_at TEXT,
			UNIQUE(instance_id, client_id, config_path)
		)
	`)
	if err != nil {
		return err
	}

	// Config backups table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS config_backups (
			backup_id TEXT PRIMARY KEY,
			change_set_id TEXT NOT NULL,
			client_id TEXT NOT NULL,
			original_path TEXT NOT NULL,
			backup_path TEXT NOT NULL,
			file_existed INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return err
	}

	// Permission grants table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS permission_grants (
			grant_id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL REFERENCES connector_instances(instance_id) ON DELETE CASCADE,
			permission_type TEXT NOT NULL,
			permission_value TEXT NOT NULL,
			granted_at TEXT NOT NULL DEFAULT (datetime('now')),
			granted_by TEXT NOT NULL DEFAULT 'user',
			UNIQUE(instance_id, permission_type, permission_value)
		)
	`)
	if err != nil {
		return err
	}

	// Consent ledger table (append-only audit log)
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS consent_ledger (
			entry_id INTEGER PRIMARY KEY AUTOINCREMENT,
			event_type TEXT NOT NULL,
			entity_type TEXT NOT NULL,
			entity_id TEXT NOT NULL,
			action TEXT NOT NULL,
			details TEXT,
			prev_hash TEXT,
			entry_hash TEXT NOT NULL,
			timestamp TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return err
	}

	// KB sources table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS kb_sources (
			source_id TEXT PRIMARY KEY,
			path TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'folder',
			patterns TEXT,
			excludes TEXT,
			sync_mode TEXT NOT NULL DEFAULT 'watch',
			status TEXT NOT NULL DEFAULT 'active',
			last_sync TEXT,
			doc_count INTEGER DEFAULT 0,
			chunk_count INTEGER DEFAULT 0,
			size_bytes INTEGER DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			error TEXT
		)
	`)
	if err != nil {
		return err
	}

	// KB documents table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS kb_documents (
			document_id TEXT PRIMARY KEY,
			source_id TEXT NOT NULL REFERENCES kb_sources(source_id) ON DELETE CASCADE,
			path TEXT NOT NULL UNIQUE,
			title TEXT,
			mime_type TEXT,
			size INTEGER,
			modified_at TEXT,
			indexed_at TEXT NOT NULL,
			hash TEXT,
			metadata TEXT,
			chunk_count INTEGER DEFAULT 0
		)
	`)
	if err != nil {
		return err
	}

	// KB chunks table
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS kb_chunks (
			chunk_id TEXT PRIMARY KEY,
			document_id TEXT NOT NULL REFERENCES kb_documents(document_id) ON DELETE CASCADE,
			chunk_index INTEGER NOT NULL,
			content TEXT NOT NULL,
			start_char INTEGER,
			end_char INTEGER,
			metadata TEXT
		)
	`)
	if err != nil {
		return err
	}

	// FTS5 virtual table for full-text search
	_, err = tx.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS kb_fts USING fts5(
			chunk_id UNINDEXED,
			document_id UNINDEXED,
			content,
			tokenize='porter unicode61'
		)
	`)
	if err != nil {
		return err
	}

	// Create indexes
	_, err = tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_instances_status ON connector_instances(status);
		CREATE INDEX IF NOT EXISTS idx_instances_package ON connector_instances(package_id);
		CREATE INDEX IF NOT EXISTS idx_bindings_instance ON client_bindings(instance_id);
		CREATE INDEX IF NOT EXISTS idx_bindings_client ON client_bindings(client_id);
		CREATE INDEX IF NOT EXISTS idx_documents_source ON kb_documents(source_id);
		CREATE INDEX IF NOT EXISTS idx_chunks_document ON kb_chunks(document_id);
		CREATE INDEX IF NOT EXISTS idx_ledger_entity ON consent_ledger(entity_type, entity_id);
	`)
	if err != nil {
		return err
	}

	// Record migration
	_, err = tx.Exec("INSERT INTO migrations (version) VALUES (1)")
	if err != nil {
		return err
	}

	return tx.Commit()
}

// Health checks database connectivity.
func (s *Store) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.db.PingContext(ctx)
}

// runMigration002 adds policy user grants table.
func (s *Store) runMigration002() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// User grants table for policy engine
	_, err = tx.Exec(`
		CREATE TABLE IF NOT EXISTS user_grants (
			instance_id TEXT NOT NULL,
			permission_type TEXT NOT NULL,
			grant_data TEXT NOT NULL,
			granted_at TEXT NOT NULL DEFAULT (datetime('now')),
			PRIMARY KEY (instance_id, permission_type)
		)
	`)
	if err != nil {
		return err
	}

	// Create index
	_, err = tx.Exec(`
		CREATE INDEX IF NOT EXISTS idx_user_grants_instance ON user_grants(instance_id)
	`)
	if err != nil {
		return err
	}

	// Record migration
	_, err = tx.Exec("INSERT INTO migrations (version) VALUES (2)")
	if err != nil {
		return err
	}

	return tx.Commit()
}

// runMigration003 updates FTS5 schema with title and path columns.
func (s *Store) runMigration003() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Drop and recreate FTS5 table with enhanced schema
	_, err = tx.Exec(`DROP TABLE IF EXISTS kb_fts`)
	if err != nil {
		return err
	}

	// Recreate FTS5 with title and path columns for better search
	_, err = tx.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS kb_fts USING fts5(
			document_id UNINDEXED,
			chunk_id UNINDEXED,
			content,
			title,
			path,
			tokenize='porter unicode61'
		)
	`)
	if err != nil {
		return err
	}

	// Repopulate FTS from existing chunks
	_, err = tx.Exec(`
		INSERT INTO kb_fts (document_id, chunk_id, content, title, path)
		SELECT c.document_id, c.chunk_id, c.content, d.title, d.path
		FROM kb_chunks c
		JOIN kb_documents d ON c.document_id = d.document_id
	`)
	if err != nil {
		// Ignore error if no data exists yet
	}

	// Record migration
	_, err = tx.Exec("INSERT INTO migrations (version) VALUES (3)")
	if err != nil {
		return err
	}

	return tx.Commit()
}
