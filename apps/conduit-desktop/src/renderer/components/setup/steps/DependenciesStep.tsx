import { useState, useEffect } from 'react'
import { useSetupStore, DependencyStatus } from '@/stores'
import {
  ArrowRight,
  ArrowLeft,
  CheckCircle,
  XCircle,
  AlertCircle,
  Loader2,
  ExternalLink,
  Download,
  RefreshCw,
  FolderOpen,
  X,
  Rocket,
} from 'lucide-react'
import { cn } from '@/lib/utils'

interface InstallProgress {
  name: string
  stage: string
  percent: number
  message: string
}

const REQUIRED_DEPENDENCIES: Omit<DependencyStatus, 'installed' | 'version'>[] = [
  {
    name: 'Ollama',
    required: true,
    installUrl: 'https://ollama.com/download',
    brewFormula: 'ollama',
    supportsAutoInstall: true,
  },
  {
    name: 'Docker or Podman',
    required: true,
    installUrl: 'https://podman.io/getting-started/installation',
    brewFormula: 'podman',
    supportsAutoInstall: true, // Requires Homebrew
  },
]

const OPTIONAL_DEPENDENCIES: Omit<DependencyStatus, 'installed' | 'version'>[] = [
  {
    name: 'Homebrew',
    required: false,
    installUrl: 'https://brew.sh',
    supportsAutoInstall: true,
  },
]

export function DependenciesStep(): JSX.Element {
  const { setStep, setDependencies, dependencies, setOperation, setOperationError } = useSetupStore()

  const [isChecking, setIsChecking] = useState(true)
  const [isInstalling, setIsInstalling] = useState<string | null>(null)
  const [isAutoInstalling, setIsAutoInstalling] = useState<string | null>(null)
  const [installProgress, setInstallProgress] = useState<InstallProgress | null>(null)
  const [isLocating, setIsLocating] = useState<string | null>(null)
  const [locateError, setLocateError] = useState<string | null>(null)

  // Check dependencies on mount
  useEffect(() => {
    checkDependencies()
  }, [])

  // Listen for install progress events
  useEffect(() => {
    const unsubscribe = window.conduit.onInstallProgress((progress: InstallProgress) => {
      setInstallProgress(progress)
      if (progress.stage === 'complete') {
        // Refresh dependencies after a short delay
        setTimeout(async () => {
          await checkDependencies()
          setIsAutoInstalling(null)
          setInstallProgress(null)
        }, 500)
      }
    })
    return unsubscribe
  }, [])

  const checkDependencies = async () => {
    setIsChecking(true)
    try {
      const results = await window.conduit.checkDependencies()
      setDependencies(results)
    } catch (error) {
      setOperationError(`Failed to check dependencies: ${error}`)
    } finally {
      setIsChecking(false)
    }
  }

  const handleInstall = async (dep: DependencyStatus) => {
    if (!dep.brewFormula) {
      // Open external URL for manual installation
      window.conduit.openExternal(dep.installUrl!)
      return
    }

    setIsInstalling(dep.name)
    setOperation(`Installing ${dep.name}...`)
    try {
      const result = await window.conduit.installDependency({
        name: dep.name,
        brewFormula: dep.brewFormula,
      })

      if (result.success) {
        // Refresh dependencies after install
        await checkDependencies()
        setOperation(null)
      } else {
        setOperationError(result.error || `Failed to install ${dep.name}`)
      }
    } catch (error) {
      setOperationError(`Failed to install ${dep.name}: ${error}`)
    } finally {
      setIsInstalling(null)
    }
  }

  const handleOpenUrl = (url: string) => {
    window.conduit.openExternal(url)
  }

  const handleLocateManually = async (depName: string) => {
    setIsLocating(depName)
    setLocateError(null)

    try {
      // Open file browser
      const result = await window.conduit.browseForBinary({ name: depName })
      if (result.cancelled) {
        setIsLocating(null)
        return
      }

      if (!result.path) {
        setLocateError('No file selected')
        setIsLocating(null)
        return
      }

      // Validate the selected binary
      const validation = await window.conduit.validateBinaryPath({ name: depName, binaryPath: result.path })
      if (!validation.valid) {
        setLocateError(validation.error || 'Invalid binary')
        setIsLocating(null)
        return
      }

      // Save the custom path
      const saveResult = await window.conduit.saveCustomPath({ name: depName, binaryPath: result.path })
      if (!saveResult.success) {
        setLocateError(saveResult.error || 'Failed to save path')
        setIsLocating(null)
        return
      }

      // Refresh dependencies to pick up the new path
      await checkDependencies()
    } catch (error) {
      setLocateError(`Failed to locate ${depName}: ${error}`)
    } finally {
      setIsLocating(null)
    }
  }

  const handleAutoInstall = async (depName: string) => {
    setIsAutoInstalling(depName)
    setInstallProgress({ name: depName, stage: 'starting', percent: 0, message: 'Starting installation...' })
    setLocateError(null)

    try {
      const result = await window.conduit.autoInstallDependency({ name: depName })
      if (!result.success) {
        setLocateError(result.error || `Failed to install ${depName}`)
        setIsAutoInstalling(null)
        setInstallProgress(null)
      }
      // Success will be handled by the progress event listener
    } catch (error) {
      setLocateError(`Failed to install ${depName}: ${error}`)
      setIsAutoInstalling(null)
      setInstallProgress(null)
    }
  }

  const handleContinue = () => {
    setStep('services')
  }

  const handleBack = () => {
    setStep('cli-install')
  }

  const getDependency = (name: string): DependencyStatus | undefined => {
    return dependencies.find((d) => d.name === name)
  }

  const requiredMet = REQUIRED_DEPENDENCIES.every((dep) => {
    const status = getDependency(dep.name)
    return status?.installed
  })

  const hasHomebrew = getDependency('Homebrew')?.installed

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h2 className="text-2xl font-semibold text-macos-text-primary dark:text-macos-text-dark-primary">
          Check Dependencies
        </h2>
        <p className="mt-2 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          Conduit requires a few tools to run AI models and vector databases
        </p>
      </div>

      {/* Loading state */}
      {isChecking ? (
        <div className="flex items-center gap-3 p-4 rounded-xl bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30">
          <Loader2 className="w-5 h-5 animate-spin text-macos-blue" />
          <span className="text-macos-text-secondary dark:text-macos-text-dark-secondary">
            Checking installed software...
          </span>
        </div>
      ) : (
        <>
          {/* Required dependencies */}
          <div className="space-y-3">
            <h3 className="text-sm font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
              Required
            </h3>
            {REQUIRED_DEPENDENCIES.map((dep) => {
              const status = getDependency(dep.name)
              return (
                <DependencyRow
                  key={dep.name}
                  dependency={{ ...dep, installed: status?.installed || false, version: status?.version, binaryPath: status?.binaryPath }}
                  isInstalling={isInstalling === dep.name}
                  isAutoInstalling={isAutoInstalling === dep.name}
                  installProgress={isAutoInstalling === dep.name ? installProgress : null}
                  isLocating={isLocating === dep.name}
                  hasHomebrew={hasHomebrew || false}
                  onInstall={() => handleInstall({ ...dep, installed: false })}
                  onAutoInstall={() => handleAutoInstall(dep.name)}
                  onOpenUrl={() => handleOpenUrl(dep.installUrl!)}
                  onLocate={() => handleLocateManually(dep.name)}
                />
              )
            })}
          </div>

          {/* Optional dependencies */}
          <div className="space-y-3">
            <h3 className="text-sm font-medium text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
              Optional (enables auto-install)
            </h3>
            {OPTIONAL_DEPENDENCIES.map((dep) => {
              const status = getDependency(dep.name)
              return (
                <DependencyRow
                  key={dep.name}
                  dependency={{ ...dep, installed: status?.installed || false, version: status?.version, binaryPath: status?.binaryPath }}
                  isInstalling={isInstalling === dep.name}
                  isAutoInstalling={isAutoInstalling === dep.name}
                  installProgress={isAutoInstalling === dep.name ? installProgress : null}
                  isLocating={isLocating === dep.name}
                  hasHomebrew={true} // Always show external link for optional
                  onInstall={() => {}}
                  onAutoInstall={() => handleAutoInstall(dep.name)}
                  onOpenUrl={() => handleOpenUrl(dep.installUrl!)}
                  onLocate={() => handleLocateManually(dep.name)}
                  isOptional
                />
              )
            })}
          </div>

          {/* Locate error display */}
          {locateError && (
            <div className="p-3 rounded-xl bg-macos-red/10 border border-macos-red/20">
              <div className="flex items-start gap-3">
                <XCircle className="w-5 h-5 text-macos-red flex-shrink-0 mt-0.5" />
                <div className="flex-1">
                  <p className="text-sm text-macos-text-primary dark:text-macos-text-dark-primary">
                    {locateError}
                  </p>
                </div>
                <button
                  onClick={() => setLocateError(null)}
                  className="text-macos-text-tertiary hover:text-macos-text-primary"
                >
                  <X className="w-4 h-4" />
                </button>
              </div>
            </div>
          )}

          {/* Refresh button */}
          <button
            onClick={checkDependencies}
            className="flex items-center gap-2 text-sm text-macos-blue hover:underline"
          >
            <RefreshCw className="w-4 h-4" />
            Recheck dependencies
          </button>

          {/* Warning if missing */}
          {!requiredMet && (
            <div className="p-4 rounded-xl bg-macos-orange/10 border border-macos-orange/20">
              <div className="flex items-start gap-3">
                <AlertCircle className="w-5 h-5 text-macos-orange flex-shrink-0 mt-0.5" />
                <div>
                  <p className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                    Missing required dependencies
                  </p>
                  <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                    Please install the missing software to continue. You can install them manually or use the buttons above.
                  </p>
                </div>
              </div>
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
          disabled={!requiredMet}
          className={cn(
            'flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-colors',
            requiredMet
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

interface DependencyRowProps {
  dependency: DependencyStatus & { supportsAutoInstall?: boolean }
  isInstalling: boolean
  isAutoInstalling: boolean
  installProgress: InstallProgress | null
  isLocating: boolean
  hasHomebrew: boolean
  onInstall: () => void
  onAutoInstall: () => void
  onOpenUrl: () => void
  onLocate: () => void
  isOptional?: boolean
}

function DependencyRow({
  dependency,
  isInstalling,
  isAutoInstalling,
  installProgress,
  isLocating,
  hasHomebrew,
  onInstall,
  onAutoInstall,
  onOpenUrl,
  onLocate,
  isOptional,
}: DependencyRowProps): JSX.Element {
  const isAnyOperationActive = isInstalling || isAutoInstalling || isLocating

  // Show progress bar when auto-installing
  if (isAutoInstalling && installProgress) {
    return (
      <div className="p-3 rounded-xl border border-macos-blue/20 bg-macos-blue/5">
        <div className="flex items-center gap-3 mb-2">
          <Loader2 className="w-5 h-5 animate-spin text-macos-blue flex-shrink-0" />
          <div className="flex-1 min-w-0">
            <p className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
              {dependency.name}
            </p>
            <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
              {installProgress.message}
            </p>
          </div>
        </div>
        {/* Progress bar */}
        <div className="h-1.5 bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary rounded-full overflow-hidden">
          <div
            className="h-full bg-macos-blue rounded-full transition-all duration-300"
            style={{ width: `${installProgress.percent}%` }}
          />
        </div>
      </div>
    )
  }

  return (
    <div
      className={cn(
        'flex items-center gap-3 p-3 rounded-xl border transition-colors',
        dependency.installed
          ? 'border-macos-green/20 bg-macos-green/5'
          : isOptional
            ? 'border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30'
            : 'border-macos-orange/20 bg-macos-orange/5'
      )}
    >
      {/* Status icon */}
      {dependency.installed ? (
        <CheckCircle className="w-5 h-5 text-macos-green flex-shrink-0" />
      ) : isOptional ? (
        <AlertCircle className="w-5 h-5 text-macos-text-tertiary flex-shrink-0" />
      ) : (
        <XCircle className="w-5 h-5 text-macos-orange flex-shrink-0" />
      )}

      {/* Name and version */}
      <div className="flex-1 min-w-0">
        <p className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
          {dependency.name}
        </p>
        {dependency.installed && dependency.version && (
          <p className="text-xs text-macos-text-tertiary">{dependency.version}</p>
        )}
        {dependency.installed && dependency.binaryPath && (
          <p className="text-xs text-macos-text-tertiary truncate" title={dependency.binaryPath}>
            {dependency.binaryPath}
          </p>
        )}
      </div>

      {/* Action buttons */}
      {!dependency.installed && (
        <div className="flex items-center gap-2">
          {/* Locate button - always show for non-optional deps */}
          {!isOptional && (
            <button
              onClick={onLocate}
              disabled={isAnyOperationActive}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
                isAnyOperationActive
                  ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                  : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-macos-text-primary dark:text-macos-text-dark-primary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary'
              )}
              title="Locate the binary manually if auto-detection fails"
            >
              {isLocating ? (
                <>
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  Locating...
                </>
              ) : (
                <>
                  <FolderOpen className="w-3.5 h-3.5" />
                  Locate
                </>
              )}
            </button>
          )}

          {/* Auto-install button (primary action) */}
          {dependency.supportsAutoInstall && (
            <button
              onClick={onAutoInstall}
              disabled={isAnyOperationActive}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
                isAnyOperationActive
                  ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                  : 'bg-macos-blue text-white hover:bg-macos-blue/90'
              )}
              title="Automatically download and install"
            >
              <Rocket className="w-3.5 h-3.5" />
              Auto Install
            </button>
          )}

          {/* Manual install via Homebrew - show as secondary option if Homebrew available */}
          {hasHomebrew && dependency.brewFormula && !dependency.supportsAutoInstall && (
            <button
              onClick={onInstall}
              disabled={isAnyOperationActive}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
                isAnyOperationActive
                  ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                  : 'bg-macos-blue text-white hover:bg-macos-blue/90'
              )}
            >
              {isInstalling ? (
                <>
                  <Loader2 className="w-3.5 h-3.5 animate-spin" />
                  Installing...
                </>
              ) : (
                <>
                  <Download className="w-3.5 h-3.5" />
                  Install
                </>
              )}
            </button>
          )}

          {/* Download link - always available as fallback */}
          <button
            onClick={onOpenUrl}
            disabled={isAnyOperationActive}
            className={cn(
              'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
              isAnyOperationActive
                ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-macos-text-primary dark:text-macos-text-dark-primary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary'
            )}
            title="Open download page in browser"
          >
            <ExternalLink className="w-3.5 h-3.5" />
            Download
          </button>
        </div>
      )}
    </div>
  )
}
