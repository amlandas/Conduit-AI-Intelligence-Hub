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
	bug70Fixed = false // #70 any apostrophe forces lexical-only search
	bug71Fixed = false // #71 embedding client has no timeout
	bug72Fixed = false // #72 chunkID ignores its index parameter
	bug73Fixed = false // #73 fallback ladder: inverted sort and stripped wildcards
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

// KNOWN BUG #70 -- this test documents current broken behavior. WP-3.4 flips these assertions.
// Correct behavior: only a balanced double-quoted phrase should mark a query as an exact quote;
// a bare apostrophe inside a word must not disable semantic search.
//
// Mechanism (verified against internal/kb/hybrid_search.go on branch v2):
//
//	:288      analyzeQuery sets HasQuotedPhrase when the query contains " OR '
//	:370      classifyQueryType returns QueryTypeExactQuote for HasQuotedPhrase
//	:403-405  selectMode returns HybridModeLexical for HasQuotedPhrase
//
// So "don't", "Alice's", "O'Brien", "it's" and every possessive silently
// disable the vector half of hybrid search.
func TestKnownBug_Issue70(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	apostropheQueries := []string{
		"summer's day",
		"don't index this",
		"O'Brien",
		"the keeper's ledger",
	}

	for _, q := range apostropheQueries {
		t.Run(q, func(t *testing.T) {
			a := gi.Hybrid.analyzeQuery(q)

			if bug70Fixed {
				// Correct behaviour: an apostrophe is just a character.
				if a.HasQuotedPhrase {
					t.Errorf("query %q should not be treated as a quoted phrase", q)
				}
				if got := gi.Hybrid.selectMode(a); got == HybridModeLexical {
					t.Errorf("query %q should not be forced into lexical-only mode", q)
				}
			} else {
				// Current behaviour: apostrophe == quoted phrase == lexical only.
				if !a.HasQuotedPhrase {
					t.Errorf("expected HasQuotedPhrase for %q", q)
				}
				if a.QueryType != QueryTypeExactQuote {
					t.Errorf("expected QueryTypeExactQuote for %q, got %s", q, a.QueryType)
				}
				if got := gi.Hybrid.selectMode(a); got != HybridModeLexical {
					t.Errorf("expected lexical mode for %q, got %s", q, got)
				}
			}
		})
	}

	// A double quote genuinely does signal an exact-phrase intent, and stays
	// lexical under both worlds. This half of the check is the control.
	a := gi.Hybrid.analyzeQuery(`"big science"`)
	if !a.HasQuotedPhrase || gi.Hybrid.selectMode(a) != HybridModeLexical {
		t.Errorf("a double-quoted query must still select lexical mode")
	}

	// End to end: the apostrophe changes the shape of the response, not just an
	// internal flag. Lexical mode skips fusion, so the score is raw bm25 and
	// neither Confidence nor StrategiesUsed is populated.
	withApostrophe, err := gi.Hybrid.Search(ctx, "summer's day", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	withoutApostrophe, err := gi.Hybrid.Search(ctx, "summers day", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	if bug70Fixed {
		if withApostrophe.Mode != withoutApostrophe.Mode {
			t.Errorf("apostrophe still changes the search mode: %s vs %s", withApostrophe.Mode, withoutApostrophe.Mode)
		}
	} else {
		if withApostrophe.Mode != HybridModeLexical {
			t.Errorf("expected lexical mode, got %s", withApostrophe.Mode)
		}
		if withoutApostrophe.Mode != HybridModeFusion {
			t.Errorf("expected fusion mode for the apostrophe-free query, got %s", withoutApostrophe.Mode)
		}
		if withApostrophe.Confidence != "" {
			t.Errorf("lexical mode currently leaves Confidence empty; got %q", withApostrophe.Confidence)
		}
		if len(withApostrophe.Results) > 0 && withApostrophe.Results[0].Score >= 0 {
			t.Errorf("lexical mode currently returns a raw negative bm25 score; got %.6f", withApostrophe.Results[0].Score)
		}
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

// KNOWN BUG #72 -- this test documents current broken behavior. WP-3.4 flips these assertions.
// Correct behavior: chunkID must mix its index parameter (and ideally a document identifier)
// into the hash, so that identical text in two places gets two distinct chunk ids.
//
// Mechanism (verified against internal/kb/chunker.go on branch v2):
//
//	:178-181  func (c *Chunker) chunkID(content string, index int) string {
//	              h := sha256.Sum256([]byte(content))
//	              return "chunk_" + hex.EncodeToString(h[:8])
//	          }
//
// `index` is accepted and never read. Two documents that share a paragraph get
// the same chunk id for it.
//
// Blast radius note: Indexer.Index does NOT use these ids. It calls
// generateUniqueChunkID(documentID, content, index) at indexer.go:463 and
// overwrites Chunk.ChunkID before insert, which is why the collision does not
// corrupt SQLite today. Anything that consumes Chunker output directly --
// chunker.go:453 within ChunkSmart, and any future caller -- is exposed.
func TestKnownBug_Issue72(t *testing.T) {
	c := NewChunker()

	// Two different "documents" that happen to share a paragraph verbatim.
	// This is exactly the shape of the boilerplate paragraph shared by
	// 06-fixture-lantern-keeper.txt and 07-fixture-harbour-ledger.txt.
	shared := "Boilerplate notice: this paragraph is repeated verbatim in another fixture document."
	docA := c.Chunk(shared, ChunkOptions{MaxSize: 1000})
	docB := c.Chunk(shared, ChunkOptions{MaxSize: 1000})

	if len(docA) != 1 || len(docB) != 1 {
		t.Fatalf("fixture drifted: got %d and %d chunks, want 1 each", len(docA), len(docB))
	}

	if bug72Fixed {
		// Correct behaviour: identical text in two documents gets two ids.
		if docA[0].ChunkID == docB[0].ChunkID {
			t.Errorf("identical content in two documents still collides: %s", docA[0].ChunkID)
		}
		if c.chunkID("same", 0) == c.chunkID("same", 7) {
			t.Errorf("chunkID still ignores its index parameter")
		}
	} else {
		// Current behaviour: content-only hashing, so both collide.
		if docA[0].ChunkID != docB[0].ChunkID {
			t.Errorf("expected a cross-document chunk id collision; got %s and %s", docA[0].ChunkID, docB[0].ChunkID)
		}
		if c.chunkID("same", 0) != c.chunkID("same", 7) {
			t.Errorf("expected chunkID to ignore its index parameter")
		}
	}

	// The indexer's own id generator is the thing that saves the database
	// today. It is unaffected by this bug and must keep behaving this way.
	idx := NewIndexer(nil)
	if idx.generateUniqueChunkID("docA", shared, 0) == idx.generateUniqueChunkID("docB", shared, 0) {
		t.Errorf("Indexer.generateUniqueChunkID must not collide across documents")
	}
	if idx.generateUniqueChunkID("docA", shared, 0) == idx.generateUniqueChunkID("docA", shared, 1) {
		t.Errorf("Indexer.generateUniqueChunkID must not collide across chunk indexes")
	}
}

// KNOWN BUG #73 -- this test documents current broken behavior. WP-3.4 flips these assertions.
// Correct behavior: the level-2 fallback must sort SQLite bm25 scores ASCENDING (most negative
// first), and the level-1 relaxed query's prefix wildcards must survive sanitization.
//
// Two mechanisms, both verified against branch v2:
//
//	(a) hybrid_search.go:1251-1253
//	        sort.Slice(allHits, func(i, j int) bool { return allHits[i].Score > allHits[j].Score })
//	    Descending. SQLite's bm25() is negative-is-better -- searcher.go:253 orders
//	    by `score ASC` for exactly that reason -- so this puts the WORST match first.
//
//	(b) hybrid_search.go:1185 builds `clean + "*"` for every word, then
//	    searcher.go:177 lists "*" among the characters sanitizeFTSQuery deletes.
//	    prepareFTSQuery re-adds a wildcard only to the LAST term, so a relaxed
//	    query of three words gets one working prefix instead of three.
func TestKnownBug_Issue73(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	t.Run("a_partial_results_are_sorted_worst_first", func(t *testing.T) {
		// Two documents contain "lantern": the keeper fixture four times
		// (bm25 -2.999, the better match) and the harbour ledger once
		// (bm25 -1.411, the worse match).
		res := gi.Hybrid.searchPartial(ctx, "lantern", HybridSearchOptions{Limit: 10})
		if len(res.Results) != 2 {
			t.Fatalf("fixture drifted: got %d results, want 2", len(res.Results))
		}

		if bug73Fixed {
			// Correct behaviour: most negative bm25 first.
			if res.Results[0].DocumentID != "06-fixture-lantern-keeper" {
				t.Errorf("rank 1 should be the better bm25 match; got %s", res.Results[0].DocumentID)
			}
			if res.Results[0].Score > res.Results[1].Score {
				t.Errorf("results must be in ascending bm25 order; got %.6f then %.6f",
					res.Results[0].Score, res.Results[1].Score)
			}
		} else {
			// Current behaviour: descending sort over negative scores puts the
			// weakest match at rank 1.
			if res.Results[0].DocumentID != "07-fixture-harbour-ledger" {
				t.Errorf("expected the weaker match at rank 1; got %s", res.Results[0].DocumentID)
			}
			if !(res.Results[0].Score > res.Results[1].Score) {
				t.Errorf("expected descending scores (worst first); got %.6f then %.6f",
					res.Results[0].Score, res.Results[1].Score)
			}
		}
	})

	t.Run("b_relaxed_wildcards_are_stripped_before_reaching_fts5", func(t *testing.T) {
		s := NewSearcher(gi.DB)

		// This is the exact string searchRelaxed builds for "lanter harbou zzzz".
		const relaxedQuery = "lanter* OR harbou* OR zzzz*"

		if bug73Fixed {
			if got := s.prepareFTSQuery(relaxedQuery); got != "lanter* OR harbou* OR zzzz*" {
				t.Errorf("relaxed wildcards are still being stripped: %q", got)
			}
			if res := gi.Hybrid.searchRelaxed(ctx, "lanter harbou zzzz", HybridSearchOptions{Limit: 10}); len(res.Results) == 0 {
				t.Errorf("the relaxed rung should now match via prefix search")
			}
		} else {
			if got := sanitizeFTSQuery(relaxedQuery); got != "lanter OR harbou OR zzzz" {
				t.Errorf("expected every wildcard to be stripped; got %q", got)
			}
			if got := s.prepareFTSQuery(relaxedQuery); got != "lanter OR harbou OR zzzz*" {
				t.Errorf("expected only the last term to be re-wildcarded; got %q", got)
			}
			// Consequence: the relaxed rung finds nothing even though two of
			// the three prefixes match real words in the corpus.
			if res := gi.Hybrid.searchRelaxed(ctx, "lanter harbou zzzz", HybridSearchOptions{Limit: 10}); len(res.Results) != 0 {
				t.Errorf("expected the relaxed rung to return nothing; got %d results", len(res.Results))
			}
			// And the level-2 rung, which searches each word alone and so gets
			// its wildcard back, succeeds where level 1 failed.
			if res := gi.Hybrid.searchPartial(ctx, "lanter harbou zzzz", HybridSearchOptions{Limit: 10}); len(res.Results) == 0 {
				t.Errorf("expected the partial rung to rescue the query")
			}
		}
	})

	t.Run("control: the FTS5 layer itself orders bm25 correctly", func(t *testing.T) {
		// searcher.go:253 does the right thing. The defect is only in the
		// fallback ladder's re-sort.
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
