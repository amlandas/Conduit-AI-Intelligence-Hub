package kb

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Regression tests for GitHub issue #97: a knowledge base that was added but
// never synced answered every search with a bare "No results found".
//
// Do not delete a test here. If one starts failing, #97 has regressed.

var synced = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestIssue97_GuidanceClassification(t *testing.T) {
	cases := []struct {
		name         string
		sources      []*Source
		wantAction   bool
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:       "a populated knowledge base says nothing",
			sources:    []*Source{{Name: "docs", LastSync: synced, ChunkCount: 42}},
			wantAction: false,
			// This is the case that must stay silent. A note on every empty
			// search is noise, and noise is how a real warning gets ignored.
		},
		{
			name:         "no sources at all",
			sources:      nil,
			wantAction:   true,
			wantContains: []string{"no sources", "conduit kb add", "conduit kb sync"},
		},
		{
			name:         "the reported case: added, never synced",
			sources:      []*Source{{Name: "docs"}, {Name: "notes"}},
			wantAction:   true,
			wantContains: []string{"Nothing has been indexed yet", "docs", "notes", "conduit kb sync"},
		},
		{
			name: "one of several never synced",
			sources: []*Source{
				{Name: "docs", LastSync: synced, ChunkCount: 10},
				{Name: "notes"},
			},
			wantAction:   true,
			wantContains: []string{"1 of 2 sources have never been synced", "notes", "conduit kb sync"},
			wantAbsent:   []string{"Nothing has been indexed yet"},
		},
		{
			name:         "synced but empty is a different sentence",
			sources:      []*Source{{Name: "docs", LastSync: synced, ChunkCount: 0}},
			wantAction:   true,
			wantContains: []string{"has been synced", "no indexed content", "conduit kb sync"},
			wantAbsent:   []string{"never been synced"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := BuildIndexGuidance(tc.sources)
			if g.Actionable() != tc.wantAction {
				t.Fatalf("Actionable() = %v, want %v", g.Actionable(), tc.wantAction)
			}

			msg := g.Message()
			if !tc.wantAction {
				if msg != "" {
					t.Errorf("a healthy knowledge base produced guidance: %q", msg)
				}
				return
			}
			if msg == "" {
				t.Fatal("actionable guidance produced no message")
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(msg, want) {
					t.Errorf("message is missing %q:\n%s", want, msg)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(msg, absent) {
					t.Errorf("message should not contain %q:\n%s", absent, msg)
				}
			}
		})
	}
}

// TestIssue97_GuidanceNamesTheCommand: whatever the wording, the note is
// useless without the command that fixes it.
func TestIssue97_GuidanceNamesTheCommand(t *testing.T) {
	for _, sources := range [][]*Source{
		nil,
		{{Name: "a"}},
		{{Name: "a", LastSync: synced}, {Name: "b"}},
		{{Name: "a", LastSync: synced, ChunkCount: 0}},
	} {
		msg := BuildIndexGuidance(sources).Message()
		if !strings.Contains(msg, "conduit kb sync") {
			t.Errorf("guidance for %d source(s) never names `conduit kb sync`:\n%s",
				len(sources), msg)
		}
	}
}

// TestIssue97_NameListIsCapped keeps the note a sentence rather than a dump.
func TestIssue97_NameListIsCapped(t *testing.T) {
	var sources []*Source
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		sources = append(sources, &Source{Name: n})
	}

	msg := BuildIndexGuidance(sources).Message()
	if !strings.Contains(msg, "and 2 more") {
		t.Errorf("seven never-synced sources were not summarised:\n%s", msg)
	}
	if strings.Contains(msg, "g") && !strings.Contains(msg, "and 2 more") {
		t.Errorf("name list was not capped:\n%s", msg)
	}
}

// TestIssue97_SourceManagerReadsRealSyncState goes through the database, so a
// change to how last_sync is stored or scanned is caught.
func TestIssue97_SourceManagerReadsRealSyncState(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	sm := NewSourceManager(db)
	if _, err := sm.Add(ctx, AddSourceRequest{
		Path: t.TempDir(), Name: "never-synced", SyncMode: "manual",
	}); err != nil {
		t.Fatalf("add source: %v", err)
	}

	g, err := sm.IndexGuidance(ctx)
	if err != nil {
		t.Fatalf("IndexGuidance: %v", err)
	}
	if len(g.NeverSynced) != 1 || g.NeverSynced[0] != "never-synced" {
		t.Fatalf("a freshly added source is not reported as never synced: %+v", g)
	}
	if note := sm.IndexGuidanceNote(ctx); !strings.Contains(note, "conduit kb sync") {
		t.Errorf("IndexGuidanceNote does not name the fix: %q", note)
	}

	// Once the row carries a sync timestamp and chunks, the note must go away.
	if _, err := db.ExecContext(ctx,
		`UPDATE kb_sources SET last_sync = datetime('now'), chunk_count = 7`); err != nil {
		t.Fatalf("mark synced: %v", err)
	}
	if note := sm.IndexGuidanceNote(ctx); note != "" {
		t.Errorf("a synced, populated source still produced guidance: %q", note)
	}
}
