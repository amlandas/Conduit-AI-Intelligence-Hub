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

interface KBStore {
  sources: KBSource[]
  searchResults: SearchResult[]
  searchQuery: string
  loading: boolean
  syncing: { [sourceId: string]: boolean }
  syncProgress: { [sourceId: string]: number }

  setSources: (sources: KBSource[]) => void
  updateSource: (id: string, updates: Partial<KBSource>) => void
  addSource: (source: KBSource) => void
  removeSource: (id: string) => void
  setSearchResults: (results: SearchResult[]) => void
  setSearchQuery: (query: string) => void
  setLoading: (loading: boolean) => void
  setSyncing: (sourceId: string, syncing: boolean) => void
  setSyncProgress: (sourceId: string, progress: number) => void
  refresh: () => Promise<void>
  search: (query: string) => Promise<void>
}

export const useKBStore = create<KBStore>((set, get) => ({
  sources: [],
  searchResults: [],
  searchQuery: '',
  loading: false,
  syncing: {},
  syncProgress: {},

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

  search: async (query: string) => {
    if (!query.trim()) {
      get().setSearchResults([])
      return
    }
    get().setLoading(true)
    get().setSearchQuery(query)
    try {
      const result = await window.conduit.searchKB(query)
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
