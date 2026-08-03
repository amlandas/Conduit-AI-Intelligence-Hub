package kb

// Golden retrieval tests. These photograph the retrieval behaviour of the v1
// engine (SQLite FTS5 + RRF fusion) against a fixed, committed corpus, so that
// the v2 rebuild -- in particular the Qdrant -> sqlite-vec migration -- can be
// shown not to have moved search behaviour by accident.
//
// Read this before changing any expectation in this file:
//
//   * These tests pin CURRENT behaviour, not desired behaviour. Several of the
//     pinned values are wrong on purpose; they carry a comment saying so and a
//     cross-reference to the issue that tracks the defect.
//   * The whole file is hermetic: a temp SQLite database, the committed corpus
//     under testdata/corpus, and a HybridSearcher whose semantic side is nil.
//     No Ollama, no Qdrant, no network, no clock dependence.
//   * If an expectation moves, that is a behaviour change. Decide whether it
//     was intended before editing the number.

import (
	"context"
	"math"
	"strings"
	"testing"
)

// scoreEpsilon is the tolerance for float comparisons of RRF/BM25 scores.
//
// The arithmetic is deterministic run to run on a given machine (verified with
// -count=3). The tolerance exists only to absorb a possible sub-ULP difference
// in libm's log() between the Linux and macOS CI runners, which SQLite's bm25()
// calls. Any genuine behaviour change moves these scores by 1e-3 or more, so
// 1e-9 still catches everything that matters.
const scoreEpsilon = 1e-9

func floatsClose(a, b float64) bool { return math.Abs(a-b) <= scoreEpsilon }

// TestGolden_CorpusIngestion pins what ingestion produces for the golden
// corpus: document count, per-document chunk count, and total indexed rows.
//
// The chunk counts here are a direct consequence of Chunker.Chunk's windowing
// (see TestGolden_ChunkerHonoursSplitters). Issue #76 moved them: cuts now land
// on paragraph and sentence boundaries instead of blindly at MaxSize, and the
// redundant trailing chunk is gone, so the 1100-1485 character documents are 2
// chunks rather than 3.
func TestGolden_CorpusIngestion(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	if len(gi.Docs) != 7 {
		t.Fatalf("golden corpus size changed: got %d documents, want 7", len(gi.Docs))
	}

	wantChunks := map[string]int{
		"01-gettysburg-address":         3,
		"02-declaration-preamble":       2,
		"03-moby-dick-loomings":         2,
		"04-alice-down-the-rabbit-hole": 2,
		"05-sonnet-18":                  1,
		"06-fixture-lantern-keeper":     1,
		"07-fixture-harbour-ledger":     1,
	}
	total := 0
	for id, want := range wantChunks {
		got, ok := gi.ChunkCount[id]
		if !ok {
			t.Errorf("document %s was not ingested", id)
			continue
		}
		if got != want {
			t.Errorf("chunk count for %s: got %d, want %d", id, got, want)
		}
		total += want
	}

	stats, err := gi.Indexer.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalDocuments != 7 {
		t.Errorf("TotalDocuments: got %d, want 7", stats.TotalDocuments)
	}
	if stats.TotalChunks != total {
		t.Errorf("TotalChunks: got %d, want %d", stats.TotalChunks, total)
	}
	if stats.KAGEnabled {
		t.Errorf("KAG should be off in the hermetic harness")
	}
}

// TestGolden_FTSKeywordSearch pins the FTS5 result ordering for a fixed set of
// queries. Scores are raw SQLite bm25() output, where MORE NEGATIVE IS BETTER;
// the SQL orders by score ASC, which is correct. Compare against
// TestKnownBug_Issue73, where the same sign convention is mishandled.
func TestGolden_FTSKeywordSearch(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	tests := []struct {
		name string
		// query as the user typed it
		query string
		// wantDocs is the ranked list of document ids, one entry per returned
		// chunk. A document appears more than once when several of its chunks
		// match.
		wantDocs []string
		// wantTopScore is the bm25 score of rank 1 (negative == better).
		// Zero means "no results expected".
		wantTopScore float64
		note         string
	}{
		{
			name:         "phrase shared by two documents",
			query:        "created equal",
			wantDocs:     []string{"01-gettysburg-address", "02-declaration-preamble"},
			wantTopScore: -1.5915914261298685,
			note: "both documents contain 'all men are created equal'. Issue #76 moved this ranking: with " +
				"boundary-aware cuts the gettysburg chunk carrying the phrase is now the shorter of the two, " +
				"so BM25 length normalisation favours it instead of the preamble",
		},
		{
			name:         "term frequency gradient",
			query:        "lantern",
			wantDocs:     []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"},
			wantTopScore: -2.6057274240106465,
			note:         "lantern appears 4x in the keeper fixture and 1x in the harbour fixture",
		},
		{
			name:         "single document, several matching chunks",
			query:        "rabbit",
			wantDocs:     []string{"04-alice-down-the-rabbit-hole", "04-alice-down-the-rabbit-hole"},
			wantTopScore: -2.3712867256466472,
			note:         "chunk overlap means the same document can occupy several slots (two chunks since #76, three before)",
		},
		{
			name:         "term present in two documents, three chunks",
			query:        "government",
			wantDocs:     []string{"02-declaration-preamble", "01-gettysburg-address", "01-gettysburg-address"},
			wantTopScore: -1.3106597756377871,
		},
		{
			name:         "stemming: 'people' matches 'people' and 'people's'",
			query:        "people",
			wantDocs:     []string{"01-gettysburg-address", "01-gettysburg-address", "02-declaration-preamble", "03-moby-dick-loomings", "02-declaration-preamble", "03-moby-dick-loomings"},
			wantTopScore: -0.0000015936073059,
			note: "moby-dick only matches via \"people's hats\" -- porter stemming, not a substring match. " +
				"The near-zero top score is BM25 IDF: after #76 the term appears in half the chunks in the corpus",
		},
		{
			name:         "identical paragraph in two documents",
			query:        "boilerplate notice",
			wantDocs:     []string{"07-fixture-harbour-ledger", "06-fixture-lantern-keeper"},
			wantTopScore: -2.5441189067585825,
			note:         "duplicate content is NOT deduplicated; the shorter host document ranks first",
		},
		{
			name:     "no match returns an empty, non-error result",
			query:    "whale",
			wantDocs: nil,
			note:     "'whale' appears nowhere in the corpus; FTS5 returns zero rows rather than an error",
		},
		{
			name:         "apostrophe in query still finds the sonnet",
			query:        "summer's day",
			wantDocs:     []string{"05-sonnet-18"},
			wantTopScore: -2.5609151512518666,
			note: `#70/#75: the query is now "summer's" "day"* -- one term for the possessive rather ` +
				`than summer AND s AND day*, which is why the score moved`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := gi.Searcher.Search(ctx, tt.query, SearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q): %v", tt.query, err)
			}

			got := docIDs(res.Results)
			if !equalStrings(got, tt.wantDocs) {
				t.Errorf("ranking for %q:\n got %v\nwant %v\n%s", tt.query, got, tt.wantDocs, tt.note)
			}

			if tt.wantTopScore != 0 {
				if len(res.Results) == 0 {
					t.Fatalf("expected results for %q", tt.query)
				}
				if !floatsClose(res.Results[0].Score, tt.wantTopScore) {
					t.Errorf("top bm25 score for %q: got %.16f, want %.16f", tt.query, res.Results[0].Score, tt.wantTopScore)
				}
			}

			// BM25 is negative-is-better and the SQL orders ASC, so the score
			// sequence must be non-decreasing.
			for i := 1; i < len(res.Results); i++ {
				if res.Results[i].Score < res.Results[i-1].Score {
					t.Errorf("FTS results for %q are not in bm25 order: rank %d (%.6f) < rank %d (%.6f)",
						tt.query, i, res.Results[i].Score, i-1, res.Results[i-1].Score)
				}
			}
		})
	}
}

// TestGolden_HybridFusionOrdering pins hybrid search with the semantic side
// switched off, which is the shape every CI run and every semantic-outage
// production run takes.
//
// Note the score convention flips between modes: fusion returns small positive
// RRF scores, lexical mode passes raw negative bm25 straight through. That is
// current behaviour, not a typo -- see the sixth-bug note in the WP report.
func TestGolden_HybridFusionOrdering(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	tests := []struct {
		name           string
		query          string
		wantMode       HybridSearchMode
		wantDocs       []string
		wantTopScore   float64
		wantConfidence string
		wantStrategies int
	}{
		{
			name:           "exploratory query goes through fusion",
			query:          "created equal",
			wantMode:       HybridModeFusion,
			wantDocs:       []string{"01-gettysburg-address", "02-declaration-preamble"}, // #76 moved this ranking
			wantTopScore:   0.018032786885245903,                                         // #69: 1/(60+1) * 1.1 (agreement), unweighted
			wantConfidence: "medium",
			wantStrategies: 1,
		},
		{
			name:           "entity query gets an exact-match boost",
			query:          "Wonderland",
			wantMode:       HybridModeFusion,
			wantDocs:       []string{"04-alice-down-the-rabbit-hole", "04-alice-down-the-rabbit-hole"}, // #76: two chunks, not three
			wantTopScore:   0.021639344262295086,                                                      // ... * 1.2 for the single-word entity hit
			wantConfidence: "medium",
			wantStrategies: 1,
		},
		{
			// #70: an apostrophe no longer forces lexical mode, so this query
			// goes through fusion like any other and carries fusion's score
			// scale, confidence and strategy count.
			name:           "apostrophe goes through fusion like any other query",
			query:          "summer's day",
			wantMode:       HybridModeFusion,
			wantDocs:       []string{"05-sonnet-18"},
			wantTopScore:   0.018032786885245903,
			wantConfidence: "medium",
			wantStrategies: 1,
		},
		{
			// The control: a genuine double-quoted phrase still selects lexical
			// mode.
			name:           "a double-quoted phrase still selects lexical mode",
			query:          `"summer's day"`,
			wantMode:       HybridModeLexical,
			wantDocs:       []string{"05-sonnet-18"},
			wantTopScore:   -1.2461497559769139,
			wantConfidence: "", // KNOWN GAP: searchFTSOnly never sets Confidence or StrategiesUsed. Fixed by #77.
			wantStrategies: 0,
		},
		{
			name:           "no match returns an empty fusion result",
			query:          "whale",
			wantMode:       HybridModeFusion,
			wantDocs:       nil,
			wantConfidence: "none",
			wantStrategies: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := gi.Hybrid.Search(ctx, tt.query, HybridSearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q): %v", tt.query, err)
			}
			if res.Mode != tt.wantMode {
				t.Errorf("mode for %q: got %s, want %s", tt.query, res.Mode, tt.wantMode)
			}
			if got := docIDs(res.Results); !equalStrings(got, tt.wantDocs) {
				t.Errorf("ranking for %q:\n got %v\nwant %v", tt.query, got, tt.wantDocs)
			}
			if res.Confidence != tt.wantConfidence {
				t.Errorf("confidence for %q: got %q, want %q", tt.query, res.Confidence, tt.wantConfidence)
			}
			if res.StrategiesUsed != tt.wantStrategies {
				t.Errorf("strategies for %q: got %d, want %d", tt.query, res.StrategiesUsed, tt.wantStrategies)
			}
			if tt.wantTopScore != 0 {
				if len(res.Results) == 0 {
					t.Fatalf("expected results for %q", tt.query)
				}
				if !floatsClose(res.Results[0].Score, tt.wantTopScore) {
					t.Errorf("top score for %q: got %.18f, want %.18f", tt.query, res.Results[0].Score, tt.wantTopScore)
				}
			}
		})
	}
}

// TestGolden_MMRReordersFinalRanking pins the fact that the final ranking is
// NOT sorted by the Score field: MMR runs last and reorders for diversity while
// leaving Score untouched. Consumers that re-sort by Score get a different order
// than the one Conduit returned.
func TestGolden_MMRReordersFinalRanking(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	res, err := gi.Hybrid.Search(ctx, "people", HybridSearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}

	wantOrder := []string{
		"01-gettysburg-address",
		"03-moby-dick-loomings", // promoted by MMR: least similar snippet
		"02-declaration-preamble",
		"02-declaration-preamble",
		"03-moby-dick-loomings",
		"01-gettysburg-address",
	}
	if got := docIDs(res.Results); !equalStrings(got, wantOrder) {
		t.Fatalf("MMR ordering for 'people':\n got %v\nwant %v", got, wantOrder)
	}
	if !res.MMRApplied {
		t.Errorf("expected MMRApplied to be set")
	}

	// The pinned surprise: rank 2 scores lower than rank 3.
	if !(res.Results[1].Score < res.Results[2].Score) {
		t.Errorf("expected the MMR-promoted hit at rank 2 (%.6f) to score below rank 3 (%.6f); "+
			"if this now holds, MMR stopped reordering or started rewriting Score",
			res.Results[1].Score, res.Results[2].Score)
	}
}

// TestGolden_RetrievalSuite wires up the RetrievalTestCase / EvaluateTestCase
// scaffolding that already existed in internal/kb/retrieval_test_suite.go but
// had no caller anywhere in the tree. The built-in GetRetrievalTestSuite()
// cases reference PDFs that are not in this repository, so they cannot run;
// the harness types themselves are reusable, so they are reused here against
// the committed corpus.
func TestGolden_RetrievalSuite(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	cases := []RetrievalTestCase{
		{
			Name:           "entity_ranks_first",
			Query:          "Wonderland",
			ExpectedTopDoc: "04-alice-down-the-rabbit-hole.txt",
			MustContain:    []string{"Wonderland"},
			MinResults:     1,
			MaxRank:        1,
			Description:    "a proper noun present in the title must take rank 1",
		},
		{
			Name:           "term_frequency_beats_recency",
			Query:          "lantern",
			ExpectedTopDoc: "06-fixture-lantern-keeper.txt",
			MustContain:    []string{"lantern"},
			MinResults:     2,
			MaxRank:        1,
			Description:    "the document mentioning lantern four times outranks the one mentioning it once",
		},
		{
			Name:           "shared_phrase_returns_both_documents",
			Query:          "created equal",
			ExpectedTopDoc: "01-gettysburg-address.txt", // #76 moved rank 1; see TestGolden_FTSKeywordSearch
			MustContain:    []string{"created equal"},
			MinResults:     2,
			MaxRank:        1,
			Description:    "a phrase present in two documents returns both",
		},
		{
			Name:        "duplicate_paragraph_is_not_filtered",
			Query:       "boilerplate notice",
			MustContain: []string{"Boilerplate notice"},
			MinResults:  2,
			Description: "verbatim duplicate content survives ingestion; no dedup exists today",
		},
	}

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			res, err := gi.Searcher.Search(ctx, tc.Query, SearchOptions{Limit: 10, Highlight: true})
			if err != nil {
				t.Fatalf("Search(%q): %v", tc.Query, err)
			}
			passed, reason := EvaluateTestCase(tc, res.Results)
			if !passed {
				t.Errorf("retrieval case %q failed: %s (%s)", tc.Name, reason, tc.Description)
			}
		})
	}
}

// TestGolden_SearchFiltersAndPagination pins the filter and paging behaviour
// that the hybrid layer relies on.
func TestGolden_SearchFiltersAndPagination(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	t.Run("mime type filter excludes everything on a miss", func(t *testing.T) {
		res, err := gi.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 10, MimeTypes: []string{"application/pdf"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Results) != 0 {
			t.Errorf("expected no results for a non-matching mime filter, got %d", len(res.Results))
		}
	})

	t.Run("mime type filter keeps matching documents", func(t *testing.T) {
		res, err := gi.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 10, MimeTypes: []string{"text/plain"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if got := docIDs(res.Results); !equalStrings(got, []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"}) {
			t.Errorf("got %v", got)
		}
	})

	t.Run("unknown source id filters everything out", func(t *testing.T) {
		res, err := gi.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 10, SourceIDs: []string{"does-not-exist"}})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Results) != 0 {
			t.Errorf("expected no results, got %d", len(res.Results))
		}
	})

	t.Run("limit and offset walk the ranking", func(t *testing.T) {
		first, err := gi.Searcher.Search(ctx, "government", SearchOptions{Limit: 2})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		second, err := gi.Searcher.Search(ctx, "government", SearchOptions{Limit: 2, Offset: 2})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		// #76: "government" now matches three chunks rather than four, so the
		// second page holds the single remainder.
		if got := docIDs(first.Results); !equalStrings(got, []string{"02-declaration-preamble", "01-gettysburg-address"}) {
			t.Errorf("page 1: got %v", got)
		}
		if got := docIDs(second.Results); !equalStrings(got, []string{"01-gettysburg-address"}) {
			t.Errorf("page 2: got %v", got)
		}
		// TotalHits ignores paging.
		if first.TotalHits != 3 || second.TotalHits != 3 {
			t.Errorf("TotalHits should be 3 on both pages, got %d and %d", first.TotalHits, second.TotalHits)
		}
	})

	t.Run("highlighted snippets are windowed around the first match", func(t *testing.T) {
		res, err := gi.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 1, Highlight: true, ContextLen: 20})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if len(res.Results) == 0 {
			t.Fatal("expected a result")
		}
		snippet := res.Results[0].Snippet
		if !strings.Contains(strings.ToLower(snippet), "lantern") {
			t.Errorf("snippet does not contain the query term: %q", snippet)
		}
		// ContextLen 20 gives 20 chars either side of the match plus the term.
		if len(snippet) > 120 {
			t.Errorf("snippet is longer than the requested context window: %d chars", len(snippet))
		}
	})
}

// TestGolden_ReindexReplacesDocument pins the update path: re-indexing the same
// document id removes the previous chunks from FTS5 rather than duplicating.
func TestGolden_ReindexReplacesDocument(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	// "midnight" appears only in the body of 06-fixture-lantern-keeper. Note
	// that a body-only term is required here: kb_fts indexes title and path as
	// well as content, so a term that also occurs in the title would keep
	// matching after the body is replaced.
	before, err := gi.Searcher.Search(ctx, "midnight", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := docIDs(before.Results); !equalStrings(got, []string{"06-fixture-lantern-keeper"}) {
		t.Fatalf("precondition failed: got %v", got)
	}

	var doc corpusDoc
	for _, d := range gi.Docs {
		if d.DocumentID == "06-fixture-lantern-keeper" {
			doc = d
		}
	}

	replacement := "The keeper replaced every lamp with an electric bulb. No open flame remains."
	d := &Document{
		DocumentID: doc.DocumentID,
		SourceID:   "",
		Path:       "/corpus/" + doc.FileName,
		Title:      doc.Title,
		MimeType:   "text/plain",
	}
	// Reuse the original source id by reading it back from the row we are about
	// to replace; kb_documents.source_id has a foreign key.
	if err := gi.DB.QueryRowContext(ctx, `SELECT source_id FROM kb_documents WHERE document_id = ?`, doc.DocumentID).Scan(&d.SourceID); err != nil {
		t.Fatalf("read source id: %v", err)
	}

	chunks := gi.Chunker.Chunk(replacement, goldenChunkOptions)
	if err := gi.Indexer.Index(ctx, d, chunks); err != nil {
		t.Fatalf("re-index: %v", err)
	}

	after, err := gi.Searcher.Search(ctx, "midnight", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(after.Results) != 0 {
		t.Errorf("after re-index, the replaced body should be gone from FTS5; got %v", docIDs(after.Results))
	}

	// The title is still indexed and still says "Lantern Keeper Notes", so the
	// document keeps matching "lantern" even though its body no longer mentions
	// one. Pinned deliberately: title-driven matches are a live behaviour.
	byTitle, err := gi.Searcher.Search(ctx, "lantern", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := docIDs(byTitle.Results); !equalStrings(got, []string{"06-fixture-lantern-keeper", "07-fixture-harbour-ledger"}) {
		t.Errorf("title-driven match changed; got %v", got)
	}

	fresh, err := gi.Searcher.Search(ctx, "electric bulb", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := docIDs(fresh.Results); !equalStrings(got, []string{"06-fixture-lantern-keeper"}) {
		t.Errorf("replacement content not searchable; got %v", got)
	}
}

// TestGolden_IngestionIsDeterministic runs the whole pipeline twice and
// requires byte-identical rankings. If this ever flakes, the harness has picked
// up a dependency on map iteration order, the clock, or a random id, and every
// other golden expectation in this package becomes untrustworthy.
func TestGolden_IngestionIsDeterministic(t *testing.T) {
	ctx := context.Background()
	queries := []string{"created equal", "lantern", "people", "Wonderland", "government"}

	run := func() map[string][]string {
		gi := ingestGoldenCorpus(t)
		out := make(map[string][]string, len(queries))
		for _, q := range queries {
			res, err := gi.Hybrid.Search(ctx, q, HybridSearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q): %v", q, err)
			}
			out[q] = docIDs(res.Results)
		}
		return out
	}

	a, b := run(), run()
	for _, q := range queries {
		if !equalStrings(a[q], b[q]) {
			t.Errorf("non-deterministic ranking for %q:\n run1 %v\n run2 %v", q, a[q], b[q])
		}
	}
}
