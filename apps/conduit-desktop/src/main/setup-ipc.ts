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

// Known installation paths for dependencies
// Electron apps don't inherit shell PATH, so we check common locations directly
const KNOWN_PATHS: Record<string, string[]> = {
  brew: [
    '/opt/homebrew/bin/brew',       // macOS Apple Silicon
    '/usr/local/bin/brew',          // macOS Intel
    '/home/linuxbrew/.linuxbrew/bin/brew', // Linux
  ],
  ollama: [
    '/opt/homebrew/bin/ollama',     // Homebrew Apple Silicon
    '/usr/local/bin/ollama',        // Homebrew Intel / Official installer
    '/usr/bin/ollama',              // System package
    path.join(os.homedir(), '.ollama', 'bin', 'ollama'), // Ollama's own installer
  ],
  docker: [
    '/opt/homebrew/bin/docker',     // Homebrew
    '/usr/local/bin/docker',        // Docker Desktop symlink
    '/usr/bin/docker',              // System package
    '/Applications/Docker.app/Contents/Resources/bin/docker', // App bundle
  ],
  podman: [
    '/opt/homebrew/bin/podman',     // Homebrew Apple Silicon
    '/usr/local/bin/podman',        // Homebrew Intel / System
    '/usr/bin/podman',              // System package
  ],
}

interface BinaryResult {
  found: boolean
  path?: string
  version?: string
}

// Find a binary by checking custom paths, known paths, then PATH
async function findBinary(name: string, versionArgs: string[] = ['--version']): Promise<BinaryResult> {
  // 0. Check for user-configured custom path first
  try {
    const customPaths = await loadCustomPaths()
    const customPath = customPaths[name]
    if (customPath) {
      try {
        await fsPromises.access(customPath, fs.constants.X_OK)
        const { stdout } = await execFileAsync(customPath, versionArgs)
        return { found: true, path: customPath, version: stdout.trim() }
      } catch {
        // Custom path no longer valid, continue with other checks
      }
    }
  } catch {
    // Ignore errors loading custom paths
  }

  const knownPaths = KNOWN_PATHS[name] || []

  // 1. Check known installation paths
  for (const binPath of knownPaths) {
    try {
      await fsPromises.access(binPath, fs.constants.X_OK)
      // File exists and is executable, try to get version
      try {
        const { stdout } = await execFileAsync(binPath, versionArgs)
        return { found: true, path: binPath, version: stdout.trim() }
      } catch {
        // Binary exists but version command failed - still counts as found
        return { found: true, path: binPath }
      }
    } catch {
      // Path doesn't exist or not executable, continue to next
    }
  }

  // 2. Fall back to PATH-based detection
  try {
    const { stdout } = await execFileAsync(name, versionArgs)
    return { found: true, version: stdout.trim() }
  } catch {
    return { found: false }
  }
}

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
  binaryPath?: string       // Path where the binary was found
  customPath?: string       // User-specified custom path (if any)
}

// Store for custom binary paths (persisted in app data)
const customPathsFile = path.join(app.getPath('userData'), 'custom-paths.json')

async function loadCustomPaths(): Promise<Record<string, string>> {
  try {
    const content = await fsPromises.readFile(customPathsFile, 'utf-8')
    return JSON.parse(content)
  } catch {
    return {}
  }
}

async function saveCustomPaths(paths: Record<string, string>): Promise<void> {
  await fsPromises.writeFile(customPathsFile, JSON.stringify(paths, null, 2))
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
  // Uses findBinary() to check known installation paths first (fixes Homebrew detection)
  ipcMain.handle('setup:check-dependencies', async (): Promise<DependencyStatus[]> => {
    const results: DependencyStatus[] = []

    // Load custom paths for reference
    const customPaths = await loadCustomPaths()

    // Check Homebrew
    const brewResult = await findBinary('brew')
    if (brewResult.found) {
      results.push({
        name: 'Homebrew',
        installed: true,
        version: brewResult.version?.split('\n')[0].replace('Homebrew ', ''),
        required: false,
        binaryPath: brewResult.path,
        customPath: customPaths['brew'],
      })
    } else {
      results.push({
        name: 'Homebrew',
        installed: false,
        required: false,
        installUrl: 'https://brew.sh',
      })
    }

    // Check Ollama
    const ollamaResult = await findBinary('ollama')
    if (ollamaResult.found) {
      results.push({
        name: 'Ollama',
        installed: true,
        version: ollamaResult.version,
        required: true,
        binaryPath: ollamaResult.path,
        customPath: customPaths['ollama'],
      })
    } else {
      results.push({
        name: 'Ollama',
        installed: false,
        required: true,
        installUrl: 'https://ollama.com/download',
        brewFormula: 'ollama',
      })
    }

    // Check Docker or Podman (prefer Podman)
    const podmanResult = await findBinary('podman')
    if (podmanResult.found) {
      results.push({
        name: 'Docker or Podman',
        installed: true,
        version: `Podman ${podmanResult.version?.split(' ').pop() || ''}`,
        required: true,
        binaryPath: podmanResult.path,
        customPath: customPaths['podman'],
      })
    } else {
      // Try Docker
      const dockerResult = await findBinary('docker')
      if (dockerResult.found) {
        results.push({
          name: 'Docker or Podman',
          installed: true,
          version: dockerResult.version,
          required: true,
          binaryPath: dockerResult.path,
          customPath: customPaths['docker'],
        })
      } else {
        // Neither found
        results.push({
          name: 'Docker or Podman',
          installed: false,
          required: true,
          installUrl: 'https://podman.io/getting-started/installation',
          brewFormula: 'podman',
        })
      }
    }

    return results
  })

  // Install dependency via Homebrew
  // Uses findBinary() to locate brew in known paths
  ipcMain.handle('setup:install-dependency', async (_, { name, brewFormula }: { name: string; brewFormula: string }): Promise<{ success: boolean; error?: string }> => {
    try {
      const brewResult = await findBinary('brew')
      if (!brewResult.found) {
        return { success: false, error: 'Homebrew is not installed. Please install it first.' }
      }
      const brewPath = brewResult.path || 'brew'
      await execFileAsync(brewPath, ['install', brewFormula])
      return { success: true }
    } catch (err) {
      return { success: false, error: `Failed to install ${name}: ${(err as Error).message}` }
    }
  })

  // Validate a user-specified binary path
  ipcMain.handle('setup:validate-binary-path', async (_, { name, binaryPath }: { name: string; binaryPath: string }): Promise<{ valid: boolean; version?: string; error?: string }> => {
    try {
      // Check if file exists and is executable
      await fsPromises.access(binaryPath, fs.constants.X_OK)

      // Try to run --version to verify it works
      const { stdout } = await execFileAsync(binaryPath, ['--version'])
      return { valid: true, version: stdout.trim() }
    } catch (err) {
      const error = err as NodeJS.ErrnoException
      if (error.code === 'ENOENT') {
        return { valid: false, error: 'File not found' }
      } else if (error.code === 'EACCES') {
        return { valid: false, error: 'File is not executable' }
      } else {
        return { valid: false, error: `Failed to run binary: ${error.message}` }
      }
    }
  })

  // Save a custom binary path
  ipcMain.handle('setup:save-custom-path', async (_, { name, binaryPath }: { name: string; binaryPath: string }): Promise<{ success: boolean; error?: string }> => {
    try {
      // Map dependency display names to internal names
      const nameMap: Record<string, string> = {
        'Homebrew': 'brew',
        'Ollama': 'ollama',
        'Docker or Podman': 'podman', // Save as podman by default, could be docker
      }
      const internalName = nameMap[name] || name.toLowerCase()

      const customPaths = await loadCustomPaths()
      customPaths[internalName] = binaryPath
      await saveCustomPaths(customPaths)
      return { success: true }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
  })

  // Clear a custom binary path
  ipcMain.handle('setup:clear-custom-path', async (_, { name }: { name: string }): Promise<{ success: boolean; error?: string }> => {
    try {
      const nameMap: Record<string, string> = {
        'Homebrew': 'brew',
        'Ollama': 'ollama',
        'Docker or Podman': 'podman',
      }
      const internalName = nameMap[name] || name.toLowerCase()

      const customPaths = await loadCustomPaths()
      delete customPaths[internalName]
      await saveCustomPaths(customPaths)
      return { success: true }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
  })

  // Open file dialog to browse for a binary
  ipcMain.handle('setup:browse-for-binary', async (_, { name }: { name: string }): Promise<{ path?: string; cancelled: boolean }> => {
    const { dialog } = await import('electron')
    const result = await dialog.showOpenDialog({
      title: `Locate ${name}`,
      properties: ['openFile'],
      message: `Select the ${name} executable`,
    })

    if (result.canceled || result.filePaths.length === 0) {
      return { cancelled: true }
    }

    return { path: result.filePaths[0], cancelled: false }
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
  // Uses findBinary() to locate ollama in known paths
  ipcMain.handle('setup:check-models', async (): Promise<string[]> => {
    try {
      const ollamaResult = await findBinary('ollama')
      if (!ollamaResult.found) {
        return []
      }
      const ollamaPath = ollamaResult.path || 'ollama'
      const { stdout } = await execFileAsync(ollamaPath, ['list'])
      const lines = stdout.trim().split('\n').slice(1) // Skip header
      return lines.map(line => line.split(/\s+/)[0]).filter(Boolean)
    } catch {
      return []
    }
  })

  // Pull an Ollama model with progress
  // Uses findBinary() to locate ollama in known paths
  ipcMain.handle('setup:pull-model', async (event, { model }: { model: string }): Promise<{ success: boolean; error?: string }> => {
    // Find ollama binary first
    const ollamaResult = await findBinary('ollama')
    if (!ollamaResult.found) {
      return { success: false, error: 'Ollama is not installed' }
    }
    const ollamaPath = ollamaResult.path || 'ollama'

    return new Promise((resolve) => {
      const windows = BrowserWindow.getAllWindows()
      let child: ChildProcess

      try {
        child = spawn(ollamaPath, ['pull', model])

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

  // Auto-install dependency (one-click installation)
  // Supports: Ollama (official installer), Podman (Homebrew)
  ipcMain.handle('setup:auto-install-dependency', async (_, { name }: { name: string }): Promise<{ success: boolean; error?: string }> => {
    const windows = BrowserWindow.getAllWindows()

    const sendProgress = (stage: string, percent: number, message: string): void => {
      windows.forEach((window) => {
        window.webContents.send('install:progress', { name, stage, percent, message })
      })
    }

    try {
      switch (name) {
        case 'Ollama': {
          // Install Ollama using official installer script
          sendProgress('downloading', 10, 'Downloading Ollama installer...')

          return new Promise((resolve) => {
            // Use the official Ollama installer
            // This script is safe and doesn't require sudo for most operations
            const child = spawn('sh', ['-c', 'curl -fsSL https://ollama.com/install.sh | sh'], {
              shell: true,
              env: { ...process.env }
            })

            let output = ''
            let errorOutput = ''

            child.stdout?.on('data', (data: Buffer) => {
              output += data.toString()
              // Parse progress from installer output
              if (output.includes('Downloading')) {
                sendProgress('downloading', 30, 'Downloading Ollama...')
              } else if (output.includes('Installing')) {
                sendProgress('installing', 60, 'Installing Ollama...')
              }
            })

            child.stderr?.on('data', (data: Buffer) => {
              errorOutput += data.toString()
              // Installer also outputs progress to stderr
              if (errorOutput.includes('Downloading') || errorOutput.includes('curl')) {
                sendProgress('downloading', 40, 'Downloading Ollama...')
              }
            })

            child.on('close', async (code) => {
              if (code === 0) {
                sendProgress('verifying', 90, 'Verifying installation...')
                // Verify installation
                const result = await findBinary('ollama')
                if (result.found) {
                  sendProgress('complete', 100, 'Ollama installed successfully!')
                  resolve({ success: true })
                } else {
                  resolve({ success: false, error: 'Installation completed but Ollama not found in PATH' })
                }
              } else {
                resolve({ success: false, error: `Installer exited with code ${code}: ${errorOutput}` })
              }
            })

            child.on('error', (err) => {
              resolve({ success: false, error: `Failed to run installer: ${err.message}` })
            })
          })
        }

        case 'Podman':
        case 'Docker or Podman': {
          // Check if Homebrew is available for Podman installation
          const brewResult = await findBinary('brew')
          if (!brewResult.found) {
            return {
              success: false,
              error: 'Homebrew is required for automatic Podman installation. Please install Homebrew first, or download Podman manually.'
            }
          }

          sendProgress('installing', 20, 'Installing Podman via Homebrew...')

          return new Promise((resolve) => {
            const brewPath = brewResult.path || 'brew'
            const child = spawn(brewPath, ['install', 'podman'], {
              env: { ...process.env }
            })

            let errorOutput = ''

            child.stdout?.on('data', (data: Buffer) => {
              const output = data.toString()
              // Homebrew outputs progress to stdout
              if (output.includes('Downloading')) {
                sendProgress('downloading', 40, 'Downloading Podman...')
              } else if (output.includes('Installing') || output.includes('Pouring')) {
                sendProgress('installing', 60, 'Installing Podman...')
              }
            })

            child.stderr?.on('data', (data: Buffer) => {
              errorOutput += data.toString()
            })

            child.on('close', async (code) => {
              if (code === 0) {
                sendProgress('verifying', 90, 'Verifying installation...')
                // Verify installation
                const result = await findBinary('podman')
                if (result.found) {
                  sendProgress('complete', 100, 'Podman installed successfully!')
                  resolve({ success: true })
                } else {
                  resolve({ success: false, error: 'Installation completed but Podman not found' })
                }
              } else {
                resolve({ success: false, error: `Homebrew install failed: ${errorOutput}` })
              }
            })

            child.on('error', (err) => {
              resolve({ success: false, error: `Failed to run Homebrew: ${err.message}` })
            })
          })
        }

        case 'Homebrew': {
          // Install Homebrew using official installer
          sendProgress('downloading', 10, 'Downloading Homebrew installer...')

          return new Promise((resolve) => {
            // Official Homebrew installer - requires user interaction for sudo
            // We use osascript to run in a new Terminal window so user can provide password
            const installCommand = '/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"'

            if (process.platform === 'darwin') {
              // Open Terminal with the install command
              const script = `tell application "Terminal"
                activate
                do script "${installCommand}"
              end tell`

              spawn('osascript', ['-e', script], { detached: true, stdio: 'ignore' }).unref()

              // We can't track progress for Terminal-based install
              sendProgress('installing', 50, 'Homebrew installer opened in Terminal. Follow the prompts to complete installation.')
              resolve({ success: true })
            } else {
              resolve({ success: false, error: 'Automatic Homebrew installation is only supported on macOS' })
            }
          })
        }

        default:
          return { success: false, error: `Auto-installation not supported for ${name}. Please install manually.` }
      }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
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
// Uses findBinary() to locate the binary in known paths
async function execContainerCommand(args: string[]): Promise<{ stdout: string; stderr: string }> {
  // Try Podman first
  const podmanResult = await findBinary('podman')
  if (podmanResult.found && podmanResult.path) {
    return await execFileAsync(podmanResult.path, args)
  }

  // Fall back to Docker
  const dockerResult = await findBinary('docker')
  if (dockerResult.found && dockerResult.path) {
    return await execFileAsync(dockerResult.path, args)
  }

  // Last resort: try PATH-based execution
  try {
    return await execFileAsync('podman', args)
  } catch {
    return await execFileAsync('docker', args)
  }
}
