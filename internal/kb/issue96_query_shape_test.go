package kb

// Regression tests for GitHub issue #96: filler words zeroed out lexical
// search.
//
// These run against the golden corpus (internal/kb/testdata/corpus) through the
// same ingestGoldenCorpus harness as golden_retrieval_test.go, so a natural
// language question is a golden retrieval case like any other. They belong with
// the #69-#77 set in known_bugs_test.go in spirit; they live in their own file
// only because that file's header scopes it to the earlier batch.
//
// Do not delete a test here. If one starts failing, #96 has regressed.

import (
	"context"
	"strings"
	"testing"
)

// TestIssue96_QuestionFormMatchesKeywordForm is the reported defect.
//
// Was: FTS5 joins juxtaposed terms with an implicit AND, and kb_fts is created
// with tokenize='porter unicode61', which carries no stopword list. So
// "how do tokens expire" was compiled to `"how" "do" "tokens" "expire"*` and
// required the chunk to literally contain the words "how" and "do". The
// reporter got 0 hits for the question and 1 hit for "tokens expire" -- same
// knowledge base, same subject, and nothing to explain the difference.
//
// The invariant: asking a question returns what asking in keywords returns.
func TestIssue96_QuestionFormMatchesKeywordForm(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	cases := []struct {
		name     string
		question string
		keywords string
	}{
		{"when-is", "when is the lantern trimmed", "lantern trimmed"},
		{"who-does", "who signs the harbour ledger", "signs harbour ledger"},
		{"what-is-in", "what is in the ledger", "ledger"},
		{"why-is-at", "why is a lantern trimmed at midnight", "lantern trimmed midnight"},
		{"how-does", "how does the keeper count the ships", "keeper count ships"},
		{"what-did", "what did our fathers do", "fathers"},
		// "all" is NOT scaffolding -- it is a quantifier that changes what the
		// user asked for -- so the keyword form has to keep it. Dropping it
		// reorders these two documents, which is the filter's boundary working.
		{"are-all", "are all men created equal", "all men created equal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			asked, err := gi.Searcher.Search(ctx, tc.question, SearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q): %v", tc.question, err)
			}
			keyed, err := gi.Searcher.Search(ctx, tc.keywords, SearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q): %v", tc.keywords, err)
			}

			if len(keyed.Results) == 0 {
				t.Fatalf("test is inert: the keyword form %q finds nothing", tc.keywords)
			}
			if len(asked.Results) == 0 {
				t.Fatalf("#96: question %q found nothing, but keyword form %q found %d",
					tc.question, tc.keywords, len(keyed.Results))
			}
			if got, want := docIDs(asked.Results), docIDs(keyed.Results); !equalStrings(got, want) {
				t.Errorf("question and keyword forms disagree.\nquestion %q -> %v\nkeywords %q -> %v",
					tc.question, got, tc.keywords, want)
			}
		})
	}
}

// TestIssue96_QuestionIsAnsweredByThePrimaryRung is the half that "it returns
// something" does not cover.
//
// The fallback ladder ALREADY rescued these queries: searchRelaxed OR-s the
// terms, so "how do tokens expire" came back at FallbackLevel 1 with
// Confidence "low" and the note "Using relaxed matching - verify relevance",
// ranked alongside any document that merely contained the word "how". Two
// things were wrong with that. SearchWithFallback is reachable only from the
// MCP server -- internal/kbservice calls hybrid.Search -- so a CLI user got a
// bare zero and no ladder at all. And a well-formed question about indexed
// content is not a low-confidence guess; labelling it one teaches the caller to
// distrust a correct answer.
//
// A natural-language question must be answered by the PRIMARY rung.
func TestIssue96_QuestionIsAnsweredByThePrimaryRung(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	for _, q := range []string{
		"when is the lantern trimmed",
		"how does the keeper count the ships",
		"are all men created equal",
		"why is a lantern trimmed at midnight",
	} {
		t.Run(q, func(t *testing.T) {
			// Rung 0 on its own, with no ladder underneath to hide a miss.
			plain, err := gi.Hybrid.Search(ctx, q, HybridSearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("Search(%q): %v", q, err)
			}
			if len(plain.Results) == 0 {
				t.Fatalf("#96: %q needs the fallback ladder to return anything; "+
					"the primary lexical rung must handle a question form", q)
			}

			res, err := gi.Hybrid.SearchWithFallback(ctx, q, HybridSearchOptions{Limit: 10})
			if err != nil {
				t.Fatalf("SearchWithFallback(%q): %v", q, err)
			}
			if res.FallbackLevel != 0 {
				t.Errorf("%q answered at fallback level %d (%q); want level 0",
					q, res.FallbackLevel, res.Note)
			}
			if res.Confidence == "low" || res.Confidence == "speculative" {
				t.Errorf("%q reported confidence %q; a question about indexed "+
					"content is not a guess", q, res.Confidence)
			}
		})
	}
}

// TestIssue96_ScaffoldFilterInvariants pins the rules that keep the filter from
// doing damage.
func TestIssue96_ScaffoldFilterInvariants(t *testing.T) {
	s := NewSearcher(nil) // prepareFTSQuery never touches the database

	t.Run("a query of nothing but scaffolding is left alone", func(t *testing.T) {
		// Dropping every token would turn "the" into a match-all. A search for a
		// function word is a legitimate, if unusual, search for that word.
		for in, want := range map[string]string{
			"the":     `"the"*`,
			"a":       `"a"`,
			"how":     `"how"*`,
			"what is": `"what" "is"*`,
		} {
			if got := s.prepareFTSQuery(in); got != want {
				t.Errorf("prepareFTSQuery(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("a quoted scaffold word survives", func(t *testing.T) {
		// Quoting is the documented escape hatch (#70, #75): it means "I mean
		// this literally", and it is how a user searches for the word "how".
		for in, want := range map[string]string{
			`"the" lantern`:  `"the" "lantern"*`,
			`"how to" guide`: `"how to" "guide"*`,
		} {
			if got := s.prepareFTSQuery(in); got != want {
				t.Errorf("prepareFTSQuery(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("#75 literals and RFC-2119 modals are not scaffolding", func(t *testing.T) {
		// NOT / AND / OR / NEAR are search terms a user typed on purpose (#75).
		// may / must / should / shall are load-bearing vocabulary in exactly the
		// specification documents Conduit is pointed at.
		for in, want := range map[string]string{
			"foo NOT bar": `"foo" "NOT" "bar"*`,
			// "b" is one rune, so ftsPrefixable declines the wildcard; the
			// point here is only that "OR" survived as a literal.
			"a OR b":           `"OR" "b"`,
			"MUST NOT reuse":   `"MUST" "NOT" "reuse"*`,
			"should retry":     `"should" "retry"*`,
			"may cache":        `"may" "cache"*`,
			"it is idempotent": `"it" "idempotent"*`,
		} {
			if got := s.prepareFTSQuery(in); got != want {
				t.Errorf("prepareFTSQuery(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("the filter never applies to a phrase token", func(t *testing.T) {
		toks := []ftsToken{{text: "the", phrase: true}, {text: "of", phrase: true}}
		if got := contentFTSTokens(toks); len(got) != 2 {
			t.Errorf("contentFTSTokens dropped a quoted phrase: %v", got)
		}
	})
}

// TestIssue96_FilteringOnlyWidensResults executes the safety argument.
//
// Removing a conjunct from an implicit AND can only ever return a superset of
// what the full conjunction returned, which is why this fix cannot break a
// query that used to work. What it must NOT do is stop requiring the content
// words -- that would turn a precise search into a fuzzy one.
func TestIssue96_FilteringOnlyWidensResults(t *testing.T) {
	gi := ingestGoldenCorpus(t)
	ctx := context.Background()

	for _, q := range []string{
		"the lantern", "of the people", "in the ledger",
		"the lantern is trimmed", "this rabbit hole",
	} {
		t.Run(q, func(t *testing.T) {
			res, err := gi.Searcher.Search(ctx, q, SearchOptions{Limit: 20})
			if err != nil {
				t.Fatalf("Search(%q): %v", q, err)
			}
			if len(res.Results) == 0 {
				t.Fatalf("%q returned nothing; the content words are in the corpus", q)
			}
			// Every surviving content word is still a hard conjunct. Only the
			// last one carries a prefix wildcard, so check the others exactly.
			toks := contentFTSTokens(splitFTSQuery(q))
			for i, tk := range toks {
				if i == len(toks)-1 {
					continue // wildcarded: "trimmed" may match "trimmed", "trimming", ...
				}
				for _, hit := range res.Results {
					hay := strings.ToLower(hit.Snippet + " " + hit.Title + " " + hit.Path)
					if !strings.Contains(hay, strings.ToLower(tk.text)) {
						t.Errorf("hit %s does not contain content term %q: %q",
							hit.ChunkID, tk.text, hit.Snippet)
					}
				}
			}
		})
	}
}
