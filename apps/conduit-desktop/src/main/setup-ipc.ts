/**
 * Setup Wizard IPC Handlers
 *
 * Handles CLI installation, dependency detection, service management,
 * and AI model download during first-run setup.
 */

import { ipcMain, app, shell, BrowserWindow } from 'electron'
import { execFile, spawn, ChildProcess } from 'child_process'
import { promisify } from 'util'
import * as fs from 'fs'
import * as path from 'path'
import * as os from 'os'

const execFileAsync = promisify(execFile)
const fsPromises = fs.promises

interface CLIStatus {
  installed: boolean
  version: string | null
  path: string | null
  bundledVersion: string | null
  needsUpdate: boolean
}

interface DependencyStatus {
  name: string
  installed: boolean
  version?: string
  required: boolean
  installUrl?: string
  brewFormula?: string
}

interface ServiceStatus {
  name: string
  running: boolean
  port?: number
  container?: string
  error?: string
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

export function setupSetupIpcHandlers(): void {
  // Check CLI installation status
  ipcMain.handle('setup:check-cli', async (): Promise<CLIStatus> => {
    const manifest = await getBundledManifest()
    const bundledVersion = manifest?.version || null

    try {
      // Try to find conduit in PATH
      const { stdout } = await execFileAsync('which', ['conduit'])
      const cliPath = stdout.trim()

      if (cliPath) {
        // Get version
        try {
          const { stdout: versionOut } = await execFileAsync('conduit', ['version'])
          const version = versionOut.trim().replace(/^v/, '')

          // Check if update needed
          const needsUpdate = bundledVersion
            ? compareVersions(bundledVersion, version) > 0
            : false

          return {
            installed: true,
            version,
            path: cliPath,
            bundledVersion,
            needsUpdate,
          }
        } catch {
          return {
            installed: true,
            version: null,
            path: cliPath,
            bundledVersion,
            needsUpdate: false,
          }
        }
      }
    } catch {
      // conduit not found in PATH
    }

    return {
      installed: false,
      version: null,
      path: null,
      bundledVersion,
      needsUpdate: false,
    }
  })

  // Install CLI from bundled binaries
  ipcMain.handle('setup:install-cli', async (): Promise<{ success: boolean; version?: string; path?: string; error?: string }> => {
    try {
      const bundledPath = getBundledBinPath()
      const installPath = getCLIInstallPath()

      // Ensure install directory exists
      await fsPromises.mkdir(installPath, { recursive: true })

      // Copy binaries
      const binaries = ['conduit', 'conduit-daemon']
      for (const binary of binaries) {
        const src = path.join(bundledPath, binary)
        const dest = path.join(installPath, binary)

        // Check if source exists
        await fsPromises.access(src)

        // Copy file
        await fsPromises.copyFile(src, dest)

        // Make executable
        await fsPromises.chmod(dest, 0o755)
      }

      // Get installed version
      const manifest = await getBundledManifest()

      // Check if install path is in PATH
      const pathEnv = process.env.PATH || ''
      if (!pathEnv.includes(installPath)) {
        // Suggest adding to PATH
        console.log(`Note: ${installPath} should be added to PATH`)
      }

      return {
        success: true,
        version: manifest?.version,
        path: installPath,
      }
    } catch (err) {
      return {
        success: false,
        error: (err as Error).message,
      }
    }
  })

  // Check dependencies
  ipcMain.handle('setup:check-dependencies', async (): Promise<DependencyStatus[]> => {
    const results: DependencyStatus[] = []

    // Check Homebrew
    try {
      const { stdout } = await execFileAsync('brew', ['--version'])
      results.push({
        name: 'Homebrew',
        installed: true,
        version: stdout.split('\n')[0].replace('Homebrew ', ''),
        required: false,
      })
    } catch {
      results.push({
        name: 'Homebrew',
        installed: false,
        required: false,
        installUrl: 'https://brew.sh',
      })
    }

    // Check Ollama
    try {
      const { stdout } = await execFileAsync('ollama', ['--version'])
      results.push({
        name: 'Ollama',
        installed: true,
        version: stdout.trim(),
        required: true,
      })
    } catch {
      results.push({
        name: 'Ollama',
        installed: false,
        required: true,
        installUrl: 'https://ollama.com/download',
        brewFormula: 'ollama',
      })
    }

    // Check Docker or Podman
    let containerRuntime = false
    try {
      const { stdout } = await execFileAsync('podman', ['--version'])
      results.push({
        name: 'Docker or Podman',
        installed: true,
        version: `Podman ${stdout.trim().split(' ').pop()}`,
        required: true,
      })
      containerRuntime = true
    } catch {
      try {
        const { stdout } = await execFileAsync('docker', ['--version'])
        results.push({
          name: 'Docker or Podman',
          installed: true,
          version: stdout.trim(),
          required: true,
        })
        containerRuntime = true
      } catch {
        // Neither found
      }
    }

    if (!containerRuntime) {
      results.push({
        name: 'Docker or Podman',
        installed: false,
        required: true,
        installUrl: 'https://podman.io/getting-started/installation',
        brewFormula: 'podman',
      })
    }

    return results
  })

  // Install dependency via Homebrew
  ipcMain.handle('setup:install-dependency', async (_, { name, brewFormula }: { name: string; brewFormula: string }): Promise<{ success: boolean; error?: string }> => {
    try {
      await execFileAsync('brew', ['install', brewFormula])
      return { success: true }
    } catch (err) {
      return { success: false, error: `Failed to install ${name}: ${(err as Error).message}` }
    }
  })

  // Check services status
  ipcMain.handle('setup:check-services', async (): Promise<ServiceStatus[]> => {
    const results: ServiceStatus[] = []

    // Check Conduit Daemon
    try {
      const socketPath = path.join(os.homedir(), '.conduit', 'conduit.sock')
      await fsPromises.access(socketPath)
      results.push({
        name: 'Conduit Daemon',
        running: true,
        port: undefined,
      })
    } catch {
      results.push({
        name: 'Conduit Daemon',
        running: false,
      })
    }

    // Check Qdrant (via container runtime)
    try {
      const { stdout } = await execContainerCommand(['ps', '--format', '{{.Names}}'])
      const containers = stdout.trim().split('\n')
      const qdrantRunning = containers.some(c => c.includes('qdrant'))
      results.push({
        name: 'Qdrant',
        running: qdrantRunning,
        port: qdrantRunning ? 6333 : undefined,
        container: qdrantRunning ? 'qdrant' : undefined,
      })
    } catch {
      results.push({
        name: 'Qdrant',
        running: false,
      })
    }

    // Check FalkorDB
    try {
      const { stdout } = await execContainerCommand(['ps', '--format', '{{.Names}}'])
      const containers = stdout.trim().split('\n')
      const falkorRunning = containers.some(c => c.includes('falkordb'))
      results.push({
        name: 'FalkorDB',
        running: falkorRunning,
        port: falkorRunning ? 6379 : undefined,
        container: falkorRunning ? 'falkordb' : undefined,
      })
    } catch {
      results.push({
        name: 'FalkorDB',
        running: false,
      })
    }

    return results
  })

  // Start a specific service
  ipcMain.handle('setup:start-service', async (_, { name }: { name: string }): Promise<{ success: boolean; error?: string }> => {
    try {
      switch (name) {
        case 'Conduit Daemon': {
          const installPath = getCLIInstallPath()
          const daemonPath = path.join(installPath, 'conduit-daemon')
          // Start daemon in background
          const child = spawn(daemonPath, [], {
            detached: true,
            stdio: 'ignore',
          })
          child.unref()
          // Wait a moment for startup
          await new Promise(resolve => setTimeout(resolve, 1000))
          return { success: true }
        }
        case 'Qdrant': {
          await execContainerCommand([
            'run', '-d',
            '--name', 'qdrant',
            '-p', '6333:6333',
            '-p', '6334:6334',
            '-v', 'qdrant_storage:/qdrant/storage',
            'qdrant/qdrant',
          ])
          return { success: true }
        }
        case 'FalkorDB': {
          await execContainerCommand([
            'run', '-d',
            '--name', 'falkordb',
            '-p', '6379:6379',
            'falkordb/falkordb',
          ])
          return { success: true }
        }
        default:
          return { success: false, error: `Unknown service: ${name}` }
      }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
  })

  // Start all services
  ipcMain.handle('setup:start-all-services', async (): Promise<{ success: boolean; error?: string }> => {
    const errors: string[] = []

    // Start services in sequence
    const startService = async (name: string): Promise<{ success: boolean; error?: string }> => {
      try {
        switch (name) {
          case 'Conduit Daemon': {
            const installPath = getCLIInstallPath()
            const daemonPath = path.join(installPath, 'conduit-daemon')
            const child = spawn(daemonPath, [], {
              detached: true,
              stdio: 'ignore',
            })
            child.unref()
            await new Promise(resolve => setTimeout(resolve, 1000))
            return { success: true }
          }
          case 'Qdrant': {
            await execContainerCommand([
              'run', '-d',
              '--name', 'qdrant',
              '-p', '6333:6333',
              '-p', '6334:6334',
              '-v', 'qdrant_storage:/qdrant/storage',
              'qdrant/qdrant',
            ])
            return { success: true }
          }
          case 'FalkorDB': {
            await execContainerCommand([
              'run', '-d',
              '--name', 'falkordb',
              '-p', '6379:6379',
              'falkordb/falkordb',
            ])
            return { success: true }
          }
          default:
            return { success: false, error: `Unknown service: ${name}` }
        }
      } catch (err) {
        return { success: false, error: (err as Error).message }
      }
    }

    const services = ['Conduit Daemon', 'Qdrant', 'FalkorDB']
    for (const service of services) {
      const result = await startService(service)
      if (!result.success) {
        errors.push(`${service}: ${result.error}`)
      }
    }

    if (errors.length > 0) {
      return { success: false, error: errors.join('; ') }
    }
    return { success: true }
  })

  // Check installed Ollama models
  ipcMain.handle('setup:check-models', async (): Promise<string[]> => {
    try {
      const { stdout } = await execFileAsync('ollama', ['list'])
      const lines = stdout.trim().split('\n').slice(1) // Skip header
      return lines.map(line => line.split(/\s+/)[0]).filter(Boolean)
    } catch {
      return []
    }
  })

  // Pull an Ollama model with progress
  ipcMain.handle('setup:pull-model', async (event, { model }: { model: string }): Promise<{ success: boolean; error?: string }> => {
    return new Promise((resolve) => {
      const windows = BrowserWindow.getAllWindows()
      let child: ChildProcess

      try {
        child = spawn('ollama', ['pull', model])

        let lastProgress = 0
        child.stdout?.on('data', (data: Buffer) => {
          const output = data.toString()
          // Parse progress from output (e.g., "pulling manifest... 100%")
          const match = output.match(/(\d+)%/)
          if (match) {
            const progress = parseInt(match[1], 10)
            if (progress !== lastProgress) {
              lastProgress = progress
              windows.forEach((window) => {
                window.webContents.send('ollama:pull-progress', { model, progress })
              })
            }
          }
        })

        child.stderr?.on('data', (data: Buffer) => {
          const output = data.toString()
          // Ollama uses stderr for progress too
          const match = output.match(/(\d+)%/)
          if (match) {
            const progress = parseInt(match[1], 10)
            if (progress !== lastProgress) {
              lastProgress = progress
              windows.forEach((window) => {
                window.webContents.send('ollama:pull-progress', { model, progress })
              })
            }
          }
        })

        child.on('close', (code) => {
          if (code === 0) {
            windows.forEach((window) => {
              window.webContents.send('ollama:pull-progress', { model, progress: 100 })
            })
            resolve({ success: true })
          } else {
            resolve({ success: false, error: `ollama pull exited with code ${code}` })
          }
        })

        child.on('error', (err) => {
          resolve({ success: false, error: err.message })
        })
      } catch (err) {
        resolve({ success: false, error: (err as Error).message })
      }
    })
  })

  // Shell operations
  ipcMain.handle('shell:open-external', async (_, url: string): Promise<void> => {
    await shell.openExternal(url)
  })

  ipcMain.handle('shell:open-terminal', async (): Promise<void> => {
    // macOS: open Terminal app
    if (process.platform === 'darwin') {
      spawn('open', ['-a', 'Terminal'])
    }
  })
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

// Helper: Execute container command (tries podman first, then docker)
async function execContainerCommand(args: string[]): Promise<{ stdout: string; stderr: string }> {
  try {
    return await execFileAsync('podman', args)
  } catch {
    return await execFileAsync('docker', args)
  }
}
