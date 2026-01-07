/**
 * TerminalInstallStep Component
 *
 * Setup wizard step that guides users to run the CLI install script.
 * Uses native Terminal.app for reliable execution instead of embedded terminal.
 */
import { useState, useCallback } from 'react'
import { useSetupStore } from '@/stores'
import {
  ArrowLeft,
  ArrowRight,
  AlertCircle,
  CheckCircle,
  Terminal,
  Copy,
  Check,
  ExternalLink,
} from 'lucide-react'
import { cn } from '@/lib/utils'

// Install script URL
const INSTALL_SCRIPT_URL = 'https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh'

// The command to run in the terminal
const INSTALL_COMMAND = `curl -fsSL ${INSTALL_SCRIPT_URL} | bash`

type InstallStatus = 'ready' | 'verifying' | 'success' | 'error'

export function TerminalInstallStep(): JSX.Element {
  const { setStep, setCLIInstalled } = useSetupStore()
  const [status, setStatus] = useState<InstallStatus>('ready')
  const [errorMessage, setErrorMessage] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  // Open native Terminal.app
  const handleOpenTerminal = useCallback(() => {
    console.log('[TerminalInstallStep] Opening Terminal.app')
    window.conduit.openTerminal()
  }, [])

  // Copy command to clipboard
  const handleCopyCommand = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(INSTALL_COMMAND)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch (err) {
      console.error('Failed to copy:', err)
    }
  }, [])

  // Verify CLI installation and continue
  const handleVerifyAndContinue = useCallback(async () => {
    console.log('[TerminalInstallStep] Verifying CLI installation...')
    setStatus('verifying')
    setErrorMessage(null)

    try {
      const result = await window.conduit.checkCLI()
      console.log('[TerminalInstallStep] CLI check result:', result)

      if (result.installed) {
        // CLI binary found - version is optional (might fail to parse)
        setCLIInstalled(true, result.version || 'unknown', result.path || undefined)
        setStatus('success')
        // Auto-advance after a short delay
        setTimeout(() => setStep('complete'), 1000)
      } else {
        setErrorMessage('Conduit CLI not found. Please run the installation command first.')
        setStatus('error')
      }
    } catch (err) {
      console.error('[TerminalInstallStep] CLI verification error:', err)
      setErrorMessage(`Failed to verify CLI: ${(err as Error).message}`)
      setStatus('error')
    }
  }, [setStep, setCLIInstalled])

  // Navigate back
  const handleBack = useCallback(() => {
    setStep('welcome')
  }, [setStep])

  // Skip installation
  const handleSkip = useCallback(() => {
    setStep('complete')
  }, [setStep])

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="mb-6">
        <h2 className="text-2xl font-semibold text-macos-text-primary dark:text-macos-text-dark-primary">
          Install Conduit
        </h2>
        <p className="mt-2 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          {status === 'ready' && 'Run the installation command in Terminal to set up Conduit.'}
          {status === 'verifying' && 'Checking installation...'}
          {status === 'success' && 'Installation verified successfully!'}
          {status === 'error' && 'Installation could not be verified.'}
        </p>
      </div>

      {/* Main content */}
      <div className="flex-1 min-h-0">
        {/* Ready state - show instructions */}
        {(status === 'ready' || status === 'error') && (
          <div className="space-y-6">
            {/* Step 1: Copy command */}
            <div className="p-4 rounded-xl bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-secondary/50">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-6 h-6 rounded-full bg-macos-blue text-white flex items-center justify-center text-sm font-medium">
                  1
                </div>
                <h3 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                  Copy the installation command
                </h3>
              </div>
              <div className="p-3 rounded-lg bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary">
                <div className="flex items-center justify-between gap-2">
                  <code className="text-sm font-mono text-macos-text-primary dark:text-macos-text-dark-primary break-all flex-1">
                    {INSTALL_COMMAND}
                  </code>
                  <button
                    onClick={handleCopyCommand}
                    className={cn(
                      'flex items-center gap-1 px-3 py-1.5 rounded-md text-sm font-medium transition-colors flex-shrink-0',
                      copied
                        ? 'bg-macos-green/10 text-macos-green'
                        : 'bg-macos-blue/10 text-macos-blue hover:bg-macos-blue/20'
                    )}
                  >
                    {copied ? <Check className="w-4 h-4" /> : <Copy className="w-4 h-4" />}
                    {copied ? 'Copied!' : 'Copy'}
                  </button>
                </div>
              </div>
            </div>

            {/* Step 2: Open Terminal */}
            <div className="p-4 rounded-xl bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-secondary/50">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-6 h-6 rounded-full bg-macos-blue text-white flex items-center justify-center text-sm font-medium">
                  2
                </div>
                <h3 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                  Open Terminal and paste the command
                </h3>
              </div>
              <button
                onClick={handleOpenTerminal}
                className="flex items-center gap-2 px-4 py-2.5 rounded-lg bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-macos-text-primary dark:text-macos-text-dark-primary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary transition-colors"
              >
                <Terminal className="w-5 h-5" />
                Open Terminal.app
                <ExternalLink className="w-4 h-4 ml-1 opacity-50" />
              </button>
              <p className="mt-2 text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
                Press ⌘V to paste, then press Enter to run
              </p>
            </div>

            {/* Step 3: Verify */}
            <div className="p-4 rounded-xl bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-secondary/50">
              <div className="flex items-center gap-2 mb-3">
                <div className="w-6 h-6 rounded-full bg-macos-blue text-white flex items-center justify-center text-sm font-medium">
                  3
                </div>
                <h3 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                  Click "Verify Installation" when done
                </h3>
              </div>
              <button
                onClick={handleVerifyAndContinue}
                className="flex items-center gap-2 px-6 py-3 rounded-lg bg-macos-blue text-white font-medium hover:bg-macos-blue/90 transition-colors"
              >
                <CheckCircle className="w-5 h-5" />
                Verify Installation
              </button>
            </div>

            {/* Error message */}
            {status === 'error' && errorMessage && (
              <div className="p-4 rounded-xl bg-macos-red/10 border border-macos-red/20">
                <div className="flex items-start gap-3">
                  <AlertCircle className="w-5 h-5 text-macos-red flex-shrink-0 mt-0.5" />
                  <div>
                    <p className="text-sm text-macos-red font-medium">Verification Failed</p>
                    <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary mt-1">
                      {errorMessage}
                    </p>
                  </div>
                </div>
              </div>
            )}
          </div>
        )}

        {/* Verifying state */}
        {status === 'verifying' && (
          <div className="flex items-center justify-center h-full">
            <div className="text-center">
              <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-macos-blue/10 flex items-center justify-center">
                <div className="w-8 h-8 border-2 border-macos-blue border-t-transparent rounded-full animate-spin" />
              </div>
              <p className="text-macos-text-secondary dark:text-macos-text-dark-secondary">
                Checking for Conduit CLI...
              </p>
            </div>
          </div>
        )}

        {/* Success state */}
        {status === 'success' && (
          <div className="flex items-center justify-center h-full">
            <div className="text-center">
              <div className="w-16 h-16 mx-auto mb-4 rounded-full bg-macos-green/10 flex items-center justify-center">
                <CheckCircle className="w-8 h-8 text-macos-green" />
              </div>
              <h3 className="text-lg font-medium text-macos-text-primary dark:text-macos-text-dark-primary mb-2">
                Installation Verified!
              </h3>
              <p className="text-macos-text-secondary dark:text-macos-text-dark-secondary">
                Conduit CLI is installed and ready to use.
              </p>
            </div>
          </div>
        )}
      </div>

      {/* Navigation */}
      <div className="flex-shrink-0 flex justify-between pt-6 border-t border-macos-separator dark:border-macos-separator-dark mt-6">
        <button
          onClick={handleBack}
          disabled={status === 'verifying'}
          className={cn(
            'flex items-center gap-2 px-4 py-2 rounded-lg transition-colors',
            status === 'verifying'
              ? 'text-macos-text-tertiary dark:text-macos-text-dark-tertiary cursor-not-allowed'
              : 'text-macos-text-secondary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary'
          )}
        >
          <ArrowLeft className="w-4 h-4" />
          Back
        </button>

        {(status === 'ready' || status === 'error') && (
          <button
            onClick={handleSkip}
            className="flex items-center gap-2 px-4 py-2 rounded-lg text-macos-text-secondary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary transition-colors"
          >
            Skip for now
            <ArrowRight className="w-4 h-4" />
          </button>
        )}
      </div>
    </div>
  )
}

export default TerminalInstallStep
