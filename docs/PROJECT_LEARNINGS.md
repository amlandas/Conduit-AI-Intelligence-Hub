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

---

*Last Updated: January 2026*
