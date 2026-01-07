# Known Issues and Workarounds

This document lists known issues in Conduit along with their workarounds.

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
