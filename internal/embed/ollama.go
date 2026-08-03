package embed

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
)

// DefaultOllamaHost is the conventional Ollama endpoint.
const DefaultOllamaHost = "http://localhost:11434"

// OllamaConfig configures an OllamaProvider.
type OllamaConfig struct {
	// Host is the Ollama API endpoint. Defaults to DefaultOllamaHost.
	Host string

	// Model is the Ollama embedding model name, e.g. "nomic-embed-text".
	Model string

	// Dimensions is the expected vector width. If 0, it is learned from the
	// first successful response.
	Dimensions int

	// Timeout bounds a single Embed call including retries.
	Timeout time.Duration

	// BatchSize caps texts per request. Defaults to DefaultBatchSize.
	BatchSize int

	// Retry controls bounded retries. Zero value uses DefaultRetryPolicy.
	Retry RetryPolicy

	// QueryPrefix and DocPrefix are model-specific instruction prefixes.
	QueryPrefix string
	DocPrefix   string

	// InputSuffix is appended to every input. See ModelSpec.InputSuffix.
	InputSuffix string

	// HTTPClient overrides the internal client. Supplied mainly by tests.
	HTTPClient *http.Client

	// Logger overrides the component logger.
	Logger *zerolog.Logger
}

// OllamaProvider embeds text via a local Ollama daemon.
//
// Ollama is an OPTIONAL provider in v2: the managed llama-server sidecar is
// primary. This implementation is new code and, unlike the legacy
// internal/kb.EmbeddingService, always configures an http.Client with an
// explicit timeout (known bug #71).
//
// It is safe for concurrent use.
type OllamaProvider struct {
	client    *api.Client
	http      *http.Client
	model     string
	host      string
	batchSize int
	timeout   time.Duration
	retry     RetryPolicy
	logger    zerolog.Logger

	queryPrefix string
	docPrefix   string
	inputSuffix string

	mu  sync.RWMutex
	dim int
}

var _ Provider = (*OllamaProvider)(nil)

// NewOllamaProvider constructs a provider against a running Ollama daemon.
//
// It does not contact the daemon; call Health to verify reachability.
func NewOllamaProvider(cfg OllamaConfig) (*OllamaProvider, error) {
	if strings.TrimSpace(cfg.Host) == "" {
		cfg.Host = DefaultOllamaHost
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("embed: Ollama model is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}

	hostURL, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, fmt.Errorf("embed: invalid Ollama host %q: %w", cfg.Host, err)
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = newTimeoutHTTPClient(cfg.Timeout)
	} else if httpClient.Timeout == 0 {
		cp := *httpClient
		cp.Timeout = cfg.Timeout
		httpClient = &cp
	}

	logger := observability.Logger("embed.ollama")
	if cfg.Logger != nil {
		logger = *cfg.Logger
	}

	return &OllamaProvider{
		client:      api.NewClient(hostURL, httpClient),
		http:        httpClient,
		model:       cfg.Model,
		host:        strings.TrimRight(cfg.Host, "/"),
		batchSize:   cfg.BatchSize,
		timeout:     cfg.Timeout,
		retry:       cfg.Retry,
		logger:      logger,
		dim:         cfg.Dimensions,
		queryPrefix: cfg.QueryPrefix,
		docPrefix:   cfg.DocPrefix,
		inputSuffix: cfg.InputSuffix,
	}, nil
}

// ModelID implements Provider.
func (p *OllamaProvider) ModelID() string { return p.model }

// Dimensions implements Provider.
func (p *OllamaProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dim
}

// Close implements Provider.
func (p *OllamaProvider) Close() error {
	p.http.CloseIdleConnections()
	return nil
}

// Embed implements Provider, applying the configured document prefix.
func (p *OllamaProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return p.embedWithPrefix(ctx, texts, p.docPrefix)
}

// EmbedQuery embeds search queries, applying the model's query prefix.
func (p *OllamaProvider) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return p.embedWithPrefix(ctx, texts, p.queryPrefix)
}

func (p *OllamaProvider) embedWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	out := make([][]float32, 0, len(texts))
	for _, batch := range chunkTexts(texts, p.batchSize) {
		payload := decorate(batch, prefix, p.inputSuffix)

		var vecs [][]float32
		err := retryCall(ctx, p.retry, nil, func(ctx context.Context) error {
			v, err := p.embedBatch(ctx, payload)
			if err != nil {
				return err
			}
			vecs = v
			return nil
		})
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}

	if len(out) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d vectors, got %d", len(texts), len(out))
	}
	return out, nil
}

// embedBatch performs one Ollama /api/embed round trip.
func (p *OllamaProvider) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	resp, err := p.client.Embed(ctx, &api.EmbedRequest{
		Model: p.model,
		Input: texts,
	})
	if err != nil {
		// Ollama surfaces transport failures and daemon errors alike; treat
		// them as transient so a briefly-restarting daemon is tolerated. The
		// context deadline still bounds total time spent retrying.
		if ctx.Err() != nil {
			return nil, fmt.Errorf("embed: ollama request: %w", err)
		}
		return nil, markTransient(fmt.Errorf("embed: ollama request: %w", err))
	}
	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("%w (model %q)", ErrEmptyResponse, p.model)
	}
	if len(resp.Embeddings) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d vectors, got %d", len(texts), len(resp.Embeddings))
	}

	if err := p.recordDimension(len(resp.Embeddings[0])); err != nil {
		return nil, err
	}
	for i, v := range resp.Embeddings {
		if len(v) != len(resp.Embeddings[0]) {
			return nil, fmt.Errorf("%w: vector %d has width %d, want %d", ErrDimensionMismatch, i, len(v), len(resp.Embeddings[0]))
		}
	}

	return resp.Embeddings, nil
}

func (p *OllamaProvider) recordDimension(got int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dim == 0 {
		p.dim = got
		return nil
	}
	if p.dim != got {
		return fmt.Errorf("%w: got %d, want %d", ErrDimensionMismatch, got, p.dim)
	}
	return nil
}

// Health implements Provider by embedding a trivial probe string.
func (p *OllamaProvider) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()

	vecs, err := p.embedBatch(ctx, []string{"health check"})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, unwrapTransient(err))
	}
	if len(vecs) == 0 || len(vecs[0]) == 0 {
		return fmt.Errorf("%w: empty probe vector", ErrUnavailable)
	}
	return nil
}
