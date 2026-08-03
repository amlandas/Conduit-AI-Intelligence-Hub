package kb

import (
	"context"
	"fmt"

	"github.com/simpleflo/conduit/internal/embed"
)

const (
	// DefaultEmbeddingModel is the default Ollama embedding model.
	// nomic-embed-text produces 768-dimensional vectors and is MIT licensed.
	DefaultEmbeddingModel = "nomic-embed-text"

	// DefaultEmbeddingDimension is the vector dimension for nomic-embed-text.
	DefaultEmbeddingDimension = 768
)

// queryEmbedder is implemented by providers that distinguish query text from
// document text. Getting this wrong is not a crash but a silent retrieval
// quality loss: asymmetric models such as nomic-embed expect "search_query: "
// on queries and "search_document: " on passages.
type queryEmbedder interface {
	EmbedQuery(ctx context.Context, texts []string) ([][]float32, error)
}

// ProviderEmbedder adapts an embed.Provider to the Embedder seam that
// SemanticSearcher and the SQLite vector index consume.
//
// This replaces internal/kb.EmbeddingService, deleted in WP-3.4 as issue #71.
// That type built its Ollama client with `api.NewClient(url, http.DefaultClient)`
// -- and http.DefaultClient has Timeout == 0, i.e. no timeout at all. Every
// embedding request inherited that, so a hung Ollama blocked an indexing or
// search goroutine until the process died. The only thing that could unblock a
// call was the caller's context, and the ingestion path passed contexts with no
// deadline.
//
// Everything in internal/embed is bounded by BOTH an http.Client.Timeout and a
// context deadline, by construction (see the package doc). Routing every
// embedding through it is the fix: there is no longer a client in the tree that
// can wait forever.
type ProviderEmbedder struct {
	provider embed.Provider
}

var _ Embedder = (*ProviderEmbedder)(nil)

// NewProviderEmbedder wraps an embed.Provider.
func NewProviderEmbedder(p embed.Provider) *ProviderEmbedder {
	return &ProviderEmbedder{provider: p}
}

// Embed embeds a single query string.
func (e *ProviderEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	var (
		vecs [][]float32
		err  error
	)
	if qe, ok := e.provider.(queryEmbedder); ok {
		vecs, err = qe.EmbedQuery(ctx, []string{text})
	} else {
		vecs, err = e.provider.Embed(ctx, []string{text})
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
func (e *ProviderEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return e.provider.Embed(ctx, texts)
}

// Dimension reports the vector width the provider produces.
func (e *ProviderEmbedder) Dimension() int { return e.provider.Dimensions() }

// Model reports the model identifier.
func (e *ProviderEmbedder) Model() string { return e.provider.ModelID() }

// HealthCheck probes the provider.
func (e *ProviderEmbedder) HealthCheck(ctx context.Context) error {
	return e.provider.Health(ctx)
}

// Close releases provider resources.
func (e *ProviderEmbedder) Close() error { return e.provider.Close() }
