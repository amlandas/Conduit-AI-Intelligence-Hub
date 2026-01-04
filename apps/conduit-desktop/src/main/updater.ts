/**
 * Auto-update module for Conduit Desktop
 *
 * Uses electron-updater to check for and install updates from GitHub Releases.
 * Includes version compatibility logic for bundled CLI binaries.
 * In development mode, updates are disabled.
 */

import { autoUpdater, UpdateInfo } from 'electron-updater'
import { BrowserWindow, ipcMain, app } from 'electron'
import { is } from '@electron-toolkit/utils'
import * as path from 'path'
import * as fs from 'fs'
import * as os from 'os'

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
  // CLI version compatibility info
  cliUpdateRequired: boolean
  bundledCLIVersion: string | null
  installedCLIVersion: string | null
}

// Get package.json metadata
function getPackageMetadata(): { minCLIVersion: string; bundledCLIVersion: string } | null {
  try {
    const pkgPath = app.isPackaged
      ? path.join(process.resourcesPath, 'app.asar', 'package.json')
      : path.join(__dirname, '../../package.json')
    const pkg = JSON.parse(fs.readFileSync(pkgPath, 'utf-8'))
    return pkg.conduit || null
  } catch {
    return null
  }
}

// Get currently installed CLI version
async function getInstalledCLIVersion(): Promise<string | null> {
  try {
    const { execFile } = await import('child_process')
    const { promisify } = await import('util')
    const execFileAsync = promisify(execFile)
    const { stdout } = await execFileAsync('conduit', ['version'])
    return stdout.trim().replace(/^v/, '')
  } catch {
    return null
  }
}

// Compare semver versions (returns 1 if a > b, -1 if a < b, 0 if equal)
function compareVersions(a: string, b: string): number {
  const partsA = a.split('.').map(Number)
  const partsB = b.split('.').map(Number)
  for (let i = 0; i < Math.max(partsA.length, partsB.length); i++) {
    const partA = partsA[i] || 0
    const partB = partsB[i] || 0
    if (partA > partB) return 1
    if (partA < partB) return -1
  }
  return 0
}

// Install CLI binaries from app bundle to ~/.local/bin
async function installCLIFromBundle(): Promise<boolean> {
  try {
    const bundledPath = app.isPackaged
      ? path.join(process.resourcesPath, 'bin')
      : path.join(__dirname, '../../resources/bin')
    const installPath = path.join(os.homedir(), '.local', 'bin')

    // Ensure install directory exists
    await fs.promises.mkdir(installPath, { recursive: true })

    // Copy binaries
    const binaries = ['conduit', 'conduit-daemon']
    for (const binary of binaries) {
      const src = path.join(bundledPath, binary)
      const dest = path.join(installPath, binary)
      await fs.promises.copyFile(src, dest)
      await fs.promises.chmod(dest, 0o755)
    }

    console.log('CLI binaries installed successfully to', installPath)
    return true
  } catch (err) {
    console.error('Failed to install CLI binaries:', err)
    return false
  }
}

let updateStatus: UpdateStatus = {
  checking: false,
  available: false,
  downloading: false,
  downloaded: false,
  progress: 0,
  error: null,
  version: null,
  releaseNotes: null,
  cliUpdateRequired: false,
  bundledCLIVersion: null,
  installedCLIVersion: null
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
    releaseNotes: null,
    cliUpdateRequired: false,
    bundledCLIVersion: null,
    installedCLIVersion: null
  }
}

// Check if CLI update is needed based on installed vs bundled versions
async function checkCLIUpdateNeeded(): Promise<void> {
  const metadata = getPackageMetadata()
  const installedVersion = await getInstalledCLIVersion()

  updateStatus.bundledCLIVersion = metadata?.bundledCLIVersion || null
  updateStatus.installedCLIVersion = installedVersion

  if (metadata?.minCLIVersion && installedVersion) {
    // CLI update required if installed version is below minimum
    updateStatus.cliUpdateRequired = compareVersions(metadata.minCLIVersion, installedVersion) > 0
  } else if (!installedVersion && metadata?.bundledCLIVersion) {
    // CLI not installed but we have bundled version
    updateStatus.cliUpdateRequired = true
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

  autoUpdater.on('update-downloaded', async (info: UpdateInfo) => {
    console.log('Update downloaded:', info.version)
    updateStatus.downloading = false
    updateStatus.downloaded = true
    updateStatus.version = info.version

    // Check if CLI update is needed after Desktop update
    await checkCLIUpdateNeeded()

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

  // Update CLI from bundled binaries (called after Desktop update if needed)
  ipcMain.handle('update:install-cli', async (): Promise<{ success: boolean; error?: string }> => {
    try {
      const success = await installCLIFromBundle()
      if (success) {
        // Refresh CLI version info
        await checkCLIUpdateNeeded()
        sendStatusToRenderer()
        return { success: true }
      }
      return { success: false, error: 'Failed to install CLI binaries' }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
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
