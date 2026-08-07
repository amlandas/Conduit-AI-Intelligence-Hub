# Conduit — Developer Context (v2)

**Start here if you are working on the code.**

This document describes Conduit 2.0. If something you read elsewhere mentions a
daemon, Qdrant, FalkorDB, Podman or an Electron GUI as a current feature, that
document is stale — see [CHANGELOG.md](CHANGELOG.md) for what was removed and
why.

---

## What Conduit is

A local-first knowledge base that AI clients query over MCP.

It is **one binary**. There is no daemon, no socket, no HTTP API, and no
background service. Every command opens a SQLite file, does its work in
process, and exits. That is the whole architecture, and it is why
`conduit kb search` works on a machine where nothing else is running.

SQLite holds everything: the FTS5 keyword index, the vectors, the chunk and
document metadata, and (optionally) the knowledge graph edges. One file, WAL
mode, one backup.

---

## Build and test

```bash
# Build
CGO_ENABLED=1 go build -tags fts5 -o conduit ./cmd/conduit
# or:
make build

# Test
CGO_ENABLED=1 go test -tags fts5 ./...
# or:
make test

make fmt      # gofmt
make vet      # go vet
make lint     # golangci-lint
make run-mcp  # build and run the KB MCP server over stdio
make help     # list all targets
```

**`CGO_ENABLED=1` and `-tags fts5` are not optional.** A build without them
compiles cleanly, starts cleanly, and then fails every search with
`no such module: fts5`. Any build command you write, script you add, or CI job
you touch must set both.

Requires Go 1.21+ and a C compiler.

---

## Package map

### `cmd/conduit`

The entry point. Thin — it hands off to `internal/cli`.

### `internal/cli`

Conduit's command surface (Cobra). Every command is a thin frontend over a
library call.

Two things to know before editing:

- **Output is a contract.** Human-readable output and every `--json` shape are
  consumed by scripts and by the frozen desktop GUI. Treat them as API.
- **Removed commands are documented, not deleted.** `removed.go` registers the
  retired v1 surface as hidden stubs that name what went away and what to use
  instead. Deleting them outright would give the user cobra's "unknown command"
  and no idea why a documented command vanished. They are hidden so `--help`
  shows only what Conduit can actually do.

Files map to command groups: `kb.go`, `mcp.go`, `model.go`, `kag.go`,
`doctor.go`, `setup.go`, `status.go`, `uninstall.go`, `backup.go`,
`config_cmd.go`, `ollama.go`, `root.go`, `removed.go`.

### `internal/kbservice`

**The in-process knowledge base library.** Everything the CLI, the MCP server,
and any future frontend can do to a knowledge base is a method here. No HTTP
layer, no socket, no daemon.

- Concurrency is SQLite's job. The database is opened in WAL mode with a busy
  timeout, so a `conduit kb sync` in one terminal and a `kb_search` arriving
  over MCP in another serialise at the database rather than at a daemon.
  Writers serialise (SQLite has one writer); readers never block.
- The map-shaped results returned by `Search` are a compatibility contract,
  reproduced exactly as the removed HTTP daemon produced them.
- `pathsafety.go` enforces `policy.forbidden_paths` / `policy.warn_paths` on
  `kb add`, after symlink resolution.

### `internal/kb`

The retrieval engine. The largest package and the one with the most invariants.

- `hybrid_search.go` — RRF fusion of FTS5 and vector results. RRF is the *only*
  fusion method; `HybridSearchOptions` has 4 fields.
- `vecstore_sqlite.go` — the vector store. Raw little-endian float32 BLOBs plus
  a precomputed L2 norm in `kb_vectors`, searched by an exact brute-force
  cosine scan in pure Go. The scan is two-phase: top-K over
  `(chunk_id, norm, embedding)` alone, then a join back to `kb_chunks` and
  `kb_documents` for the survivors. Exactness is the point — filters are
  ordinary SQL predicates evaluated *before* the distance, so a selective
  source filter can never silently cost recall the way post-filtering an
  approximate index would.
- `embedding_stamp.go` — the identity of whatever built the stored vectors,
  in one row of `kb_embedding_stamp`, written in the same transaction as the
  first vectors it describes. Its job is to make an embedding-model change
  *visible*: the width check in `vecstore_sqlite.go` catches only a swap that
  changes the width, and a same-width swap silently mixes two incomparable
  vector spaces (#107). Model names are canonicalised through the
  `internal/embed` registry before comparison — the same weights answer to
  several names — and a difference involving a model the registry does not know
  warns without disabling anything. Never make that comparison a string
  compare: it would report a model change on every `embed.provider` switch and
  turn semantic search off on a healthy knowledge base.
- `chunker.go`, `indexer.go`, `searcher.go`, `semantic_search.go`,
  `result_processor.go`, `content_cleaner.go` — the ingest and retrieval path.
- `graph_store_sqlite.go`, `graph_schema.go`, `kag_search.go`,
  `pattern_extractor.go`, `entity_extractor.go` — the opt-in knowledge graph.
- `golden_retrieval_test.go`, `known_bugs_test.go`, `retrieval_test_suite.go`,
  `fallback_test.go`, `fusion_test.go` — the golden harness. **A ranking change
  must show up here as a deliberate diff.** `known_bugs_test.go` carries
  regression tests for issues #69–#77 by number; do not weaken them.

### `internal/embed`

The embedding provider layer. Three implementations behind one interface:

- `llamaserver.go` / `sidecar.go` — the default. A `llama-server` process bound
  to `127.0.0.1` on a port Conduit picks, shared as a singleton across the
  process, stopped after an idle timeout.
- `ollama.go` — an Ollama daemon the user already runs.
- provider `none` — no model, no port. Lexical-only search. **A first-class
  mode, not a degraded one.** Code must not treat it as an error path.

`registry.go` pins each model to an exact HuggingFace repo, file and SHA-256,
plus its pooling mode and required instruction prefixes. Pooling and prefixes
are not cosmetic: the wrong mode degrades retrieval silently, with no error and
no obvious signal in the vectors. `download.go` verifies the hash before the
file is renamed into place; there is no flag to install an unverified model.

### `internal/mcpserver`

The Knowledge Base MCP server, built on the official
`github.com/modelcontextprotocol/go-sdk`. Negotiates spec revision 2026-07-28
and stays compatible with the older legacy `initialize` handshake.

**The stdio transport owns `os.Stdout`.** Nothing in this package, or anything
it calls, may write to stdout — doing so corrupts the protocol frame stream.
All diagnostics go to stderr via the zerolog global logger.

`tools.go` registers the seven tools. Its descriptions were carried over
verbatim from the hand-rolled server and are a deliberate asset: they teach AI
clients how to query well, and client prompts are tuned against the exact
wording. A test byte-freezes the schemas. Do not "improve" them casually.

### `internal/setup`

The small amount of machine preparation Conduit still needs, and its removal.
Optional document extraction tools, MCP client configuration, shell PATH
handling, and uninstall.

Replaces the old `internal/installer`, which existed to orchestrate a container
runtime, Qdrant and FalkorDB images, an Ollama service and a launchd/systemd
unit — none of which exist now. Nothing here installs a package manager or
edits system state without being asked; bootstrapping a bare machine is the
install script's job.

`safety.go` and its tests carry the adversarially-reviewed teardown guards:
path checks are identity-based, not string-prefix based.

### `internal/config`

**The single source of truth for configuration.** The `Config` struct *is* the
schema; anything not reachable from it is not a Conduit setting.

Precedence, highest wins:

1. command-line flags (`--db`, `--data-dir`, `--log-level`)
2. environment (`CONDUIT_*`, nested keys use `_` for `.`)
3. configuration file (`./conduit.yaml`, then `~/.conduit`, then `/etc/conduit`)
4. compiled defaults (`DefaultConfig`)

Exactly one file is read: the first found, most specific first, so a project
directory can override a user's settings. `Load` reports unknown keys rather
than ignoring them, so a stale key from a removed subsystem is visible.

### `internal/store`

SQLite open and migrate. Migrations are embedded via `go:embed migrations/*.sql`.

Migration 006 adds `kb_embedding_stamp`. Two tables — the vector tables and the
stamp — are created by *both* the migration chain here and
`SQLiteVectorIndex.ensureSchema` in `internal/kb`, because a database can reach
the vector index before the chain has run and `internal/store` cannot import
`internal/kb` without a cycle. `TestSchemaParityMigrationVsEnsureSchema`
compares what SQLite actually stored from each path; WP-2.3 found the two had
drifted once already.

### `internal/querylog`

The local-only query-*shape* log. Its privacy contract is enforced by
construction: the `Record` struct has no field that can hold query text, entity
names, titles, paths, snippets or results — not redacted at write time,
structurally absent. A redaction test guards against a field being added.
Nothing here opens a socket; the file is 0600 under the data directory and
Conduit never reads it back.

### `internal/observability`

Logging and metrics helpers (zerolog).

### `pkg/models` and `tests/`

Shared types, plus `tests/integration` and `tests/scripts`.

---

## Design principles

**Library-first.** Business logic lives in `internal/kbservice` and is
importable and testable without a process boundary. The CLI is a shell over it;
so is the MCP server. If you find yourself putting logic in a Cobra `RunE`,
move it down. (This replaces v1's "GUI must call the CLI" rule, which existed
because the daemon was the real source of truth. There is no daemon now — the
library is.)

**No LLM in the hot path.** Retrieval returns raw chunks and lets the connected
AI client synthesise. This is what makes search fast, private and predictable.
It is also why an indexed document is a prompt-injection vector — see SEC-003
in [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md). Do not add a summarisation or
rerank-by-LLM step to the query path without a very good reason and an explicit
decision.

**RRF-only fusion.** One fusion algorithm, four options, deterministic results.
v1 had 13 option fields, most of which the engine either never read or
overwrote from a preset before use — knobs that appeared to work and did
nothing. If you add a retrieval option, prove with a test that changing it
changes an observable result.

**Honest degradation.** Every subsystem states its actual state rather than
faking success. `embed.provider: none` is a supported configuration, not a
failure. `kag_query` distinguishes "graph disabled" from "graph empty" from "no
match". `kb sync` exits 2 on partial success rather than reporting success.
`doctor` exits non-zero when a check genuinely failed. **Never report success
for work that did not happen** — that single failure mode is what made the v1
documentation untrustworthy.

**Opt-in for anything expensive or unproven.** `kb.kag.enabled` is `false` by
default and no graph tables exist until it is true. `conduit setup` does not
download a few-hundred-megabyte model unless asked.

**Determinism.** The same query against the same index returns the same
ranking. Tie-breaks are explicit.

---

## Where the living plan is

Work-package planning, benchmarks and decision records live in `.eng-lead-kb/`:

- `BENCH-WP-2.1.md` — the vector-scan benchmark that justified brute-force
  cosine over an approximate index
- `BAKEOFF-WP-2.2.md` — the embedding model comparison behind the registry
- `MCP-PORT-NOTES.md` — the SDK port notes

That directory belongs to the engineering lead; treat it as read-only
reference.

---

## Documentation map

| Document | Contents |
|---|---|
| [README.md](README.md) | What Conduit is, quick start, security posture |
| [CHANGELOG.md](CHANGELOG.md) | Full v1 → v2 lineage, BREAKING CHANGES |
| [CLAUDE.md](CLAUDE.md) | Rules for AI coding agents on this repo |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development workflow |
| [docs/INSTALL_V2.md](docs/INSTALL_V2.md) | Installation and upgrade |
| [docs/USER_GUIDE.md](docs/USER_GUIDE.md) | Day-to-day use |
| [docs/ADMIN_GUIDE.md](docs/ADMIN_GUIDE.md) | Configuration schema, diagnostics, backup |
| [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) | Security advisories, current limitations |
| [docs/EMBEDDING_SIDECAR.md](docs/EMBEDDING_SIDECAR.md) | How the sidecar works |

Documents under `docs/HLD/` and several older design notes describe v1 and
carry a historical banner. They are kept for lineage, not as guidance.

---

**Last updated**: August 2026 (Conduit 2.0.0-beta)
