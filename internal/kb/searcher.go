package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// Searcher provides full-text search over the knowledge base.
type Searcher struct {
	db     *sql.DB
	logger zerolog.Logger
}

// NewSearcher creates a new searcher.
func NewSearcher(db *sql.DB) *Searcher {
	return &Searcher{
		db:     db,
		logger: observability.Logger("kb.searcher"),
	}
}

// SearchOptions configures search behavior.
type SearchOptions struct {
	Limit      int      // Max results (default 10)
	Offset     int      // Pagination offset
	SourceIDs  []string // Filter by source IDs
	MimeTypes  []string // Filter by MIME types
	MinScore   float64  // Minimum BM25 score threshold
	Highlight  bool     // Include highlighted snippets
	ContextLen int      // Characters of context around matches
}

// Search performs a full-text search.
func (s *Searcher) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error) {
	start := time.Now()

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.ContextLen <= 0 {
		opts.ContextLen = 150
	}

	// Prepare query for FTS5
	ftsQuery := s.prepareFTSQuery(query)

	// Build the search SQL
	sql, args := s.buildSearchSQL(ftsQuery, opts)

	rows, err := s.db.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search query: %w", err)
	}
	defer rows.Close()

	var hits []SearchHit
	for rows.Next() {
		var hit SearchHit
		var score float64
		var metadata string

		if err := rows.Scan(
			&hit.DocumentID, &hit.ChunkID, &hit.Path, &hit.Title,
			&hit.Snippet, &score, &metadata,
		); err != nil {
			s.logger.Warn().Err(err).Msg("scan search result")
			continue
		}

		hit.Score = score
		json.Unmarshal([]byte(metadata), &hit.Metadata)

		// Generate snippet if highlighting is enabled
		if opts.Highlight {
			hit.Snippet = s.highlightSnippet(hit.Snippet, query, opts.ContextLen)
		}

		hits = append(hits, hit)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate results: %w", err)
	}

	// Get total count
	totalHits := s.countResults(ctx, ftsQuery, opts)

	result := &SearchResult{
		Results:    hits,
		TotalHits:  totalHits,
		Query:      query,
		SearchTime: float64(time.Since(start).Milliseconds()),
	}

	s.logger.Debug().
		Str("query", query).
		Int("hits", len(hits)).
		Int("total", totalHits).
		Float64("time_ms", result.SearchTime).
		Msg("search completed")

	return result, nil
}

// prepareFTSQuery prepares a query string for FTS5.
func (s *Searcher) prepareFTSQuery(query string) string {
	// Escape special FTS5 characters
	query = strings.ReplaceAll(query, "\"", "\"\"")

	// Split into terms
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return ""
	}

	// Build phrase or term query
	if len(terms) == 1 {
		// Single term - use prefix matching
		return fmt.Sprintf("%s*", terms[0])
	}

	// Multiple terms - use AND logic with prefix on last term
	var parts []string
	for i, term := range terms {
		if i == len(terms)-1 {
			parts = append(parts, fmt.Sprintf("%s*", term))
		} else {
			parts = append(parts, term)
		}
	}
	return strings.Join(parts, " ")
}

// buildSearchSQL constructs the search query with filters.
func (s *Searcher) buildSearchSQL(ftsQuery string, opts SearchOptions) (string, []interface{}) {
	var args []interface{}
	args = append(args, ftsQuery)

	sql := `
		SELECT
			f.document_id,
			f.chunk_id,
			f.path,
			f.title,
			f.content as snippet,
			bm25(kb_fts, 1.0, 0.75, 0.5) as score,
			COALESCE(c.metadata, '{}') as metadata
		FROM kb_fts f
		JOIN kb_chunks c ON f.chunk_id = c.chunk_id
		JOIN kb_documents d ON f.document_id = d.document_id
		WHERE kb_fts MATCH ?
	`

	// Add source filter
	if len(opts.SourceIDs) > 0 {
		placeholders := make([]string, len(opts.SourceIDs))
		for i, sid := range opts.SourceIDs {
			placeholders[i] = "?"
			args = append(args, sid)
		}
		sql += fmt.Sprintf(" AND d.source_id IN (%s)", strings.Join(placeholders, ","))
	}

	// Add MIME type filter
	if len(opts.MimeTypes) > 0 {
		placeholders := make([]string, len(opts.MimeTypes))
		for i, mt := range opts.MimeTypes {
			placeholders[i] = "?"
			args = append(args, mt)
		}
		sql += fmt.Sprintf(" AND d.mime_type IN (%s)", strings.Join(placeholders, ","))
	}

	// Add score threshold
	if opts.MinScore > 0 {
		sql += " AND bm25(kb_fts, 1.0, 0.75, 0.5) < ?"
		args = append(args, -opts.MinScore) // BM25 returns negative scores
	}

	// Order by relevance (BM25 returns negative, so ASC for highest relevance)
	sql += " ORDER BY score ASC"

	// Add pagination
	sql += " LIMIT ? OFFSET ?"
	args = append(args, opts.Limit, opts.Offset)

	return sql, args
}

// countResults returns the total number of matching results.
func (s *Searcher) countResults(ctx context.Context, ftsQuery string, opts SearchOptions) int {
	var args []interface{}
	args = append(args, ftsQuery)

	sql := `
		SELECT COUNT(*)
		FROM kb_fts f
		JOIN kb_documents d ON f.document_id = d.document_id
		WHERE kb_fts MATCH ?
	`

	if len(opts.SourceIDs) > 0 {
		placeholders := make([]string, len(opts.SourceIDs))
		for i, sid := range opts.SourceIDs {
			placeholders[i] = "?"
			args = append(args, sid)
		}
		sql += fmt.Sprintf(" AND d.source_id IN (%s)", strings.Join(placeholders, ","))
	}

	if len(opts.MimeTypes) > 0 {
		placeholders := make([]string, len(opts.MimeTypes))
		for i, mt := range opts.MimeTypes {
			placeholders[i] = "?"
			args = append(args, mt)
		}
		sql += fmt.Sprintf(" AND d.mime_type IN (%s)", strings.Join(placeholders, ","))
	}

	var count int
	s.db.QueryRowContext(ctx, sql, args...).Scan(&count)
	return count
}

// highlightSnippet creates a highlighted snippet around matching terms.
func (s *Searcher) highlightSnippet(content, query string, contextLen int) string {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return truncateWithContext(content, 0, contextLen*2)
	}

	contentLower := strings.ToLower(content)
	bestPos := -1
	bestTerm := ""

	// Find first occurrence of any term
	for _, term := range terms {
		pos := strings.Index(contentLower, term)
		if pos >= 0 && (bestPos < 0 || pos < bestPos) {
			bestPos = pos
			bestTerm = term
		}
	}

	if bestPos < 0 {
		return truncateWithContext(content, 0, contextLen*2)
	}

	// Extract context around match
	start := bestPos - contextLen
	if start < 0 {
		start = 0
	}
	end := bestPos + len(bestTerm) + contextLen
	if end > len(content) {
		end = len(content)
	}

	snippet := content[start:end]

	// Add ellipsis if truncated
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	return snippet
}

// truncateWithContext truncates content to a maximum length.
func truncateWithContext(content string, start, maxLen int) string {
	if start >= len(content) {
		return ""
	}

	end := start + maxLen
	if end > len(content) {
		end = len(content)
	}

	snippet := content[start:end]

	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(content) {
		snippet = snippet + "..."
	}

	return snippet
}

// SearchByPath searches for documents by path prefix.
func (s *Searcher) SearchByPath(ctx context.Context, pathPrefix string, limit int) ([]Document, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT document_id, source_id, path, title, mime_type, size,
		       modified_at, indexed_at, hash, metadata, chunk_count
		FROM kb_documents
		WHERE path LIKE ?
		ORDER BY path
		LIMIT ?
	`, pathPrefix+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("query by path: %w", err)
	}
	defer rows.Close()

	var docs []Document
	for rows.Next() {
		var doc Document
		var modifiedAt, indexedAt sql.NullString
		var metadata string

		if err := rows.Scan(
			&doc.DocumentID, &doc.SourceID, &doc.Path, &doc.Title, &doc.MimeType,
			&doc.Size, &modifiedAt, &indexedAt, &doc.Hash, &metadata, &doc.ChunkCount,
		); err != nil {
			continue
		}

		if modifiedAt.Valid {
			doc.ModifiedAt, _ = time.Parse("2006-01-02 15:04:05", modifiedAt.String)
		}
		if indexedAt.Valid {
			doc.IndexedAt, _ = time.Parse("2006-01-02 15:04:05", indexedAt.String)
		}
		json.Unmarshal([]byte(metadata), &doc.Metadata)

		docs = append(docs, doc)
	}

	return docs, rows.Err()
}

// Suggest provides search suggestions based on indexed content.
func (s *Searcher) Suggest(ctx context.Context, prefix string, limit int) ([]string, error) {
	if limit <= 0 {
		limit = 5
	}

	// Use FTS5 prefix query
	ftsQuery := prefix + "*"

	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT title
		FROM kb_fts
		WHERE kb_fts MATCH ?
		LIMIT ?
	`, ftsQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("suggest query: %w", err)
	}
	defer rows.Close()

	var suggestions []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err == nil {
			suggestions = append(suggestions, title)
		}
	}

	return suggestions, rows.Err()
}
