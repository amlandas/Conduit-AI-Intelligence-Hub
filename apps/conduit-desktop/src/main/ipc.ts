/**
 * IPC Handlers for Conduit Desktop
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * All operations delegate to `conduit` CLI commands with --json flag.
 * NO direct HTTP API calls to daemon.
 */

import { ipcMain, BrowserWindow } from 'electron'
import { execFile } from 'child_process'
import { promisify } from 'util'
import * as path from 'path'
import * as os from 'os'

const execFileAsync = promisify(execFile)

// Get the CLI installation path
function getCLIInstallPath(): string {
  const home = os.homedir()
  return path.join(home, '.local', 'bin')
}

// Execute CLI command and parse JSON output
async function execCLI(args: string[]): Promise<unknown> {
  const conduitPath = path.join(getCLIInstallPath(), 'conduit')
  const { stdout } = await execFileAsync(conduitPath, args)
  try {
    return JSON.parse(stdout)
  } catch {
    // If not valid JSON, return as string
    return stdout.trim()
  }
}

// Polling state for event simulation (replaces SSE)
let pollingInterval: NodeJS.Timeout | null = null
let isConnected = false

export function setupIpcHandlers(): void {
  // ═══════════════════════════════════════════════════════════════
  // Daemon Status - DELEGATES TO CLI: conduit status --json
  // ═══════════════════════════════════════════════════════════════
  ipcMain.handle('daemon:status', async () => {
    try {
      return await execCLI(['status', '--json'])
    } catch (err) {
      return { error: (err as Error).message, connected: false }
    }
  })

  // ═══════════════════════════════════════════════════════════════
  // Daemon Stats - DELEGATES TO CLI: conduit stats --json
  // ═══════════════════════════════════════════════════════════════
  ipcMain.handle('daemon:stats', async () => {
    try {
      return await execCLI(['stats', '--json'])
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // ═══════════════════════════════════════════════════════════════
  // Instance Management - DELEGATES TO CLI
  // ═══════════════════════════════════════════════════════════════

  // conduit list --json
  ipcMain.handle('instances:list', async () => {
    try {
      return await execCLI(['list', '--json'])
    } catch (err) {
      return { error: (err as Error).message, instances: [] }
    }
  })

  // conduit create <package-id> --name <name> --json
  ipcMain.handle('instances:create', async (_, config: { package_id: string; display_name?: string; package_version?: string; image_ref?: string; config?: Record<string, string> }) => {
    try {
      const args = ['create', config.package_id, '--json']
      if (config.display_name) {
        args.push('--name', config.display_name)
      }
      if (config.package_version) {
        args.push('--version', config.package_version)
      }
      if (config.image_ref) {
        args.push('--image', config.image_ref)
      }
      if (config.config) {
        const configStr = Object.entries(config.config).map(([k, v]) => `${k}=${v}`).join(',')
        args.push('--config', configStr)
      }
      return await execCLI(args)
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit start <id> --json
  ipcMain.handle('instances:start', async (_, id: string) => {
    try {
      return await execCLI(['start', id, '--json'])
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit stop <id> --json
  ipcMain.handle('instances:stop', async (_, id: string) => {
    try {
      return await execCLI(['stop', id, '--json'])
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit remove <id> --json
  ipcMain.handle('instances:delete', async (_, id: string) => {
    try {
      return await execCLI(['remove', id, '--json'])
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // ═══════════════════════════════════════════════════════════════
  // Knowledge Base - DELEGATES TO CLI
  // ═══════════════════════════════════════════════════════════════

  // conduit kb list --json
  ipcMain.handle('kb:sources', async () => {
    try {
      return await execCLI(['kb', 'list', '--json'])
    } catch (err) {
      return { error: (err as Error).message, sources: [] }
    }
  })

  // conduit kb add <path> --name <name> --json
  ipcMain.handle('kb:add-source', async (_, config: { path: string; name?: string; patterns?: string[]; excludes?: string[] }) => {
    try {
      const args = ['kb', 'add', config.path, '--json']
      if (config.name) {
        args.push('--name', config.name)
      }
      if (config.patterns && config.patterns.length > 0) {
        args.push('--patterns', config.patterns.join(','))
      }
      if (config.excludes && config.excludes.length > 0) {
        args.push('--excludes', config.excludes.join(','))
      }
      return await execCLI(args)
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit kb remove <id> --json
  ipcMain.handle('kb:remove-source', async (_, id: string) => {
    try {
      return await execCLI(['kb', 'remove', id, '--json'])
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // kb:sync is already handled via spawn in setup-ipc.ts with progress output
  // This handler provides a simple non-progress version
  ipcMain.handle('kb:sync', async (_, id: string) => {
    try {
      // Use longer timeout for sync (can take minutes)
      const conduitPath = path.join(getCLIInstallPath(), 'conduit')
      const { stdout } = await execFileAsync(conduitPath, ['kb', 'sync', id], { timeout: 600000 })
      return { success: true, output: stdout }
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit kb search <query> --json
  // Supports full RAG tuning options from RAGTuningPanel
  ipcMain.handle('kb:search', async (_, query: string, options?: {
    // Search mode
    searchMode?: 'hybrid' | 'semantic' | 'fts5'
    mode?: string  // Legacy: 'semantic' | 'fts5'
    raw?: boolean
    // RAG tuning options
    minScore?: number        // --min-score: Minimum similarity threshold (0.0-1.0)
    semanticWeight?: number  // --semantic-weight: Semantic vs lexical weight (0.0-1.0)
    mmrLambda?: number       // --mmr-lambda: Relevance vs diversity (0.0-1.0)
    maxResults?: number      // --limit: Maximum results to return
    reranking?: boolean      // false = --no-rerank flag
    // Legacy snake_case (backward compatibility)
    limit?: number
    min_score?: number
    semantic_weight?: number
  }) => {
    try {
      const args = ['kb', 'search', query, '--json']

      // Search mode (prefer new searchMode, fallback to legacy mode)
      const searchMode = options?.searchMode || options?.mode
      if (searchMode === 'semantic') {
        args.push('--semantic')
      } else if (searchMode === 'fts5') {
        args.push('--fts5')
      }
      // 'hybrid' is default, no flag needed

      if (options?.raw) {
        args.push('--raw')
      }

      // Max results (prefer maxResults, fallback to limit)
      const maxResults = options?.maxResults ?? options?.limit
      if (maxResults !== undefined && maxResults > 0) {
        args.push('--limit', String(maxResults))
      }

      // Min score threshold (prefer minScore, fallback to min_score)
      const minScore = options?.minScore ?? options?.min_score
      if (minScore !== undefined && minScore >= 0) {
        args.push('--min-score', String(minScore))
      }

      // Semantic weight (prefer semanticWeight, fallback to semantic_weight)
      const semanticWeight = options?.semanticWeight ?? options?.semantic_weight
      if (semanticWeight !== undefined && semanticWeight >= 0 && semanticWeight <= 1) {
        args.push('--semantic-weight', String(semanticWeight))
      }

      // MMR lambda (new option)
      if (options?.mmrLambda !== undefined && options.mmrLambda >= 0 && options.mmrLambda <= 1) {
        args.push('--mmr-lambda', String(options.mmrLambda))
      }

      // Reranking (false = disable)
      if (options?.reranking === false) {
        args.push('--no-rerank')
      }

      return await execCLI(args)
    } catch (err) {
      return { error: (err as Error).message, results: [], total_hits: 0 }
    }
  })

  // conduit kb kag-query <query> --format json
  ipcMain.handle('kb:kag-search', async (_, query: string, options?: { max_hops?: number }) => {
    try {
      const args = ['kb', 'kag-query', query, '--format', 'json']
      if (options?.max_hops) {
        args.push('--max-hops', String(options.max_hops))
      }
      return await execCLI(args)
    } catch (err) {
      return { error: (err as Error).message, entities: [], relations: [] }
    }
  })

  // ═══════════════════════════════════════════════════════════════
  // Instance Permissions (Advanced Mode)
  // DELEGATES TO CLI: conduit permissions <id> --json
  // ═══════════════════════════════════════════════════════════════

  ipcMain.handle('instances:permissions', async (_, id: string) => {
    try {
      return await execCLI(['permissions', id, '--json'])
    } catch (err) {
      return { error: (err as Error).message, permissions: [] }
    }
  })

  ipcMain.handle('instances:set-permission', async (_, id: string, permId: string, granted: boolean) => {
    try {
      return await execCLI(['permissions', id, '--set', `${permId}=${granted}`, '--json'])
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit audit <id> --json
  ipcMain.handle('instances:audit', async (_, id: string) => {
    try {
      return await execCLI(['audit', id, '--json'])
    } catch (err) {
      return { error: (err as Error).message, audit_logs: [] }
    }
  })

  // ═══════════════════════════════════════════════════════════════
  // Client Bindings
  // DELEGATES TO CLI: conduit client bindings/bind/unbind --json
  // ═══════════════════════════════════════════════════════════════

  // conduit client bindings --json
  ipcMain.handle('bindings:list', async () => {
    try {
      return await execCLI(['client', 'bindings', '--json'])
    } catch (err) {
      return { error: (err as Error).message, bindings: [] }
    }
  })

  // conduit client bind <instance-id> --client <client> --scope <scope> --json
  ipcMain.handle('bindings:create', async (_, config: { instance_id: string; client_id?: string; scope?: string }) => {
    try {
      const args = ['client', 'bind', config.instance_id, '--json']
      if (config.client_id) {
        args.push('--client', config.client_id)
      }
      if (config.scope) {
        args.push('--scope', config.scope)
      }
      return await execCLI(args)
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // conduit client unbind <instance-id> --client <client> --json
  ipcMain.handle('bindings:delete', async (_, instanceId: string, clientId?: string) => {
    try {
      const args = ['client', 'unbind', instanceId, '--json']
      if (clientId) {
        args.push('--client', clientId)
      }
      return await execCLI(args)
    } catch (err) {
      return { error: (err as Error).message }
    }
  })

  // ═══════════════════════════════════════════════════════════════
  // Events (Polling-based - replaces SSE)
  // Instead of SSE, we poll status periodically
  // ═══════════════════════════════════════════════════════════════

  ipcMain.handle('events:connect', () => {
    if (pollingInterval) {
      return { connected: true }
    }

    // Poll daemon status every 3 seconds
    pollingInterval = setInterval(async () => {
      try {
        const status = await execCLI(['status', '--json'])
        isConnected = true
        const windows = BrowserWindow.getAllWindows()
        windows.forEach((window) => {
          window.webContents.send('conduit:status', status)
        })
      } catch {
        if (isConnected) {
          isConnected = false
          const windows = BrowserWindow.getAllWindows()
          windows.forEach((window) => {
            window.webContents.send('conduit:disconnected')
          })
        }
      }
    }, 3000)

    // Initial check
    execCLI(['status', '--json']).then(() => {
      isConnected = true
      const windows = BrowserWindow.getAllWindows()
      windows.forEach((window) => {
        window.webContents.send('conduit:connected')
      })
    }).catch(() => {
      isConnected = false
    })

    return { connected: true }
  })

  ipcMain.handle('events:disconnect', () => {
    if (pollingInterval) {
      clearInterval(pollingInterval)
      pollingInterval = null
    }
    isConnected = false
    return { connected: false }
  })

  ipcMain.handle('events:status', () => {
    return { connected: isConnected }
  })
}
