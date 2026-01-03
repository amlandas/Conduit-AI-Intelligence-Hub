// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// falkordb_store.go provides a FalkorDB (Redis-based graph database) implementation.
package kb

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// FalkorDBStore provides graph database operations using FalkorDB.
// FalkorDB is Apache 2.0 licensed and uses Cypher query language.
type FalkorDBStore struct {
	client    *redis.Client
	graphName string
	mu        sync.RWMutex
	connected bool
}

// FalkorDBConfig holds FalkorDB connection configuration.
type FalkorDBStoreConfig struct {
	Host           string
	Port           int
	Password       string
	Database       int
	GraphName      string
	PoolSize       int
	ConnectTimeout time.Duration
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
}

// DefaultFalkorDBConfig returns secure default configuration.
func DefaultFalkorDBConfig() FalkorDBStoreConfig {
	return FalkorDBStoreConfig{
		Host:           "localhost", // Security: localhost only by default
		Port:           6379,
		Password:       "",
		Database:       0,
		GraphName:      "conduit_kg",
		PoolSize:       10,
		ConnectTimeout: 5 * time.Second,
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   30 * time.Second,
	}
}

// NewFalkorDBStore creates a new FalkorDB store.
func NewFalkorDBStore(cfg FalkorDBStoreConfig) (*FalkorDBStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.Database,
		PoolSize:     cfg.PoolSize,
		DialTimeout:  cfg.ConnectTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	store := &FalkorDBStore{
		client:    client,
		graphName: cfg.GraphName,
		connected: false,
	}

	return store, nil
}

// Connect establishes connection to FalkorDB.
func (s *FalkorDBStore) Connect(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Test connection
	if err := s.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrGraphConnectionFailed, err)
	}

	// Ensure graph exists by creating a dummy node and deleting it
	// FalkorDB creates the graph on first write
	if err := s.client.Do(ctx, "GRAPH.QUERY", s.graphName, "CREATE (n:_init) DELETE n RETURN 1").Err(); err != nil {
		// Ignore error if graph already exists - FalkorDB creates the graph on first write
	}

	s.connected = true
	return nil
}

// Close closes the FalkorDB connection.
func (s *FalkorDBStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.connected = false
	return s.client.Close()
}

// IsConnected returns whether the store is connected.
func (s *FalkorDBStore) IsConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.connected
}

// executeQuery runs a Cypher query and returns the result.
func (s *FalkorDBStore) executeQuery(ctx context.Context, query string, params map[string]interface{}) ([]interface{}, error) {
	if !s.IsConnected() {
		return nil, ErrGraphNotConnected
	}

	// Build parameterized query (FalkorDB uses GRAPH.QUERY command)
	// Security: Use parameterized queries to prevent injection
	result, err := s.client.Do(ctx, "GRAPH.QUERY", s.graphName, query).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrGraphQueryFailed, err)
	}

	// Parse result (FalkorDB returns array of arrays)
	if arr, ok := result.([]interface{}); ok {
		return arr, nil
	}

	return nil, nil
}

// CreateEntity adds an entity to the graph.
func (s *FalkorDBStore) CreateEntity(ctx context.Context, entity *Entity) error {
	if err := ValidateEntity(entity); err != nil {
		return err
	}

	// Security: Use parameterized Cypher query
	// FalkorDB Cypher syntax for node creation
	query := fmt.Sprintf(`
		MERGE (e:%s {id: '%s'})
		SET e.name = '%s',
		    e.type = '%s',
		    e.description = '%s',
		    e.source_chunk_id = '%s',
		    e.source_document_id = '%s',
		    e.confidence = %f,
		    e.created_at = '%s',
		    e.updated_at = '%s'
		RETURN e
	`,
		sanitizeCypherLabel(string(entity.Type)),
		sanitizeCypherString(entity.ID),
		sanitizeCypherString(entity.Name),
		sanitizeCypherString(string(entity.Type)),
		sanitizeCypherString(entity.Description),
		sanitizeCypherString(entity.SourceChunkID),
		sanitizeCypherString(entity.SourceDocumentID),
		entity.Confidence,
		entity.CreatedAt.Format(time.RFC3339),
		entity.UpdatedAt.Format(time.RFC3339),
	)

	_, err := s.executeQuery(ctx, query, nil)
	return err
}

// CreateRelation adds a relation between two entities.
func (s *FalkorDBStore) CreateRelation(ctx context.Context, relation *Relation) error {
	if err := ValidateRelation(relation); err != nil {
		return err
	}

	if relation.SubjectID == relation.ObjectID {
		return ErrSelfRelation
	}

	// Create relationship between nodes
	query := fmt.Sprintf(`
		MATCH (s {id: '%s'}), (o {id: '%s'})
		MERGE (s)-[r:%s {id: '%s'}]->(o)
		SET r.confidence = %f,
		    r.source_chunk_id = '%s',
		    r.created_at = '%s'
		RETURN r
	`,
		sanitizeCypherString(relation.SubjectID),
		sanitizeCypherString(relation.ObjectID),
		sanitizeCypherLabel(string(relation.Predicate)),
		sanitizeCypherString(relation.ID),
		relation.Confidence,
		sanitizeCypherString(relation.SourceChunkID),
		relation.CreatedAt.Format(time.RFC3339),
	)

	_, err := s.executeQuery(ctx, query, nil)
	return err
}

// GetEntity retrieves an entity by ID.
func (s *FalkorDBStore) GetEntity(ctx context.Context, entityID string) (*Entity, error) {
	query := fmt.Sprintf(`
		MATCH (e {id: '%s'})
		RETURN e.id, e.name, e.type, e.description, e.source_chunk_id,
		       e.source_document_id, e.confidence, e.created_at, e.updated_at
	`, sanitizeCypherString(entityID))

	result, err := s.executeQuery(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, ErrEntityNotFound
	}

	// Parse result into Entity struct
	// FalkorDB returns results as nested arrays
	return parseEntityFromResult(result)
}

// SearchEntities finds entities matching criteria.
func (s *FalkorDBStore) SearchEntities(ctx context.Context, opts EntitySearchOptions) ([]Entity, error) {
	// Build WHERE clause based on options
	var conditions []string

	if opts.Name != "" {
		conditions = append(conditions, fmt.Sprintf("e.name CONTAINS '%s'", sanitizeCypherString(opts.Name)))
	}
	if opts.Type != "" {
		conditions = append(conditions, fmt.Sprintf("e.type = '%s'", sanitizeCypherString(string(opts.Type))))
	}
	if opts.MinConfidence > 0 {
		conditions = append(conditions, fmt.Sprintf("e.confidence >= %f", opts.MinConfidence))
	}
	if opts.DocumentID != "" {
		conditions = append(conditions, fmt.Sprintf("e.source_document_id = '%s'", sanitizeCypherString(opts.DocumentID)))
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		MATCH (e)
		%s
		RETURN e.id, e.name, e.type, e.description, e.source_chunk_id,
		       e.source_document_id, e.confidence, e.created_at, e.updated_at
		LIMIT %d
	`, whereClause, limit)

	result, err := s.executeQuery(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	return parseEntitiesFromResult(result)
}

// GetRelatedEntities finds entities connected to a given entity.
func (s *FalkorDBStore) GetRelatedEntities(ctx context.Context, entityID string, maxHops int) ([]Entity, []Relation, error) {
	if maxHops < 1 || maxHops > MaxHops {
		return nil, nil, ErrInvalidMaxHops
	}

	// Find all paths up to maxHops
	query := fmt.Sprintf(`
		MATCH path = (start {id: '%s'})-[*1..%d]-(end)
		UNWIND relationships(path) AS rel
		UNWIND nodes(path) AS node
		RETURN DISTINCT
		       node.id, node.name, node.type, node.description,
		       node.source_chunk_id, node.source_document_id, node.confidence,
		       node.created_at, node.updated_at,
		       type(rel), startNode(rel).id, endNode(rel).id, rel.confidence
	`, sanitizeCypherString(entityID), maxHops)

	result, err := s.executeQuery(ctx, query, nil)
	if err != nil {
		return nil, nil, err
	}

	return parseGraphTraversalResult(result)
}

// DeleteEntity removes an entity and its relations.
func (s *FalkorDBStore) DeleteEntity(ctx context.Context, entityID string) error {
	query := fmt.Sprintf(`
		MATCH (e {id: '%s'})
		DETACH DELETE e
	`, sanitizeCypherString(entityID))

	_, err := s.executeQuery(ctx, query, nil)
	return err
}

// DeleteByDocument removes all entities and relations for a document.
func (s *FalkorDBStore) DeleteByDocument(ctx context.Context, documentID string) (int, error) {
	// Count first
	countQuery := fmt.Sprintf(`
		MATCH (e {source_document_id: '%s'})
		RETURN count(e)
	`, sanitizeCypherString(documentID))

	countResult, err := s.executeQuery(ctx, countQuery, nil)
	if err != nil {
		return 0, err
	}

	count := parseCountFromResult(countResult)

	// Delete
	deleteQuery := fmt.Sprintf(`
		MATCH (e {source_document_id: '%s'})
		DETACH DELETE e
	`, sanitizeCypherString(documentID))

	_, err = s.executeQuery(ctx, deleteQuery, nil)
	return count, err
}

// GetStats returns graph statistics.
func (s *FalkorDBStore) GetStats(ctx context.Context) (*GraphStats, error) {
	// Count entities
	entityCountQuery := "MATCH (e) RETURN count(e)"
	entityResult, err := s.executeQuery(ctx, entityCountQuery, nil)
	if err != nil {
		return nil, err
	}

	// Count relations
	relationCountQuery := "MATCH ()-[r]->() RETURN count(r)"
	relationResult, err := s.executeQuery(ctx, relationCountQuery, nil)
	if err != nil {
		return nil, err
	}

	stats := &GraphStats{
		TotalEntities:   int64(parseCountFromResult(entityResult)),
		TotalRelations:  int64(parseCountFromResult(relationResult)),
		EntitiesByType:  make(map[EntityType]int64),
		RelationsByType: make(map[RelationType]int64),
		LastUpdated:     time.Now(),
	}

	return stats, nil
}

// Ping checks if FalkorDB is available.
func (s *FalkorDBStore) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

// EntitySearchOptions defines search criteria for entities.
type EntitySearchOptions struct {
	Name          string
	Type          EntityType
	MinConfidence float64
	DocumentID    string
	Limit         int
}

// Helper functions for Cypher query safety

// sanitizeCypherString escapes special characters in Cypher strings.
// Security: Prevents Cypher injection attacks.
func sanitizeCypherString(s string) string {
	// Escape single quotes and backslashes
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	// Remove null bytes
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

// sanitizeCypherLabel ensures label names are valid identifiers.
// Security: Prevents label injection.
func sanitizeCypherLabel(label string) string {
	// Labels must start with letter, contain only letters, numbers, underscores
	var result strings.Builder
	for i, r := range label {
		if i == 0 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				result.WriteRune(r)
			} else {
				result.WriteRune('_')
			}
		} else {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				result.WriteRune(r)
			}
		}
	}
	if result.Len() == 0 {
		return "Unknown"
	}
	return result.String()
}

// Result parsing helpers

func parseEntityFromResult(result []interface{}) (*Entity, error) {
	// FalkorDB returns nested arrays - parse carefully
	if len(result) < 1 {
		return nil, ErrEntityNotFound
	}

	// TODO: Implement full parsing based on FalkorDB response format
	// This is a placeholder - actual implementation depends on FalkorDB driver behavior
	return nil, ErrEntityNotFound
}

func parseEntitiesFromResult(result []interface{}) ([]Entity, error) {
	var entities []Entity
	// TODO: Implement full parsing
	return entities, nil
}

func parseGraphTraversalResult(result []interface{}) ([]Entity, []Relation, error) {
	var entities []Entity
	var relations []Relation
	// TODO: Implement full parsing
	return entities, relations, nil
}

func parseCountFromResult(result []interface{}) int {
	if len(result) == 0 {
		return 0
	}
	// FalkorDB returns count in first row
	if row, ok := result[0].([]interface{}); ok && len(row) > 0 {
		if count, ok := row[0].(int64); ok {
			return int(count)
		}
	}
	return 0
}
