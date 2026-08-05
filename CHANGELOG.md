# Changelog

All notable changes to Conduit will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [2.0.0-beta] - Unreleased

Conduit 2.0 is a ground-up simplification: **one binary, zero containers, zero
external services, zero background processes.**

Version 1 documented a system larger than the one that actually ran. Connectors
that never started containers, a graph that returned nothing, retrieval knobs
the search engine ignored, and path protections nothing enforced. Version 2's
rule is that a feature is either verified end to end or it is deleted. A good
deal of what follows is deletion.

### Added

**Retrieval**

- `kb_lexical_search` MCP tool — pure FTS5/BM25 keyword search with no
  embeddings, no fusion and no diversity filtering, designed for an agentic
  search/read/refine loop. ([#80](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/80))
- In-database vector store: `kb_vectors` holds raw little-endian float32 BLOBs
  plus a precomputed L2 norm, searched by an exact brute-force cosine scan in
  pure Go. The scan is two-phase — top-K over vectors alone, then a join back
  to chunks and documents for the survivors.
  ([#78](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/78))
- `Rank` field on search results, and a unified score scale across modes.
  ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))

**Embeddings**

- `internal/embed`: a provider layer with three implementations —
  `llama-server` (default, managed loopback sidecar, shared singleton, idle
  shutdown), `ollama`, and `none` (lexical-only, a first-class mode).
  ([#79](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/79))
- Pinned model registry: each model tied to an exact HuggingFace repository,
  file and SHA-256, with pooling mode and required instruction prefixes
  recorded — using the wrong pooling mode degrades retrieval silently, with no
  error and no obvious signal in the vectors.
  ([#79](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/79))
- `conduit model` command group: `download`, `list`, `verify`, `path`.
  Downloads verify SHA-256 before the file is renamed into place; a mismatch
  deletes the download and fails, and there is no override flag. Re-running is
  idempotent, so install scripts can call it unconditionally.
  ([#83](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/83))

**Privacy instrumentation**

- `internal/querylog`: a local-only, append-only JSONL log of query *shape* —
  token count, whether the query looks like it names an entity, requested
  traversal depth. The record type structurally cannot hold query text, entity
  names, titles, paths, snippets or results; a redaction test guards against a
  field being added. File mode 0600, never read back by Conduit, never
  uploaded anywhere. Controlled by `telemetry.local_query_log` (default
  `true`). ([#81](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/81))

**Safety and tooling**

- `policy.forbidden_paths` and `policy.warn_paths` are now *enforced* on
  `conduit kb add`, after symlink resolution.
  ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))
- `scripts/remove-v1.sh` — tears down the v1 daemon, launchd/systemd units and
  containers that installing v2 leaves behind. Dry run is the default; it never
  deletes the knowledge base, configuration or models.
  ([#83](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/83))
- `--db` global flag: the workspace-isolation seam. One binary, many
  independent knowledge bases.
  ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82))
- Golden retrieval harness and characterization tests, so a change in ranking
  behaviour shows up in a diff instead of being discovered in production.
  ([#74](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/74))
- CI that builds, vets and tests on macOS and Linux with `CGO_ENABLED=1` and
  `-tags fts5`; release Go version pinned.
  ([#65](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/65))

### Changed

- **MCP server ported to the official Go SDK**, negotiating spec revision
  2026-07-28 and falling back for older clients. Existing tool descriptions and
  argument names were carried over verbatim — they are a tuned asset and client
  prompts depend on the exact wording. A test byte-freezes the schemas.
  ([#80](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/80))
- **Fusion is RRF-only.** `HybridSearchOptions` went from 13 fields to 4.
  ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))
- **Retrieval tuning collapsed to one knob.** `kb.rag.recall_mode`
  (`high` / `balanced` / `precise`) replaces `semantic_weight`, `use_mmr`,
  `mmr_lambda` and `rerank` — four keys that fed fields the engine either never
  read or overwrote from the preset before use, so setting them changed nothing
  a user could observe.
  ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))
- **Knowledge graph demoted to opt-in.** `kb.kag.enabled` defaults to `false`;
  no graph tables exist in the database until it is `true`. The default
  extraction provider is `pattern` — no LLM, no network. `kag_query` now
  reports three honest states instead of conflating "graph is off" with "no
  match". ([#81](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/81))
- **CLI is library-first.** Commands in `internal/cli` are thin shells over
  `internal/kbservice`; business logic is importable and testable without a
  process boundary.
  ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82))
- **One config schema.** The `config.Config` struct *is* the schema. Precedence
  is flags > environment (`CONDUIT_*`) > file > compiled defaults. Exactly one
  file is read — working directory, then `~/.conduit`, then `/etc/conduit` — so
  a project can override a user's settings. Unknown keys are reported by name
  rather than silently ignored, making leftovers from a removed subsystem
  visible. ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82))
- `doctor`, `status` and `setup` repurposed for a world with no service that
  can be "down": `status` reports the knowledge base file and retrieval
  configuration, `doctor` diagnoses with remedies and a meaningful exit code,
  `setup` initialises without installing anything that runs in the background.
  ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82),
  [#83](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/83))
- `conduit setup` no longer downloads the embedding model unless `--model` is
  passed — setup should not start a few-hundred-megabyte transfer on a metered
  connection without being asked.
  ([#83](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/83))
- Desktop Electron app frozen: no feature work, no dependency bumps, no
  security patches. Dependabot dispositioned.
  ([#66](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/66))

### Removed

- **The daemon.** `conduit-daemon`, its Unix socket, its HTTP API, its SSE
  event stream, and the launchd agent / systemd unit that started it at login.
  ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82))
- **The container runtime requirement.** No Podman, no Docker.
  ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82))
- **Qdrant.** Vectors moved into the knowledge base SQLite file.
  ([#78](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/78))
- **FalkorDB.** The graph moved into the same SQLite file as edge tables.
  ([#81](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/81))
- **The third-party connector subsystem** — instances, bindings, container
  images, per-instance permissions and audit. Worth recording why: the instance
  lifecycle never actually ran containers. The daemon handlers that claimed to
  start and stop them only wrote a status column.
  ([#82](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/82))
- **A 645-line policy engine** whose only consumer had already been deleted, and
  which held a second hardcoded copy of the forbidden-path lists that nothing
  read. ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))
- An untimed embedding client that could hang indefinitely.
  ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))
- The Electron desktop GUI as a shipping product.
  ([#66](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/66))

### Fixed

Installer hardening, from a review of `scripts/install.sh`:

- **The MCP entry now records the absolute path of the installed binary**, not
  the bare name `conduit`. An AI client launched from a GUI inherits no shell
  PATH, so it never read the block the installer appends to `~/.zshrc`, and the
  server failed to spawn with nothing naming PATH as the cause. With `--prefix`
  the directory may be on no PATH at all.
- **`--prefix` must be an absolute path with no shell metacharacters, and the
  PATH block it writes is single-quoted.** The prefix was previously
  interpolated unescaped into a double-quoted `export PATH="…"` line, so a
  prefix containing `$( )` or backticks became code that ran in every login
  shell thereafter.
- **`--version` must look like a v2 tag.** A value such as
  `../download/v2.0.0-beta.3` traversed the download URL while every message
  went on quoting the tag as given.
- **`--client <typo>` fails before anything is installed.** It used to install
  the binary, run setup, and print "Done." with no client configured anywhere.
  `conduit setup` now also returns an error rather than exiting 0 when the MCP
  registration fails.
- **A failed release lookup says what actually failed.** Rate limiting (HTTP
  403/429), an unreachable API, an HTTP error and a 200 carrying something that
  is not a releases list are four distinct messages; all of them name
  `--version`, which skips the API entirely. Previously every one of them
  reported "no Conduit 2.0 release has been published yet", and a 403 reported
  it *in addition to* the correct error.
- **A directory where the binary belongs is refused.** `mv` moves a file
  *inside* a directory destination rather than failing, so the install reported
  success with the binary at `conduit/conduit.new.<pid>`.
- **The install prefix is checked for writability before the download**, not
  after 13 MB and a verified checksum.
- **`--from-source` piped from `curl` explains itself.** It died with
  `BASH_SOURCE[0]: unbound variable` on bash 3.2; it now detects the missing
  checkout and prints clone instructions. The release path works piped.
- **`--log-level` now does something.** It was a documented flag applied to
  nothing, which is why raw zerolog JSON appeared in the installer's output and
  in ordinary command output. The installer also no longer runs `doctor` twice
  or prints the next steps twice.
- **`install.sh` no longer runs a package manager unattended.** It passes
  `--skip-tools` to `conduit setup`, so installing Conduit cannot turn into a
  `brew install poppler`. It reports when `pdftotext` is missing.
- **v1 detection matches `remove-v1.sh`**, adding `conduit-daemon.service`,
  three more `conduit-daemon` locations, and the Qdrant/FalkorDB containers.
- **`uninstall.sh` honours `CONDUIT_PREFIX`**, which `install.sh` has always
  read — the matching uninstall previously left that install in place.
- Shadow warnings now cover a regular file at `/usr/local/bin/conduit`, not
  only a symlink, and consult PATH order before warning at all.

- MCP handoff: every search hit now prints a `document_id:` line, and
  `kb_get_document` accepts a document path as an alternative key, so AI
  clients can go from a search hit to the full document without touching the
  filesystem — found on the first day of dogfooding
  ([#91](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/91)).

All nine tracked retrieval defects were fixed at root cause rather than patched
around, and every characterization test was flipped to an enforcement test.
([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84), on the
harness from [#74](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/74))

- [#69](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/69) —
  hybrid search options the engine never read. Fixed by deletion.
- [#70](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/70) —
  FTS5 query construction deleted operator characters instead of quoting them,
  making filenames, version strings and boolean-looking terms unsearchable.
- [#71](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/71) —
  two different chunk-ID functions disagreed; unified to one.
- [#72](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/72) —
  the chunker ignored its configured splitters and emitted a redundant trailing
  chunk.
- [#73](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/73) —
  the never-zero-results fallback ladder fell through to the partial rung when
  the relaxed rung merely matched nothing, rather than when it failed.
- [#75](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/75),
  [#76](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/76) —
  source filtering was not honoured in hybrid mode.
- [#77](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/77) —
  response contract: mixed score scales across modes and dishonest confidence
  values.
- A latent nondeterministic tie-break that ordered equal-scoring results
  differently between runs. Search is now deterministic.

### Security

- Dependency bumps clearing all 22 `go.mod` Dependabot alerts on the v2 line
  ([#67](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/67)),
  backported to `main` for v1
  ([#68](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/68)).
- **SEC-001 resolved by removal.** The v1 Qdrant (6333/6334) and FalkorDB
  (6379) containers published on `0.0.0.0` with no authentication, exposing the
  entire vector store and knowledge graph to anything on the local network. v2
  has no containers and no network listeners; the only loopback service is the
  optional embedding sidecar, bound to `127.0.0.1` on a port Conduit picks.
  ([#78](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/78),
  [#81](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/81))
- **Private-key exposure closed.** `conduit kb add ~/.ssh` was previously
  accepted, chunking every private key into a full-text index that the MCP
  server would hand to any connected AI client on request. Forbidden paths are
  now enforced, after symlink resolution.
  ([#84](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/84))
- Model downloads are SHA-256 verified against a pinned registry, so a
  corrupted or substituted file cannot be installed.
  ([#79](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/79))
- Uninstall and v1 teardown were adversarially reviewed and 22 findings fixed.
  Path guards are identity-based rather than string-prefix based, dry run is
  the default for teardown, and `--all` is the only thing that deletes data.
  ([#83](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/83))
- **SEC-002 stands.** The v1 desktop DMGs remain unsigned, ship an end-of-life
  Electron/Chromium, and contain a renderer-to-shell IPC handler. They are
  unsupported; delete them.
  ([#66](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pull/66))
- **SEC-003 is unchanged and by design.** Indexed document content flows
  verbatim to connected AI clients, so an indexed document is a
  prompt-injection vector. See [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md).

### BREAKING CHANGES

1. **The daemon is gone.** Nothing runs in the background. `conduit-daemon`,
   its Unix socket and its HTTP API no longer exist. Anything that spoke to
   that API must be rewritten to shell out to the CLI or import
   `internal/kbservice`.

2. **No data migration.** A v1 knowledge base cannot be read by v2. There is no
   converter and none is planned. **Wipe and re-ingest:** re-add your sources
   with `conduit kb add` and run `conduit kb sync`. Your source documents are
   untouched; only the index is rebuilt.

3. **Commands removed.** `start`, `stop`, `restart`, `service`, `events`,
   `install`, `list`, `remove`, `create`, `stats`, `permissions`, `audit`,
   `logs`, `client`, `deps`, `install-deps`, `qdrant`, `falkordb`.

   Each is registered as a hidden stub that names what was removed and what to
   use instead, so you get an explanation rather than "unknown command". They
   are hidden so `--help` lists only what Conduit can actually do.
   Knowledge-base equivalents survive under `conduit kb` (`kb list`,
   `kb remove`, `kb stats`) and `conduit mcp` (`mcp logs`).

4. **Config keys removed.** A config file carrying them still loads, and every
   unrecognised key is reported by name:

   - Daemon and transport: `socket`, `runtime`, HTTP server timeouts
   - Vector server: Qdrant host/port keys
   - Graph server: `kb.kag.graph.falkordb` and its host/port/password
   - Retrieval: `kb.rag.semantic_weight`, `kb.rag.use_mmr`, `kb.rag.mmr_lambda`,
     `kb.rag.rerank`, `kb.rag.enabled` — replaced by `kb.rag.recall_mode`
   - `policy.allow_network_egress`, `ai`

5. **The desktop GUI is retired.** No replacement is planned. The CLI and the
   MCP server are the supported interfaces.

6. **Platform support narrowed.** macOS arm64 and Linux x86_64. Binaries are
   not code-signed or notarized.

### Migrating from 1.x

```bash
./scripts/remove-v1.sh              # dry run: report what v1 left behind
./scripts/remove-v1.sh --yes        # tear down daemon + containers, keep data
./scripts/install.sh --from-source  # install v2
conduit model download              # optional: enables semantic search
conduit kb add <path> --name "..."  # re-add sources
conduit kb sync                     # re-index
conduit doctor                      # confirm
```

Also `rm -rf /Applications/Conduit.app` if you installed the desktop app.

Full instructions: [docs/INSTALL_V2.md](docs/INSTALL_V2.md).

---

## [1.0.42] - 2026-01-07

### V1.0 Release - Private Knowledge Base for AI Tools

The first stable release of Conduit. **This line is frozen and receives no
fixes.** Read the security advisories in
[docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md) before running it.

### Highlights

- **RAG (Retrieval-Augmented Generation)**: Hybrid search combining semantic and keyword matching
- **KAG (Knowledge-Augmented Generation)**: Knowledge graph for multi-hop reasoning
- **MCP Integration**: Works with Claude Code, Cursor, and other AI tools via Model Context Protocol
- **100% Local**: All documents and processing stay on your machine

### Added

- `--rebuild-vectors` flag for `conduit kb sync` to force vector regeneration
- Exit code 2 for partial success when semantic indexing fails
- Clear warnings with actionable guidance for sync issues
- `docs/KNOWN_ISSUES.md` documenting common issues and workarounds
- GitHub Discussions for community support
- GitHub issue templates for bug reports and feature requests
- Comprehensive `CONTRIBUTING.md` guide
- `docs/QUICK_START.md` for new users

### Changed

- README.md completely revamped with new positioning as "Private Knowledge Base for AI Tools"
- CLI installation promoted as the primary method
- Desktop App moved to "Experimental" status
- Documentation reorganized by user type (Quick Start, Power User, Developer)

### Fixed

- Silent fallback to FTS-only when Qdrant fails (#41)
- Single-source sync now correctly passes `--rebuild-vectors` flag

---

## Pre-1.0 History

Conduit v1.0 is the culmination of the v0.x development cycle. Key milestones:

- **v0.1.41**: KB CLI compliance fixes, RAG tuning panel
- **v0.1.40**: Dashboard infrastructure status fixes
- **v0.1.39**: FalkorDB and KAG integration
- **v0.1.30**: Hybrid RAG search with MMR diversity
- **v0.1.20**: Qdrant vector database integration
- **v0.1.10**: Dependency management system
- **v0.1.0**: Initial release with MCP server and FTS5 search

For detailed history, see the [GitHub Releases](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/releases) page.

---

## Release Types

- **Major (x.0.0)**: Breaking changes or major new capabilities
- **Minor (1.x.0)**: New features, backwards compatible
- **Patch (1.0.x)**: Bug fixes and minor improvements
