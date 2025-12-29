package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
)

// SourceManager manages KB source folders.
type SourceManager struct {
	db       *sql.DB
	indexer  *Indexer
	chunker  *Chunker
	logger   zerolog.Logger
}

// NewSourceManager creates a new source manager.
func NewSourceManager(db *sql.DB) *SourceManager {
	return &SourceManager{
		db:      db,
		indexer: NewIndexer(db),
		chunker: NewChunker(),
		logger:  observability.Logger("kb.source"),
	}
}

// Add adds a new source folder.
func (sm *SourceManager) Add(ctx context.Context, req AddSourceRequest) (*Source, error) {
	// Validate path exists
	info, err := os.Stat(req.Path)
	if err != nil {
		return nil, fmt.Errorf("path not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", req.Path)
	}

	// Normalize path
	absPath, err := filepath.Abs(req.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve path: %w", err)
	}

	// Set defaults
	patterns := req.Patterns
	if len(patterns) == 0 {
		patterns = DefaultPatterns
	}
	excludes := req.Excludes
	if len(excludes) == 0 {
		excludes = DefaultExcludes
	}
	syncMode := req.SyncMode
	if syncMode == "" {
		syncMode = "manual"
	}
	name := req.Name
	if name == "" {
		name = filepath.Base(absPath)
	}

	source := &Source{
		SourceID:  uuid.New().String(),
		Path:      absPath,
		Name:      name,
		Type:      "folder",
		Patterns:  patterns,
		Excludes:  excludes,
		SyncMode:  syncMode,
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	patternsJSON, _ := json.Marshal(source.Patterns)
	excludesJSON, _ := json.Marshal(source.Excludes)

	_, err = sm.db.ExecContext(ctx, `
		INSERT INTO kb_sources
		(source_id, path, name, type, patterns, excludes, sync_mode, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))
	`, source.SourceID, source.Path, source.Name, source.Type,
		string(patternsJSON), string(excludesJSON), source.SyncMode, source.Status)

	if err != nil {
		return nil, fmt.Errorf("insert source: %w", err)
	}

	sm.logger.Info().
		Str("source_id", source.SourceID).
		Str("path", source.Path).
		Str("name", source.Name).
		Msg("added source")

	return source, nil
}

// Remove removes a source and its indexed documents.
func (sm *SourceManager) Remove(ctx context.Context, sourceID string) error {
	// Delete from FTS first (due to foreign key constraints)
	_, err := sm.db.ExecContext(ctx, `
		DELETE FROM kb_fts WHERE document_id IN (
			SELECT document_id FROM kb_documents WHERE source_id = ?
		)
	`, sourceID)
	if err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}

	// Delete chunks
	_, err = sm.db.ExecContext(ctx, `
		DELETE FROM kb_chunks WHERE document_id IN (
			SELECT document_id FROM kb_documents WHERE source_id = ?
		)
	`, sourceID)
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}

	// Delete documents
	_, err = sm.db.ExecContext(ctx, `
		DELETE FROM kb_documents WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return fmt.Errorf("delete documents: %w", err)
	}

	// Delete source
	result, err := sm.db.ExecContext(ctx, `DELETE FROM kb_sources WHERE source_id = ?`, sourceID)
	if err != nil {
		return fmt.Errorf("delete source: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("source not found: %s", sourceID)
	}

	sm.logger.Info().
		Str("source_id", sourceID).
		Msg("removed source")

	return nil
}

// List returns all sources.
func (sm *SourceManager) List(ctx context.Context) ([]*Source, error) {
	rows, err := sm.db.QueryContext(ctx, `
		SELECT source_id, path, name, type, patterns, excludes, sync_mode, status,
		       last_sync, doc_count, chunk_count, size_bytes, created_at, updated_at, error
		FROM kb_sources
		ORDER BY name
	`)
	if err != nil {
		return nil, fmt.Errorf("query sources: %w", err)
	}
	defer rows.Close()

	var sources []*Source
	for rows.Next() {
		var src Source
		var patterns, excludes string
		var lastSync, createdAt, updatedAt sql.NullString
		var errorMsg sql.NullString

		err := rows.Scan(
			&src.SourceID, &src.Path, &src.Name, &src.Type,
			&patterns, &excludes, &src.SyncMode, &src.Status,
			&lastSync, &src.DocCount, &src.ChunkCount, &src.SizeBytes,
			&createdAt, &updatedAt, &errorMsg,
		)
		if err != nil {
			continue
		}

		json.Unmarshal([]byte(patterns), &src.Patterns)
		json.Unmarshal([]byte(excludes), &src.Excludes)

		if lastSync.Valid {
			src.LastSync, _ = time.Parse("2006-01-02 15:04:05", lastSync.String)
		}
		if createdAt.Valid {
			src.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt.String)
		}
		if updatedAt.Valid {
			src.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt.String)
		}
		if errorMsg.Valid {
			src.Error = errorMsg.String
		}

		sources = append(sources, &src)
	}

	return sources, rows.Err()
}

// Get returns a source by ID.
func (sm *SourceManager) Get(ctx context.Context, sourceID string) (*Source, error) {
	row := sm.db.QueryRowContext(ctx, `
		SELECT source_id, path, name, type, patterns, excludes, sync_mode, status,
		       last_sync, doc_count, chunk_count, size_bytes, created_at, updated_at, error
		FROM kb_sources
		WHERE source_id = ?
	`, sourceID)

	var src Source
	var patterns, excludes string
	var lastSync, createdAt, updatedAt sql.NullString
	var errorMsg sql.NullString

	err := row.Scan(
		&src.SourceID, &src.Path, &src.Name, &src.Type,
		&patterns, &excludes, &src.SyncMode, &src.Status,
		&lastSync, &src.DocCount, &src.ChunkCount, &src.SizeBytes,
		&createdAt, &updatedAt, &errorMsg,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("source not found: %s", sourceID)
	}
	if err != nil {
		return nil, fmt.Errorf("scan source: %w", err)
	}

	json.Unmarshal([]byte(patterns), &src.Patterns)
	json.Unmarshal([]byte(excludes), &src.Excludes)

	if lastSync.Valid {
		src.LastSync, _ = time.Parse("2006-01-02 15:04:05", lastSync.String)
	}
	if createdAt.Valid {
		src.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt.String)
	}
	if updatedAt.Valid {
		src.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt.String)
	}
	if errorMsg.Valid {
		src.Error = errorMsg.String
	}

	return &src, nil
}

// Sync synchronizes a source folder.
func (sm *SourceManager) Sync(ctx context.Context, sourceID string) (*SyncResult, error) {
	start := time.Now()

	source, err := sm.Get(ctx, sourceID)
	if err != nil {
		return nil, err
	}

	result := &SyncResult{}

	// Get existing documents for this source
	existingDocs := make(map[string]string) // path -> hash
	rows, err := sm.db.QueryContext(ctx, `
		SELECT path, hash FROM kb_documents WHERE source_id = ?
	`, sourceID)
	if err != nil {
		return nil, fmt.Errorf("query existing docs: %w", err)
	}
	for rows.Next() {
		var path, hash string
		rows.Scan(&path, &hash)
		existingDocs[path] = hash
	}
	rows.Close()

	// Track processed files
	processedFiles := make(map[string]bool)

	// Walk the source directory
	err = filepath.WalkDir(source.Path, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			result.Errors = append(result.Errors, SyncError{Path: path, Message: err.Error()})
			return nil
		}

		// Skip excluded directories
		if d.IsDir() {
			for _, exclude := range source.Excludes {
				if matched, _ := filepath.Match(exclude, d.Name()); matched {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Check if file matches patterns
		if !sm.matchesPatterns(d.Name(), source.Patterns) {
			return nil
		}

		processedFiles[path] = true

		// Read file content
		content, metadata, err := sm.readFile(path)
		if err != nil {
			result.Errors = append(result.Errors, SyncError{Path: path, Message: err.Error()})
			return nil
		}

		// Calculate hash
		hash := sm.hashContent(content)

		// Check if document needs update
		existingHash, exists := existingDocs[path]
		if exists && existingHash == hash {
			// No change
			return nil
		}

		// Create document
		doc := &Document{
			DocumentID: sm.documentID(path),
			SourceID:   sourceID,
			Path:       path,
			Title:      metadata.Title,
			MimeType:   metadata.MimeType,
			Size:       metadata.Size,
			ModifiedAt: metadata.ModifiedAt,
			Hash:       hash,
			Metadata:   metadata.Extra,
		}

		// Chunk content
		chunks := sm.chunker.Chunk(content, ChunkOptions{
			MaxSize:   1000,
			Overlap:   100,
			Splitters: []string{"\n\n", "\n", ". ", " "},
		})

		// Index document
		if err := sm.indexer.Index(ctx, doc, chunks); err != nil {
			result.Errors = append(result.Errors, SyncError{Path: path, Message: err.Error()})
			return nil
		}

		if exists {
			result.Updated++
		} else {
			result.Added++
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("walk directory: %w", err)
	}

	// Delete documents that no longer exist
	for path := range existingDocs {
		if !processedFiles[path] {
			docID := sm.documentID(path)
			if err := sm.indexer.Delete(ctx, docID); err != nil {
				result.Errors = append(result.Errors, SyncError{Path: path, Message: err.Error()})
			} else {
				result.Deleted++
			}
		}
	}

	result.Duration = time.Since(start)

	// Update source stats
	sm.updateSourceStats(ctx, sourceID)

	sm.logger.Info().
		Str("source_id", sourceID).
		Int("added", result.Added).
		Int("updated", result.Updated).
		Int("deleted", result.Deleted).
		Dur("duration", result.Duration).
		Msg("sync completed")

	return result, nil
}

// SyncAll synchronizes all active sources.
func (sm *SourceManager) SyncAll(ctx context.Context) error {
	sources, err := sm.List(ctx)
	if err != nil {
		return err
	}

	for _, src := range sources {
		if src.Status != "active" {
			continue
		}
		if _, err := sm.Sync(ctx, src.SourceID); err != nil {
			sm.logger.Error().Err(err).Str("source_id", src.SourceID).Msg("sync failed")
		}
	}

	return nil
}

// matchesPatterns checks if a filename matches any pattern.
func (sm *SourceManager) matchesPatterns(filename string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
	}
	return false
}

// readFile reads a file and extracts metadata.
func (sm *SourceManager) readFile(path string) (string, *FileMetadata, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}

	// Skip large files
	if info.Size() > 10*1024*1024 { // 10MB
		return "", nil, fmt.Errorf("file too large: %d bytes", info.Size())
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}

	metadata := &FileMetadata{
		Title:      filepath.Base(path),
		MimeType:   sm.detectMimeType(path),
		Size:       info.Size(),
		ModifiedAt: info.ModTime(),
		Extra:      make(map[string]string),
	}

	// Extract title from markdown
	if strings.HasSuffix(path, ".md") {
		if title := extractMarkdownTitle(string(content)); title != "" {
			metadata.Title = title
		}
	}

	return string(content), metadata, nil
}

// detectMimeType returns the MIME type based on extension.
func (sm *SourceManager) detectMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".md":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".go":
		return "text/x-go"
	case ".py":
		return "text/x-python"
	case ".js":
		return "text/javascript"
	case ".ts":
		return "text/typescript"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "text/x-yaml"
	default:
		return "text/plain"
	}
}

// hashContent returns a SHA256 hash of content.
func (sm *SourceManager) hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// documentID generates a consistent document ID from path.
func (sm *SourceManager) documentID(path string) string {
	h := sha256.Sum256([]byte(path))
	return "doc_" + hex.EncodeToString(h[:8])
}

// updateSourceStats updates the source statistics.
func (sm *SourceManager) updateSourceStats(ctx context.Context, sourceID string) {
	sm.db.ExecContext(ctx, `
		UPDATE kb_sources SET
			doc_count = (SELECT COUNT(*) FROM kb_documents WHERE source_id = ?),
			chunk_count = (SELECT COUNT(*) FROM kb_chunks c
			               JOIN kb_documents d ON c.document_id = d.document_id
			               WHERE d.source_id = ?),
			size_bytes = (SELECT COALESCE(SUM(size), 0) FROM kb_documents WHERE source_id = ?),
			last_sync = datetime('now'),
			updated_at = datetime('now')
		WHERE source_id = ?
	`, sourceID, sourceID, sourceID, sourceID)
}

// extractMarkdownTitle extracts the first H1 heading from markdown.
func extractMarkdownTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# ")
		}
	}
	return ""
}
