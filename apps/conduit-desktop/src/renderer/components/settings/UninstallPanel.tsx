/**
 * UninstallPanel - GUI component for uninstalling Conduit
 *
 * Provides three uninstall tiers:
 * 1. Keep Data - Remove binaries, services, but keep ~/.conduit/
 * 2. All - Remove everything except system tools (Docker/Podman)
 * 3. Full - Same as All, plus optional Ollama removal
 */

import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { Modal, ModalFooter } from '../ui/Modal'
import {
  AlertTriangle,
  Database,
  Server,
  HardDrive,
  CheckCircle2,
  XCircle,
  Loader2,
  ExternalLink,
  Box,
  Cpu,
  FileCode
} from 'lucide-react'

interface UninstallInfo {
  hasDaemonService: boolean
  daemonRunning: boolean
  servicePath: string | null

  hasBinaries: boolean
  conduitPath: string | null
  daemonPath: string | null
  conduitVersion: string | null

  hasDataDir: boolean
  dataDirPath: string | null
  dataDirSize: string | null
  dataDirSizeRaw: number

  containerRuntime: string | null
  hasQdrantContainer: boolean
  qdrantContainerRunning: boolean
  qdrantVectorCount: number
  hasFalkorDBContainer: boolean
  falkordbContainerRunning: boolean

  hasOllama: boolean
  ollamaRunning: boolean
  ollamaModels: string[]
  ollamaSize: string | null
  ollamaSizeRaw: number

  hasShellConfig: boolean
  shellConfigFiles: string[]

  hasSymlinks: boolean
  symlinks: string[]
}

interface UninstallOptions {
  tier: 'keep-data' | 'all' | 'full'
  removeOllama: boolean
  force: boolean
}

interface UninstallResult {
  success: boolean
  itemsRemoved: string[]
  itemsFailed: string[]
  errors: string[]
}

type UninstallTier = 'keep-data' | 'all' | 'full'

interface TierOptionProps {
  tier: UninstallTier
  currentTier: UninstallTier
  title: string
  description: string
  details: string[]
  icon: typeof Server
  danger?: boolean
  onSelect: (tier: UninstallTier) => void
}

function TierOption({
  tier,
  currentTier,
  title,
  description,
  details,
  icon: Icon,
  danger,
  onSelect
}: TierOptionProps): JSX.Element {
  const isActive = tier === currentTier

  return (
    <button
      onClick={() => onSelect(tier)}
      className={cn(
        'w-full text-left p-4 rounded-lg border-2 transition-colors',
        isActive
          ? danger
            ? 'border-macos-red bg-macos-red/5'
            : 'border-macos-blue bg-macos-blue/5'
          : 'border-macos-separator dark:border-macos-separator-dark hover:border-macos-blue/50'
      )}
    >
      <div className="flex items-start gap-3">
        <div
          className={cn(
            'p-2 rounded-lg',
            isActive
              ? danger
                ? 'bg-macos-red text-white'
                : 'bg-macos-blue text-white'
              : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary'
          )}
        >
          <Icon className="w-5 h-5" />
        </div>
        <div className="flex-1">
          <h3 className="font-medium">{title}</h3>
          <p className="mt-0.5 text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
            {description}
          </p>
          <ul className="mt-2 text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary space-y-0.5">
            {details.map((detail, i) => (
              <li key={i} className="flex items-center gap-1">
                <span className={cn(detail.startsWith('+') ? 'text-macos-red' : '')}>
                  {detail}
                </span>
              </li>
            ))}
          </ul>
        </div>
        {isActive && (
          <div className={danger ? 'text-macos-red' : 'text-macos-blue'}>
            <CheckCircle2 className="w-5 h-5" />
          </div>
        )}
      </div>
    </button>
  )
}

interface ConfirmationModalProps {
  open: boolean
  onClose: () => void
  onConfirm: () => void
  info: UninstallInfo | null
  tier: UninstallTier
  removeOllama: boolean
  isLoading: boolean
}

function ConfirmationModal({
  open,
  onClose,
  onConfirm,
  info,
  tier,
  removeOllama,
  isLoading
}: ConfirmationModalProps): JSX.Element | null {
  const [confirmText, setConfirmText] = useState('')
  const isConfirmed = confirmText === 'UNINSTALL'

  useEffect(() => {
    if (!open) setConfirmText('')
  }, [open])

  const needsConfirmation = tier !== 'keep-data'

  return (
    <Modal open={open} onClose={onClose} title="Confirm Uninstallation" className="w-[520px]">
      <div className="p-5 space-y-4">
        <div className="flex items-start gap-3 p-4 bg-macos-orange/10 dark:bg-macos-orange/20 rounded-lg">
          <AlertTriangle className="w-6 h-6 text-macos-orange shrink-0 mt-0.5" />
          <div>
            <h3 className="font-medium text-macos-orange">Warning</h3>
            <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary mt-1">
              {tier === 'keep-data'
                ? 'This will remove Conduit binaries and services. Your data will be preserved.'
                : 'This will permanently delete your data. This action cannot be undone.'}
            </p>
          </div>
        </div>

        {info && tier !== 'keep-data' && (
          <div className="space-y-2">
            <h4 className="text-sm font-medium">What will be removed:</h4>
            <ul className="text-sm text-macos-text-secondary space-y-1.5">
              {info.hasDataDir && (
                <li className="flex items-center gap-2">
                  <Database className="w-4 h-4 text-macos-red" />
                  <span>Data directory ({info.dataDirSize})</span>
                </li>
              )}
              {info.hasQdrantContainer && (
                <li className="flex items-center gap-2">
                  <Box className="w-4 h-4 text-macos-red" />
                  <span>Qdrant container ({info.qdrantVectorCount.toLocaleString()} vectors)</span>
                </li>
              )}
              {info.hasFalkorDBContainer && (
                <li className="flex items-center gap-2">
                  <Box className="w-4 h-4 text-macos-red" />
                  <span>FalkorDB container</span>
                </li>
              )}
              {removeOllama && info.hasOllama && (
                <li className="flex items-center gap-2">
                  <Cpu className="w-4 h-4 text-macos-red" />
                  <span>
                    Ollama ({info.ollamaSize})
                    {info.ollamaModels.length > 0 && (
                      <span className="text-macos-text-tertiary">
                        {' '}
                        - {info.ollamaModels.slice(0, 3).join(', ')}
                        {info.ollamaModels.length > 3 && ` +${info.ollamaModels.length - 3} more`}
                      </span>
                    )}
                  </span>
                </li>
              )}
            </ul>
          </div>
        )}

        {needsConfirmation && (
          <div className="space-y-2">
            <label className="text-sm font-medium">
              Type <span className="font-mono text-macos-red">UNINSTALL</span> to confirm:
            </label>
            <input
              type="text"
              value={confirmText}
              onChange={(e) => setConfirmText(e.target.value.toUpperCase())}
              placeholder="Type UNINSTALL"
              className={cn(
                'w-full px-3 py-2 rounded-lg border-2 transition-colors',
                'bg-white dark:bg-macos-bg-dark-tertiary',
                isConfirmed
                  ? 'border-macos-green'
                  : 'border-macos-separator dark:border-macos-separator-dark',
                'focus:outline-none focus:border-macos-blue'
              )}
              disabled={isLoading}
              autoFocus
            />
          </div>
        )}
      </div>

      <ModalFooter>
        <button
          onClick={onClose}
          disabled={isLoading}
          className="px-4 py-2 rounded-lg text-sm font-medium hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary transition-colors"
        >
          Cancel
        </button>
        <button
          onClick={onConfirm}
          disabled={isLoading || (needsConfirmation && !isConfirmed)}
          className={cn(
            'px-4 py-2 rounded-lg text-sm font-medium text-white transition-colors',
            'disabled:opacity-50 disabled:cursor-not-allowed',
            isLoading ? 'bg-macos-text-tertiary' : 'bg-macos-red hover:bg-macos-red/90'
          )}
        >
          {isLoading ? (
            <span className="flex items-center gap-2">
              <Loader2 className="w-4 h-4 animate-spin" />
              Uninstalling...
            </span>
          ) : (
            'Uninstall Conduit'
          )}
        </button>
      </ModalFooter>
    </Modal>
  )
}

export function UninstallPanel(): JSX.Element {
  const [info, setInfo] = useState<UninstallInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [tier, setTier] = useState<UninstallTier>('keep-data')
  const [removeOllama, setRemoveOllama] = useState(false)
  const [showConfirm, setShowConfirm] = useState(false)
  const [isUninstalling, setIsUninstalling] = useState(false)
  const [result, setResult] = useState<UninstallResult | null>(null)

  useEffect(() => {
    loadInfo()
  }, [])

  const loadInfo = async (): Promise<void> => {
    try {
      setLoading(true)
      setError(null)
      const data = await window.conduit.getUninstallInfo()
      setInfo(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load uninstall info')
    } finally {
      setLoading(false)
    }
  }

  const handleUninstall = async (): Promise<void> => {
    try {
      setIsUninstalling(true)
      const options: UninstallOptions = {
        tier,
        removeOllama: tier === 'full' && removeOllama,
        force: true
      }
      const result = await window.conduit.executeUninstall(options)
      setResult(result)
      if (result.success) {
        setShowConfirm(false)
      }
    } catch (err) {
      setResult({
        success: false,
        itemsRemoved: [],
        itemsFailed: [],
        errors: [err instanceof Error ? err.message : 'Uninstall failed']
      })
    } finally {
      setIsUninstalling(false)
    }
  }

  const handleOpenDataDir = async (): Promise<void> => {
    await window.conduit.openDataDir()
  }

  if (loading) {
    return (
      <div className="card p-8 flex flex-col items-center justify-center gap-3">
        <Loader2 className="w-8 h-8 animate-spin text-macos-text-secondary" />
        <p className="text-sm text-macos-text-secondary">Loading uninstall information...</p>
      </div>
    )
  }

  if (error) {
    return (
      <div className="card p-6">
        <div className="flex items-start gap-3 text-macos-red">
          <XCircle className="w-5 h-5 shrink-0 mt-0.5" />
          <div>
            <h3 className="font-medium">Failed to load uninstall information</h3>
            <p className="text-sm text-macos-text-secondary mt-1">{error}</p>
            <button
              onClick={loadInfo}
              className="mt-3 text-sm text-macos-blue hover:underline"
            >
              Try again
            </button>
          </div>
        </div>
      </div>
    )
  }

  if (result) {
    return (
      <div className="card p-6">
        <div
          className={cn(
            'flex items-start gap-3',
            result.success ? 'text-macos-green' : 'text-macos-red'
          )}
        >
          {result.success ? (
            <CheckCircle2 className="w-6 h-6 shrink-0 mt-0.5" />
          ) : (
            <XCircle className="w-6 h-6 shrink-0 mt-0.5" />
          )}
          <div className="flex-1">
            <h3 className="font-medium">
              {result.success ? 'Uninstallation Complete' : 'Uninstallation Failed'}
            </h3>

            {result.itemsRemoved.length > 0 && (
              <div className="mt-3">
                <h4 className="text-sm font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                  Removed:
                </h4>
                <ul className="mt-1 text-sm text-macos-text-secondary space-y-0.5">
                  {result.itemsRemoved.map((item, i) => (
                    <li key={i} className="flex items-center gap-1">
                      <CheckCircle2 className="w-3.5 h-3.5 text-macos-green" />
                      {item}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {result.itemsFailed.length > 0 && (
              <div className="mt-3">
                <h4 className="text-sm font-medium text-macos-red">Failed:</h4>
                <ul className="mt-1 text-sm text-macos-text-secondary space-y-0.5">
                  {result.itemsFailed.map((item, i) => (
                    <li key={i} className="flex items-center gap-1">
                      <XCircle className="w-3.5 h-3.5 text-macos-red" />
                      {item}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            {result.errors.length > 0 && (
              <div className="mt-3">
                <h4 className="text-sm font-medium text-macos-red">Errors:</h4>
                <ul className="mt-1 text-sm text-macos-text-secondary space-y-0.5">
                  {result.errors.map((err, i) => (
                    <li key={i}>{err}</li>
                  ))}
                </ul>
              </div>
            )}

            {result.success && (
              <p className="mt-4 text-sm text-macos-text-secondary">
                To complete the uninstallation, drag Conduit Desktop to Trash.
              </p>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Installation Status */}
      {info && (
        <div className="card p-4 space-y-4">
          <h3 className="text-sm font-medium">Current Installation</h3>

          <div className="grid grid-cols-2 gap-3 text-sm">
            {/* Binaries */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'w-2 h-2 rounded-full',
                  info.hasBinaries ? 'bg-macos-green' : 'bg-macos-text-tertiary'
                )}
              />
              <span className="text-macos-text-secondary">CLI Binaries</span>
              {info.conduitVersion && (
                <span className="text-xs text-macos-text-tertiary">{info.conduitVersion}</span>
              )}
            </div>

            {/* Daemon */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'w-2 h-2 rounded-full',
                  info.daemonRunning
                    ? 'bg-macos-green'
                    : info.hasDaemonService
                      ? 'bg-macos-orange'
                      : 'bg-macos-text-tertiary'
                )}
              />
              <span className="text-macos-text-secondary">Daemon Service</span>
              <span className="text-xs text-macos-text-tertiary">
                {info.daemonRunning ? 'Running' : info.hasDaemonService ? 'Stopped' : 'Not installed'}
              </span>
            </div>

            {/* Data Directory */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'w-2 h-2 rounded-full',
                  info.hasDataDir ? 'bg-macos-blue' : 'bg-macos-text-tertiary'
                )}
              />
              <span className="text-macos-text-secondary">Data Directory</span>
              {info.dataDirSize && (
                <span className="text-xs text-macos-text-tertiary">{info.dataDirSize}</span>
              )}
              {info.hasDataDir && (
                <button
                  onClick={handleOpenDataDir}
                  className="ml-auto text-macos-blue hover:text-macos-blue/80"
                  title="Open in Finder"
                >
                  <ExternalLink className="w-3.5 h-3.5" />
                </button>
              )}
            </div>

            {/* Qdrant */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'w-2 h-2 rounded-full',
                  info.qdrantContainerRunning
                    ? 'bg-macos-green'
                    : info.hasQdrantContainer
                      ? 'bg-macos-orange'
                      : 'bg-macos-text-tertiary'
                )}
              />
              <span className="text-macos-text-secondary">Qdrant</span>
              {info.hasQdrantContainer && (
                <span className="text-xs text-macos-text-tertiary">
                  {info.qdrantVectorCount.toLocaleString()} vectors
                </span>
              )}
            </div>

            {/* FalkorDB */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'w-2 h-2 rounded-full',
                  info.falkordbContainerRunning
                    ? 'bg-macos-green'
                    : info.hasFalkorDBContainer
                      ? 'bg-macos-orange'
                      : 'bg-macos-text-tertiary'
                )}
              />
              <span className="text-macos-text-secondary">FalkorDB</span>
              <span className="text-xs text-macos-text-tertiary">
                {info.hasFalkorDBContainer
                  ? info.falkordbContainerRunning
                    ? 'Running'
                    : 'Stopped'
                  : 'Not installed'}
              </span>
            </div>

            {/* Ollama */}
            <div className="flex items-center gap-2">
              <div
                className={cn(
                  'w-2 h-2 rounded-full',
                  info.ollamaRunning
                    ? 'bg-macos-green'
                    : info.hasOllama
                      ? 'bg-macos-orange'
                      : 'bg-macos-text-tertiary'
                )}
              />
              <span className="text-macos-text-secondary">Ollama</span>
              {info.hasOllama && info.ollamaSize && (
                <span className="text-xs text-macos-text-tertiary">
                  {info.ollamaModels.length} models ({info.ollamaSize})
                </span>
              )}
            </div>
          </div>
        </div>
      )}

      {/* Tier Selection */}
      <div className="space-y-3">
        <h3 className="text-sm font-medium">Choose what to remove:</h3>

        <TierOption
          tier="keep-data"
          currentTier={tier}
          title="Uninstall Only"
          description="Remove binaries and services, but keep your data for reinstall"
          details={[
            'Stops daemon service',
            'Removes CLI binaries',
            'Cleans shell configuration',
            'Keeps ~/.conduit/ and containers'
          ]}
          icon={Server}
          onSelect={setTier}
        />

        <TierOption
          tier="all"
          currentTier={tier}
          title="Uninstall + Remove Data"
          description="Remove everything including your knowledge base data"
          details={[
            'Everything above PLUS:',
            info?.dataDirSize ? `+ Removes ~/.conduit/ (${info.dataDirSize})` : '+ Removes ~/.conduit/',
            info?.hasQdrantContainer
              ? `+ Stops and removes Qdrant (${info.qdrantVectorCount.toLocaleString()} vectors)`
              : '+ Stops and removes Qdrant',
            '+ Stops and removes FalkorDB'
          ]}
          icon={Database}
          danger
          onSelect={setTier}
        />

        <TierOption
          tier="full"
          currentTier={tier}
          title="Full Cleanup"
          description="Remove everything with optional dependency cleanup"
          details={[
            'Everything above PLUS optional:',
            info?.hasOllama
              ? `+ Ollama (${info.ollamaModels.length} models, ${info.ollamaSize})`
              : '+ Ollama (if installed)',
            'Docker/Podman are NEVER removed'
          ]}
          icon={HardDrive}
          danger
          onSelect={setTier}
        />
      </div>

      {/* Ollama Checkbox (only for full tier) */}
      {tier === 'full' && info?.hasOllama && (
        <div className="card p-4">
          <label className="flex items-center gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={removeOllama}
              onChange={(e) => setRemoveOllama(e.target.checked)}
              className="w-4 h-4 rounded border-macos-separator accent-macos-red"
            />
            <div className="flex-1">
              <span className="text-sm font-medium">Remove Ollama</span>
              <p className="text-xs text-macos-text-secondary mt-0.5">
                This will remove Ollama and all downloaded models ({info.ollamaSize})
              </p>
            </div>
          </label>
        </div>
      )}

      {/* Notice about Docker/Podman */}
      <div className="flex items-start gap-2 text-xs text-macos-text-tertiary">
        <FileCode className="w-4 h-4 shrink-0 mt-0.5" />
        <p>
          Docker and Podman are system tools and will never be removed. Only Conduit-specific
          containers (conduit-qdrant, conduit-falkordb) may be removed.
        </p>
      </div>

      {/* Action Buttons */}
      <div className="flex items-center justify-end gap-3 pt-2">
        <button
          onClick={() => setShowConfirm(true)}
          className={cn(
            'px-4 py-2 rounded-lg text-sm font-medium text-white transition-colors',
            'bg-macos-red hover:bg-macos-red/90'
          )}
        >
          Uninstall Conduit
        </button>
      </div>

      {/* Confirmation Modal */}
      <ConfirmationModal
        open={showConfirm}
        onClose={() => setShowConfirm(false)}
        onConfirm={handleUninstall}
        info={info}
        tier={tier}
        removeOllama={removeOllama}
        isLoading={isUninstalling}
      />
    </div>
  )
}
