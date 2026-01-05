import { useState, useEffect } from 'react'
import { useSetupStore } from '@/stores'
import {
  ArrowRight,
  ArrowLeft,
  CheckCircle,
  Download,
  Loader2,
  Brain,
  MessageSquare,
  FileText,
  AlertCircle,
} from 'lucide-react'
import { cn } from '@/lib/utils'

interface ModelInfo {
  name: string
  displayName: string
  description: string
  size: string
  purpose: 'embeddings' | 'chat' | 'extraction'
  installed: boolean
  downloading: boolean
  progress: number
}

const RECOMMENDED_MODELS: Omit<ModelInfo, 'installed' | 'downloading' | 'progress'>[] = [
  {
    name: 'nomic-embed-text',
    displayName: 'Nomic Embed Text',
    description: 'Fast, high-quality text embeddings for semantic search',
    size: '274 MB',
    purpose: 'embeddings',
  },
  {
    name: 'qwen2.5-coder:7b',
    displayName: 'Qwen 2.5 Coder 7B',
    description: 'Excellent for code understanding and generation',
    size: '4.7 GB',
    purpose: 'chat',
  },
  {
    name: 'mistral:7b-instruct',
    displayName: 'Mistral 7B Instruct',
    description: 'General-purpose model for entity extraction',
    size: '4.1 GB',
    purpose: 'extraction',
  },
]

export function ModelsStep(): JSX.Element {
  const {
    setStep,
    modelsInstalled,
    addInstalledModel,
    setOperation,
    setOperationError,
  } = useSetupStore()

  const [models, setModels] = useState<ModelInfo[]>([])
  const [isChecking, setIsChecking] = useState(true)
  const [downloadingModel, setDownloadingModel] = useState<string | null>(null)

  // Check installed models on mount
  useEffect(() => {
    checkModels()
  }, [])

  const checkModels = async () => {
    setIsChecking(true)
    try {
      const installedModels: string[] = await window.conduit.checkModels()

      const modelList = RECOMMENDED_MODELS.map((model) => ({
        ...model,
        installed: installedModels.some((m) => m.includes(model.name.split(':')[0])),
        downloading: false,
        progress: 0,
      }))

      setModels(modelList)
    } catch (error) {
      setOperationError(`Failed to check models: ${error}`)
      // Still set models with unknown status
      setModels(
        RECOMMENDED_MODELS.map((model) => ({
          ...model,
          installed: modelsInstalled.includes(model.name),
          downloading: false,
          progress: 0,
        }))
      )
    } finally {
      setIsChecking(false)
    }
  }

  const handlePullModel = async (modelName: string) => {
    setDownloadingModel(modelName)
    setOperation(`Downloading ${modelName}...`)

    // Update model state to show downloading
    setModels((prev) =>
      prev.map((m) => (m.name === modelName ? { ...m, downloading: true, progress: 0 } : m))
    )

    try {
      // Subscribe to progress updates
      const cleanup = window.conduit.onOllamaPullProgress((data: { model: string; progress: number }) => {
        if (data.model === modelName) {
          setModels((prev) =>
            prev.map((m) => (m.name === modelName ? { ...m, progress: data.progress } : m))
          )
        }
      })

      const result = await window.conduit.pullModel({ model: modelName })

      cleanup()

      if (result.success) {
        addInstalledModel(modelName)
        setModels((prev) =>
          prev.map((m) =>
            m.name === modelName ? { ...m, installed: true, downloading: false, progress: 100 } : m
          )
        )
        setOperation(null)
      } else {
        setOperationError(result.error || `Failed to download ${modelName}`)
        setModels((prev) =>
          prev.map((m) => (m.name === modelName ? { ...m, downloading: false, progress: 0 } : m))
        )
      }
    } catch (error) {
      setOperationError(`Failed to download ${modelName}: ${error}`)
      setModels((prev) =>
        prev.map((m) => (m.name === modelName ? { ...m, downloading: false, progress: 0 } : m))
      )
    } finally {
      setDownloadingModel(null)
    }
  }

  const handlePullAll = async () => {
    const pendingModels = models.filter((m) => !m.installed)
    for (const model of pendingModels) {
      await handlePullModel(model.name)
    }
  }

  const handleContinue = () => {
    setStep('complete')
  }

  const handleBack = () => {
    setStep('services')
  }

  const handleSkip = () => {
    setStep('complete')
  }

  // At minimum, embedding model should be installed to continue
  const embeddingInstalled = models.find((m) => m.purpose === 'embeddings')?.installed
  const allInstalled = models.every((m) => m.installed)
  const anyDownloading = models.some((m) => m.downloading)

  const getPurposeIcon = (purpose: string) => {
    switch (purpose) {
      case 'embeddings':
        return Brain
      case 'chat':
        return MessageSquare
      case 'extraction':
        return FileText
      default:
        return Brain
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div>
        <h2 className="text-2xl font-semibold text-macos-text-primary dark:text-macos-text-dark-primary">
          Download AI Models
        </h2>
        <p className="mt-2 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          Conduit uses local AI models for embeddings and knowledge extraction
        </p>
      </div>

      {/* Loading state */}
      {isChecking ? (
        <div className="flex items-center gap-3 p-4 rounded-xl bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30">
          <Loader2 className="w-5 h-5 animate-spin text-macos-blue" />
          <span className="text-macos-text-secondary dark:text-macos-text-dark-secondary">
            Checking installed models...
          </span>
        </div>
      ) : (
        <>
          {/* Models list */}
          <div className="space-y-3">
            {models.map((model) => {
              const Icon = getPurposeIcon(model.purpose)
              return (
                <div
                  key={model.name}
                  className={cn(
                    'p-4 rounded-xl border transition-colors',
                    model.installed
                      ? 'border-macos-green/20 bg-macos-green/5'
                      : 'border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-secondary/30'
                  )}
                >
                  <div className="flex items-start gap-3">
                    {/* Icon */}
                    <div
                      className={cn(
                        'w-10 h-10 rounded-lg flex items-center justify-center flex-shrink-0',
                        model.installed
                          ? 'bg-macos-green/10'
                          : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary'
                      )}
                    >
                      <Icon
                        className={cn(
                          'w-5 h-5',
                          model.installed ? 'text-macos-green' : 'text-macos-text-tertiary'
                        )}
                      />
                    </div>

                    {/* Info */}
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <p className="font-medium text-macos-text-primary dark:text-macos-text-dark-primary">
                          {model.displayName}
                        </p>
                        <span className="px-2 py-0.5 rounded-full bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
                          {model.size}
                        </span>
                        {model.installed && (
                          <CheckCircle className="w-4 h-4 text-macos-green" />
                        )}
                      </div>
                      <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                        {model.description}
                      </p>

                      {/* Progress bar when downloading */}
                      {model.downloading && (
                        <div className="mt-2">
                          <div className="h-1.5 rounded-full bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary overflow-hidden">
                            <div
                              className="h-full bg-macos-blue transition-all duration-300"
                              style={{ width: `${model.progress}%` }}
                            />
                          </div>
                          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary mt-1">
                            {model.progress}% downloaded
                          </p>
                        </div>
                      )}
                    </div>

                    {/* Action button */}
                    {!model.installed && !model.downloading && (
                      <button
                        onClick={() => handlePullModel(model.name)}
                        disabled={downloadingModel !== null}
                        className={cn(
                          'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors flex-shrink-0',
                          downloadingModel !== null
                            ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                            : 'bg-macos-blue text-white hover:bg-macos-blue/90'
                        )}
                      >
                        <Download className="w-3.5 h-3.5" />
                        Pull
                      </button>
                    )}
                    {model.downloading && (
                      <Loader2 className="w-5 h-5 animate-spin text-macos-blue flex-shrink-0" />
                    )}
                  </div>
                </div>
              )
            })}
          </div>

          {/* Download all button */}
          {!allInstalled && !anyDownloading && (
            <button
              onClick={handlePullAll}
              className="w-full flex items-center justify-center gap-2 px-4 py-3 rounded-lg font-medium bg-macos-blue text-white hover:bg-macos-blue/90 transition-colors"
            >
              <Download className="w-4 h-4" />
              Download All Models
            </button>
          )}

          {/* Warning about embedding model */}
          {!embeddingInstalled && (
            <div className="p-3 rounded-lg bg-macos-orange/10 border border-macos-orange/20">
              <div className="flex items-start gap-2">
                <AlertCircle className="w-4 h-4 text-macos-orange flex-shrink-0 mt-0.5" />
                <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                  The embedding model (Nomic Embed Text) is required for semantic search.
                  Other models can be downloaded later.
                </p>
              </div>
            </div>
          )}

          {/* Note about model sizes */}
          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary text-center">
            Models are stored locally by Ollama. Total download size: ~9 GB
          </p>
        </>
      )}

      {/* Navigation */}
      <div className="flex justify-between pt-4">
        <button
          onClick={handleBack}
          disabled={anyDownloading}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-macos-text-secondary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary transition-colors disabled:opacity-50"
        >
          <ArrowLeft className="w-4 h-4" />
          Back
        </button>
        <div className="flex items-center gap-3">
          {!embeddingInstalled && (
            <button
              onClick={handleSkip}
              disabled={anyDownloading}
              className="px-4 py-2 rounded-lg text-macos-text-secondary hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-secondary transition-colors disabled:opacity-50"
            >
              Skip for now
            </button>
          )}
          <button
            onClick={handleContinue}
            disabled={anyDownloading}
            className={cn(
              'flex items-center gap-2 px-6 py-2 rounded-lg font-medium transition-colors',
              anyDownloading
                ? 'bg-macos-bg-tertiary text-macos-text-tertiary cursor-not-allowed'
                : 'bg-macos-blue text-white hover:bg-macos-blue/90'
            )}
          >
            {embeddingInstalled ? 'Continue' : 'Continue Anyway'}
            <ArrowRight className="w-4 h-4" />
          </button>
        </div>
      </div>
    </div>
  )
}
