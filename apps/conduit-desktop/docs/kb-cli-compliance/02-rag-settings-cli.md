# Fix 2: RAG Settings to CLI

**Completed**: 2026-01-06
**Files Modified**:
- `src/renderer/stores/kb.ts`
- `src/renderer/components/kb/KBView.tsx`
- `src/main/ipc.ts`

---

## Problem

RAGTuningPanel settings were purely cosmetic - adjusting them had no effect on actual searches because they weren't being passed to the CLI.

---

## Solution

Wire RAGTuningPanel settings through the component hierarchy to the CLI command.

---

## Data Flow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              RAGTuningPanel                                  │
│  Settings: minScore, semanticWeight, mmrLambda, maxResults,                 │
│            reranking, searchMode                                             │
│                                                                              │
│  onChange(settings) ─────────────────────────────────────────────┐          │
└─────────────────────────────────────────────────────────────────────────────┘
                                                                   │
                                                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                                KBView                                        │
│  [ragSettings, setRagSettings] = useState<RAGSettings | null>()             │
│                                                                              │
│  handleSearch(e) {                                                          │
│    const options = ragSettings ? {                                          │
│      minScore, semanticWeight, mmrLambda, maxResults, reranking, searchMode │
│    } : undefined                                                             │
│    search(query, options)  ──────────────────────────────────────┐          │
│  }                                                                           │
└─────────────────────────────────────────────────────────────────────────────┘
                                                                   │
                                                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                              kb.ts Store                                     │
│                                                                              │
│  search: async (query: string, options?: RAGSearchOptions) => {             │
│    await window.conduit.searchKB(query, options)  ───────────────┐          │
│  }                                                                           │
└─────────────────────────────────────────────────────────────────────────────┘
                                                                   │
                                                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                             preload/index.ts                                 │
│                                                                              │
│  searchKB: (query, options) => ipcRenderer.invoke('kb:search', query, opts) │
└─────────────────────────────────────────────────────────────────────────────┘
                                                                   │
                                                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                               ipc.ts (main)                                  │
│                                                                              │
│  ipcMain.handle('kb:search', (_, query, options) => {                       │
│    const args = ['kb', 'search', query, '--json']                           │
│    if (options.minScore) args.push('--min-score', options.minScore)         │
│    if (options.semanticWeight) args.push('--semantic-weight', ...)          │
│    if (options.mmrLambda) args.push('--mmr-lambda', ...)                    │
│    if (options.maxResults) args.push('--limit', ...)                        │
│    if (options.reranking === false) args.push('--no-rerank')                │
│    if (options.searchMode === 'semantic') args.push('--semantic')           │
│    if (options.searchMode === 'fts5') args.push('--fts5')                   │
│    return execCLI(args)                                                      │
│  })                                                                          │
└─────────────────────────────────────────────────────────────────────────────┘
                                                                   │
                                                                   ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                                   CLI                                        │
│                                                                              │
│  conduit kb search "query" --json \                                         │
│    --min-score 0.15 \                                                       │
│    --semantic-weight 0.5 \                                                  │
│    --mmr-lambda 0.7 \                                                       │
│    --limit 10 \                                                             │
│    [--semantic | --fts5] \                                                  │
│    [--no-rerank]                                                            │
└─────────────────────────────────────────────────────────────────────────────┘
```

---

## Schema Definitions

### RAGSettings (from RAGTuningPanel.tsx)

```typescript
export interface RAGSettings {
  minScore: number           // 0.0-1.0, default 0.15
  semanticWeight: number     // 0.0-1.0, default 0.5
  mmrLambda: number          // 0.0-1.0, default 0.7
  maxResults: number         // 5-50, default 10
  reranking: boolean         // default true
  searchMode: 'hybrid' | 'semantic' | 'fts5'  // default 'hybrid'
}
```

### RAGSearchOptions (from kb.ts store)

```typescript
export interface RAGSearchOptions {
  minScore?: number
  semanticWeight?: number
  mmrLambda?: number
  maxResults?: number
  reranking?: boolean
  searchMode?: 'hybrid' | 'semantic' | 'fts5'
}
```

### CLI Flags Mapping

| RAGSettings field | CLI Flag | Example |
|-------------------|----------|---------|
| minScore | `--min-score` | `--min-score 0.15` |
| semanticWeight | `--semantic-weight` | `--semantic-weight 0.5` |
| mmrLambda | `--mmr-lambda` | `--mmr-lambda 0.7` |
| maxResults | `--limit` | `--limit 10` |
| reranking (false) | `--no-rerank` | `--no-rerank` |
| searchMode='semantic' | `--semantic` | `--semantic` |
| searchMode='fts5' | `--fts5` | `--fts5` |
| searchMode='hybrid' | (none) | (default) |

---

## Changes Made

### 1. Added RAGSearchOptions interface to kb.ts

```typescript
export interface RAGSearchOptions {
  minScore?: number
  semanticWeight?: number
  mmrLambda?: number
  maxResults?: number
  reranking?: boolean
  searchMode?: 'hybrid' | 'semantic' | 'fts5'
}
```

### 2. Updated search function signature in kb.ts

```typescript
search: (query: string, options?: RAGSearchOptions) => Promise<void>
```

### 3. Updated KBView.tsx

- Added `ragSettings` state
- Updated `handleSearch` to convert settings to options
- Wired RAGTuningPanel's `onChange` to `setRagSettings`

### 4. Updated ipc.ts kb:search handler

- Added support for all RAG tuning options
- Maintained backward compatibility with legacy snake_case options
- Properly builds CLI args for each option

---

## Testing

With RAGTuningPanel visible (Advanced Mode):

1. Adjust minScore slider → Search should show different results
2. Change searchMode to "Semantic" → Results should change
3. Disable reranking → Results order may change
4. Increase maxResults → More results should appear
