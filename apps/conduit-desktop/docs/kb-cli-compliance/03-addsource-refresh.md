# Fix 3: handleAddSource Refresh from CLI

**Completed**: 2026-01-06
**Files Modified**: `src/renderer/components/kb/KBView.tsx`

---

## Problem

After successfully adding a source via CLI, the component was updating local Zustand state directly instead of refreshing from CLI to get authoritative state.

**Risk**: Local state could drift from actual CLI state (e.g., if CLI adds metadata fields not known to GUI).

---

## Changes Made

### 1. Updated handleAddSource Function

**Before**:
```typescript
const handleAddSource = async (name: string, path: string): Promise<void> => {
  const result = await window.conduit.addKBSource({ name, path })
  if (result && typeof result === 'object' && 'id' in result) {
    addSource({  // LOCAL STATE UPDATE - DEVIATION
      id: (result as { id: string }).id,
      name,
      path
    })
  }
}
```

**After**:
```typescript
const handleAddSource = async (name: string, path: string): Promise<void> => {
  const result = await window.conduit.addKBSource({ name, path })
  // Check if CLI returned an error
  if (result && typeof result === 'object' && 'error' in result) {
    throw new Error((result as { error: string }).error)
  }
  // CLI succeeded - refresh to get authoritative state from CLI
  // Don't update local state directly; let refresh() sync with server
  await refresh()
}
```

### 2. Removed Unused Import

Removed `addSource` from useKBStore destructuring since it's no longer used.

---

## Data Flow

### Before (Local State Update)
```
User clicks "Add Source"
    ↓
CLI: conduit kb add <path> --name <name> --json
    ↓
CLI returns: { id: "abc123" }
    ↓
LOCAL STATE UPDATE with { id, name, path }  ← Could miss fields CLI added
    ↓
UI shows potentially incomplete data
```

### After (CLI Refresh)
```
User clicks "Add Source"
    ↓
CLI: conduit kb add <path> --name <name> --json
    ↓
CLI returns: { id: "abc123" }
    ↓
CLI: conduit kb list --json (via refresh())
    ↓
Store updated with COMPLETE source data from CLI
    ↓
UI shows accurate, authoritative data
```

---

## API Contract

### Add Source (to CLI via IPC)
```typescript
window.conduit.addKBSource({ name: string, path: string })
```

### Response (from CLI)
**Success**:
```typescript
{ id: string }
// Or other fields - doesn't matter since we refresh
```

**Error**:
```typescript
{ error: string }
```

### Refresh (to CLI via IPC)
```typescript
window.conduit.listKBSources()
// Returns: { sources: Array<KBSource> }
```

---

## Design Principle Alignment

This fix aligns with the "GUI is a thin wrapper over CLI" principle:

1. **CLI is source of truth** - We never assume we know what the CLI returns
2. **Stateless display** - Store holds what CLI tells us, nothing more
3. **Refresh after mutations** - After any CUD operation, refresh from CLI
