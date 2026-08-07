package kb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// EmbedProbe is the part of an embedder that capability detection needs.
//
// kb.Embedder satisfies it. Taking an interface rather than assuming Ollama on
// port 11434 is the point: since WP-3.2 the embedding provider is configurable
// ("llama-server", "ollama" or "none"), so nothing here may hardcode one.
type EmbedProbe interface {
	// Model reports the embedding model identifier.
	Model() string

	// HealthCheck verifies the provider can currently serve requests.
	HealthCheck(ctx context.Context) error
}

// Capabilities describes the available search features.
type Capabilities struct {
	// FTS5Available indicates if SQLite FTS5 is available
	FTS5Available bool `json:"fts5_available"`

	// SemanticAvailable indicates if semantic search is available
	// (vector index present + embedding provider reachable)
	SemanticAvailable bool `json:"semantic_available"`

	// VectorStatus describes the state of the in-database vector index
	VectorStatus string `json:"vector_status"`

	// EmbeddingModel is the model that BUILT the stored vectors, read from the
	// knowledge base's embedding stamp (WP-4.3). It falls back to the configured
	// model when nothing has been stamped, and is empty when embeddings are
	// disabled.
	//
	// It used to report the configured model unconditionally, which quietly
	// asserted that the current model had produced the stored vectors -- the one
	// claim that is false in precisely the situation a reader needs to know
	// about.
	EmbeddingModel string `json:"embedding_model,omitempty"`

	// ActiveEmbeddingModel is the model configured now. It is set only when it
	// differs from EmbeddingModel, i.e. when the vectors need rebuilding.
	ActiveEmbeddingModel string `json:"active_embedding_model,omitempty"`

	// EmbedStatus describes embedding provider connectivity.
	EmbedStatus string `json:"embed_status"`
}

// DefaultProbeTimeout bounds a capability probe so a status command cannot
// hang on an unreachable provider.
const DefaultProbeTimeout = 5 * time.Second

// DetectCapabilities checks available search features.
//
// A nil embedder means embeddings are switched off (embed.provider = "none"),
// which is a supported configuration and not an error: lexical search still
// works and SemanticAvailable is simply false.
func DetectCapabilities(ctx context.Context, db *sql.DB, embedder EmbedProbe) *Capabilities {
	return DetectCapabilitiesWithTimeout(ctx, db, embedder, DefaultProbeTimeout)
}

// DetectCapabilitiesWithTimeout is DetectCapabilities with an explicit probe
// budget. `conduit doctor` uses a longer one than `mcp status` because starting
// a cold embedding sidecar is exactly what doctor is being asked to test.
func DetectCapabilitiesWithTimeout(ctx context.Context, db *sql.DB, embedder EmbedProbe, probeTimeout time.Duration) *Capabilities {
	caps := &Capabilities{}

	// Check FTS5
	caps.FTS5Available = checkFTS5(ctx, db)

	// Check the vector index
	vectorOK, vectorStatus := checkVectorIndex(ctx, db)
	caps.VectorStatus = vectorStatus

	// Check the embedding provider
	embedOK, embedStatus := checkEmbedder(ctx, embedder, probeTimeout)
	caps.EmbedStatus = embedStatus

	modelMismatch := false
	if embedder != nil {
		caps.EmbeddingModel = embedder.Model()

		// Prefer what actually built the vectors over what is configured.
		if db != nil {
			if stamp, err := ReadEmbeddingStamp(ctx, db); err == nil && stamp != nil && stamp.Known() {
				active := NewEmbeddingIdentity(embedder.Model(), "", probeDimension(embedder), "")
				caps.EmbeddingModel = stamp.Canonical
				if !strings.EqualFold(stamp.Canonical, active.Canonical) {
					caps.ActiveEmbeddingModel = active.Canonical
				}
				modelMismatch = stamp.Compare(active) == StampMismatch
			}
		}
	}

	// Semantic search needs somewhere to search, something to embed with, and
	// vectors that the embedder can actually be compared against. A reachable
	// provider over a foreign vector space is not a working capability, and
	// reporting it as one is what let issue #107 stay invisible.
	caps.SemanticAvailable = vectorOK && embedOK && !modelMismatch

	return caps
}

// probeDimension recovers the embedder's vector width from an EmbedProbe.
//
// EmbedProbe is deliberately narrow -- Model and HealthCheck are all capability
// detection needs to know about a provider -- but the width matters here, and
// every production embedder is a kb.Embedder, which has it.
//
// Omitting this made the capability report a lie in the one case it exists to
// catch: with a zero width, Compare's width branch cannot fire, so a
// same-model-different-width knowledge base reported "semantic search:
// available" while every search refused. A capability that is reported but
// cannot be exercised is precisely the #107 defect in a status line.
//
// A provider that genuinely cannot say returns 0, and Compare then ignores the
// width, which is the correct conservative fallback: this must never be
// stricter than the guard that enforces.
func probeDimension(embedder EmbedProbe) int {
	if d, ok := embedder.(interface{ Dimension() int }); ok {
		return d.Dimension()
	}
	return 0
}

// checkFTS5 verifies FTS5 extension is available.
func checkFTS5(ctx context.Context, db *sql.DB) bool {
	if db == nil {
		return false
	}

	// Check if kb_fts table exists (FTS5 virtual table)
	var exists int
	err := db.QueryRowContext(ctx,
		"SELECT 1 FROM sqlite_master WHERE type='table' AND name='kb_fts'").Scan(&exists)
	if err != nil {
		return false
	}

	return exists == 1
}

// checkVectorIndex verifies the in-database vector table is usable.
//
// There is no service to reach: the index is a table in the knowledge base
// file, so "available" means the schema is present. An empty table is still
// available -- it just has nothing to return yet.
func checkVectorIndex(ctx context.Context, db *sql.DB) (bool, string) {
	if db == nil {
		return false, "no database"
	}

	var name string
	err := db.QueryRowContext(ctx,
		"SELECT name FROM sqlite_master WHERE type='table' AND name='kb_vectors'").Scan(&name)
	if err == sql.ErrNoRows {
		return false, "not initialized"
	}
	if err != nil {
		return false, "unavailable"
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM kb_vectors").Scan(&count); err != nil {
		return false, "unavailable"
	}
	if count == 0 {
		return true, "ready (empty)"
	}
	return true, fmt.Sprintf("ready (%d vectors)", count)
}

// checkEmbedder probes the configured embedding provider.
func checkEmbedder(ctx context.Context, embedder EmbedProbe, timeout time.Duration) (bool, string) {
	if embedder == nil {
		return false, "disabled (embed.provider = none)"
	}
	if timeout <= 0 {
		timeout = DefaultProbeTimeout
	}

	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := embedder.HealthCheck(probeCtx); err != nil {
		if probeCtx.Err() != nil {
			return false, fmt.Sprintf("not reachable (no response within %s)", timeout)
		}
		return false, fmt.Sprintf("not reachable: %v", err)
	}
	return true, "connected"
}

// Summary returns a human-readable summary of capabilities.
func (c *Capabilities) Summary() string {
	var status string

	if c.FTS5Available {
		status += "FTS5: available\n"
	} else {
		status += "FTS5: not available\n"
	}

	switch {
	case c.SemanticAvailable:
		status += fmt.Sprintf("Semantic: available (model: %s)\n", c.EmbeddingModel)
	case c.ActiveEmbeddingModel != "":
		// Naming both models is the whole point: "not available" alone would
		// send a user to look at a provider that is working perfectly.
		status += fmt.Sprintf("Semantic: not available (vectors were built by %s, "+
			"current model is %s; run `%s`)\n",
			c.EmbeddingModel, c.ActiveEmbeddingModel, RebuildRemedy)
	default:
		status += fmt.Sprintf("Semantic: not available (vectors: %s, embeddings: %s)\n",
			c.VectorStatus, c.EmbedStatus)
	}

	return status
}

// SearchMode returns the recommended search mode based on capabilities.
func (c *Capabilities) SearchMode() string {
	if c.SemanticAvailable {
		return "hybrid"
	}
	if c.FTS5Available {
		return "fts5"
	}
	return "none"
}
