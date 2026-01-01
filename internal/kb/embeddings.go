package kb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ollama/ollama/api"
	"github.com/rs/zerolog"
	"github.com/simpleflo/conduit/internal/observability"
)

const (
	// DefaultEmbeddingModel is the default embedding model for semantic search.
	// nomic-embed-text produces 768-dimensional vectors and is MIT licensed.
	DefaultEmbeddingModel = "nomic-embed-text"

	// DefaultEmbeddingDimension is the vector dimension for nomic-embed-text.
	DefaultEmbeddingDimension = 768

	// DefaultOllamaHost is the default Ollama API endpoint.
	DefaultOllamaHost = "http://localhost:11434"

	// DefaultBatchSize is the default number of texts to embed in parallel.
	DefaultBatchSize = 10
)

// EmbeddingService generates vector embeddings via Ollama.
type EmbeddingService struct {
	client    *api.Client
	model     string
	dimension int
	batchSize int
	logger    zerolog.Logger
	mu        sync.RWMutex
	ready     bool
}

// EmbeddingConfig configures the embedding service.
type EmbeddingConfig struct {
	OllamaHost string // Ollama API endpoint (default: http://localhost:11434)
	Model      string // Embedding model name (default: nomic-embed-text)
	Dimension  int    // Vector dimension (default: 768)
	BatchSize  int    // Batch size for parallel embedding (default: 10)
}

// NewEmbeddingService creates a new embedding service.
func NewEmbeddingService(cfg EmbeddingConfig) (*EmbeddingService, error) {
	if cfg.OllamaHost == "" {
		cfg.OllamaHost = DefaultOllamaHost
	}
	if cfg.Model == "" {
		cfg.Model = DefaultEmbeddingModel
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = DefaultEmbeddingDimension
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = DefaultBatchSize
	}

	// Parse the Ollama host URL
	ollamaURL, err := url.Parse(cfg.OllamaHost)
	if err != nil {
		return nil, fmt.Errorf("invalid Ollama host URL: %w", err)
	}

	// Create Ollama client
	client := api.NewClient(ollamaURL, http.DefaultClient)

	svc := &EmbeddingService{
		client:    client,
		model:     cfg.Model,
		dimension: cfg.Dimension,
		batchSize: cfg.BatchSize,
		logger:    observability.Logger("kb.embeddings"),
	}

	return svc, nil
}

// EnsureModel ensures the embedding model is available, pulling it if necessary.
func (svc *EmbeddingService) EnsureModel(ctx context.Context) error {
	svc.mu.Lock()
	defer svc.mu.Unlock()

	if svc.ready {
		return nil
	}

	// Check if model is available
	svc.logger.Info().Str("model", svc.model).Msg("checking embedding model availability")

	// Try to get model info first
	showReq := &api.ShowRequest{Model: svc.model}
	_, err := svc.client.Show(ctx, showReq)
	if err == nil {
		svc.ready = true
		svc.logger.Info().Str("model", svc.model).Msg("embedding model ready")
		return nil
	}

	// Model not found, try to pull it
	svc.logger.Info().Str("model", svc.model).Msg("pulling embedding model (this may take a few minutes)")

	pullReq := &api.PullRequest{Model: svc.model}
	progressFn := func(resp api.ProgressResponse) error {
		if resp.Total > 0 {
			pct := float64(resp.Completed) / float64(resp.Total) * 100
			svc.logger.Debug().
				Str("status", resp.Status).
				Float64("progress", pct).
				Msg("pulling model")
		}
		return nil
	}

	if err := svc.client.Pull(ctx, pullReq, progressFn); err != nil {
		return fmt.Errorf("failed to pull embedding model %s: %w", svc.model, err)
	}

	svc.ready = true
	svc.logger.Info().Str("model", svc.model).Msg("embedding model pulled and ready")
	return nil
}

// Embed generates an embedding for a single text.
func (svc *EmbeddingService) Embed(ctx context.Context, text string) ([]float32, error) {
	embeddings, err := svc.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
func (svc *EmbeddingService) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	// Ensure model is available
	if err := svc.EnsureModel(ctx); err != nil {
		return nil, err
	}

	start := time.Now()
	embeddings := make([][]float32, len(texts))
	errors := make([]error, len(texts))

	// Process in batches using goroutines
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, svc.batchSize)

	for i, text := range texts {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(idx int, txt string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			embedding, err := svc.embedSingle(ctx, txt)
			if err != nil {
				errors[idx] = err
				return
			}
			embeddings[idx] = embedding
		}(i, text)
	}

	wg.Wait()

	// Check for errors
	for i, err := range errors {
		if err != nil {
			svc.logger.Warn().
				Err(err).
				Int("index", i).
				Msg("embedding generation failed for text")
			return nil, fmt.Errorf("embedding failed for text %d: %w", i, err)
		}
	}

	svc.logger.Debug().
		Int("count", len(texts)).
		Dur("duration", time.Since(start)).
		Msg("batch embedding completed")

	return embeddings, nil
}

// embedSingle generates an embedding for a single text.
func (svc *EmbeddingService) embedSingle(ctx context.Context, text string) ([]float32, error) {
	req := &api.EmbedRequest{
		Model: svc.model,
		Input: text,
	}

	resp, err := svc.client.Embed(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("embed request failed: %w", err)
	}

	if len(resp.Embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings in response")
	}

	// Convert float64 to float32
	embedding := make([]float32, len(resp.Embeddings[0]))
	for i, v := range resp.Embeddings[0] {
		embedding[i] = float32(v)
	}

	return embedding, nil
}

// Dimension returns the embedding dimension.
func (svc *EmbeddingService) Dimension() int {
	return svc.dimension
}

// Model returns the embedding model name.
func (svc *EmbeddingService) Model() string {
	return svc.model
}

// IsReady returns whether the embedding service is ready.
func (svc *EmbeddingService) IsReady() bool {
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return svc.ready
}

// HealthCheck verifies the embedding service is operational.
func (svc *EmbeddingService) HealthCheck(ctx context.Context) error {
	// Test embedding with a simple text
	embedding, err := svc.Embed(ctx, "health check")
	if err != nil {
		return fmt.Errorf("embedding health check failed: %w", err)
	}

	if len(embedding) != svc.dimension {
		return fmt.Errorf("unexpected embedding dimension: got %d, want %d", len(embedding), svc.dimension)
	}

	return nil
}
