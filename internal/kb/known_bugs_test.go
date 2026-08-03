package kb

// Characterization tests for the five tracked retrieval defects, GitHub issues
// #69 to #73.
//
// HOW TO USE THIS FILE
//
// Every test here PASSES TODAY by asserting the broken behaviour. Each one is
// gated on a `bugNNFixed` constant that is currently false. When WP-3.4 fixes a
// bug, flip that ONE constant to true: the test then asserts the correct
// behaviour instead, and the assertions for both worlds sit side by side so the
// diff shows exactly what changed.
//
// Do not delete a test when its bug is fixed. The "correct" branch becomes the
// regression test.

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Flip one of these to true in WP-3.4 when the corresponding fix lands.
const (
	bug69Fixed = false // #69 adaptive query-type weighting is dead code
	bug71Fixed = false // #71 embedding client has no timeout
)

// KNOWN BUG #69 -- this test documents current broken behavior. WP-3.4 flips these assertions.
// Correct behavior: when the caller does not set SemanticWeight, searchFusion must use the
// query-type weights from strategyWeightMatrix instead of a hard 50/50 split.
//
// Mechanism (verified against internal/kb/hybrid_search.go on branch v2):
//
//	:201-203  Search() applies `if opts.SemanticWeight <= 0 { opts.SemanticWeight = 0.5 }`
//	:493      searchFusion() reads the adaptive weights from strategyWeightMatrix
//	:494-498  ... then unconditionally overrides them, because SemanticWeight is now
//	          always > 0 thanks to the defaulting above. The "allow override from
//	          options" branch can never be skipped, so the matrix is dead code.
//
// The test proves it arithmetically. For an entity query with a single matching
// entity, a lexical-only fusion produces
//
//	score(rank 1) = lexicalWeight * 1/(k+1) * entityBoost * agreementBoost
//	              = lexicalWeight * 1/61     * 1.2         * 1.1
//
// so the lexical weight actually used can be read back off the score. The
// matrix says 0.6 for an entity query; the observed value is 0.5.
func TestKnownBug_Issue69(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	const query = "Wonderland" // classified as QueryTypeEntity, matches one document

	analysis := gi.Hybrid.analyzeQuery(query)
	if analysis.QueryType != QueryTypeEntity {
		t.Fatalf("fixture drifted: %q is classified as %s, expected %s", query, analysis.QueryType, QueryTypeEntity)
	}

	adaptive := gi.Hybrid.getWeightsForQueryType(QueryTypeEntity)
	if adaptive.Lexical != 0.6 {
		t.Fatalf("fixture drifted: the matrix no longer says 0.6 lexical for entity queries (got %.2f)", adaptive.Lexical)
	}

	res, err := gi.Hybrid.Search(ctx, query, HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(res.Results) == 0 {
		t.Fatal("expected results")
	}

	const (
		rrfRank1       = 1.0 / 61.0 // RRFConstant 60, rank 1
		entityBoost    = 1.2        // single-word entity present in the hit
		agreementBoost = 1.1        // one of two possible strategies contributed
	)
	observedLexicalWeight := res.Results[0].Score / (rrfRank1 * entityBoost * agreementBoost)

	if bug69Fixed {
		// Correct behaviour: the adaptive matrix is honoured.
		if math.Abs(observedLexicalWeight-0.6) > 1e-9 {
			t.Errorf("adaptive weighting is not being applied: observed lexical weight %.6f, want 0.6", observedLexicalWeight)
		}
	} else {
		// Current behaviour: always 50/50, whatever the query type.
		if math.Abs(observedLexicalWeight-0.5) > 1e-9 {
			t.Errorf("expected the hard-coded 50/50 split; observed lexical weight %.6f, want 0.5", observedLexicalWeight)
		}
	}

	// The same 50/50 result appears for every query type, which is the point:
	// classification happens, and then nothing downstream uses it.
	for _, q := range []string{
		"Ishmael",    // entity      -> matrix says lexical 0.6
		"Gettysburg", // entity      -> matrix says lexical 0.6
		"Liberty",    // entity      -> matrix says lexical 0.6
	} {
		r, err := gi.Hybrid.Search(ctx, q, HybridSearchOptions{Limit: 1})
		if err != nil || len(r.Results) == 0 {
			t.Fatalf("Search(%q): %v (%d results)", q, err, len(r.Results))
		}
		got := r.Results[0].Score / (rrfRank1 * entityBoost * agreementBoost)
		want := 0.5
		if bug69Fixed {
			want = gi.Hybrid.getWeightsForQueryType(gi.Hybrid.analyzeQuery(q).QueryType).Lexical
		}
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("query %q: observed lexical weight %.6f, want %.6f", q, got, want)
		}
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

// KNOWN BUG #71 -- this test documents current broken behavior. WP-3.4 flips these assertions.
// Correct behavior: the embedding HTTP client must carry a timeout (or the service must impose
// a per-request deadline), so a hung Ollama cannot block an indexing or search goroutine forever.
//
// Mechanism (verified against internal/kb/embeddings.go on branch v2):
//
//	:72  api.NewClient(ollamaURL, http.DefaultClient)
//
// http.DefaultClient has Timeout == 0, i.e. no timeout at all. Every embedding
// request inherits that. The only thing that can unblock a call is the caller's
// context -- and the daemon's indexing path passes contexts that have no
// deadline.
//
// The proof is hermetic: a local httptest server that accepts the connection
// and never answers, plus a bounded wait in the test so CI can never hang.
func TestKnownBug_Issue71(t *testing.T) {
	// The static half of the proof: no timeout on the client the service uses.
	if bug71Fixed {
		if http.DefaultClient.Timeout != 0 {
			t.Fatalf("this test mutated http.DefaultClient; it must not")
		}
	} else if http.DefaultClient.Timeout != 0 {
		t.Fatalf("http.DefaultClient unexpectedly has a timeout of %v", http.DefaultClient.Timeout)
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

	svc, err := NewEmbeddingService(EmbeddingConfig{OllamaHost: srv.URL})
	if err != nil {
		t.Fatalf("NewEmbeddingService: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		// context.Background() deliberately: this mirrors the call sites that
		// have no deadline of their own, which is where the missing client
		// timeout actually bites.
		_, err := svc.Embed(context.Background(), "does the client give up on its own?")
		done <- err
	}()

	// A generous-but-bounded wait. Anything under a few seconds is enough to
	// distinguish "no timeout" from "any sane timeout"; keeping it short keeps
	// CI fast.
	const blockedFor = 2 * time.Second

	select {
	case err := <-done:
		releaseHandler()
		if bug71Fixed {
			// Correct behaviour: the client gives up on its own.
			if err == nil {
				t.Errorf("expected a timeout error from the embedding client, got nil")
			}
		} else {
			t.Errorf("the embedding call returned after %v against a server that never responds "+
				"(err=%v); a timeout appears to have been added -- flip bug71Fixed to true", blockedFor, err)
		}
	case <-time.After(blockedFor):
		if bug71Fixed {
			t.Errorf("the embedding call is still blocked after %v; the timeout fix is not effective", blockedFor)
		}
		// Current behaviour: still blocked, because there is no timeout.
		releaseHandler()
		select {
		case <-done:
			// Unblocked once the server answered, as expected.
		case <-time.After(5 * time.Second):
			t.Fatalf("the embedding call did not finish even after the server responded")
		}
	}
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
