package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// SemanticSearcher provides semantic search over the knowledge base.
// It combines embedding generation and vector similarity search.
type SemanticSearcher struct {
	embeddings  *EmbeddingService
	vectorStore *VectorStore
	db          *sql.DB // For metadata lookups
	logger      zerolog.Logger
}

// SemanticSearchConfig configures the semantic searcher.
type SemanticSearchConfig struct {
	EmbeddingConfig   EmbeddingConfig
	VectorStoreConfig VectorStoreConfig
}

// NewSemanticSearcher creates a new semantic searcher.
func NewSemanticSearcher(db *sql.DB, cfg SemanticSearchConfig) (*SemanticSearcher, error) {
	// Create embedding service
	embeddings, err := NewEmbeddingService(cfg.EmbeddingConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding service: %w", err)
	}

	// Create vector store
	vectorStore, err := NewVectorStore(cfg.VectorStoreConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create vector store: %w", err)
	}

	return &SemanticSearcher{
		embeddings:  embeddings,
		vectorStore: vectorStore,
		db:          db,
		logger:      observability.Logger("kb.semantic"),
	}, nil
}

// SemanticSearchOptions configures semantic search behavior.
type SemanticSearchOptions struct {
	Limit      int      // Max results (default 10)
	Offset     int      // Pagination offset
	SourceIDs  []string // Filter by source IDs
	MimeTypes  []string // Filter by MIME types (fetched from metadata)
	MinScore   float64  // Minimum similarity score threshold (0-1)
	ContextLen int      // Characters of context for snippets
}

// SemanticSearchResult contains semantic search results.
type SemanticSearchResult struct {
	Results    []SemanticSearchHit `json:"results"`
	TotalHits  int                 `json:"total_hits"`
	Query      string              `json:"query"`
	SearchTime float64             `json:"search_time_ms"`
}

// SemanticSearchHit represents a single semantic search result.
type SemanticSearchHit struct {
	DocumentID   string            `json:"document_id"`
	ChunkID      string            `json:"chunk_id"`
	ChunkIndex   int               `json:"chunk_index"`
	Path         string            `json:"path"`
	Title        string            `json:"title"`
	Snippet      string            `json:"snippet"`
	Score        float64           `json:"score"`       // Similarity score (0-1)
	Confidence   string            `json:"confidence"`  // "high", "medium", "low"
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Search performs a semantic search using the query text.
func (ss *SemanticSearcher) Search(ctx context.Context, query string, opts SemanticSearchOptions) (*SemanticSearchResult, error) {
	start := time.Now()

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.ContextLen <= 0 {
		opts.ContextLen = 300
	}
	if opts.MinScore <= 0 {
		opts.MinScore = 0.3 // Default minimum score for relevance
	}

	// Generate embedding for the query
	queryEmbedding, err := ss.embeddings.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to embed query: %w", err)
	}

	// Search vector store
	vectorOpts := VectorSearchOptions{
		Limit:     opts.Limit * 2, // Fetch more to filter
		Offset:    opts.Offset,
		SourceIDs: opts.SourceIDs,
		MinScore:  opts.MinScore,
	}

	vectorResults, err := ss.vectorStore.Search(ctx, queryEmbedding, vectorOpts)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Convert and enrich results
	hits := make([]SemanticSearchHit, 0, len(vectorResults))
	for _, vr := range vectorResults {
		// Apply MIME type filter if specified (requires metadata lookup)
		if len(opts.MimeTypes) > 0 {
			mimeType, err := ss.getDocumentMimeType(ctx, vr.DocumentID)
			if err == nil && !containsString(opts.MimeTypes, mimeType) {
				continue
			}
		}

		hit := SemanticSearchHit{
			DocumentID: vr.DocumentID,
			ChunkID:    vr.ID,
			ChunkIndex: vr.ChunkIndex,
			Path:       vr.Path,
			Title:      vr.Title,
			Snippet:    ss.createSnippet(vr.Content, opts.ContextLen),
			Score:      float64(vr.Score),
			Confidence: ss.scoreToConfidence(vr.Score),
			Metadata:   vr.Metadata,
		}

		hits = append(hits, hit)

		if len(hits) >= opts.Limit {
			break
		}
	}

	result := &SemanticSearchResult{
		Results:    hits,
		TotalHits:  len(hits),
		Query:      query,
		SearchTime: float64(time.Since(start).Milliseconds()),
	}

	ss.logger.Debug().
		Str("query", query).
		Int("hits", len(hits)).
		Float64("time_ms", result.SearchTime).
		Msg("semantic search completed")

	return result, nil
}

// SearchSimilar finds documents similar to the given document.
func (ss *SemanticSearcher) SearchSimilar(ctx context.Context, documentID string, opts SemanticSearchOptions) (*SemanticSearchResult, error) {
	start := time.Now()

	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	// Get the first chunk's content from the document
	content, err := ss.getDocumentFirstChunk(ctx, documentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get document content: %w", err)
	}

	// Generate embedding for the document content
	embedding, err := ss.embeddings.Embed(ctx, content)
	if err != nil {
		return nil, fmt.Errorf("failed to embed document: %w", err)
	}

	// Search vector store, excluding the source document
	vectorOpts := VectorSearchOptions{
		Limit:     opts.Limit + 5, // Extra to account for filtering
		Offset:    opts.Offset,
		SourceIDs: opts.SourceIDs,
		MinScore:  opts.MinScore,
	}

	vectorResults, err := ss.vectorStore.Search(ctx, embedding, vectorOpts)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Filter and convert results
	hits := make([]SemanticSearchHit, 0, opts.Limit)
	for _, vr := range vectorResults {
		// Skip chunks from the same document
		if vr.DocumentID == documentID {
			continue
		}

		hit := SemanticSearchHit{
			DocumentID: vr.DocumentID,
			ChunkID:    vr.ID,
			ChunkIndex: vr.ChunkIndex,
			Path:       vr.Path,
			Title:      vr.Title,
			Snippet:    ss.createSnippet(vr.Content, 200),
			Score:      float64(vr.Score),
			Confidence: ss.scoreToConfidence(vr.Score),
			Metadata:   vr.Metadata,
		}

		hits = append(hits, hit)

		if len(hits) >= opts.Limit {
			break
		}
	}

	return &SemanticSearchResult{
		Results:    hits,
		TotalHits:  len(hits),
		Query:      fmt.Sprintf("similar to: %s", documentID),
		SearchTime: float64(time.Since(start).Milliseconds()),
	}, nil
}

// IndexDocument indexes a document's chunks into the vector store.
func (ss *SemanticSearcher) IndexDocument(ctx context.Context, doc *Document, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Extract chunk contents
	contents := make([]string, len(chunks))
	for i, chunk := range chunks {
		contents[i] = chunk.Content
	}

	// Generate embeddings for all chunks
	embeddings, err := ss.embeddings.EmbedBatch(ctx, contents)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// Create vector points
	points := make([]VectorPoint, len(chunks))
	for i, chunk := range chunks {
		points[i] = VectorPoint{
			ID:         chunk.ChunkID,
			Vector:     embeddings[i],
			DocumentID: doc.DocumentID,
			ChunkIndex: chunk.Index,
			Path:       doc.Path,
			Title:      doc.Title,
			Content:    chunk.Content,
			Metadata: map[string]string{
				"source_id": doc.SourceID,
				"mime_type": doc.MimeType,
			},
		}
	}

	// Upsert to vector store
	if err := ss.vectorStore.UpsertBatch(ctx, points); err != nil {
		return fmt.Errorf("failed to upsert vectors: %w", err)
	}

	ss.logger.Debug().
		Str("document_id", doc.DocumentID).
		Int("chunks", len(chunks)).
		Msg("indexed document vectors")

	return nil
}

// DeleteDocument removes a document's vectors from the store.
func (ss *SemanticSearcher) DeleteDocument(ctx context.Context, documentID string) error {
	return ss.vectorStore.DeleteByDocument(ctx, documentID)
}

// DeleteBySource removes all vectors for a source (all documents in a KB).
// Returns the number of vectors deleted.
func (ss *SemanticSearcher) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
	deleted, err := ss.vectorStore.DeleteBySource(ctx, sourceID)
	if err != nil {
		return 0, err
	}
	ss.logger.Info().
		Str("source_id", sourceID).
		Int("deleted", deleted).
		Msg("deleted source vectors")
	return deleted, nil
}

// getDocumentMimeType retrieves a document's MIME type from SQLite.
func (ss *SemanticSearcher) getDocumentMimeType(ctx context.Context, documentID string) (string, error) {
	var mimeType string
	err := ss.db.QueryRowContext(ctx,
		`SELECT mime_type FROM kb_documents WHERE document_id = ?`,
		documentID,
	).Scan(&mimeType)
	return mimeType, err
}

// getDocumentFirstChunk retrieves the first chunk content for a document.
func (ss *SemanticSearcher) getDocumentFirstChunk(ctx context.Context, documentID string) (string, error) {
	var content string
	err := ss.db.QueryRowContext(ctx,
		`SELECT content FROM kb_chunks WHERE document_id = ? ORDER BY chunk_index LIMIT 1`,
		documentID,
	).Scan(&content)
	return content, err
}

// createSnippet creates a snippet from content, truncating if necessary.
func (ss *SemanticSearcher) createSnippet(content string, maxLen int) string {
	if len(content) <= maxLen {
		return content
	}

	// Try to break at a sentence boundary
	snippet := content[:maxLen]
	lastPeriod := -1
	for i := len(snippet) - 1; i >= len(snippet)/2; i-- {
		if snippet[i] == '.' || snippet[i] == '!' || snippet[i] == '?' {
			lastPeriod = i
			break
		}
	}

	if lastPeriod > 0 {
		return snippet[:lastPeriod+1]
	}

	// Fall back to word boundary
	lastSpace := -1
	for i := len(snippet) - 1; i >= len(snippet)*3/4; i-- {
		if snippet[i] == ' ' {
			lastSpace = i
			break
		}
	}

	if lastSpace > 0 {
		return snippet[:lastSpace] + "..."
	}

	return snippet + "..."
}

// scoreToConfidence converts a similarity score to a confidence level.
func (ss *SemanticSearcher) scoreToConfidence(score float32) string {
	switch {
	case score >= 0.8:
		return "high"
	case score >= 0.6:
		return "medium"
	default:
		return "low"
	}
}

// containsString checks if a slice contains a string.
func containsString(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// EmbeddingService returns the embedding service for external use.
func (ss *SemanticSearcher) EmbeddingService() *EmbeddingService {
	return ss.embeddings
}

// VectorStore returns the vector store for external use.
func (ss *SemanticSearcher) VectorStore() *VectorStore {
	return ss.vectorStore
}

// HealthCheck verifies both embedding service and vector store are operational.
func (ss *SemanticSearcher) HealthCheck(ctx context.Context) error {
	if err := ss.embeddings.HealthCheck(ctx); err != nil {
		return fmt.Errorf("embedding service: %w", err)
	}
	if err := ss.vectorStore.HealthCheck(ctx); err != nil {
		return fmt.Errorf("vector store: %w", err)
	}
	return nil
}

// GetStats returns combined statistics.
func (ss *SemanticSearcher) GetStats(ctx context.Context) (*SemanticSearchStats, error) {
	vsStats, err := ss.vectorStore.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	return &SemanticSearchStats{
		VectorCount:    vsStats.VectorCount,
		CollectionName: vsStats.CollectionName,
		EmbeddingModel: ss.embeddings.Model(),
		Dimension:      ss.embeddings.Dimension(),
		Status:         vsStats.Status,
	}, nil
}

// SemanticSearchStats contains semantic search statistics.
type SemanticSearchStats struct {
	VectorCount    int    `json:"vector_count"`
	CollectionName string `json:"collection_name"`
	EmbeddingModel string `json:"embedding_model"`
	Dimension      int    `json:"dimension"`
	Status         string `json:"status"`
}

// Close closes the semantic searcher and its resources.
func (ss *SemanticSearcher) Close() error {
	return ss.vectorStore.Close()
}

// MigrateFromFTS migrates existing FTS-indexed documents to vector search.
// This reads all chunks from SQLite and generates embeddings for them.
func (ss *SemanticSearcher) MigrateFromFTS(ctx context.Context, progressFn func(current, total int)) error {
	// Count total documents
	var totalDocs int
	err := ss.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_documents`).Scan(&totalDocs)
	if err != nil {
		return fmt.Errorf("failed to count documents: %w", err)
	}

	if totalDocs == 0 {
		ss.logger.Info().Msg("no documents to migrate")
		return nil
	}

	ss.logger.Info().Int("total", totalDocs).Msg("starting FTS to vector migration")

	// Get all documents
	rows, err := ss.db.QueryContext(ctx, `
		SELECT document_id, source_id, path, title, mime_type
		FROM kb_documents
		ORDER BY document_id
	`)
	if err != nil {
		return fmt.Errorf("failed to query documents: %w", err)
	}
	defer rows.Close()

	current := 0
	for rows.Next() {
		var doc Document
		if err := rows.Scan(&doc.DocumentID, &doc.SourceID, &doc.Path, &doc.Title, &doc.MimeType); err != nil {
			ss.logger.Warn().Err(err).Msg("failed to scan document")
			continue
		}

		// Get chunks for this document
		chunks, err := ss.getDocumentChunks(ctx, doc.DocumentID)
		if err != nil {
			ss.logger.Warn().Err(err).Str("doc_id", doc.DocumentID).Msg("failed to get chunks")
			continue
		}

		// Index the document
		if err := ss.IndexDocument(ctx, &doc, chunks); err != nil {
			ss.logger.Warn().Err(err).Str("doc_id", doc.DocumentID).Msg("failed to index document")
			continue
		}

		current++
		if progressFn != nil {
			progressFn(current, totalDocs)
		}

		if current%10 == 0 {
			ss.logger.Info().
				Int("current", current).
				Int("total", totalDocs).
				Msg("migration progress")
		}
	}

	ss.logger.Info().Int("migrated", current).Msg("FTS to vector migration completed")
	return nil
}

// getDocumentChunks retrieves all chunks for a document.
func (ss *SemanticSearcher) getDocumentChunks(ctx context.Context, documentID string) ([]Chunk, error) {
	rows, err := ss.db.QueryContext(ctx, `
		SELECT chunk_id, chunk_index, content, start_char, end_char, metadata
		FROM kb_chunks
		WHERE document_id = ?
		ORDER BY chunk_index
	`, documentID)
	if err != nil {
		return nil, err
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
