// Package embed provides the embedding provider layer for Conduit.
//
// The package exists so that Conduit can produce embeddings with no external
// service installed by the user. The primary provider is a managed llama-server
// sidecar (see sidecar.go) speaking the OpenAI-compatible /v1/embeddings API.
// Ollama is retained as an optional provider behind the same interface.
//
// Design rules for everything in this package:
//
//   - Every outbound call is bounded by a context deadline AND an
//     http.Client.Timeout. http.DefaultClient is never used (it has no timeout;
//     that is known bug #71 in the legacy internal/kb path).
//   - The sidecar binds 127.0.0.1 only. No other interface is ever used.
//   - No secrets are read, written, or logged.
package embed

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// Provider produces vector embeddings for a batch of texts.
//
// Implementations must be safe for concurrent use by multiple goroutines.
type Provider interface {
	// Embed returns one vector per input text, in the same order as the input.
	// An empty input slice returns (nil, nil) without performing any I/O.
	//
	// The supplied context bounds the whole call including any retries.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions reports the vector width this provider produces. It returns 0
	// if the width is not yet known (some providers learn it from the first
	// successful response).
	Dimensions() int

	// ModelID reports the model identifier this provider embeds with.
	ModelID() string

	// Health verifies the provider can currently serve requests. It performs a
	// cheap probe and must respect the context deadline.
	Health(ctx context.Context) error

	// Close releases any resources held by the provider. It is safe to call
	// more than once. Close does not necessarily stop a shared sidecar; see
	// Manager.Shutdown for that.
	Close() error
}

// Sentinel errors returned by this package. Callers should test with
// errors.Is rather than comparing strings.
var (
	// ErrBinaryNotFound indicates the llama-server executable could not be
	// located. The error text carries an actionable install hint.
	ErrBinaryNotFound = errors.New("embed: llama-server binary not found")

	// ErrModelNotFound indicates the configured GGUF model file is missing.
	ErrModelNotFound = errors.New("embed: model file not found")

	// ErrUnavailable indicates the provider is not currently reachable.
	ErrUnavailable = errors.New("embed: provider unavailable")

	// ErrDimensionMismatch indicates the provider returned a vector whose width
	// disagrees with the width previously observed or configured.
	ErrDimensionMismatch = errors.New("embed: embedding dimension mismatch")

	// ErrEmptyResponse indicates the provider returned no vectors for a
	// non-empty input.
	ErrEmptyResponse = errors.New("embed: provider returned no embeddings")
)

// Default tuning values. All are overridable via Config.
const (
	// DefaultTimeout bounds a single embedding call, retries included.
	DefaultTimeout = 30 * time.Second

	// DefaultMaxAttempts is the total number of tries (1 initial + 2 retries).
	DefaultMaxAttempts = 3

	// DefaultBaseDelay is the first backoff interval.
	DefaultBaseDelay = 200 * time.Millisecond

	// DefaultMaxDelay caps exponential backoff growth.
	DefaultMaxDelay = 5 * time.Second

	// DefaultBatchSize caps how many texts are sent in one HTTP request.
	DefaultBatchSize = 32
)

// RetryPolicy controls bounded retries with exponential backoff and jitter.
//
// Only errors classified as transient (see transientError) are retried.
// Backoff never sleeps past the context deadline.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts, not the number of retries.
	// Values < 1 are treated as 1.
	MaxAttempts int

	// BaseDelay is the delay before the second attempt.
	BaseDelay time.Duration

	// MaxDelay caps the delay between attempts.
	MaxDelay time.Duration
}

// DefaultRetryPolicy returns the package default retry policy.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: DefaultMaxAttempts,
		BaseDelay:   DefaultBaseDelay,
		MaxDelay:    DefaultMaxDelay,
	}
}

// normalize fills in sane values for zero fields.
func (p RetryPolicy) normalize() RetryPolicy {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultBaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultMaxDelay
	}
	if p.MaxDelay < p.BaseDelay {
		p.MaxDelay = p.BaseDelay
	}
	return p
}

// delayFor returns the backoff before the given zero-based attempt index.
// Attempt 0 has no delay. Jitter is full-jitter over [0, exp) to avoid
// synchronised retry storms across N conduit processes.
func (p RetryPolicy) delayFor(attempt int, rnd func() float64) time.Duration {
	if attempt <= 0 {
		return 0
	}
	d := p.BaseDelay
	for i := 1; i < attempt; i++ {
		d *= 2
		if d >= p.MaxDelay {
			d = p.MaxDelay
			break
		}
	}
	if d > p.MaxDelay {
		d = p.MaxDelay
	}
	// Full jitter, but keep at least half the interval so we still back off.
	jittered := time.Duration(float64(d) * (0.5 + 0.5*rnd()))
	return jittered
}

// transient marks an error as worth retrying.
type transient struct{ err error }

func (t *transient) Error() string { return t.err.Error() }
func (t *transient) Unwrap() error { return t.err }

// markTransient wraps err so retryCall will retry it.
func markTransient(err error) error {
	if err == nil {
		return nil
	}
	return &transient{err: err}
}

// isTransient reports whether err was marked retryable.
func isTransient(err error) bool {
	var t *transient
	return errors.As(err, &t)
}

// retryCall runs fn up to policy.MaxAttempts times, retrying only transient
// failures and honouring the context deadline for both the call and the sleep.
func retryCall(ctx context.Context, policy RetryPolicy, rnd func() float64, fn func(ctx context.Context) error) error {
	policy = policy.normalize()
	if rnd == nil {
		rnd = rand.Float64
	}

	var lastErr error
	for attempt := 0; attempt < policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if lastErr != nil {
				return fmt.Errorf("%w (last error: %v)", err, lastErr)
			}
			return err
		}

		if d := policy.delayFor(attempt, rnd); d > 0 {
			timer := time.NewTimer(d)
			select {
			case <-ctx.Done():
				timer.Stop()
				if lastErr != nil {
					return fmt.Errorf("%w (last error: %v)", ctx.Err(), lastErr)
				}
				return ctx.Err()
			case <-timer.C:
			}
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}
		lastErr = err

		// Never retry a context failure or a permanent error.
		if ctx.Err() != nil || !isTransient(err) {
			return unwrapTransient(err)
		}
	}

	return fmt.Errorf("embed: giving up after %d attempts: %w", policy.MaxAttempts, unwrapTransient(lastErr))
}

// unwrapTransient strips the retry marker so callers see the underlying error.
func unwrapTransient(err error) error {
	var t *transient
	if errors.As(err, &t) {
		return t.err
	}
	return err
}

// chunkTexts splits texts into batches of at most size elements.
func chunkTexts(texts []string, size int) [][]string {
	if size <= 0 {
		size = DefaultBatchSize
	}
	var out [][]string
	for i := 0; i < len(texts); i += size {
		end := i + size
		if end > len(texts) {
			end = len(texts)
		}
		out = append(out, texts[i:end])
	}
	return out
}
