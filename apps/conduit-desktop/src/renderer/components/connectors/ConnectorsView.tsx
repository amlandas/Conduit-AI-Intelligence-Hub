import { useState } from 'react'
import { cn } from '@/lib/utils'
import { useInstancesStore, useSettingsStore } from '@/stores'
import { AddConnectorModal } from './AddConnectorModal'
import { ConnectorPermissions } from './ConnectorPermissions'
import {
  Cable,
  Play,
  Square,
  Trash2,
  RefreshCw,
  Plus,
  Loader2,
  Shield
} from 'lucide-react'

export function ConnectorsView(): JSX.Element {
  const { instances, loading, refresh, addInstance } = useInstancesStore()
  const { isFeatureVisible } = useSettingsStore()
  const [addModalOpen, setAddModalOpen] = useState(false)
  const [expandedInstances, setExpandedInstances] = useState<Set<string>>(new Set())

  const toggleExpanded = (id: string): void => {
    setExpandedInstances((prev) => {
      const newSet = new Set(prev)
      if (newSet.has(id)) {
        newSet.delete(id)
      } else {
        newSet.add(id)
      }
      return newSet
    })
  }

  const handleStart = async (id: string): Promise<void> => {
    try {
      await window.conduit.startInstance(id)
    } catch (err) {
      console.error('Failed to start instance:', err)
    }
  }

  const handleStop = async (id: string): Promise<void> => {
    try {
      await window.conduit.stopInstance(id)
    } catch (err) {
      console.error('Failed to stop instance:', err)
    }
  }

  const handleDelete = async (id: string): Promise<void> => {
    try {
      await window.conduit.deleteInstance(id)
    } catch (err) {
      console.error('Failed to delete instance:', err)
    }
  }

  const handleAddConnector = async (connector: string, name: string): Promise<void> => {
    const result = await window.conduit.createInstance({ connector, name })
    if (result && typeof result === 'object' && 'id' in result) {
      addInstance({
        id: (result as { id: string }).id,
        name,
        connector,
        status: 'STOPPED'
      })
    }
  }

  const getStatusColor = (status: string): string => {
    switch (status) {
      case 'RUNNING':
        return 'status-dot-online'
      case 'STOPPED':
        return 'status-dot-offline'
      case 'STARTING':
      case 'STOPPING':
        return 'status-dot-warning'
      case 'ERROR':
        return 'status-dot-error'
      default:
        return 'status-dot-offline'
    }
  }

  const isTransitioning = (status: string): boolean => {
    return status === 'STARTING' || status === 'STOPPING'
  }

  return (
    <div className="space-y-6 animate-fade-in">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Connectors</h1>
        <div className="flex items-center gap-2">
          <button onClick={() => refresh()} className="btn btn-secondary">
            <RefreshCw className={cn('w-4 h-4 mr-1.5', loading && 'animate-spin')} />
            Refresh
          </button>
          <button onClick={() => setAddModalOpen(true)} className="btn btn-primary">
            <Plus className="w-4 h-4 mr-1.5" />
            Add Connector
          </button>
        </div>
      </div>

      {/* Connectors List */}
      {instances.length === 0 ? (
        <div className="card p-8 text-center">
          <Cable className="w-12 h-12 mx-auto text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary" />
          <p className="mt-3 text-macos-text-secondary dark:text-macos-text-dark-secondary">
            No connectors configured
          </p>
          <p className="mt-1 text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
            Add a connector to extend AI client capabilities
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {instances.map((instance) => {
            const isExpanded = expandedInstances.has(instance.id)

            return (
              <div key={instance.id} className="card overflow-hidden">
                <div className="p-4">
                  <div className="flex items-center gap-4">
                    <div className="p-2.5 rounded-lg bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary">
                      <Cable className="w-6 h-6 text-macos-blue" />
                    </div>

                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <h3 className="font-medium">{instance.name}</h3>
                        <span
                          className={cn(
                            'status-dot',
                            getStatusColor(instance.status),
                            isTransitioning(instance.status) && 'animate-pulse'
                          )}
                        />
                        <span className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary uppercase">
                          {instance.status}
                        </span>
                      </div>
                      <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                        {instance.connector}
                      </p>
                    </div>

                    <div className="flex items-center gap-2">
                      {/* Permissions Button - Advanced Mode */}
                      {isFeatureVisible('showContainerControls') && (
                        <button
                          onClick={() => toggleExpanded(instance.id)}
                          className={cn(
                            'p-2 rounded transition-colors',
                            isExpanded
                              ? 'bg-macos-green/10 text-macos-green'
                              : 'hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary text-macos-text-secondary'
                          )}
                          title="Permissions"
                        >
                          <Shield className="w-4 h-4" />
                        </button>
                      )}

                      {instance.status === 'RUNNING' ? (
                        <button
                          onClick={() => handleStop(instance.id)}
                          className="btn btn-secondary"
                          disabled={isTransitioning(instance.status)}
                        >
                          {isTransitioning(instance.status) ? (
                            <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                          ) : (
                            <Square className="w-4 h-4 mr-1.5" />
                          )}
                          Stop
                        </button>
                      ) : (
                        <button
                          onClick={() => handleStart(instance.id)}
                          className="btn btn-primary"
                          disabled={isTransitioning(instance.status)}
                        >
                          {instance.status === 'STARTING' ? (
                            <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                          ) : (
                            <Play className="w-4 h-4 mr-1.5" />
                          )}
                          Start
                        </button>
                      )}
                      <button
                        onClick={() => handleDelete(instance.id)}
                        className="p-2 rounded hover:bg-macos-red/10 text-macos-red"
                        disabled={instance.status === 'RUNNING'}
                        title={instance.status === 'RUNNING' ? 'Stop connector first' : 'Delete'}
                      >
                        <Trash2 className="w-4 h-4" />
                      </button>
                    </div>
                  </div>
                </div>

                {/* Expandable Permissions Panel */}
                {isExpanded && isFeatureVisible('showContainerControls') && (
                  <div className="border-t border-macos-separator dark:border-macos-separator-dark">
                    <ConnectorPermissions
                      instanceId={instance.id}
                      instanceName={instance.name}
                      className="border-0 rounded-none shadow-none"
                    />
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}

      <AddConnectorModal
        open={addModalOpen}
        onClose={() => setAddModalOpen(false)}
        onAdd={handleAddConnector}
      />
    </div>
  )
}
