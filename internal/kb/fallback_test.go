package kb

// Tests for the "never zero results" fallback ladder in SearchWithFallback.
//
// The ladder has four rungs:
//
//	level 0 -- ordinary hybrid search succeeded
//	level 1 -- searchRelaxed: every word joined with OR, each word suffixed
//	           with '*' (but see #73: the sanitizer removes all but the last
//	           wildcard before the query reaches FTS5)
//	level 2 -- searchPartial: each word searched on its own, results merged
//	level 3 -- nothing found; empty result with an advisory note
//
// Reaching a specific rung takes carefully chosen queries; the comments on each
// case explain why that query lands where it does.

import (
	"context"
	"testing"
)

func TestFallbackLadder(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		query          string
		wantLevel      int
		wantDocs       []string
		wantConfidence string
		wantNote       string
		why            string
	}{
		{
			name:           "level 0: primary hybrid search finds results",
			query:          "lantern",
			wantLevel:      0,
			wantDocs:       []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"},
			wantConfidence: "medium",
			wantNote:       "",
			why:            "an ordinary term that matches; the ladder is never entered",
		},
		{
			name:           "level 0: a prefix also satisfies the primary search",
			query:          "lanter",
			wantLevel:      0,
			wantDocs:       []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"},
			wantConfidence: "medium",
			why:            "prepareFTSQuery appends '*' to a single-term query, so prefixes never need the ladder",
		},
		{
			name:      "level 1: relaxed OR matching rescues a query with one bad word",
			query:     "lantern zzzznothing",
			wantLevel: 1,
			// Raw bm25 scores, correctly ordered (most negative first).
			wantDocs:       []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"},
			wantConfidence: "low",
			wantNote:       "Using relaxed matching - verify relevance",
			why:            "primary ANDs the terms and finds nothing; relaxed ORs them and 'lantern' still matches",
		},
		{
			name:      "level 2: partial word matching rescues a query relaxed matching cannot",
			query:     "lanter harbou zzzz",
			wantLevel: 2,
			// KNOWN BUG #73: this ordering is backwards. -1.41 is a WORSE bm25
			// score than -2.99, yet it is returned first.
			wantDocs:       []string{"07-fixture-harbour-ledger", "06-fixture-lantern-keeper"},
			wantConfidence: "speculative",
			wantNote:       "Partial word matching - results may not fully match query",
			why: "relaxed builds 'lanter* OR harbou* OR zzzz*', the sanitizer strips every wildcard except " +
				"the last, so it becomes 'lanter OR harbou OR zzzz*' and matches nothing; partial then searches " +
				"each word alone, where the single-term path re-adds the wildcard and 'lanter*' matches",
		},
		{
			name:           "level 3: nothing matches at any rung",
			query:          "zzzznothing",
			wantLevel:      3,
			wantDocs:       nil,
			wantConfidence: "none",
			wantNote:       "No matching documents found. Try different search terms or verify documents are indexed.",
			why:            "no rung can match a term that appears nowhere in the corpus",
		},
		{
			name:           "level 3: every word is too short for any rung",
			query:          "zz",
			wantLevel:      3,
			wantDocs:       nil,
			wantConfidence: "none",
			wantNote:       "No matching documents found. Try different search terms or verify documents are indexed.",
			why:            "relaxed skips words under 2 characters and partial skips words under 3, so a 2-character miss dead-ends",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := gi.Hybrid.SearchWithFallback(ctx, tt.query, HybridSearchOptions{Limit: 5})
			if err != nil {
				t.Fatalf("SearchWithFallback(%q): %v", tt.query, err)
			}
			if res.FallbackLevel != tt.wantLevel {
				t.Errorf("fallback level for %q: got %d, want %d\nwhy: %s", tt.query, res.FallbackLevel, tt.wantLevel, tt.why)
			}
			if got := docIDs(res.Results); !equalStrings(got, tt.wantDocs) {
				t.Errorf("results for %q:\n got %v\nwant %v", tt.query, got, tt.wantDocs)
			}
			if res.Confidence != tt.wantConfidence {
				t.Errorf("confidence for %q: got %q, want %q", tt.query, res.Confidence, tt.wantConfidence)
			}
			if res.Note != tt.wantNote {
				t.Errorf("note for %q: got %q, want %q", tt.query, res.Note, tt.wantNote)
			}
			if res.TotalHits != len(res.Results) {
				t.Errorf("TotalHits (%d) disagrees with len(Results) (%d)", res.TotalHits, len(res.Results))
			}
		})
	}
}

// TestFallback_RelaxedRungInIsolation pins searchRelaxed on its own so that a
// change to the rung is visible even if the ladder stops reaching it.
func TestFallback_RelaxedRungInIsolation(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		query    string
		wantDocs []string
		notes    string
	}{
		{
			name:     "the last word keeps its wildcard, so prefix matching works for it alone",
			query:    "lanter ledg",
			wantDocs: []string{"07-fixture-harbour-ledger", "06-fixture-lantern-keeper"},
			notes:    "'ledg*' matches 'ledger' in both documents; 'lanter' matches nothing because its wildcard was stripped",
		},
		{
			name:     "a prefix that is not the last word matches nothing",
			query:    "lanter harbou zzzz",
			wantDocs: nil,
			notes:    "KNOWN BUG #73: every wildcard except the last is removed by sanitizeFTSQuery",
		},
		{
			name:     "relaxed results keep raw bm25 ordering (most negative first) -- this rung is correct",
			query:    "lantern ledger",
			wantDocs: []string{"07-fixture-harbour-ledger", "06-fixture-lantern-keeper"},
			notes: "the relaxed query is 'lantern OR ledger*', not 'lantern', so the ranking differs from a " +
				"plain lantern search; what matters here is that the ordering is ascending bm25, which is correct",
		},
		{
			name:     "a query with no word of two or more characters returns an empty result",
			query:    "a b",
			wantDocs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := gi.Hybrid.searchRelaxed(ctx, tt.query, HybridSearchOptions{Limit: 10})
			if got := docIDs(res.Results); !equalStrings(got, tt.wantDocs) {
				t.Errorf("searchRelaxed(%q):\n got %v\nwant %v\n%s", tt.query, got, tt.wantDocs, tt.notes)
			}
			for i := 1; i < len(res.Results); i++ {
				if res.Results[i].Score < res.Results[i-1].Score {
					t.Errorf("relaxed rung is no longer in bm25 order at rank %d", i)
				}
			}
		})
	}
}

// TestFallback_PartialRungInIsolation pins searchPartial on its own, including
// the inverted ordering tracked as #73.
func TestFallback_PartialRungInIsolation(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	t.Run("each word is searched separately and results are merged by chunk id", func(t *testing.T) {
		res := gi.Hybrid.searchPartial(ctx, "lantern ledger keeper", HybridSearchOptions{Limit: 10})
		if got := sortedCopy(uniqueDocIDs(res.Results)); !equalStrings(got, []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("words shorter than three characters are skipped", func(t *testing.T) {
		res := gi.Hybrid.searchPartial(ctx, "a of to", HybridSearchOptions{Limit: 10})
		if len(res.Results) != 0 {
			t.Errorf("expected no results, got %d", len(res.Results))
		}
	})

	t.Run("KNOWN BUG #73: results are ordered worst-first", func(t *testing.T) {
		res := gi.Hybrid.searchPartial(ctx, "lantern ledger", HybridSearchOptions{Limit: 10})
		if len(res.Results) < 2 {
			t.Fatalf("expected at least two results, got %d", len(res.Results))
		}
		if !(res.Results[0].Score > res.Results[1].Score) {
			t.Errorf("expected the descending sort over negative bm25 scores to put the worse match first; "+
				"got %.6f then %.6f", res.Results[0].Score, res.Results[1].Score)
		}
	})

	t.Run("the per-word limit is hard-coded to five", func(t *testing.T) {
		// "the" matches many chunks, but each word contributes at most 5 hits.
		res := gi.Hybrid.searchPartial(ctx, "the", HybridSearchOptions{Limit: 50})
		if len(res.Results) > 5 {
			t.Errorf("searchPartial returned %d hits for a single word; the per-word limit of 5 was removed", len(res.Results))
		}
	})
}

// TestFallback_LimitIsRespected pins that every rung honours the caller's limit.
func TestFallback_LimitIsRespected(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	for _, q := range []string{"lantern", "lantern zzzznothing", "lanter harbou zzzz"} {
		res, err := gi.Hybrid.SearchWithFallback(ctx, q, HybridSearchOptions{Limit: 1})
		if err != nil {
			t.Fatalf("SearchWithFallback(%q): %v", q, err)
		}
		if len(res.Results) > 1 {
			t.Errorf("query %q (level %d) returned %d results for Limit 1", q, res.FallbackLevel, len(res.Results))
		}
	}
}
