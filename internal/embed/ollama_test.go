package embed

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ollamaEmbedHandler fakes Ollama's POST /api/embed endpoint.
func ollamaEmbedHandler(t *testing.T, dims int, capture *[]string, mu *sync.Mutex) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/embed") {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if capture != nil {
			mu.Lock()
			*capture = append(*capture, req.Input...)
			mu.Unlock()
		}

		vecs := make([][]float32, len(req.Input))
		for i := range vecs {
			v := make([]float32, dims)
			for j := range v {
				v[j] = float32(i + 1)
			}
			vecs[i] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "embeddings": vecs})
	}
}

func TestOllamaProvider_Embed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(ollamaEmbedHandler(t, 5, nil, nil))
	defer srv.Close()

	p, err := NewOllamaProvider(OllamaConfig{Host: srv.URL, Model: "nomic-embed-text"})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	defer func() { _ = p.Close() }()

	vecs, err := p.Embed(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 3 || len(vecs[0]) != 5 {
		t.Fatalf("got %d x %d, want 3 x 5", len(vecs), len(vecs[0]))
	}
	if p.Dimensions() != 5 {
		t.Errorf("Dimensions() = %d, want 5", p.Dimensions())
	}
	if p.ModelID() != "nomic-embed-text" {
		t.Errorf("ModelID() = %q", p.ModelID())
	}
}

// TestOllamaProvider_HasTimeout is the bug #71 guard for the Ollama path: this
// is new code and must never inherit the legacy timeout-less client.
func TestOllamaProvider_HasTimeout(t *testing.T) {
	t.Parallel()

	p, err := NewOllamaProvider(OllamaConfig{Model: "m"})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	if p.http.Timeout == 0 {
		t.Fatal("Ollama provider built a client with no timeout (bug #71 class defect)")
	}
	if p.http == http.DefaultClient {
		t.Fatal("Ollama provider is using http.DefaultClient")
	}

	// A caller-supplied timeout-less client must be corrected, not trusted.
	supplied := &http.Client{}
	p2, err := NewOllamaProvider(OllamaConfig{Model: "m", HTTPClient: supplied})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	if p2.http.Timeout == 0 {
		t.Error("provider accepted a timeout-less client")
	}
	if supplied.Timeout != 0 {
		t.Error("provider mutated the caller's http.Client instead of copying it")
	}
}

func TestOllamaProvider_HangingServerTripsDeadline(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	p, _ := NewOllamaProvider(OllamaConfig{
		Host:    srv.URL,
		Model:   "m",
		Timeout: 300 * time.Millisecond,
		Retry:   RetryPolicy{MaxAttempts: 1},
	})

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := p.Embed(context.Background(), []string{"x"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout against a hanging Ollama")
		}
		if elapsed := time.Since(start); elapsed > 4*time.Second {
			t.Errorf("took %s to time out", elapsed)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Embed did not return within 4s (bug #71 regression)")
	}
}

func TestOllamaProvider_PrefixAndSuffixApplied(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(ollamaEmbedHandler(t, 3, &seen, &mu))
	defer srv.Close()

	p, _ := NewOllamaProvider(OllamaConfig{
		Host:        srv.URL,
		Model:       "qwen3-embedding",
		DocPrefix:   "doc: ",
		QueryPrefix: "qry: ",
		InputSuffix: "<|endoftext|>",
	})

	if _, err := p.Embed(context.Background(), []string{"alpha"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if _, err := p.EmbedQuery(context.Background(), []string{"beta"}); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"doc: alpha<|endoftext|>", "qry: beta<|endoftext|>"}
	if len(seen) != 2 || seen[0] != want[0] || seen[1] != want[1] {
		t.Errorf("inputs = %q, want %q", seen, want)
	}
}

func TestOllamaProvider_Batching(t *testing.T) {
	t.Parallel()

	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		ollamaEmbedHandler(t, 2, nil, nil)(w, r)
	}))
	defer srv.Close()

	p, _ := NewOllamaProvider(OllamaConfig{Host: srv.URL, Model: "m", BatchSize: 2})
	vecs, err := p.Embed(context.Background(), []string{"a", "b", "c", "d", "e"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 5 {
		t.Fatalf("got %d vectors, want 5", len(vecs))
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("made %d requests, want 3 batches", n)
	}
}

func TestOllamaProvider_EmptyInput(t *testing.T) {
	t.Parallel()

	p, _ := NewOllamaProvider(OllamaConfig{Host: "http://127.0.0.1:1", Model: "m"})
	vecs, err := p.Embed(context.Background(), nil)
	if err != nil || vecs != nil {
		t.Fatalf("Embed(nil) = %v, %v; want nil, nil", vecs, err)
	}
}

func TestOllamaProvider_Unavailable(t *testing.T) {
	t.Parallel()

	p, _ := NewOllamaProvider(OllamaConfig{
		Host:    "http://127.0.0.1:1",
		Model:   "m",
		Timeout: 2 * time.Second,
		Retry:   RetryPolicy{MaxAttempts: 1},
	})
	if err := p.Health(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Health err = %v, want ErrUnavailable", err)
	}
}

func TestOllamaProvider_EmptyResponseIsAnError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"m","embeddings":[]}`))
	}))
	defer srv.Close()

	p, _ := NewOllamaProvider(OllamaConfig{Host: srv.URL, Model: "m", Retry: RetryPolicy{MaxAttempts: 1}})
	_, err := p.Embed(context.Background(), []string{"a"})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
}

func TestNewOllamaProvider_Validation(t *testing.T) {
	t.Parallel()

	if _, err := NewOllamaProvider(OllamaConfig{}); err == nil {
		t.Error("expected an error when Model is empty")
	}
	if _, err := NewOllamaProvider(OllamaConfig{Model: "m", Host: "://bad"}); err == nil {
		t.Error("expected an error for an unparseable host")
	}
	p, err := NewOllamaProvider(OllamaConfig{Model: "m"})
	if err != nil {
		t.Fatalf("NewOllamaProvider: %v", err)
	}
	if p.host != strings.TrimRight(DefaultOllamaHost, "/") {
		t.Errorf("host = %q, want the default %q", p.host, DefaultOllamaHost)
	}
}

func TestOllamaProvider_Health(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(ollamaEmbedHandler(t, 4, nil, nil))
	defer srv.Close()

	p, _ := NewOllamaProvider(OllamaConfig{Host: srv.URL, Model: "m"})
	if err := p.Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
}

func TestDecorate(t *testing.T) {
	t.Parallel()

	in := []string{"a", "b"}

	if got := decorate(in, "", ""); &got[0] != &in[0] {
		t.Error("decorate with no prefix/suffix should return the input slice unchanged")
	}

	got := decorate(in, "p:", "!s")
	if got[0] != "p:a!s" || got[1] != "p:b!s" {
		t.Errorf("decorate = %q", got)
	}
	if in[0] != "a" {
		t.Error("decorate mutated the caller's slice")
	}
}
