import { useEffect, useState, lazy, Suspense, useMemo, useCallback } from 'react'
import { NavigationShell } from './components/layout/NavigationShell'
import { useDaemonStore, useInstancesStore, useKBStore, useSettingsStore, useSetupStore } from './stores'
import { UpdateBanner } from './components/ui/UpdateBanner'
import { SetupWizard } from './components/setup/SetupWizard'
import { Loader2 } from 'lucide-react'

// Lazy load views for better initial load performance
const DashboardView = lazy(() => import('./components/dashboard/DashboardView').then(m => ({ default: m.DashboardView })))
const KBView = lazy(() => import('./components/kb/KBView').then(m => ({ default: m.KBView })))
const ConnectorsView = lazy(() => import('./components/connectors/ConnectorsView').then(m => ({ default: m.ConnectorsView })))
const SettingsView = lazy(() => import('./components/settings/SettingsView').then(m => ({ default: m.SettingsView })))

// Loading fallback component
function ViewLoader(): JSX.Element {
  return (
    <div className="h-full flex items-center justify-center">
      <Loader2 className="w-8 h-8 animate-spin text-macos-blue" />
    </div>
  )
}

// Startup loading component
function StartupLoader(): JSX.Element {
  return (
    <div className="h-screen flex flex-col items-center justify-center bg-macos-bg-primary dark:bg-macos-bg-primary-dark">
      <Loader2 className="w-12 h-12 animate-spin text-macos-blue mb-4" />
      <p className="text-macos-text-secondary dark:text-macos-text-secondary-dark">
        Checking Conduit installation...
      </p>
    </div>
  )
}

type Route = '/' | '/kb' | '/connectors' | '/settings'

export default function App(): JSX.Element {
  const [route, setRoute] = useState<Route>('/')
  const [searchOpen, setSearchOpen] = useState(false)
  const [startupCheckDone, setStartupCheckDone] = useState(false) // Track if initial CLI check is done
  const { theme } = useSettingsStore()
  const { setupCompleted, cliInstalled, resetSetup, setCLIInstalled, completeSetup } = useSetupStore()
  const { setSSEConnected, handleSSEEvent, refresh: refreshDaemon } = useDaemonStore()
  const { updateInstance, addInstance, removeInstance, refresh: refreshInstances } = useInstancesStore()
  const { addSource, removeSource, updateSource, setSyncing, refresh: refreshKB } = useKBStore()

  // ═══════════════════════════════════════════════════════════════
  // CRITICAL: Verify CLI exists on startup - this is the FIRST check
  // GUI cannot function without CLI. Setup state is derived from CLI.
  // ═══════════════════════════════════════════════════════════════
  useEffect(() => {
    const verifyCLI = async (): Promise<void> => {
      console.log('[App] Starting CLI verification...')
      try {
        // Check if CLI binary exists and is executable
        // Uses existing setup:check-cli IPC handler
        const result = await window.conduit.checkCLI()
        console.log('[App] CLI check result:', result)

        if (result.installed && result.version) {
          // CLI exists - update store and mark setup as complete
          // Setup completion is DERIVED from CLI existence, not persisted
          console.log('[App] CLI found, setting cliInstalled=true, calling completeSetup')
          setCLIInstalled(true, result.version, result.path || undefined)
          completeSetup()  // CLI installed = setup complete
        } else {
          // CLI NOT installed - show wizard to install it
          console.warn('[App] CLI not found, showing setup wizard')
          resetSetup()
        }
      } catch (error) {
        // Error checking CLI - assume not installed, show wizard
        console.error('[App] Failed to verify CLI:', error)
        resetSetup()
      } finally {
        console.log('[App] Setting startupCheckDone=true')
        setStartupCheckDone(true)
      }
    }

    verifyCLI()
  }, []) // Only run once on mount

  // Apply theme with system preference detection and live updates
  useEffect(() => {
    const root = document.documentElement
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')

    const applyTheme = (): void => {
      if (theme === 'system') {
        root.classList.toggle('dark', mediaQuery.matches)
      } else {
        root.classList.toggle('dark', theme === 'dark')
      }
    }

    // Apply immediately
    applyTheme()

    // Listen for system theme changes when using 'system' theme
    const handleChange = (): void => {
      if (theme === 'system') {
        applyTheme()
      }
    }

    mediaQuery.addEventListener('change', handleChange)
    return () => mediaQuery.removeEventListener('change', handleChange)
  }, [theme])

  // Listen for menu navigation
  useEffect(() => {
    const unsubNavigate = window.conduit.onNavigate((path: string) => {
      setRoute(path as Route)
    })
    const unsubSearch = window.conduit.onOpenSearch(() => {
      setSearchOpen(true)
    })
    return () => {
      unsubNavigate()
      unsubSearch()
    }
  }, [])

  // Subscribe to SSE events
  useEffect(() => {
    const unsubConnected = window.conduit.onConnected(() => {
      setSSEConnected(true)
    })

    const unsubDisconnected = window.conduit.onDisconnected(() => {
      setSSEConnected(false)
    })

    const unsubEvent = window.conduit.onEvent((event: unknown) => {
      const e = event as { type: string; data: Record<string, unknown> }
      switch (e.type) {
        case 'daemon_status':
          // Trigger refresh to sync state from CLI (stateless pattern)
          handleSSEEvent(e)
          break
        case 'instance_created':
          addInstance({
            id: e.data.instance_id as string,
            name: e.data.name as string,
            connector: e.data.connector as string,
            status: 'STOPPED'
          })
          break
        case 'instance_deleted':
          removeInstance(e.data.instance_id as string)
          break
        case 'instance_status_changed':
          updateInstance(e.data.instance_id as string, {
            status: e.data.status as 'RUNNING' | 'STOPPED' | 'STARTING' | 'STOPPING' | 'ERROR'
          })
          break
        case 'kb_source_added':
          addSource({
            id: e.data.source_id as string,
            name: e.data.name as string,
            path: e.data.path as string
          })
          break
        case 'kb_source_removed':
          removeSource(e.data.source_id as string)
          break
        case 'kb_sync_started':
          setSyncing(e.data.source_id as string, true)
          break
        case 'kb_sync_completed':
          setSyncing(e.data.source_id as string, false)
          updateSource(e.data.source_id as string, {
            documents: e.data.added as number,
            lastSync: new Date().toISOString()
          })
          break
        case 'kb_sync_failed':
          setSyncing(e.data.source_id as string, false)
          break
      }
    })

    // Initial data load
    refreshDaemon()
    refreshInstances()
    refreshKB()

    return () => {
      unsubConnected()
      unsubDisconnected()
      unsubEvent()
    }
  }, [])

  // Memoize view rendering to prevent unnecessary re-renders
  const currentView = useMemo(() => {
    switch (route) {
      case '/kb':
        return <KBView />
      case '/connectors':
        return <ConnectorsView />
      case '/settings':
        return <SettingsView />
      default:
        return <DashboardView />
    }
  }, [route])

  // Memoize navigation handler for NavigationShell
  const handleNavigate = useCallback((newRoute: Route) => {
    setRoute(newRoute)
  }, [])

  // Memoize search open change handler
  const handleSearchOpenChange = useCallback((open: boolean) => {
    setSearchOpen(open)
  }, [])

  // ═══════════════════════════════════════════════════════════════
  // RENDER LOGIC: CLI verification gates everything
  // ═══════════════════════════════════════════════════════════════

  // Debug logging for render decisions
  console.log('[App] Render state:', { startupCheckDone, cliInstalled, setupCompleted })

  // Still checking CLI on startup - show loading spinner
  if (!startupCheckDone) {
    console.log('[App] Rendering: StartupLoader (startupCheckDone=false)')
    return <StartupLoader />
  }

  // CLI not installed OR setup not complete - show setup wizard
  // Uses cliInstalled from store (updated during setup flow)
  // This ensures we NEVER show dashboard without verified CLI
  if (!cliInstalled || !setupCompleted) {
    console.log('[App] Rendering: SetupWizard (cliInstalled=', cliInstalled, ', setupCompleted=', setupCompleted, ')')
    return <SetupWizard />
  }

  console.log('[App] Rendering: Main app')

  // CLI verified AND setup complete - show main app
  return (
    <div className="h-screen flex flex-col">
      {/* Global update banner */}
      <UpdateBanner />

      {/* Main app */}
      <NavigationShell
        currentRoute={route}
        onNavigate={handleNavigate}
        searchOpen={searchOpen}
        onSearchOpenChange={handleSearchOpenChange}
        className="flex-1 min-h-0"
      >
        <Suspense fallback={<ViewLoader />}>
          {currentView}
        </Suspense>
      </NavigationShell>
    </div>
  )
}
