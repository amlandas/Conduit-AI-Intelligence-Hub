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
	db       *sql.DB
	source   *SourceManager
	searcher *Searcher
	indexer  *Indexer
	logger   zerolog.Logger

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
func NewMCPServer(db *sql.DB) *MCPServer {
	return &MCPServer{
		db:       db,
		source:   NewSourceManager(db),
		searcher: NewSearcher(db),
		indexer:  NewIndexer(db),
		logger:   observability.Logger("kb.mcp"),
		input:    os.Stdin,
		output:   os.Stdout,
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
				"description": "Search the knowledge base for relevant documents and information",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The search query",
						},
						"limit": map[string]interface{}{
							"type":        "integer",
							"description": "Maximum number of results (default 10)",
						},
						"source_id": map[string]interface{}{
							"type":        "string",
							"description": "Filter by source ID",
						},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "kb_list_sources",
				"description": "List all knowledge base sources",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
				},
			},
			{
				"name":        "kb_get_document",
				"description": "Get full content of a document by ID",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"document_id": map[string]interface{}{
							"type":        "string",
							"description": "The document ID",
						},
					},
					"required": []string{"document_id"},
				},
			},
			{
				"name":        "kb_stats",
				"description": "Get knowledge base statistics",
				"inputSchema": map[string]interface{}{
					"type":       "object",
					"properties": map[string]interface{}{},
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
	case "kb_list_sources":
		return s.toolListSources(ctx)
	case "kb_get_document":
		return s.toolGetDocument(ctx, call.Arguments)
	case "kb_stats":
		return s.toolStats(ctx)
	default:
		return nil, fmt.Errorf("unknown tool: %s", call.Name)
	}
}

// toolSearch performs a search.
func (s *MCPServer) toolSearch(ctx context.Context, args json.RawMessage) (interface{}, error) {
	var params struct {
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
		SourceID string `json:"source_id"`
	}
	if err := json.Unmarshal(args, &params); err != nil {
		return nil, fmt.Errorf("parse search args: %w", err)
	}

	opts := SearchOptions{
		Limit:     params.Limit,
		Highlight: true,
	}
	if params.SourceID != "" {
		opts.SourceIDs = []string{params.SourceID}
	}

	result, err := s.searcher.Search(ctx, params.Query, opts)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	// Format results as content blocks
	var content []map[string]interface{}
	for _, hit := range result.Results {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": fmt.Sprintf("**%s** (score: %.2f)\nPath: %s\n\n%s",
				hit.Title, hit.Score, hit.Path, hit.Snippet),
		})
	}

	if len(content) == 0 {
		content = append(content, map[string]interface{}{
			"type": "text",
			"text": "No results found for: " + params.Query,
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
func (s *MCPServer) toolStats(ctx context.Context) (interface{}, error) {
	stats, err := s.indexer.GetStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("get stats: %w", err)
	}

	text := fmt.Sprintf(`# Knowledge Base Statistics

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
