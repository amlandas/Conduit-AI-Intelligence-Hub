package kb

// Table tests for the fusion stage: RRF arithmetic, agreement tracking,
// exact-match boosting, the similarity floor, reranking, MMR and the confidence
// model. All pure functions over in-memory hit lists -- no database, no network.

import (
	"math"
	"testing"
)

func hit(chunkID string, snippet string, score float64) SearchHit {
	return SearchHit{ChunkID: chunkID, DocumentID: "doc_" + chunkID, Snippet: snippet, Score: score}
}

func chunkIDs(hits []SearchHit) []string {
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.ChunkID)
	}
	return out
}

func scoreOf(hits []SearchHit, chunkID string) (float64, bool) {
	for _, h := range hits {
		if h.ChunkID == chunkID {
			return h.Score, true
		}
	}
	return 0, false
}

func TestQueryTypeWeightMatrix(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	tests := []struct {
		qt          QueryType
		wantSem     float64
		wantLexical float64
	}{
		{QueryTypeExactQuote, 0.1, 0.9},
		{QueryTypeEntity, 0.4, 0.6},
		{QueryTypeConceptual, 0.8, 0.2},
		{QueryTypeFactual, 0.5, 0.5},
		{QueryTypeExploratory, 0.7, 0.3},
		{QueryType("unrecognised"), 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(string(tt.qt), func(t *testing.T) {
			got := hs.getWeightsForQueryType(tt.qt)
			if got.Semantic != tt.wantSem || got.Lexical != tt.wantLexical {
				t.Errorf("weights for %s: got {sem %.2f, lex %.2f}, want {sem %.2f, lex %.2f}",
					tt.qt, got.Semantic, got.Lexical, tt.wantSem, tt.wantLexical)
			}
			if math.Abs(got.Semantic+got.Lexical-1.0) > 1e-9 {
				t.Errorf("weights for %s do not sum to 1", tt.qt)
			}
		})
	}
	// The matrix is real and differentiated. Whether it is ever USED is a
	// separate question -- see TestKnownBug_Issue69.
}

func TestQueryClassification(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	tests := []struct {
		name            string
		query           string
		wantType        QueryType
		wantQuoted      bool
		wantProperNouns []string
		wantMode        HybridSearchMode
	}{
		{name: "bare noun is exploratory", query: "lantern", wantType: QueryTypeExploratory, wantMode: HybridModeFusion},
		{name: "double-quoted phrase is an exact quote", query: `"big science"`, wantType: QueryTypeExactQuote, wantQuoted: true, wantMode: HybridModeLexical},
		{name: "how-question is conceptual", query: "how does replication work", wantType: QueryTypeConceptual, wantMode: HybridModeFusion},
		{name: "explain is conceptual", query: "explain reciprocal rank fusion", wantType: QueryTypeConceptual, wantMode: HybridModeFusion},
		{name: "four-digit year is factual", query: "revenue in 1863", wantType: QueryTypeFactual, wantMode: HybridModeFusion},
		{
			// "how much" is in factualPatterns, but the conceptual patterns are
			// checked first and "^how" matches, so the factual rule is
			// unreachable for any query starting with a wh-word.
			name: "conceptual patterns win over factual ones", query: "how much did it cost",
			wantType: QueryTypeConceptual, wantMode: HybridModeFusion,
		},
		{name: "capitalised pair is an entity query", query: "Oak Ridge", wantType: QueryTypeEntity, wantProperNouns: []string{"Oak Ridge"}, wantMode: HybridModeFusion},
		{name: "single capitalised word is an entity query", query: "Wonderland", wantType: QueryTypeEntity, wantMode: HybridModeFusion},
		{name: "stopword capitalisation is ignored mid-sentence", query: "tell me The story", wantType: QueryTypeExploratory, wantMode: HybridModeFusion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := hs.analyzeQuery(tt.query)
			if a.QueryType != tt.wantType {
				t.Errorf("QueryType(%q) = %s, want %s", tt.query, a.QueryType, tt.wantType)
			}
			if a.HasQuotedPhrase != tt.wantQuoted {
				t.Errorf("HasQuotedPhrase(%q) = %v, want %v", tt.query, a.HasQuotedPhrase, tt.wantQuoted)
			}
			if !equalStrings(a.ProperNouns, tt.wantProperNouns) {
				t.Errorf("ProperNouns(%q) = %v, want %v", tt.query, a.ProperNouns, tt.wantProperNouns)
			}
			if got := hs.selectMode(a); got != tt.wantMode {
				t.Errorf("selectMode(%q) = %s, want %s", tt.query, got, tt.wantMode)
			}
		})
	}
}

// TestRRFArithmetic pins the exact reciprocal-rank-fusion formula:
//
//	score(d) = lexicalWeight/(k + ftsRank) + semanticWeight/(k + semanticRank)
//
// with 1-indexed ranks. Both the live implementation
// (applyRRFWithAgreement) and the unreferenced legacy one (applyRRF) are
// covered, because the legacy formula is the one the v2 rebuild is most likely
// to lift.
func TestRRFArithmetic(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	fts := []SearchHit{hit("a", "aaa", 0), hit("b", "bbb", 0), hit("c", "ccc", 0)}
	sem := []SearchHit{hit("c", "ccc", 0), hit("d", "ddd", 0), hit("a", "aaa", 0)}
	const k = 60
	weights := StrategyWeights{Semantic: 0.4, Lexical: 0.6}

	want := map[string]float64{
		"a": 0.6/61 + 0.4/63, // fts rank 1, semantic rank 3
		"c": 0.6/63 + 0.4/61, // fts rank 3, semantic rank 1
		"b": 0.6 / 62,        // fts only, rank 2
		"d": 0.4 / 62,        // semantic only, rank 2
	}

	got, info := hs.applyRRFWithAgreement(fts, sem, k, weights)

	if order := chunkIDs(got); !equalStrings(order, []string{"a", "c", "b", "d"}) {
		t.Errorf("fusion order: got %v, want [a c b d]", order)
	}
	for id, wantScore := range want {
		gotScore, ok := scoreOf(got, id)
		if !ok {
			t.Errorf("chunk %s missing from fusion output", id)
			continue
		}
		if math.Abs(gotScore-wantScore) > 1e-12 {
			t.Errorf("RRF score for %s: got %.15f, want %.15f", id, gotScore, wantScore)
		}
	}

	wantStrategies := map[string][]SearchStrategy{
		"a": {StrategyFTSExact, StrategySemantic},
		"b": {StrategyFTSExact},
		"c": {StrategyFTSExact, StrategySemantic},
		"d": {StrategySemantic},
	}
	for id, wantS := range wantStrategies {
		gotS := info.chunkStrategies[id]
		if len(gotS) != len(wantS) {
			t.Errorf("strategies for %s: got %v, want %v", id, gotS, wantS)
			continue
		}
		for i := range wantS {
			if gotS[i] != wantS[i] {
				t.Errorf("strategies for %s: got %v, want %v", id, gotS, wantS)
				break
			}
		}
	}

	// WP-3.2 deleted agreementInfo.chunkBestRank (asserted here) and the legacy
	// applyRRF (exercised here). Neither had a caller in production code: the
	// rank map was written and never read, and every fusion path goes through
	// applyRRFWithAgreement, whose formula is pinned above.
}

func TestRRFEdgeCases(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)
	weights := StrategyWeights{Semantic: 0.5, Lexical: 0.5}

	t.Run("both lists empty", func(t *testing.T) {
		got, _ := hs.applyRRFWithAgreement(nil, nil, 60, weights)
		if len(got) != 0 {
			t.Errorf("got %d hits, want 0", len(got))
		}
	})

	t.Run("lexical only", func(t *testing.T) {
		got, _ := hs.applyRRFWithAgreement([]SearchHit{hit("a", "aaa", -3)}, nil, 60, weights)
		if len(got) != 1 {
			t.Fatalf("got %d hits, want 1", len(got))
		}
		if math.Abs(got[0].Score-0.5/61) > 1e-12 {
			t.Errorf("score = %.15f, want %.15f", got[0].Score, 0.5/61)
		}
		// RRF overwrites the incoming bm25 score. This is why the fusion path
		// returns positive scores and the lexical-only path returns negative
		// ones.
		if got[0].Score < 0 {
			t.Errorf("fusion should have replaced the negative bm25 score")
		}
	})

	t.Run("k of zero makes rank 1 dominate", func(t *testing.T) {
		got, _ := hs.applyRRFWithAgreement([]SearchHit{hit("a", "aaa", 0), hit("b", "bbb", 0)}, nil, 0, weights)
		if math.Abs(got[0].Score-0.5/1) > 1e-12 {
			t.Errorf("score = %.15f, want 0.5", got[0].Score)
		}
	})

	t.Run("duplicate chunk ids in one list keep the worst rank", func(t *testing.T) {
		// The rank map is written in order, so a repeated chunk id ends up
		// holding its LAST (worst) rank while the hit payload is also the last.
		got, info := hs.applyRRFWithAgreement([]SearchHit{hit("a", "first", 0), hit("a", "second", 0)}, nil, 60, weights)
		if len(got) != 1 {
			t.Fatalf("got %d hits, want 1 (deduplicated by chunk id)", len(got))
		}
		if math.Abs(got[0].Score-0.5/62) > 1e-12 {
			t.Errorf("score = %.15f, want %.15f (rank 2 won)", got[0].Score, 0.5/62)
		}
		if len(info.chunkStrategies["a"]) != 2 {
			t.Errorf("strategy list should record both appearances, got %v", info.chunkStrategies["a"])
		}
	})
}

func TestBoostExactMatches(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	tests := []struct {
		name      string
		hits      []SearchHit
		entities  []string
		wantScore map[string]float64
		wantOrder []string
	}{
		{
			name: "multi-word entity boosts 1.5x, single-word 1.2x",
			hits: []SearchHit{
				hit("multi", "the Oak Ridge laboratory", 1.0),
				hit("single", "the Ridge alone", 1.0),
				hit("none", "nothing here", 1.0),
			},
			entities:  []string{"Oak Ridge", "Ridge"},
			wantScore: map[string]float64{"multi": 1.8, "single": 1.2, "none": 1.0},
			wantOrder: []string{"multi", "single", "none"},
		},
		{
			name:      "boosts compound multiplicatively",
			hits:      []SearchHit{hit("many", "aaa bbb ccc ddd eee fff", 1.0)},
			entities:  []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff"},
			wantScore: map[string]float64{"many": 2.985984}, // 1.2^6, just under the 3.0 cap
			wantOrder: []string{"many"},
		},
		{
			name:      "boost is capped at 3x",
			hits:      []SearchHit{hit("capped", "aaa bbb ccc ddd eee fff ggg", 1.0)},
			entities:  []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff", "ggg"},
			wantScore: map[string]float64{"capped": 3.0}, // 1.2^7 = 3.583 -> capped
			wantOrder: []string{"capped"},
		},
		{
			name:      "matching is case insensitive",
			hits:      []SearchHit{hit("lower", "oak ridge national laboratory", 1.0)},
			entities:  []string{"Oak Ridge"},
			wantScore: map[string]float64{"lower": 1.5},
			wantOrder: []string{"lower"},
		},
		{
			name:      "no entities is a no-op",
			hits:      []SearchHit{hit("x", "anything", 0.42)},
			entities:  nil,
			wantScore: map[string]float64{"x": 0.42},
			wantOrder: []string{"x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hs.boostExactMatches(append([]SearchHit(nil), tt.hits...), tt.entities)
			if order := chunkIDs(got); !equalStrings(order, tt.wantOrder) {
				t.Errorf("order: got %v, want %v", order, tt.wantOrder)
			}
			for id, want := range tt.wantScore {
				gotScore, ok := scoreOf(got, id)
				if !ok {
					t.Errorf("chunk %s missing", id)
					continue
				}
				if math.Abs(gotScore-want) > 1e-9 {
					t.Errorf("score for %s: got %.9f, want %.9f", id, gotScore, want)
				}
			}
		})
	}
}

func TestApplyAgreementBoost(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)
	info := agreementInfo{chunkStrategies: map[string][]SearchStrategy{
		"both": {StrategyFTSExact, StrategySemantic},
		"fts":  {StrategyFTSExact},
		"sem":  {StrategySemantic},
	}}

	// WP-3.2 removed applyAgreementBoost's queryType parameter. It only fed a
	// "conceptual query" branch that set agreementBonus = 1.1 -- exactly what
	// the general formula 1 + (1/2 * 0.2) already produced for a single-strategy
	// hit. The table below used to run these same expectations once per query
	// type to document that; the boost no longer has a query type to ignore.
	tests := []struct {
		name string
		want map[string]float64
	}{
		{
			name: "20% for agreement, 10% for a single strategy",
			want: map[string]float64{"both": 1.2, "fts": 1.1, "sem": 1.1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := []SearchHit{hit("both", "b", 1), hit("fts", "f", 1), hit("sem", "s", 1)}
			got := hs.applyAgreementBoost(in, info)
			for id, want := range tt.want {
				gotScore, ok := scoreOf(got, id)
				if !ok {
					t.Errorf("chunk %s missing", id)
					continue
				}
				if math.Abs(gotScore-want) > 1e-9 {
					t.Errorf("score for %s: got %.9f, want %.9f", id, gotScore, want)
				}
			}
			// Agreement re-sorts descending.
			for i := 1; i < len(got); i++ {
				if got[i].Score > got[i-1].Score {
					t.Errorf("agreement boost did not re-sort: rank %d scores above rank %d", i, i-1)
				}
			}
		})
	}
}

func TestApplySimilarityFloor(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)
	hits := []SearchHit{hit("a", "", 0.5), hit("b", "", 0.001), hit("c", "", 0.0009), hit("d", "", -1)}

	tests := []struct {
		floor float64
		want  []string
		notes string
	}{
		{floor: 0, want: []string{"a", "b", "c", "d"}, notes: "a floor of zero disables filtering entirely, including for negative scores"},
		{floor: DefaultSimilarityFloor, want: []string{"a", "b"}},
		{floor: 0.01, want: []string{"a"}},
		{floor: 10, want: nil},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := hs.applySimilarityFloor(hits, tt.floor)
			if !equalStrings(chunkIDs(got), tt.want) {
				t.Errorf("floor %v: got %v, want %v\n%s", tt.floor, chunkIDs(got), tt.want, tt.notes)
			}
		})
	}

	// The floor is applied to RRF scores, which for a lexical-only fusion top
	// hit are around 0.009 -- an order of magnitude above the default floor of
	// 0.001, but only about nine times. A larger RRFConstant would push real
	// results under the floor.
	shrunk := hs.applySimilarityFloor([]SearchHit{hit("top", "", 0.5/1001)}, DefaultSimilarityFloor)
	if len(shrunk) != 0 {
		t.Errorf("expected a rank-1 hit fused with k=1000 to be rejected by the default floor")
	}
}

func TestApplyReranking(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	semantic := []SearchHit{hit("a", "aaa", 0.9), hit("b", "bbb", 0.1)}
	in := []SearchHit{hit("b", "bbb", 1.0), hit("a", "aaa", 0.95), hit("c", "ccc", 0.9)}

	got := hs.applyReranking(append([]SearchHit(nil), in...), "query", 10, semantic)

	// final = rrf * (1 + semanticScore); c has no semantic score so it is
	// unchanged.
	wantScores := map[string]float64{"a": 0.95 * 1.9, "b": 1.0 * 1.1, "c": 0.9}
	for id, want := range wantScores {
		gotScore, ok := scoreOf(got, id)
		if !ok {
			t.Fatalf("chunk %s missing", id)
		}
		if math.Abs(gotScore-want) > 1e-9 {
			t.Errorf("reranked score for %s: got %.9f, want %.9f", id, gotScore, want)
		}
	}
	if order := chunkIDs(got); !equalStrings(order, []string{"a", "b", "c"}) {
		t.Errorf("rerank order: got %v, want [a b c]", order)
	}

	t.Run("topN truncates the candidate list", func(t *testing.T) {
		got := hs.applyReranking(append([]SearchHit(nil), in...), "query", 2, semantic)
		if len(got) != 2 {
			t.Errorf("got %d candidates, want 2 -- reranking DISCARDS everything past topN", len(got))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		if got := hs.applyReranking(nil, "q", 10, semantic); len(got) != 0 {
			t.Errorf("got %d, want 0", len(got))
		}
	})
}

func TestApplyMMR(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	base := []SearchHit{
		hit("m1", "alpha beta gamma delta", 1.00),
		hit("m2", "alpha beta gamma delta", 0.99), // near-duplicate of m1
		hit("m3", "zulu yankee xray whiskey", 0.50),
	}

	tests := []struct {
		name   string
		lambda float64
		limit  int
		want   []string
		notes  string
	}{
		{name: "balanced default keeps the relevance order", lambda: DefaultMMRLambda, limit: 3, want: []string{"m1", "m2", "m3"}},
		{name: "aggressive diversity promotes the dissimilar hit", lambda: 0.5, limit: 3, want: []string{"m1", "m3", "m2"},
			notes: "this is the RecallModePrecise setting"},
		{name: "lambda 1 is pure relevance", lambda: 1.0, limit: 3, want: []string{"m1", "m2", "m3"}},
		{name: "limit truncates", lambda: 0.5, limit: 2, want: []string{"m1", "m3"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hs.applyMMR(append([]SearchHit(nil), base...), tt.lambda, tt.limit)
			if !equalStrings(chunkIDs(got), tt.want) {
				t.Errorf("MMR(lambda=%.1f, limit=%d) = %v, want %v\n%s", tt.lambda, tt.limit, chunkIDs(got), tt.want, tt.notes)
			}
			// MMR reorders but never rewrites Score.
			for _, h := range got {
				orig, _ := scoreOf(base, h.ChunkID)
				if h.Score != orig {
					t.Errorf("MMR rewrote the score of %s: %v -> %v", h.ChunkID, orig, h.Score)
				}
			}
		})
	}

	t.Run("single hit is returned unchanged", func(t *testing.T) {
		one := []SearchHit{hit("only", "x", 1)}
		if got := hs.applyMMR(one, 0.5, 10); len(got) != 1 || got[0].ChunkID != "only" {
			t.Errorf("got %v", chunkIDs(got))
		}
	})
}

func TestTextSimilarity(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	tests := []struct {
		name  string
		a, b  string
		want  float64
		notes string
	}{
		{name: "identical", a: "the quick brown fox", b: "the quick brown fox", want: 1.0},
		{name: "disjoint", a: "alpha beta", b: "gamma delta", want: 0.0},
		{name: "half overlap", a: "alpha beta gamma", b: "alpha beta delta", want: 0.5},
		{name: "case insensitive", a: "Alpha BETA", b: "alpha beta", want: 1.0},
		{name: "punctuation is trimmed", a: "alpha, beta.", b: "alpha beta", want: 1.0},
		{
			name: "tokens shorter than three characters are discarded",
			a:    "a b c", b: "a b c", want: 0.0,
			notes: "both token sets end up empty, so the function reports 0 similarity for identical strings",
		},
		{name: "empty input", a: "", b: "anything at all", want: 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hs.textSimilarity(tt.a, tt.b); math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("textSimilarity(%q, %q) = %.4f, want %.4f\n%s", tt.a, tt.b, got, tt.want, tt.notes)
			}
		})
	}
}

func TestCalculateOverallConfidence(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)
	info := agreementInfo{chunkStrategies: map[string][]SearchStrategy{
		"both": {StrategyFTSExact, StrategySemantic},
		"one":  {StrategyFTSExact},
	}}

	tests := []struct {
		name       string
		hits       []SearchHit
		strategies int
		degraded   bool
		want       string
		notes      string
	}{
		{name: "no hits", hits: nil, strategies: 2, want: "none"},
		{name: "degraded with hits", hits: []SearchHit{hit("both", "", 1)}, strategies: 1, degraded: true, want: "medium"},
		{name: "two strategies, half agree", hits: []SearchHit{hit("both", "", 1), hit("one", "", 1)}, strategies: 2, want: "very_high"},
		{
			name: "BUG SURFACE: a single hit with zero agreement still reports very_high",
			hits: []SearchHit{hit("one", "", 1)}, strategies: 2, want: "very_high",
			notes: "the gate is `highAgreementCount >= len(hits)/2` with integer division, so len(hits)==1 " +
				"makes the right-hand side 0 and the condition always holds -- candidate sixth bug, pinned not fixed",
		},
		{
			name: "two strategies, minority agreement",
			hits: []SearchHit{hit("both", "", 1), hit("one", "", 1), hit("one", "", 1), hit("one", "", 1)}, strategies: 2, want: "high",
			notes: "one agreeing hit out of four: below the half threshold, above zero",
		},
		{
			name: "two strategies, zero agreement",
			hits: []SearchHit{hit("one", "", 1), hit("one", "", 1)}, strategies: 2, want: "medium",
			notes: "no hit was found by both strategies, so the two-strategy branches are skipped entirely",
		},
		{name: "single strategy", hits: []SearchHit{hit("one", "", 1)}, strategies: 1, want: "medium"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hs.calculateOverallConfidence(tt.hits, info, tt.strategies, tt.degraded); got != tt.want {
				t.Errorf("confidence = %q, want %q\n%s", got, tt.want, tt.notes)
			}
		})
	}
}

// TestRecallModePresets pins the option defaults that Search applies before any
// searching happens. The v2 rebuild must keep these, or every score in the
// golden files moves.
func TestRecallModePresets(t *testing.T) {
	tests := []struct {
		mode          RecallMode
		wantMMR       bool
		wantRerank    bool
		wantFloor     float64
		wantLambda    float64
		wantRerankTop int
	}{
		{mode: RecallModeHigh, wantMMR: false, wantRerank: true, wantFloor: 0.0, wantLambda: 1.0, wantRerankTop: 50},
		{mode: RecallModePrecise, wantMMR: true, wantRerank: true, wantFloor: 0.01, wantLambda: 0.5, wantRerankTop: 30},
		{mode: RecallModeBalanced, wantMMR: true, wantRerank: true, wantFloor: DefaultSimilarityFloor, wantLambda: DefaultMMRLambda, wantRerankTop: DefaultRerankTopN},
		{mode: "", wantMMR: true, wantRerank: true, wantFloor: DefaultSimilarityFloor, wantLambda: DefaultMMRLambda, wantRerankTop: DefaultRerankTopN},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			opts := applyRecallModeForTest(HybridSearchOptions{RecallMode: tt.mode})
			if opts.EnableMMR != tt.wantMMR {
				t.Errorf("EnableMMR = %v, want %v", opts.EnableMMR, tt.wantMMR)
			}
			if opts.EnableRerank != tt.wantRerank {
				t.Errorf("EnableRerank = %v, want %v", opts.EnableRerank, tt.wantRerank)
			}
			if opts.SimilarityFloor != tt.wantFloor {
				t.Errorf("SimilarityFloor = %v, want %v", opts.SimilarityFloor, tt.wantFloor)
			}
			if opts.MMRLambda != tt.wantLambda {
				t.Errorf("MMRLambda = %v, want %v", opts.MMRLambda, tt.wantLambda)
			}
			if opts.RerankTopN != tt.wantRerankTop {
				t.Errorf("RerankTopN = %v, want %v", opts.RerankTopN, tt.wantRerankTop)
			}
		})
	}
}

// applyRecallModeForTest mirrors the preset block at the top of
// HybridSearcher.Search. It is duplicated here because that block is inlined in
// Search and there is no seam to call it directly. If the two ever disagree,
// TestRecallModePresets is lying -- keep them in sync, and prefer extracting
// the real function during WP-3.4.
func applyRecallModeForTest(opts HybridSearchOptions) HybridSearchOptions {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.RRFConstant <= 0 {
		opts.RRFConstant = 60
	}
	if opts.SemanticWeight <= 0 {
		opts.SemanticWeight = 0.5
	}
	opts.BoostExactMatch = true
	if opts.RecallMode == "" {
		opts.RecallMode = RecallModeBalanced
	}
	switch opts.RecallMode {
	case RecallModeHigh:
		opts.EnableMMR = false
		opts.EnableRerank = true
		opts.SimilarityFloor = 0.0
		opts.MMRLambda = 1.0
		if opts.RerankTopN <= 0 {
			opts.RerankTopN = 50
		}
	case RecallModePrecise:
		opts.EnableMMR = true
		opts.EnableRerank = true
		opts.SimilarityFloor = 0.01
		opts.MMRLambda = 0.5
		if opts.RerankTopN <= 0 {
			opts.RerankTopN = 30
		}
	default:
		opts.EnableMMR = true
		opts.EnableRerank = true
		if opts.SimilarityFloor <= 0 {
			opts.SimilarityFloor = DefaultSimilarityFloor
		}
		if opts.MMRLambda <= 0 {
			opts.MMRLambda = DefaultMMRLambda
		}
		if opts.RerankTopN <= 0 {
			opts.RerankTopN = DefaultRerankTopN
		}
	}
	return opts
}
