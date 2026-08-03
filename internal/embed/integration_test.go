package embed

import (
	"context"
	"os"
	"testing"
	"time"
)

// Integration tests in this file exercise the real llama-server and the real
// Ollama daemon. CI has neither, so every test here is skip-gated on the
// binary or endpoint actually being present. They never fail a clean CI run.
//
// Run them locally with:
//
//	CONDUIT_TEST_GGUF=/path/to/nomic-embed-text-v1.5.f16.gguf \
//	  go test -tags fts5 -run Integration ./internal/embed/

// envTestGGUF points the sidecar integration test at a real model file.
const envTestGGUF = "CONDUIT_TEST_GGUF"

// requireLlamaServer skips unless llama-server is installed.
func requireLlamaServer(t *testing.T) string {
	t.Helper()
	bin, err := FindLlamaServer("")
	if err != nil {
		t.Skipf("llama-server not installed, skipping integration test: %v", err)
	}
	return bin
}

// requireTestModel skips unless a real GGUF was provided.
func requireTestModel(t *testing.T) string {
	t.Helper()
	path := os.Getenv(envTestGGUF)
	if path == "" {
		t.Skipf("set %s to a GGUF path to run this integration test", envTestGGUF)
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s=%q is not readable: %v", envTestGGUF, path, err)
	}
	return path
}

// TestIntegration_RealLlamaServer runs the full managed-sidecar lifecycle
// against a real llama-server and a real model.
func TestIntegration_RealLlamaServer(t *testing.T) {
	bin := requireLlamaServer(t)
	modelPath := requireTestModel(t)

	spec, err := LookupModel(DefaultModelID)
	if err != nil {
		t.Fatalf("LookupModel: %v", err)
	}

	cfg, err := ManagerConfigForModel(t.TempDir(), spec.ID, modelPath)
	if err != nil {
		t.Fatalf("ManagerConfigForModel: %v", err)
	}
	cfg.BinaryPath = bin
	cfg.IdleTimeout = -1
	cfg.StartupTimeout = 3 * time.Minute

	m, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := m.Shutdown(ctx); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
		_ = m.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	p, err := m.Provider(ctx)
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}

	vecs, err := p.Embed(ctx, []string{
		"The lantern is trimmed at dusk.",
		"The harbour master signs the ledger each morning.",
	})
	if err != nil {
		t.Fatalf("Embed against real llama-server: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	if len(vecs[0]) != spec.Dimensions {
		t.Errorf("vector width %d, want %d for %s", len(vecs[0]), spec.Dimensions, spec.ID)
	}

	// Distinct inputs must produce distinct vectors; identical output would
	// mean pooling or batching is broken.
	same := true
	for i := range vecs[0] {
		if vecs[0][i] != vecs[1][i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two different texts produced identical vectors")
	}

	if err := p.Health(ctx); err != nil {
		t.Errorf("Health: %v", err)
	}

	// A second manager must reuse the running instance, not spawn another.
	second, err := NewManager(cfg)
	if err != nil {
		t.Fatalf("second NewManager: %v", err)
	}
	defer func() { _ = second.Close() }()

	ep1, _ := m.Ensure(ctx)
	ep2, err := second.Ensure(ctx)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if ep1 != ep2 {
		t.Errorf("second manager got %q, want the shared %q", ep2, ep1)
	}
}

// TestIntegration_RealOllama exercises the Ollama provider against a live
// daemon, if one happens to be running.
func TestIntegration_RealOllama(t *testing.T) {
	model := os.Getenv("CONDUIT_TEST_OLLAMA_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}

	p, err := NewOllamaProvider(OllamaConfig{
		Model:   model,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	defer func() { _ = p.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := p.Health(ctx); err != nil {
		t.Skipf("Ollama not reachable with model %q, skipping: %v", model, err)
	}

	vecs, err := p.Embed(ctx, []string{"alpha text", "beta text", "gamma text"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("got %d vectors, want 3", len(vecs))
	}
	if p.Dimensions() <= 0 {
		t.Error("provider did not learn its dimension")
	}
	for i, v := range vecs {
		if len(v) != p.Dimensions() {
			t.Errorf("vector %d has width %d, want %d", i, len(v), p.Dimensions())
		}
	}
}
