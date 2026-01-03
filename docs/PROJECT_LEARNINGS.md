# Conduit Project Learnings

This document captures the key insights, decisions, and adjustments made throughout the development of Conduit's knowledge base and retrieval system. It serves as a historical record of our journey from initial implementation to production-ready retrieval quality.

---

## Document Purpose

- **Audience**: Future maintainers, contributors, and anyone wanting to understand the evolution of Conduit's design decisions
- **Scope**: Knowledge base, search, and retrieval systems
- **Format**: Chronological entries with context, decision, and outcome

---

## Timeline of Key Events

### Phase 1: Initial FTS5 Implementation
**Date**: December 2025

#### Context
Conduit needed a way to index and search documents in the knowledge base. The initial requirement was to support text search across PDFs, markdown, and plain text files.

#### Decision
Implemented SQLite FTS5 (Full-Text Search 5) as the primary search mechanism.

**Rationale**:
- Zero external dependencies (SQLite is embedded)
- Fast text search with ranking
- Supports phrase queries and boolean operators
- Works offline, no cloud services needed

#### Implementation
- Created `kb_fts` virtual table with FTS5
- Indexed document chunks with metadata
- Implemented basic search with BM25 ranking

#### Outcome
- ✅ Fast keyword search working
- ✅ Phrase matching ("Oak Ridge") functional
- ❌ No semantic understanding - "car" wouldn't find "automobile"
- ❌ Exact keyword matches required

#### Lesson Learned
> **FTS5 is excellent for exact matching but lacks semantic understanding. Users expect search to understand meaning, not just keywords.**

---

### Phase 2: Adding Semantic Search with Vector Database
**Date**: December 2025

#### Context
User feedback indicated that keyword search was too literal. Queries like "how to authenticate users" wouldn't find documents about "login implementation" or "user credentials."

#### Decision
Added Qdrant vector database alongside FTS5 for semantic search.

**Rationale**:
- Vector embeddings capture semantic meaning
- Similar concepts cluster together in vector space
- Industry standard for RAG (Retrieval-Augmented Generation) applications
- Qdrant runs locally in a container

#### Implementation
- Integrated Qdrant vector store
- Used Ollama with `nomic-embed-text` model for embeddings
- Created embedding pipeline: chunk → embed → store
- Implemented semantic search endpoint

#### Key Technical Decisions

| Decision | Alternative Considered | Why This Choice |
|----------|----------------------|-----------------|
| Qdrant | Chroma, Milvus, Pinecone | Local-first, good Go SDK, production-ready |
| nomic-embed-text | all-MiniLM, BGE | Good quality, runs on Ollama, 768 dimensions |
| 1000 char chunks | 512, 2000 | Balance between context and embedding quality |
| 100 char overlap | 50, 200 | Preserve sentence context at boundaries |

#### Outcome
- ✅ Semantic search working
- ✅ "authentication" finds "login" documents
- ✅ Natural language queries supported
- ❌ Exact phrase matching degraded (vector similarity ≠ exact match)
- ❌ Proper nouns (like "Oak Ridge") not handled well

#### Lesson Learned
> **Vector search excels at semantic similarity but struggles with exact matches and proper nouns. Pure vector search trades precision for recall.**

---

### Phase 3: Smart Chunking Implementation
**Date**: January 2026

#### Context
Initial chunking used fixed character counts, which often split sentences mid-thought. This produced chunks with incomplete context, hurting both embedding quality and readability.

#### Decision
Implemented content-aware "smart chunking" based on file type.

**Approaches by Content Type**:
- **Code files**: Split on function/class boundaries
- **Markdown**: Split on headers, preserve structure
- **PDF**: Handle page breaks, hyphenation, columns
- **Plain text**: Sentence-aware boundaries

#### Implementation Challenges

**Challenge 1: Go Regex Limitations**

The initial sentence-splitting regex used Perl-style lookahead:
```regex
([.!?]+)\s+(?=[A-Z"'\(\[])
```

This caused a runtime panic because Go's `regexp` package doesn't support lookahead assertions.

**Solution**: Replaced with manual rune-by-rune parsing:
```go
// Check for sentence ending: punctuation followed by space and uppercase
if (runes[i] == '.' || runes[i] == '!' || runes[i] == '?') && i+2 < len(runes) {
    if unicode.IsSpace(runes[i+1]) {
        // Look for uppercase letter starting next sentence
        ...
    }
}
```

**Lesson Learned**:
> **Go's regexp package is RE2-based, not PCRE. Features like lookahead (?=), lookbehind (?<=), and backreferences are not supported. Always test regex patterns in Go specifically.**

#### Outcome
- ✅ Better chunk boundaries
- ✅ Code functions kept together
- ✅ Markdown sections preserved
- ✅ No more mid-sentence splits

---

### Phase 4: Result Processing for RAG
**Date**: January 2026

#### Context
Raw search results returned individual chunks, but AI clients needed cleaner, more contextual results. Multiple chunks from the same document should be merged, and boilerplate should be filtered.

#### Decision
Added result processing layer between search and output.

**Features Implemented**:
- Chunk merging: Combine adjacent chunks from same document
- Boilerplate filtering: Remove common noise patterns
- Source citation: Clean attribution for AI context
- Processing modes: "raw" vs "processed" output

#### Boilerplate Patterns Identified
```
- Page numbers: "Page 1 of 10"
- Academic: "All rights reserved", "Copyright © 2024"
- PDF artifacts: Download timestamps, IP addresses
- JSTOR: "This content downloaded from..."
```

#### Outcome
- ✅ Cleaner results for AI consumption
- ✅ Better context with merged chunks
- ⚠️ Boilerplate filtering applied post-retrieval (not ideal)

---

### Phase 5: Retrieval Quality Analysis
**Date**: January 2026

#### Context
Testing revealed significant retrieval quality issues. A query for "huge laboratories like Oak Ridge" (an exact phrase from an indexed document) returned:
1. Alphabet_10K-Report.pdf ranked first (irrelevant)
2. The exact passage not in top results
3. JSTOR metadata appearing in results

#### Root Cause Analysis

| Symptom | Root Cause | Stage |
|---------|-----------|-------|
| Wrong document ranked first | Pure vector search, no keyword component | Retrieval |
| Exact phrase not found | Vector similarity ≠ exact match | Retrieval |
| JSTOR boilerplate in results | Cleaning applied after embedding | Extraction |
| OCR errors (staSs, Sollrish) | PDF extraction quality | Extraction |

#### Key Insight: The Retrieval Pipeline

```
Document → Extraction → Cleaning → Chunking → Embedding → Indexing
                                                              ↓
User ← Result Processing ← Re-ranking ← Retrieval ← Query Processing ← Query
```

**Critical realization**: Quality degrades at EVERY stage. Fixing only retrieval doesn't help if the indexed content is garbage.

#### Lesson Learned
> **"Garbage in, garbage out" applies multiplicatively across pipeline stages. A 90% quality extraction × 90% chunking × 90% embedding × 90% retrieval = 65% end-to-end quality. Fix the foundations first.**

---

### Phase 6: Pre-Chunking Content Cleaning (Implemented)
**Date**: January 2026

#### Context
Analysis revealed that boilerplate (JSTOR metadata, copyright notices, page numbers) was being indexed and polluting the vector space. The principle "garbage in, garbage out" applies multiplicatively across pipeline stages.

#### Decision
Move content cleaning to BEFORE chunking and embedding.

**Implementation**:
- Created `ContentCleaner` module in `internal/kb/content_cleaner.go`
- Boilerplate removal patterns: JSTOR, copyright, page numbers, IP addresses
- OCR error fixes: Common ligature misrecognitions (fi, fl, ff → S)
- PDF artifact handling: Hyphenation, page breaks, repeated headers
- Whitespace normalization

#### Key Files
- `internal/kb/content_cleaner.go` - New content cleaning module
- `internal/kb/source.go` - Integrated cleaning before hashing/chunking

#### Lesson Learned
> **Clean BEFORE embedding, not after retrieval. Once garbage is embedded, it's too late to fix. The hash is now computed on cleaned content, so boilerplate changes don't trigger re-indexing.**

---

### Phase 7: Hybrid Search with RRF (Implemented)
**Date**: January 2026

#### Context
Pure vector search returned Alphabet 10K report for "Oak Ridge laboratories" query. The semantic similarity was matching "laboratories" to general organizational terms while missing the exact phrase.

#### Decision
Implement true hybrid search using Reciprocal Rank Fusion (RRF).

**Architecture**:
```
Query
  ├── Query Analysis (detect proper nouns, quotes)
  ├── FTS5 Search → Ranked List A (exact matches)
  ├── Vector Search → Ranked List B (semantic matches)
  └── RRF Fusion → Combined Ranked List
       └── Proper Noun Boost → Final Results
```

**Implementation**:
- Created `HybridSearcher` in `internal/kb/hybrid_search.go`
- Parallel FTS5 + vector search execution
- RRF fusion with k=60 constant
- Query analysis for proper noun detection
- Automatic mode selection (auto, fusion, semantic, lexical)
- Proper noun boosting for exact matches

**RRF Formula**:
```go
RRF_score(doc) = Σ (weight_i / (k + rank_i(doc)))
```

#### Key Features
1. **Query Analysis**: Detects quoted phrases and proper nouns
2. **Automatic Mode Selection**: Chooses best strategy based on query
3. **Proper Noun Boosting**: 50% boost for results containing exact proper noun matches
4. **Configurable Weights**: Default 50/50 semantic/lexical balance

#### Outcome
- Hybrid RRF should correctly rank "Weinberg_Big_Science.pdf" for "Oak Ridge" queries
- Exact phrase matching restored while keeping semantic understanding
- CLI updated to show hybrid mode: `[hybrid RRF]`

---

### Phase 8: Query Understanding (Implemented)
**Date**: January 2026

#### Context
Users needed better control over search behavior without manually specifying modes.

#### Decision
Implement automatic query analysis with intelligent mode selection.

**Detection Patterns**:
- Quoted phrases (`"exact phrase"`) → prioritize lexical matching
- Proper nouns (multi-word capitalized sequences like "Oak Ridge") → boost exact matches
- Natural language → use hybrid fusion

#### Implementation
Query analysis is integrated into `HybridSearcher.analyzeQuery()` and returned in API responses for transparency.

---

### Phase 9: Retrieval Test Suite (Implemented)
**Date**: January 2026

#### Context
Need systematic way to validate retrieval quality before/after changes.

#### Decision
Create a structured test suite with expected outcomes.

**Implementation**:
- Created `internal/kb/retrieval_test_suite.go`
- Test cases cover: exact phrases, semantic queries, boilerplate filtering, hybrid effectiveness
- Includes `EvaluateTestCase()` function for automated validation

**Sample Test Cases**:
| Test | Query | Expected | Validates |
|------|-------|----------|-----------|
| exact_phrase_oak_ridge | "huge laboratories like Oak Ridge" | Weinberg rank 1-3 | Exact phrase matching |
| semantic_revenue | "revenue growth" | Alphabet 10K | Semantic understanding |
| no_boilerplate_jstor | "scientific research" | No JSTOR metadata | Boilerplate filtering |

---

### Phase 10: RAG-Playground Analysis and Missing Features
**Date**: January 2026

#### Context
After implementing hybrid RRF search, testing showed improvement (Weinberg document ranked #1 for "Oak Ridge" query) but quality still not production-ready. JSTOR boilerplate still appeared in results, OCR errors visible, and repeated content in merged chunks.

Rather than continuing incremental experiments, we analyzed an existing high-quality RAG implementation: the RAG-Playground project.

#### Key Insight: RAG-Playground's Approach

**Surprising finding**: RAG-Playground's text extraction is SIMPLER than Conduit's:
- No OCR correction
- Only 3 basic post-processing patterns
- Quality comes from **retrieval-time filtering**, not extraction-time cleaning

#### RAG-Playground Configuration (from config.py)
```python
DENSE_K: int = 40
LEXICAL_K: int = 40
FUSION_RRF_K: int = 60
USE_MMR: bool = True
MMR_LAMBDA: float = 0.7      # 70% relevance, 30% diversity
SIMILARITY_FLOOR: float = 0.18  # Reject results below this threshold
RERANK_TOP_N: int = 30       # Rerank top 30 candidates
RERANK_KEEP: int = 8         # Keep top 8 after reranking
```

#### Feature Comparison

| Feature | RAG-Playground | Conduit (Before) | Impact |
|---------|---------------|------------------|--------|
| Dense + Lexical + RRF | ✅ | ✅ | Baseline hybrid |
| MMR diversity penalty | ✅ (λ=0.7) | ❌ | Reduces repeated content |
| Similarity floor | ✅ (0.18) | ❌ | Rejects low-quality noise |
| Reranking | ✅ (CrossEncoder) | ❌ | Final quality filter |

#### Three Missing Features Identified

1. **MMR (Maximal Marginal Relevance)**: Diversity penalty to avoid returning near-duplicate chunks
   - Formula: `MMR = argmax[λ * sim(d, q) - (1-λ) * max(sim(d, d'))]`
   - λ=0.7 means 70% relevance, 30% diversity

2. **Similarity Floor**: Confidence threshold to reject low-quality results
   - Results with score < 0.18 are discarded
   - Prevents garbage from appearing in results

3. **Reranking**: Full attention scoring of query-document pairs
   - Uses CrossEncoder or LLM to re-score top candidates
   - Expensive but high quality

#### Lesson Learned
> **Focus quality efforts at retrieval-time, not extraction-time.** Perfect extraction is impossible (OCR errors, PDF quirks). But retrieval-time filtering (MMR, floor, reranking) can compensate for noisy data. Cast a wide net, then filter aggressively.

---

### Phase 11: Architecture Trade-off Analysis - Graph RAG vs. Retrieval Enhancement
**Date**: January 2026

#### Context
Before implementing the missing features, we conducted a thorough trade-off analysis. Key consideration: Conduit serves as an MCP connector feeding AI tools, with transparency for human users.

#### Use Case Analysis

| Aspect | Conduit | RAG-Playground |
|--------|---------|----------------|
| **Primary Consumer** | AI tools (Claude, GPT) | Human users |
| **Output Format** | Raw chunks with metadata | Synthesized answers |
| **Reasoning Location** | AI client does reasoning | Graph RAG does reasoning |
| **Quality Bar** | Relevant chunks, minimal noise | Polished, human-readable |

#### Three Options Considered

**Option A: Three Features Only (MMR, Floor, Reranking)**
- Fast, algorithmic, no LLM calls
- Good for AI consumption
- Missing entity awareness

**Option B: Full Graph RAG**
- Multi-hop reasoning, query decomposition, LLM summarization
- High latency (+500-2000ms for LLM calls)
- Overkill: AI client will re-synthesize anyway

**Option C: Three Features + Lightweight Entity Enhancement (CHOSEN)**
- MMR diversity (λ=0.7)
- Similarity floor (0.18 threshold)
- Reranking (embedding-based)
- Entity extraction at index time, boosting at search time
- No LLM in hot path

#### Decision Rationale

1. **Conduit feeds AI tools** → LLM summarization is redundant (client will re-synthesize)
2. **Transparency requires "not gibberish"** → similarity floor + MMR achieve this
3. **Proper noun handling matters** → entity boosting helps without full graph
4. **Latency matters for MCP** → no LLM calls in retrieval path
5. **Graph RAG's multi-hop reasoning** → handled by AI client anyway

#### MCP Integration Consideration

Conduit will be exposed as an MCP server for AI tools:
```
AI Tool ──▶ MCP Server ──▶ Conduit Daemon ──▶ Results
```

This reinforces Option C:
- Low latency (AI is waiting on tool call)
- Concise responses (context window budget)
- Structured data (MCP tool response format)

#### Implementation Plan

1. **MMR Diversity**: Penalize results similar to already-selected ones
2. **Similarity Floor**: Reject results below confidence threshold
3. **Reranking**: Re-score top candidates using embeddings
4. **Entity Extraction**: Extract proper nouns during indexing
5. **Entity Boosting**: Boost chunks containing query entities

#### Lesson Learned
> **Match architecture to consumer.** When the consumer is an AI that will do its own reasoning, focus on precision and noise reduction rather than synthesis. LLM-in-the-loop retrieval is valuable for human-facing answers but redundant for AI-to-AI pipelines.

---

### Phase 12: Query-Adaptive Confidence Model
**Date**: January 2026

#### Context
Testing revealed that a hard similarity floor (0.01) was filtering out valid exact-match results. The query "Google is not a conventional company" returned zero results despite the text being correctly indexed. Root cause: RRF scores for single-strategy matches (~0.008) fell below the floor.

More importantly, the initial design assumed lexical match = high confidence, but for AI consumption, **semantic relevance often matters more than lexical precision**.

#### Key Insight: Fixed Confidence Rankings Are Wrong

| Query Type | What User Wants | Best Strategy |
|------------|-----------------|---------------|
| Exact quote: `"Google is not conventional"` | Literal text | Exact match |
| Conceptual: `how does authentication work` | Relevant info | **Semantic** |
| Entity: `Oak Ridge laboratories` | Entity info | Hybrid |

For conceptual queries (predominant in AI tool usage), semantic search should rank HIGHER because it captures intent, not just words.

#### Revised Architecture: Parallel Search + Agreement-Based Confidence

**Instead of cascade (try one, fall back), run strategies in parallel:**

```
Query → ┬─▶ Exact Match
        ├─▶ Semantic Search
        ├─▶ Hybrid RRF
        └─▶ Relaxed Match
                │
                ▼
        Agreement Analysis
        ──────────────────
        doc_A: Found by 3/4 → VERY HIGH
        doc_B: Found by 2/4 → HIGH
        doc_C: Semantic only → MEDIUM-HIGH (conceptual query)
        doc_D: Lexical only  → MEDIUM (words match, meaning may not)
```

#### Confidence Scoring Model

| Scenario | Confidence | Rationale |
|----------|------------|-----------|
| Multiple strategies agree | VERY HIGH | Cross-validation |
| Semantic + entity boost | HIGH | Meaning + specificity |
| Semantic only (conceptual) | HIGH | Captures intent |
| Hybrid RRF | MEDIUM-HIGH | Balanced |
| Lexical only | MEDIUM | Words match, meaning may not |
| Relaxed/Partial only | LOW | Weak signal |

#### Performance Considerations for MCP Integration

**Latency Budget Analysis:**

| Component | Current | Target | Notes |
|-----------|---------|--------|-------|
| Query parsing | <1ms | <1ms | Fast |
| FTS5 search | ~5-20ms | ~10ms | SQLite, local |
| Semantic search | ~50-200ms | ~100ms | Qdrant + Ollama embedding |
| RRF fusion | ~1-5ms | <5ms | In-memory |
| MMR + Reranking | ~10-50ms | <30ms | Text similarity |
| **Total** | ~70-280ms | **<150ms** | Target for MCP |

**AI Model Tool Call Timeouts (researched):**

| Model | Default Timeout | Max Timeout | Notes |
|-------|-----------------|-------------|-------|
| Claude (Anthropic) | 60s | 600s | Tool use has generous timeout |
| GPT-4/5 (OpenAI) | 60s | 300s | Function calls |
| Gemini (Google) | 30s | 120s | More aggressive |
| Local models | Varies | - | Depends on implementation |

**MCP Server Recommendations:**

| Setting | Recommended Value | Rationale |
|---------|-------------------|-----------|
| Search timeout | 5s | Fail fast, let client retry |
| Embedding timeout | 10s | Ollama can be slow on first call |
| Total request timeout | 15s | Well under AI model limits |
| Connection pool | Keep-alive | Reduce connection overhead |

#### Design Principle: Never Zero Results

A knowledge base search should NEVER return zero results for reasonable queries. Instead:
1. Always return something (even low confidence)
2. Include confidence metadata in response
3. Let AI client decide whether to use results

```json
{
  "results": [...],
  "confidence": "high",
  "strategies_matched": ["semantic", "hybrid"],
  "search_time_ms": 145
}
```

#### Query Adaptive Design

┌─────────────────────────────────────────────────────────────────────────┐
  │                         QUERY-ADAPTIVE SEARCH FLOW                      │
  └─────────────────────────────────────────────────────────────────────────┘

  1. QUERY INTENT (~5ms)
     ─────────────────────
     User Query: "how does authentication work in Oak Ridge systems"
                                │
                                ▼
     ┌──────────────────────────────────────────────┐
     │ Classify Query Type                          │
     │ ├─ Has quotes? → EXACT_QUOTE                 │
     │ ├─ Has proper nouns? → ENTITY ✓ (Oak Ridge)  │
     │ ├─ Question words? → CONCEPTUAL ✓ (how does) │
     │ └─ Default → EXPLORATORY                     │
     │                                              │
     │ Result: ENTITY + CONCEPTUAL signals          │
     └──────────────────────────────────────────────┘

  2. STRATEGY SELECTION (~1ms)
     ──────────────────────────
                                │
                                ▼
     ┌──────────────────────────────────────────────┐
     │ Load Weights for Query Type                  │
     │                                              │
     │ CONCEPTUAL + ENTITY:                         │
     │   Semantic Weight: 0.6                       │
     │   Lexical Weight:  0.4                       │
     │   Entity Boost:    +50%                      │
     └──────────────────────────────────────────────┘

  3. PARALLEL FETCH (~150ms) ◀── BOTTLENECK
     ────────────────────────
                                │
              ┌─────────────────┼─────────────────┐
              ▼                 ▼                 ▼
     ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
     │ FTS5 Exact   │  │ FTS5 Relaxed │  │  Semantic    │
     │   ~15ms      │  │   ~15ms      │  │   ~150ms     │
     │              │  │              │  │              │
     │ "Oak Ridge"  │  │ oak* ridge*  │  │ Query embed  │
     │ exact match  │  │ wildcards    │  │ → Qdrant     │
     │              │  │              │  │ similarity   │
     │ Results: 3   │  │ Results: 8   │  │ Results: 12  │
     └──────────────┘  └──────────────┘  └──────────────┘
              │                 │                 │
              └─────────────────┴─────────────────┘
                                │
                                ▼

  4. DETERMINE OUTPUT (~70ms)
     ─────────────────────────

     4a. Agreement Analysis (~10ms)
     ┌──────────────────────────────────────────────┐
     │ Chunk "weinberg_p42":                        │
     │   Found by: FTS Exact ✓, FTS Relaxed ✓,      │
     │             Semantic ✓                       │
     │   Agreement: 3/3 = 100% → VERY HIGH          │
     │                                              │
     │ Chunk "alphabet_10k_p12":                    │
     │   Found by: Semantic ✓ only                  │
     │   Agreement: 1/3 = 33% → MEDIUM              │
     └──────────────────────────────────────────────┘
                                │
                                ▼
     4b. Query-Adaptive Weighting (~5ms)
     ┌──────────────────────────────────────────────┐
     │ Apply weights based on query type:           │
     │                                              │
     │ weinberg_p42:                                │
     │   Semantic: 0.6 × (1/rank) = 0.6 × 0.2 = 0.12│
     │   Lexical:  0.4 × (1/rank) = 0.4 × 1.0 = 0.40│
     │   Entity boost: "Oak Ridge" found → +50%    │
     │   Agreement bonus: 100% → +20%              │
     │   Final: 0.52 × 1.5 × 1.2 = 0.94 ✓ TOP      │
     └──────────────────────────────────────────────┘
                                │
                                ▼
     4c. Post-Processing (~50ms)
     ┌──────────────────────────────────────────────┐
     │ ├─ Similarity Floor: Remove score < 0.001   │
     │ ├─ Reranking: Re-score top 30 semantically  │
     │ └─ MMR Diversity: Reduce near-duplicates    │
     └──────────────────────────────────────────────┘

  5. SHARE OUTPUT (~5ms)
     ─────────────────────
                                │
                                ▼
     ┌──────────────────────────────────────────────┐
     │ {                                            │
     │   "results": [                               │
     │     {                                        │
     │       "path": "Weinberg_Big_Science.pdf",    │
     │       "snippet": "...Oak Ridge...",          │
     │       "score": 0.94,                         │
     │       "strategies": ["fts_exact",            │
     │                      "fts_relaxed",          │
     │                      "semantic"],            │
     │       "agreement": "very_high"               │
     │     },                                       │
     │     ...                                      │
     │   ],                                         │
     │   "confidence": "high",                      │
     │   "query_type": "entity_conceptual",         │
     │   "search_time_ms": 226,                     │
     │   "strategies_used": 3,                      │
     │   "note": null                               │
     │ }                                            │
     └──────────────────────────────────────────────┘

  Summary Table

  | Phase        | What Happens                                   | Time   |
  |--------------|------------------------------------------------|--------|
  | 1. Intent    | Classify query (quotes? entities? conceptual?) | ~5ms   |
  | 2. Lookup    | Load strategy weights for query type           | ~1ms   |
  | 3. Fetch     | Run FTS5 + Semantic in parallel                | ~150ms |
  | 4. Determine | Agreement → Weighting → Post-process           | ~70ms  |
  | 5. Share     | Assemble JSON with confidence metadata         | ~5ms   |
  | Total        |                                                | ~230ms |

  The AI client receives not just results, but why those results are confident (agreement across strategies, query-type-appropriate weighting).



#### Lesson Learned
> **Semantic relevance > lexical precision for AI consumers.** When serving AI tools, prioritize meaning over exact words. A semantically relevant result is more valuable than a coincidental word match. Design confidence scoring to reflect this.

---

## Technical Debt and Future Improvements

### Known Issues
1. **PDF extraction quality**: Consider using better PDF libraries or OCR post-processing
2. **Embedding model**: nomic-embed-text is good but not specialized; domain-specific models may help

### Implemented (Previously Listed as Issues)
- ~~No query understanding~~: Implemented in Phase 8
- ~~No relevance feedback~~: Query analysis now provides transparency

### Future Enhancements
1. **Cross-encoder re-ranking**: Score query-document pairs with full attention (deferred)
2. **Query expansion**: Add synonyms for better recall
3. **Hierarchical indexing**: Document → Section → Paragraph levels
4. **User feedback loop**: Learn from explicit user ratings
4. **No relevance feedback**: Can't learn from user behavior

### Potential Improvements
1. **Cross-encoder re-ranking**: Score query-document pairs with full attention
2. **Query expansion**: Add synonyms for better recall
3. **Hierarchical indexing**: Document → Section → Paragraph levels
4. **Caching**: Frequently accessed embeddings and results

---

## Metrics and Benchmarks

### Retrieval Quality Metrics (To Be Implemented)
- **Precision@k**: Fraction of top-k results that are relevant
- **Recall@k**: Fraction of relevant documents in top-k
- **MRR (Mean Reciprocal Rank)**: Average of 1/rank of first relevant result
- **NDCG**: Normalized Discounted Cumulative Gain

### Test Queries (To Be Created)
| Query | Expected Top Result | Tests |
|-------|--------------------|----- |
| "huge laboratories like Oak Ridge" | Weinberg_Big_Science.pdf | Exact phrase |
| "revenue growth" | Alphabet_10K-Report.pdf | Semantic match |
| "sustainability initiatives" | GA-Sustainability-Report.pdf | Topic match |

---

## References and Resources

### Papers
- Robertson et al., "The Probabilistic Relevance Framework: BM25 and Beyond"
- Cormack et al., "Reciprocal Rank Fusion outperforms Condorcet and individual Rank Learning Methods"
- Nogueira et al., "Document Ranking with a Pretrained Sequence-to-Sequence Model"

### Tools and Libraries
- [Qdrant](https://qdrant.tech/) - Vector database
- [SQLite FTS5](https://www.sqlite.org/fts5.html) - Full-text search
- [Ollama](https://ollama.ai/) - Local LLM and embedding models
- [nomic-embed-text](https://huggingface.co/nomic-ai/nomic-embed-text-v1) - Embedding model

---

## Changelog

| Date | Change | Author |
|------|--------|--------|
| 2025-12 | Initial FTS5 implementation | - |
| 2025-12 | Added Qdrant semantic search | - |
| 2026-01 | Smart chunking by content type | - |
| 2026-01 | Result processing for RAG | - |
| 2026-01 | Fixed regex lookahead panic | - |
| 2026-01 | Retrieval quality analysis | - |
| 2026-01 | Pre-chunking content cleaning | - |
| 2026-01 | Hybrid search with RRF fusion | - |
| 2026-01 | Query understanding (proper nouns, quotes) | - |
| 2026-01 | Retrieval test suite | - |
| 2026-01 | RAG-Playground analysis | - |
| 2026-01 | Architecture trade-off analysis (Graph RAG vs Option C) | - |
| 2026-01 | MMR diversity, similarity floor, reranking, entity boosting | - |
| 2026-01 | Vector cleanup on KB removal | - |

---

### Phase 13: Vector Cleanup on KB Removal
**Date**: January 2026

#### Context
Testing revealed that when running `conduit kb remove`, the FTS5 data was deleted from SQLite but Qdrant vectors persisted as orphans. This caused stale semantic search results from deleted KB sources.

#### Root Cause Analysis
The `SourceManager.Remove()` method used raw SQL to delete from SQLite tables (`kb_fts`, `kb_chunks`, `kb_documents`, `kb_sources`) but never called vector deletion methods. In contrast, the `Sync()` method correctly used `Indexer.Delete()` for removed files, which properly cleaned up vectors.

```
# Problem Flow
SourceManager.Remove(sourceID)
    → SQL: DELETE FROM kb_fts, kb_chunks, kb_documents, kb_sources
    → ❌ No Qdrant cleanup → Orphaned vectors

# Solution Flow
SourceManager.Remove(sourceID)
    → indexer.DeleteBySource(sourceID)  # Delete vectors FIRST
        → semantic.DeleteBySource(sourceID)
            → vectorStore.DeleteBySource(sourceID) [filter: source_id = X]
    → SQL: DELETE FROM kb_fts, kb_chunks, kb_documents, kb_sources
```

#### Critical Design Decision: Order of Deletion

**Vectors must be deleted BEFORE SQLite records** because:
1. The `source_id` filter in Qdrant requires the relationship to still exist
2. Once SQLite records are deleted, we lose the source-to-document mapping
3. The deletion is atomic from the user's perspective even if partially fails

#### Implementation

**Files Modified**:
| File | Changes |
|------|---------|
| `internal/kb/vectorstore.go` | Added `DeleteBySource(ctx, sourceID)` with count and filter-based deletion |
| `internal/kb/semantic_search.go` | Added `DeleteBySource(ctx, sourceID)` wrapper |
| `internal/kb/indexer.go` | Added `DeleteBySource(ctx, sourceID)` that calls semantic if enabled |
| `internal/kb/source.go` | Added `RemoveResult` struct; updated `Remove()` to delete vectors first |
| `internal/daemon/handlers.go` | Updated handler to return JSON with deletion statistics |
| `cmd/conduit/main.go` | Updated CLI to display vector count in removal confirmation |

**Key Code Pattern** (VectorStore.DeleteBySource):
```go
func (vs *VectorStore) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
    // Count first for reporting
    countResult, _ := vs.client.Count(ctx, &qdrant.CountPoints{
        CollectionName: vs.collectionName,
        Filter: &qdrant.Filter{
            Must: []*qdrant.Condition{
                qdrant.NewMatch("source_id", sourceID),
            },
        },
    })

    // Delete using filter (not point IDs)
    vs.client.Delete(ctx, &qdrant.DeletePoints{
        CollectionName: vs.collectionName,
        Points: &qdrant.PointsSelector{
            PointsSelectorOneOf: &qdrant.PointsSelector_Filter{
                Filter: &qdrant.Filter{
                    Must: []*qdrant.Condition{
                        qdrant.NewMatch("source_id", sourceID),
                    },
                },
            },
        },
    })
    return int(countResult), nil
}
```

#### UX Enhancement

Before:
```
$ conduit kb remove "My KB"
✓ Removed source: My KB (5 documents)
```

After:
```
$ conduit kb remove "My KB"
✓ Removed source: My KB (5 documents, 50 vectors)
```

The CLI now transparently shows that both FTS5 documents and Qdrant vectors were cleaned up.

#### Graceful Degradation

If semantic search is not enabled (Qdrant unavailable), the deletion chain handles this gracefully:
```go
func (idx *Indexer) DeleteBySource(ctx context.Context, sourceID string) (int, error) {
    if idx.semantic == nil {
        return 0, nil  // No vectors to delete
    }
    return idx.semantic.DeleteBySource(ctx, sourceID)
}
```

#### Lesson Learned
> **Abstraction consistency matters.** When bypass patterns exist (raw SQL vs abstracted methods), they create silent bugs. The `Sync()` method correctly used `Indexer.Delete()` which cleaned vectors, but `Remove()` used raw SQL and missed it. Solution: Always use the same abstraction layer for related operations.

---

---

### Phase 14: Knowledge-Augmented Generation (KAG) Implementation
**Date**: January 2026

#### Context
After implementing hybrid RAG search, certain query types still performed poorly:
- **Aggregation queries**: "List all threat models mentioned in the KB"
- **Multi-hop reasoning**: "How does technology X relate to organization Y?"
- **Entity disambiguation**: "Is 'Oak Ridge' the lab or the location?"

These require understanding entities and their relationships, not just text similarity.

#### Root Cause Analysis

| Query Type | RAG Performance | Root Cause |
|------------|-----------------|------------|
| Aggregation | Poor | RAG retrieves chunks, not entities |
| Multi-hop | Poor | No graph structure to traverse |
| Entity queries | Medium | Relies on keyword/semantic match |

**Key Insight**: RAG treats documents as bags of words/vectors. It doesn't understand that "Kubernetes" is a technology that "uses" "Docker" and is "part_of" "Cloud Native".

#### Decision: Parallel KAG Pipeline

Instead of replacing RAG, add a parallel KAG pipeline:

```
┌─────────────────────┐     ┌─────────────────────┐
│   RAG Pipeline      │     │   KAG Pipeline      │
│   (existing)        │     │   (new)             │
│   ├─ FTS5          │     │   ├─ Entity Extractor│
│   ├─ Qdrant        │     │   ├─ SQLite Graph    │
│   └─ Hybrid Search │     │   └─ KAG Search      │
└─────────────────────┘     └─────────────────────┘
         │                           │
         └───────────┬───────────────┘
                     ▼
              AI Client (Claude, GPT)
              chooses best tool for query
```

**Why Parallel**:
1. RAG excels at semantic/keyword queries
2. KAG excels at entity/relation queries
3. AI client can choose appropriate tool
4. No regression in existing functionality

#### Technology Selection

**Graph Database: FalkorDB vs Neo4j vs NetworkX**

| Criteria | FalkorDB | Neo4j | NetworkX |
|----------|----------|-------|----------|
| License | Apache 2.0 | GPL/Commercial | BSD |
| Performance | 500x faster p99 | Baseline | In-memory only |
| Query Language | Cypher | Cypher | Python API |
| Deployment | Docker (Redis) | Docker/Native | Embedded |
| **Decision** | **Chosen** | Too restrictive | Too limited |

**LLM for Extraction: Mistral 7B vs GPT-4 vs Claude**

| Criteria | Mistral 7B | GPT-4 | Claude 3.5 |
|----------|------------|-------|------------|
| License | Apache 2.0 | API only | API only |
| NER F1 | 0.6376 | 0.82+ | 0.80+ |
| Latency | ~2-5s local | ~1s API | ~1s API |
| Cost | Free | $$/query | $$/query |
| Privacy | Local | Cloud | Cloud |
| **Decision** | **Default** | Optional | Optional |

**RAM Budget Analysis (32GB M4)**:
- macOS + Apps: ~8GB
- nomic-embed-text: ~0.3GB
- qwen2.5-coder (chat): ~4.5GB
- mistral:7b (KAG): ~4.1GB
- Qdrant + FalkorDB: ~3GB
- **Total**: ~20GB, **Headroom**: ~12GB

#### Security Considerations

| Risk | Mitigation |
|------|------------|
| Prompt injection in LLM | `sanitizePromptInput()` filters patterns |
| Malicious entity names | Validator rejects suspicious content |
| Low-quality extractions | Confidence threshold (0.6) |
| Network exposure | FalkorDB localhost-only by default |
| Opt-out complexity | KAG disabled by default |

#### Implementation Highlights

**1. Deterministic Entity IDs**
Problem: Same entity extracted from different chunks creates duplicates.
Solution: Generate ID from `sha256(name + type + documentID)`.

```go
func GenerateEntityID(name, entityType, documentID string) string {
    data := fmt.Sprintf("%s:%s:%s",
        strings.ToLower(name), entityType, documentID)
    h := sha256.Sum256([]byte(data))
    return "ent_" + hex.EncodeToString(h[:8])
}
```

**2. Background Extraction Workers**
Problem: Synchronous extraction blocks indexing.
Solution: Queue-based background workers with configurable parallelism.

**3. Multi-Provider Support**
Problem: Users have different preferences (local vs cloud).
Solution: `LLMProvider` interface with Ollama, OpenAI, Anthropic implementations.

#### Testing Strategy

| Level | Tests | Coverage |
|-------|-------|----------|
| Unit | Type normalization, validation, ID generation | Core logic |
| Integration | Extractor + SQLite | Pipeline |
| E2E | CLI commands | User workflows |
| Security | Prompt injection patterns | Attack surface |

#### Outcome

- ✅ `kag_query` MCP tool working
- ✅ Entity/relation extraction with Ollama
- ✅ SQLite-based graph storage
- ✅ Security hardening (prompt sanitization, validation)
- ✅ Graceful degradation when KAG disabled
- ✅ All 10 unit tests passing

#### Lessons Learned

> **1. Parallel > Replace**: When adding new capabilities (KAG), run parallel to existing systems (RAG). Let the AI client choose the best tool for each query type.

> **2. Local-first defaults matter**: Default to Ollama (local) not OpenAI (cloud). Users can opt-in to cloud for better quality, but privacy-first is the safe default.

> **3. Deterministic IDs prevent deduplication headaches**: By generating IDs from content hashes, the same entity from different chunks automatically merges.

> **4. Prompt injection is a real threat**: The `sanitizePromptInput()` function caught several injection patterns in test documents. Always sanitize user-provided content before sending to LLMs.

> **5. Background workers need graceful shutdown**: Without proper `stopCh` handling, workers would hang on daemon shutdown. Always implement clean shutdown patterns.

---

*Last Updated: January 2026*
