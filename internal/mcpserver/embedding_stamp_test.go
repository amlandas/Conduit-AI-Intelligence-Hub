package mcpserver

// What an AI client is told when the embedding model changed (WP-4.3, #107).
//
// The contract here is the one #97 established: tool names, descriptions and
// input schemas are byte-frozen -- clients are prompt-tuned against them -- but
// what a tool RETURNS is allowed to be more helpful. A model change is exactly
// the case that needs it: without a word in the result, an AI client reports to
// its user that their documents do not mention the thing they asked about, when
// in fact half the retrieval engine was switched off.

import (
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/embed"
	"github.com/simpleflo/conduit/internal/kb"
)

func TestDegradedBanner_NamesBothModelsAndTheRemedy(t *testing.T) {
	mismatch := &kb.ModelMismatchError{
		Stamped: kb.NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "llama-server", 768, embed.PrefixSchemeNone),
		Active:  kb.NewEmbeddingIdentity(embed.ModelMxbaiEmbedLargeV1, "llama-server", 1024, embed.PrefixSchemeNone),
		Op:      "semantic search",
	}

	banner := degradedBanner(&kb.HybridSearchResult{
		DegradedMode: true,
		Note:         mismatch.Note(),
	})

	if banner == "" {
		t.Fatal("no banner for a degraded result")
	}
	// The label an AI client matches on is unchanged.
	if !strings.HasPrefix(banner, "retrieval: degraded") {
		t.Errorf("banner does not open with the established label:\n%s", banner)
	}
	for _, want := range []string{
		embed.ModelNomicEmbedTextV15,
		embed.ModelMxbaiEmbedLargeV1,
		kb.RebuildRemedy,
	} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner does not mention %q:\n%s", want, banner)
		}
	}
}

func TestDegradedBanner_SilentWhenNothingFailed(t *testing.T) {
	if banner := degradedBanner(&kb.HybridSearchResult{}); banner != "" {
		t.Errorf("banner on a healthy result: %q", banner)
	}
	if banner := degradedBanner(nil); banner != "" {
		t.Errorf("banner on a nil result: %q", banner)
	}
}
