import { ipcMain, BrowserWindow } from 'electron'
import { daemonClient, DaemonEvent } from './daemon-client'

export function setupIpcHandlers(): void {
  // Daemon status
  ipcMain.handle('daemon:status', async () => {
    try {
      return await daemonClient.get('/status')
    } catch (err) {
      return { error: (err as Error).message, connected: false }
    }
  })

  ipcMain.handle('daemon:stats', async () => {
    try {
      return await daemonClient.get('/stats')
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // Instance management
  ipcMain.handle('instances:list', async () => {
    try {
      return await daemonClient.get('/instances')
    } catch (err) {
      return { error: (err as Error).message, instances: [] }
    }
  })

  ipcMain.handle('instances:create', async (_, config) => {
    return daemonClient.post('/instances', config)
  })

  ipcMain.handle('instances:start', async (_, id: string) => {
    return daemonClient.post(`/instances/${id}/start`)
  })

  ipcMain.handle('instances:stop', async (_, id: string) => {
    return daemonClient.post(`/instances/${id}/stop`)
  })

  ipcMain.handle('instances:delete', async (_, id: string) => {
    return daemonClient.delete(`/instances/${id}`)
  })

  // Knowledge Base
  ipcMain.handle('kb:sources', async () => {
    try {
      return await daemonClient.get('/kb/sources')
    } catch (err) {
      return { error: (err as Error).message, sources: [] }
    }
  })

  ipcMain.handle('kb:add-source', async (_, config) => {
    return daemonClient.post('/kb/sources', config)
  })

  ipcMain.handle('kb:remove-source', async (_, id: string) => {
    return daemonClient.delete(`/kb/sources/${id}`)
  })

  ipcMain.handle('kb:sync', async (_, id: string) => {
    return daemonClient.post(`/kb/sources/${id}/sync`)
  })

  ipcMain.handle('kb:search', async (_, query: string, options?: object) => {
    const params = new URLSearchParams({ q: query })
    if (options) {
      Object.entries(options).forEach(([key, value]) => {
        if (value !== undefined) {
          params.set(key, String(value))
        }
      })
    }
    return daemonClient.get(`/kb/search?${params.toString()}`)
  })

  ipcMain.handle('kb:kag-search', async (_, query: string, options?: object) => {
    const params = new URLSearchParams({ q: query })
    if (options) {
      Object.entries(options).forEach(([key, value]) => {
        if (value !== undefined) {
          params.set(key, String(value))
        }
      })
    }
    try {
      return await daemonClient.get(`/kb/kag/search?${params.toString()}`)
    } catch (err) {
      return { error: (err as Error).message, entities: [], relations: [] }
    }
  })

  // Instance permissions (Advanced Mode)
  ipcMain.handle('instances:permissions', async (_, id: string) => {
    try {
      return await daemonClient.get(`/instances/${id}/permissions`)
    } catch (err) {
      // Return mock data when API not available
      return { error: (err as Error).message }
    }
  })

  ipcMain.handle('instances:set-permission', async (_, id: string, permId: string, granted: boolean) => {
    try {
      return await daemonClient.post(`/instances/${id}/permissions`, { permission: permId, granted })
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  ipcMain.handle('instances:audit', async (_, id: string) => {
    try {
      return await daemonClient.post(`/instances/${id}/audit`)
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // Client bindings
  ipcMain.handle('bindings:list', async () => {
    try {
      return await daemonClient.get('/bindings')
    } catch (err) {
      return { error: (err as Error).message, bindings: [] }
    }
  })

  ipcMain.handle('bindings:create', async (_, config) => {
    return daemonClient.post('/bindings', config)
  })

  ipcMain.handle('bindings:delete', async (_, id: string) => {
    return daemonClient.delete(`/bindings/${id}`)
  })

  // SSE Events
  ipcMain.handle('events:connect', () => {
    daemonClient.connectSSE()
    return { connected: true }
  })

  ipcMain.handle('events:disconnect', () => {
    daemonClient.disconnectSSE()
    return { connected: false }
  })

  ipcMain.handle('events:status', () => {
    return { connected: daemonClient.getIsConnected() }
  })

  // Forward SSE events to renderer
  daemonClient.on('event', (event: DaemonEvent) => {
    const windows = BrowserWindow.getAllWindows()
    windows.forEach((window) => {
      window.webContents.send('conduit:event', event)
    })
  })

  daemonClient.on('connected', () => {
    const windows = BrowserWindow.getAllWindows()
    windows.forEach((window) => {
      window.webContents.send('conduit:connected')
    })
  })

  daemonClient.on('disconnected', () => {
    const windows = BrowserWindow.getAllWindows()
    windows.forEach((window) => {
      window.webContents.send('conduit:disconnected')
    })
  })

  daemonClient.on('error', (err: Error) => {
    const windows = BrowserWindow.getAllWindows()
    windows.forEach((window) => {
      window.webContents.send('conduit:error', err.message)
    })
  })

  // Auto-connect to SSE on app start
  daemonClient.connectSSE()
}
