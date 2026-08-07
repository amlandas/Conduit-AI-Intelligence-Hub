package cli

// Doctor rendering for the embedding-model identity stamp (WP-4.3, #107).
//
// renderEmbedStampCheck is a pure function of the comparison state, so every
// state a user can be in gets an assertion here -- including the ones that are
// awkward to reach on a real machine, such as an upgraded knowledge base whose
// vectors were never stamped.

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/embed"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/kbservice"
)

func stampIdentity(model string, dim int) kb.EmbeddingIdentity {
	return kb.NewEmbeddingIdentity(model, "llama-server", dim, embed.PrefixSchemeNone)
}

func stampOf(model string, dim int, adopted bool) *kb.EmbeddingStamp {
	return &kb.EmbeddingStamp{
		EmbeddingIdentity: stampIdentity(model, dim),
		CreatedAt:         time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
		Adopted:           adopted,
	}
}

func TestDoctor_EmbedStampStates(t *testing.T) {
	nomic := embed.ModelNomicEmbedTextV15
	mxbai := embed.ModelMxbaiEmbedLargeV1

	cases := []struct {
		name       string
		status     *kbservice.EmbeddingStampStatus
		stored     *kb.EmbeddingStamp
		wantStatus checkStatus
		wantIn     []string
		wantRemedy bool
	}{
		{
			name:       "embeddings off, nothing ever stamped",
			status:     nil,
			wantStatus: checkSkip,
			wantIn:     []string{"embed.provider=none"},
		},
		{
			name:       "embeddings off, vectors carry a stamp",
			status:     nil,
			stored:     stampOf(nomic, 768, false),
			wantStatus: checkSkip,
			wantIn:     []string{nomic, "embed.provider=none", "2026-08-01"},
		},
		{
			name: "nothing indexed yet",
			status: &kbservice.EmbeddingStampStatus{
				Active: stampIdentity(nomic, 768), Verdict: kb.StampOK,
			},
			wantStatus: checkOK,
			wantIn:     []string{nomic, "first index"},
		},
		{
			name: "healthy",
			status: &kbservice.EmbeddingStampStatus{
				Stamp: stampOf(nomic, 768, false), Active: stampIdentity(nomic, 768),
				Verdict: kb.StampOK, Vectors: 1234,
			},
			wantStatus: checkOK,
			wantIn:     []string{nomic, "768d", "1234 vectors", "stamped 2026-08-01"},
		},
		{
			name: "adopted for legacy vectors",
			status: &kbservice.EmbeddingStampStatus{
				Stamp: stampOf(nomic, 768, true), Active: stampIdentity(nomic, 768),
				Verdict: kb.StampOK, Vectors: 10,
			},
			wantStatus: checkOK,
			wantIn:     []string{nomic, "assumed"},
		},
		{
			name: "model changed",
			status: &kbservice.EmbeddingStampStatus{
				Stamp: stampOf(nomic, 768, false), Active: stampIdentity(mxbai, 1024),
				Verdict: kb.StampMismatch, Vectors: 42,
			},
			wantStatus: checkFail,
			wantIn:     []string{nomic, mxbai, "42 vectors"},
			wantRemedy: true,
		},
		{
			name: "unprovable difference",
			status: &kbservice.EmbeddingStampStatus{
				Stamp: stampOf("home-grown", 768, false), Active: stampIdentity(nomic, 768),
				Verdict: kb.StampUnknown, Vectors: 7,
			},
			wantStatus: checkWarn,
			wantIn:     []string{"home-grown", nomic, "cannot be compared"},
			wantRemedy: true,
		},
		{
			name: "prefix scheme changed",
			status: &kbservice.EmbeddingStampStatus{
				Stamp: stampOf(nomic, 768, false), Active: stampIdentity(nomic, 768),
				Verdict: kb.StampPrefixChanged, Vectors: 7,
			},
			wantStatus: checkWarn,
			wantIn:     []string{nomic, "instruction prefixes"},
			wantRemedy: true,
		},
		{
			name: "vectors with no stamp and a width that does not match",
			status: &kbservice.EmbeddingStampStatus{
				Active: stampIdentity(nomic, 768), Verdict: kb.StampOK, Vectors: 9,
			},
			wantStatus: checkWarn,
			wantIn:     []string{"no model record", nomic},
			wantRemedy: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderEmbedStampCheck(tc.status, tc.stored)

			if got.Name != embedStampCheckName {
				t.Errorf("check name = %q, want %q", got.Name, embedStampCheckName)
			}
			if got.Status != tc.wantStatus {
				t.Errorf("status = %q, want %q (detail: %s)", got.Status, tc.wantStatus, got.Detail)
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("detail %q does not mention %q", got.Detail, want)
				}
			}
			if tc.wantRemedy {
				if got.Remedy == "" {
					t.Error("no remedy offered for a state the user has to act on")
				} else if !strings.Contains(got.Remedy, kb.RebuildRemedy) {
					t.Errorf("remedy %q does not name %q", got.Remedy, kb.RebuildRemedy)
				}
			}
			// Every state must render an icon; a blank one would print a ragged
			// line in the doctor table.
			if got.icon() == "" {
				t.Error("no icon for this state")
			}
		})
	}
}

// TestKBStats_ReportsVectorsAndModel pins F8. `kb stats` reported chunk counts
// and said nothing about how many of them were searchable by meaning, or by
// which model — leaving out the fact most likely to be wrong.
func TestKBStats_ReportsVectorsAndModel(t *testing.T) {
	env := newTestEnv(t)
	env.mustRun(t, "kb", "add", corpus(t), "--name", "docs")
	env.mustRun(t, "kb", "sync")

	out := env.mustRun(t, "kb", "stats")
	// embed.provider=none in this harness, so there is no vector space to
	// describe and the section is correctly silent rather than misleading.
	if strings.Contains(out, "Vectors:") {
		t.Errorf("kb stats described a vector space in lexical-only mode:\n%s", out)
	}
	for _, want := range []string{"Documents:", "Chunks:"} {
		if !strings.Contains(out, want) {
			t.Errorf("kb stats no longer reports %q:\n%s", want, out)
		}
	}
}

// TestKBSearch_JSONCarriesNoDegradedKeysWhenHealthy guards the contract half of
// F5: the new keys are additive and absent when nothing failed, so the shapes
// scripts already parse are unchanged.
func TestKBSearch_JSONCarriesNoDegradedKeysWhenHealthy(t *testing.T) {
	env := newTestEnv(t)
	env.mustRun(t, "kb", "add", corpus(t), "--name", "docs")
	env.mustRun(t, "kb", "sync")

	out := env.mustRun(t, "kb", "search", "authentication", "--json")
	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("kb search --json invalid: %v\n%s", err, out)
	}
	if _, present := resp["degraded"]; present {
		t.Errorf(`"degraded" present on a healthy lexical-only search: %v`, resp["degraded"])
	}
	if _, present := resp["note"]; present {
		t.Errorf(`"note" present on a healthy lexical-only search: %v`, resp["note"])
	}
}

// TestDoctor_ReportsTheStampLine is the end-to-end half: the check really is
// wired into `conduit doctor`, and on a lexical-only install it is information
// rather than a failure.
func TestDoctor_ReportsTheStampLine(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "doctor")
	if code != 0 {
		t.Fatalf("doctor exited %d\n%s", code, out)
	}
	if !strings.Contains(out, embedStampCheckName) {
		t.Errorf("doctor does not report the embedding model stamp:\n%s", out)
	}
	if strings.Contains(out, "✗ "+embedStampCheckName) {
		t.Errorf("doctor failed the stamp check on a lexical-only install:\n%s", out)
	}
}
