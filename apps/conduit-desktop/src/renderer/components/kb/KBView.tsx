/**
 * KBView Component
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * KB sync operations delegate to CLI with progress tracking.
 */
import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { useKBStore, useSettingsStore } from '@/stores'
import { AddSourceModal } from './AddSourceModal'
import { RAGTuningPanel } from './RAGTuningPanel'
import { KAGPanel } from './KAGPanel'
import {
  FolderOpen,
  RefreshCw,
  Trash2,
  Search,
  FileText,
  Loader2,
  Plus,
  Database,
  GitBranch,
  CheckCircle,
  XCircle,
  AlertCircle
} from 'lucide-react'

interface SyncResult {
  success: boolean
  processed?: number
  extracted?: number
  errors: number
  errorTypes: string[]
  error?: string
}

export function KBView(): JSX.Element {
  const {
    sources,
    searchResults,
    searchQuery,
    refresh,
    search,
    removeSource,
    addSource
  } = useKBStore()
  const { isFeatureVisible } = useSettingsStore()
  const [query, setQuery] = useState(searchQuery)
  const [addModalOpen, setAddModalOpen] = useState(false)

  // Sync state
  const [ragSyncing, setRagSyncing] = useState(false)
  const [kagSyncing, setKagSyncing] = useState(false)
  const [ragProgress, setRagProgress] = useState(0)
  const [kagProgress, setKagProgress] = useState(0)
  const [ragResult, setRagResult] = useState<SyncResult | null>(null)
  const [kagResult, setKagResult] = useState<SyncResult | null>(null)
  const [mcpConfigured, setMcpConfigured] = useState(false)

  // Subscribe to progress events and check MCP config
  useEffect(() => {
    const cleanupRag = window.conduit.onKBSyncProgress(({ percent }: { percent: number }) => {
      setRagProgress(percent)
    })

    const cleanupKag = window.conduit.onKAGSyncProgress(({ percent }: { percent: number }) => {
      setKagProgress(percent)
    })

    // Check if MCP is already configured
    window.conduit.checkMCPConfig().then(({ configured }: { configured: boolean }) => {
      setMcpConfigured(configured)
    })

    return () => {
      cleanupRag()
      cleanupKag()
    }
  }, [])

  const handleSearch = (e: React.FormEvent): void => {
    e.preventDefault()
    search(query)
  }

  // RAG Sync - delegate to CLI: conduit kb sync
  // Also auto-configures MCP KB server after successful sync
  const handleRAGSync = async (): Promise<void> => {
    setRagSyncing(true)
    setRagProgress(0)
    setRagResult(null)
    try {
      const result = await window.conduit.syncKBWithProgress()
      setRagResult(result)
      // Refresh sources to get updated counts
      await refresh()
      // Check MCP configuration (should be auto-configured by CLI)
      if (result.success) {
        const { configured } = await window.conduit.checkMCPConfig()
        setMcpConfigured(configured)
      }
    } catch (err) {
      setRagResult({ success: false, errors: 1, errorTypes: [(err as Error).message] })
    } finally {
      setRagSyncing(false)
    }
  }

  // KAG Sync - delegate to CLI: conduit kb kag-sync
  const handleKAGSync = async (): Promise<void> => {
    setKagSyncing(true)
    setKagProgress(0)
    setKagResult(null)
    try {
      const result = await window.conduit.kagSyncWithProgress()
      setKagResult(result)
      // Refresh sources to get updated counts
      await refresh()
    } catch (err) {
      setKagResult({ success: false, errors: 1, errorTypes: [(err as Error).message] })
    } finally {
      setKagSyncing(false)
    }
  }

  const handleRemove = async (sourceId: string): Promise<void> => {
    try {
      await window.conduit.removeKBSource(sourceId)
      removeSource(sourceId)
    } catch (err) {
      console.error('Failed to remove source:', err)
    }
  }

  const handleAddSource = async (name: string, path: string): Promise<void> => {
    const result = await window.conduit.addKBSource({ name, path })
    if (result && typeof result === 'object' && 'id' in result) {
      addSource({
        id: (result as { id: string }).id,
        name,
        path
      })
    }
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Knowledge Base</h1>
        <div className="flex items-center gap-2">
          <button onClick={() => refresh()} className="btn btn-secondary">
            <RefreshCw className="w-4 h-4 mr-1.5" />
            Refresh
          </button>
          <button onClick={() => setAddModalOpen(true)} className="btn btn-primary">
            <Plus className="w-4 h-4 mr-1.5" />
            Add Source
          </button>
        </div>
      </div>

      {/* Search */}
      <form onSubmit={handleSearch} className="relative">
        <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-5 h-5 text-macos-text-secondary dark:text-macos-text-dark-secondary" />
        <input
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search documents..."
          className="w-full pl-10 pr-4 py-2.5 rounded-lg border border-macos-separator dark:border-macos-separator-dark bg-white dark:bg-macos-bg-dark-secondary focus:outline-none focus:ring-2 focus:ring-macos-blue"
        />
      </form>

      {/* Search Results */}
      {searchResults.length > 0 && (
        <section>
          <h2 className="text-lg font-medium mb-3">
            Search Results ({searchResults.length})
          </h2>
          <div className="space-y-2">
            {searchResults.map((result) => (
              <div key={result.id} className="card p-4">
                <div className="flex items-start gap-3">
                  <FileText className="w-5 h-5 text-macos-blue flex-shrink-0 mt-0.5" />
                  <div className="flex-1 min-w-0">
                    <h3 className="font-medium text-sm truncate">{result.path}</h3>
                    <p className="mt-1 text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary line-clamp-2">
                      {result.content}
                    </p>
                    <div className="mt-2 text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
                      Score: {(result.score * 100).toFixed(1)}%
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </section>
      )}

      {/* Sync Actions */}
      {sources.length > 0 && (
        <section>
          <h2 className="text-lg font-medium mb-3">Sync Operations</h2>
          <div className="grid grid-cols-2 gap-4">
            {/* RAG Sync Card */}
            <div className="card p-4">
              <div className="flex items-center gap-3 mb-3">
                <div className="p-2 rounded-lg bg-macos-blue/10">
                  <Database className="w-5 h-5 text-macos-blue" />
                </div>
                <div>
                  <h3 className="font-medium text-sm">RAG Sync</h3>
                  <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
                    Index documents for semantic search
                  </p>
                </div>
              </div>

              {ragSyncing && (
                <div className="mb-3">
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="text-macos-text-secondary dark:text-macos-text-dark-secondary">Syncing...</span>
                    <span className="text-macos-blue">{ragProgress}%</span>
                  </div>
                  <div className="h-2 bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary rounded-full overflow-hidden">
                    <div
                      className="h-full bg-macos-blue transition-all duration-300"
                      style={{ width: `${ragProgress}%` }}
                    />
                  </div>
                </div>
              )}

              {ragResult && (
                <div className={cn(
                  'mb-3 p-2 rounded-lg text-xs',
                  ragResult.success ? 'bg-macos-green/10 text-macos-green' : 'bg-macos-red/10 text-macos-red'
                )}>
                  <div className="flex items-center gap-2">
                    {ragResult.success ? (
                      <CheckCircle className="w-4 h-4" />
                    ) : (
                      <XCircle className="w-4 h-4" />
                    )}
                    <span>
                      {ragResult.success
                        ? `Processed: ${ragResult.processed || 0} chunks`
                        : ragResult.error || 'Sync failed'}
                    </span>
                  </div>
                  {ragResult.errors > 0 && (
                    <div className="mt-1 flex items-center gap-1 text-macos-orange">
                      <AlertCircle className="w-3 h-3" />
                      <span>{ragResult.errors} errors</span>
                    </div>
                  )}
                  {ragResult.success && mcpConfigured && (
                    <div className="mt-1 flex items-center gap-1 text-macos-blue">
                      <CheckCircle className="w-3 h-3" />
                      <span>MCP KB server auto-configured</span>
                    </div>
                  )}
                </div>
              )}

              <button
                onClick={handleRAGSync}
                disabled={ragSyncing}
                className="btn btn-primary w-full"
              >
                {ragSyncing ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                    Syncing...
                  </>
                ) : (
                  <>
                    <Database className="w-4 h-4 mr-1.5" />
                    Run RAG Sync
                  </>
                )}
              </button>
            </div>

            {/* KAG Sync Card */}
            <div className="card p-4">
              <div className="flex items-center gap-3 mb-3">
                <div className="p-2 rounded-lg bg-macos-purple/10">
                  <GitBranch className="w-5 h-5 text-macos-purple" />
                </div>
                <div>
                  <h3 className="font-medium text-sm">KAG Sync</h3>
                  <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
                    Extract entities for knowledge graph
                  </p>
                </div>
              </div>

              {kagSyncing && (
                <div className="mb-3">
                  <div className="flex items-center justify-between text-xs mb-1">
                    <span className="text-macos-text-secondary dark:text-macos-text-dark-secondary">Extracting...</span>
                    <span className="text-macos-purple">{kagProgress}%</span>
                  </div>
                  <div className="h-2 bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary rounded-full overflow-hidden">
                    <div
                      className="h-full bg-macos-purple transition-all duration-300"
                      style={{ width: `${kagProgress}%` }}
                    />
                  </div>
                </div>
              )}

              {kagResult && (
                <div className={cn(
                  'mb-3 p-2 rounded-lg text-xs',
                  kagResult.success ? 'bg-macos-green/10 text-macos-green' : 'bg-macos-red/10 text-macos-red'
                )}>
                  <div className="flex items-center gap-2">
                    {kagResult.success ? (
                      <CheckCircle className="w-4 h-4" />
                    ) : (
                      <XCircle className="w-4 h-4" />
                    )}
                    <span>
                      {kagResult.success
                        ? `Extracted: ${kagResult.extracted || 0} entities`
                        : kagResult.error || 'Extraction failed'}
                    </span>
                  </div>
                  {kagResult.errors > 0 && (
                    <div className="mt-1 flex items-center gap-1 text-macos-orange">
                      <AlertCircle className="w-3 h-3" />
                      <span>{kagResult.errors} errors</span>
                    </div>
                  )}
                </div>
              )}

              <button
                onClick={handleKAGSync}
                disabled={kagSyncing}
                className="btn btn-secondary w-full"
              >
                {kagSyncing ? (
                  <>
                    <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                    Extracting...
                  </>
                ) : (
                  <>
                    <GitBranch className="w-4 h-4 mr-1.5" />
                    Run KAG Sync
                  </>
                )}
              </button>
            </div>
          </div>
        </section>
      )}

      {/* Sources */}
      <section>
        <h2 className="text-lg font-medium mb-3">Sources ({sources.length})</h2>
        {sources.length === 0 ? (
          <div className="card p-8 text-center">
            <FolderOpen className="w-12 h-12 mx-auto text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
            <p className="mt-3 text-macos-text-secondary dark:text-macos-text-dark-secondary">
              No sources added yet
            </p>
            <p className="mt-1 text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
              Add a folder to start indexing documents
            </p>
            <p className="mt-2 text-xs text-macos-blue">
              After adding a source, run RAG Sync to index documents
            </p>
          </div>
        ) : (
          <div className="space-y-2">
            {sources.map((source) => (
              <div key={source.id} className="card p-4">
                <div className="flex items-center gap-3">
                  <FolderOpen className="w-5 h-5 text-macos-blue flex-shrink-0" />
                  <div className="flex-1 min-w-0">
                    <h3 className="font-medium text-sm">{source.name}</h3>
                    <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary truncate">
                      {source.path}
                    </p>
                  </div>
                  <div className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
                    {source.documents || 0} docs
                  </div>
                  <button
                    onClick={() => handleRemove(source.id)}
                    className="p-1.5 rounded hover:bg-macos-red/10 text-macos-red"
                    title="Remove source"
                  >
                    <Trash2 className="w-4 h-4" />
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>

      {/* Advanced Mode: RAG Tuning Panel */}
      {isFeatureVisible('showRAGTuning') && (
        <RAGTuningPanel className="mt-6" />
      )}

      {/* Advanced Mode: KAG Panel */}
      {isFeatureVisible('showKAGPanel') && (
        <KAGPanel className="mt-6" showSettings={true} />
      )}

      <AddSourceModal
        open={addModalOpen}
        onClose={() => setAddModalOpen(false)}
        onAdd={handleAddSource}
      />
    </div>
  )
}
