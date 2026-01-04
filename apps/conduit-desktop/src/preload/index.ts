import { contextBridge, ipcRenderer } from 'electron'
import { electronAPI } from '@electron-toolkit/preload'

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

  // Bindings
  listBindings: () => Promise<unknown>
  createBinding: (config: unknown) => Promise<unknown>
  deleteBinding: (id: string) => Promise<unknown>

  // Events
  connectEvents: () => Promise<unknown>
  disconnectEvents: () => Promise<unknown>
  getEventsStatus: () => Promise<unknown>

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

  // Bindings
  listBindings: () => ipcRenderer.invoke('bindings:list'),
  createBinding: (config) => ipcRenderer.invoke('bindings:create', config),
  deleteBinding: (id) => ipcRenderer.invoke('bindings:delete', id),

  // Events
  connectEvents: () => ipcRenderer.invoke('events:connect'),
  disconnectEvents: () => ipcRenderer.invoke('events:disconnect'),
  getEventsStatus: () => ipcRenderer.invoke('events:status'),

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
