package daemon

// Runtime attach/detach/reindex for semantic search.
//
// These endpoints predate WP-2.1, when semantic search meant "a Qdrant
// container that may or may not be running" and attaching was how you enabled
// it without restarting the daemon. Vectors now live in the knowledge base file
// itself, so attaching is only about (re)creating the searcher and rewiring the
// indexers -- there is nothing external to wait for.
//
// TODO(WP-3.2): remove with dead-stack teardown. The route names and the CLI
// verbs that call them are vestigial; the whole attach/detach concept goes away
// once semantic search is unconditionally on.

import (
	"context"
	"net/http"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/pkg/models"
)

// handleSemanticAttach enables semantic search at runtime.
func (d *Daemon) handleSemanticAttach(w http.ResponseWriter, r *http.Request) {
	// Check if already attached
	d.mu.RLock()
	if d.kbSemantic != nil {
		d.mu.RUnlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "already_attached",
			"message": "Semantic search is already enabled",
		})
		return
	}
	d.mu.RUnlock()

	semantic, err := kb.NewSemanticSearcher(d.store.DB(), semanticSearchConfig())
	if err != nil {
		writeError(w, http.StatusInternalServerError, models.ErrIndexFailed,
			"Failed to initialize semantic search: "+err.Error())
		return
	}

	// Atomically update daemon components
	d.mu.Lock()
	d.kbSemantic = semantic
	d.kbSource.SetSemanticSearcher(semantic)
	d.kbIndexer.SetSemanticSearcher(semantic)
	d.kbHybrid = kb.NewHybridSearcher(d.kbSearcher, semantic)
	d.mu.Unlock()

	d.logger.Info().Msg("semantic search enabled via hot-reload")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "attached",
		"message": "Semantic search enabled. Use 'conduit kb sync' to index existing documents.",
	})
}

// handleSemanticDetach disables semantic search at runtime.
// This gracefully falls back to FTS5-only search.
func (d *Daemon) handleSemanticDetach(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	if d.kbSemantic == nil {
		d.mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":  "already_detached",
			"message": "Semantic search is already disabled",
		})
		return
	}

	// Gracefully disable semantic search
	d.kbSemantic = nil
	d.kbSource.SetSemanticSearcher(nil)
	d.kbIndexer.SetSemanticSearcher(nil)
	d.kbHybrid = kb.NewHybridSearcher(d.kbSearcher, nil)
	d.mu.Unlock()

	d.logger.Info().Msg("semantic search disabled via hot-reload")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":  "detached",
		"message": "Semantic search disabled. FTS5 fallback active.",
	})
}

// handleSemanticReindex triggers re-indexing of all documents into the vector index.
func (d *Daemon) handleSemanticReindex(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	semantic := d.kbSemantic
	d.mu.RUnlock()

	if semantic == nil {
		writeError(w, http.StatusServiceUnavailable, models.ErrRuntimeUnavailable,
			"Semantic search not enabled.")
		return
	}

	// Run migration in background
	go func() {
		ctx := context.Background()
		err := semantic.MigrateFromFTS(ctx, func(current, total int) {
			d.logger.Info().
				Int("current", current).
				Int("total", total).
				Msg("reindex progress")
		})
		if err != nil {
			d.logger.Error().Err(err).Msg("reindex failed")
		} else {
			d.logger.Info().Msg("reindex completed")
		}
	}()

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"status":  "started",
		"message": "Re-indexing started in background. Check progress with 'conduit kb stats'.",
	})
}

// handleSemanticStatus reports the state of the vector index.
func (d *Daemon) handleSemanticStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	d.mu.RLock()
	semantic := d.kbSemantic
	d.mu.RUnlock()

	vectorInfo := map[string]interface{}{
		"backend": "sqlite",
	}

	if semantic != nil {
		if stats, err := semantic.VectorIndex().GetStats(ctx); err == nil {
			vectorInfo["vector_count"] = stats.VectorCount
			vectorInfo["status"] = stats.Status
			vectorInfo["collection"] = stats.CollectionName
		} else {
			vectorInfo["status"] = "error"
			vectorInfo["error"] = err.Error()
		}
	} else {
		vectorInfo["status"] = "detached"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"vectors": vectorInfo,
		"semantic_search": map[string]interface{}{
			"enabled": semantic != nil,
		},
	})
}
