import { useState, lazy, Suspense } from 'react'
import { cn } from '@/lib/utils'
import { useSettingsStore, AppMode } from '@/stores'
import { RAGTuningPanel, RAGSettings } from '../kb/RAGTuningPanel'
import { DaemonControls } from './DaemonControls'
import { LogViewer } from './LogViewer'
import {
  Monitor,
  Moon,
  Sun,
  Gauge,
  Wrench,
  Code,
  ChevronRight,
  Database,
  Network,
  Clock,
  Wifi,
  RefreshCw,
  RotateCcw,
  FileCode,
  Server,
  ScrollText,
  Loader2
} from 'lucide-react'

// Lazy load Monaco editor for better initial load performance
const ConfigEditor = lazy(() => import('./ConfigEditor').then(m => ({ default: m.ConfigEditor })))

interface ModeOptionProps {
  mode: AppMode
  currentMode: AppMode
  title: string
  description: string
  icon: typeof Gauge
  onSelect: (mode: AppMode) => void
}

function ModeOption({ mode, currentMode, title, description, icon: Icon, onSelect }: ModeOptionProps): JSX.Element {
  const isActive = mode === currentMode

  return (
    <button
      onClick={() => onSelect(mode)}
      className={cn(
        'w-full text-left p-4 rounded-lg border-2 transition-colors',
        isActive
          ? 'border-macos-blue bg-macos-blue/5'
          : 'border-macos-separator dark:border-macos-separator-dark hover:border-macos-blue/50'
      )}
    >
      <div className="flex items-start gap-3">
        <div
          className={cn(
            'p-2 rounded-lg',
            isActive
              ? 'bg-macos-blue text-white'
              : 'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary'
          )}
        >
          <Icon className="w-5 h-5" />
        </div>
        <div className="flex-1">
          <h3 className="font-medium">{title}</h3>
          <p className="mt-0.5 text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
            {description}
          </p>
        </div>
        {isActive && (
          <div className="text-macos-blue">
            <ChevronRight className="w-5 h-5" />
          </div>
        )}
      </div>
    </button>
  )
}

interface SyncPolicy {
  autoSync: boolean
  syncInterval: number // minutes
  syncOnStart: boolean
}

interface NetworkConfig {
  requestTimeout: number // seconds
  retryAttempts: number
  connectionPoolSize: number
}

const DEFAULT_SYNC_POLICY: SyncPolicy = {
  autoSync: true,
  syncInterval: 60,
  syncOnStart: true
}

const DEFAULT_NETWORK_CONFIG: NetworkConfig = {
  requestTimeout: 30,
  retryAttempts: 3,
  connectionPoolSize: 10
}

export function SettingsView(): JSX.Element {
  const { mode, theme, setMode, setTheme, isFeatureVisible } = useSettingsStore()
  const [ragSettings, setRagSettings] = useState<RAGSettings | undefined>(undefined)
  const [syncPolicy, setSyncPolicy] = useState<SyncPolicy>(DEFAULT_SYNC_POLICY)
  const [networkConfig, setNetworkConfig] = useState<NetworkConfig>(DEFAULT_NETWORK_CONFIG)

  const handleRAGChange = (settings: RAGSettings): void => {
    setRagSettings(settings)
    // In a real app, persist to daemon config
    console.log('RAG settings changed:', settings)
  }

  return (
    <div className="space-y-8 animate-fade-in max-w-2xl">
      <h1 className="text-2xl font-semibold">Settings</h1>

      {/* Appearance */}
      <section>
        <h2 className="text-lg font-medium mb-4">Appearance</h2>
        <div className="card p-4">
          <h3 className="text-sm font-medium mb-3">Theme</h3>
          <div className="flex gap-2">
            <button
              onClick={() => setTheme('system')}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2 rounded-lg border-2 transition-colors',
                theme === 'system'
                  ? 'border-macos-blue bg-macos-blue/5'
                  : 'border-macos-separator dark:border-macos-separator-dark'
              )}
            >
              <Monitor className="w-4 h-4" />
              <span className="text-sm">System</span>
            </button>
            <button
              onClick={() => setTheme('light')}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2 rounded-lg border-2 transition-colors',
                theme === 'light'
                  ? 'border-macos-blue bg-macos-blue/5'
                  : 'border-macos-separator dark:border-macos-separator-dark'
              )}
            >
              <Sun className="w-4 h-4" />
              <span className="text-sm">Light</span>
            </button>
            <button
              onClick={() => setTheme('dark')}
              className={cn(
                'flex-1 flex items-center justify-center gap-2 py-2 rounded-lg border-2 transition-colors',
                theme === 'dark'
                  ? 'border-macos-blue bg-macos-blue/5'
                  : 'border-macos-separator dark:border-macos-separator-dark'
              )}
            >
              <Moon className="w-4 h-4" />
              <span className="text-sm">Dark</span>
            </button>
          </div>
        </div>
      </section>

      {/* Mode Selection */}
      <section>
        <h2 className="text-lg font-medium mb-4">Interface Mode</h2>
        <div className="space-y-3">
          <ModeOption
            mode="default"
            currentMode={mode}
            title="Default"
            description="Clean interface with essential features. System handles optimization automatically."
            icon={Gauge}
            onSelect={setMode}
          />
          <ModeOption
            mode="advanced"
            currentMode={mode}
            title="Advanced"
            description="Fine-tune RAG/KAG settings, manage containers, and access detailed stats."
            icon={Wrench}
            onSelect={setMode}
          />
          <ModeOption
            mode="developer"
            currentMode={mode}
            title="Developer"
            description="Full access to config files, logs, raw JSON, and API controls."
            icon={Code}
            onSelect={setMode}
          />
        </div>
      </section>

      {/* RAG Default Settings - Advanced Mode */}
      {isFeatureVisible('showRAGTuning') && (
        <section>
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
            <Database className="w-5 h-5 text-macos-purple" />
            RAG Default Settings
          </h2>
          <RAGTuningPanel settings={ragSettings} onChange={handleRAGChange} />
        </section>
      )}

      {/* Sync Policies - Advanced Mode */}
      {isFeatureVisible('showSyncPolicies') && (
        <section>
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
            <RefreshCw className="w-5 h-5 text-macos-green" />
            Sync Policies
          </h2>
          <div className="card p-4 space-y-4">
            {/* Auto Sync Toggle */}
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium">Auto-Sync Sources</h3>
                <p className="text-xs text-macos-text-secondary">
                  Automatically sync KB sources on a schedule
                </p>
              </div>
              <button
                onClick={() => setSyncPolicy(prev => ({ ...prev, autoSync: !prev.autoSync }))}
                className={cn(
                  'relative w-10 h-6 rounded-full transition-colors',
                  syncPolicy.autoSync ? 'bg-macos-green' : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary'
                )}
              >
                <span
                  className={cn(
                    'absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform',
                    syncPolicy.autoSync ? 'translate-x-4' : 'translate-x-0.5'
                  )}
                />
              </button>
            </div>

            {/* Sync Interval */}
            {syncPolicy.autoSync && (
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-sm">Sync Interval</span>
                  <span className="text-sm font-mono text-macos-green">{syncPolicy.syncInterval} min</span>
                </div>
                <input
                  type="range"
                  min={15}
                  max={240}
                  step={15}
                  value={syncPolicy.syncInterval}
                  onChange={(e) => setSyncPolicy(prev => ({ ...prev, syncInterval: parseInt(e.target.value) }))}
                  className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                  style={{
                    background: `linear-gradient(to right, rgb(52, 199, 89) 0%, rgb(52, 199, 89) ${((syncPolicy.syncInterval - 15) / 225) * 100}%, rgb(200, 200, 200) ${((syncPolicy.syncInterval - 15) / 225) * 100}%, rgb(200, 200, 200) 100%)`
                  }}
                />
                <div className="flex justify-between text-xs text-macos-text-tertiary">
                  <span>15 min</span>
                  <span>4 hours</span>
                </div>
              </div>
            )}

            {/* Sync on Start Toggle */}
            <div className="flex items-center justify-between">
              <div>
                <h3 className="text-sm font-medium">Sync on Startup</h3>
                <p className="text-xs text-macos-text-secondary">
                  Sync all sources when Conduit starts
                </p>
              </div>
              <button
                onClick={() => setSyncPolicy(prev => ({ ...prev, syncOnStart: !prev.syncOnStart }))}
                className={cn(
                  'relative w-10 h-6 rounded-full transition-colors',
                  syncPolicy.syncOnStart ? 'bg-macos-green' : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary'
                )}
              >
                <span
                  className={cn(
                    'absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform',
                    syncPolicy.syncOnStart ? 'translate-x-4' : 'translate-x-0.5'
                  )}
                />
              </button>
            </div>

            <button
              onClick={() => setSyncPolicy(DEFAULT_SYNC_POLICY)}
              className="flex items-center gap-1 text-xs text-macos-text-secondary hover:text-macos-green"
            >
              <RotateCcw className="w-3.5 h-3.5" />
              Reset to Defaults
            </button>
          </div>
        </section>
      )}

      {/* Network Config - Advanced Mode */}
      {isFeatureVisible('showSyncPolicies') && (
        <section>
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
            <Wifi className="w-5 h-5 text-macos-blue" />
            Network Configuration
          </h2>
          <div className="card p-4 space-y-4">
            {/* Request Timeout */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Clock className="w-4 h-4 text-macos-text-secondary" />
                  <span className="text-sm">Request Timeout</span>
                </div>
                <span className="text-sm font-mono text-macos-blue">{networkConfig.requestTimeout}s</span>
              </div>
              <input
                type="range"
                min={10}
                max={120}
                step={10}
                value={networkConfig.requestTimeout}
                onChange={(e) => setNetworkConfig(prev => ({ ...prev, requestTimeout: parseInt(e.target.value) }))}
                className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                style={{
                  background: `linear-gradient(to right, rgb(0, 122, 255) 0%, rgb(0, 122, 255) ${((networkConfig.requestTimeout - 10) / 110) * 100}%, rgb(200, 200, 200) ${((networkConfig.requestTimeout - 10) / 110) * 100}%, rgb(200, 200, 200) 100%)`
                }}
              />
            </div>

            {/* Retry Attempts */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <RefreshCw className="w-4 h-4 text-macos-text-secondary" />
                  <span className="text-sm">Retry Attempts</span>
                </div>
                <span className="text-sm font-mono text-macos-blue">{networkConfig.retryAttempts}</span>
              </div>
              <input
                type="range"
                min={0}
                max={5}
                step={1}
                value={networkConfig.retryAttempts}
                onChange={(e) => setNetworkConfig(prev => ({ ...prev, retryAttempts: parseInt(e.target.value) }))}
                className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                style={{
                  background: `linear-gradient(to right, rgb(0, 122, 255) 0%, rgb(0, 122, 255) ${(networkConfig.retryAttempts / 5) * 100}%, rgb(200, 200, 200) ${(networkConfig.retryAttempts / 5) * 100}%, rgb(200, 200, 200) 100%)`
                }}
              />
            </div>

            {/* Connection Pool */}
            <div className="space-y-2">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Network className="w-4 h-4 text-macos-text-secondary" />
                  <span className="text-sm">Connection Pool Size</span>
                </div>
                <span className="text-sm font-mono text-macos-blue">{networkConfig.connectionPoolSize}</span>
              </div>
              <input
                type="range"
                min={5}
                max={50}
                step={5}
                value={networkConfig.connectionPoolSize}
                onChange={(e) => setNetworkConfig(prev => ({ ...prev, connectionPoolSize: parseInt(e.target.value) }))}
                className="w-full h-1.5 rounded-full appearance-none cursor-pointer"
                style={{
                  background: `linear-gradient(to right, rgb(0, 122, 255) 0%, rgb(0, 122, 255) ${((networkConfig.connectionPoolSize - 5) / 45) * 100}%, rgb(200, 200, 200) ${((networkConfig.connectionPoolSize - 5) / 45) * 100}%, rgb(200, 200, 200) 100%)`
                }}
              />
            </div>

            <button
              onClick={() => setNetworkConfig(DEFAULT_NETWORK_CONFIG)}
              className="flex items-center gap-1 text-xs text-macos-text-secondary hover:text-macos-blue"
            >
              <RotateCcw className="w-3.5 h-3.5" />
              Reset to Defaults
            </button>
          </div>
        </section>
      )}

      {/* Developer Mode: Daemon Controls */}
      {isFeatureVisible('showDaemonControls') && (
        <section>
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
            <Server className="w-5 h-5 text-macos-blue" />
            Daemon Service
          </h2>
          <DaemonControls />
        </section>
      )}

      {/* Developer Mode: Config Editor */}
      {isFeatureVisible('showConfigEditor') && (
        <section>
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
            <FileCode className="w-5 h-5 text-macos-orange" />
            Configuration
          </h2>
          <Suspense
            fallback={
              <div className="card p-8 flex items-center justify-center">
                <Loader2 className="w-6 h-6 animate-spin text-macos-text-secondary" />
              </div>
            }
          >
            <ConfigEditor />
          </Suspense>
        </section>
      )}

      {/* Developer Mode: Log Viewer */}
      {isFeatureVisible('showLogViewer') && (
        <section>
          <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
            <ScrollText className="w-5 h-5 text-macos-purple" />
            Daemon Logs
          </h2>
          <LogViewer />
        </section>
      )}

      {/* About */}
      <section>
        <h2 className="text-lg font-medium mb-4">About</h2>
        <div className="card p-4">
          <div className="flex items-center gap-4">
            <div className="w-16 h-16 rounded-xl bg-gradient-to-br from-macos-blue to-macos-purple flex items-center justify-center text-white text-2xl font-bold">
              C
            </div>
            <div>
              <h3 className="font-semibold text-lg">Conduit Desktop</h3>
              <p className="text-sm text-macos-text-secondary dark:text-macos-text-dark-secondary">
                Version 0.1.0
              </p>
              <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary mt-1">
                AI Intelligence Hub for macOS
              </p>
            </div>
          </div>
        </div>
      </section>
    </div>
  )
}
