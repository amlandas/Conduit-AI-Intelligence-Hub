package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/observability"
)

// LlamaServerConfig configures a LlamaServerProvider.
type LlamaServerConfig struct {
	// BaseURL is the root of the llama-server HTTP API, e.g.
	// "http://127.0.0.1:8081". Required.
	BaseURL string

	// Model is the model identifier sent in the request body. llama-server
	// ignores it in single-model mode but the OpenAI schema requires it.
	Model string

	// Dimensions is the expected vector width. If 0, the width is learned from
	// the first successful response.
	Dimensions int

	// Timeout bounds a single Embed call including retries.
	// Defaults to DefaultTimeout.
	Timeout time.Duration

	// BatchSize caps texts per HTTP request. Defaults to DefaultBatchSize.
	BatchSize int

	// Retry controls bounded retries. Zero value uses DefaultRetryPolicy.
	Retry RetryPolicy

	// QueryPrefix and DocPrefix are model-specific instruction prefixes (for
	// example nomic-embed-text requires "search_query: " / "search_document: ").
	// Embed applies DocPrefix; EmbedQuery applies QueryPrefix.
	QueryPrefix string
	DocPrefix   string

	// InputSuffix is appended to every input regardless of kind. See
	// ModelSpec.InputSuffix for why this is not cosmetic.
	InputSuffix string

	// HTTPClient overrides the internal client. Supplied mainly by tests. If
	// nil, a client with an explicit Timeout is constructed. http.DefaultClient
	// is never used.
	HTTPClient *http.Client

	// Logger overrides the component logger.
	Logger *zerolog.Logger
}

// LlamaServerProvider embeds text via a llama-server instance speaking the
// OpenAI-compatible POST /v1/embeddings API.
//
// It is safe for concurrent use.
type LlamaServerProvider struct {
	baseURL   string
	model     string
	batchSize int
	timeout   time.Duration
	retry     RetryPolicy
	client    *http.Client
	logger    zerolog.Logger

	queryPrefix string
	docPrefix   string
	inputSuffix string

	mu  sync.RWMutex
	dim int
}

var _ Provider = (*LlamaServerProvider)(nil)

// decorate applies a model's prefix and suffix to a batch of inputs. It always
// returns a fresh slice so the caller's input is never mutated.
func decorate(texts []string, prefix, suffix string) []string {
	if prefix == "" && suffix == "" {
		return texts
	}
	out := make([]string, len(texts))
	for i, t := range texts {
		out[i] = prefix + t + suffix
	}
	return out
}

// newTimeoutHTTPClient builds an http.Client with an explicit overall timeout
// and bounded dial/TLS/response-header phases.
//
// This deliberately does NOT use http.DefaultClient, whose zero Timeout means
// a hung server wedges the caller forever (known bug #71).
func newTimeoutHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := &http.Transport{
		DialContext:           dialer.DialContext,
		MaxIdleConns:          16,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// ResponseHeaderTimeout is intentionally left unset: a large batch can
		// legitimately take a while to produce its first byte on a cold model.
		// The overall client Timeout plus the caller's context bound the call.
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}
}

// NewLlamaServerProvider constructs a provider against an already-running
// llama-server. Use Manager.Provider to get one backed by a managed sidecar.
func NewLlamaServerProvider(cfg LlamaServerConfig) (*LlamaServerProvider, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("embed: BaseURL is required")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}
	if cfg.Model == "" {
		cfg.Model = "conduit-embed"
	}

	client := cfg.HTTPClient
	if client == nil {
		client = newTimeoutHTTPClient(cfg.Timeout)
	} else if client.Timeout == 0 {
		// A caller-supplied client without a timeout would reintroduce bug #71.
		// Copy it and impose one rather than mutating the caller's value.
		cp := *client
		cp.Timeout = cfg.Timeout
		client = &cp
	}

	logger := observability.Logger("embed.llamaserver")
	if cfg.Logger != nil {
		logger = *cfg.Logger
	}

	return &LlamaServerProvider{
		baseURL:     strings.TrimRight(cfg.BaseURL, "/"),
		model:       cfg.Model,
		batchSize:   cfg.BatchSize,
		timeout:     cfg.Timeout,
		retry:       cfg.Retry,
		client:      client,
		logger:      logger,
		dim:         cfg.Dimensions,
		queryPrefix: cfg.QueryPrefix,
		docPrefix:   cfg.DocPrefix,
		inputSuffix: cfg.InputSuffix,
	}, nil
}

// ModelID implements Provider.
func (p *LlamaServerProvider) ModelID() string { return p.model }

// Dimensions implements Provider.
func (p *LlamaServerProvider) Dimensions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.dim
}

// Close implements Provider. The HTTP transport's idle connections are
// released; the sidecar process itself is owned by the Manager.
func (p *LlamaServerProvider) Close() error {
	p.client.CloseIdleConnections()
	return nil
}

// Embed implements Provider, applying the configured document prefix.
func (p *LlamaServerProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return p.embedWithPrefix(ctx, texts, p.docPrefix)
}

// EmbedQuery embeds search queries, applying the model's query prefix. Models
// such as nomic-embed-text produce materially worse retrieval without it.
func (p *LlamaServerProvider) EmbedQuery(ctx context.Context, texts []string) ([][]float32, error) {
	return p.embedWithPrefix(ctx, texts, p.queryPrefix)
}

func (p *LlamaServerProvider) embedWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Impose the provider timeout even when the caller passed a bare context.
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	out := make([][]float32, 0, len(texts))
	for _, batch := range chunkTexts(texts, p.batchSize) {
		payload := decorate(batch, prefix, p.inputSuffix)

		var vecs [][]float32
		err := retryCall(ctx, p.retry, nil, func(ctx context.Context) error {
			v, err := p.postEmbeddings(ctx, payload)
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

// openAIEmbeddingRequest is the OpenAI-compatible request body.
type openAIEmbeddingRequest struct {
	Model          string   `json:"model"`
	Input          []string `json:"input"`
	EncodingFormat string   `json:"encoding_format"`
}

// openAIEmbeddingResponse is the OpenAI-compatible success body.
type openAIEmbeddingResponse struct {
	Object string `json:"object"`
	Model  string `json:"model"`
	Data   []struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// openAIErrorResponse is the OpenAI-compatible error body. llama-server also
// emits a bare {"error":"..."} in some paths, so Message is decoded leniently.
type openAIErrorResponse struct {
	Error json.RawMessage `json:"error"`
}

// errorMessage extracts a human-readable message from either error shape.
func (e *openAIErrorResponse) errorMessage() string {
	if len(e.Error) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(e.Error, &s); err == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	}
	if err := json.Unmarshal(e.Error, &obj); err == nil {
		if obj.Type != "" && obj.Message != "" {
			return fmt.Sprintf("%s: %s", obj.Type, obj.Message)
		}
		if obj.Message != "" {
			return obj.Message
		}
	}
	return string(e.Error)
}

// postEmbeddings performs one POST /v1/embeddings round trip.
func (p *LlamaServerProvider) postEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(openAIEmbeddingRequest{
		Model:          p.model,
		Input:          texts,
		EncodingFormat: "float",
	})
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// Connection-level failures are transient: the sidecar may be
		// restarting or still loading its model.
		return nil, markTransient(fmt.Errorf("embed: %w: %v", ErrUnavailable, err))
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
	}()

	// Cap the response read so a malfunctioning server cannot exhaust memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, markTransient(fmt.Errorf("embed: read response: %w", err))
	}

	if resp.StatusCode != http.StatusOK {
		var errBody openAIErrorResponse
		msg := ""
		if json.Unmarshal(raw, &errBody) == nil {
			msg = errBody.errorMessage()
		}
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
			if len(msg) > 512 {
				msg = msg[:512]
			}
		}
		err := fmt.Errorf("embed: llama-server returned %d: %s", resp.StatusCode, msg)
		// 5xx, 429 and 408 are worth retrying; 4xx generally is not.
		if resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout {
			return nil, markTransient(err)
		}
		return nil, err
	}

	var parsed openAIEmbeddingResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(parsed.Data) == 0 {
		return nil, fmt.Errorf("%w (model %q)", ErrEmptyResponse, p.model)
	}

	// The spec does not guarantee ordering, so sort by index before use.
	sort.Slice(parsed.Data, func(i, j int) bool { return parsed.Data[i].Index < parsed.Data[j].Index })

	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embed: expected %d vectors, got %d", len(texts), len(parsed.Data))
	}

	vecs := make([][]float32, len(parsed.Data))
	for i, d := range parsed.Data {
		if len(d.Embedding) == 0 {
			return nil, fmt.Errorf("%w (empty vector at index %d)", ErrEmptyResponse, i)
		}
		vecs[i] = d.Embedding
	}

	if err := p.recordDimension(len(vecs[0])); err != nil {
		return nil, err
	}
	for i, v := range vecs {
		if len(v) != len(vecs[0]) {
			return nil, fmt.Errorf("%w: vector %d has width %d, want %d", ErrDimensionMismatch, i, len(v), len(vecs[0]))
		}
	}

	return vecs, nil
}

// recordDimension learns the vector width on first use and enforces it after.
func (p *LlamaServerProvider) recordDimension(got int) error {
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

// Health implements Provider by probing llama-server's /health endpoint.
func (p *LlamaServerProvider) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, healthProbeTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("embed: build health request: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
	}()

	// llama-server answers 503 with {"status":"loading model"} while warming up.
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: health returned %d", ErrUnavailable, resp.StatusCode)
	}
	return nil
}
