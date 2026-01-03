// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// kag_search.go implements graph-based search over the knowledge graph.
package kb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// KAGSearcher provides graph-based search over the knowledge graph.
type KAGSearcher struct {
	db         *sql.DB
	graphStore *FalkorDBStore
	logger     zerolog.Logger
}

// NewKAGSearcher creates a new KAG searcher.
func NewKAGSearcher(db *sql.DB, graphStore *FalkorDBStore) *KAGSearcher {
	return &KAGSearcher{
		db:         db,
		graphStore: graphStore,
		logger:     observability.Logger("kb.kag_search"),
	}
}

// KAGSearchRequest represents a KAG search request.
type KAGSearchRequest struct {
	// Query is the natural language search query
	Query string `json:"query"`

	// EntityHints are optional entity names to focus the search
	EntityHints []string `json:"entities,omitempty"`

	// MaxHops is the maximum relationship hops to traverse (default: 2)
	MaxHops int `json:"max_hops,omitempty"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`

	// IncludeRelations includes related entities in results
	IncludeRelations bool `json:"include_relations,omitempty"`

	// SourceFilter limits search to specific document sources
	SourceFilter string `json:"source_id,omitempty"`
}

// KAGSearchResult represents a KAG search result.
type KAGSearchResult struct {
	// Entities are the matching entities
	Entities []EntityResult `json:"entities"`

	// Relations are the relationships between entities
	Relations []RelationResult `json:"relations,omitempty"`

	// Context is formatted context for LLM consumption
	Context string `json:"context"`

	// Query is the original query
	Query string `json:"query"`

	// TotalEntities is the total number of matching entities
	TotalEntities int `json:"total_entities"`
}

// EntityResult represents an entity in search results.
type EntityResult struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Type             string  `json:"type"`
	Description      string  `json:"description,omitempty"`
	Confidence       float64 `json:"confidence"`
	SourceDocumentID string  `json:"source_document_id,omitempty"`
	SourceDocTitle   string  `json:"source_document_title,omitempty"`
}

// RelationResult represents a relation in search results.
type RelationResult struct {
	SubjectName string  `json:"subject"`
	Predicate   string  `json:"predicate"`
	ObjectName  string  `json:"object"`
	Confidence  float64 `json:"confidence"`
}

// Search performs a KAG search.
func (s *KAGSearcher) Search(ctx context.Context, req *KAGSearchRequest) (*KAGSearchResult, error) {
	// Apply defaults
	if req.MaxHops <= 0 {
		req.MaxHops = 2
	}
	if req.MaxHops > MaxHops {
		req.MaxHops = MaxHops
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}

	// Search for matching entities in SQLite
	entities, err := s.searchEntities(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search entities: %w", err)
	}

	// Get relations if requested
	var relations []RelationResult
	if req.IncludeRelations && len(entities) > 0 {
		relations, err = s.getRelations(ctx, entities, req.MaxHops)
		if err != nil {
			s.logger.Warn().Err(err).Msg("failed to get relations, continuing without")
		}
	}

	// Format context for LLM
	context := s.formatContext(req.Query, entities, relations)

	return &KAGSearchResult{
		Entities:      entities,
		Relations:     relations,
		Context:       context,
		Query:         req.Query,
		TotalEntities: len(entities),
	}, nil
}

// searchEntities searches for entities matching the query.
func (s *KAGSearcher) searchEntities(ctx context.Context, req *KAGSearchRequest) ([]EntityResult, error) {
	// Build query based on hints or free text
	var query string
	var args []interface{}

	if len(req.EntityHints) > 0 {
		// Search by specific entity names
		placeholders := make([]string, len(req.EntityHints))
		for i, hint := range req.EntityHints {
			placeholders[i] = "e.name LIKE ?"
			args = append(args, "%"+hint+"%")
		}
		query = fmt.Sprintf(`
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE (%s)
		`, strings.Join(placeholders, " OR "))
	} else {
		// Free text search
		query = `
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE e.name LIKE ? OR e.description LIKE ?
		`
		args = append(args, "%"+req.Query+"%", "%"+req.Query+"%")
	}

	// Add source filter if specified
	if req.SourceFilter != "" {
		query += " AND d.source_id = ?"
		args = append(args, req.SourceFilter)
	}

	// Order by confidence and limit
	query += " ORDER BY e.confidence DESC LIMIT ?"
	args = append(args, req.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []EntityResult
	for rows.Next() {
		var e EntityResult
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description,
			&e.Confidence, &e.SourceDocumentID, &e.SourceDocTitle); err != nil {
			continue
		}
		entities = append(entities, e)
	}

	return entities, rows.Err()
}

// getRelations retrieves relations for the given entities.
func (s *KAGSearcher) getRelations(ctx context.Context, entities []EntityResult, maxHops int) ([]RelationResult, error) {
	if len(entities) == 0 {
		return nil, nil
	}

	// Collect entity IDs
	entityIDs := make([]interface{}, len(entities))
	placeholders := make([]string, len(entities))
	for i, e := range entities {
		entityIDs[i] = e.ID
		placeholders[i] = "?"
	}

	// Query relations where entities are subject or object
	query := fmt.Sprintf(`
		SELECT DISTINCT
		       COALESCE(se.name, r.subject_id) as subject_name,
		       r.predicate,
		       COALESCE(oe.name, r.object_id) as object_name,
		       r.confidence
		FROM kb_relations r
		LEFT JOIN kb_entities se ON r.subject_id = se.entity_id
		LEFT JOIN kb_entities oe ON r.object_id = oe.entity_id
		WHERE r.subject_id IN (%s) OR r.object_id IN (%s)
		ORDER BY r.confidence DESC
		LIMIT 50
	`, strings.Join(placeholders, ","), strings.Join(placeholders, ","))

	args := append(entityIDs, entityIDs...)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var relations []RelationResult
	for rows.Next() {
		var r RelationResult
		if err := rows.Scan(&r.SubjectName, &r.Predicate, &r.ObjectName, &r.Confidence); err != nil {
			continue
		}
		relations = append(relations, r)
	}

	return relations, rows.Err()
}

// formatContext formats the search results as LLM-friendly context.
func (s *KAGSearcher) formatContext(query string, entities []EntityResult, relations []RelationResult) string {
	if len(entities) == 0 {
		return fmt.Sprintf("No entities found matching query: %s", query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Knowledge Graph Results for: %s\n\n", query))

	// Format entities
	sb.WriteString("## Entities\n")
	for i, e := range entities {
		if i >= 10 {
			sb.WriteString(fmt.Sprintf("... and %d more entities\n", len(entities)-10))
			break
		}
		sb.WriteString(fmt.Sprintf("- **%s** (%s)", e.Name, e.Type))
		if e.Description != "" {
			desc := e.Description
			if len(desc) > 100 {
				desc = desc[:100] + "..."
			}
			sb.WriteString(fmt.Sprintf(": %s", desc))
		}
		sb.WriteString("\n")
	}

	// Format relations if present
	if len(relations) > 0 {
		sb.WriteString("\n## Relationships\n")
		for i, r := range relations {
			if i >= 15 {
				sb.WriteString(fmt.Sprintf("... and %d more relationships\n", len(relations)-15))
				break
			}
			sb.WriteString(fmt.Sprintf("- %s → %s → %s\n", r.SubjectName, r.Predicate, r.ObjectName))
		}
	}

	return sb.String()
}

// GetEntityByName retrieves an entity by name.
func (s *KAGSearcher) GetEntityByName(ctx context.Context, name string) (*EntityResult, error) {
	var e EntityResult
	err := s.db.QueryRowContext(ctx, `
		SELECT entity_id, name, type, description, confidence, source_document_id
		FROM kb_entities
		WHERE name = ? OR name LIKE ?
		ORDER BY confidence DESC
		LIMIT 1
	`, name, "%"+name+"%").Scan(&e.ID, &e.Name, &e.Type, &e.Description, &e.Confidence, &e.SourceDocumentID)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// GetRelatedEntities retrieves entities related to a given entity.
func (s *KAGSearcher) GetRelatedEntities(ctx context.Context, entityID string, maxHops int) ([]EntityResult, error) {
	if maxHops <= 0 {
		maxHops = 1
	}
	if maxHops > MaxHops {
		maxHops = MaxHops
	}

	// For now, single-hop relations from SQLite
	// FalkorDB can be used for multi-hop if connected
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT e.entity_id, e.name, e.type, e.description, e.confidence, e.source_document_id
		FROM kb_entities e
		JOIN kb_relations r ON (e.entity_id = r.object_id OR e.entity_id = r.subject_id)
		WHERE (r.subject_id = ? OR r.object_id = ?) AND e.entity_id != ?
		ORDER BY e.confidence DESC
		LIMIT 20
	`, entityID, entityID, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entities []EntityResult
	for rows.Next() {
		var e EntityResult
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description, &e.Confidence, &e.SourceDocumentID); err != nil {
			continue
		}
		entities = append(entities, e)
	}

	return entities, rows.Err()
}

// GetStats returns KAG statistics.
func (s *KAGSearcher) GetStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Entity count
	var entityCount int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_entities").Scan(&entityCount)
	stats["total_entities"] = entityCount

	// Relation count
	var relationCount int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_relations").Scan(&relationCount)
	stats["total_relations"] = relationCount

	// Entity types
	typeStats := make(map[string]int)
	rows, err := s.db.QueryContext(ctx, "SELECT type, COUNT(*) FROM kb_entities GROUP BY type")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var t string
			var c int
			rows.Scan(&t, &c)
			typeStats[t] = c
		}
	}
	stats["entity_types"] = typeStats

	// Relation types
	relStats := make(map[string]int)
	rows, err = s.db.QueryContext(ctx, "SELECT predicate, COUNT(*) FROM kb_relations GROUP BY predicate")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			var c int
			rows.Scan(&p, &c)
			relStats[p] = c
		}
	}
	stats["relation_types"] = relStats

	// FalkorDB status
	if s.graphStore != nil {
		stats["graph_db_connected"] = s.graphStore.IsConnected()
	} else {
		stats["graph_db_connected"] = false
	}

	return stats, nil
}
