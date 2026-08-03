package kb

// Table tests for the FTS5 query pipeline: sanitizeFTSQuery (strip characters
// that make FTS5 throw) followed by Searcher.prepareFTSQuery (add prefix
// wildcards). These pin CURRENT behaviour, including the places where the
// current behaviour is wrong.

import (
	"context"
	"testing"
)

func TestSanitizeFTSQuery(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  string
		notes string
	}{
		{name: "plain words are untouched", in: "hello world", want: "hello world"},
		{name: "double quotes are deleted", in: `"exact phrase"`, want: "exact phrase"},
		{name: "single quote becomes a space, splitting the word", in: "it's fine", want: "it s fine",
			notes: "this is why possessives produce a stray one-letter term"},
		{name: "hyphen becomes a space so it is not read as NOT", in: "ASL-3", want: "ASL 3"},
		{name: "compound word is split on the hyphen", in: "self-objectification", want: "self objectification"},
		{name: "colon is removed to avoid column-specifier parsing", in: "title:foo", want: "title foo"},
		{name: "plus is removed", in: "a+b", want: "a b"},
		{name: "parentheses are removed", in: "(group)", want: "group"},
		{name: "caret is removed", in: "caret^2", want: "caret 2"},
		{name: "braces are removed", in: "{near}", want: "near"},
		{name: "brackets are removed", in: "[bracket]", want: "bracket"},
		{name: "asterisk is removed even when the user meant a prefix", in: "wild*card", want: "wild card"},
		{
			name:  "BUG SURFACE: the period is documented as special but never stripped",
			in:    "file.txt",
			want:  "file.txt",
			notes: "the sanitizer's own doc comment lists '.' as a character that breaks FTS5, but the strip list omits it; see TestFTSQuery_PeriodProducesSyntaxError",
		},
		{name: "runs of whitespace collapse", in: "  spaced   out  ", want: "spaced out"},
		{name: "empty input stays empty", in: "", want: ""},
		{name: "a query of only special characters collapses to empty", in: "***", want: ""},
		{name: "leading hyphen is dropped", in: "-only", want: "only"},
		{
			name:  "BUG SURFACE: FTS5 boolean operators survive sanitization",
			in:    "foo NOT bar",
			want:  "foo NOT bar",
			notes: "NOT/AND/OR/NEAR are passed through to the FTS5 parser as operators, so a user query containing them is silently reinterpreted",
		},
		{name: "unicode is preserved", in: "café naïve", want: "café naïve"},
		{name: "digits are preserved", in: "score 1863", want: "score 1863"},
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
		{name: "single term gets a prefix wildcard", in: "lantern", want: "lantern*"},
		{name: "one-character term gets no wildcard", in: "a", want: "a"},
		{name: "two-character term does get a wildcard", in: "ab", want: "ab*"},
		{
			name:  "only the LAST term of a multi-term query gets a wildcard",
			in:    "hello world",
			want:  "hello world*",
			notes: "terms are ANDed implicitly by FTS5; earlier terms must match exactly",
		},
		{name: "term containing a period never gets a wildcard", in: "file.txt", want: "file.txt"},
		{name: "quotes are stripped before wildcarding", in: `"exact phrase"`, want: "exact phrase*"},
		{name: "apostrophe splits the term and the fragment is ANDed in", in: "it's fine", want: "it s fine*"},
		{name: "empty query prepares to empty", in: "", want: ""},
		{name: "special-characters-only query prepares to empty", in: "***", want: ""},
		{
			name:  "KNOWN BUG #73 SURFACE: user-supplied wildcards are stripped, then only the last term is re-wildcarded",
			in:    "lanter* OR ledg*",
			want:  "lanter OR ledg*",
			notes: "this is exactly what happens to the query built by searchRelaxed",
		},
		{name: "hyphenated term is split and the tail gets the wildcard", in: "self-objectification", want: "self objectification*"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := s.prepareFTSQuery(tt.in); got != tt.want {
				t.Errorf("prepareFTSQuery(%q) = %q, want %q\n%s", tt.in, got, tt.want, tt.notes)
			}
		})
	}
}

// TestFTSQuery_PeriodProducesSyntaxError pins a defect that is NOT one of the
// five tracked issues (#69-#73): the sanitizer leaves '.' in place, so any
// query containing a filename or a decimal number fails at the SQLite layer.
// Searcher.Search surfaces the error; HybridSearcher swallows it and reports
// zero results with confidence "none".
//
// This is a candidate sixth bug. It is pinned, not fixed.
func TestFTSQuery_PeriodProducesSyntaxError(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	for _, q := range []string{"file.txt", "3.14", "main.go"} {
		t.Run(q, func(t *testing.T) {
			_, err := gi.Searcher.Search(ctx, q, SearchOptions{Limit: 5})
			if err == nil {
				t.Fatalf("expected an FTS5 syntax error for %q; if this now passes, the sanitizer was fixed and this test should become a positive assertion", q)
			}

			// The hybrid layer turns the error into silence.
			res, herr := gi.Hybrid.Search(ctx, q, HybridSearchOptions{Limit: 5})
			if herr != nil {
				t.Fatalf("hybrid Search returned an error, which it never used to: %v", herr)
			}
			if res.TotalHits != 0 || res.Confidence != "none" {
				t.Errorf("hybrid result for %q: hits=%d confidence=%q, want 0/\"none\"", q, res.TotalHits, res.Confidence)
			}
			// Pinned deliberately: DegradedMode stays false even though the
			// only available strategy failed outright. It is reserved for
			// semantic failures, so a caller cannot distinguish "your query
			// was rejected" from "nothing matched".
			if res.DegradedMode {
				t.Errorf("DegradedMode became true for %q; it used to stay false on an FTS5 failure", q)
			}
		})
	}
}

// TestFTSQuery_BareBooleanOperatorsError is the same class of defect reached
// from a different direction: a query that is nothing but an FTS5 keyword.
func TestFTSQuery_BareBooleanOperatorsError(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	// AND / OR / NOT become "AND*" / "OR*" / "NOT*" which FTS5 rejects.
	for _, q := range []string{"AND", "OR", "NOT"} {
		if _, err := gi.Searcher.Search(ctx, q, SearchOptions{Limit: 5}); err == nil {
			t.Errorf("expected a syntax error for the bare operator %q", q)
		}
	}

	// NEAR is accepted by the parser, so it merely returns nothing.
	if _, err := gi.Searcher.Search(ctx, "NEAR", SearchOptions{Limit: 5}); err != nil {
		t.Errorf("NEAR is currently parseable; got %v", err)
	}

	// And an operator embedded in an otherwise ordinary query silently changes
	// its meaning rather than being treated as a search term.
	withNot, err := gi.Searcher.Search(ctx, "people NOT nation", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	plain, err := gi.Searcher.Search(ctx, "people", SearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(withNot.Results) >= len(plain.Results) {
		t.Errorf("expected 'people NOT nation' to be interpreted as an FTS5 NOT and return fewer hits than 'people'; got %d vs %d",
			len(withNot.Results), len(plain.Results))
	}
}

// TestSearcher_Suggest pins the suggestion path, which bypasses the sanitizer
// entirely and concatenates the raw prefix with '*'.
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

	// Unsanitized input reaches FTS5 directly.
	if _, err := gi.Searcher.Suggest(ctx, "lant(ern", 5); err == nil {
		t.Errorf("expected Suggest to propagate an FTS5 syntax error for unsanitized input")
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
