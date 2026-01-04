import { contextBridge, ipcRenderer } from 'electron'
import { electronAPI } from '@electron-toolkit/preload'

export interface UpdateStatus {
  checking: boolean
  available: boolean
  downloading: boolean
  downloaded: boolean
  progress: number
  error: string | null
  version: string | null
  releaseNotes: string | null
  // CLI version compatibility info
  cliUpdateRequired: boolean
  bundledCLIVersion: string | null
  installedCLIVersion: string | null
}

export interface CLIStatus {
  installed: boolean
  version: string | null
  path: string | null
  bundledVersion: string | null
  needsUpdate: boolean
}

export interface DependencyStatus {
  name: string
  installed: boolean
  version?: string
  required: boolean
  installUrl?: string
  brewFormula?: string
}

export interface ServiceStatus {
  name: string
  running: boolean
  port?: number
  container?: string
  error?: string
}

export interface ConduitAPI {
  // App info
  getVersion: () => Promise<string>
  getPlatform: () => Promise<string>

  // Daemon
  getDaemonStatus: () => Promise<unknown>
  getDaemonStats: () => Promise<unknown>

  // Instances
  listInstances: () => Promise<unknown>
  createInstance: (config: unknown) => Promise<unknown>
  startInstance: (id: string) => Promise<unknown>
  stopInstance: (id: string) => Promise<unknown>
  deleteInstance: (id: string) => Promise<unknown>

  // Knowledge Base
  listKBSources: () => Promise<unknown>
  addKBSource: (config: unknown) => Promise<unknown>
  removeKBSource: (id: string) => Promise<unknown>
  syncKBSource: (id: string) => Promise<unknown>
  searchKB: (query: string, options?: object) => Promise<unknown>
  searchKAG: (query: string, options?: object) => Promise<unknown>

  // Permissions (Advanced Mode)
  getInstancePermissions: (id: string) => Promise<unknown>
  setInstancePermission: (id: string, permId: string, granted: boolean) => Promise<unknown>
  auditInstance: (id: string) => Promise<unknown>

  // Bindings
  listBindings: () => Promise<unknown>
  createBinding: (config: unknown) => Promise<unknown>
  deleteBinding: (id: string) => Promise<unknown>

  // Events
  connectEvents: () => Promise<unknown>
  disconnectEvents: () => Promise<unknown>
  getEventsStatus: () => Promise<unknown>

  // Auto-updates
  checkForUpdates: () => Promise<UpdateStatus>
  downloadUpdate: () => Promise<void>
  installUpdate: () => void
  getUpdateStatus: () => Promise<UpdateStatus>
  installCLIUpdate: () => Promise<{ success: boolean; error?: string }>
  onUpdateStatus: (callback: (status: UpdateStatus) => void) => () => void

  // Setup wizard
  checkCLI: () => Promise<CLIStatus>
  installCLI: (options?: { installPath?: string }) => Promise<{ success: boolean; version?: string; path?: string; error?: string }>
  checkDependencies: () => Promise<DependencyStatus[]>
  installDependency: (options: { name: string; brewFormula: string }) => Promise<{ success: boolean; error?: string }>
  checkServices: () => Promise<ServiceStatus[]>
  startService: (options: { name: string }) => Promise<{ success: boolean; error?: string }>
  startAllServices: () => Promise<{ success: boolean; error?: string }>
  checkModels: () => Promise<string[]>
  pullModel: (options: { model: string }) => Promise<{ success: boolean; error?: string }>
  onOllamaPullProgress: (callback: (data: { model: string; progress: number }) => void) => () => void

  // Shell operations
  openExternal: (url: string) => Promise<void>
  openTerminal: () => Promise<void>

  // Event listeners
  onEvent: (callback: (event: unknown) => void) => () => void
  onConnected: (callback: () => void) => () => void
  onDisconnected: (callback: () => void) => () => void
  onError: (callback: (message: string) => void) => () => void
  onNavigate: (callback: (path: string) => void) => () => void
  onOpenSearch: (callback: () => void) => () => void
}

const conduitAPI: ConduitAPI = {
  // App info
  getVersion: () => ipcRenderer.invoke('app:get-version'),
  getPlatform: () => ipcRenderer.invoke('app:get-platform'),

  // Daemon
  getDaemonStatus: () => ipcRenderer.invoke('daemon:status'),
  getDaemonStats: () => ipcRenderer.invoke('daemon:stats'),

  // Instances
  listInstances: () => ipcRenderer.invoke('instances:list'),
  createInstance: (config) => ipcRenderer.invoke('instances:create', config),
  startInstance: (id) => ipcRenderer.invoke('instances:start', id),
  stopInstance: (id) => ipcRenderer.invoke('instances:stop', id),
  deleteInstance: (id) => ipcRenderer.invoke('instances:delete', id),

  // Knowledge Base
  listKBSources: () => ipcRenderer.invoke('kb:sources'),
  addKBSource: (config) => ipcRenderer.invoke('kb:add-source', config),
  removeKBSource: (id) => ipcRenderer.invoke('kb:remove-source', id),
  syncKBSource: (id) => ipcRenderer.invoke('kb:sync', id),
  searchKB: (query, options) => ipcRenderer.invoke('kb:search', query, options),
  searchKAG: (query, options) => ipcRenderer.invoke('kb:kag-search', query, options),

  // Permissions (Advanced Mode)
  getInstancePermissions: (id) => ipcRenderer.invoke('instances:permissions', id),
  setInstancePermission: (id, permId, granted) =>
    ipcRenderer.invoke('instances:set-permission', id, permId, granted),
  auditInstance: (id) => ipcRenderer.invoke('instances:audit', id),

  // Bindings
  listBindings: () => ipcRenderer.invoke('bindings:list'),
  createBinding: (config) => ipcRenderer.invoke('bindings:create', config),
  deleteBinding: (id) => ipcRenderer.invoke('bindings:delete', id),

  // Events
  connectEvents: () => ipcRenderer.invoke('events:connect'),
  disconnectEvents: () => ipcRenderer.invoke('events:disconnect'),
  getEventsStatus: () => ipcRenderer.invoke('events:status'),

  // Auto-updates
  checkForUpdates: () => ipcRenderer.invoke('update:check'),
  downloadUpdate: () => ipcRenderer.invoke('update:download'),
  installUpdate: () => ipcRenderer.invoke('update:install'),
  getUpdateStatus: () => ipcRenderer.invoke('update:get-status'),
  installCLIUpdate: () => ipcRenderer.invoke('update:install-cli'),
  onUpdateStatus: (callback) => {
    const handler = (_: unknown, status: UpdateStatus): void => callback(status)
    ipcRenderer.on('update:status', handler)
    return () => ipcRenderer.removeListener('update:status', handler)
  },

  // Setup wizard
  checkCLI: () => ipcRenderer.invoke('setup:check-cli'),
  installCLI: (options) => ipcRenderer.invoke('setup:install-cli', options),
  checkDependencies: () => ipcRenderer.invoke('setup:check-dependencies'),
  installDependency: (options) => ipcRenderer.invoke('setup:install-dependency', options),
  checkServices: () => ipcRenderer.invoke('setup:check-services'),
  startService: (options) => ipcRenderer.invoke('setup:start-service', options),
  startAllServices: () => ipcRenderer.invoke('setup:start-all-services'),
  checkModels: () => ipcRenderer.invoke('setup:check-models'),
  pullModel: (options) => ipcRenderer.invoke('setup:pull-model', options),
  onOllamaPullProgress: (callback) => {
    const handler = (_: unknown, data: { model: string; progress: number }): void => callback(data)
    ipcRenderer.on('ollama:pull-progress', handler)
    return () => ipcRenderer.removeListener('ollama:pull-progress', handler)
  },

  // Shell operations
  openExternal: (url) => ipcRenderer.invoke('shell:open-external', url),
  openTerminal: () => ipcRenderer.invoke('shell:open-terminal'),

  // Event listeners with cleanup
  onEvent: (callback) => {
    const handler = (_: unknown, event: unknown): void => callback(event)
    ipcRenderer.on('conduit:event', handler)
    return () => ipcRenderer.removeListener('conduit:event', handler)
  },
  onConnected: (callback) => {
    const handler = (): void => callback()
    ipcRenderer.on('conduit:connected', handler)
    return () => ipcRenderer.removeListener('conduit:connected', handler)
  },
  onDisconnected: (callback) => {
    const handler = (): void => callback()
    ipcRenderer.on('conduit:disconnected', handler)
    return () => ipcRenderer.removeListener('conduit:disconnected', handler)
  },
  onError: (callback) => {
    const handler = (_: unknown, message: string): void => callback(message)
    ipcRenderer.on('conduit:error', handler)
    return () => ipcRenderer.removeListener('conduit:error', handler)
  },
  onNavigate: (callback) => {
    const handler = (_: unknown, path: string): void => callback(path)
    ipcRenderer.on('navigate', handler)
    return () => ipcRenderer.removeListener('navigate', handler)
  },
  onOpenSearch: (callback) => {
    const handler = (): void => callback()
    ipcRenderer.on('open-search', handler)
    return () => ipcRenderer.removeListener('open-search', handler)
  }
}

if (process.contextIsolated) {
  try {
    contextBridge.exposeInMainWorld('electron', electronAPI)
    contextBridge.exposeInMainWorld('conduit', conduitAPI)
  } catch (error) {
    console.error(error)
  }
} else {
  // @ts-expect-error - fallback for non-isolated context (should not happen)
  window.electron = electronAPI
  // @ts-expect-error
  window.conduit = conduitAPI
}
