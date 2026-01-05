import { useState } from 'react'
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
  Plus
} from 'lucide-react'

export function KBView(): JSX.Element {
  const {
    sources,
    searchResults,
    searchQuery,
    syncing,
    refresh,
    search,
    removeSource,
    addSource
  } = useKBStore()
  const { isFeatureVisible } = useSettingsStore()
  const [query, setQuery] = useState(searchQuery)
  const [addModalOpen, setAddModalOpen] = useState(false)

  const handleSearch = (e: React.FormEvent): void => {
    e.preventDefault()
    search(query)
  }

  const handleSync = async (sourceId: string): Promise<void> => {
    try {
      await window.conduit.syncKBSource(sourceId)
    } catch (err) {
      console.error('Failed to sync source:', err)
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

      {/* Sources */}
      <section>
        <h2 className="text-lg font-medium mb-3">Sources ({sources.length})</h2>
        {sources.length === 0 ? (
          <div className="card p-8 text-center">
            <FolderOpen className="w-12 h-12 mx-auto text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary" />
            <p className="mt-3 text-macos-text-secondary dark:text-macos-text-dark-secondary">
              No sources added yet
            </p>
            <p className="mt-1 text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
              Add a folder to start indexing documents
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
                  <div className="flex items-center gap-1">
                    <button
                      onClick={() => handleSync(source.id)}
                      disabled={syncing[source.id]}
                      className={cn(
                        'p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary',
                        syncing[source.id] && 'opacity-50'
                      )}
                      title="Sync"
                    >
                      {syncing[source.id] ? (
                        <Loader2 className="w-4 h-4 animate-spin" />
                      ) : (
                        <RefreshCw className="w-4 h-4" />
                      )}
                    </button>
                    <button
                      onClick={() => handleRemove(source.id)}
                      className="p-1.5 rounded hover:bg-macos-red/10 text-macos-red"
                      title="Remove"
                    >
                      <Trash2 className="w-4 h-4" />
                    </button>
                  </div>
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
