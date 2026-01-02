package kb

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

// Default configuration values based on RAG-Playground analysis
const (
	DefaultMMRLambda       = 0.7   // 70% relevance, 30% diversity
	DefaultSimilarityFloor = 0.001 // Minimum RRF score threshold (lowered to avoid filtering valid results)
	DefaultRerankTopN      = 30    // Rerank top 30 candidates
	DefaultRerankKeep      = 10    // Keep top 10 after reranking
)

// QueryType represents the classified intent of a search query.
type QueryType string

const (
	QueryTypeExactQuote  QueryType = "exact_quote"  // User wants literal text match
	QueryTypeEntity      QueryType = "entity"       // Query focuses on named entities
	QueryTypeConceptual  QueryType = "conceptual"   // User seeking understanding/explanation
	QueryTypeFactual     QueryType = "factual"      // User wants specific data/facts
	QueryTypeExploratory QueryType = "exploratory"  // Broad topic exploration
)

// SearchStrategy identifies which search method found a result.
type SearchStrategy string

const (
	StrategyFTSExact   SearchStrategy = "fts_exact"   // FTS5 exact phrase match
	StrategyFTSRelaxed SearchStrategy = "fts_relaxed" // FTS5 with wildcards/stemming
	StrategySemantic   SearchStrategy = "semantic"    // Vector similarity search
)

// StrategyWeights defines the RRF weights for each query type.
type StrategyWeights struct {
	Semantic float64
	Lexical  float64
}

// strategyWeightMatrix maps query types to optimal strategy weights.
var strategyWeightMatrix = map[QueryType]StrategyWeights{
	QueryTypeExactQuote:  {Semantic: 0.1, Lexical: 0.9},  // Lexical dominant
	QueryTypeEntity:      {Semantic: 0.4, Lexical: 0.6},  // Balanced, lexical edge
	QueryTypeConceptual:  {Semantic: 0.8, Lexical: 0.2},  // Semantic dominant
	QueryTypeFactual:     {Semantic: 0.5, Lexical: 0.5},  // Equal weight
	QueryTypeExploratory: {Semantic: 0.7, Lexical: 0.3},  // Semantic preferred
}

// Precompiled regex patterns for query classification
var (
	conceptualPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^(how|why|what|when|where|who|which)\b`),
		regexp.MustCompile(`(?i)\b(explain|describe|understand|concept|meaning)\b`),
		regexp.MustCompile(`(?i)\b(difference|compare|versus|vs\.?)\b`),
	}
	factualPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b\d{4}\b`),                                    // Year
		regexp.MustCompile(`(?i)\b(price|cost|revenue|amount|number)\b`),   // Metrics
		regexp.MustCompile(`(?i)\b(version|release|date|when was)\b`),      // Specific data
		regexp.MustCompile(`(?i)\b(how much|how many|percentage|ratio)\b`), // Quantities
	}
)

// HybridSearcher combines FTS5 (lexical) and vector (semantic) search using RRF.
type HybridSearcher struct {
	fts      *Searcher
	semantic *SemanticSearcher
	logger   zerolog.Logger
}

// HybridSearchMode determines how searches are combined.
type HybridSearchMode string

const (
	// HybridModeAuto automatically selects the best mode based on query analysis
	HybridModeAuto HybridSearchMode = "auto"
	// HybridModeFusion uses RRF to combine FTS5 and vector results
	HybridModeFusion HybridSearchMode = "fusion"
	// HybridModeSemantic uses only vector search
	HybridModeSemantic HybridSearchMode = "semantic"
	// HybridModeLexical uses only FTS5 search
	HybridModeLexical HybridSearchMode = "lexical"
)

// HybridSearchOptions configures hybrid search behavior.
type HybridSearchOptions struct {
	Limit           int              // Max results (default 10)
	Mode            HybridSearchMode // Search mode (default auto)
	SemanticWeight  float64          // Weight for semantic results in fusion (0-1, default 0.5)
	RRFConstant     int              // RRF k constant (default 60)
	BoostExactMatch bool             // Boost results with exact query match (default true)
	SourceIDs       []string         // Filter by source IDs
	MimeTypes       []string         // Filter by MIME types

	// Quality enhancement options (Phase 11)
	EnableMMR       bool    // Enable Maximal Marginal Relevance for diversity (default true)
	MMRLambda       float64 // MMR lambda: 0=max diversity, 1=max relevance (default 0.7)
	SimilarityFloor float64 // Minimum score threshold, reject below this (default 0.01)
	EnableRerank    bool    // Enable reranking of top candidates (default true)
	RerankTopN      int     // Number of candidates to consider for reranking (default 30)
}

// HybridSearchResult contains combined search results with metadata.
type HybridSearchResult struct {
	Results        []SearchHit      `json:"results"`
	TotalHits      int              `json:"total_hits"`
	Query          string           `json:"query"`
	SearchTime     float64          `json:"search_time_ms"`
	Mode           HybridSearchMode `json:"mode"`
	FTSHits        int              `json:"fts_hits"`
	SemanticHits   int              `json:"semantic_hits"`
	QueryAnalysis  QueryAnalysis    `json:"query_analysis,omitempty"`

	// Quality enhancement metrics
	RejectedByFloor int  `json:"rejected_by_floor,omitempty"` // Count of results below similarity floor
	MMRApplied      bool `json:"mmr_applied,omitempty"`       // Whether MMR diversity was applied
	Reranked        bool `json:"reranked,omitempty"`          // Whether reranking was applied

	// Query-adaptive confidence model (Phase 12)
	Confidence       string   `json:"confidence,omitempty"`         // Overall confidence: very_high, high, medium, low
	StrategiesUsed   int      `json:"strategies_used,omitempty"`    // Number of strategies that contributed
	DegradedMode     bool     `json:"degraded_mode,omitempty"`      // True if semantic search timed out/failed
	Note             string   `json:"note,omitempty"`               // Human-readable note about results
	FallbackLevel    int      `json:"fallback_level,omitempty"`     // 0=primary, 1=relaxed, 2=partial
}

// QueryAnalysis provides insight into how the query was interpreted.
type QueryAnalysis struct {
	HasQuotedPhrase bool      `json:"has_quoted_phrase,omitempty"`
	ProperNouns     []string  `json:"proper_nouns,omitempty"`     // Multi-word proper nouns (e.g., "Oak Ridge")
	Entities        []string  `json:"entities,omitempty"`         // All detected entities (single + multi-word)
	SuggestedMode   string    `json:"suggested_mode,omitempty"`
	QueryType       QueryType `json:"query_type,omitempty"`       // Classified query type
	IsConceptual    bool      `json:"is_conceptual,omitempty"`    // True if query seeks understanding
	IsFactual       bool      `json:"is_factual,omitempty"`       // True if query seeks specific data
}

// EnhancedSearchHit extends SearchHit with agreement metadata.
type EnhancedSearchHit struct {
	SearchHit
	FoundBy      []SearchStrategy `json:"found_by,omitempty"`      // Which strategies found this result
	Agreement    float64          `json:"agreement,omitempty"`     // Agreement score (0-1)
	Confidence   string           `json:"confidence,omitempty"`    // Result-level confidence
	BestRank     int              `json:"best_rank,omitempty"`     // Best rank across strategies
}

// NewHybridSearcher creates a new hybrid searcher.
func NewHybridSearcher(fts *Searcher, semantic *SemanticSearcher) *HybridSearcher {
	return &HybridSearcher{
		fts:      fts,
		semantic: semantic,
		logger:   observability.Logger("kb.hybrid"),
	}
}

// Search performs hybrid search using the configured mode.
func (hs *HybridSearcher) Search(ctx context.Context, query string, opts HybridSearchOptions) (*HybridSearchResult, error) {
	start := time.Now()

	// Apply defaults
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.RRFConstant <= 0 {
		opts.RRFConstant = 60 // Standard RRF constant
	}
	if opts.SemanticWeight <= 0 {
		opts.SemanticWeight = 0.5 // Equal weight by default
	}
	opts.BoostExactMatch = true // Always boost exact matches

	// Apply Phase 11 quality enhancement defaults
	// Note: EnableMMR and EnableRerank default to true (zero value is false, so we check explicitly)
	if opts.MMRLambda <= 0 {
		opts.MMRLambda = DefaultMMRLambda
	}
	if opts.SimilarityFloor <= 0 {
		opts.SimilarityFloor = DefaultSimilarityFloor
	}
	if opts.RerankTopN <= 0 {
		opts.RerankTopN = DefaultRerankTopN
	}
	// Enable MMR and Rerank by default (only disable if explicitly set to false via API)
	// Since Go zero value for bool is false, we use a different approach:
	// We always enable these unless the caller explicitly passes options
	opts.EnableMMR = true
	opts.EnableRerank = true

	// Analyze query
	analysis := hs.analyzeQuery(query)

	// Determine mode if auto
	mode := opts.Mode
	if mode == "" || mode == HybridModeAuto {
		mode = hs.selectMode(analysis)
		analysis.SuggestedMode = string(mode)
	}

	hs.logger.Debug().
		Str("query", query).
		Str("mode", string(mode)).
		Bool("has_quoted", analysis.HasQuotedPhrase).
		Strs("proper_nouns", analysis.ProperNouns).
		Msg("hybrid search starting")

	var result *HybridSearchResult

	switch mode {
	case HybridModeLexical:
		result = hs.searchFTSOnly(ctx, query, opts)
	case HybridModeSemantic:
		result = hs.searchSemanticOnly(ctx, query, opts)
	default:
		result = hs.searchFusion(ctx, query, opts, analysis)
	}

	result.Query = query
	result.Mode = mode
	result.SearchTime = float64(time.Since(start).Milliseconds())
	result.QueryAnalysis = analysis

	return result, nil
}

// analyzeQuery examines the query to determine the best search strategy.
func (hs *HybridSearcher) analyzeQuery(query string) QueryAnalysis {
	analysis := QueryAnalysis{}

	// Check for quoted phrases (exact match intent)
	if strings.Contains(query, `"`) || strings.Contains(query, `'`) {
		analysis.HasQuotedPhrase = true
	}

	// Detect entities: both single-word and multi-word proper nouns
	// This helps identify named entities that need exact matching
	words := strings.Fields(query)
	var currentProperNoun []string
	entitySet := make(map[string]bool) // Deduplicate entities

	// Common words to skip (not entities even if capitalized)
	skipWords := map[string]bool{
		"The": true, "A": true, "An": true, "In": true, "On": true,
		"At": true, "To": true, "For": true, "Of": true, "And": true,
		"Or": true, "But": true, "Is": true, "Are": true, "Was": true,
		"Were": true, "Be": true, "Been": true, "Being": true,
		"Have": true, "Has": true, "Had": true, "Do": true, "Does": true,
		"Did": true, "Will": true, "Would": true, "Could": true, "Should": true,
		"May": true, "Might": true, "Must": true, "Can": true,
		"What": true, "Where": true, "When": true, "Why": true, "How": true,
		"Who": true, "Which": true, "That": true, "This": true, "These": true,
		"Those": true, "I": true, "You": true, "He": true, "She": true,
		"It": true, "We": true, "They": true, "My": true, "Your": true,
	}

	for i, word := range words {
		// Clean punctuation
		cleanWord := strings.Trim(word, `"'.,;:!?()[]{}`)
		if cleanWord == "" {
			continue
		}

		// Check if word starts with uppercase
		firstRune := []rune(cleanWord)[0]
		isCapitalized := unicode.IsUpper(firstRune) && len(cleanWord) > 1

		// Skip common words even if capitalized (unless at sentence start)
		if isCapitalized && skipWords[cleanWord] && i > 0 {
			isCapitalized = false
		}

		if isCapitalized {
			currentProperNoun = append(currentProperNoun, cleanWord)
			// Also add as single-word entity if it's a significant word
			if len(cleanWord) >= 3 && !skipWords[cleanWord] {
				entitySet[cleanWord] = true
			}
		} else {
			if len(currentProperNoun) >= 2 {
				multiWord := strings.Join(currentProperNoun, " ")
				analysis.ProperNouns = append(analysis.ProperNouns, multiWord)
				entitySet[multiWord] = true
			}
			currentProperNoun = nil
		}
	}

	// Don't forget the last proper noun sequence
	if len(currentProperNoun) >= 2 {
		multiWord := strings.Join(currentProperNoun, " ")
		analysis.ProperNouns = append(analysis.ProperNouns, multiWord)
		entitySet[multiWord] = true
	}

	// Convert entity set to slice
	for entity := range entitySet {
		analysis.Entities = append(analysis.Entities, entity)
	}

	// Phase 12: Classify query type
	analysis.QueryType = hs.classifyQueryType(query, analysis)

	// Set convenience flags
	analysis.IsConceptual = analysis.QueryType == QueryTypeConceptual
	analysis.IsFactual = analysis.QueryType == QueryTypeFactual

	return analysis
}

// classifyQueryType determines the intent category of the query.
func (hs *HybridSearcher) classifyQueryType(query string, analysis QueryAnalysis) QueryType {
	// Priority 1: Exact quote - user wants literal text
	if analysis.HasQuotedPhrase {
		return QueryTypeExactQuote
	}

	// Priority 2: Check for conceptual patterns (understanding-seeking)
	for _, pattern := range conceptualPatterns {
		if pattern.MatchString(query) {
			// If also has entities, it's a mix - still conceptual but about specific things
			if len(analysis.Entities) > 0 {
				return QueryTypeConceptual // Conceptual takes priority for AI consumption
			}
			return QueryTypeConceptual
		}
	}

	// Priority 3: Check for factual patterns (data-seeking)
	for _, pattern := range factualPatterns {
		if pattern.MatchString(query) {
			return QueryTypeFactual
		}
	}

	// Priority 4: Entity-focused if proper nouns detected
	if len(analysis.ProperNouns) > 0 || len(analysis.Entities) > 0 {
		return QueryTypeEntity
	}

	// Default: exploratory
	return QueryTypeExploratory
}

// selectMode chooses the best search mode based on query analysis.
func (hs *HybridSearcher) selectMode(analysis QueryAnalysis) HybridSearchMode {
	// If query has quoted phrases, prefer lexical for exact match
	if analysis.HasQuotedPhrase {
		return HybridModeLexical
	}

	// If query has proper nouns, use fusion to catch both exact and semantic
	if len(analysis.ProperNouns) > 0 {
		return HybridModeFusion
	}

	// Default to fusion for best of both worlds
	return HybridModeFusion
}

// searchFusion performs parallel FTS5 and semantic search, then combines with RRF.
// Phase 12: Enhanced with agreement analysis and query-adaptive weighting.
func (hs *HybridSearcher) searchFusion(ctx context.Context, query string, opts HybridSearchOptions, analysis QueryAnalysis) *HybridSearchResult {
	// Fetch more candidates than needed for better fusion
	candidateLimit := opts.Limit * 3
	if candidateLimit < 30 {
		candidateLimit = 30
	}

	var ftsHits []SearchHit
	var semanticHits []SearchHit
	var wg sync.WaitGroup
	var ftsErr, semErr error
	semanticDegraded := false

	// Run FTS5 search
	wg.Add(1)
	go func() {
		defer wg.Done()
		ftsOpts := SearchOptions{
			Limit:     candidateLimit,
			SourceIDs: opts.SourceIDs,
			MimeTypes: opts.MimeTypes,
			Highlight: true,
		}
		result, err := hs.fts.Search(ctx, query, ftsOpts)
		if err != nil {
			ftsErr = err
			return
		}
		ftsHits = result.Results
	}()

	// Run semantic search (if available)
	if hs.semantic != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			semOpts := SemanticSearchOptions{
				Limit:     candidateLimit,
				SourceIDs: opts.SourceIDs,
				MimeTypes: opts.MimeTypes,
			}
			result, err := hs.semantic.Search(ctx, query, semOpts)
			if err != nil {
				semErr = err
				semanticDegraded = true
				return
			}
			// Convert SemanticSearchHit to SearchHit
			for _, hit := range result.Results {
				semanticHits = append(semanticHits, SearchHit{
					DocumentID: hit.DocumentID,
					ChunkID:    hit.ChunkID,
					Path:       hit.Path,
					Title:      hit.Title,
					Snippet:    hit.Snippet,
					Score:      hit.Score,
					Metadata:   hit.Metadata,
				})
			}
		}()
	}

	wg.Wait()

	// Log any errors but continue with available results
	if ftsErr != nil {
		hs.logger.Warn().Err(ftsErr).Msg("FTS5 search failed, using semantic only")
	}
	if semErr != nil {
		hs.logger.Warn().Err(semErr).Msg("semantic search failed, using FTS5 only")
		semanticDegraded = true
	}

	// Phase 12: Get query-type-specific weights
	weights := hs.getWeightsForQueryType(analysis.QueryType)
	if opts.SemanticWeight > 0 {
		// Allow override from options
		weights.Semantic = opts.SemanticWeight
		weights.Lexical = 1.0 - opts.SemanticWeight
	}

	// Phase 12: Apply RRF fusion with agreement tracking
	fused, agreementInfo := hs.applyRRFWithAgreement(ftsHits, semanticHits, opts.RRFConstant, weights)

	// Boost exact matches for entities (both single-word and multi-word)
	if opts.BoostExactMatch && len(analysis.Entities) > 0 {
		fused = hs.boostExactMatches(fused, analysis.Entities)
	}

	// Phase 12: Apply agreement-based boost
	fused = hs.applyAgreementBoost(fused, agreementInfo, analysis.QueryType)

	result := &HybridSearchResult{
		FTSHits:      len(ftsHits),
		SemanticHits: len(semanticHits),
		DegradedMode: semanticDegraded,
	}

	// Calculate strategies used
	strategiesUsed := 0
	if len(ftsHits) > 0 {
		strategiesUsed++
	}
	if len(semanticHits) > 0 {
		strategiesUsed++
	}
	result.StrategiesUsed = strategiesUsed

	// Phase 11: Apply similarity floor (reject low-confidence results)
	beforeFloor := len(fused)
	fused = hs.applySimilarityFloor(fused, opts.SimilarityFloor)
	result.RejectedByFloor = beforeFloor - len(fused)

	// Phase 11: Apply reranking on top candidates
	if opts.EnableRerank && len(fused) > 0 {
		fused = hs.applyReranking(fused, query, opts.RerankTopN, semanticHits)
		result.Reranked = true
	}

	// Phase 11: Apply MMR for diversity (after reranking)
	if opts.EnableMMR && len(fused) > 1 {
		fused = hs.applyMMR(fused, opts.MMRLambda, opts.Limit)
		result.MMRApplied = true
	}

	// Final limit
	if len(fused) > opts.Limit {
		fused = fused[:opts.Limit]
	}

	result.Results = fused
	result.TotalHits = len(fused)

	// Phase 12: Calculate overall confidence
	result.Confidence = hs.calculateOverallConfidence(fused, agreementInfo, strategiesUsed, semanticDegraded)

	// Add note if degraded
	if semanticDegraded {
		result.Note = "Semantic search unavailable, using lexical search only"
	}

	return result
}

// getWeightsForQueryType returns the optimal weights for the given query type.
func (hs *HybridSearcher) getWeightsForQueryType(queryType QueryType) StrategyWeights {
	if weights, ok := strategyWeightMatrix[queryType]; ok {
		return weights
	}
	// Default: equal weights
	return StrategyWeights{Semantic: 0.5, Lexical: 0.5}
}

// agreementInfo tracks which strategies found each result.
type agreementInfo struct {
	chunkStrategies map[string][]SearchStrategy // chunkID -> strategies that found it
	chunkBestRank   map[string]int              // chunkID -> best rank across strategies
}

// applyRRFWithAgreement implements RRF fusion while tracking strategy agreement.
func (hs *HybridSearcher) applyRRFWithAgreement(ftsHits, semanticHits []SearchHit, k int, weights StrategyWeights) ([]SearchHit, agreementInfo) {
	info := agreementInfo{
		chunkStrategies: make(map[string][]SearchStrategy),
		chunkBestRank:   make(map[string]int),
	}

	// Create maps of chunk_id -> rank for each list
	ftsRanks := make(map[string]int)
	for i, hit := range ftsHits {
		rank := i + 1 // 1-indexed rank
		ftsRanks[hit.ChunkID] = rank
		info.chunkStrategies[hit.ChunkID] = append(info.chunkStrategies[hit.ChunkID], StrategyFTSExact)
		if existing, ok := info.chunkBestRank[hit.ChunkID]; !ok || rank < existing {
			info.chunkBestRank[hit.ChunkID] = rank
		}
	}

	semRanks := make(map[string]int)
	for i, hit := range semanticHits {
		rank := i + 1
		semRanks[hit.ChunkID] = rank
		info.chunkStrategies[hit.ChunkID] = append(info.chunkStrategies[hit.ChunkID], StrategySemantic)
		if existing, ok := info.chunkBestRank[hit.ChunkID]; !ok || rank < existing {
			info.chunkBestRank[hit.ChunkID] = rank
		}
	}

	// Collect all unique chunks and their data
	allChunks := make(map[string]SearchHit)
	for _, hit := range ftsHits {
		allChunks[hit.ChunkID] = hit
	}
	for _, hit := range semanticHits {
		if _, exists := allChunks[hit.ChunkID]; !exists {
			allChunks[hit.ChunkID] = hit
		}
	}

	// Calculate RRF scores with query-type-specific weights
	type scoredHit struct {
		hit      SearchHit
		rrfScore float64
	}

	var scored []scoredHit

	for chunkID, hit := range allChunks {
		var rrfScore float64

		// FTS5 contribution
		if rank, ok := ftsRanks[chunkID]; ok {
			rrfScore += weights.Lexical * (1.0 / float64(k+rank))
		}

		// Semantic contribution
		if rank, ok := semRanks[chunkID]; ok {
			rrfScore += weights.Semantic * (1.0 / float64(k+rank))
		}

		scored = append(scored, scoredHit{
			hit:      hit,
			rrfScore: rrfScore,
		})
	}

	// Sort by RRF score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].rrfScore > scored[j].rrfScore
	})

	// Convert back to SearchHit slice with RRF scores
	result := make([]SearchHit, len(scored))
	for i, s := range scored {
		hit := s.hit
		hit.Score = s.rrfScore
		result[i] = hit
	}

	return result, info
}

// applyAgreementBoost boosts results that were found by multiple strategies.
func (hs *HybridSearcher) applyAgreementBoost(hits []SearchHit, info agreementInfo, queryType QueryType) []SearchHit {
	for i := range hits {
		chunkID := hits[i].ChunkID
		strategies := info.chunkStrategies[chunkID]
		numStrategies := len(strategies)

		// Calculate agreement score (0-1)
		// With 2 possible strategies (FTS, Semantic), max agreement is 2
		agreement := float64(numStrategies) / 2.0
		if agreement > 1.0 {
			agreement = 1.0
		}

		// Agreement bonus: up to 20% boost for full agreement
		agreementBonus := 1.0 + (agreement * 0.2)

		// Query-type-specific adjustments
		// For conceptual queries, boost semantic-only results
		if queryType == QueryTypeConceptual {
			hasSemantic := false
			for _, s := range strategies {
				if s == StrategySemantic {
					hasSemantic = true
					break
				}
			}
			if hasSemantic && numStrategies == 1 {
				// Semantic-only result for conceptual query: smaller penalty
				agreementBonus = 1.1 // 10% boost instead of no boost
			}
		}

		hits[i].Score *= agreementBonus
	}

	// Re-sort after boosting
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	return hits
}

// calculateOverallConfidence determines the overall confidence level of results.
func (hs *HybridSearcher) calculateOverallConfidence(hits []SearchHit, info agreementInfo, strategiesUsed int, degraded bool) string {
	if len(hits) == 0 {
		return "none"
	}

	// Count results with high agreement
	highAgreementCount := 0
	for _, hit := range hits {
		if strategies := info.chunkStrategies[hit.ChunkID]; len(strategies) >= 2 {
			highAgreementCount++
		}
	}

	// Determine confidence
	if degraded {
		// Degraded mode: lower confidence
		if len(hits) > 0 {
			return "medium"
		}
		return "low"
	}

	if strategiesUsed >= 2 && highAgreementCount >= len(hits)/2 {
		return "very_high"
	}

	if strategiesUsed >= 2 && highAgreementCount > 0 {
		return "high"
	}

	if strategiesUsed >= 1 && len(hits) > 0 {
		return "medium"
	}

	return "low"
}

// applyRRF implements Reciprocal Rank Fusion to combine ranked lists.
// Formula: RRF(d) = Σ 1/(k + rank_i(d)) for each ranking list i
func (hs *HybridSearcher) applyRRF(ftsHits, semanticHits []SearchHit, k int, semanticWeight float64) []SearchHit {
	// Create maps of chunk_id -> rank for each list
	ftsRanks := make(map[string]int)
	for i, hit := range ftsHits {
		ftsRanks[hit.ChunkID] = i + 1 // 1-indexed rank
	}

	semRanks := make(map[string]int)
	for i, hit := range semanticHits {
		semRanks[hit.ChunkID] = i + 1
	}

	// Collect all unique chunks and their data
	allChunks := make(map[string]SearchHit)
	for _, hit := range ftsHits {
		allChunks[hit.ChunkID] = hit
	}
	for _, hit := range semanticHits {
		if _, exists := allChunks[hit.ChunkID]; !exists {
			allChunks[hit.ChunkID] = hit
		}
	}

	// Calculate RRF scores
	type scoredHit struct {
		hit      SearchHit
		rrfScore float64
	}

	var scored []scoredHit
	ftsWeight := 1.0 - semanticWeight

	for chunkID, hit := range allChunks {
		var rrfScore float64

		// FTS5 contribution
		if rank, ok := ftsRanks[chunkID]; ok {
			rrfScore += ftsWeight * (1.0 / float64(k+rank))
		}

		// Semantic contribution
		if rank, ok := semRanks[chunkID]; ok {
			rrfScore += semanticWeight * (1.0 / float64(k+rank))
		}

		scored = append(scored, scoredHit{
			hit:      hit,
			rrfScore: rrfScore,
		})
	}

	// Sort by RRF score descending
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].rrfScore > scored[j].rrfScore
	})

	// Convert back to SearchHit slice with RRF scores
	result := make([]SearchHit, len(scored))
	for i, s := range scored {
		hit := s.hit
		hit.Score = s.rrfScore
		result[i] = hit
	}

	return result
}

// boostExactMatches increases the score of results containing exact entity matches.
// Multi-word entities (proper nouns) get a stronger boost than single-word entities.
func (hs *HybridSearcher) boostExactMatches(hits []SearchHit, entities []string) []SearchHit {
	// Sort entities by length (longer first) for better matching
	sortedEntities := make([]string, len(entities))
	copy(sortedEntities, entities)
	sort.Slice(sortedEntities, func(i, j int) bool {
		return len(sortedEntities[i]) > len(sortedEntities[j])
	})

	for i := range hits {
		content := strings.ToLower(hits[i].Snippet + " " + hits[i].Title + " " + hits[i].Path)
		totalBoost := 1.0

		for _, entity := range sortedEntities {
			entityLower := strings.ToLower(entity)
			if strings.Contains(content, entityLower) {
				// Boost based on entity length: multi-word gets more boost
				wordCount := len(strings.Fields(entity))
				if wordCount >= 2 {
					// Multi-word entity: 50% boost
					totalBoost *= 1.5
				} else {
					// Single-word entity: 20% boost
					totalBoost *= 1.2
				}
			}
		}

		// Cap total boost at 3x to avoid runaway scores
		if totalBoost > 3.0 {
			totalBoost = 3.0
		}

		hits[i].Score *= totalBoost
	}

	// Re-sort after boosting
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	return hits
}

// applySimilarityFloor removes results below the minimum score threshold.
// This prevents low-confidence garbage from appearing in results.
func (hs *HybridSearcher) applySimilarityFloor(hits []SearchHit, floor float64) []SearchHit {
	if floor <= 0 {
		return hits
	}

	var filtered []SearchHit
	for _, hit := range hits {
		if hit.Score >= floor {
			filtered = append(filtered, hit)
		}
	}

	if len(filtered) < len(hits) {
		hs.logger.Debug().
			Int("before", len(hits)).
			Int("after", len(filtered)).
			Float64("floor", floor).
			Msg("similarity floor applied")
	}

	return filtered
}

// applyReranking re-scores candidates using semantic similarity signals.
// This improves precision by leveraging the semantic scores directly.
func (hs *HybridSearcher) applyReranking(hits []SearchHit, query string, topN int, semanticHits []SearchHit) []SearchHit {
	if len(hits) == 0 {
		return hits
	}

	// Limit candidates to consider
	candidates := hits
	if len(candidates) > topN {
		candidates = candidates[:topN]
	}

	// Build a map of semantic scores for reranking boost
	semScores := make(map[string]float64)
	for _, sh := range semanticHits {
		semScores[sh.ChunkID] = sh.Score
	}

	// Rerank by combining RRF score with semantic score
	// Formula: final_score = rrf_score * (1 + semantic_score)
	// This boosts results that have high semantic relevance
	for i := range candidates {
		if semScore, ok := semScores[candidates[i].ChunkID]; ok {
			// Semantic scores are typically 0-1 (cosine similarity)
			// Boost the RRF score proportionally
			candidates[i].Score *= (1.0 + semScore)
		}
	}

	// Re-sort by new scores
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	hs.logger.Debug().
		Int("candidates", len(candidates)).
		Msg("reranking applied")

	return candidates
}

// applyMMR implements Maximal Marginal Relevance for result diversity.
// MMR = λ * sim(d, q) - (1-λ) * max(sim(d, d')) where d' is already selected
// This greedily selects documents that are both relevant and diverse.
func (hs *HybridSearcher) applyMMR(hits []SearchHit, lambda float64, limit int) []SearchHit {
	if len(hits) <= 1 {
		return hits
	}

	// We use text-based similarity as a proxy for embedding similarity
	// This avoids expensive embedding computations while still promoting diversity

	var selected []SearchHit
	remaining := make([]SearchHit, len(hits))
	copy(remaining, hits)

	// Always select the top result first
	selected = append(selected, remaining[0])
	remaining = remaining[1:]

	// Greedily select remaining results using MMR
	for len(selected) < limit && len(remaining) > 0 {
		bestIdx := -1
		bestScore := math.Inf(-1)

		for i, candidate := range remaining {
			// Relevance: use the current score (already computed from RRF + reranking)
			relevance := candidate.Score

			// Diversity: compute max similarity to already selected results
			maxSimilarity := 0.0
			for _, sel := range selected {
				sim := hs.textSimilarity(candidate.Snippet, sel.Snippet)
				if sim > maxSimilarity {
					maxSimilarity = sim
				}
			}

			// MMR score: balance relevance vs diversity
			mmrScore := lambda*relevance - (1-lambda)*maxSimilarity*relevance

			if mmrScore > bestScore {
				bestScore = mmrScore
				bestIdx = i
			}
		}

		if bestIdx >= 0 {
			selected = append(selected, remaining[bestIdx])
			// Remove selected from remaining
			remaining = append(remaining[:bestIdx], remaining[bestIdx+1:]...)
		} else {
			break
		}
	}

	hs.logger.Debug().
		Int("input", len(hits)).
		Int("output", len(selected)).
		Float64("lambda", lambda).
		Msg("MMR diversity applied")

	return selected
}

// textSimilarity computes Jaccard similarity between two text snippets.
// Returns a value between 0 (completely different) and 1 (identical).
func (hs *HybridSearcher) textSimilarity(text1, text2 string) float64 {
	// Tokenize into word sets
	words1 := hs.tokenize(text1)
	words2 := hs.tokenize(text2)

	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	// Compute Jaccard similarity: |intersection| / |union|
	set1 := make(map[string]bool)
	for _, w := range words1 {
		set1[w] = true
	}

	set2 := make(map[string]bool)
	for _, w := range words2 {
		set2[w] = true
	}

	intersection := 0
	for w := range set1 {
		if set2[w] {
			intersection++
		}
	}

	union := len(set1) + len(set2) - intersection
	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// tokenize splits text into lowercase words, filtering out short words and punctuation.
func (hs *HybridSearcher) tokenize(text string) []string {
	text = strings.ToLower(text)
	words := strings.Fields(text)

	var tokens []string
	for _, w := range words {
		// Remove punctuation
		w = strings.Trim(w, `"'.,;:!?()[]{}`)
		// Keep words with at least 3 characters
		if len(w) >= 3 {
			tokens = append(tokens, w)
		}
	}

	return tokens
}

// searchFTSOnly performs FTS5-only search.
func (hs *HybridSearcher) searchFTSOnly(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	ftsOpts := SearchOptions{
		Limit:     opts.Limit,
		SourceIDs: opts.SourceIDs,
		MimeTypes: opts.MimeTypes,
		Highlight: true,
	}

	result, err := hs.fts.Search(ctx, query, ftsOpts)
	if err != nil {
		hs.logger.Error().Err(err).Msg("FTS5 search failed")
		return &HybridSearchResult{}
	}

	return &HybridSearchResult{
		Results:   result.Results,
		TotalHits: result.TotalHits,
		FTSHits:   len(result.Results),
	}
}

// searchSemanticOnly performs semantic-only search.
func (hs *HybridSearcher) searchSemanticOnly(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	if hs.semantic == nil {
		hs.logger.Warn().Msg("semantic search requested but not available, falling back to FTS5")
		return hs.searchFTSOnly(ctx, query, opts)
	}

	semOpts := SemanticSearchOptions{
		Limit:     opts.Limit,
		SourceIDs: opts.SourceIDs,
		MimeTypes: opts.MimeTypes,
	}

	result, err := hs.semantic.Search(ctx, query, semOpts)
	if err != nil {
		hs.logger.Error().Err(err).Msg("semantic search failed")
		return &HybridSearchResult{}
	}

	// Convert SemanticSearchHit to SearchHit
	var hits []SearchHit
	for _, hit := range result.Results {
		hits = append(hits, SearchHit{
			DocumentID: hit.DocumentID,
			ChunkID:    hit.ChunkID,
			Path:       hit.Path,
			Title:      hit.Title,
			Snippet:    hit.Snippet,
			Score:      hit.Score,
			Metadata:   hit.Metadata,
		})
	}

	return &HybridSearchResult{
		Results:      hits,
		TotalHits:    result.TotalHits,
		SemanticHits: len(hits),
	}
}

// HasSemanticSearch returns true if semantic search is available.
func (hs *HybridSearcher) HasSemanticSearch() bool {
	return hs.semantic != nil
}

// SearchWithFallback implements the "never zero results" principle.
// It tries progressively more relaxed search strategies until results are found.
// Phase 12: This ensures AI clients always get something useful.
func (hs *HybridSearcher) SearchWithFallback(ctx context.Context, query string, opts HybridSearchOptions) (*HybridSearchResult, error) {
	start := time.Now()

	// Phase 1: Primary search (standard hybrid)
	result, err := hs.Search(ctx, query, opts)
	if err != nil {
		return nil, err
	}

	if len(result.Results) > 0 {
		result.FallbackLevel = 0
		return result, nil
	}

	hs.logger.Debug().Str("query", query).Msg("primary search returned no results, trying relaxed search")

	// Phase 2: Relaxed search (lower thresholds, broader matching)
	relaxedOpts := opts
	relaxedOpts.SimilarityFloor = 0.0001 // Very low floor
	relaxedOpts.EnableMMR = false        // Don't filter for diversity
	relaxedOpts.Limit = opts.Limit * 2   // Get more candidates

	relaxedResult := hs.searchRelaxed(ctx, query, relaxedOpts)
	if len(relaxedResult.Results) > 0 {
		// Limit to original request
		if len(relaxedResult.Results) > opts.Limit {
			relaxedResult.Results = relaxedResult.Results[:opts.Limit]
		}
		relaxedResult.TotalHits = len(relaxedResult.Results)
		relaxedResult.FallbackLevel = 1
		relaxedResult.Confidence = "low"
		relaxedResult.Note = "Using relaxed matching - verify relevance"
		relaxedResult.SearchTime = float64(time.Since(start).Milliseconds())
		return relaxedResult, nil
	}

	hs.logger.Debug().Str("query", query).Msg("relaxed search returned no results, trying partial match")

	// Phase 3: Partial word matching (split query into individual words)
	partialResult := hs.searchPartial(ctx, query, opts)
	if len(partialResult.Results) > 0 {
		partialResult.FallbackLevel = 2
		partialResult.Confidence = "speculative"
		partialResult.Note = "Partial word matching - results may not fully match query"
		partialResult.SearchTime = float64(time.Since(start).Milliseconds())
		return partialResult, nil
	}

	// Phase 4: No results found - return empty with suggestions
	hs.logger.Info().Str("query", query).Msg("no results found after all fallback attempts")

	return &HybridSearchResult{
		Results:       []SearchHit{},
		TotalHits:     0,
		Query:         query,
		SearchTime:    float64(time.Since(start).Milliseconds()),
		Mode:          HybridModeFusion,
		FallbackLevel: 3,
		Confidence:    "none",
		Note:          "No matching documents found. Try different search terms or verify documents are indexed.",
	}, nil
}

// searchRelaxed performs a relaxed FTS5 search with wildcards and stemming.
func (hs *HybridSearcher) searchRelaxed(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	// Modify query for relaxed matching
	// Add wildcards to each word for prefix matching
	words := strings.Fields(query)
	var relaxedTerms []string
	for _, word := range words {
		clean := strings.Trim(word, `"'.,;:!?()[]{}`)
		if len(clean) >= 2 {
			// Use OR for broader matching
			relaxedTerms = append(relaxedTerms, clean+"*")
		}
	}

	if len(relaxedTerms) == 0 {
		return &HybridSearchResult{}
	}

	relaxedQuery := strings.Join(relaxedTerms, " OR ")

	ftsOpts := SearchOptions{
		Limit:     opts.Limit,
		SourceIDs: opts.SourceIDs,
		MimeTypes: opts.MimeTypes,
		Highlight: true,
	}

	result, err := hs.fts.Search(ctx, relaxedQuery, ftsOpts)
	if err != nil {
		hs.logger.Warn().Err(err).Str("query", relaxedQuery).Msg("relaxed FTS5 search failed")
		return &HybridSearchResult{}
	}

	return &HybridSearchResult{
		Results:   result.Results,
		TotalHits: result.TotalHits,
		FTSHits:   len(result.Results),
		Mode:      HybridModeLexical,
	}
}

// searchPartial searches for each word in the query individually and merges results.
func (hs *HybridSearcher) searchPartial(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	words := strings.Fields(query)

	// Collect unique results from searching each significant word
	seen := make(map[string]bool)
	var allHits []SearchHit

	for _, word := range words {
		clean := strings.Trim(word, `"'.,;:!?()[]{}`)
		if len(clean) < 3 {
			continue // Skip short words
		}

		ftsOpts := SearchOptions{
			Limit:     5, // Small limit per word
			SourceIDs: opts.SourceIDs,
			MimeTypes: opts.MimeTypes,
			Highlight: true,
		}

		result, err := hs.fts.Search(ctx, clean, ftsOpts)
		if err != nil {
			continue
		}

		for _, hit := range result.Results {
			if !seen[hit.ChunkID] {
				seen[hit.ChunkID] = true
				allHits = append(allHits, hit)
			}
		}
	}

	// Sort by score and limit
	sort.Slice(allHits, func(i, j int) bool {
		return allHits[i].Score > allHits[j].Score
	})

	if len(allHits) > opts.Limit {
		allHits = allHits[:opts.Limit]
	}

	return &HybridSearchResult{
		Results:   allHits,
		TotalHits: len(allHits),
		FTSHits:   len(allHits),
		Mode:      HybridModeLexical,
	}
}
