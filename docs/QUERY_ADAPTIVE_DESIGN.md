# Query-Adaptive Confidence Model - Technical Design

**Version**: 1.0
**Date**: January 2026
**Status**: Design Phase

---

## 1. The Core Challenge

We need to implement:
1. **Query Intent Understanding** - Classify what the user is looking for
2. **Strategy Selection** - Map intent to retrieval technique weights
3. **Parallel Execution** - Run multiple strategies efficiently
4. **Agreement-Based Confidence** - Score results by cross-validation
5. **Never Zero Results** - Always return something useful

**Constraint**: Complete within 10 seconds (MCP tool call budget)

---

## 2. Feasibility Analysis

### 2.1 Current Architecture Performance

```
┌─────────────────────────────────────────────────────────────┐
│                    Current Hybrid Search                    │
├─────────────────────────────────────────────────────────────┤
│ Component              │ Cold Start │ Warm    │ Parallel?  │
├────────────────────────┼────────────┼─────────┼────────────┤
│ Query Analysis         │ ~5ms       │ ~5ms    │ -          │
│ FTS5 Search            │ ~20ms      │ ~15ms   │ ✓ Yes      │
│ Semantic Search        │ ~10s       │ ~150ms  │ ✓ Yes      │
│   └─ Ollama Embed      │ ~10s       │ ~100ms  │ (nested)   │
│   └─ Qdrant Query      │ ~50ms      │ ~30ms   │ (nested)   │
│ RRF Fusion             │ ~5ms       │ ~5ms    │ -          │
│ Entity Boosting        │ ~5ms       │ ~5ms    │ -          │
│ Similarity Floor       │ ~1ms       │ ~1ms    │ -          │
│ Reranking              │ ~30ms      │ ~30ms   │ -          │
│ MMR Diversity          │ ~20ms      │ ~20ms   │ -          │
├────────────────────────┼────────────┼─────────┼────────────┤
│ TOTAL                  │ ~10.1s     │ ~230ms  │            │
└─────────────────────────────────────────────────────────────┘
```

**Key Insight**: The parallel execution means we pay max(FTS5, Semantic), not sum.

### 2.2 Enhanced Model Budget

```
┌─────────────────────────────────────────────────────────────┐
│                Enhanced Query-Adaptive Model                │
├─────────────────────────────────────────────────────────────┤
│ Component              │ Added Time │ Cumulative │ Notes    │
├────────────────────────┼────────────┼────────────┼──────────┤
│ Query Classification   │ +5ms       │ 10ms       │ Regex    │
│ Strategy Selection     │ +1ms       │ 11ms       │ Lookup   │
│ Parallel Strategies:   │            │            │          │
│   ├─ FTS5 Exact        │ ~15ms      │            │ ┐        │
│   ├─ FTS5 Relaxed      │ ~15ms      │            │ │ Max    │
│   └─ Semantic          │ ~150ms     │            │ ┘ =150ms │
│ (Parallel subtotal)    │            │ 161ms      │          │
│ Agreement Analysis     │ +10ms      │ 171ms      │ Set ops  │
│ Confidence Scoring     │ +5ms       │ 176ms      │ Math     │
│ Dynamic Weighting      │ +5ms       │ 181ms      │ Adjust   │
│ RRF Fusion             │ ~5ms       │ 186ms      │          │
│ Post-processing        │ ~50ms      │ 236ms      │          │
├────────────────────────┼────────────┼────────────┼──────────┤
│ TOTAL (warm)           │            │ ~250ms     │ ✓ Safe   │
│ TOTAL (cold)           │            │ ~10.3s     │ ⚠ Risk  │
└─────────────────────────────────────────────────────────────┘
```

**Verdict**: Warm latency is excellent. Cold start is the risk.

---

## 3. Query Classification Design

### 3.1 Query Types

| Type | Pattern | Example | Primary Strategy |
|------|---------|---------|------------------|
| **EXACT_QUOTE** | `"..."` in query | `"not a conventional company"` | Lexical exact |
| **ENTITY** | Proper nouns | `Oak Ridge laboratories` | Hybrid + boost |
| **CONCEPTUAL** | Question words, abstract | `how does auth work` | Semantic first |
| **FACTUAL** | Specific data request | `revenue 2024` | Hybrid |
| **EXPLORATORY** | Broad topic | `machine learning` | Semantic |

### 3.2 Classification Algorithm

```go
type QueryType string

const (
    QueryTypeExactQuote  QueryType = "exact_quote"
    QueryTypeEntity      QueryType = "entity"
    QueryTypeConceptual  QueryType = "conceptual"
    QueryTypeFactual     QueryType = "factual"
    QueryTypeExploratory QueryType = "exploratory"
)

func classifyQuery(query string) QueryType {
    // Phase 1: Exact indicators (fast, deterministic)
    if hasQuotes(query) {
        return QueryTypeExactQuote
    }

    // Phase 2: Entity detection
    entities := extractEntities(query)
    if len(entities) > 0 {
        return QueryTypeEntity
    }

    // Phase 3: Conceptual indicators
    conceptualPatterns := []string{
        `(?i)^(how|why|what|when|where|who|which)`,  // Question words
        `(?i)(explain|describe|understand|concept)`,  // Understanding
        `(?i)(difference|compare|versus|vs)`,         // Comparison
    }
    for _, pattern := range conceptualPatterns {
        if regexp.MustCompile(pattern).MatchString(query) {
            return QueryTypeConceptual
        }
    }

    // Phase 4: Factual indicators
    factualPatterns := []string{
        `\d{4}`,                    // Year
        `(?i)(price|cost|revenue)`, // Metrics
        `(?i)(version|release)`,    // Specific data
    }
    for _, pattern := range factualPatterns {
        if regexp.MustCompile(pattern).MatchString(query) {
            return QueryTypeFactual
        }
    }

    // Default: exploratory
    return QueryTypeExploratory
}
```

**Cost**: ~5ms (precompiled regexes)

### 3.3 Strategy Weight Matrix

```go
// Weights for RRF fusion based on query type
var strategyWeights = map[QueryType]struct {
    Semantic float64
    Lexical  float64
}{
    QueryTypeExactQuote:  {Semantic: 0.1, Lexical: 0.9},  // Lexical dominant
    QueryTypeEntity:      {Semantic: 0.4, Lexical: 0.6},  // Balanced, lexical edge
    QueryTypeConceptual:  {Semantic: 0.8, Lexical: 0.2},  // Semantic dominant
    QueryTypeFactual:     {Semantic: 0.5, Lexical: 0.5},  // Equal
    QueryTypeExploratory: {Semantic: 0.7, Lexical: 0.3},  // Semantic preferred
}
```

---

## 4. Agreement-Based Confidence Design

### 4.1 Core Concept

A result found by multiple strategies is more trustworthy than one found by a single strategy.

```
Document X:
  ├─ Found by FTS5 Exact: rank 1
  ├─ Found by FTS5 Relaxed: rank 3
  └─ Found by Semantic: rank 5

Agreement Score = 3/3 strategies = VERY HIGH confidence

Document Y:
  └─ Found by Semantic only: rank 2

Agreement Score = 1/3 strategies = MEDIUM confidence
```

### 4.2 Implementation

```go
type SearchStrategy string

const (
    StrategyFTSExact    SearchStrategy = "fts_exact"
    StrategyFTSRelaxed  SearchStrategy = "fts_relaxed"
    StrategySemantic    SearchStrategy = "semantic"
)

type AgreementResult struct {
    ChunkID      string
    Hit          SearchHit
    FoundBy      []SearchStrategy  // Which strategies found this
    BestRank     int               // Best rank across strategies
    Agreement    float64           // 0-1 agreement score
    Confidence   string            // "very_high", "high", "medium", "low"
}

func analyzeAgreement(
    ftsExact []SearchHit,
    ftsRelaxed []SearchHit,
    semantic []SearchHit,
) []AgreementResult {
    // Build lookup maps: chunkID -> (strategy, rank)
    found := make(map[string][]struct{
        Strategy SearchStrategy
        Rank     int
    })

    for i, hit := range ftsExact {
        found[hit.ChunkID] = append(found[hit.ChunkID],
            struct{Strategy SearchStrategy; Rank int}{StrategyFTSExact, i+1})
    }
    for i, hit := range ftsRelaxed {
        found[hit.ChunkID] = append(found[hit.ChunkID],
            struct{Strategy SearchStrategy; Rank int}{StrategyFTSRelaxed, i+1})
    }
    for i, hit := range semantic {
        found[hit.ChunkID] = append(found[hit.ChunkID],
            struct{Strategy SearchStrategy; Rank int}{StrategySemantic, i+1})
    }

    // Calculate agreement and confidence
    results := make([]AgreementResult, 0)
    for chunkID, sources := range found {
        agreement := float64(len(sources)) / 3.0  // 3 strategies

        bestRank := math.MaxInt
        strategies := make([]SearchStrategy, 0)
        for _, s := range sources {
            strategies = append(strategies, s.Strategy)
            if s.Rank < bestRank {
                bestRank = s.Rank
            }
        }

        confidence := scoreToConfidence(agreement, len(sources))

        results = append(results, AgreementResult{
            ChunkID:    chunkID,
            Hit:        getHit(chunkID, ftsExact, ftsRelaxed, semantic),
            FoundBy:    strategies,
            BestRank:   bestRank,
            Agreement:  agreement,
            Confidence: confidence,
        })
    }

    return results
}

func scoreToConfidence(agreement float64, strategyCount int) string {
    switch {
    case strategyCount >= 3:
        return "very_high"
    case strategyCount == 2:
        return "high"
    case strategyCount == 1 && agreement >= 0.5:
        return "medium"
    default:
        return "low"
    }
}
```

**Cost**: ~10ms (map operations on typical result sets)

---

## 5. Query-Adaptive Weighting

### 5.1 Dynamic Weight Adjustment

The key insight: **Semantic relevance matters more for conceptual queries, lexical precision matters more for exact lookups.**

```go
func adjustWeightsForQueryType(
    results []AgreementResult,
    queryType QueryType,
) []AgreementResult {
    weights := strategyWeights[queryType]

    for i := range results {
        result := &results[i]

        // Calculate weighted score based on which strategies found it
        var score float64
        for _, strategy := range result.FoundBy {
            switch strategy {
            case StrategySemantic:
                score += weights.Semantic * (1.0 / float64(result.BestRank))
            case StrategyFTSExact, StrategyFTSRelaxed:
                score += weights.Lexical * (1.0 / float64(result.BestRank))
            }
        }

        // Bonus for cross-strategy agreement
        agreementBonus := result.Agreement * 0.2  // Up to 20% boost
        result.Hit.Score = score * (1 + agreementBonus)
    }

    // Sort by adjusted score
    sort.Slice(results, func(i, j int) bool {
        return results[i].Hit.Score > results[j].Hit.Score
    })

    return results
}
```

### 5.2 Example: Conceptual Query

```
Query: "how does authentication work"
Query Type: CONCEPTUAL
Weights: Semantic=0.8, Lexical=0.2

Document A (found by: Semantic rank 1, FTS rank 8):
  - Semantic contribution: 0.8 * (1/1) = 0.8
  - Lexical contribution:  0.2 * (1/8) = 0.025
  - Agreement bonus: (2/3) * 0.2 = 0.133
  - Final score: 0.825 * 1.133 = 0.935 ✓ HIGH

Document B (found by: FTS rank 1 only):
  - Semantic contribution: 0
  - Lexical contribution:  0.2 * (1/1) = 0.2
  - Agreement bonus: (1/3) * 0.2 = 0.067
  - Final score: 0.2 * 1.067 = 0.213

Result: Semantic-found document A ranks higher for conceptual query
```

---

## 6. Never Zero Results Design

### 6.1 Fallback Cascade

```
Primary Search (parallel, 150ms budget)
├── FTS5 Exact + Relaxed
├── Semantic
└── Agreement Analysis
        │
        ▼
    Results?
    ├── YES → Return with confidence
    └── NO  → Fallback Phase 1 (100ms budget)
                │
                ├── Relaxed FTS5 (wildcards, stemming)
                ├── Lower semantic threshold
                │
                ▼
            Results?
            ├── YES → Return with "low" confidence
            └── NO  → Fallback Phase 2 (50ms budget)
                        │
                        ├── Partial word matching
                        ├── Related document suggestions
                        │
                        ▼
                    Results?
                    ├── YES → Return with "speculative" confidence
                    └── NO  → Return "no_results" with suggestions
```

### 6.2 Implementation

```go
func (hs *HybridSearcher) SearchWithFallback(
    ctx context.Context,
    query string,
    opts HybridSearchOptions,
) (*HybridSearchResult, error) {

    // Phase 1: Primary search (most time budget)
    primaryCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
    defer cancel()

    result := hs.searchParallel(primaryCtx, query, opts)
    if len(result.Results) > 0 {
        return result, nil
    }

    // Phase 2: Relaxed search
    relaxedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
    defer cancel()

    relaxedOpts := opts
    relaxedOpts.SimilarityFloor = 0.0001  // Lower threshold
    result = hs.searchRelaxed(relaxedCtx, query, relaxedOpts)
    if len(result.Results) > 0 {
        result.Confidence = "low"
        result.Note = "Using relaxed matching"
        return result, nil
    }

    // Phase 3: Partial matching
    partialCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
    defer cancel()

    result = hs.searchPartial(partialCtx, query)
    if len(result.Results) > 0 {
        result.Confidence = "speculative"
        result.Note = "Partial word matching - verify relevance"
        return result, nil
    }

    // Phase 4: No results - return suggestions
    return &HybridSearchResult{
        Results:    []SearchHit{},
        TotalHits:  0,
        Query:      query,
        Confidence: "no_results",
        Note:       "No matches found. Try broader terms or check source documents.",
        Suggestions: hs.generateSuggestions(query),
    }, nil
}
```

---

## 7. Cold Start Mitigation

### 7.1 The Problem

Cold start (Ollama model loading) can take 10+ seconds, exceeding our safe window.

### 7.2 Solutions

| Strategy | Implementation | Latency Impact |
|----------|----------------|----------------|
| **Warm-up on startup** | Pre-embed on daemon start | +2-5s startup |
| **FTS5-first fast path** | Return FTS5 while semantic loads | 15ms first result |
| **Graceful degradation** | Return FTS5-only with flag | 15ms, lower quality |
| **Background indexing** | Keep model warm via periodic embeds | Continuous |

### 7.3 Recommended: Progressive Results

```go
func (hs *HybridSearcher) SearchProgressive(
    ctx context.Context,
    query string,
    opts HybridSearchOptions,
) (<-chan ProgressiveResult, error) {

    results := make(chan ProgressiveResult, 3)

    go func() {
        defer close(results)

        // Fast path: FTS5 results first (15ms)
        ftsResult := hs.searchFTSOnly(ctx, query, opts)
        results <- ProgressiveResult{
            Type:       "preliminary",
            Results:    ftsResult.Results,
            Confidence: "fts_only",
            Note:       "Initial results - semantic search pending",
        }

        // Full path: Semantic + fusion
        fullResult := hs.searchFusion(ctx, query, opts)
        results <- ProgressiveResult{
            Type:       "final",
            Results:    fullResult.Results,
            Confidence: fullResult.Confidence,
        }
    }()

    return results, nil
}
```

**For MCP**: Since MCP expects a single response, we use the fallback approach:
- If semantic completes in time → full hybrid results
- If semantic times out → FTS5-only with degraded flag

---

## 8. Complete Search Flow

```
┌────────────────────────────────────────────────────────────────────────┐
│                        Query Adaptive Search                           │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  1. Query Classification (~5ms)                                        │
│     ├─ Detect quotes → EXACT_QUOTE                                     │
│     ├─ Detect entities → ENTITY                                        │
│     ├─ Detect question words → CONCEPTUAL                              │
│     ├─ Detect metrics/dates → FACTUAL                                  │
│     └─ Default → EXPLORATORY                                           │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  2. Load Strategy Weights (~1ms)                                       │
│     QueryType → {SemanticWeight, LexicalWeight}                        │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  3. Parallel Strategy Execution (~150ms) ◀─── BOTTLENECK               │
│     ┌─────────────┬─────────────┬─────────────┐                        │
│     │ FTS5 Exact  │ FTS5 Relaxed│  Semantic   │                        │
│     │   ~15ms     │   ~15ms     │   ~150ms    │                        │
│     └─────────────┴─────────────┴─────────────┘                        │
│                    (all parallel)                                       │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  4. Agreement Analysis (~10ms)                                         │
│     For each chunk:                                                     │
│       - Count strategies that found it                                  │
│       - Record best rank                                                │
│       - Calculate agreement score                                       │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  5. Query-Adaptive Weighting (~5ms)                                    │
│     Apply QueryType-specific weights:                                   │
│       score = Σ(weight[strategy] × 1/rank) × (1 + agreement_bonus)     │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  6. Post-Processing (~50ms)                                            │
│     ├─ Similarity Floor (reject low scores)                            │
│     ├─ Reranking (semantic re-scoring)                                 │
│     └─ MMR Diversity (reduce duplicates)                               │
└────────────────────────────────────────────────────────────────────────┘
                                  │
                                  ▼
┌────────────────────────────────────────────────────────────────────────┐
│  7. Result Assembly (~5ms)                                             │
│     {                                                                   │
│       results: [...],                                                   │
│       confidence: "high",                                               │
│       query_type: "conceptual",                                         │
│       strategies_matched: ["semantic", "fts_relaxed"],                  │
│       agreement_scores: {...}                                           │
│     }                                                                   │
└────────────────────────────────────────────────────────────────────────┘

TOTAL (warm): ~230-280ms ✓
TOTAL (cold): ~10-12s ⚠ (need FTS5 fallback)
```

---

## 9. Implementation Phases

### Phase 1: Query Classification (Low Risk)
- Add `classifyQuery()` function
- Add `QueryType` enum
- Unit tests for classification patterns
- **Effort**: 1-2 hours
- **Risk**: Low (additive, no breaking changes)

### Phase 2: Strategy Weight Matrix (Low Risk)
- Add weight configuration
- Integrate into RRF fusion
- **Effort**: 1-2 hours
- **Risk**: Low (configuration change)

### Phase 3: Agreement Analysis (Medium Risk)
- Add multi-strategy result tracking
- Implement agreement scoring
- Add confidence levels
- **Effort**: 3-4 hours
- **Risk**: Medium (changes result structure)

### Phase 4: Query-Adaptive Weighting (Medium Risk)
- Implement dynamic weight adjustment
- Integrate with existing fusion
- **Effort**: 2-3 hours
- **Risk**: Medium (affects ranking)

### Phase 5: Never Zero Results (Low Risk)
- Add fallback cascade
- Add suggestions generator
- **Effort**: 2-3 hours
- **Risk**: Low (additive)

### Phase 6: Cold Start Mitigation (Medium Risk)
- Add FTS5-first fast path
- Add graceful degradation
- **Effort**: 2-3 hours
- **Risk**: Medium (async handling)

---

## 10. Conclusion

**Feasibility**: YES, implementable within latency budget

| Concern | Assessment |
|---------|------------|
| Query understanding latency | ~10ms (fast regex/string ops) |
| Multiple strategies latency | ~150ms (parallel execution) |
| Agreement analysis latency | ~10ms (set operations) |
| Total warm latency | ~280ms (well under 10s) |
| Cold start risk | Mitigated by FTS5 fallback |

**Key Enablers**:
1. Parallel execution (already implemented)
2. Fast regex-based classification
3. In-memory agreement analysis
4. FTS5 fallback for cold starts

**Next Steps**:
1. Implement Phase 1-2 (low risk, high value)
2. Test with production queries
3. Measure actual latencies
4. Iterate on Phase 3-6

---

**Document History**:
| Version | Date | Author | Changes |
|---------|------|--------|---------|
| 1.0 | Jan 2026 | Conduit Team | Initial design |
