// Package daemon implements the Conduit daemon core.
package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/adapters"
	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/observability"
	"github.com/simpleflo/conduit/internal/store"
)

// Daemon is the core Conduit daemon.
type Daemon struct {
	cfg    *config.Config
	store  *store.Store
	router chi.Router
	server *http.Server
	logger zerolog.Logger

	// Module managers
	adapters     adapters.Registry
	kbSource     *kb.SourceManager
	kbSearcher   *kb.Searcher
	kbIndexer    *kb.Indexer
	kbSemantic   *kb.SemanticSearcher // Optional: nil if Qdrant/Ollama unavailable
	kbHybrid     *kb.HybridSearcher   // Combines FTS5 and semantic search
	kbQdrant     *kb.QdrantManager    // Manages Qdrant container lifecycle

	// State
	mu        sync.RWMutex
	running   bool
	ready     bool
	startTime time.Time

	// Shutdown
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// New creates a new Daemon instance.
func New(cfg *config.Config) (*Daemon, error) {
	// Ensure directories exist
	if err := cfg.EnsureDirectories(); err != nil {
		return nil, fmt.Errorf("create directories: %w", err)
	}

	// Initialize store
	st, err := store.New(cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("create store: %w", err)
	}

	// Initialize adapters registry with all built-in adapters
	adapterRegistry := adapters.DefaultRegistry(st.DB())

	// Initialize KB services
	kbSource := kb.NewSourceManager(st.DB())
	kbSearcher := kb.NewSearcher(st.DB())
	kbIndexer := kb.NewIndexer(st.DB())

	logger := observability.Logger("daemon")

	// Initialize Qdrant manager for managed container lifecycle
	// This ensures storage directory exists, container is running, and collection is healthy
	qdrantCfg := kb.QdrantConfig{
		DataDir:        cfg.DataDir,
		ContainerName:  "conduit-qdrant",
		HTTPPort:       6333,
		GRPCPort:       6334,
		CollectionName: "conduit_kb",
	}
	kbQdrant := kb.NewQdrantManager(qdrantCfg)

	// Try to ensure Qdrant is ready (non-blocking if container runtime unavailable)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	if err := kbQdrant.EnsureReady(ctx); err != nil {
		logger.Warn().Err(err).Msg("Qdrant not ready, semantic search will be unavailable")
	}
	cancel()

	// Try to initialize semantic search (optional - graceful if unavailable)
	var kbSemantic *kb.SemanticSearcher
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

	// Only initialize semantic search if Qdrant manager detected a container runtime
	if kbQdrant.IsAvailable() {
		kbSemantic, err = kb.NewSemanticSearcher(st.DB(), semanticCfg)
		if err != nil {
			logger.Warn().Err(err).Msg("semantic search unavailable, falling back to FTS5 only")
			kbSemantic = nil
		} else {
			// Wire semantic search into both indexers for new documents
			// The SourceManager has its own internal indexer that does the actual syncing
			kbSource.SetSemanticSearcher(kbSemantic)
			kbIndexer.SetSemanticSearcher(kbSemantic)
			logger.Info().Msg("semantic search enabled")
		}
	} else {
		logger.Info().Msg("no container runtime available, semantic search disabled")
	}

	// Create hybrid searcher (always available - falls back to FTS5 if semantic unavailable)
	kbHybrid := kb.NewHybridSearcher(kbSearcher, kbSemantic)

	d := &Daemon{
		cfg:        cfg,
		store:      st,
		logger:     logger,
		adapters:   adapterRegistry,
		kbSource:   kbSource,
		kbSearcher: kbSearcher,
		kbIndexer:  kbIndexer,
		kbSemantic: kbSemantic,
		kbHybrid:   kbHybrid,
		kbQdrant:   kbQdrant,
		shutdownCh: make(chan struct{}),
	}

	// Setup router
	d.setupRouter()

	return d, nil
}

// setupRouter configures the HTTP router.
func (d *Daemon) setupRouter() {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(d.loggingMiddleware)

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Health endpoints
		r.Get("/health", d.handleHealth)
		r.Get("/ready", d.handleReady)

		// Instance endpoints
		r.Route("/instances", func(r chi.Router) {
			r.Get("/", d.handleListInstances)
			r.Post("/", d.handleCreateInstance)
			r.Get("/{instanceID}", d.handleGetInstance)
			r.Delete("/{instanceID}", d.handleDeleteInstance)
			r.Post("/{instanceID}/start", d.handleStartInstance)
			r.Post("/{instanceID}/stop", d.handleStopInstance)
		})

		// Binding endpoints
		r.Route("/bindings", func(r chi.Router) {
			r.Get("/", d.handleListBindings)
			r.Post("/", d.handleCreateBinding)
			r.Get("/{bindingID}", d.handleGetBinding)
			r.Delete("/{bindingID}", d.handleDeleteBinding)
		})

		// Client endpoints
		r.Route("/clients", func(r chi.Router) {
			r.Get("/", d.handleListClients)
			r.Get("/{clientID}", d.handleGetClient)
		})

		// KB endpoints
		r.Route("/kb", func(r chi.Router) {
			r.Route("/sources", func(r chi.Router) {
				r.Get("/", d.handleListKBSources)
				r.Post("/", d.handleAddKBSource)
				r.Get("/{sourceID}", d.handleGetKBSource)
				r.Delete("/{sourceID}", d.handleDeleteKBSource)
				r.Post("/{sourceID}/sync", d.handleSyncKBSource)
			})
			r.Get("/search", d.handleKBSearch)
			r.Post("/migrate", d.handleKBMigrate)
		})

		// Status endpoint
		r.Get("/status", d.handleStatus)
	})

	d.router = r
}

// loggingMiddleware logs HTTP requests.
func (d *Daemon) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		d.logger.Debug().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", ww.Status()).
			Dur("duration", time.Since(start)).
			Str("request_id", middleware.GetReqID(r.Context())).
			Msg("request completed")
	})
}

// Start starts the daemon.
func (d *Daemon) Start(ctx context.Context) error {
	d.mu.Lock()
	if d.running {
		d.mu.Unlock()
		return fmt.Errorf("daemon already running")
	}
	d.running = true
	d.startTime = time.Now()
	d.mu.Unlock()

	d.logger.Info().
		Str("socket", d.cfg.SocketPath).
		Str("data_dir", d.cfg.DataDir).
		Msg("starting daemon")

	// Remove existing socket file
	socketDir := filepath.Dir(d.cfg.SocketPath)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	os.Remove(d.cfg.SocketPath)

	// Create Unix socket listener
	listener, err := net.Listen("unix", d.cfg.SocketPath)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}

	// Set socket permissions
	if err := os.Chmod(d.cfg.SocketPath, 0600); err != nil {
		listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	// Create HTTP server
	d.server = &http.Server{
		Handler:      d.router,
		ReadTimeout:  d.cfg.API.ReadTimeout,
		WriteTimeout: d.cfg.API.WriteTimeout,
		IdleTimeout:  d.cfg.API.IdleTimeout,
	}

	// Start server in goroutine
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		if err := d.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			d.logger.Error().Err(err).Msg("server error")
		}
	}()

	// Start health checker
	d.wg.Add(1)
	go d.healthCheckLoop(ctx)

	// Mark as ready
	d.mu.Lock()
	d.ready = true
	d.mu.Unlock()

	observability.LogEvent(d.logger, observability.EventDaemonStarted, map[string]interface{}{
		"socket":   d.cfg.SocketPath,
		"data_dir": d.cfg.DataDir,
	})

	d.logger.Info().Msg("daemon started")
	return nil
}

// Stop gracefully stops the daemon.
func (d *Daemon) Stop(ctx context.Context) error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}
	d.running = false
	d.ready = false
	d.mu.Unlock()

	d.logger.Info().Msg("stopping daemon")

	// Signal shutdown
	close(d.shutdownCh)

	// Shutdown HTTP server
	if d.server != nil {
		if err := d.server.Shutdown(ctx); err != nil {
			d.logger.Error().Err(err).Msg("server shutdown error")
		}
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-ctx.Done():
		d.logger.Warn().Msg("shutdown timeout, some goroutines may still be running")
	}

	// Close store
	if d.store != nil {
		d.store.Close()
	}

	// Remove socket file
	os.Remove(d.cfg.SocketPath)

	observability.LogEvent(d.logger, observability.EventDaemonStopped, nil)
	d.logger.Info().Msg("daemon stopped")

	return nil
}

// Run runs the daemon until interrupted.
func (d *Daemon) Run() error {
	ctx := context.Background()

	if err := d.Start(ctx); err != nil {
		return err
	}

	// Wait for interrupt signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		d.logger.Info().Str("signal", sig.String()).Msg("received shutdown signal")
	case <-d.shutdownCh:
		// Shutdown requested programmatically
	}

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	return d.Stop(shutdownCtx)
}

// Ready returns whether the daemon is ready to serve requests.
func (d *Daemon) Ready() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.ready
}

// Store returns the daemon's store instance.
func (d *Daemon) Store() *store.Store {
	return d.store
}

// Config returns the daemon's configuration.
func (d *Daemon) Config() *config.Config {
	return d.cfg
}

// healthCheckLoop periodically checks the health of running instances.
func (d *Daemon) healthCheckLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.cfg.Runtime.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.shutdownCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.checkInstanceHealth(ctx)
		}
	}
}

// checkInstanceHealth checks the health of all running instances.
func (d *Daemon) checkInstanceHealth(ctx context.Context) {
	// TODO: Implement health checks for running instances
	// This will check container health and update instance status
}
