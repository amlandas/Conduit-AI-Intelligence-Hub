import { useState } from 'react'
import { Modal, ModalFooter } from '@/components/ui/Modal'
import { cn } from '@/lib/utils'
import { Cable, Loader2, Check } from 'lucide-react'

interface Connector {
  id: string
  name: string
  description: string
}

const AVAILABLE_CONNECTORS: Connector[] = [
  {
    id: 'filesystem',
    name: 'Filesystem',
    description: 'Access local files and directories'
  },
  {
    id: 'github',
    name: 'GitHub',
    description: 'Access GitHub repositories and issues'
  },
  {
    id: 'fetch',
    name: 'Web Fetch',
    description: 'Fetch content from web URLs'
  },
  {
    id: 'brave-search',
    name: 'Brave Search',
    description: 'Search the web using Brave Search'
  }
]

interface AddConnectorModalProps {
  open: boolean
  onClose: () => void
  onAdd: (connector: string, name: string) => Promise<void>
}

export function AddConnectorModal({ open, onClose, onAdd }: AddConnectorModalProps): JSX.Element {
  const [selectedConnector, setSelectedConnector] = useState<string | null>(null)
  const [instanceName, setInstanceName] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()
    if (!selectedConnector || !instanceName.trim()) {
      setError('Please select a connector and provide a name')
      return
    }

    setLoading(true)
    setError(null)

    try {
      await onAdd(selectedConnector, instanceName.trim())
      setSelectedConnector(null)
      setInstanceName('')
      onClose()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const handleClose = (): void => {
    if (!loading) {
      setSelectedConnector(null)
      setInstanceName('')
      setError(null)
      onClose()
    }
  }

  return (
    <Modal open={open} onClose={handleClose} title="Add Connector" className="w-[560px]">
      <form onSubmit={handleSubmit}>
        <div className="p-5 space-y-5">
          {/* Connector selection */}
          <div>
            <label className="block text-sm font-medium mb-2">
              Select Connector Type
            </label>
            <div className="grid grid-cols-2 gap-2">
              {AVAILABLE_CONNECTORS.map((connector) => (
                <button
                  key={connector.id}
                  type="button"
                  onClick={() => setSelectedConnector(connector.id)}
                  className={cn(
                    'p-3 text-left rounded-lg border-2 transition-colors',
                    selectedConnector === connector.id
                      ? 'border-macos-blue bg-macos-blue/5'
                      : 'border-macos-separator dark:border-macos-separator-dark hover:border-macos-blue/50'
                  )}
                >
                  <div className="flex items-start gap-2">
                    <Cable className="w-4 h-4 mt-0.5 flex-shrink-0 text-macos-blue" />
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-sm">{connector.name}</span>
                        {selectedConnector === connector.id && (
                          <Check className="w-4 h-4 text-macos-blue" />
                        )}
                      </div>
                      <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary mt-0.5">
                        {connector.description}
                      </p>
                    </div>
                  </div>
                </button>
              ))}
            </div>
          </div>

          {/* Instance name */}
          <div>
            <label htmlFor="instance-name" className="block text-sm font-medium mb-1.5">
              Instance Name
            </label>
            <input
              id="instance-name"
              type="text"
              value={instanceName}
              onChange={(e) => setInstanceName(e.target.value)}
              placeholder="my-filesystem"
              className="w-full px-3 py-2 rounded-lg border border-macos-separator dark:border-macos-separator-dark bg-white dark:bg-macos-bg-dark-tertiary focus:outline-none focus:ring-2 focus:ring-macos-blue"
              disabled={loading}
            />
            <p className="mt-1.5 text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
              A unique name for this connector instance
            </p>
          </div>

          {error && (
            <div className="p-3 rounded-lg bg-macos-red/10 text-macos-red text-sm">
              {error}
            </div>
          )}
        </div>

        <ModalFooter>
          <button
            type="button"
            onClick={handleClose}
            className="btn btn-secondary"
            disabled={loading}
          >
            Cancel
          </button>
          <button
            type="submit"
            className="btn btn-primary"
            disabled={loading || !selectedConnector || !instanceName.trim()}
          >
            {loading ? (
              <>
                <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                Creating...
              </>
            ) : (
              'Create Connector'
            )}
          </button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
