# Conduit

**A private knowledge base your AI tools can search, over MCP.**

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Version](https://img.shields.io/badge/version-2.0.0--beta-orange.svg)](CHANGELOG.md)

---

Conduit indexes folders of your documents and exposes them to AI clients
(Claude Code, Cursor, VS Code, or anything that speaks the
[Model Context Protocol](https://modelcontextprotocol.io)) as a set of search
tools. When you ask your assistant a question, it can look the answer up in
your own files instead of guessing.

It is **one binary**. There is no daemon, no background service, no containers,
and nothing that starts at login. Every command opens a SQLite file, does its
work, and exits. Your documents, the index, and the embeddings never leave the
machine.

---

## Quick start

### 1. Install

From the latest release (no Go toolchain needed — downloads the prebuilt
binary and verifies its SHA-256 checksum):

```bash
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub
cd Conduit-AI-Intelligence-Hub
./scripts/install.sh
```

Or build from source (requires Go 1.21+ and a C compiler):

```bash
./scripts/install.sh --from-source
```

Either way the script installs the binary to `~/.local/bin`, creates the data
directory, and registers the MCP server with Claude Code. Re-running it is safe
and is the supported way to upgrade.

If `~/.local/bin` is not already on your `PATH`, it also appends a two-line
block to your shell's startup file, marked with a `# Conduit` comment so
`uninstall.sh` can remove exactly that block later. The installer names the file
it wrote to; open a new terminal or source that file.

Full options and troubleshooting are in
[docs/INSTALL_V2.md](docs/INSTALL_V2.md).

**Requirements**

| | |
|---|---|
| Platforms | macOS arm64 (Apple Silicon), Linux x86_64 |
| Building from source | Go 1.21+ and a C compiler |
| Disk | ~13 MB for the binary; ~262 MB more for the default embedding model |
| Network | Once to install, once more to fetch the model |

`cgo` is not optional. The knowledge base is SQLite with the FTS5 extension; a
`CGO_ENABLED=0` build compiles and then fails every search with
`no such module: fts5`. `install.sh` sets `CGO_ENABLED=1` and `-tags fts5`. If
you build by hand, so must you.

**How long this takes.** Installing from a release is a ~13 MB download and a
checksum check — seconds on a decent link. `--from-source` compiles instead, so
it is a minute or two plus the Go toolchain you already have. The model download
is a separate 262 MB, and depends entirely on your connection — budget more than
you think on a slow link. Indexing time scales with your corpus.

### 2. Get the embedding model (optional but recommended)

```bash
conduit model download
```

This fetches `nomic-embed-text-v1.5` and verifies its SHA-256 against a pinned
registry entry. A mismatch deletes the download and fails; there is no flag to
install an unverified model.

Skipping this is a supported choice, not a broken install — search falls back
to FTS5 keyword matching. Set `embed.provider: none` in the config to make that
the intended mode rather than a degraded one.

### 3. Add documents and index them

```bash
conduit kb add ./docs --name "Project Docs"
conduit kb sync
```

### 4. Check it works

```bash
conduit kb search "how does authentication work"
conduit doctor
```

### 5. Point an AI client at it

```bash
conduit setup                      # or: conduit mcp configure
conduit mcp configure --client cursor
```

`conduit setup` creates the data directory, installs optional document
extraction tools, optionally downloads the model (`--model`), configures your
AI client, and reports what still needs attention. Supported clients:
`claude-code` (default), `cursor`, `vscode`.

If you prefer to wire it up yourself, the server runs over stdio:

```json
{
  "mcpServers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}
```

---

## What it does

**Hybrid search.** Every query runs against both a SQLite FTS5 keyword index
and a vector index, and the two result lists are fused with Reciprocal Rank
Fusion (RRF). Keyword search finds the exact identifier you named; vector
search finds the paragraph that means the same thing in different words. RRF is
the only fusion method — the results are deterministic, and the same query
against the same index returns the same ranking every time.

Three modes are available explicitly:

```bash
conduit kb search "authentication"                  # hybrid (default)
conduit kb search "authentication" --semantic       # vectors only
conduit kb search "class AuthProvider" --fts5       # keywords only
```

Retrieval tuning is a single knob, `--recall high|balanced|precise`, plus
`--min-score` for pure semantic search.

**Everything in one SQLite file.** Keyword index, vectors, chunk metadata, and
(optionally) graph edges all live in the same database. Vectors are stored as
raw little-endian float32 BLOBs with a precomputed L2 norm, and similarity is
an exact brute-force cosine scan in pure Go. At the target corpus size that
costs tens of milliseconds and buys exactness: filters are ordinary SQL
predicates evaluated before the distance, so a selective source filter can
never silently cost you recall the way post-filtering an approximate index can.

Backup is `cp ~/.conduit/conduit.db somewhere`. WAL mode means concurrent
readers are fine.

**Embeddings, three ways.** `embed.provider` accepts:

- `llama-server` (default) — Conduit starts a `llama-server` sidecar bound to
  loopback, shared as a singleton across the process, and stops it after an
  idle timeout.
- `ollama` — use an Ollama daemon you already run.
- `none` — no model, no port, lexical-only search. Fully supported.

**Workspace isolation.** The global `--db` flag points at a different knowledge
base file. One binary, many independent knowledge bases:

```bash
conduit --db ~/work.db kb add ./work-docs
conduit --db ~/personal.db kb search "recipes"
```

A project can also carry its own `conduit.yaml`; the working directory is
searched before `~/.conduit` and `/etc/conduit`.

**Optional knowledge graph (KAG).** Off by default (`kb.kag.enabled: false`) —
no graph tables exist in the database until you turn it on. The default
extraction provider is `pattern`: no LLM, no network. `kag_query` reports its
state honestly rather than pretending an empty graph is a no-match.

### The 7 MCP tools

| Tool | What it does |
|---|---|
| `kb_search` | Hybrid search (FTS5 + semantic, RRF-fused). The default. |
| `kb_lexical_search` | Pure FTS5/BM25 keyword search. No vectors, no fusion, no diversity filtering — the grep of the knowledge base, for iterative refinement loops. |
| `kb_search_with_context` | Search returning merged, boilerplate-filtered, citation-ready passages. Best for RAG. |
| `kb_list_sources` | List sources with IDs, paths, document counts, sync status. |
| `kb_get_document` | Fetch a full document by `document_id` (printed on every search hit) or by path. |
| `kb_stats` | Source, document and chunk counts; search capability status. |
| `kag_query` | Query the knowledge graph for entities and relationships. |

The server is built on the official MCP Go SDK and negotiates spec revision
2026-07-28, falling back to legacy revisions for older clients.

---

## What changed in v2

Conduit 1.x described a system considerably larger than the one that actually
ran. Version 2 is a deliberate subtraction: everything that could not be
verified end to end was either fixed or removed.

| | Conduit 1.x | Conduit 2.0 |
|---|---|---|
| Processes | `conduit` CLI + `conduit-daemon` background service | one binary, runs and exits |
| Install-at-login | launchd agent / systemd unit | nothing |
| Containers | Podman or Docker required | none |
| Vector store | Qdrant container (ports 6333/6334) | vectors in the SQLite file |
| Graph store | FalkorDB container (port 6379) | SQLite edge tables, opt-in |
| Embeddings | Ollama required | llama-server sidecar, Ollama, or none |
| Desktop GUI | Electron app | retired ([frozen](apps/conduit-desktop/README.md)) |
| MCP server | hand-rolled | official Go SDK, spec 2026-07-28 |
| MCP tools | 6 | 7 (`kb_lexical_search` is new) |
| Retrieval tuning | 4 config keys the engine ignored or overwrote | one `recall_mode` preset that works |
| Path safety | forbidden-path lists existed, nothing read them | enforced on `conduit kb add` |
| Third-party connectors | documented, never actually ran containers | removed |

The moving parts that v1 spent most of its install script setting up are gone,
which is why v2's install script is short.

**Breaking:** there is no data migration. A v1 knowledge base cannot be read by
v2 — re-add your sources and re-index. Removed commands still exist as hidden
stubs that explain what happened and what to use instead, rather than giving
you a bare "unknown command".

See [CHANGELOG.md](CHANGELOG.md) for the full per-area lineage with PR
references.

---

## Security posture

**Local by construction.** Documents, index, vectors and embeddings stay on the
machine. Conduit opens no listening socket for its own API — the MCP server
talks over stdio to the AI client that launched it. The only network activity
is downloading the binary and the model.

**Loopback-only sidecar.** When `embed.provider` is `llama-server`, Conduit
starts the sidecar bound to `127.0.0.1` on a port it picks, and shuts it down
after an idle timeout. Nothing is published to your LAN. (Contrast v1, where
the Qdrant and FalkorDB containers bound `0.0.0.0` with no authentication — see
SEC-001 in [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md).)

**Verified model downloads.** Every model in the registry is pinned to an exact
file in an exact HuggingFace repository with an exact SHA-256. Downloads are
verified and discarded on mismatch. There is no override flag.

**Path safety.** `policy.forbidden_paths` is enforced on `conduit kb add`,
after resolving symlinks. The defaults refuse `/`, `/etc`, `/var`, `/usr`,
`~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.config/gcloud` and `~/.kube`.
`policy.warn_paths` (`~/.config`, `~/Documents`, `~/Desktop`) warns without
blocking. Both lists are yours to edit.

**Local-only query-shape log.** `telemetry.local_query_log` (default `true`)
appends one line per query to `<data_dir>/query-shape.jsonl`, recording token
count, whether the query looks like it names an entity, and requested traversal
depth. It cannot contain your query text — the record type has no field that
could hold it, which a test enforces. The file is mode 0600, Conduit never
reads it back, and nothing uploads it anywhere. Set the key to `false` and no
file is created.

### Prompt injection: read this one

By design ("no LLM in the hot path"), Conduit's MCP tools return **raw chunks
of your indexed documents directly to the AI client**. That is what makes it
fast, private, and predictable — and it means **an indexed document is a
prompt-injection vector.**

If you index untrusted content — third-party PDFs, scraped web pages, shared
drives, a dependency's docs — any instructions embedded in that content arrive
at your AI assistant as tool output and may influence what it does, including
what it does with your other tools. Conduit cannot meaningfully sanitize this,
and does not claim to.

Practical guidance:

- Index content you trust. Treat a new source like installing a dependency.
- Use `--db` to keep untrusted corpora in a separate knowledge base from the
  one your coding agent has open.
- Read KB search results in agent transcripts with the same skepticism you
  apply to fetched web pages.
- Prefer AI clients that visually attribute tool output separately from your
  own instructions.

This is tracked as SEC-003 in [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md).

---

## Conduit 1.x is frozen

Version 1 is no longer developed and receives no fixes.

**The published desktop DMGs are unsupported and should not be run.** They are
unsigned, ship an end-of-life Electron/Chromium with unpatched CVEs, and
contain an IPC handler that lets the renderer execute arbitrary shell commands
(SEC-002). Delete them:

```bash
rm -rf /Applications/Conduit.app
```

Installing v2 does **not** remove the v1 stack. The old daemon keeps starting at
login and the containers keep holding ports 6333/6334/6379. Tear it down:

```bash
./scripts/remove-v1.sh          # dry run — reports, changes nothing
./scripts/remove-v1.sh --yes    # remove the v1 stack, keep all data
```

It never deletes your knowledge base, configuration, or models. To remove
Conduit entirely, data included, use `conduit uninstall --all`.

See SEC-001, SEC-002 and SEC-003 in
[docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md).

---

## Documentation

| Document | For |
|---|---|
| [docs/INSTALL_V2.md](docs/INSTALL_V2.md) | Installation, upgrade, moving from 1.x |
| [docs/USER_GUIDE.md](docs/USER_GUIDE.md) | Day-to-day use: sources, search, MCP clients |
| [docs/ADMIN_GUIDE.md](docs/ADMIN_GUIDE.md) | Configuration schema, models, diagnostics, backup |
| [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) | Security advisories and current limitations |
| [docs/EMBEDDING_SIDECAR.md](docs/EMBEDDING_SIDECAR.md) | How the llama-server sidecar works |
| [CONTEXT.md](CONTEXT.md) | Start here if you are working on the code |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Development workflow |
| [CHANGELOG.md](CHANGELOG.md) | Full v1 → v2 lineage |

---

## Uninstall

```bash
conduit uninstall --dry-run     # preview
conduit uninstall               # interactive; keeps data
conduit uninstall --all         # remove data too (prompts)
```

Conduit runs no service and no containers, so there is nothing of that kind to
tear down. Tools you may share with other projects (Ollama, poppler) are never
removed.

---

## License

MIT — see [LICENSE](LICENSE).
