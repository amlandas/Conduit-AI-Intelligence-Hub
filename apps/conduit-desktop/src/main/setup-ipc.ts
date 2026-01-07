/**
 * Setup & Dashboard IPC Handlers
 *
 * SIMPLIFIED: The setup wizard now embeds a terminal that runs the CLI install script.
 * This file only contains:
 * - CLI detection for App.tsx launch (setup:check-cli)
 * - KB sync operations (used by dashboard)
 * - MCP configuration (used by dashboard)
 * - Shell utilities
 */

import { ipcMain, app, shell, BrowserWindow } from 'electron'
import { execFile, spawn } from 'child_process'
import { promisify } from 'util'
import * as fs from 'fs'
import * as path from 'path'
import * as os from 'os'
import type { ServiceStatus } from '../shared/types'

const execFileAsync = promisify(execFile)
const fsPromises = fs.promises

// ServiceStatus is imported from shared/types

interface CLIStatus {
  installed: boolean
  version: string | null
  path: string | null
  bundledVersion: string | null
  needsUpdate: boolean
}

// Get path to bundled binaries
function getBundledBinPath(): string {
  if (app.isPackaged) {
    return path.join(process.resourcesPath, 'bin')
  }
  // Dev mode - use resources folder in project
  return path.join(__dirname, '../../resources/bin')
}

// Get CLI install destination
function getCLIInstallPath(): string {
  const home = os.homedir()
  return path.join(home, '.local', 'bin')
}

// Build enhanced PATH that includes common installation directories
// Electron apps launched from Dock/Finder don't inherit shell PATH from .zshrc/.bashrc
function getEnhancedEnv(): NodeJS.ProcessEnv {
  const home = os.homedir()
  const additionalPaths = [
    '/opt/homebrew/bin',           // Homebrew on Apple Silicon
    '/usr/local/bin',              // Homebrew on Intel, manual installs
    path.join(home, '.local', 'bin'), // User-local installs (common for install scripts)
    '/usr/bin',
    '/bin',
    '/usr/sbin',
    '/sbin',
  ]
  return {
    ...process.env,
    PATH: `${additionalPaths.join(':')}:${process.env.PATH || ''}`,
    HOME: home,
  }
}

// Read bundled manifest
async function getBundledManifest(): Promise<{ version: string } | null> {
  try {
    const manifestPath = path.join(getBundledBinPath(), 'manifest.json')
    const content = await fsPromises.readFile(manifestPath, 'utf-8')
    return JSON.parse(content)
  } catch {
    return null
  }
}

// Helper: Compare semver versions (returns 1 if a > b, -1 if a < b, 0 if equal)
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

export function setupSetupIpcHandlers(): void {
  // ════════════════════════════════════════════════════════════════════════════
  // CLI Detection (Used by App.tsx to determine if setup wizard is needed)
  // ════════════════════════════════════════════════════════════════════════════

  ipcMain.handle('setup:check-cli', async (): Promise<CLIStatus> => {
    const manifest = await getBundledManifest()
    const bundledVersion = manifest?.version || null
    const enhancedEnv = getEnhancedEnv()

    // Helper to get version from a CLI path
    const getVersionFromPath = async (cliPath: string): Promise<string | null> => {
      try {
        // CLI uses --version flag, not 'version' subcommand
        // Output format: "conduit version <version> (built <date>)"
        const { stdout } = await execFileAsync(cliPath, ['--version'], { env: enhancedEnv })
        // Parse version from "conduit version 0.1.0 (built ...)" or "conduit version d079a9c (built ...)"
        const match = stdout.match(/conduit version ([^\s(]+)/)
        return match ? match[1].replace(/^v/, '') : stdout.trim()
      } catch (err) {
        console.error(`[setup-ipc] Failed to get version from ${cliPath}:`, err)
        return null
      }
    }

    // First, check common installation paths directly
    // This is more reliable than `which` since Electron doesn't inherit shell PATH
    const commonPaths = [
      '/usr/local/bin/conduit',
      path.join(os.homedir(), '.local', 'bin', 'conduit'),
      '/opt/homebrew/bin/conduit',
    ]

    for (const cliPath of commonPaths) {
      try {
        await fsPromises.access(cliPath, fs.constants.X_OK)
        // File exists and is executable
        const version = await getVersionFromPath(cliPath)
        const needsUpdate = bundledVersion && version
          ? compareVersions(bundledVersion, version) > 0
          : false

        console.log(`[setup-ipc] Found conduit at ${cliPath}, version: ${version}`)
        return {
          installed: true,
          version,
          path: cliPath,
          bundledVersion,
          needsUpdate,
        }
      } catch {
        // Path doesn't exist or isn't executable, try next
      }
    }

    // Fallback: try `which` with enhanced PATH
    try {
      const { stdout } = await execFileAsync('which', ['conduit'], { env: enhancedEnv })
      const cliPath = stdout.trim()

      if (cliPath) {
        const version = await getVersionFromPath(cliPath)
        const needsUpdate = bundledVersion && version
          ? compareVersions(bundledVersion, version) > 0
          : false

        console.log(`[setup-ipc] Found conduit via which at ${cliPath}, version: ${version}`)
        return {
          installed: true,
          version,
          path: cliPath,
          bundledVersion,
          needsUpdate,
        }
      }
    } catch {
      // conduit not found via which either
    }

    console.log('[setup-ipc] Conduit CLI not found')
    return {
      installed: false,
      version: null,
      path: null,
      bundledVersion,
      needsUpdate: false,
    }
  })

  // ════════════════════════════════════════════════════════════════════════════
  // Shell Operations
  // ════════════════════════════════════════════════════════════════════════════

  ipcMain.handle('shell:open-external', async (_, url: string): Promise<void> => {
    await shell.openExternal(url)
  })

  ipcMain.handle('shell:open-terminal', async (): Promise<void> => {
    // macOS: open Terminal app
    if (process.platform === 'darwin') {
      spawn('open', ['-a', 'Terminal'])
    }
  })

  // ════════════════════════════════════════════════════════════════════════════
  // Service Status Detection (Used by Dashboard)
  // ════════════════════════════════════════════════════════════════════════════

  ipcMain.handle('setup:check-services', async (): Promise<ServiceStatus[]> => {
    const services: ServiceStatus[] = []
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')

    try {
      // Get status from CLI - enhanced schema with detailed info
      const { stdout } = await execFileAsync(conduitPath, ['status', '--json'], {
        env: getEnhancedEnv(),
        timeout: 10000
      })
      const cliStatus = JSON.parse(stdout) as {
        cli?: { version?: string; build_time?: string; path?: string };
        daemon?: {
          ready?: boolean;
          version?: string;
          build_time?: string;
          uptime?: string;
          pid?: number;
          socket?: string;
        };
        dependencies?: {
          container_runtime?: {
            name?: string;  // 'podman' or 'docker'
            available?: boolean;
            path?: string;
            version?: string;
            managed_by?: string;
          };
          qdrant?: {
            available?: boolean;
            host?: string;
            http_port?: number;
            grpc_port?: number;
            managed_by?: string;
            collection?: string;
            status?: string;
            vectors_count?: number;
            indexed_vectors?: number;
            container?: { name?: string; image?: string; status?: string };
          };
          ollama?: {
            available?: boolean;
            host?: string;
            path?: string;
            managed_by?: string;
            version?: string;
            models_installed?: string[];
            models_required?: { embedding?: string; extraction?: string };
          };
          sqlite?: {
            available?: boolean;
            version?: string;
            fts5_enabled?: boolean;
            database_path?: string;
            documents_count?: number;
            chunks_count?: number;
          };
          falkordb?: {
            available?: boolean;
            host?: string;
            port?: number;
            managed_by?: string;
            graph_name?: string;
            container?: { name?: string; image?: string; status?: string };
            entities_count?: number;
            relationships_count?: number;
          };
        };
        kag?: {
          enabled?: boolean;
          provider?: string;
          model?: string;
          stats?: {
            chunks_processed?: number;
            chunks_pending?: number;
            chunks_error?: number;
            entities_extracted?: number;
            relationships_extracted?: number;
          };
        };
      }

      // 1. Daemon status
      services.push({
        name: 'Conduit Daemon',
        running: cliStatus.daemon?.ready ?? false,
      })

      // 2. Container Runtime
      const containerRuntime = cliStatus.dependencies?.container_runtime
      if (containerRuntime) {
        services.push({
          name: 'Container Runtime',
          running: containerRuntime.available ?? false,
          container: containerRuntime.name, // Now directly 'podman' or 'docker'
        })
      }

      // 3. Qdrant
      const qdrant = cliStatus.dependencies?.qdrant
      services.push({
        name: 'Qdrant',
        running: qdrant?.available ?? false,
        port: qdrant?.http_port ?? 6333,
      })

      // 4. Ollama - from CLI (GUI is a thin wrapper over CLI)
      const ollama = cliStatus.dependencies?.ollama
      services.push({
        name: 'Ollama',
        running: ollama?.available ?? false,
        port: 11434,
      })

      // 5. FalkorDB - from CLI (GUI is a thin wrapper over CLI)
      const falkordb = cliStatus.dependencies?.falkordb
      services.push({
        name: 'FalkorDB',
        running: falkordb?.available ?? false,
        port: falkordb?.port ?? 6379,
      })

    } catch (err) {
      console.error('[setup-ipc] Failed to get CLI status:', err)
      // If CLI fails, daemon is likely not running
      services.push({ name: 'Conduit Daemon', running: false })
      services.push({ name: 'Ollama', running: false, port: 11434 })
      services.push({ name: 'FalkorDB', running: false, port: 6379 })
    }

    console.log('[setup-ipc] Service status:', services)
    return services
  })

  ipcMain.handle('setup:start-service', async (): Promise<{ success: boolean; error?: string }> => {
    return { success: false, error: 'Services are managed by CLI install script' }
  })

  // ════════════════════════════════════════════════════════════════════════════
  // KB Sync Operations (Used by Dashboard)
  // ════════════════════════════════════════════════════════════════════════════

  // KB Sync with progress - DELEGATES TO CLI: conduit kb sync
  ipcMain.handle('kb:sync-with-progress', async (_, sourceId?: string): Promise<{ success: boolean; processed: number; errors: number; errorTypes: string[]; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const windows = BrowserWindow.getAllWindows()

    const sendProgress = (percent: number, message: string, stats?: { processed: number; errors: number }): void => {
      windows.forEach((window) => {
        window.webContents.send('kb:sync-progress', { sourceId, percent, message, ...stats })
      })
    }

    return new Promise((resolve) => {
      const args = ['kb', 'sync']
      if (sourceId) {
        args.push(sourceId)
      }

      const child = spawn(conduitPath, args, { env: { ...process.env } })

      let processed = 0
      let errors = 0
      const errorTypes: string[] = []
      let lastOutput = ''

      child.stdout?.on('data', (data: Buffer) => {
        const output = data.toString()
        lastOutput += output

        if (output.includes('Syncing source:') || output.includes('Syncing all sources')) {
          sendProgress(10, 'Syncing...', { processed, errors })
        }

        const addedMatch = output.match(/Added:\s*(\d+)/)
        const updatedMatch = output.match(/Updated:\s*(\d+)/)
        const deletedMatch = output.match(/Deleted:\s*(\d+)/)

        if (addedMatch || updatedMatch || deletedMatch) {
          const added = addedMatch ? parseInt(addedMatch[1], 10) : 0
          const updated = updatedMatch ? parseInt(updatedMatch[1], 10) : 0
          const deleted = deletedMatch ? parseInt(deletedMatch[1], 10) : 0
          processed = added + updated + deleted
          sendProgress(80, `Processed ${processed} documents`, { processed, errors })
        }

        if (output.includes('Sync complete')) {
          sendProgress(95, 'Sync complete', { processed, errors })
        }

        const semanticErrorMatch = output.match(/Vectors:\s*(\d+)\s*documents?\s*failed/)
        if (semanticErrorMatch) {
          errors = parseInt(semanticErrorMatch[1], 10)
        }

        const errorsMatch = output.match(/Errors?:\s*(\d+)/)
        if (errorsMatch) {
          errors = parseInt(errorsMatch[1], 10)
        }

        const errorTypeMatch = output.match(/Error:\s*(.+)/g)
        if (errorTypeMatch) {
          errorTypeMatch.forEach((e) => {
            const type = e.replace('Error: ', '').trim()
            if (!errorTypes.includes(type)) {
              errorTypes.push(type)
            }
          })
        }
      })

      child.stderr?.on('data', (data: Buffer) => {
        lastOutput += data.toString()
      })

      child.on('close', async (code) => {
        if (code === 0) {
          sendProgress(100, 'Sync complete', { processed, errors })

          // Auto-configure MCP KB server after successful sync
          try {
            await execFileAsync(conduitPath, ['mcp', 'configure', '--client', 'claude-code'])
            console.log('MCP KB server auto-configured for Claude Code')
          } catch (mcpErr) {
            console.warn('Failed to auto-configure MCP:', mcpErr)
          }

          resolve({ success: true, processed, errors, errorTypes })
        } else {
          resolve({ success: false, processed, errors, errorTypes, error: `Sync failed: ${lastOutput}` })
        }
      })

      child.on('error', (err) => {
        resolve({ success: false, processed, errors, errorTypes, error: err.message })
      })
    })
  })

  // KAG Sync with progress - DELEGATES TO CLI: conduit kb kag-sync
  ipcMain.handle('kb:kag-sync-with-progress', async (_, sourceId?: string): Promise<{ success: boolean; extracted: number; errors: number; errorTypes: string[]; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const windows = BrowserWindow.getAllWindows()

    const sendProgress = (percent: number, message: string, stats?: { extracted: number; errors: number }): void => {
      windows.forEach((window) => {
        window.webContents.send('kb:kag-sync-progress', { sourceId, percent, message, ...stats })
      })
    }

    return new Promise((resolve) => {
      const args = ['kb', 'kag-sync']
      if (sourceId) {
        args.push(sourceId)
      }

      const child = spawn(conduitPath, args, { env: { ...process.env } })

      let extracted = 0
      let errors = 0
      const errorTypes: string[] = []
      let lastOutput = ''

      child.stdout?.on('data', (data: Buffer) => {
        const output = data.toString()
        lastOutput += output

        const startMatch = output.match(/Extracting entities from (\d+) chunks/)
        if (startMatch) {
          sendProgress(5, `Starting extraction...`, { extracted, errors })
        }

        const progressMatch = output.match(/\[(\d+)\/(\d+)\]\s*Processing chunk/)
        if (progressMatch) {
          const current = parseInt(progressMatch[1], 10)
          const total = parseInt(progressMatch[2], 10)
          const percent = Math.round((current / total) * 90) + 5
          sendProgress(percent, `Processing ${current}/${total} chunks...`, { extracted: current, errors })
        }

        const processedMatch = output.match(/Processed:\s*(\d+)\s*chunks/)
        if (processedMatch) {
          extracted = parseInt(processedMatch[1], 10)
          sendProgress(95, `Processed ${extracted} chunks`, { extracted, errors })
        }

        const errorsMatch = output.match(/Errors?:\s*(\d+)\s*chunks?\s*failed/)
        if (errorsMatch) {
          errors = parseInt(errorsMatch[1], 10)
        }

        const generalErrorsMatch = output.match(/(\d+)\s+errors?/)
        if (generalErrorsMatch && !output.includes('chunks failed')) {
          errors = parseInt(generalErrorsMatch[1], 10)
        }

        const errorTypeMatch = output.match(/Error:\s*(.+)/g)
        if (errorTypeMatch) {
          errorTypeMatch.forEach((e) => {
            const type = e.replace('Error: ', '').trim()
            if (!errorTypes.includes(type)) {
              errorTypes.push(type)
            }
          })
        }
      })

      child.stderr?.on('data', (data: Buffer) => {
        lastOutput += data.toString()
      })

      child.on('close', (code) => {
        if (code === 0) {
          sendProgress(100, 'KAG sync complete', { extracted, errors })
          resolve({ success: true, extracted, errors, errorTypes })
        } else {
          resolve({ success: false, extracted, errors, errorTypes, error: `KAG sync failed: ${lastOutput}` })
        }
      })

      child.on('error', (err) => {
        resolve({ success: false, extracted, errors, errorTypes, error: err.message })
      })
    })
  })

  // ════════════════════════════════════════════════════════════════════════════
  // MCP Configuration (Used by Dashboard)
  // ════════════════════════════════════════════════════════════════════════════

  // MCP Auto-configure - DELEGATES TO CLI: conduit mcp configure
  ipcMain.handle('mcp:configure', async (_, options?: { client?: string }): Promise<{ success: boolean; configured: boolean; configPath?: string; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const clientId = options?.client || 'claude-code'

    try {
      const args = ['mcp', 'configure', '--client', clientId]
      const { stdout } = await execFileAsync(conduitPath, args)

      const alreadyConfigured = stdout.includes('already configured')
      const configured = stdout.includes('configured')

      const pathMatch = stdout.match(/Config:\s*(.+)/)
      const configPath = pathMatch ? pathMatch[1].trim() : undefined

      return {
        success: true,
        configured: configured || alreadyConfigured,
        configPath
      }
    } catch (err) {
      return {
        success: false,
        configured: false,
        error: (err as Error).message
      }
    }
  })

  // ════════════════════════════════════════════════════════════════════════════
  // Daemon Version Check (Used by Dashboard for auto-restart on version mismatch)
  // ════════════════════════════════════════════════════════════════════════════

  ipcMain.handle('daemon:get-bundled-version', async (): Promise<string | null> => {
    const manifest = await getBundledManifest()
    return manifest?.version || null
  })

  ipcMain.handle('daemon:restart', async (): Promise<{ success: boolean; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const daemonPath = path.join(getCLIInstallPath(), 'conduit-daemon')

    try {
      // Stop existing daemon gracefully
      try {
        await execFileAsync('pkill', ['-f', 'conduit-daemon'], { env: getEnhancedEnv() })
      } catch {
        // Daemon may not be running, that's OK
      }

      // Wait for daemon to stop
      await new Promise(resolve => setTimeout(resolve, 2000))

      // Start new daemon - it will detach itself
      spawn(daemonPath, [], {
        detached: true,
        stdio: 'ignore',
        env: getEnhancedEnv()
      }).unref()

      // Wait for daemon to start
      await new Promise(resolve => setTimeout(resolve, 3000))

      // Verify daemon is running
      const { stdout } = await execFileAsync(conduitPath, ['status', '--json'], {
        env: getEnhancedEnv(),
        timeout: 10000
      })
      const status = JSON.parse(stdout)

      if (status.daemon?.ready) {
        console.log('[setup-ipc] Daemon restarted successfully, version:', status.daemon?.version)
        return { success: true }
      } else {
        return { success: false, error: 'Daemon did not start' }
      }
    } catch (err) {
      console.error('[setup-ipc] Failed to restart daemon:', err)
      return { success: false, error: (err as Error).message }
    }
  })

  // MCP Check - DELEGATES TO CLI: conduit mcp status --json
  ipcMain.handle('mcp:check', async (_, options?: { client?: string }): Promise<{ configured: boolean; configPath?: string; clients?: Record<string, { configured: boolean; configPath: string }> }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const clientId = options?.client || 'claude-code'

    try {
      const { stdout } = await execFileAsync(conduitPath, ['mcp', 'status', '--json'])
      const result = JSON.parse(stdout) as Record<string, { configured: boolean; configPath: string; serverName?: string }>

      const clientConfig = result[clientId]
      if (clientConfig) {
        return {
          configured: clientConfig.configured,
          configPath: clientConfig.configPath,
          clients: result
        }
      }

      return { configured: false, clients: result }
    } catch (err) {
      console.error('Failed to check MCP status via CLI:', err)
      return { configured: false }
    }
  })
}

