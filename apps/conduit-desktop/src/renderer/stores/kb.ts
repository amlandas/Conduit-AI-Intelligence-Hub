import { create } from 'zustand'

export interface KBSource {
  id: string
  name: string
  path: string
  documents?: number
  vectors?: number
  lastSync?: string
  syncing?: boolean
  syncProgress?: number
}

export interface SearchResult {
  id: string
  path: string
  content: string
  score: number
  source_id: string
}

// RAG search options - passed to CLI for advanced tuning
export interface RAGSearchOptions {
  minScore?: number           // --min-score: Minimum similarity threshold (0.0-1.0)
  semanticWeight?: number     // --semantic-weight: Semantic vs lexical weight (0.0-1.0)
  mmrLambda?: number          // --mmr-lambda: Relevance vs diversity (0.0-1.0)
  maxResults?: number         // --limit: Maximum results to return
  reranking?: boolean         // --no-rerank: Disable semantic reranking (false = disabled)
  searchMode?: 'hybrid' | 'semantic' | 'fts5'  // --semantic or --fts5 flags
}

// Sync operation result - persists across tab switches
export interface SyncResult {
  success: boolean
  processed?: number
  extracted?: number
  errors: number
  errorTypes: string[]
  error?: string
}

interface KBStore {
  sources: KBSource[]
  searchResults: SearchResult[]
  searchQuery: string
  loading: boolean
  syncing: { [sourceId: string]: boolean }
  syncProgress: { [sourceId: string]: number }

  // Global RAG/KAG sync state - persists across tab switches
  ragSyncing: boolean
  ragProgress: number
  ragResult: SyncResult | null
  kagSyncing: boolean
  kagProgress: number
  kagResult: SyncResult | null
  mcpConfigured: boolean

  setSources: (sources: KBSource[]) => void
  updateSource: (id: string, updates: Partial<KBSource>) => void
  addSource: (source: KBSource) => void
  removeSource: (id: string) => void
  setSearchResults: (results: SearchResult[]) => void
  setSearchQuery: (query: string) => void
  setLoading: (loading: boolean) => void
  setSyncing: (sourceId: string, syncing: boolean) => void
  setSyncProgress: (sourceId: string, progress: number) => void

  // Global RAG/KAG sync setters
  setRagSyncing: (syncing: boolean) => void
  setRagProgress: (progress: number) => void
  setRagResult: (result: SyncResult | null) => void
  setKagSyncing: (syncing: boolean) => void
  setKagProgress: (progress: number) => void
  setKagResult: (result: SyncResult | null) => void
  setMcpConfigured: (configured: boolean) => void

  refresh: () => Promise<void>
  search: (query: string, options?: RAGSearchOptions) => Promise<void>
}

export const useKBStore = create<KBStore>((set, get) => ({
  sources: [],
  searchResults: [],
  searchQuery: '',
  loading: false,
  syncing: {},
  syncProgress: {},

  // Global RAG/KAG sync state - initial values
  ragSyncing: false,
  ragProgress: 0,
  ragResult: null,
  kagSyncing: false,
  kagProgress: 0,
  kagResult: null,
  mcpConfigured: false,

  setSources: (sources) => set({ sources }),

  updateSource: (id, updates) =>
    set((state) => ({
      sources: state.sources.map((src) =>
        src.id === id ? { ...src, ...updates } : src
      )
    })),

  addSource: (source) =>
    set((state) => ({
      sources: [...state.sources, source]
    })),

  removeSource: (id) =>
    set((state) => ({
      sources: state.sources.filter((src) => src.id !== id)
    })),

  setSearchResults: (results) => set({ searchResults: results }),

  setSearchQuery: (query) => set({ searchQuery: query }),

  setLoading: (loading) => set({ loading }),

  setSyncing: (sourceId, syncing) =>
    set((state) => ({
      syncing: { ...state.syncing, [sourceId]: syncing }
    })),

  setSyncProgress: (sourceId, progress) =>
    set((state) => ({
      syncProgress: { ...state.syncProgress, [sourceId]: progress }
    })),

  // Global RAG/KAG sync setters
  setRagSyncing: (syncing) => set({ ragSyncing: syncing }),
  setRagProgress: (progress) => set({ ragProgress: progress }),
  setRagResult: (result) => set({ ragResult: result }),
  setKagSyncing: (syncing) => set({ kagSyncing: syncing }),
  setKagProgress: (progress) => set({ kagProgress: progress }),
  setKagResult: (result) => set({ kagResult: result }),
  setMcpConfigured: (configured) => set({ mcpConfigured: configured }),

  refresh: async () => {
    get().setLoading(true)
    try {
      const result = await window.conduit.listKBSources()
      if (result && typeof result === 'object' && 'sources' in result) {
        get().setSources((result as { sources: KBSource[] }).sources || [])
      }
    } catch (err) {
      console.error('Failed to refresh KB sources:', err)
    } finally {
      get().setLoading(false)
    }
  },

  search: async (query: string, options?: RAGSearchOptions) => {
    if (!query.trim()) {
      get().setSearchResults([])
      return
    }
    get().setLoading(true)
    get().setSearchQuery(query)
    try {
      // Pass RAG options to CLI via IPC
      const result = await window.conduit.searchKB(query, options)
      if (result && typeof result === 'object' && 'results' in result) {
        get().setSearchResults((result as { results: SearchResult[] }).results || [])
      }
    } catch (err) {
      console.error('Failed to search KB:', err)
    } finally {
      get().setLoading(false)
    }
  }
}))
