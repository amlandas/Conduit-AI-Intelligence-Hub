import { useState } from 'react'
import { cn } from '@/lib/utils'
import {
  Network,
  Search,
  Loader2,
  Circle,
  ArrowRight,
  RotateCcw,
  Settings2,
  AlertTriangle
} from 'lucide-react'

interface Entity {
  id: string
  name: string
  type: string
  confidence: number
  properties?: Record<string, string>
}

interface Relation {
  id: string
  source: string
  target: string
  type: string
  confidence: number
}

interface KAGSearchResult {
  entities: Entity[]
  relations: Relation[]
  query: string
}

export interface KAGSettings {
  confidenceThreshold: number
  maxEntitiesPerChunk: number
  maxRelationsPerChunk: number
  maxGraphHops: number
  maxEntitiesReturned: number
  backgroundProcessing: boolean
  workerCount: number
}

const DEFAULT_KAG_SETTINGS: KAGSettings = {
  confidenceThreshold: 0.6,
  maxEntitiesPerChunk: 20,
  maxRelationsPerChunk: 30,
  maxGraphHops: 2,
  maxEntitiesReturned: 20,
  backgroundProcessing: true,
  workerCount: 2
}

interface KAGPanelProps {
  className?: string
  showSettings?: boolean
}

const TYPE_COLORS: Record<string, string> = {
  PERSON: 'bg-macos-blue',
  ORGANIZATION: 'bg-macos-purple',
  LOCATION: 'bg-macos-green',
  CONCEPT: 'bg-macos-orange',
  TECHNOLOGY: 'bg-macos-pink',
  EVENT: 'bg-macos-yellow',
  DEFAULT: 'bg-macos-gray'
}

function EntityNode({ entity }: { entity: Entity }): JSX.Element {
  const color = TYPE_COLORS[entity.type.toUpperCase()] || TYPE_COLORS.DEFAULT

  return (
    <div className="flex items-center gap-2 p-2 rounded-lg bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50">
      <Circle className={cn('w-3 h-3 flex-shrink-0', color.replace('bg-', 'text-'))} fill="currentColor" />
      <div className="flex-1 min-w-0">
        <p className="text-sm font-medium truncate">{entity.name}</p>
        <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary dark:text-macos-text-dark-tertiary">
          {entity.type} · {(entity.confidence * 100).toFixed(0)}%
        </p>
      </div>
    </div>
  )
}

function RelationEdge({ relation, entities }: { relation: Relation; entities: Entity[] }): JSX.Element {
  const source = entities.find((e) => e.id === relation.source)
  const target = entities.find((e) => e.id === relation.target)

  return (
    <div className="flex items-center gap-2 text-sm">
      <span className="font-medium truncate max-w-[120px]">{source?.name || relation.source}</span>
      <div className="flex items-center gap-1 text-macos-text-secondary">
        <ArrowRight className="w-3 h-3" />
        <span className="text-xs italic">{relation.type}</span>
        <ArrowRight className="w-3 h-3" />
      </div>
      <span className="font-medium truncate max-w-[120px]">{target?.name || relation.target}</span>
    </div>
  )
}

export function KAGPanel({ className, showSettings = false }: KAGPanelProps): JSX.Element {
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<KAGSearchResult | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [settings, setSettings] = useState<KAGSettings>(DEFAULT_KAG_SETTINGS)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [activeTab, setActiveTab] = useState<'entities' | 'relations'>('entities')

  const handleSearch = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()
    if (!query.trim()) return

    setLoading(true)
    setError(null)
    setResult(null)
    try {
      // Call KAG search API via IPC - delegates to CLI: conduit kb kag-query
      const response = await window.conduit.searchKAG(query, {
        maxHops: settings.maxGraphHops,
        maxEntities: settings.maxEntitiesReturned,
        minConfidence: settings.confidenceThreshold
      })
      if (response) {
        // Check if CLI returned an error response
        if (typeof response === 'object' && 'error' in response) {
          setError((response as { error: string }).error)
        } else {
          setResult(response as KAGSearchResult)
        }
      } else {
        setError('No response from KAG search')
      }
    } catch (err) {
      console.error('KAG search failed:', err)
      const errorMessage = (err as Error).message || 'KAG search failed'
      // Provide user-friendly error messages for common failures
      if (errorMessage.includes('FalkorDB') || errorMessage.includes('connection')) {
        setError('Knowledge graph is unavailable. Please ensure FalkorDB is running.')
      } else if (errorMessage.includes('timeout')) {
        setError('Search timed out. Try a simpler query or check service status.')
      } else {
        setError(errorMessage)
      }
    } finally {
      setLoading(false)
    }
  }

  const updateSetting = <K extends keyof KAGSettings>(key: K, value: KAGSettings[K]): void => {
    setSettings((prev) => ({ ...prev, [key]: value }))
  }

  return (
    <div className={cn('card', className)}>
      <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <Network className="w-5 h-5 text-macos-green" />
            <h3 className="font-medium">Knowledge Graph (KAG)</h3>
          </div>
          {showSettings && (
            <button
              onClick={() => setSettingsOpen(!settingsOpen)}
              className={cn(
                'p-1.5 rounded-lg transition-colors',
                settingsOpen
                  ? 'bg-macos-blue/10 text-macos-blue'
                  : 'hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary'
              )}
            >
              <Settings2 className="w-4 h-4" />
            </button>
          )}
        </div>
      </div>

      {/* Settings Panel (collapsible) */}
      {settingsOpen && (
        <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30">
          <div className="space-y-4">
            <h4 className="text-xs font-medium text-macos-text-secondary uppercase tracking-wide">
              KAG Settings
            </h4>

            {/* Confidence Threshold */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm">Confidence Threshold</span>
                <span className="text-sm font-mono text-macos-green">
                  {settings.confidenceThreshold.toFixed(2)}
                </span>
              </div>
              <input
                type="range"
                min={0}
                max={1}
                step={0.05}
                value={settings.confidenceThreshold}
                onChange={(e) => updateSetting('confidenceThreshold', parseFloat(e.target.value))}
                className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                style={{
                  background: `linear-gradient(to right, rgb(52, 199, 89) 0%, rgb(52, 199, 89) ${settings.confidenceThreshold * 100}%, rgb(200, 200, 200) ${settings.confidenceThreshold * 100}%, rgb(200, 200, 200) 100%)`
                }}
              />
            </div>

            {/* Max Graph Hops */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm">Max Graph Hops</span>
                <span className="text-sm font-mono text-macos-green">{settings.maxGraphHops}</span>
              </div>
              <input
                type="range"
                min={1}
                max={3}
                step={1}
                value={settings.maxGraphHops}
                onChange={(e) => updateSetting('maxGraphHops', parseInt(e.target.value))}
                className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                style={{
                  background: `linear-gradient(to right, rgb(52, 199, 89) 0%, rgb(52, 199, 89) ${((settings.maxGraphHops - 1) / 2) * 100}%, rgb(200, 200, 200) ${((settings.maxGraphHops - 1) / 2) * 100}%, rgb(200, 200, 200) 100%)`
                }}
              />
            </div>

            {/* Max Entities Returned */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <span className="text-sm">Max Entities</span>
                <span className="text-sm font-mono text-macos-green">{settings.maxEntitiesReturned}</span>
              </div>
              <input
                type="range"
                min={10}
                max={100}
                step={10}
                value={settings.maxEntitiesReturned}
                onChange={(e) => updateSetting('maxEntitiesReturned', parseInt(e.target.value))}
                className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                style={{
                  background: `linear-gradient(to right, rgb(52, 199, 89) 0%, rgb(52, 199, 89) ${((settings.maxEntitiesReturned - 10) / 90) * 100}%, rgb(200, 200, 200) ${((settings.maxEntitiesReturned - 10) / 90) * 100}%, rgb(200, 200, 200) 100%)`
                }}
              />
            </div>

            <button
              onClick={() => setSettings(DEFAULT_KAG_SETTINGS)}
              className="flex items-center gap-1 text-xs text-macos-text-secondary hover:text-macos-green"
            >
              <RotateCcw className="w-3.5 h-3.5" />
              Reset to Defaults
            </button>
          </div>
        </div>
      )}

      {/* Search Input */}
      <div className="p-4">
        <form onSubmit={handleSearch} className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-macos-text-secondary" />
          <input
            type="text"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search entities and relations..."
            className="w-full pl-9 pr-4 py-2 rounded-lg border border-macos-separator dark:border-macos-separator-dark bg-white dark:bg-macos-bg-dark-secondary focus:outline-none focus:ring-2 focus:ring-macos-green text-sm"
          />
          {loading && (
            <Loader2 className="absolute right-3 top-1/2 -translate-y-1/2 w-4 h-4 animate-spin text-macos-green" />
          )}
        </form>
      </div>

      {/* Error Display */}
      {error && (
        <div className="mx-4 mb-4 p-3 rounded-lg bg-macos-red/10 border border-macos-red/20">
          <div className="flex items-start gap-2">
            <AlertTriangle className="w-4 h-4 text-macos-red flex-shrink-0 mt-0.5" />
            <div className="flex-1">
              <p className="text-sm font-medium text-macos-red">Search Failed</p>
              <p className="text-xs text-macos-red/80 mt-0.5">{error}</p>
            </div>
            <button
              onClick={() => setError(null)}
              className="text-macos-red/60 hover:text-macos-red text-xs"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {/* Results */}
      {result && (
        <div className="border-t border-macos-separator dark:border-macos-separator-dark">
          {/* Tabs */}
          <div className="flex border-b border-macos-separator dark:border-macos-separator-dark">
            <button
              onClick={() => setActiveTab('entities')}
              className={cn(
                'flex-1 py-2 text-sm font-medium transition-colors',
                activeTab === 'entities'
                  ? 'text-macos-green border-b-2 border-macos-green'
                  : 'text-macos-text-secondary hover:text-macos-text-primary'
              )}
            >
              Entities ({result.entities.length})
            </button>
            <button
              onClick={() => setActiveTab('relations')}
              className={cn(
                'flex-1 py-2 text-sm font-medium transition-colors',
                activeTab === 'relations'
                  ? 'text-macos-green border-b-2 border-macos-green'
                  : 'text-macos-text-secondary hover:text-macos-text-primary'
              )}
            >
              Relations ({result.relations.length})
            </button>
          </div>

          {/* Tab Content */}
          <div className="p-4 max-h-[300px] overflow-auto">
            {activeTab === 'entities' ? (
              <div className="space-y-2">
                {result.entities.length === 0 ? (
                  <p className="text-sm text-macos-text-secondary text-center py-4">
                    No entities found
                  </p>
                ) : (
                  result.entities.map((entity) => (
                    <EntityNode key={entity.id} entity={entity} />
                  ))
                )}
              </div>
            ) : (
              <div className="space-y-3">
                {result.relations.length === 0 ? (
                  <p className="text-sm text-macos-text-secondary text-center py-4">
                    No relations found
                  </p>
                ) : (
                  result.relations.map((relation) => (
                    <RelationEdge
                      key={relation.id}
                      relation={relation}
                      entities={result.entities}
                    />
                  ))
                )}
              </div>
            )}
          </div>

          {/* Entity Type Legend */}
          {activeTab === 'entities' && result.entities.length > 0 && (
            <div className="px-4 py-3 border-t border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30">
              <div className="flex flex-wrap gap-3 text-xs">
                {Object.entries(TYPE_COLORS)
                  .filter(([type]) => type !== 'DEFAULT')
                  .map(([type, color]) => (
                    <div key={type} className="flex items-center gap-1">
                      <Circle
                        className={cn('w-2.5 h-2.5', color.replace('bg-', 'text-'))}
                        fill="currentColor"
                      />
                      <span className="text-macos-text-secondary">{type}</span>
                    </div>
                  ))}
              </div>
            </div>
          )}
        </div>
      )}

      {/* Empty State */}
      {!result && !loading && !error && (
        <div className="px-4 pb-4">
          <div className="text-center py-6 text-macos-text-secondary dark:text-macos-text-dark-secondary">
            <Network className="w-10 h-10 mx-auto mb-2 opacity-50" />
            <p className="text-sm">Search for entities and explore relationships</p>
            <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary mt-1">
              Try searching for concepts, people, or technologies
            </p>
          </div>
        </div>
      )}
    </div>
  )
}
