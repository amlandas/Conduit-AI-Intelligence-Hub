package kb

import (
	"context"
	"math"
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
}

// QueryAnalysis provides insight into how the query was interpreted.
type QueryAnalysis struct {
	HasQuotedPhrase bool     `json:"has_quoted_phrase,omitempty"`
	ProperNouns     []string `json:"proper_nouns,omitempty"`     // Multi-word proper nouns (e.g., "Oak Ridge")
	Entities        []string `json:"entities,omitempty"`         // All detected entities (single + multi-word)
	SuggestedMode   string   `json:"suggested_mode,omitempty"`
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

	return analysis
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
	}

	// Apply RRF fusion
	fused := hs.applyRRF(ftsHits, semanticHits, opts.RRFConstant, opts.SemanticWeight)

	// Boost exact matches for entities (both single-word and multi-word)
	if opts.BoostExactMatch && len(analysis.Entities) > 0 {
		fused = hs.boostExactMatches(fused, analysis.Entities)
	}

	result := &HybridSearchResult{
		FTSHits:      len(ftsHits),
		SemanticHits: len(semanticHits),
	}

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

	return result
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
