/**
 * DashboardView Component
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * Service status comes from CLI commands, cards are clickable to start services.
 */
import { cn } from '@/lib/utils'
import { useDaemonStore, useInstancesStore, useKBStore, useSettingsStore } from '@/stores'
import { OllamaModels } from './OllamaModels'
import { RawJSONViewer } from '../ui/RawJSONViewer'
import {
  Server,
  Database,
  Cpu,
  HardDrive,
  Box,
  Layers,
  RefreshCw,
  Loader2,
  Play
} from 'lucide-react'

interface StatusCardProps {
  title: string
  icon: typeof Server
  status: 'online' | 'offline' | 'warning' | 'unknown'
  detail?: string
  className?: string
  onClick?: () => void
  loading?: boolean
  canStart?: boolean
}

function StatusCard({ title, icon: Icon, status, detail, className, onClick, loading, canStart }: StatusCardProps): JSX.Element {
  const statusColors = {
    online: 'status-dot-online',
    offline: 'status-dot-offline',
    warning: 'status-dot-warning',
    unknown: 'status-dot-offline'
  }

  const isClickable = onClick && status === 'offline' && canStart

  return (
    <div
      className={cn(
        'card p-4',
        isClickable && 'cursor-pointer hover:bg-macos-bg-secondary/50 dark:hover:bg-macos-bg-dark-tertiary/50 transition-colors',
        className
      )}
      onClick={isClickable ? onClick : undefined}
      role={isClickable ? 'button' : undefined}
      tabIndex={isClickable ? 0 : undefined}
    >
      <div className="flex items-start gap-3">
        <div className="p-2 rounded-lg bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">
          {loading ? (
            <Loader2 className="w-5 h-5 animate-spin text-macos-blue" />
          ) : (
            <Icon className="w-5 h-5 text-macos-text-secondary dark:text-macos-text-dark-secondary" />
          )}
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className={cn('status-dot', statusColors[status])} />
            <h3 className="font-medium text-sm">{title}</h3>
            {isClickable && !loading && (
              <Play className="w-3 h-3 text-macos-green ml-auto" />
            )}
          </div>
          {detail && (
            <p className="mt-1 text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary truncate">
              {detail}
            </p>
          )}
          {isClickable && !loading && (
            <p className="mt-1 text-xs text-macos-blue">Click to start</p>
          )}
        </div>
      </div>
    </div>
  )
}

export function DashboardView(): JSX.Element {
  const { status, stats, refresh, startService, startingService } = useDaemonStore()
  const { instances } = useInstancesStore()
  const { sources } = useKBStore()
  const { isFeatureVisible } = useSettingsStore()

  const runningCount = instances.filter((i) => i.status === 'RUNNING').length

  // Service start handlers - delegate to CLI via store
  const handleStartDaemon = (): void => {
    startService('Conduit Daemon')
  }

  const handleStartQdrant = (): void => {
    startService('Qdrant')
  }

  const handleStartFalkorDB = (): void => {
    startService('FalkorDB')
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Dashboard</h1>
        <button
          onClick={() => refresh()}
          className="btn btn-secondary"
        >
          <RefreshCw className="w-4 h-4 mr-1.5" />
          Refresh
        </button>
      </div>

      {/* Infrastructure Status */}
      <section>
        <h2 className="text-lg font-medium mb-3">Infrastructure Status</h2>
        <div className="grid grid-cols-3 gap-4">
          <StatusCard
            title="Daemon"
            icon={Server}
            status={status.connected ? 'online' : 'offline'}
            detail={status.uptime ? `Uptime: ${status.uptime}` : undefined}
            onClick={handleStartDaemon}
            loading={startingService === 'Conduit Daemon'}
            canStart={true}
          />
          <StatusCard
            title="Ollama"
            icon={Cpu}
            status={stats.ollamaStatus === 'running' ? 'online' : stats.ollamaStatus === 'stopped' ? 'offline' : 'unknown'}
            detail="localhost:11434"
          />
          <StatusCard
            title="Qdrant"
            icon={Database}
            status={stats.qdrantStatus === 'running' ? 'online' : stats.qdrantStatus === 'stopped' ? 'offline' : 'unknown'}
            detail={stats.totalVectors ? `${stats.totalVectors.toLocaleString()} vectors` : undefined}
            onClick={handleStartQdrant}
            loading={startingService === 'Qdrant'}
            canStart={true}
          />
          <StatusCard
            title="Container Runtime"
            icon={Box}
            status={stats.containerRuntime ? 'online' : 'offline'}
            detail={stats.containerRuntime || 'Not available'}
          />
          <StatusCard
            title="SQLite/FTS5"
            icon={HardDrive}
            status="online"
            detail={stats.totalDocuments ? `${stats.totalDocuments.toLocaleString()} documents` : 'Ready'}
          />
          <StatusCard
            title="FalkorDB"
            icon={Layers}
            status={stats.falkordbStatus === 'running' ? 'online' : 'offline'}
            detail="Knowledge graph (optional)"
            onClick={handleStartFalkorDB}
            loading={startingService === 'FalkorDB'}
            canStart={true}
          />
        </div>
      </section>

      {/* Quick Stats */}
      <section>
        <h2 className="text-lg font-medium mb-3">Overview</h2>
        <div className="grid grid-cols-4 gap-4">
          <div className="card p-4">
            <div className="text-2xl font-semibold text-macos-blue">{instances.length}</div>
            <div className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
              Total Connectors
            </div>
          </div>
          <div className="card p-4">
            <div className="text-2xl font-semibold text-macos-green">{runningCount}</div>
            <div className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
              Running
            </div>
          </div>
          <div className="card p-4">
            <div className="text-2xl font-semibold text-macos-purple">{sources.length}</div>
            <div className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
              KB Sources
            </div>
          </div>
          <div className="card p-4">
            <div className="text-2xl font-semibold text-macos-orange">
              {stats.totalDocuments?.toLocaleString() || 0}
            </div>
            <div className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
              Documents
            </div>
          </div>
        </div>
      </section>

      {/* Ollama Models */}
      <OllamaModels />

      {/* Developer Mode: Raw JSON Status */}
      {isFeatureVisible('showRawJSON') && (
        <section>
          <h2 className="text-lg font-medium mb-3">Raw API Response</h2>
          <div className="space-y-3">
            <RawJSONViewer
              data={{ status, stats }}
              title="Daemon Status & Stats"
            />
            <RawJSONViewer
              data={{ instances, totalRunning: runningCount }}
              title="Connector Instances"
            />
            <RawJSONViewer
              data={{ sources, totalSources: sources.length }}
              title="KB Sources"
            />
          </div>
        </section>
      )}
    </div>
  )
}
