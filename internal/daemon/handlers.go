package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/pkg/models"
)

// Response helpers

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, code models.ErrorCode, message string) {
	writeJSON(w, status, map[string]interface{}{
		"error": map[string]interface{}{
			"code":    code,
			"message": message,
		},
	})
}

// Health endpoints

// handleHealth returns the health status of the daemon.
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "healthy"
	checks := map[string]string{
		"database": "ok",
	}

	// Check database connectivity
	if err := d.store.Health(r.Context()); err != nil {
		status = "unhealthy"
		checks["database"] = err.Error()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    status,
		"checks":    checks,
		"timestamp": time.Now().Format(time.RFC3339),
	})
}

// handleReady returns whether the daemon is ready to serve requests.
func (d *Daemon) handleReady(w http.ResponseWriter, r *http.Request) {
	if d.Ready() {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ready":     true,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	} else {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"ready":     false,
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

// handleStatus returns the overall daemon status.
func (d *Daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get instance counts by status
	instances, _ := d.store.ListInstances(ctx)
	statusCounts := make(map[string]int)
	for _, inst := range instances {
		statusCounts[string(inst.Status)]++
	}

	// Get binding count
	bindings, _ := d.store.ListBindings(ctx)

	// Build dependencies status
	dependencies := d.getDependencyStatus(ctx)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"daemon": map[string]interface{}{
			"version":    Version,
			"build_time": BuildTime,
			"uptime":     time.Since(d.startTime).String(),
			"ready":      d.Ready(),
		},
		"instances": map[string]interface{}{
			"total":     len(instances),
			"by_status": statusCounts,
		},
		"bindings": map[string]interface{}{
			"total": len(bindings),
		},
		"dependencies": dependencies,
		"timestamp":    time.Now().Format(time.RFC3339),
	})
}

// getDependencyStatus returns the status of all managed dependencies.
func (d *Daemon) getDependencyStatus(ctx context.Context) map[string]interface{} {
	deps := make(map[string]interface{})

	// Container Runtime status
	containerInfo := map[string]interface{}{
		"available": false,
		"runtime":   "none",
		"version":   "",
	}
	if d.kbQdrant != nil && d.kbQdrant.IsAvailable() {
		runtime := d.kbQdrant.GetContainerRuntime()
		containerInfo["available"] = true
		containerInfo["runtime"] = runtime
		containerInfo["managed_by"] = "conduit"
		containerInfo["container"] = d.kbQdrant.GetContainerName()
	}
	deps["container_runtime"] = containerInfo

	// Qdrant (Vector DB) status
	qdrantInfo := map[string]interface{}{
		"available":  false,
		"status":     "unknown",
		"collection": "conduit_kb",
		"vectors":    int64(0),
	}
	if d.kbQdrant != nil {
		health := d.kbQdrant.CheckHealth(ctx)
		qdrantInfo["available"] = health.ContainerRunning && health.APIReachable
		httpPort, grpcPort := d.kbQdrant.GetPorts()
		qdrantInfo["http_port"] = httpPort
		qdrantInfo["grpc_port"] = grpcPort
		if health.ContainerRunning {
			qdrantInfo["status"] = health.CollectionStatus
			if qdrantInfo["status"] == "" {
				qdrantInfo["status"] = "running"
			}
			qdrantInfo["vectors"] = health.TotalPoints
			qdrantInfo["indexed_vectors"] = health.IndexedVectors
			if health.NeedsRecovery {
				qdrantInfo["status"] = "needs_recovery"
			}
		} else {
			qdrantInfo["status"] = "not_running"
		}
		if health.Error != "" {
			qdrantInfo["error"] = health.Error
		}
	}
	deps["qdrant"] = qdrantInfo

	// Semantic Search status
	semanticInfo := map[string]interface{}{
		"enabled":         d.kbSemantic != nil,
		"embedding_model": "nomic-embed-text",
		"embedding_host":  "http://localhost:11434",
	}
	deps["semantic_search"] = semanticInfo

	// FTS5 status (always available)
	fts5Info := map[string]interface{}{
		"available": true,
		"engine":    "SQLite FTS5",
	}
	deps["full_text_search"] = fts5Info

	return deps
}

// Instance endpoints

// handleListInstances returns all connector instances.
func (d *Daemon) handleListInstances(w http.ResponseWriter, r *http.Request) {
	instances, err := d.store.ListInstances(r.Context())
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to list instances")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to list instances")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"instances": instances,
		"count":     len(instances),
	})
}

// handleCreateInstance creates a new connector instance.
func (d *Daemon) handleCreateInstance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PackageID      string            `json:"package_id"`
		PackageVersion string            `json:"package_version"`
		DisplayName    string            `json:"display_name"`
		ImageRef       string            `json:"image_ref"`
		Config         map[string]string `json:"config,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, models.ErrConfigInvalid, "invalid request body")
		return
	}

	// TODO: Validate package and run audit
	// For now, create instance directly

	instance := &models.ConnectorInstance{
		InstanceID:     generateID("inst"),
		PackageID:      req.PackageID,
		PackageVersion: req.PackageVersion,
		DisplayName:    req.DisplayName,
		ImageRef:       req.ImageRef,
		Config:         req.Config,
		Status:         models.StatusCreated,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := d.store.CreateInstance(r.Context(), instance); err != nil {
		d.logger.Error().Err(err).Msg("failed to create instance")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to create instance")
		return
	}

	writeJSON(w, http.StatusCreated, instance)
}

// handleGetInstance returns a specific instance.
func (d *Daemon) handleGetInstance(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")

	instance, err := d.store.GetInstance(r.Context(), instanceID)
	if err != nil {
		if conduitErr, ok := err.(*models.ConduitError); ok && conduitErr.Code == models.ErrInstanceNotFound {
			writeError(w, http.StatusNotFound, models.ErrInstanceNotFound, "instance not found")
			return
		}
		d.logger.Error().Err(err).Msg("failed to get instance")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to get instance")
		return
	}

	writeJSON(w, http.StatusOK, instance)
}

// handleDeleteInstance removes an instance.
func (d *Daemon) handleDeleteInstance(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")

	// TODO: Stop container if running, cleanup resources

	if err := d.store.DeleteInstance(r.Context(), instanceID); err != nil {
		if conduitErr, ok := err.(*models.ConduitError); ok && conduitErr.Code == models.ErrInstanceNotFound {
			writeError(w, http.StatusNotFound, models.ErrInstanceNotFound, "instance not found")
			return
		}
		d.logger.Error().Err(err).Msg("failed to delete instance")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to delete instance")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleStartInstance starts a connector instance.
func (d *Daemon) handleStartInstance(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")

	instance, err := d.store.GetInstance(r.Context(), instanceID)
	if err != nil {
		if conduitErr, ok := err.(*models.ConduitError); ok && conduitErr.Code == models.ErrInstanceNotFound {
			writeError(w, http.StatusNotFound, models.ErrInstanceNotFound, "instance not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to get instance")
		return
	}

	// Check valid transition
	if !models.IsValidTransition(instance.Status, models.StatusStarting) {
		writeError(w, http.StatusConflict, models.ErrInvalidTransition,
			"cannot start instance in current state: "+string(instance.Status))
		return
	}

	// TODO: Start container using RuntimeProvider
	// For now, just update status

	if err := d.store.UpdateInstanceStatus(r.Context(), instanceID, models.StatusRunning, ""); err != nil {
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update instance status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"instance_id": instanceID,
		"status":      models.StatusRunning,
		"message":     "instance started",
	})
}

// handleStopInstance stops a connector instance.
func (d *Daemon) handleStopInstance(w http.ResponseWriter, r *http.Request) {
	instanceID := chi.URLParam(r, "instanceID")

	instance, err := d.store.GetInstance(r.Context(), instanceID)
	if err != nil {
		if conduitErr, ok := err.(*models.ConduitError); ok && conduitErr.Code == models.ErrInstanceNotFound {
			writeError(w, http.StatusNotFound, models.ErrInstanceNotFound, "instance not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to get instance")
		return
	}

	// Check valid transition
	if !models.IsValidTransition(instance.Status, models.StatusStopping) {
		writeError(w, http.StatusConflict, models.ErrInvalidTransition,
			"cannot stop instance in current state: "+string(instance.Status))
		return
	}

	// TODO: Stop container using RuntimeProvider

	if err := d.store.UpdateInstanceStopped(r.Context(), instanceID); err != nil {
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to update instance status")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"instance_id": instanceID,
		"status":      models.StatusStopped,
		"message":     "instance stopped",
	})
}

// Binding endpoints

// handleListBindings returns all bindings.
func (d *Daemon) handleListBindings(w http.ResponseWriter, r *http.Request) {
	bindings, err := d.store.ListBindings(r.Context())
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to list bindings")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to list bindings")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"bindings": bindings,
		"count":    len(bindings),
	})
}

// handleCreateBinding creates a new client binding.
func (d *Daemon) handleCreateBinding(w http.ResponseWriter, r *http.Request) {
	var req struct {
		InstanceID string `json:"instance_id"`
		ClientID   string `json:"client_id"`
		Scope      string `json:"scope,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, models.ErrConfigInvalid, "invalid request body")
		return
	}

	if req.Scope == "" {
		req.Scope = "project"
	}

	// TODO: Use adapter to inject MCP configuration
	// For now, create binding record

	binding := &models.ClientBinding{
		BindingID:   generateID("bind"),
		InstanceID:  req.InstanceID,
		ClientID:    req.ClientID,
		Scope:       req.Scope,
		ConfigPath:  "", // Will be set by adapter
		ChangeSetID: generateID("cs"),
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	if err := d.store.CreateBinding(r.Context(), binding); err != nil {
		d.logger.Error().Err(err).Msg("failed to create binding")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to create binding")
		return
	}

	writeJSON(w, http.StatusCreated, binding)
}

// handleGetBinding returns a specific binding.
func (d *Daemon) handleGetBinding(w http.ResponseWriter, r *http.Request) {
	bindingID := chi.URLParam(r, "bindingID")

	binding, err := d.store.GetBinding(r.Context(), bindingID)
	if err != nil {
		if conduitErr, ok := err.(*models.ConduitError); ok && conduitErr.Code == models.ErrBindingNotFound {
			writeError(w, http.StatusNotFound, models.ErrBindingNotFound, "binding not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to get binding")
		return
	}

	writeJSON(w, http.StatusOK, binding)
}

// handleDeleteBinding removes a binding.
func (d *Daemon) handleDeleteBinding(w http.ResponseWriter, r *http.Request) {
	bindingID := chi.URLParam(r, "bindingID")

	// TODO: Use adapter to rollback MCP configuration

	if err := d.store.DeleteBinding(r.Context(), bindingID); err != nil {
		if conduitErr, ok := err.(*models.ConduitError); ok && conduitErr.Code == models.ErrBindingNotFound {
			writeError(w, http.StatusNotFound, models.ErrBindingNotFound, "binding not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to delete binding")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// Client endpoints

// handleListClients returns all detected clients.
func (d *Daemon) handleListClients(w http.ResponseWriter, r *http.Request) {
	results, err := d.adapters.DetectAll(r.Context())
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to detect clients")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to detect clients")
		return
	}

	var clients []models.ClientInfo
	for clientID, detect := range results {
		adapter, _ := d.adapters.Get(clientID)
		clients = append(clients, models.ClientInfo{
			ClientID:    clientID,
			DisplayName: adapter.DisplayName(),
			Installed:   detect.Installed,
			Version:     detect.Version,
			Notes:       detect.Notes,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"clients": clients,
		"count":   len(clients),
	})
}

// handleGetClient returns a specific client.
func (d *Daemon) handleGetClient(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientID")

	adapter, err := d.adapters.Get(clientID)
	if err != nil {
		writeError(w, http.StatusNotFound, "E_NOT_FOUND", "client not found")
		return
	}

	detect, err := adapter.Detect(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "detection failed")
		return
	}

	writeJSON(w, http.StatusOK, models.ClientInfo{
		ClientID:    clientID,
		DisplayName: adapter.DisplayName(),
		Installed:   detect.Installed,
		Version:     detect.Version,
		Notes:       detect.Notes,
	})
}

// KB endpoints

// handleListKBSources returns all KB sources.
func (d *Daemon) handleListKBSources(w http.ResponseWriter, r *http.Request) {
	sources, err := d.kbSource.List(r.Context())
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to list KB sources")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", "failed to list sources")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sources": sources,
		"count":   len(sources),
	})
}

// handleAddKBSource adds a new KB source.
func (d *Daemon) handleAddKBSource(w http.ResponseWriter, r *http.Request) {
	var req kb.AddSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, models.ErrConfigInvalid, "invalid request body")
		return
	}

	source, err := d.kbSource.Add(r.Context(), req)
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to add KB source")
		writeError(w, http.StatusBadRequest, models.ErrConfigInvalid, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, source)
}

// handleGetKBSource returns a specific KB source.
func (d *Daemon) handleGetKBSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")

	source, err := d.kbSource.Get(r.Context(), sourceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "E_NOT_FOUND", "source not found")
		return
	}

	writeJSON(w, http.StatusOK, source)
}

// handleDeleteKBSource removes a KB source.
func (d *Daemon) handleDeleteKBSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")

	result, err := d.kbSource.Remove(r.Context(), sourceID)
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to remove KB source")
		writeError(w, http.StatusNotFound, "E_NOT_FOUND", err.Error())
		return
	}

	d.logger.Info().
		Str("source_id", sourceID).
		Int("documents", result.DocumentsDeleted).
		Int("vectors", result.VectorsDeleted).
		Msg("removed KB source")

	// Return the removal statistics for transparency
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"documents_deleted": result.DocumentsDeleted,
		"vectors_deleted":   result.VectorsDeleted,
	})
}

// handleSyncKBSource triggers a sync for a KB source.
func (d *Daemon) handleSyncKBSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceID")

	result, err := d.kbSource.Sync(r.Context(), sourceID)
	if err != nil {
		d.logger.Error().Err(err).Msg("failed to sync KB source")
		writeError(w, http.StatusInternalServerError, "E_INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleKBSearch searches the knowledge base.
// Supports modes: "hybrid" (default), "semantic", "fts5"
// Use raw=true to skip result processing (chunk merging, boilerplate filtering)
func (d *Daemon) handleKBSearch(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, models.ErrConfigInvalid, "query parameter 'q' is required")
		return
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "hybrid" // Default to hybrid
	}

	// Check if raw results are requested
	rawResults := r.URL.Query().Get("raw") == "true"

	ctx := r.Context()

	switch mode {
	case "semantic":
		// Force semantic search only
		if d.kbSemantic == nil {
			writeError(w, http.StatusServiceUnavailable, "E_SEMANTIC_UNAVAILABLE",
				"semantic search unavailable: Qdrant or Ollama not running")
			return
		}
		result, err := d.kbSemantic.Search(ctx, query, d.kbSemanticOpts(r))
		if err != nil {
			d.logger.Error().Err(err).Msg("semantic search failed")
			writeError(w, http.StatusInternalServerError, "E_INTERNAL", "semantic search failed")
			return
		}
		if rawResults {
			writeJSON(w, http.StatusOK, d.convertSemanticResult(result, "semantic"))
		} else {
			writeJSON(w, http.StatusOK, d.processSemanticResult(result, "semantic"))
		}

	case "fts5":
		// Force FTS5 keyword search only
		result, err := d.kbSearcher.Search(ctx, query, d.kbSearchOpts(r))
		if err != nil {
			d.logger.Error().Err(err).Msg("fts5 search failed")
			writeError(w, http.StatusInternalServerError, "E_INTERNAL", "fts5 search failed")
			return
		}
		if rawResults {
			resp := map[string]interface{}{
				"results":     result.Results,
				"total_hits":  result.TotalHits,
				"query":       result.Query,
				"search_time": result.SearchTime,
				"search_mode": "fts5",
				"processed":   false,
			}
			writeJSON(w, http.StatusOK, resp)
		} else {
			writeJSON(w, http.StatusOK, d.processFTS5Result(result, "fts5"))
		}

	case "hybrid":
		fallthrough
	default:
		// True hybrid search using RRF (Reciprocal Rank Fusion)
		hybridOpts := d.kbHybridOpts(r)
		result, err := d.kbHybrid.Search(ctx, query, hybridOpts)
		if err != nil {
			d.logger.Error().Err(err).Msg("hybrid search failed")
			writeError(w, http.StatusInternalServerError, "E_INTERNAL", "hybrid search failed")
			return
		}

		if rawResults {
			resp := map[string]interface{}{
				"results":        result.Results,
				"total_hits":     result.TotalHits,
				"query":          result.Query,
				"search_time":    result.SearchTime,
				"search_mode":    string(result.Mode),
				"fts_hits":       result.FTSHits,
				"semantic_hits":  result.SemanticHits,
				"query_analysis": result.QueryAnalysis,
				"processed":      false,
			}
			writeJSON(w, http.StatusOK, resp)
		} else {
			writeJSON(w, http.StatusOK, d.processHybridResult(result))
		}
	}
}

// kbHybridOpts parses hybrid search options from request.
// Uses RAG config defaults, with query parameter overrides for advanced users.
func (d *Daemon) kbHybridOpts(r *http.Request) kb.HybridSearchOptions {
	ragCfg := d.cfg.KB.RAG

	// Start with config defaults
	opts := kb.HybridSearchOptions{
		Limit:           ragCfg.DefaultLimit,
		Mode:            kb.HybridModeAuto,
		SemanticWeight:  ragCfg.SemanticWeight,
		RRFConstant:     60,
		EnableMMR:       ragCfg.EnableMMR,
		MMRLambda:       ragCfg.MMRLambda,
		SimilarityFloor: ragCfg.MinScore,
		EnableRerank:    ragCfg.EnableRerank,
	}

	// Fallback to safe defaults if config values are zero
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	// Note: SimilarityFloor of 0 is valid (no filtering), only set default if negative
	if opts.SimilarityFloor < 0 {
		opts.SimilarityFloor = 0.0
	}

	// Query parameter overrides (advanced mode)
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}

	if modeStr := r.URL.Query().Get("hybrid_mode"); modeStr != "" {
		switch modeStr {
		case "fusion":
			opts.Mode = kb.HybridModeFusion
		case "semantic":
			opts.Mode = kb.HybridModeSemantic
		case "lexical":
			opts.Mode = kb.HybridModeLexical
		default:
			opts.Mode = kb.HybridModeAuto
		}
	}

	// Advanced RAG parameter overrides
	if minScoreStr := r.URL.Query().Get("min_score"); minScoreStr != "" {
		if minScore, err := strconv.ParseFloat(minScoreStr, 64); err == nil && minScore >= 0 && minScore <= 1 {
			opts.SimilarityFloor = minScore
		}
	}

	if semWeightStr := r.URL.Query().Get("semantic_weight"); semWeightStr != "" {
		if semWeight, err := strconv.ParseFloat(semWeightStr, 64); err == nil && semWeight >= 0 && semWeight <= 1 {
			opts.SemanticWeight = semWeight
		}
	}

	if mmrLambdaStr := r.URL.Query().Get("mmr_lambda"); mmrLambdaStr != "" {
		if mmrLambda, err := strconv.ParseFloat(mmrLambdaStr, 64); err == nil && mmrLambda >= 0 && mmrLambda <= 1 {
			opts.MMRLambda = mmrLambda
		}
	}

	if mmrStr := r.URL.Query().Get("enable_mmr"); mmrStr != "" {
		opts.EnableMMR = mmrStr == "true" || mmrStr == "1"
	}

	if rerankStr := r.URL.Query().Get("enable_rerank"); rerankStr != "" {
		opts.EnableRerank = rerankStr == "true" || rerankStr == "1"
	}

	return opts
}

// processHybridResult processes hybrid search results for cleaner output.
func (d *Daemon) processHybridResult(result *kb.HybridSearchResult) map[string]interface{} {
	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(result.Results)

	return map[string]interface{}{
		"results":        processed,
		"total_hits":     result.TotalHits,
		"query":          result.Query,
		"search_time":    result.SearchTime,
		"search_mode":    string(result.Mode),
		"fts_hits":       result.FTSHits,
		"semantic_hits":  result.SemanticHits,
		"query_analysis": result.QueryAnalysis,
		"processed":      true,
	}
}

// kbSemanticOpts parses semantic search options from request.
// Uses RAG config defaults, with query parameter overrides for advanced users.
func (d *Daemon) kbSemanticOpts(r *http.Request) kb.SemanticSearchOptions {
	ragCfg := d.cfg.KB.RAG

	// Start with config defaults
	opts := kb.SemanticSearchOptions{
		Limit:      ragCfg.DefaultLimit,
		MinScore:   ragCfg.MinScore,
		ContextLen: 300,
	}

	// Fallback to safe defaults if config values are zero
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	// Note: MinScore of 0 is valid (no filtering), only set default if negative
	if opts.MinScore < 0 {
		opts.MinScore = 0.0
	}

	// Query parameter overrides
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}

	if sourceID := r.URL.Query().Get("source_id"); sourceID != "" {
		opts.SourceIDs = []string{sourceID}
	}

	// Advanced: min_score override
	if minScoreStr := r.URL.Query().Get("min_score"); minScoreStr != "" {
		if minScore, err := strconv.ParseFloat(minScoreStr, 64); err == nil && minScore >= 0 && minScore <= 1 {
			opts.MinScore = minScore
		}
	}

	return opts
}

// convertSemanticResult converts semantic search results to a common response format.
func (d *Daemon) convertSemanticResult(result *kb.SemanticSearchResult, mode string) map[string]interface{} {
	// Convert semantic hits to common format
	results := make([]map[string]interface{}, len(result.Results))
	for i, hit := range result.Results {
		results[i] = map[string]interface{}{
			"document_id": hit.DocumentID,
			"chunk_id":    hit.ChunkID,
			"path":        hit.Path,
			"title":       hit.Title,
			"snippet":     hit.Snippet,
			"score":       hit.Score,
			"confidence":  hit.Confidence,
			"metadata":    hit.Metadata,
		}
	}

	return map[string]interface{}{
		"results":     results,
		"total_hits":  result.TotalHits,
		"query":       result.Query,
		"search_time": result.SearchTime,
		"search_mode": mode,
		"processed":   false,
	}
}

// processSemanticResult processes semantic search results with chunk merging and boilerplate filtering.
func (d *Daemon) processSemanticResult(result *kb.SemanticSearchResult, mode string) map[string]interface{} {
	// Convert semantic hits to SearchHit format for processing
	hits := make([]kb.SearchHit, len(result.Results))
	for i, r := range result.Results {
		hits[i] = kb.SearchHit{
			DocumentID: r.DocumentID,
			ChunkID:    r.ChunkID,
			Path:       r.Path,
			Title:      r.Title,
			Snippet:    r.Snippet,
			Score:      r.Score,
			Metadata:   r.Metadata,
		}
	}

	// Process results
	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(hits)

	// Convert to response format
	results := make([]map[string]interface{}, len(processed))
	for i, p := range processed {
		results[i] = map[string]interface{}{
			"document_id": p.DocumentID,
			"path":        p.Path,
			"title":       p.Title,
			"content":     p.Content,
			"score":       p.Score,
			"chunk_count": p.ChunkCount,
			"metadata":    p.Metadata,
			"source":      p.Source,
		}
	}

	return map[string]interface{}{
		"results":     results,
		"total_hits":  result.TotalHits,
		"query":       result.Query,
		"search_time": result.SearchTime,
		"search_mode": mode,
		"processed":   true,
	}
}

// processFTS5Result processes FTS5 search results with chunk merging and boilerplate filtering.
func (d *Daemon) processFTS5Result(result *kb.SearchResult, mode string) map[string]interface{} {
	// Process results
	processor := kb.NewResultProcessor()
	processed := processor.ProcessResults(result.Results)

	// Convert to response format
	results := make([]map[string]interface{}, len(processed))
	for i, p := range processed {
		results[i] = map[string]interface{}{
			"document_id": p.DocumentID,
			"path":        p.Path,
			"title":       p.Title,
			"content":     p.Content,
			"score":       p.Score,
			"chunk_count": p.ChunkCount,
			"metadata":    p.Metadata,
			"source":      p.Source,
		}
	}

	return map[string]interface{}{
		"results":     results,
		"total_hits":  result.TotalHits,
		"query":       result.Query,
		"search_time": result.SearchTime,
		"search_mode": mode,
		"processed":   true,
	}
}

// Helper functions

func generateID(prefix string) string {
	return prefix + "_" + randomString(12)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond) // Add some entropy
	}
	return string(b)
}

// kbSearchOpts parses search options from request.
func (d *Daemon) kbSearchOpts(r *http.Request) kb.SearchOptions {
	opts := kb.SearchOptions{
		Limit:     10,
		Highlight: true,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			opts.Limit = limit
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if offset, err := strconv.Atoi(offsetStr); err == nil && offset >= 0 {
			opts.Offset = offset
		}
	}

	if sourceID := r.URL.Query().Get("source_id"); sourceID != "" {
		opts.SourceIDs = []string{sourceID}
	}

	return opts
}

// handleKBMigrate migrates existing FTS-indexed documents to vector search.
func (d *Daemon) handleKBMigrate(w http.ResponseWriter, r *http.Request) {
	if d.kbSemantic == nil {
		writeError(w, http.StatusServiceUnavailable, "E_SEMANTIC_UNAVAILABLE",
			"semantic search unavailable: Qdrant or Ollama not running")
		return
	}

	// Use background context to avoid cancellation when HTTP client times out
	// Migration is a long-running operation that should complete even if client disconnects
	ctx := context.Background()

	// Run migration
	var migratedCount int
	progressFn := func(current, total int) {
		migratedCount = current
		d.logger.Info().
			Int("current", current).
			Int("total", total).
			Msg("migration progress")
	}

	if err := d.kbSemantic.MigrateFromFTS(ctx, progressFn); err != nil {
		d.logger.Error().Err(err).Msg("migration failed")
		writeError(w, http.StatusInternalServerError, "E_MIGRATION_FAILED", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":   "completed",
		"migrated": migratedCount,
	})
}

// Version information (set at build time)
var (
	Version   = "dev"
	BuildTime = "unknown"
)
