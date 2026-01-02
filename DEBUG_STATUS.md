# Conduit KB Sync - Debug Status

**Date:** January 1, 2026
**Status:** RESOLVED

---

## Problem Summary

`conduit kb sync` was not properly indexing documents (PDFs, text files) on remote machines when installed via the install script. Multiple root causes were identified and fixed.

---

## All Issues Resolved

### Phase 1: Initial Fixes (Early January 1)

| Issue | Root Cause | Fix | Commit |
|-------|------------|-----|--------|
| Pattern corruption | Database stored `.pdf` instead of `*.pdf` | `normalizePatterns()` in source.go | `9d147e5` |
| Chunk ID collisions | Same file in different dirs caused UNIQUE failures | `generateUniqueChunkID()` in indexer.go | `41ed74f` |
| CGO build failures | Xcode CLT not checked before build | CGO prerequisites check in install.sh | `fdea3bb` |
| Daemon persistence | Old daemon process persisted after reinstall | Daemon cleanup in install.sh | `6b10253` |

### Phase 2: Service Configuration (Mid January 1)

| Issue | Root Cause | Fix | Commit |
|-------|------------|-----|--------|
| Tools not found by daemon | launchd/systemd services missing PATH | Added PATH to service configurations | `e398ab9` |
| CLI panic on sync | Unsafe type assertions in response handling | Nil-safe type assertions | `bed1cc9` |

### Phase 3: Vector Store & Container Issues (Late January 1)

| Issue | Root Cause | Fix | Commit |
|-------|------------|-----|--------|
| Qdrant container fails to start | `docker-credential-gcloud` not in PATH during SSH/launchd | Temporarily disable credential helpers | `4b06a53` |
| Qdrant storage broken after reinstall | Container volume mount invalid after `~/.conduit` removal | Always recreate container | `4b06a53` |
| Orphaned container after uninstall | Uninstall script didn't remove Qdrant container | Added `remove_qdrant_container` step | `4b06a53` |
| Panic on invalid UTF-8 | Qdrant client panics on non-UTF-8 strings | `sanitizeUTF8()` in vectorstore.go | `4b06a53` |

---

## Files Modified

| File | Changes |
|------|---------|
| `internal/kb/source.go` | Pattern normalization, SetSemanticSearcher |
| `internal/kb/indexer.go` | Unique chunk IDs, semantic search integration |
| `internal/kb/vectorstore.go` | UTF-8 sanitization for Qdrant payloads |
| `cmd/conduit/main.go` | Nil-safe type assertions in sync response |
| `scripts/install.sh` | PATH in services, CGO check, credential helper bypass, container recreation |
| `scripts/uninstall.sh` | Qdrant container removal step |

---

## Verification

Successfully tested on remote macOS machine (192.168.1.60):

```bash
# Fresh install
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash

# Add source and sync
conduit kb add /path/to/documents
conduit kb sync

# Results
# - 5 documents indexed
# - 773 chunks created
# - 773 vectors in Qdrant
# - Semantic search returning relevant results
```

---

## Hybrid Search Implementation

**Status:** COMPLETE

### Search Modes

| Mode | Flag | Description |
|------|------|-------------|
| Hybrid (default) | none | Tries semantic first, falls back to FTS5 if unavailable |
| Semantic | `--semantic` | Vector-based search only (requires Qdrant + Ollama) |
| Keyword | `--fts5` | Full-text keyword search only (always available) |

### Architecture

```
DOCUMENT INGESTION
───────────────────────────────────────────────────────────
Document → Extract Text → Chunk → Ollama (nomic-embed-text)
                                         ↓
                                  768-dim vectors
                                         ↓
                                  Store in Qdrant + FTS5

QUERY FLOW
───────────────────────────────────────────────────────────
User Query → Ollama (embed) → Vector Search → Ranked Results
         ↘                                   ↗
           FTS5 fallback if Qdrant unavailable
```

### Technology Stack

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Vector DB | Qdrant | Single binary, easy self-host, excellent Go client |
| Embedding Model | nomic-embed-text | Ollama-hosted, 768 dims, MIT licensed |
| FTS Fallback | SQLite FTS5 | Always available, no external dependencies |

---

## Usage

```bash
# Search (hybrid by default)
conduit kb search "machine learning"

# Force semantic only
conduit kb search "NLP concepts" --semantic

# Force keyword only
conduit kb search "deployment" --fts5

# Migrate existing documents to vector store
conduit kb migrate
```

---

## Key Lessons Learned

1. **Background services need explicit PATH** - launchd/systemd don't inherit user's shell environment
2. **Docker credential helpers can block operations** - need to bypass when not in interactive terminal
3. **Container volume mounts break when host directory is deleted** - always recreate container
4. **UTF-8 validation is critical for external services** - Qdrant requires valid UTF-8
5. **Graceful fallback hides failures** - need to check logs, not just CLI output

---

**Last Updated:** January 1, 2026
