package daemon

import (
	"context"
	"net/http"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/pkg/models"
)

// handleQdrantAttach enables semantic search at runtime.
// This allows the daemon to start using Qdrant without a restart.
func (d *Daemon) handleQdrantAttach(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

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

	// Re-check if Qdrant is now available
	if err := d.kbQdrant.EnsureReady(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, models.ErrRuntimeUnavailable,
			"Qdrant not ready: "+err.Error())
		return
	}

	if !d.kbQdrant.IsAvailable() {
		writeError(w, http.StatusServiceUnavailable, models.ErrRuntimeUnavailable,
			"No container runtime available for Qdrant")
		return
	}

	// Initialize semantic search with the same config as in daemon.go New()
	semanticCfg := kb.SemanticSearchConfig{
		EmbeddingConfig: kb.EmbeddingConfig{
			OllamaHost: "http://localhost:11434",
			Model:      "nomic-embed-text",
			Dimension:  768,
			BatchSize:  10,
		},
		VectorStoreConfig: kb.VectorStoreConfig{
			Host:           "localhost",
			Port:           6334, // gRPC port
			CollectionName: "conduit_kb",
			Dimension:      768,
			BatchSize:      100,
		},
	}

	semantic, err := kb.NewSemanticSearcher(d.store.DB(), semanticCfg)
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

// handleQdrantDetach disables semantic search at runtime.
// This gracefully falls back to FTS5-only search.
func (d *Daemon) handleQdrantDetach(w http.ResponseWriter, r *http.Request) {
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

// handleQdrantReindex triggers re-indexing of all documents into vector store.
func (d *Daemon) handleQdrantReindex(w http.ResponseWriter, r *http.Request) {
	d.mu.RLock()
	semantic := d.kbSemantic
	d.mu.RUnlock()

	if semantic == nil {
		writeError(w, http.StatusServiceUnavailable, models.ErrRuntimeUnavailable,
			"Semantic search not enabled. Run 'conduit qdrant attach' first.")
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

// handleQdrantStatus returns the current Qdrant status.
func (d *Daemon) handleQdrantStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get Qdrant health
	health := d.kbQdrant.CheckHealth(ctx)

	// Check if semantic search is enabled in daemon
	d.mu.RLock()
	semanticEnabled := d.kbSemantic != nil
	d.mu.RUnlock()

	status := map[string]interface{}{
		"qdrant": map[string]interface{}{
			"container_running": health.ContainerRunning,
			"api_reachable":     health.APIReachable,
			"collection_status": health.CollectionStatus,
			"indexed_vectors":   health.IndexedVectors,
			"total_points":      health.TotalPoints,
			"needs_recovery":    health.NeedsRecovery,
		},
		"semantic_search": map[string]interface{}{
			"enabled": semanticEnabled,
		},
		"container_runtime": d.kbQdrant.GetContainerRuntime(),
	}

	if health.Error != "" {
		status["qdrant"].(map[string]interface{})["error"] = health.Error
	}

	writeJSON(w, http.StatusOK, status)
}
