package kb

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// Indexing guidance for an empty search result (issue #97).
//
// A user ran `conduit kb add` twice, skipped `conduit kb sync`, searched, and
// got:
//
//	No results found for: authentication
//
// Which was true, and useless. Their documents were not missing; they had never
// been indexed. Nothing in the output distinguished "your knowledge base does
// not contain this" from "your knowledge base is empty because you have not run
// the second command yet".
//
// The distinction is cheap to make -- kb_sources carries last_sync, NULL until
// the first sync, and chunk_count -- and it is only made when a search returns
// nothing, so it costs nothing on the hot path.
//
// It lives in internal/kb rather than in either frontend because the CLI and
// the MCP server are peers and both need the identical sentence. An AI client
// calling kb_search against an unsynced knowledge base will otherwise report,
// with complete confidence, that the user's documents do not mention the thing
// they asked about.

// IndexGuidance describes the indexing state behind a zero-hit search.
type IndexGuidance struct {
	// TotalSources is how many sources are configured.
	TotalSources int `json:"total_sources"`

	// NeverSynced names sources that have never been indexed at all.
	NeverSynced []string `json:"never_synced,omitempty"`

	// EmptyIndexed names sources that were synced but hold no chunks -- an
	// empty directory, or include/exclude patterns that matched nothing.
	EmptyIndexed []string `json:"empty_indexed,omitempty"`
}

// BuildIndexGuidance classifies sources by indexing state.
//
// A source counts as never synced when LastSync is the zero time, which is how
// SourceManager surfaces a NULL last_sync. last_sync and the chunk counts are
// written together by updateSourceStats, so the two signals cannot disagree.
func BuildIndexGuidance(sources []*Source) *IndexGuidance {
	g := &IndexGuidance{TotalSources: len(sources)}
	for _, src := range sources {
		if src == nil {
			continue
		}
		switch {
		case src.LastSync.IsZero():
			g.NeverSynced = append(g.NeverSynced, src.Name)
		case src.ChunkCount == 0:
			g.EmptyIndexed = append(g.EmptyIndexed, src.Name)
		}
	}
	return g
}

// Actionable reports whether there is anything worth telling the user.
//
// False means the knowledge base is populated and the query genuinely found
// nothing. That is a legitimate answer and must not be dressed up as a
// configuration problem -- a note on every empty search would be noise, and
// noise is how a real warning gets ignored.
func (g *IndexGuidance) Actionable() bool {
	if g == nil {
		return false
	}
	return g.TotalSources == 0 || len(g.NeverSynced) > 0 || len(g.EmptyIndexed) > 0
}

// Message renders the guidance, or "" when there is nothing to add.
func (g *IndexGuidance) Message() string {
	if !g.Actionable() {
		return ""
	}

	if g.TotalSources == 0 {
		return "This knowledge base has no sources. Add one with " +
			"`conduit kb add <path>`, then run `conduit kb sync` to index it."
	}

	var b strings.Builder

	switch {
	case len(g.NeverSynced) == g.TotalSources:
		fmt.Fprintf(&b, "Nothing has been indexed yet: %s never been synced",
			plural(len(g.NeverSynced), "this source has", "these sources have"))
	case len(g.NeverSynced) > 0:
		fmt.Fprintf(&b, "%d of %d sources have never been synced",
			len(g.NeverSynced), g.TotalSources)
	default:
		fmt.Fprintf(&b, "Every source has been synced, but %s no indexed content",
			plural(len(g.EmptyIndexed), "one holds", "some hold"))
	}

	if names := g.names(); names != "" {
		fmt.Fprintf(&b, " (%s)", names)
	}
	b.WriteString(". ")

	if len(g.NeverSynced) > 0 {
		fmt.Fprintf(&b, "Run `conduit kb sync` to index %s, then search again. "+
			"Until then this result says nothing about whether the content exists.",
			plural(len(g.NeverSynced), "it", "them"))
	} else {
		b.WriteString("Run `conduit kb sync` to re-index, and check that the " +
			"source path still holds files Conduit can read.")
	}

	return b.String()
}

// names lists the affected sources, capped so the note stays a sentence.
func (g *IndexGuidance) names() string {
	const maxNames = 5

	all := append(append([]string(nil), g.NeverSynced...), g.EmptyIndexed...)
	sort.Strings(all)
	if len(all) == 0 {
		return ""
	}
	if len(all) > maxNames {
		return strings.Join(all[:maxNames], ", ") +
			fmt.Sprintf(" and %d more", len(all)-maxNames)
	}
	return strings.Join(all, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// IndexGuidance reads source state and classifies it.
func (sm *SourceManager) IndexGuidance(ctx context.Context) (*IndexGuidance, error) {
	sources, err := sm.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	return BuildIndexGuidance(sources), nil
}

// IndexGuidanceNote is the convenience form: the sentence to append to a
// no-results message, or "" when the knowledge base is healthy and the query
// simply missed.
//
// It swallows its error on purpose. This runs on a path that has already
// succeeded -- the search worked and found nothing -- and failing to build an
// advisory note is not a reason to turn a valid empty result into an error.
func (sm *SourceManager) IndexGuidanceNote(ctx context.Context) string {
	g, err := sm.IndexGuidance(ctx)
	if err != nil {
		return ""
	}
	return g.Message()
}
