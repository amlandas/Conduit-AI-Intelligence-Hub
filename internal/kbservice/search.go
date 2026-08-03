package kbservice

import (
	"context"
	"fmt"

	"github.com/simpleflo/conduit/internal/kb"
)

// Search modes accepted by SearchRequest.Mode.
const (
	SearchModeHybrid   = "hybrid"
	SearchModeSemantic = "semantic"
	SearchModeFTS5     = "fts5"
)

// Unset is the sentinel for "the caller did not supply this tuning value, use
// the configured default". It matches the CLI's -1 flag defaults, and it
// matters: 0.0 is a meaningful min-score (no filtering), so a plain zero value
// cannot mean "unset".
const Unset = -1.0

// SearchRequest describes one knowledge base query.
//
// The zero value is not useful; build it with NewSearchRequest so the Unset
// sentinels are correct.
type SearchRequest struct {
	Query string

	// Mode is "hybrid" (default), "semantic" or "fts5".
	Mode string

	// Raw skips result processing (chunk merging, boilerplate filtering).
	Raw bool

	// Limit caps results. 0 means use the configured default.
	Limit int

	// Offset paginates. Honoured by semantic and fts5 modes.
	Offset int

	// SourceID filters to a single source when non-empty.
	SourceID string

	// MinScore, SemanticWeight and MMRLambda override RAG config when >= 0.
	// Values outside [0,1] are ignored, matching the removed HTTP layer.
	MinScore       float64
	SemanticWeight float64
	MMRLambda      float64

	// DisableMMR and DisableRerank force the corresponding stage off.
	DisableMMR    bool
	DisableRerank bool
}

// NewSearchRequest returns a request with tuning values marked unset.
func NewSearchRequest(query string) SearchRequest {
	return SearchRequest{
		Query:          query,
		Mode:           SearchModeHybrid,
		MinScore:       Unset,
		SemanticWeight: Unset,
		MMRLambda:      Unset,
	}
}

// Search runs a knowledge base query and returns the response in the shape
// callers already parse.
//
// The returned map is a compatibility contract reproduced from the removed
// daemon's GET /api/v1/kb/search. Keys, nesting and types are identical; do
// not "clean it up" without versioning the consumers.
func (s *Service) Search(ctx context.Context, req SearchRequest) (map[string]interface{}, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	switch req.Mode {
	case SearchModeSemantic:
		if s.semantic == nil {
			return nil, ErrSemanticUnavailable
		}
		result, err := s.semantic.Search(ctx, req.Query, s.semanticOpts(req))
		if err != nil {
			return nil, fmt.Errorf("semantic search failed: %w", err)
		}
		if req.Raw {
			return convertSemanticResult(result, SearchModeSemantic), nil
		}
		return processSemanticResult(result, SearchModeSemantic), nil

	case SearchModeFTS5:
		result, err := s.searcher.Search(ctx, req.Query, s.ftsOpts(req))
		if err != nil {
			return nil, fmt.Errorf("fts5 search failed: %w", err)
		}
		if req.Raw {
			return map[string]interface{}{
				"results":     result.Results,
				"total_hits":  result.TotalHits,
				"query":       result.Query,
				"search_time": result.SearchTime,
				"search_mode": SearchModeFTS5,
				"processed":   false,
			}, nil
		}
		return processFTS5Result(result, SearchModeFTS5), nil

	default:
		// Hybrid: RRF fusion over lexical and (when available) semantic.
		result, err := s.hybrid.Search(ctx, req.Query, s.hybridOpts(req))
		if err != nil {
			return nil, fmt.Errorf("hybrid search failed: %w", err)
		}
		if req.Raw {
			return map[string]interface{}{
				"results":        result.Results,
				"total_hits":     result.TotalHits,
				"query":          result.Query,
				"search_time":    result.SearchTime,
				"search_mode":    string(result.Mode),
				"fts_hits":       result.FTSHits,
				"semantic_hits":  result.SemanticHits,
				"query_analysis": result.QueryAnalysis,
				"processed":      false,
			}, nil
		}
		return processHybridResult(result), nil
	}
}

// hybridOpts builds hybrid options from RAG config plus request overrides.
func (s *Service) hybridOpts(req SearchRequest) kb.HybridSearchOptions {
	ragCfg := s.cfg.KB.RAG

	opts := kb.HybridSearchOptions{
		Limit:           ragCfg.DefaultLimit,
		Mode:            kb.HybridModeAuto,
		SemanticWeight:  ragCfg.SemanticWeight,
		RRFConstant:     60,
		EnableMMR:       ragCfg.EnableMMR,
		MMRLambda:       ragCfg.MMRLambda,
		SimilarityFloor: ragCfg.MinScore,
		EnableRerank:    ragCfg.EnableRerank,
	}

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	// A SimilarityFloor of 0 is valid (no filtering); only a negative is wrong.
	if opts.SimilarityFloor < 0 {
		opts.SimilarityFloor = 0.0
	}

	if req.Limit > 0 {
		opts.Limit = req.Limit
	}
	if inUnitRange(req.MinScore) {
		opts.SimilarityFloor = req.MinScore
	}
	if inUnitRange(req.SemanticWeight) {
		opts.SemanticWeight = req.SemanticWeight
	}
	if inUnitRange(req.MMRLambda) {
		opts.MMRLambda = req.MMRLambda
	}
	if req.DisableMMR {
		opts.EnableMMR = false
	}
	if req.DisableRerank {
		opts.EnableRerank = false
	}

	return opts
}

// semanticOpts builds semantic options from RAG config plus request overrides.
func (s *Service) semanticOpts(req SearchRequest) kb.SemanticSearchOptions {
	ragCfg := s.cfg.KB.RAG

	opts := kb.SemanticSearchOptions{
		Limit:      ragCfg.DefaultLimit,
		MinScore:   ragCfg.MinScore,
		ContextLen: 300,
	}

	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.MinScore < 0 {
		opts.MinScore = 0.0
	}

	if req.Limit > 0 {
		opts.Limit = req.Limit
	}
	if req.Offset > 0 {
		opts.Offset = req.Offset
	}
	if req.SourceID != "" {
		opts.SourceIDs = []string{req.SourceID}
	}
	if inUnitRange(req.MinScore) {
		opts.MinScore = req.MinScore
	}

	return opts
}

// ftsOpts builds lexical options from request overrides.
func (s *Service) ftsOpts(req SearchRequest) kb.SearchOptions {
	opts := kb.SearchOptions{
		Limit:     10,
		Highlight: true,
	}
	if req.Limit > 0 {
		opts.Limit = req.Limit
	}
	if req.Offset > 0 {
		opts.Offset = req.Offset
	}
	if req.SourceID != "" {
		opts.SourceIDs = []string{req.SourceID}
	}
	return opts
}

// inUnitRange reports whether v is a supplied override in [0,1].
func inUnitRange(v float64) bool { return v >= 0 && v <= 1 }

// processHybridResult merges chunks and filters boilerplate for hybrid results.
func processHybridResult(result *kb.HybridSearchResult) map[string]interface{} {
	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(result.Results)

	return map[string]interface{}{
		"results":        processed,
		"total_hits":     result.TotalHits,
		"query":          result.Query,
		"search_time":    result.SearchTime,
		"search_mode":    string(result.Mode),
		"fts_hits":       result.FTSHits,
		"semantic_hits":  result.SemanticHits,
		"query_analysis": result.QueryAnalysis,
		"processed":      true,
	}
}

// convertSemanticResult renders raw semantic hits in the common response shape.
func convertSemanticResult(result *kb.SemanticSearchResult, mode string) map[string]interface{} {
	results := make([]map[string]interface{}, len(result.Results))
	for i, hit := range result.Results {
		results[i] = map[string]interface{}{
			"document_id": hit.DocumentID,
			"chunk_id":    hit.ChunkID,
			"path":        hit.Path,
			"title":       hit.Title,
			"snippet":     hit.Snippet,
			"score":       hit.Score,
			"confidence":  hit.Confidence,
			"metadata":    hit.Metadata,
		}
	}

	return map[string]interface{}{
		"results":     results,
		"total_hits":  result.TotalHits,
		"query":       result.Query,
		"search_time": result.SearchTime,
		"search_mode": mode,
		"processed":   false,
	}
}

// processSemanticResult merges chunks and filters boilerplate for semantic hits.
func processSemanticResult(result *kb.SemanticSearchResult, mode string) map[string]interface{} {
	hits := make([]kb.SearchHit, len(result.Results))
	for i, r := range result.Results {
		hits[i] = kb.SearchHit{
			DocumentID: r.DocumentID,
			ChunkID:    r.ChunkID,
			Path:       r.Path,
			Title:      r.Title,
			Snippet:    r.Snippet,
			Score:      r.Score,
			Metadata:   r.Metadata,
		}
	}

	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(hits)

	results := make([]map[string]interface{}, len(processed))
	for i, p := range processed {
		results[i] = map[string]interface{}{
			"document_id": p.DocumentID,
			"path":        p.Path,
			"title":       p.Title,
			"content":     p.Content,
			"score":       p.Score,
			"chunk_count": p.ChunkCount,
			"metadata":    p.Metadata,
			"source":      p.Source,
		}
	}

	return map[string]interface{}{
		"results":     results,
		"total_hits":  result.TotalHits,
		"query":       result.Query,
		"search_time": result.SearchTime,
		"search_mode": mode,
		"processed":   true,
	}
}

// processFTS5Result merges chunks and filters boilerplate for lexical hits.
func processFTS5Result(result *kb.SearchResult, mode string) map[string]interface{} {
	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(result.Results)

	results := make([]map[string]interface{}, len(processed))
	for i, p := range processed {
		results[i] = map[string]interface{}{
			"document_id": p.DocumentID,
			"path":        p.Path,
			"title":       p.Title,
			"content":     p.Content,
			"score":       p.Score,
			"chunk_count": p.ChunkCount,
			"metadata":    p.Metadata,
			"source":      p.Source,
		}
	}

	return map[string]interface{}{
		"results":     results,
		"total_hits":  result.TotalHits,
		"query":       result.Query,
		"search_time": result.SearchTime,
		"search_mode": mode,
		"processed":   true,
	}
}
