# KB CLI Compliance Fixes - Implementation Plan

**Created**: 2026-01-06
**Branch**: `feature/kb-cli-compliance-fixes`
**PR**: TBD
**Status**: In Progress

---

## Overview

This document tracks the implementation of 4 fixes to ensure the Knowledge Base tab fully adheres to the "GUI is a thin wrapper over CLI" design principle.

---

## Fixes to Implement

### Fix 1: KAGPanel Mock Data Fallback (P0 - Critical)

**Problem**: When KAG search fails, the component shows hardcoded mock data instead of the actual error, hiding real failures from users.

**File**: `src/renderer/components/kb/KAGPanel.tsx`
**Lines**: 126-143

**Current Behavior**:
```typescript
} catch (err) {
  console.error('KAG search failed:', err)
  // Show mock data for UI development
  setResult({
    query,
    entities: [...hardcoded mock data...],
    relations: [...]
  })
}
```

**Target Behavior**:
- Show actual error message to user
- Add error state to component
- Display user-friendly error UI

**Implementation Steps**:
1. Add `error` state to component: `const [error, setError] = useState<string | null>(null)`
2. On catch, set error state instead of mock data
3. Clear error on new search
4. Add error UI display below search results

**Documentation**: See `./01-kagpanel-error-handling.md`

---

### Fix 2: RAGTuningPanel Settings to CLI (P1 - High)

**Problem**: RAG settings (minScore, semanticWeight, mmrLambda, etc.) exist only in local React state and are never passed to CLI search commands.

**Files to Modify**:
1. `src/renderer/components/kb/KBView.tsx` - Pass settings to search
2. `src/renderer/stores/kb.ts` - Update search function signature
3. `src/main/ipc.ts` - Pass options to CLI command
4. `src/preload/index.ts` - Already supports options (verify)

**Current Flow**:
```
KBView.search(query) → kb.store.search(query) → IPC('kb:search', query) → CLI
                                                    ↑ options not passed
```

**Target Flow**:
```
KBView.search(query, ragSettings) → kb.store.search(query, options) → IPC('kb:search', query, options) → CLI with flags
```

**CLI Options to Support**:
| Setting | CLI Flag |
|---------|----------|
| minScore | `--min-score` |
| semanticWeight | `--semantic-weight` |
| mmrLambda | `--mmr-lambda` |
| maxResults | `--limit` |
| reranking | `--rerank` / `--no-rerank` |
| searchMode | `--mode hybrid\|semantic\|fts5` |

**Implementation Steps**:
1. Verify CLI supports these flags (check `cmd/conduit/main.go`)
2. Update `kb.ts` store: `search(query, options?)`
3. Update `KBView.tsx`: pass RAG settings to search
4. Update `ipc.ts`: build CLI args from options
5. Wire RAGTuningPanel settings to KBView

**Documentation**: See `./02-rag-settings-cli.md`

---

### Fix 3: handleAddSource Local State (P1 - Medium)

**Problem**: After CLI add succeeds, updates local Zustand store directly instead of calling `refresh()` to get authoritative state from CLI.

**File**: `src/renderer/components/kb/KBView.tsx`
**Lines**: 148-157

**Current Behavior**:
```typescript
const handleAddSource = async (name: string, path: string): Promise<void> => {
  const result = await window.conduit.addKBSource({ name, path })
  if (result && typeof result === 'object' && 'id' in result) {
    addSource({  // LOCAL STATE UPDATE
      id: (result as { id: string }).id,
      name,
      path
    })
  }
}
```

**Target Behavior**:
```typescript
const handleAddSource = async (name: string, path: string): Promise<void> => {
  const result = await window.conduit.addKBSource({ name, path })
  if (result && typeof result === 'object' && !('error' in result)) {
    await refresh()  // GET AUTHORITATIVE STATE FROM CLI
  }
}
```

**Implementation Steps**:
1. Replace `addSource()` call with `await refresh()`
2. Handle error case properly (CLI may return `{ error: string }`)
3. Ensure error is shown to user if add fails

**Documentation**: See `./03-addsource-refresh.md`

---

### Fix 4: Optional Chaining on searchKAG (P2 - Low)

**Problem**: Optional chaining `?.` suggests uncertainty about API availability.

**File**: `src/renderer/components/kb/KAGPanel.tsx`
**Line**: 118

**Current**:
```typescript
const response = await window.conduit.searchKAG?.(query, options)
```

**Target**:
```typescript
const response = await window.conduit.searchKAG(query, options)
```

**Implementation Steps**:
1. Remove optional chaining
2. Verify `searchKAG` is defined in preload
3. Handle undefined response explicitly in result processing

**Documentation**: See `./04-optional-chaining.md`

---

## Implementation Order

1. **Fix 1** (KAGPanel mock data) - Standalone, no dependencies
2. **Fix 4** (Optional chaining) - Quick fix, same file as Fix 1
3. **Fix 3** (handleAddSource) - Standalone, quick fix
4. **Fix 2** (RAG settings to CLI) - Largest change, multiple files

---

## Testing Checklist

After each fix:
- [ ] TypeScript compiles without errors
- [ ] GUI runs without console errors
- [ ] Feature works as expected

After all fixes:
- [ ] KB Search with RAG settings works
- [ ] KAG search shows real errors on failure
- [ ] Add source refreshes from CLI
- [ ] Build completes successfully

---

## Commit Strategy

Each fix will be committed separately:
1. `fix(kb): show real errors instead of mock data in KAGPanel`
2. `fix(kb): remove optional chaining on searchKAG`
3. `fix(kb): refresh from CLI after adding source`
4. `feat(kb): wire RAG tuning settings to CLI search`

---

## Files Modified (Summary)

| File | Fixes |
|------|-------|
| `src/renderer/components/kb/KAGPanel.tsx` | 1, 4 |
| `src/renderer/components/kb/KBView.tsx` | 2, 3 |
| `src/renderer/stores/kb.ts` | 2 |
| `src/main/ipc.ts` | 2 |

---

## Reference Documentation

This folder contains detailed implementation notes for each fix:
- `01-kagpanel-error-handling.md` - KAGPanel error state design
- `02-rag-settings-cli.md` - RAG settings schema and CLI mapping
- `03-addsource-refresh.md` - Add source flow fix
- `04-optional-chaining.md` - API cleanup notes
