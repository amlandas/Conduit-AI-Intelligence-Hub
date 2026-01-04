import { useSetupStore } from '@/stores'
import { ArrowRight, Zap, Shield, Brain } from 'lucide-react'

const features = [
  {
    icon: Brain,
    title: 'AI-Powered Knowledge',
    description: 'Connect your documents, code, and data sources to your AI assistants',
  },
  {
    icon: Zap,
    title: 'Instant Setup',
    description: 'Automated installation of CLI tools, services, and AI models',
  },
  {
    icon: Shield,
    title: 'Local & Private',
    description: 'All data stays on your machine with local LLMs and vector storage',
  },
]

export function WelcomeStep(): JSX.Element {
  const { setStep } = useSetupStore()

  const handleContinue = () => {
    setStep('cli-install')
  }

  return (
    <div className="space-y-8">
      {/* Hero section */}
      <div className="text-center">
        <div className="w-20 h-20 mx-auto mb-6 rounded-2xl bg-gradient-to-br from-macos-blue to-macos-purple flex items-center justify-center">
          <span className="text-4xl">🔮</span>
        </div>
        <h1 className="text-3xl font-bold text-macos-text-primary dark:text-macos-text-dark-primary mb-3">
          Welcome to Conduit
        </h1>
        <p className="text-lg text-macos-text-secondary dark:text-macos-text-dark-secondary max-w-md mx-auto">
          Your AI Intelligence Hub for connecting knowledge sources to AI assistants
        </p>
      </div>

      {/* Features */}
      <div className="grid gap-4">
        {features.map((feature) => (
          <div
            key={feature.title}
            className="flex items-start gap-4 p-4 rounded-xl bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-secondary/50"
          >
            <div className="w-10 h-10 rounded-lg bg-macos-blue/10 flex items-center justify-center flex-shrink-0">
              <feature.icon className="w-5 h-5 text-macos-blue" />
            </div>
            <div>
              <h3 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                {feature.title}
              </h3>
              <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                {feature.description}
              </p>
            </div>
          </div>
        ))}
      </div>

      {/* What will be installed */}
      <div className="p-4 rounded-xl bg-macos-bg-tertiary/50 dark:bg-macos-bg-dark-tertiary/50">
        <h4 className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary mb-2">
          This wizard will help you set up:
        </h4>
        <ul className="space-y-1.5 text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
          <li className="flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-macos-blue" />
            Conduit CLI and daemon tools
          </li>
          <li className="flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-macos-blue" />
            Required dependencies (Docker/Podman, Qdrant, FalkorDB)
          </li>
          <li className="flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-macos-blue" />
            Background services for vector search and graph database
          </li>
          <li className="flex items-center gap-2">
            <span className="w-1.5 h-1.5 rounded-full bg-macos-blue" />
            AI models for embeddings and knowledge extraction
          </li>
        </ul>
      </div>

      {/* Continue button */}
      <div className="flex justify-end">
        <button
          onClick={handleContinue}
          className="flex items-center gap-2 px-6 py-3 rounded-lg bg-macos-blue text-white font-medium hover:bg-macos-blue/90 transition-colors"
        >
          Get Started
          <ArrowRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  )
}
