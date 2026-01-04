import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { Settings2, RotateCcw, Info } from 'lucide-react'

export interface RAGSettings {
  minScore: number
  semanticWeight: number
  mmrLambda: number
  maxResults: number
  reranking: boolean
  searchMode: 'hybrid' | 'semantic' | 'fts5'
}

const DEFAULT_SETTINGS: RAGSettings = {
  minScore: 0.15,
  semanticWeight: 0.5,
  mmrLambda: 0.7,
  maxResults: 10,
  reranking: true,
  searchMode: 'hybrid'
}

interface RAGTuningPanelProps {
  settings?: RAGSettings
  onChange?: (settings: RAGSettings) => void
  className?: string
}

interface SliderProps {
  label: string
  description: string
  value: number
  min: number
  max: number
  step: number
  onChange: (value: number) => void
}

function Slider({ label, description, value, min, max, step, onChange }: SliderProps): JSX.Element {
  const percentage = ((value - min) / (max - min)) * 100

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5">
          <span className="text-sm font-medium">{label}</span>
          <button className="p-0.5 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary group">
            <Info className="w-3.5 h-3.5 text-macos-text-tertiary group-hover:text-macos-text-secondary" />
          </button>
        </div>
        <span className="text-sm font-mono text-macos-blue">{value.toFixed(2)}</span>
      </div>
      <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
        {description}
      </p>
      <div className="relative">
        <input
          type="range"
          min={min}
          max={max}
          step={step}
          value={value}
          onChange={(e) => onChange(parseFloat(e.target.value))}
          className="w-full h-1.5 rounded-full appearance-none cursor-pointer bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary"
          style={{
            background: `linear-gradient(to right, rgb(0, 122, 255) 0%, rgb(0, 122, 255) ${percentage}%, rgb(200, 200, 200) ${percentage}%, rgb(200, 200, 200) 100%)`
          }}
        />
      </div>
    </div>
  )
}

export function RAGTuningPanel({
  settings: externalSettings,
  onChange,
  className
}: RAGTuningPanelProps): JSX.Element {
  const [settings, setSettings] = useState<RAGSettings>(externalSettings || DEFAULT_SETTINGS)
  const [isDirty, setIsDirty] = useState(false)

  useEffect(() => {
    if (externalSettings) {
      setSettings(externalSettings)
    }
  }, [externalSettings])

  const updateSetting = <K extends keyof RAGSettings>(key: K, value: RAGSettings[K]): void => {
    const newSettings = { ...settings, [key]: value }
    setSettings(newSettings)
    setIsDirty(true)
    onChange?.(newSettings)
  }

  const resetToDefaults = (): void => {
    setSettings(DEFAULT_SETTINGS)
    setIsDirty(true)
    onChange?.(DEFAULT_SETTINGS)
  }

  return (
    <div className={cn('card', className)}>
      <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Settings2 className="w-5 h-5 text-macos-purple" />
            <h3 className="font-medium">RAG Search Settings</h3>
          </div>
          <button
            onClick={resetToDefaults}
            className="flex items-center gap-1 text-xs text-macos-text-secondary hover:text-macos-blue"
          >
            <RotateCcw className="w-3.5 h-3.5" />
            Reset to Defaults
          </button>
        </div>
      </div>

      <div className="p-4 space-y-6">
        {/* Search Quality Section */}
        <div className="space-y-4">
          <h4 className="text-xs font-medium text-macos-text-secondary dark:text-macos-text-dark-secondary uppercase tracking-wide">
            Search Quality
          </h4>

          <Slider
            label="Min Score Threshold"
            description="Lower = more results, higher = stricter relevance"
            value={settings.minScore}
            min={0}
            max={1}
            step={0.05}
            onChange={(v) => updateSetting('minScore', v)}
          />

          <Slider
            label="Semantic Weight"
            description="Balance between semantic (meaning) and lexical (keywords)"
            value={settings.semanticWeight}
            min={0}
            max={1}
            step={0.1}
            onChange={(v) => updateSetting('semanticWeight', v)}
          />

          <Slider
            label="MMR Lambda (Diversity)"
            description="Higher = more relevant, lower = more diverse results"
            value={settings.mmrLambda}
            min={0}
            max={1}
            step={0.1}
            onChange={(v) => updateSetting('mmrLambda', v)}
          />
        </div>

        {/* Performance Section */}
        <div className="space-y-4">
          <h4 className="text-xs font-medium text-macos-text-secondary dark:text-macos-text-dark-secondary uppercase tracking-wide">
            Performance
          </h4>

          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">Max Results</span>
              <span className="text-sm font-mono text-macos-blue">{settings.maxResults}</span>
            </div>
            <input
              type="range"
              min={5}
              max={50}
              step={5}
              value={settings.maxResults}
              onChange={(e) => updateSetting('maxResults', parseInt(e.target.value))}
              className="w-full h-1.5 rounded-full appearance-none cursor-pointer bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary"
              style={{
                background: `linear-gradient(to right, rgb(0, 122, 255) 0%, rgb(0, 122, 255) ${((settings.maxResults - 5) / 45) * 100}%, rgb(200, 200, 200) ${((settings.maxResults - 5) / 45) * 100}%, rgb(200, 200, 200) 100%)`
              }}
            />
          </div>

          <div className="flex items-center justify-between">
            <div>
              <span className="text-sm font-medium">Enable Reranking</span>
              <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
                Improves result quality with additional scoring pass
              </p>
            </div>
            <button
              onClick={() => updateSetting('reranking', !settings.reranking)}
              className={cn(
                'relative w-10 h-6 rounded-full transition-colors',
                settings.reranking ? 'bg-macos-green' : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary'
              )}
            >
              <span
                className={cn(
                  'absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform',
                  settings.reranking ? 'translate-x-4' : 'translate-x-0.5'
                )}
              />
            </button>
          </div>
        </div>

        {/* Search Mode Section */}
        <div className="space-y-3">
          <h4 className="text-xs font-medium text-macos-text-secondary dark:text-macos-text-dark-secondary uppercase tracking-wide">
            Search Mode
          </h4>

          <div className="flex gap-2">
            {(['hybrid', 'semantic', 'fts5'] as const).map((mode) => (
              <button
                key={mode}
                onClick={() => updateSetting('searchMode', mode)}
                className={cn(
                  'flex-1 py-2 px-3 rounded-lg text-sm font-medium transition-colors',
                  settings.searchMode === mode
                    ? 'bg-macos-blue text-white'
                    : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary hover:bg-macos-bg-tertiary'
                )}
              >
                {mode === 'fts5' ? 'FTS5' : mode.charAt(0).toUpperCase() + mode.slice(1)}
              </button>
            ))}
          </div>
          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
            {settings.searchMode === 'hybrid' && 'Combines semantic understanding with keyword matching (recommended)'}
            {settings.searchMode === 'semantic' && 'Uses vector similarity for meaning-based search'}
            {settings.searchMode === 'fts5' && 'Fast full-text search using SQLite FTS5'}
          </p>
        </div>
      </div>

      {isDirty && (
        <div className="px-4 py-3 bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary border-t border-macos-separator dark:border-macos-separator-dark">
          <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
            Changes are applied immediately to new searches
          </p>
        </div>
      )}
    </div>
  )
}
