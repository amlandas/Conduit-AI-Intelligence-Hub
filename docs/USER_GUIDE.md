# Conduit User Guide

**Version**: 2.0.0-beta
**Last Updated**: August 2026

---

## Contents

1. [The mental model](#the-mental-model)
2. [Installation](#installation)
3. [Quick start](#quick-start)
4. [Managing sources](#managing-sources)
5. [Searching](#searching)
6. [Connecting AI clients](#connecting-ai-clients)
7. [The embedding model](#the-embedding-model)
8. [Workspaces](#workspaces)
9. [The knowledge graph (optional)](#the-knowledge-graph-optional)
10. [Checking on things](#checking-on-things)
11. [Backup](#backup)
12. [Privacy and safety](#privacy-and-safety)
13. [Troubleshooting](#troubleshooting)
14. [Command reference](#command-reference)

---

## The mental model

Conduit is **one binary**. There is no service to start, nothing running in the
background, and nothing that starts at login.

Every command opens one SQLite file, does its work, and exits. That file —
`~/.conduit/conduit.db` by default — holds everything: the keyword index, the
vectors, your document metadata, and optionally a graph. Backing up Conduit
means copying one file.

You point Conduit at folders. It reads the files, splits them into chunks, and
indexes them. Then you point an AI client at `conduit mcp kb`, and the
assistant gets a set of search tools it can call while answering you.

Nothing is uploaded. The only time Conduit uses the network is to download
itself and to download the embedding model.

---

## Installation

See [INSTALL_V2.md](INSTALL_V2.md) for the full guide, including release-binary
installs, upgrades, and moving from Conduit 1.x.

The short version:

```bash
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub
cd Conduit-AI-Intelligence-Hub
./scripts/install.sh                   # newest published release
```

Or build it yourself, which is the only option on Intel Macs and Linux arm64:

```bash
./scripts/install.sh --from-source     # needs Go 1.21+ and a C compiler
```

> **Coming from Conduit 1.x?** Installing v2 does not remove the v1 daemon or
> its containers. Run `./scripts/remove-v1.sh` (dry run by default) first, and
> read the security advisories in [KNOWN_ISSUES.md](KNOWN_ISSUES.md). There is
> no data migration — you re-add your sources and re-index.

---

## Quick start

```bash
# 1. Prepare the machine and configure Claude Code
conduit setup

# 2. Get the embedding model (optional; enables semantic search)
conduit model download

# 3. Add a folder
conduit kb add ./docs --name "Project Docs"

# 4. Index it
conduit kb sync

# 5. Try it
conduit kb search "how does authentication work"
```

Then restart your AI client. Ask it something answerable from your documents
and it will call the search tools.

---

## Managing sources

A *source* is a folder Conduit indexes.

### Adding

```bash
conduit kb add ./docs --name "Project Docs"
conduit kb add /path/to/notes --patterns "*.md,*.txt"
conduit kb add ./src --excludes "node_modules,dist"
conduit kb add ./docs --sync auto
```

| Flag | Meaning |
|---|---|
| `--name` | Display name for the source |
| `--patterns` | File patterns to index, comma-separated |
| `--excludes` | Directories to exclude, comma-separated |
| `--sync` | `manual` (default) or `auto` |
| `--json` | Machine-readable output |

**Default patterns.** With no `--patterns`, Conduit indexes documentation
(`*.md`, `*.txt`, `*.rst`), source code (`*.go`, `*.py`, `*.js`, `*.ts`,
`*.java`, `*.rs`, `*.rb`, `*.c`, `*.cpp`, `*.h`, `*.hpp`, `*.cs`, `*.swift`,
`*.kt`, and shell/batch scripts), config (`*.json`, `*.yaml`, `*.yml`, `*.xml`,
`*.jsonld`, `*.toml`, `*.ini`, `*.cfg`), data (`*.csv`, `*.tsv`), and documents
(`*.pdf`, `*.doc`, `*.docx`, `*.odt`, `*.rtf`).

PDF and office formats need external extraction tools; `conduit setup` offers
to install them, and `conduit doctor` tells you if they are missing.

**Default excludes.** `node_modules`, `.git`, `.svn`, `.hg`, `__pycache__`,
`.pytest_cache`, `vendor`, `dist`, `build`, `target`, `.DS_Store`,
`Thumbs.db`.

**Paths Conduit will refuse.** Some directories are blocked outright because
indexing them would copy secrets into a searchable database that the MCP server
hands to AI clients. By default: `/`, `/etc`, `/var`, `/usr`, `~/.ssh`,
`~/.aws`, `~/.gnupg`, `~/.config/gcloud`, `~/.kube`. Symlinks are resolved
first, so you cannot get around it with a link.

Others warn but proceed: `~/.config`, `~/Documents`, `~/Desktop`.

Both lists are configuration (`policy.forbidden_paths`, `policy.warn_paths`)
and you can edit them — see the [Admin Guide](ADMIN_GUIDE.md).

### Listing and removing

```bash
conduit kb list                    # alias: conduit kb ls
conduit kb list --json
conduit kb remove "Project Docs"
conduit kb remove test --force
```

### Indexing

```bash
conduit kb sync                      # sync all sources
conduit kb sync <source-id>          # sync one source
conduit kb sync --rebuild-vectors    # force rebuild of the vector index
```

**Exit codes matter here.** `kb sync` tells you the truth rather than reporting
success for work that did not happen:

| Code | Meaning |
|---|---|
| 0 | Full success — keyword and semantic indexing both completed |
| 1 | Error — the sync failed |
| 2 | **Partial success** — keyword indexing worked, semantic indexing failed |

Exit code 2 means search still works, but only on keywords. Run
`conduit doctor` to find out why the embedding step failed.

If you enabled semantic search *after* indexing documents, existing documents
have no vectors. Backfill them:

```bash
conduit kb migrate
```

This requires an embedding provider and fails when `embed.provider` is `none`.

---

## Searching

```bash
conduit kb search "how does authentication work"
```

### How it works

Every query runs twice — once against the FTS5 keyword index, once against the
vector index — and the two ranked lists are combined with Reciprocal Rank
Fusion (RRF). Keyword search finds the exact identifier you typed; vector
search finds the paragraph that means the same thing in other words. RRF is the
only fusion method, so results are deterministic: the same query against the
same index returns the same ranking every time.

Hybrid mode adapts to what you typed. Quoted phrases push toward exact lexical
matching; proper nouns boost exact matches; natural-language questions balance
the two.

### Asking questions

Keyword search requires every word you type to be present in the same chunk, so
the grammar of a question would otherwise work against you: `how do tokens
expire` would demand the words "how" and "do" as well as "tokens" and "expire".

Conduit drops that scaffolding from the keyword half of the search — question
words, `is`/`are`/`do`/`does`, `a`/`the`, and common prepositions and pronouns —
so these two searches return the same thing:

```bash
conduit kb search "how do tokens expire"
conduit kb search "tokens expire"
```

Three things are worth knowing:

- **Only the keyword half.** Semantic search sees the sentence exactly as you
  typed it, because there the phrasing genuinely carries meaning.
- **Quoting overrides it.** If you actually want the word "how", quote it:
  `conduit kb search '"how" to guide'`.
- **Nothing else is dropped.** `and`, `or` and `not` are searched for as words,
  and so are `may`, `must`, `should` and `shall` — they matter too much in
  specifications to treat as noise. A search made of nothing but question words
  (`conduit kb search "the"`) searches for those words.

### Modes

```bash
conduit kb search "authentication"                  # hybrid (default)
conduit kb search "authentication" --semantic       # vectors only
conduit kb search "class AuthProvider" --fts5       # keywords only
```

`--semantic` needs an embedding provider. `--fts5` always works.

### Tuning

```bash
conduit kb search "ASL-3 safeguards" --recall high        # widen recall
conduit kb search "authentication" --recall precise       # fewer, more distinct
conduit kb search "AI safety" --semantic --min-score 0.0  # pure semantic, no floor
```

| Flag | Meaning |
|---|---|
| `--recall` | `high`, `balanced` (default), or `precise` |
| `--min-score` | Minimum similarity for `--semantic` (0.0–1.0) |
| `--limit` | Maximum results (default 10) |
| `--context` | Include N adjacent chunks around each hit |
| `--raw` | Raw chunks, skipping merge and boilerplate filtering |
| `--json` | Machine-readable output |

`--recall high` disables diversity filtering and keeps everything similar;
`precise` deduplicates aggressively. If a search feels like it is hiding
results, try `--recall high` first.

By default results are processed: chunks from the same document are merged and
boilerplate is filtered. `--raw` turns that off.

### Search always returns something

If your query matches nothing directly, Conduit relaxes it in stages rather
than handing back an empty list. Results from a relaxed stage are still real
matches, just for a looser interpretation of what you asked.

---

## Connecting AI clients

### The easy way

```bash
conduit setup                            # configures Claude Code by default
conduit setup --client cursor
conduit mcp configure                    # just the MCP step
conduit mcp configure --client vscode
conduit mcp configure --check            # is it already configured?
conduit mcp configure --force            # overwrite existing config
```

Supported clients:

| Client | Config file |
|---|---|
| `claude-code` (default) | `~/.claude.json` |
| `cursor` | `.cursor/settings/extensions.json` |
| `vscode` | `.vscode/settings.json` |

Restart the client afterwards.

### By hand

The server runs over stdio:

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

To point a client at a specific knowledge base, add `--db`:

```json
{
  "mcpServers": {
    "conduit-work": {
      "command": "conduit",
      "args": ["--db", "/Users/you/work.db", "mcp", "kb"]
    }
  }
}
```

### What the AI can do

Seven tools:

| Tool | What it does |
|---|---|
| `kb_search` | Hybrid search (keyword + semantic, RRF-fused). The default. |
| `kb_lexical_search` | Pure FTS5/BM25 keyword search — no vectors, no fusion, no filtering. The grep of the knowledge base. Best for hunting an exact identifier, error string, symbol or config key, and for iterative refinement loops. |
| `kb_search_with_context` | Merged, boilerplate-filtered, citation-ready passages. Best when the assistant is going to quote sources. |
| `kb_list_sources` | Sources with IDs, paths, document counts, sync status. |
| `kb_get_document` | Full content of one document, by `document_id` (printed on every search hit) or by path. |
| `kb_stats` | Source, document and chunk counts; search capability status. |
| `kag_query` | Entities and relationships from the knowledge graph (if enabled). |

You rarely need to name these — a well-behaved client picks the right one. If
your assistant is struggling to find something you know is indexed, asking it
to "use the lexical search tool for the exact string X" usually works.

### Checking the connection

```bash
conduit mcp status
conduit mcp status --json
```

---

## The embedding model

Semantic search needs a model. Without one, Conduit runs on keyword search
alone — **a supported mode, not a broken install.**

```bash
conduit model list        # what's available and what's downloaded
conduit model download    # fetch the configured (or default) model
conduit model verify      # re-hash a local file against the registry pin
conduit model path        # where a model lives on disk
```

Available models:

| Model | Dimensions | Context | Size |
|---|---|---|---|
| `nomic-embed-text-v1.5` (default) | 768 | 2048 tokens | 261.6 MB |
| `mxbai-embed-large-v1` | 1024 | 512 tokens | 638.6 MB |
| `qwen3-embedding-0.6b` | 1024 | 32768 tokens | 609.5 MB |

All Apache-2.0.

```bash
conduit model download qwen3-embedding-0.6b
conduit model download --force              # re-fetch even if valid
conduit model download --timeout 30m
```

Every model is pinned to an exact file in an exact repository with an exact
SHA-256. The download goes to a temporary name and is only renamed into place
once the hash matches; a mismatch deletes it and fails. There is no flag to
install an unverified model. Re-running `download` is safe and idempotent.

**Changing models changes vector dimensions.** After switching, rebuild:

```bash
conduit kb sync --rebuild-vectors
```

---

## Workspaces

The global `--db` flag selects a knowledge base file. One binary, many
independent knowledge bases that never see each other:

```bash
conduit --db ~/work.db kb add ./work-docs --name "Work"
conduit --db ~/work.db kb sync
conduit --db ~/work.db kb search "Q3 planning"

conduit --db ~/personal.db kb add ~/notes --name "Notes"
conduit --db ~/personal.db kb search "sourdough"
```

This is the recommended way to keep untrusted or unrelated content away from
the knowledge base your coding assistant has open. See
[Privacy and safety](#privacy-and-safety).

A project directory can also carry its own `conduit.yaml`, which is read in
preference to `~/.conduit/conduit.yaml`.

---

## The knowledge graph (optional)

KAG extracts named entities and the relationships between them, for questions
that need connecting two facts rather than finding one passage.

**It is off by default.** No graph tables exist in your database until you turn
it on:

```bash
conduit config set kb.kag.enabled true
conduit kb kag-sync
conduit kb kag-query "threat models"
```

The default extraction provider is `pattern` — no LLM, no network, no model
download. It is fast and modest in what it finds. Ollama-based extraction is
available if you already run Ollama.

```bash
conduit kb kag-sync                  # extract from unprocessed chunks
conduit kb kag-sync --force          # re-extract everything
conduit kb kag-status                # progress dashboard
conduit kb kag-retry                 # retry failed extractions
conduit kb kag-dedupe                # merge duplicate entities
conduit kb kag-vectorize             # embed entities (enables --hybrid)
conduit kb kag-query "API security" --max-hops 3
conduit kb kag-query "threat model" --hybrid --format json
```

`kag_query` reports its state honestly. "The graph is disabled", "the graph is
empty", and "nothing matched" are three different answers, and you will be told
which one you got.

Whether the graph is worth its cost is an open question the project is
deliberately measuring rather than guessing at — see
[Privacy and safety](#privacy-and-safety).

---

## Checking on things

```bash
conduit status              # what's in the knowledge base
conduit status --json
conduit doctor              # diagnose problems, with remedies
conduit doctor --json
conduit doctor --probe-timeout 30
conduit kb stats
conduit config              # current configuration
conduit version
```

`status` reports the file and its contents. There is no service that can be
"down", so there is nothing to report about one.

`doctor` checks that configuration loads and contains no keys Conduit no longer
understands; the knowledge base file is present, readable and writable; FTS5 is
compiled in; the embedding provider is reachable (skipped when
`embed.provider` is `none`); the vector index exists and is populated; and at
least one AI client has the MCP server configured. It exits 0 if everything
needed works and 1 if a check failed.

Use `--probe-timeout` when the embedding model is cold and needs longer than
the default 15 seconds to answer.

---

## Backup

The knowledge base is one file:

```bash
cp ~/.conduit/conduit.db ~/backups/conduit-$(date +%F).db
```

Or use the built-in command, which archives the data directory (database,
configuration, knowledge base data) as a compressed tarball:

```bash
conduit backup
conduit backup --output ~/backups/conduit.tar.gz
```

Your original documents are never modified and do not need backing up on
Conduit's account.

---

## Privacy and safety

**Nothing leaves the machine.** Documents, index, vectors and embeddings stay
local. Conduit opens no listening socket for its own API — the MCP server talks
over stdio to the client that launched it. When semantic search is enabled with
the default provider, Conduit runs a `llama-server` sidecar bound to
`127.0.0.1` and shuts it down after an idle period. The only network use is
downloading the binary and the model.

**Path safety.** Directories in `policy.forbidden_paths` are refused by
`kb add`, after resolving symlinks.

**The query-shape log.** Conduit keeps a local file,
`~/.conduit/query-shape.jsonl`, with one line per query. It records the
*shape* of the query — how many tokens, whether it looks like it names an
entity, how many hops were requested — and **cannot contain the query itself.**
The record type has no field capable of holding your query text, entity names,
document titles, paths, snippets or results; that is enforced structurally and
guarded by a test, not by redaction at write time.

The file is mode 0600, Conduit never reads it back, and nothing uploads it
anywhere. It exists to answer one question with evidence instead of opinion:
does anyone actually ask multi-hop questions? The knowledge graph's future
depends on the answer, and it cannot be answered retroactively.

Turn it off and no file is created:

```bash
conduit config set telemetry.local_query_log false
```

### Prompt injection: the one caveat worth understanding

Conduit's MCP tools return **raw chunks of your indexed documents directly to
the AI client.** No model summarises them first — that is what keeps search
fast, private and predictable.

It also means **an indexed document is a prompt-injection vector.** If you index
untrusted content — a third-party PDF, a scraped web page, a shared drive, a
dependency's documentation — instructions hidden in that content arrive at your
assistant as tool output and may influence what it does, including what it does
with your other tools. Conduit cannot meaningfully sanitize this and does not
claim to.

What to do about it:

- Index content you trust. Treat adding a source like installing a dependency.
- Use `--db` to keep untrusted corpora in a separate knowledge base from the
  one your coding agent has open.
- Read search results in agent transcripts with the same skepticism you apply
  to a fetched web page.
- Prefer AI clients that visually distinguish tool output from your own
  instructions.

Tracked as SEC-003 in [KNOWN_ISSUES.md](KNOWN_ISSUES.md).

---

## Troubleshooting

### "no such module: fts5"

The binary was built without FTS5. Rebuild with both flags:

```bash
CGO_ENABLED=1 go build -tags fts5 -o conduit ./cmd/conduit
```

Or re-run `./scripts/install.sh --from-source`, which sets them.

### Search returns nothing useful

1. Is anything indexed? `conduit kb stats`
2. Did the last sync fully succeed? Re-run `conduit kb sync` and check the exit
   code — 2 means semantic indexing failed.
3. Are there vectors? `conduit status`. If not, `conduit model download` then
   `conduit kb migrate`.
4. Try `--recall high` to stop diversity filtering from hiding near-duplicates.
5. For an exact string, use `--fts5`.

### Semantic search is unavailable

`conduit doctor` will say why. Usually: no model downloaded
(`conduit model download`), or `embed.provider` is set to `none`.

### `kb sync` exits 2

Keyword indexing worked; the embedding step did not. Search works on keywords
meanwhile. `conduit doctor --probe-timeout 30` — a cold model often just needs
longer than the default probe.

### The AI client doesn't see the tools

```bash
conduit mcp status
conduit mcp configure --check
```

Restart the client after configuring. Confirm `conduit` is on your `PATH` in
the environment the client launches from — `~/.local/bin` is not always there.

### Config warnings about unrecognised keys

You have a config file from Conduit 1.x. The named keys are ignored and it is
safe to delete them; see the Admin Guide for the current schema.

### A source path was refused

It is inside `policy.forbidden_paths`. That is deliberate. If you genuinely
need to index it, edit the list in the config — but understand that everything
in it becomes searchable by any AI client you connect.

---

## Command reference

Global flags, available on every command:

| Flag | Meaning |
|---|---|
| `--db` | Path to the knowledge base file (default `<data-dir>/conduit.db`) |
| `--data-dir` | Conduit data directory (default `~/.conduit`) |
| `--log-level` | `debug`, `info`, `warn`, `error` |

| Command | Purpose |
|---|---|
| `conduit setup` | Prepare the machine, configure an AI client |
| `conduit status` | Knowledge base state |
| `conduit doctor` | Diagnose problems with remedies |
| `conduit config` | Show configuration (`get` / `set` / `unset` subcommands) |
| `conduit version` | Version information |
| `conduit backup` | Archive the data directory |
| `conduit uninstall` | Remove Conduit |
| `conduit kb add` | Add a folder |
| `conduit kb list` | List sources |
| `conduit kb remove` | Remove a source |
| `conduit kb sync` | Index new and changed documents |
| `conduit kb search` | Search |
| `conduit kb stats` | Statistics |
| `conduit kb migrate` | Backfill vectors for already-indexed documents |
| `conduit kb kag-*` | Knowledge graph operations (opt-in) |
| `conduit mcp kb` | Run the MCP server over stdio |
| `conduit mcp configure` | Configure an AI client |
| `conduit mcp status` | MCP configuration and capabilities |
| `conduit mcp logs` | MCP-related log output |
| `conduit model list` | Pinned embedding models |
| `conduit model download` | Download and verify a model |
| `conduit model verify` | Re-hash a local model against its pin |
| `conduit model path` | Path of a model artifact |
| `conduit ollama status` | Ollama status and loaded models |
| `conduit ollama models` | Available Ollama models |
| `conduit ollama pull` | Pull an Ollama model |
| `conduit ollama warmup` | Preload Ollama models into memory |

Commands from Conduit 1.x that no longer exist (`start`, `stop`, `service`,
`qdrant`, `falkordb`, `deps`, and others) still respond, with an explanation of
what was removed and what to use instead. See
[CHANGELOG.md](../CHANGELOG.md#breaking-changes).

Every command accepts `--help`.

---

## Uninstalling

```bash
conduit uninstall --dry-run     # preview
conduit uninstall --info        # what's installed
conduit uninstall               # interactive; keeps your data
conduit uninstall --keep-data   # remove binary and PATH entries, keep the index
conduit uninstall --all         # remove data too (prompts unless --force)
```

`--all` is the only thing that deletes your knowledge base. Shared tools
(Ollama, poppler) are never removed.

A machine that once ran a Conduit 1.x installer also has a daemon service and
container leftovers that `conduit uninstall` knows nothing about. Remove those
with `./scripts/remove-v1.sh` (dry run by default).

---

## See also

| Document | Contents |
|---|---|
| [INSTALL_V2.md](INSTALL_V2.md) | Installation, upgrade, moving from 1.x |
| [ADMIN_GUIDE.md](ADMIN_GUIDE.md) | Full configuration schema, diagnostics, tuning |
| [KNOWN_ISSUES.md](KNOWN_ISSUES.md) | Security advisories and current limitations |
| [EMBEDDING_SIDECAR.md](EMBEDDING_SIDECAR.md) | How the sidecar works |
| [../CHANGELOG.md](../CHANGELOG.md) | What changed from v1 |
