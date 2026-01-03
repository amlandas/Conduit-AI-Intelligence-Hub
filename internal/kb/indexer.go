package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// Indexer manages document indexing in SQLite with FTS5 and optional vector search.
type Indexer struct {
	db               *sql.DB
	semantic         *SemanticSearcher
	entityExtractor  *EntityExtractor
	kagConfig        KAGConfig
	logger           zerolog.Logger
	semanticErrors   int // Counter for semantic indexing failures in current batch
	extractionErrors int // Counter for KAG extraction failures in current batch
}

// NewIndexer creates a new indexer.
func NewIndexer(db *sql.DB) *Indexer {
	return &Indexer{
		db:     db,
		logger: observability.Logger("kb.indexer"),
	}
}

// SetSemanticSearcher enables vector-based semantic search.
// When set, documents will be indexed into both FTS5 and the vector store.
func (idx *Indexer) SetSemanticSearcher(semantic *SemanticSearcher) {
	idx.semantic = semantic
	idx.logger.Info().Msg("semantic search enabled for indexer")
}

// HasSemanticSearch returns whether semantic search is enabled.
func (idx *Indexer) HasSemanticSearch() bool {
	return idx.semantic != nil
}

// SetEntityExtractor enables KAG entity extraction during indexing.
// When set, documents will also have entities extracted and stored in the graph.
func (idx *Indexer) SetEntityExtractor(extractor *EntityExtractor, config KAGConfig) {
	idx.entityExtractor = extractor
	idx.kagConfig = config
	idx.logger.Info().Msg("KAG entity extraction enabled for indexer")
}

// HasEntityExtraction returns whether KAG entity extraction is enabled.
func (idx *Indexer) HasEntityExtraction() bool {
	return idx.entityExtractor != nil && idx.kagConfig.Enabled
}

// ResetSemanticErrors resets the semantic error counter.
// Call this before starting a batch operation to track errors for that batch.
func (idx *Indexer) ResetSemanticErrors() {
	idx.semanticErrors = 0
}

// GetSemanticErrors returns the number of semantic indexing failures since last reset.
func (idx *Indexer) GetSemanticErrors() int {
	return idx.semanticErrors
}

// ResetExtractionErrors resets the KAG extraction error counter.
func (idx *Indexer) ResetExtractionErrors() {
	idx.extractionErrors = 0
}

// GetExtractionErrors returns the number of KAG extraction failures since last reset.
func (idx *Indexer) GetExtractionErrors() int {
	return idx.extractionErrors
}

// Index indexes a document and its chunks.
// If semantic search is enabled, it also generates and stores vector embeddings.
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

	// Create a copy of chunks with unique IDs for vector indexing
	chunksWithIDs := make([]Chunk, len(chunks))

	// Insert chunks with unique IDs that include document context
	for i, chunk := range chunks {
		// Generate unique chunk ID that includes document ID to avoid collisions
		uniqueChunkID := idx.generateUniqueChunkID(doc.DocumentID, chunk.Content, i)

		// Store chunk with ID for vector indexing
		chunksWithIDs[i] = chunk
		chunksWithIDs[i].ChunkID = uniqueChunkID

		chunkMetaJSON, _ := json.Marshal(chunk.Metadata)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO kb_chunks
			(chunk_id, document_id, chunk_index, content, start_char, end_char, metadata)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, uniqueChunkID, doc.DocumentID, chunk.Index, chunk.Content,
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
		`, uniqueChunkID, doc.DocumentID, uniqueChunkID, chunk.Content, doc.Title, doc.Path)

		if err != nil {
			// Try alternative insert without rowid reference
			_, err = tx.ExecContext(ctx, `
				INSERT INTO kb_fts (document_id, chunk_id, content, title, path)
				VALUES (?, ?, ?, ?, ?)
			`, doc.DocumentID, uniqueChunkID, chunk.Content, doc.Title, doc.Path)
			if err != nil {
				return fmt.Errorf("insert FTS %d: %w", chunk.Index, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Index vectors if semantic search is enabled
	if idx.semantic != nil {
		if err := idx.semantic.IndexDocument(ctx, doc, chunksWithIDs); err != nil {
			// Log warning but don't fail - FTS indexing succeeded
			idx.logger.Warn().
				Err(err).
				Str("document_id", doc.DocumentID).
				Msg("vector indexing failed, falling back to FTS only")
			idx.semanticErrors++ // Track for reporting
		} else {
			idx.logger.Debug().
				Str("document_id", doc.DocumentID).
				Int("vectors", len(chunksWithIDs)).
				Msg("indexed document vectors")
		}
	}

	// Queue entity extraction if KAG is enabled
	if idx.HasEntityExtraction() {
		for _, chunk := range chunksWithIDs {
			// Queue chunk for background extraction (non-blocking)
			if idx.kagConfig.Extraction.EnableBackground {
				if err := idx.entityExtractor.QueueChunk(chunk.ChunkID, doc.DocumentID, doc.Title, chunk.Content); err != nil {
					idx.logger.Debug().
						Err(err).
						Str("chunk_id", chunk.ChunkID).
						Msg("failed to queue chunk for extraction, queue may be full")
					idx.extractionErrors++
				}
			} else {
				// Synchronous extraction (slower but immediate)
				_, err := idx.entityExtractor.ExtractFromChunk(ctx, chunk.ChunkID, doc.DocumentID, doc.Title, chunk.Content)
				if err != nil {
					idx.logger.Debug().
						Err(err).
						Str("chunk_id", chunk.ChunkID).
						Msg("entity extraction failed for chunk")
					idx.extractionErrors++
				}
			}
		}
		idx.logger.Debug().
			Str("document_id", doc.DocumentID).
			Int("chunks_queued", len(chunksWithIDs)).
			Bool("background", idx.kagConfig.Extraction.EnableBackground).
			Msg("queued document for entity extraction")
	}

	idx.logger.Debug().
		Str("document_id", doc.DocumentID).
		Str("path", doc.Path).
		Int("chunks", len(chunks)).
		Bool("semantic", idx.semantic != nil).
		Bool("kag", idx.HasEntityExtraction()).
		Msg("indexed document")

	return nil
}

// Delete removes a document and its chunks from the index.
// If semantic search is enabled, it also removes vector embeddings.
func (idx *Indexer) Delete(ctx context.Context, documentID string) error {
	tx, err := idx.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := idx.deleteInTx(ctx, tx, documentID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	// Delete vectors if semantic search is enabled
	if idx.semantic != nil {
		if err := idx.semantic.DeleteDocument(ctx, documentID); err != nil {
			idx.logger.Warn().
				Err(err).
				Str("document_id", documentID).
				Msg("failed to delete document vectors")
		}
	}

	return nil
}

// DeleteBySource removes all vectors for a source (KB) from the vector store.
// This is called when removing a KB to clean up all associated vectors.
// Returns the number of vectors deleted, or 0 if semantic search is not enabled.
func (idx *Indexer) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
	if idx.semantic == nil {
		// Semantic search not enabled, no vectors to delete
		return 0, nil
	}

	deleted, err := idx.semantic.DeleteBySource(ctx, sourceID)
	if err != nil {
		idx.logger.Warn().
			Err(err).
			Str("source_id", sourceID).
			Msg("failed to delete source vectors")
		return 0, err
	}

	idx.logger.Info().
		Str("source_id", sourceID).
		Int("deleted", deleted).
		Msg("deleted source vectors")

	return deleted, nil
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

	// KAG statistics
	stats.KAGEnabled = idx.HasEntityExtraction()
	if stats.KAGEnabled {
		row = idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_entities`)
		row.Scan(&stats.TotalEntities)

		row = idx.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_relations`)
		row.Scan(&stats.TotalRelations)
	}

	return stats, nil
}

// IndexStats contains indexing statistics.
type IndexStats struct {
	TotalSources   int   `json:"total_sources"`
	TotalDocuments int   `json:"total_documents"`
	TotalChunks    int   `json:"total_chunks"`
	TotalBytes     int64 `json:"total_bytes"`
	// KAG statistics
	TotalEntities  int `json:"total_entities,omitempty"`
	TotalRelations int `json:"total_relations,omitempty"`
	KAGEnabled     bool `json:"kag_enabled"`
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

// generateUniqueChunkID generates a globally unique chunk ID by including
// the document ID in the hash. This prevents collisions when identical
// content exists in multiple documents.
func (idx *Indexer) generateUniqueChunkID(documentID, content string, index int) string {
	// Combine document ID, content, and index to ensure uniqueness
	data := fmt.Sprintf("%s:%d:%s", documentID, index, content)
	h := sha256.Sum256([]byte(data))
	return "chunk_" + hex.EncodeToString(h[:8])
}
