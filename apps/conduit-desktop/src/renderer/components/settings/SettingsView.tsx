import { cn } from '@/lib/utils'
import { useSettingsStore, AppMode } from '@/stores'
import {
  Monitor,
  Moon,
  Sun,
  Gauge,
  Wrench,
  Code,
  ChevronRight
} from 'lucide-react'

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

export function SettingsView(): JSX.Element {
  const { mode, theme, setMode, setTheme } = useSettingsStore()

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
