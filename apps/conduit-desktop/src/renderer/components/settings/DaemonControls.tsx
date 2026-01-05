import { useState } from 'react'
import { cn } from '@/lib/utils'
import { useDaemonStore } from '@/stores'
import {
  Server,
  Play,
  Square,
  RefreshCw,
  Loader2,
  Terminal,
  AlertTriangle,
  CheckCircle,
  XCircle
} from 'lucide-react'

interface DaemonControlsProps {
  className?: string
}

type LogLevel = 'debug' | 'info' | 'warn' | 'error'

export function DaemonControls({ className }: DaemonControlsProps): JSX.Element {
  const { status, refresh } = useDaemonStore()
  const [starting, setStarting] = useState(false)
  const [stopping, setStopping] = useState(false)
  const [restarting, setRestarting] = useState(false)
  const [logLevel, setLogLevel] = useState<LogLevel>('info')
  const [actionError, setActionError] = useState<string | null>(null)
  const [actionSuccess, setActionSuccess] = useState<string | null>(null)

  const clearMessages = (): void => {
    setActionError(null)
    setActionSuccess(null)
  }

  const handleStart = async (): Promise<void> => {
    clearMessages()
    setStarting(true)
    try {
      // In production, this would call the daemon control API
      // await window.conduit.startDaemon?.()
      await new Promise((resolve) => setTimeout(resolve, 1500)) // Simulate
      setActionSuccess('Daemon started successfully')
      refresh()
    } catch (err) {
      setActionError(`Failed to start daemon: ${(err as Error).message}`)
    } finally {
      setStarting(false)
    }
  }

  const handleStop = async (): Promise<void> => {
    clearMessages()
    setStopping(true)
    try {
      // await window.conduit.stopDaemon?.()
      await new Promise((resolve) => setTimeout(resolve, 1000))
      setActionSuccess('Daemon stopped')
      refresh()
    } catch (err) {
      setActionError(`Failed to stop daemon: ${(err as Error).message}`)
    } finally {
      setStopping(false)
    }
  }

  const handleRestart = async (): Promise<void> => {
    clearMessages()
    setRestarting(true)
    try {
      // await window.conduit.restartDaemon?.()
      await new Promise((resolve) => setTimeout(resolve, 2000))
      setActionSuccess('Daemon restarted successfully')
      refresh()
    } catch (err) {
      setActionError(`Failed to restart daemon: ${(err as Error).message}`)
    } finally {
      setRestarting(false)
    }
  }

  const handleLogLevelChange = async (level: LogLevel): Promise<void> => {
    setLogLevel(level)
    try {
      // await window.conduit.setLogLevel?.(level)
    } catch (err) {
      console.error('Failed to set log level:', err)
    }
  }

  const isProcessing = starting || stopping || restarting

  return (
    <div className={cn('card', className)}>
      {/* Header */}
      <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Server className="w-5 h-5 text-macos-blue" />
            <h3 className="font-medium">Daemon Controls</h3>
          </div>
          <div className="flex items-center gap-2">
            <span
              className={cn(
                'status-dot',
                status.connected ? 'status-dot-online' : 'status-dot-offline'
              )}
            />
            <span className="text-sm text-macos-text-secondary">
              {status.connected ? 'Running' : 'Stopped'}
            </span>
          </div>
        </div>
      </div>

      {/* Status Messages */}
      {actionError && (
        <div className="px-4 py-2 bg-macos-red/10 border-b border-macos-red/20 flex items-center gap-2 text-macos-red">
          <XCircle className="w-4 h-4" />
          <span className="text-sm">{actionError}</span>
        </div>
      )}
      {actionSuccess && (
        <div className="px-4 py-2 bg-macos-green/10 border-b border-macos-green/20 flex items-center gap-2 text-macos-green">
          <CheckCircle className="w-4 h-4" />
          <span className="text-sm">{actionSuccess}</span>
        </div>
      )}

      <div className="p-4 space-y-6">
        {/* Control Buttons */}
        <div className="space-y-3">
          <h4 className="text-xs font-medium text-macos-text-secondary uppercase tracking-wide">
            Service Control
          </h4>
          <div className="flex gap-2">
            <button
              onClick={handleStart}
              disabled={status.connected || isProcessing}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2.5 rounded-lg font-medium transition-colors',
                !status.connected && !isProcessing
                  ? 'bg-macos-green text-white hover:bg-macos-green/90'
                  : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary text-macos-text-tertiary'
              )}
            >
              {starting ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Play className="w-4 h-4" />
              )}
              Start
            </button>
            <button
              onClick={handleStop}
              disabled={!status.connected || isProcessing}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2.5 rounded-lg font-medium transition-colors',
                status.connected && !isProcessing
                  ? 'bg-macos-red text-white hover:bg-macos-red/90'
                  : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary text-macos-text-tertiary'
              )}
            >
              {stopping ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <Square className="w-4 h-4" />
              )}
              Stop
            </button>
            <button
              onClick={handleRestart}
              disabled={!status.connected || isProcessing}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2.5 rounded-lg font-medium transition-colors',
                status.connected && !isProcessing
                  ? 'bg-macos-orange text-white hover:bg-macos-orange/90'
                  : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary text-macos-text-tertiary'
              )}
            >
              {restarting ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : (
                <RefreshCw className="w-4 h-4" />
              )}
              Restart
            </button>
          </div>
        </div>

        {/* Log Level */}
        <div className="space-y-3">
          <div className="flex items-center gap-2">
            <Terminal className="w-4 h-4 text-macos-text-secondary" />
            <h4 className="text-xs font-medium text-macos-text-secondary uppercase tracking-wide">
              Log Level
            </h4>
          </div>
          <div className="flex gap-2">
            {(['debug', 'info', 'warn', 'error'] as LogLevel[]).map((level) => (
              <button
                key={level}
                onClick={() => handleLogLevelChange(level)}
                className={cn(
                  'flex-1 py-2 rounded-lg text-sm font-medium transition-colors capitalize',
                  logLevel === level
                    ? 'bg-macos-blue text-white'
                    : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary hover:bg-macos-bg-tertiary'
                )}
              >
                {level}
              </button>
            ))}
          </div>
          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
            {logLevel === 'debug' && 'Verbose output for debugging. May impact performance.'}
            {logLevel === 'info' && 'Standard logging. Shows normal operation details.'}
            {logLevel === 'warn' && 'Only warnings and errors. Recommended for production.'}
            {logLevel === 'error' && 'Minimal logging. Only critical errors.'}
          </p>
        </div>

        {/* Status Info */}
        {status.connected && (
          <div className="space-y-3">
            <h4 className="text-xs font-medium text-macos-text-secondary uppercase tracking-wide">
              Daemon Info
            </h4>
            <div className="grid grid-cols-2 gap-3 text-sm">
              <div className="p-3 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50">
                <p className="text-macos-text-tertiary dark:text-macos-text-dark-tertiary text-xs">Uptime</p>
                <p className="font-mono">{status.uptime || '—'}</p>
              </div>
              <div className="p-3 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50">
                <p className="text-macos-text-tertiary dark:text-macos-text-dark-tertiary text-xs">Version</p>
                <p className="font-mono">{status.version || '0.1.0'}</p>
              </div>
              <div className="p-3 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50">
                <p className="text-macos-text-tertiary dark:text-macos-text-dark-tertiary text-xs">PID</p>
                <p className="font-mono">{status.pid || '—'}</p>
              </div>
              <div className="p-3 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50">
                <p className="text-macos-text-tertiary dark:text-macos-text-dark-tertiary text-xs">Socket</p>
                <p className="font-mono truncate">~/.conduit/conduit.sock</p>
              </div>
            </div>
          </div>
        )}

        {/* Warning */}
        <div className="flex items-start gap-2 p-3 rounded-lg bg-macos-orange/10 text-macos-orange">
          <AlertTriangle className="w-4 h-4 flex-shrink-0 mt-0.5" />
          <div className="text-xs">
            <p className="font-medium">Caution</p>
            <p className="mt-0.5 text-macos-text-secondary">
              Stopping the daemon will disconnect all AI clients. Make sure no critical operations are in progress.
            </p>
          </div>
        </div>
      </div>
    </div>
  )
}
