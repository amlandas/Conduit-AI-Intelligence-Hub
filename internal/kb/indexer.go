package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/simpleflo/conduit/internal/observability"
	"github.com/rs/zerolog"
)

// Indexer manages document indexing in SQLite with FTS5.
type Indexer struct {
	db     *sql.DB
	logger zerolog.Logger
}

// NewIndexer creates a new indexer.
func NewIndexer(db *sql.DB) *Indexer {
	return &Indexer{
		db:     db,
		logger: observability.Logger("kb.indexer"),
	}
}

// Index indexes a document and its chunks.
func (idx *Indexer) Index(ctx context.Context, doc *Document, chunks []Chunk) error {
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing document and chunks if updating
	if err := idx.deleteInTx(ctx, tx, doc.DocumentID); err != nil {
		return fmt.Errorf("delete existing: %w", err)
	}

	// Insert document
	metadataJSON, _ := json.Marshal(doc.Metadata)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO kb_documents
		(document_id, source_id, path, title, mime_type, size, modified_at, indexed_at, hash, metadata, chunk_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'), ?, ?, ?)
	`, doc.DocumentID, doc.SourceID, doc.Path, doc.Title, doc.MimeType,
		doc.Size, doc.ModifiedAt.Format("2006-01-02 15:04:05"),
		doc.Hash, string(metadataJSON), len(chunks))

	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	// Insert chunks
	for _, chunk := range chunks {
		chunkMetaJSON, _ := json.Marshal(chunk.Metadata)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO kb_chunks
			(chunk_id, document_id, chunk_index, content, start_char, end_char, metadata)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, chunk.ChunkID, doc.DocumentID, chunk.Index, chunk.Content,
			chunk.StartChar, chunk.EndChar, string(chunkMetaJSON))

		if err != nil {
			return fmt.Errorf("insert chunk %d: %w", chunk.Index, err)
		}

		// Insert into FTS index
		_, err = tx.ExecContext(ctx, `
			INSERT INTO kb_fts (rowid, document_id, chunk_id, content, title, path)
			VALUES (
				(SELECT rowid FROM kb_chunks WHERE chunk_id = ?),
				?, ?, ?, ?, ?
			)
		`, chunk.ChunkID, doc.DocumentID, chunk.ChunkID, chunk.Content, doc.Title, doc.Path)

		if err != nil {
			// Try alternative insert without rowid reference
			_, err = tx.ExecContext(ctx, `
				INSERT INTO kb_fts (document_id, chunk_id, content, title, path)
				VALUES (?, ?, ?, ?, ?)
			`, doc.DocumentID, chunk.ChunkID, chunk.Content, doc.Title, doc.Path)
			if err != nil {
				return fmt.Errorf("insert FTS %d: %w", chunk.Index, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	idx.logger.Debug().
		Str("document_id", doc.DocumentID).
		Str("path", doc.Path).
		Int("chunks", len(chunks)).
		Msg("indexed document")

	return nil
}

// Delete removes a document and its chunks from the index.
func (idx *Indexer) Delete(ctx context.Context, documentID string) error {
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := idx.deleteInTx(ctx, tx, documentID); err != nil {
		return err
	}

	return tx.Commit()
}

// deleteInTx deletes a document within a transaction.
func (idx *Indexer) deleteInTx(ctx context.Context, tx *sql.Tx, documentID string) error {
	// Delete from FTS first
	_, err := tx.ExecContext(ctx, `DELETE FROM kb_fts WHERE document_id = ?`, documentID)
	if err != nil {
		return fmt.Errorf("delete fts: %w", err)
	}

	// Delete chunks
	_, err = tx.ExecContext(ctx, `DELETE FROM kb_chunks WHERE document_id = ?`, documentID)
	if err != nil {
		return fmt.Errorf("delete chunks: %w", err)
	}

	// Delete document
	_, err = tx.ExecContext(ctx, `DELETE FROM kb_documents WHERE document_id = ?`, documentID)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}

	return nil
}

// GetDocument retrieves a document by ID.
func (idx *Indexer) GetDocument(ctx context.Context, documentID string) (*Document, error) {
	row := idx.db.QueryRowContext(ctx, `
		SELECT document_id, source_id, path, title, mime_type, size,
		       modified_at, indexed_at, hash, metadata, chunk_count
		FROM kb_documents
		WHERE document_id = ?
	`, documentID)

	var doc Document
	var modifiedAt, indexedAt sql.NullString
	var metadata string

	err := row.Scan(
		&doc.DocumentID, &doc.SourceID, &doc.Path, &doc.Title, &doc.MimeType,
		&doc.Size, &modifiedAt, &indexedAt, &doc.Hash, &metadata, &doc.ChunkCount,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("document not found: %s", documentID)
	}
	if err != nil {
		return nil, fmt.Errorf("scan document: %w", err)
	}

	if modifiedAt.Valid {
		doc.ModifiedAt, _ = time.Parse("2006-01-02 15:04:05", modifiedAt.String)
	}
	if indexedAt.Valid {
		doc.IndexedAt, _ = time.Parse("2006-01-02 15:04:05", indexedAt.String)
	}
	json.Unmarshal([]byte(metadata), &doc.Metadata)

	return &doc, nil
}

// GetChunks retrieves chunks for a document.
func (idx *Indexer) GetChunks(ctx context.Context, documentID string) ([]Chunk, error) {
	rows, err := idx.db.QueryContext(ctx, `
		SELECT chunk_id, chunk_index, content, start_char, end_char, metadata
		FROM kb_chunks
		WHERE document_id = ?
		ORDER BY chunk_index
	`, documentID)
	if err != nil {
		return nil, fmt.Errorf("query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var chunk Chunk
		var metadata string

		if err := rows.Scan(
			&chunk.ChunkID, &chunk.Index, &chunk.Content,
			&chunk.StartChar, &chunk.EndChar, &metadata,
		); err != nil {
			continue
		}

		json.Unmarshal([]byte(metadata), &chunk.Metadata)
		chunks = append(chunks, chunk)
	}

	return chunks, rows.Err()
}

// GetStats returns indexing statistics.
func (idx *Indexer) GetStats(ctx context.Context) (*IndexStats, error) {
	stats := &IndexStats{}

	row := idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_documents`)
	row.Scan(&stats.TotalDocuments)

	row = idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_chunks`)
	row.Scan(&stats.TotalChunks)

	row = idx.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(size), 0) FROM kb_documents`)
	row.Scan(&stats.TotalBytes)

	row = idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_sources`)
	row.Scan(&stats.TotalSources)

	return stats, nil
}

// IndexStats contains indexing statistics.
type IndexStats struct {
	TotalSources   int   `json:"total_sources"`
	TotalDocuments int   `json:"total_documents"`
	TotalChunks    int   `json:"total_chunks"`
	TotalBytes     int64 `json:"total_bytes"`
}

// Optimize runs VACUUM and other optimizations on the database.
func (idx *Indexer) Optimize(ctx context.Context) error {
	// Optimize FTS index
	_, err := idx.db.ExecContext(ctx, `INSERT INTO kb_fts(kb_fts) VALUES('optimize')`)
	if err != nil {
		idx.logger.Warn().Err(err).Msg("FTS optimize failed")
	}

	// Analyze tables
	_, err = idx.db.ExecContext(ctx, `ANALYZE kb_documents`)
	if err != nil {
		return fmt.Errorf("analyze documents: %w", err)
	}

	_, err = idx.db.ExecContext(ctx, `ANALYZE kb_chunks`)
	if err != nil {
		return fmt.Errorf("analyze chunks: %w", err)
	}

	idx.logger.Info().Msg("index optimized")
	return nil
}

// Rebuild rebuilds the FTS index from chunks.
func (idx *Indexer) Rebuild(ctx context.Context) error {
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear FTS
	_, err = tx.ExecContext(ctx, `DELETE FROM kb_fts`)
	if err != nil {
		return fmt.Errorf("clear fts: %w", err)
	}

	// Rebuild from chunks
	_, err = tx.ExecContext(ctx, `
		INSERT INTO kb_fts (document_id, chunk_id, content, title, path)
		SELECT c.document_id, c.chunk_id, c.content, d.title, d.path
		FROM kb_chunks c
		JOIN kb_documents d ON c.document_id = d.document_id
	`)
	if err != nil {
		return fmt.Errorf("rebuild fts: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	idx.logger.Info().Msg("FTS index rebuilt")
	return nil
}
