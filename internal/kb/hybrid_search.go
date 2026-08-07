package kb

import (
	"context"
	"errors"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

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
	QueryTypeExactQuote  QueryType = "exact_quote" // User wants literal text match
	QueryTypeEntity      QueryType = "entity"      // Query focuses on named entities
	QueryTypeConceptual  QueryType = "conceptual"  // User seeking understanding/explanation
	QueryTypeFactual     QueryType = "factual"     // User wants specific data/facts
	QueryTypeExploratory QueryType = "exploratory" // Broad topic exploration
)

// SearchStrategy identifies which search method found a result.
type SearchStrategy string

const (
	StrategyFTSExact   SearchStrategy = "fts_exact"   // FTS5 exact phrase match
	StrategyFTSRelaxed SearchStrategy = "fts_relaxed" // FTS5 with wildcards/stemming
	StrategySemantic   SearchStrategy = "semantic"    // Vector similarity search
)

// WP-3.4 deleted StrategyWeights, strategyWeightMatrix and
// getWeightsForQueryType -- issue #69.
//
// The matrix claimed to tune the semantic/lexical balance per query type. It
// never did: Search defaulted opts.SemanticWeight to 0.5 when unset, and
// searchFusion then read the matrix and UNCONDITIONALLY overrode it from
// opts.SemanticWeight, which was now always > 0. Every query fused 50/50
// whatever the classifier said. Two years of tuning values that nothing read.
//
// Rather than wire it up -- which would silently change every ranking in the
// product on the strength of five hand-picked constants nobody has measured --
// fusion is now plain unweighted RRF with a configurable k. Query
// classification survives because selectMode still uses it to pick lexical
// versus fusion, which is a decision the code actually acts on.

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

// SemanticProvider is the semantic half of hybrid retrieval. *SemanticSearcher
// is the production implementation; tests inject fakes to drive the fusion and
// degraded-mode branches without an embedding service.
type SemanticProvider interface {
	Search(ctx context.Context, query string, opts SemanticSearchOptions) (*SemanticSearchResult, error)
}

// DefaultRRFConstant is the k in the reciprocal-rank formula 1/(k + rank).
// 60 is the value from the original RRF paper and the one every stored score in
// the golden corpus was computed with.
const DefaultRRFConstant = 60

// HybridSearcher combines FTS5 (lexical) and vector (semantic) search using RRF.
type HybridSearcher struct {
	fts         *Searcher
	semantic    SemanticProvider
	rrfConstant int
	logger      zerolog.Logger
}

// HybridOption configures a HybridSearcher at construction.
type HybridOption func(*HybridSearcher)

// WithRRFConstant sets the k in RRF's 1/(k + rank).
//
// It is an engine tuning constant rather than a per-query knob, which is why it
// lives on the searcher and not in HybridSearchOptions: changing it per call
// makes scores from two calls incomparable, and nothing ever varied it.
// Non-positive values are ignored.
func WithRRFConstant(k int) HybridOption {
	return func(hs *HybridSearcher) {
		if k > 0 {
			hs.rrfConstant = k
		}
	}
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

// RecallMode controls the precision/recall tradeoff via presets.
// Higher recall returns more results (good for finding all variants),
// higher precision returns fewer, more relevant results.
type RecallMode string

const (
	// RecallModeHigh maximizes recall - returns all potentially relevant results.
	// Good for threat intelligence, finding all variants, comprehensive searches.
	// Disables MMR diversity filtering, no similarity floor, fetches more candidates.
	RecallModeHigh RecallMode = "high"

	// RecallModeBalanced provides a balance between recall and precision.
	// Good for general queries where you want relevant results without too much noise.
	// Uses moderate MMR (λ=0.7), standard similarity floor.
	RecallModeBalanced RecallMode = "balanced"

	// RecallModePrecise maximizes precision - returns fewer, highly relevant results.
	// Good for specific lookups where you want to avoid duplicates.
	// Aggressive MMR filtering (λ=0.5), higher similarity floor.
	RecallModePrecise RecallMode = "precise"
)

// SearchFilter restricts which documents a search may return.
type SearchFilter struct {
	SourceIDs []string // Only these sources, when non-empty
	MimeTypes []string // Only these MIME types, when non-empty
}

// HybridSearchOptions configures one hybrid search.
//
// WP-3.4 cut this from thirteen fields to four. The audit behind that (#69):
//
//	Limit            live
//	Mode             live
//	RecallMode       live -- and the ONLY thing that set the quality knobs
//	SourceIDs        live -> Filter.SourceIDs
//	MimeTypes        live -> Filter.MimeTypes
//	RRFConstant      live, but never varied -> WithRRFConstant on the searcher
//	SemanticWeight   deleted with the adaptive-weighting machinery (#69)
//	BoostExactMatch  DEAD: Search set it to true unconditionally, ignoring the caller
//	EnableMMR        DEAD: every RecallMode branch overwrote it
//	EnableRerank     DEAD: every RecallMode branch set it to true
//	MMRLambda        overwritten by the high and precise presets; honoured only in balanced
//	SimilarityFloor  overwritten by the high and precise presets; honoured only in balanced
//	RerankTopN       honoured when > 0, otherwise preset
//
// The last three were the dangerous ones: a caller could set them, watch two of
// the three presets silently discard the value, and have no way to tell. They
// are now internal to the preset (see recallSettings), so the precision/recall
// tradeoff has exactly one control surface.
type HybridSearchOptions struct {
	Limit      int              // Max results (default 10)
	Mode       HybridSearchMode // Search mode (default auto)
	RecallMode RecallMode       // Precision/recall preset (default balanced)
	Filter     SearchFilter     // Source and MIME-type restrictions
}

// recallSettings are the quality-stage knobs a RecallMode preset resolves to.
type recallSettings struct {
	enableMMR       bool
	mmrLambda       float64 // 0 = max diversity, 1 = max relevance
	similarityFloor float64 // reject fused scores below this
	enableRerank    bool
	rerankTopN      int
}

// resolveRecallMode expands a preset into the stage settings it implies.
//
// This was an inline switch in Search with no seam, which is why
// TestRecallModePresets used to carry a hand-copied duplicate of it.
func resolveRecallMode(mode RecallMode) recallSettings {
	switch mode {
	case RecallModeHigh:
		// Everything potentially relevant: no diversity filtering, no floor,
		// and a wider rerank window.
		return recallSettings{
			enableMMR:       false,
			mmrLambda:       1.0,
			similarityFloor: 0.0,
			enableRerank:    true,
			rerankTopN:      50,
		}
	case RecallModePrecise:
		return recallSettings{
			enableMMR:       true,
			mmrLambda:       0.5,
			similarityFloor: 0.01,
			enableRerank:    true,
			rerankTopN:      30,
		}
	default: // RecallModeBalanced, and anything unrecognised
		return recallSettings{
			enableMMR:       true,
			mmrLambda:       DefaultMMRLambda,
			similarityFloor: DefaultSimilarityFloor,
			enableRerank:    true,
			rerankTopN:      DefaultRerankTopN,
		}
	}
}

// HybridSearchResult contains combined search results with metadata.
type HybridSearchResult struct {
	Results       []SearchHit      `json:"results"`
	TotalHits     int              `json:"total_hits"`
	Query         string           `json:"query"`
	SearchTime    float64          `json:"search_time_ms"`
	Mode          HybridSearchMode `json:"mode"`
	FTSHits       int              `json:"fts_hits"`
	SemanticHits  int              `json:"semantic_hits"`
	QueryAnalysis QueryAnalysis    `json:"query_analysis,omitempty"`

	// Quality enhancement metrics
	RejectedByFloor int  `json:"rejected_by_floor,omitempty"` // Count of results below similarity floor
	MMRApplied      bool `json:"mmr_applied,omitempty"`       // Whether MMR diversity was applied
	Reranked        bool `json:"reranked,omitempty"`          // Whether reranking was applied

	// Query-adaptive confidence model (Phase 12)
	Confidence     string `json:"confidence,omitempty"`      // Overall confidence: very_high, high, medium, low, speculative, none
	StrategiesUsed int    `json:"strategies_used,omitempty"` // Number of strategies that contributed

	// DegradedMode is true when a retrieval strategy FAILED, as opposed to
	// matching nothing. Note carries the reason. Issue #75 widened this from
	// "semantic search failed" to cover the lexical half too, which used to
	// report an outright FTS5 error as an ordinary empty result.
	DegradedMode bool `json:"degraded_mode,omitempty"`

	Note          string `json:"note,omitempty"`           // Human-readable note about results
	FallbackLevel int    `json:"fallback_level,omitempty"` // 0=primary, 1=relaxed, 2=partial, 3=none
}

// QueryAnalysis provides insight into how the query was interpreted.
type QueryAnalysis struct {
	HasQuotedPhrase bool      `json:"has_quoted_phrase,omitempty"`
	ProperNouns     []string  `json:"proper_nouns,omitempty"` // Multi-word proper nouns (e.g., "Oak Ridge")
	Entities        []string  `json:"entities,omitempty"`     // All detected entities (single + multi-word)
	SuggestedMode   string    `json:"suggested_mode,omitempty"`
	QueryType       QueryType `json:"query_type,omitempty"`    // Classified query type
	IsConceptual    bool      `json:"is_conceptual,omitempty"` // True if query seeks understanding
	IsFactual       bool      `json:"is_factual,omitempty"`    // True if query seeks specific data
}

// WP-3.2 deleted EnhancedSearchHit (SearchHit plus found_by / agreement /
// best_rank). Nothing ever constructed or returned one. The agreement data it
// advertised lives in agreementInfo and is consumed by
// calculateOverallConfidence, which reports one confidence for the result set
// rather than one per hit.

// NewHybridSearcher creates a hybrid searcher over the production semantic
// searcher. A nil semantic searcher yields a lexical-only searcher.
//
// The explicit nil check matters: assigning a nil *SemanticSearcher straight
// into the SemanticProvider field would produce an interface that is non-nil
// but panics on call, and every "is semantic available?" test in this file is a
// nil comparison.
func NewHybridSearcher(fts *Searcher, semantic *SemanticSearcher, opts ...HybridOption) *HybridSearcher {
	if semantic == nil {
		return NewHybridSearcherWith(fts, nil, opts...)
	}
	return NewHybridSearcherWith(fts, semantic, opts...)
}

// NewHybridSearcherWith creates a hybrid searcher over any semantic provider.
// This is the injection seam used by tests.
func NewHybridSearcherWith(fts *Searcher, semantic SemanticProvider, opts ...HybridOption) *HybridSearcher {
	hs := &HybridSearcher{
		fts:         fts,
		semantic:    semantic,
		rrfConstant: DefaultRRFConstant,
		logger:      observability.Logger("kb.hybrid"),
	}
	for _, opt := range opts {
		opt(hs)
	}
	return hs
}

// Search performs hybrid search using the configured mode.
func (hs *HybridSearcher) Search(ctx context.Context, query string, opts HybridSearchOptions) (*HybridSearchResult, error) {
	start := time.Now()

	// Apply defaults
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.RecallMode == "" {
		opts.RecallMode = RecallModeBalanced
	}
	recall := resolveRecallMode(opts.RecallMode)

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
		result = hs.searchFusion(ctx, query, opts, recall, analysis)
	}

	result.Query = query
	result.Mode = mode
	result.SearchTime = float64(time.Since(start).Milliseconds())
	result.QueryAnalysis = analysis
	assignRanks(result.Results)

	return result, nil
}

// assignRanks stamps the 1-based position of every hit in the returned order.
//
// It is called at the last possible moment on every path, after MMR has had its
// say, so Rank always describes the order the caller actually receives.
func assignRanks(hits []SearchHit) {
	for i := range hits {
		hits[i].Rank = i + 1
	}
}

// analyzeQuery examines the query to determine the best search strategy.
func (hs *HybridSearcher) analyzeQuery(query string) QueryAnalysis {
	analysis := QueryAnalysis{}

	// Check for quoted phrases (exact match intent).
	//
	// Issue #70: this used to be `contains(query, '"') || contains(query, '\'')`,
	// so every contraction and possessive -- "don't", "Alice's", "O'Brien" --
	// was classified as an exact quote and silently forced the whole search into
	// lexical-only mode, disabling the vector half. Only a balanced pair of
	// DOUBLE quotes signals phrase intent; an apostrophe is just a character in
	// a word. FTS5 agrees: its phrase delimiter is '"', never '\''.
	analysis.HasQuotedPhrase = hasQuotedPhrase(query)

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

// hasQuotedPhrase reports whether the query contains a balanced double-quoted
// phrase, i.e. whether the user asked for a literal match.
func hasQuotedPhrase(query string) bool {
	for _, tk := range splitFTSQuery(query) {
		if tk.phrase {
			return true
		}
	}
	return false
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

// searchFusion performs parallel FTS5 and semantic search, then combines them
// with unweighted RRF.
func (hs *HybridSearcher) searchFusion(ctx context.Context, query string, opts HybridSearchOptions, recall recallSettings, analysis QueryAnalysis) *HybridSearchResult {
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
			SourceIDs: opts.Filter.SourceIDs,
			MimeTypes: opts.Filter.MimeTypes,
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
				SourceIDs: opts.Filter.SourceIDs,
				MimeTypes: opts.Filter.MimeTypes,
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

	// A failed strategy is NOT the same as a strategy that matched nothing, and
	// the caller has to be able to tell the difference (issue #75). Both cases
	// set DegradedMode and contribute a reason to Note.
	var degradedNotes []string
	lexicalDegraded := false
	if ftsErr != nil {
		hs.logger.Warn().Err(ftsErr).Msg("FTS5 search failed")
		lexicalDegraded = true
		degradedNotes = append(degradedNotes, "Lexical search failed: "+ftsErr.Error())
	}
	if semErr != nil {
		// A model change is reported in the RESULT, to every frontend, by the
		// degraded note below -- so logging it here as well would print it to a
		// CLI user twice. Debug keeps it available when someone is actually
		// debugging. The generic failure stays at warn because its note does not
		// carry the underlying error, and that detail has to live somewhere.
		var mismatch *ModelMismatchError
		if errors.As(semErr, &mismatch) {
			hs.logger.Debug().Err(semErr).Msg("semantic leg skipped: embedding model changed")
		} else {
			hs.logger.Warn().Err(semErr).Msg("semantic search failed, using FTS5 only")
		}
		semanticDegraded = true
	}
	if semanticDegraded {
		degradedNotes = append(degradedNotes, semanticDegradedNote(semErr))
	}

	// Apply RRF fusion with agreement tracking.
	fused, agreementInfo := hs.applyRRFWithAgreement(ftsHits, semanticHits, hs.rrfConstant)

	// Boost exact matches for entities (both single-word and multi-word)
	if len(analysis.Entities) > 0 {
		fused = hs.boostExactMatches(fused, analysis.Entities)
	}

	// Phase 12: Apply agreement-based boost
	fused = hs.applyAgreementBoost(fused, agreementInfo)

	result := &HybridSearchResult{
		FTSHits:      len(ftsHits),
		SemanticHits: len(semanticHits),
		DegradedMode: semanticDegraded || lexicalDegraded,
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

	// Apply similarity floor (reject low-confidence results)
	beforeFloor := len(fused)
	fused = hs.applySimilarityFloor(fused, recall.similarityFloor)
	result.RejectedByFloor = beforeFloor - len(fused)

	// Apply reranking on top candidates
	if recall.enableRerank && len(fused) > 0 {
		fused = hs.applyReranking(fused, query, recall.rerankTopN, semanticHits)
		result.Reranked = true
	}

	// Apply MMR for diversity (after reranking)
	if recall.enableMMR && len(fused) > 1 {
		fused = hs.applyMMR(fused, recall.mmrLambda, opts.Limit)
		result.MMRApplied = true
	}

	// Final limit
	if len(fused) > opts.Limit {
		fused = fused[:opts.Limit]
	}

	result.Results = fused
	result.TotalHits = len(fused)

	// Phase 12: Calculate overall confidence
	result.Confidence = hs.calculateOverallConfidence(fused, agreementInfo, strategiesUsed, result.DegradedMode)

	if len(degradedNotes) > 0 {
		result.Note = strings.Join(degradedNotes, " ")
	}

	return result
}

// semanticDegradedNote explains a failed semantic leg in one sentence.
//
// The generic sentence is right for a provider that is down or slow: retry, or
// look at doctor. It is wrong for a model change, where the vectors are intact,
// the provider is healthy, nothing will fix itself, and there is exactly one
// command that helps -- so that case gets its own sentence naming both models
// and the remedy (issue #107).
func semanticDegradedNote(err error) string {
	var mismatch *ModelMismatchError
	if errors.As(err, &mismatch) {
		return mismatch.Note()
	}
	return "Semantic search unavailable, using lexical search only"
}

// agreementInfo tracks which strategies found each result.
//
// WP-3.2 dropped the chunkBestRank map: it was written on every fusion and read
// by nothing but a test asserting it had been written.
type agreementInfo struct {
	chunkStrategies map[string][]SearchStrategy // chunkID -> strategies that found it
}

// applyRRFWithAgreement implements RRF fusion while tracking strategy
// agreement.
//
//	score(d) = sum over the strategies that found d of 1/(k + rank)
//
// with 1-indexed ranks. Unweighted since #69: the per-strategy weights this
// used to take were always 0.5/0.5 in practice, so they scaled every score by
// the same constant and changed no ordering.
func (hs *HybridSearcher) applyRRFWithAgreement(ftsHits, semanticHits []SearchHit, k int) ([]SearchHit, agreementInfo) {
	info := agreementInfo{
		chunkStrategies: make(map[string][]SearchStrategy),
	}

	// Create maps of chunk_id -> rank for each list
	ftsRanks := make(map[string]int)
	for i, hit := range ftsHits {
		ftsRanks[hit.ChunkID] = i + 1 // 1-indexed rank
		info.chunkStrategies[hit.ChunkID] = append(info.chunkStrategies[hit.ChunkID], StrategyFTSExact)
	}

	semRanks := make(map[string]int)
	for i, hit := range semanticHits {
		semRanks[hit.ChunkID] = i + 1
		info.chunkStrategies[hit.ChunkID] = append(info.chunkStrategies[hit.ChunkID], StrategySemantic)
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

	for chunkID, hit := range allChunks {
		var rrfScore float64

		// FTS5 contribution
		if rank, ok := ftsRanks[chunkID]; ok {
			rrfScore += 1.0 / float64(k+rank)
		}

		// Semantic contribution
		if rank, ok := semRanks[chunkID]; ok {
			rrfScore += 1.0 / float64(k+rank)
		}

		scored = append(scored, scoredHit{
			hit:      hit,
			rrfScore: rrfScore,
		})
	}

	// Sort by RRF score descending, breaking ties on chunk id.
	//
	// The tie-break is load-bearing, not cosmetic: `scored` is built by ranging
	// over a map, so its incoming order is randomised per run, and sort.Slice is
	// not stable. Two chunks with equal fused scores -- which is common now that
	// fusion is unweighted, e.g. one chunk at lexical rank 1 / semantic rank 3
	// against another at lexical rank 3 / semantic rank 1 -- would otherwise
	// swap places between identical searches over an unchanged index.
	sort.Slice(scored, func(i, j int) bool {
		if scored[i].rrfScore != scored[j].rrfScore {
			return scored[i].rrfScore > scored[j].rrfScore
		}
		return scored[i].hit.ChunkID < scored[j].hit.ChunkID
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
//
// It takes no query type: the one branch that consulted it was a no-op (see
// below), so boosting depends only on how many strategies found a chunk.
func (hs *HybridSearcher) applyAgreementBoost(hits []SearchHit, info agreementInfo) []SearchHit {
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

		// WP-3.2 removed a "conceptual query" branch here. It claimed to give
		// semantic-only hits a special 1.1x bonus on conceptual queries, but a
		// single-strategy hit already scores agreement = 1/2, hence
		// 1.0 + 0.5*0.2 = 1.1 -- the branch assigned the value the line above
		// had just computed. It changed nothing for any input, which is why
		// deleting it leaves every fusion score identical.

		hits[i].Score *= agreementBonus
	}

	// Re-sort after boosting. Stable, so equally scored hits keep the order the
	// fusion stage put them in rather than being reshuffled.
	sort.SliceStable(hits, func(i, j int) bool {
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

	// Issue #77: the gate was `highAgreementCount >= len(hits)/2` using integer
	// division, so a SINGLE result with zero agreement scored 0 >= 0 and came
	// back "very_high" -- the highest confidence in the vocabulary, awarded to
	// the case with the least corroboration. The comparison is now
	// multiplication (a real majority, no truncation) with an explicit minimum
	// of one corroborated hit.
	if strategiesUsed >= 2 && highAgreementCount > 0 && highAgreementCount*2 >= len(hits) {
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

// WP-3.2 deleted applyRRF, the unweighted-signature twin of
// applyRRFWithAgreement. It had no callers: every fusion path goes through
// applyRRFWithAgreement, which computes the same reciprocal-rank formula and
// additionally records which strategy found each chunk.

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

	// Re-sort after boosting. Stable, for the same reason as above.
	sort.SliceStable(hits, func(i, j int) bool {
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

	// Re-sort by new scores, stably.
	sort.SliceStable(candidates, func(i, j int) bool {
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

// normalizeToReciprocalRank rewrites Score onto the same scale fusion uses.
//
// Issue #77: the single-strategy modes passed their native score straight
// through. Lexical mode returned raw SQLite bm25 -- negative-is-better,
// unbounded, and not comparable between queries -- under the same JSON `score`
// key that fusion fills with a small positive RRF value. A client had no way to
// know which convention it was reading, and no way to compare the two.
//
// The chosen normalization is the reciprocal rank itself:
//
//	score(rank) = 1/(k + rank)
//
// which is exactly what fusion computes for a hit that one strategy found at
// that rank. So a lexical-mode result and a single-strategy fusion result now
// carry not merely comparable numbers but the SAME number. It is monotone
// decreasing in rank, positive, bounded by 1/(k+1), and loses nothing a caller
// could act on: the underlying bm25 magnitude was never comparable across
// queries, only the ordering was, and the ordering is preserved exactly.
func (hs *HybridSearcher) normalizeToReciprocalRank(hits []SearchHit) {
	for i := range hits {
		hits[i].Score = 1.0 / float64(hs.rrfConstant+i+1)
	}
}

// singleStrategyResult builds the result envelope for a mode that ran exactly
// one strategy, so that Confidence and StrategiesUsed are populated the way
// fusion populates them (issue #77: searchFTSOnly and searchSemanticOnly left
// both fields at their zero values, and a client reading `confidence` could not
// tell "not computed" from "no confidence").
func (hs *HybridSearcher) singleStrategyResult(hits []SearchHit, totalHits int) *HybridSearchResult {
	hs.normalizeToReciprocalRank(hits)

	strategiesUsed := 0
	if len(hits) > 0 {
		strategiesUsed = 1
	}

	return &HybridSearchResult{
		Results:        hits,
		TotalHits:      totalHits,
		StrategiesUsed: strategiesUsed,
		Confidence: hs.calculateOverallConfidence(
			hits, agreementInfo{chunkStrategies: map[string][]SearchStrategy{}}, strategiesUsed, false),
	}
}

// searchFTSOnly performs FTS5-only search.
func (hs *HybridSearcher) searchFTSOnly(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	ftsOpts := SearchOptions{
		Limit:     opts.Limit,
		SourceIDs: opts.Filter.SourceIDs,
		MimeTypes: opts.Filter.MimeTypes,
		Highlight: true,
	}

	result, err := hs.fts.Search(ctx, query, ftsOpts)
	if err != nil {
		// Issue #75: this used to return a bare zero value, which a caller
		// could not distinguish from "nothing matched".
		hs.logger.Error().Err(err).Msg("FTS5 search failed")
		return &HybridSearchResult{
			DegradedMode: true,
			Confidence:   "none",
			Note:         "Lexical search failed: " + err.Error(),
		}
	}

	out := hs.singleStrategyResult(result.Results, result.TotalHits)
	out.FTSHits = len(result.Results)
	return out
}

// searchSemanticOnly performs semantic-only search.
func (hs *HybridSearcher) searchSemanticOnly(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	if hs.semantic == nil {
		hs.logger.Warn().Msg("semantic search requested but not available, falling back to FTS5")
		return hs.searchFTSOnly(ctx, query, opts)
	}

	semOpts := SemanticSearchOptions{
		Limit:     opts.Limit,
		SourceIDs: opts.Filter.SourceIDs,
		MimeTypes: opts.Filter.MimeTypes,
	}

	result, err := hs.semantic.Search(ctx, query, semOpts)
	if err != nil {
		// A model change is not a transient failure: the semantic leg will stay
		// unusable until the vectors are rebuilt. Returning nothing would tell a
		// user their knowledge base is empty, so fall through to lexical -- which
		// is completely unaffected -- and carry the explanation with it.
		var mismatch *ModelMismatchError
		if errors.As(err, &mismatch) {
			// Debug, for the same reason as in searchFusion: the note below
			// reaches every frontend, and repeating it in the log prints it to a
			// CLI user twice.
			hs.logger.Debug().Err(err).Msg("semantic leg skipped: embedding model changed")
			out := hs.searchFTSOnly(ctx, query, opts)
			out.DegradedMode = true
			out.Note = strings.TrimSpace(mismatch.Note() + " " + out.Note)
			return out
		}
		hs.logger.Error().Err(err).Msg("semantic search failed")
		return &HybridSearchResult{
			DegradedMode: true,
			Confidence:   "none",
			Note:         "Semantic search failed: " + err.Error(),
		}
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

	// Semantic-only goes through the same normalization as lexical-only, so
	// HybridSearcher.Score means one thing in every mode. Callers that want the
	// raw cosine similarity call SemanticSearcher directly -- kbservice's
	// "semantic" search mode does exactly that and is unaffected.
	out := hs.singleStrategyResult(hits, result.TotalHits)
	out.SemanticHits = len(hits)
	return out
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

	// Phase 2: Relaxed search (broader matching, more candidates).
	//
	// The similarity floor and MMR settings this used to override are no longer
	// reachable here and never were: searchRelaxed is a straight FTS5 query that
	// never enters the fusion pipeline, so neither stage runs on this rung.
	relaxedOpts := opts
	relaxedOpts.RecallMode = RecallModeHigh
	relaxedOpts.Limit = opts.Limit * 2 // Get more candidates

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
		relaxedResult.Query = query
		hs.normalizeToReciprocalRank(relaxedResult.Results)
		assignRanks(relaxedResult.Results)
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
		partialResult.Query = query
		hs.normalizeToReciprocalRank(partialResult.Results)
		assignRanks(partialResult.Results)
		return partialResult, nil
	}

	// Phase 4: No results found - return empty with suggestions.
	//
	// Issue #75: the ladder used to build this from scratch, throwing away the
	// primary search's DegradedMode. A query that found nothing because the
	// lexical engine had FAILED came back looking exactly like a query that
	// found nothing because nothing matched.
	hs.logger.Info().Str("query", query).Msg("no results found after all fallback attempts")

	if result.DegradedMode {
		return &HybridSearchResult{
			Results:       []SearchHit{},
			TotalHits:     0,
			Query:         query,
			SearchTime:    float64(time.Since(start).Milliseconds()),
			Mode:          result.Mode,
			FallbackLevel: 3,
			Confidence:    "none",
			DegradedMode:  true,
			Note: result.Note + " No matching documents found, but a retrieval strategy failed, " +
				"so this is not evidence that the knowledge base lacks the content.",
		}, nil
	}

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

// searchRelaxed performs a relaxed FTS5 search: every term prefix-matched, all
// of them OR'd, so any one word is enough to produce a hit.
//
// Issue #73(b): this used to build the FTS5 string `a* OR b* OR c*` and hand it
// to Searcher.Search -- whose sanitizer deleted every '*' as a dangerous
// character and then re-added exactly one, to the last term. A three-word
// relaxed query therefore reached FTS5 as `a OR b OR c*`: two exact terms and
// one prefix. The "relaxed" rung was stricter than the partial rung below it,
// which got its wildcard back by searching one word at a time.
//
// The expression is now built explicitly and passed through SearchExpr, which
// does not sanitize. Nothing here comes from raw user text without being
// quoted first, so the operators are ours and the terms are theirs.
// Issue #96: query scaffolding is dropped here too. OR-ing "how" against a
// corpus matches every document containing the word "how", which on this rung
// is not merely useless but actively harmful: it dilutes the ranking with
// documents that share nothing but grammar.
func (hs *HybridSearcher) searchRelaxed(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	var relaxedTerms []string
	for _, tk := range contentFTSTokens(splitFTSQuery(query)) {
		if utf8.RuneCountInString(tk.text) < 2 {
			continue
		}
		term := quoteFTSToken(tk.text)
		if !tk.phrase && ftsPrefixable(tk.text) {
			term += "*"
		}
		relaxedTerms = append(relaxedTerms, term)
	}

	if len(relaxedTerms) == 0 {
		return &HybridSearchResult{}
	}

	relaxedQuery := strings.Join(relaxedTerms, " OR ")

	ftsOpts := SearchOptions{
		Limit:     opts.Limit,
		SourceIDs: opts.Filter.SourceIDs,
		MimeTypes: opts.Filter.MimeTypes,
		Highlight: true,
	}

	result, err := hs.fts.SearchExpr(ctx, relaxedQuery, query, ftsOpts)
	if err != nil {
		hs.logger.Warn().Err(err).Str("query", relaxedQuery).Msg("relaxed FTS5 search failed")
		return &HybridSearchResult{
			DegradedMode: true,
			Confidence:   "none",
			Note:         "Relaxed lexical search failed: " + err.Error(),
			Mode:         HybridModeLexical,
		}
	}

	return &HybridSearchResult{
		Results:   result.Results,
		TotalHits: result.TotalHits,
		FTSHits:   len(result.Results),
		Mode:      HybridModeLexical,
	}
}

// searchPartial searches for each word in the query individually and merges
// results.
//
// Since #73(b) made the relaxed rung above actually relax, this rung is a
// safety net rather than the workhorse it used to be: an OR of prefix terms
// subsumes a union of single-term searches, so level 2 is now reached only when
// the relaxed rung fails outright. It is kept because "never zero results" is
// the contract, and a rung that costs one query per word is cheap insurance.
func (hs *HybridSearcher) searchPartial(ctx context.Context, query string, opts HybridSearchOptions) *HybridSearchResult {
	// Collect unique results from searching each significant word
	seen := make(map[string]bool)
	var allHits []SearchHit

	// Issue #96: scaffolding words are skipped here for the same reason as on
	// the relaxed rung -- a single-word search for "how" returns noise.
	for _, tk := range contentFTSTokens(splitFTSQuery(query)) {
		clean := tk.text
		if utf8.RuneCountInString(clean) < 3 {
			continue // Skip short words
		}

		ftsOpts := SearchOptions{
			Limit:     5, // Small limit per word
			SourceIDs: opts.Filter.SourceIDs,
			MimeTypes: opts.Filter.MimeTypes,
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

	// Sort by score and limit.
	//
	// Issue #73(a): this sorted DESCENDING. These are raw SQLite bm25() scores,
	// where more negative is a better match -- searcher.go orders by `score ASC`
	// for exactly that reason -- so a descending sort put the worst match at
	// rank 1 on every query that reached this rung.
	sort.SliceStable(allHits, func(i, j int) bool {
		return allHits[i].Score < allHits[j].Score
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
