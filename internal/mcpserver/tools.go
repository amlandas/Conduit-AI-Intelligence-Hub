package mcpserver

import (
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tool names. Exported so callers and tests can refer to them without string
// literals drifting.
const (
	ToolSearch            = "kb_search"
	ToolLexicalSearch     = "kb_lexical_search"
	ToolSearchWithContext = "kb_search_with_context"
	ToolListSources       = "kb_list_sources"
	ToolGetDocument       = "kb_get_document"
	ToolStats             = "kb_stats"
	ToolKAGQuery          = "kag_query"
)

// ToolNames lists every tool this server registers.
var ToolNames = []string{
	ToolSearch,
	ToolLexicalSearch,
	ToolSearchWithContext,
	ToolListSources,
	ToolGetDocument,
	ToolStats,
	ToolKAGQuery,
}

// ---------------------------------------------------------------------------
// Tool argument types.
//
// These mirror the anonymous structs the previous hand-rolled server
// unmarshalled tool arguments into, field-for-field and json-tag-for-json-tag,
// so argument names on the wire are unchanged.
// ---------------------------------------------------------------------------

type searchArgs struct {
	Query      string `json:"query"`
	Limit      int    `json:"limit"`
	SourceID   string `json:"source_id"`
	Mode       string `json:"mode"`
	RecallMode string `json:"recall_mode"`
}

type lexicalSearchArgs struct {
	Query    string `json:"query"`
	Limit    int    `json:"limit"`
	SourceID string `json:"source_id"`
}

type searchWithContextArgs struct {
	Query      string `json:"query"`
	SourceID   string `json:"source_id"`
	Limit      int    `json:"limit"`
	Mode       string `json:"mode"`
	RecallMode string `json:"recall_mode"`
}

type listSourcesArgs struct{}

type getDocumentArgs struct {
	DocumentID string `json:"document_id"`
}

type statsArgs struct {
	SourceID string `json:"source_id"`
}

type kagQueryArgs struct {
	Query            string   `json:"query"`
	Entities         []string `json:"entities"`
	IncludeRelations *bool    `json:"include_relations"`
	MaxHops          int      `json:"max_hops"`
	Limit            int      `json:"limit"`
	SourceID         string   `json:"source_id"`
}

// ---------------------------------------------------------------------------
// Schema helpers.
// ---------------------------------------------------------------------------

// object builds an object schema. A nil-vs-empty distinction matters here:
// jsonschema-go marshals a non-nil empty Properties map as `"properties": {}`,
// which is what the previous server emitted for argument-less tools.
func object(props map[string]*jsonschema.Schema, required ...string) *jsonschema.Schema {
	if props == nil {
		props = map[string]*jsonschema.Schema{}
	}
	return &jsonschema.Schema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

func str(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "string", Description: description}
}

func strEnum(description string, values ...string) *jsonschema.Schema {
	enum := make([]any, len(values))
	for i, v := range values {
		enum[i] = v
	}
	return &jsonschema.Schema{Type: "string", Description: description, Enum: enum}
}

func integer(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "integer", Description: description}
}

func boolean(description string) *jsonschema.Schema {
	return &jsonschema.Schema{Type: "boolean", Description: description}
}

func stringArray(description string) *jsonschema.Schema {
	return &jsonschema.Schema{
		Type:        "array",
		Items:       &jsonschema.Schema{Type: "string"},
		Description: description,
	}
}

// recallModeSchema is the shared recall_mode property. Its description is
// identical in kb_search and kb_search_with_context, exactly as before.
func recallModeSchema() *jsonschema.Schema {
	return strEnum(
		"Precision/recall tradeoff: 'high' (disable diversity filtering, get all similar results), 'balanced' (default, moderate filtering), 'precise' (aggressive deduplication)",
		"high", "balanced", "precise",
	)
}

// ---------------------------------------------------------------------------
// Tool registration.
//
// Descriptions below are carried over VERBATIM from the previous hand-rolled
// server (internal/kb/mcp_server.go). They are a deliberate asset: they teach
// AI clients how to query the knowledge base well. Do not "improve" them
// casually -- client prompts are tuned against this wording.
// ---------------------------------------------------------------------------

func (s *Server) registerTools() {
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        ToolSearch,
		Description: "Search the knowledge base for relevant documents using hybrid search (FTS5 keyword matching + semantic similarity when available). Use short keyword phrases for best results.",
		InputSchema: object(map[string]*jsonschema.Schema{
			"query":       str("The search query. Use short keyword phrases (e.g., 'authentication JWT' rather than 'how does authentication work with JWT tokens')."),
			"limit":       integer("Maximum number of results (default: 10, max: 50)"),
			"source_id":   str("Filter results to a specific knowledge base source. Use kb_list_sources to see available source IDs."),
			"mode":        strEnum("Search mode: 'hybrid' (default, best results), 'semantic' (vector similarity only), or 'fts5' (keyword matching only)", "hybrid", "semantic", "fts5"),
			"recall_mode": recallModeSchema(),
		}, "query"),
	}, s.toolSearch)

	// kb_lexical_search is new in the SDK port. It is the only tool that
	// bypasses the hybrid searcher entirely: raw FTS5/BM25, no vectors, no
	// fusion, no diversity filtering. The description is written for agentic
	// iterative keyword refinement -- a grep-style search/read/refine loop.
	mcp.AddTool(s.mcp, &mcp.Tool{
		Name: ToolLexicalSearch,
		Description: "Pure keyword search over the knowledge base using SQLite FTS5 with BM25 ranking. " +
			"No embeddings, no semantic expansion, no result fusion, no diversity filtering -- this is the grep of the knowledge base. " +
			"Results are deterministic and every hit is explained by the literal terms you supplied, which makes it the right tool for an iterative refinement loop: search, read the hits, adjust the keywords, search again. " +
			"Use it when you are hunting for a specific identifier, error string, function or symbol name, config key, file name, proper noun, or exact phrase you already know appears in the corpus. " +
			"Use kb_search instead when you need conceptual or paraphrase matching, and kb_search_with_context when you want merged, citation-ready passages rather than raw hits. " +
			"How to drive the loop: start with 2-4 distinctive terms (rare words beat common ones); if you get zero hits, drop the rarest or most misspelled term, or try a synonym or a shorter word stem; if you get too many hits, add another distinctive term or narrow with source_id from kb_list_sources; if hits look right but truncated, follow up with kb_get_document on the document_id. " +
			"Query handling: all terms are ANDed together, the last term gets a prefix match (so 'auth' also matches 'authentication'), and FTS5 operator characters (quotes, parentheses, +, *, ^, :, {}, [], hyphens) are stripped or split into separate terms -- boolean and phrase syntax is not available, so express intent through word choice. " +
			"Scores are raw BM25: they are negative, and more negative means more relevant. Results are returned in BM25 order, best first.",
		InputSchema: object(map[string]*jsonschema.Schema{
			"query":     str("Keyword query. Distinctive literal terms work best (e.g., 'ErrConnectionClosed retry' rather than 'how do I handle a dropped connection'). Terms are ANDed; the last term is prefix-matched."),
			"limit":     integer("Maximum number of hits (default: 10, max: 50)"),
			"source_id": str("Filter results to a specific knowledge base source. Use kb_list_sources to see available source IDs."),
		}, "query"),
	}, s.toolLexicalSearch)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        ToolListSources,
		Description: "List all knowledge base sources with their IDs, paths, document counts, and sync status. Use this to discover available sources before searching or filtering.",
		InputSchema: object(nil),
	}, s.toolListSources)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        ToolGetDocument,
		Description: "Retrieve the full content of a specific document by its ID. Use document IDs from search results.",
		InputSchema: object(map[string]*jsonschema.Schema{
			"document_id": str("The document ID from search results"),
		}, "document_id"),
	}, s.toolGetDocument)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        ToolStats,
		Description: "Get knowledge base statistics including source counts, document counts, chunk counts, and search capability status.",
		InputSchema: object(map[string]*jsonschema.Schema{
			"source_id": str("Get stats for a specific source. If omitted, returns aggregate stats for all sources."),
		}),
	}, s.toolStats)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        ToolSearchWithContext,
		Description: "Search with processed, prompt-ready results. Returns merged chunks from same documents, filters boilerplate, and provides citation-ready source information. Best for RAG use cases.",
		InputSchema: object(map[string]*jsonschema.Schema{
			"query":       str("The search query for finding relevant context"),
			"source_id":   str("Filter results to a specific source ID"),
			"limit":       integer("Maximum documents to return (default: 5)"),
			"mode":        strEnum("Search mode: 'hybrid' (default), 'semantic', or 'fts5'", "hybrid", "semantic", "fts5"),
			"recall_mode": recallModeSchema(),
		}, "query"),
	}, s.toolSearchWithContext)

	mcp.AddTool(s.mcp, &mcp.Tool{
		Name:        ToolKAGQuery,
		Description: "Query the knowledge graph for entities and their relationships. Use for multi-hop reasoning, aggregation queries, or finding connections between concepts. Complements RAG search with structured entity lookups.",
		InputSchema: object(map[string]*jsonschema.Schema{
			"query":             str("Natural language query or entity name to search for"),
			"entities":          stringArray("Optional list of specific entity names to find (more precise than free text)"),
			"include_relations": boolean("Include relationships between found entities (default: true)"),
			"max_hops":          integer("Maximum relationship hops to traverse (default: 2, max: 3)"),
			"limit":             integer("Maximum entities to return (default: 20)"),
			"source_id":         str("Filter to entities from a specific source"),
		}, "query"),
	}, s.toolKagQuery)
}
