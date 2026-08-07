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
	vectors  *kb.SQLiteVectorIndex
	hybrid   *kb.HybridSearcher
	graph    *kb.GraphStore

	embedder  kb.Embedder
	embedInfo EmbedderInfo

	// stampAdopted records that this open inferred an embedding-model stamp for
	// vectors that predate stamping, so `conduit doctor` can say so.
	stampAdopted bool

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
		identity := kb.NewEmbeddingIdentity(embedder.Model(), info.Provider, dim, info.PrefixScheme)
		vectors, verr := kb.NewSQLiteVectorIndex(s.db, kb.VectorIndexConfig{
			Dimension: dim,
			BatchSize: 100,
			Identity:  identity,
		})
		if verr != nil {
			logger.Warn().Err(verr).Msg("vector index unavailable; retrieval is lexical-only")
			s.embedInfo.Available = false
			s.embedInfo.Err = verr.Error()
		} else {
			s.vectors = vectors
			s.semantic = kb.NewSemanticSearcherWith(s.db, embedder, vectors)
			s.source.SetSemanticSearcher(s.semantic)
			s.indexer.SetSemanticSearcher(s.semantic)

			// Adopt an identity for vectors that predate stamping (WP-4.3).
			//
			// This has to happen on open, not on the next write. A knowledge base
			// upgraded with unstamped vectors that is then indexed with a DIFFERENT
			// model would otherwise be stamped with that new model at the first
			// write, silently blessing exactly the mix the stamp exists to prevent.
			// Adopting first means the change is compared against something.
			//
			// A failure here is never fatal: the knowledge base may be on read-only
			// media, and losing the check is much better than losing the command.
			adopted, aerr := vectors.AdoptLegacyStamp(context.Background())
			if aerr != nil {
				logger.Debug().Err(aerr).Msg("could not record embedding-model stamp for existing vectors")
			}
			s.stampAdopted = adopted
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
//
// A rebuild is refused outright when the embedding model has changed, because
// re-indexing a document DELETES its vectors before writing the replacements,
// and the replacements are exactly what the model guard refuses. Letting it run
// would destroy usable vectors and put nothing back -- the user would end up
// strictly worse off than if they had done nothing, and on a single-source
// knowledge base the stamp would be left describing zero vectors.
//
// Rebuilding one source cannot fix a model change in any case: the other
// sources would stay in the old model's space, which is the mixture this whole
// mechanism exists to prevent. The whole knowledge base has to move at once, so
// the error names the command that does that.
//
// PrepareVectorRebuild is the supported route. It clears the vector space
// first, which removes the mismatch, so the rebuild that follows passes this
// check and proceeds normally.
func (s *Service) Sync(ctx context.Context, sourceID string, rebuildVectors bool) (*kb.SyncResult, error) {
	if rebuildVectors {
		status, err := s.EmbeddingStampStatus(ctx)
		if err != nil {
			return nil, err
		}
		if mismatch := status.mismatchError("rebuild vectors for one source"); mismatch != nil {
			return nil, fmt.Errorf(
				"%w — rebuilding a single source would delete its vectors and be unable to "+
					"replace them, and would leave every other source in the old model's space. "+
					"Rebuild the whole knowledge base instead: `%s`",
				mismatch, kb.RebuildRemedy)
		}
	}
	return s.source.SyncWithOptions(ctx, sourceID, &kb.SyncOptions{RebuildVectors: rebuildVectors})
}

// MigrateResult reports what `conduit kb migrate` did.
type MigrateResult struct {
	// Documents is how many documents were embedded.
	Documents int `json:"documents"`
	// Failed is how many were skipped after an error.
	Failed int `json:"failed"`
	// Total is how many were in scope.
	Total int `json:"total"`

	// FullReembed is true when the pass rebuilt EVERY vector because the
	// embedding model had changed, rather than filling in missing ones.
	FullReembed bool `json:"full_reembed"`
	// FromModel and ToModel are set only on a full re-embed.
	FromModel string `json:"from_model,omitempty"`
	ToModel   string `json:"to_model,omitempty"`
}

// Migrate brings the vector index into line with the full-text index.
//
// It has two behaviours, chosen from the knowledge base's own state rather than
// from a flag, because the user cannot be expected to know which one they need:
//
//   - Ordinary backfill: documents that are indexed but have no vectors get
//     them. Documents that already have vectors are left alone.
//   - Model change: when the stored vectors were built by a different embedding
//     model, filling in the gaps would only deepen the mix. Every vector is
//     discarded and rebuilt, and the knowledge base is restamped as it goes.
//
// It is an error when embeddings are switched off; there is nothing to migrate
// to.
func (s *Service) Migrate(ctx context.Context, progress func(current, total int)) (*MigrateResult, error) {
	if s.semantic == nil {
		return nil, ErrSemanticUnavailable
	}

	status, err := s.EmbeddingStampStatus(ctx)
	if err != nil {
		return nil, err
	}
	if status != nil && status.Verdict == kb.StampMismatch {
		return s.reembedAll(ctx, status, progress)
	}

	res, err := s.semantic.ReembedDocuments(ctx, kb.ReembedMissing, progress)
	return &MigrateResult{Documents: res.Embedded, Failed: res.Failed, Total: res.Total}, err
}

// ReembedAll discards every stored vector and rebuilds it with the currently
// configured model.
//
// This is the engine a `conduit model upgrade` command fronts, and the same code
// path `kb migrate` and `kb sync --rebuild-vectors` take after a model change.
// It is deliberately not conditional: a caller asking for this has already
// decided.
func (s *Service) ReembedAll(ctx context.Context, progress func(current, total int)) (*MigrateResult, error) {
	if s.semantic == nil {
		return nil, ErrSemanticUnavailable
	}
	status, err := s.EmbeddingStampStatus(ctx)
	if err != nil {
		return nil, err
	}
	return s.reembedAll(ctx, status, progress)
}

// reembedAll performs the destructive rebuild. status may be nil.
func (s *Service) reembedAll(ctx context.Context, status *EmbeddingStampStatus, progress func(current, total int)) (*MigrateResult, error) {
	out := &MigrateResult{FullReembed: true}
	if status != nil {
		if status.Stamp != nil {
			out.FromModel = status.Stamp.Display()
		}
		out.ToModel = status.Active.Display()
	}

	// Clearing before rebuilding is what makes the guard let these writes
	// through, and it is also the only ordering in which the stamp never
	// describes vectors that are not there. See SQLiteVectorIndex.ResetVectorSpace.
	if err := s.semantic.ResetVectorSpace(ctx); err != nil {
		return out, err
	}

	res, err := s.semantic.ReembedDocuments(ctx, kb.ReembedAll, progress)
	out.Documents, out.Failed, out.Total = res.Embedded, res.Failed, res.Total
	return out, err
}

// PrepareVectorRebuild clears the vector space when, and only when, the stored
// vectors were built by a different embedding model.
//
// `conduit kb sync --rebuild-vectors` calls it once before syncing every source.
// Without it the rebuild could not succeed: each write would be refused by the
// very guard that detected the change. With it, the sync re-embeds into an empty
// space and stamps it with the model actually in use.
//
// It returns the mismatch it acted on so the caller can tell the user what
// happened, or (nil, nil) when there was nothing to do -- which is the case for
// every ordinary rebuild.
func (s *Service) PrepareVectorRebuild(ctx context.Context) (*kb.ModelMismatchError, error) {
	if s.semantic == nil {
		return nil, nil
	}
	status, err := s.EmbeddingStampStatus(ctx)
	if err != nil || status == nil || status.Verdict != kb.StampMismatch {
		return nil, err
	}
	if err := s.semantic.ResetVectorSpace(ctx); err != nil {
		return nil, err
	}
	return status.mismatchError("rebuild vectors"), nil
}

// EmbeddingStampStatus describes how the stored vectors and the configured
// embedder relate. It is what `conduit doctor` reports.
type EmbeddingStampStatus struct {
	// Stamp is the recorded identity of the stored vectors, nil when none has
	// been recorded.
	Stamp *kb.EmbeddingStamp
	// Active is the identity of the embedder configured now.
	Active kb.EmbeddingIdentity
	// Verdict is the comparison outcome.
	Verdict kb.StampVerdict
	// Vectors is how many vectors are stored.
	Vectors int64
	// AdoptedThisOpen is true when this process inferred the stamp for
	// pre-existing vectors rather than reading one that was already there.
	AdoptedThisOpen bool
}

// mismatchError renders this status as the error a refused operation returns,
// or nil when the two identities agree.
//
// It is the single place that reason is derived, so a message can never explain
// the mismatch differently from the check that found it.
func (st *EmbeddingStampStatus) mismatchError(op string) *kb.ModelMismatchError {
	if st == nil || st.Stamp == nil || st.Verdict != kb.StampMismatch {
		return nil
	}
	return &kb.ModelMismatchError{
		Stamped: st.Stamp.EmbeddingIdentity,
		Active:  st.Active,
		Op:      op,
		Reason:  st.Stamp.MismatchReason(st.Active),
	}
}

// StoredEmbeddingStamp returns the recorded identity of the stored vectors
// without needing an embedder.
//
// It exists for embed.provider=none, where there is no vector index to ask but
// the record of what built the existing vectors is still worth showing: it is
// what they will be compared against if embeddings are turned back on.
func (s *Service) StoredEmbeddingStamp(ctx context.Context) (*kb.EmbeddingStamp, error) {
	return kb.ReadEmbeddingStamp(ctx, s.db)
}

// EmbeddingStampStatus returns the stamp comparison, or nil when embeddings are
// switched off (there is no vector space to describe).
func (s *Service) EmbeddingStampStatus(ctx context.Context) (*EmbeddingStampStatus, error) {
	if s.vectors == nil {
		return nil, nil
	}
	stamp, err := s.vectors.Stamp(ctx)
	if err != nil {
		return nil, err
	}
	count, err := s.VectorCount(ctx)
	if err != nil {
		count = 0
	}
	active := s.vectors.Identity()
	return &EmbeddingStampStatus{
		Stamp:           stamp,
		Active:          active,
		Verdict:         stamp.Compare(active),
		Vectors:         count,
		AdoptedThisOpen: s.stampAdopted,
	}, nil
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
