package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// SemanticSearcher provides semantic search over the knowledge base.
// It combines embedding generation and vector similarity search.
//
// Both collaborators are interfaces so that retrieval can be exercised without
// Ollama and without a live vector backend: tests inject deterministic
// embeddings, or a deliberately failing index to drive the degraded path.
type SemanticSearcher struct {
	embeddings Embedder
	vectors    VectorIndex
	db         *sql.DB // For metadata lookups
	logger     zerolog.Logger
}

// WP-3.4 deleted NewSemanticSearcher and SemanticSearchConfig along with
// internal/kb.EmbeddingService, the untimed Ollama client tracked as issue #71.
// The constructor's whole purpose was to build that client from an
// EmbeddingConfig; production wires the searcher through
// NewSemanticSearcherWith with an embedder built from internal/embed (see
// kbservice.newEmbedder and kb.NewProviderEmbedder), where every call is
// bounded by an http.Client timeout as well as the context.

// NewSemanticSearcherWith builds a semantic searcher from supplied
// collaborators. This is the injection seam used by tests and by any caller
// that wants a non-default embedder or index.
func NewSemanticSearcherWith(db *sql.DB, embeddings Embedder, vectors VectorIndex) *SemanticSearcher {
	return &SemanticSearcher{
		embeddings: embeddings,
		vectors:    vectors,
		db:         db,
		logger:     observability.Logger("kb.semantic"),
	}
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
	DocumentID string            `json:"document_id"`
	ChunkID    string            `json:"chunk_id"`
	ChunkIndex int               `json:"chunk_index"`
	Path       string            `json:"path"`
	Title      string            `json:"title"`
	Snippet    string            `json:"snippet"`
	Score      float64           `json:"score"`      // Similarity score (0-1)
	Confidence string            `json:"confidence"` // "high", "medium", "low"
	Metadata   map[string]string `json:"metadata,omitempty"`
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
	// MinScore of 0 is valid (no filtering) - only set default if negative
	if opts.MinScore < 0 {
		opts.MinScore = 0.0 // No filtering - return all results ranked by score
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

	vectorResults, err := ss.vectors.Search(ctx, queryEmbedding, vectorOpts)
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

	vectorResults, err := ss.vectors.Search(ctx, embedding, vectorOpts)
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

// EmbedChunks generates embeddings for a document's chunks.
//
// This is deliberately separate from the write: embedding is a network round
// trip to Ollama that can take seconds, and holding a SQLite write transaction
// open across it would block every other writer for the duration. Ingestion
// calls this first, then writes the result inside its transaction.
func (ss *SemanticSearcher) EmbedChunks(ctx context.Context, chunks []Chunk) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	contents := make([]string, len(chunks))
	for i, chunk := range chunks {
		contents[i] = chunk.Content
	}

	embeddings, err := ss.embeddings.EmbedBatch(ctx, contents)
	if err != nil {
		return nil, fmt.Errorf("failed to generate embeddings: %w", err)
	}
	if len(embeddings) != len(chunks) {
		return nil, fmt.Errorf("embedder returned %d vectors for %d chunks", len(embeddings), len(chunks))
	}
	return embeddings, nil
}

// vectorPoints assembles the rows for a document's chunks and their embeddings.
func vectorPoints(doc *Document, chunks []Chunk, embeddings [][]float32) []VectorPoint {
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
	return points
}

// IndexVectorsTx writes precomputed embeddings inside a caller-supplied
// transaction, so that chunk text, vector and metadata commit atomically.
func (ss *SemanticSearcher) IndexVectorsTx(ctx context.Context, tx *sql.Tx, doc *Document, chunks []Chunk, embeddings [][]float32) error {
	if len(chunks) == 0 {
		return nil
	}
	if len(embeddings) != len(chunks) {
		return fmt.Errorf("have %d vectors for %d chunks", len(embeddings), len(chunks))
	}
	return ss.vectors.UpsertTx(ctx, tx, vectorPoints(doc, chunks, embeddings))
}

// IndexDocument embeds a document's chunks and writes them in one step.
// Ingestion uses the EmbedChunks/IndexVectorsTx pair instead so the write can
// join the surrounding transaction; this remains for standalone callers such as
// the FTS backfill.
func (ss *SemanticSearcher) IndexDocument(ctx context.Context, doc *Document, chunks []Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	embeddings, err := ss.EmbedChunks(ctx, chunks)
	if err != nil {
		return err
	}

	if err := ss.vectors.Upsert(ctx, vectorPoints(doc, chunks, embeddings)); err != nil {
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
	return ss.vectors.DeleteByDocument(ctx, documentID)
}

// DeleteBySource removes all vectors for a source (all documents in a KB).
// Returns the number of vectors deleted.
func (ss *SemanticSearcher) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
	deleted, err := ss.vectors.DeleteBySource(ctx, sourceID)
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

// EmbeddingService returns the embedder for external use.
func (ss *SemanticSearcher) EmbeddingService() Embedder {
	return ss.embeddings
}

// VectorIndex returns the vector index for external use.
func (ss *SemanticSearcher) VectorIndex() VectorIndex {
	return ss.vectors
}

// HealthCheck verifies both embedding service and vector store are operational.
func (ss *SemanticSearcher) HealthCheck(ctx context.Context) error {
	if err := ss.embeddings.HealthCheck(ctx); err != nil {
		return fmt.Errorf("embedding service: %w", err)
	}
	if err := ss.vectors.HealthCheck(ctx); err != nil {
		return fmt.Errorf("vector store: %w", err)
	}
	return nil
}

// GetStats returns combined statistics.
func (ss *SemanticSearcher) GetStats(ctx context.Context) (*SemanticSearchStats, error) {
	vsStats, err := ss.vectors.GetStats(ctx)
	if err != nil {
		return nil, err
	}

	stats := &SemanticSearchStats{
		VectorCount:    vsStats.VectorCount,
		CollectionName: vsStats.CollectionName,
		EmbeddingModel: ss.embeddings.Model(),
		ActiveModel:    ss.embeddings.Model(),
		Dimension:      ss.embeddings.Dimension(),
		Status:         vsStats.Status,
	}

	// EmbeddingModel answers "what produced these vectors", which is the
	// question a reader of vector stats is actually asking. Before WP-4.3 it
	// reported the CURRENTLY CONFIGURED model, which is the same answer only
	// when nothing has changed -- and silently wrong exactly when it matters.
	if stamper, ok := ss.vectors.(interface {
		Stamp(context.Context) (*EmbeddingStamp, error)
	}); ok {
		if stamp, err := stamper.Stamp(ctx); err == nil && stamp != nil && stamp.Known() {
			stats.EmbeddingModel = stamp.Canonical
			if stamp.Dimensions > 0 {
				stats.Dimension = stamp.Dimensions
			}
		}
	}

	return stats, nil
}

// SemanticSearchStats contains semantic search statistics.
type SemanticSearchStats struct {
	VectorCount    int    `json:"vector_count"`
	CollectionName string `json:"collection_name"`
	// EmbeddingModel is the model that BUILT the stored vectors, taken from the
	// knowledge base's embedding stamp. It falls back to the configured model
	// when nothing has been stamped yet.
	EmbeddingModel string `json:"embedding_model"`
	// ActiveModel is the model configured right now. It differs from
	// EmbeddingModel only when the vectors need rebuilding.
	ActiveModel string `json:"active_model,omitempty"`
	Dimension   int    `json:"dimension"`
	Status      string `json:"status"`
}

// Close closes the semantic searcher and its resources.
func (ss *SemanticSearcher) Close() error {
	return ss.vectors.Close()
}

// ReembedScope selects which documents a re-embed visits.
type ReembedScope int

const (
	// ReembedMissing visits only documents that have at least one chunk with no
	// vector. This is the backfill `conduit kb migrate` has always claimed to
	// do: turn on embeddings, then give the already-indexed corpus its vectors.
	ReembedMissing ReembedScope = iota

	// ReembedAll visits every document. This is what a model change requires --
	// existing vectors are not stale, they are in the wrong space, and only
	// producing them again fixes that. It is the engine a `model upgrade`
	// command fronts.
	ReembedAll
)

// ReembedResult reports what a re-embed did.
type ReembedResult struct {
	// Total is how many documents were in scope.
	Total int `json:"total"`
	// Embedded is how many were embedded successfully.
	Embedded int `json:"embedded"`
	// Failed is how many were skipped because embedding or writing failed.
	Failed int `json:"failed"`
}

// ReembedDocuments generates and stores embeddings for documents already in the
// full-text index.
//
// Before WP-4.3 this was MigrateFromFTS, which visited EVERY document
// unconditionally -- so the backfill it advertised re-embedded a corpus that
// mostly had vectors already, at full model cost. Scope makes the two jobs
// distinct: ReembedMissing is the cheap backfill, ReembedAll is the expensive
// thing a model change genuinely needs.
//
// A document that fails is counted and skipped, not fatal: one unreadable
// document must not cost a user the other nine hundred.
func (ss *SemanticSearcher) ReembedDocuments(ctx context.Context, scope ReembedScope, progressFn func(current, total int)) (ReembedResult, error) {
	var res ReembedResult

	// Creates the vector tables if this database predates them, so the scope
	// predicate below can reference kb_vectors safely.
	if err := ss.vectors.HealthCheck(ctx); err != nil {
		return res, err
	}

	where := ""
	if scope == ReembedMissing {
		// "Has at least one chunk with no vector." Written as a correlated
		// EXISTS rather than a LEFT JOIN so a document with a thousand chunks
		// stops being examined at the first gap.
		where = `WHERE EXISTS (
			SELECT 1 FROM kb_chunks c
			WHERE c.document_id = d.document_id
			  AND NOT EXISTS (SELECT 1 FROM kb_vectors v WHERE v.chunk_id = c.chunk_id)
		)`
	}

	if err := ss.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kb_documents d `+where).Scan(&res.Total); err != nil {
		return res, fmt.Errorf("failed to count documents: %w", err)
	}
	if res.Total == 0 {
		ss.logger.Info().Msg("no documents need embedding")
		return res, nil
	}

	ss.logger.Info().
		Int("total", res.Total).
		Bool("full_reembed", scope == ReembedAll).
		Msg("starting document embedding pass")

	rows, err := ss.db.QueryContext(ctx, `
		SELECT d.document_id, d.source_id, d.path, d.title, d.mime_type
		FROM kb_documents d `+where+`
		ORDER BY d.document_id
	`)
	if err != nil {
		return res, fmt.Errorf("failed to query documents: %w", err)
	}

	// The document list is read fully before any embedding happens. Holding the
	// cursor open across a network round trip per document would keep a read
	// transaction alive for the whole pass.
	type docRow struct{ doc Document }
	var pending []docRow
	for rows.Next() {
		var d Document
		if err := rows.Scan(&d.DocumentID, &d.SourceID, &d.Path, &d.Title, &d.MimeType); err != nil {
			rows.Close()
			return res, fmt.Errorf("scan document: %w", err)
		}
		pending = append(pending, docRow{doc: d})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, fmt.Errorf("read documents: %w", err)
	}
	rows.Close()

	for i := range pending {
		doc := pending[i].doc

		chunks, err := ss.getDocumentChunks(ctx, doc.DocumentID)
		if err != nil {
			ss.logger.Warn().Err(err).Str("doc_id", doc.DocumentID).Msg("failed to get chunks")
			res.Failed++
			continue
		}

		if err := ss.IndexDocument(ctx, &doc, chunks); err != nil {
			// A model mismatch is not a per-document problem and will fail for
			// every remaining document too. Stop and say so once.
			if errors.Is(err, ErrEmbeddingModelMismatch) {
				return res, err
			}
			ss.logger.Warn().Err(err).Str("doc_id", doc.DocumentID).Msg("failed to index document")
			res.Failed++
			continue
		}

		res.Embedded++
		if progressFn != nil {
			progressFn(res.Embedded, res.Total)
		}

		if res.Embedded%10 == 0 {
			ss.logger.Info().
				Int("current", res.Embedded).
				Int("total", res.Total).
				Msg("embedding progress")
		}
	}

	ss.logger.Info().
		Int("embedded", res.Embedded).
		Int("failed", res.Failed).
		Msg("document embedding pass completed")
	return res, nil
}

// ResetVectorSpace discards every stored vector and the model stamp describing
// them. See SQLiteVectorIndex.ResetVectorSpace for why the two go together.
func (ss *SemanticSearcher) ResetVectorSpace(ctx context.Context) error {
	resetter, ok := ss.vectors.(interface {
		ResetVectorSpace(context.Context) error
	})
	if !ok {
		return fmt.Errorf("vector index does not support re-embedding")
	}
	return resetter.ResetVectorSpace(ctx)
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
