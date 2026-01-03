package kb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// MCPServer provides MCP protocol access to the knowledge base.
type MCPServer struct {
	db          *sql.DB
	source      *SourceManager
	searcher    *Searcher
	hybrid      *HybridSearcher
	kagSearcher *KAGSearcher
	indexer     *Indexer
	logger      zerolog.Logger

	input  io.Reader
	output io.Writer
	mu     sync.Mutex
}

// MCPRequest represents an incoming MCP request.
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse represents an outgoing MCP response.
type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPError represents an MCP error.
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// MCPNotification represents an MCP notification.
type MCPNotification struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// NewMCPServer creates a new KB MCP server.
// If hybrid is nil, a default HybridSearcher will be created with FTS5 only.
func NewMCPServer(db *sql.DB, hybrid *HybridSearcher) *MCPServer {
	searcher := NewSearcher(db)

	// If no hybrid searcher provided, create one with just FTS5
	if hybrid == nil {
		hybrid = NewHybridSearcher(searcher, nil)
	}

	// Create KAG searcher (uses SQLite by default, can connect to FalkorDB later)
	kagSearcher := NewKAGSearcher(db, nil)

	return &MCPServer{
		db:          db,
		source:      NewSourceManager(db),
		searcher:    searcher,
		hybrid:      hybrid,
		kagSearcher: kagSearcher,
		indexer:     NewIndexer(db),
		logger:      observability.Logger("kb.mcp"),
		input:       os.Stdin,
		output:      os.Stdout,
	}
}

// SetIO sets the input/output streams for testing.
func (s *MCPServer) SetIO(input io.Reader, output io.Writer) {
	s.input = input
	s.output = output
}

// Run starts the MCP server main loop.
func (s *MCPServer) Run(ctx context.Context) error {
	s.logger.Info().Msg("KB MCP server starting")

	decoder := json.NewDecoder(s.input)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var req MCPRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			s.logger.Error().Err(err).Msg("decode request")
			continue
		}

		s.handleRequest(ctx, &req)
	}
}

// handleRequest processes a single MCP request.
func (s *MCPServer) handleRequest(ctx context.Context, req *MCPRequest) {
	s.logger.Debug().
		Str("method", req.Method).
		Interface("id", req.ID).
		Msg("handling request")

	var result interface{}
	var err error

	switch req.Method {
	case "initialize":
		result = s.handleInitialize(req.Params)
	case "initialized":
		// No response needed for notification
		return
	case "tools/list":
		result = s.handleToolsList()
	case "tools/call":
		result, err = s.handleToolCall(ctx, req.Params)
	case "resources/list":
		result = s.handleResourcesList(ctx)
	case "resources/read":
		result, err = s.handleResourceRead(ctx, req.Params)
	case "prompts/list":
		result = s.handlePromptsList()
	default:
		err = fmt.Errorf("unknown method: %s", req.Method)
	}

	if req.ID != nil {
		s.sendResponse(req.ID, result, err)
	}
}

// handleInitialize handles the initialize request.
func (s *MCPServer) handleInitialize(params json.RawMessage) interface{} {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]interface{}{
			"name":    "conduit-kb",
			"version": "1.0.0",
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
			"resources": map[string]interface{}{
				"listChanged": true,
				"subscribe":   false,
			},
			"prompts": map[string]interface{}{
				"listChanged": false,
			},
		},
	}
}

// handleToolsList returns available tools.
func (s *MCPServer) handleToolsList() interface{} {
	return map[string]interface{}{
		"tools": []map[string]interface{}{
			{
				"name":        "kb_search",
				"description": "Search the knowledge base for relevant documents using hybrid search (FTS5 keyword matching + semantic similarity when available). Use short keyword phrases for best results.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The search query. Use short keyword phrases (e.g., 'authentication JWT' rather than 'how does authentication work with JWT tokens').",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of results (default: 10, max: 50)",
						},
						"source_id": map[string]interface{}{
							"type":        "string",
							"description": "Filter results to a specific knowledge base source. Use kb_list_sources to see available source IDs.",
						},
						"mode": map[string]interface{}{
							"type":        "string",
							"description": "Search mode: 'hybrid' (default, best results), 'semantic' (vector similarity only), or 'fts5' (keyword matching only)",
							"enum":        []string{"hybrid", "semantic", "fts5"},
						},
						"recall_mode": map[string]interface{}{
							"type":        "string",
							"description": "Precision/recall tradeoff: 'high' (disable diversity filtering, get all similar results), 'balanced' (default, moderate filtering), 'precise' (aggressive deduplication)",
							"enum":        []string{"high", "balanced", "precise"},
						},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "kb_list_sources",
				"description": "List all knowledge base sources with their IDs, paths, document counts, and sync status. Use this to discover available sources before searching or filtering.",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				"name":        "kb_get_document",
				"description": "Retrieve the full content of a specific document by its ID. Use document IDs from search results.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"document_id": map[string]interface{}{
							"type":        "string",
							"description": "The document ID from search results",
						},
					},
					"required": []string{"document_id"},
				},
			},
			{
				"name":        "kb_stats",
				"description": "Get knowledge base statistics including source counts, document counts, chunk counts, and search capability status.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"source_id": map[string]interface{}{
							"type":        "string",
							"description": "Get stats for a specific source. If omitted, returns aggregate stats for all sources.",
						},
					},
				},
			},
			{
				"name":        "kb_search_with_context",
				"description": "Search with processed, prompt-ready results. Returns merged chunks from same documents, filters boilerplate, and provides citation-ready source information. Best for RAG use cases.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The search query for finding relevant context",
						},
						"source_id": map[string]interface{}{
							"type":        "string",
							"description": "Filter results to a specific source ID",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum documents to return (default: 5)",
						},
						"mode": map[string]interface{}{
							"type":        "string",
							"description": "Search mode: 'hybrid' (default), 'semantic', or 'fts5'",
							"enum":        []string{"hybrid", "semantic", "fts5"},
						},
						"recall_mode": map[string]interface{}{
							"type":        "string",
							"description": "Precision/recall tradeoff: 'high' (disable diversity filtering, get all similar results), 'balanced' (default, moderate filtering), 'precise' (aggressive deduplication)",
							"enum":        []string{"high", "balanced", "precise"},
						},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "kag_query",
				"description": "Query the knowledge graph for entities and their relationships. Use for multi-hop reasoning, aggregation queries, or finding connections between concepts. Complements RAG search with structured entity lookups.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Natural language query or entity name to search for",
						},
						"entities": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Optional list of specific entity names to find (more precise than free text)",
						},
						"include_relations": map[string]interface{}{
							"type":        "boolean",
							"description": "Include relationships between found entities (default: true)",
						},
						"max_hops": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum relationship hops to traverse (default: 2, max: 3)",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum entities to return (default: 20)",
						},
						"source_id": map[string]interface{}{
							"type":        "string",
							"description": "Filter to entities from a specific source",
						},
					},
					"required": []string{"query"},
				},
			},
		},
	}
}

// handleToolCall handles tool execution.
func (s *MCPServer) handleToolCall(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var call struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, fmt.Errorf("parse tool call: %w", err)
	}

	switch call.Name {
	case "kb_search":
		return s.toolSearch(ctx, call.Arguments)
	case "kb_search_with_context":
		return s.toolSearchWithContext(ctx, call.Arguments)
	case "kb_list_sources":
		return s.toolListSources(ctx)
	case "kb_get_document":
		return s.toolGetDocument(ctx, call.Arguments)
	case "kb_stats":
		return s.toolStats(ctx, call.Arguments)
	case "kag_query":
		return s.toolKagQuery(ctx, call.Arguments)
	default:
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// toolSearch performs a search using the hybrid searcher.
func (s *MCPServer) toolSearch(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Query      string `json:"query"`
		Limit      int    `json:"limit"`
		SourceID   string `json:"source_id"`
		Mode       string `json:"mode"`
		RecallMode string `json:"recall_mode"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse search args: %w", err)
	}

	if params.Limit <= 0 {
		params.Limit = 10
	}
	if params.Limit > 50 {
		params.Limit = 50 // Cap at max
	}
	if params.Mode == "" {
		params.Mode = "hybrid"
	}

	// Determine hybrid search mode
	var hybridMode HybridSearchMode
	switch params.Mode {
	case "semantic":
		hybridMode = HybridModeSemantic
	case "fts5", "lexical":
		hybridMode = HybridModeLexical
	default:
		hybridMode = HybridModeFusion // hybrid mode
	}

	// Map recall_mode string to RecallMode constant
	var recallMode RecallMode
	switch params.RecallMode {
	case "high":
		recallMode = RecallModeHigh
	case "precise":
		recallMode = RecallModePrecise
	default:
		recallMode = RecallModeBalanced
	}

	opts := HybridSearchOptions{
		Limit:      params.Limit,
		Mode:       hybridMode,
		RecallMode: recallMode,
	}
	if params.SourceID != "" {
		opts.SourceIDs = []string{params.SourceID}
	}

	// Use hybrid searcher with fallback for better results
	result, err := s.hybrid.SearchWithFallback(ctx, params.Query, opts)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Format results as content blocks
	var content []map[string]interface{}

	// Add search metadata
	modeStr := string(result.Mode)
	if result.DegradedMode {
		modeStr += " (degraded - semantic unavailable)"
	}

	for _, hit := range result.Results {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("**%s** (score: %.4f)\nPath: %s\n\n%s",
				hit.Title, hit.Score, hit.Path, hit.Snippet),
		})
	}

	if len(content) == 0 {
		noteText := "No results found for: " + params.Query
		if result.Note != "" {
			noteText += "\n\n" + result.Note
		}
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": noteText,
		})
	}

	return map[string]interface{}{
		"content": content,
	}, nil
}

// toolSearchWithContext performs a search and returns processed, prompt-ready results.
func (s *MCPServer) toolSearchWithContext(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Query      string `json:"query"`
		SourceID   string `json:"source_id"`
		Limit      int    `json:"limit"`
		Mode       string `json:"mode"`
		RecallMode string `json:"recall_mode"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse search args: %w", err)
	}

	if params.Limit <= 0 {
		params.Limit = 5 // Default to 5 for processed results
	}
	if params.Mode == "" {
		params.Mode = "hybrid"
	}

	// Determine hybrid search mode
	var hybridMode HybridSearchMode
	switch params.Mode {
	case "semantic":
		hybridMode = HybridModeSemantic
	case "fts5", "lexical":
		hybridMode = HybridModeLexical
	default:
		hybridMode = HybridModeFusion // hybrid mode
	}

	// Map recall_mode string to RecallMode constant
	var recallMode RecallMode
	switch params.RecallMode {
	case "high":
		recallMode = RecallModeHigh
	case "precise":
		recallMode = RecallModePrecise
	default:
		recallMode = RecallModeBalanced
	}

	opts := HybridSearchOptions{
		Limit:      params.Limit * 3, // Fetch more to allow for merging
		Mode:       hybridMode,
		RecallMode: recallMode,
	}
	if params.SourceID != "" {
		opts.SourceIDs = []string{params.SourceID}
	}

	// Use hybrid searcher with fallback
	result, err := s.hybrid.SearchWithFallback(ctx, params.Query, opts)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Process results using the result processor
	processor := NewResultProcessor()
	processed := processor.ProcessResults(result.Results)

	// Limit to requested number of documents
	if len(processed) > params.Limit {
		processed = processed[:params.Limit]
	}

	// Format as prompt-ready content
	var content []map[string]interface{}

	if len(processed) == 0 {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": "No relevant documents found for: " + params.Query,
		})
	} else {
		// Build a nicely formatted response
		var sb strings.Builder
		sb.WriteString("## Relevant Context\n\n")
		sb.WriteString(fmt.Sprintf("*Found %d documents for: \"%s\"*\n\n", len(processed), params.Query))

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

		content = append(content, map[string]interface{}{
			"type": "text",
			"text": sb.String(),
		})
	}

	return map[string]interface{}{
		"content": content,
	}, nil
}

// toolListSources lists all sources.
func (s *MCPServer) toolListSources(ctx context.Context) (interface{}, error) {
	sources, err := s.source.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
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

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}, nil
}

// toolGetDocument gets a document by ID.
func (s *MCPServer) toolGetDocument(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		DocumentID string `json:"document_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse args: %w", err)
	}

	doc, err := s.indexer.GetDocument(ctx, params.DocumentID)
	if err != nil {
		return nil, fmt.Errorf("get document: %w", err)
	}

	chunks, err := s.indexer.GetChunks(ctx, params.DocumentID)
	if err != nil {
		return nil, fmt.Errorf("get chunks: %w", err)
	}

	// Reconstruct document content from chunks
	var contentParts []string
	for _, chunk := range chunks {
		contentParts = append(contentParts, chunk.Content)
	}

	// Remove overlapping content
	content := s.removeOverlaps(contentParts)

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": fmt.Sprintf("# %s\n\nPath: %s\nType: %s\nSize: %d bytes\n\n---\n\n%s",
					doc.Title, doc.Path, doc.MimeType, doc.Size, content),
			},
		},
	}, nil
}

// removeOverlaps removes overlapping content from chunks.
func (s *MCPServer) removeOverlaps(parts []string) string {
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

// toolStats returns KB statistics.
func (s *MCPServer) toolStats(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		SourceID string `json:"source_id"`
	}
	if args != nil && len(args) > 0 {
		if err := json.Unmarshal(args, &params); err != nil {
			return nil, fmt.Errorf("parse stats args: %w", err)
		}
	}

	var text string

	if params.SourceID != "" {
		// Get stats for a specific source
		source, err := s.source.Get(ctx, params.SourceID)
		if err != nil {
			return nil, fmt.Errorf("get source: %w", err)
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
			return nil, fmt.Errorf("get stats: %w", err)
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

	return map[string]interface{}{
		"content": []map[string]interface{}{
			{"type": "text", "text": text},
		},
	}, nil
}

// handleResourcesList returns available resources.
func (s *MCPServer) handleResourcesList(ctx context.Context) interface{} {
	sources, _ := s.source.List(ctx)

	var resources []map[string]interface{}
	for _, src := range sources {
		resources = append(resources, map[string]interface{}{
			"uri":         fmt.Sprintf("kb://source/%s", src.SourceID),
			"name":        src.Name,
			"description": fmt.Sprintf("Knowledge base source: %s (%d documents)", src.Path, src.DocCount),
			"mimeType":    "application/json",
		})
	}

	return map[string]interface{}{
		"resources": resources,
	}
}

// handleResourceRead reads a resource.
func (s *MCPServer) handleResourceRead(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("parse resource request: %w", err)
	}

	// Parse URI: kb://source/{sourceID}
	if strings.HasPrefix(req.URI, "kb://source/") {
		sourceID := strings.TrimPrefix(req.URI, "kb://source/")
		source, err := s.source.Get(ctx, sourceID)
		if err != nil {
			return nil, fmt.Errorf("get source: %w", err)
		}

		content, _ := json.MarshalIndent(source, "", "  ")
		return map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"uri":      req.URI,
					"mimeType": "application/json",
					"text":     string(content),
				},
			},
		}, nil
	}

	return nil, fmt.Errorf("unknown resource URI: %s", req.URI)
}

// handlePromptsList returns available prompts.
func (s *MCPServer) handlePromptsList() interface{} {
	return map[string]interface{}{
		"prompts": []map[string]interface{}{
			{
				"name":        "kb_context",
				"description": "Get relevant context from knowledge base for a topic",
				"arguments": []map[string]interface{}{
					{
						"name":        "topic",
						"description": "The topic to get context for",
						"required":    true,
					},
				},
			},
		},
	}
}

// toolKagQuery performs a knowledge graph query.
func (s *MCPServer) toolKagQuery(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Query            string   `json:"query"`
		Entities         []string `json:"entities"`
		IncludeRelations *bool    `json:"include_relations"`
		MaxHops          int      `json:"max_hops"`
		Limit            int      `json:"limit"`
		SourceID         string   `json:"source_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse kag_query args: %w", err)
	}

	// Set defaults
	includeRelations := true
	if params.IncludeRelations != nil {
		includeRelations = *params.IncludeRelations
	}

	// Build search request
	req := &KAGSearchRequest{
		Query:            params.Query,
		EntityHints:      params.Entities,
		MaxHops:          params.MaxHops,
		Limit:            params.Limit,
		IncludeRelations: includeRelations,
		SourceFilter:     params.SourceID,
	}

	// Perform search
	result, err := s.kagSearcher.Search(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("kag search: %w", err)
	}

	// Format response for MCP
	var content []map[string]interface{}

	// Add formatted context as main content
	content = append(content, map[string]interface{}{
		"type": "text",
		"text": result.Context,
	})

	// Add entity details if present
	if len(result.Entities) > 0 {
		entityDetails := fmt.Sprintf("\n---\nFound %d entities", len(result.Entities))
		if len(result.Relations) > 0 {
			entityDetails += fmt.Sprintf(" with %d relationships", len(result.Relations))
		}
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": entityDetails,
		})
	}

	return map[string]interface{}{
		"content": content,
	}, nil
}

// sendResponse sends an MCP response.
func (s *MCPServer) sendResponse(id interface{}, result interface{}, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	resp := MCPResponse{
		JSONRPC: "2.0",
		ID:      id,
	}

	if err != nil {
		resp.Error = &MCPError{
			Code:    -32000,
			Message: err.Error(),
		}
	} else {
		resp.Result = result
	}

	data, _ := json.Marshal(resp)
	fmt.Fprintln(s.output, string(data))
}
