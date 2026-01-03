// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// entity_extractor.go orchestrates entity and relation extraction from document chunks.
package kb

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// EntityExtractor orchestrates entity and relation extraction from chunks.
type EntityExtractor struct {
	provider       LLMProvider
	db             *sql.DB
	graphStore     *FalkorDBStore
	config         KAGConfig
	validator      *ExtractionValidator
	logger         zerolog.Logger
	mu             sync.Mutex
	extractionJobs chan extractionJob
	workerWg       sync.WaitGroup
	stopCh         chan struct{}
}

// EntityExtractorConfig holds configuration for the extractor.
type EntityExtractorConfig struct {
	Provider   LLMProvider
	DB         *sql.DB
	GraphStore *FalkorDBStore
	Config     KAGConfig
	NumWorkers int
}

// extractionJob represents a chunk to be processed.
type extractionJob struct {
	ChunkID       string
	DocumentID    string
	DocumentTitle string
	Content       string
}

// NewEntityExtractor creates a new entity extractor.
func NewEntityExtractor(cfg EntityExtractorConfig) (*EntityExtractor, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("LLM provider is required")
	}
	if cfg.DB == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	numWorkers := cfg.NumWorkers
	if numWorkers <= 0 {
		numWorkers = 2 // Default to 2 concurrent extractions
	}

	logger := observability.Logger("kb.extractor")

	extractor := &EntityExtractor{
		provider:       cfg.Provider,
		db:             cfg.DB,
		graphStore:     cfg.GraphStore,
		config:         cfg.Config,
		validator:      NewExtractionValidator(),
		logger:         logger,
		extractionJobs: make(chan extractionJob, 100),
		stopCh:         make(chan struct{}),
	}

	// Start background workers if enabled
	if cfg.Config.Extraction.EnableBackground {
		for i := 0; i < numWorkers; i++ {
			extractor.workerWg.Add(1)
			go extractor.worker(i)
		}
	}

	return extractor, nil
}

// ExtractFromChunk extracts entities and relations from a single chunk.
func (e *EntityExtractor) ExtractFromChunk(ctx context.Context, chunkID, documentID, documentTitle, content string) (*ExtractionResult, error) {
	startTime := time.Now()

	// Build extraction request
	req := &ExtractionRequest{
		ChunkID:             chunkID,
		DocumentID:          documentID,
		DocumentTitle:       documentTitle,
		Content:             content,
		MaxEntities:         e.config.Extraction.MaxEntitiesPerChunk,
		MaxRelations:        e.config.Extraction.MaxRelationsPerChunk,
		ConfidenceThreshold: e.config.Extraction.ConfidenceThreshold,
	}

	// Call LLM provider
	resp, err := e.provider.ExtractEntities(ctx, req)
	if err != nil {
		e.updateExtractionStatus(chunkID, "error", 0, 0, err.Error())
		return &ExtractionResult{
			ChunkID:    chunkID,
			DocumentID: documentID,
			Error:      err.Error(),
		}, err
	}

	// Validate extracted entities and relations
	validatedEntities := make([]Entity, 0, len(resp.Entities))
	for _, extracted := range resp.Entities {
		entity := e.validator.ValidateAndConvertEntity(extracted, chunkID, documentID)
		if entity != nil {
			validatedEntities = append(validatedEntities, *entity)
		}
	}

	validatedRelations := make([]Relation, 0, len(resp.Relations))
	for _, extracted := range resp.Relations {
		relation := e.validator.ValidateAndConvertRelation(extracted, chunkID, validatedEntities)
		if relation != nil {
			validatedRelations = append(validatedRelations, *relation)
		}
	}

	// Store in SQLite
	if err := e.storeEntities(ctx, validatedEntities); err != nil {
		e.logger.Warn().Err(err).Str("chunk_id", chunkID).Msg("failed to store entities in SQLite")
	}

	if err := e.storeRelations(ctx, validatedRelations); err != nil {
		e.logger.Warn().Err(err).Str("chunk_id", chunkID).Msg("failed to store relations in SQLite")
	}

	// Store in graph database if available
	if e.graphStore != nil && e.graphStore.IsConnected() {
		for _, entity := range validatedEntities {
			if err := e.graphStore.CreateEntity(ctx, &entity); err != nil {
				e.logger.Debug().Err(err).Str("entity_id", entity.ID).Msg("failed to store entity in graph")
			}
		}
		for _, relation := range validatedRelations {
			if err := e.graphStore.CreateRelation(ctx, &relation); err != nil {
				e.logger.Debug().Err(err).Str("relation_id", relation.ID).Msg("failed to store relation in graph")
			}
		}
	}

	// Update extraction status
	processingTimeMs := time.Since(startTime).Milliseconds()
	e.updateExtractionStatus(chunkID, "completed", len(validatedEntities), len(validatedRelations), "")

	return &ExtractionResult{
		ChunkID:          chunkID,
		DocumentID:       documentID,
		Entities:         validatedEntities,
		Relations:        validatedRelations,
		ProcessingTimeMs: processingTimeMs,
	}, nil
}

// QueueChunk adds a chunk to the background extraction queue.
func (e *EntityExtractor) QueueChunk(chunkID, documentID, documentTitle, content string) error {
	select {
	case e.extractionJobs <- extractionJob{
		ChunkID:       chunkID,
		DocumentID:    documentID,
		DocumentTitle: documentTitle,
		Content:       content,
	}:
		e.updateExtractionStatus(chunkID, "queued", 0, 0, "")
		return nil
	default:
		return fmt.Errorf("extraction queue is full")
	}
}

// worker processes extraction jobs from the queue.
func (e *EntityExtractor) worker(id int) {
	defer e.workerWg.Done()

	e.logger.Debug().Int("worker_id", id).Msg("extraction worker started")

	for {
		select {
		case <-e.stopCh:
			e.logger.Debug().Int("worker_id", id).Msg("extraction worker stopping")
			return
		case job := <-e.extractionJobs:
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(e.config.Extraction.TimeoutSeconds)*time.Second)

			_, err := e.ExtractFromChunk(ctx, job.ChunkID, job.DocumentID, job.DocumentTitle, job.Content)
			if err != nil {
				e.logger.Warn().Err(err).Str("chunk_id", job.ChunkID).Int("worker_id", id).Msg("extraction failed")
			} else {
				e.logger.Debug().Str("chunk_id", job.ChunkID).Int("worker_id", id).Msg("extraction completed")
			}

			cancel()
		}
	}
}

// Stop stops the background workers.
func (e *EntityExtractor) Stop() {
	close(e.stopCh)
	e.workerWg.Wait()
}

// Close releases resources.
func (e *EntityExtractor) Close() error {
	e.Stop()
	return e.provider.Close()
}

// storeEntities stores entities in SQLite.
func (e *EntityExtractor) storeEntities(ctx context.Context, entities []Entity) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO kb_entities
		(entity_id, name, type, description, source_chunk_id, source_document_id, confidence, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Format(time.RFC3339)
	for _, entity := range entities {
		metadataJSON := "{}"
		if entity.Metadata != nil {
			// Serialize metadata to JSON
			if data, err := serializeMetadata(entity.Metadata); err == nil {
				metadataJSON = data
			}
		}

		_, err := stmt.ExecContext(ctx,
			entity.ID,
			entity.Name,
			string(entity.Type),
			entity.Description,
			entity.SourceChunkID,
			entity.SourceDocumentID,
			entity.Confidence,
			metadataJSON,
			now,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// storeRelations stores relations in SQLite.
func (e *EntityExtractor) storeRelations(ctx context.Context, relations []Relation) error {
	tx, err := e.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR REPLACE INTO kb_relations
		(relation_id, subject_id, predicate, object_id, source_chunk_id, confidence, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now().Format(time.RFC3339)
	for _, relation := range relations {
		metadataJSON := "{}"
		if relation.Metadata != nil {
			if data, err := serializeMetadata(relation.Metadata); err == nil {
				metadataJSON = data
			}
		}

		_, err := stmt.ExecContext(ctx,
			relation.ID,
			relation.SubjectID,
			string(relation.Predicate),
			relation.ObjectID,
			relation.SourceChunkID,
			relation.Confidence,
			metadataJSON,
			now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// updateExtractionStatus updates the extraction status in the database.
func (e *EntityExtractor) updateExtractionStatus(chunkID, status string, entityCount, relationCount int, errorMsg string) {
	now := time.Now().Format(time.RFC3339)

	_, err := e.db.Exec(`
		INSERT OR REPLACE INTO kb_extraction_status
		(chunk_id, status, entity_count, relation_count, error_message, extracted_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, chunkID, status, entityCount, relationCount, errorMsg, now, now)

	if err != nil {
		e.logger.Warn().Err(err).Str("chunk_id", chunkID).Msg("failed to update extraction status")
	}
}

// GetExtractionStatus returns the extraction status for a chunk.
func (e *EntityExtractor) GetExtractionStatus(ctx context.Context, chunkID string) (string, error) {
	var status string
	err := e.db.QueryRowContext(ctx,
		"SELECT status FROM kb_extraction_status WHERE chunk_id = ?",
		chunkID,
	).Scan(&status)

	if err == sql.ErrNoRows {
		return "pending", nil
	}
	return status, err
}

// GetExtractionStats returns overall extraction statistics.
func (e *EntityExtractor) GetExtractionStats(ctx context.Context) (map[string]int64, error) {
	stats := make(map[string]int64)

	// Count by status
	rows, err := e.db.QueryContext(ctx, `
		SELECT status, COUNT(*) as count
		FROM kb_extraction_status
		GROUP BY status
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int64
		if err := rows.Scan(&status, &count); err != nil {
			continue
		}
		stats[status] = count
	}

	// Total entities
	var entityCount int64
	e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_entities").Scan(&entityCount)
	stats["total_entities"] = entityCount

	// Total relations
	var relationCount int64
	e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_relations").Scan(&relationCount)
	stats["total_relations"] = relationCount

	return stats, nil
}

// GenerateEntityID generates a unique ID for an entity.
func GenerateEntityID(name, entityType, documentID string) string {
	h := sha256.New()
	h.Write([]byte(name + "|" + entityType + "|" + documentID))
	return "ent_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// GenerateRelationID generates a unique ID for a relation.
func GenerateRelationID(subjectID, predicate, objectID string) string {
	h := sha256.New()
	h.Write([]byte(subjectID + "|" + predicate + "|" + objectID))
	return "rel_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// serializeMetadata converts metadata map to JSON string.
func serializeMetadata(metadata map[string]interface{}) (string, error) {
	if metadata == nil {
		return "{}", nil
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "{}", err
	}
	return string(data), nil
}

