import { useCallback } from 'react'
import { useSetupStore, SetupStep } from '@/stores'
import { cn } from '@/lib/utils'
import {
  CheckCircle,
  Circle,
  Loader2,
  Rocket,
  Terminal,
  Package,
  Server,
  Cpu,
  AlertCircle,
  ExternalLink
} from 'lucide-react'

// Step components
import { WelcomeStep } from './steps/WelcomeStep'
import { CLIInstallStep } from './steps/CLIInstallStep'
import { DependenciesStep } from './steps/DependenciesStep'
import { ServicesStep } from './steps/ServicesStep'
import { ModelsStep } from './steps/ModelsStep'
import { CompleteStep } from './steps/CompleteStep'

interface StepConfig {
  id: SetupStep
  title: string
  description: string
  icon: typeof Rocket
}

const steps: StepConfig[] = [
  {
    id: 'welcome',
    title: 'Welcome',
    description: 'Get started with Conduit',
    icon: Rocket,
  },
  {
    id: 'cli-install',
    title: 'CLI Tools',
    description: 'Install command-line tools',
    icon: Terminal,
  },
  {
    id: 'dependencies',
    title: 'Dependencies',
    description: 'Check required software',
    icon: Package,
  },
  {
    id: 'services',
    title: 'Services',
    description: 'Start background services',
    icon: Server,
  },
  {
    id: 'models',
    title: 'AI Models',
    description: 'Download AI models',
    icon: Cpu,
  },
  {
    id: 'complete',
    title: 'Complete',
    description: 'Ready to use',
    icon: CheckCircle,
  },
]

function StepIndicator({
  step,
  currentStep,
  isCompleted,
}: {
  step: StepConfig
  currentStep: SetupStep
  isCompleted: boolean
}): JSX.Element {
  const isCurrent = step.id === currentStep
  const stepIndex = steps.findIndex((s) => s.id === step.id)
  const currentIndex = steps.findIndex((s) => s.id === currentStep)
  const isPast = stepIndex < currentIndex

  return (
    <div
      className={cn(
        'flex items-center gap-3 px-3 py-2 rounded-lg transition-colors',
        isCurrent && 'bg-macos-blue/10',
        !isCurrent && !isPast && 'opacity-50'
      )}
    >
      <div
        className={cn(
          'w-8 h-8 rounded-full flex items-center justify-center',
          isPast || isCompleted
            ? 'bg-macos-green text-white'
            : isCurrent
              ? 'bg-macos-blue text-white'
              : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary'
        )}
      >
        {isPast || isCompleted ? (
          <CheckCircle className="w-5 h-5" />
        ) : isCurrent ? (
          <step.icon className="w-4 h-4" />
        ) : (
          <Circle className="w-4 h-4" />
        )}
      </div>
      <div className="flex-1 min-w-0">
        <p
          className={cn(
            'text-sm font-medium truncate',
            isCurrent
              ? 'text-macos-text-primary dark:text-macos-text-dark-primary'
              : 'text-macos-text-secondary dark:text-macos-text-dark-secondary'
          )}
        >
          {step.title}
        </p>
        <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary truncate">
          {step.description}
        </p>
      </div>
    </div>
  )
}

export function SetupWizard(): JSX.Element {
  const {
    currentStep,
    setupCompleted,
    currentOperation,
    operationError,
  } = useSetupStore()

  const renderStep = useCallback(() => {
    switch (currentStep) {
      case 'welcome':
        return <WelcomeStep />
      case 'cli-install':
        return <CLIInstallStep />
      case 'dependencies':
        return <DependenciesStep />
      case 'services':
        return <ServicesStep />
      case 'models':
        return <ModelsStep />
      case 'complete':
        return <CompleteStep />
      default:
        return <WelcomeStep />
    }
  }, [currentStep])

  return (
    <div className="h-screen flex bg-macos-bg-primary dark:bg-macos-bg-dark-primary">
      {/* Sidebar with steps */}
      <aside className="w-64 flex-shrink-0 border-r border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-secondary/50 p-4">
        {/* Title bar space */}
        <div className="h-8 mb-4 drag-region" />

        {/* Logo and title */}
        <div className="mb-6 px-3">
          <h1 className="text-xl font-semibold text-macos-text-primary dark:text-macos-text-dark-primary">
            Conduit Setup
          </h1>
          <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
            AI Intelligence Hub
          </p>
        </div>

        {/* Step indicators */}
        <nav className="space-y-1">
          {steps.map((step) => (
            <StepIndicator
              key={step.id}
              step={step}
              currentStep={currentStep}
              isCompleted={setupCompleted && step.id === 'complete'}
            />
          ))}
        </nav>

        {/* Current operation indicator */}
        {currentOperation && (
          <div className="mt-6 px-3">
            <div className="flex items-center gap-2 text-sm text-macos-text-secondary">
              <Loader2 className="w-4 h-4 animate-spin text-macos-blue" />
              <span className="truncate">{currentOperation}</span>
            </div>
          </div>
        )}

        {/* Error indicator */}
        {operationError && (
          <div className="mt-6 px-3">
            <div className="flex items-start gap-2 text-sm text-macos-red">
              <AlertCircle className="w-4 h-4 flex-shrink-0 mt-0.5" />
              <span>{operationError}</span>
            </div>
          </div>
        )}

        {/* Help link */}
        <div className="absolute bottom-4 left-4 right-4">
          <a
            href="https://conduit.simpleflo.dev/docs/installation"
            target="_blank"
            rel="noopener noreferrer"
            className="flex items-center gap-2 text-sm text-macos-blue hover:underline"
          >
            <ExternalLink className="w-4 h-4" />
            Installation Help
          </a>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-auto">
        {/* Title bar drag region */}
        <div className="h-8 drag-region" />

        {/* Step content */}
        <div className="p-8 max-w-2xl mx-auto">
          {renderStep()}
        </div>
      </main>
    </div>
  )
}
