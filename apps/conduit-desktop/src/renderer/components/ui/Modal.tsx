import { ReactNode, useEffect } from 'react'
import { cn } from '@/lib/utils'
import { X } from 'lucide-react'

interface ModalProps {
  open: boolean
  onClose: () => void
  title: string
  children: ReactNode
  className?: string
}

export function Modal({ open, onClose, title, children, className }: ModalProps): JSX.Element | null {
  useEffect(() => {
    const handleEscape = (e: KeyboardEvent): void => {
      if (e.key === 'Escape' && open) {
        onClose()
      }
    }
    document.addEventListener('keydown', handleEscape)
    return () => document.removeEventListener('keydown', handleEscape)
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center"
      role="dialog"
      aria-modal="true"
      aria-labelledby="modal-title"
    >
      <div
        className="absolute inset-0 bg-black/20 dark:bg-black/40"
        onClick={onClose}
      />
      <div
        className={cn(
          'relative bg-white dark:bg-macos-bg-dark-secondary rounded-xl shadow-macos-lg',
          'animate-slide-in-bottom',
          'max-h-[85vh] overflow-hidden flex flex-col',
          className || 'w-[480px]'
        )}
      >
        <div className="flex items-center justify-between px-5 py-4 border-b border-macos-separator dark:border-macos-separator-dark">
          <h2 id="modal-title" className="text-lg font-semibold">
            {title}
          </h2>
          <button
            onClick={onClose}
            className="p-1 rounded hover:bg-macos-bg-secondary dark:hover:bg-macos-bg-dark-tertiary transition-colors"
          >
            <X className="w-5 h-5 text-macos-text-secondary dark:text-macos-text-dark-secondary" />
          </button>
        </div>
        <div className="flex-1 overflow-auto">{children}</div>
      </div>
    </div>
  )
}

interface ModalFooterProps {
  children: ReactNode
}

export function ModalFooter({ children }: ModalFooterProps): JSX.Element {
  return (
    <div className="flex items-center justify-end gap-2 px-5 py-4 border-t border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/50 dark:bg-macos-bg-dark-tertiary/50">
      {children}
    </div>
  )
}
