# Known Issues and Limitations

Security advisories, and an honest list of what Conduit 2.0 does not do.

**Last Updated**: August 2026 (Conduit 2.0.0-beta)

---

## Contents

- [Security advisories](#security-advisories) — SEC-001, SEC-002, SEC-003
- [Conduit 2.0 limitations](#conduit-20-limitations)
- [Historical: Conduit 1.x issues](#historical-conduit-1x-issues)
- [Reporting new issues](#reporting-new-issues)

---

## Security advisories

### SEC-001: Knowledge Base Data Stores Exposed to the Local Network

**Severity**: High
**Affects**: All Conduit 1.x releases up to and including v1.0.42 / desktop v0.1.43
**Status**: **Not applicable to Conduit 2.0** — resolved by removal. Existing 1.x installs must act.

#### Description

The Qdrant (ports 6333/6334) and FalkorDB (port 6379) containers created by the
v1 installer, CLI and daemon publish their ports on **all network interfaces**
(`0.0.0.0`), and neither service is configured with authentication. Any device
on the same network can read, modify, or delete the entire vector store and
knowledge graph, bypassing the daemon's Unix-socket security and policy engine.
The Qdrant HTTP API is additionally reachable from malicious webpages via DNS
rebinding.

#### Resolution in Conduit 2.0

There are no containers and no network listeners. Vectors and the graph live in
the knowledge base SQLite file. The only local service is the optional
embedding sidecar, which binds to `127.0.0.1` on a port Conduit picks and shuts
down after an idle timeout.

**The recommended fix is to move to 2.0 and tear the v1 stack down:**

```bash
./scripts/remove-v1.sh          # dry run — reports, changes nothing
./scripts/remove-v1.sh --yes    # remove daemon + containers, keep all data
```

#### Workaround (if you must stay on 1.x)

Recreate the containers with loopback-only bindings:

```bash
# Qdrant
docker stop conduit-qdrant && docker rm conduit-qdrant
docker run -d --name conduit-qdrant --restart unless-stopped \
  -p 127.0.0.1:6333:6333 -p 127.0.0.1:6334:6334 \
  -v ~/.conduit/qdrant:/qdrant/storage docker.io/qdrant/qdrant:latest

# FalkorDB
docker stop conduit-falkordb && docker rm conduit-falkordb
docker run -d --name conduit-falkordb --restart unless-stopped \
  -p 127.0.0.1:6379:6379 \
  -v ~/.conduit/falkordb:/data docker.io/falkordb/falkordb:latest
```

(Substitute `podman` for `docker` if you use Podman.) Data is preserved — the
volumes are unchanged. Verify with `docker ps`: the port column should show
`127.0.0.1:6333->...` rather than `0.0.0.0:6333->...`.

---

### SEC-002: Desktop App (Electron GUI) Is Unsupported — Do Not Use

**Severity**: High
**Affects**: All desktop releases (v0.1.0–v0.1.43)
**Status**: Development halted; no fix planned, ever

#### Description

Desktop GUI development is halted. The published DMGs are unsigned, run an
end-of-life Electron/Chromium version with years of unpatched CVEs, and contain
an IPC handler (`terminal:spawn`) that allows the renderer process to execute
arbitrary shell commands — meaning any renderer compromise leads to code
execution as the user.

**This advisory still applies.** The DMGs remain downloadable from the releases
page and will never be patched. Conduit 2.0 has no GUI and no replacement is
planned.

#### Workaround

Delete it and use the CLI, which together with the MCP server is the supported
interface:

```bash
rm -rf /Applications/Conduit.app
```

The source is retained frozen at `apps/conduit-desktop/` for historical
reference only. Do not build or ship it.

---

### SEC-003: Indexed Documents Flow Verbatim to AI Clients (Prompt Injection)

**Severity**: Informational (by design)
**Affects**: All versions, including 2.0
**Status**: Documented behaviour, not a defect

#### Description

By design ("no LLM in the hot path"), Conduit's MCP search tools return raw
chunks of your indexed documents directly to the connected AI client. That is
what makes retrieval fast, private and predictable — no model sits between your
documents and the answer.

It also means **an indexed document is a prompt-injection vector.** If you index
untrusted content (third-party PDFs, scraped pages, shared drives, a
dependency's documentation), instructions embedded in that content will reach
your AI assistant as tool output and may influence its behaviour, including its
use of other tools available to it. Conduit sanitizes input to its own
entity-extraction pipeline, but does not — and cannot meaningfully — sanitize
what your AI client chooses to trust.

#### Recommendation

- Index content you trust. Treat adding a source as a trust decision, like
  installing a dependency.
- Use `--db` to keep untrusted corpora in a knowledge base separate from the
  one your coding agent has open.
- Keep `policy.forbidden_paths` tight — it is the one automated control here,
  and it is enforced on `conduit kb add` after symlink resolution.
- Treat KB search results in agent transcripts with the same skepticism as
  fetched web content.
- Prefer AI clients that visually attribute tool output distinctly from user
  instructions.

---

## Conduit 2.0 limitations

Things Conduit 2.0 genuinely does not do. None of these are secretly fine.

### Platform support: no Windows

**Status**: Deliberate scope decision, not an oversight.

macOS arm64 (Apple Silicon) and Linux x86_64 are supported and have published
binaries. Intel Macs and Linux arm64 can build from source; there are no
published binaries for them.

Windows is not supported. `scripts/install-windows.ps1` exists only to say so
rather than leaving a plausible-looking installer that wastes your time. The
specific gaps:

- The embedding sidecar is supervised with POSIX process groups and `flock`
  (`internal/embed/sysutil_unix.go`). The Windows half of that file is a stub
  that has never been exercised.
- Data directory, executable discovery and config location conventions all
  differ.
- There is no signed Windows artifact.

**Workaround**: WSL2, treated as Linux.

### Binaries are unsigned and un-notarized

**Status**: Open. No timeline.

Published binaries are not code-signed on any platform and are not notarized on
macOS. Gatekeeper will object to a downloaded binary, and you will have to
clear it manually.

Building from source (`./scripts/install.sh --from-source`) sidesteps this
entirely and is the recommended path until signing exists.

Note the asymmetry: **embedding models are hash-verified but the binary that
verifies them is not signed.** Model integrity is enforced; binary provenance
is not.

### Unfiltered search latency on large corpora and slower machines

**Status**: Known characteristic of the design. Watch it; do not be surprised
by it.

Vector search is an exact brute-force cosine scan rather than an approximate
index. Cost grows **linearly** with the number of chunks.

At the design target — roughly 5–50K chunks at 768 dimensions — a scan costs
tens of milliseconds, well inside budget. But that figure comes from
benchmarking on developer-grade hardware
(`.eng-lead-kb/BENCH-WP-2.1.md`). **An unfiltered query against a corpus at the
top of that range, on a slower machine, is the case where p95 latency becomes
noticeable.**

Mitigations, in order of effectiveness:

- Filter by source. Filters are SQL predicates evaluated *before* the distance
  computation, so a selective filter cuts scan cost proportionally. This is the
  main reason the design chose exactness over an approximate index.
- Split unrelated corpora across separate `--db` files.
- Use `kb_lexical_search` / `--fts5` for exact-identifier lookups; FTS5 is
  indexed and does not scan.

Beyond ~50K chunks, expect to feel it. An approximate index has not been added
because it would trade away the exact-filtering guarantee, and no evidence yet
says that trade is worth making.

### The fallback ladder's level-2 rung is a deletion candidate

**Status**: Under review for removal.

Search never returns zero results: it relaxes the query in stages. The ladder
has four rungs (0 primary, 1 relaxed, 2 partial, 3 none).

After the [#73](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/73)
fix, the relaxed rung already covers the union of single-term searches. Level 2
(`searchPartial`) is therefore reachable only when the relaxed rung fails
outright, rather than merely matching nothing — a much narrower case than it
was written for.

It is retained for now because deleting a safety net on the strength of an
argument rather than evidence is how v1 accumulated features nobody could
justify. It is flagged here so its eventual removal is not a surprise.

### Knowledge graph value is unproven

**Status**: Deliberately gated on evidence.

KAG is off by default (`kb.kag.enabled: false`). The default extraction
provider is `pattern` — no LLM, no network — and it is modest in what it finds.

Whether multi-hop graph retrieval earns its complexity is an open question. The
project measures it with the local query-shape log rather than guessing (see
the [Admin Guide](ADMIN_GUIDE.md#the-query-shape-log)). Do not build on KAG
expecting it to grow; it may be removed.

### Semantic search requires two things Conduit does not bundle

A `llama-server` binary and a model file. Neither ships with Conduit. Without
them, search runs on FTS5 keyword matching — a supported mode, not a broken
install. Set `embed.provider: none` to make that explicit and silence the
probe.

### A model change is only detected for models Conduit knows

Conduit records which embedding model built a knowledge base's vectors and
refuses to mix two vector spaces (issue #107). The check resolves model names
through the pinned registry, so that the same model reached through a different
provider — Ollama's `nomic-embed-text` and the registry's
`nomic-embed-text-v1.5` — is correctly recognised as one model rather than two.

The limit is the other direction. If the model is **not** in the registry, such
as a locally built Ollama tag, Conduit cannot prove that a differently spelled
name is a different model, and refuses to guess: it warns once, records the
identifier it saw, and disables nothing. So this case is not caught:

> A knowledge base indexed with an unregistered model, whose `embed.model` is
> then changed to a *different* unregistered model of the same width.

This is deliberate. The alternative — treating any unrecognised difference as a
model change — would disable semantic search on a working knowledge base every
time a user ran a model Conduit has no entry for, which is a worse and far more
common failure than the one it would catch.

Mitigations: watch the ⚠ on `conduit doctor`'s **embedding model stamp** line,
which reports exactly this state; or use a registry model.

A width change is always caught, whatever the model is called.

### An upgraded knowledge base's model is assumed, not known

Knowledge bases indexed before 2.0.0-beta.5 have vectors and no record of what
produced them. On first open, Conduit assumes the currently configured model
built them, provided the vector width matches, and logs one line saying so.
`conduit doctor` marks the stamp `(assumed: ...)`.

If you changed embedding model *before* upgrading, that assumption is wrong and
nothing on disk could reveal it. Run `conduit kb sync --rebuild-vectors` once
after upgrading if that applies to you.

### No data migration from Conduit 1.x

A v1 knowledge base cannot be read by v2, and no converter exists or is planned.
Re-add your sources and re-index. Source documents are untouched; only derived
data is rebuilt.

### Open issues

Additional tracked work, including issues
[#85](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/85),
[#86](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/86) and
[#87](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/87), is
tracked on the
[issue tracker](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues).
Check there for current status rather than relying on this file.

---

## Historical: Conduit 1.x issues

> **HISTORICAL — these describe Conduit 1.x, retired August 2026.** They are
> retained because 1.x installs still exist in the wild. Neither applies to
> Conduit 2.0, which has no containers, no Qdrant and no FalkorDB.

### KB-001: Silent Fallback to FTS When Qdrant Fails

**Affects**: v0.1.41 and earlier · **Status**: Mitigated in v0.1.42+ ·
**Issue**: [#41](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/41)

When Qdrant was unavailable during `conduit kb sync`, the system silently fell
back to FTS5-only indexing and reported success, leaving semantic search
degraded with no warning.

Mitigated in v0.1.42+ by exit code 2 on partial success, a warning with
remediation steps, and the `--rebuild-vectors` flag. On 1.x, diagnose with
`conduit status` (a `0 vectors` reading means FTS5 works but semantic search
does not), restart Qdrant, then `conduit kb sync --rebuild-vectors`.

Conduit 2.0 keeps the honest exit code and drops the cause: there is no Qdrant
to fail.

### KB-002: Container Storage Mount Issues

**Affects**: First-time 1.x installations · **Status**: Documented workaround

Qdrant or FalkorDB containers could fail to initialise storage when data
directories were created after the container started, logging
`Can't create directory for collection conduit_kb`. Restarting the affected
container remounted the volumes correctly.

Conduit 2.0 has no containers.

---

## Reporting new issues

If you encounter an issue not listed here:

1. Check the [GitHub Issues](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues) for existing reports
2. Run `conduit doctor` and include the output (`--json` is fine)
3. Include `conduit version` and `conduit status`
4. Create a new issue with reproduction steps

For MCP integration problems, set `mcp.kb.logging.to_stderr: true` and include
the server log from your AI client.
