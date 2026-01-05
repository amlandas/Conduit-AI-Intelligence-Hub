import { useState, useEffect, useCallback } from 'react'
import { cn } from '@/lib/utils'
import { useKBStore, SearchResult } from '@/stores'
import { Search, FileText, Loader2, X } from 'lucide-react'

interface SearchModalProps {
  open: boolean
  onClose: () => void
}

export function SearchModal({ open, onClose }: SearchModalProps): JSX.Element | null {
  const [query, setQuery] = useState('')
  const [results, setResults] = useState<SearchResult[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedIndex, setSelectedIndex] = useState(0)
  const { sources } = useKBStore()

  const handleSearch = useCallback(async (searchQuery: string) => {
    if (!searchQuery.trim()) {
      setResults([])
      return
    }

    setLoading(true)
    try {
      const result = await window.conduit.searchKB(searchQuery)
      if (result && typeof result === 'object' && 'results' in result) {
        setResults((result as { results: SearchResult[] }).results || [])
        setSelectedIndex(0)
      }
    } catch (err) {
      console.error('Search failed:', err)
      setResults([])
    } finally {
      setLoading(false)
    }
  }, [])

  // Debounced search
  useEffect(() => {
    const timer = setTimeout(() => {
      handleSearch(query)
    }, 300)
    return () => clearTimeout(timer)
  }, [query, handleSearch])

  // Keyboard navigation
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent): void => {
      if (!open) return

      if (e.key === 'Escape') {
        onClose()
      } else if (e.key === 'ArrowDown') {
        e.preventDefault()
        setSelectedIndex((i) => Math.min(i + 1, results.length - 1))
      } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        setSelectedIndex((i) => Math.max(i - 1, 0))
      } else if (e.key === 'Enter' && results[selectedIndex]) {
        // Could open the file or navigate to it
        console.log('Selected:', results[selectedIndex])
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [open, onClose, results, selectedIndex])

  // Reset on close
  useEffect(() => {
    if (!open) {
      setQuery('')
      setResults([])
      setSelectedIndex(0)
    }
  }, [open])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-start justify-center pt-[15vh]"
      onClick={onClose}
    >
      <div className="absolute inset-0 bg-black/20 dark:bg-black/40" />
      <div
        className="relative w-[640px] bg-white dark:bg-macos-bg-dark-secondary rounded-xl shadow-macos-lg overflow-hidden animate-slide-in-bottom"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search input */}
        <div className="flex items-center gap-3 px-4 py-3 border-b border-macos-separator dark:border-macos-separator-dark">
          {loading ? (
            <Loader2 className="w-5 h-5 text-macos-blue animate-spin flex-shrink-0" />
          ) : (
            <Search className="w-5 h-5 text-macos-text-secondary dark:text-macos-text-dark-secondary flex-shrink-0" />
          )}
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search knowledge base..."
            className="flex-1 bg-transparent outline-none text-base"
            autoFocus
          />
          {query && (
            <button
              onClick={() => setQuery('')}
              className="p-1 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
            >
              <X className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
            </button>
          )}
          <kbd className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary px-1.5 py-0.5 rounded bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">
            ESC
          </kbd>
        </div>

        {/* Results */}
        <div className="max-h-[400px] overflow-auto">
          {query && results.length === 0 && !loading ? (
            <div className="p-6 text-center text-macos-text-secondary dark:text-macos-text-dark-secondary">
              <p>No results found for "{query}"</p>
              {sources.length === 0 && (
                <p className="text-sm mt-1 text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
                  Add knowledge sources to enable search
                </p>
              )}
            </div>
          ) : results.length > 0 ? (
            <div className="py-2">
              {results.map((result, index) => (
                <button
                  key={result.id}
                  className={cn(
                    'w-full px-4 py-3 flex items-start gap-3 text-left transition-colors',
                    index === selectedIndex
                      ? 'bg-macos-blue/10'
                      : 'hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary'
                  )}
                  onMouseEnter={() => setSelectedIndex(index)}
                >
                  <FileText
                    className={cn(
                      'w-5 h-5 flex-shrink-0 mt-0.5',
                      index === selectedIndex ? 'text-macos-blue' : 'text-macos-text-secondary'
                    )}
                  />
                  <div className="flex-1 min-w-0">
                    <p className="font-medium text-sm truncate">{result.path}</p>
                    <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary line-clamp-2 mt-0.5">
                      {result.content}
                    </p>
                  </div>
                  <span className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary flex-shrink-0">
                    {(result.score * 100).toFixed(0)}%
                  </span>
                </button>
              ))}
            </div>
          ) : !query ? (
            <div className="p-6 text-center text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
              <p>Type to search across {sources.length} knowledge source{sources.length !== 1 ? 's' : ''}</p>
              <div className="flex items-center justify-center gap-4 mt-3 text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
                <span><kbd className="px-1 py-0.5 rounded bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">↑↓</kbd> Navigate</span>
                <span><kbd className="px-1 py-0.5 rounded bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">↵</kbd> Select</span>
                <span><kbd className="px-1 py-0.5 rounded bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">ESC</kbd> Close</span>
              </div>
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}
