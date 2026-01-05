/**
 * OllamaModels Component
 *
 * PRINCIPLE: GUI is a thin wrapper over CLI
 * All operations delegate to `conduit` CLI commands via IPC - NO direct HTTP calls.
 */
import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { Cpu, Download, RefreshCw, Loader2, Check } from 'lucide-react'

const RECOMMENDED_MODELS = [
  { name: 'nomic-embed-text', purpose: 'Embeddings', size: '~274MB' },
  { name: 'qwen2.5-coder:7b', purpose: 'Code Chat', size: '~4.7GB' },
  { name: 'mistral:7b-instruct-q4_K_M', purpose: 'KAG Extraction', size: '~4.1GB' }
]

export function OllamaModels(): JSX.Element {
  const [models, setModels] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [pulling, setPulling] = useState<string | null>(null)
  const [pullProgress, setPullProgress] = useState<number>(0)

  // Fetch models via CLI: conduit ollama models
  const fetchModels = async (): Promise<void> => {
    setLoading(true)
    try {
      const installedModels = await window.conduit.checkModels()
      setModels(installedModels)
    } catch (err) {
      console.error('Failed to fetch Ollama models:', err)
      setModels([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchModels()

    // Subscribe to pull progress events
    const cleanup = window.conduit.onOllamaPullProgress(({ model, progress }: { model: string; progress: number }) => {
      if (pulling === model || model) {
        setPullProgress(progress)
      }
    })

    return cleanup
  }, [pulling])

  // Pull model via CLI: conduit ollama pull <model>
  const handlePull = async (modelName: string): Promise<void> => {
    setPulling(modelName)
    setPullProgress(0)
    try {
      const result = await window.conduit.pullModel({ model: modelName })
      if (!result.success) {
        console.error('Failed to pull model:', result.error)
      }
      // Refresh the list after pulling
      await fetchModels()
    } catch (err) {
      console.error('Failed to pull model:', err)
    } finally {
      setPulling(null)
      setPullProgress(0)
    }
  }

  const isModelInstalled = (modelName: string): boolean => {
    // Check if any installed model starts with the base model name
    const baseName = modelName.split(':')[0]
    return models.some((m) => m.startsWith(baseName))
  }

  return (
    <section>
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-lg font-medium">Ollama Models</h2>
        <button
          onClick={fetchModels}
          className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
          disabled={loading}
        >
          <RefreshCw className={cn('w-4 h-4', loading && 'animate-spin')} />
        </button>
      </div>

      {/* Installed Models */}
      <div className="card p-4 mb-4">
        <h3 className="text-sm font-medium mb-3 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          Installed Models
        </h3>
        {loading ? (
          <div className="flex items-center justify-center py-4">
            <Loader2 className="w-5 h-5 animate-spin text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
          </div>
        ) : models.length === 0 ? (
          <p className="text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary py-2">
            No models installed. Pull a model below to get started.
          </p>
        ) : (
          <div className="space-y-2">
            {models.map((modelName) => (
              <div
                key={modelName}
                className="flex items-center gap-3 p-2 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50"
              >
                <Cpu className="w-4 h-4 text-macos-purple flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{modelName}</p>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>

      {/* Recommended Models */}
      <div className="card p-4">
        <h3 className="text-sm font-medium mb-3 text-macos-text-secondary dark:text-macos-text-dark-secondary">
          Recommended for Conduit
        </h3>
        <div className="space-y-2">
          {RECOMMENDED_MODELS.map((model) => {
            const installed = isModelInstalled(model.name)
            const isPulling = pulling === model.name

            return (
              <div
                key={model.name}
                className="flex items-center gap-3 p-2 rounded-lg hover:bg-macos-bg-secondary/50 dark:hover:bg-macos-bg-dark-tertiary/50"
              >
                <Cpu className="w-4 h-4 text-macos-blue flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium">{model.name}</p>
                  <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
                    {model.purpose} · {model.size}
                  </p>
                </div>
                {installed ? (
                  <span className="flex items-center gap-1 text-xs text-macos-green">
                    <Check className="w-3.5 h-3.5" />
                    Installed
                  </span>
                ) : (
                  <button
                    onClick={() => handlePull(model.name)}
                    disabled={isPulling}
                    className="flex items-center gap-1 text-xs text-macos-blue hover:underline disabled:opacity-50"
                  >
                    {isPulling ? (
                      <>
                        <Loader2 className="w-3.5 h-3.5 animate-spin" />
                        {pullProgress > 0 ? `${pullProgress}%` : 'Starting...'}
                      </>
                    ) : (
                      <>
                        <Download className="w-3.5 h-3.5" />
                        Pull
                      </>
                    )}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </section>
  )
}
