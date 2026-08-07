package kbservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/simpleflo/conduit/internal/config"
	"github.com/simpleflo/conduit/internal/embed"
	"github.com/simpleflo/conduit/internal/kb"
)

// queryEmbedder is implemented by providers that distinguish query text from
// document text. Getting this wrong is not a crash but a silent retrieval
// quality loss: asymmetric models such as nomic-embed expect "search_query: "
// on queries and "search_document: " on passages.
type queryEmbedder interface {
	EmbedQuery(ctx context.Context, texts []string) ([][]float32, error)
}

// embedAdapter presents an embed.Provider as the kb.Embedder seam that
// SemanticSearcher and the SQLite vector index consume.
//
// Resolution is lazy on purpose. With the llama-server provider, obtaining the
// provider spawns (or adopts) the sidecar process; doing that when a command
// merely opens the knowledge base would make `conduit kb list` load a model.
// The sidecar starts on the first embedding call and not before.
type embedAdapter struct {
	modelID    string
	dimensions int

	mu       sync.Mutex
	resolved embed.Provider
	resolve  func(ctx context.Context) (embed.Provider, error)
	closer   func() error
}

var _ kb.Embedder = (*embedAdapter)(nil)

func (a *embedAdapter) provider(ctx context.Context) (embed.Provider, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.resolved != nil {
		return a.resolved, nil
	}
	p, err := a.resolve(ctx)
	if err != nil {
		return nil, err
	}
	a.resolved = p
	if a.dimensions <= 0 {
		a.dimensions = p.Dimensions()
	}
	return p, nil
}

// Embed embeds a single query string.
func (a *embedAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	p, err := a.provider(ctx)
	if err != nil {
		return nil, err
	}

	var vecs [][]float32
	if qe, ok := p.(queryEmbedder); ok {
		vecs, err = qe.EmbedQuery(ctx, []string{text})
	} else {
		vecs, err = p.Embed(ctx, []string{text})
	}
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed: provider returned no vector for query")
	}
	return vecs[0], nil
}

// EmbedBatch embeds documents (index-time text).
func (a *embedAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	p, err := a.provider(ctx)
	if err != nil {
		return nil, err
	}
	return p.Embed(ctx, texts)
}

// Dimension reports the configured vector width.
func (a *embedAdapter) Dimension() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dimensions
}

// Model reports the model identifier.
func (a *embedAdapter) Model() string { return a.modelID }

// HealthCheck probes the provider, resolving it if necessary.
func (a *embedAdapter) HealthCheck(ctx context.Context) error {
	p, err := a.provider(ctx)
	if err != nil {
		return err
	}
	return p.Health(ctx)
}

// Close releases provider resources. It never stops a shared sidecar: other
// conduit processes may be using it, and the sidecar retires itself on idle.
func (a *embedAdapter) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	var err error
	if a.resolved != nil {
		err = a.resolved.Close()
		a.resolved = nil
	}
	if a.closer != nil {
		if cerr := a.closer(); err == nil {
			err = cerr
		}
	}
	return nil
}

// EmbedderInfo describes the embedding wiring a Service ended up with. It is
// what `conduit doctor` and `conduit status` report.
type EmbedderInfo struct {
	// Provider is the configured provider name, including "none".
	Provider string `json:"provider"`

	// Model is the model identifier, empty when Provider is "none".
	Model string `json:"model,omitempty"`

	// Dimensions is the vector width, 0 when Provider is "none".
	Dimensions int `json:"dimensions,omitempty"`

	// PrefixScheme identifies the instruction decoration this provider applies
	// to its inputs (embed.PrefixSchemeID). It is part of a vector's meaning and
	// is recorded in the embedding stamp, because the same model reached through
	// two providers is not decorated the same way: the llama-server path takes
	// its prefixes from the pinned registry, the Ollama path is wired without
	// them.
	PrefixScheme string `json:"prefix_scheme,omitempty"`

	// Available is false when the provider is "none" or could not be
	// constructed at all. It does NOT mean the provider answered a probe:
	// reachability is a per-call property, checked by doctor.
	Available bool `json:"available"`

	// Err explains why Available is false for reasons other than "none".
	Err string `json:"error,omitempty"`
}

// newEmbedder builds the kb.Embedder for the configured provider.
//
// A nil embedder with a nil error is the legitimate "none" outcome: the caller
// runs lexical-only, which is a supported mode rather than a failure.
func newEmbedder(cfg *config.Config) (kb.Embedder, EmbedderInfo, error) {
	ec := cfg.Embed

	info := EmbedderInfo{Provider: ec.Provider}

	timeout := time.Duration(ec.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = embed.DefaultTimeout
	}
	batch := ec.BatchSize
	if batch <= 0 {
		batch = embed.DefaultBatchSize
	}

	switch ec.Provider {
	case config.EmbedProviderNone:
		return nil, info, nil

	case config.EmbedProviderOllama:
		model := ec.Model
		if model == "" {
			model = defaultOllamaEmbedModel
		}
		p, err := embed.NewOllamaProvider(embed.OllamaConfig{
			Host:       ec.Ollama.Host,
			Model:      model,
			Dimensions: ec.Dimensions,
			Timeout:    timeout,
			BatchSize:  batch,
		})
		if err != nil {
			info.Err = err.Error()
			return nil, info, fmt.Errorf("embed provider %q: %w", ec.Provider, err)
		}
		dims := ec.Dimensions
		if dims <= 0 {
			dims = p.Dimensions()
		}
		info.Model = model
		info.Dimensions = dims
		// No prefixes are passed to OllamaConfig above, so none are applied.
		// Recording that faithfully is the point: a knowledge base built here and
		// later served by llama-server, which DOES apply the registry prefixes,
		// is worth a word to the user even though the model is the same.
		info.PrefixScheme = embed.PrefixSchemeNone
		info.Available = true
		return &embedAdapter{
			modelID:    model,
			dimensions: dims,
			resolve:    func(context.Context) (embed.Provider, error) { return p, nil },
		}, info, nil

	case config.EmbedProviderLlamaServer:
		modelID := ec.Model
		if modelID == "" {
			modelID = embed.DefaultModelID
		}
		mcfg, err := embed.ManagerConfigForModel(cfg.DataDir, modelID, ec.LlamaServer.ModelPath)
		if err != nil {
			info.Err = err.Error()
			return nil, info, fmt.Errorf("embed provider %q: %w", ec.Provider, err)
		}
		mcfg.BinaryPath = ec.LlamaServer.Binary
		mcfg.Timeout = timeout
		mcfg.BatchSize = batch
		if ec.LlamaServer.IdleTimeout != 0 {
			mcfg.IdleTimeout = ec.LlamaServer.IdleTimeout
		}
		if ec.Dimensions > 0 {
			mcfg.Dimensions = ec.Dimensions
		}

		mgr, err := embed.NewManager(mcfg)
		if err != nil {
			info.Err = err.Error()
			return nil, info, fmt.Errorf("embed provider %q: %w", ec.Provider, err)
		}

		info.Model = modelID
		info.Dimensions = mcfg.Dimensions
		info.PrefixScheme = embed.PrefixSchemeID(mcfg.DocPrefix, mcfg.QueryPrefix, mcfg.InputSuffix)
		info.Available = true
		return &embedAdapter{
			modelID:    modelID,
			dimensions: mcfg.Dimensions,
			resolve:    mgr.Provider,
			closer:     mgr.Close,
		}, info, nil

	default:
		// config.normalize guarantees this is unreachable, but a wrong model is
		// worse than no model: refuse rather than guess.
		info.Err = fmt.Sprintf("unknown embed provider %q", ec.Provider)
		return nil, info, fmt.Errorf("%s", info.Err)
	}
}

// defaultOllamaEmbedModel is the Ollama tag used when embed.model is unset.
const defaultOllamaEmbedModel = "nomic-embed-text"
