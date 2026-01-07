/**
 * Daemon Store
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * STATELESS: Store raw CLI responses, don't merge with old state.
 * Components derive display values from raw data.
 */
import { create } from 'zustand'
import type {
  ServiceStatus,
  DaemonStatus,
  DaemonStats,
  CLIStatusResponse
} from '../../shared/types'

// Re-export types for component imports
export type { ServiceStatus, DaemonStatus, DaemonStats, CLIStatusResponse }

interface DaemonStore {
  // Raw CLI response - single source of truth
  cliResponse: CLIStatusResponse | null
  // Services list from checkServices
  services: ServiceStatus[]
  // UI state only
  sseConnected: boolean
  lastError: string | null
  startingService: string | null
  isRefreshing: boolean
  isManualRefresh: boolean
  isRestartingDaemon: boolean
  versionMismatch: { bundled: string; running: string } | null

  // Derived getters (computed from cliResponse)
  getStatus: () => DaemonStatus
  getStats: () => DaemonStats

  // SSE event handlers (UI state updates)
  setSSEConnected: (connected: boolean) => void
  handleSSEEvent: (event: { type: string; data: Record<string, unknown> }) => void

  // Actions
  refresh: (manual?: boolean) => Promise<void>
  refreshServices: () => Promise<void>
  startService: (name: string) => Promise<{ success: boolean; error?: string }>
}

export const useDaemonStore = create<DaemonStore>((set, get) => ({
  cliResponse: null,
  services: [],
  sseConnected: false,
  lastError: null,
  startingService: null,
  isRefreshing: false,
  isManualRefresh: false,
  isRestartingDaemon: false,
  versionMismatch: null,

  // Derive DaemonStatus from raw CLI response
  getStatus: () => {
    const cli = get().cliResponse
    const services = get().services
    const daemonService = services.find((s) => s.name === 'Conduit Daemon')

    return {
      // Use daemon service running status, fallback to CLI daemon.ready
      connected: daemonService?.running ?? cli?.daemon?.ready ?? false,
      version: cli?.daemon?.version,
      uptime: cli?.daemon?.uptime,
      instances: cli?.instances?.total,
      bindings: cli?.bindings?.total,
      pid: cli?.daemon?.pid,
    }
  },

  // Derive DaemonStats from raw CLI response + services
  getStats: () => {
    const services = get().services
    const ollamaService = services.find(
      (s) => s.name === 'Ollama' || s.name.toLowerCase().includes('ollama')
    )
    const qdrantService = services.find((s) => s.name === 'Qdrant')
    const falkorService = services.find((s) => s.name === 'FalkorDB')
    const containerService = services.find((s) => s.name === 'Container Runtime')

    return {
      ollamaStatus: ollamaService?.running ? 'running' : 'stopped',
      qdrantStatus: qdrantService?.running ? 'running' : 'stopped',
      falkordbStatus: falkorService?.running ? 'running' : 'stopped',
      containerRuntime:
        containerService?.running && containerService.container
          ? containerService.container
          : undefined,
    }
  },

  // SSE connection state
  setSSEConnected: (connected) => set({ sseConnected: connected }),

  // Handle SSE events - trigger refresh instead of direct state mutation
  handleSSEEvent: (event) => {
    // SSE events indicate something changed - refresh to get latest state
    if (event.type === 'daemon_status') {
      // Daemon sent status update - refresh to sync
      get().refresh()
    }
  },

  // Refresh all status via CLI
  refresh: async (manual = false) => {
    // Prevent concurrent refreshes
    if (get().isRefreshing) return

    set({ isRefreshing: true, isManualRefresh: manual })
    try {
      // Get daemon status from CLI - store raw response
      const cliOutput = (await window.conduit.getDaemonStatus()) as CLIStatusResponse

      if (cliOutput && typeof cliOutput === 'object' && !cliOutput.error) {
        // Replace entire response - don't merge with old state
        set({ cliResponse: cliOutput, lastError: null })

        // Check for version mismatch - but only if not already restarting
        if (!get().isRestartingDaemon && cliOutput.daemon?.ready && cliOutput.daemon?.version) {
          const bundledVersion = await window.conduit.getBundledVersion()
          const runningVersion = cliOutput.daemon.version

          // Normalize versions for comparison (remove 'v' prefix if present)
          const normBundled = bundledVersion?.replace(/^v/, '') || ''
          const normRunning = runningVersion.replace(/^v/, '')

          // Check if versions differ (ignore dirty suffix for comparison)
          const bundledBase = normBundled.split('-dirty')[0]
          const runningBase = normRunning.split('-dirty')[0]

          if (bundledVersion && bundledBase !== runningBase) {
            console.log(`[daemon-store] Version mismatch detected: bundled=${bundledVersion}, running=${runningVersion}`)
            set({ versionMismatch: { bundled: bundledVersion, running: runningVersion }, isRestartingDaemon: true })

            // Auto-restart daemon with new binary
            console.log('[daemon-store] Auto-restarting daemon with updated binary...')
            const result = await window.conduit.restartDaemon()

            if (result.success) {
              console.log('[daemon-store] Daemon restarted successfully')
              set({ versionMismatch: null, isRestartingDaemon: false })
              // Refresh again to get updated status
              set({ isRefreshing: false }) // Allow re-entry
              return get().refresh()
            } else {
              console.error('[daemon-store] Failed to restart daemon:', result.error)
              set({ isRestartingDaemon: false, lastError: `Failed to restart daemon: ${result.error}` })
            }
          } else {
            // Versions match, clear any previous mismatch
            set({ versionMismatch: null })
          }
        }
      } else {
        set({ cliResponse: null, lastError: cliOutput?.error || 'Failed to get daemon status' })
      }

      // Refresh services via CLI
      await get().refreshServices()
    } catch (err) {
      set({
        cliResponse: null,
        lastError: (err as Error).message
      })
    } finally {
      set({ isRefreshing: false, isManualRefresh: false })
    }
  },

  // Refresh service status via CLI
  refreshServices: async () => {
    try {
      const services = await window.conduit.checkServices()
      // Replace entire services list - don't merge
      set({ services })
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

// Convenience selectors for backward compatibility
// Components can use these instead of calling getStatus()/getStats()
export const selectStatus = (state: DaemonStore): DaemonStatus => state.getStatus()
export const selectStats = (state: DaemonStore): DaemonStats => state.getStats()
