import { create } from 'zustand'

export interface DaemonStatus {
  connected: boolean
  version?: string
  uptime?: string
  startTime?: string
  instances?: number
  bindings?: number
  pid?: number
}

export interface DaemonStats {
  totalSources?: number
  totalDocuments?: number
  totalVectors?: number
  ollamaStatus?: 'running' | 'stopped' | 'unknown'
  qdrantStatus?: 'running' | 'stopped' | 'unknown'
  containerRuntime?: string
}

interface DaemonStore {
  status: DaemonStatus
  stats: DaemonStats
  sseConnected: boolean
  lastError: string | null

  setStatus: (status: Partial<DaemonStatus>) => void
  setStats: (stats: Partial<DaemonStats>) => void
  setSSEConnected: (connected: boolean) => void
  setError: (error: string | null) => void
  refresh: () => Promise<void>
}

export const useDaemonStore = create<DaemonStore>((set, get) => ({
  status: { connected: false },
  stats: {},
  sseConnected: false,
  lastError: null,

  setStatus: (status) =>
    set((state) => ({
      status: { ...state.status, ...status }
    })),

  setStats: (stats) =>
    set((state) => ({
      stats: { ...state.stats, ...stats }
    })),

  setSSEConnected: (connected) => set({ sseConnected: connected }),

  setError: (error) => set({ lastError: error }),

  refresh: async () => {
    try {
      const status = await window.conduit.getDaemonStatus()
      if (status && typeof status === 'object' && !('error' in status)) {
        set({ status: { connected: true, ...status as object } })
      } else {
        set({ status: { connected: false } })
      }

      const stats = await window.conduit.getDaemonStats()
      if (stats && typeof stats === 'object' && !('error' in stats)) {
        get().setStats(stats as DaemonStats)
      }
    } catch (err) {
      set({ status: { connected: false } })
      get().setError((err as Error).message)
    }
  }
}))
