import { useSetupStore } from '@/stores'
import {
  CheckCircle,
  Rocket,
  BookOpen,
  Terminal,
  Settings,
  ExternalLink,
} from 'lucide-react'

export function CompleteStep(): JSX.Element {
  const { completeSetup, cliVersion, modelsInstalled, daemonRunning, qdrantRunning } = useSetupStore()

  const handleLaunch = () => {
    completeSetup()
    // The App component will detect setupCompleted and show the main UI
  }

  const handleOpenDocs = () => {
    window.conduit.openExternal('https://conduit.simpleflo.dev/docs')
  }

  const handleOpenTerminal = () => {
    window.conduit.openTerminal()
  }

  return (
    <div className="space-y-8">
      {/* Success header */}
      <div className="text-center">
        <div className="w-20 h-20 mx-auto mb-6 rounded-full bg-macos-green/10 flex items-center justify-center">
          <CheckCircle className="w-10 h-10 text-macos-green" />
        </div>
        <h1 className="text-3xl font-bold text-macos-text-primary dark:text-macos-text-dark-primary mb-3">
          Setup Complete!
        </h1>
        <p className="text-lg text-macos-text-secondary dark:text-macos-text-dark-secondary max-w-md mx-auto">
          Conduit is ready to use. Start by adding your first knowledge source.
        </p>
      </div>

      {/* Summary of what was set up */}
      <div className="p-4 rounded-xl bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-secondary/50">
        <h3 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary mb-3">
          What&apos;s been set up:
        </h3>
        <div className="space-y-2">
          <SummaryItem
            label="CLI Tools"
            value={cliVersion ? `v${cliVersion}` : 'Installed'}
            success
          />
          <SummaryItem
            label="Daemon"
            value={daemonRunning ? 'Running' : 'Not running'}
            success={daemonRunning}
          />
          <SummaryItem
            label="Vector Database (Qdrant)"
            value={qdrantRunning ? 'Running' : 'Not running'}
            success={qdrantRunning}
          />
          <SummaryItem
            label="AI Models"
            value={
              modelsInstalled.length > 0
                ? `${modelsInstalled.length} installed`
                : 'None installed'
            }
            success={modelsInstalled.length > 0}
          />
        </div>
      </div>

      {/* Quick start tips */}
      <div className="space-y-4">
        <h3 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
          Quick Start Tips:
        </h3>
        <div className="grid gap-3">
          <QuickTip
            icon={BookOpen}
            title="Add a knowledge source"
            description="Go to Knowledge Base → Add Source to index your first documents"
          />
          <QuickTip
            icon={Terminal}
            title="Use the CLI"
            description="Open Terminal and run 'conduit help' to see available commands"
          />
          <QuickTip
            icon={Settings}
            title="Configure connectors"
            description="Go to Connectors to set up MCP servers for your AI assistants"
          />
        </div>
      </div>

      {/* Action buttons */}
      <div className="flex flex-col gap-3">
        <button
          onClick={handleLaunch}
          className="w-full flex items-center justify-center gap-2 px-6 py-3 rounded-lg bg-macos-blue text-white font-medium hover:bg-macos-blue/90 transition-colors"
        >
          <Rocket className="w-5 h-5" />
          Launch Conduit
        </button>

        <div className="flex gap-3">
          <button
            onClick={handleOpenDocs}
            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 rounded-lg bg-macos-bg-secondary dark:bg-macos-bg-dark-secondary text-macos-text-primary dark:text-macos-text-dark-primary hover:bg-macos-bg-tertiary dark:hover:bg-macos-bg-dark-tertiary transition-colors"
          >
            <BookOpen className="w-4 h-4" />
            Documentation
            <ExternalLink className="w-3 h-3 text-macos-text-tertiary" />
          </button>
          <button
            onClick={handleOpenTerminal}
            className="flex-1 flex items-center justify-center gap-2 px-4 py-2 rounded-lg bg-macos-bg-secondary dark:bg-macos-bg-dark-secondary text-macos-text-primary dark:text-macos-text-dark-primary hover:bg-macos-bg-tertiary dark:hover:bg-macos-bg-dark-tertiary transition-colors"
          >
            <Terminal className="w-4 h-4" />
            Open Terminal
          </button>
        </div>
      </div>

      {/* Footer note */}
      <p className="text-xs text-center text-macos-text-tertiary">
        You can re-run setup anytime from Settings → Run Setup Wizard
      </p>
    </div>
  )
}

interface SummaryItemProps {
  label: string
  value: string
  success: boolean
}

function SummaryItem({ label, value, success }: SummaryItemProps): JSX.Element {
  return (
    <div className="flex items-center justify-between py-1">
      <span className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
        {label}
      </span>
      <span
        className={`text-sm font-medium ${success ? 'text-macos-green' : 'text-macos-text-tertiary'}`}
      >
        {value}
      </span>
    </div>
  )
}

interface QuickTipProps {
  icon: typeof BookOpen
  title: string
  description: string
}

function QuickTip({ icon: Icon, title, description }: QuickTipProps): JSX.Element {
  return (
    <div className="flex items-start gap-3 p-3 rounded-lg bg-macos-bg-tertiary/30 dark:bg-macos-bg-dark-tertiary/30">
      <div className="w-8 h-8 rounded-lg bg-macos-blue/10 flex items-center justify-center flex-shrink-0">
        <Icon className="w-4 h-4 text-macos-blue" />
      </div>
      <div>
        <p className="font-medium text-sm text-macos-text-primary dark:text-macos-text-dark-primary">
          {title}
        </p>
        <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
          {description}
        </p>
      </div>
    </div>
  )
}
