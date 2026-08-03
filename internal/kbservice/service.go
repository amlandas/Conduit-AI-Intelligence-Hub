// Package kbservice is the in-process knowledge base library.
//
// Everything the CLI, the MCP server and any future frontend can do to a
// knowledge base is a method here. There is no HTTP layer, no socket and no
// daemon: a command opens the SQLite file, does the work and closes it.
//
// # Concurrency
//
// Concurrent access is SQLite's job. The database is opened in WAL mode with a
// busy timeout (see internal/store), so a `conduit kb sync` running in one
// terminal and a `kb_search` arriving over MCP in another serialise at the
// database rather than at a daemon. Writers still serialise -- SQLite has one
// writer -- but readers never block.
//
// # Response shapes
//
// The map-shaped results returned by Search are a compatibility contract:
// scripts and the frozen desktop GUI parse them. They are reproduced here
// exactly as the removed HTTP daemon produced them.
package kbservice

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/observability"
	"github.com/simpleflo/conduit/internal/store"
)

// Service is an open knowledge base.
//
// Construct it with Open and always Close it. It is safe for concurrent use by
// multiple goroutines within one process.
type Service struct {
	cfg   *config.Config
	store *store.Store
	db    *sql.DB

	source   *kb.SourceManager
	searcher *kb.Searcher
	indexer  *kb.Indexer
	semantic *kb.SemanticSearcher
	hybrid   *kb.HybridSearcher
	graph    *kb.GraphStore

	embedder  kb.Embedder
	embedInfo EmbedderInfo

	logger zerolog.Logger
}

// Open opens the knowledge base described by cfg.
//
// The database file is created (with its schema) if it does not exist, so
// there is no separate "init" step. An embedding provider that cannot be
// constructed is not fatal: retrieval degrades to lexical-only and the reason
// is recorded in EmbedInfo, which is what `conduit doctor` reports.
func Open(cfg *config.Config) (*Service, error) {
	if cfg == nil {
		return nil, fmt.Errorf("kbservice: nil config")
	}
	if err := cfg.EnsureDirectories(); err != nil {
		return nil, fmt.Errorf("create directories: %w", err)
	}

	st, err := store.New(cfg.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("open knowledge base %s: %w", cfg.DatabasePath(), err)
	}

	logger := observability.Logger("kbservice")

	s := &Service{
		cfg:    cfg,
		store:  st,
		db:     st.DB(),
		logger: logger,
	}

	s.source = kb.NewSourceManager(s.db)
	s.source.SetMaxFileSize(cfg.KB.MaxFileSize)
	s.searcher = kb.NewSearcher(s.db)
	s.indexer = kb.NewIndexer(s.db)
	s.graph = kb.NewGraphStore(s.db, cfg.KB.KAG.Enabled)

	embedder, info, err := newEmbedder(cfg)
	s.embedInfo = info
	if err != nil {
		logger.Warn().Err(err).Msg("embedding provider unavailable; retrieval is lexical-only")
	}
	s.embedder = embedder

	if embedder != nil {
		dim := embedder.Dimension()
		if dim <= 0 {
			dim = kb.DefaultEmbeddingDimension
		}
		vectors, verr := kb.NewSQLiteVectorIndex(s.db, kb.VectorIndexConfig{
			Dimension: dim,
			BatchSize: 100,
		})
		if verr != nil {
			logger.Warn().Err(verr).Msg("vector index unavailable; retrieval is lexical-only")
			s.embedInfo.Available = false
			s.embedInfo.Err = verr.Error()
		} else {
			s.semantic = kb.NewSemanticSearcherWith(s.db, embedder, vectors)
			s.source.SetSemanticSearcher(s.semantic)
			s.indexer.SetSemanticSearcher(s.semantic)
		}
	}

	// NewHybridSearcher treats a nil *SemanticSearcher as lexical-only.
	s.hybrid = kb.NewHybridSearcher(s.searcher, s.semantic)

	if s.graph.Enabled() {
		if err := s.graph.EnsureSchema(context.Background()); err != nil {
			logger.Warn().Err(err).Msg("knowledge graph schema unavailable")
		}
	}

	return s, nil
}

// Close releases the database and any embedding resources.
func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	if c, ok := s.embedder.(interface{ Close() error }); ok && s.embedder != nil {
		_ = c.Close()
	}
	if s.store != nil {
		return s.store.Close()
	}
	return nil
}

// DB exposes the underlying database handle for callers that build their own
// collaborators over the same file (the MCP server does this).
func (s *Service) DB() *sql.DB { return s.db }

// Config returns the configuration this service was opened with.
func (s *Service) Config() *config.Config { return s.cfg }

// Hybrid returns the hybrid searcher.
func (s *Service) Hybrid() *kb.HybridSearcher { return s.hybrid }

// Graph returns the graph store, which is inert unless kb.kag.enabled is true.
func (s *Service) Graph() *kb.GraphStore { return s.graph }

// Indexer returns the document indexer.
func (s *Service) Indexer() *kb.Indexer { return s.indexer }

// Sources returns the source manager.
func (s *Service) Sources() *kb.SourceManager { return s.source }

// Semantic returns the semantic searcher, or nil when embeddings are off.
func (s *Service) Semantic() *kb.SemanticSearcher { return s.semantic }

// Embedder returns the configured embedder, or nil in lexical-only mode.
func (s *Service) Embedder() kb.Embedder { return s.embedder }

// EmbedInfo describes how embeddings are wired for this service.
func (s *Service) EmbedInfo() EmbedderInfo { return s.embedInfo }

// SemanticAvailable reports whether semantic retrieval is wired up. It says
// nothing about whether the provider currently answers; that is a probe.
func (s *Service) SemanticAvailable() bool { return s.semantic != nil }

// DatabasePath returns the file this service is bound to.
func (s *Service) DatabasePath() string { return s.cfg.DatabasePath() }

// ---------------------------------------------------------------------------
// Sources
// ---------------------------------------------------------------------------

// SourceList is the response shape for a source listing.
type SourceList struct {
	Sources []*kb.Source `json:"sources"`
	Count   int          `json:"count"`
}

// ListSources returns every configured source.
func (s *Service) ListSources(ctx context.Context) (*SourceList, error) {
	sources, err := s.source.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	if sources == nil {
		sources = []*kb.Source{}
	}
	return &SourceList{Sources: sources, Count: len(sources)}, nil
}

// GetSource returns a single source by ID.
func (s *Service) GetSource(ctx context.Context, sourceID string) (*kb.Source, error) {
	return s.source.Get(ctx, sourceID)
}

// FindSource resolves a source by ID, name or path, in that order of
// preference. It is what the CLI's <name-or-id> arguments mean.
func (s *Service) FindSource(ctx context.Context, nameOrID string) (*kb.Source, error) {
	list, err := s.source.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	for _, src := range list {
		if src.SourceID == nameOrID || src.Name == nameOrID || src.Path == nameOrID {
			return src, nil
		}
	}
	return nil, fmt.Errorf("source not found: %s", nameOrID)
}

// AddSource registers a new source. It does not index anything; run Sync.
//
// The path is checked against kb.policy.forbidden_paths first: indexing copies
// a directory's contents into a searchable database that the MCP server exposes
// to connected AI clients, so ~/.ssh and friends are refused outright. Paths
// under kb.policy.warn_paths are allowed and returned as warnings for the
// caller to show. See AddSourceWithWarnings when the warnings matter.
func (s *Service) AddSource(ctx context.Context, req kb.AddSourceRequest) (*kb.Source, error) {
	src, _, err := s.AddSourceWithWarnings(ctx, req)
	return src, err
}

// AddSourceWithWarnings is AddSource, plus the non-fatal path-safety warnings.
func (s *Service) AddSourceWithWarnings(ctx context.Context, req kb.AddSourceRequest) (*kb.Source, []string, error) {
	warnings, err := checkSourcePath(s.cfg, req.Path)
	if err != nil {
		return nil, nil, err
	}
	src, err := s.source.Add(ctx, req)
	if err != nil {
		return nil, nil, err
	}
	return src, warnings, nil
}

// RemoveResult is the response shape for a source removal.
type RemoveResult struct {
	DocumentsDeleted int `json:"documents_deleted"`
	VectorsDeleted   int `json:"vectors_deleted"`
}

// RemoveSource deletes a source and everything indexed from it.
func (s *Service) RemoveSource(ctx context.Context, sourceID string) (*RemoveResult, error) {
	res, err := s.source.Remove(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	return &RemoveResult{
		DocumentsDeleted: res.DocumentsDeleted,
		VectorsDeleted:   res.VectorsDeleted,
	}, nil
}

// Sync indexes a source. RebuildVectors forces re-embedding of every document.
func (s *Service) Sync(ctx context.Context, sourceID string, rebuildVectors bool) (*kb.SyncResult, error) {
	return s.source.SyncWithOptions(ctx, sourceID, &kb.SyncOptions{RebuildVectors: rebuildVectors})
}

// Migrate embeds documents that are in the full-text index but have no vector.
// It returns the number migrated. It is a no-op error when embeddings are off.
func (s *Service) Migrate(ctx context.Context, progress func(current, total int)) (int, error) {
	if s.semantic == nil {
		return 0, ErrSemanticUnavailable
	}
	var migrated int
	fn := func(current, total int) {
		migrated = current
		if progress != nil {
			progress(current, total)
		}
	}
	if err := s.semantic.MigrateFromFTS(ctx, fn); err != nil {
		return migrated, err
	}
	return migrated, nil
}

// ErrSemanticUnavailable is returned by operations that require embeddings
// when the configured provider is "none" or could not be constructed.
var ErrSemanticUnavailable = fmt.Errorf("semantic search unavailable: no embedding provider (embed.provider is %q or the provider failed to start)", "none")

// ---------------------------------------------------------------------------
// Stats
// ---------------------------------------------------------------------------

// Stats aggregates knowledge base totals.
type Stats struct {
	Sources    int   `json:"sources"`
	Documents  int   `json:"documents"`
	Chunks     int   `json:"chunks"`
	TotalBytes int64 `json:"total_bytes"`
}

// Stats returns aggregate counts across all sources.
func (s *Service) Stats(ctx context.Context) (*Stats, []*kb.Source, error) {
	sources, err := s.source.List(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list sources: %w", err)
	}
	st := &Stats{Sources: len(sources)}
	for _, src := range sources {
		st.Documents += src.DocCount
		st.Chunks += src.ChunkCount
		st.TotalBytes += src.SizeBytes
	}
	return st, sources, nil
}

// VectorCount returns the number of stored embeddings, or 0 when the vector
// table has never been created.
func (s *Service) VectorCount(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM kb_vectors`).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}
