# Embedding Sidecar and Model Registry

**Status:** WP-2.2, v2 line. The provider layer and sidecar manager are
implemented in `internal/embed`. First-run model download is **not** implemented
here; it lands in WP-3.3.

Conduit v2 must produce embeddings with **no external service installed by the
user**. A managed `llama-server` process is the primary embedding provider;
Ollama is demoted to an optional provider behind the same interface.

---

## Architecture

```
internal/embed
├── provider.go      Provider interface, retry/backoff, error taxonomy
├── llamaserver.go   OpenAI-compatible POST /v1/embeddings client (hand-rolled net/http)
├── ollama.go        Optional Ollama provider (new code, timeout-configured client)
├── fake.go          Deterministic FakeProvider for tests
├── sidecar.go       Shared-singleton llama-server lifecycle manager
├── registry.go      Pinned model registry (name -> URL -> SHA-256)
└── sysutil_{unix,windows}.go   File locking and process-group control
```

All four providers satisfy one interface:

```go
type Provider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int
    ModelID() string
    Health(ctx context.Context) error
    Close() error
}
```

`LlamaServerProvider` and `OllamaProvider` additionally expose `EmbedQuery`,
which applies the model's **query** prefix instead of its document prefix.
Using the wrong one costs measurable retrieval accuracy — see
`.eng-lead-kb/BAKEOFF-WP-2.2.md` §2.4.

### Timeouts

Every call is bounded twice: by the caller's `context.Context` **and** by an
explicit `http.Client.Timeout`. `http.DefaultClient` is never used in this
package — its zero `Timeout` means a hung server wedges the caller forever,
which is known bug #71 in the legacy `internal/kb` path. A caller-supplied
client without a timeout is copied and given one rather than trusted.

Retries are bounded (3 attempts by default) with exponential backoff and full
jitter, and apply **only** to transient failures: connection errors, HTTP 5xx,
429 and 408. A 4xx is permanent and fails immediately. Backoff never sleeps past
the context deadline.

---

## Sidecar lifecycle

One `llama-server` process is shared by every Conduit process on the machine.

**State lives in `<data_dir>/embed/`:**

| File | Purpose |
|---|---|
| `sidecar.lock` | `flock` advisory lock serialising spawn and teardown |
| `sidecar.json` | pid, port, model, dimensions, started-at, last-used |

`sidecar.json` is written atomically (temp file + rename) while holding the
lock. `flock` is used rather than an `O_EXCL` sentinel because the kernel
releases it automatically when the holder dies — a Conduit process killed
mid-spawn must not wedge every other process.

**`Ensure(ctx)` algorithm:**

1. In-memory fast path — reuse the endpoint if health was confirmed within 5s.
2. Read the state file; if the pid is alive, the model matches, and `/health`
   answers 200, reuse it.
3. Otherwise take the lock and re-check (another process may have won the race).
4. Reap stale state: kill the recorded pid if it is somehow alive, remove the file.
5. Allocate an ephemeral port, spawn, poll `/health` until ready.
6. Write the state file, release the lock.

**Idle shutdown** is global, not per-process. Every process refreshes
`last_used` in the shared state file (throttled to at most once per
`IdleTimeout/4`, capped at 5s). A reaper goroutine shuts the sidecar down once
that timestamp is older than `IdleTimeout` (default 5 minutes). A later request
transparently respawns it.

**No orphans.** Children are started in their own process group and signalled as
a group (`SIGTERM`, escalating to `SIGKILL` after 5s), so grandchildren die too.
A test proves this by having the fake sidecar fork a long-lived child and
asserting both pids are gone after `Shutdown`.

**Binding.** The sidecar is started with `--host 127.0.0.1` and an ephemeral
port. It is never reachable off the loopback interface; a test asserts the
argument vector contains no `0.0.0.0` or `::`.

### Binary discovery

1. `kb.embed.binary_path` config override, if set
2. `llama-server` on `PATH`
3. `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`

If none match, the error names every location searched and carries an
actionable hint: **`brew install llama.cpp`**. Conduit does **not** auto-download
the binary.

### llama-server arguments

```
--host 127.0.0.1 --port <ephemeral> -m <gguf>
--embedding --pooling <mean|cls|last>
-c <ctx> -b <ctx> -ub <ctx>
```

Two of these are non-obvious and both are load-bearing:

- **`--pooling` is always passed explicitly.** llama-server otherwise falls back
  to GGUF metadata, and to `none` when that metadata is missing — which yields
  per-token vectors or an outright failure. Wrong pooling degrades quality
  silently, with no error and no obvious signal in the vectors.
- **`-b` and `-ub` are raised to the context size.** In embedding mode attention
  is non-causal, so an entire input sequence must fit in one physical ubatch;
  `-ub` does not split. Oversized inputs return HTTP 500. llama.cpp still has no
  startup validation for this (upstream PR #18123 remains open).

---

## Pinned model registry

Every model is pinned to an exact artifact with a verified SHA-256, so WP-3.3's
downloader can reject anything that does not match. Hashes and sizes were read
from the HuggingFace model-tree API on 2026-08-02, where the LFS `oid` **is** the
file's SHA-256.

| Model ID | Dims | Ctx | Pooling | Artifact | Size | SHA-256 |
|---|---|---|---|---|---|---|
| `nomic-embed-text-v1.5` **(default)** | 768 | 2048 | mean | [`nomic-ai/nomic-embed-text-v1.5-GGUF` / `nomic-embed-text-v1.5.f16.gguf`](https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.f16.gguf) | 274,290,560 | `f7af6f66802f4df86eda10fe9bbcfc75c39562bed48ef6ace719a251cf1c2fdb` |
| `qwen3-embedding-0.6b` | 1024 | 32768 | **last** | [`Qwen/Qwen3-Embedding-0.6B-GGUF` / `Qwen3-Embedding-0.6B-Q8_0.gguf`](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B-GGUF/resolve/main/Qwen3-Embedding-0.6B-Q8_0.gguf) | 639,150,592 | `06507c7b42688469c4e7298b0a1e16deff06caf291cf0a5b278c308249c3e439` |
| `mxbai-embed-large-v1` | 1024 | 512 | cls | [`ChristianAzinn/mxbai-embed-large-v1-gguf` / `mxbai-embed-large-v1_fp16.gguf`](https://huggingface.co/ChristianAzinn/mxbai-embed-large-v1-gguf/resolve/main/mxbai-embed-large-v1_fp16.gguf) | 669,603,712 | `819c2adf5ce6df2b6bd2ae4ca90d2a69f060afeb438d0c171db57daa02e39c3d` |

All three are Apache-2.0.

### Per-model prompt decoration

Prefixes and suffixes are part of the spec, not caller responsibility. Removing
them cost 0.04–0.08 MRR in the bake-off.

| Model | Document prefix | Query prefix | Suffix (all inputs) |
|---|---|---|---|
| `nomic-embed-text-v1.5` | `search_document: ` | `search_query: ` | — |
| `qwen3-embedding-0.6b` | — | `Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: ` | `<\|endoftext\|>` |
| `mxbai-embed-large-v1` | — | `Represent this sentence for searching relevant passages: ` | — |

**Why Qwen3 has a suffix:** its published GGUF does not append an EOS token, and
the model pools the **last** token. Without the suffix, retrieval accuracy drops
sharply — [llama.cpp #14234](https://github.com/ggml-org/llama.cpp/issues/14234).

**Why the suffix is per-model:** `<|endoftext|>` is not a special token for
BERT-style models. Applying it to `nomic-embed-text` changes the vector of the
string `"test"` by cosine 0.66, i.e. it actively damages the embedding. A global
suffix would be a correctness bug.

### Adding a model

`ModelSpec.Validate()` rejects a spec without a 64-hex-character SHA-256, a
positive byte size, HuggingFace coordinates, and a pooling mode of `mean`, `cls`
or `last`. `TestRegistry_AllSpecsAreValid` runs it over every entry, so a model
cannot be added without a verified hash.

Use `ManagerConfigForModel(dataDir, modelID, modelPath)` to build a
`ManagerConfig` — it copies pooling, dimensions and prompt decoration from the
spec so a caller cannot accidentally run a model with the wrong settings, and
caps the sidecar context at 8192 so `-b`/`-ub` do not balloon RAM on Qwen3's
32k window.

---

## Model files

Expected location: `<data_dir>/models/<gguf filename>`.

When the file is missing, the error names the exact expected path and points at
`conduit embed download <model-id>` — **a placeholder; WP-3.3 implements it.**
Nothing in this package downloads anything.

---

## Testing

Everything in `internal/embed` is hermetic. CI has neither llama-server nor
Ollama, and no test requires them.

- **Provider logic** runs against `httptest` servers covering OpenAI-compatible
  success, both llama.cpp error shapes, non-JSON bodies, out-of-order `index`
  fields, dimension drift, and a **hanging server that must trip the deadline in
  under 4 seconds** (the bug #71 guard).
- **Sidecar lifecycle** runs against a fake `llama-server` that the test compiles
  from `testdata/fakesidecar/`. It proves spawn, port discovery, second-client
  reuse, concurrent-`Ensure` deduplication, stale-pid recovery, corrupt-state
  recovery, idle shutdown and respawn, active-use protection, startup-failure
  cleanup, and no orphaned grandchildren.
- **Real llama-server and Ollama** tests live in `integration_test.go` and are
  skip-gated:

  ```sh
  CONDUIT_TEST_GGUF=~/.conduit/models/nomic-embed-text-v1.5.f16.gguf \
    go test -tags fts5 -run Integration ./internal/embed/
  ```

Package coverage: **84.5%**.

---

## Open items for later work packages

- **WP-3.3** — first-run download with SHA-256 verification against this registry.
- **WP-3.4** — flip `internal/kb` onto this provider layer and retire the
  timeout-less client in `internal/kb/embeddings.go` (bug #71).
- **Matryoshka truncation** — llama-server silently ignores the OpenAI
  `dimensions` parameter ([#25210](https://github.com/ggml-org/llama.cpp/issues/25210),
  open). Truncating nomic v1.5 below 768 dims must be done client-side with
  re-normalisation.
- **Embedding drift watch** — [#26282](https://github.com/ggml-org/llama.cpp/issues/26282)
  (open) reports vectors drifting between cold start and warm state. If it
  generalises, vectors written at index time would differ from those computed at
  query time. Consider a fixed-probe determinism check at daemon start.
