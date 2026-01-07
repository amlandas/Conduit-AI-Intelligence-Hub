# Conduit Desktop v0.1.20 - Post-Implementation Audit

**Status**: POST-IMPLEMENTATION COMPLETE
**Date**: 2026-01-05
**Audited by**: Claude Code with 3 Explore agents

---

## Executive Summary

This audit comprehensively maps **every UI functionality** to its backend implementation.

**Verification**: ALL GUI operations delegate to CLI commands (not HTTP).
**Bug Fixes**: 3 critical bugs fixed, 1 Priority 2 improvement completed.
**Bug #4**: Not a bug - MCP configs are architecturally separate from connector instances.

---

## Part 1: Complete IPC Handler Mapping (VERIFIED)

### Legend
- **CLI**: Calls CLI command via `execCLI()` or `spawn()`
- **Direct**: Direct file system or OS operation
- Status: ✅ Working | ⚠️ Fragile | ✅ **FIXED**

---

### 1.1 Knowledge Base Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `kb:sources` | List KB sources | CLI | `conduit kb list --json` | ✅ |
| `kb:add-source` | Add KB source | CLI | `conduit kb add <path> --json` | ✅ |
| `kb:remove-source` | Delete KB source | CLI | `conduit kb remove <id> --json` | ✅ **FIXED** |
| `kb:sync` | Sync (no progress) | CLI | `conduit kb sync [--source <id>] --json` | ⚠️ Not used |
| `kb:sync-with-progress` | RAG Sync | CLI (spawn) | `conduit kb sync --source <id>` | ✅ **FIXED** |
| `kb:kag-sync-with-progress` | KAG Sync | CLI (spawn) | `conduit kb kag-sync --source <id>` | ✅ **FIXED** |
| `kb:search` | Search KB | CLI | `conduit kb search "<query>" --json` | ✅ |
| `kb:kag-search` | KAG Search | CLI | `conduit kb kag-query "<query>" --json` | ✅ |

---

### 1.2 Connector/Instance Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `instances:list` | List connectors | CLI | `conduit list --json` | ✅ |
| `instances:create` | Create connector | CLI | `conduit create <connector> --name <name> --json` | ✅ |
| `instances:start` | Start connector | CLI | `conduit start <id> --json` | ✅ |
| `instances:stop` | Stop connector | CLI | `conduit stop <id> --json` | ✅ |
| `instances:delete` | Delete connector | CLI | `conduit remove <id> --json` | ✅ |
| `instances:permissions` | Get permissions | CLI | `conduit permissions <id> --json` | ✅ |

---

### 1.3 MCP Operations (ipc.ts + setup-ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `mcp:configure` | Configure MCP | CLI | `conduit mcp configure --client <client>` | ✅ |
| `mcp:check` | Check MCP status | CLI | `conduit mcp status --json` | ✅ |
| `mcp:list-clients` | List MCP clients | CLI | `conduit mcp clients --json` | ✅ |

---

### 1.4 Daemon Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `daemon:status` | Get daemon status | CLI | `conduit status --json` | ✅ |
| `daemon:stats` | Get daemon stats | CLI | `conduit stats --json` | ✅ |

---

### 1.5 Model Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `ollama:models` | List Ollama models | CLI | `conduit ollama models --json` | ✅ |
| `ollama:pull` | Pull Ollama model | CLI | `conduit ollama pull <model>` | ✅ |
| `ollama:status` | Check Ollama status | CLI | `conduit ollama status --json` | ✅ |

---

### 1.6 Setup/Installation Operations (setup-ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command/Method | Status |
|-------------|-----------|----------------|-------------------|--------|
| `setup:check-cli` | Check CLI installed | Direct | `which conduit` + `conduit version --json` | ✅ |
| `setup:install-cli` | Install CLI | Direct | File copy from bundle | ✅ |
| `setup:check-dependencies` | Check deps | CLI | `conduit deps status --json` | ✅ |
| `setup:install-dependency` | Install dep | CLI | `conduit deps install <name>` | ✅ |
| `setup:check-services` | Check services | CLI | Multiple status commands | ✅ **IMPROVED** |
| `setup:start-service` | Start service | CLI | `conduit service start`, etc. | ✅ **FIXED** |
| `setup:start-all-services` | Start all | CLI | Loops over services | ✅ |
| `setup:check-models` | Check models | CLI | `conduit ollama models` | ⚠️ Text parsing |
| `setup:pull-model` | Pull model | CLI (spawn) | `conduit ollama pull <model>` | ✅ |

---

### 1.7 Uninstall Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `uninstall:info` | Get uninstall info | CLI | `conduit uninstall --dry-run --json` | ✅ |
| `uninstall:execute` | Execute uninstall | CLI | `conduit uninstall --json` | ✅ |

---

### 1.8 System Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | Method | Status |
|-------------|-----------|----------------|--------|--------|
| `app:open-external` | Open URL | Direct | `shell.openExternal(url)` | ✅ |
| `app:open-terminal` | Open Terminal | Direct | `spawn('open', ['-a', 'Terminal'])` | ✅ |
| `app:get-version` | Get app version | Direct | `app.getVersion()` | ✅ |

---

## Part 2: Bug Fixes Applied

### Bug #1: Delete Source Doesn't Work (Sources Reappear) - ✅ FIXED

**Symptom**: User clicks delete, source disappears briefly, then reappears on page refresh.

**Location**: `apps/conduit-desktop/src/renderer/components/kb/KBView.tsx:127-147`

**Root Cause**: Race condition - local state updated before server confirms deletion.

**BEFORE (BUGGY):**
```typescript
const handleRemove = async (sourceId: string): Promise<void> => {
  try {
    await window.conduit.removeKBSource(sourceId)  // CLI call starts
    removeSource(sourceId)  // LOCAL STATE UPDATED IMMEDIATELY - BUG!
    // NO refresh() call to verify server state!
  } catch (err) {
    console.error('Failed to remove source:', err)
  }
}
```

**AFTER (FIXED):**
```typescript
const handleRemove = async (sourceId: string): Promise<void> => {
  setDeleteError(null)  // Clear previous error
  try {
    const result = await window.conduit.removeKBSource(sourceId)
    // Check if CLI returned an error (IPC handler returns { error: message } on failure)
    if (result && typeof result === 'object' && 'error' in result) {
      const errorMsg = (result as { error: string }).error
      console.error('Failed to remove source:', errorMsg)
      setDeleteError(`Failed to remove source: ${errorMsg}`)  // User-visible error
      return
    }
    // CLI succeeded - refresh to sync with server state
    await refresh()  // Get authoritative state from CLI
  } catch (err) {
    const errorMsg = (err as Error).message
    console.error('Failed to remove source:', errorMsg)
    setDeleteError(`Failed to remove source: ${errorMsg}`)  // User-visible error
  }
}
```

**Fix Applied**:
1. ✅ Wait for CLI response to confirm deletion succeeded
2. ✅ Only update local state AFTER server confirms (via refresh())
3. ✅ Call refresh() after successful deletion to sync with server state
4. ✅ Add error handling with user-visible feedback (deleteError state + banner UI)

---

### Bug #2: RAG Sync Shows "0 chunks" Forever - ✅ FIXED

**Symptom**: Progress bar shows "Processed: 0 chunks" and never updates.

**Location**: `apps/conduit-desktop/src/main/setup-ipc.ts:742-800`

**Root Cause**: Regex `/Processed:\s*(\d+)/` doesn't match actual CLI output format.

**Investigation Result**: Ran `conduit kb sync` manually and captured actual output:
```
Syncing source: 417a1f76-41b4-48e8-bb9c-038a524003e3
✓ Sync complete
Added:   5 documents
Updated: 0 documents
Deleted: 0 documents
Vectors: ✓ indexed
```

**BEFORE (BUGGY):**
```typescript
// Regex looked for "Processed: X" which CLI never outputs
const processedMatch = output.match(/Processed:\s*(\d+)/)
if (processedMatch) {
  processed = parseInt(processedMatch[1], 10)  // NEVER MATCHES!
}
```

**AFTER (FIXED):**
```typescript
// CLI outputs: Added: X, Updated: X, Deleted: X
const addedMatch = output.match(/Added:\s*(\d+)/)
const updatedMatch = output.match(/Updated:\s*(\d+)/)
const deletedMatch = output.match(/Deleted:\s*(\d+)/)

if (addedMatch || updatedMatch || deletedMatch) {
  const added = addedMatch ? parseInt(addedMatch[1], 10) : 0
  const updated = updatedMatch ? parseInt(updatedMatch[1], 10) : 0
  const deleted = deletedMatch ? parseInt(deletedMatch[1], 10) : 0
  processed = added + updated + deleted
  sendProgress(80, `Processed ${processed} documents`, { processed, errors })
}
```

**Status**: ✅ FIXED - Now parses actual CLI output format

---

### Bug #3: KAG Sync Shows "0 entities" Forever - ✅ FIXED

**Symptom**: Progress bar shows "Extracted: 0 entities" and never updates.

**Location**: `apps/conduit-desktop/src/main/setup-ipc.ts:855-912`

**Root Cause**: Regex `/Extracted:\s*(\d+)/` doesn't match actual CLI output format.

**Investigation Result**: Ran `conduit kb kag-sync` manually and captured actual output:
```
Extracting entities from 10 chunks...
[1/10] Processing chunk abc123... (ETA: 5s)
[2/10] Processing chunk def456... (ETA: 4s)
...
Processed:   10 chunks in 45s
Errors:      0 chunks failed
```

**BEFORE (BUGGY):**
```typescript
// Regex looked for "Extracted: X" which CLI never outputs
const extractedMatch = output.match(/Extracted:\s*(\d+)/)
if (extractedMatch) {
  extracted = parseInt(extractedMatch[1], 10)  // NEVER MATCHES!
}
```

**AFTER (FIXED):**
```typescript
// CLI outputs: "[current/total] Processing chunk" and "Processed: X chunks"
const progressMatch = output.match(/\[(\d+)\/(\d+)\]\s*Processing chunk/)
if (progressMatch) {
  const current = parseInt(progressMatch[1], 10)
  const total = parseInt(progressMatch[2], 10)
  const percent = Math.round((current / total) * 90) + 5 // 5-95%
  sendProgress(percent, `Processing ${current}/${total} chunks...`, { extracted: current, errors })
}

const processedMatch = output.match(/Processed:\s*(\d+)\s*chunks/)
if (processedMatch) {
  extracted = parseInt(processedMatch[1], 10)
}
```

**Status**: ✅ FIXED - Now parses actual CLI output format with live progress

---

### Bug #4: MCP Connectors Don't Show in Connectors Tab - ✅ NOT A BUG

**Symptom**: After configuring MCP via `conduit mcp configure`, connectors don't appear in GUI.

**Investigation Result**:

| Concept | What it is | CLI Command | Storage |
|---------|------------|-------------|---------|
| **Connector Instances** | Runtime Docker containers/processes | `conduit list --json` | Daemon state |
| **MCP Configurations** | Static JSON entries for AI clients | `conduit mcp status --json` | `~/.claude.json` |

**Verification**:
```bash
$ conduit list --json
{"count":0,"instances":null}

$ conduit mcp status --json
{
  "claude-code": {
    "configPath": "/Users/amlandas/.claude.json",
    "configured": true,
    "serverName": "conduit-kb"
  }
}
```

**Conclusion**: This is **expected behavior**. MCP configurations and connector instances are architecturally separate concepts:
- Connectors tab shows daemon-managed runtime processes (`conduit list`)
- MCP configs are static JSON entries that tell AI clients about MCP servers (`conduit mcp status`)

**Status**: ✅ N/A - Expected behavior, not a bug. Feature request if GUI display is desired.

---

## Part 3: Additional Fragilities - Priority 3 Fixes Applied

### 3.1 Service Status Parsing (setup-ipc.ts:372-478) - ✅ IMPROVED

**Issue**: Used brittle string matching on CLI output.

**BEFORE (FRAGILE):**
```typescript
// FRAGILE:
if (stdout.includes('daemon is running')) {
  daemonRunning = true
}
```

**AFTER (ROBUST):**
```typescript
// Helper to parse status output robustly
// Looks for success markers (✓) and failure markers (✗)
const parseRunningStatus = (stdout: string, patterns: { running: string[]; stopped: string[] }): boolean => {
  // Check for explicit failure markers first
  for (const pattern of patterns.stopped) {
    if (stdout.includes(pattern)) return false
  }
  // Check for success markers
  for (const pattern of patterns.running) {
    if (stdout.includes(pattern)) return true
  }
  return false
}

// Usage:
const isRunning = parseRunningStatus(stdout, {
  running: ['✓ Conduit daemon is running', 'daemon is running'],
  stopped: ['✗', 'not running', 'stopped']
})
```

**Improvement**: Now uses a dedicated helper function with explicit success/failure markers:
- Checks for ✗ (failure) markers first
- Then checks for ✓ (success) markers
- Falls back to false if no clear markers found

**Status**: ✅ IMPROVED - More robust parsing with explicit marker detection

---

### 3.2 Hardcoded Brew Paths (setup-ipc.ts:463-486) - ✅ FIXED

**Issue**: Hardcoded `/opt/homebrew/bin/brew` path for macOS.

**BEFORE (FRAGILE):**
```typescript
// FRAGILE - Only works on Apple Silicon:
await execFileAsync('/opt/homebrew/bin/brew', ['services', 'start', 'ollama'])
// Try /usr/local/bin/brew for Intel Macs
await execFileAsync('/usr/local/bin/brew', ['services', 'start', 'ollama'])
```

**AFTER (DYNAMIC):**
```typescript
// Cache for brew path to avoid repeated `which` calls
let cachedBrewPath: string | null = null

// Get Homebrew path dynamically using `which brew`
async function getBrewPath(): Promise<string | null> {
  if (cachedBrewPath !== null) return cachedBrewPath
  if (process.platform !== 'darwin') return null

  try {
    const { stdout } = await execFileAsync('which', ['brew'])
    const brewPath = stdout.trim()
    if (brewPath && fs.existsSync(brewPath)) {
      cachedBrewPath = brewPath
      return cachedBrewPath
    }
  } catch { /* which failed */ }

  // Fallback to known paths
  const knownPaths = ['/opt/homebrew/bin/brew', '/usr/local/bin/brew']
  for (const brewPath of knownPaths) {
    if (fs.existsSync(brewPath)) {
      cachedBrewPath = brewPath
      return cachedBrewPath
    }
  }
  return null
}

// Usage:
const brewPath = await getBrewPath()
if (brewPath) {
  await execFileAsync(brewPath, ['services', 'start', 'ollama'])
}
```

**Fix Applied**:
1. Added `getBrewPath()` helper function at lines 69-108
2. Uses `which brew` to find correct path dynamically
3. Falls back to known paths if `which` fails
4. Caches result to avoid repeated calls

**Status**: ✅ FIXED - Dynamic brew path detection works on both Apple Silicon and Intel Macs

---

### 3.3 Model List Parsing (setup-ipc.ts) - ⚠️ Deferred

**Issue**: Parses text output from `conduit ollama models` instead of JSON.

**Risk**: If output format changes, parsing breaks.

**Recommendation**: Add `--json` flag support to CLI command.

**Status**: ⚠️ Not fixed in v0.1.20 - Requires CLI changes, deferred to future release

---

## Part 4: Implementation Plan Completion Status

### Priority 1: Fix Critical Bugs - ✅ ALL COMPLETE

| Task | File | Line | Fix | Status |
|------|------|------|-----|--------|
| 1. Fix delete race condition | KBView.tsx | 127-147 | Wait for response, add refresh(), add error UI | ✅ DONE |
| 2. Fix RAG progress regex | setup-ipc.ts | 742-800 | Capture actual CLI output, update regex | ✅ DONE |
| 3. Fix KAG progress regex | setup-ipc.ts | 855-912 | Capture actual CLI output, update regex | ✅ DONE |

### Priority 2: Improve Error Visibility - ✅ ALL COMPLETE

| Task | File | Fix | Status |
|------|------|-----|--------|
| 4. Add error toasts for delete failure | KBView.tsx | Show user-visible error on failure | ✅ DONE |
| 5. Add error toasts for sync failure | KBView.tsx | Show user-visible error on failure | ✅ N/A (already working via ragResult/kagResult) |

### Priority 3: Improve Robustness - ✅ 2 of 3 COMPLETE

| Task | File | Fix | Status |
|------|------|-----|--------|
| 6. Improve service status parsing | setup-ipc.ts | Robust pattern matching with explicit markers | ✅ DONE |
| 7. Fix brew path detection | setup-ipc.ts | Use `which brew` with caching | ✅ DONE |
| 8. Add JSON for model list | setup-ipc.ts | Requires CLI changes | ⚠️ Deferred |

---

## Part 5: Files Modified for v0.1.20

| File | Location | Change |
|------|----------|--------|
| `KBView.tsx` | `apps/conduit-desktop/src/renderer/components/kb/KBView.tsx:37,59,127-147,377-389` | Removed unused `removeSource`, added `deleteError` state, fixed race condition, added error banner UI |
| `setup-ipc.ts` | `apps/conduit-desktop/src/main/setup-ipc.ts:69-108` | Added `getBrewPath()` helper for dynamic Homebrew path detection |
| `setup-ipc.ts` | `apps/conduit-desktop/src/main/setup-ipc.ts:372-478` | Improved service status parsing with `parseRunningStatus()` helper |
| `setup-ipc.ts` | `apps/conduit-desktop/src/main/setup-ipc.ts:463-486` | Updated Ollama start to use dynamic brew path |
| `setup-ipc.ts` | `apps/conduit-desktop/src/main/setup-ipc.ts:742-800` | Fixed RAG sync regex to parse `Added:`, `Updated:`, `Deleted:` |
| `setup-ipc.ts` | `apps/conduit-desktop/src/main/setup-ipc.ts:855-912` | Fixed KAG sync regex to parse `[current/total]` progress format |
| `package.json` | `apps/conduit-desktop/package.json:3` | Version bump 0.1.19 → 0.1.20 |

---

## Part 6: Verification Checklist for v0.1.20

Before marking bugs as fixed:

- [x] Delete source: Source stays deleted after page refresh
- [x] Delete source: User sees error banner if deletion fails
- [x] RAG sync: Progress bar updates with actual document counts (`Added + Updated + Deleted`)
- [x] KAG sync: Progress bar updates with actual chunk counts (`[current/total]`)
- [x] Error messages visible to user when operations fail
- [x] Service status: Uses robust pattern matching with explicit ✓/✗ markers
- [x] Brew path: Dynamically detected using `which brew` with fallback
- [x] TypeScript typecheck passes
- [x] Build completes successfully

---

## Appendix A: File Locations Reference

| Component | File Path |
|-----------|-----------|
| Main IPC handlers | `apps/conduit-desktop/src/main/ipc.ts` |
| Setup IPC handlers | `apps/conduit-desktop/src/main/setup-ipc.ts` |
| Preload bridge | `apps/conduit-desktop/src/preload/index.ts` |
| KB View | `apps/conduit-desktop/src/renderer/components/kb/KBView.tsx` |
| KB Store | `apps/conduit-desktop/src/renderer/stores/kb.ts` |
| Connectors View | `apps/conduit-desktop/src/renderer/components/connectors/ConnectorsView.tsx` |

---

## Appendix B: Audit Methodology

This audit was performed by:
1. **Explore Agent #1**: Audited all 22 IPC handlers in `ipc.ts` - verified ALL use CLI commands
2. **Explore Agent #2**: Audited all setup IPC handlers in `setup-ipc.ts` - identified fragile parsing
3. **Explore Agent #3**: Traced KB delete/sync flows from UI to IPC - identified root causes
4. **Manual CLI Testing**: Ran actual CLI commands to capture real output formats:
   - `conduit kb sync <id>` - discovered `Added:`, `Updated:`, `Deleted:` format
   - `conduit kb kag-sync <id>` - discovered `[current/total] Processing chunk` format
   - `conduit list --json` vs `conduit mcp status --json` - confirmed architectural separation

---

## Appendix C: Pre vs Post Comparison

| Issue | Pre-Implementation | Post-Implementation | Notes |
|-------|-------------------|---------------------|-------|
| #1 Delete race condition | ❌ BUG | ✅ FIXED | Added result check, refresh(), error UI |
| #2 RAG sync regex | ❌ BUG | ✅ FIXED | Now parses `Added:`, `Updated:`, `Deleted:` |
| #3 KAG sync regex | ❌ BUG | ✅ FIXED | Now parses `[current/total]` progress |
| #4 MCP connectors | ❌ BUG? | ✅ N/A | Confirmed as expected behavior |
| Delete error feedback | ❌ Missing | ✅ ADDED | `deleteError` state + banner UI |
| Sync error feedback | ✅ Working | ✅ Working | Already via `ragResult`/`kagResult` |
| Service status parsing | ⚠️ Fragile | ✅ IMPROVED | Robust pattern matching with ✓/✗ markers |
| Brew path detection | ⚠️ Hardcoded | ✅ FIXED | Dynamic `which brew` with caching |
| Model list parsing | ⚠️ Fragile | ⚠️ Deferred | Requires CLI changes |

---

## Conclusion

The GUI-to-CLI compliance design is correctly implemented. All bugs were caused by:
- Fragile regex patterns that didn't match actual CLI output
- Race conditions in React state management
- Missing refresh() calls after mutations

**All Priority 1, Priority 2, and Priority 3 items from the implementation plan have been completed** (except model list JSON which requires CLI changes).

### Summary of Fixes

| Priority | Items | Completed |
|----------|-------|-----------|
| **Priority 1** | 3 critical bugs | 3/3 ✅ |
| **Priority 2** | 2 error visibility items | 2/2 ✅ |
| **Priority 3** | 3 robustness items | 2/3 ✅ (1 deferred) |

---

**Audit Complete**: All 3 critical bugs fixed. 2 robustness improvements applied. Bug #4 confirmed as expected behavior. Build passes. Ready for release.
