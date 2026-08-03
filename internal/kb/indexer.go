package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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
//
// When semantic search is enabled the chunk text, its embedding and its
// metadata are written in a SINGLE transaction, so a crash can never leave the
// FTS index and the vector index disagreeing about what is indexed.
//
// Embeddings are generated BEFORE the transaction opens. They are a network
// round trip to Ollama, and SQLite allows one writer at a time: holding the
// write lock across that call would stall every other writer for seconds. If
// embedding fails, ingestion proceeds without vectors -- lexical search still
// works, and the failure is counted for reporting.
func (idx *Indexer) Index(ctx context.Context, doc *Document, chunks []Chunk) error {
	// Chunk IDs must be known before embedding so the vector rows can be keyed
	// by them inside the transaction.
	//
	// The id is recomputed rather than trusted: a caller may have chunked
	// without setting ChunkOptions.DocumentID, and the document identity has to
	// be in the hash. ChunkID is the same function Chunker uses, so a chunker
	// that was told the document id produces exactly these values (issue #72 was
	// two id functions that could disagree).
	chunksWithIDs := make([]Chunk, len(chunks))
	for i, chunk := range chunks {
		chunksWithIDs[i] = chunk
		chunksWithIDs[i].ChunkID = ChunkID(doc.DocumentID, i, chunk.Content)
	}

	var embeddings [][]float32
	if idx.semantic != nil && len(chunksWithIDs) > 0 {
		var embErr error
		embeddings, embErr = idx.semantic.EmbedChunks(ctx, chunksWithIDs)
		if embErr != nil {
			idx.logger.Warn().
				Err(embErr).
				Str("document_id", doc.DocumentID).
				Msg("embedding failed, indexing lexical content only")
			idx.semanticErrors++
			embeddings = nil
		}
	}

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

	// Insert chunks with unique IDs that include document context
	for i, chunk := range chunks {
		uniqueChunkID := chunksWithIDs[i].ChunkID

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

	// Write vectors in the SAME transaction as the chunk text and metadata.
	// A vector write failure aborts the whole document rather than committing a
	// half-indexed state; the caller retries the document as a unit.
	if idx.semantic != nil && embeddings != nil {
		if err := idx.semantic.IndexVectorsTx(ctx, tx, doc, chunksWithIDs, embeddings); err != nil {
			return fmt.Errorf("index vectors: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
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

	// Delete vectors in the same transaction. The foreign key on kb_vectors
	// would cascade from kb_chunks anyway, but doing it explicitly keeps the
	// behaviour identical whether or not foreign_keys is enabled on the
	// connection, and tolerates a database that predates migration 005.
	if _, err := tx.ExecContext(ctx, `DELETE FROM kb_vectors WHERE document_id = ?`, documentID); err != nil {
		if !isMissingTableErr(err) {
			return fmt.Errorf("delete vectors: %w", err)
		}
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

// isMissingTableErr reports whether err is SQLite complaining that a table does
// not exist, which is the expected shape on a database that predates the
// migration introducing it.
func isMissingTableErr(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no such table")
}

// ErrDocumentNotFound reports that no indexed document matched a lookup.
//
// It carries no detail about the lookup key or the corpus, so a caller can
// decide for itself how much to say. Issue #91: the MCP server answers a path
// miss with a fixed message rather than reflecting anything back to the client.
var ErrDocumentNotFound = errors.New("document not found")

// GetDocument retrieves a document by ID.
func (idx *Indexer) GetDocument(ctx context.Context, documentID string) (*Document, error) {
	doc, err := idx.getDocumentBy(ctx, "document_id", documentID)
	if errors.Is(err, ErrDocumentNotFound) {
		// Message text is unchanged from before this became a shared helper:
		// "document not found: <id>".
		return nil, fmt.Errorf("%w: %s", ErrDocumentNotFound, documentID)
	}
	return doc, err
}

// GetDocumentByPath retrieves a document by its indexed path (#91).
//
// kb_documents.path is UNIQUE, so at most one row can match. Paths are stored
// absolute and matched exactly: no filesystem resolution, no cleaning, no
// relative-path guessing happens here, because a lookup key must not be able to
// reach outside what is already indexed. A miss returns ErrDocumentNotFound.
func (idx *Indexer) GetDocumentByPath(ctx context.Context, path string) (*Document, error) {
	return idx.getDocumentBy(ctx, "path", path)
}

// getDocumentBy loads one document row by a unique column.
//
// column is never caller-supplied: it comes from the fixed set of literals used
// by GetDocument and GetDocumentByPath, both of which name a UNIQUE column. The
// value is always bound as a parameter.
func (idx *Indexer) getDocumentBy(ctx context.Context, column, value string) (*Document, error) {
	row := idx.db.QueryRowContext(ctx, `
		SELECT document_id, source_id, path, title, mime_type, size,
		       modified_at, indexed_at, hash, metadata, chunk_count
		FROM kb_documents
		WHERE `+column+` = ?
	`, value)

	var doc Document
	var modifiedAt, indexedAt sql.NullString
	var metadata string

	err := row.Scan(
		&doc.DocumentID, &doc.SourceID, &doc.Path, &doc.Title, &doc.MimeType,
		&doc.Size, &modifiedAt, &indexedAt, &doc.Hash, &metadata, &doc.ChunkCount,
	)
	if err == sql.ErrNoRows {
		return nil, ErrDocumentNotFound
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
	TotalEntities  int  `json:"total_entities,omitempty"`
	TotalRelations int  `json:"total_relations,omitempty"`
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

// WP-3.4 deleted Indexer.generateUniqueChunkID. It was the second of the two
// chunk-id functions tracked as issue #72; the surviving one is kb.ChunkID in
// chunker.go, which hashes the identical "documentID:index:content" payload.
