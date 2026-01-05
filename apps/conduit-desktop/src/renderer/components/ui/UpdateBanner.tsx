import { useState, useEffect, useCallback } from 'react'
import { cn } from '@/lib/utils'
import {
  Download,
  RefreshCw,
  X,
  CheckCircle,
  AlertCircle,
  Loader2,
  Sparkles
} from 'lucide-react'

interface UpdateStatus {
  checking: boolean
  available: boolean
  downloading: boolean
  downloaded: boolean
  progress: number
  error: string | null
  version: string | null
  releaseNotes: string | null
}

interface UpdateBannerProps {
  className?: string
}

export function UpdateBanner({ className }: UpdateBannerProps): JSX.Element | null {
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [dismissed, setDismissed] = useState(false)
  const [showNotes, setShowNotes] = useState(false)

  useEffect(() => {
    // Get initial status
    window.conduit.getUpdateStatus().then(setStatus)

    // Subscribe to status updates
    const unsubscribe = window.conduit.onUpdateStatus((newStatus: UpdateStatus) => {
      setStatus(newStatus)
      // Reset dismissed state when a new update is available
      if (newStatus.available && !status?.available) {
        setDismissed(false)
      }
    })

    return unsubscribe
  }, [])

  const handleCheckForUpdates = useCallback(async () => {
    await window.conduit.checkForUpdates()
  }, [])

  const handleDownload = useCallback(async () => {
    await window.conduit.downloadUpdate()
  }, [])

  const handleInstall = useCallback(() => {
    window.conduit.installUpdate()
  }, [])

  const handleDismiss = useCallback(() => {
    setDismissed(true)
  }, [])

  // Don't show if dismissed or no relevant status
  if (dismissed || !status) {
    return null
  }

  // Show error banner
  if (status.error) {
    return (
      <div
        className={cn(
          'flex items-center gap-3 px-4 py-2 bg-macos-red/10 border-b border-macos-red/20',
          className
        )}
      >
        <AlertCircle className="w-4 h-4 text-macos-red flex-shrink-0" />
        <span className="text-sm text-macos-red flex-1">
          Update error: {status.error}
        </span>
        <button
          onClick={handleCheckForUpdates}
          className="text-xs text-macos-red hover:underline"
        >
          Retry
        </button>
        <button
          onClick={handleDismiss}
          className="p-1 hover:bg-macos-red/20 rounded"
        >
          <X className="w-3 h-3 text-macos-red" />
        </button>
      </div>
    )
  }

  // Show checking status
  if (status.checking) {
    return (
      <div
        className={cn(
          'flex items-center gap-3 px-4 py-2 bg-macos-blue/5 border-b border-macos-blue/20',
          className
        )}
      >
        <Loader2 className="w-4 h-4 text-macos-blue animate-spin flex-shrink-0" />
        <span className="text-sm text-macos-text-secondary flex-1">
          Checking for updates...
        </span>
      </div>
    )
  }

  // Show download progress
  if (status.downloading) {
    return (
      <div
        className={cn(
          'flex items-center gap-3 px-4 py-2 bg-macos-blue/5 border-b border-macos-blue/20',
          className
        )}
      >
        <Download className="w-4 h-4 text-macos-blue flex-shrink-0" />
        <div className="flex-1">
          <div className="flex items-center justify-between mb-1">
            <span className="text-sm text-macos-text-secondary">
              Downloading update v{status.version}...
            </span>
            <span className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
              {Math.round(status.progress)}%
            </span>
          </div>
          <div className="h-1.5 bg-macos-bg-secondary rounded-full overflow-hidden">
            <div
              className="h-full bg-macos-blue transition-all duration-300"
              style={{ width: `${status.progress}%` }}
            />
          </div>
        </div>
      </div>
    )
  }

  // Show ready to install
  if (status.downloaded) {
    return (
      <div
        className={cn(
          'flex items-center gap-3 px-4 py-2 bg-macos-green/10 border-b border-macos-green/20',
          className
        )}
      >
        <CheckCircle className="w-4 h-4 text-macos-green flex-shrink-0" />
        <span className="text-sm text-macos-text-primary flex-1">
          Update v{status.version} ready to install
        </span>
        <button
          onClick={handleInstall}
          className="flex items-center gap-1.5 px-3 py-1 bg-macos-green text-white text-sm font-medium rounded-lg hover:bg-macos-green/90 transition-colors"
        >
          <RefreshCw className="w-3 h-3" />
          Restart & Install
        </button>
        <button
          onClick={handleDismiss}
          className="p-1 hover:bg-macos-green/20 rounded"
        >
          <X className="w-3 h-3 text-macos-green" />
        </button>
      </div>
    )
  }

  // Show update available
  if (status.available) {
    return (
      <div className={cn('border-b border-macos-blue/20', className)}>
        <div className="flex items-center gap-3 px-4 py-2 bg-macos-blue/5">
          <Sparkles className="w-4 h-4 text-macos-blue flex-shrink-0" />
          <span className="text-sm text-macos-text-primary flex-1">
            Update v{status.version} is available
          </span>
          {status.releaseNotes && (
            <button
              onClick={() => setShowNotes(!showNotes)}
              className="text-xs text-macos-blue hover:underline"
            >
              {showNotes ? 'Hide notes' : 'View notes'}
            </button>
          )}
          <button
            onClick={handleDownload}
            className="flex items-center gap-1.5 px-3 py-1 bg-macos-blue text-white text-sm font-medium rounded-lg hover:bg-macos-blue/90 transition-colors"
          >
            <Download className="w-3 h-3" />
            Download
          </button>
          <button
            onClick={handleDismiss}
            className="p-1 hover:bg-macos-blue/20 rounded"
          >
            <X className="w-3 h-3 text-macos-blue" />
          </button>
        </div>
        {showNotes && status.releaseNotes && (
          <div className="px-4 py-2 bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50 text-xs text-macos-text-secondary max-h-32 overflow-auto">
            <pre className="whitespace-pre-wrap font-sans">{status.releaseNotes}</pre>
          </div>
        )}
      </div>
    )
  }

  // No update available or nothing to show
  return null
}

// Manual check for updates button (for Settings)
interface CheckUpdatesButtonProps {
  className?: string
}

export function CheckUpdatesButton({ className }: CheckUpdatesButtonProps): JSX.Element {
  const [checking, setChecking] = useState(false)
  const [lastCheck, setLastCheck] = useState<string | null>(null)

  const handleCheck = async (): Promise<void> => {
    setChecking(true)
    try {
      await window.conduit.checkForUpdates()
      setLastCheck(new Date().toLocaleTimeString())
    } finally {
      setChecking(false)
    }
  }

  return (
    <div className={cn('flex items-center gap-3', className)}>
      <button
        onClick={handleCheck}
        disabled={checking}
        className={cn(
          'flex items-center gap-2 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
          checking
            ? 'bg-macos-bg-secondary text-macos-text-tertiary'
            : 'bg-macos-blue text-white hover:bg-macos-blue/90'
        )}
      >
        {checking ? (
          <Loader2 className="w-4 h-4 animate-spin" />
        ) : (
          <RefreshCw className="w-4 h-4" />
        )}
        {checking ? 'Checking...' : 'Check for Updates'}
      </button>
      {lastCheck && (
        <span className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
          Last checked: {lastCheck}
        </span>
      )}
    </div>
  )
}
