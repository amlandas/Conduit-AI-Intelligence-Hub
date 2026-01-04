import * as http from 'http'
import * as os from 'os'
import * as path from 'path'
import { EventEmitter } from 'events'

const SOCKET_PATH = path.join(os.homedir(), '.conduit', 'conduit.sock')

export interface DaemonEvent {
  id: string
  type: string
  data: unknown
  timestamp: string
}

export class DaemonClient extends EventEmitter {
  private sseRequest: http.ClientRequest | null = null
  private reconnectTimer: NodeJS.Timeout | null = null
  private isConnected = false

  async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    return new Promise((resolve, reject) => {
      const options: http.RequestOptions = {
        socketPath: SOCKET_PATH,
        path: `/api/v1${path}`,
        method,
        headers: {
          'Content-Type': 'application/json'
        }
      }

      const req = http.request(options, (res) => {
        let data = ''
        res.on('data', (chunk) => {
          data += chunk
        })
        res.on('end', () => {
          if (res.statusCode && res.statusCode >= 200 && res.statusCode < 300) {
            try {
              resolve(JSON.parse(data) as T)
            } catch {
              resolve(data as unknown as T)
            }
          } else {
            reject(new Error(`HTTP ${res.statusCode}: ${data}`))
          }
        })
      })

      req.on('error', (err) => {
        reject(err)
      })

      if (body) {
        req.write(JSON.stringify(body))
      }
      req.end()
    })
  }

  async get<T>(path: string): Promise<T> {
    return this.request<T>('GET', path)
  }

  async post<T>(path: string, body?: unknown): Promise<T> {
    return this.request<T>('POST', path, body)
  }

  async delete<T>(path: string): Promise<T> {
    return this.request<T>('DELETE', path)
  }

  connectSSE(): void {
    if (this.sseRequest) {
      return
    }

    const options: http.RequestOptions = {
      socketPath: SOCKET_PATH,
      path: '/api/v1/events',
      method: 'GET',
      headers: {
        Accept: 'text/event-stream',
        'Cache-Control': 'no-cache'
      }
    }

    this.sseRequest = http.request(options, (res) => {
      if (res.statusCode !== 200) {
        this.emit('error', new Error(`SSE connection failed: ${res.statusCode}`))
        this.scheduleReconnect()
        return
      }

      this.isConnected = true
      this.emit('connected')

      let buffer = ''
      res.on('data', (chunk: Buffer) => {
        buffer += chunk.toString()
        const lines = buffer.split('\n\n')
        buffer = lines.pop() || ''

        for (const block of lines) {
          if (!block.trim()) continue
          const event = this.parseSSEBlock(block)
          if (event) {
            this.emit('event', event)
          }
        }
      })

      res.on('end', () => {
        this.isConnected = false
        this.sseRequest = null
        this.emit('disconnected')
        this.scheduleReconnect()
      })

      res.on('error', (err) => {
        this.emit('error', err)
        this.isConnected = false
        this.sseRequest = null
        this.scheduleReconnect()
      })
    })

    this.sseRequest.on('error', (err) => {
      this.emit('error', err)
      this.sseRequest = null
      this.scheduleReconnect()
    })

    this.sseRequest.end()
  }

  private parseSSEBlock(block: string): DaemonEvent | null {
    const lines = block.split('\n')
    let id = ''
    let eventType = 'message'
    let data = ''

    for (const line of lines) {
      if (line.startsWith('id:')) {
        id = line.slice(3).trim()
      } else if (line.startsWith('event:')) {
        eventType = line.slice(6).trim()
      } else if (line.startsWith('data:')) {
        data = line.slice(5).trim()
      }
    }

    if (!data) return null

    try {
      const parsed = JSON.parse(data)
      return {
        id,
        type: eventType,
        data: parsed,
        timestamp: new Date().toISOString()
      }
    } catch {
      return {
        id,
        type: eventType,
        data,
        timestamp: new Date().toISOString()
      }
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) return
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      this.connectSSE()
    }, 5000)
  }

  disconnectSSE(): void {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.sseRequest) {
      this.sseRequest.destroy()
      this.sseRequest = null
    }
    this.isConnected = false
  }

  getIsConnected(): boolean {
    return this.isConnected
  }
}

export const daemonClient = new DaemonClient()
