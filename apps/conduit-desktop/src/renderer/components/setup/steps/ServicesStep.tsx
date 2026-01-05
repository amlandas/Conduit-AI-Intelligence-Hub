import { useState, useEffect } from 'react'
import { useSetupStore } from '@/stores'
import {
  ArrowRight,
  ArrowLeft,
  Loader2,
  Server,
  Database,
  PlayCircle,
  RefreshCw,
  Cpu,
} from 'lucide-react'
import { cn } from '@/lib/utils'

interface ServiceStatus {
  name: string
  running: boolean
  port?: number
  container?: string
  error?: string
}

export function ServicesStep(): JSX.Element {
  const {
    setStep,
    setDaemonRunning,
    setQdrantRunning,
    setFalkorDBRunning,
    daemonRunning,
    qdrantRunning,
    falkordbRunning,
    setOperation,
    setOperationError,
  } = useSetupStore()

  const [services, setServices] = useState<ServiceStatus[]>([])
  const [isChecking, setIsChecking] = useState(true)
  const [isStarting, setIsStarting] = useState<string | null>(null)

  // Check services on mount
  useEffect(() => {
    checkServices()
  }, [])

  const checkServices = async () => {
    setIsChecking(true)
    try {
      const results = await window.conduit.checkServices()
      setServices(results)

      // Update store
      const daemon = results.find((s: ServiceStatus) => s.name === 'Conduit Daemon')
      const qdrant = results.find((s: ServiceStatus) => s.name === 'Qdrant')
      const falkordb = results.find((s: ServiceStatus) => s.name === 'FalkorDB')

      if (daemon) setDaemonRunning(daemon.running)
      if (qdrant) setQdrantRunning(qdrant.running)
      if (falkordb) setFalkorDBRunning(falkordb.running)
    } catch (error) {
      setOperationError(`Failed to check services: ${error}`)
    } finally {
      setIsChecking(false)
    }
  }

  const handleStartService = async (serviceName: string) => {
    setIsStarting(serviceName)
    setOperation(`Starting ${serviceName}...`)
    try {
      const result = await window.conduit.startService({ name: serviceName })

      if (result.success) {
        // Refresh services after start
        await checkServices()
        setOperation(null)
      } else {
        setOperationError(result.error || `Failed to start ${serviceName}`)
      }
    } catch (error) {
      setOperationError(`Failed to start ${serviceName}: ${error}`)
    } finally {
      setIsStarting(null)
    }
  }

  const handleStartAll = async () => {
    setIsStarting('all')
    setOperation('Starting all services...')
    try {
      const result = await window.conduit.startAllServices()

      if (result.success) {
        await checkServices()
        setOperation(null)
      } else {
        setOperationError(result.error || 'Failed to start all services')
      }
    } catch (error) {
      setOperationError(`Failed to start services: ${error}`)
    } finally {
      setIsStarting(null)
    }
  }

  const handleContinue = () => {
    setStep('models')
  }

  const handleBack = () => {
    setStep('dependencies')
  }

  // At minimum, daemon and Qdrant should be running to continue
  const canContinue = daemonRunning && qdrantRunning

  const allRunning = services.every((s) => s.running)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h2 className="text-2xl font-semibold text-macos-text-primary dark:text-macos-text-dark-primary">
          Start Services
        </h2>
        <p className="mt-2 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          Conduit needs background services for vector search and graph database
        </p>
      </div>

      {/* Loading state */}
      {isChecking ? (
        <div className="flex items-center gap-3 p-4 rounded-xl bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30">
          <Loader2 className="w-5 h-5 animate-spin text-macos-blue" />
          <span className="text-macos-text-secondary dark:text-macos-text-dark-secondary">
            Checking service status...
          </span>
        </div>
      ) : (
        <>
          {/* Services list */}
          <div className="space-y-3">
            {services.map((service) => (
              <ServiceRow
                key={service.name}
                service={service}
                isStarting={isStarting === service.name || isStarting === 'all'}
                onStart={() => handleStartService(service.name)}
              />
            ))}
          </div>

          {/* Start all button */}
          {!allRunning && (
            <button
              onClick={handleStartAll}
              disabled={isStarting !== null}
              className={cn(
                'w-full flex items-center justify-center gap-2 px-4 py-3 rounded-lg font-medium transition-colors',
                isStarting
                  ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                  : 'bg-macos-blue text-white hover:bg-macos-blue/90'
              )}
            >
              {isStarting === 'all' ? (
                <>
                  <Loader2 className="w-4 h-4 animate-spin" />
                  Starting all services...
                </>
              ) : (
                <>
                  <PlayCircle className="w-4 h-4" />
                  Start All Services
                </>
              )}
            </button>
          )}

          {/* Refresh button */}
          <button
            onClick={checkServices}
            className="flex items-center gap-2 text-sm text-macos-blue hover:underline"
          >
            <RefreshCw className="w-4 h-4" />
            Refresh status
          </button>

          {/* Info about FalkorDB */}
          {!falkordbRunning && (
            <div className="p-3 rounded-lg bg-macos-bg-tertiary/50 dark:bg-macos-bg-dark-tertiary/50">
              <p className="text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
                <strong>Note:</strong> FalkorDB is optional. It enables Knowledge Graph (KAG) features
                for advanced entity extraction. You can start it later from the dashboard.
              </p>
            </div>
          )}
        </>
      )}

      {/* Navigation */}
      <div className="flex justify-between pt-4">
        <button
          onClick={handleBack}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-macos-text-secondary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary transition-colors"
        >
          <ArrowLeft className="w-4 h-4" />
          Back
        </button>
        <button
          onClick={handleContinue}
          disabled={!canContinue}
          className={cn(
            'flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-colors',
            canContinue
              ? 'bg-macos-blue text-white hover:bg-macos-blue/90'
              : 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
          )}
        >
          Continue
          <ArrowRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  )
}

interface ServiceRowProps {
  service: ServiceStatus
  isStarting: boolean
  onStart: () => void
}

function ServiceRow({ service, isStarting, onStart }: ServiceRowProps): JSX.Element {
  const getIcon = () => {
    switch (service.name) {
      case 'Conduit Daemon':
        return Server
      case 'Qdrant':
        return Database
      case 'FalkorDB':
        return Cpu
      default:
        return Server
    }
  }

  const Icon = getIcon()

  return (
    <div
      className={cn(
        'flex items-center gap-3 p-4 rounded-xl border transition-colors',
        service.running
          ? 'border-macos-green/20 bg-macos-green/5'
          : 'border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30'
      )}
    >
      {/* Status icon */}
      <div
        className={cn(
          'w-10 h-10 rounded-lg flex items-center justify-center',
          service.running ? 'bg-macos-green/10' : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary'
        )}
      >
        <Icon
          className={cn('w-5 h-5', service.running ? 'text-macos-green' : 'text-macos-text-tertiary')}
        />
      </div>

      {/* Name and details */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <p className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
            {service.name}
          </p>
          {service.running && (
            <span className="flex items-center gap-1 px-2 py-0.5 rounded-full bg-macos-green/10 text-macos-green text-xs">
              <span className="w-1.5 h-1.5 rounded-full bg-macos-green animate-pulse" />
              Running
            </span>
          )}
        </div>
        {service.running && service.port && (
          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">Port {service.port}</p>
        )}
        {service.container && (
          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">Container: {service.container}</p>
        )}
        {service.error && <p className="text-xs text-macos-red">{service.error}</p>}
      </div>

      {/* Action button */}
      {!service.running && (
        <button
          onClick={onStart}
          disabled={isStarting}
          className={cn(
            'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
            isStarting
              ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
              : 'bg-macos-blue text-white hover:bg-macos-blue/90'
          )}
        >
          {isStarting ? (
            <>
              <Loader2 className="w-3.5 h-3.5 animate-spin" />
              Starting...
            </>
          ) : (
            <>
              <PlayCircle className="w-3.5 h-3.5" />
              Start
            </>
          )}
        </button>
      )}
    </div>
  )
}
