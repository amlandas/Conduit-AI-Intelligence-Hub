package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/querylog"
)

// textResult builds a CallToolResult from one or more text blocks. The
// previous server emitted `{"content": [{"type":"text","text":...}, ...]}`;
// mcp.TextContent marshals to exactly that shape.
func textResult(texts ...string) *mcp.CallToolResult {
	content := make([]mcp.Content, 0, len(texts))
	for _, t := range texts {
		content = append(content, &mcp.TextContent{Text: t})
	}
	return &mcp.CallToolResult{Content: content}
}

// hybridMode maps the wire `mode` argument onto a kb.HybridSearchMode, exactly
// as the previous server did (including the undocumented "lexical" alias).
func hybridMode(mode string) kb.HybridSearchMode {
	switch mode {
	case "semantic":
		return kb.HybridModeSemantic
	case "fts5", "lexical":
		return kb.HybridModeLexical
	default:
		return kb.HybridModeFusion // hybrid mode
	}
}

// recallMode maps the wire `recall_mode` argument onto a kb.RecallMode.
func recallMode(mode string) kb.RecallMode {
	switch mode {
	case "high":
		return kb.RecallModeHigh
	case "precise":
		return kb.RecallModePrecise
	default:
		return kb.RecallModeBalanced
	}
}

// formatHit renders a single search hit. kb_search and kb_lexical_search share
// this so citation fields (title, score, path, document_id, snippet) are
// identical.
//
// The document_id line exists because of issue #91: a client that had just run
// kb_search had no way to fetch the full document. The result text showed a
// title and a path, kb_get_document wanted an opaque internal ID, and the ID
// appeared nowhere -- so Claude Code concluded "the document ID isn't the path"
// and fell back to reading the file off disk, which clients without filesystem
// access (Claude Desktop, ChatGPT) cannot do.
//
// The label is deliberately the exact argument name kb_get_document takes, so
// the value can be copied across without a translation step. It sits directly
// under Path so the rest of the block stays byte-for-byte what scripts and
// tuned client prompts already parse.
func formatHit(hit kb.SearchHit) string {
	return fmt.Sprintf("**%s** (score: %.4f)\nPath: %s\ndocument_id: %s\n\n%s",
		hit.Title, hit.Score, hit.Path, hit.DocumentID, hit.Snippet)
}

// degradedBanner renders the degraded-mode note for a hybrid result, or "" when
// nothing failed.
//
// The previous server computed a "<mode> (degraded - semantic unavailable)"
// string here and then threw it away -- the value was assigned and never used,
// so the only degraded signal that ever reached a client was result.Note on the
// EMPTY-result path. A client receiving five hits from a half-working engine
// was told nothing at all.
//
// It follows the same convention as graphDisabledNote below: a labelled line
// plus an explanation, prepended to the content the client is already getting,
// rather than a protocol-level error. An AI client can detect it by matching
// "retrieval: degraded".
func degradedBanner(res *kb.HybridSearchResult) string {
	if res == nil || !res.DegradedMode {
		return ""
	}
	reason := res.Note
	if reason == "" {
		reason = "A retrieval strategy was unavailable for this query."
	}
	return "retrieval: degraded\n\n" + reason + "\n\n" +
		"The results below come from the remaining strategies and may be less complete than usual."
}

// toolSearch performs a search using the hybrid searcher.
func (s *Server) toolSearch(ctx context.Context, _ *mcp.CallToolRequest, args searchArgs) (*mcp.CallToolResult, any, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50 // Cap at max
	}

	opts := kb.HybridSearchOptions{
		Limit:      limit,
		Mode:       hybridMode(args.Mode),
		RecallMode: recallMode(args.RecallMode),
	}
	if args.SourceID != "" {
		opts.Filter.SourceIDs = []string{args.SourceID}
	}

	// Query-shape instrumentation: features only, never the query text.
	// kb_search has no hop argument, so hop depth is recorded as 0 -- which is
	// exactly the baseline the graph's evidence gate is measured against.
	s.queryLog.Log(querylog.Shape(ToolSearch, args.Query, 0, s.graph.Enabled()))

	// Use hybrid searcher with fallback for better results
	result, err := s.hybrid.SearchWithFallback(ctx, args.Query, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("search: %w", err)
	}

	// Format results as content blocks -- one block per hit -- behind a
	// degraded-mode banner when a retrieval strategy failed outright.
	var texts []string
	if banner := degradedBanner(result); banner != "" {
		texts = append(texts, banner)
	}
	for _, hit := range result.Results {
		texts = append(texts, formatHit(hit))
	}

	if len(result.Results) == 0 {
		noteText := "No results found for: " + args.Query
		if result.Note != "" {
			noteText += "\n\n" + result.Note
		}
		texts = append(texts, noteText)
	}

	return textResult(texts...), nil, nil
}

// toolLexicalSearch performs a pure FTS5/BM25 keyword search.
//
// This bypasses the hybrid searcher completely: no embeddings, no RRF fusion,
// no MMR/diversity filtering, no fallback ladder. Hits come back in raw BM25
// order (most negative score first) with the same citation fields kb_search
// emits, so an agent can iterate on keywords without the ranking shifting
// underneath it.
func (s *Server) toolLexicalSearch(ctx context.Context, _ *mcp.CallToolRequest, args lexicalSearchArgs) (*mcp.CallToolResult, any, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50 // Cap at max, matching kb_search
	}

	opts := kb.SearchOptions{
		Limit: limit,
		// Highlight matches the FTS-only path inside HybridSearcher, so
		// snippets look the same as kb_search with mode=fts5.
		Highlight: true,
	}
	if args.SourceID != "" {
		opts.SourceIDs = []string{args.SourceID}
	}

	result, err := s.searcher.Search(ctx, args.Query, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("lexical search: %w", err)
	}

	var texts []string
	for _, hit := range result.Results {
		texts = append(texts, formatHit(hit))
	}

	if len(texts) == 0 {
		texts = append(texts, "No results found for: "+args.Query)
	}

	return textResult(texts...), nil, nil
}

// toolSearchWithContext performs a search and returns processed, prompt-ready results.
func (s *Server) toolSearchWithContext(ctx context.Context, _ *mcp.CallToolRequest, args searchWithContextArgs) (*mcp.CallToolResult, any, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 5 // Default to 5 for processed results
	}

	opts := kb.HybridSearchOptions{
		Limit:      limit * 3, // Fetch more to allow for merging
		Mode:       hybridMode(args.Mode),
		RecallMode: recallMode(args.RecallMode),
	}
	if args.SourceID != "" {
		opts.Filter.SourceIDs = []string{args.SourceID}
	}

	// Use hybrid searcher with fallback
	result, err := s.hybrid.SearchWithFallback(ctx, args.Query, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("search: %w", err)
	}

	// Process results using the result processor
	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(result.Results)

	// Limit to requested number of documents
	if len(processed) > limit {
		processed = processed[:limit]
	}

	banner := degradedBanner(result)

	if len(processed) == 0 {
		text := "No relevant documents found for: " + args.Query
		if banner != "" {
			text = banner + "\n\n" + text
		}
		return textResult(text), nil, nil
	}

	// Build a nicely formatted response
	var sb strings.Builder
	if banner != "" {
		sb.WriteString(banner)
		sb.WriteString("\n\n")
	}
	sb.WriteString("## Relevant Context\n\n")
	sb.WriteString(fmt.Sprintf("*Found %d documents for: \"%s\"*\n\n", len(processed), args.Query))

	for i, p := range processed {
		sb.WriteString(fmt.Sprintf("### %d. %s\n", i+1, p.Title))
		sb.WriteString(fmt.Sprintf("*Source: %s", p.Source.File))
		if p.Source.Page > 0 {
			sb.WriteString(fmt.Sprintf(" (page %d)", p.Source.Page))
		}
		if p.Source.Section != "" {
			sb.WriteString(fmt.Sprintf(" - %s", p.Source.Section))
		}
		sb.WriteString("*\n")
		// #91: the retrieval key, under the citation line and labelled with the
		// exact argument name kb_get_document takes. Without it this tool is a
		// dead end -- Source shows a bare filename (ResultProcessor stores the
		// basename), so there is not even a path to fall back to.
		sb.WriteString(fmt.Sprintf("document_id: %s\n\n", p.DocumentID))
		sb.WriteString(p.Content)
		sb.WriteString("\n")

		if i < len(processed)-1 {
			sb.WriteString("\n---\n\n")
		}
	}

	return textResult(sb.String()), nil, nil
}

// toolListSources lists all sources.
func (s *Server) toolListSources(ctx context.Context, _ *mcp.CallToolRequest, _ listSourcesArgs) (*mcp.CallToolResult, any, error) {
	sources, err := s.source.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list sources: %w", err)
	}

	var lines []string
	for _, src := range sources {
		lines = append(lines, fmt.Sprintf("- **%s** (%s)\n  Path: %s\n  Documents: %d | Chunks: %d | Status: %s",
			src.Name, src.SourceID, src.Path, src.DocCount, src.ChunkCount, src.Status))
	}

	text := "# Knowledge Base Sources\n\n"
	if len(lines) == 0 {
		text += "No sources configured. Use `conduit kb add` to add a source."
	} else {
		text += strings.Join(lines, "\n\n")
	}

	return textResult(text), nil, nil
}

// getDocumentKeyError is the message for a call that supplies neither key or
// both. It names the flow rather than just the constraint, because the client
// that hits it is usually one that never learned where document_id comes from.
const getDocumentKeyError = "kb_get_document needs exactly one of document_id or path. " +
	"Run kb_search first, then copy the value from the 'document_id:' line of the hit you want."

// getDocumentPathMiss is the answer to a path that is not in the index.
//
// It is a fixed string: it does not echo the requested path and says nothing
// about what the corpus does contain, so a wrong or probing path learns only
// that it did not match.
const getDocumentPathMiss = "No indexed document has that path. " +
	"path must be the absolute path exactly as printed on a search hit's 'Path:' line. " +
	"Run kb_search and use the 'document_id:' value from a hit instead."

// toolGetDocument gets a document by ID, or by path (#91).
//
// Two keys, exactly one of them: document_id is the primary key search results
// now carry, and path is the alternative for a caller holding only the location
// (kb_documents.path is UNIQUE, so it identifies a document just as precisely).
func (s *Server) toolGetDocument(ctx context.Context, _ *mcp.CallToolRequest, args getDocumentArgs) (*mcp.CallToolResult, any, error) {
	documentID := strings.TrimSpace(args.DocumentID)
	path := strings.TrimSpace(args.Path)

	if (documentID == "") == (path == "") {
		return nil, nil, errors.New(getDocumentKeyError)
	}

	var doc *kb.Document
	var err error
	if documentID != "" {
		doc, err = s.indexer.GetDocument(ctx, documentID)
		if err != nil {
			return nil, nil, fmt.Errorf("get document: %w", err)
		}
	} else {
		doc, err = s.indexer.GetDocumentByPath(ctx, path)
		if errors.Is(err, kb.ErrDocumentNotFound) {
			// Deliberately not wrapped: a relative path, a typo and a path
			// belonging to another machine all get the same answer.
			return nil, nil, errors.New(getDocumentPathMiss)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("get document: %w", err)
		}
	}

	chunks, err := s.indexer.GetChunks(ctx, doc.DocumentID)
	if err != nil {
		return nil, nil, fmt.Errorf("get chunks: %w", err)
	}

	// Reconstruct document content from chunks
	var contentParts []string
	for _, chunk := range chunks {
		contentParts = append(contentParts, chunk.Content)
	}

	// Remove overlapping content
	content := removeOverlaps(contentParts)

	return textResult(fmt.Sprintf("# %s\n\nPath: %s\nType: %s\nSize: %d bytes\n\n---\n\n%s",
		doc.Title, doc.Path, doc.MimeType, doc.Size, content)), nil, nil
}

// toolStats returns KB statistics.
func (s *Server) toolStats(ctx context.Context, _ *mcp.CallToolRequest, args statsArgs) (*mcp.CallToolResult, any, error) {
	var text string

	if args.SourceID != "" {
		// Get stats for a specific source
		source, err := s.source.Get(ctx, args.SourceID)
		if err != nil {
			return nil, nil, fmt.Errorf("get source: %w", err)
		}

		text = fmt.Sprintf(`# Knowledge Base Statistics: %s

- **Source ID**: %s
- **Path**: %s
- **Documents**: %d
- **Chunks**: %d
- **Status**: %s
`,
			source.Name,
			source.SourceID,
			source.Path,
			source.DocCount,
			source.ChunkCount,
			source.Status,
		)
	} else {
		// Get aggregate stats
		stats, err := s.indexer.GetStats(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("get stats: %w", err)
		}

		text = fmt.Sprintf(`# Knowledge Base Statistics

- **Sources**: %d
- **Documents**: %d
- **Chunks**: %d
- **Total Size**: %.2f MB
`,
			stats.TotalSources,
			stats.TotalDocuments,
			stats.TotalChunks,
			float64(stats.TotalBytes)/(1024*1024),
		)
	}

	return textResult(text), nil, nil
}

// graphDisabledNote is the degraded-mode banner kag_query emits when the
// knowledge graph is turned off.
//
// It follows the existing degraded-mode convention (kb.SearchResult.Note): a
// short human-readable line, appended to the content the client is already
// getting, rather than a protocol-level error. An AI client can detect the
// disabled state by matching "graph: disabled"; a human reading the transcript
// gets an explanation and a next step.
const graphDisabledNote = "graph: disabled\n\n" +
	"The knowledge graph is not enabled on this Conduit install, so there are no " +
	"entities or relationships to traverse. Enable it with `kb.kag.enabled: true` " +
	"in ~/.conduit/conduit.yaml, then run `conduit kb kag-sync` to populate it.\n\n" +
	"Answering from hybrid retrieval over the same query instead -- the passages " +
	"below are real indexed content, not graph results."

// graphEmptyNote covers the enabled-but-unpopulated case: the feature is on, but
// nothing has been extracted yet, so the honest answer is still "no graph data".
const graphEmptyNote = "graph: enabled, empty\n\n" +
	"The knowledge graph is enabled but contains no entities matching this query. " +
	"Run `conduit kb kag-sync` to extract entities from indexed documents.\n\n" +
	"Answering from hybrid retrieval over the same query instead."

// toolKagQuery performs a knowledge graph query.
//
// Two behaviors, both non-failing:
//
//   - Graph disabled (the default): return a labelled note plus hybrid search
//     results for the same query, so the client still receives grounded,
//     citable content instead of an error it has to recover from.
//   - Graph enabled: search entities and traverse the SQLite edge tables, in
//     the same response shape as before.
func (s *Server) toolKagQuery(ctx context.Context, _ *mcp.CallToolRequest, args kagQueryArgs) (*mcp.CallToolResult, any, error) {
	graphEnabled := s.graph.Enabled()

	// Query-shape instrumentation. Records features of the query, never the
	// query itself -- see internal/querylog.
	s.queryLog.Log(querylog.Shape(ToolKAGQuery, args.Query, args.MaxHops, graphEnabled))

	if !graphEnabled {
		return s.kagFallback(ctx, args, graphDisabledNote)
	}

	// Set defaults
	includeRelations := true
	if args.IncludeRelations != nil {
		includeRelations = *args.IncludeRelations
	}

	maxHops := args.MaxHops
	if maxHops <= 0 || maxHops > s.graphMaxHops {
		maxHops = s.graphMaxHops
	}

	// Build search request
	req := &kb.KAGSearchRequest{
		Query:            args.Query,
		EntityHints:      args.Entities,
		MaxHops:          maxHops,
		Limit:            args.Limit,
		IncludeRelations: includeRelations,
		SourceFilter:     args.SourceID,
	}

	// Perform search
	result, err := s.kagSearcher.Search(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("kag search: %w", err)
	}

	// An enabled graph with no matching entities is still a dead end for the
	// caller. Degrade the same way rather than returning "no entities found".
	if len(result.Entities) == 0 {
		return s.kagFallback(ctx, args, graphEmptyNote)
	}

	// Add formatted context as main content
	texts := []string{result.Context}

	// Add entity details if present
	entityDetails := fmt.Sprintf("\n---\nFound %d entities", len(result.Entities))
	if len(result.Relations) > 0 {
		entityDetails += fmt.Sprintf(" with %d relationships", len(result.Relations))
	}
	texts = append(texts, entityDetails)

	return textResult(texts...), nil, nil
}

// kagFallback answers a kag_query from hybrid retrieval, prefixed with a note
// explaining why no graph results are present.
//
// The fallback deliberately reuses the kb_search formatting (formatHit) so the
// hits carry the same title/score/path/snippet citation fields the client
// already knows how to read.
func (s *Server) kagFallback(ctx context.Context, args kagQueryArgs, note string) (*mcp.CallToolResult, any, error) {
	limit := args.Limit
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	opts := kb.HybridSearchOptions{
		Limit:      limit,
		Mode:       kb.HybridModeFusion,
		RecallMode: kb.RecallModeBalanced,
	}
	if args.SourceID != "" {
		opts.Filter.SourceIDs = []string{args.SourceID}
	}

	// Entity hints are query terms too; folding them in keeps the fallback
	// faithful to what the caller asked for.
	query := args.Query
	if len(args.Entities) > 0 {
		query = strings.TrimSpace(query + " " + strings.Join(args.Entities, " "))
	}

	texts := []string{note}

	result, err := s.hybrid.SearchWithFallback(ctx, query, opts)
	if banner := degradedBanner(result); banner != "" {
		texts = append(texts, banner)
	}
	if err != nil {
		// Retrieval failing too is worth saying plainly, but it is still not a
		// protocol error: the client asked a question and gets an answer about
		// why there is no content.
		s.logger.Warn().Err(err).Msg("kag_query fallback retrieval failed")
		texts = append(texts, "Retrieval fallback also failed: "+err.Error())
		return textResult(texts...), nil, nil
	}

	for _, hit := range result.Results {
		texts = append(texts, formatHit(hit))
	}

	if len(result.Results) == 0 {
		noteText := "No results found for: " + query
		if result.Note != "" {
			noteText += "\n\n" + result.Note
		}
		texts = append(texts, noteText)
	}

	return textResult(texts...), nil, nil
}

// removeOverlaps removes overlapping content from chunks.
func removeOverlaps(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}

	result := parts[0]
	for i := 1; i < len(parts); i++ {
		// Find overlap between end of result and start of next part
		overlapLen := 0
		maxOverlap := min(len(result), len(parts[i]), 200) // Check up to 200 chars

		for j := 1; j <= maxOverlap; j++ {
			if strings.HasSuffix(result, parts[i][:j]) {
				overlapLen = j
			}
		}

		if overlapLen > 0 {
			result += parts[i][overlapLen:]
		} else {
			result += parts[i]
		}
	}

	return result
}
