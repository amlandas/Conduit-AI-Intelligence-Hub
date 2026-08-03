// Command fakesidecar is a stand-in for llama-server used by the sidecar
// manager tests.
//
// It accepts the same flags the manager passes to the real binary, binds
// 127.0.0.1 on the requested port, and serves the two endpoints the manager
// depends on: GET /health and POST /v1/embeddings. Behaviour is steered by
// environment variables so a single binary covers every lifecycle scenario.
//
// It lives under testdata/ so the go tool excludes it from normal builds; the
// test compiles it on demand. It uses only the standard library.
package main

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Environment knobs honoured by the fake.
const (
	// envStartupDelay delays binding the listener, simulating model load.
	envStartupDelay = "FAKE_SIDECAR_STARTUP_DELAY"
	// envExitCode makes the process exit immediately with the given code,
	// simulating a sidecar that dies during startup.
	envExitCode = "FAKE_SIDECAR_EXIT_CODE"
	// envSpawnChild makes the fake spawn a long-lived grandchild and record its
	// pid at the given path, so tests can prove process-group cleanup.
	envSpawnChild = "FAKE_SIDECAR_CHILD_PIDFILE"
	// envIgnoreSIGTERM makes the fake ignore SIGTERM so tests can exercise the
	// SIGKILL escalation path.
	envDimensions = "FAKE_SIDECAR_DIMENSIONS"
)

func main() {
	if code := os.Getenv(envExitCode); code != "" {
		n, err := strconv.Atoi(code)
		if err != nil {
			n = 1
		}
		fmt.Fprintln(os.Stderr, "fakesidecar: exiting immediately as instructed")
		os.Exit(n)
	}

	port, model := parseArgs(os.Args[1:])
	if port == 0 {
		log.Fatal("fakesidecar: --port is required")
	}

	dims := 8
	if d := os.Getenv(envDimensions); d != "" {
		if n, err := strconv.Atoi(d); err == nil && n > 0 {
			dims = n
		}
	}

	if pidFile := os.Getenv(envSpawnChild); pidFile != "" {
		spawnGrandchild(pidFile)
	}

	if delay := os.Getenv(envStartupDelay); delay != "" {
		if d, err := time.ParseDuration(delay); err == nil {
			// Emit something on stderr so the manager's tail buffer has content.
			fmt.Fprintf(os.Stderr, "fakesidecar: loading model %s...\n", model)
			time.Sleep(d)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		handleEmbeddings(w, r, model, dims)
	})

	// 127.0.0.1 only, matching the real sidecar's binding contract.
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("fakesidecar: listen %s: %v", addr, err)
	}
	fmt.Fprintf(os.Stderr, "fakesidecar: listening on %s\n", addr)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := srv.Serve(ln); err != nil {
		log.Fatalf("fakesidecar: serve: %v", err)
	}
}

// parseArgs extracts the flags the manager passes. Unknown flags are ignored,
// mirroring how tolerant the real binary needs us to be about extra args.
func parseArgs(args []string) (port int, model string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				port, _ = strconv.Atoi(args[i+1])
				i++
			}
		case "-m", "--model":
			if i+1 < len(args) {
				model = args[i+1]
				i++
			}
		}
	}
	return port, model
}

// spawnGrandchild starts a long-lived child process and records its pid.
//
// The manager puts the sidecar in its own process group and signals the group,
// so this grandchild must die too. A test asserts exactly that.
func spawnGrandchild(pidFile string) {
	cmd := exec.Command("sleep", "600")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fakesidecar: failed to spawn grandchild: %v\n", err)
		return
	}
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600)
	go func() { _ = cmd.Wait() }()
}

// embeddingRequest mirrors the OpenAI-compatible request body.
type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// handleEmbeddings returns deterministic vectors in OpenAI response shape.
func handleEmbeddings(w http.ResponseWriter, r *http.Request, model string, dims int) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req embeddingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid request body","type":"invalid_request_error"}}`))
		return
	}
	if len(req.Input) == 0 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"input is required","type":"invalid_request_error"}}`))
		return
	}

	type datum struct {
		Object    string    `json:"object"`
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	}
	resp := struct {
		Object string  `json:"object"`
		Model  string  `json:"model"`
		Data   []datum `json:"data"`
	}{Object: "list", Model: model}

	for i, in := range req.Input {
		resp.Data = append(resp.Data, datum{
			Object:    "embedding",
			Index:     i,
			Embedding: deterministicVector(in, dims),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// deterministicVector maps text to a stable vector so tests can assert on it.
func deterministicVector(text string, dims int) []float32 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	state := h.Sum64()

	out := make([]float32, dims)
	for i := range out {
		state = state*6364136223846793005 + 1442695040888963407
		out[i] = float32(int64(state>>33)) / float32(1<<31)
	}
	return out
}
