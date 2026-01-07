/**
 * Shared Type Definitions
 *
 * Single source of truth for types shared across Electron processes:
 * - Main process (setup-ipc.ts)
 * - Preload (index.ts)
 * - Renderer (stores)
 */

/**
 * Service status from CLI checkServices
 */
export interface ServiceStatus {
  name: string
  running: boolean
  port?: number
  container?: string
  error?: string
}

/**
 * Daemon connection status
 */
export interface DaemonStatus {
  connected: boolean
  version?: string
  uptime?: string
  startTime?: string
  instances?: number
  bindings?: number
  pid?: number
}

/**
 * Daemon statistics
 */
export interface DaemonStats {
  totalSources?: number
  totalDocuments?: number
  totalVectors?: number
  ollamaStatus?: 'running' | 'stopped' | 'unknown'
  qdrantStatus?: 'running' | 'stopped' | 'unknown'
  falkordbStatus?: 'running' | 'stopped' | 'unknown'
  containerRuntime?: string
}

/**
 * CLI Status Response - Raw response from conduit status --json
 * This is what the CLI returns, used for stateless display
 */
export interface CLIStatusResponse {
  cli?: {
    version?: string
    build_time?: string
    path?: string
  }
  daemon?: {
    ready?: boolean
    version?: string
    build_time?: string
    uptime?: string
    pid?: number
    socket?: string
  }
  dependencies?: {
    container_runtime?: {
      available?: boolean
      name?: string
      path?: string
      version?: string
    }
    qdrant?: {
      available?: boolean
      status?: string
      vectors?: number
      error?: string
    }
    ollama?: {
      available?: boolean
      host?: string
      path?: string
      version?: string
      managed_by?: string
    }
    sqlite?: {
      available?: boolean
      version?: string
      fts5_enabled?: boolean
    }
    falkordb?: {
      available?: boolean
      host?: string
      port?: number
      managed_by?: string
      graph_name?: string
      entities_count?: number
      relationships_count?: number
    }
  }
  instances?: {
    total?: number
    by_status?: Record<string, number>
  }
  bindings?: {
    total?: number
  }
  kag?: {
    enabled?: boolean
    provider?: string
    model?: string
  }
  timestamp?: string
  error?: string
}

/**
 * Installation status for setup wizard
 */
export interface InstallationStatus {
  installed: boolean
  version: string | null
  location: string | null
  needsInstall: boolean
  needsUpdate: boolean
  hasContainerRuntime: boolean
  containerRuntime: string | null
  containerVersion: string | null
  hasPodman: boolean
  hasDocker: boolean
  hasQdrantImage: boolean
  hasQdrantContainer: boolean
  qdrantContainerRunning: boolean
  hasFalkorDBImage: boolean
  hasFalkorDBContainer: boolean
  falkordbContainerRunning: boolean
  daemonRunning: boolean
  hasOllama: boolean
  ollamaRunning: boolean
  ollamaModels: string[]
  ollamaSize: string | null
  ollamaSizeRaw: number
}

/**
 * Uninstall options
 */
export interface UninstallOptions {
  removeConfig: boolean
  removeData: boolean
  removeContainers: boolean
  removeOllama: boolean
}

/**
 * KB Source for Knowledge Base
 */
export interface KBSource {
  id: number
  type: string
  path: string
  doc_count: number
  added_at: string
  last_synced: string | null
  settings: Record<string, unknown>
}

/**
 * Connector Instance
 */
export interface Instance {
  id: string
  name: string
  config: string
  status: 'PENDING' | 'RUNNING' | 'PAUSED' | 'FAILED'
  lastSync?: string
  lastError?: string
  createdAt: string
  updatedAt: string
}

/**
 * Service start result
 */
export interface ServiceStartResult {
  success: boolean
  error?: string
}
