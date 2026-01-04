import { useEffect, useState, lazy, Suspense, useMemo, useCallback } from 'react'
import { NavigationShell } from './components/layout/NavigationShell'
import { useDaemonStore, useInstancesStore, useKBStore, useSettingsStore } from './stores'
import { UpdateBanner } from './components/ui/UpdateBanner'
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

type Route = '/' | '/kb' | '/connectors' | '/settings'

export default function App(): JSX.Element {
  const [route, setRoute] = useState<Route>('/')
  const [searchOpen, setSearchOpen] = useState(false)
  const { theme } = useSettingsStore()
  const { setSSEConnected, setStatus, refresh: refreshDaemon } = useDaemonStore()
  const { updateInstance, addInstance, removeInstance, refresh: refreshInstances } = useInstancesStore()
  const { addSource, removeSource, updateSource, setSyncing, refresh: refreshKB } = useKBStore()

  // Apply theme
  useEffect(() => {
    const root = document.documentElement
    if (theme === 'system') {
      const isDark = window.matchMedia('(prefers-color-scheme: dark)').matches
      root.classList.toggle('dark', isDark)
    } else {
      root.classList.toggle('dark', theme === 'dark')
    }
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
          setStatus({
            connected: true,
            uptime: e.data.uptime as string,
            startTime: e.data.start_time as string
          })
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
