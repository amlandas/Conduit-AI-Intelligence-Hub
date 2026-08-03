package embed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// embedHandler builds a handler returning dims-wide vectors for every input.
func embedHandler(t *testing.T, dims int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}

		var req openAIEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		resp := map[string]any{"object": "list", "model": req.Model}
		data := make([]map[string]any, 0, len(req.Input))
		for i := range req.Input {
			vec := make([]float32, dims)
			for j := range vec {
				vec[j] = float32(i + 1)
			}
			data = append(data, map[string]any{"object": "embedding", "index": i, "embedding": vec})
		}
		resp["data"] = data
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func TestLlamaServerProvider_Embed(t *testing.T) {
	t.Parallel()

	var gotBody openAIEmbeddingRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &gotBody)
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		embedHandler(t, 4)(w, r)
	}))
	defer srv.Close()

	p, err := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL, Model: "test-model"})
	if err != nil {
		t.Fatalf("NewLlamaServerProvider: %v", err)
	}
	defer func() { _ = p.Close() }()

	vecs, err := p.Embed(context.Background(), []string{"alpha", "beta"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("got %d vectors, want 2", len(vecs))
	}
	if len(vecs[0]) != 4 {
		t.Fatalf("got width %d, want 4", len(vecs[0]))
	}
	if p.Dimensions() != 4 {
		t.Errorf("Dimensions() = %d, want 4 (learned from response)", p.Dimensions())
	}
	if p.ModelID() != "test-model" {
		t.Errorf("ModelID() = %q, want test-model", p.ModelID())
	}
	if gotBody.EncodingFormat != "float" {
		t.Errorf("encoding_format = %q, want float", gotBody.EncodingFormat)
	}
}

func TestLlamaServerProvider_EmptyInputMakesNoRequest(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		embedHandler(t, 4)(w, r)
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL})
	vecs, err := p.Embed(context.Background(), nil)
	if err != nil || vecs != nil {
		t.Fatalf("Embed(nil) = %v, %v; want nil, nil", vecs, err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Errorf("made %d HTTP requests for empty input, want 0", n)
	}
}

func TestLlamaServerProvider_PrefixesApplied(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			raw, _ := io.ReadAll(r.Body)
			var req openAIEmbeddingRequest
			_ = json.Unmarshal(raw, &req)
			mu.Lock()
			seen = append(seen, req.Input...)
			mu.Unlock()
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		embedHandler(t, 3)(w, r)
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL:     srv.URL,
		DocPrefix:   "search_document: ",
		QueryPrefix: "search_query: ",
	})

	if _, err := p.Embed(context.Background(), []string{"doc"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if _, err := p.EmbedQuery(context.Background(), []string{"qry"}); err != nil {
		t.Fatalf("EmbedQuery: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []string{"search_document: doc", "search_query: qry"}
	if len(seen) != len(want) {
		t.Fatalf("saw %v, want %v", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("input[%d] = %q, want %q", i, seen[i], want[i])
		}
	}
}

func TestLlamaServerProvider_Batching(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var batchSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			raw, _ := io.ReadAll(r.Body)
			var req openAIEmbeddingRequest
			_ = json.Unmarshal(raw, &req)
			mu.Lock()
			batchSizes = append(batchSizes, len(req.Input))
			mu.Unlock()
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
		}
		embedHandler(t, 2)(w, r)
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL, BatchSize: 2})

	texts := []string{"a", "b", "c", "d", "e"}
	vecs, err := p.Embed(context.Background(), texts)
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vecs) != len(texts) {
		t.Fatalf("got %d vectors, want %d", len(vecs), len(texts))
	}

	mu.Lock()
	defer mu.Unlock()
	want := []int{2, 2, 1}
	if fmt.Sprint(batchSizes) != fmt.Sprint(want) {
		t.Errorf("batch sizes = %v, want %v", batchSizes, want)
	}
}

func TestLlamaServerProvider_OutOfOrderResponseIsSorted(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return indices reversed; the client must restore input order.
		body := `{"object":"list","model":"m","data":[
			{"object":"embedding","index":1,"embedding":[2,2]},
			{"object":"embedding","index":0,"embedding":[1,1]}]}`
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL})
	vecs, err := p.Embed(context.Background(), []string{"first", "second"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if vecs[0][0] != 1 || vecs[1][0] != 2 {
		t.Errorf("vectors not reordered by index: got %v", vecs)
	}
}

func TestLlamaServerProvider_ErrorShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		status     int
		body       string
		wantSubstr string
		retried    bool
	}{
		{
			name:       "openai object error",
			status:     http.StatusBadRequest,
			body:       `{"error":{"message":"input too long","type":"invalid_request_error"}}`,
			wantSubstr: "invalid_request_error: input too long",
		},
		{
			name:       "bare string error",
			status:     http.StatusBadRequest,
			body:       `{"error":"model not loaded"}`,
			wantSubstr: "model not loaded",
		},
		{
			name:       "non json body",
			status:     http.StatusBadRequest,
			body:       `something exploded`,
			wantSubstr: "something exploded",
		},
		{
			name:       "server error is retried then surfaced",
			status:     http.StatusInternalServerError,
			body:       `{"error":{"message":"kv cache full","type":"server_error"}}`,
			wantSubstr: "kv cache full",
			retried:    true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&hits, 1)
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			p, _ := NewLlamaServerProvider(LlamaServerConfig{
				BaseURL: srv.URL,
				Timeout: 3 * time.Second,
				Retry:   RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond},
			})

			_, err := p.Embed(context.Background(), []string{"x"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantSubstr)
			}

			hitCount := atomic.LoadInt32(&hits)
			if tc.retried && hitCount != 2 {
				t.Errorf("transient failure hit server %d times, want 2 (retried)", hitCount)
			}
			if !tc.retried && hitCount != 1 {
				t.Errorf("permanent failure hit server %d times, want 1 (no retry)", hitCount)
			}
		})
	}
}

func TestLlamaServerProvider_RetrySucceedsAfterTransientFailure(t *testing.T) {
	t.Parallel()

	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":{"message":"loading model","type":"unavailable"}}`))
			return
		}
		embedHandler(t, 3)(w, r)
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL: srv.URL,
		Retry:   RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond},
	})

	vecs, err := p.Embed(context.Background(), []string{"x"})
	if err != nil {
		t.Fatalf("Embed after transient failure: %v", err)
	}
	if len(vecs) != 1 {
		t.Fatalf("got %d vectors, want 1", len(vecs))
	}
	if n := atomic.LoadInt32(&hits); n != 2 {
		t.Errorf("server hit %d times, want 2", n)
	}
}

// TestLlamaServerProvider_HangingServerTripsDeadline is the regression guard
// for bug #71: a server that never responds must not wedge the caller. The
// whole test is bounded well under five seconds.
func TestLlamaServerProvider_HangingServerTripsDeadline(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release // never respond until the test tears down
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL: srv.URL,
		Timeout: 300 * time.Millisecond,
		Retry:   RetryPolicy{MaxAttempts: 1},
	})

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		_, err := p.Embed(context.Background(), []string{"hangs"})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error from a hanging server")
		}
		if elapsed := time.Since(start); elapsed > 4*time.Second {
			t.Errorf("took %s to time out, want well under 4s", elapsed)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Embed did not return within 4s against a hanging server (bug #71 regression)")
	}
}

// TestLlamaServerProvider_CallerContextWins proves the caller's deadline is
// honoured even when the provider timeout is much larger.
func TestLlamaServerProvider_CallerContextWins(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		srv.Close()
	}()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL: srv.URL,
		Timeout: 60 * time.Second,
		Retry:   RetryPolicy{MaxAttempts: 1},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := p.Embed(ctx, []string{"x"}); err == nil {
		t.Fatal("expected the caller deadline to abort the call")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("caller deadline took %s to take effect", elapsed)
	}
}

// TestNoDefaultHTTPClient proves the constructed client always carries a
// timeout, including when a caller passes a timeout-less client.
func TestNoDefaultHTTPClient(t *testing.T) {
	t.Parallel()

	p, err := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL:    "http://127.0.0.1:1",
		HTTPClient: &http.Client{}, // no timeout, like http.DefaultClient
	})
	if err != nil {
		t.Fatalf("NewLlamaServerProvider: %v", err)
	}
	if p.client.Timeout == 0 {
		t.Fatal("provider accepted a client with no timeout (bug #71 class defect)")
	}
	if p.client == http.DefaultClient {
		t.Fatal("provider is using http.DefaultClient")
	}
}

func TestLlamaServerProvider_DimensionMismatch(t *testing.T) {
	t.Parallel()

	var call int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&call, 1)
		width := 4
		if n > 1 {
			width = 8 // width changes underneath the client
		}
		vec := make([]float32, width)
		body := map[string]any{
			"object": "list", "model": "m",
			"data": []map[string]any{{"object": "embedding", "index": 0, "embedding": vec}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL, Retry: RetryPolicy{MaxAttempts: 1}})

	if _, err := p.Embed(context.Background(), []string{"a"}); err != nil {
		t.Fatalf("first Embed: %v", err)
	}
	_, err := p.Embed(context.Background(), []string{"b"})
	if !errors.Is(err, ErrDimensionMismatch) {
		t.Fatalf("err = %v, want ErrDimensionMismatch", err)
	}
}

func TestLlamaServerProvider_EmptyDataIsAnError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","model":"m","data":[]}`))
	}))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL, Retry: RetryPolicy{MaxAttempts: 1}})
	_, err := p.Embed(context.Background(), []string{"a"})
	if !errors.Is(err, ErrEmptyResponse) {
		t.Fatalf("err = %v, want ErrEmptyResponse", err)
	}
}

func TestLlamaServerProvider_ConnectionRefusedIsUnavailable(t *testing.T) {
	t.Parallel()

	// Port 1 on loopback is not listening.
	p, _ := NewLlamaServerProvider(LlamaServerConfig{
		BaseURL: "http://127.0.0.1:1",
		Timeout: 2 * time.Second,
		Retry:   RetryPolicy{MaxAttempts: 1},
	})

	_, err := p.Embed(context.Background(), []string{"a"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestLlamaServerProvider_Health(t *testing.T) {
	t.Parallel()

	t.Run("healthy", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(embedHandler(t, 2))
		defer srv.Close()
		p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL})
		if err := p.Health(context.Background()); err != nil {
			t.Fatalf("Health: %v", err)
		}
	})

	t.Run("loading model returns 503", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"loading model"}`))
		}))
		defer srv.Close()
		p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL})
		err := p.Health(context.Background())
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})
}

func TestNewLlamaServerProvider_RequiresBaseURL(t *testing.T) {
	t.Parallel()
	if _, err := NewLlamaServerProvider(LlamaServerConfig{}); err == nil {
		t.Fatal("expected an error when BaseURL is empty")
	}
}

func TestLlamaServerProvider_ConcurrentEmbed(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(embedHandler(t, 6))
	defer srv.Close()

	p, _ := NewLlamaServerProvider(LlamaServerConfig{BaseURL: srv.URL, Dimensions: 6})

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			vecs, err := p.Embed(context.Background(), []string{fmt.Sprintf("text-%d", i)})
			if err != nil {
				errs <- err
				return
			}
			if len(vecs) != 1 || len(vecs[0]) != 6 {
				errs <- fmt.Errorf("unexpected shape %d x %d", len(vecs), len(vecs[0]))
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent Embed: %v", err)
	}
}
