/**
 * Setup Store
 *
 * Simplified state management for the setup wizard.
 * CLI is the source of truth - GUI checks CLI status on every startup.
 */
import { create } from 'zustand'

// Simplified to 3 steps: welcome, terminal-install, complete
export type SetupStep = 'welcome' | 'terminal-install' | 'complete'

// Keep DependencyStatus for backward compatibility (dashboard may still use it)
export interface DependencyStatus {
  name: string
  installed: boolean
  version?: string
  required: boolean
  installUrl?: string
  brewFormula?: string
  binaryPath?: string
  customPath?: string
  supportsAutoInstall?: boolean
}

export interface SetupState {
  // Setup completion
  setupCompleted: boolean
  setupCompletedAt: string | null

  // Current step
  currentStep: SetupStep

  // CLI installation (still needed for App.tsx detection)
  cliInstalled: boolean
  cliVersion: string | null
  cliInstallPath: string | null

  // Terminal installation state
  installationSuccess: boolean | null
  installationError: string | null

  // Legacy: Keep for backward compatibility with other components
  dependencies: DependencyStatus[]
  daemonRunning: boolean
  qdrantRunning: boolean
  falkordbRunning: boolean
  modelsInstalled: string[]
  modelsPending: string[]
  currentOperation: string | null
  operationProgress: number
  operationError: string | null

  // Actions
  setStep: (step: SetupStep) => void
  setCLIInstalled: (installed: boolean, version?: string, path?: string) => void
  setInstallationResult: (success: boolean, error?: string) => void
  completeSetup: () => void
  resetSetup: () => void

  // Legacy actions (keep for backward compatibility)
  setDependencies: (deps: DependencyStatus[]) => void
  updateDependency: (name: string, status: Partial<DependencyStatus>) => void
  setDaemonRunning: (running: boolean) => void
  setQdrantRunning: (running: boolean) => void
  setFalkorDBRunning: (running: boolean) => void
  addInstalledModel: (model: string) => void
  setModelsPending: (models: string[]) => void
  setOperation: (operation: string | null, progress?: number) => void
  setOperationError: (error: string | null) => void
}

export const useSetupStore = create<SetupState>()((set) => ({
  // Initial state
  setupCompleted: false,
  setupCompletedAt: null,
  currentStep: 'welcome',
  cliInstalled: false,
  cliVersion: null,
  cliInstallPath: null,
  installationSuccess: null,
  installationError: null,

  // Legacy state (kept for backward compatibility)
  dependencies: [],
  daemonRunning: false,
  qdrantRunning: false,
  falkordbRunning: false,
  modelsInstalled: [],
  modelsPending: ['nomic-embed-text', 'qwen2.5-coder:7b', 'mistral:7b-instruct'],
  currentOperation: null,
  operationProgress: 0,
  operationError: null,

  // Actions
  setStep: (step) => set({ currentStep: step, operationError: null }),

  setCLIInstalled: (installed, version, path) => set({
    cliInstalled: installed,
    cliVersion: version || null,
    cliInstallPath: path || null,
  }),

  setInstallationResult: (success, error) => set({
    installationSuccess: success,
    installationError: error || null,
  }),

  completeSetup: () => set({
    setupCompleted: true,
    setupCompletedAt: new Date().toISOString(),
    currentStep: 'complete',
    currentOperation: null,
    operationError: null,
  }),

  resetSetup: () => set({
    setupCompleted: false,
    setupCompletedAt: null,
    currentStep: 'welcome',
    cliInstalled: false,
    cliVersion: null,
    cliInstallPath: null,
    installationSuccess: null,
    installationError: null,
    dependencies: [],
    daemonRunning: false,
    qdrantRunning: false,
    falkordbRunning: false,
    modelsInstalled: [],
    modelsPending: ['nomic-embed-text', 'qwen2.5-coder:7b', 'mistral:7b-instruct'],
    currentOperation: null,
    operationProgress: 0,
    operationError: null,
  }),

  // Legacy actions (kept for backward compatibility)
  setDependencies: (deps) => set({ dependencies: deps }),

  updateDependency: (name, status) => set((state) => ({
    dependencies: state.dependencies.map((d) =>
      d.name === name ? { ...d, ...status } : d
    ),
  })),

  setDaemonRunning: (running) => set({ daemonRunning: running }),
  setQdrantRunning: (running) => set({ qdrantRunning: running }),
  setFalkorDBRunning: (running) => set({ falkordbRunning: running }),

  addInstalledModel: (model) => set((state) => ({
    modelsInstalled: [...state.modelsInstalled, model],
    modelsPending: state.modelsPending.filter((m) => m !== model),
  })),

  setModelsPending: (models) => set({ modelsPending: models }),

  setOperation: (operation, progress = 0) => set({
    currentOperation: operation,
    operationProgress: progress,
    operationError: null,
  }),

  setOperationError: (error) => set({
    operationError: error,
    currentOperation: null,
  }),
}))
