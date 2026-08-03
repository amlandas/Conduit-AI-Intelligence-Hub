package kb

// Regression tests for the tracked retrieval defects, GitHub issues #69 to #77.
//
// HOW TO USE THIS FILE
//
// WP-1.1 wrote these as CHARACTERIZATION tests: each asserted the broken
// behaviour and was gated on a `bugNNFixed` constant so that WP-3.4 could flip
// one constant per fix and see exactly what moved. Every one of those constants
// is now gone -- each test asserts the CORRECT behaviour and carries a "Was:"
// paragraph recording the defect it guards against, so the mechanism survives
// even though the broken branch does not.
//
// Do not delete a test here. If one starts failing, the corresponding issue has
// regressed.

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/embed"
)

// ISSUE #69 -- FIXED in WP-3.4 by deletion. Regression test.
//
// Was: strategyWeightMatrix mapped each query type to a semantic/lexical
// weighting, and searchFusion read it -- then unconditionally overrode it from
// opts.SemanticWeight, which Search had already defaulted to 0.5. The override
// branch could never be skipped, so every query fused 50/50 whatever the
// classifier decided. The matrix was dead code that looked live.
//
// Now: fusion is unweighted RRF with a configurable k. The test reads the
// weighting back off the score arithmetically. For an entity query matching one
// document through the lexical side only,
//
//	score(rank 1) = 1/(k+1) * entityBoost * agreementBoost
//	              = 1/61     * 1.2         * 1.1
//
// with no weight factor anywhere in it.
func TestIssue69_FusionIsUnweightedRRF(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	const (
		rrfRank1       = 1.0 / 61.0 // DefaultRRFConstant 60, rank 1
		entityBoost    = 1.2        // single-word entity present in the hit
		agreementBoost = 1.1        // one of two possible strategies contributed
	)

	// Every query type produces the same unweighted contribution, because there
	// is no longer a weight to differ.
	for _, q := range []string{
		"Wonderland", // entity
		"Ishmael",    // entity
		"Gettysburg", // entity
		"Liberty",    // entity
	} {
		t.Run(q, func(t *testing.T) {
			r, err := gi.Hybrid.Search(ctx, q, HybridSearchOptions{Limit: 1})
			if err != nil || len(r.Results) == 0 {
				t.Fatalf("Search(%q): %v (%d results)", q, err, len(r.Results))
			}
			got := r.Results[0].Score / (rrfRank1 * entityBoost * agreementBoost)
			if math.Abs(got-1.0) > 1e-9 {
				t.Errorf("query %q: observed a fusion weight of %.6f, want 1.0 (unweighted)", q, got)
			}
		})
	}

	// The classifier still runs and still drives mode selection, which is the
	// one decision the engine acts on. Deleting the weights did not delete that.
	if got := gi.Hybrid.analyzeQuery("Wonderland").QueryType; got != QueryTypeEntity {
		t.Errorf("query classification stopped working: got %s", got)
	}
	if got := gi.Hybrid.selectMode(gi.Hybrid.analyzeQuery(`"big science"`)); got != HybridModeLexical {
		t.Errorf("classification no longer drives mode selection: got %s", got)
	}

	// k is configurable, at the searcher rather than per call.
	tuned := NewHybridSearcher(gi.Searcher, nil, WithRRFConstant(10))
	r, err := tuned.Search(ctx, "Wonderland", HybridSearchOptions{Limit: 1})
	if err != nil || len(r.Results) == 0 {
		t.Fatalf("Search with k=10: %v (%d results)", err, len(r.Results))
	}
	wantScore := (1.0 / 11.0) * entityBoost * agreementBoost
	if math.Abs(r.Results[0].Score-wantScore) > 1e-9 {
		t.Errorf("k=10 score = %.9f, want %.9f", r.Results[0].Score, wantScore)
	}
}

// ISSUE #70 -- FIXED in WP-3.4. Regression test.
//
// Was: analyzeQuery set HasQuotedPhrase when the query contained EITHER a
// double quote or a single quote. classifyQueryType then returned
// QueryTypeExactQuote and selectMode returned HybridModeLexical, so "don't",
// "Alice's", "O'Brien" and every possessive silently disabled the vector half
// of hybrid search.
//
// Now: only a balanced pair of double quotes marks a phrase. FTS5 agrees --
// its phrase delimiter is '"', never '\''.
func TestIssue70_ApostropheIsNotAQuotedPhrase(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	apostropheQueries := []string{
		"summer's day",
		"don't index this",
		"O'Brien",
		"the keeper's ledger",
		`unbalanced " quote`,
	}

	for _, q := range apostropheQueries {
		t.Run(q, func(t *testing.T) {
			a := gi.Hybrid.analyzeQuery(q)
			if a.HasQuotedPhrase {
				t.Errorf("query %q should not be treated as a quoted phrase", q)
			}
			if a.QueryType == QueryTypeExactQuote {
				t.Errorf("query %q should not be classified as an exact quote", q)
			}
			if got := gi.Hybrid.selectMode(a); got == HybridModeLexical {
				t.Errorf("query %q should not be forced into lexical-only mode", q)
			}
		})
	}

	// The control: a real double-quoted phrase still means exact-match intent.
	for _, q := range []string{`"big science"`, `find "big science" now`} {
		a := gi.Hybrid.analyzeQuery(q)
		if !a.HasQuotedPhrase {
			t.Errorf("query %q must be recognised as a quoted phrase", q)
		}
		if a.QueryType != QueryTypeExactQuote {
			t.Errorf("query %q should classify as an exact quote, got %s", q, a.QueryType)
		}
		if gi.Hybrid.selectMode(a) != HybridModeLexical {
			t.Errorf("a double-quoted query must still select lexical mode")
		}
	}

	// End to end: the apostrophe no longer changes the shape of the response.
	withApostrophe, err := gi.Hybrid.Search(ctx, "summer's day", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	withoutApostrophe, err := gi.Hybrid.Search(ctx, "summers day", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if withApostrophe.Mode != withoutApostrophe.Mode {
		t.Errorf("apostrophe still changes the search mode: %s vs %s", withApostrophe.Mode, withoutApostrophe.Mode)
	}
	if withApostrophe.Mode != HybridModeFusion {
		t.Errorf("expected fusion mode for %q, got %s", "summer's day", withApostrophe.Mode)
	}
	// And the apostrophe query still finds the sonnet: quoting keeps "summer's"
	// one term instead of splitting it into `summer` AND `s`.
	if len(withApostrophe.Results) == 0 || withApostrophe.Results[0].DocumentID != "05-sonnet-18" {
		t.Errorf("summer's day: got %v, want 05-sonnet-18 at rank 1", docIDs(withApostrophe.Results))
	}
}

// ISSUE #71 -- FIXED in WP-3.4. Regression test.
//
// Was: internal/kb/embeddings.go built its Ollama client as
// `api.NewClient(ollamaURL, http.DefaultClient)`. http.DefaultClient has
// Timeout == 0, i.e. no timeout at all, and every embedding request inherited
// that. The only thing that could unblock a call was the caller's context --
// and the ingestion path passed contexts with no deadline, so a hung Ollama
// held an indexing or search goroutine until the process died.
//
// Now: embeddings.go is deleted. The live path is kb.ProviderEmbedder over an
// internal/embed provider, and everything in that package is bounded by an
// http.Client.Timeout as well as the context, by construction.
//
// The proof is hermetic: a local httptest server that accepts the connection
// and never answers, plus a bounded wait in the test so CI can never hang.
func TestIssue71_EmbeddingCallsHaveADeadline(t *testing.T) {
	// The premise: the shared default client still has no timeout, which is why
	// nothing may be built on it.
	if http.DefaultClient.Timeout != 0 {
		t.Fatalf("something in this binary mutated http.DefaultClient (timeout %v); it must not",
			http.DefaultClient.Timeout)
	}

	// release lets the handler finish so the server can shut down cleanly.
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	// LIFO: unblock the handler first, then close the server. httptest.Close
	// waits for outstanding requests, so the order matters.
	defer srv.Close()
	defer releaseHandler()

	// The live wiring, with the timeout dialled down so the test is quick. A
	// production embedder is built exactly this way -- see
	// kbservice.newEmbedder -- with embed.DefaultTimeout instead.
	provider, err := embed.NewOllamaProvider(embed.OllamaConfig{
		Host:       srv.URL,
		Model:      DefaultEmbeddingModel,
		Dimensions: DefaultEmbeddingDimension,
		Timeout:    500 * time.Millisecond,
		Retry:      embed.RetryPolicy{MaxAttempts: 1},
	})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	defer provider.Close()

	svc := NewProviderEmbedder(provider)

	done := make(chan error, 1)
	go func() {
		// context.Background() deliberately: this mirrors the call sites that
		// have no deadline of their own, which is where a missing client
		// timeout actually bites.
		_, err := svc.Embed(context.Background(), "does the client give up on its own?")
		done <- err
	}()

	// A generous-but-bounded wait. Anything under a few seconds distinguishes
	// "no timeout" from "any sane timeout"; keeping it short keeps CI fast.
	const blockedFor = 5 * time.Second

	select {
	case err := <-done:
		releaseHandler()
		if err == nil {
			t.Errorf("expected a timeout error from the embedding client against a server " +
				"that never responds, got nil")
		}
	case <-time.After(blockedFor):
		releaseHandler()
		t.Errorf("the embedding call is still blocked after %v with no deadline of its own; "+
			"issue #71 has regressed", blockedFor)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("the embedding call did not finish even after the server responded")
		}
	}

	// The static half: no Embedder in the tree may be built on the untimed
	// default client. ProviderEmbedder cannot be -- it only wraps
	// embed.Provider, and that package never uses http.DefaultClient.
	var _ Embedder = (*ProviderEmbedder)(nil)
}

// ISSUE #72 -- FIXED in WP-3.4. Regression test.
//
// Was: Chunker.chunkID(content, index) hashed the content alone, so two
// documents sharing a paragraph verbatim got one chunk id for it, and the index
// parameter was accepted and discarded. Indexer.Index papered over this by
// overwriting every id with its OWN generator, generateUniqueChunkID -- two
// functions that could disagree about the identity of the same chunk.
//
// Now: kb.ChunkID(documentID, index, content) is the only chunk-id function in
// the package; the chunker and the indexer both call it.
func TestIssue72_ChunkIDsIncludeDocumentAndIndex(t *testing.T) {
	c := NewChunker()

	// Two different documents that happen to share a paragraph verbatim.
	// This is exactly the shape of the boilerplate paragraph shared by
	// 06-fixture-lantern-keeper.txt and 07-fixture-harbour-ledger.txt.
	shared := "Boilerplate notice: this paragraph is repeated verbatim in another fixture document."
	docA := c.Chunk(shared, ChunkOptions{MaxSize: 1000, DocumentID: "docA"})
	docB := c.Chunk(shared, ChunkOptions{MaxSize: 1000, DocumentID: "docB"})

	if len(docA) != 1 || len(docB) != 1 {
		t.Fatalf("fixture drifted: got %d and %d chunks, want 1 each", len(docA), len(docB))
	}

	if docA[0].ChunkID == docB[0].ChunkID {
		t.Errorf("identical content in two documents still collides: %s", docA[0].ChunkID)
	}
	if ChunkID("d", 0, "same") == ChunkID("d", 7, "same") {
		t.Errorf("ChunkID ignores its index parameter")
	}
	if ChunkID("docA", 0, "same") == ChunkID("docB", 0, "same") {
		t.Errorf("ChunkID ignores its document parameter")
	}

	// There is exactly one id function, and the indexer uses it: an indexed
	// chunk carries the id the chunker would have produced for the same
	// document, index and content.
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()
	chunks, err := gi.Indexer.GetChunks(ctx, "06-fixture-lantern-keeper")
	if err != nil {
		t.Fatalf("GetChunks: %v", err)
	}
	if len(chunks) == 0 {
		t.Fatal("expected indexed chunks")
	}
	for i, ch := range chunks {
		if want := ChunkID("06-fixture-lantern-keeper", i, ch.Content); ch.ChunkID != want {
			t.Errorf("indexed chunk %d has id %s, want %s -- the indexer is not using kb.ChunkID", i, ch.ChunkID, want)
		}
	}

	// And the corpus really does contain duplicate content across documents, so
	// the collision this fix prevents is reachable in practice.
	keeper, err := gi.Indexer.GetChunks(ctx, "07-fixture-harbour-ledger")
	if err != nil {
		t.Fatalf("GetChunks: %v", err)
	}
	for _, a := range chunks {
		for _, b := range keeper {
			if a.ChunkID == b.ChunkID {
				t.Errorf("chunk id %s is shared across two documents", a.ChunkID)
			}
		}
	}
}

// ISSUE #73 -- FIXED in WP-3.4. Regression test.
//
// Two mechanisms:
//
//	(a) searchPartial sorted `Score > Score`, i.e. DESCENDING. These are raw
//	    SQLite bm25 scores, where more negative is better -- searcher.go orders
//	    by `score ASC` for exactly that reason -- so the level-2 rung returned
//	    its worst match at rank 1.
//
//	(b) searchRelaxed built the FTS5 string `a* OR b* OR c*` and passed it
//	    through Searcher.Search, whose sanitizer deleted every '*' and then
//	    re-added exactly one, to the last term. A three-word relaxed query
//	    reached FTS5 as `a OR b OR c*`, so the "relaxed" rung was stricter than
//	    the partial rung beneath it.
func TestIssue73_FallbackLadder(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	t.Run("a_partial_results_are_sorted_best_first", func(t *testing.T) {
		// Two documents contain "lantern": the keeper fixture four times (the
		// better, more negative bm25) and the harbour ledger once.
		res := gi.Hybrid.searchPartial(ctx, "lantern", HybridSearchOptions{Limit: 10})
		if len(res.Results) != 2 {
			t.Fatalf("fixture drifted: got %d results, want 2", len(res.Results))
		}
		if res.Results[0].DocumentID != "06-fixture-lantern-keeper" {
			t.Errorf("rank 1 should be the better bm25 match; got %s", res.Results[0].DocumentID)
		}
		for i := 1; i < len(res.Results); i++ {
			if res.Results[i].Score < res.Results[i-1].Score {
				t.Errorf("results must be in ascending bm25 order; rank %d (%.6f) beats rank %d (%.6f)",
					i, res.Results[i].Score, i-1, res.Results[i-1].Score)
			}
		}
	})

	t.Run("b_relaxed_prefixes_all_survive", func(t *testing.T) {
		// Every term is prefix-matched, not just the last one, so a query whose
		// first two words are prefixes of real corpus words matches.
		res := gi.Hybrid.searchRelaxed(ctx, "lanter harbou zzzz", HybridSearchOptions{Limit: 10})
		if len(res.Results) == 0 {
			t.Fatalf("the relaxed rung must match via prefix search on every term")
		}
		got := sortedCopy(uniqueDocIDs(res.Results))
		want := []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"}
		if !equalStrings(got, want) {
			t.Errorf("relaxed results: got %v, want %v", got, want)
		}
		// Relaxed is now genuinely broader than partial, which is the whole
		// point of putting it higher on the ladder.
		partial := gi.Hybrid.searchPartial(ctx, "lanter harbou zzzz", HybridSearchOptions{Limit: 10})
		if len(res.Results) < len(partial.Results) {
			t.Errorf("the relaxed rung returned %d results, fewer than the partial rung below it (%d)",
				len(res.Results), len(partial.Results))
		}
	})

	t.Run("c_relaxed_rung_orders_bm25_ascending", func(t *testing.T) {
		res := gi.Hybrid.searchRelaxed(ctx, "lantern ledger", HybridSearchOptions{Limit: 10})
		for i := 1; i < len(res.Results); i++ {
			if res.Results[i].Score < res.Results[i-1].Score {
				t.Errorf("relaxed rung is not in ascending bm25 order at rank %d", i)
			}
		}
	})

	t.Run("control: the FTS5 layer itself orders bm25 correctly", func(t *testing.T) {
		res, err := gi.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 10})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Results) < 2 {
			t.Fatalf("expected at least two results")
		}
		if res.Results[0].Score > res.Results[1].Score {
			t.Errorf("the FTS5 SQL should return ascending bm25 scores; got %.6f then %.6f",
				res.Results[0].Score, res.Results[1].Score)
		}
		if res.Results[0].DocumentID != "06-fixture-lantern-keeper" {
			t.Errorf("FTS5 rank 1 should be the better match; got %s", res.Results[0].DocumentID)
		}
	})
}

// ISSUE #77 -- response contract. FIXED in WP-3.4.
//
// Four defects, all in what a client could believe about a result:
//
//  1. Lexical mode passed raw SQLite bm25 through the same `score` key that
//     fusion fills with a small positive RRF value. Negative-is-better versus
//     higher-is-better, under one name, with no way to tell which.
//  2. Lexical and semantic-only modes left Confidence and StrategiesUsed at
//     their zero values, so "" was both "not computed" and "no confidence".
//  3. MMR reorders for diversity as the last stage and does not touch Score,
//     so sorting by Score gave a different order than the one returned.
//  4. calculateOverallConfidence gated "very_high" on
//     `highAgreementCount >= len(hits)/2` -- integer division, so a single
//     uncorroborated hit scored 0 >= 0 and came back very_high.
func TestIssue77_ResponseContract(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	t.Run("scores are on one scale in every mode", func(t *testing.T) {
		modes := []struct {
			name  string
			query string
			mode  HybridSearchMode
		}{
			{"fusion", "lantern", HybridModeFusion},
			{"lexical", "lantern", HybridModeLexical},
		}
		for _, m := range modes {
			t.Run(m.name, func(t *testing.T) {
				res, err := gi.Hybrid.Search(ctx, m.query, HybridSearchOptions{Limit: 10, Mode: m.mode})
				if err != nil {
					t.Fatalf("Search: %v", err)
				}
				if len(res.Results) == 0 {
					t.Fatal("expected results")
				}
				for _, h := range res.Results {
					if h.Score <= 0 {
						t.Errorf("%s mode returned a non-positive score %.6f; every mode reports "+
							"reciprocal rank now", m.name, h.Score)
					}
					if h.Score > 3.0/float64(DefaultRRFConstant+1) {
						t.Errorf("%s mode score %.6f is off the reciprocal-rank scale", m.name, h.Score)
					}
				}
			})
		}

		// Rank 1 in lexical mode scores exactly what fusion gives a rank-1 hit
		// found by one strategy: the two are the same number, not merely
		// comparable.
		lex, err := gi.Hybrid.Search(ctx, "lantern", HybridSearchOptions{Limit: 10, Mode: HybridModeLexical})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		want := 1.0 / float64(DefaultRRFConstant+1)
		if math.Abs(lex.Results[0].Score-want) > 1e-12 {
			t.Errorf("lexical rank 1 = %.12f, want %.12f", lex.Results[0].Score, want)
		}
		for i := 1; i < len(lex.Results); i++ {
			if lex.Results[i].Score >= lex.Results[i-1].Score {
				t.Errorf("lexical scores are not monotone decreasing at rank %d", i+1)
			}
		}
	})

	t.Run("lexical mode reports confidence and strategy count", func(t *testing.T) {
		res, err := gi.Hybrid.Search(ctx, "lantern", HybridSearchOptions{Limit: 10, Mode: HybridModeLexical})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Confidence != "medium" {
			t.Errorf("confidence = %q, want medium (one strategy, results present)", res.Confidence)
		}
		if res.StrategiesUsed != 1 {
			t.Errorf("StrategiesUsed = %d, want 1", res.StrategiesUsed)
		}

		empty, err := gi.Hybrid.Search(ctx, "whale", HybridSearchOptions{Limit: 10, Mode: HybridModeLexical})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if empty.Confidence != "none" {
			t.Errorf("confidence for a miss = %q, want none", empty.Confidence)
		}
		if empty.StrategiesUsed != 0 {
			t.Errorf("StrategiesUsed for a miss = %d, want 0", empty.StrategiesUsed)
		}
	})

	t.Run("Rank is authoritative and MMR-aware", func(t *testing.T) {
		// "people" is the query where MMR demonstrably reorders: rank 2 scores
		// below rank 3 (see TestGolden_MMRReordersFinalRanking).
		res, err := gi.Hybrid.Search(ctx, "people", HybridSearchOptions{Limit: 10})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Results) < 3 {
			t.Fatalf("expected at least three results, got %d", len(res.Results))
		}
		for i, h := range res.Results {
			if h.Rank != i+1 {
				t.Errorf("hit at position %d has Rank %d", i, h.Rank)
			}
		}
		// The fixture must still exercise the interesting case, or this test is
		// vacuous: Score alone does not imply the returned order.
		if !(res.Results[1].Score < res.Results[2].Score) {
			t.Errorf("fixture drifted: MMR no longer reorders 'people', so Rank is not being tested " +
				"against a Score that disagrees with it")
		}
	})

	t.Run("Rank is set on every fallback rung", func(t *testing.T) {
		for _, q := range []string{"lantern", "lantern zzzznothing", "lanter harbou zzzz"} {
			res, err := gi.Hybrid.SearchWithFallback(ctx, q, HybridSearchOptions{Limit: 5})
			if err != nil {
				t.Fatalf("SearchWithFallback(%q): %v", q, err)
			}
			for i, h := range res.Results {
				if h.Rank != i+1 {
					t.Errorf("query %q (level %d): hit at position %d has Rank %d", q, res.FallbackLevel, i, h.Rank)
				}
				if h.Score <= 0 {
					t.Errorf("query %q (level %d): rank %d has non-positive score %.6f",
						q, res.FallbackLevel, h.Rank, h.Score)
				}
			}
		}
	})

	t.Run("single-hit confidence is not inflated", func(t *testing.T) {
		// A query matching exactly one chunk, lexical side only: no strategy
		// corroborated it, so it cannot be the top of the confidence scale.
		res, err := gi.Hybrid.Search(ctx, "midnight", HybridSearchOptions{Limit: 10})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Results) != 1 {
			t.Fatalf("fixture drifted: got %d results for 'midnight', want 1", len(res.Results))
		}
		if res.Confidence == "very_high" {
			t.Errorf("a single uncorroborated hit reported very_high confidence")
		}
	})
}
