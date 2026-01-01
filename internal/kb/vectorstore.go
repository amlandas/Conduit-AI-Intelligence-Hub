package kb

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// chunkIDToUUID converts a chunk ID string to a deterministic UUID v5.
// This allows us to use string chunk IDs internally while Qdrant requires UUIDs.
func chunkIDToUUID(chunkID string) string {
	// Use a fixed namespace UUID for conduit chunk IDs
	namespace := uuid.MustParse("6ba7b810-9dad-11d1-80b4-00c04fd430c8") // DNS namespace
	// Create a deterministic UUID from the chunk ID
	hash := sha256.Sum256([]byte(chunkID))
	return uuid.NewSHA1(namespace, hash[:]).String()
}

const (
	// DefaultQdrantHost is the default Qdrant gRPC endpoint.
	DefaultQdrantHost = "localhost"

	// DefaultQdrantPort is the default Qdrant gRPC port.
	DefaultQdrantPort = 6334

	// DefaultCollectionName is the default collection name for KB vectors.
	DefaultCollectionName = "conduit_kb"

	// DefaultUpsertBatchSize is the default batch size for upserting points.
	DefaultUpsertBatchSize = 100
)

// VectorStore manages vector storage in Qdrant.
type VectorStore struct {
	client         *qdrant.Client
	collectionName string
	dimension      uint64
	batchSize      int
	logger         zerolog.Logger
	mu             sync.RWMutex
	ready          bool
}

// VectorStoreConfig configures the vector store.
type VectorStoreConfig struct {
	Host           string // Qdrant host (default: localhost)
	Port           int    // Qdrant gRPC port (default: 6334)
	CollectionName string // Collection name (default: conduit_kb)
	Dimension      int    // Vector dimension (default: 768 for nomic-embed-text)
	BatchSize      int    // Batch size for upserts (default: 100)
}

// VectorPoint represents a point to store in the vector database.
type VectorPoint struct {
	ID         string            // Unique identifier (chunk_id)
	Vector     []float32         // Embedding vector
	DocumentID string            // Reference to parent document
	ChunkIndex int               // Chunk index within document
	Path       string            // Document path for filtering
	Title      string            // Document title for display
	Content    string            // Chunk content for retrieval
	Metadata   map[string]string // Additional metadata
}

// VectorSearchResult represents a search result from the vector store.
type VectorSearchResult struct {
	ID         string            // Point ID (chunk_id)
	Score      float32           // Similarity score (0-1, higher is better)
	DocumentID string            // Parent document ID
	ChunkIndex int               // Chunk index
	Path       string            // Document path
	Title      string            // Document title
	Content    string            // Chunk content
	Metadata   map[string]string // Additional metadata
}

// NewVectorStore creates a new vector store.
func NewVectorStore(cfg VectorStoreConfig) (*VectorStore, error) {
	if cfg.Host == "" {
		cfg.Host = DefaultQdrantHost
	}
	if cfg.Port <= 0 {
		cfg.Port = DefaultQdrantPort
	}
	if cfg.CollectionName == "" {
		cfg.CollectionName = DefaultCollectionName
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = DefaultEmbeddingDimension
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultUpsertBatchSize
	}

	// Create Qdrant client
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: cfg.Host,
		Port: cfg.Port,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Qdrant client: %w", err)
	}

	store := &VectorStore{
		client:         client,
		collectionName: cfg.CollectionName,
		dimension:      uint64(cfg.Dimension),
		batchSize:      cfg.BatchSize,
		logger:         observability.Logger("kb.vectorstore"),
	}

	return store, nil
}

// EnsureCollection ensures the collection exists, creating it if necessary.
func (vs *VectorStore) EnsureCollection(ctx context.Context) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	if vs.ready {
		return nil
	}

	// Check if collection exists
	collections, err := vs.client.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("failed to list collections: %w", err)
	}

	collectionExists := false
	for _, col := range collections {
		if col == vs.collectionName {
			collectionExists = true
			break
		}
	}

	if collectionExists {
		vs.logger.Info().Str("collection", vs.collectionName).Msg("collection exists")
		vs.ready = true
		return nil
	}

	// Create collection
	vs.logger.Info().
		Str("collection", vs.collectionName).
		Uint64("dimension", vs.dimension).
		Msg("creating collection")

	err = vs.client.CreateCollection(ctx, &qdrant.CreateCollection{
		CollectionName: vs.collectionName,
		VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
			Size:     vs.dimension,
			Distance: qdrant.Distance_Cosine,
		}),
	})
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	// Create payload indexes for efficient filtering
	indexes := []string{"document_id", "path", "source_id"}
	for _, field := range indexes {
		_, err = vs.client.CreateFieldIndex(ctx, &qdrant.CreateFieldIndexCollection{
			CollectionName: vs.collectionName,
			FieldName:      field,
			FieldType:      qdrant.FieldType_FieldTypeKeyword.Enum(),
		})
		if err != nil {
			vs.logger.Warn().Err(err).Str("field", field).Msg("failed to create field index")
		}
	}

	vs.ready = true
	vs.logger.Info().Str("collection", vs.collectionName).Msg("collection created")
	return nil
}

// Upsert inserts or updates a single point.
func (vs *VectorStore) Upsert(ctx context.Context, point VectorPoint) error {
	return vs.UpsertBatch(ctx, []VectorPoint{point})
}

// UpsertBatch inserts or updates multiple points.
func (vs *VectorStore) UpsertBatch(ctx context.Context, points []VectorPoint) error {
	if len(points) == 0 {
		return nil
	}

	// Ensure collection exists
	if err := vs.EnsureCollection(ctx); err != nil {
		return err
	}

	start := time.Now()

	// Convert to Qdrant points
	qdrantPoints := make([]*qdrant.PointStruct, len(points))
	for i, p := range points {
		payload := map[string]any{
			"document_id": p.DocumentID,
			"chunk_id":    p.ID, // Store original chunk ID for retrieval
			"chunk_index": p.ChunkIndex,
			"path":        p.Path,
			"title":       p.Title,
			"content":     p.Content,
		}
		// Add metadata fields
		for k, v := range p.Metadata {
			payload[k] = v
		}

		// Convert chunk ID to UUID for Qdrant (which requires UUID format)
		pointUUID := chunkIDToUUID(p.ID)

		qdrantPoints[i] = &qdrant.PointStruct{
			Id:      qdrant.NewID(pointUUID),
			Vectors: qdrant.NewVectors(p.Vector...),
			Payload: qdrant.NewValueMap(payload),
		}
	}

	// Upsert in batches
	for i := 0; i < len(qdrantPoints); i += vs.batchSize {
		end := i + vs.batchSize
		if end > len(qdrantPoints) {
			end = len(qdrantPoints)
		}
		batch := qdrantPoints[i:end]

		_, err := vs.client.Upsert(ctx, &qdrant.UpsertPoints{
			CollectionName: vs.collectionName,
			Points:         batch,
		})
		if err != nil {
			return fmt.Errorf("failed to upsert batch %d-%d: %w", i, end, err)
		}
	}

	vs.logger.Debug().
		Int("count", len(points)).
		Dur("duration", time.Since(start)).
		Msg("upserted points")

	return nil
}

// Delete removes a point by ID.
func (vs *VectorStore) Delete(ctx context.Context, id string) error {
	return vs.DeleteBatch(ctx, []string{id})
}

// DeleteBatch removes multiple points by ID.
func (vs *VectorStore) DeleteBatch(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	pointIDs := make([]*qdrant.PointId, len(ids))
	for i, id := range ids {
		pointIDs[i] = qdrant.NewID(id)
	}

	_, err := vs.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: vs.collectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Points{
				Points: &qdrant.PointsIdsList{
					Ids: pointIDs,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete points: %w", err)
	}

	vs.logger.Debug().Int("count", len(ids)).Msg("deleted points")
	return nil
}

// DeleteByDocument removes all points for a document.
func (vs *VectorStore) DeleteByDocument(ctx context.Context, documentID string) error {
	_, err := vs.client.Delete(ctx, &qdrant.DeletePoints{
		CollectionName: vs.collectionName,
		Points: &qdrant.PointsSelector{
			PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
				Filter: &qdrant.Filter{
					Must: []*qdrant.Condition{
						qdrant.NewMatch("document_id", documentID),
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete document points: %w", err)
	}

	vs.logger.Debug().Str("document_id", documentID).Msg("deleted document points")
	return nil
}

// Search performs a similarity search.
func (vs *VectorStore) Search(ctx context.Context, queryVector []float32, opts VectorSearchOptions) ([]VectorSearchResult, error) {
	if err := vs.EnsureCollection(ctx); err != nil {
		return nil, err
	}

	if opts.Limit <= 0 {
		opts.Limit = 10
	}

	start := time.Now()

	// Build filter if specified
	var filter *qdrant.Filter
	if len(opts.SourceIDs) > 0 || opts.PathPrefix != "" {
		var conditions []*qdrant.Condition

		if len(opts.SourceIDs) > 0 {
			// Match any of the source IDs
			sourceMatches := make([]*qdrant.Condition, len(opts.SourceIDs))
			for i, sid := range opts.SourceIDs {
				sourceMatches[i] = qdrant.NewMatch("source_id", sid)
			}
			conditions = append(conditions, &qdrant.Condition{
				ConditionOneOf: &qdrant.Condition_Filter{
					Filter: &qdrant.Filter{
						Should: sourceMatches,
					},
				},
			})
		}

		if opts.PathPrefix != "" {
			// Match path prefix - Qdrant doesn't have native prefix matching,
			// so we use a text match with the prefix
			conditions = append(conditions, qdrant.NewMatchText("path", opts.PathPrefix))
		}

		if len(conditions) > 0 {
			filter = &qdrant.Filter{Must: conditions}
		}
	}

	// Execute search
	searchResult, err := vs.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: vs.collectionName,
		Query:          qdrant.NewQuery(queryVector...),
		Limit:          qdrant.PtrOf(uint64(opts.Limit)),
		Offset:         qdrant.PtrOf(uint64(opts.Offset)),
		Filter:         filter,
		WithPayload:    qdrant.NewWithPayload(true),
		ScoreThreshold: qdrant.PtrOf(float32(opts.MinScore)),
	})
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}

	// Convert results
	results := make([]VectorSearchResult, len(searchResult))
	for i, point := range searchResult {
		result := VectorSearchResult{
			ID:       point.Id.GetUuid(),
			Score:    point.Score,
			Metadata: make(map[string]string),
		}

		// Extract payload fields
		if payload := point.Payload; payload != nil {
			if v, ok := payload["document_id"]; ok {
				result.DocumentID = v.GetStringValue()
			}
			if v, ok := payload["chunk_index"]; ok {
				result.ChunkIndex = int(v.GetIntegerValue())
			}
			if v, ok := payload["path"]; ok {
				result.Path = v.GetStringValue()
			}
			if v, ok := payload["title"]; ok {
				result.Title = v.GetStringValue()
			}
			if v, ok := payload["content"]; ok {
				result.Content = v.GetStringValue()
			}

			// Extract remaining fields as metadata
			for k, v := range payload {
				switch k {
				case "document_id", "chunk_index", "path", "title", "content":
					continue
				default:
					result.Metadata[k] = v.GetStringValue()
				}
			}
		}

		results[i] = result
	}

	vs.logger.Debug().
		Int("results", len(results)).
		Dur("duration", time.Since(start)).
		Msg("search completed")

	return results, nil
}

// VectorSearchOptions configures vector search behavior.
type VectorSearchOptions struct {
	Limit      int      // Max results (default 10)
	Offset     int      // Pagination offset
	SourceIDs  []string // Filter by source IDs
	PathPrefix string   // Filter by path prefix
	MinScore   float64  // Minimum similarity score threshold (0-1)
}

// GetStats returns vector store statistics.
func (vs *VectorStore) GetStats(ctx context.Context) (*VectorStoreStats, error) {
	info, err := vs.client.GetCollectionInfo(ctx, vs.collectionName)
	if err != nil {
		return nil, fmt.Errorf("failed to get collection info: %w", err)
	}

	stats := &VectorStoreStats{
		CollectionName: vs.collectionName,
		Status:         info.Status.String(),
		SegmentCount:   int(info.SegmentsCount),
	}

	// Safely dereference pointer fields
	if info.PointsCount != nil {
		stats.VectorCount = int(*info.PointsCount)
	}

	return stats, nil
}

// VectorStoreStats contains vector store statistics.
type VectorStoreStats struct {
	CollectionName string `json:"collection_name"`
	VectorCount    int    `json:"vector_count"`
	SegmentCount   int    `json:"segment_count"`
	Status         string `json:"status"`
}

// HealthCheck verifies the vector store is operational.
func (vs *VectorStore) HealthCheck(ctx context.Context) error {
	_, err := vs.client.ListCollections(ctx)
	if err != nil {
		return fmt.Errorf("vector store health check failed: %w", err)
	}
	return nil
}

// Close closes the vector store connection.
func (vs *VectorStore) Close() error {
	return vs.client.Close()
}
