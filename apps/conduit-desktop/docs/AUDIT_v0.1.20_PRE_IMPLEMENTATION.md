# Conduit Desktop v0.1.20 - Pre-Implementation Audit

**Status**: PRE-IMPLEMENTATION BASELINE
**Date**: 2026-01-05
**Audited by**: Claude Code with 3 Explore agents

---

## Executive Summary

This audit comprehensively maps **every UI functionality** to its backend implementation.

**Good News**: ALL GUI operations delegate to CLI commands (not HTTP).
**Bad News**: Fragile CLI output parsing and race conditions are causing the reported bugs.

---

## Part 1: Complete IPC Handler Mapping (VERIFIED)

### Legend
- **CLI**: Calls CLI command via `execCLI()` or `spawn()`
- **Direct**: Direct file system or OS operation
- Status: Working | Fragile | BUG

---

### 1.1 Knowledge Base Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `kb:sources` | List KB sources | CLI | `conduit kb list --json` | Working |
| `kb:add-source` | Add KB source | CLI | `conduit kb add <path> --json` | Working |
| `kb:remove-source` | Delete KB source | CLI | `conduit kb remove <id> --json` | **BUG: Race condition** |
| `kb:sync` | Sync (no progress) | CLI | `conduit kb sync [--source <id>] --json` | Not used |
| `kb:sync-with-progress` | RAG Sync | CLI (spawn) | `conduit kb sync --source <id>` | **BUG: Regex mismatch** |
| `kb:kag-sync-with-progress` | KAG Sync | CLI (spawn) | `conduit kb kag-sync --source <id>` | **BUG: Regex mismatch** |
| `kb:search` | Search KB | CLI | `conduit kb search "<query>" --json` | Working |
| `kb:kag-search` | KAG Search | CLI | `conduit kb kag-query "<query>" --json` | Working |

---

### 1.2 Connector/Instance Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `instances:list` | List connectors | CLI | `conduit list --json` | Working |
| `instances:create` | Create connector | CLI | `conduit create <connector> --name <name> --json` | Working |
| `instances:start` | Start connector | CLI | `conduit start <id> --json` | Working |
| `instances:stop` | Stop connector | CLI | `conduit stop <id> --json` | Working |
| `instances:delete` | Delete connector | CLI | `conduit remove <id> --json` | Working |
| `instances:permissions` | Get permissions | CLI | `conduit permissions <id> --json` | Working |

---

### 1.3 MCP Operations (ipc.ts + setup-ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `mcp:configure` | Configure MCP | CLI | `conduit mcp configure --client <client>` | Working |
| `mcp:check` | Check MCP status | CLI | `conduit mcp status --json` | Working |
| `mcp:list-clients` | List MCP clients | CLI | `conduit mcp clients --json` | Working |

---

### 1.4 Daemon Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `daemon:status` | Get daemon status | CLI | `conduit status --json` | Working |
| `daemon:stats` | Get daemon stats | CLI | `conduit stats --json` | Working |

---

### 1.5 Model Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `ollama:models` | List Ollama models | CLI | `conduit ollama models --json` | Working |
| `ollama:pull` | Pull Ollama model | CLI | `conduit ollama pull <model>` | Working |
| `ollama:status` | Check Ollama status | CLI | `conduit ollama status --json` | Working |

---

### 1.6 Setup/Installation Operations (setup-ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command/Method | Status |
|-------------|-----------|----------------|-------------------|--------|
| `setup:check-cli` | Check CLI installed | Direct | `which conduit` + `conduit version --json` | Working |
| `setup:install-cli` | Install CLI | Direct | File copy from bundle | Working |
| `setup:check-dependencies` | Check deps | CLI | `conduit deps status --json` | Working |
| `setup:install-dependency` | Install dep | CLI | `conduit deps install <name>` | Working |
| `setup:check-services` | Check services | CLI | Multiple status commands | Fragile parsing |
| `setup:start-service` | Start service | CLI | `conduit service start`, etc. | Hardcoded paths |
| `setup:start-all-services` | Start all | CLI | Loops over services | Working |
| `setup:check-models` | Check models | CLI | `conduit ollama models` | Text parsing |
| `setup:pull-model` | Pull model | CLI (spawn) | `conduit ollama pull <model>` | Working |

---

### 1.7 Uninstall Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | CLI Command | Status |
|-------------|-----------|----------------|-------------|--------|
| `uninstall:info` | Get uninstall info | CLI | `conduit uninstall --dry-run --json` | Working |
| `uninstall:execute` | Execute uninstall | CLI | `conduit uninstall --json` | Working |

---

### 1.8 System Operations (ipc.ts)

| IPC Channel | UI Action | Implementation | Method | Status |
|-------------|-----------|----------------|--------|--------|
| `app:open-external` | Open URL | Direct | `shell.openExternal(url)` | Working |
| `app:open-terminal` | Open Terminal | Direct | `spawn('open', ['-a', 'Terminal'])` | Working |
| `app:get-version` | Get app version | Direct | `app.getVersion()` | Working |

---

## Part 2: Bug Root Cause Analysis

### Bug #1: Delete Source Doesn't Work (Sources Reappear)

**Symptom**: User clicks delete, source disappears briefly, then reappears on page refresh.

**Location**: `apps/conduit-desktop/src/renderer/components/kb/KBView.tsx:126-133`

**Root Cause**: Race condition - local state updated before server confirms deletion.

```typescript
// CURRENT (BUGGY):
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

---

### Bug #2: RAG Sync Shows "0 chunks" Forever

**Symptom**: Progress bar shows "Processed: 0 chunks" and never updates.

**Location**: `apps/conduit-desktop/src/main/setup-ipc.ts:719-806`

**Root Cause**: Regex `/Processed:\s*(\d+)/` doesn't match actual CLI output format.

---

### Bug #3: KAG Sync Shows "0 entities" Forever

**Symptom**: Progress bar shows "Extracted: 0 entities" and never updates.

**Location**: `apps/conduit-desktop/src/main/setup-ipc.ts:809-891`

**Root Cause**: Regex `/Extracted:\s*(\d+)/` doesn't match actual CLI output format.

---

## Part 3: Fragilities Found

| Issue | Location | Risk |
|-------|----------|------|
| String matching for service status | setup-ipc.ts:329-404 | Breaks if CLI wording changes |
| Hardcoded `/opt/homebrew/bin/brew` | setup-ipc.ts:408-506 | Fails on Intel Macs |
| Text parsing for model list | setup-ipc.ts | Breaks if output format changes |

---

## File Locations Reference

| Component | File Path |
|-----------|-----------|
| Main IPC handlers | `apps/conduit-desktop/src/main/ipc.ts` |
| Setup IPC handlers | `apps/conduit-desktop/src/main/setup-ipc.ts` |
| Preload bridge | `apps/conduit-desktop/src/preload/index.ts` |
| KB View | `apps/conduit-desktop/src/renderer/components/kb/KBView.tsx` |
| KB Store | `apps/conduit-desktop/src/renderer/stores/kb.ts` |
| Connectors View | `apps/conduit-desktop/src/renderer/components/connectors/ConnectorsView.tsx` |

---

## Audit Methodology

This audit was performed by:
1. **Explore Agent #1**: Audited all 22 IPC handlers in `ipc.ts` - verified ALL use CLI commands
2. **Explore Agent #2**: Audited all setup IPC handlers in `setup-ipc.ts` - identified fragile parsing
3. **Explore Agent #3**: Traced KB delete/sync flows from UI to IPC - identified root causes

**Conclusion**: The GUI-to-CLI compliance design is correctly implemented. The bugs are caused by:
- Fragile regex patterns that don't match actual CLI output
- Race conditions in React state management
- Missing refresh() calls after mutations

---

**This is the PRE-IMPLEMENTATION baseline. Compare with POST-IMPLEMENTATION audit to verify fixes.**
