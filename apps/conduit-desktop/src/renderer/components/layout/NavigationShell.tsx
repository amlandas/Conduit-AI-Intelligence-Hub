import { ReactNode } from 'react'
import { cn } from '@/lib/utils'
import { useDaemonStore, useInstancesStore, useKBStore, useSettingsStore } from '@/stores'
import { SearchModal } from './SearchModal'
import {
  LayoutDashboard,
  Database,
  Cable,
  Settings,
  Search
} from 'lucide-react'

type Route = '/' | '/kb' | '/connectors' | '/settings'

interface NavItem {
  route: Route
  label: string
  icon: typeof LayoutDashboard
}

const navItems: NavItem[] = [
  { route: '/', label: 'Dashboard', icon: LayoutDashboard },
  { route: '/kb', label: 'Knowledge Base', icon: Database },
  { route: '/connectors', label: 'Connectors', icon: Cable },
  { route: '/settings', label: 'Settings', icon: Settings }
]

interface NavigationShellProps {
  children: ReactNode
  currentRoute: Route
  onNavigate: (route: Route) => void
  searchOpen: boolean
  onSearchOpenChange: (open: boolean) => void
  className?: string
}

export function NavigationShell({
  children,
  currentRoute,
  onNavigate,
  searchOpen,
  onSearchOpenChange,
  className
}: NavigationShellProps): JSX.Element {
  const { status, sseConnected } = useDaemonStore()
  const { instances } = useInstancesStore()
  const { sources } = useKBStore()
  const { sidebarCollapsed } = useSettingsStore()

  const runningInstances = instances.filter((i) => i.status === 'RUNNING').length

  return (
    <div className={cn('h-full flex flex-col', className)}>
      {/* Title bar drag region */}
      <div className="h-[var(--titlebar-height)] flex-shrink-0 drag-region" />

      <div className="flex-1 flex overflow-hidden">
        {/* Sidebar */}
        <aside
          className={cn(
            'flex-shrink-0 vibrancy-sidebar border-r border-macos-separator dark:border-macos-separator-dark flex flex-col',
            sidebarCollapsed ? 'w-16' : 'w-[var(--sidebar-width)]'
          )}
        >
          {/* Search button */}
          <div className="px-3 py-2">
            <button
              onClick={() => onSearchOpenChange(true)}
              className={cn(
                'w-full flex items-center gap-2 px-2 py-1.5 rounded-md text-sm',
                'bg-macos-bg-secondary dark:bg-macos-bg-dark-tertiary',
                'text-macos-text-secondary dark:text-macos-text-dark-secondary',
                'hover:bg-macos-bg-tertiary dark:hover:bg-macos-separator-dark',
                'transition-colors no-drag no-select'
              )}
            >
              <Search className="w-4 h-4" />
              {!sidebarCollapsed && (
                <>
                  <span className="flex-1 text-left">Search</span>
                  <kbd className="text-xs opacity-60">⌘K</kbd>
                </>
              )}
            </button>
          </div>

          {/* Navigation */}
          <nav className="flex-1 px-2 py-2 space-y-0.5">
            {navItems.map((item) => (
              <button
                key={item.route}
                onClick={() => onNavigate(item.route)}
                className={cn(
                  'sidebar-item w-full no-drag',
                  currentRoute === item.route && 'active'
                )}
              >
                <item.icon className="w-4 h-4 flex-shrink-0" />
                {!sidebarCollapsed && <span>{item.label}</span>}
              </button>
            ))}
          </nav>
        </aside>

        {/* Main content */}
        <main className="flex-1 overflow-auto bg-white dark:bg-macos-bg-dark-primary">
          <div className="p-6">{children}</div>
        </main>
      </div>

      {/* Status bar */}
      <footer className="h-[var(--statusbar-height)] flex-shrink-0 px-4 flex items-center gap-4 text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary border-t border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary dark:bg-macos-bg-dark-secondary">
        <div className="flex items-center gap-1.5">
          <span
            className={cn(
              'status-dot',
              status.connected ? 'status-dot-online' : 'status-dot-offline'
            )}
          />
          <span>Daemon {status.connected ? 'Running' : 'Offline'}</span>
        </div>

        {sseConnected && (
          <div className="flex items-center gap-1.5">
            <span className="status-dot status-dot-online animate-pulse" />
            <span>Live</span>
          </div>
        )}

        <div className="flex-1" />

        <span>{runningInstances} Connector{runningInstances !== 1 ? 's' : ''}</span>
        <span>{sources.length} Source{sources.length !== 1 ? 's' : ''}</span>
      </footer>

      <SearchModal open={searchOpen} onClose={() => onSearchOpenChange(false)} />
    </div>
  )
}
