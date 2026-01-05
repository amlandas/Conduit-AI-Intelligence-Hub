/**
 * Setup Wizard IPC Handlers
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * All operations delegate to `conduit` CLI commands - NO logic reimplementation.
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
  binaryPath?: string       // Path where the binary was found
  customPath?: string       // User-specified custom path (if any)
}

// NOTE: Custom paths are now managed via CLI: `conduit config set/unset deps.<name>.path`
// The old custom-paths.json file is deprecated.

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

  // Check dependencies - DELEGATES TO CLI: conduit deps status --json
  ipcMain.handle('setup:check-dependencies', async (): Promise<DependencyStatus[]> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const results: DependencyStatus[] = []

    try {
      const { stdout } = await execFileAsync(conduitPath, ['deps', 'status', '--json'])
      const deps = JSON.parse(stdout) as Record<string, { installed: boolean; path?: string; version?: string; required: boolean }>

      // Map CLI output to GUI format
      if (deps.homebrew) {
        results.push({
          name: 'Homebrew',
          installed: deps.homebrew.installed,
          version: deps.homebrew.version?.replace('Homebrew ', ''),
          required: false,
          binaryPath: deps.homebrew.path,
          installUrl: 'https://brew.sh',
        })
      }

      if (deps.ollama) {
        results.push({
          name: 'Ollama',
          installed: deps.ollama.installed,
          version: deps.ollama.version,
          required: true,
          binaryPath: deps.ollama.path,
          installUrl: 'https://ollama.com/download',
          brewFormula: 'ollama',
        })
      }

      // Container runtime: prefer Podman, fall back to Docker
      if (deps.podman?.installed) {
        results.push({
          name: 'Docker or Podman',
          installed: true,
          version: `Podman ${deps.podman.version?.split(' ').pop() || ''}`,
          required: true,
          binaryPath: deps.podman.path,
        })
      } else if (deps.docker?.installed) {
        results.push({
          name: 'Docker or Podman',
          installed: true,
          version: deps.docker.version,
          required: true,
          binaryPath: deps.docker.path,
        })
      } else {
        results.push({
          name: 'Docker or Podman',
          installed: false,
          required: true,
          installUrl: 'https://podman.io/getting-started/installation',
          brewFormula: 'podman',
        })
      }
    } catch (err) {
      // CLI not available or failed - return empty/error state
      console.error('Failed to check dependencies via CLI:', err)
      results.push(
        { name: 'Homebrew', installed: false, required: false, installUrl: 'https://brew.sh' },
        { name: 'Ollama', installed: false, required: true, installUrl: 'https://ollama.com/download', brewFormula: 'ollama' },
        { name: 'Docker or Podman', installed: false, required: true, installUrl: 'https://podman.io/getting-started/installation', brewFormula: 'podman' }
      )
    }

    return results
  })

  // Install dependency via Homebrew - DELEGATES TO CLI: conduit deps install
  ipcMain.handle('setup:install-dependency', async (_, { name, brewFormula }: { name: string; brewFormula: string }): Promise<{ success: boolean; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    try {
      // CLI handles platform detection and installation method
      await execFileAsync(conduitPath, ['deps', 'install', brewFormula])
      return { success: true }
    } catch (err) {
      return { success: false, error: `Failed to install ${name}: ${(err as Error).message}` }
    }
  })

  // Validate a user-specified binary path - DELEGATES TO CLI: conduit deps validate
  ipcMain.handle('setup:validate-binary-path', async (_, { binaryPath }: { name: string; binaryPath: string }): Promise<{ valid: boolean; version?: string; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    try {
      const { stdout } = await execFileAsync(conduitPath, ['deps', 'validate', binaryPath, '--json'])
      const result = JSON.parse(stdout) as { valid: boolean; version?: string; error?: string }
      return result
    } catch (err) {
      return { valid: false, error: `Validation failed: ${(err as Error).message}` }
    }
  })

  // Save a custom binary path - DELEGATES TO CLI: conduit config set
  ipcMain.handle('setup:save-custom-path', async (_, { name, binaryPath }: { name: string; binaryPath: string }): Promise<{ success: boolean; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    try {
      // Map dependency display names to internal names
      const nameMap: Record<string, string> = {
        'Homebrew': 'brew',
        'Ollama': 'ollama',
        'Docker or Podman': 'podman',
      }
      const internalName = nameMap[name] || name.toLowerCase()

      // Use CLI to persist custom path
      await execFileAsync(conduitPath, ['config', 'set', `deps.${internalName}.path`, binaryPath])
      return { success: true }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
  })

  // Clear a custom binary path - DELEGATES TO CLI: conduit config unset
  ipcMain.handle('setup:clear-custom-path', async (_, { name }: { name: string }): Promise<{ success: boolean; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    try {
      const nameMap: Record<string, string> = {
        'Homebrew': 'brew',
        'Ollama': 'ollama',
        'Docker or Podman': 'podman',
      }
      const internalName = nameMap[name] || name.toLowerCase()

      // Use CLI to remove custom path
      await execFileAsync(conduitPath, ['config', 'unset', `deps.${internalName}.path`])
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

  // Check services status - delegate to CLI commands
  ipcMain.handle('setup:check-services', async (): Promise<ServiceStatus[]> => {
    const results: ServiceStatus[] = []
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')

    // Check Conduit Daemon via CLI: conduit service status
    try {
      const { stdout } = await execFileAsync(conduitPath, ['service', 'status'])
      // CLI outputs "✓ Conduit daemon is running" when running
      const isRunning = stdout.includes('daemon is running')
      results.push({
        name: 'Conduit Daemon',
        running: isRunning,
        port: undefined,
      })
    } catch {
      results.push({
        name: 'Conduit Daemon',
        running: false,
      })
    }

    // Check Ollama via CLI: conduit ollama status
    try {
      const { stdout } = await execFileAsync(conduitPath, ['ollama', 'status'])
      // CLI outputs "✓ Ollama is running" when running
      const isRunning = stdout.includes('✓ Ollama is running') || stdout.includes('is running')
      results.push({
        name: 'Ollama',
        running: isRunning,
        port: isRunning ? 11434 : undefined,
      })
    } catch {
      results.push({
        name: 'Ollama',
        running: false,
      })
    }

    // Check Qdrant via CLI: conduit qdrant status
    try {
      const { stdout } = await execFileAsync(conduitPath, ['qdrant', 'status'])
      // CLI outputs "API Status:        ✓ reachable" when running
      const isRunning = stdout.includes('✓ reachable') || stdout.includes('✓ running')
      results.push({
        name: 'Qdrant',
        running: isRunning,
        port: isRunning ? 6333 : undefined,
        container: isRunning ? 'qdrant' : undefined,
      })
    } catch {
      results.push({
        name: 'Qdrant',
        running: false,
      })
    }

    // Check FalkorDB via CLI: conduit falkordb status
    try {
      const { stdout } = await execFileAsync(conduitPath, ['falkordb', 'status'])
      // CLI outputs "✗ not installed" when not running
      const isRunning = !stdout.includes('✗ not installed') && !stdout.includes('not running')
      results.push({
        name: 'FalkorDB',
        running: isRunning,
        port: isRunning ? 6379 : undefined,
        container: isRunning ? 'falkordb' : undefined,
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
          // Delegate to CLI: conduit service start
          const conduitPath = path.join(getCLIInstallPath(), 'conduit')
          await execFileAsync(conduitPath, ['service', 'start'])
          return { success: true }
        }
        case 'Qdrant': {
          // Delegate to CLI: conduit qdrant install
          const conduitPath = path.join(getCLIInstallPath(), 'conduit')
          await execFileAsync(conduitPath, ['qdrant', 'install'])
          return { success: true }
        }
        case 'FalkorDB': {
          // Delegate to CLI: conduit falkordb install
          const conduitPath = path.join(getCLIInstallPath(), 'conduit')
          await execFileAsync(conduitPath, ['falkordb', 'install'])
          return { success: true }
        }
        case 'Container Runtime':
        case 'Podman': {
          // Start Podman machine on macOS
          // Note: CLI doesn't have a dedicated command for this yet
          // This directly starts podman machine as infrastructure management
          if (process.platform === 'darwin') {
            try {
              // Try to start podman machine
              await execFileAsync('podman', ['machine', 'start'])
              return { success: true }
            } catch (podmanErr) {
              // If podman machine fails, maybe Docker is available?
              return {
                success: false,
                error: 'Could not start Podman machine. If using Docker, please start Docker Desktop manually.'
              }
            }
          } else {
            // On Linux, podman should just work if installed
            return { success: true }
          }
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

    // Start services in sequence - delegate to CLI commands
    const startService = async (name: string): Promise<{ success: boolean; error?: string }> => {
      try {
        const conduitPath = path.join(getCLIInstallPath(), 'conduit')
        switch (name) {
          case 'Conduit Daemon': {
            // Delegate to CLI: conduit service start
            await execFileAsync(conduitPath, ['service', 'start'])
            return { success: true }
          }
          case 'Qdrant': {
            // Delegate to CLI: conduit qdrant install
            await execFileAsync(conduitPath, ['qdrant', 'install'])
            return { success: true }
          }
          case 'FalkorDB': {
            // Delegate to CLI: conduit falkordb install
            await execFileAsync(conduitPath, ['falkordb', 'install'])
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

  // Check installed Ollama models - delegate to CLI
  ipcMain.handle('setup:check-models', async (): Promise<string[]> => {
    try {
      const conduitPath = path.join(getCLIInstallPath(), 'conduit')
      const { stdout } = await execFileAsync(conduitPath, ['ollama', 'models'])
      // CLI outputs "Available models:" followed by model list lines
      // Parse lines after "Available models:" to extract model names
      const lines = stdout.trim().split('\n')
      const models: string[] = []
      let inModelList = false
      for (const line of lines) {
        if (line.includes('Available models:')) {
          inModelList = true
          continue
        }
        if (line.includes('Required models status:')) {
          break // Stop at the required models section
        }
        if (inModelList && line.trim().startsWith('NAME')) {
          continue // Skip header line
        }
        if (inModelList && line.trim()) {
          // Model name is first column (e.g., "  nomic-embed-text:latest")
          const modelName = line.trim().split(/\s+/)[0]
          if (modelName && !modelName.startsWith('✓') && !modelName.startsWith('✗')) {
            models.push(modelName)
          }
        }
      }
      return models
    } catch {
      return []
    }
  })

  // Pull an Ollama model with progress - delegate to CLI
  ipcMain.handle('setup:pull-model', async (event, { model }: { model: string }): Promise<{ success: boolean; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')

    return new Promise((resolve) => {
      const windows = BrowserWindow.getAllWindows()
      let child: ChildProcess

      try {
        // Delegate to CLI: conduit ollama pull <model>
        child = spawn(conduitPath, ['ollama', 'pull', model])

        let lastProgress = 0
        const parseProgress = (output: string): void => {
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
        }

        child.stdout?.on('data', (data: Buffer) => {
          parseProgress(data.toString())
        })

        child.stderr?.on('data', (data: Buffer) => {
          // Ollama uses stderr for progress too
          parseProgress(data.toString())
        })

        child.on('close', (code) => {
          if (code === 0) {
            windows.forEach((window) => {
              window.webContents.send('ollama:pull-progress', { model, progress: 100 })
            })
            resolve({ success: true })
          } else {
            resolve({ success: false, error: `conduit ollama pull exited with code ${code}` })
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

  // Auto-install dependency (one-click installation) - DELEGATES TO CLI: conduit deps install
  // CLI handles platform detection, installation methods, and progress reporting
  ipcMain.handle('setup:auto-install-dependency', async (_, { name }: { name: string }): Promise<{ success: boolean; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const windows = BrowserWindow.getAllWindows()

    const sendProgress = (stage: string, percent: number, message: string): void => {
      windows.forEach((window) => {
        window.webContents.send('install:progress', { name, stage, percent, message })
      })
    }

    // Map display name to CLI dependency name
    const depNameMap: Record<string, string> = {
      'Ollama': 'ollama',
      'Podman': 'podman',
      'Docker or Podman': 'podman',
      'Homebrew': 'homebrew',
    }
    const depName = depNameMap[name]

    if (!depName) {
      return { success: false, error: `Auto-installation not supported for ${name}. Please install manually.` }
    }

    sendProgress('installing', 10, `Installing ${name}...`)

    return new Promise((resolve) => {
      const child = spawn(conduitPath, ['deps', 'install', depName], {
        env: { ...process.env }
      })

      let errorOutput = ''

      // Parse CLI progress output: PROGRESS:<percent>:<message>
      child.stdout?.on('data', (data: Buffer) => {
        const lines = data.toString().split('\n')
        for (const line of lines) {
          const match = line.match(/^PROGRESS:(\d+):(.+)$/)
          if (match) {
            const percent = parseInt(match[1], 10)
            const message = match[2]
            const stage = percent < 30 ? 'downloading' : percent < 80 ? 'installing' : percent < 100 ? 'verifying' : 'complete'
            sendProgress(stage, percent, message)
          }
        }
      })

      child.stderr?.on('data', (data: Buffer) => {
        errorOutput += data.toString()
      })

      child.on('close', (code) => {
        if (code === 0) {
          sendProgress('complete', 100, `${name} installed successfully!`)
          resolve({ success: true })
        } else {
          resolve({ success: false, error: `Installation failed: ${errorOutput || `exit code ${code}`}` })
        }
      })

      child.on('error', (err) => {
        resolve({ success: false, error: `Failed to run CLI: ${err.message}` })
      })
    })
  })

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

      // Parse CLI output for progress
      child.stdout?.on('data', (data: Buffer) => {
        const output = data.toString()
        lastOutput += output

        // Parse progress from CLI output
        // Example: "Syncing... 50% (100/200 chunks)"
        const progressMatch = output.match(/(\d+)%/)
        if (progressMatch) {
          const percent = parseInt(progressMatch[1], 10)
          sendProgress(percent, `Syncing... ${percent}%`, { processed, errors })
        }

        // Parse processed count
        const processedMatch = output.match(/Processed:\s*(\d+)/)
        if (processedMatch) {
          processed = parseInt(processedMatch[1], 10)
        }

        // Parse error count
        const errorsMatch = output.match(/Errors?:\s*(\d+)/)
        if (errorsMatch) {
          errors = parseInt(errorsMatch[1], 10)
        }

        // Parse error types
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
            // Don't fail the sync if MCP configuration fails
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

      // Parse CLI output for progress
      child.stdout?.on('data', (data: Buffer) => {
        const output = data.toString()
        lastOutput += output

        // Parse progress from CLI output
        const progressMatch = output.match(/(\d+)%/)
        if (progressMatch) {
          const percent = parseInt(progressMatch[1], 10)
          sendProgress(percent, `Extracting... ${percent}%`, { extracted, errors })
        }

        // Parse extracted count
        const extractedMatch = output.match(/Extracted:\s*(\d+)/)
        if (extractedMatch) {
          extracted = parseInt(extractedMatch[1], 10)
        }

        // Parse entities/relations count
        const entitiesMatch = output.match(/Entities:\s*(\d+)/)
        if (entitiesMatch) {
          extracted += parseInt(entitiesMatch[1], 10)
        }

        // Parse error count
        const errorsMatch = output.match(/Errors?:\s*(\d+)/)
        if (errorsMatch) {
          errors = parseInt(errorsMatch[1], 10)
        }

        // Parse error types
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

  // MCP Auto-configure - DELEGATES TO CLI: conduit mcp configure
  ipcMain.handle('mcp:configure', async (_, options?: { client?: string }): Promise<{ success: boolean; configured: boolean; configPath?: string; error?: string }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const clientId = options?.client || 'claude-code'

    try {
      const args = ['mcp', 'configure', '--client', clientId]
      const { stdout, stderr } = await execFileAsync(conduitPath, args)

      const alreadyConfigured = stdout.includes('already configured')
      const configured = stdout.includes('configured')

      // Extract config path from output
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

  // MCP Check - DELEGATES TO CLI: conduit mcp status --json
  // GUI should NOT inspect config files directly - CLI is single source of truth
  ipcMain.handle('mcp:check', async (_, options?: { client?: string }): Promise<{ configured: boolean; configPath?: string; clients?: Record<string, { configured: boolean; configPath: string }> }> => {
    const conduitPath = path.join(getCLIInstallPath(), 'conduit')
    const clientId = options?.client || 'claude-code'

    try {
      const { stdout } = await execFileAsync(conduitPath, ['mcp', 'status', '--json'])
      const result = JSON.parse(stdout) as Record<string, { configured: boolean; configPath: string; serverName?: string }>

      // Return configuration status for the requested client
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
      // Fallback: return not configured if CLI fails
      return { configured: false }
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

// NOTE: ensurePodmanMachineReady, execContainerCommand, and findBinary have been REMOVED.
// All container/dependency operations now delegate to CLI commands:
//   - conduit deps status --json
//   - conduit deps install <dep>
//   - conduit deps validate <path>
//   - conduit config set/unset/get
