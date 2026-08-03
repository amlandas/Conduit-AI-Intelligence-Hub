package kb

// In-database vector storage (WP-2.1).
//
// Vectors live in the same SQLite file as FTS5 and the chunk metadata. That is
// the whole point of the design: one file, one transaction, one backup, no
// second daemon to keep alive and no network listener to secure.
//
// Storage format
//
//	kb_vectors(chunk_id, document_id, source_id, dim, norm, embedding)
//
// `embedding` is the raw float32 vector serialised little-endian; `norm` is its
// precomputed L2 norm. Cosine similarity is therefore dot(q,v)/(|q|*|v|), where
// the two norms are scalars we already have. Storing the raw vector rather than
// a pre-normalised one keeps the data lossless for future re-ranking work.
//
// Query strategy
//
// A brute-force scan. At the target corpus size (5-50K chunks x 768 dims) an
// exact scan costs tens of milliseconds -- see .eng-lead-kb/BENCH-WP-2.1.md --
// which is well inside the 100ms budget, and it buys exactness: every filter is
// an ordinary SQL predicate evaluated before the distance is computed, so a
// selective source filter can never silently cost recall the way post-filtering
// an approximate index would.
//
// The scan is two-phase. Phase one reads only (chunk_id, norm, embedding) and
// keeps a top-K list; phase two joins the K survivors back to kb_chunks and
// kb_documents for their text and metadata. Joining during the scan would make
// every one of the 50K rows pay for a join it almost certainly does not need.

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

const (
	// DefaultCollectionName names the logical chunk-vector collection. It is
	// reported in stats output; there is no separate physical collection.
	DefaultCollectionName = "conduit_kb"

	// EntityCollectionName names the logical entity-vector collection.
	EntityCollectionName = "conduit_entities"

	// DefaultUpsertBatchSize is the number of rows written per statement batch.
	DefaultUpsertBatchSize = 100
)

// ---------------------------------------------------------------------------
// Interfaces -- the injection seams
// ---------------------------------------------------------------------------

// Embedder turns text into vectors. *EmbeddingService is the production
// implementation; tests substitute deterministic fakes so that retrieval can be
// exercised without Ollama.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	Dimension() int
	Model() string
	HealthCheck(ctx context.Context) error
}

// VectorIndex stores and searches chunk embeddings.
//
// UpsertTx exists so that ingestion can write chunk text, embedding and
// metadata inside a single transaction; Upsert is the standalone convenience
// wrapper around it.
type VectorIndex interface {
	Upsert(ctx context.Context, points []VectorPoint) error
	UpsertTx(ctx context.Context, tx *sql.Tx, points []VectorPoint) error
	Search(ctx context.Context, query []float32, opts VectorSearchOptions) ([]VectorSearchResult, error)
	DeleteByDocument(ctx context.Context, documentID string) error
	DeleteBySource(ctx context.Context, sourceID string) (int, error)
	GetStats(ctx context.Context) (*VectorStoreStats, error)
	HealthCheck(ctx context.Context) error
	Close() error
}

// EntityVectorIndex stores and searches KAG entity embeddings.
type EntityVectorIndex interface {
	UpsertEntityBatch(ctx context.Context, points []EntityVectorPoint) error
	SearchEntities(ctx context.Context, query []float32, opts VectorEntitySearchOptions) ([]EntitySearchResult, error)
	DeleteEntityByID(ctx context.Context, entityID string) error
	GetEntityStats(ctx context.Context) (*VectorStoreStats, error)
}

// Compile-time proof that the production types satisfy the seams.
var (
	_ Embedder          = (*EmbeddingService)(nil)
	_ VectorIndex       = (*SQLiteVectorIndex)(nil)
	_ EntityVectorIndex = (*SQLiteVectorIndex)(nil)
)

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

// VectorPoint represents a chunk embedding to store.
type VectorPoint struct {
	ID         string            // Unique identifier (chunk_id)
	Vector     []float32         // Embedding vector
	DocumentID string            // Reference to parent document
	ChunkIndex int               // Chunk index within document
	Path       string            // Document path for filtering
	Title      string            // Document title for display
	Content    string            // Chunk content for retrieval
	Metadata   map[string]string // Additional metadata (source_id, mime_type)
}

// VectorSearchResult represents a single hit from the vector index.
type VectorSearchResult struct {
	ID         string            // Point ID (chunk_id)
	Score      float32           // Cosine similarity (higher is better)
	DocumentID string            // Parent document ID
	ChunkIndex int               // Chunk index
	Path       string            // Document path
	Title      string            // Document title
	Content    string            // Chunk content
	Metadata   map[string]string // Additional metadata
}

// VectorSearchOptions configures vector search behavior.
type VectorSearchOptions struct {
	Limit      int      // Max results (default 10)
	Offset     int      // Pagination offset
	SourceIDs  []string // Filter by source IDs
	PathPrefix string   // Filter by document path prefix
	MinScore   float64  // Minimum similarity score threshold
}

// EntityVectorPoint represents a KAG entity embedding to store.
type EntityVectorPoint struct {
	ID          string    // Canonical entity_id
	Vector      []float32 // Embedding vector
	Name        string    // Entity name
	Type        string    // Entity type
	Description string    // Entity description
	SourceIDs   string    // Comma-separated source document IDs
	Confidence  float64   // Entity confidence score
}

// EntitySearchResult represents a semantic search result for entities.
type EntitySearchResult struct {
	ID          string  // Entity ID
	Score       float32 // Cosine similarity (higher is better)
	Name        string  // Entity name
	Type        string  // Entity type
	Description string  // Entity description
	SourceIDs   string  // Source document IDs
	Confidence  float64 // Entity confidence
}

// VectorEntitySearchOptions configures entity vector search behavior.
type VectorEntitySearchOptions struct {
	Limit      int     // Max results (default 20)
	Offset     int     // Pagination offset
	EntityType string  // Filter by entity type
	MinScore   float64 // Minimum similarity score threshold
}

// VectorStoreStats contains vector index statistics.
type VectorStoreStats struct {
	CollectionName string `json:"collection_name"`
	VectorCount    int    `json:"vector_count"`
	SegmentCount   int    `json:"segment_count"`
	Status         string `json:"status"`
}

// VectorIndexConfig configures the SQLite-backed vector index.
type VectorIndexConfig struct {
	// Dimension is the expected embedding width. Vectors of a different width
	// are rejected on write, which turns an embedding-model swap into a loud
	// error rather than silently unusable results.
	Dimension int
	// BatchSize is the number of rows written per statement batch.
	BatchSize int
	// CollectionName is a cosmetic label surfaced in stats output.
	CollectionName string
}

// ---------------------------------------------------------------------------
// SQLiteVectorIndex
// ---------------------------------------------------------------------------

// SQLiteVectorIndex is a VectorIndex backed by the Conduit SQLite database.
type SQLiteVectorIndex struct {
	db             *sql.DB
	dimension      int
	batchSize      int
	collectionName string
	logger         zerolog.Logger

	schemaOnce sync.Once
	schemaErr  error
}

// NewSQLiteVectorIndex opens a vector index over an existing Conduit database.
//
// Construction performs no query against user data and never fails because the
// index happens to be empty: an unpopulated database is a perfectly valid
// starting state, and a caller must be able to build the index before anything
// has ever been ingested.
func NewSQLiteVectorIndex(db *sql.DB, cfg VectorIndexConfig) (*SQLiteVectorIndex, error) {
	if db == nil {
		return nil, fmt.Errorf("vector index requires a database handle")
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = DefaultEmbeddingDimension
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultUpsertBatchSize
	}
	if cfg.CollectionName == "" {
		cfg.CollectionName = DefaultCollectionName
	}

	return &SQLiteVectorIndex{
		db:             db,
		dimension:      cfg.Dimension,
		batchSize:      cfg.BatchSize,
		collectionName: cfg.CollectionName,
		logger:         observability.Logger("kb.vecstore"),
	}, nil
}

// ensureSchema creates the vector tables if the database predates migration 005.
// Migrations normally do this; the lazy path keeps a hand-rolled or partially
// migrated database working instead of failing at the first write.
func (vi *SQLiteVectorIndex) ensureSchema(ctx context.Context) error {
	vi.schemaOnce.Do(func() {
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS kb_vectors (
				chunk_id    TEXT PRIMARY KEY REFERENCES kb_chunks(chunk_id) ON DELETE CASCADE,
				document_id TEXT NOT NULL,
				source_id   TEXT NOT NULL DEFAULT '',
				dim         INTEGER NOT NULL,
				norm        REAL NOT NULL,
				embedding   BLOB NOT NULL,
				created_at  TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			// Must match migration 005 exactly, foreign key included: a database
			// created by one path and opened by the other has to behave the same.
			`CREATE TABLE IF NOT EXISTS kb_entity_vectors (
				entity_id   TEXT PRIMARY KEY REFERENCES kb_entities(entity_id) ON DELETE CASCADE,
				name        TEXT NOT NULL DEFAULT '',
				entity_type TEXT NOT NULL DEFAULT '',
				description TEXT NOT NULL DEFAULT '',
				source_ids  TEXT NOT NULL DEFAULT '',
				confidence  REAL NOT NULL DEFAULT 0.0,
				dim         INTEGER NOT NULL,
				norm        REAL NOT NULL,
				embedding   BLOB NOT NULL,
				created_at  TEXT NOT NULL DEFAULT (datetime('now'))
			)`,
			`CREATE INDEX IF NOT EXISTS idx_vectors_document ON kb_vectors(document_id)`,
			`CREATE INDEX IF NOT EXISTS idx_vectors_source ON kb_vectors(source_id)`,
		}
		for _, s := range stmts {
			if _, err := vi.db.ExecContext(ctx, s); err != nil {
				vi.schemaErr = fmt.Errorf("ensure vector schema: %w", err)
				return
			}
		}
	})
	return vi.schemaErr
}

// Dimension returns the expected embedding width.
func (vi *SQLiteVectorIndex) Dimension() int { return vi.dimension }

// Upsert writes points using their own transaction.
func (vi *SQLiteVectorIndex) Upsert(ctx context.Context, points []VectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	if err := vi.ensureSchema(ctx); err != nil {
		return err
	}

	tx, err := vi.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin vector transaction: %w", err)
	}
	defer tx.Rollback()

	if err := vi.UpsertTx(ctx, tx, points); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertTx writes points inside a caller-supplied transaction. This is the seam
// that lets ingestion commit chunk text, embedding and metadata atomically.
func (vi *SQLiteVectorIndex) UpsertTx(ctx context.Context, tx *sql.Tx, points []VectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	start := time.Now()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO kb_vectors (chunk_id, document_id, source_id, dim, norm, embedding)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(chunk_id) DO UPDATE SET
			document_id = excluded.document_id,
			source_id   = excluded.source_id,
			dim         = excluded.dim,
			norm        = excluded.norm,
			embedding   = excluded.embedding
	`)
	if err != nil {
		return fmt.Errorf("prepare vector upsert: %w", err)
	}
	defer stmt.Close()

	for _, p := range points {
		if len(p.Vector) != vi.dimension {
			return fmt.Errorf("vector for chunk %s has dimension %d, want %d", p.ID, len(p.Vector), vi.dimension)
		}
		blob := encodeVector(p.Vector)
		norm := l2Norm(p.Vector)
		sourceID := p.Metadata["source_id"]

		if _, err := stmt.ExecContext(ctx, p.ID, p.DocumentID, sourceID, len(p.Vector), norm, blob); err != nil {
			return fmt.Errorf("upsert vector %s: %w", p.ID, err)
		}
	}

	vi.logger.Debug().
		Int("count", len(points)).
		Dur("duration", time.Since(start)).
		Msg("upserted vectors")
	return nil
}

// Search returns the nearest neighbours of query by cosine similarity.
func (vi *SQLiteVectorIndex) Search(ctx context.Context, query []float32, opts VectorSearchOptions) ([]VectorSearchResult, error) {
	if err := vi.ensureSchema(ctx); err != nil {
		return nil, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("query vector is empty")
	}

	queryNorm := l2Norm(query)
	if queryNorm == 0 {
		// Every similarity would be 0/0. Returning nothing is more honest than
		// returning an arbitrary ordering.
		return nil, nil
	}

	start := time.Now()

	// Phase one: scan candidate vectors, keeping only the top (limit+offset).
	sqlText, args := vi.buildScanQuery(opts)
	rows, err := vi.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("vector scan failed: %w", err)
	}

	want := opts.Limit + opts.Offset
	top := newTopK(want)

	var (
		chunkID string
		norm    float64
		blob    sql.RawBytes
		scratch = make([]float32, 0, len(query))
		scanned int
	)
	for rows.Next() {
		if err := rows.Scan(&chunkID, &norm, &blob); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan vector row: %w", err)
		}
		scanned++
		if norm == 0 || len(blob) != len(query)*4 {
			continue
		}
		vec, ok := viewFloat32(blob)
		if !ok {
			scratch = decodeVectorInto(blob, scratch)
			vec = scratch
		}
		score := float32(float64(dotProduct(query, vec)) / (queryNorm * norm))
		top.push(chunkID, score)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("vector scan: %w", err)
	}
	rows.Close()

	ranked := top.sorted()

	// MinScore is applied after ranking, matching the score-threshold semantics
	// the previous backend exposed.
	filtered := ranked[:0]
	for _, c := range ranked {
		if float64(c.score) < opts.MinScore {
			continue
		}
		filtered = append(filtered, c)
	}
	ranked = filtered

	if opts.Offset > 0 {
		if opts.Offset >= len(ranked) {
			return nil, nil
		}
		ranked = ranked[opts.Offset:]
	}
	if len(ranked) > opts.Limit {
		ranked = ranked[:opts.Limit]
	}
	if len(ranked) == 0 {
		return nil, nil
	}

	// Phase two: enrich the survivors with their text and metadata.
	results, err := vi.enrich(ctx, ranked)
	if err != nil {
		return nil, err
	}

	vi.logger.Debug().
		Int("scanned", scanned).
		Int("results", len(results)).
		Dur("duration", time.Since(start)).
		Msg("vector search completed")

	return results, nil
}

// buildScanQuery assembles the phase-one scan with its filters pushed into SQL.
func (vi *SQLiteVectorIndex) buildScanQuery(opts VectorSearchOptions) (string, []any) {
	var (
		b     strings.Builder
		args  []any
		where []string
	)

	b.WriteString(`SELECT v.chunk_id, v.norm, v.embedding FROM kb_vectors v`)

	if opts.PathPrefix != "" {
		b.WriteString(` JOIN kb_documents d ON d.document_id = v.document_id`)
		where = append(where, `d.path LIKE ? ESCAPE '\'`)
		args = append(args, escapeLikePrefix(opts.PathPrefix)+"%")
	}

	if len(opts.SourceIDs) > 0 {
		placeholders := make([]string, len(opts.SourceIDs))
		for i, sid := range opts.SourceIDs {
			placeholders[i] = "?"
			args = append(args, sid)
		}
		where = append(where, `v.source_id IN (`+strings.Join(placeholders, ",")+`)`)
	}

	if len(where) > 0 {
		b.WriteString(" WHERE ")
		b.WriteString(strings.Join(where, " AND "))
	}

	return b.String(), args
}

// enrich resolves the top-K chunk ids to full results in a single query.
func (vi *SQLiteVectorIndex) enrich(ctx context.Context, ranked []candidate) ([]VectorSearchResult, error) {
	placeholders := make([]string, len(ranked))
	args := make([]any, len(ranked))
	for i, c := range ranked {
		placeholders[i] = "?"
		args[i] = c.id
	}

	rows, err := vi.db.QueryContext(ctx, `
		SELECT v.chunk_id, v.document_id, v.source_id,
		       COALESCE(c.chunk_index, 0), COALESCE(c.content, ''),
		       COALESCE(d.path, ''), COALESCE(d.title, ''), COALESCE(d.mime_type, '')
		FROM kb_vectors v
		LEFT JOIN kb_chunks c ON c.chunk_id = v.chunk_id
		LEFT JOIN kb_documents d ON d.document_id = v.document_id
		WHERE v.chunk_id IN (`+strings.Join(placeholders, ",")+`)
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("enrich vector results: %w", err)
	}
	defer rows.Close()

	byID := make(map[string]VectorSearchResult, len(ranked))
	for rows.Next() {
		var (
			r                  VectorSearchResult
			sourceID, mimeType string
		)
		if err := rows.Scan(&r.ID, &r.DocumentID, &sourceID,
			&r.ChunkIndex, &r.Content, &r.Path, &r.Title, &mimeType); err != nil {
			return nil, fmt.Errorf("scan enriched row: %w", err)
		}
		r.Metadata = map[string]string{}
		if sourceID != "" {
			r.Metadata["source_id"] = sourceID
		}
		if mimeType != "" {
			r.Metadata["mime_type"] = mimeType
		}
		byID[r.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("enrich vector results: %w", err)
	}

	// Preserve similarity order; the IN () query returns rows in rowid order.
	out := make([]VectorSearchResult, 0, len(ranked))
	for _, c := range ranked {
		r, ok := byID[c.id]
		if !ok {
			continue
		}
		r.Score = c.score
		out = append(out, r)
	}
	return out, nil
}

// Delete removes a single chunk vector.
func (vi *SQLiteVectorIndex) Delete(ctx context.Context, chunkID string) error {
	return vi.DeleteBatch(ctx, []string{chunkID})
}

// DeleteBatch removes several chunk vectors by id.
func (vi *SQLiteVectorIndex) DeleteBatch(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := vi.ensureSchema(ctx); err != nil {
		return err
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	_, err := vi.db.ExecContext(ctx,
		`DELETE FROM kb_vectors WHERE chunk_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return fmt.Errorf("delete vectors: %w", err)
	}
	return nil
}

// DeleteByDocument removes every vector belonging to a document.
func (vi *SQLiteVectorIndex) DeleteByDocument(ctx context.Context, documentID string) error {
	if err := vi.ensureSchema(ctx); err != nil {
		return err
	}
	if _, err := vi.db.ExecContext(ctx, `DELETE FROM kb_vectors WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("delete document vectors: %w", err)
	}
	vi.logger.Debug().Str("document_id", documentID).Msg("deleted document vectors")
	return nil
}

// DeleteBySource removes every vector belonging to a source and reports how
// many rows went away.
func (vi *SQLiteVectorIndex) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
	if err := vi.ensureSchema(ctx); err != nil {
		return 0, err
	}
	res, err := vi.db.ExecContext(ctx, `DELETE FROM kb_vectors WHERE source_id = ?`, sourceID)
	if err != nil {
		return 0, fmt.Errorf("delete source vectors: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, nil // the delete succeeded; only the count is unavailable
	}
	vi.logger.Info().Str("source_id", sourceID).Int64("deleted", n).Msg("deleted source vectors")
	return int(n), nil
}

// GetStats reports how many chunk vectors are stored.
func (vi *SQLiteVectorIndex) GetStats(ctx context.Context) (*VectorStoreStats, error) {
	if err := vi.ensureSchema(ctx); err != nil {
		return nil, err
	}
	var count int
	if err := vi.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_vectors`).Scan(&count); err != nil {
		return nil, fmt.Errorf("count vectors: %w", err)
	}
	return &VectorStoreStats{
		CollectionName: vi.collectionName,
		VectorCount:    count,
		SegmentCount:   1,
		Status:         "green",
	}, nil
}

// HealthCheck verifies the vector tables are reachable. An empty index is
// healthy -- "no data yet" is not a failure.
func (vi *SQLiteVectorIndex) HealthCheck(ctx context.Context) error {
	if err := vi.ensureSchema(ctx); err != nil {
		return err
	}
	var n int
	if err := vi.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_vectors`).Scan(&n); err != nil {
		return fmt.Errorf("vector index health check failed: %w", err)
	}
	return nil
}

// Close is a no-op: the index borrows the caller's database handle and does not
// own its lifetime.
func (vi *SQLiteVectorIndex) Close() error { return nil }

// ---------------------------------------------------------------------------
// Entity vectors (KAG)
// ---------------------------------------------------------------------------

// UpsertEntityBatch writes entity embeddings.
func (vi *SQLiteVectorIndex) UpsertEntityBatch(ctx context.Context, points []EntityVectorPoint) error {
	if len(points) == 0 {
		return nil
	}
	if err := vi.ensureSchema(ctx); err != nil {
		return err
	}

	tx, err := vi.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin entity vector transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO kb_entity_vectors
			(entity_id, name, entity_type, description, source_ids, confidence, dim, norm, embedding)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entity_id) DO UPDATE SET
			name        = excluded.name,
			entity_type = excluded.entity_type,
			description = excluded.description,
			source_ids  = excluded.source_ids,
			confidence  = excluded.confidence,
			dim         = excluded.dim,
			norm        = excluded.norm,
			embedding   = excluded.embedding
	`)
	if err != nil {
		return fmt.Errorf("prepare entity vector upsert: %w", err)
	}
	defer stmt.Close()

	for _, p := range points {
		if len(p.Vector) != vi.dimension {
			return fmt.Errorf("vector for entity %s has dimension %d, want %d", p.ID, len(p.Vector), vi.dimension)
		}
		if _, err := stmt.ExecContext(ctx, p.ID, p.Name, p.Type, p.Description, p.SourceIDs,
			p.Confidence, len(p.Vector), l2Norm(p.Vector), encodeVector(p.Vector)); err != nil {
			return fmt.Errorf("upsert entity vector %s: %w", p.ID, err)
		}
	}

	return tx.Commit()
}

// SearchEntities returns the nearest entity embeddings by cosine similarity.
func (vi *SQLiteVectorIndex) SearchEntities(ctx context.Context, query []float32, opts VectorEntitySearchOptions) ([]EntitySearchResult, error) {
	if err := vi.ensureSchema(ctx); err != nil {
		return nil, err
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if len(query) == 0 {
		return nil, fmt.Errorf("query vector is empty")
	}
	queryNorm := l2Norm(query)
	if queryNorm == 0 {
		return nil, nil
	}

	sqlText := `SELECT entity_id, name, entity_type, description, source_ids, confidence, norm, embedding
	            FROM kb_entity_vectors`
	var args []any
	if opts.EntityType != "" {
		sqlText += ` WHERE entity_type = ?`
		args = append(args, opts.EntityType)
	}

	rows, err := vi.db.QueryContext(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("entity vector scan failed: %w", err)
	}
	defer rows.Close()

	type scoredEntity struct {
		res   EntitySearchResult
		score float32
	}
	var all []scoredEntity
	scratch := make([]float32, 0, len(query))

	for rows.Next() {
		var (
			r    EntitySearchResult
			norm float64
			blob sql.RawBytes
		)
		if err := rows.Scan(&r.ID, &r.Name, &r.Type, &r.Description, &r.SourceIDs,
			&r.Confidence, &norm, &blob); err != nil {
			return nil, fmt.Errorf("scan entity vector row: %w", err)
		}
		if norm == 0 || len(blob) != len(query)*4 {
			continue
		}
		vec, ok := viewFloat32(blob)
		if !ok {
			scratch = decodeVectorInto(blob, scratch)
			vec = scratch
		}
		score := float32(float64(dotProduct(query, vec)) / (queryNorm * norm))
		if float64(score) < opts.MinScore {
			continue
		}
		r.Score = score
		all = append(all, scoredEntity{res: r, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("entity vector scan: %w", err)
	}

	sortDescByScore(all, func(s scoredEntity) float32 { return s.score })

	if opts.Offset > 0 {
		if opts.Offset >= len(all) {
			return nil, nil
		}
		all = all[opts.Offset:]
	}
	if len(all) > opts.Limit {
		all = all[:opts.Limit]
	}

	out := make([]EntitySearchResult, len(all))
	for i, s := range all {
		out[i] = s.res
	}
	return out, nil
}

// DeleteEntityByID removes a single entity embedding.
func (vi *SQLiteVectorIndex) DeleteEntityByID(ctx context.Context, entityID string) error {
	if err := vi.ensureSchema(ctx); err != nil {
		return err
	}
	if _, err := vi.db.ExecContext(ctx, `DELETE FROM kb_entity_vectors WHERE entity_id = ?`, entityID); err != nil {
		return fmt.Errorf("delete entity vector: %w", err)
	}
	return nil
}

// GetEntityStats reports how many entity vectors are stored.
func (vi *SQLiteVectorIndex) GetEntityStats(ctx context.Context) (*VectorStoreStats, error) {
	if err := vi.ensureSchema(ctx); err != nil {
		return nil, err
	}
	var count int
	if err := vi.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_entity_vectors`).Scan(&count); err != nil {
		return nil, fmt.Errorf("count entity vectors: %w", err)
	}
	return &VectorStoreStats{
		CollectionName: EntityCollectionName,
		VectorCount:    count,
		SegmentCount:   1,
		Status:         "green",
	}, nil
}

// ---------------------------------------------------------------------------
// Vector encoding and math
// ---------------------------------------------------------------------------

// nativeLittleEndian reports whether this machine stores float32 the same way
// the BLOB format does. Every platform Conduit ships on is little-endian; the
// check exists so the unsafe fast path can be skipped rather than corrupt data
// if that ever stops being true.
var nativeLittleEndian = func() bool {
	var x uint32 = 1
	return *(*byte)(unsafe.Pointer(&x)) == 1
}()

// encodeVector serialises a vector as little-endian float32.
func encodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// decodeVector deserialises a little-endian float32 BLOB.
func decodeVector(b []byte) []float32 {
	return decodeVectorInto(b, nil)
}

// decodeVectorInto deserialises into out, reusing its capacity when possible.
func decodeVectorInto(b []byte, out []float32) []float32 {
	n := len(b) / 4
	if cap(out) < n {
		out = make([]float32, n)
	}
	out = out[:n]
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// viewFloat32 reinterprets a BLOB as []float32 without copying.
//
// This is the difference between a comfortable and a marginal scan budget: at
// 50K x 768 the copy path moves 150MB per query. It is only safe when the bytes
// are natively ordered and 4-byte aligned, so both are checked and the caller
// falls back to decodeVectorInto when they do not hold. The returned slice
// aliases the driver's buffer and must not outlive the current row.
func viewFloat32(b []byte) ([]float32, bool) {
	if !nativeLittleEndian || len(b) == 0 || len(b)%4 != 0 {
		return nil, false
	}
	if uintptr(unsafe.Pointer(&b[0]))%4 != 0 {
		return nil, false
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), len(b)/4), true
}

// dotProduct computes the inner product of two equal-length vectors. The manual
// 4-way unroll gives the compiler independent accumulator chains, which roughly
// halves the runtime of the inner loop of every search.
func dotProduct(a, b []float32) float32 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= n; i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < n; i++ {
		s0 += a[i] * b[i]
	}
	return s0 + s1 + s2 + s3
}

// l2Norm returns the Euclidean length of a vector.
func l2Norm(v []float32) float64 {
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	return math.Sqrt(sum)
}

// cosineSimilarity is the similarity used for ranking. Exposed for tests and
// for callers that already hold both vectors in memory.
func cosineSimilarity(a, b []float32) float32 {
	na, nb := l2Norm(a), l2Norm(b)
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(float64(dotProduct(a, b)) / (na * nb))
}

// escapeLikePrefix escapes LIKE wildcards so a path prefix is matched literally.
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// ---------------------------------------------------------------------------
// Top-K selection
// ---------------------------------------------------------------------------

// candidate is a scored chunk id from the scan phase.
type candidate struct {
	id    string
	score float32
}

// topK keeps the k highest-scoring candidates seen so far.
//
// It holds a small sorted slice rather than a heap: k is on the order of tens
// while n is tens of thousands, so almost every row is rejected by a single
// comparison against the current worst score and never touches the slice at all.
type topK struct {
	k     int
	items []candidate
	worst float32
}

func newTopK(k int) *topK {
	if k < 1 {
		k = 1
	}
	return &topK{k: k, items: make([]candidate, 0, k), worst: float32(math.Inf(-1))}
}

func (t *topK) push(id string, score float32) {
	if len(t.items) == t.k {
		if score <= t.worst {
			return
		}
		t.items[len(t.items)-1] = candidate{id, score}
	} else {
		t.items = append(t.items, candidate{id, score})
	}
	// Bubble the new entry into place; the slice is kept sorted descending.
	for i := len(t.items) - 1; i > 0 && t.items[i].score > t.items[i-1].score; i-- {
		t.items[i], t.items[i-1] = t.items[i-1], t.items[i]
	}
	if len(t.items) == t.k {
		t.worst = t.items[len(t.items)-1].score
	}
}

// sorted returns the retained candidates, best first.
func (t *topK) sorted() []candidate { return t.items }

// sortDescByScore sorts in place by descending score using insertion sort,
// which is the right choice for the small slices this package produces.
func sortDescByScore[T any](items []T, score func(T) float32) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && score(items[j]) > score(items[j-1]); j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
