# Conduit MCP Server Design Document

**Version**: 1.0.0
**Last Updated**: January 2026
**Status**: Active

---

## Table of Contents

1. [Overview](#overview)
2. [Architecture](#architecture)
3. [Security Model](#security-model)
4. [Tool Specifications](#tool-specifications)
5. [Configuration](#configuration)
6. [Integration Guide](#integration-guide)
7. [Protocol Reference](#protocol-reference)
8. [Troubleshooting](#troubleshooting)

---

## Overview

### Purpose

The Conduit MCP Server exposes the Knowledge Base to AI tools via the Model Context Protocol (MCP). It enables AI assistants like Claude Code, Cursor, and VS Code Copilot to search and retrieve information from your indexed documents.

### Design Principles

1. **AI Reads, Humans Write**: The MCP server provides read-only access. All administrative operations (add/remove/sync sources) are reserved for the CLI and native desktop application.

2. **Safety First**: No destructive operations are exposed via MCP to prevent accidental or adversarial knowledge base modifications.

3. **Graceful Degradation**: The server works with FTS5 (keyword search) alone and enhances with semantic search when Qdrant + Ollama are available.

4. **Source Awareness**: All search operations support filtering by source ID, enabling multi-KB scenarios where different projects have isolated knowledge bases.

### Capabilities

| Feature | Availability | Description |
|---------|--------------|-------------|
| Keyword Search (FTS5) | Always | SQLite full-text search with BM25 ranking |
| Semantic Search | Optional | Vector similarity via Qdrant + Ollama embeddings |
| Hybrid Search | When both available | Combines FTS5 + semantic with RRF fusion |
| Source Filtering | Always | Filter results by knowledge base source |
| Document Retrieval | Always | Full document content access |

---

## Architecture

### Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              AI Client                                   │
│                    (Claude Code, Cursor, VS Code)                        │
└─────────────────────────────────────┬───────────────────────────────────┘
                                      │ stdio (JSON-RPC 2.0)
                                      ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         conduit mcp kb                                   │
│                      (Native Binary Process)                             │
├─────────────────────────────────────────────────────────────────────────┤
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌────────────┐  │
│  │   Protocol   │  │    Tool      │  │  Capability  │  │   Result   │  │
│  │   Handler    │  │   Router     │  │  Detector    │  │  Processor │  │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └─────┬──────┘  │
│         │                 │                 │                │          │
│         └────────────────┬┴─────────────────┴────────────────┘          │
│                          ▼                                               │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │                      Hybrid Searcher                               │  │
│  │  ┌─────────────────┐              ┌─────────────────┐              │  │
│  │  │  FTS5 Searcher  │              │ Semantic Search │              │  │
│  │  │  (Always On)    │              │  (Optional)     │              │  │
│  │  └────────┬────────┘              └────────┬────────┘              │  │
│  │           │                                │                        │  │
│  └───────────┼────────────────────────────────┼────────────────────────┘  │
└──────────────┼────────────────────────────────┼─────────────────────────┘
               │                                │
               ▼                                ▼
┌──────────────────────────┐    ┌──────────────────────────────────────┐
│  ~/.conduit/conduit.db   │    │        Qdrant Container              │
│  (SQLite + FTS5)         │    │  (Vector Database - localhost:6333)  │
└──────────────────────────┘    └──────────────────────────────────────┘
                                              │
                                              ▼
                                ┌──────────────────────────────────────┐
                                │          Ollama Service              │
                                │  (Embeddings - localhost:11434)      │
                                └──────────────────────────────────────┘
```

### Process Flow

1. **Initialization**: `conduit mcp kb` starts, opens SQLite database, detects capabilities
2. **Capability Detection**: Checks FTS5 availability, Qdrant connection, Ollama model
3. **Request Loop**: Reads JSON-RPC requests from stdin, routes to tool handlers
4. **Response**: Writes JSON-RPC responses to stdout

### Data Flow for Search

```
Query: "authentication"
         │
         ▼
    ┌──────────────────┐
    │ Hybrid Searcher  │
    └────────┬─────────┘
             │
    ┌────────┴────────┐
    ▼                 ▼
┌────────┐      ┌──────────┐
│  FTS5  │      │ Semantic │
│ Search │      │  Search  │
└───┬────┘      └────┬─────┘
    │                │
    ▼                ▼
 BM25 Ranked     Cosine Similarity
  Results          Results
    │                │
    └───────┬────────┘
            ▼
    ┌───────────────┐
    │  RRF Fusion   │
    │ (1/(60+rank)) │
    └───────┬───────┘
            ▼
    ┌───────────────┐
    │ Result Post-  │
    │  Processing   │
    └───────┬───────┘
            ▼
    Merged, Deduped,
    Formatted Results
```

---

## Security Model

### Read-Only Access

The MCP server is intentionally read-only. This design decision prevents:

| Threat | Mitigation |
|--------|------------|
| Accidental deletion via AI | No delete operations exposed |
| Adversarial prompt injection | Cannot modify KB structure |
| Runaway sync operations | Sync not available via MCP |
| Unauthorized source addition | Add source requires CLI/native app |

### Operations NOT Available via MCP

| Operation | Why Restricted | Alternative |
|-----------|----------------|-------------|
| `kb add <path>` | Creates persistent state | Use `conduit kb add` CLI |
| `kb remove <source>` | Destructive | Use `conduit kb remove` CLI |
| `kb sync` | Resource-intensive | Use `conduit kb sync` CLI |
| `qdrant purge` | Destructive | Use `conduit qdrant purge` CLI |

### Path Security

Search results never expose:
- Paths outside indexed sources
- Paths in policy-forbidden directories
- Raw file system paths (uses relative paths where possible)

---

## Tool Specifications

### kb_search

Search the knowledge base for relevant documents using hybrid search.

**Input Schema**:
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query. Use short keyword phrases for best results (e.g., 'authentication JWT' rather than 'how does authentication work with JWT tokens')."
    },
    "source_id": {
      "type": "string",
      "description": "Filter results to a specific knowledge base source. Use kb_list_sources to see available source IDs."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum number of results (default: 10, max: 50)",
      "default": 10,
      "minimum": 1,
      "maximum": 50
    },
    "mode": {
      "type": "string",
      "enum": ["hybrid", "semantic", "fts5"],
      "description": "Search mode. 'hybrid' (default) combines keyword and semantic search. 'semantic' uses vector similarity only. 'fts5' uses keyword matching only.",
      "default": "hybrid"
    }
  },
  "required": ["query"]
}
```

**Output**: Array of search results with document ID, path, score, and snippet.

**Example**:
```json
{
  "name": "kb_search",
  "arguments": {
    "query": "ASL-3 deployment",
    "source_id": "anthropic-docs",
    "limit": 5
  }
}
```

---

### kb_search_with_context

Search with processed, prompt-ready results. Returns merged chunks with citations.

**Input Schema**:
```json
{
  "type": "object",
  "properties": {
    "query": {
      "type": "string",
      "description": "The search query for finding relevant context."
    },
    "source_id": {
      "type": "string",
      "description": "Filter to a specific source ID."
    },
    "limit": {
      "type": "integer",
      "description": "Maximum documents to return (default: 5)",
      "default": 5
    },
    "mode": {
      "type": "string",
      "enum": ["hybrid", "semantic", "fts5"],
      "default": "hybrid"
    }
  },
  "required": ["query"]
}
```

**Output**: Markdown-formatted context with merged chunks and source citations.

**Best For**: RAG (Retrieval-Augmented Generation) use cases where you need synthesized context.

---

### kb_list_sources

List all knowledge base sources with their IDs and statistics.

**Input Schema**:
```json
{
  "type": "object",
  "properties": {}
}
```

**Output**: List of sources with:
- Source ID (for filtering)
- Display name
- Path
- Document count
- Chunk count
- Last sync timestamp

**Example Output**:
```
Knowledge Base Sources

• anthropic-docs (ID: abc123)
  Path: /Users/name/docs/anthropic
  Documents: 15 | Chunks: 342 | Last sync: 2 hours ago

• project-notes (ID: def456)
  Path: /Users/name/projects/notes
  Documents: 87 | Chunks: 1,204 | Last sync: 1 day ago
```

---

### kb_get_document

Retrieve the full content of a document by its ID.

**Input Schema**:
```json
{
  "type": "object",
  "properties": {
    "document_id": {
      "type": "string",
      "description": "The document ID from search results."
    }
  },
  "required": ["document_id"]
}
```

**Output**: Full document content with metadata (title, path, size, MIME type).

---

### kb_stats

Get knowledge base statistics, optionally filtered by source.

**Input Schema**:
```json
{
  "type": "object",
  "properties": {
    "source_id": {
      "type": "string",
      "description": "Get stats for a specific source. If omitted, returns aggregate stats."
    }
  }
}
```

**Output**: Statistics including:
- Source count
- Document count
- Chunk count
- Total size
- Search capabilities (FTS5, semantic availability)

---

## Configuration

### Configuration File

Location: `~/.conduit/conduit.yaml`

```yaml
# MCP Server Configuration
mcp:
  kb:
    # Search behavior
    search:
      default_mode: hybrid      # hybrid, semantic, fts5
      default_limit: 10         # Default results per search
      max_limit: 50             # Maximum allowed limit
      semantic_fallback: true   # Fall back to FTS5 if semantic unavailable

    # Logging
    logging:
      level: info               # debug, info, warn, error
      to_stderr: false          # Log to stderr (visible in AI client)

# RAG tuning (also affects MCP search)
kb:
  rag:
    min_score: 0.0              # No filtering - return all results
    semantic_weight: 0.5        # Balance between semantic and keyword
    enable_mmr: true            # Diversity filtering
    mmr_lambda: 0.7             # Relevance vs diversity
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONDUIT_HOME` | Data directory | `~/.conduit` |
| `CONDUIT_MCP_LOG_LEVEL` | MCP-specific log level | `info` |
| `CONDUIT_MCP_MAX_LIMIT` | Max search results | `50` |

---

## Integration Guide

### Claude Code

**Automatic Registration** (via install script):
The installation script auto-registers Conduit KB with Claude Code.

**Manual Configuration**:
Add to `~/.claude.json`:

```json
{
  "mcpServers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"],
      "env": {
        "CONDUIT_HOME": "/Users/yourname/.conduit"
      }
    }
  }
}
```

**Verification**:
```bash
# In Claude Code, type:
/mcp

# You should see conduit-kb listed with its tools
```

---

### Cursor

Add to Cursor settings (`.cursor/settings/extensions.json`):

```json
{
  "mcpServers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}
```

---

### VS Code

Add to VS Code settings (`.vscode/settings.json`):

```json
{
  "mcp.servers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}
```

---

## Protocol Reference

### JSON-RPC 2.0

The MCP server uses JSON-RPC 2.0 over stdio.

**Request Format**:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "kb_search",
    "arguments": {
      "query": "authentication"
    }
  }
}
```

**Response Format**:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "Found 5 results for: authentication\n\n..."
      }
    ]
  }
}
```

**Error Format**:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "error": {
    "code": -32000,
    "message": "Search failed: database connection error"
  }
}
```

### Protocol Version

- **MCP Version**: 2024-11-05
- **Server Name**: conduit-kb
- **Server Version**: 1.0.0

---

## Troubleshooting

### Common Issues

#### "No results found" for domain-specific terms

**Symptom**: Searching for terms like "ASL-3" or "CBRN" returns no results.

**Cause**: These domain-specific terms may not embed well with the default model.

**Solution**: Use shorter, keyword-focused queries. The AI client should decompose complex questions into simple keyword searches.

---

#### "Semantic search unavailable"

**Symptom**: Hybrid search falls back to FTS5 only.

**Cause**: Qdrant container not running or Ollama model not available.

**Diagnosis**:
```bash
conduit qdrant status
conduit mcp status
```

**Solution**:
```bash
conduit qdrant start
# Or install if not present:
conduit qdrant install
```

---

#### MCP server not appearing in AI client

**Symptom**: `/mcp` in Claude Code doesn't show conduit-kb.

**Diagnosis**:
```bash
conduit mcp status
cat ~/.claude.json | jq '.mcpServers'
```

**Solution**:
1. Verify config file syntax
2. Check conduit binary is in PATH
3. Restart the AI client

---

#### Slow search performance

**Symptom**: Searches taking >1 second.

**Possible Causes**:
- Large knowledge base (>10,000 documents)
- Semantic search timeout (Qdrant/Ollama slow)

**Solutions**:
1. Use `--fts5` mode for faster keyword-only search
2. Check Qdrant container resources
3. Reduce result limit

---

### Diagnostic Commands

```bash
# Check MCP server health and capabilities
conduit mcp status

# View MCP-related logs
conduit mcp logs --tail 50

# Test MCP server manually (interactive)
echo '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' | conduit mcp kb

# Check database health
conduit kb stats

# Verify Qdrant connection
conduit qdrant status
```

---

## Appendix: Version History

| Version | Date | Changes |
|---------|------|---------|
| 1.0.0 | Jan 2026 | Initial release with read-only tools |

---

## Appendix: Future Considerations

The following features are intentionally not included in v1.0 but may be considered for future versions:

1. **Streaming responses**: For large result sets
2. **WebSocket transport**: Alternative to stdio for long-running connections
3. **Resource subscriptions**: Notify when sources change
4. **Custom prompt templates**: User-defined MCP prompts
5. **Tool access control**: Per-client tool restrictions

These will only be implemented if there's demonstrated need and after careful security review.
