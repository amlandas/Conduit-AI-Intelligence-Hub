# Known Issues and Workarounds

This document lists known issues in Conduit along with their workarounds.

---

## SEC-001: Knowledge Base Data Stores Exposed to the Local Network (SECURITY)

**Severity**: High
**Affects**: All releases up to and including v1.0.42 / desktop v0.1.43
**Status**: Fixed in source (2026-07); existing installs must apply the workaround below

### Description

The Qdrant (ports 6333/6334) and FalkorDB (port 6379) containers created by the
installer, the CLI, and the daemon publish their ports on **all network
interfaces** (`0.0.0.0`), and neither service is configured with
authentication. Any device on the same network can read, modify, or delete the
entire vector store and knowledge graph, bypassing the daemon's Unix-socket
security and policy engine. The Qdrant HTTP API is additionally reachable from
malicious webpages via DNS rebinding.

### Workaround (existing installs)

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

## SEC-002: Desktop App (Electron GUI) Is Unsupported — Do Not Use (SECURITY)

**Severity**: High
**Affects**: All desktop releases (v0.1.0–v0.1.43)
**Status**: Development halted; no fix planned

### Description

Desktop GUI development is halted. The published DMGs are unsigned, run an
end-of-life Electron/Chromium version with years of unpatched CVEs, and contain
an IPC handler (`terminal:spawn`) that allows the renderer process to execute
arbitrary shell commands, meaning any renderer compromise leads to code
execution as the user.

### Workaround

Uninstall the desktop app and use the CLI, which is the source of truth for all
functionality:

```bash
rm -rf /Applications/Conduit.app
```

All features remain available via `conduit` commands (see docs/CLI reference).

---

## SEC-003: Indexed Documents Flow Verbatim to AI Clients (Prompt-Injection Caveat)

**Severity**: Informational (by design)
**Affects**: All versions
**Status**: Documented behavior

### Description

By design ("no LLM in the hot path"), MCP search tools return raw chunks of
your indexed documents directly to the connected AI client. If you index
untrusted content (third-party PDFs, scraped pages, shared docs), instructions
embedded in that content will reach your AI assistant as tool output and may
influence its behavior (prompt injection). Conduit sanitizes input to its own
entity-extraction pipeline, but does not — and cannot meaningfully — sanitize
what your AI client chooses to trust.

### Recommendation

Only index content you trust, treat KB search results in agent transcripts
with the same skepticism as web content, and prefer AI clients that attribute
tool output distinctly from user instructions.

---

## KB-001: Silent Fallback to FTS When Qdrant Fails

**Severity**: Medium
**Affects**: v0.1.41 and earlier
**Status**: Mitigated in v0.1.42+
**GitHub Issue**: [#41](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/41)

### Description

When Qdrant is unavailable or experiencing errors during `conduit kb sync`, the system silently falls back to FTS5 (keyword-only) indexing. Users are not warned that semantic search is degraded, and the sync reports success.

This can happen when:
- Qdrant container has storage mount issues (directories created after container start)
- Qdrant is temporarily unavailable
- Ollama embedding model fails

### How to Detect

Run `conduit status` and check the Vector Database line:

```bash
conduit status
```

If you see:
```
Vector Database: ✓ Qdrant (missing, 0 vectors)
```

This indicates FTS5 is working but Qdrant has 0 vectors - semantic search is not functional.

### Workaround

**Step 1**: Diagnose the issue
```bash
conduit doctor
```

**Step 2**: Restart Qdrant (fixes most container issues)
```bash
conduit qdrant stop
conduit qdrant start
```

**Step 3**: Rebuild vectors for all sources
```bash
conduit kb sync --rebuild-vectors
```

Or for a specific source:
```bash
conduit kb sync <source-id> --rebuild-vectors
```

**Step 4**: Verify vectors were created
```bash
conduit status
```

You should see non-zero vector count:
```
Vector Database: ✓ Qdrant (running, 195 vectors)
```

### Mitigation in v0.1.42+

- Exit code 2 returned when sync completes with semantic indexing failures
- Clear warning message with remediation steps
- `--rebuild-vectors` flag to force re-indexing

---

## KB-002: Container Storage Mount Issues

**Severity**: Low
**Affects**: First-time installations
**Status**: Documented workaround

### Description

When Qdrant or FalkorDB containers are installed, they may fail to initialize storage properly if the data directories were created after the container started.

The daemon logs will show errors like:
```
Can't create directory for collection conduit_kb. Error: failed to create directory `./storage/collections/conduit_kb`: No such file or directory
```

### Workaround

Restart the affected container:

```bash
# For Qdrant
conduit qdrant stop
conduit qdrant start

# For FalkorDB
conduit falkordb stop
conduit falkordb start
```

The container will properly mount the storage volumes on restart.

---

## Reporting New Issues

If you encounter an issue not listed here:

1. Check the [GitHub Issues](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues) for existing reports
2. Run `conduit doctor` and include the output in your report
3. Include relevant daemon logs: `conduit daemon logs`
4. Create a new issue with reproduction steps

---

**Last Updated**: January 2026
