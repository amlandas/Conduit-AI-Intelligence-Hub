package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

// Search performs a full-text search over user-supplied text.
//
// The query is sanitized into a safe FTS5 expression first; see
// sanitizeFTSQuery. Callers that build FTS5 syntax themselves must use
// SearchExpr instead, or the sanitizer will quote their operators into
// literals.
func (s *Searcher) Search(ctx context.Context, query string, opts SearchOptions) (*SearchResult, error) {
	return s.SearchExpr(ctx, s.prepareFTSQuery(query), query, opts)
}

// SearchExpr runs an FTS5 MATCH expression the caller has already built.
//
// displayQuery is the human-readable query used for snippet highlighting and
// echoed back in the result; it may differ from ftsQuery.
//
// An empty expression matches nothing. It is reported as zero results rather
// than an error: `MATCH ''` is an FTS5 syntax error, and a query that sanitizes
// away to nothing is a user typing punctuation, not a fault.
func (s *Searcher) SearchExpr(ctx context.Context, ftsQuery, displayQuery string, opts SearchOptions) (*SearchResult, error) {
	start := time.Now()

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.ContextLen <= 0 {
		opts.ContextLen = 150
	}

	if strings.TrimSpace(ftsQuery) == "" {
		return &SearchResult{
			Results:    nil,
			TotalHits:  0,
			Query:      displayQuery,
			SearchTime: float64(time.Since(start).Milliseconds()),
		}, nil
	}

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
			hit.Snippet = s.highlightSnippet(hit.Snippet, displayQuery, opts.ContextLen)
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
		Query:      displayQuery,
		SearchTime: float64(time.Since(start).Milliseconds()),
	}

	s.logger.Debug().
		Str("query", displayQuery).
		Int("hits", len(hits)).
		Int("total", totalHits).
		Float64("time_ms", result.SearchTime).
		Msg("search completed")

	return result, nil
}

// ---------------------------------------------------------------------------
// FTS5 query construction
//
// Issues #70 and #75. The old approach was to DELETE every character FTS5 might
// treat as syntax and hope the remainder parsed. It did not:
//
//   - '.' was documented as dangerous and never actually stripped, so any query
//     containing a filename ("main.go"), a version ("v1.2.3") or a decimal
//     ("3.14") reached FTS5 as a bareword and failed with a syntax error. The
//     hybrid layer then swallowed the error and reported "no results".
//   - AND / OR / NOT / NEAR survived untouched, so a user searching for
//     "people NOT nation" silently got a boolean query instead of three words,
//     and a query of exactly "AND" was a syntax error.
//   - Apostrophes were replaced with spaces, splitting "summer's" into two
//     terms, one of which was the single letter "s".
//   - Hyphens were replaced with spaces, so "ASL-3" stopped being one thing.
//
// The fix is to quote instead of delete. Every token becomes an FTS5 string
// literal, which is exactly as expressive as a bareword for ordinary words (the
// unicode61 tokenizer splits the contents identically) but cannot be
// reinterpreted as syntax. Terms the user explicitly double-quoted stay
// phrases; everything else is just text.
// ---------------------------------------------------------------------------

// ftsToken is one unit of a user query.
type ftsToken struct {
	text string
	// phrase records that the user delimited this with double quotes and so
	// asked for a literal phrase match. Such a token never gets a wildcard.
	phrase bool
}

// splitFTSQuery splits raw user text into tokens.
//
// Text inside a balanced pair of double quotes becomes one phrase token.
// Everything else splits on whitespace. An unbalanced quote is not a phrase
// delimiter and is treated as an ordinary character -- which is the other half
// of issue #70: only a real, closed pair signals exact-phrase intent.
func splitFTSQuery(query string) []ftsToken {
	var toks []ftsToken
	rest := query

	for {
		open := strings.IndexByte(rest, '"')
		if open < 0 {
			return append(toks, bareFTSTokens(rest)...)
		}
		closeAt := strings.IndexByte(rest[open+1:], '"')
		if closeAt < 0 {
			// Unbalanced: no phrase here, just a stray character.
			return append(toks, bareFTSTokens(strings.ReplaceAll(rest, `"`, " "))...)
		}

		toks = append(toks, bareFTSTokens(rest[:open])...)
		if inner := strings.TrimSpace(rest[open+1 : open+1+closeAt]); inner != "" {
			toks = append(toks, ftsToken{text: inner, phrase: true})
		}
		rest = rest[open+1+closeAt+1:]
	}
}

// bareFTSTokens splits unquoted text on whitespace.
func bareFTSTokens(s string) []ftsToken {
	fields := strings.Fields(s)
	out := make([]ftsToken, 0, len(fields))
	for _, f := range fields {
		out = append(out, ftsToken{text: f})
	}
	return out
}

// quoteFTSToken renders one token as an FTS5 string literal.
//
// Every token is quoted, not just the ones containing obvious metacharacters.
// Quoting unconditionally is what makes the result total: there is no input for
// which this produces a syntax error, and no input for which a search term is
// reinterpreted as an operator. FTS5 escapes an embedded double quote by
// doubling it.
func quoteFTSToken(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, `""`) + `"`
}

// sanitizeFTSQuery turns user text into a safe FTS5 MATCH expression.
//
// Tokens are joined with a space, which FTS5 reads as an implicit AND -- the
// same semantics the old sanitizer produced. The result is either a valid
// expression or the empty string, never something FTS5 will reject.
func sanitizeFTSQuery(query string) string {
	toks := splitFTSQuery(query)
	parts := make([]string, 0, len(toks))
	for _, tk := range toks {
		parts = append(parts, quoteFTSToken(tk.text))
	}
	return strings.Join(parts, " ")
}

// prepareFTSQuery sanitizes user text and adds a prefix wildcard to the final
// token, so that a partially typed last word still matches.
//
// A token the user quoted is left exact: they asked for that literal text.
func (s *Searcher) prepareFTSQuery(query string) string {
	toks := splitFTSQuery(query)
	if len(toks) == 0 {
		return ""
	}

	parts := make([]string, 0, len(toks))
	for i, tk := range toks {
		q := quoteFTSToken(tk.text)
		if i == len(toks)-1 && !tk.phrase && ftsPrefixable(tk.text) {
			q += "*"
		}
		parts = append(parts, q)
	}
	return strings.Join(parts, " ")
}

// ftsPrefixable reports whether a trailing wildcard is worth adding. A
// single-character term would match almost everything, and a term with no
// alphanumeric content has nothing to prefix.
func ftsPrefixable(term string) bool {
	if utf8.RuneCountInString(term) < 2 {
		return false
	}
	for _, r := range term {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
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

	// Quote the prefix before appending the wildcard. This path used to
	// concatenate raw user input with '*' and hand it straight to FTS5, so
	// "lant(ern" was a syntax error rather than a miss (issue #75).
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil, nil
	}
	ftsQuery := quoteFTSToken(prefix)
	if ftsPrefixable(prefix) {
		ftsQuery += "*"
	}

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
