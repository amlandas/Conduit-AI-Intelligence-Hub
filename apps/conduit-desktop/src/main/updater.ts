/**
 * Auto-update module for Conduit Desktop
 *
 * Uses electron-updater to check for and install updates from GitHub Releases.
 * In development mode, updates are disabled.
 */

import { autoUpdater, UpdateInfo } from 'electron-updater'
import { BrowserWindow, ipcMain } from 'electron'
import { is } from '@electron-toolkit/utils'

// Configure logging
autoUpdater.logger = console

// Disable auto-download by default - let user decide
autoUpdater.autoDownload = false
autoUpdater.autoInstallOnAppQuit = true

export interface UpdateStatus {
  checking: boolean
  available: boolean
  downloading: boolean
  downloaded: boolean
  progress: number
  error: string | null
  version: string | null
  releaseNotes: string | null
}

let updateStatus: UpdateStatus = {
  checking: false,
  available: false,
  downloading: false,
  downloaded: false,
  progress: 0,
  error: null,
  version: null,
  releaseNotes: null
}

let mainWindow: BrowserWindow | null = null

function sendStatusToRenderer(): void {
  if (mainWindow && !mainWindow.isDestroyed()) {
    mainWindow.webContents.send('update:status', updateStatus)
  }
}

function resetStatus(): void {
  updateStatus = {
    checking: false,
    available: false,
    downloading: false,
    downloaded: false,
    progress: 0,
    error: null,
    version: null,
    releaseNotes: null
  }
}

export function initAutoUpdater(window: BrowserWindow): void {
  mainWindow = window

  // Skip auto-update in development
  if (is.dev) {
    console.log('Auto-updater disabled in development mode')
    return
  }

  // Set up event handlers
  autoUpdater.on('checking-for-update', () => {
    console.log('Checking for update...')
    resetStatus()
    updateStatus.checking = true
    sendStatusToRenderer()
  })

  autoUpdater.on('update-available', (info: UpdateInfo) => {
    console.log('Update available:', info.version)
    updateStatus.checking = false
    updateStatus.available = true
    updateStatus.version = info.version
    updateStatus.releaseNotes =
      typeof info.releaseNotes === 'string'
        ? info.releaseNotes
        : Array.isArray(info.releaseNotes)
          ? info.releaseNotes.map((n) => n.note).join('\n')
          : null
    sendStatusToRenderer()
  })

  autoUpdater.on('update-not-available', (info: UpdateInfo) => {
    console.log('Update not available, current version:', info.version)
    updateStatus.checking = false
    updateStatus.available = false
    updateStatus.version = info.version
    sendStatusToRenderer()
  })

  autoUpdater.on('error', (err: Error) => {
    console.error('Update error:', err)
    updateStatus.checking = false
    updateStatus.downloading = false
    updateStatus.error = err.message
    sendStatusToRenderer()
  })

  autoUpdater.on('download-progress', (progressObj) => {
    const percent = Math.round(progressObj.percent)
    console.log('Download progress:', percent, '%')
    updateStatus.downloading = true
    updateStatus.progress = progressObj.percent
    sendStatusToRenderer()
  })

  autoUpdater.on('update-downloaded', (info: UpdateInfo) => {
    console.log('Update downloaded:', info.version)
    updateStatus.downloading = false
    updateStatus.downloaded = true
    updateStatus.version = info.version
    sendStatusToRenderer()
  })

  // Set up IPC handlers
  ipcMain.handle('update:check', async (): Promise<UpdateStatus> => {
    try {
      await autoUpdater.checkForUpdates()
    } catch (err) {
      console.error('Failed to check for updates:', err)
      updateStatus.error = (err as Error).message
    }
    return updateStatus
  })

  ipcMain.handle('update:download', async (): Promise<void> => {
    try {
      await autoUpdater.downloadUpdate()
    } catch (err) {
      console.error('Failed to download update:', err)
      updateStatus.error = (err as Error).message
      sendStatusToRenderer()
    }
  })

  ipcMain.handle('update:install', (): void => {
    // This will quit the app and install the update
    autoUpdater.quitAndInstall(false, true)
  })

  ipcMain.handle('update:get-status', (): UpdateStatus => {
    return updateStatus
  })

  // Check for updates on startup (after a short delay)
  setTimeout(() => {
    console.log('Checking for updates on startup...')
    autoUpdater.checkForUpdates().catch((err) => {
      console.error('Startup update check failed:', err)
    })
  }, 5000) // Wait 5 seconds after app start
}

export function checkForUpdates(): void {
  if (is.dev) {
    console.log('Update check skipped in development mode')
    return
  }
  autoUpdater.checkForUpdates().catch((err) => {
    console.error('Update check failed:', err)
  })
}
