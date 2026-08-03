# Conduit Administrator Guide

**Version**: 2.0.0-beta
**Last Updated**: August 2026

Configuration, diagnostics, and operations for Conduit 2.0.

> Conduit 2.0 runs no daemon, no containers and no external services. There is
> no service to supervise, no ports to firewall (except the optional loopback
> embedding sidecar), and no cluster. "Administration" here means: configure it,
> diagnose it, back it up, remove it.

---

## Contents

1. [What's on disk](#whats-on-disk)
2. [Configuration](#configuration)
3. [Configuration reference](#configuration-reference)
4. [Embedding providers and models](#embedding-providers-and-models)
5. [Path safety policy](#path-safety-policy)
6. [Workspace isolation](#workspace-isolation)
7. [The knowledge graph](#the-knowledge-graph)
8. [The query-shape log](#the-query-shape-log)
9. [Diagnostics](#diagnostics)
10. [Backup and restore](#backup-and-restore)
11. [Performance](#performance)
12. [Prompt injection (SEC-003)](#prompt-injection-sec-003)
13. [Uninstall and v1 teardown](#uninstall-and-v1-teardown)

---

## What's on disk

Everything lives under the data directory, `~/.conduit` by default:

| Path | Contents |
|---|---|
| `~/.conduit/conduit.db` | **Everything.** FTS5 index, vectors, chunks, document metadata, graph edges. |
| `~/.conduit/conduit.yaml` | Configuration (optional; defaults apply without it). |
| `~/.conduit/conduit.log` | Log output. |
| `~/.conduit/models/` | Downloaded GGUF embedding models. |
| `~/.conduit/query-shape.jsonl` | Local query-shape log (see below). |
| `~/.conduit/backups/` | Default destination for `conduit backup`. |

The binary is installed to `~/.local/bin/conduit` by default.

The database runs in WAL mode, so you may also see `conduit.db-wal` and
`conduit.db-shm`. A `conduit kb sync` in one terminal and an MCP search in
another serialise at SQLite rather than at a daemon: writers serialise, readers
never block.

---

## Configuration

### Precedence

Highest wins:

1. **Command-line flags** — `--db`, `--data-dir`, `--log-level`
2. **Environment variables** — `CONDUIT_*`
3. **Configuration file** — the first one found
4. **Compiled defaults**

A flag only takes precedence when you actually set it; an unset flag carries
its own default and does not clobber the file.

### File location

Exactly one file is read — the first found, searched most specific first:

1. `./conduit.yaml` (working directory)
2. `~/.conduit/conduit.yaml`
3. `/etc/conduit/conduit.yaml`

The working directory comes first so a project can carry its own configuration
next to its own knowledge base. A missing config file is the normal case, not
an error.

### Environment variables

Prefix `CONDUIT_`, with `.` replaced by `_`:

| Config key | Environment variable |
|---|---|
| `data_dir` | `CONDUIT_DATA_DIR` |
| `db_path` | `CONDUIT_DB_PATH` |
| `kb.chunk_size` | `CONDUIT_KB_CHUNK_SIZE` |
| `embed.provider` | `CONDUIT_EMBED_PROVIDER` |
| `kb.rag.recall_mode` | `CONDUIT_KB_RAG_RECALL_MODE` |
| `kb.kag.enabled` | `CONDUIT_KB_KAG_ENABLED` |
| `telemetry.local_query_log` | `CONDUIT_TELEMETRY_LOCAL_QUERY_LOG` |

Every leaf key in the schema is bound, so the pattern holds throughout.

### Managing configuration

```bash
conduit config                              # current effective configuration
conduit config --all                        # all options
conduit config get embed.provider
conduit config set embed.provider none
conduit config set kb.rag.default_limit 20
conduit config set kb.kag.enabled true
conduit config unset embed.model
```

`config set` writes to `~/.conduit/conduit.yaml`.

### Unknown keys are reported, not ignored

The `Config` struct *is* the schema. Anything not reachable from it is not a
Conduit setting, and Conduit says so rather than silently ignoring it:

```
warning: ~/.conduit/conduit.yaml contains 10 unrecognised key(s):
  ai
  kb.kag.graph.falkordb
  kb.rag.enabled
  kb.rag.mmr_lambda
  ...
These are ignored. Keys for the daemon, container runtime, Qdrant and FalkorDB
were removed in Conduit 2.0.
```

This is the expected result of carrying a v1 config forward. The file still
loads; delete the named keys to silence it. `conduit doctor` checks for them
too.

---

## Configuration reference

Complete schema, with defaults.

### Top level

```yaml
data_dir: ~/.conduit          # root of everything Conduit owns
db_path: ""                   # "" means <data_dir>/conduit.db
log_level: info               # debug, info, warn, error
log_format: json              # json or console
```

`db_path` is the workspace-isolation seam; the `--db` flag binds to it.

### Knowledge base ingestion

```yaml
kb:
  workers: 4                  # parallel indexing workers
  max_file_size: 104857600    # 100 MB; larger files are skipped
  chunk_size: 1000            # characters per chunk
  chunk_overlap: 100          # overlap between adjacent chunks
  watch_debounce: 500ms       # debounce for auto-sync sources
```

Chunk size is a retrieval trade-off, not a performance knob. Smaller chunks
give more precise hits and less surrounding context; larger chunks give the AI
more to work with but dilute relevance. Changing it requires a re-index
(`conduit kb sync --rebuild-vectors`) to affect existing documents.

### Retrieval

```yaml
kb:
  rag:
    min_score: 0.0            # minimum similarity for --semantic (0.0-1.0)
    recall_mode: balanced     # high, balanced, precise
    default_limit: 10
```

`recall_mode` is the single retrieval knob:

| Mode | Behaviour |
|---|---|
| `high` | Diversity filtering off. Every similar result is kept. Use when recall matters more than tidiness. |
| `balanced` | Default. Moderate filtering. |
| `precise` | Aggressive deduplication. Fewer, more distinct results. |

`min_score: 0.0` means no filtering — return everything and let the client
decide. That is deliberate; the AI client is better placed to judge relevance
than a fixed threshold is.

> **Migrating from v1**: `semantic_weight`, `use_mmr`, `mmr_lambda`, `rerank`
> and `kb.rag.enabled` no longer exist. They fed fields the search engine either
> never read or overwrote from the preset before use, so setting them changed
> nothing observable. `recall_mode` replaces all five.

### Embeddings

```yaml
embed:
  provider: llama-server      # llama-server, ollama, none
  model: ""                   # "" means the registry default
  dimensions: 0               # 0 means take it from the model registry
  timeout_seconds: 30         # bounds one embedding call including retries
  batch_size: 32
  llama_server:
    binary: ""                # "" means search PATH and the data directory
    model_path: ""            # "" means <data_dir>/models/<registry filename>
    port: 0                   # 0 means pick a free loopback port
    idle_timeout: 10m         # 0 disables automatic shutdown
  ollama:
    host: http://localhost:11434
```

Leave `dimensions` at 0. It is the only value that cannot be wrong — anything
else must match the model exactly, and a mismatch corrupts retrieval quietly.

### MCP server

```yaml
mcp:
  kb:
    search:
      default_mode: hybrid    # hybrid, semantic, fts5
      default_limit: 10
      max_limit: 50
      semantic_fallback: true # fall back to FTS5 when semantic is unavailable
    logging:
      level: info
      to_stderr: false        # stderr is visible in the AI client
```

`to_stderr: true` is the way to debug an MCP integration — the output lands in
your AI client's server log. **Nothing may write to stdout**: the stdio
transport owns it, and anything written there corrupts the protocol stream.

### Knowledge graph

```yaml
kb:
  kag:
    enabled: false            # opt-in; no graph tables exist until true
    provider: pattern         # pattern (no LLM, no network) or ollama
    preload_model: false      # only meaningful for provider: ollama
    graph:
      backend: sqlite         # only value supported
      max_hops: 2             # traversal depth, 1-2
    extraction:
      confidence_threshold: 0.7
      max_entities_per_chunk: 20
      max_relations_per_chunk: 50
      batch_size: 10
      timeout_seconds: 60
    ollama:
      model: mistral:7b-instruct-q4_K_M
      host: http://localhost:11434
      keep_alive: 5m
```

`graph.backend` accepts only `sqlite`; the field exists so a pre-2.0 config
still parses. There is no host, port or password because there is no server.

### Path safety policy

```yaml
policy:
  forbidden_paths:            # kb add refuses these outright
    - /
    - /etc
    - /var
    - /usr
    - ~/.ssh
    - ~/.aws
    - ~/.gnupg
    - ~/.config/gcloud
    - ~/.kube
  warn_paths:                 # kb add warns but proceeds
    - ~/.config
    - ~/Documents
    - ~/Desktop
```

### Telemetry

```yaml
telemetry:
  local_query_log: true       # local-only query-shape log; never uploaded
```

---

## Embedding providers and models

### Choosing a provider

| Provider | What it does | When to use |
|---|---|---|
| `llama-server` (default) | Conduit starts and supervises a `llama-server` process bound to `127.0.0.1`, shared as a singleton, stopped after `idle_timeout`. | Default. Self-contained. |
| `ollama` | Uses an Ollama daemon you already run. | You already have Ollama and want one model server. |
| `none` | No embeddings. Search is FTS5 keyword matching only. | Constrained machines, or when keyword search is enough. **A supported mode, not a degraded one.** |

Setting `none` means no model is loaded and no port is opened. `conduit doctor`
skips the embedding probe rather than reporting a failure.

`llama-server` is not bundled. It must be on `PATH` or in the data directory,
or named explicitly in `embed.llama_server.binary`. See
[EMBEDDING_SIDECAR.md](EMBEDDING_SIDECAR.md).

### The model registry

Models are **pinned**: each is tied to an exact file in an exact HuggingFace
repository with an exact SHA-256, plus the pooling mode and instruction
prefixes it requires. Pooling and prefixes are not cosmetic — the wrong mode
degrades retrieval silently, with no error and no obvious signal in the
vectors.

```bash
conduit model list
conduit model download [model-id]
conduit model verify [model-id]
conduit model path [model-id]
```

| Model | Dimensions | Context | Quant | Size |
|---|---|---|---|---|
| `nomic-embed-text-v1.5` (default) | 768 | 2048 | F16 | 261.6 MB |
| `mxbai-embed-large-v1` | 1024 | 512 | F16 | 638.6 MB |
| `qwen3-embedding-0.6b` | 1024 | 32768 | Q8_0 | 609.5 MB |

All Apache-2.0.

**Download integrity is not optional.** The file downloads to a temporary name
and is renamed into place only once its SHA-256 matches the registry. A
mismatch deletes the download and fails. There is no flag to install an
unverified model. `conduit model download` is idempotent — an already-correct
model is left alone — which is why install scripts call it unconditionally.

```bash
conduit model download --force          # re-fetch; existing file kept until the new one verifies
conduit model download --timeout 30m
conduit model download --json
```

`conduit model verify` re-hashes a local file and answers one question: is the
GGUF on this machine the exact artifact Conduit expects, or has it been
truncated, corrupted or replaced.

### Changing models

Different models produce different vector widths, so existing vectors become
wrong. After switching:

```bash
conduit config set embed.model qwen3-embedding-0.6b
conduit model download qwen3-embedding-0.6b
conduit kb sync --rebuild-vectors
```

Leave `embed.dimensions` at 0 so the width comes from the registry.

---

## Path safety policy

`policy.forbidden_paths` and `policy.warn_paths` are **enforced** on
`conduit kb add`, after symlink resolution.

This closes a real v1 exposure: `conduit kb add ~/.ssh` was accepted, and every
private key in it was chunked into a full-text index that the MCP server would
hand to any connected AI client on request. The lists existed in v1 config —
expanded, printed, and covered by tests — but nothing ever read them.

A refusal looks like this:

```
refusing to index ~/.ssh: it is inside ~/.ssh, which kb.policy.forbidden_paths
marks as forbidden. Indexing it would copy its contents into a searchable
database that the MCP server exposes to connected AI clients.
```

Warnings do not block. `~/Documents` is both a perfectly reasonable thing to
index and a place people keep tax returns, so it warns and proceeds.

**Editing the lists.** They are yours. Add directories that hold credentials in
your environment; remove entries you genuinely need indexed. Understand what
removing one means: everything under that path becomes searchable by every AI
client connected to this knowledge base.

Entries beginning with `~` are expanded at load time.

---

## Workspace isolation

`--db` selects the knowledge base file. Separate files share nothing:

```bash
conduit --db /srv/kb/engineering.db kb add /srv/docs/engineering
conduit --db /srv/kb/engineering.db kb sync
conduit --db /srv/kb/legal.db kb add /srv/docs/legal
```

Set it per client in the MCP configuration:

```json
{
  "mcpServers": {
    "conduit-eng": {
      "command": "conduit",
      "args": ["--db", "/srv/kb/engineering.db", "mcp", "kb"]
    }
  }
}
```

Or per project, with a `conduit.yaml` in the project directory setting
`db_path` — the working directory is searched before `~/.conduit`.

This is the recommended containment boundary for untrusted content. Because
indexed content reaches the AI client verbatim, a corpus you do not fully trust
belongs in a different file from the one your coding assistant has open. See
[Prompt injection](#prompt-injection-sec-003).

---

## The knowledge graph

Off by default. `kb.kag.enabled: false` means **no graph tables exist in the
database** — not empty tables, no tables.

```bash
conduit config set kb.kag.enabled true
conduit kb kag-sync
conduit kb kag-status
```

The default extraction provider is `pattern`: no LLM, no network, no model
download. Ollama-based extraction is available via `kb.kag.provider: ollama`,
and costs a model download plus real extraction time.

| Command | Purpose |
|---|---|
| `conduit kb kag-sync` | Extract entities from indexed chunks |
| `conduit kb kag-sync --force` | Re-extract everything |
| `conduit kb kag-status` | Progress, statistics, error breakdown |
| `conduit kb kag-retry` | Retry failed extractions |
| `conduit kb kag-dedupe` | Merge duplicate entities |
| `conduit kb kag-vectorize` | Embed entities, enabling `kag-query --hybrid` |
| `conduit kb kag-query <q>` | Query the graph |

`kag_query` reports three distinct states — graph disabled, graph empty, no
match — rather than conflating them. In v1 a disabled graph and a genuine miss
looked identical, which is how a graph that returned nothing went unnoticed.

**Whether the graph earns its cost is an open question**, and the project is
measuring it rather than guessing — see below.

---

## The query-shape log

`~/.conduit/query-shape.jsonl`, one line per knowledge base query. Enabled by
default (`telemetry.local_query_log: true`).

### What it records

The *shape* of a query: token count, whether the query looks like it names an
entity, and the requested traversal depth.

### What it cannot record

Your query text. Entity names. Document titles. Paths. Snippets. Results.

This is not redaction at write time — the record type **has no field capable of
holding any of them.** The only way to leak a query through this writer would be
to add a field to the struct, which is exactly what a dedicated test guards
against.

### Guarantees

- Local file, mode `0600`, under the data directory
- Nothing in the package opens a socket
- Conduit never reads it back
- Nothing uploads it anywhere; there is no endpoint, no identifier, and no
  service behind it

### Why it exists

The knowledge graph's future is gated on evidence rather than opinion: does
anyone actually ask multi-hop questions? Nobody could answer that in v1 because
nothing was measured, and it cannot be answered retroactively. This measures it
at the smallest privacy cost the design allows.

### Turning it off

```bash
conduit config set telemetry.local_query_log false
```

No file is created. Nothing else changes.

---

## Diagnostics

### `conduit doctor`

The main tool. Checks, in order:

- configuration loads, and contains no keys Conduit no longer understands
- the knowledge base file is present, readable and writable
- SQLite FTS5 is compiled in and initialised
- the embedding provider is configured and reachable (**skipped** when
  `embed.provider` is `none`)
- the vector index exists, and whether it is populated
- at least one AI client has the MCP server configured

```bash
conduit doctor
conduit doctor --json
conduit doctor --probe-timeout 30    # allow a cold model time to load
```

| Exit code | Meaning |
|---|---|
| 0 | Everything needed works (warnings may still print) |
| 1 | At least one check failed |

Raise `--probe-timeout` above the 15-second default when the embedding model is
cold; a first load can exceed it on a slower machine, and that is a slow start
rather than a broken install.

### `conduit status`

```bash
conduit status
conduit status --json
```

Reports the knowledge base file, its contents, and how retrieval is configured.
There is no background service, so nothing can be "down" and nothing is
reported about one. For diagnosis with remedies, use `doctor`.

### `conduit mcp status`

MCP configuration state across clients, search capability, vector index and
Ollama connectivity.

### Logs

```bash
conduit mcp logs --tail 100
conduit mcp logs --follow
conduit --log-level debug kb search "test"
```

For MCP integration problems, `mcp.kb.logging.to_stderr: true` surfaces server
diagnostics in the AI client's own log, which is usually faster than
correlating files.

### Sync exit codes

| Code | Meaning |
|---|---|
| 0 | Full success — keyword and semantic indexing both completed |
| 1 | Error |
| 2 | Partial success — keyword indexing worked, semantic indexing failed |

Script against these. Code 2 is the important one: search works, but on
keywords only.

---

## Backup and restore

### The whole thing is one file

```bash
cp ~/.conduit/conduit.db ~/backups/conduit-$(date +%F).db
```

That is a complete backup of the index, the vectors, the metadata and the
graph. Restore by copying it back.

Prefer to copy when Conduit is not mid-sync. In WAL mode a hot copy can miss
recent writes held in `conduit.db-wal`; either copy all three files
(`conduit.db`, `-wal`, `-shm`) or use the command below.

### `conduit backup`

Archives the data directory — database, configuration, knowledge base data — as
a compressed tarball:

```bash
conduit backup
conduit backup --output ~/backups/conduit-backup.tar.gz
```

Default destination is `~/.conduit/backups/`.

### What does not need backing up

Your source documents — Conduit never modifies them. Downloaded models: they
are re-fetchable and hash-verified, and re-downloading is cheaper than storing
them twice.

### Disaster recovery without a backup

Nothing is lost that cannot be rebuilt, because the index is derived data:

```bash
conduit setup
conduit model download
conduit kb add <path> --name "..."   # for each source
conduit kb sync
```

---

## Performance

### What the design costs and buys

Vector search is an **exact brute-force cosine scan**, not an approximate
index. Vectors are raw little-endian float32 BLOBs in the same SQLite file,
with a precomputed L2 norm, scanned in pure Go. The scan is two-phase: top-K
over vectors alone, then a join back to chunks and documents for the survivors
only.

At the target corpus size (roughly 5–50K chunks at 768 dimensions) that costs
tens of milliseconds. What it buys is exactness: every filter is an ordinary
SQL predicate evaluated *before* the distance is computed, so a selective
source filter can never silently cost recall the way post-filtering an
approximate index would.

Scan cost grows linearly with corpus size. On slower machines an unfiltered
query against the upper end of that range is the case to watch — see
[KNOWN_ISSUES.md](KNOWN_ISSUES.md).

### Tuning

| Setting | Effect |
|---|---|
| `kb.workers` | Parallel indexing workers. Raise for faster sync on a many-core machine; it costs RAM and disk I/O. |
| `kb.chunk_size` / `kb.chunk_overlap` | Retrieval quality trade-off, and indirectly index size. |
| `kb.max_file_size` | Skip threshold. Lower it to keep large generated files out. |
| `embed.batch_size` | Texts per provider request during indexing. |
| `embed.timeout_seconds` | Bounds one embedding call including retries. |
| `embed.llama_server.idle_timeout` | How long the sidecar lingers. Longer avoids repeated cold starts; 0 keeps it until the process exits. |

### Cold start

The first semantic query after an idle period pays for loading the model. This
is normal, and is why `doctor --probe-timeout` exists. A longer `idle_timeout`
trades resident memory for fewer cold starts.

### Keeping the database tidy

Removing a source deletes its documents, but SQLite does not return the space
to the filesystem automatically:

```bash
sqlite3 ~/.conduit/conduit.db "VACUUM;"
```

Do this when nothing else is using the database.

---

## Prompt injection (SEC-003)

Worth stating plainly in an operations context: Conduit's MCP tools return
**raw chunks of indexed documents directly to the AI client.** That is the
"no LLM in the hot path" design, and it is what makes retrieval fast, private
and predictable.

It also means **an indexed document is a prompt-injection vector.** Instructions
embedded in indexed content arrive at the assistant as tool output and may
influence its behaviour, including its use of other tools. Conduit cannot
meaningfully sanitize this and does not claim to.

Operationally:

- Treat adding a source as a trust decision, like adding a dependency.
- Use separate `--db` files to keep untrusted corpora away from the knowledge
  base an agent has open.
- Keep `policy.forbidden_paths` tight; it is the one automated control here.
- Prefer AI clients that visually attribute tool output separately from user
  instructions.

Full text in [KNOWN_ISSUES.md](KNOWN_ISSUES.md).

---

## Uninstall and v1 teardown

### Removing Conduit 2.0

```bash
conduit uninstall --info        # what's installed
conduit uninstall --dry-run     # preview, change nothing
conduit uninstall               # interactive; keeps data
conduit uninstall --keep-data   # remove binary and PATH entries, keep the index
conduit uninstall --all         # remove data too (prompts unless --force)
conduit uninstall --all --force
conduit uninstall --json
conduit uninstall --prefix /usr/local/bin   # only the install in this directory
```

Data is kept unless you ask for it to go. `--all` is the only thing that
deletes the knowledge base.

Conduit runs no service and no containers, so there is nothing of that kind to
tear down. Tools you may share with other projects are never removed — remove
those yourself (Ollama: see <https://ollama.com/download>; poppler:
`brew uninstall poppler`).

### Removing the Conduit 1.x stack

Installing v2 does **not** remove v1. The old daemon keeps starting at login
and the containers keep holding ports 6333/6334/6379.

```bash
./scripts/remove-v1.sh                      # dry run — the default. Reports only.
./scripts/remove-v1.sh --yes                # remove the v1 stack, keep all data
./scripts/remove-v1.sh --yes --purge-data   # also drop the Qdrant/FalkorDB stores
```

It never deletes your knowledge base, configuration or embedding models —
those belong to v2 as much as they did to v1. Only `--purge-data` removes
anything under the data directory, and even then only the two container storage
directories v2 has no use for.

Also remove the desktop app if it was installed; the published DMGs are
unsupported (SEC-002):

```bash
rm -rf /Applications/Conduit.app
```

---

## See also

| Document | Contents |
|---|---|
| [INSTALL_V2.md](INSTALL_V2.md) | Installation, upgrade, moving from 1.x |
| [USER_GUIDE.md](USER_GUIDE.md) | Day-to-day use |
| [KNOWN_ISSUES.md](KNOWN_ISSUES.md) | Security advisories, current limitations |
| [EMBEDDING_SIDECAR.md](EMBEDDING_SIDECAR.md) | How the sidecar works |
| [../CONTEXT.md](../CONTEXT.md) | Architecture, for developers |
| [../CHANGELOG.md](../CHANGELOG.md) | Removed config keys and commands |
