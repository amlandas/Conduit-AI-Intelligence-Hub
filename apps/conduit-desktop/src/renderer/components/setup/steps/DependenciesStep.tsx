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
} from 'lucide-react'
import { cn } from '@/lib/utils'

const REQUIRED_DEPENDENCIES: Omit<DependencyStatus, 'installed' | 'version'>[] = [
  {
    name: 'Ollama',
    required: true,
    installUrl: 'https://ollama.com/download',
    brewFormula: 'ollama',
  },
  {
    name: 'Docker or Podman',
    required: true,
    installUrl: 'https://podman.io/getting-started/installation',
    brewFormula: 'podman',
  },
]

const OPTIONAL_DEPENDENCIES: Omit<DependencyStatus, 'installed' | 'version'>[] = [
  {
    name: 'Homebrew',
    required: false,
    installUrl: 'https://brew.sh',
  },
]

export function DependenciesStep(): JSX.Element {
  const { setStep, setDependencies, dependencies, setOperation, setOperationError } = useSetupStore()

  const [isChecking, setIsChecking] = useState(true)
  const [isInstalling, setIsInstalling] = useState<string | null>(null)

  // Check dependencies on mount
  useEffect(() => {
    checkDependencies()
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
                  dependency={{ ...dep, installed: status?.installed || false, version: status?.version }}
                  isInstalling={isInstalling === dep.name}
                  hasHomebrew={hasHomebrew || false}
                  onInstall={() => handleInstall({ ...dep, installed: false })}
                  onOpenUrl={() => handleOpenUrl(dep.installUrl!)}
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
                  dependency={{ ...dep, installed: status?.installed || false, version: status?.version }}
                  isInstalling={isInstalling === dep.name}
                  hasHomebrew={true} // Always show external link for optional
                  onInstall={() => {}}
                  onOpenUrl={() => handleOpenUrl(dep.installUrl!)}
                  isOptional
                />
              )
            })}
          </div>

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
  dependency: DependencyStatus
  isInstalling: boolean
  hasHomebrew: boolean
  onInstall: () => void
  onOpenUrl: () => void
  isOptional?: boolean
}

function DependencyRow({
  dependency,
  isInstalling,
  hasHomebrew,
  onInstall,
  onOpenUrl,
  isOptional,
}: DependencyRowProps): JSX.Element {
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
      </div>

      {/* Action button */}
      {!dependency.installed && (
        <div className="flex items-center gap-2">
          {hasHomebrew && dependency.brewFormula ? (
            <button
              onClick={onInstall}
              disabled={isInstalling}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
                isInstalling
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
          ) : (
            <button
              onClick={onOpenUrl}
              className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-macos-text-primary dark:text-macos-text-dark-primary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary transition-colors"
            >
              <ExternalLink className="w-3.5 h-3.5" />
              Download
            </button>
          )}
        </div>
      )}
    </div>
  )
}
