import { cn } from '@/lib/utils'
import { useDaemonStore, useInstancesStore, useKBStore } from '@/stores'
import { OllamaModels } from './OllamaModels'
import {
  Server,
  Database,
  Cpu,
  HardDrive,
  Box,
  Layers,
  RefreshCw
} from 'lucide-react'

interface StatusCardProps {
  title: string
  icon: typeof Server
  status: 'online' | 'offline' | 'warning' | 'unknown'
  detail?: string
  className?: string
}

function StatusCard({ title, icon: Icon, status, detail, className }: StatusCardProps): JSX.Element {
  const statusColors = {
    online: 'status-dot-online',
    offline: 'status-dot-offline',
    warning: 'status-dot-warning',
    unknown: 'status-dot-offline'
  }

  return (
    <div className={cn('card p-4', className)}>
      <div className="flex items-start gap-3">
        <div className="p-2 rounded-lg bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">
          <Icon className="w-5 h-5 text-macos-text-secondary dark:text-macos-text-dark-secondary" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className={cn('status-dot', statusColors[status])} />
            <h3 className="font-medium text-sm">{title}</h3>
          </div>
          {detail && (
            <p className="mt-1 text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary truncate">
              {detail}
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

export function DashboardView(): JSX.Element {
  const { status, stats, refresh } = useDaemonStore()
  const { instances } = useInstancesStore()
  const { sources } = useKBStore()

  const runningCount = instances.filter((i) => i.status === 'RUNNING').length

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
            status="offline"
            detail="Knowledge graph (optional)"
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
    </div>
  )
}
