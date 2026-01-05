import { create } from 'zustand'
import { persist } from 'zustand/middleware'

export type SetupStep =
  | 'welcome'
  | 'cli-install'
  | 'dependencies'
  | 'services'
  | 'models'
  | 'complete'

export interface DependencyStatus {
  name: string
  installed: boolean
  version?: string
  required: boolean
  installUrl?: string
  brewFormula?: string
  binaryPath?: string       // Path where the binary was found
  customPath?: string       // User-specified custom path (if any)
  supportsAutoInstall?: boolean  // Whether auto-installation is supported
}

export interface SetupState {
  // Setup completion
  setupCompleted: boolean
  setupCompletedAt: string | null

  // Current step
  currentStep: SetupStep

  // CLI installation
  cliInstalled: boolean
  cliVersion: string | null
  cliInstallPath: string | null

  // Dependencies
  dependencies: DependencyStatus[]

  // Services
  daemonRunning: boolean
  qdrantRunning: boolean
  falkordbRunning: boolean

  // Models
  modelsInstalled: string[]
  modelsPending: string[]

  // Progress
  currentOperation: string | null
  operationProgress: number
  operationError: string | null

  // Actions
  setStep: (step: SetupStep) => void
  setCLIInstalled: (installed: boolean, version?: string, path?: string) => void
  setDependencies: (deps: DependencyStatus[]) => void
  updateDependency: (name: string, status: Partial<DependencyStatus>) => void
  setDaemonRunning: (running: boolean) => void
  setQdrantRunning: (running: boolean) => void
  setFalkorDBRunning: (running: boolean) => void
  addInstalledModel: (model: string) => void
  setModelsPending: (models: string[]) => void
  setOperation: (operation: string | null, progress?: number) => void
  setOperationError: (error: string | null) => void
  completeSetup: () => void
  resetSetup: () => void
}

export const useSetupStore = create<SetupState>()(
  persist(
    (set) => ({
      // Initial state
      setupCompleted: false,
      setupCompletedAt: null,
      currentStep: 'welcome',
      cliInstalled: false,
      cliVersion: null,
      cliInstallPath: null,
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
    }),
    {
      name: 'conduit-setup',
      partialize: (state) => ({
        setupCompleted: state.setupCompleted,
        setupCompletedAt: state.setupCompletedAt,
        cliInstalled: state.cliInstalled,
        cliVersion: state.cliVersion,
        cliInstallPath: state.cliInstallPath,
      }),
    }
  )
)
