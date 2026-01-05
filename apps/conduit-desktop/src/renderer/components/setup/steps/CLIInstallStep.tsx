import { useState, useEffect } from 'react'
import { useSetupStore } from '@/stores'
import {
  ArrowRight,
  ArrowLeft,
  Terminal,
  CheckCircle,
  AlertCircle,
  Loader2,
  FolderOpen,
  RefreshCw,
} from 'lucide-react'
import { cn } from '@/lib/utils'

interface CLIStatus {
  installed: boolean
  version: string | null
  path: string | null
  bundledVersion: string | null
  needsUpdate: boolean
}

export function CLIInstallStep(): JSX.Element {
  const { setStep, setCLIInstalled, setOperation, setOperationError } = useSetupStore()

  const [cliStatus, setCLIStatus] = useState<CLIStatus | null>(null)
  const [isChecking, setIsChecking] = useState(true)
  const [isInstalling, setIsInstalling] = useState(false)
  const [installPath] = useState('~/.local/bin')

  // Check CLI status on mount
  useEffect(() => {
    checkCLIStatus()
  }, [])

  const checkCLIStatus = async () => {
    setIsChecking(true)
    try {
      const status = await window.conduit.checkCLI()
      setCLIStatus(status)
      if (status.installed && !status.needsUpdate) {
        setCLIInstalled(true, status.version || undefined, status.path || undefined)
      }
    } catch (error) {
      setOperationError(`Failed to check CLI status: ${error}`)
    } finally {
      setIsChecking(false)
    }
  }

  const handleInstall = async () => {
    setIsInstalling(true)
    setOperation('Installing CLI tools...')
    try {
      const result = await window.conduit.installCLI({
        installPath: installPath.replace('~', ''),
      })

      if (result.success) {
        setCLIInstalled(true, result.version, result.path)
        setCLIStatus({
          installed: true,
          version: result.version || null,
          path: result.path || null,
          bundledVersion: cliStatus?.bundledVersion || null,
          needsUpdate: false,
        })
        setOperation(null)
      } else {
        setOperationError(result.error || 'Installation failed')
      }
    } catch (error) {
      setOperationError(`Installation failed: ${error}`)
    } finally {
      setIsInstalling(false)
    }
  }

  const handleContinue = () => {
    setStep('dependencies')
  }

  const handleBack = () => {
    setStep('welcome')
  }

  const canContinue = cliStatus?.installed && !cliStatus?.needsUpdate

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h2 className="text-2xl font-semibold text-macos-text-primary dark:text-macos-text-dark-primary">
          Install CLI Tools
        </h2>
        <p className="mt-2 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          Conduit CLI provides command-line access and runs the background daemon
        </p>
      </div>

      {/* Status card */}
      <div className="p-4 rounded-xl border border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30">
        {isChecking ? (
          <div className="flex items-center gap-3">
            <Loader2 className="w-5 h-5 animate-spin text-macos-blue" />
            <span className="text-macos-text-secondary dark:text-macos-text-dark-secondary">
              Checking CLI installation...
            </span>
          </div>
        ) : cliStatus ? (
          <div className="space-y-4">
            {/* Current status */}
            <div className="flex items-start gap-3">
              {cliStatus.installed && !cliStatus.needsUpdate ? (
                <CheckCircle className="w-5 h-5 text-macos-green flex-shrink-0 mt-0.5" />
              ) : cliStatus.installed && cliStatus.needsUpdate ? (
                <AlertCircle className="w-5 h-5 text-macos-orange flex-shrink-0 mt-0.5" />
              ) : (
                <AlertCircle className="w-5 h-5 text-macos-text-tertiary dark:text-macos-text-dark-tertiary flex-shrink-0 mt-0.5" />
              )}
              <div className="flex-1">
                <p className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                  {cliStatus.installed && !cliStatus.needsUpdate
                    ? 'CLI tools are installed'
                    : cliStatus.installed && cliStatus.needsUpdate
                      ? 'CLI tools need update'
                      : 'CLI tools not found'}
                </p>
                {cliStatus.installed && (
                  <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                    Version {cliStatus.version} at {cliStatus.path}
                  </p>
                )}
                {cliStatus.needsUpdate && (
                  <p className="text-sm text-macos-orange">
                    Update available: v{cliStatus.bundledVersion}
                  </p>
                )}
              </div>
              <button
                onClick={checkCLIStatus}
                className="p-2 rounded-lg hover:bg-macos-bg-tertiary dark:hover:bg-macos-bg-dark-tertiary transition-colors"
                title="Refresh status"
              >
                <RefreshCw className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
              </button>
            </div>

            {/* Install/Update section */}
            {(!cliStatus.installed || cliStatus.needsUpdate) && (
              <div className="pt-4 border-t border-macos-separator dark:border-macos-separator-dark">
                <div className="flex items-center gap-2 mb-3">
                  <FolderOpen className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
                  <span className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                    Install location: <code className="font-mono">{installPath}</code>
                  </span>
                </div>

                <p className="text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary mb-4">
                  This will install <code className="font-mono">conduit</code> and{' '}
                  <code className="font-mono">conduit-daemon</code> to your local bin directory.
                  Make sure <code className="font-mono">{installPath}</code> is in your PATH.
                </p>

                <button
                  onClick={handleInstall}
                  disabled={isInstalling}
                  className={cn(
                    'flex items-center gap-2 px-4 py-2 rounded-lg font-medium transition-colors',
                    isInstalling
                      ? 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-macos-text-tertiary dark:text-macos-text-dark-tertiary cursor-not-allowed'
                      : 'bg-macos-blue text-white hover:bg-macos-blue/90'
                  )}
                >
                  {isInstalling ? (
                    <>
                      <Loader2 className="w-4 h-4 animate-spin" />
                      Installing...
                    </>
                  ) : (
                    <>
                      <Terminal className="w-4 h-4" />
                      {cliStatus.needsUpdate ? 'Update CLI Tools' : 'Install CLI Tools'}
                    </>
                  )}
                </button>
              </div>
            )}
          </div>
        ) : (
          <div className="flex items-center gap-3 text-macos-red">
            <AlertCircle className="w-5 h-5" />
            <span>Failed to check CLI status</span>
          </div>
        )}
      </div>

      {/* What gets installed */}
      <div className="p-4 rounded-xl bg-macos-bg-tertiary/30 dark:bg-macos-bg-dark-tertiary/30">
        <h4 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary mb-3">
          Included tools:
        </h4>
        <div className="grid grid-cols-2 gap-3">
          <div className="flex items-start gap-2">
            <Terminal className="w-4 h-4 text-macos-blue mt-0.5" />
            <div>
              <p className="text-sm font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                conduit
              </p>
              <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">CLI for managing sources and search</p>
            </div>
          </div>
          <div className="flex items-start gap-2">
            <Terminal className="w-4 h-4 text-macos-purple mt-0.5" />
            <div>
              <p className="text-sm font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                conduit-daemon
              </p>
              <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">Background service for API access</p>
            </div>
          </div>
        </div>
      </div>

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
              : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-macos-text-tertiary dark:text-macos-text-dark-tertiary cursor-not-allowed'
          )}
        >
          Continue
          <ArrowRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  )
}
