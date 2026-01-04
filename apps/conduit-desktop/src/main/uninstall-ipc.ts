/**
 * Uninstall IPC Handlers
 *
 * Provides IPC handlers for the Desktop app's uninstall functionality.
 * Delegates to the Go CLI for actual uninstallation to ensure consistency.
 */

import { ipcMain, shell } from 'electron'
import { execFileSync } from 'child_process'
import * as os from 'os'
import * as path from 'path'
import * as fs from 'fs'
import * as http from 'http'

export interface UninstallInfo {
  hasDaemonService: boolean
  daemonRunning: boolean
  servicePath: string | null

  hasBinaries: boolean
  conduitPath: string | null
  daemonPath: string | null
  conduitVersion: string | null

  hasDataDir: boolean
  dataDirPath: string | null
  dataDirSize: string | null
  dataDirSizeRaw: number

  containerRuntime: string | null
  hasQdrantContainer: boolean
  qdrantContainerRunning: boolean
  qdrantVectorCount: number
  hasFalkorDBContainer: boolean
  falkordbContainerRunning: boolean

  hasOllama: boolean
  ollamaRunning: boolean
  ollamaModels: string[]
  ollamaSize: string | null
  ollamaSizeRaw: number

  hasShellConfig: boolean
  shellConfigFiles: string[]

  hasSymlinks: boolean
  symlinks: string[]
}

export interface UninstallOptions {
  tier: 'keep-data' | 'all' | 'full'
  removeOllama: boolean
  force: boolean
}

export interface UninstallResult {
  success: boolean
  itemsRemoved: string[]
  itemsFailed: string[]
  errors: string[]
}

/**
 * Sets up IPC handlers for uninstall functionality
 */
export function setupUninstallIpcHandlers(): void {
  // Get uninstall info
  ipcMain.handle('uninstall:get-info', async (): Promise<UninstallInfo> => {
    return getUninstallInfo()
  })

  // Dry run - show what would be removed
  ipcMain.handle(
    'uninstall:dry-run',
    async (_, options: UninstallOptions): Promise<string[]> => {
      return dryRunUninstall(options)
    }
  )

  // Execute uninstall
  ipcMain.handle(
    'uninstall:execute',
    async (_, options: UninstallOptions): Promise<UninstallResult> => {
      return executeUninstall(options)
    }
  )

  // Open data directory in Finder/Explorer
  ipcMain.handle('uninstall:open-data-dir', async (): Promise<void> => {
    const homeDir = os.homedir()
    const conduitHome = path.join(homeDir, '.conduit')
    if (fs.existsSync(conduitHome)) {
      await shell.openPath(conduitHome)
    }
  })
}

/**
 * Gather information about what's installed
 */
function getUninstallInfo(): UninstallInfo {
  const homeDir = os.homedir()
  const info: UninstallInfo = {
    hasDaemonService: false,
    daemonRunning: false,
    servicePath: null,

    hasBinaries: false,
    conduitPath: null,
    daemonPath: null,
    conduitVersion: null,

    hasDataDir: false,
    dataDirPath: null,
    dataDirSize: null,
    dataDirSizeRaw: 0,

    containerRuntime: null,
    hasQdrantContainer: false,
    qdrantContainerRunning: false,
    qdrantVectorCount: 0,
    hasFalkorDBContainer: false,
    falkordbContainerRunning: false,

    hasOllama: false,
    ollamaRunning: false,
    ollamaModels: [],
    ollamaSize: null,
    ollamaSizeRaw: 0,

    hasShellConfig: false,
    shellConfigFiles: [],

    hasSymlinks: false,
    symlinks: []
  }

  // Check daemon service
  if (process.platform === 'darwin') {
    const plistPath = path.join(homeDir, 'Library', 'LaunchAgents', 'dev.simpleflo.conduit.plist')
    if (fs.existsSync(plistPath)) {
      info.hasDaemonService = true
      info.servicePath = plistPath
    }
  } else if (process.platform === 'linux') {
    const servicePath = path.join(homeDir, '.config', 'systemd', 'user', 'conduit.service')
    if (fs.existsSync(servicePath)) {
      info.hasDaemonService = true
      info.servicePath = servicePath
    }
  }

  // Check daemon running via HTTP
  info.daemonRunning = checkDaemonRunningSync()

  // Check binaries
  const localBin = path.join(homeDir, '.local', 'bin')
  const conduitPath = path.join(localBin, 'conduit')
  const daemonPath = path.join(localBin, 'conduit-daemon')

  if (fs.existsSync(conduitPath)) {
    info.hasBinaries = true
    info.conduitPath = conduitPath
    try {
      info.conduitVersion = execFileSync(conduitPath, ['--version'], { encoding: 'utf8' }).trim()
    } catch {
      // Ignore version errors
    }
  }
  if (fs.existsSync(daemonPath)) {
    info.hasBinaries = true
    info.daemonPath = daemonPath
  }

  // Check data directory
  const conduitHome = path.join(homeDir, '.conduit')
  if (fs.existsSync(conduitHome) && fs.statSync(conduitHome).isDirectory()) {
    info.hasDataDir = true
    info.dataDirPath = conduitHome
    info.dataDirSizeRaw = getDirectorySize(conduitHome)
    info.dataDirSize = formatSize(info.dataDirSizeRaw)
  }

  // Check container runtime and containers
  info.containerRuntime = detectContainerRuntime()
  if (info.containerRuntime) {
    const qdrantInfo = checkContainer(info.containerRuntime, 'conduit-qdrant')
    info.hasQdrantContainer = qdrantInfo.exists
    info.qdrantContainerRunning = qdrantInfo.running

    const falkorInfo = checkContainer(info.containerRuntime, 'conduit-falkordb')
    info.hasFalkorDBContainer = falkorInfo.exists
    info.falkordbContainerRunning = falkorInfo.running

    // Get Qdrant vector count if running
    if (info.qdrantContainerRunning) {
      info.qdrantVectorCount = getQdrantVectorCount()
    }
  }

  // Check Ollama
  if (commandExists('ollama')) {
    info.hasOllama = true
    info.ollamaRunning = checkOllamaRunningSync()
    info.ollamaModels = getOllamaModels()

    const ollamaDir = path.join(homeDir, '.ollama')
    if (fs.existsSync(ollamaDir)) {
      info.ollamaSizeRaw = getDirectorySize(ollamaDir)
      info.ollamaSize = formatSize(info.ollamaSizeRaw)
    }
  }

  // Check shell configs
  const shellConfigs = [
    path.join(homeDir, '.zshrc'),
    path.join(homeDir, '.bashrc'),
    path.join(homeDir, '.bash_profile')
  ]
  const localBinPath = path.join(homeDir, '.local', 'bin')

  for (const config of shellConfigs) {
    if (fs.existsSync(config)) {
      const content = fs.readFileSync(config, 'utf8')
      if (content.includes(localBinPath) || content.includes('# Conduit')) {
        info.hasShellConfig = true
        info.shellConfigFiles.push(config)
      }
    }
  }

  // Check symlinks
  const possibleSymlinks = ['/usr/local/bin/conduit', '/usr/local/bin/conduit-daemon']
  for (const link of possibleSymlinks) {
    try {
      const target = fs.readlinkSync(link)
      if (target.includes('.local/bin')) {
        info.hasSymlinks = true
        info.symlinks.push(link)
      }
    } catch {
      // Not a symlink or doesn't exist
    }
  }

  return info
}

/**
 * Dry run - returns what would be removed
 */
function dryRunUninstall(options: UninstallOptions): string[] {
  const conduitPath = findConduitCLI()
  if (!conduitPath) {
    return ['Error: Conduit CLI not found. Cannot perform dry run.']
  }

  try {
    const flags = buildCLIFlags(options, true)
    const result = execFileSync(conduitPath, ['uninstall', ...flags], {
      encoding: 'utf8',
      timeout: 30000
    })
    return result
      .split('\n')
      .filter((line) => line.includes('[DRY RUN]'))
      .map((line) => line.replace('[DRY RUN]', '').trim())
  } catch (error) {
    return [`Error: ${error instanceof Error ? error.message : String(error)}`]
  }
}

/**
 * Execute uninstall via CLI
 */
function executeUninstall(options: UninstallOptions): UninstallResult {
  const result: UninstallResult = {
    success: true,
    itemsRemoved: [],
    itemsFailed: [],
    errors: []
  }

  const conduitPath = findConduitCLI()
  if (!conduitPath) {
    result.success = false
    result.errors.push('Conduit CLI not found. Please uninstall manually.')
    return result
  }

  try {
    const flags = buildCLIFlags(options, false)
    const output = execFileSync(conduitPath, ['uninstall', ...flags, '--json'], {
      encoding: 'utf8',
      timeout: 120000 // 2 minutes for uninstall
    })

    // Parse JSON result from CLI
    const cliResult = JSON.parse(output) as {
      success: boolean
      itemsRemoved: string[]
      itemsFailed: string[]
      errors: string[]
    }

    result.success = cliResult.success
    result.itemsRemoved = cliResult.itemsRemoved || []
    result.itemsFailed = cliResult.itemsFailed || []
    result.errors = cliResult.errors || []
  } catch (error) {
    result.success = false
    result.errors.push(`Uninstall failed: ${error instanceof Error ? error.message : String(error)}`)
  }

  return result
}

// Helper functions

function findConduitCLI(): string | null {
  const homeDir = os.homedir()

  // Check PATH first using 'which' command
  try {
    const whichCmd = process.platform === 'win32' ? 'where.exe' : 'which'
    const result = execFileSync(whichCmd, ['conduit'], { encoding: 'utf8' }).trim()
    if (result) return result.split('\n')[0]
  } catch {
    // Not in PATH
  }

  // Check ~/.local/bin
  const localBinPath = path.join(homeDir, '.local', 'bin', 'conduit')
  if (fs.existsSync(localBinPath)) {
    return localBinPath
  }

  return null
}

function buildCLIFlags(options: UninstallOptions, dryRun: boolean): string[] {
  const flags: string[] = []

  // Tier flags
  switch (options.tier) {
    case 'keep-data':
      flags.push('--keep-data')
      break
    case 'all':
      flags.push('--all')
      break
    case 'full':
      flags.push('--full')
      break
  }

  if (options.removeOllama) {
    flags.push('--remove-ollama')
  }

  if (options.force) {
    flags.push('--force')
  }

  if (dryRun) {
    flags.push('--dry-run')
  }

  return flags
}

function checkDaemonRunningSync(): boolean {
  // Simple sync check using curl
  try {
    execFileSync('curl', ['-s', '--connect-timeout', '2', 'http://localhost:9090/health'], {
      encoding: 'utf8',
      timeout: 3000
    })
    return true
  } catch {
    return false
  }
}

function detectContainerRuntime(): string | null {
  if (commandExists('podman')) return 'podman'
  if (commandExists('docker')) return 'docker'
  return null
}

function commandExists(cmd: string): boolean {
  try {
    const whichCmd = process.platform === 'win32' ? 'where.exe' : 'which'
    execFileSync(whichCmd, [cmd], { stdio: 'ignore' })
    return true
  } catch {
    return false
  }
}

function checkContainer(runtime: string, name: string): { exists: boolean; running: boolean } {
  try {
    // Check if container exists
    const allContainers = execFileSync(runtime, ['ps', '-a', '--format', '{{.Names}}'], {
      encoding: 'utf8'
    })
    const exists = allContainers.split('\n').some((n) => n.trim() === name)

    if (!exists) {
      return { exists: false, running: false }
    }

    // Check if running
    const runningContainers = execFileSync(runtime, ['ps', '--format', '{{.Names}}'], {
      encoding: 'utf8'
    })
    const running = runningContainers.split('\n').some((n) => n.trim() === name)

    return { exists, running }
  } catch {
    return { exists: false, running: false }
  }
}

function getQdrantVectorCount(): number {
  try {
    const response = execFileSync('curl', ['-s', 'http://localhost:6333/collections/conduit_kb'], {
      encoding: 'utf8',
      timeout: 5000
    })
    const match = response.match(/"points_count":\s*(\d+)/)
    return match ? parseInt(match[1], 10) : 0
  } catch {
    return 0
  }
}

function checkOllamaRunningSync(): boolean {
  try {
    execFileSync('curl', ['-s', '--connect-timeout', '2', 'http://localhost:11434/api/tags'], {
      timeout: 3000
    })
    return true
  } catch {
    return false
  }
}

function getOllamaModels(): string[] {
  try {
    const response = execFileSync('curl', ['-s', 'http://localhost:11434/api/tags'], {
      encoding: 'utf8',
      timeout: 5000
    })
    const data = JSON.parse(response) as { models?: Array<{ name: string }> }
    return data.models?.map((m) => m.name) || []
  } catch {
    return []
  }
}

function getDirectorySize(dirPath: string): number {
  let size = 0
  try {
    const files = fs.readdirSync(dirPath)
    for (const file of files) {
      const filePath = path.join(dirPath, file)
      const stats = fs.statSync(filePath)
      if (stats.isDirectory()) {
        size += getDirectorySize(filePath)
      } else {
        size += stats.size
      }
    }
  } catch {
    // Ignore permission errors
  }
  return size
}

function formatSize(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let unitIndex = 0
  let size = bytes

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024
    unitIndex++
  }

  return `${size.toFixed(1)} ${units[unitIndex]}`
}
