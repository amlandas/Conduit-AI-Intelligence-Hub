package kb

// Hermetic tests for the embedding-model identity stamp (WP-4.3, issue #107).
//
// Every test here runs against a real SQLite file with the real schema and
// deterministic vectors. The bug being guarded against is a SAME-WIDTH model
// swap, so the fixtures deliberately keep the dimension constant and change
// only the model name -- which is exactly the case the pre-existing dimension
// guard cannot see.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/embed"
	"github.com/simpleflo/conduit/internal/store"
)

// stampDim is the width every stamp test uses. It is shared by both "models" in
// the swap tests: if the widths differed, the old dimension guard would catch
// the swap and these tests would prove nothing.
const stampDim = 8

// identityFor builds an identity for a model name at the shared test width.
func identityFor(model string) EmbeddingIdentity {
	return NewEmbeddingIdentity(model, "test", stampDim, embed.PrefixSchemeNone)
}

// newStampedIndex opens a throwaway database with a vector index that knows
// which model it is embedding with.
func newStampedIndex(t *testing.T, model string) (*sql.DB, *SQLiteVectorIndex) {
	t.Helper()
	db := newTestDB(t)
	return db, indexOver(t, db, identityFor(model))
}

// indexOver builds a second index over an existing database, which is how a
// model swap is simulated: the file stays, the embedder changes.
func indexOver(t *testing.T, db *sql.DB, id EmbeddingIdentity) *SQLiteVectorIndex {
	t.Helper()
	vi, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: stampDim, Identity: id})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	return vi
}

// writeOnePoint seeds the rows a vector points at and upserts it.
func writeOnePoint(t *testing.T, db *sql.DB, vi *SQLiteVectorIndex, chunkID string, one int) error {
	t.Helper()
	seedChunk(t, db, "src-1", "doc-1", chunkID, "/docs/a.txt", "A", "content "+chunkID)
	return vi.Upsert(context.Background(), []VectorPoint{{
		ID:         chunkID,
		Vector:     unitVector(stampDim, one),
		DocumentID: "doc-1",
		Metadata:   map[string]string{"source_id": "src-1"},
	}})
}

// ---------------------------------------------------------------------------
// The stamp is written with the first vector, and not before
// ---------------------------------------------------------------------------

// TestStamp_WrittenWithFirstVectorWrite pins both halves of "transactionally
// with the first successful vector write": nothing is recorded by merely opening
// an index, and the record appears the moment vectors do.
func TestStamp_WrittenWithFirstVectorWrite(t *testing.T) {
	ctx := context.Background()
	db, vi := newStampedIndex(t, embed.ModelNomicEmbedTextV15)

	// Opening the index touches the schema but must not assert anything about a
	// knowledge base that has never been indexed.
	if err := vi.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
	stamp, err := ReadEmbeddingStamp(ctx, db)
	if err != nil {
		t.Fatalf("ReadEmbeddingStamp: %v", err)
	}
	if stamp != nil {
		t.Fatalf("stamp exists before any vector was written: %+v", stamp)
	}

	if err := writeOnePoint(t, db, vi, "chunk-1", 0); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	stamp, err = ReadEmbeddingStamp(ctx, db)
	if err != nil {
		t.Fatalf("ReadEmbeddingStamp: %v", err)
	}
	if stamp == nil {
		t.Fatal("no stamp after the first vector write")
	}
	if stamp.Canonical != embed.ModelNomicEmbedTextV15 {
		t.Errorf("canonical model = %q, want %q", stamp.Canonical, embed.ModelNomicEmbedTextV15)
	}
	if stamp.Dimensions != stampDim {
		t.Errorf("dimensions = %d, want %d", stamp.Dimensions, stampDim)
	}
	if stamp.Adopted {
		t.Error("stamp written alongside its vectors is marked adopted")
	}
}

// TestStamp_WriteRollbackLeavesNoStamp proves the stamp really does share the
// caller's transaction. If it were written on its own connection, a rolled-back
// ingestion would leave a knowledge base claiming a model it holds no vectors
// from.
func TestStamp_WriteRollbackLeavesNoStamp(t *testing.T) {
	ctx := context.Background()
	db, vi := newStampedIndex(t, embed.ModelNomicEmbedTextV15)
	seedChunk(t, db, "src-1", "doc-1", "chunk-1", "/docs/a.txt", "A", "content")

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := vi.UpsertTx(ctx, tx, []VectorPoint{{
		ID: "chunk-1", Vector: unitVector(stampDim, 0), DocumentID: "doc-1",
	}}); err != nil {
		t.Fatalf("UpsertTx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	stamp, err := ReadEmbeddingStamp(ctx, db)
	if err != nil {
		t.Fatalf("ReadEmbeddingStamp: %v", err)
	}
	if stamp != nil {
		t.Fatalf("rolled-back write left a stamp behind: %+v", stamp)
	}
}

// ---------------------------------------------------------------------------
// THE bug: a same-width model swap
// ---------------------------------------------------------------------------

// TestStamp_SameWidthModelSwapIsRefused is issue #107 itself.
//
// Both models are 8-dimensional, so the pre-existing dimension guard sees
// nothing wrong. Before WP-4.3 the write below succeeded and the search below
// returned confident nonsense.
func TestStamp_SameWidthModelSwapIsRefused(t *testing.T) {
	ctx := context.Background()
	db, first := newStampedIndex(t, embed.ModelNomicEmbedTextV15)

	if err := writeOnePoint(t, db, first, "chunk-1", 0); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Same file, same width, different model.
	second := indexOver(t, db, identityFor(embed.ModelMxbaiEmbedLargeV1))

	seedChunk(t, db, "src-1", "doc-1", "chunk-2", "/docs/a.txt", "A", "content 2")
	err := second.Upsert(ctx, []VectorPoint{{
		ID: "chunk-2", Vector: unitVector(stampDim, 1), DocumentID: "doc-1",
	}})
	if !errors.Is(err, ErrEmbeddingModelMismatch) {
		t.Fatalf("write with a different model: err = %v, want ErrEmbeddingModelMismatch", err)
	}

	// Nothing was written: refusing must not half-apply.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_vectors`).Scan(&n); err != nil {
		t.Fatalf("count vectors: %v", err)
	}
	if n != 1 {
		t.Errorf("vector count = %d, want 1 (the refused write must not land)", n)
	}

	// And the query side refuses too, naming both models and the remedy.
	_, err = second.Search(ctx, unitVector(stampDim, 0), VectorSearchOptions{Limit: 5})
	if !errors.Is(err, ErrEmbeddingModelMismatch) {
		t.Fatalf("search with a different model: err = %v, want ErrEmbeddingModelMismatch", err)
	}
	var mismatch *ModelMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("search error is not a *ModelMismatchError: %T", err)
	}
	note := mismatch.Note()
	for _, want := range []string{
		embed.ModelNomicEmbedTextV15,
		embed.ModelMxbaiEmbedLargeV1,
		RebuildRemedy,
	} {
		if !strings.Contains(note, want) {
			t.Errorf("degraded note %q does not mention %q", note, want)
		}
	}
}

// TestStamp_HybridDegradesToLexicalOnModelSwap pins the user-visible half of the
// same defect: the search still answers, from the lexical leg, and says why the
// other leg is missing.
func TestStamp_HybridDegradesToLexicalOnModelSwap(t *testing.T) {
	ctx := context.Background()
	db, first := newStampedIndex(t, embed.ModelNomicEmbedTextV15)
	if err := writeOnePoint(t, db, first, "chunk-1", 0); err != nil {
		t.Fatalf("first write: %v", err)
	}

	second := indexOver(t, db, identityFor(embed.ModelMxbaiEmbedLargeV1))
	semantic := NewSemanticSearcherWith(db, &stubEmbedder{dim: stampDim}, second)
	hybrid := NewHybridSearcher(NewSearcher(db), semantic)

	res, err := hybrid.Search(ctx, "content", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("hybrid search returned an error instead of degrading: %v", err)
	}
	if !res.DegradedMode {
		t.Error("DegradedMode = false, want true after a model change")
	}
	for _, want := range []string{embed.ModelNomicEmbedTextV15, embed.ModelMxbaiEmbedLargeV1, RebuildRemedy} {
		if !strings.Contains(res.Note, want) {
			t.Errorf("hybrid note %q does not mention %q", res.Note, want)
		}
	}
}

// TestStamp_LexicalUnaffectedByModelSwap states the boundary plainly: a model
// change must cost the semantic leg and nothing else.
func TestStamp_LexicalUnaffectedByModelSwap(t *testing.T) {
	ctx := context.Background()
	db, first := newStampedIndex(t, embed.ModelNomicEmbedTextV15)
	if err := writeOnePoint(t, db, first, "chunk-1", 0); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// Give the lexical index something to find.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO kb_fts (document_id, chunk_id, content, title, path)
		VALUES ('doc-1', 'chunk-1', 'periwinkle telemetry', 'A', '/docs/a.txt')`); err != nil {
		t.Fatalf("seed fts: %v", err)
	}

	second := indexOver(t, db, identityFor(embed.ModelMxbaiEmbedLargeV1))
	semantic := NewSemanticSearcherWith(db, &stubEmbedder{dim: stampDim}, second)
	hybrid := NewHybridSearcher(NewSearcher(db), semantic)

	res, err := hybrid.Search(ctx, "periwinkle", HybridSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}
	if len(res.Results) == 0 {
		t.Fatal("no lexical results: a model change must not cost keyword search")
	}
}

// ---------------------------------------------------------------------------
// The false positive this design exists to avoid
// ---------------------------------------------------------------------------

// TestStamp_AliasedModelIsNotAMismatch is the protection against the failure
// mode that would be worse than the bug: telling a user who merely switched
// embed.provider from "ollama" to "llama-server" that their knowledge base is
// poisoned, and disabling semantic search on it.
//
// "nomic-embed-text" (the Ollama tag) and "nomic-embed-text-v1.5" (the registry
// key) are the same weights.
func TestStamp_AliasedModelIsNotAMismatch(t *testing.T) {
	ctx := context.Background()
	db, ollama := newStampedIndex(t, "nomic-embed-text")

	if err := writeOnePoint(t, db, ollama, "chunk-1", 0); err != nil {
		t.Fatalf("write via the ollama spelling: %v", err)
	}

	llamaServer := indexOver(t, db, identityFor(embed.ModelNomicEmbedTextV15))

	seedChunk(t, db, "src-1", "doc-1", "chunk-2", "/docs/a.txt", "A", "content 2")
	if err := llamaServer.Upsert(ctx, []VectorPoint{{
		ID: "chunk-2", Vector: unitVector(stampDim, 1), DocumentID: "doc-1",
	}}); err != nil {
		t.Fatalf("write via the registry spelling was refused: %v", err)
	}
	if _, err := llamaServer.Search(ctx, unitVector(stampDim, 0), VectorSearchOptions{Limit: 5}); err != nil {
		t.Fatalf("search via the registry spelling was refused: %v", err)
	}
}

// TestStamp_AliasSpellingsResolveTogether checks the canonicalisation table
// directly, including the shapes a config file can produce: an Ollama tag, a
// ":latest" suffix, a bare GGUF filename and a full model path.
func TestStamp_AliasSpellingsResolveTogether(t *testing.T) {
	same := [][]string{
		{
			"nomic-embed-text",
			"nomic-embed-text:latest",
			"NOMIC-EMBED-TEXT",
			"nomic-embed-text-v1.5",
			"nomic-embed-text-v1.5.f16.gguf",
			"/opt/models/nomic-embed-text-v1.5.f16.gguf",
			"  nomic-embed-text  ",
		},
		{
			"qwen3-embedding-0.6b",
			"qwen3-embedding:0.6b",
			"Qwen3-Embedding-0.6B-Q8_0.gguf",
		},
		{
			"mxbai-embed-large",
			"mxbai-embed-large-v1",
			"mxbai-embed-large-v1_fp16.gguf",
		},
	}

	for _, group := range same {
		want, ok := embed.CanonicalModelID(group[0])
		if !ok {
			t.Fatalf("%q did not resolve to a registry model", group[0])
		}
		for _, spelling := range group[1:] {
			got, ok := embed.CanonicalModelID(spelling)
			if !ok {
				t.Errorf("%q did not resolve to a registry model", spelling)
				continue
			}
			if got != want {
				t.Errorf("CanonicalModelID(%q) = %q, want %q", spelling, got, want)
			}
		}
	}

	// Different models must stay different. A canonicaliser that collapses
	// everything is not a fix, it is the original bug with extra steps.
	a, _ := embed.CanonicalModelID("nomic-embed-text")
	b, _ := embed.CanonicalModelID("mxbai-embed-large")
	if a == b {
		t.Errorf("two different models both canonicalised to %q", a)
	}
}

// ---------------------------------------------------------------------------
// Unknown models: warn, disable nothing
// ---------------------------------------------------------------------------

// TestStamp_UnknownModelDisablesNothing pins the third comparison outcome. A
// model Conduit has never heard of cannot be PROVEN different from the stamped
// one, so acting on the difference would risk the false positive above.
func TestStamp_UnknownModelDisablesNothing(t *testing.T) {
	ctx := context.Background()
	db, custom := newStampedIndex(t, "my-own-embedding-model")

	if err := writeOnePoint(t, db, custom, "chunk-1", 0); err != nil {
		t.Fatalf("write with an unregistered model: %v", err)
	}

	stamp, err := ReadEmbeddingStamp(ctx, db)
	if err != nil || stamp == nil {
		t.Fatalf("ReadEmbeddingStamp: stamp=%v err=%v", stamp, err)
	}
	if stamp.Resolved {
		t.Error("an unregistered model was recorded as resolved")
	}
	if stamp.Observed != "my-own-embedding-model" {
		t.Errorf("observed model = %q, want the identifier we actually saw", stamp.Observed)
	}

	// A different unregistered model: still not provable, so nothing is refused.
	other := indexOver(t, db, identityFor("some-other-model"))
	seedChunk(t, db, "src-1", "doc-1", "chunk-2", "/docs/a.txt", "A", "content 2")
	if err := other.Upsert(ctx, []VectorPoint{{
		ID: "chunk-2", Vector: unitVector(stampDim, 1), DocumentID: "doc-1",
	}}); err != nil {
		t.Fatalf("write refused on an unprovable difference: %v", err)
	}
	if _, err := other.Search(ctx, unitVector(stampDim, 0), VectorSearchOptions{Limit: 5}); err != nil {
		t.Fatalf("search refused on an unprovable difference: %v", err)
	}

	// A registered model against an unregistered stamp is equally unprovable.
	registered := indexOver(t, db, identityFor(embed.ModelNomicEmbedTextV15))
	if _, err := registered.Search(ctx, unitVector(stampDim, 0), VectorSearchOptions{Limit: 5}); err != nil {
		t.Fatalf("search refused when only one side is registered: %v", err)
	}
}

// TestStamp_CompareVerdicts states the whole comparison table in one place, so
// that a change to the logic has to be a deliberate edit to these rows.
func TestStamp_CompareVerdicts(t *testing.T) {
	known := func(model string) EmbeddingIdentity { return identityFor(model) }
	stampOf := func(id EmbeddingIdentity) *EmbeddingStamp {
		return &EmbeddingStamp{EmbeddingIdentity: id}
	}

	cases := []struct {
		name   string
		stamp  *EmbeddingStamp
		active EmbeddingIdentity
		want   StampVerdict
	}{
		{"no stamp", nil, known(embed.ModelNomicEmbedTextV15), StampOK},
		{"no active identity", stampOf(known(embed.ModelNomicEmbedTextV15)), EmbeddingIdentity{}, StampOK},
		{"identical", stampOf(known(embed.ModelNomicEmbedTextV15)), known(embed.ModelNomicEmbedTextV15), StampOK},
		{"alias", stampOf(known("nomic-embed-text")), known(embed.ModelNomicEmbedTextV15), StampOK},
		{"two registered models", stampOf(known(embed.ModelNomicEmbedTextV15)), known(embed.ModelMxbaiEmbedLargeV1), StampMismatch},
		{"registered vs unknown", stampOf(known(embed.ModelNomicEmbedTextV15)), known("mystery"), StampUnknown},
		{"unknown vs registered", stampOf(known("mystery")), known(embed.ModelNomicEmbedTextV15), StampUnknown},
		{"two unknowns", stampOf(known("mystery-a")), known("mystery-b"), StampUnknown},
		{"same unknown", stampOf(known("mystery-a")), known("mystery-a"), StampOK},
		{
			"same model, different width",
			stampOf(NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "test", 768, embed.PrefixSchemeNone)),
			NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "test", 256, embed.PrefixSchemeNone),
			StampMismatch,
		},
		{
			"same model, different prefixes",
			stampOf(NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "ollama", stampDim, embed.PrefixSchemeNone)),
			NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "llama-server", stampDim, embed.PrefixSchemeID("d: ", "q: ", "")),
			StampPrefixChanged,
		},
		{
			"same model, different provider, same decoration",
			stampOf(NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "ollama", stampDim, embed.PrefixSchemeNone)),
			NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "llama-server", stampDim, embed.PrefixSchemeNone),
			StampOK,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.stamp.Compare(tc.active); got != tc.want {
				t.Errorf("verdict = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestStamp_PrefixChangeDoesNotDisableSearch backs the row above with the
// behaviour it implies. Different decoration on the same weights costs accuracy,
// not comparability, and refusing over it would be the ollama -> llama-server
// false positive wearing a different hat.
func TestStamp_PrefixChangeDoesNotDisableSearch(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	bare := indexOver(t, db, NewEmbeddingIdentity(
		embed.ModelNomicEmbedTextV15, "ollama", stampDim, embed.PrefixSchemeNone))
	if err := writeOnePoint(t, db, bare, "chunk-1", 0); err != nil {
		t.Fatalf("first write: %v", err)
	}

	prefixed := indexOver(t, db, NewEmbeddingIdentity(
		embed.ModelNomicEmbedTextV15, "llama-server", stampDim,
		embed.PrefixSchemeID("search_document: ", "search_query: ", "")))

	seedChunk(t, db, "src-1", "doc-1", "chunk-2", "/docs/a.txt", "A", "content 2")
	if err := prefixed.Upsert(ctx, []VectorPoint{{
		ID: "chunk-2", Vector: unitVector(stampDim, 1), DocumentID: "doc-1",
	}}); err != nil {
		t.Fatalf("write refused over a prefix-scheme change: %v", err)
	}
	if _, err := prefixed.Search(ctx, unitVector(stampDim, 0), VectorSearchOptions{Limit: 5}); err != nil {
		t.Fatalf("search refused over a prefix-scheme change: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Legacy adoption
// ---------------------------------------------------------------------------

// TestStamp_LegacyAdoption covers the upgrade path both this project's machines
// are on: a knowledge base full of vectors that nothing ever stamped.
func TestStamp_LegacyAdoption(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	// An index with no identity is exactly what pre-WP-4.3 Conduit was.
	legacy, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: stampDim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	if err := writeOnePoint(t, db, legacy, "chunk-1", 0); err != nil {
		t.Fatalf("legacy write: %v", err)
	}
	if stamp, _ := ReadEmbeddingStamp(ctx, db); stamp != nil {
		t.Fatalf("an index with no identity stamped anyway: %+v", stamp)
	}

	upgraded := indexOver(t, db, identityFor(embed.ModelNomicEmbedTextV15))
	adopted, err := upgraded.AdoptLegacyStamp(ctx)
	if err != nil {
		t.Fatalf("AdoptLegacyStamp: %v", err)
	}
	if !adopted {
		t.Fatal("adopted = false, want true: vectors are present and the widths match")
	}

	stamp, err := ReadEmbeddingStamp(ctx, db)
	if err != nil || stamp == nil {
		t.Fatalf("ReadEmbeddingStamp: stamp=%v err=%v", stamp, err)
	}
	if stamp.Canonical != embed.ModelNomicEmbedTextV15 {
		t.Errorf("adopted model = %q, want %q", stamp.Canonical, embed.ModelNomicEmbedTextV15)
	}
	if !stamp.Adopted {
		t.Error("adopted stamp is not marked as an assumption")
	}

	// Adoption is once. A second open must not overwrite a stamp it did not
	// write -- that is how a later model change stays visible.
	swapped := indexOver(t, db, identityFor(embed.ModelMxbaiEmbedLargeV1))
	again, err := swapped.AdoptLegacyStamp(ctx)
	if err != nil {
		t.Fatalf("second AdoptLegacyStamp: %v", err)
	}
	if again {
		t.Fatal("adoption ran twice; the second model would have overwritten the first")
	}
	if _, err := swapped.Search(ctx, unitVector(stampDim, 0), VectorSearchOptions{Limit: 5}); !errors.Is(err, ErrEmbeddingModelMismatch) {
		t.Fatalf("after adoption a model change was not detected: err = %v", err)
	}
}

// TestStamp_NoAdoptionWithoutVectors pins the other branch: an empty knowledge
// base has nothing to make an assumption about, so it makes none and waits for
// the first real write.
func TestStamp_NoAdoptionWithoutVectors(t *testing.T) {
	ctx := context.Background()
	db, vi := newStampedIndex(t, embed.ModelNomicEmbedTextV15)

	adopted, err := vi.AdoptLegacyStamp(ctx)
	if err != nil {
		t.Fatalf("AdoptLegacyStamp: %v", err)
	}
	if adopted {
		t.Error("adopted a stamp for a knowledge base with no vectors")
	}
	if stamp, _ := ReadEmbeddingStamp(ctx, db); stamp != nil {
		t.Errorf("stamp written with no vectors present: %+v", stamp)
	}
}

// TestStamp_NoAdoptionOnWidthDisagreement leaves the width case to the guard
// that already handles it. Adopting here would record a model that provably did
// not produce the stored vectors.
func TestStamp_NoAdoptionOnWidthDisagreement(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)

	legacy, err := NewSQLiteVectorIndex(db, VectorIndexConfig{Dimension: stampDim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	if err := writeOnePoint(t, db, legacy, "chunk-1", 0); err != nil {
		t.Fatalf("legacy write: %v", err)
	}

	wider, err := NewSQLiteVectorIndex(db, VectorIndexConfig{
		Dimension: stampDim * 2,
		Identity:  NewEmbeddingIdentity(embed.ModelNomicEmbedTextV15, "test", stampDim*2, embed.PrefixSchemeNone),
	})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	adopted, err := wider.AdoptLegacyStamp(ctx)
	if err != nil {
		t.Fatalf("AdoptLegacyStamp: %v", err)
	}
	if adopted {
		t.Error("adopted a model whose width disagrees with the stored vectors")
	}
}

// ---------------------------------------------------------------------------
// Rebuilding
// ---------------------------------------------------------------------------

// TestStamp_ResetVectorSpaceClearsBothHalves pins the invariant that makes a
// rebuild possible at all: vectors and the stamp describing them go together.
func TestStamp_ResetVectorSpaceClearsBothHalves(t *testing.T) {
	ctx := context.Background()
	db, vi := newStampedIndex(t, embed.ModelNomicEmbedTextV15)
	if err := writeOnePoint(t, db, vi, "chunk-1", 0); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := vi.ResetVectorSpace(ctx); err != nil {
		t.Fatalf("ResetVectorSpace: %v", err)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM kb_vectors`).Scan(&n); err != nil {
		t.Fatalf("count vectors: %v", err)
	}
	if n != 0 {
		t.Errorf("vector count after reset = %d, want 0", n)
	}
	if stamp, _ := ReadEmbeddingStamp(ctx, db); stamp != nil {
		t.Errorf("stamp survived the reset: %+v", stamp)
	}

	// The new model can now write, and stamps as it does.
	swapped := indexOver(t, db, identityFor(embed.ModelMxbaiEmbedLargeV1))
	seedChunk(t, db, "src-1", "doc-1", "chunk-9", "/docs/a.txt", "A", "content 9")
	if err := swapped.Upsert(ctx, []VectorPoint{{
		ID: "chunk-9", Vector: unitVector(stampDim, 2), DocumentID: "doc-1",
	}}); err != nil {
		t.Fatalf("write after reset: %v", err)
	}
	stamp, _ := ReadEmbeddingStamp(ctx, db)
	if stamp == nil || stamp.Canonical != embed.ModelMxbaiEmbedLargeV1 {
		t.Fatalf("stamp after rebuild = %+v, want %q", stamp, embed.ModelMxbaiEmbedLargeV1)
	}
}

// ---------------------------------------------------------------------------
// Entity vectors
// ---------------------------------------------------------------------------

// TestStamp_EntityVectorsShareTheStamp checks that KAG's entity vectors, which
// come out of the same embedder, are held to the same rule. Mixing them is the
// same bug on a different table.
func TestStamp_EntityVectorsShareTheStamp(t *testing.T) {
	ctx := context.Background()
	db, first := newStampedIndex(t, embed.ModelNomicEmbedTextV15)

	seedEntity(t, db, "e1", "Alpha", "concept", 0.9)
	if err := first.UpsertEntityBatch(ctx, []EntityVectorPoint{{
		ID: "e1", Vector: unitVector(stampDim, 0), Name: "Alpha", Type: "concept",
	}}); err != nil {
		t.Fatalf("entity upsert: %v", err)
	}
	if stamp, _ := ReadEmbeddingStamp(ctx, db); stamp == nil {
		t.Fatal("an entity-vector write did not stamp the knowledge base")
	}

	second := indexOver(t, db, identityFor(embed.ModelMxbaiEmbedLargeV1))
	seedEntity(t, db, "e2", "Beta", "concept", 0.9)
	err := second.UpsertEntityBatch(ctx, []EntityVectorPoint{{
		ID: "e2", Vector: unitVector(stampDim, 1), Name: "Beta", Type: "concept",
	}})
	if !errors.Is(err, ErrEmbeddingModelMismatch) {
		t.Errorf("entity write with a different model: err = %v, want ErrEmbeddingModelMismatch", err)
	}
	_, err = second.SearchEntities(ctx, unitVector(stampDim, 0), VectorEntitySearchOptions{Limit: 5})
	if !errors.Is(err, ErrEmbeddingModelMismatch) {
		t.Errorf("entity search with a different model: err = %v, want ErrEmbeddingModelMismatch", err)
	}
}

// ---------------------------------------------------------------------------
// Schema parity
// ---------------------------------------------------------------------------

// TestSchemaParityMigrationVsEnsureSchema compares the schema a migrated
// database ends up with against the one the vector index creates lazily.
//
// The two paths exist because a database can reach the vector index before the
// migration chain has created its tables, and they are separate code because
// internal/store cannot import internal/kb without a cycle. WP-2.3 found they
// had already drifted once. This compares what SQLite actually stored, so it
// catches a difference in a CHECK constraint or a default that a string
// comparison of the two literals could not.
func TestSchemaParityMigrationVsEnsureSchema(t *testing.T) {
	ctx := context.Background()

	// Path one: the migration chain, which is what store.New runs.
	migrated := newTestDB(t)

	// Path two: a database with the vector tables removed, reopened through the
	// vector index so ensureSchema has to recreate them.
	lazy := newTestDB(t)
	for _, table := range []string{"kb_vectors", "kb_entity_vectors", "kb_embedding_stamp"} {
		if _, err := lazy.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
	vi, err := NewSQLiteVectorIndex(lazy, VectorIndexConfig{Dimension: stampDim})
	if err != nil {
		t.Fatalf("NewSQLiteVectorIndex: %v", err)
	}
	if err := vi.HealthCheck(ctx); err != nil {
		t.Fatalf("HealthCheck (drives ensureSchema): %v", err)
	}

	for _, table := range []string{"kb_vectors", "kb_entity_vectors", "kb_embedding_stamp"} {
		want := tableSQL(t, migrated, table)
		got := tableSQL(t, lazy, table)
		if want == "" {
			t.Fatalf("%s missing from the migrated database", table)
		}
		if normalizeSQL(got) != normalizeSQL(want) {
			t.Errorf("%s differs between the migration and ensureSchema paths:\n migration:  %s\n ensureSchema: %s",
				table, want, got)
		}
	}
}

// tableSQL returns the CREATE statement SQLite recorded for a table.
func tableSQL(t *testing.T, db *sql.DB, table string) string {
	t.Helper()
	var stmt sql.NullString
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name = ?`, table).Scan(&stmt)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	if err != nil {
		t.Fatalf("read schema for %s: %v", table, err)
	}
	return stmt.String
}

// normalizeSQL collapses whitespace so that indentation cannot fail a parity
// check that is about structure.
func normalizeSQL(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestSchemaParity_MigrationVersionRecorded guards the other half of the
// migration contract: a fresh database must record every migration, or the next
// release will try to re-run them.
func TestSchemaParity_MigrationVersionRecorded(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "versions.db")
	st, err := store.New(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available")
		}
		t.Fatalf("store.New: %v", err)
	}
	defer st.Close()

	var version int
	if err := st.DB().QueryRow(`SELECT COALESCE(MAX(version), 0) FROM migrations`).Scan(&version); err != nil {
		t.Fatalf("read migration version: %v", err)
	}
	if version < 6 {
		t.Errorf("max migration version = %d, want at least 6 (the embedding stamp)", version)
	}
}

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// stubEmbedder returns a fixed vector. The stamp tests care about which model
// name is claimed, not about what the numbers are.
type stubEmbedder struct{ dim int }

func (s *stubEmbedder) Embed(context.Context, string) ([]float32, error) {
	return unitVector(s.dim, 0), nil
}

func (s *stubEmbedder) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = unitVector(s.dim, i%s.dim)
	}
	return out, nil
}

func (s *stubEmbedder) Dimension() int                    { return s.dim }
func (s *stubEmbedder) Model() string                     { return "stub" }
func (s *stubEmbedder) HealthCheck(context.Context) error { return nil }
