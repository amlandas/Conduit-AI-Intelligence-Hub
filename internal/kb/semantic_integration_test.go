package kb

// Integration tests for the parts of retrieval that still need a live service.
//
// WHAT USED TO BE HERE
//
// Before WP-2.1 this file also covered the vector store round trip, two-strategy
// fusion, and the degraded-mode branch -- all skip-gated behind a live Qdrant,
// and therefore never run in CI. That was the migration's blind spot, and it is
// now closed: vectors live in SQLite, and SemanticSearcher takes an Embedder and
// a VectorIndex rather than constructing them, so all three are hermetic:
//
//	vecstore_sqlite_test.go  - upsert / search / delete, WAL concurrency,
//	                           single-transaction ingestion
//	semantic_fusion_test.go  - two-strategy fusion with real injected vectors,
//	                           and degraded mode when the vector index errors
//
// WHAT REMAINS SKIP-GATED
//
// Only the Ollama round trip below. Generating a real embedding needs the real
// model; nothing about it can be faked without testing the fake instead. It
// skips when Ollama is absent, which is every CI run today.
//
// WP-2.2 (embedding sidecar) owns closing this last gate.

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/simpleflo/conduit/internal/embed"
)

// probeTimeout is deliberately tiny: on a machine without the service, the
// connection is refused immediately; on a machine with it, the loopback
// handshake is sub-millisecond. It exists only so a firewalled port that
// blackholes SYNs cannot stall the suite.
const probeTimeout = 250 * time.Millisecond

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// requireOllama skips unless something is listening on the Ollama port.
func requireOllama(t *testing.T) string {
	t.Helper()
	host := envOr("CONDUIT_TEST_OLLAMA_HOST", "http://127.0.0.1:11434")
	addr := envOr("CONDUIT_TEST_OLLAMA_ADDR", "127.0.0.1:11434")

	conn, err := net.DialTimeout("tcp", addr, probeTimeout)
	if err != nil {
		t.Skipf("SKIP-GATED (CI blind spot): no Ollama on %s: %v", addr, err)
	}
	_ = conn.Close()
	return host
}

// TestSemanticIntegration_EmbeddingService covers the embedding round trip.
// CI blind spot: everything about vector generation -- dimension, determinism,
// batch behaviour, model availability.
func TestSemanticIntegration_EmbeddingService(t *testing.T) {
	host := requireOllama(t)

	// WP-3.4 (#71) repointed this at the live path: an internal/embed provider
	// wrapped by kb.NewProviderEmbedder. The deleted kb.EmbeddingService built
	// its client on the untimed http.DefaultClient.
	provider, err := embed.NewOllamaProvider(embed.OllamaConfig{
		Host:       host,
		Model:      DefaultEmbeddingModel,
		Dimensions: DefaultEmbeddingDimension,
	})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	t.Cleanup(func() { _ = provider.Close() })
	svc := NewProviderEmbedder(provider)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := svc.HealthCheck(ctx); err != nil {
		t.Skipf("SKIP-GATED: Ollama is up but the %s model is not usable: %v", svc.Model(), err)
	}

	t.Run("dimension matches the configured value", func(t *testing.T) {
		vec, err := svc.Embed(ctx, "the keeper trims the lantern at dusk")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(vec) != svc.Dimension() {
			t.Errorf("embedding dimension: got %d, want %d", len(vec), svc.Dimension())
		}
		if len(vec) != DefaultEmbeddingDimension {
			t.Errorf("embedding dimension: got %d, want the nomic-embed-text default %d", len(vec), DefaultEmbeddingDimension)
		}
	})

	t.Run("embedding the same text twice is stable", func(t *testing.T) {
		a, err := svc.Embed(ctx, "identical input")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		b, err := svc.Embed(ctx, "identical input")
		if err != nil {
			t.Fatalf("Embed: %v", err)
		}
		if len(a) != len(b) {
			t.Fatalf("dimension changed between calls: %d vs %d", len(a), len(b))
		}
		for i := range a {
			if a[i] != b[i] {
				t.Fatalf("embedding is not deterministic at index %d: %v vs %v", i, a[i], b[i])
			}
		}
	})

	t.Run("batch preserves input order", func(t *testing.T) {
		texts := []string{"lantern", "ledger", "rabbit"}
		batch, err := svc.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("EmbedBatch: %v", err)
		}
		if len(batch) != len(texts) {
			t.Fatalf("got %d embeddings, want %d", len(batch), len(texts))
		}
		for i, text := range texts {
			single, err := svc.Embed(ctx, text)
			if err != nil {
				t.Fatalf("Embed(%q): %v", text, err)
			}
			for j := range single {
				if batch[i][j] != single[j] {
					t.Fatalf("batch entry %d does not match the single embedding of %q", i, text)
				}
			}
		}
	})

	t.Run("empty batch is a no-op", func(t *testing.T) {
		got, err := svc.EmbedBatch(ctx, nil)
		if err != nil || got != nil {
			t.Errorf("EmbedBatch(nil) = %v, %v; want nil, nil", got, err)
		}
	})
}
