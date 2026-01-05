/**
 * Daemon Store
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * Service status comes from CLI commands via IPC, not daemon API.
 */
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
  falkordbStatus?: 'running' | 'stopped' | 'unknown'
  containerRuntime?: string
}

export interface ServiceStatus {
  name: string
  running: boolean
  port?: number
  container?: string
  error?: string
}

interface DaemonStore {
  status: DaemonStatus
  stats: DaemonStats
  services: ServiceStatus[]
  sseConnected: boolean
  lastError: string | null
  startingService: string | null

  setStatus: (status: Partial<DaemonStatus>) => void
  setStats: (stats: Partial<DaemonStats>) => void
  setServices: (services: ServiceStatus[]) => void
  setSSEConnected: (connected: boolean) => void
  setError: (error: string | null) => void
  refresh: () => Promise<void>
  refreshServices: () => Promise<void>
  startService: (name: string) => Promise<{ success: boolean; error?: string }>
}

export const useDaemonStore = create<DaemonStore>((set, get) => ({
  status: { connected: false },
  stats: {},
  services: [],
  sseConnected: false,
  lastError: null,
  startingService: null,

  setStatus: (status) =>
    set((state) => ({
      status: { ...state.status, ...status }
    })),

  setStats: (stats) =>
    set((state) => ({
      stats: { ...state.stats, ...stats }
    })),

  setServices: (services) => set({ services }),

  setSSEConnected: (connected) => set({ sseConnected: connected }),

  setError: (error) => set({ lastError: error }),

  // Refresh all status via CLI
  refresh: async () => {
    try {
      // Get daemon status
      const status = await window.conduit.getDaemonStatus()
      if (status && typeof status === 'object' && !('error' in status)) {
        set({ status: { connected: true, ...status as object } })
      } else {
        set({ status: { connected: false } })
      }

      // Get daemon stats
      const stats = await window.conduit.getDaemonStats()
      if (stats && typeof stats === 'object' && !('error' in stats)) {
        get().setStats(stats as DaemonStats)
      }

      // Also refresh services via CLI
      await get().refreshServices()
    } catch (err) {
      set({ status: { connected: false } })
      get().setError((err as Error).message)
    }
  },

  // Refresh service status via CLI commands
  refreshServices: async () => {
    try {
      const services = await window.conduit.checkServices()
      set({ services })

      // Update stats with service status
      const ollamaService = services.find((s: ServiceStatus) => s.name === 'Ollama' || s.name.toLowerCase().includes('ollama'))
      const qdrantService = services.find((s: ServiceStatus) => s.name === 'Qdrant')
      const falkorService = services.find((s: ServiceStatus) => s.name === 'FalkorDB')
      const daemonService = services.find((s: ServiceStatus) => s.name === 'Conduit Daemon')

      get().setStats({
        ollamaStatus: ollamaService?.running ? 'running' : 'stopped',
        qdrantStatus: qdrantService?.running ? 'running' : 'stopped',
        falkordbStatus: falkorService?.running ? 'running' : 'stopped',
      })

      // Update daemon connected status
      if (daemonService) {
        get().setStatus({ connected: daemonService.running })
      }
    } catch (err) {
      console.error('Failed to refresh services:', err)
    }
  },

  // Start a service via CLI
  startService: async (name: string) => {
    set({ startingService: name })
    try {
      const result = await window.conduit.startService({ name })
      // Refresh services after starting
      await get().refreshServices()
      return result
    } finally {
      set({ startingService: null })
    }
  }
}))
