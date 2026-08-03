package kb

// Table tests for the fusion stage: RRF arithmetic, agreement tracking,
// exact-match boosting, the similarity floor, reranking, MMR and the confidence
// model. All pure functions over in-memory hit lists -- no database, no network.

import (
	"math"
	"reflect"
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

// WP-3.4 deleted TestQueryTypeWeightMatrix along with the matrix it covered.
// The matrix mapped query types to semantic/lexical fusion weights that
// searchFusion overrode before reading (issue #69), so the test proved only
// that a table of unused constants was internally consistent. Fusion is now
// unweighted RRF; TestIssue69_FusionIsUnweightedRRF in known_bugs_test.go
// covers what replaced it.

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
//	score(d) = 1/(k + ftsRank) + 1/(k + semanticRank)
//
// with 1-indexed ranks, summed over the strategies that found d. The
// per-strategy weights this used to carry went with the adaptive-weighting
// machinery in #69; they were always 0.5/0.5 in practice, so they scaled every
// score identically and changed no ordering.
func TestRRFArithmetic(t *testing.T) {
	hs := NewHybridSearcher(nil, nil)

	fts := []SearchHit{hit("a", "aaa", 0), hit("b", "bbb", 0), hit("c", "ccc", 0)}
	sem := []SearchHit{hit("c", "ccc", 0), hit("d", "ddd", 0), hit("a", "aaa", 0)}
	const k = 60

	want := map[string]float64{
		"a": 1.0/61 + 1.0/63, // fts rank 1, semantic rank 3
		"c": 1.0/63 + 1.0/61, // fts rank 3, semantic rank 1
		"b": 1.0 / 62,        // fts only, rank 2
		"d": 1.0 / 62,        // semantic only, rank 2
	}

	got, info := hs.applyRRFWithAgreement(fts, sem, k)

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

	t.Run("both lists empty", func(t *testing.T) {
		got, _ := hs.applyRRFWithAgreement(nil, nil, 60)
		if len(got) != 0 {
			t.Errorf("got %d hits, want 0", len(got))
		}
	})

	t.Run("lexical only", func(t *testing.T) {
		got, _ := hs.applyRRFWithAgreement([]SearchHit{hit("a", "aaa", -3)}, nil, 60)
		if len(got) != 1 {
			t.Fatalf("got %d hits, want 1", len(got))
		}
		if math.Abs(got[0].Score-1.0/61) > 1e-12 {
			t.Errorf("score = %.15f, want %.15f", got[0].Score, 1.0/61)
		}
		// RRF overwrites the incoming bm25 score. This is why the fusion path
		// returns positive scores and the lexical-only path returns negative
		// ones.
		if got[0].Score < 0 {
			t.Errorf("fusion should have replaced the negative bm25 score")
		}
	})

	t.Run("k of zero makes rank 1 dominate", func(t *testing.T) {
		got, _ := hs.applyRRFWithAgreement([]SearchHit{hit("a", "aaa", 0), hit("b", "bbb", 0)}, nil, 0)
		if math.Abs(got[0].Score-1.0/1) > 1e-12 {
			t.Errorf("score = %.15f, want 1.0", got[0].Score)
		}
	})

	t.Run("duplicate chunk ids in one list keep the worst rank", func(t *testing.T) {
		// The rank map is written in order, so a repeated chunk id ends up
		// holding its LAST (worst) rank while the hit payload is also the last.
		got, info := hs.applyRRFWithAgreement([]SearchHit{hit("a", "first", 0), hit("a", "second", 0)}, nil, 60)
		if len(got) != 1 {
			t.Fatalf("got %d hits, want 1 (deduplicated by chunk id)", len(got))
		}
		if math.Abs(got[0].Score-1.0/62) > 1e-12 {
			t.Errorf("score = %.15f, want %.15f (rank 2 won)", got[0].Score, 1.0/62)
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
			name: "#77: a single hit with zero agreement is not very_high",
			hits: []SearchHit{hit("one", "", 1)}, strategies: 2, want: "medium",
			notes: "the gate used to be `highAgreementCount >= len(hits)/2` with integer division, so " +
				"len(hits)==1 made the right-hand side 0 and the condition always held -- the highest " +
				"confidence in the vocabulary awarded to the least corroborated case",
		},
		{
			name: "#77: a single hit that BOTH strategies found is very_high",
			hits: []SearchHit{hit("both", "", 1)}, strategies: 2, want: "very_high",
			notes: "one hit, one corroboration: a real majority, not an artefact of truncating division",
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

// TestRecallModePresets covers resolveRecallMode, the single place the
// precision/recall knobs are decided.
//
// WP-3.4 note: this test used to carry a hand-copied duplicate of the preset
// switch, because the real one was inlined in Search with no seam. Extracting
// resolveRecallMode removed the duplicate -- and with it the risk of the test
// agreeing with itself while disagreeing with the engine.
func TestRecallModePresets(t *testing.T) {
	tests := []struct {
		mode RecallMode
		want recallSettings
	}{
		{
			mode: RecallModeHigh,
			want: recallSettings{enableMMR: false, mmrLambda: 1.0, similarityFloor: 0.0, enableRerank: true, rerankTopN: 50},
		},
		{
			mode: RecallModePrecise,
			want: recallSettings{enableMMR: true, mmrLambda: 0.5, similarityFloor: 0.01, enableRerank: true, rerankTopN: 30},
		},
		{
			mode: RecallModeBalanced,
			want: recallSettings{enableMMR: true, mmrLambda: DefaultMMRLambda, similarityFloor: DefaultSimilarityFloor, enableRerank: true, rerankTopN: DefaultRerankTopN},
		},
		{
			mode: "", // unset falls back to balanced
			want: recallSettings{enableMMR: true, mmrLambda: DefaultMMRLambda, similarityFloor: DefaultSimilarityFloor, enableRerank: true, rerankTopN: DefaultRerankTopN},
		},
		{
			mode: "nonsense", // as does anything unrecognised
			want: recallSettings{enableMMR: true, mmrLambda: DefaultMMRLambda, similarityFloor: DefaultSimilarityFloor, enableRerank: true, rerankTopN: DefaultRerankTopN},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			if got := resolveRecallMode(tt.mode); got != tt.want {
				t.Errorf("resolveRecallMode(%q) = %+v, want %+v", tt.mode, got, tt.want)
			}
		})
	}
}

// TestHybridSearchOptionsSurface guards the shrink from #69: the option struct
// is the public control surface for search, and every field on it must be one
// the engine actually reads.
func TestHybridSearchOptionsSurface(t *testing.T) {
	const wantFields = 4
	if got := reflect.TypeOf(HybridSearchOptions{}).NumField(); got != wantFields {
		t.Errorf("HybridSearchOptions has %d fields, want %d. Adding a knob here means adding a way "+
			"for a caller to be silently ignored -- which is what issue #69 was. Prefer RecallMode.",
			got, wantFields)
	}
}

// TestWithRRFConstant covers the searcher-level k that replaced the per-call
// RRFConstant option.
func TestWithRRFConstant(t *testing.T) {
	if got := NewHybridSearcher(nil, nil).rrfConstant; got != DefaultRRFConstant {
		t.Errorf("default k = %d, want %d", got, DefaultRRFConstant)
	}
	if got := NewHybridSearcher(nil, nil, WithRRFConstant(10)).rrfConstant; got != 10 {
		t.Errorf("k = %d, want 10", got)
	}
	// A non-positive k would divide by the rank alone and is rejected.
	if got := NewHybridSearcher(nil, nil, WithRRFConstant(0)).rrfConstant; got != DefaultRRFConstant {
		t.Errorf("k = %d, want the default %d for a non-positive override", got, DefaultRRFConstant)
	}
}
