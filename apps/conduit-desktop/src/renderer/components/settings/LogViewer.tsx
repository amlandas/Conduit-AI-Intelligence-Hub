import { useState, useEffect, useRef, useCallback } from 'react'
import { cn } from '@/lib/utils'
import {
  ScrollText,
  Pause,
  Play,
  Trash2,
  Download,
  Filter,
  ChevronDown
} from 'lucide-react'

interface LogEntry {
  id: string
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  component: string
  message: string
  details?: Record<string, unknown>
}

interface LogViewerProps {
  className?: string
}

const LEVEL_STYLES: Record<string, { bg: string; text: string; badge: string }> = {
  debug: {
    bg: 'bg-macos-gray/5',
    text: 'text-macos-text-secondary',
    badge: 'bg-macos-gray/20 text-macos-gray'
  },
  info: {
    bg: 'bg-macos-blue/5',
    text: 'text-macos-text-primary',
    badge: 'bg-macos-blue/20 text-macos-blue'
  },
  warn: {
    bg: 'bg-macos-orange/5',
    text: 'text-macos-orange',
    badge: 'bg-macos-orange/20 text-macos-orange'
  },
  error: {
    bg: 'bg-macos-red/5',
    text: 'text-macos-red',
    badge: 'bg-macos-red/20 text-macos-red'
  }
}

// Sample log entries for demonstration
const SAMPLE_LOGS: LogEntry[] = [
  {
    id: '1',
    timestamp: new Date().toISOString(),
    level: 'info',
    component: 'daemon',
    message: 'Daemon started successfully'
  },
  {
    id: '2',
    timestamp: new Date().toISOString(),
    level: 'info',
    component: 'sse',
    message: 'SSE endpoint listening on /api/v1/events'
  },
  {
    id: '3',
    timestamp: new Date().toISOString(),
    level: 'debug',
    component: 'kb',
    message: 'Initializing knowledge base store'
  },
  {
    id: '4',
    timestamp: new Date().toISOString(),
    level: 'info',
    component: 'qdrant',
    message: 'Connected to Qdrant at localhost:6333',
    details: { vectors: 531, collections: 2 }
  },
  {
    id: '5',
    timestamp: new Date().toISOString(),
    level: 'warn',
    component: 'ollama',
    message: 'Model qwen2.5-coder:7b not found, using fallback'
  }
]

export function LogViewer({ className }: LogViewerProps): JSX.Element {
  const [logs, setLogs] = useState<LogEntry[]>(SAMPLE_LOGS)
  const [paused, setPaused] = useState(false)
  const [filterLevel, setFilterLevel] = useState<string | null>(null)
  const [filterComponent, setFilterComponent] = useState<string | null>(null)
  const [showFilters, setShowFilters] = useState(false)
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set())
  const scrollRef = useRef<HTMLDivElement>(null)
  const autoScrollRef = useRef(true)

  // Simulate incoming logs
  useEffect(() => {
    if (paused) return

    const components = ['daemon', 'kb', 'qdrant', 'ollama', 'sse', 'lifecycle']
    const levels: LogEntry['level'][] = ['debug', 'info', 'info', 'info', 'warn', 'error']
    const messages = [
      'Processing request',
      'Cache hit',
      'Sync completed',
      'Health check passed',
      'Connection established',
      'Query executed'
    ]

    const interval = setInterval(() => {
      const newLog: LogEntry = {
        id: Date.now().toString(),
        timestamp: new Date().toISOString(),
        level: levels[Math.floor(Math.random() * levels.length)],
        component: components[Math.floor(Math.random() * components.length)],
        message: messages[Math.floor(Math.random() * messages.length)]
      }
      setLogs((prev) => [...prev.slice(-499), newLog])
    }, 2000)

    return () => clearInterval(interval)
  }, [paused])

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScrollRef.current && scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs])

  const handleScroll = useCallback(() => {
    if (scrollRef.current) {
      const { scrollTop, scrollHeight, clientHeight } = scrollRef.current
      autoScrollRef.current = scrollHeight - scrollTop - clientHeight < 50
    }
  }, [])

  const toggleExpanded = (id: string): void => {
    setExpandedIds((prev) => {
      const newSet = new Set(prev)
      if (newSet.has(id)) {
        newSet.delete(id)
      } else {
        newSet.add(id)
      }
      return newSet
    })
  }

  const clearLogs = (): void => {
    setLogs([])
  }

  const exportLogs = (): void => {
    const content = logs
      .map((log) => `[${log.timestamp}] [${log.level.toUpperCase()}] [${log.component}] ${log.message}`)
      .join('\n')
    const blob = new Blob([content], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `conduit-logs-${new Date().toISOString().split('T')[0]}.log`
    a.click()
    URL.revokeObjectURL(url)
  }

  const filteredLogs = logs.filter((log) => {
    if (filterLevel && log.level !== filterLevel) return false
    if (filterComponent && log.component !== filterComponent) return false
    return true
  })

  const uniqueComponents = [...new Set(logs.map((log) => log.component))]

  return (
    <div className={cn('card overflow-hidden', className)}>
      {/* Header */}
      <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ScrollText className="w-5 h-5 text-macos-purple" />
            <h3 className="font-medium">Log Viewer</h3>
            {!paused && (
              <span className="flex items-center gap-1 text-xs text-macos-green">
                <span className="w-2 h-2 rounded-full bg-macos-green animate-pulse" />
                Live
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setShowFilters(!showFilters)}
              className={cn(
                'p-1.5 rounded transition-colors',
                showFilters
                  ? 'bg-macos-blue/10 text-macos-blue'
                  : 'hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary'
              )}
              title="Filters"
            >
              <Filter className="w-4 h-4" />
            </button>
            <button
              onClick={() => setPaused(!paused)}
              className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
              title={paused ? 'Resume' : 'Pause'}
            >
              {paused ? <Play className="w-4 h-4" /> : <Pause className="w-4 h-4" />}
            </button>
            <button
              onClick={clearLogs}
              className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
              title="Clear logs"
            >
              <Trash2 className="w-4 h-4" />
            </button>
            <button
              onClick={exportLogs}
              className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
              title="Export logs"
            >
              <Download className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>

      {/* Filters */}
      {showFilters && (
        <div className="px-4 py-3 border-b border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30">
          <div className="flex items-center gap-4">
            <div className="flex items-center gap-2">
              <span className="text-xs text-macos-text-secondary">Level:</span>
              <div className="flex gap-1">
                {['debug', 'info', 'warn', 'error'].map((level) => (
                  <button
                    key={level}
                    onClick={() => setFilterLevel(filterLevel === level ? null : level)}
                    className={cn(
                      'px-2 py-1 rounded text-xs font-medium capitalize transition-colors',
                      filterLevel === level
                        ? LEVEL_STYLES[level].badge
                        : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary hover:opacity-80'
                    )}
                  >
                    {level}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-macos-text-secondary">Component:</span>
              <select
                value={filterComponent || ''}
                onChange={(e) => setFilterComponent(e.target.value || null)}
                className="px-2 py-1 rounded text-xs bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary border-0 outline-none"
              >
                <option value="">All</option>
                {uniqueComponents.map((comp) => (
                  <option key={comp} value={comp}>
                    {comp}
                  </option>
                ))}
              </select>
            </div>
            {(filterLevel || filterComponent) && (
              <button
                onClick={() => {
                  setFilterLevel(null)
                  setFilterComponent(null)
                }}
                className="text-xs text-macos-blue hover:underline"
              >
                Clear filters
              </button>
            )}
          </div>
        </div>
      )}

      {/* Log entries */}
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        className="h-[400px] overflow-auto font-mono text-xs"
      >
        {filteredLogs.length === 0 ? (
          <div className="h-full flex items-center justify-center text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
            No logs to display
          </div>
        ) : (
          filteredLogs.map((log) => {
            const styles = LEVEL_STYLES[log.level]
            const isExpanded = expandedIds.has(log.id)

            return (
              <div
                key={log.id}
                className={cn('px-3 py-1.5 border-b border-macos-separator/50 dark:border-macos-separator-dark/50', styles.bg)}
              >
                <div className="flex items-start gap-2">
                  <span className="text-macos-text-tertiary dark:text-macos-text-dark-tertiary w-[140px] flex-shrink-0">
                    {new Date(log.timestamp).toLocaleTimeString()}
                  </span>
                  <span className={cn('px-1.5 py-0.5 rounded text-[10px] uppercase font-medium w-[50px] text-center', styles.badge)}>
                    {log.level}
                  </span>
                  <span className="text-macos-purple w-[80px] flex-shrink-0">
                    [{log.component}]
                  </span>
                  <span className={cn('flex-1', styles.text)}>{log.message}</span>
                  {log.details && (
                    <button
                      onClick={() => toggleExpanded(log.id)}
                      className="p-0.5 hover:bg-black/10 rounded"
                    >
                      <ChevronDown
                        className={cn('w-3 h-3 transition-transform', isExpanded && 'rotate-180')}
                      />
                    </button>
                  )}
                </div>
                {log.details && isExpanded && (
                  <pre className="mt-1 ml-[280px] p-2 rounded bg-black/10 text-[10px] overflow-auto">
                    {JSON.stringify(log.details, null, 2)}
                  </pre>
                )}
              </div>
            )
          })
        )}
      </div>

      {/* Footer */}
      <div className="px-4 py-2 border-t border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30 text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
        <div className="flex items-center justify-between">
          <span>{filteredLogs.length} entries</span>
          <span>{paused ? 'Paused' : 'Auto-scrolling'}</span>
        </div>
      </div>
    </div>
  )
}
