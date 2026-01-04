// Package kb provides knowledge base functionality including KAG (Knowledge-Augmented Generation).
// kag_search.go implements graph-based search over the knowledge graph.
package kb

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// Common stopwords to filter from queries for better matching
var stopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "for": true, "from": true, "has": true, "he": true,
	"in": true, "is": true, "it": true, "its": true, "of": true, "on": true,
	"or": true, "that": true, "the": true, "to": true, "was": true, "were": true,
	"will": true, "with": true, "this": true, "these": true, "those": true,
	"what": true, "which": true, "who": true, "whom": true, "how": true,
	"about": true, "into": true, "through": true, "during": true, "before": true,
	"after": true, "above": true, "below": true, "between": true, "under": true,
	"summary": true, "details": true, "information": true, "explain": true,
	"describe": true, "list": true, "show": true, "find": true, "get": true,
}

// tokenizeQuery splits a query into meaningful search terms, removing stopwords.
func tokenizeQuery(query string) []string {
	// Normalize: lowercase and remove punctuation except hyphens
	query = strings.ToLower(query)

	// Replace non-alphanumeric (except hyphen and space) with space
	re := regexp.MustCompile(`[^a-z0-9\-\s]`)
	query = re.ReplaceAllString(query, " ")

	// Split on whitespace
	words := strings.Fields(query)

	// Filter stopwords and short words
	tokens := make([]string, 0, len(words))
	seen := make(map[string]bool)

	for _, word := range words {
		// Skip stopwords
		if stopwords[word] {
			continue
		}
		// Skip very short words (1-2 chars) unless they look like acronyms
		if len(word) <= 2 && !isLikelyAcronym(word) {
			continue
		}
		// Skip duplicates
		if seen[word] {
			continue
		}
		seen[word] = true
		tokens = append(tokens, word)
	}

	return tokens
}

// isLikelyAcronym checks if a short string is likely an acronym (all caps in original)
func isLikelyAcronym(s string) bool {
	// In lowercase context, 2-letter words are often acronyms (AI, ML, DB)
	return len(s) == 2 && unicode.IsLetter(rune(s[0])) && unicode.IsLetter(rune(s[1]))
}

// entityMatchScore represents a scored entity match
type entityMatchScore struct {
	entity EntityResult
	score  float64 // Combined match quality score
}

// calculateMatchScore calculates a match quality score for an entity against tokens
func calculateMatchScore(entity EntityResult, tokens []string) float64 {
	if len(tokens) == 0 {
		return entity.Confidence
	}

	name := strings.ToLower(entity.Name)
	desc := strings.ToLower(entity.Description)

	var score float64
	matchedTokens := 0

	for _, token := range tokens {
		tokenScore := 0.0

		// Exact name match (highest priority)
		if name == token {
			tokenScore = 1.0
		} else if strings.HasPrefix(name, token) {
			// Name prefix match
			tokenScore = 0.8
		} else if strings.Contains(name, token) {
			// Name contains token
			tokenScore = 0.6
		} else if strings.Contains(desc, token) {
			// Description contains token
			tokenScore = 0.3
		}

		if tokenScore > 0 {
			matchedTokens++
			score += tokenScore
		}
	}

	// Coverage bonus: reward matching more tokens
	if matchedTokens > 0 {
		coverage := float64(matchedTokens) / float64(len(tokens))
		score = score * (0.5 + 0.5*coverage) // 50% base + 50% coverage bonus
	}

	// Combine with entity confidence (weighted 70% match score, 30% confidence)
	return 0.7*score + 0.3*entity.Confidence
}

// KAGSearcher provides graph-based search over the knowledge graph.
type KAGSearcher struct {
	db               *sql.DB
	graphStore       *FalkorDBStore
	vectorStore      *VectorStore
	embeddingService *EmbeddingService
	logger           zerolog.Logger
}

// KAGSearcherConfig configures the KAG searcher.
type KAGSearcherConfig struct {
	DB               *sql.DB
	GraphStore       *FalkorDBStore
	VectorStore      *VectorStore      // Optional: enables semantic entity search
	EmbeddingService *EmbeddingService // Optional: enables semantic entity search
}

// NewKAGSearcher creates a new KAG searcher.
func NewKAGSearcher(db *sql.DB, graphStore *FalkorDBStore) *KAGSearcher {
	return &KAGSearcher{
		db:         db,
		graphStore: graphStore,
		logger:     observability.Logger("kb.kag_search"),
	}
}

// NewKAGSearcherWithConfig creates a KAG searcher with full configuration.
func NewKAGSearcherWithConfig(cfg KAGSearcherConfig) *KAGSearcher {
	return &KAGSearcher{
		db:               cfg.DB,
		graphStore:       cfg.GraphStore,
		vectorStore:      cfg.VectorStore,
		embeddingService: cfg.EmbeddingService,
		logger:           observability.Logger("kb.kag_search"),
	}
}

// HasSemanticSearch returns true if semantic entity search is available.
func (s *KAGSearcher) HasSemanticSearch() bool {
	return s.vectorStore != nil && s.embeddingService != nil
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
// Uses hybrid search (lexical + semantic) when available, otherwise falls back to lexical only.
func (s *KAGSearcher) searchEntities(ctx context.Context, req *KAGSearchRequest) ([]EntityResult, error) {
	// Use hybrid search if semantic search is available
	if s.HasSemanticSearch() {
		return s.searchEntitiesHybrid(ctx, req)
	}

	// Fall back to lexical-only search
	candidateLimit := req.Limit * 3
	if candidateLimit < 50 {
		candidateLimit = 50
	}

	results, err := s.searchEntitiesLexical(ctx, req, candidateLimit)
	if err != nil {
		return nil, err
	}

	if len(results) > req.Limit {
		return results[:req.Limit], nil
	}
	return results, nil
}

// searchEntitiesOriginal is the original implementation preserved for reference.
// It performs tokenized lexical search with scoring.
func (s *KAGSearcher) searchEntitiesOriginal(ctx context.Context, req *KAGSearchRequest) ([]EntityResult, error) {
	// Tokenize the query for better matching
	tokens := tokenizeQuery(req.Query)

	// Build query based on hints or tokenized free text
	var query string
	var args []interface{}

	if len(req.EntityHints) > 0 {
		// Search by specific entity names (existing behavior)
		placeholders := make([]string, len(req.EntityHints))
		for i, hint := range req.EntityHints {
			placeholders[i] = "LOWER(e.name) LIKE LOWER(?)"
			args = append(args, "%"+hint+"%")
		}
		query = fmt.Sprintf(`
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE (%s)
		`, strings.Join(placeholders, " OR "))
	} else if len(tokens) > 0 {
		// Tokenized free text search - match ANY token in name or description
		conditions := make([]string, 0, len(tokens)*2)
		for _, token := range tokens {
			// Match in name (case-insensitive)
			conditions = append(conditions, "LOWER(e.name) LIKE LOWER(?)")
			args = append(args, "%"+token+"%")
			// Match in description (case-insensitive)
			conditions = append(conditions, "LOWER(e.description) LIKE LOWER(?)")
			args = append(args, "%"+token+"%")
		}
		query = fmt.Sprintf(`
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE (%s)
		`, strings.Join(conditions, " OR "))
	} else {
		// Fallback: use original query if no tokens extracted
		query = `
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE LOWER(e.name) LIKE LOWER(?) OR LOWER(e.description) LIKE LOWER(?)
		`
		args = append(args, "%"+req.Query+"%", "%"+req.Query+"%")
	}

	// Add source filter if specified
	if req.SourceFilter != "" {
		query += " AND d.source_id = ?"
		args = append(args, req.SourceFilter)
	}

	// Fetch more candidates for re-ranking (3x the limit, min 50)
	candidateLimit := req.Limit * 3
	if candidateLimit < 50 {
		candidateLimit = 50
	}
	query += " ORDER BY e.confidence DESC LIMIT ?"
	args = append(args, candidateLimit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []EntityResult
	for rows.Next() {
		var e EntityResult
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description,
			&e.Confidence, &e.SourceDocumentID, &e.SourceDocTitle); err != nil {
			continue
		}
		candidates = append(candidates, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// If we have tokens, score and re-rank results
	if len(tokens) > 0 && len(candidates) > 0 {
		scored := make([]entityMatchScore, len(candidates))
		for i, entity := range candidates {
			scored[i] = entityMatchScore{
				entity: entity,
				score:  calculateMatchScore(entity, tokens),
			}
		}

		// Sort by score descending
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})

		// Take top results up to limit
		resultLimit := req.Limit
		if resultLimit > len(scored) {
			resultLimit = len(scored)
		}

		results := make([]EntityResult, resultLimit)
		for i := 0; i < resultLimit; i++ {
			results[i] = scored[i].entity
		}
		return results, nil
	}

	// No tokens - return candidates up to limit
	if len(candidates) > req.Limit {
		return candidates[:req.Limit], nil
	}
	return candidates, nil
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

	// Semantic search availability
	stats["semantic_search_available"] = s.HasSemanticSearch()

	return stats, nil
}

// ============================================================================
// Semantic and Hybrid Entity Search
// ============================================================================

// searchEntitiesSemantic performs semantic (vector) search for entities.
// Returns entities sorted by semantic similarity to the query.
func (s *KAGSearcher) searchEntitiesSemantic(ctx context.Context, query string, limit int) ([]EntityResult, error) {
	if !s.HasSemanticSearch() {
		return nil, fmt.Errorf("semantic search not available: vectorStore or embeddingService not configured")
	}

	// Generate query embedding
	queryVector, err := s.embeddingService.Embed(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("generate query embedding: %w", err)
	}

	// Search entity vectors
	vectorResults, err := s.vectorStore.SearchEntities(ctx, queryVector, VectorEntitySearchOptions{
		Limit:    limit,
		MinScore: 0.0, // Return all results for RRF fusion
	})
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	// Convert to EntityResult
	results := make([]EntityResult, len(vectorResults))
	for i, vr := range vectorResults {
		results[i] = EntityResult{
			ID:               vr.ID,
			Name:             vr.Name,
			Type:             vr.Type,
			Description:      vr.Description,
			Confidence:       vr.Confidence,
			SourceDocumentID: vr.SourceIDs, // May be comma-separated
		}
	}

	return results, nil
}

// entityRRFScore represents an entity with its RRF fusion score.
type entityRRFScore struct {
	entity       EntityResult
	rrfScore     float64
	lexicalRank  int  // 0 if not found lexically
	semanticRank int  // 0 if not found semantically
	foundByBoth  bool // True if found by both strategies
}

// RRF constant k (higher k = more emphasis on lower ranks)
const entityRRFConstant = 60

// searchEntitiesHybrid performs hybrid search combining lexical and semantic results.
// Uses Reciprocal Rank Fusion (RRF) to merge and rank results from both strategies.
func (s *KAGSearcher) searchEntitiesHybrid(ctx context.Context, req *KAGSearchRequest) ([]EntityResult, error) {
	// Determine if we can use semantic search
	useSemanticSearch := s.HasSemanticSearch()

	// Fetch more candidates for better fusion
	candidateLimit := req.Limit * 3
	if candidateLimit < 50 {
		candidateLimit = 50
	}

	// Get lexical results (always available)
	lexicalResults, err := s.searchEntitiesLexical(ctx, req, candidateLimit)
	if err != nil {
		s.logger.Warn().Err(err).Msg("lexical entity search failed")
		lexicalResults = nil
	}

	// Get semantic results (if available)
	var semanticResults []EntityResult
	if useSemanticSearch {
		semanticResults, err = s.searchEntitiesSemantic(ctx, req.Query, candidateLimit)
		if err != nil {
			s.logger.Warn().Err(err).Msg("semantic entity search failed, falling back to lexical only")
			semanticResults = nil
		}
	}

	// If only lexical results, return them directly (with token-based scoring applied)
	if len(semanticResults) == 0 {
		if len(lexicalResults) > req.Limit {
			return lexicalResults[:req.Limit], nil
		}
		return lexicalResults, nil
	}

	// If only semantic results, return them directly
	if len(lexicalResults) == 0 {
		if len(semanticResults) > req.Limit {
			return semanticResults[:req.Limit], nil
		}
		return semanticResults, nil
	}

	// Apply RRF fusion
	fused := s.fuseEntityResults(lexicalResults, semanticResults)

	// Apply limit
	if len(fused) > req.Limit {
		fused = fused[:req.Limit]
	}

	return fused, nil
}

// fuseEntityResults applies RRF fusion to combine lexical and semantic entity results.
func (s *KAGSearcher) fuseEntityResults(lexicalResults, semanticResults []EntityResult) []EntityResult {
	// Build rank maps (1-indexed)
	lexicalRanks := make(map[string]int)
	for i, e := range lexicalResults {
		lexicalRanks[e.ID] = i + 1
	}

	semanticRanks := make(map[string]int)
	for i, e := range semanticResults {
		semanticRanks[e.ID] = i + 1
	}

	// Collect all unique entities
	allEntities := make(map[string]EntityResult)
	for _, e := range lexicalResults {
		allEntities[e.ID] = e
	}
	for _, e := range semanticResults {
		if _, exists := allEntities[e.ID]; !exists {
			allEntities[e.ID] = e
		}
	}

	// Calculate RRF scores
	scored := make([]entityRRFScore, 0, len(allEntities))

	// Weights: equal for both strategies
	lexicalWeight := 0.5
	semanticWeight := 0.5

	for entityID, entity := range allEntities {
		var rrfScore float64
		var lexRank, semRank int

		// Lexical contribution
		if rank, ok := lexicalRanks[entityID]; ok {
			rrfScore += lexicalWeight * (1.0 / float64(entityRRFConstant+rank))
			lexRank = rank
		}

		// Semantic contribution
		if rank, ok := semanticRanks[entityID]; ok {
			rrfScore += semanticWeight * (1.0 / float64(entityRRFConstant+rank))
			semRank = rank
		}

		foundByBoth := lexRank > 0 && semRank > 0

		// Agreement boost: 20% bonus for entities found by both strategies
		if foundByBoth {
			rrfScore *= 1.2
		}

		scored = append(scored, entityRRFScore{
			entity:       entity,
			rrfScore:     rrfScore,
			lexicalRank:  lexRank,
			semanticRank: semRank,
			foundByBoth:  foundByBoth,
		})
	}

	// Sort by RRF score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].rrfScore > scored[j].rrfScore
	})

	// Convert back to EntityResult slice
	results := make([]EntityResult, len(scored))
	for i, s := range scored {
		results[i] = s.entity
	}

	return results
}

// searchEntitiesLexical performs lexical (token-based) search for entities.
// This is the current searchEntities() implementation extracted for use in hybrid search.
func (s *KAGSearcher) searchEntitiesLexical(ctx context.Context, req *KAGSearchRequest, candidateLimit int) ([]EntityResult, error) {
	// Tokenize the query for better matching
	tokens := tokenizeQuery(req.Query)

	// Build query based on hints or tokenized free text
	var query string
	var args []interface{}

	if len(req.EntityHints) > 0 {
		// Search by specific entity names (existing behavior)
		placeholders := make([]string, len(req.EntityHints))
		for i, hint := range req.EntityHints {
			placeholders[i] = "LOWER(e.name) LIKE LOWER(?)"
			args = append(args, "%"+hint+"%")
		}
		query = fmt.Sprintf(`
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE (%s)
		`, strings.Join(placeholders, " OR "))
	} else if len(tokens) > 0 {
		// Tokenized free text search - match ANY token in name or description
		conditions := make([]string, 0, len(tokens)*2)
		for _, token := range tokens {
			// Match in name (case-insensitive)
			conditions = append(conditions, "LOWER(e.name) LIKE LOWER(?)")
			args = append(args, "%"+token+"%")
			// Match in description (case-insensitive)
			conditions = append(conditions, "LOWER(e.description) LIKE LOWER(?)")
			args = append(args, "%"+token+"%")
		}
		query = fmt.Sprintf(`
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE (%s)
		`, strings.Join(conditions, " OR "))
	} else {
		// Fallback: use original query if no tokens extracted
		query = `
			SELECT e.entity_id, e.name, e.type, e.description, e.confidence,
			       e.source_document_id, COALESCE(d.title, '') as doc_title
			FROM kb_entities e
			LEFT JOIN kb_documents d ON e.source_document_id = d.document_id
			WHERE LOWER(e.name) LIKE LOWER(?) OR LOWER(e.description) LIKE LOWER(?)
		`
		args = append(args, "%"+req.Query+"%", "%"+req.Query+"%")
	}

	// Add source filter if specified
	if req.SourceFilter != "" {
		query += " AND d.source_id = ?"
		args = append(args, req.SourceFilter)
	}

	query += " ORDER BY e.confidence DESC LIMIT ?"
	args = append(args, candidateLimit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var candidates []EntityResult
	for rows.Next() {
		var e EntityResult
		if err := rows.Scan(&e.ID, &e.Name, &e.Type, &e.Description,
			&e.Confidence, &e.SourceDocumentID, &e.SourceDocTitle); err != nil {
			continue
		}
		candidates = append(candidates, e)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// If we have tokens, score and re-rank results
	if len(tokens) > 0 && len(candidates) > 0 {
		scored := make([]entityMatchScore, len(candidates))
		for i, entity := range candidates {
			scored[i] = entityMatchScore{
				entity: entity,
				score:  calculateMatchScore(entity, tokens),
			}
		}

		// Sort by score descending
		sort.Slice(scored, func(i, j int) bool {
			return scored[i].score > scored[j].score
		})

		// Convert back to results
		results := make([]EntityResult, len(scored))
		for i := range scored {
			results[i] = scored[i].entity
		}
		return results, nil
	}

	return candidates, nil
}
