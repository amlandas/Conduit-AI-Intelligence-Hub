package kbservice

// Service-level tests for the embedding-model identity stamp (WP-4.3, #107).
//
// These run a real ingestion over a real SQLite file with a deterministic fake
// embedding provider. The fake is what makes the defect testable at all: it can
// claim to be any model at any width, so a swap between two DIFFERENT models of
// the SAME width -- the case the dimension guard cannot see -- is reproducible
// with no network, no sidecar and no model download.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/embed"
	"github.com/simpleflo/conduit/internal/kb"
)

const fakeEmbedDim = 16

// useFakeEmbedder rewires an open service onto a deterministic embedder that
// claims to be model. Calling it again with a different model is exactly what a
// user does by editing embed.model and re-running a command.
func useFakeEmbedder(t *testing.T, svc *Service, model string) *embed.FakeProvider {
	t.Helper()
	return useFakeEmbedderAt(t, svc, model, fakeEmbedDim)
}

// useFakeEmbedderAt is useFakeEmbedder with an explicit vector width, so that a
// model change that also changes width can be reproduced.
func useFakeEmbedderAt(t *testing.T, svc *Service, model string, dim int) *embed.FakeProvider {
	t.Helper()

	provider := embed.NewFakeProvider(model, dim, 7)
	embedder := kb.NewProviderEmbedder(provider)
	identity := kb.NewEmbeddingIdentity(model, "test", dim, embed.PrefixSchemeNone)

	vectors, err := kb.NewSQLiteVectorIndex(svc.db, kb.VectorIndexConfig{
		Dimension: dim,
		Identity:  identity,
	})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}

	svc.embedder = embedder
	svc.vectors = vectors
	svc.semantic = kb.NewSemanticSearcherWith(svc.db, embedder, vectors)
	svc.source.SetSemanticSearcher(svc.semantic)
	svc.indexer.SetSemanticSearcher(svc.semantic)
	svc.hybrid = kb.NewHybridSearcher(svc.searcher, svc.semantic)
	svc.embedInfo = EmbedderInfo{
		Provider:     "test",
		Model:        model,
		Dimensions:   dim,
		PrefixScheme: embed.PrefixSchemeNone,
		Available:    true,
	}
	return provider
}

// seedIndexedCorpus adds a source, syncs it, and returns the source id.
func seedIndexedCorpus(t *testing.T, svc *Service) string {
	t.Helper()
	ctx := context.Background()

	src, err := svc.AddSource(ctx, kb.AddSourceRequest{
		Path:     corpusDir(t),
		Name:     "corpus",
		Patterns: []string{"*.md"},
	})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if _, err := svc.Sync(ctx, src.SourceID, false); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	return src.SourceID
}

func vectorCount(t *testing.T, svc *Service) int64 {
	t.Helper()
	n, err := svc.VectorCount(context.Background())
	if err != nil {
		t.Fatalf("VectorCount: %v", err)
	}
	return n
}

// ---------------------------------------------------------------------------
// Ingestion
// ---------------------------------------------------------------------------

// TestSync_StampsTheKnowledgeBase pins the ordinary happy path: index a corpus,
// and the knowledge base now records what embedded it.
func TestSync_StampsTheKnowledgeBase(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)

	seedIndexedCorpus(t, svc)

	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Stamp == nil {
		t.Fatal("no stamp after a sync that produced vectors")
	}
	if status.Stamp.Canonical != embed.ModelNomicEmbedTextV15 {
		t.Errorf("stamped model = %q, want %q", status.Stamp.Canonical, embed.ModelNomicEmbedTextV15)
	}
	if status.Verdict != kb.StampOK {
		t.Errorf("verdict = %v, want StampOK", status.Verdict)
	}
	if status.Vectors == 0 {
		t.Error("no vectors were written")
	}
}

// TestSync_ModelChangeIndexesLexicalOnly is the graceful-degradation rule: a
// model change costs the vectors of the documents being indexed, and nothing
// else. The documents are still fully searchable by keyword, and the aggregate
// warning names the one command that fixes it.
func TestSync_ModelChangeIndexesLexicalOnly(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)

	dir := corpusDir(t)
	src, err := svc.AddSource(ctx, kb.AddSourceRequest{
		Path: dir, Name: "corpus", Patterns: []string{"*.md"},
	})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if _, err := svc.Sync(ctx, src.SourceID, false); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	before := vectorCount(t, svc)

	// Same width, different model.
	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)

	// A new document, so the sync has real work to do.
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n\nquokka telemetry\n"), 0600); err != nil {
		t.Fatalf("write new document: %v", err)
	}

	res, err := svc.Sync(ctx, src.SourceID, false)
	if err != nil {
		t.Fatalf("sync after a model change returned an error instead of degrading: %v", err)
	}
	if res.Added != 1 {
		t.Errorf("added = %d, want 1: the document must still be indexed lexically", res.Added)
	}
	if res.SemanticErrors == 0 {
		t.Error("semantic_errors = 0, want the refused vector write to be counted")
	}
	if !strings.Contains(res.ModelMismatch, kb.RebuildRemedy) {
		t.Errorf("model_mismatch = %q, want the rebuild remedy named", res.ModelMismatch)
	}
	if got := vectorCount(t, svc); got != before {
		t.Errorf("vector count = %d, want %d: no new-model vector may join the old space", got, before)
	}

	// The document is genuinely searchable by keyword.
	out, err := svc.Search(ctx, SearchRequest{Query: "quokka", Mode: SearchModeFTS5, Limit: 5})
	if err != nil {
		t.Fatalf("lexical search after a model change: %v", err)
	}
	if results, _ := out["results"].([]map[string]interface{}); len(results) == 0 {
		t.Error("the lexically indexed document was not findable")
	}
}

// TestSearch_HybridDegradesAfterModelChange checks the search half at the level
// a user meets it: results still arrive, and the response says why one strategy
// is missing.
func TestSearch_HybridDegradesAfterModelChange(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	seedIndexedCorpus(t, svc)

	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)

	out, err := svc.Search(ctx, SearchRequest{Query: "authentication", Limit: 5, Raw: true})
	if err != nil {
		t.Fatalf("hybrid search after a model change: %v", err)
	}
	if out["semantic_hits"] != 0 {
		t.Errorf("semantic_hits = %v, want 0: the semantic leg must be skipped", out["semantic_hits"])
	}
	if fts, _ := out["fts_hits"].(int); fts == 0 {
		t.Error("fts_hits = 0: the lexical leg must be unaffected")
	}
}

// TestSearch_DegradedNoteReachesEveryMode pins F5 and F7 together: the reason a
// leg is missing is in the RESULT, so every frontend can show it, and asking
// for semantic search explicitly does not turn a degraded answer into an error.
//
// Before this, the note existed only inside the MCP server's banner. A CLI user
// saw a shorter result list with no explanation, `--json` carried nothing at
// all, and `kb search --semantic` failed outright where the MCP server's
// mode=semantic degraded — the same condition answered two different ways by
// two frontends that are supposed to be peers.
func TestSearch_DegradedNoteReachesEveryMode(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	seedIndexedCorpus(t, svc)
	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)

	modes := []struct {
		name string
		req  SearchRequest
	}{
		{"hybrid", SearchRequest{Query: "authentication", Limit: 5, MinScore: Unset}},
		{"hybrid raw", SearchRequest{Query: "authentication", Limit: 5, MinScore: Unset, Raw: true}},
		{"semantic", SearchRequest{Query: "authentication", Mode: SearchModeSemantic, Limit: 5, MinScore: Unset}},
		{"semantic raw", SearchRequest{Query: "authentication", Mode: SearchModeSemantic, Limit: 5, MinScore: Unset, Raw: true}},
	}

	for _, tc := range modes {
		t.Run(tc.name, func(t *testing.T) {
			out, err := svc.Search(ctx, tc.req)
			if err != nil {
				t.Fatalf("search returned an error instead of degrading: %v", err)
			}
			if degraded, _ := out["degraded"].(bool); !degraded {
				t.Error(`no "degraded" key: a caller cannot tell one leg from two`)
			}
			note, _ := out["note"].(string)
			for _, want := range []string{
				embed.ModelNomicEmbedTextV15,
				embed.ModelMxbaiEmbedLargeV1,
				kb.RebuildRemedy,
			} {
				if !strings.Contains(note, want) {
					t.Errorf("note %q does not mention %q", note, want)
				}
			}
			// The lexical leg still answered, which is the whole point of
			// degrading rather than failing.
			if isEmptyResults(out["results"]) {
				t.Error("no results: degrading must still answer from the lexical leg")
			}
		})
	}
}

// TestSearch_HealthyResultCarriesNoDegradedKeys keeps the additive keys
// genuinely additive: a search with nothing wrong looks exactly as it did.
func TestSearch_HealthyResultCarriesNoDegradedKeys(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	seedIndexedCorpus(t, svc)

	for _, mode := range []string{SearchModeHybrid, SearchModeSemantic, SearchModeFTS5} {
		out, err := svc.Search(ctx, SearchRequest{
			Query: "authentication", Mode: mode, Limit: 5, MinScore: Unset,
		})
		if err != nil {
			t.Fatalf("%s search: %v", mode, err)
		}
		if _, present := out["degraded"]; present {
			t.Errorf(`%s: "degraded" present on a healthy search`, mode)
		}
		if _, present := out["note"]; present {
			t.Errorf(`%s: "note" present on a healthy search`, mode)
		}
	}
}

// ---------------------------------------------------------------------------
// Model-aware migrate
// ---------------------------------------------------------------------------

// TestMigrate_BackfillsOnlyMissingVectors pins the cheap half of `kb migrate`.
// Before WP-4.3 this pass re-embedded the entire corpus every time, which for a
// backfill is pure waste.
func TestMigrate_BackfillsOnlyMissingVectors(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	provider := useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	seedIndexedCorpus(t, svc)

	// Everything already has vectors, so there is nothing to backfill.
	callsBefore := provider.Calls()
	res, err := svc.Migrate(ctx, nil)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Total != 0 || res.Documents != 0 {
		t.Errorf("Migrate on a fully embedded corpus did %d/%d documents, want 0", res.Documents, res.Total)
	}
	if res.FullReembed {
		t.Error("FullReembed = true without a model change")
	}
	if provider.Calls() != callsBefore {
		t.Error("Migrate embedded something on a corpus that needed nothing")
	}

	// Now strip one document's vectors, as an interrupted earlier run would.
	var docID string
	if err := svc.db.QueryRowContext(ctx,
		`SELECT document_id FROM kb_documents ORDER BY document_id LIMIT 1`).Scan(&docID); err != nil {
		t.Fatalf("pick a document: %v", err)
	}
	if _, err := svc.db.ExecContext(ctx,
		`DELETE FROM kb_vectors WHERE document_id = ?`, docID); err != nil {
		t.Fatalf("strip vectors: %v", err)
	}

	res, err = svc.Migrate(ctx, nil)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Total != 1 || res.Documents != 1 {
		t.Errorf("backfill did %d of %d documents, want 1 of 1 (only the one with a gap)",
			res.Documents, res.Total)
	}
	if res.FullReembed {
		t.Error("a backfill reported itself as a full re-embed")
	}
}

// TestMigrate_ModelChangeReembedsEverything pins the expensive half. Filling in
// gaps after a model change would deepen the mix rather than fix it, so the
// whole vector space is replaced and re-stamped.
func TestMigrate_ModelChangeReembedsEverything(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	seedIndexedCorpus(t, svc)

	before := vectorCount(t, svc)
	if before == 0 {
		t.Fatal("fixture produced no vectors")
	}

	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)

	var docs int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_documents`).Scan(&docs); err != nil {
		t.Fatalf("count documents: %v", err)
	}

	res, err := svc.Migrate(ctx, nil)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !res.FullReembed {
		t.Fatal("FullReembed = false after a model change")
	}
	if res.Total != docs || res.Documents != docs {
		t.Errorf("re-embedded %d of %d documents, want all %d", res.Documents, res.Total, docs)
	}
	if !strings.Contains(res.FromModel, embed.ModelNomicEmbedTextV15) ||
		!strings.Contains(res.ToModel, embed.ModelMxbaiEmbedLargeV1) {
		t.Errorf("from/to = %q/%q, want the two models named", res.FromModel, res.ToModel)
	}

	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Verdict != kb.StampOK {
		t.Errorf("verdict after re-embed = %v, want StampOK", status.Verdict)
	}
	if status.Stamp == nil || status.Stamp.Canonical != embed.ModelMxbaiEmbedLargeV1 {
		t.Errorf("stamp after re-embed = %+v, want %q", status.Stamp, embed.ModelMxbaiEmbedLargeV1)
	}
	if got := vectorCount(t, svc); got != before {
		t.Errorf("vector count after re-embed = %d, want %d", got, before)
	}

	// And semantic retrieval is usable again.
	if _, err := svc.Search(ctx, SearchRequest{
		Query: "authentication", Mode: SearchModeSemantic, Limit: 5, MinScore: Unset,
	}); err != nil {
		t.Fatalf("semantic search after re-embed: %v", err)
	}
}

// TestMigrate_FailureMidReembedLeavesCoherentState documents exactly what a user
// is left holding when a re-embed dies half way.
//
// The old vectors are already gone -- they had to be, or the writes that replace
// them would be refused by the very guard that detected the change -- so what
// remains is: the new model's stamp, the new model's vectors for the documents
// that got that far, and no vectors at all for the rest. Nothing is mixed,
// keyword search is untouched, and re-running the command finishes the job.
func TestMigrate_FailureMidReembedLeavesCoherentState(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	seedIndexedCorpus(t, svc)

	provider := useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)

	// Fail every embedding after the first document.
	var calls int
	provider.Hook = func(context.Context, []string) error {
		calls++
		if calls > 1 {
			return embed.ErrFakeFailure
		}
		return nil
	}

	res, err := svc.Migrate(ctx, nil)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Failed == 0 {
		t.Fatal("the fixture did not actually fail any document")
	}
	if res.Documents == 0 {
		t.Fatal("the fixture did not actually succeed at any document")
	}

	// Every vector present is from the new model, and the stamp says so.
	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Stamp == nil || status.Stamp.Canonical != embed.ModelMxbaiEmbedLargeV1 {
		t.Fatalf("stamp after a partial re-embed = %+v, want %q", status.Stamp, embed.ModelMxbaiEmbedLargeV1)
	}
	if status.Verdict != kb.StampOK {
		t.Errorf("verdict after a partial re-embed = %v, want StampOK", status.Verdict)
	}

	// Documents that did not get that far simply have no vectors -- they are not
	// carrying stale ones from the old model.
	var mixed int
	if err := svc.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kb_vectors WHERE dim != ?`, fakeEmbedDim).Scan(&mixed); err != nil {
		t.Fatalf("check for mixed widths: %v", err)
	}
	if mixed != 0 {
		t.Errorf("%d vectors of a foreign width survived the re-embed", mixed)
	}

	// Re-running finishes the job.
	provider.Hook = nil
	if _, err := svc.Migrate(ctx, nil); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if _, err := svc.Search(ctx, SearchRequest{
		Query: "authentication", Mode: SearchModeSemantic, Limit: 5, MinScore: Unset,
	}); err != nil {
		t.Fatalf("semantic search after finishing the re-embed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Rebuild via sync
// ---------------------------------------------------------------------------

// TestPrepareVectorRebuild_ClearsOnlyOnAModelChange pins both halves: an
// ordinary rebuild is left exactly as it was, and a rebuild after a model change
// empties the space so the sync that follows can refill it.
func TestPrepareVectorRebuild_ClearsOnlyOnAModelChange(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	sourceID := seedIndexedCorpus(t, svc)
	before := vectorCount(t, svc)

	// No model change: nothing is destroyed.
	mismatch, err := svc.PrepareVectorRebuild(ctx)
	if err != nil {
		t.Fatalf("PrepareVectorRebuild: %v", err)
	}
	if mismatch != nil {
		t.Errorf("reported a model change where there was none: %v", mismatch)
	}
	if got := vectorCount(t, svc); got != before {
		t.Errorf("an ordinary rebuild discarded vectors: %d, want %d", got, before)
	}

	// Model change: the space is emptied, and the sync that follows refills and
	// re-stamps it.
	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)
	mismatch, err = svc.PrepareVectorRebuild(ctx)
	if err != nil {
		t.Fatalf("PrepareVectorRebuild: %v", err)
	}
	if mismatch == nil {
		t.Fatal("a model change was not reported")
	}
	if !errors.Is(mismatch, kb.ErrEmbeddingModelMismatch) {
		t.Errorf("returned error is not an embedding mismatch: %v", mismatch)
	}
	if got := vectorCount(t, svc); got != 0 {
		t.Errorf("vector count after preparing a rebuild = %d, want 0", got)
	}

	if _, err := svc.Sync(ctx, sourceID, true); err != nil {
		t.Fatalf("rebuild sync: %v", err)
	}
	if got := vectorCount(t, svc); got != before {
		t.Errorf("vector count after the rebuild = %d, want %d", got, before)
	}
	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Stamp == nil || status.Stamp.Canonical != embed.ModelMxbaiEmbedLargeV1 {
		t.Errorf("stamp after the rebuild = %+v, want %q", status.Stamp, embed.ModelMxbaiEmbedLargeV1)
	}
	if status.Verdict != kb.StampOK {
		t.Errorf("verdict after the rebuild = %v, want StampOK", status.Verdict)
	}
}

// ---------------------------------------------------------------------------
// Legacy adoption at open
// ---------------------------------------------------------------------------

// TestOpen_AdoptsStampForLegacyVectors covers the upgrade both of this project's
// machines will perform: a knowledge base full of vectors that nothing stamped.
//
// The assumption has to be made when the knowledge base is OPENED, not when it
// is next written. Deferring it would mean an upgraded machine whose model had
// also changed got stamped with the NEW model at its first write, blessing the
// exact mixture the stamp exists to prevent.
func TestOpen_AdoptsStampForLegacyVectors(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))

	// Write vectors through an index with no identity: that is pre-WP-4.3.
	legacyVectors, err := kb.NewSQLiteVectorIndex(svc.db, kb.VectorIndexConfig{Dimension: fakeEmbedDim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	legacyEmbedder := kb.NewProviderEmbedder(embed.NewFakeProvider(embed.ModelNomicEmbedTextV15, fakeEmbedDim, 7))
	svc.embedder = legacyEmbedder
	svc.semantic = kb.NewSemanticSearcherWith(svc.db, legacyEmbedder, legacyVectors)
	svc.source.SetSemanticSearcher(svc.semantic)
	svc.indexer.SetSemanticSearcher(svc.semantic)
	seedIndexedCorpus(t, svc)

	if stamp, _ := kb.ReadEmbeddingStamp(ctx, svc.db); stamp != nil {
		t.Fatalf("the legacy path stamped anyway: %+v", stamp)
	}

	// Re-open the same file with an identity, as an upgraded binary would.
	upgraded := useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	_ = upgraded
	adopted, err := svc.vectors.AdoptLegacyStamp(ctx)
	if err != nil {
		t.Fatalf("AdoptLegacyStamp: %v", err)
	}
	if !adopted {
		t.Fatal("adopted = false: vectors are present and the widths match")
	}

	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Stamp == nil || !status.Stamp.Adopted {
		t.Fatalf("stamp = %+v, want one marked as an assumption", status.Stamp)
	}
	if status.Verdict != kb.StampOK {
		t.Errorf("verdict = %v, want StampOK", status.Verdict)
	}

	// And the adopted stamp does its job: a LATER model change is caught.
	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)
	status, err = svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Verdict != kb.StampMismatch {
		t.Errorf("verdict after a post-adoption model change = %v, want StampMismatch", status.Verdict)
	}
}

// TestOpen_LegacyVectorsOfAnotherWidthAreNotBlessed replays, end to end, the
// scenario that failed review.
//
// A knowledge base is indexed by pre-WP-4.3 Conduit at one width. The user then
// changes embedding model to one of a DIFFERENT width and upgrades. Adoption
// declines (the widths disagree), and before the fix that meant no stamp at
// all — so the next sync wrote freely, stamped the knowledge base with the new
// model, and left the old vectors permanently unreadable while doctor reported
// green and the backfill saw nothing to do.
func TestOpen_LegacyVectorsOfAnotherWidthAreNotBlessed(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))

	// --- pre-WP-4.3: vectors written with no identity, at the wide width ------
	const wideDim = fakeEmbedDim       // 16
	const narrowDim = fakeEmbedDim / 2 // 8

	legacyVectors, err := kb.NewSQLiteVectorIndex(svc.db, kb.VectorIndexConfig{Dimension: wideDim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	legacyEmbedder := kb.NewProviderEmbedder(embed.NewFakeProvider(embed.ModelNomicEmbedTextV15, wideDim, 7))
	svc.embedder = legacyEmbedder
	svc.semantic = kb.NewSemanticSearcherWith(svc.db, legacyEmbedder, legacyVectors)
	svc.source.SetSemanticSearcher(svc.semantic)
	svc.indexer.SetSemanticSearcher(svc.semantic)

	dir := corpusDir(t)
	src, err := svc.AddSource(ctx, kb.AddSourceRequest{
		Path: dir, Name: "legacy", Patterns: []string{"*.md"},
	})
	if err != nil {
		t.Fatalf("AddSource: %v", err)
	}
	if _, err := svc.Sync(ctx, src.SourceID, false); err != nil {
		t.Fatalf("legacy sync: %v", err)
	}
	legacyVectorCount := vectorCount(t, svc)
	if legacyVectorCount == 0 {
		t.Fatal("fixture produced no legacy vectors")
	}
	if stamp, _ := kb.ReadEmbeddingStamp(ctx, svc.db); stamp != nil {
		t.Fatalf("the legacy path stamped anyway: %+v", stamp)
	}

	// --- upgrade, with a narrower model configured ----------------------------
	useFakeEmbedderAt(t, svc, embed.ModelMxbaiEmbedLargeV1, narrowDim)
	adopted, err := svc.vectors.AdoptLegacyStamp(ctx)
	if err != nil {
		t.Fatalf("AdoptLegacyStamp: %v", err)
	}
	if !adopted {
		t.Fatal("adopted = false: leaving no stamp is what let the next write bless the wrong model")
	}

	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Stamp == nil {
		t.Fatal("no stamp after adoption")
	}
	if status.Stamp.Dimensions != wideDim {
		t.Errorf("stamped width = %d, want %d (what is actually on disk)", status.Stamp.Dimensions, wideDim)
	}
	if status.Verdict != kb.StampMismatch {
		t.Fatalf("verdict = %v, want StampMismatch", status.Verdict)
	}

	// --- the defect: a further sync must NOT write and must NOT re-stamp ------
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n\nquokka telemetry\n"), 0600); err != nil {
		t.Fatalf("write new document: %v", err)
	}
	res, err := svc.Sync(ctx, src.SourceID, false)
	if err != nil {
		t.Fatalf("sync after the upgrade returned an error instead of degrading: %v", err)
	}
	if res.Added != 1 {
		t.Errorf("added = %d, want 1: the document must still be indexed lexically", res.Added)
	}
	if res.SemanticErrors == 0 {
		t.Error("semantic_errors = 0: the refused vector write must be counted")
	}

	after, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if after.Stamp.Dimensions != wideDim {
		t.Errorf("the stamp was overwritten to %dd; the wrong model was blessed over unreadable vectors",
			after.Stamp.Dimensions)
	}

	// The reviewer's exact symptom: the stamp must describe every vector stored,
	// not a fraction of them.
	var total, atStampedWidth int
	if err := svc.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(dim = ?), 0) FROM kb_vectors`,
		after.Stamp.Dimensions).Scan(&total, &atStampedWidth); err != nil {
		t.Fatalf("count vectors: %v", err)
	}
	if total != atStampedWidth {
		t.Errorf("stamp describes %d of %d stored vectors", atStampedWidth, total)
	}
	if int64(total) != legacyVectorCount {
		t.Errorf("vector count = %d, want %d: no new-width vector may join the old space",
			total, legacyVectorCount)
	}

	// --- doctor reds, and the backfill does not report "nothing to do" --------
	if after.Verdict != kb.StampMismatch {
		t.Errorf("verdict = %v, want StampMismatch so doctor can report it", after.Verdict)
	}

	// --- and the model-aware migrate recovers ---------------------------------
	mig, err := svc.Migrate(ctx, nil)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if !mig.FullReembed {
		t.Fatal("Migrate backfilled instead of rebuilding; the dark vectors would have survived")
	}
	recovered, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if recovered.Verdict != kb.StampOK {
		t.Errorf("verdict after recovery = %v, want StampOK", recovered.Verdict)
	}
	if recovered.Stamp.Dimensions != narrowDim {
		t.Errorf("width after recovery = %d, want %d", recovered.Stamp.Dimensions, narrowDim)
	}
	if _, err := svc.Search(ctx, SearchRequest{
		Query: "authentication", Mode: SearchModeSemantic, Limit: 5, MinScore: Unset,
	}); err != nil {
		t.Fatalf("semantic search after recovery: %v", err)
	}
}

// TestSync_SingleSourceRebuildRefusedOnModelChange pins F6.
//
// Re-indexing a document deletes its vectors before writing the replacements,
// and the replacements are what the guard refuses. Running the rebuild would
// therefore destroy usable vectors and put nothing back — the user strictly
// worse off than if they had done nothing, and on a single-source knowledge
// base the stamp would be left describing zero vectors, breaking the invariant
// the whole design rests on.
func TestSync_SingleSourceRebuildRefusedOnModelChange(t *testing.T) {
	ctx := context.Background()
	svc := openTestService(t, testConfig(t))
	useFakeEmbedder(t, svc, embed.ModelNomicEmbedTextV15)
	sourceID := seedIndexedCorpus(t, svc)
	before := vectorCount(t, svc)

	useFakeEmbedder(t, svc, embed.ModelMxbaiEmbedLargeV1)

	_, err := svc.Sync(ctx, sourceID, true)
	if err == nil {
		t.Fatal("a single-source rebuild under a model change was allowed to run")
	}
	if !errors.Is(err, kb.ErrEmbeddingModelMismatch) {
		t.Errorf("error is not an embedding mismatch: %v", err)
	}
	if !strings.Contains(err.Error(), kb.RebuildRemedy) {
		t.Errorf("error %q does not name the command that does work", err)
	}

	// Nothing was destroyed. This is the whole point of refusing early.
	if got := vectorCount(t, svc); got != before {
		t.Errorf("vector count = %d, want %d: the refused rebuild deleted vectors anyway", got, before)
	}
	status, err := svc.EmbeddingStampStatus(ctx)
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status.Stamp == nil || status.Vectors == 0 {
		t.Errorf("stamp now describes %d vectors; it must never describe an empty space", status.Vectors)
	}

	// The whole-knowledge-base route still works, and is what the message names.
	if _, err := svc.PrepareVectorRebuild(ctx); err != nil {
		t.Fatalf("PrepareVectorRebuild: %v", err)
	}
	if _, err := svc.Sync(ctx, sourceID, true); err != nil {
		t.Fatalf("rebuild after PrepareVectorRebuild: %v", err)
	}
	if got := vectorCount(t, svc); got != before {
		t.Errorf("vector count after the supported rebuild = %d, want %d", got, before)
	}
}

// TestEmbeddingStampStatus_NilWithoutEmbeddings keeps embed.provider=none a
// supported mode rather than a diagnosis: there is no vector space, so there is
// nothing to compare and nothing to complain about.
func TestEmbeddingStampStatus_NilWithoutEmbeddings(t *testing.T) {
	svc := openTestService(t, testConfig(t))
	status, err := svc.EmbeddingStampStatus(context.Background())
	if err != nil {
		t.Fatalf("EmbeddingStampStatus: %v", err)
	}
	if status != nil {
		t.Errorf("status = %+v, want nil when embeddings are off", status)
	}
}
