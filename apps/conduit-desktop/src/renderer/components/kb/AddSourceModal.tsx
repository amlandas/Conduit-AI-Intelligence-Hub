import { useState } from 'react'
import { Modal, ModalFooter } from '@/components/ui/Modal'
import { FolderOpen, Loader2 } from 'lucide-react'

interface AddSourceModalProps {
  open: boolean
  onClose: () => void
  onAdd: (name: string, path: string) => Promise<void>
}

export function AddSourceModal({ open, onClose, onAdd }: AddSourceModalProps): JSX.Element {
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = async (e: React.FormEvent): Promise<void> => {
    e.preventDefault()
    if (!name.trim() || !path.trim()) {
      setError('Name and path are required')
      return
    }

    setLoading(true)
    setError(null)

    try {
      await onAdd(name.trim(), path.trim())
      setName('')
      setPath('')
      onClose()
    } catch (err) {
      setError((err as Error).message)
    } finally {
      setLoading(false)
    }
  }

  const handleClose = (): void => {
    if (!loading) {
      setName('')
      setPath('')
      setError(null)
      onClose()
    }
  }

  return (
    <Modal open={open} onClose={handleClose} title="Add Knowledge Source">
      <form onSubmit={handleSubmit}>
        <div className="p-5 space-y-4">
          <div>
            <label htmlFor="source-name" className="block text-sm font-medium mb-1.5">
              Name
            </label>
            <input
              id="source-name"
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="My Documents"
              className="w-full px-3 py-2 rounded-lg border border-macos-separator dark:border-macos-separator-dark bg-white dark:bg-macos-bg-dark-tertiary focus:outline-none focus:ring-2 focus:ring-macos-blue"
              disabled={loading}
              autoFocus
            />
          </div>

          <div>
            <label htmlFor="source-path" className="block text-sm font-medium mb-1.5">
              Path
            </label>
            <div className="relative">
              <FolderOpen className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-macos-text-secondary dark:text-macos-text-dark-secondary" />
              <input
                id="source-path"
                type="text"
                value={path}
                onChange={(e) => setPath(e.target.value)}
                placeholder="/Users/you/Documents"
                className="w-full pl-10 pr-3 py-2 rounded-lg border border-macos-separator dark:border-macos-separator-dark bg-white dark:bg-macos-bg-dark-tertiary focus:outline-none focus:ring-2 focus:ring-macos-blue"
                disabled={loading}
              />
            </div>
            <p className="mt-1.5 text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary">
              Enter the full path to a folder containing documents to index
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
            disabled={loading || !name.trim() || !path.trim()}
          >
            {loading ? (
              <>
                <Loader2 className="w-4 h-4 mr-1.5 animate-spin" />
                Adding...
              </>
            ) : (
              'Add Source'
            )}
          </button>
        </ModalFooter>
      </form>
    </Modal>
  )
}
