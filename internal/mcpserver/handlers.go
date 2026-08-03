package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/simpleflo/conduit/internal/kb"
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
// this so citation fields (title, score, path, snippet) are identical.
func formatHit(hit kb.SearchHit) string {
	return fmt.Sprintf("**%s** (score: %.4f)\nPath: %s\n\n%s",
		hit.Title, hit.Score, hit.Path, hit.Snippet)
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
		opts.SourceIDs = []string{args.SourceID}
	}

	// Use hybrid searcher with fallback for better results
	result, err := s.hybrid.SearchWithFallback(ctx, args.Query, opts)
	if err != nil {
		return nil, nil, fmt.Errorf("search: %w", err)
	}

	// Format results as content blocks -- one block per hit, as before.
	//
	// Degraded-mode note: the previous implementation computed a
	// "<mode> (degraded - semantic unavailable)" string here but never emitted
	// it (the value was discarded), so the only degraded-mode signal that ever
	// reached a client was result.Note on the empty-result path below. That
	// behavior is preserved verbatim; surfacing the degraded banner on
	// non-empty results would change output for every search and is out of
	// scope for this port.
	var texts []string
	for _, hit := range result.Results {
		texts = append(texts, formatHit(hit))
	}

	if len(texts) == 0 {
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
		opts.SourceIDs = []string{args.SourceID}
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

	if len(processed) == 0 {
		return textResult("No relevant documents found for: " + args.Query), nil, nil
	}

	// Build a nicely formatted response
	var sb strings.Builder
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
		sb.WriteString("*\n\n")
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

// toolGetDocument gets a document by ID.
func (s *Server) toolGetDocument(ctx context.Context, _ *mcp.CallToolRequest, args getDocumentArgs) (*mcp.CallToolResult, any, error) {
	doc, err := s.indexer.GetDocument(ctx, args.DocumentID)
	if err != nil {
		return nil, nil, fmt.Errorf("get document: %w", err)
	}

	chunks, err := s.indexer.GetChunks(ctx, args.DocumentID)
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

// toolKagQuery performs a knowledge graph query.
func (s *Server) toolKagQuery(ctx context.Context, _ *mcp.CallToolRequest, args kagQueryArgs) (*mcp.CallToolResult, any, error) {
	// Set defaults
	includeRelations := true
	if args.IncludeRelations != nil {
		includeRelations = *args.IncludeRelations
	}

	// Build search request
	req := &kb.KAGSearchRequest{
		Query:            args.Query,
		EntityHints:      args.Entities,
		MaxHops:          args.MaxHops,
		Limit:            args.Limit,
		IncludeRelations: includeRelations,
		SourceFilter:     args.SourceID,
	}

	// Perform search
	result, err := s.kagSearcher.Search(ctx, req)
	if err != nil {
		return nil, nil, fmt.Errorf("kag search: %w", err)
	}

	// Add formatted context as main content
	texts := []string{result.Context}

	// Add entity details if present
	if len(result.Entities) > 0 {
		entityDetails := fmt.Sprintf("\n---\nFound %d entities", len(result.Entities))
		if len(result.Relations) > 0 {
			entityDetails += fmt.Sprintf(" with %d relationships", len(result.Relations))
		}
		texts = append(texts, entityDetails)
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
