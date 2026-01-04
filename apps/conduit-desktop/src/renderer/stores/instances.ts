import { create } from 'zustand'

export interface Instance {
  id: string
  name: string
  connector: string
  status: 'RUNNING' | 'STOPPED' | 'STARTING' | 'STOPPING' | 'ERROR'
  health?: 'healthy' | 'unhealthy' | 'unknown'
  createdAt?: string
}

interface InstancesStore {
  instances: Instance[]
  loading: boolean

  setInstances: (instances: Instance[]) => void
  updateInstance: (id: string, updates: Partial<Instance>) => void
  addInstance: (instance: Instance) => void
  removeInstance: (id: string) => void
  setLoading: (loading: boolean) => void
  refresh: () => Promise<void>
}

export const useInstancesStore = create<InstancesStore>((set, get) => ({
  instances: [],
  loading: false,

  setInstances: (instances) => set({ instances }),

  updateInstance: (id, updates) =>
    set((state) => ({
      instances: state.instances.map((inst) =>
        inst.id === id ? { ...inst, ...updates } : inst
      )
    })),

  addInstance: (instance) =>
    set((state) => ({
      instances: [...state.instances, instance]
    })),

  removeInstance: (id) =>
    set((state) => ({
      instances: state.instances.filter((inst) => inst.id !== id)
    })),

  setLoading: (loading) => set({ loading }),

  refresh: async () => {
    get().setLoading(true)
    try {
      const result = await window.conduit.listInstances()
      if (result && typeof result === 'object' && 'instances' in result) {
        get().setInstances((result as { instances: Instance[] }).instances || [])
      }
    } catch (err) {
      console.error('Failed to refresh instances:', err)
    } finally {
      get().setLoading(false)
    }
  }
}))
