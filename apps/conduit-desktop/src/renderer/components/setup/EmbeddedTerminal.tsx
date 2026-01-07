/**
 * EmbeddedTerminal Component
 *
 * Provides an embedded terminal using xterm.js connected to node-pty via IPC.
 * Used by the setup wizard to run the CLI install script interactively.
 */
import { useEffect, useRef, useCallback, useState } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

interface EmbeddedTerminalProps {
  /** Command to run in the terminal. If not provided, opens interactive shell. */
  command?: string
  /** Working directory for the command */
  cwd?: string
  /** Called when the terminal process exits */
  onExit?: (exitCode: number) => void
  /** Called when success is detected in terminal output */
  onSuccess?: () => void
  /** Called when an error is detected in terminal output */
  onError?: (error: string) => void
  /** Patterns to detect success in terminal output */
  successPatterns?: string[]
  /** Patterns to detect errors in terminal output */
  errorPatterns?: string[]
  /** Auto-start the command on mount */
  autoStart?: boolean
  /** Additional CSS classes */
  className?: string
}

// Default patterns for detecting success/error in install script output
const DEFAULT_SUCCESS_PATTERNS = [
  'Installation Complete!',
  '✓ Setup complete',
  'conduit CLI:',
]

const DEFAULT_ERROR_PATTERNS = [
  'error:',
  'Error:',
  'ERROR:',
  'failed',
  'Failed',
  'FAILED',
]

export function EmbeddedTerminal({
  command,
  cwd,
  onExit,
  onSuccess,
  onError,
  successPatterns = DEFAULT_SUCCESS_PATTERNS,
  errorPatterns = DEFAULT_ERROR_PATTERNS,
  autoStart = true,
  className = '',
}: EmbeddedTerminalProps): JSX.Element {
  const terminalRef = useRef<HTMLDivElement>(null)
  const terminalInstance = useRef<Terminal | null>(null)
  const fitAddon = useRef<FitAddon | null>(null)
  const [isStarted, setIsStarted] = useState(false)
  const outputBuffer = useRef<string>('')

  // Check output for success/error patterns
  const checkPatterns = useCallback((text: string) => {
    outputBuffer.current += text

    // Check for success patterns
    if (onSuccess) {
      for (const pattern of successPatterns) {
        if (outputBuffer.current.includes(pattern)) {
          onSuccess()
          break
        }
      }
    }

    // Check for error patterns (only if success not already detected)
    if (onError) {
      for (const pattern of errorPatterns) {
        if (outputBuffer.current.toLowerCase().includes(pattern.toLowerCase())) {
          // Don't trigger error callback immediately - wait for exit code
          break
        }
      }
    }
  }, [onSuccess, onError, successPatterns, errorPatterns])

  // Initialize terminal
  useEffect(() => {
    if (!terminalRef.current) return

    // Create terminal instance
    const terminal = new Terminal({
      cursorBlink: true,
      fontSize: 13,
      fontFamily: 'SF Mono, Menlo, Monaco, "Courier New", monospace',
      theme: {
        background: '#1e1e1e',
        foreground: '#d4d4d4',
        cursor: '#aeafad',
        cursorAccent: '#000000',
        selectionBackground: '#264f78',
        black: '#000000',
        red: '#cd3131',
        green: '#0dbc79',
        yellow: '#e5e510',
        blue: '#2472c8',
        magenta: '#bc3fbc',
        cyan: '#11a8cd',
        white: '#e5e5e5',
        brightBlack: '#666666',
        brightRed: '#f14c4c',
        brightGreen: '#23d18b',
        brightYellow: '#f5f543',
        brightBlue: '#3b8eea',
        brightMagenta: '#d670d6',
        brightCyan: '#29b8db',
        brightWhite: '#e5e5e5',
      },
      allowProposedApi: true,
    })

    // Create fit addon for responsive sizing
    const fit = new FitAddon()
    terminal.loadAddon(fit)

    // Open terminal in DOM
    terminal.open(terminalRef.current)
    fit.fit()

    // Store references
    terminalInstance.current = terminal
    fitAddon.current = fit

    // Handle user input - send to PTY
    terminal.onData((data) => {
      window.conduit.sendTerminalInput(data)
    })

    // Handle terminal resize
    const handleResize = (): void => {
      if (fitAddon.current && terminalInstance.current) {
        fitAddon.current.fit()
        window.conduit.resizeTerminal(
          terminalInstance.current.cols,
          terminalInstance.current.rows
        )
      }
    }

    // Set up resize observer
    const resizeObserver = new ResizeObserver(handleResize)
    resizeObserver.observe(terminalRef.current)

    // Also handle window resize
    window.addEventListener('resize', handleResize)

    // Subscribe to terminal data from PTY
    const unsubscribeData = window.conduit.onTerminalData((data: string) => {
      terminal.write(data)
      checkPatterns(data)
    })

    // Subscribe to terminal exit
    const unsubscribeExit = window.conduit.onTerminalExit((exitCode: number) => {
      terminal.write(`\r\n\x1b[90m[Process exited with code ${exitCode}]\x1b[0m\r\n`)
      if (onExit) {
        onExit(exitCode)
      }
      // Check for error based on exit code
      if (exitCode !== 0 && onError) {
        onError(`Process exited with code ${exitCode}`)
      }
    })

    // Cleanup
    return () => {
      resizeObserver.disconnect()
      window.removeEventListener('resize', handleResize)
      unsubscribeData()
      unsubscribeExit()
      terminal.dispose()
      terminalInstance.current = null
      fitAddon.current = null
    }
  }, [checkPatterns, onExit, onError])

  // Start terminal if autoStart is enabled
  useEffect(() => {
    if (autoStart && !isStarted && terminalInstance.current) {
      startTerminal()
    }
  }, [autoStart, isStarted])

  // Start terminal session
  const startTerminal = useCallback(async () => {
    if (isStarted) return

    setIsStarted(true)
    outputBuffer.current = ''

    try {
      const result = await window.conduit.spawnTerminal({ command, cwd })
      if (!result.success) {
        terminalInstance.current?.write(
          `\x1b[31mFailed to start terminal: ${result.error}\x1b[0m\r\n`
        )
        if (onError) {
          onError(result.error || 'Failed to start terminal')
        }
      }
    } catch (err) {
      const errorMessage = (err as Error).message
      terminalInstance.current?.write(
        `\x1b[31mError: ${errorMessage}\x1b[0m\r\n`
      )
      if (onError) {
        onError(errorMessage)
      }
    }
  }, [command, cwd, isStarted, onError])

  // Expose start method for external use
  useEffect(() => {
    // Re-fit when component becomes visible
    if (fitAddon.current) {
      setTimeout(() => fitAddon.current?.fit(), 100)
    }
  }, [])

  return (
    <div
      className={`embedded-terminal ${className}`}
      style={{
        width: '100%',
        height: '100%',
        minHeight: '300px',
        borderRadius: '8px',
        overflow: 'hidden',
        background: '#1e1e1e',
      }}
    >
      <div
        ref={terminalRef}
        style={{
          width: '100%',
          height: '100%',
          padding: '8px',
        }}
      />
    </div>
  )
}

export default EmbeddedTerminal
