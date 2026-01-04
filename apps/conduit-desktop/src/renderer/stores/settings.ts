import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type AppMode = 'default' | 'advanced' | 'developer'
export type Theme = 'system' | 'light' | 'dark'

interface SettingsStore {
  mode: AppMode
  theme: Theme
  sidebarCollapsed: boolean

  setMode: (mode: AppMode) => void
  setTheme: (theme: Theme) => void
  toggleSidebar: () => void
  isFeatureVisible: (feature: FeatureFlag) => boolean
}

type FeatureFlag =
  | 'showContainerControls'
  | 'showRAGTuning'
  | 'showKAGPanel'
  | 'showConfigEditor'
  | 'showLogViewer'
  | 'showRawJSON'
  | 'showAPIControls'
  | 'showDaemonControls'
  | 'showHealthHistory'
  | 'showResourceUsage'
  | 'showChunkResults'
  | 'showSearchModeSelector'
  | 'showSyncPolicies'

const featureMatrix: Record<FeatureFlag, AppMode[]> = {
  showContainerControls: ['advanced', 'developer'],
  showRAGTuning: ['advanced', 'developer'],
  showKAGPanel: ['advanced', 'developer'],
  showConfigEditor: ['developer'],
  showLogViewer: ['developer'],
  showRawJSON: ['developer'],
  showAPIControls: ['developer'],
  showDaemonControls: ['developer'],
  showHealthHistory: ['advanced', 'developer'],
  showResourceUsage: ['advanced', 'developer'],
  showChunkResults: ['developer'],
  showSearchModeSelector: ['advanced', 'developer'],
  showSyncPolicies: ['advanced', 'developer']
}

export const useSettingsStore = create<SettingsStore>()(
  persist(
    (set, get) => ({
      mode: 'default',
      theme: 'system',
      sidebarCollapsed: false,

      setMode: (mode) => set({ mode }),
      setTheme: (theme) => set({ theme }),
      toggleSidebar: () => set((state) => ({ sidebarCollapsed: !state.sidebarCollapsed })),

      isFeatureVisible: (feature) => {
        const allowedModes = featureMatrix[feature]
        return allowedModes?.includes(get().mode) ?? false
      }
    }),
    {
      name: 'conduit-settings'
    }
  )
)
