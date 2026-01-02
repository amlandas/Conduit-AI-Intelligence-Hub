package kb

import "strings"

// RetrievalTestCase defines a test case for retrieval quality validation.
type RetrievalTestCase struct {
	Name            string   // Test case name
	Query           string   // Search query
	ExpectedDocIDs  []string // Expected document IDs in results (any order)
	ExpectedTopDoc  string   // Expected document ID at rank 1 (optional)
	MustContain     []string // Phrases that must appear in top results
	MustNotContain  []string // Phrases that should NOT appear (boilerplate)
	MinResults      int      // Minimum expected results
	MaxRank         int      // Maximum acceptable rank for expected doc (optional)
	Description     string   // What this test validates
}

// GetRetrievalTestSuite returns the standard test suite for retrieval quality.
// These tests can be used to validate search quality before/after changes.
func GetRetrievalTestSuite() []RetrievalTestCase {
	return []RetrievalTestCase{
		// Exact phrase matching tests
		{
			Name:           "exact_phrase_oak_ridge",
			Query:          "huge laboratories like Oak Ridge",
			ExpectedTopDoc: "Weinberg_Big_Science.pdf",
			MustContain:    []string{"Oak Ridge", "Big Science", "laboratories"},
			MinResults:     1,
			MaxRank:        3,
			Description:    "Exact phrase from document should rank highly",
		},
		{
			Name:           "proper_noun_detection",
			Query:          "Oak Ridge laboratories",
			ExpectedTopDoc: "Weinberg_Big_Science.pdf",
			MustContain:    []string{"Oak Ridge"},
			MinResults:     1,
			MaxRank:        1,
			Description:    "Proper noun should trigger exact matching boost",
		},

		// Semantic understanding tests
		{
			Name:           "semantic_revenue",
			Query:          "revenue growth",
			ExpectedTopDoc: "Alphabet_10K-Report.pdf",
			MustContain:    []string{"revenue"},
			MinResults:     1,
			Description:    "Semantic query should find financial document",
		},
		{
			Name:           "semantic_sustainability",
			Query:          "environmental sustainability initiatives",
			ExpectedTopDoc: "GA-Sustainability-Report.pdf",
			MustContain:    []string{"sustainability"},
			MinResults:     1,
			Description:    "Semantic query should match topic",
		},

		// Boilerplate filtering tests
		{
			Name:           "no_boilerplate_jstor",
			Query:          "scientific research",
			MustNotContain: []string{"downloaded from", "129.177.32.58", "UTC"},
			MinResults:     1,
			Description:    "JSTOR metadata should be filtered out",
		},
		{
			Name:           "no_boilerplate_copyright",
			Query:          "annual report",
			MustNotContain: []string{"All rights reserved", "Copyright ©"},
			MinResults:     1,
			Description:    "Copyright notices should be filtered out",
		},

		// Hybrid mode effectiveness tests
		{
			Name:           "hybrid_beats_semantic",
			Query:          "Big Science",
			ExpectedTopDoc: "Weinberg_Big_Science.pdf",
			MustContain:    []string{"Big Science"},
			MinResults:     1,
			MaxRank:        1,
			Description:    "Title match should boost ranking (hybrid advantage)",
		},

		// Edge cases
		{
			Name:        "quoted_phrase",
			Query:       `"Big Science"`,
			MustContain: []string{"Big Science"},
			MinResults:  1,
			Description: "Quoted phrase should trigger exact matching",
		},
		{
			Name:        "short_query",
			Query:       "science",
			MinResults:  1,
			Description: "Short queries should still return results",
		},
	}
}

// RetrievalMetrics holds quality metrics for retrieval evaluation.
type RetrievalMetrics struct {
	Precision    float64 // Precision@k: relevant results / total results
	Recall       float64 // Recall@k: found relevant / total relevant
	MRR          float64 // Mean Reciprocal Rank
	NDCG         float64 // Normalized Discounted Cumulative Gain
	TestsPassed  int     // Number of test cases passed
	TestsFailed  int     // Number of test cases failed
	FailedCases  []string // Names of failed test cases
}

// EvaluateTestCase checks if a search result passes a test case.
func EvaluateTestCase(tc RetrievalTestCase, results []SearchHit) (passed bool, reason string) {
	// Check minimum results
	if len(results) < tc.MinResults {
		return false, "insufficient results"
	}

	// Check expected top document
	if tc.ExpectedTopDoc != "" && tc.MaxRank > 0 {
		found := false
		for i, result := range results {
			if i >= tc.MaxRank {
				break
			}
			if pathContainsDoc(result.Path, tc.ExpectedTopDoc) {
				found = true
				break
			}
		}
		if !found {
			return false, "expected document not in top results"
		}
	}

	// Check must contain phrases
	for _, phrase := range tc.MustContain {
		found := false
		for _, result := range results {
			if snippetContainsIgnoreCase(result.Snippet, phrase) {
				found = true
				break
			}
		}
		if !found {
			return false, "missing required phrase: " + phrase
		}
	}

	// Check must not contain phrases (boilerplate)
	for _, phrase := range tc.MustNotContain {
		for _, result := range results {
			if snippetContainsIgnoreCase(result.Snippet, phrase) {
				return false, "found boilerplate phrase: " + phrase
			}
		}
	}

	return true, ""
}

// pathContainsDoc checks if a path contains a document name.
func pathContainsDoc(s, substr string) bool {
	return strings.Contains(s, substr)
}

// snippetContainsIgnoreCase checks if text contains a phrase (case insensitive).
func snippetContainsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
