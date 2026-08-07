package kb

// Embedding-model identity stamp (WP-4.3, issue #107).
//
// # The defect this closes
//
// kb_vectors records a vector's WIDTH but not what produced it. The width check
// in SQLiteVectorIndex.UpsertTx therefore catches a swap from a 768-dim model to
// a 1024-dim one and nothing else. Swap nomic-embed-text-v1.5 for another
// 768-dim model and every write is accepted: queries are embedded in the new
// model's space and compared against chunks embedded in the old one. Cosine
// similarity between two unrelated spaces is not a weak signal, it is noise, and
// nothing in the system says a word. Rankings quietly become arbitrary.
//
// The fix is to record, once, what built the vectors, and to compare it against
// what is embedding now.
//
// # Why canonicalisation is the hard part
//
// The naive version of this check -- compare the model string -- introduces a
// worse bug than it fixes. The same weights answer to different names depending
// on how they are reached: Ollama calls nomic-embed-text-v1.5 "nomic-embed-text",
// the pinned artifact is a file called "nomic-embed-text-v1.5.f16.gguf". A user
// who switches embed.provider from "ollama" to "llama-server" changes nothing
// about their vectors, and a string comparison would tell them their knowledge
// base is poisoned and turn off semantic search on it.
//
// So identity is resolved through the internal/embed registry, which is the only
// place that knows which spellings mean the same weights (ModelSpec.Aliases).
// Comparison has three outcomes, not two:
//
//	same canonical id      -> proceed, silently
//	different canonical id -> mismatch, refuse writes and skip the semantic leg
//	either side unknown    -> warn once, disable nothing
//
// The third case is the honest answer for a model Conduit has never heard of --
// a user's own Ollama tag, say. We cannot prove it is a different model from the
// one that built the vectors, and acting on a guess would risk exactly the
// false positive described above. The observed identifier is recorded so the
// user can see what we saw and judge for themselves.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/simpleflo/conduit/internal/embed"
)

// rowQuerier is the read half of *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// execer is the write half of *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// EmbeddingIdentity describes the embedder a caller is about to use, in the
// terms the stamp is compared in.
//
// The zero value means "identity unknown". Callers that do not supply one get
// the pre-WP-4.3 behaviour -- no stamping and no checking -- because recording
// an identity we cannot name would be worse than recording nothing.
type EmbeddingIdentity struct {
	// Observed is the model identifier exactly as the provider reports it.
	Observed string

	// Canonical is Observed resolved through the embed registry, or the
	// normalised form of Observed when the registry does not know it.
	Canonical string

	// Resolved is true when Canonical came from the registry. When it is false
	// a difference in Canonical proves nothing.
	Resolved bool

	// Dimensions is the vector width the embedder produces.
	Dimensions int

	// PrefixScheme identifies the instruction decoration applied to inputs. See
	// embed.PrefixSchemeID.
	PrefixScheme string

	// Provider is the configured provider kind ("llama-server", "ollama").
	// Recorded for diagnosis only; it is never compared, because switching
	// provider while keeping the model is exactly the case that must stay quiet.
	Provider string
}

// NewEmbeddingIdentity resolves an observed model identifier into an identity.
func NewEmbeddingIdentity(observed, provider string, dimensions int, prefixScheme string) EmbeddingIdentity {
	observed = strings.TrimSpace(observed)
	if observed == "" {
		return EmbeddingIdentity{}
	}
	canonical, resolved := embed.CanonicalModelID(observed)
	if prefixScheme == "" {
		prefixScheme = embed.PrefixSchemeNone
	}
	return EmbeddingIdentity{
		Observed:     observed,
		Canonical:    canonical,
		Resolved:     resolved,
		Dimensions:   dimensions,
		PrefixScheme: prefixScheme,
		Provider:     provider,
	}
}

// Known reports whether this identity names an embedder at all.
func (id EmbeddingIdentity) Known() bool { return id.Observed != "" }

// Display returns the identity as it should appear to a user: the canonical id,
// plus the observed spelling when the two differ.
func (id EmbeddingIdentity) Display() string {
	if !id.Known() {
		return "unknown"
	}
	if id.Canonical == "" || strings.EqualFold(id.Canonical, id.Observed) {
		return id.Observed
	}
	return fmt.Sprintf("%s (reported as %q)", id.Canonical, id.Observed)
}

// EmbeddingStamp is the recorded identity of whatever built the stored vectors.
type EmbeddingStamp struct {
	EmbeddingIdentity
	CreatedAt time.Time
	UpdatedAt time.Time
	// Adopted is true when the stamp was inferred for a pre-WP-4.3 knowledge
	// base rather than written alongside the vectors it describes.
	Adopted bool
}

// StampVerdict is the outcome of comparing a stamp with the active embedder.
type StampVerdict int

const (
	// StampOK means the active embedder can safely read and extend the stored
	// vectors. It is also the verdict when there is nothing to compare.
	StampOK StampVerdict = iota

	// StampMismatch means the stored vectors were built by a different model,
	// proven. Writes are refused and the semantic leg is skipped.
	StampMismatch

	// StampUnknown means the two identities differ but at least one of them is
	// not in the registry, so the difference cannot be trusted. Nothing is
	// disabled; the user is warned once.
	StampUnknown

	// StampPrefixChanged means the same model is in use with different
	// instruction decoration. Vectors stay comparable, retrieval quality does
	// not; the user is warned once.
	StampPrefixChanged
)

// Compare judges the active identity against the stamp.
//
// A nil stamp, an unknown active identity, or an unknown stamped identity that
// happens to match all yield StampOK: this function only ever reports a problem
// it can substantiate.
func (s *EmbeddingStamp) Compare(active EmbeddingIdentity) StampVerdict {
	if s == nil || !s.Known() || !active.Known() {
		return StampOK
	}

	// Width first, and without regard to the model name. Two runs of the SAME
	// model at different widths (Matryoshka truncation via embed.dimensions)
	// produce vectors that cannot be compared at all -- the scan silently skips
	// every row whose blob length disagrees with the query -- and unlike a name,
	// a width cannot be an alias for anything else.
	if s.Dimensions > 0 && active.Dimensions > 0 && s.Dimensions != active.Dimensions {
		return StampMismatch
	}

	if strings.EqualFold(s.Canonical, active.Canonical) {
		if s.PrefixScheme != "" && active.PrefixScheme != "" && s.PrefixScheme != active.PrefixScheme {
			return StampPrefixChanged
		}
		return StampOK
	}

	// The names differ. That is only evidence of a different MODEL if both
	// names were resolved through the registry; otherwise one of them may be a
	// spelling we simply do not know about.
	if !s.Resolved || !active.Resolved {
		return StampUnknown
	}
	return StampMismatch
}

// ErrEmbeddingModelMismatch is the sentinel behind every refusal caused by the
// stamp. Callers should test with errors.Is / errors.As rather than on text.
var ErrEmbeddingModelMismatch = errors.New("embedding model mismatch")

// ModelMismatchError reports that the stored vectors and the active embedder
// disagree about which model is in use.
type ModelMismatchError struct {
	// Stamped is the identity recorded against the stored vectors.
	Stamped EmbeddingIdentity
	// Active is the identity of the embedder configured now.
	Active EmbeddingIdentity
	// Op names what was refused, e.g. "write vectors" or "semantic search".
	Op string
}

// RebuildRemedy is the single command that resolves a stamp mismatch. It is
// referenced verbatim by the CLI, the MCP degraded note and the doctor check so
// that a user is never told two different things.
const RebuildRemedy = "conduit kb sync --rebuild-vectors"

func (e *ModelMismatchError) Error() string {
	return fmt.Sprintf("%s: %s (vectors were built by %s, current model is %s; run `%s`)",
		e.Op, ErrEmbeddingModelMismatch, e.Stamped.Display(), e.Active.Display(), RebuildRemedy)
}

func (e *ModelMismatchError) Is(target error) bool { return target == ErrEmbeddingModelMismatch }

// Note returns the one-sentence explanation shown to users and AI clients.
func (e *ModelMismatchError) Note() string {
	return fmt.Sprintf("Semantic search is unavailable: vectors were built by %s, current model is %s. Run `%s`.",
		e.Stamped.Display(), e.Active.Display(), RebuildRemedy)
}

// ---------------------------------------------------------------------------
// Persistence
// ---------------------------------------------------------------------------

// embeddingStampDDL creates the stamp table.
//
// It is a singleton row: the stamp describes the knowledge base's vector space
// as a whole, and CHECK (id = 1) makes a second, contradictory row impossible
// rather than merely unlikely.
//
// This statement is shared verbatim between migration 006 and
// SQLiteVectorIndex.ensureSchema. WP-2.3 found those two paths had drifted for
// kb_entity_vectors, producing databases that behaved differently depending on
// which path created them; a shared constant plus a parity test is the fix that
// makes the drift impossible rather than merely discouraged.
const embeddingStampDDL = `CREATE TABLE IF NOT EXISTS kb_embedding_stamp (
	id              INTEGER PRIMARY KEY CHECK (id = 1),
	canonical_model TEXT NOT NULL,
	observed_model  TEXT NOT NULL,
	resolved        INTEGER NOT NULL DEFAULT 0,
	dimensions      INTEGER NOT NULL,
	prefix_scheme   TEXT NOT NULL DEFAULT '',
	provider        TEXT NOT NULL DEFAULT '',
	adopted         INTEGER NOT NULL DEFAULT 0,
	created_at      TEXT NOT NULL DEFAULT (datetime('now')),
	updated_at      TEXT NOT NULL DEFAULT (datetime('now'))
)`

// ReadEmbeddingStamp returns the stored stamp, or (nil, nil) when the knowledge
// base has never recorded one.
//
// A missing table is also (nil, nil): a database that predates migration 006 has
// no stamp, which is exactly the same state as one that has never been indexed.
func ReadEmbeddingStamp(ctx context.Context, q rowQuerier) (*EmbeddingStamp, error) {
	var (
		st                 EmbeddingStamp
		resolved, adopted  int
		createdAt, updated sql.NullString
	)
	err := q.QueryRowContext(ctx, `
		SELECT canonical_model, observed_model, resolved, dimensions,
		       prefix_scheme, provider, adopted, created_at, updated_at
		FROM kb_embedding_stamp WHERE id = 1
	`).Scan(&st.Canonical, &st.Observed, &resolved, &st.Dimensions,
		&st.PrefixScheme, &st.Provider, &adopted, &createdAt, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		if isMissingTableErr(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read embedding stamp: %w", err)
	}
	st.Resolved = resolved != 0
	st.Adopted = adopted != 0
	st.CreatedAt = parseStampTime(createdAt)
	st.UpdatedAt = parseStampTime(updated)
	return &st, nil
}

// parseStampTime decodes SQLite's datetime('now') text. A value we cannot parse
// yields the zero time rather than an error: the stamp's identity is what
// matters, and refusing to read it because a timestamp is odd would be perverse.
func parseStampTime(v sql.NullString) time.Time {
	if !v.Valid || v.String == "" {
		return time.Time{}
	}
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339} {
		if t, err := time.Parse(layout, v.String); err == nil {
			return t
		}
	}
	return time.Time{}
}

// WriteEmbeddingStamp records id as the identity of the stored vectors,
// replacing any previous stamp.
//
// created_at is preserved across updates so that "stamped on" keeps meaning the
// day the vector space was established, not the day it was last touched.
func WriteEmbeddingStamp(ctx context.Context, ex execer, id EmbeddingIdentity, adopted bool) error {
	if !id.Known() {
		return nil
	}
	_, err := ex.ExecContext(ctx, `
		INSERT INTO kb_embedding_stamp
			(id, canonical_model, observed_model, resolved, dimensions, prefix_scheme, provider, adopted)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			canonical_model = excluded.canonical_model,
			observed_model  = excluded.observed_model,
			resolved        = excluded.resolved,
			dimensions      = excluded.dimensions,
			prefix_scheme   = excluded.prefix_scheme,
			provider        = excluded.provider,
			adopted         = excluded.adopted,
			updated_at      = datetime('now')
	`, id.Canonical, id.Observed, boolToInt(id.Resolved), id.Dimensions,
		id.PrefixScheme, id.Provider, boolToInt(adopted))
	if err != nil {
		return fmt.Errorf("write embedding stamp: %w", err)
	}
	return nil
}

// ClearEmbeddingStamp removes the stamp. It is called only as part of a
// deliberate whole-knowledge-base re-embed, immediately before the vectors it
// describes are deleted.
func ClearEmbeddingStamp(ctx context.Context, ex execer) error {
	if _, err := ex.ExecContext(ctx, `DELETE FROM kb_embedding_stamp`); err != nil {
		if isMissingTableErr(err) {
			return nil
		}
		return fmt.Errorf("clear embedding stamp: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
