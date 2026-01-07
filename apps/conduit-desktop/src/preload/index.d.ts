import { ElectronAPI } from '@electron-toolkit/preload'
import { ConduitAPI } from './index'

declare global {
  interface Window {
    electron: ElectronAPI
    conduit: ConduitAPI
  }

  // Build-time constant for app version (injected by vite)
  const __APP_VERSION__: string
}
