package kb

// Table tests for the FTS5 query pipeline: sanitizeFTSQuery (quote user text
// into a safe FTS5 expression) followed by Searcher.prepareFTSQuery (add a
// prefix wildcard to the final term).
//
// Issues #70 and #75 replaced character deletion with quoting. The invariant
// these tests defend is total: NO user input produces an FTS5 syntax error, and
// NO user input is reinterpreted as an FTS5 operator.

import (
	"context"
	"strings"
	"testing"
)

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		notes string
	}{
		{name: "plain words are quoted individually", in: "hello world", want: `"hello" "world"`,
			notes: "a quoted single word matches exactly what the bareword did; FTS5 tokenizes the contents the same way"},
		{name: "a balanced double-quoted phrase stays one phrase", in: `"exact phrase"`, want: `"exact phrase"`},
		{name: "an apostrophe stays inside its word", in: "it's fine", want: `"it's" "fine"`,
			notes: "#70: this used to become `it s fine`, inventing a one-letter term"},
		{name: "hyphenated term survives as one phrase", in: "ASL-3", want: `"ASL-3"`,
			notes: "#75: the hyphen is inside a string literal, so it is not the NOT operator"},
		{name: "compound word is not split", in: "self-objectification", want: `"self-objectification"`},
		{name: "colon is safe inside quotes", in: "title:foo", want: `"title:foo"`},
		{name: "plus is safe inside quotes", in: "a+b", want: `"a+b"`},
		{name: "parentheses are safe inside quotes", in: "(group)", want: `"(group)"`},
		{name: "caret is safe inside quotes", in: "caret^2", want: `"caret^2"`},
		{name: "braces are safe inside quotes", in: "{near}", want: `"{near}"`},
		{name: "brackets are safe inside quotes", in: "[bracket]", want: `"[bracket]"`},
		{name: "asterisk is a literal, not a wildcard", in: "wild*card", want: `"wild*card"`},
		{
			name:  "#75: a filename no longer breaks the parser",
			in:    "file.txt",
			want:  `"file.txt"`,
			notes: "the period used to reach FTS5 as a bareword and produce a syntax error",
		},
		{name: "version strings are safe", in: "v1.2.3", want: `"v1.2.3"`},
		{name: "decimals are safe", in: "3.14", want: `"3.14"`},
		{name: "runs of whitespace collapse", in: "  spaced   out  ", want: `"spaced" "out"`},
		{name: "empty input stays empty", in: "", want: ""},
		{name: "a query of only special characters is a literal that matches nothing", in: "***", want: `"***"`},
		{name: "leading hyphen is a literal", in: "-only", want: `"-only"`},
		{
			name:  "#75: FTS5 boolean operators are neutralised into literals",
			in:    "foo NOT bar",
			want:  `"foo" "NOT" "bar"`,
			notes: "NOT/AND/OR/NEAR typed by a user are search terms, not operators",
		},
		{name: "embedded double quote is escaped by doubling", in: `say "hi`, want: `"say" "hi"`,
			notes: "an unbalanced quote is not a phrase delimiter; it is dropped as whitespace"},
		{name: "unicode is preserved", in: "café naïve", want: `"café" "naïve"`},
		{name: "digits are preserved", in: "score 1863", want: `"score" "1863"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFTSQuery(tt.in); got != tt.want {
				t.Errorf("sanitizeFTSQuery(%q) = %q, want %q\n%s", tt.in, got, tt.want, tt.notes)
			}
		})
	}
}

func TestPrepareFTSQuery(t *testing.T) {
	s := NewSearcher(nil) // prepareFTSQuery never touches the database

	tests := []struct {
		name  string
		in    string
		want  string
		notes string
	}{
		{name: "single term gets a prefix wildcard", in: "lantern", want: `"lantern"*`},
		{name: "one-character term gets no wildcard", in: "a", want: `"a"`},
		{name: "two-character term does get a wildcard", in: "ab", want: `"ab"*`},
		{
			name:  "only the LAST term of a multi-term query gets a wildcard",
			in:    "hello world",
			want:  `"hello" "world"*`,
			notes: "terms are ANDed implicitly by FTS5; earlier terms must match exactly",
		},
		{
			name:  "#75: a term containing a period now gets its wildcard too",
			in:    "file.txt",
			want:  `"file.txt"*`,
			notes: "the old rule refused a wildcard on anything containing '.', because a bareword with '.' did not parse",
		},
		{
			name:  "a phrase the user quoted stays exact",
			in:    `"exact phrase"`,
			want:  `"exact phrase"`,
			notes: "the user asked for that literal text, so no wildcard is added",
		},
		{name: "apostrophe stays inside its word and the word is wildcarded", in: "it's fine", want: `"it's" "fine"*`},
		{name: "empty query prepares to empty", in: "", want: ""},
		{name: "special-characters-only query has nothing to prefix", in: "***", want: `"***"`},
		{name: "hyphenated term keeps its hyphen and gets the wildcard", in: "self-objectification", want: `"self-objectification"*`},
		{
			name:  "a quoted phrase followed by a bare word wildcards only the bare word",
			in:    `"big science" lant`,
			want:  `"big science" "lant"*`,
			notes: "mixed phrase and term queries",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.prepareFTSQuery(tt.in); got != tt.want {
				t.Errorf("prepareFTSQuery(%q) = %q, want %q\n%s", tt.in, got, tt.want, tt.notes)
			}
		})
	}
}

// TestIssue75_OrdinaryTextNeverSyntaxErrors is the enforcement half of #75.
//
// Was: the sanitizer's doc comment listed '.' as a character that breaks FTS5,
// but the strip list omitted it. Any query containing a filename, a version
// string or a decimal reached SQLite as a bareword and failed with a syntax
// error, which the hybrid layer then swallowed into "no results, confidence
// none" -- indistinguishable from a genuine miss.
func TestIssue75_OrdinaryTextNeverSyntaxErrors(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	// Every one of these used to be a syntax error or a silently reinterpreted
	// boolean expression.
	hostile := []string{
		"file.txt", "3.14", "main.go", "v1.2.3", "ASL-3", "self-objectification",
		"AND", "OR", "NOT", "NEAR", "***", "-only", "(group)", "title:foo",
		"a+b", "caret^2", "{near}", "[bracket]", "wild*card", `unbalanced " quote`,
		"it's", "O'Brien", "don't", "", "   ", "café naïve",
		"NEAR(a b)", "a OR b", "col:val AND other",
	}

	for _, q := range hostile {
		t.Run(q, func(t *testing.T) {
			if _, err := gi.Searcher.Search(ctx, q, SearchOptions{Limit: 5}); err != nil {
				t.Errorf("Search(%q) returned an FTS5 error: %v", q, err)
			}
			res, err := gi.Hybrid.Search(ctx, q, HybridSearchOptions{Limit: 5})
			if err != nil {
				t.Errorf("hybrid Search(%q): %v", q, err)
				return
			}
			if res.DegradedMode {
				t.Errorf("hybrid Search(%q) reported degraded mode; nothing should have failed", q)
			}
		})
	}
}

// TestIssue75_DotsAndVersionsReturnResults is the positive half: these queries
// do not merely avoid erroring, they find the documents they name.
func TestIssue75_DotsAndVersionsReturnResults(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	// The corpus has no filenames in its text, so index one document that does.
	src := ""
	if err := gi.DB.QueryRowContext(ctx, `SELECT source_id FROM kb_documents LIMIT 1`).Scan(&src); err != nil {
		t.Fatalf("read source id: %v", err)
	}
	doc := &Document{
		DocumentID: "08-release-notes",
		SourceID:   src,
		Path:       "/corpus/08-release-notes.txt",
		Title:      "Release Notes",
		MimeType:   "text/plain",
	}
	body := "Release v1.2.3 rewrites main.go and raises the threshold to 3.14 for ASL-3 deployments."
	if err := gi.Indexer.Index(ctx, doc, gi.Chunker.Chunk(body, goldenChunkOptions)); err != nil {
		t.Fatalf("index: %v", err)
	}

	for _, q := range []string{"main.go", "v1.2.3", "3.14", "ASL-3"} {
		t.Run(q, func(t *testing.T) {
			res, err := gi.Searcher.Search(ctx, q, SearchOptions{Limit: 5})
			if err != nil {
				t.Fatalf("Search(%q): %v", q, err)
			}
			if len(res.Results) == 0 {
				t.Fatalf("Search(%q) found nothing; the document containing it is indexed", q)
			}
			if res.Results[0].DocumentID != "08-release-notes" {
				t.Errorf("Search(%q) rank 1 = %s, want 08-release-notes", q, res.Results[0].DocumentID)
			}
		})
	}
}

// TestIssue75_BooleanOperatorsAreLiterals is the other direction: a user who
// types AND / OR / NOT / NEAR gets those words searched for, not an operator.
func TestIssue75_BooleanOperatorsAreLiterals(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	// "people NOT nation" used to be parsed as an FTS5 NOT and return FEWER
	// results than "people" alone. As a literal three-word AND it returns none,
	// because no chunk contains all three words -- but it must not return the
	// boolean answer, and it must not error.
	withNot, err := gi.Searcher.Search(ctx, "people NOT nation", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	notOnly, err := gi.Searcher.Search(ctx, "NOT", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(notOnly.Results) == 0 {
		t.Errorf(`searching for the word "NOT" should match the corpus text containing it`)
	}
	// The literal reading requires the word "not" to be present in the chunk,
	// which is strictly narrower than the boolean reading.
	for _, hit := range withNot.Results {
		if !strings.Contains(strings.ToLower(hit.Snippet), "not") {
			t.Errorf("hit %s does not contain the literal term 'not': %q", hit.ChunkID, hit.Snippet)
		}
	}
}

// TestIssue75_FTSFailureIsSignalled covers the last clause of #75: if an FTS
// error does escape, the caller must be told, not handed a silent empty result.
//
// The searcher here is bound to a database with no kb_fts table at all, which
// is the one remaining way to make the lexical half fail outright.
func TestIssue75_FTSFailureIsSignalled(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `DROP TABLE kb_fts`); err != nil {
		t.Fatalf("drop kb_fts: %v", err)
	}

	hs := NewHybridSearcher(NewSearcher(db), nil)

	t.Run("fusion", func(t *testing.T) {
		res, err := hs.Search(ctx, "lantern", HybridSearchOptions{Limit: 5})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if !res.DegradedMode {
			t.Errorf("DegradedMode is false even though the only strategy failed")
		}
		if res.Confidence != "none" {
			t.Errorf("confidence = %q, want %q", res.Confidence, "none")
		}
		if !strings.Contains(res.Note, "Lexical search failed") {
			t.Errorf("note does not name the failure: %q", res.Note)
		}
	})

	t.Run("lexical mode", func(t *testing.T) {
		res, err := hs.Search(ctx, `"exact phrase"`, HybridSearchOptions{Limit: 5})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Mode != HybridModeLexical {
			t.Fatalf("expected lexical mode, got %s", res.Mode)
		}
		if !res.DegradedMode || res.Confidence != "none" || !strings.Contains(res.Note, "Lexical search failed") {
			t.Errorf("lexical-mode failure was not signalled: degraded=%v confidence=%q note=%q",
				res.DegradedMode, res.Confidence, res.Note)
		}
	})
}

// TestSearcher_Suggest pins the suggestion path, which used to bypass the
// sanitizer entirely and concatenate the raw prefix with '*'.
func TestSearcher_Suggest(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	got, err := gi.Searcher.Suggest(ctx, "lantern", 5)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	want := []string{"Lantern Keeper Notes", "Harbour Ledger"}
	if !equalStrings(sortedCopy(got), sortedCopy(want)) {
		t.Errorf("Suggest(\"lantern\") = %v, want %v (any order)", got, want)
	}

	// #75: unsanitized input used to reach FTS5 directly and raise a syntax
	// error. It is now a miss.
	res, err := gi.Searcher.Suggest(ctx, "lant(ern", 5)
	if err != nil {
		t.Errorf("Suggest with punctuation should not error: %v", err)
	}
	if len(res) != 0 {
		t.Errorf("Suggest(%q) = %v, want no suggestions", "lant(ern", res)
	}
	if _, err := gi.Searcher.Suggest(ctx, "", 5); err != nil {
		t.Errorf("Suggest with an empty prefix should not error: %v", err)
	}
}

// TestSearcher_SearchByPath pins the non-FTS lookup path.
func TestSearcher_SearchByPath(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	docs, err := gi.Searcher.SearchByPath(ctx, "/corpus/0", 20)
	if err != nil {
		t.Fatalf("SearchByPath: %v", err)
	}
	if len(docs) != 7 {
		t.Errorf("SearchByPath(\"/corpus/0\") returned %d documents, want 7", len(docs))
	}

	none, err := gi.Searcher.SearchByPath(ctx, "/nowhere/", 20)
	if err != nil {
		t.Fatalf("SearchByPath: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected no documents under /nowhere/, got %d", len(none))
	}
}
