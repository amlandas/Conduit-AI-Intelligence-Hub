/**
 * Terminal IPC Handlers
 *
 * Provides terminal emulation for the embedded terminal in setup wizard.
 * Uses node-pty for real PTY support, with fallback to child_process.spawn.
 */

import { ipcMain, BrowserWindow, app } from 'electron'
import { spawn, ChildProcess } from 'child_process'
import * as os from 'os'

// Try to load node-pty, but allow fallback if it fails
let pty: typeof import('node-pty') | null = null
try {
  pty = require('node-pty')
  console.log('[terminal-ipc] node-pty loaded successfully')
} catch (err) {
  console.warn('[terminal-ipc] node-pty not available, will use child_process fallback:', err)
}

// Store the active terminal instance (PTY or ChildProcess)
let activePty: import('node-pty').IPty | null = null
let activeProcess: ChildProcess | null = null

// Get the default shell for the current platform
function getDefaultShell(): string {
  if (process.platform === 'darwin') {
    // macOS: prefer zsh (default since Catalina)
    return '/bin/zsh'
  }
  if (process.platform === 'win32') {
    return process.env.COMSPEC || 'cmd.exe'
  }
  return process.env.SHELL || '/bin/bash'
}

// Build environment with proper PATH
function getEnv(): NodeJS.ProcessEnv {
  return {
    ...process.env,
    TERM: 'xterm-256color',
    // Ensure Homebrew and common paths are included
    PATH: `/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${process.env.PATH || ''}`,
    // Ensure HOME is set
    HOME: os.homedir(),
  }
}

// Send data to all windows
function sendToWindows(channel: string, data: unknown): void {
  BrowserWindow.getAllWindows().forEach((window) => {
    if (!window.isDestroyed()) {
      window.webContents.send(channel, data)
    }
  })
}

// Spawn using node-pty (preferred - full PTY support)
function spawnWithPty(command: string | undefined, cwd: string): { success: boolean; error?: string } {
  if (!pty) {
    return { success: false, error: 'node-pty not available' }
  }

  try {
    const shell = getDefaultShell()
    console.log('[terminal-ipc] Spawning PTY with shell:', shell, 'command:', command)

    if (command) {
      // Run specific command
      activePty = pty.spawn(shell, ['-c', command], {
        name: 'xterm-256color',
        cols: 80,
        rows: 24,
        cwd,
        env: getEnv() as { [key: string]: string },
      })
    } else {
      // Interactive shell
      activePty = pty.spawn(shell, [], {
        name: 'xterm-256color',
        cols: 80,
        rows: 24,
        cwd,
        env: getEnv() as { [key: string]: string },
      })
    }

    // Handle PTY data output
    activePty.onData((data: string) => {
      sendToWindows('terminal:data', data)
    })

    // Handle PTY exit
    activePty.onExit(({ exitCode }) => {
      console.log('[terminal-ipc] PTY exited with code:', exitCode)
      sendToWindows('terminal:exit', exitCode)
      activePty = null
    })

    return { success: true }
  } catch (err) {
    console.error('[terminal-ipc] PTY spawn failed:', err)
    activePty = null
    return { success: false, error: (err as Error).message }
  }
}

// Spawn using child_process (fallback - no PTY, but works without native modules)
function spawnWithProcess(command: string | undefined, cwd: string): { success: boolean; error?: string } {
  try {
    const shell = getDefaultShell()
    console.log('[terminal-ipc] Spawning child_process with shell:', shell, 'command:', command)

    const args = command ? ['-c', command] : ['-i']

    activeProcess = spawn(shell, args, {
      cwd,
      env: getEnv(),
      stdio: ['pipe', 'pipe', 'pipe'],
    })

    // Handle stdout
    activeProcess.stdout?.on('data', (data: Buffer) => {
      sendToWindows('terminal:data', data.toString())
    })

    // Handle stderr
    activeProcess.stderr?.on('data', (data: Buffer) => {
      sendToWindows('terminal:data', data.toString())
    })

    // Handle exit
    activeProcess.on('exit', (code) => {
      console.log('[terminal-ipc] Process exited with code:', code)
      sendToWindows('terminal:exit', code ?? 0)
      activeProcess = null
    })

    // Handle error
    activeProcess.on('error', (err) => {
      console.error('[terminal-ipc] Process error:', err)
      sendToWindows('terminal:data', `\r\n\x1b[31mError: ${err.message}\x1b[0m\r\n`)
      sendToWindows('terminal:exit', 1)
      activeProcess = null
    })

    // Send a welcome message since we don't have a real shell prompt
    if (!command) {
      sendToWindows('terminal:data', `$ `)
    }

    return { success: true }
  } catch (err) {
    console.error('[terminal-ipc] Process spawn failed:', err)
    activeProcess = null
    return { success: false, error: (err as Error).message }
  }
}

export function setupTerminalIpcHandlers(): void {
  /**
   * Spawn a new terminal session
   */
  ipcMain.handle('terminal:spawn', async (_, options?: { command?: string; cwd?: string }) => {
    // Kill any existing terminal
    if (activePty) {
      try { activePty.kill() } catch {}
      activePty = null
    }
    if (activeProcess) {
      try { activeProcess.kill() } catch {}
      activeProcess = null
    }

    const cwd = options?.cwd || os.homedir()
    console.log('[terminal-ipc] terminal:spawn called with:', options)

    // Try PTY first, fall back to child_process
    let result = spawnWithPty(options?.command, cwd)

    if (!result.success) {
      console.log('[terminal-ipc] PTY failed, trying child_process fallback')
      result = spawnWithProcess(options?.command, cwd)
    }

    return result
  })

  /**
   * Send input to the terminal
   */
  ipcMain.on('terminal:input', (_, data: string) => {
    if (activePty) {
      activePty.write(data)
    } else if (activeProcess?.stdin) {
      activeProcess.stdin.write(data)
    }
  })

  /**
   * Resize the terminal
   */
  ipcMain.on('terminal:resize', (_, { cols, rows }: { cols: number; rows: number }) => {
    if (activePty) {
      try {
        activePty.resize(cols, rows)
      } catch (err) {
        console.error('[terminal-ipc] Failed to resize PTY:', err)
      }
    }
    // Note: child_process doesn't support resize
  })

  /**
   * Kill the terminal session
   */
  ipcMain.handle('terminal:kill', async () => {
    try {
      if (activePty) {
        activePty.kill()
        activePty = null
      }
      if (activeProcess) {
        activeProcess.kill()
        activeProcess = null
      }
      return { success: true }
    } catch (err) {
      return { success: false, error: (err as Error).message }
    }
  })

  /**
   * Check if a terminal session is active
   */
  ipcMain.handle('terminal:is-active', async () => {
    return activePty !== null || activeProcess !== null
  })
}

// Cleanup on app quit
app.on('before-quit', () => {
  if (activePty) {
    try { activePty.kill() } catch {}
    activePty = null
  }
  if (activeProcess) {
    try { activeProcess.kill() } catch {}
    activeProcess = null
  }
})
