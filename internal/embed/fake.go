package embed

import (
	"context"
	"errors"
	"hash/fnv"
	"math"
	"sync"
)

// FakeProvider is a deterministic, dependency-free Provider for tests.
//
// Vectors are derived from a hash of the input text seeded with a fixed seed,
// so the same text always yields the same vector both within and across runs.
// Vectors are L2-normalised, so a dot product is a cosine similarity.
//
// FakeProvider is exported because other packages (internal/kb, daemon tests)
// need an embedder that works with no external service.
type FakeProvider struct {
	dim   int
	model string
	seed  uint64

	mu       sync.Mutex
	calls    int
	embedded int

	// EmbedErr, when set, is returned by Embed instead of vectors.
	EmbedErr error
	// HealthErr, when set, is returned by Health.
	HealthErr error
	// Hook, when set, runs at the start of each Embed call. It can be used to
	// block, to observe concurrency, or to honour a context deadline.
	Hook func(ctx context.Context, texts []string) error
}

var _ Provider = (*FakeProvider)(nil)

// NewFakeProvider returns a deterministic provider producing dim-wide vectors.
func NewFakeProvider(model string, dim int, seed uint64) *FakeProvider {
	if dim <= 0 {
		dim = 8
	}
	if model == "" {
		model = "fake-embed"
	}
	return &FakeProvider{dim: dim, model: model, seed: seed}
}

// ModelID implements Provider.
func (f *FakeProvider) ModelID() string { return f.model }

// Dimensions implements Provider.
func (f *FakeProvider) Dimensions() int { return f.dim }

// Close implements Provider.
func (f *FakeProvider) Close() error { return nil }

// Health implements Provider.
func (f *FakeProvider) Health(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return f.HealthErr
}

// Calls reports how many times Embed was invoked.
func (f *FakeProvider) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// Embedded reports how many texts were embedded in total.
func (f *FakeProvider) Embedded() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.embedded
}

// Embed implements Provider with deterministic pseudo-random vectors.
func (f *FakeProvider) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.Hook != nil {
		if err := f.Hook(ctx, texts); err != nil {
			return nil, err
		}
	}

	f.mu.Lock()
	f.calls++
	f.embedded += len(texts)
	embedErr := f.EmbedErr
	f.mu.Unlock()

	if embedErr != nil {
		return nil, embedErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := make([][]float32, len(texts))
	for i, t := range texts {
		out[i] = f.vectorFor(t)
	}
	return out, nil
}

// vectorFor deterministically maps a string to a unit vector.
//
// A splitmix64 stream seeded by FNV-1a(text) XOR seed fills the vector, which
// is then L2-normalised. Identical inputs map to identical outputs; similar
// inputs are NOT meaningfully close, so this fake is suitable for plumbing and
// determinism tests, not for retrieval-quality assertions.
func (f *FakeProvider) vectorFor(text string) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	state := h.Sum64() ^ f.seed

	vec := make([]float32, f.dim)
	var sumSq float64
	for i := range vec {
		state += 0x9E3779B97F4A7C15
		z := state
		z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
		z = (z ^ (z >> 27)) * 0x94D049BB133111EB
		z ^= z >> 31
		// Map to [-1, 1).
		v := float64(int64(z>>11))/float64(int64(1)<<52) - 1.0
		vec[i] = float32(v)
		sumSq += v * v
	}

	norm := math.Sqrt(sumSq)
	if norm == 0 {
		// Degenerate; emit a fixed unit vector so callers never see NaN.
		vec[0] = 1
		return vec
	}
	for i := range vec {
		vec[i] = float32(float64(vec[i]) / norm)
	}
	return vec
}

// ErrFakeFailure is a convenience error for tests that want a permanent
// (non-retryable) provider failure.
var ErrFakeFailure = errors.New("embed: fake provider failure")
