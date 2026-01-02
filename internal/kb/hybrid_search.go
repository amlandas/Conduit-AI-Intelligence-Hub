package kb

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
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
}

// HybridSearchResult contains combined search results with metadata.
type HybridSearchResult struct {
	Results        []SearchHit       `json:"results"`
	TotalHits      int               `json:"total_hits"`
	Query          string            `json:"query"`
	SearchTime     float64           `json:"search_time_ms"`
	Mode           HybridSearchMode  `json:"mode"`
	FTSHits        int               `json:"fts_hits"`
	SemanticHits   int               `json:"semantic_hits"`
	QueryAnalysis  QueryAnalysis     `json:"query_analysis,omitempty"`
}

// QueryAnalysis provides insight into how the query was interpreted.
type QueryAnalysis struct {
	HasQuotedPhrase bool     `json:"has_quoted_phrase,omitempty"`
	ProperNouns     []string `json:"proper_nouns,omitempty"`
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

	// Detect proper nouns (capitalized multi-word sequences)
	// This helps identify named entities that need exact matching
	words := strings.Fields(query)
	var currentProperNoun []string

	for _, word := range words {
		// Clean punctuation
		cleanWord := strings.Trim(word, `"'.,;:!?()[]{}`)
		if cleanWord == "" {
			continue
		}

		// Check if word starts with uppercase (and isn't first word or common word)
		firstRune := []rune(cleanWord)[0]
		if unicode.IsUpper(firstRune) && len(cleanWord) > 1 {
			currentProperNoun = append(currentProperNoun, cleanWord)
		} else {
			if len(currentProperNoun) >= 2 {
				analysis.ProperNouns = append(analysis.ProperNouns, strings.Join(currentProperNoun, " "))
			}
			currentProperNoun = nil
		}
	}

	// Don't forget the last proper noun sequence
	if len(currentProperNoun) >= 2 {
		analysis.ProperNouns = append(analysis.ProperNouns, strings.Join(currentProperNoun, " "))
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

	// Boost exact matches for proper nouns
	if opts.BoostExactMatch && len(analysis.ProperNouns) > 0 {
		fused = hs.boostExactMatches(fused, analysis.ProperNouns)
	}

	// Limit results
	if len(fused) > opts.Limit {
		fused = fused[:opts.Limit]
	}

	return &HybridSearchResult{
		Results:      fused,
		TotalHits:    len(fused),
		FTSHits:      len(ftsHits),
		SemanticHits: len(semanticHits),
	}
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

// boostExactMatches increases the score of results containing exact proper noun matches.
func (hs *HybridSearcher) boostExactMatches(hits []SearchHit, properNouns []string) []SearchHit {
	boostFactor := 1.5 // 50% boost for exact matches

	for i := range hits {
		content := strings.ToLower(hits[i].Snippet + " " + hits[i].Title)
		for _, pn := range properNouns {
			if strings.Contains(content, strings.ToLower(pn)) {
				hits[i].Score *= boostFactor
				break // Only boost once per hit
			}
		}
	}

	// Re-sort after boosting
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})

	return hits
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
