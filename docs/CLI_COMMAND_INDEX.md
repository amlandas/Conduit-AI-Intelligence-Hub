# Conduit CLI Command Index

**Version**: 2.0.0-beta
**Last Updated**: August 2026

Every command and flag below is taken from `conduit <command> --help` on the
2.0 binary. `--help` on any command is the authoritative source.

---

## Contents

- [Global flags](#global-flags)
- [Quick reference](#quick-reference)
- [Setup and diagnostics](#setup-and-diagnostics)
- [Knowledge base commands](#knowledge-base-commands)
- [Knowledge graph commands](#knowledge-graph-commands)
- [MCP commands](#mcp-commands)
- [Model commands](#model-commands)
- [Ollama commands](#ollama-commands)
- [Configuration commands](#configuration-commands)
- [Maintenance commands](#maintenance-commands)
- [Removed commands](#removed-commands)

---

## Global flags

Available on every command:

| Flag | Description |
|---|---|
| `--db <path>` | Path to the knowledge base SQLite file (default `<data-dir>/conduit.db`) |
| `--data-dir <path>` | Conduit data directory (default `~/.conduit`) |
| `--log-level <level>` | `debug`, `info`, `warn`, `error` |
| `-h`, `--help` | Help for the command |
| `-v`, `--version` | Version (root command only) |

`--db` is the workspace-isolation seam: one binary, many independent knowledge
bases.

---

## Quick reference

```bash
# Setup
conduit setup                              # prepare machine, configure AI client
conduit doctor                             # diagnose, with remedies
conduit status                             # knowledge base state

# Documents
conduit kb add ./docs --name "My Docs"     # add a folder
conduit kb list                            # list sources
conduit kb sync                            # index new and changed documents
conduit kb search "query"                  # search
conduit kb stats                           # statistics
conduit kb remove "My Docs"                # remove a source

# AI clients
conduit mcp configure                      # wire up Claude Code
conduit mcp kb                             # run the MCP server over stdio
conduit mcp status                         # configuration and capabilities

# Model
conduit model list                         # pinned models
conduit model download                     # fetch and verify

# Housekeeping
conduit config                             # show configuration
conduit backup                             # archive the data directory
conduit uninstall --dry-run                # preview removal
```

---

## Setup and diagnostics

### `conduit setup`

Initialise Conduit: create the data directory and knowledge base file, install
optional document extraction tools, optionally download the embedding model,
configure the MCP server in an AI client, and report what still needs
attention.

There is no service to install and no containers to pull.

| Flag | Description |
|---|---|
| `-c`, `--client <name>` | AI client to configure: `claude-code` (default), `cursor`, `vscode` |
| `--model` | Download and verify the embedding model (a few hundred MB) |
| `--skip-tools` | Skip document extraction tool installation |

The model is **not** downloaded unless `--model` is passed — setup should not
start a large transfer on a metered connection without being asked.

```bash
conduit setup
conduit setup --model
conduit setup --skip-tools
conduit setup --client cursor
```

### `conduit doctor`

Check that Conduit can do its job, and say what to do when it cannot.

Checks: configuration loads and contains no keys Conduit no longer understands;
the knowledge base file is present, readable and writable; SQLite FTS5 is
compiled in and initialised; the embedding provider is configured and reachable
(skipped when `embed.provider` is `none`); the vector index, and whether it is
populated; at least one AI client has the MCP server configured.

| Flag | Description |
|---|---|
| `--json` | Output as JSON |
| `--probe-timeout <seconds>` | Seconds to wait for the embedding provider (default 15) |

| Exit code | Meaning |
|---|---|
| 0 | Everything needed works (warnings may still print) |
| 1 | At least one check failed |

```bash
conduit doctor
conduit doctor --json
conduit doctor --probe-timeout 30    # allow a cold embedding model to load
```

### `conduit status`

Show the state of the knowledge base: the file, what it contains, and how
retrieval is configured.

Conduit has no background service, so there is nothing to start and nothing
that can be "down". For diagnosis with remedies, use `doctor`.

| Flag | Description |
|---|---|
| `--json` | Output as JSON |

### `conduit version`

Show version information.

---

## Knowledge base commands

### `conduit kb add <path>`

Add a folder to the knowledge base for document indexing.

| Flag | Description |
|---|---|
| `--name <name>` | Display name for the source |
| `--patterns <list>` | File patterns to index, comma-separated (e.g. `*.md,*.txt`) |
| `--excludes <list>` | Directories to exclude, comma-separated (e.g. `node_modules,dist`) |
| `--sync <mode>` | Sync mode: `manual` (default) or `auto` |
| `--json` | Output as JSON |

```bash
conduit kb add ./docs --name "Project Docs"
conduit kb add /path/to/notes --patterns "*.md,*.txt"
conduit kb add ./src --excludes "node_modules,dist"
```

**Default patterns**: `*.md`, `*.txt`, `*.rst`; `*.go`, `*.py`, `*.js`, `*.ts`,
`*.java`, `*.rs`, `*.rb`, `*.c`, `*.cpp`, `*.h`, `*.hpp`, `*.cs`, `*.swift`,
`*.kt`, `*.sh`, `*.bash`, `*.zsh`, `*.fish`, `*.ps1`, `*.bat`, `*.cmd`;
`*.json`, `*.yaml`, `*.yml`, `*.xml`, `*.jsonld`, `*.toml`, `*.ini`, `*.cfg`;
`*.csv`, `*.tsv`; `*.pdf`, `*.doc`, `*.docx`, `*.odt`, `*.rtf`.

**Default excludes**: `node_modules`, `.git`, `.svn`, `.hg`, `__pycache__`,
`.pytest_cache`, `vendor`, `dist`, `build`, `target`, `.DS_Store`,
`Thumbs.db`.

**Path safety**: paths inside `policy.forbidden_paths` are refused, after
symlink resolution. Paths inside `policy.warn_paths` warn but proceed. See the
[Admin Guide](ADMIN_GUIDE.md#path-safety-policy).

### `conduit kb list`

List knowledge base sources. Alias: `conduit kb ls`.

| Flag | Description |
|---|---|
| `--json` | Output as JSON |

### `conduit kb remove <name-or-id>`

Remove a knowledge base source and all its indexed documents.

| Flag | Description |
|---|---|
| `-f`, `--force` | Skip confirmation |
| `--json` | Output as JSON |

```bash
conduit kb remove "User Files"
conduit kb remove test --force
```

### `conduit kb sync [source-id]`

Synchronize sources to index new and updated documents. With no source ID, all
sources are synced.

| Flag | Description |
|---|---|
| `--rebuild-vectors` | Force rebuild of the vector index for all documents |

| Exit code | Meaning |
|---|---|
| 0 | Full success (FTS + semantic indexing) |
| 1 | Error (sync failed) |
| 2 | Partial success (FTS only, semantic indexing failed) |

```bash
conduit kb sync
conduit kb sync abc123-def456
conduit kb sync --rebuild-vectors
```

### `conduit kb search <query>`

Search using hybrid, semantic, or keyword search.

By default, hybrid search uses RRF (Reciprocal Rank Fusion) to combine semantic
and lexical results. Hybrid mode detects quoted phrases (prioritising lexical
exact matching), proper nouns (boosting exact matches), and natural language
(balancing the two).

Results are processed by default: chunks from the same document are merged and
boilerplate is filtered. `--raw` returns unprocessed results.

| Flag | Description |
|---|---|
| `--semantic` | Force semantic search (requires an embedding provider) |
| `--fts5` | Force FTS5 keyword search |
| `--recall <mode>` | Precision/recall preset: `high`, `balanced` (default), `precise` |
| `--min-score <float>` | Minimum similarity threshold for `--semantic` (0.0–1.0) |
| `--limit <n>` | Maximum results (default 10) |
| `--context <n>` | Number of adjacent chunks to include |
| `--raw` | Return raw chunks without processing |
| `--json` | Output as JSON |

```bash
conduit kb search "how does authentication work"    # hybrid RRF (default)
conduit kb search "Oak Ridge laboratories"          # auto-detects proper noun
conduit kb search "authentication" --semantic
conduit kb search "class AuthProvider" --fts5
conduit kb search "ASL-3 safeguards" --recall high  # widen recall
conduit kb search "authentication" --recall precise # fewer, more distinct
conduit kb search "AI safety" --semantic --min-score 0.0
```

### `conduit kb stats`

Show knowledge base statistics.

### `conduit kb migrate`

Migrate existing FTS5-indexed documents to the vector search index. Required to
enable semantic search for documents indexed before semantic search was
enabled; new documents are indexed in both automatically.

Requires an embedding provider, and fails when `embed.provider` is `none`.

---

## Knowledge graph commands

The knowledge graph is **opt-in**. `kb.kag.enabled` defaults to `false`, and no
graph tables exist in the database until it is `true`:

```bash
conduit config set kb.kag.enabled true
```

The default extraction provider is `pattern` — no LLM, no network.

### `conduit kb kag-sync`

Extract entities and relationships from indexed documents into the graph.

| Flag | Description |
|---|---|
| `-f`, `--force` | Re-extract from all chunks, even previously processed |
| `--provider <name>` | LLM provider: `ollama`, `openai`, `anthropic` |
| `--advanced` | Show advanced options and verbose output |

### `conduit kb kag-query <query>`

Query the knowledge graph for entities and relationships.

| Flag | Description |
|---|---|
| `--max-hops <n>` | Maximum relationship hops to traverse (default 2) |
| `--format <fmt>` | Output format: `text` (default) or `json` |
| `--hybrid` | Enable hybrid search (lexical + semantic) |
| `--ollama-host <url>` | Ollama API endpoint (default `http://localhost:11434`) |

`--hybrid` requires Ollama and entities vectorized via `kag-vectorize`.

```bash
conduit kb kag-query "threat models"
conduit kb kag-query "authentication" --max-hops 3
conduit kb kag-query "API security" --format json
```

### `conduit kb kag-status`

Extraction status dashboard: progress bar, entity and relation statistics,
error breakdown by type, system resource usage, Ollama model status.

### `conduit kb kag-retry`

Retry failed KAG extractions.

| Flag | Description |
|---|---|
| `--chunk-id <id>` | Specific chunk IDs to retry (repeatable) |
| `--max-retries <n>` | Maximum retry attempts (default 2, max 5) |
| `--dry-run` | Preview without executing |

### `conduit kb kag-dedupe`

Merge entities that are semantically the same (e.g. "Threat Model" and "threat
model"), keeping the highest confidence and best description.

| Flag | Description |
|---|---|
| `--dry-run` | Preview without making changes |

### `conduit kb kag-vectorize`

Generate vector embeddings for KAG entities, enabling `kag-query --hybrid`.

| Flag | Description |
|---|---|
| `--batch-size <n>` | Entities per batch (default 20) |
| `--ollama-host <url>` | Ollama API endpoint (default `http://localhost:11434`) |

---

## MCP commands

### `conduit mcp kb`

Run the Knowledge Base MCP server over stdio. This is the command an AI client
launches; you rarely run it by hand.

Client configuration:

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

The server registers seven tools: `kb_search`, `kb_lexical_search`,
`kb_search_with_context`, `kb_list_sources`, `kb_get_document`, `kb_stats`,
`kag_query`.

### `conduit mcp configure`

Auto-configure the Conduit MCP KB server in AI clients.

| Flag | Description |
|---|---|
| `-c`, `--client <name>` | `claude-code` (default), `cursor`, `vscode` |
| `-f`, `--force` | Overwrite existing configuration |
| `--check` | Check whether it is already configured |

| Client | Config file |
|---|---|
| `claude-code` | `~/.claude.json` |
| `cursor` | `.cursor/settings/extensions.json` |
| `vscode` | `.vscode/settings.json` |

### `conduit mcp status`

Show MCP configuration status across clients, search capabilities, vector index
and Ollama connectivity, and knowledge base statistics.

| Flag | Description |
|---|---|
| `--json` | Output as JSON |

### `conduit mcp logs`

Show logs from MCP server operations.

| Flag | Description |
|---|---|
| `-f`, `--follow` | Follow log output |
| `--tail <n>` | Number of lines to show (default 50) |

The MCP KB server runs synchronously when invoked by an AI client. For live
server diagnostics, set `mcp.kb.logging.to_stderr: true` — stderr is visible in
the AI client's own server log.

---

## Model commands

Models are **pinned**: each is tied to an exact file in an exact HuggingFace
repository with an exact SHA-256. Downloads are verified against that hash and
discarded if they do not match, so a corrupted or substituted file can never be
installed.

Without a model, Conduit still works: search falls back to keyword matching
(FTS5). Set `embed.provider` to `none` to make that the intended behaviour
rather than a degraded one.

### `conduit model list`

List the pinned embedding models, marking the configured one and showing which
are downloaded.

| Model | Dimensions | Context | Quant | Size |
|---|---|---|---|---|
| `nomic-embed-text-v1.5` (default) | 768 | 2048 | F16 | 261.6 MB |
| `mxbai-embed-large-v1` | 1024 | 512 | F16 | 638.6 MB |
| `qwen3-embedding-0.6b` | 1024 | 32768 | Q8_0 | 609.5 MB |

### `conduit model download [model-id]`

Fetch a pinned model into the data directory. Downloaded to a temporary name
and renamed into place only once its SHA-256 matches. **There is no flag to
install an unverified model.**

Re-running is safe: a model already present and correct is left alone, which is
why install scripts can call it unconditionally. With no argument, the
configured model is used, or the default if none is set.

| Flag | Description |
|---|---|
| `--force` | Re-download even if a valid copy exists (the existing file is kept until the new one verifies) |
| `--timeout <duration>` | Abort the download after this long (e.g. `30m`) |
| `--json` | Output as JSON |

### `conduit model verify [model-id]`

Re-hash a local model file and compare it to the registry pin. Reads the whole
file. Answers one question: is the GGUF on this machine the exact artifact
Conduit expects, or has it been truncated, corrupted or replaced.

| Flag | Description |
|---|---|
| `--json` | Output as JSON |

### `conduit model path [model-id]`

Print the local path of a model artifact.

---

## Ollama commands

Optional. Used when `embed.provider` is `ollama`, or for LLM-based KAG
extraction.

| Command | Description |
|---|---|
| `conduit ollama status` | Ollama status and loaded models |
| `conduit ollama models` | List available Ollama models |
| `conduit ollama pull <model>` | Pull an Ollama model |
| `conduit ollama warmup` | Preload models into memory for faster inference |

---

## Configuration commands

### `conduit config`

Display the current configuration, loaded from `~/.conduit/conduit.yaml`,
`/etc/conduit/conduit.yaml`, and `CONDUIT_*` environment variables.

| Flag | Description |
|---|---|
| `-a`, `--all` | Show all configuration options |

Unrecognised keys in the file are reported by name rather than silently
ignored — usually leftovers from Conduit 1.x.

### `conduit config get <key>`

Get a configuration value. Keys use dot notation.

```bash
conduit config get embed.provider
conduit config get kb.rag.default_limit
```

### `conduit config set <key> <value>`

Set a configuration value. Values are stored in `~/.conduit/conduit.yaml`.

```bash
conduit config set embed.provider none
conduit config set kb.rag.default_limit 20
conduit config set kb.kag.enabled true
```

### `conduit config unset <key>`

Remove a configuration value, restoring the default.

Full schema: [ADMIN_GUIDE.md](ADMIN_GUIDE.md#configuration-reference).

---

## Maintenance commands

### `conduit backup`

Create a compressed `tar.gz` of the data directory: database, configuration and
knowledge base data.

| Flag | Description |
|---|---|
| `-o`, `--output <path>` | Output path for the backup file |

The knowledge base is a single SQLite file, so `cp ~/.conduit/conduit.db ...`
is also a complete backup.

### `conduit uninstall`

Remove the Conduit binary, its MCP client entries, its PATH lines, and
optionally its data.

| Flag | Description |
|---|---|
| `--keep-data` | Remove binaries and PATH entries, keep data for reinstall |
| `--all` | Remove everything including the data directory |
| `--force` | Skip all confirmations |
| `--dry-run` | Show what would be removed without removing |
| `--info` | Show installation status without uninstalling |
| `--json` | Output results as JSON |
| `--prefix <dir>` | Remove only the install in this directory |

Data is kept unless you ask for it to go. `--all` is the only thing that
deletes the knowledge base, and it prompts unless `--force` is given.

Conduit runs no service and no containers, so there is nothing of that kind to
tear down. Shared tools are never removed.

```bash
conduit uninstall                    # interactive
conduit uninstall --keep-data
conduit uninstall --all --force
conduit uninstall --dry-run
conduit uninstall --info
```

A machine that ran a Conduit 1.x installer also has a daemon service and
container leftovers this command knows nothing about. Remove those with
`scripts/remove-v1.sh`, which defaults to a dry run.

---

## Removed commands

These existed in Conduit 1.x and were retired in 2.0. Each still responds with
an explanation of what was removed and what to use instead, rather than a bare
"unknown command". They are hidden from `--help` so it lists only what Conduit
can actually do.

| Removed | Why, and what to use |
|---|---|
| `start`, `stop`, `restart` | No daemon and no containers, so nothing to start or stop. Use `conduit status`. |
| `service` | No background service to install or manage. Use `conduit mcp configure`. |
| `events` | Came from the daemon's SSE endpoint. Commands report their own progress. |
| `install` | Third-party connectors ran as containers; the runtime was removed. |
| `list` | Connector instances no longer exist. Use `conduit kb list`. |
| `remove` | Use `conduit kb remove <name-or-id>`. |
| `create` | Connector instances no longer exist. |
| `stats` | Use `conduit kb stats` or `conduit status`. |
| `permissions`, `audit` | Connector instances no longer exist. |
| `logs` | Use `conduit mcp logs`. |
| `client` | Bindings tied AI clients to connector instances. Use `conduit mcp configure`. |
| `deps`, `install-deps` | Conduit no longer installs Podman, Docker, Qdrant or FalkorDB. Use `conduit doctor`. |
| `qdrant` | Vectors live in the knowledge base SQLite file since 2.0. |
| `falkordb` | The graph lives in the same SQLite file. Enable with `kb.kag.enabled`. |

Worth recording: the connector instance lifecycle never actually ran
containers. The daemon handlers that claimed to start and stop them only wrote
a status column.

See [CHANGELOG.md](../CHANGELOG.md#breaking-changes).

---

## See also

| Document | Contents |
|---|---|
| [USER_GUIDE.md](USER_GUIDE.md) | Day-to-day use with context |
| [ADMIN_GUIDE.md](ADMIN_GUIDE.md) | Configuration schema and operations |
| [QUICK_START.md](QUICK_START.md) | First-run walkthrough |
| [INSTALL_V2.md](INSTALL_V2.md) | Installation and upgrade |
