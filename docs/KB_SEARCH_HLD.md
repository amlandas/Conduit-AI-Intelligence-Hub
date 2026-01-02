# Knowledge Base Search - High-Level Design

**Version**: 1.0
**Date**: January 2026
**Status**: Implemented

---

## 1. Executive Summary

Conduit's Knowledge Base (KB) search system implements a hybrid retrieval architecture optimized for AI tool consumption via MCP (Model Context Protocol). The system combines lexical search (SQLite FTS5), semantic search (Qdrant + Ollama embeddings), and intelligent fusion to deliver high-quality, contextually relevant results.

**Key Design Principles**:
- **AI-First**: Optimize for AI tool consumption, not human reading
- **Never Zero Results**: Always return something; include confidence metadata
- **Semantic > Lexical**: For AI consumers, meaning matters more than exact words
- **Low Latency**: Target <150ms for MCP tool calls

---

## 2. Architecture Overview

### 2.1 Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              AI Clients                                  │
│         (Claude Code, Cursor, VS Code, Gemini CLI)                       │
└─────────────────────────────────────┬───────────────────────────────────┘
                                      │ MCP Protocol
                                      ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                           MCP Server Layer                              │
│                        (internal/kb/mcp.go)                             │
│   ┌─────────────────────────────────────────────────────────────────┐   │
│   │  Tools: kb_search, kb_add_source, kb_list, kb_sync, kb_stats    │   │
│   └─────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────┬───────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                          Hybrid Searcher                                │
│                    (internal/kb/hybrid_search.go)                       │
│   ┌──────────────────────────────────────────────────────────────────┐  │
│   │  Query Analysis → Parallel Search → RRF Fusion → Post-Processing │  │
│   └──────────────────────────────────────────────────────────────────┘  │
│         │                    │                    │                     │
│         ▼                    ▼                    ▼                     │
│   ┌──────────┐        ┌──────────┐        ┌──────────────┐             │
│   │FTS5 Search│        │ Semantic │        │Post-Processing│             │
│   │(Searcher) │        │(Semantic │        │ (MMR, Floor,  │             │
│   │           │        │ Searcher)│        │  Reranking)   │             │
│   └─────┬────┘        └────┬─────┘        └──────────────┘             │
│         │                   │                                           │
└─────────┼───────────────────┼───────────────────────────────────────────┘
          │                   │
          ▼                   ▼
┌───────────────────┐  ┌─────────────────────────────────────────────────┐
│   SQLite FTS5     │  │              Vector Layer                        │
│  (kb_chunks_fts)  │  │  ┌─────────────────┐  ┌─────────────────────┐   │
│                   │  │  │   Embeddings    │  │    Vector Store     │   │
│   BM25 Ranking    │  │  │ (Ollama + nomic │  │      (Qdrant)       │   │
│   Phrase Search   │  │  │  -embed-text)   │  │                     │   │
│   Boolean Ops     │  │  │                 │  │  Cosine Similarity  │   │
└───────────────────┘  │  └─────────────────┘  └─────────────────────┘   │
                       └─────────────────────────────────────────────────┘
```

### 2.2 Data Flow

```
                    INDEXING PIPELINE
                    ─────────────────
Document ──▶ Extraction ──▶ Cleaning ──▶ Chunking ──▶ ┬──▶ FTS5 Index
    │           │              │            │         │
    │      (pdftotext,    (boilerplate,  (smart    │
    │       textutil)      OCR fixes)   chunking)   └──▶ Embedding ──▶ Qdrant
    │
    └───────────────────────────────────────────────────────────────────────

                    RETRIEVAL PIPELINE
                    ──────────────────
Query ──▶ Analysis ──▶ ┬──▶ FTS5 Search ───────────┬──▶ RRF Fusion
              │        │                           │         │
              │        ├──▶ Semantic Search ───────┤         ▼
              │        │                           │    MMR Diversity
              │        └──▶ Entity Boosting ───────┘         │
              │                                              ▼
              │                                    Similarity Floor
              │                                              │
              └──────────────── Query Type ─────────────────▶│
                                                             ▼
                                                       Reranking
                                                             │
                                                             ▼
                                                   Final Results
                                                   + Confidence
```

---

## 3. Search Strategies

### 3.1 FTS5 Lexical Search

**Purpose**: Exact keyword and phrase matching using SQLite's Full-Text Search.

**Implementation**: `internal/kb/searcher.go`

```go
// BM25 ranking query
SELECT
    d.id, d.path, d.source_id,
    c.content, c.chunk_index,
    bm25(kb_chunks_fts) as score
FROM kb_chunks_fts f
JOIN kb_chunks c ON f.rowid = c.rowid
JOIN kb_documents d ON c.document_id = d.id
WHERE kb_chunks_fts MATCH ?
ORDER BY score
LIMIT ?
```

**Strengths**:
- Fast: ~5-20ms for typical queries
- Exact phrase matching ("Oak Ridge")
- Boolean operators (AND, OR, NOT)
- No external dependencies

**Weaknesses**:
- No semantic understanding
- "car" won't match "automobile"
- Requires exact keyword presence

### 3.2 Semantic Search

**Purpose**: Meaning-based search using vector embeddings.

**Implementation**: `internal/kb/semantic_search.go`

**Architecture**:
```
Query ──▶ Ollama (nomic-embed-text) ──▶ 768-dim vector
                                              │
                                              ▼
                          Qdrant ──▶ Cosine Similarity Search
                                              │
                                              ▼
                                    Top-K Similar Chunks
```

**Configuration**:
| Parameter | Value | Rationale |
|-----------|-------|-----------|
| Embedding Model | nomic-embed-text | Good quality, local, 768 dimensions |
| Vector Dimension | 768 | Model output size |
| Distance Metric | Cosine | Standard for text similarity |
| Batch Size | 10 | Balance throughput vs memory |

**Strengths**:
- Semantic understanding ("authentication" ≈ "login")
- Natural language queries
- Concept-based matching

**Weaknesses**:
- Slower: ~50-200ms (embedding generation)
- Exact phrases may score lower than semantic near-matches
- Requires Qdrant and Ollama running

### 3.3 Hybrid RRF Fusion

**Purpose**: Combine lexical and semantic results for best of both worlds.

**Implementation**: `internal/kb/hybrid_search.go`

**Reciprocal Rank Fusion (RRF) Formula**:
```
RRF_score(doc) = Σ (weight_i / (k + rank_i(doc)))

Where:
  k = 60 (smoothing constant)
  weight_i = strategy weight (default: 0.5 each)
  rank_i = position in strategy's result list (1-indexed)
```

**Example**:
```
Document appears at:
  - FTS5 rank 1:  0.5 × 1/(60+1) = 0.0082
  - Semantic rank 3: 0.5 × 1/(60+3) = 0.0079
  - Combined RRF: 0.0161

Document appears only in FTS5 rank 1:
  - RRF = 0.5 × 1/(60+1) = 0.0082

Document appears only in Semantic rank 1:
  - RRF = 0.5 × 1/(60+1) = 0.0082
```

**Why RRF**:
- Rank-based (not score-based) → handles different score scales
- Robust to outliers
- Simple, no hyperparameter tuning needed
- Industry standard for hybrid search

---

## 4. Query-Adaptive Confidence Model

### 4.1 Problem Statement

Traditional search ranking assumes lexical match = high confidence. However, for AI tool consumption:
- Semantic relevance often matters more than exact words
- Conceptual queries ("how does authentication work") need meaning, not keywords
- A hard similarity floor can filter valid results

### 4.2 Solution: Parallel Search with Agreement-Based Confidence

**Run all strategies in parallel, score by agreement:**

```
Query ──▶ ┬──▶ Exact Match
          ├──▶ Semantic Search
          ├──▶ Hybrid RRF
          └──▶ Relaxed Match
                    │
                    ▼
            Agreement Analysis
            ──────────────────
            doc_A: Found by 3/4 → VERY HIGH
            doc_B: Found by 2/4 → HIGH
            doc_C: Semantic only → MEDIUM-HIGH (conceptual query)
            doc_D: Lexical only  → MEDIUM (words match, meaning may not)
```

### 4.3 Confidence Scoring Matrix

| Scenario | Confidence | Rationale |
|----------|------------|-----------|
| Multiple strategies agree | VERY HIGH | Cross-validation |
| Semantic + entity boost | HIGH | Meaning + specificity |
| Semantic only (conceptual query) | HIGH | Captures intent |
| Hybrid RRF | MEDIUM-HIGH | Balanced |
| Lexical only | MEDIUM | Words match, meaning may not |
| Relaxed/Partial only | LOW | Weak signal |

### 4.4 Query Type Detection

| Query Pattern | Detection | Preferred Strategy |
|---------------|-----------|-------------------|
| `"exact phrase"` | Quoted | Lexical (exact match) |
| Multi-word caps (Oak Ridge) | Proper noun | Hybrid + entity boost |
| Natural language | Question words | Semantic |
| Technical terms | Domain vocab | Hybrid |

---

## 5. Post-Processing Pipeline

### 5.1 MMR (Maximal Marginal Relevance)

**Purpose**: Reduce redundancy in results by penalizing near-duplicates.

**Formula**:
```
MMR = argmax[λ × sim(doc, query) - (1-λ) × max(sim(doc, selected_docs))]

Where:
  λ = 0.7 (70% relevance, 30% diversity)
  sim = Jaccard text similarity
```

**Implementation**:
```go
// Greedy selection with diversity penalty
for len(selected) < limit {
    bestScore := -1.0
    for _, candidate := range remaining {
        relevance := candidate.Score
        maxSimilarity := maxSimilarityToSelected(candidate, selected)
        mmrScore := lambda*relevance - (1-lambda)*maxSimilarity
        if mmrScore > bestScore {
            bestCandidate = candidate
            bestScore = mmrScore
        }
    }
    selected = append(selected, bestCandidate)
}
```

### 5.2 Similarity Floor

**Purpose**: Filter low-confidence results.

**Configuration**:
```go
const DefaultSimilarityFloor = 0.001  // Minimum RRF score
```

**Critical Learning**: Initial floor of 0.01 was too aggressive. Single-strategy RRF scores (~0.008) fell below, causing "zero results" for valid queries.

### 5.3 Reranking

**Purpose**: Re-score top candidates using semantic similarity.

**Implementation**:
```go
// Re-score using cosine similarity between query and result embeddings
for _, result := range topCandidates {
    semanticScore := cosineSimilarity(queryEmbedding, resultEmbedding)
    result.Score = rerankWeight*semanticScore + (1-rerankWeight)*result.Score
}
```

---

## 6. Data Model

### 6.1 Database Schema

```sql
-- Document sources (directories, files)
CREATE TABLE kb_sources (
    id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    type TEXT NOT NULL,         -- 'directory' or 'file'
    pattern TEXT,               -- glob pattern for directory
    created_at TIMESTAMP,
    updated_at TIMESTAMP
);

-- Indexed documents
CREATE TABLE kb_documents (
    id TEXT PRIMARY KEY,
    source_id TEXT NOT NULL,
    path TEXT NOT NULL,
    content_hash TEXT,          -- SHA256 of cleaned content
    indexed_at TIMESTAMP,
    FOREIGN KEY (source_id) REFERENCES kb_sources(id)
);

-- Document chunks
CREATE TABLE kb_chunks (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL,
    chunk_index INTEGER,
    content TEXT,
    start_offset INTEGER,
    end_offset INTEGER,
    FOREIGN KEY (document_id) REFERENCES kb_documents(id)
);

-- FTS5 virtual table for lexical search
CREATE VIRTUAL TABLE kb_chunks_fts USING fts5(
    content,
    content='kb_chunks',
    content_rowid='rowid'
);
```

### 6.2 Vector Store (Qdrant)

**Collection**: `conduit_kb`

**Schema**:
```json
{
  "vectors": {
    "size": 768,
    "distance": "Cosine"
  },
  "payload": {
    "chunk_id": "string",
    "document_id": "string",
    "source_id": "string",
    "path": "string",
    "content": "string",
    "chunk_index": "integer"
  }
}
```

---

## 7. Performance Characteristics

### 7.1 Latency Budget

| Component | Current | Target | Notes |
|-----------|---------|--------|-------|
| Query parsing | <1ms | <1ms | Fast |
| FTS5 search | ~5-20ms | ~10ms | SQLite, local |
| Semantic search | ~50-200ms | ~100ms | Qdrant + Ollama embedding |
| RRF fusion | ~1-5ms | <5ms | In-memory |
| MMR + Reranking | ~10-50ms | <30ms | Text similarity |
| **Total** | ~70-280ms | **<150ms** | Target for MCP |

### 7.2 Optimization Strategies

1. **Embedding Cache**: Cache query embeddings for repeated searches
2. **Connection Pooling**: Keep-alive connections to Qdrant and Ollama
3. **Parallel Execution**: Run FTS5 and semantic search concurrently
4. **Result Limits**: Fetch only top-K from each strategy (default: 40)

### 7.3 Resource Usage

| Resource | Idle | Active Query | Peak |
|----------|------|--------------|------|
| Memory | ~50MB | ~100MB | ~200MB |
| Qdrant Memory | ~100MB | ~150MB | ~300MB |
| Ollama Memory | ~2GB | ~3GB | ~4GB |

---

## 8. MCP Integration

### 8.1 Tool Definition

```json
{
  "name": "kb_search",
  "description": "Search the knowledge base for relevant documents",
  "inputSchema": {
    "type": "object",
    "properties": {
      "query": {
        "type": "string",
        "description": "Search query"
      },
      "mode": {
        "type": "string",
        "enum": ["auto", "hybrid", "semantic", "fts5"],
        "default": "auto"
      },
      "limit": {
        "type": "integer",
        "default": 10,
        "maximum": 50
      }
    },
    "required": ["query"]
  }
}
```

### 8.2 Response Format

```json
{
  "results": [
    {
      "path": "/docs/auth.md",
      "snippet": "User authentication is handled by...",
      "score": 0.87,
      "source": "Documentation"
    }
  ],
  "confidence": "high",
  "strategies_matched": ["semantic", "hybrid"],
  "query_type": "conceptual",
  "search_time_ms": 145
}
```

### 8.3 Timeout Recommendations

| Setting | Value | Rationale |
|---------|-------|-----------|
| Search timeout | 5s | Fail fast, let client retry |
| Embedding timeout | 10s | Ollama can be slow on first call |
| Total request timeout | 15s | Well under AI model limits |

**AI Model Tool Call Timeouts**:
| Model | Default | Max |
|-------|---------|-----|
| Claude (Anthropic) | 60s | 600s |
| GPT-4/5 (OpenAI) | 60s | 300s |
| Gemini (Google) | 30s | 120s |

---

## 9. Graceful Degradation

The system degrades gracefully when components are unavailable:

| Scenario | Behavior | Quality Impact |
|----------|----------|----------------|
| Qdrant unavailable | FTS5-only mode | No semantic search |
| Ollama unavailable | FTS5-only mode | No embeddings |
| Both unavailable | FTS5-only mode | Keyword search only |
| FTS5 unavailable | Error (critical) | Cannot operate |

**Fallback Logging**:
```go
if err := s.semantic.Search(...); err != nil {
    s.logger.Warn().Err(err).Msg("semantic search unavailable, using FTS5 only")
    return s.fts5Only(query, opts)
}
```

---

## 9.5 Data Lifecycle: Source Removal

When a KB source is removed, all associated data must be cleaned up from both storage layers.

### Deletion Order (Critical)

```
SourceManager.Remove(sourceID)
    │
    ├─1─► indexer.DeleteBySource(sourceID)      # Vectors FIRST
    │         └─► semantic.DeleteBySource()
    │                 └─► vectorStore.DeleteBySource() [filter: source_id]
    │
    ├─2─► SQL: DELETE FROM kb_fts              # Then FTS5
    ├─3─► SQL: DELETE FROM kb_chunks           # Then chunks
    ├─4─► SQL: DELETE FROM kb_documents        # Then documents
    └─5─► SQL: DELETE FROM kb_sources          # Finally source
```

**Why this order?**
1. Vector deletion uses `source_id` filter in Qdrant - requires SQLite records to still exist
2. SQLite deletion order respects foreign key constraints
3. Partial failure (e.g., Qdrant down) doesn't orphan SQLite data

### API Response

The deletion endpoint returns statistics for transparency:

```json
{
  "documents_deleted": 5,
  "vectors_deleted": 50
}
```

### CLI Display

```
$ conduit kb remove "Project Docs"
✓ Removed source: Project Docs (5 documents, 50 vectors)
```

### Graceful Handling

If semantic search is not enabled, vector deletion is skipped gracefully:

```go
func (idx *Indexer) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
    if idx.semantic == nil {
        return 0, nil  // No vectors to delete
    }
    return idx.semantic.DeleteBySource(ctx, sourceID)
}
```

---

## 10. Future Enhancements

### 10.1 Short-Term

1. **Cross-Encoder Reranking**: Use transformer-based models for higher quality reranking
2. **Query Expansion**: Add synonyms for better recall
3. **Caching Layer**: Redis cache for frequent queries

### 10.2 Long-Term

1. **Hierarchical Indexing**: Document → Section → Paragraph levels
2. **User Feedback Loop**: Learn from explicit ratings
3. **Multi-Modal**: Support images, diagrams with vision models
4. **Graph RAG**: Entity relationships for complex queries

---

## 11. References

- [Reciprocal Rank Fusion (RRF)](https://www.cs.cmu.edu/~jgc/publication/The_Use_of_MMR_Diversity-Based_Reranking_1998.pdf)
- [SQLite FTS5](https://www.sqlite.org/fts5.html)
- [Qdrant Documentation](https://qdrant.tech/documentation/)
- [nomic-embed-text Model](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5)
- [MCP Protocol Specification](https://modelcontextprotocol.io/)

---

**Document History**:
| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | Jan 2026 | Conduit Team | Initial HLD |
| 1.1 | Jan 2026 | Conduit Team | Added Section 9.5: Data Lifecycle (vector cleanup on KB removal) |
