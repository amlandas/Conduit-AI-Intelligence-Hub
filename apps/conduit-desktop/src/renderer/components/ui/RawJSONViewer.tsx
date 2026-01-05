import { useState } from 'react'
import { cn } from '@/lib/utils'
import { Code, Copy, Check, ChevronDown, ChevronRight } from 'lucide-react'

interface RawJSONViewerProps {
  data: unknown
  title?: string
  defaultExpanded?: boolean
  className?: string
}

export function RawJSONViewer({
  data,
  title = 'Raw JSON',
  defaultExpanded = false,
  className
}: RawJSONViewerProps): JSX.Element {
  const [expanded, setExpanded] = useState(defaultExpanded)
  const [copied, setCopied] = useState(false)

  const jsonString = JSON.stringify(data, null, 2)

  const handleCopy = (): void => {
    navigator.clipboard.writeText(jsonString)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <div className={cn('rounded-lg border border-macos-separator dark:border-macos-separator-dark overflow-hidden', className)}>
      {/* Header */}
      <button
        onClick={() => setExpanded(!expanded)}
        className="w-full flex items-center justify-between px-3 py-2 bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50 hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary transition-colors"
      >
        <div className="flex items-center gap-2">
          {expanded ? (
            <ChevronDown className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
          ) : (
            <ChevronRight className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
          )}
          <Code className="w-4 h-4 text-macos-purple" />
          <span className="text-sm font-medium">{title}</span>
        </div>
        {expanded && (
          <button
            onClick={(e) => {
              e.stopPropagation()
              handleCopy()
            }}
            className="flex items-center gap-1 px-2 py-1 rounded text-xs hover:bg-macos-bg-tertiary dark:hover:bg-macos-separator-dark transition-colors"
          >
            {copied ? (
              <>
                <Check className="w-3 h-3 text-macos-green" />
                <span className="text-macos-green">Copied!</span>
              </>
            ) : (
              <>
                <Copy className="w-3 h-3" />
                <span>Copy</span>
              </>
            )}
          </button>
        )}
      </button>

      {/* Content */}
      {expanded && (
        <pre className="p-3 text-xs font-mono overflow-auto max-h-[400px] bg-macos-bg-dark-primary text-macos-text-dark-primary">
          <code>{jsonString}</code>
        </pre>
      )}
    </div>
  )
}

interface RawJSONToggleProps {
  data: unknown
  children: React.ReactNode
  className?: string
}

export function RawJSONToggle({ data, children, className }: RawJSONToggleProps): JSX.Element {
  const [showRaw, setShowRaw] = useState(false)

  return (
    <div className={className}>
      {/* Toggle Button */}
      <div className="flex justify-end mb-2">
        <button
          onClick={() => setShowRaw(!showRaw)}
          className={cn(
            'flex items-center gap-1.5 px-2 py-1 rounded text-xs font-medium transition-colors',
            showRaw
              ? 'bg-macos-purple/10 text-macos-purple'
              : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary hover:bg-macos-bg-tertiary'
          )}
        >
          <Code className="w-3.5 h-3.5" />
          {showRaw ? 'Show UI' : 'Show JSON'}
        </button>
      </div>

      {/* Content */}
      {showRaw ? (
        <pre className="p-4 rounded-lg text-xs font-mono overflow-auto max-h-[600px] bg-macos-bg-dark-primary text-macos-text-dark-primary border border-macos-separator-dark">
          <code>{JSON.stringify(data, null, 2)}</code>
        </pre>
      ) : (
        children
      )}
    </div>
  )
}
