import { useState, useEffect, useCallback } from 'react'
import Editor from '@monaco-editor/react'
import { cn } from '@/lib/utils'
import {
  FileCode,
  Save,
  RotateCcw,
  Download,
  Upload,
  Check,
  AlertTriangle,
  Loader2
} from 'lucide-react'

interface ConfigEditorProps {
  className?: string
}

const DEFAULT_CONFIG = `# Conduit Configuration
# This file configures the Conduit daemon and its components

# Daemon Settings
daemon:
  listen: "unix://~/.conduit/conduit.sock"
  log_level: "info"
  data_dir: "~/.conduit"

# Knowledge Base Settings
kb:
  # RAG Search defaults
  rag:
    min_score: 0.15
    semantic_weight: 0.5
    mmr_lambda: 0.7
    max_results: 10
    reranking: true

  # KAG Graph Settings
  kag:
    confidence_threshold: 0.6
    max_entities_per_chunk: 20
    max_relations_per_chunk: 30
    background_processing: true
    worker_count: 2

# Sync Settings
sync:
  auto_sync: true
  sync_interval: 60  # minutes
  sync_on_start: true

# Network Settings
network:
  request_timeout: 30  # seconds
  retry_attempts: 3
  connection_pool_size: 10

# Container Settings
containers:
  runtime: "podman"  # or "docker"
  default_memory: "512m"
  default_cpu: "0.5"

# Ollama Settings
ollama:
  host: "http://localhost:11434"
  default_model: "nomic-embed-text"
  chat_model: "qwen2.5-coder:7b"

# Qdrant Settings
qdrant:
  host: "localhost"
  port: 6333
  collection: "conduit_docs"
`

export function ConfigEditor({ className }: ConfigEditorProps): JSX.Element {
  const [content, setContent] = useState(DEFAULT_CONFIG)
  const [originalContent, setOriginalContent] = useState(DEFAULT_CONFIG)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [isDirty, setIsDirty] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [saveSuccess, setSaveSuccess] = useState(false)

  // Load config from daemon
  useEffect(() => {
    loadConfig()
  }, [])

  const loadConfig = async (): Promise<void> => {
    setLoading(true)
    setError(null)
    try {
      // In a real implementation, this would fetch from daemon
      // const response = await window.conduit.getConfig?.()
      // For now, use default config
      setContent(DEFAULT_CONFIG)
      setOriginalContent(DEFAULT_CONFIG)
    } catch (err) {
      setError('Failed to load configuration')
      console.error('Failed to load config:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleEditorChange = useCallback((value: string | undefined) => {
    if (value !== undefined) {
      setContent(value)
      setIsDirty(value !== originalContent)
      setError(null)
      setSaveSuccess(false)
    }
  }, [originalContent])

  const handleSave = async (): Promise<void> => {
    setSaving(true)
    setError(null)
    setSaveSuccess(false)
    try {
      // Validate YAML syntax (basic check)
      // In production, use a YAML parser
      if (content.includes('\t')) {
        throw new Error('YAML files should use spaces, not tabs')
      }

      // Save to daemon
      // await window.conduit.saveConfig?.(content)

      setOriginalContent(content)
      setIsDirty(false)
      setSaveSuccess(true)
      setTimeout(() => setSaveSuccess(false), 3000)
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const handleReset = (): void => {
    setContent(originalContent)
    setIsDirty(false)
    setError(null)
  }

  const handleExport = (): void => {
    const blob = new Blob([content], { type: 'text/yaml' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'conduit.yaml'
    a.click()
    URL.revokeObjectURL(url)
  }

  const handleImport = (): void => {
    const input = document.createElement('input')
    input.type = 'file'
    input.accept = '.yaml,.yml'
    input.onchange = (e) => {
      const file = (e.target as HTMLInputElement).files?.[0]
      if (file) {
        const reader = new FileReader()
        reader.onload = (event) => {
          const text = event.target?.result as string
          setContent(text)
          setIsDirty(text !== originalContent)
        }
        reader.readAsText(file)
      }
    }
    input.click()
  }

  if (loading) {
    return (
      <div className={cn('card', className)}>
        <div className="p-8 flex items-center justify-center">
          <Loader2 className="w-6 h-6 animate-spin text-macos-text-secondary" />
        </div>
      </div>
    )
  }

  return (
    <div className={cn('card overflow-hidden', className)}>
      {/* Header */}
      <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <FileCode className="w-5 h-5 text-macos-orange" />
            <h3 className="font-medium">Configuration Editor</h3>
            {isDirty && (
              <span className="text-xs px-2 py-0.5 rounded-full bg-macos-orange/10 text-macos-orange">
                Unsaved
              </span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <button
              onClick={handleImport}
              className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
              title="Import config"
            >
              <Upload className="w-4 h-4" />
            </button>
            <button
              onClick={handleExport}
              className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary"
              title="Export config"
            >
              <Download className="w-4 h-4" />
            </button>
            <button
              onClick={handleReset}
              disabled={!isDirty}
              className="p-1.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary disabled:opacity-50"
              title="Reset changes"
            >
              <RotateCcw className="w-4 h-4" />
            </button>
            <button
              onClick={handleSave}
              disabled={!isDirty || saving}
              className={cn(
                'flex items-center gap-1.5 px-3 py-1.5 rounded-lg text-sm font-medium transition-colors',
                isDirty && !saving
                  ? 'bg-macos-blue text-white hover:bg-macos-blue/90'
                  : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary text-macos-text-secondary'
              )}
            >
              {saving ? (
                <Loader2 className="w-4 h-4 animate-spin" />
              ) : saveSuccess ? (
                <Check className="w-4 h-4" />
              ) : (
                <Save className="w-4 h-4" />
              )}
              {saving ? 'Saving...' : saveSuccess ? 'Saved!' : 'Save'}
            </button>
          </div>
        </div>
      </div>

      {/* Error Banner */}
      {error && (
        <div className="px-4 py-2 bg-macos-red/10 border-b border-macos-red/20 flex items-center gap-2 text-macos-red">
          <AlertTriangle className="w-4 h-4" />
          <span className="text-sm">{error}</span>
        </div>
      )}

      {/* Editor */}
      <div className="h-[500px]">
        <Editor
          height="100%"
          language="yaml"
          theme="vs-dark"
          value={content}
          onChange={handleEditorChange}
          options={{
            minimap: { enabled: false },
            fontSize: 13,
            fontFamily: 'SF Mono, Monaco, Menlo, monospace',
            lineNumbers: 'on',
            scrollBeyondLastLine: false,
            wordWrap: 'on',
            tabSize: 2,
            insertSpaces: true,
            automaticLayout: true,
            padding: { top: 16, bottom: 16 }
          }}
        />
      </div>

      {/* Footer */}
      <div className="px-4 py-2 border-t border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30 text-xs text-macos-text-tertiary">
        <div className="flex items-center justify-between">
          <span>YAML Configuration</span>
          <span>~/.conduit/conduit.yaml</span>
        </div>
      </div>
    </div>
  )
}
