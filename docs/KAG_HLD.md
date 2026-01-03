# Knowledge-Augmented Generation (KAG) - High-Level Design

**Version**: 1.0
**Date**: January 2026
**Status**: Implemented

---

## 1. Executive Summary

Conduit's KAG (Knowledge-Augmented Generation) system extends the existing RAG pipeline with a knowledge graph layer that captures entities and relationships from indexed documents. This enables multi-hop reasoning and aggregation queries that pure vector/keyword search cannot handle.

**Key Design Principles**:
- **Parallel Architecture**: KAG runs alongside RAG, not replacing it
- **Security First**: Opt-in, localhost-only, prompt injection protection
- **Local-First**: Uses Ollama/Mistral 7B by default (Apache 2.0 license)
- **LLM-Friendly**: MCP tool designed for AI consumption with clear schemas

---

## 2. Architecture Overview

### 2.1 Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              Document Ingestion                              │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌────────────────────────┐   │
│  │ Extract  │ → │  Clean   │ → │  Chunk   │ → │    Store Chunks        │   │
│  └──────────┘   └──────────┘   └──────────┘   └────────────────────────┘   │
│                                      │                                       │
│                    ┌─────────────────┴─────────────────┐                    │
│                    ▼                                   ▼                    │
│  ┌─────────────────────────────────┐  ┌────────────────────────────────┐   │
│  │       RAG Pipeline (Existing)   │  │       KAG Pipeline (New)       │   │
│  │  ┌──────────┐  ┌─────────────┐  │  │  ┌─────────────────────────┐   │   │
│  │  │ Embed    │→ │   Qdrant    │  │  │  │  Entity/Relation        │   │   │
│  │  │ (Ollama) │  │  (vectors)  │  │  │  │  Extraction             │   │   │
│  │  └──────────┘  └─────────────┘  │  │  │  (Mistral 7B/Ollama)    │   │   │
│  │                                 │  │  └───────────┬─────────────┘   │   │
│  │  ┌──────────┐                   │  │              ▼                 │   │
│  │  │  FTS5    │                   │  │  ┌─────────────────────────┐   │   │
│  │  │ (SQLite) │                   │  │  │   SQLite Graph Tables   │   │   │
│  │  └──────────┘                   │  │  │   (kb_entities,         │   │   │
│  └─────────────────────────────────┘  │  │    kb_relations)        │   │   │
│                                       │  └─────────────────────────┘   │   │
│                                       │              │                 │   │
│                                       │              ▼                 │   │
│                                       │  ┌─────────────────────────┐   │   │
│                                       │  │   FalkorDB (optional)   │   │   │
│                                       │  │   (graph traversal)     │   │   │
│                                       │  └─────────────────────────┘   │   │
│                                       └────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                              MCP Tools                                       │
│  ┌────────────────┐  ┌─────────────────────┐  ┌─────────────────────────┐   │
│  │   kb_search    │  │ kb_search_with      │  │      kag_query          │   │
│  │ (hybrid RAG)   │  │ _context (RAG)      │  │   (graph query)         │   │
│  └────────────────┘  └─────────────────────┘  └─────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Data Flow

```
                    INDEXING PIPELINE (with KAG)
                    ────────────────────────────
Document ──▶ Extraction ──▶ Cleaning ──▶ Chunking ──▶ ┬──▶ FTS5 Index
    │           │              │            │         │
    │      (pdftotext,    (boilerplate,  (smart      ├──▶ Embedding ──▶ Qdrant
    │       textutil)      OCR fixes)   chunking)    │
    │                                                 └──▶ Entity Extraction ──▶ Graph
    │                                                            │
    │                                                            ▼
    │                                                   ┌─────────────────┐
    │                                                   │ LLM Provider    │
    │                                                   │ (Ollama/OpenAI/ │
    │                                                   │  Anthropic)     │
    │                                                   └────────┬────────┘
    │                                                            │
    │                                                            ▼
    │                                                   ┌─────────────────┐
    │                                                   │ Validator       │
    │                                                   │ (confidence,    │
    │                                                   │  sanitization)  │
    │                                                   └────────┬────────┘
    │                                                            │
    │                                                            ▼
    └────────────────────────────────────────────────────▶ kb_entities
                                                         │ kb_relations
                                                         │ kb_extraction_status

                    KAG QUERY PIPELINE
                    ──────────────────
Query ──▶ kag_query tool ──▶ KAGSearcher ──▶ ┬──▶ Entity Search (SQLite)
                   │                         │
                   │                         ├──▶ Relation Lookup
                   │                         │
                   │                         └──▶ Graph Traversal (FalkorDB)
                   │                                      │
                   │                                      ▼
                   └────────────────────────────▶ Formatted Context
                                                 (for LLM consumption)
```

---

## 3. Technology Choices

### 3.1 Graph Database: FalkorDB

| Aspect | Details |
|--------|---------|
| **License** | Apache 2.0 (fully permissive) |
| **Query Language** | Cypher (Neo4j compatible) |
| **Performance** | 500x faster p99 latency vs Neo4j |
| **Deployment** | Docker (Redis-based) |
| **Why chosen** | Apache 2.0, fast, Cypher, GraphRAG-optimized |

**Note**: FalkorDB is optional. SQLite-based entity/relation tables provide base functionality.

### 3.2 LLM for Extraction: Mistral 7B Instruct

| Aspect | Details |
|--------|---------|
| **License** | Apache 2.0 |
| **NER F1 Score** | 0.6376 (best among 7B models) |
| **RAM Usage** | ~4.1 GB (Q4_K_M quantization) |
| **Speed** | 10-15% faster on Apple Silicon |
| **Ollama model** | `mistral:7b-instruct-q4_K_M` |

### 3.3 Multi-Provider Support

| Provider | Models | Use Case |
|----------|--------|----------|
| **Ollama** (default) | Mistral 7B, Llama 3 | Local, privacy-first |
| **OpenAI** | GPT-4o, GPT-4o-mini | Higher quality extraction |
| **Anthropic** | Claude 3.5 Sonnet | Alternative cloud option |

---

## 4. Entity & Relation Types

### 4.1 Entity Types

| Type | Description | Examples |
|------|-------------|----------|
| `concept` | Abstract ideas, terms, topics | "Authentication", "Machine Learning" |
| `person` | Named individuals | "Alan Turing", "Linus Torvalds" |
| `organization` | Companies, institutions | "Google", "MIT", "Oak Ridge Lab" |
| `technology` | Tools, frameworks, languages | "Kubernetes", "React", "Go" |
| `location` | Geographic entities | "Silicon Valley", "Oak Ridge" |
| `section` | Document structure | "Chapter 3", "Introduction" |

### 4.2 Relation Types

| Type | Description | Example |
|------|-------------|---------|
| `mentions` | Entity references another | "Document mentions Kubernetes" |
| `defines` | Entity defines another | "RFC defines OAuth2" |
| `relates_to` | General relationship | "Authentication relates to Security" |
| `contains` | Hierarchical containment | "Chapter contains Section" |
| `part_of` | Membership/component | "Go is part of Kubernetes" |
| `uses` | Utilization relationship | "Application uses Redis" |

---

## 5. Security Architecture

### 5.1 Security Layers

```
┌─────────────────────────────────────────────────────────────────┐
│                      Security Architecture                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. INPUT VALIDATION                                             │
│     ├─ Query length limits (10KB max)                           │
│     ├─ Entity hint limits (50 max)                              │
│     └─ JSON schema validation                                    │
│                                                                  │
│  2. PROMPT INJECTION PROTECTION                                  │
│     ├─ sanitizePromptInput() filters suspicious patterns        │
│     ├─ Structured prompts with clear delimiters                 │
│     └─ Validator rejects suspicious entity names                │
│                                                                  │
│  3. EXTRACTION VALIDATION                                        │
│     ├─ Confidence threshold (default 0.6)                       │
│     ├─ Entity name length limits                                 │
│     ├─ Type normalization (whitelist)                           │
│     └─ Suspicious content filtering                             │
│                                                                  │
│  4. NETWORK ISOLATION                                            │
│     ├─ FalkorDB localhost-only by default                       │
│     └─ No external graph DB without explicit config             │
│                                                                  │
│  5. OPT-IN ACTIVATION                                            │
│     └─ KAG disabled by default (kag.enabled: false)             │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 5.2 Prompt Injection Protection

The `sanitizePromptInput()` function filters:
- "ignore previous instructions"
- Closing XML/delimiter tags
- System role injection attempts
- Unicode obfuscation patterns

### 5.3 Suspicious Content Detection

The `ExtractionValidator` filters entities with:
- Names containing injection patterns
- Unusually long descriptions
- Low confidence scores (<0.6)
- Empty or whitespace-only names

---

## 6. MCP Tool Integration

### 6.1 kag_query Tool Schema

```json
{
  "name": "kag_query",
  "description": "Query the knowledge graph for entities and their relationships. Use this for questions about concepts, people, organizations, or how things relate to each other in the indexed documents.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Natural language question or search terms"
      },
      "entities": {
        "type": "array",
        "items": {"type": "string"},
        "description": "Optional: specific entity names to search for"
      },
      "include_relations": {
        "type": "boolean",
        "default": true,
        "description": "Include relationships between entities"
      },
      "max_hops": {
        "type": "integer",
        "default": 2,
        "maximum": 3,
        "description": "Maximum relationship hops for graph traversal"
      },
      "limit": {
        "type": "integer",
        "default": 20,
        "maximum": 100,
        "description": "Maximum number of entities to return"
      },
      "source_id": {
        "type": "string",
        "description": "Optional: limit search to specific KB source"
      }
    },
    "required": ["query"]
  }
}
```

### 6.2 Response Format

```json
{
  "entities": [
    {
      "id": "ent_abc123",
      "name": "Kubernetes",
      "type": "technology",
      "description": "Container orchestration platform",
      "confidence": 0.95,
      "source_document_id": "doc_123",
      "source_document_title": "Cloud Native Guide"
    }
  ],
  "relations": [
    {
      "subject": "Kubernetes",
      "predicate": "uses",
      "object": "Docker",
      "confidence": 0.88
    }
  ],
  "context": "Knowledge Graph Results for: container orchestration\n\n## Entities\n- **Kubernetes** (technology): Container orchestration platform\n...",
  "total_entities": 15
}
```

---

## 7. CLI Commands

### 7.1 FalkorDB Management

```bash
# Install FalkorDB container
conduit falkordb install

# Start FalkorDB
conduit falkordb start

# Stop FalkorDB
conduit falkordb stop

# Check status
conduit falkordb status
```

### 7.2 KAG Operations

```bash
# Sync entities from indexed documents
conduit kb kag-sync [--source <id>]

# Show extraction status
conduit kb kag-status

# Query the knowledge graph
conduit kb kag-query "What technologies are mentioned?"
```

---

## 8. Configuration

### 8.1 Default Configuration

```yaml
kb:
  kag:
    # SECURITY: Opt-in only
    enabled: false

    # LLM provider for extraction
    provider: ollama

    # Ollama settings
    ollama:
      host: http://localhost:11434
      model: mistral:7b-instruct-q4_K_M

    # Extraction settings
    extraction:
      confidence_threshold: 0.6
      max_entities_per_chunk: 20
      max_relations_per_chunk: 30
      batch_size: 10
      enable_background: true
      background_workers: 2
      queue_size: 1000

    # Graph database (optional)
    graph:
      backend: sqlite  # or "falkordb"
      falkordb:
        host: localhost
        port: 6379
        graph_name: conduit_kg
```

### 8.2 OpenAI/Anthropic Configuration

```yaml
kb:
  kag:
    enabled: true
    provider: openai  # or "anthropic"
    openai:
      model: gpt-4o-mini
      # API key from environment: OPENAI_API_KEY
    anthropic:
      model: claude-3-5-sonnet-20241022
      # API key from environment: ANTHROPIC_API_KEY
```

---

## 9. Database Schema

### 9.1 SQLite Tables

```sql
-- Extracted entities
CREATE TABLE IF NOT EXISTS kb_entities (
    entity_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    description TEXT,
    source_chunk_id TEXT,
    source_document_id TEXT,
    confidence REAL NOT NULL DEFAULT 0.0,
    metadata TEXT,
    created_at TEXT,
    updated_at TEXT,
    FOREIGN KEY (source_chunk_id) REFERENCES kb_chunks(chunk_id),
    FOREIGN KEY (source_document_id) REFERENCES kb_documents(document_id)
);

-- Entity relationships
CREATE TABLE IF NOT EXISTS kb_relations (
    relation_id TEXT PRIMARY KEY,
    subject_id TEXT NOT NULL,
    predicate TEXT NOT NULL,
    object_id TEXT NOT NULL,
    source_chunk_id TEXT,
    confidence REAL NOT NULL DEFAULT 0.0,
    metadata TEXT,
    created_at TEXT,
    FOREIGN KEY (subject_id) REFERENCES kb_entities(entity_id),
    FOREIGN KEY (object_id) REFERENCES kb_entities(entity_id)
);

-- Extraction status tracking
CREATE TABLE IF NOT EXISTS kb_extraction_status (
    chunk_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,
    entity_count INTEGER DEFAULT 0,
    relation_count INTEGER DEFAULT 0,
    error_message TEXT,
    extracted_at TEXT,
    updated_at TEXT,
    FOREIGN KEY (chunk_id) REFERENCES kb_chunks(chunk_id)
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_entities_name ON kb_entities(name);
CREATE INDEX IF NOT EXISTS idx_entities_type ON kb_entities(type);
CREATE INDEX IF NOT EXISTS idx_entities_source_doc ON kb_entities(source_document_id);
CREATE INDEX IF NOT EXISTS idx_relations_subject ON kb_relations(subject_id);
CREATE INDEX IF NOT EXISTS idx_relations_object ON kb_relations(object_id);
CREATE INDEX IF NOT EXISTS idx_relations_predicate ON kb_relations(predicate);
CREATE INDEX IF NOT EXISTS idx_extraction_status ON kb_extraction_status(status);
```

---

## 10. Performance Characteristics

### 10.1 Latency Budget

| Operation | Current | Target | Notes |
|-----------|---------|--------|-------|
| Entity search | ~10-30ms | <50ms | SQLite query |
| Relation lookup | ~5-15ms | <30ms | SQLite join |
| Context formatting | ~1-5ms | <10ms | String processing |
| **Total kag_query** | ~20-60ms | **<100ms** | Fast for MCP |

### 10.2 Extraction Performance

| Component | Time per Chunk | Notes |
|-----------|----------------|-------|
| Ollama (Mistral 7B) | ~2-5s | Local, CPU |
| Ollama (Mistral 7B) | ~0.5-1s | With GPU |
| OpenAI (GPT-4o-mini) | ~0.5-1.5s | API latency |
| Validation + Storage | ~10-50ms | Fast |

### 10.3 RAM Budget (32GB Apple Silicon)

| Component | RAM |
|-----------|-----|
| macOS + Apps | ~8 GB |
| nomic-embed-text | ~0.3 GB |
| qwen2.5-coder (chat) | ~4.5 GB |
| mistral:7b (KAG) | ~4.1 GB |
| Qdrant | ~1-2 GB |
| FalkorDB | ~1-2 GB |
| **Total** | ~20 GB |
| **Headroom** | ~12 GB |

---

## 11. Graceful Degradation

| Scenario | Behavior | Impact |
|----------|----------|--------|
| KAG disabled | kag_query returns empty | RAG still works |
| Ollama unavailable | Extraction fails gracefully | Existing entities remain |
| FalkorDB unavailable | SQLite-only mode | No multi-hop traversal |
| Low confidence results | Filtered by validator | Higher precision |

---

## 12. Future Enhancements

### 12.1 Short-Term

1. **Entity Deduplication**: Merge similar entities across documents
2. **Incremental Updates**: Only re-extract modified chunks
3. **Entity Linking**: Connect to external knowledge bases

### 12.2 Long-Term

1. **Temporal Reasoning**: Track entity changes over time
2. **Entity Resolution**: Disambiguate similar names
3. **Hybrid RAG+KAG**: Fuse vector and graph results
4. **Semantic Relations**: Use embeddings for relation discovery

---

## 13. References

- [FalkorDB Documentation](https://docs.falkordb.com/)
- [Ollama Model Library](https://ollama.ai/library)
- [MCP Protocol Specification](https://modelcontextprotocol.io/)
- [Microsoft GraphRAG Paper](https://arxiv.org/abs/2404.16130)

---

**Document History**:
| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | Jan 2026 | Conduit Team | Initial HLD |
