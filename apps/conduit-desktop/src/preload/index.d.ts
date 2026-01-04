import { ElectronAPI } from '@electron-toolkit/preload'
import { ConduitAPI } from './index'

declare global {
  interface Window {
    electron: ElectronAPI
    conduit: ConduitAPI
  }
}
