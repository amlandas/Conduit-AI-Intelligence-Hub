import { useEffect, useState } from 'react'
import { NavigationShell } from './components/layout/NavigationShell'
import { DashboardView } from './components/dashboard/DashboardView'
import { KBView } from './components/kb/KBView'
import { ConnectorsView } from './components/connectors/ConnectorsView'
import { SettingsView } from './components/settings/SettingsView'
import { useDaemonStore, useInstancesStore, useKBStore, useSettingsStore } from './stores'

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

  const renderView = (): JSX.Element => {
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
  }

  return (
    <NavigationShell
      currentRoute={route}
      onNavigate={setRoute}
      searchOpen={searchOpen}
      onSearchOpenChange={setSearchOpen}
    >
      {renderView()}
    </NavigationShell>
  )
}
