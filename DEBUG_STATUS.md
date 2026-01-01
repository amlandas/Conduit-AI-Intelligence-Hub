# Conduit KB Sync - Debug Status

**Date:** January 1, 2026
**Issue:** PDF files not syncing on remote machine via install script

---

## Problem Summary

`conduit kb sync` works correctly on local machine but fails on remote machine when installed via the install script. Text files (.txt, .md) sync fine, but PDF files are not being indexed.

---

## What Works (Local Machine)

When building manually with these exact commands, everything works:

```bash
CGO_ENABLED=1 go build -tags "fts5" -o conduit ./cmd/conduit
CGO_ENABLED=1 go build -tags "fts5" -o conduit-daemon ./cmd/conduit-daemon
```

- PDF files are matched by patterns
- pdftotext extracts content (43,185 chars from test PDF)
- Content is indexed and searchable via `conduit kb search`

---

## Root Causes Identified & Fixed

### 1. Pattern Normalization (Commit 9d147e5)
**Problem:** Database stored patterns as `.pdf` instead of `*.pdf`
**Fix:** Added `normalizePatterns()` function in `internal/kb/source.go` that auto-corrects patterns on load
**Status:** FIXED and pushed to GitHub

### 2. Chunk ID Collisions (Commit 41ed74f)
**Problem:** Same PDF in different directories caused UNIQUE constraint failures
**Fix:** Added `generateUniqueChunkID()` in `internal/kb/indexer.go` that includes document ID in hash
**Status:** FIXED and pushed to GitHub

### 3. CGO Prerequisites Check (Commit fdea3bb)
**Problem:** Build might fail silently if Xcode Command Line Tools not installed
**Fix:** Added explicit check for Xcode/gcc before building in install script
**Status:** FIXED and pushed to GitHub

### 4. Daemon Cleanup on Reinstall (Commit 6b10253)
**Problem:** Old daemon process might persist after reinstall
**Fix:** Added `pkill`, socket cleanup, and plist verification to install script
**Status:** FIXED and pushed to GitHub

---

## What's Still Failing

Remote machine still shows 0 documents added for PDFs after running install script, despite all fixes being pushed to GitHub.

---

## Diagnostic Commands to Run on Remote Machine

```bash
# 1. Check which conduit-daemon binary is running
ps aux | grep conduit-daemon

# 2. Check the plist path
cat ~/Library/LaunchAgents/com.simpleflo.conduit.plist | grep -A1 ProgramArguments

# 3. Check daemon log for errors
cat ~/.conduit/daemon.log | tail -50

# 4. Verify pdftotext works
pdftotext -layout "/path/to/test.pdf" - | head -10

# 5. Run sync with debug logging
CONDUIT_LOG_LEVEL=debug conduit kb sync 2>&1 | tee /tmp/sync-debug.log

# 6. Check patterns in database
sqlite3 ~/.conduit/conduit.db "SELECT patterns FROM kb_sources"

# 7. Check binary version/location
which conduit-daemon
ls -la $(which conduit-daemon)

# 8. Verify FTS5 is compiled in (should show sqlite_fts5)
strings $(which conduit-daemon) | grep -i fts5 | head -5
```

---

## Possible Issues to Investigate

1. **Binary Location Mismatch**
   - Install script puts binaries in `~/.local/bin/`
   - Plist might be pointing to different location
   - Check: `cat ~/Library/LaunchAgents/com.simpleflo.conduit.plist`

2. **Old Binary Still Being Used**
   - Even after reinstall, PATH might resolve to old binary
   - Check: `which conduit-daemon` vs `~/.local/bin/conduit-daemon`

3. **Database Not Getting Fixed**
   - Pattern normalization happens at read-time, but maybe sync is failing before that
   - Try: Delete and re-add the source folder

4. **Install Script Not Actually Rebuilding**
   - Might be using cached clone or failing silently
   - Run install script with verbose output and check for errors

5. **Daemon Not Starting with New Binary**
   - Check `launchctl list | grep conduit` for exit status
   - Check `~/.conduit/daemon.log` for startup errors

---

## Files Modified

| File | Changes |
|------|---------|
| `internal/kb/source.go` | Added `normalizePatterns()` function, called in `List()` and `Get()` |
| `internal/kb/indexer.go` | Added `generateUniqueChunkID()` function |
| `scripts/install.sh` | CGO check, daemon cleanup, plist verification |

---

## Git Commits (in order)

1. `41ed74f` - fix(kb): resolve chunk ID collisions and improve sync logging
2. `9d147e5` - fix(kb): normalize corrupted file patterns on load
3. `fdea3bb` - fix(install): add CGO prerequisites check and build verification
4. `6b10253` - fix(install): robust daemon cleanup before reinstall

---

## Next Steps for Tomorrow

1. Run diagnostic commands above on remote machine
2. Share output of `~/.conduit/daemon.log` and sync debug log
3. Verify the binary being run is the newly built one
4. Consider completely fresh install:
   ```bash
   # Nuclear option - remove everything
   pkill -f conduit
   rm -rf ~/.conduit
   rm -f ~/Library/LaunchAgents/com.simpleflo.conduit.plist
   rm -f ~/.local/bin/conduit*

   # Fresh install
   curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
   ```

5. If still failing, add more debug logging to the sync process to see exactly where it's stopping

---

## Test Files

- Local test directory: `/Users/amlandas/Documents/test`
- Contains: `AERO_Q209_article05.pdf` (269,614 bytes)
- pdftotext extracts 43,185 characters from this PDF successfully

---

## IMPLEMENTED: Hybrid Search (Semantic + FTS5)

**Status:** ✅ COMPLETE (Commits 06093b7, 37d7b42, January 1, 2026)

**Decision:** Implemented hybrid search with Qdrant vector database + SQLite FTS5 with graceful fallback.

### Search Modes

| Mode | Flag | Description |
|------|------|-------------|
| Hybrid (default) | none | Tries semantic first, falls back to FTS5 if unavailable |
| Semantic | `--semantic` | Vector-based search only (requires Qdrant + Ollama) |
| Keyword | `--fts5` | Full-text keyword search only (always available) |

### Key Fixes Applied

1. **SourceManager-Indexer Wiring** - SourceManager's internal indexer now receives SemanticSearcher
2. **UUID Conversion for Qdrant** - Chunk IDs converted to UUID v5 format (Qdrant requires UUIDs)
3. **Background Context for Migration** - Long-running operations use `context.Background()` to avoid HTTP timeouts
4. **10-Minute Client Timeout** - Migration command uses extended timeout for large document sets

### Why This Change

| SQLite FTS5 (kept for fallback) | Qdrant + Embeddings (new) |
|---------------------------------|---------------------------|
| Keyword-based search only | Semantic search (understands meaning) |
| CGO build requirement | No CGO needed for vector search |
| "deployment" won't find "CI/CD" | "deployment" finds related concepts |
| Limited RAG support | Full RAG capability for AI clients |

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     DOCUMENT INGESTION                          │
├─────────────────────────────────────────────────────────────────┤
│  Document → Extract Text → Chunk → Ollama (nomic-embed-text)   │
│                                           ↓                     │
│                                    768-dim vectors              │
│                                           ↓                     │
│                                    Store in Qdrant              │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│                        QUERY FLOW                               │
├─────────────────────────────────────────────────────────────────┤
│  User Query → Ollama (embed) → Vector Search → Ranked Results  │
└─────────────────────────────────────────────────────────────────┘
```

### Technology Choices

| Component | Choice | Rationale |
|-----------|--------|-----------|
| **Vector DB** | Qdrant | Single binary, easy self-host, excellent Go client, simpler than Milvus |
| **Embedding Model** | nomic-embed-text | Available on Ollama, 768 dims, good quality, MIT licensed |
| **Alternative Models** | e5-base, BGE-base-en-v1.5 | Can be swapped based on performance needs |

### Installation Requirements

**Fully managed installation experience:**
- User should NOT need to input anything except interactive choices
- Qdrant: Auto-install via Docker or direct binary download
- Ollama embedding model: Auto-pull during first sync
- All configuration auto-generated with sensible defaults

### Implementation Status (Completed)

1. **✅ Add Qdrant dependency**
   - Go client: `github.com/qdrant/go-client`
   - Auto-install Qdrant via Docker in install script

2. **✅ Add embedding service**
   - `internal/kb/embeddings.go` - Ollama integration
   - Auto-pull `nomic-embed-text` model on first use

3. **✅ Refactor KB module**
   - `internal/kb/vectorstore.go` - Qdrant client wrapper
   - `internal/kb/semantic_search.go` - High-level semantic search API
   - `internal/kb/indexer.go` - Added `SetSemanticSearcher()` for optional vector indexing
   - Document extraction (pdftotext, etc.) unchanged

4. **✅ Update install script**
   - `install_qdrant()` - Docker-based Qdrant installation
   - `install_embedding_model()` - nomic-embed-text auto-pull
   - Verification steps for semantic search components

5. **✅ Migration path**
   - `SemanticSearcher.MigrateFromFTS()` method for existing data
   - SQLite kept for metadata, Qdrant for vectors

### Benefits Achieved

- **No CGO issues** - Qdrant client is pure Go
- **True semantic search** - Find documents by meaning
- **RAG-ready** - Perfect for AI client augmentation
- **Local-first** - All processing on user's machine
- **Graceful degradation** - FTS5 still works if vector services unavailable

### Files Created/Modified

| File | Status | Description |
|------|--------|-------------|
| `internal/kb/embeddings.go` | ✅ NEW | EmbeddingService for Ollama (768-dim vectors) |
| `internal/kb/vectorstore.go` | ✅ NEW | VectorStore for Qdrant (cosine similarity, UUID conversion) |
| `internal/kb/semantic_search.go` | ✅ NEW | SemanticSearcher combining both |
| `internal/kb/indexer.go` | ✅ MODIFIED | Added optional semantic indexing |
| `internal/kb/source.go` | ✅ MODIFIED | Added `SetSemanticSearcher()` method |
| `internal/daemon/daemon.go` | ✅ MODIFIED | Wire SemanticSearcher to SourceManager |
| `internal/daemon/handlers.go` | ✅ MODIFIED | Added `handleKBMigrate` with background context |
| `cmd/conduit/main.go` | ✅ MODIFIED | Added `kb migrate` command, `--semantic`/`--fts5` flags |
| `scripts/install.sh` | ✅ MODIFIED | Added Qdrant and embedding model installation |
| `go.mod` | ✅ MODIFIED | Added Qdrant and Ollama dependencies |

### Usage

```bash
# Start Qdrant
docker run -d -p 6333:6333 -p 6334:6334 qdrant/qdrant

# Pull embedding model
ollama pull nomic-embed-text

# Search commands (semantic search automatically enabled when services available)
conduit kb search "machine learning"           # Hybrid (default) - tries semantic, falls back to FTS5
conduit kb search "NLP concepts" --semantic    # Force semantic only (requires Qdrant + Ollama)
conduit kb search "deployment" --fts5          # Force keyword only (always available)

# Migrate existing FTS documents to vector store
conduit kb migrate                             # Adds embeddings to existing indexed documents
```

### Documents Updated

- ✅ HLD V0 - Vector search architecture added
- ✅ HLD V0.5 - Security considerations for embedding service
- ✅ HLD V1 - GUI integration with semantic search
- ✅ low-level-plan/08-KB-ARCHITECTURE.md - Complete rewrite for vector approach
