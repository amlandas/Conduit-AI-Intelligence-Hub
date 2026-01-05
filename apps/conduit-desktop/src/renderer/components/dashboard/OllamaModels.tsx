import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { Cpu, Download, Trash2, RefreshCw, Loader2, Check } from 'lucide-react'

interface OllamaModel {
  name: string
  size: string
  modified: string
  digest: string
}

const RECOMMENDED_MODELS = [
  { name: 'nomic-embed-text', purpose: 'Embeddings', size: '~274MB' },
  { name: 'qwen2.5-coder:7b', purpose: 'Code Chat', size: '~4.7GB' },
  { name: 'mistral:7b-instruct-q4_K_M', purpose: 'KAG Extraction', size: '~4.1GB' }
]

export function OllamaModels(): JSX.Element {
  const [models, setModels] = useState<OllamaModel[]>([])
  const [loading, setLoading] = useState(true)
  const [pulling, setPulling] = useState<string | null>(null)

  const fetchModels = async (): Promise<void> => {
    setLoading(true)
    try {
      // In a real implementation, this would call the daemon API
      // which would proxy to Ollama's /api/tags endpoint
      const response = await fetch('http://localhost:11434/api/tags')
      if (response.ok) {
        const data = await response.json()
        setModels(
          (data.models || []).map((m: { name: string; size: number; modified_at: string; digest: string }) => ({
            name: m.name,
            size: formatBytes(m.size),
            modified: new Date(m.modified_at).toLocaleDateString(),
            digest: m.digest?.slice(0, 12) || ''
          }))
        )
      }
    } catch (err) {
      console.error('Failed to fetch Ollama models:', err)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    fetchModels()
  }, [])

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
  }

  const handlePull = async (modelName: string): Promise<void> => {
    setPulling(modelName)
    try {
      // This would trigger a model pull via the daemon
      // For now, just simulate the API call
      await fetch('http://localhost:11434/api/pull', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: modelName })
      })
      // Refresh the list after pulling
      await fetchModels()
    } catch (err) {
      console.error('Failed to pull model:', err)
    } finally {
      setPulling(null)
    }
  }

  const isModelInstalled = (modelName: string): boolean => {
    return models.some((m) => m.name.startsWith(modelName.split(':')[0]))
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
          <p className="text-sm text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary py-2">
            No models installed. Pull a model below to get started.
          </p>
        ) : (
          <div className="space-y-2">
            {models.map((model) => (
              <div
                key={model.name}
                className="flex items-center gap-3 p-2 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50"
              >
                <Cpu className="w-4 h-4 text-macos-purple flex-shrink-0" />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium truncate">{model.name}</p>
                  <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
                    {model.size}
                  </p>
                </div>
                <button
                  className="p-1 rounded hover:bg-macos-red/10 text-macos-red opacity-0 group-hover:opacity-100 transition-opacity"
                  title="Delete model"
                >
                  <Trash2 className="w-3.5 h-3.5" />
                </button>
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
                      <Loader2 className="w-3.5 h-3.5 animate-spin" />
                    ) : (
                      <Download className="w-3.5 h-3.5" />
                    )}
                    {isPulling ? 'Pulling...' : 'Pull'}
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
