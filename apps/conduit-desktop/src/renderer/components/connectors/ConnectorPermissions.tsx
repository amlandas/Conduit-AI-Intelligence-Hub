import { useState, useEffect } from 'react'
import { cn } from '@/lib/utils'
import {
  Shield,
  ShieldCheck,
  ShieldAlert,
  ShieldX,
  ChevronDown,
  ChevronRight,
  Loader2,
  AlertTriangle,
  Info
} from 'lucide-react'

interface Permission {
  id: string
  name: string
  description: string
  granted: boolean
  required: boolean
  category: 'filesystem' | 'network' | 'process' | 'system'
}

interface AuditResult {
  timestamp: string
  passed: boolean
  issues: AuditIssue[]
}

interface AuditIssue {
  severity: 'critical' | 'warning' | 'info'
  message: string
  permission?: string
}

interface ConnectorPermissionsProps {
  instanceId: string
  instanceName: string
  className?: string
}

const CATEGORY_LABELS: Record<string, { label: string; icon: typeof Shield }> = {
  filesystem: { label: 'Filesystem', icon: Shield },
  network: { label: 'Network', icon: Shield },
  process: { label: 'Process', icon: Shield },
  system: { label: 'System', icon: Shield }
}

function PermissionToggle({
  permission,
  onToggle
}: {
  permission: Permission
  onToggle: (id: string, granted: boolean) => void
}): JSX.Element {
  return (
    <div className="flex items-center justify-between py-2 px-3 rounded-lg hover:bg-macos-bg-secondary/50 dark:hover:bg-macos-bg-dark-tertiary/50">
      <div className="flex-1">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{permission.name}</span>
          {permission.required && (
            <span className="text-xs px-1.5 py-0.5 rounded bg-macos-orange/10 text-macos-orange">
              Required
            </span>
          )}
        </div>
        <p className="text-xs text-macos-text-secondary dark:text-macos-text-dark-secondary mt-0.5">
          {permission.description}
        </p>
      </div>
      <button
        onClick={() => onToggle(permission.id, !permission.granted)}
        disabled={permission.required && permission.granted}
        className={cn(
          'relative w-10 h-6 rounded-full transition-colors',
          permission.granted ? 'bg-macos-green' : 'bg-macos-bg-tertiary dark:bg-macos-bg-dark-tertiary',
          permission.required && permission.granted && 'opacity-50 cursor-not-allowed'
        )}
      >
        <span
          className={cn(
            'absolute top-0.5 w-5 h-5 rounded-full bg-white shadow transition-transform',
            permission.granted ? 'translate-x-4' : 'translate-x-0.5'
          )}
        />
      </button>
    </div>
  )
}

function AuditIssueItem({ issue }: { issue: AuditIssue }): JSX.Element {
  const icons = {
    critical: <ShieldX className="w-4 h-4 text-macos-red" />,
    warning: <AlertTriangle className="w-4 h-4 text-macos-orange" />,
    info: <Info className="w-4 h-4 text-macos-blue" />
  }

  const colors = {
    critical: 'border-macos-red/20 bg-macos-red/5',
    warning: 'border-macos-orange/20 bg-macos-orange/5',
    info: 'border-macos-blue/20 bg-macos-blue/5'
  }

  return (
    <div className={cn('flex items-start gap-2 p-2 rounded-lg border', colors[issue.severity])}>
      {icons[issue.severity]}
      <div className="flex-1 min-w-0">
        <p className="text-sm">{issue.message}</p>
        {issue.permission && (
          <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary mt-0.5">
            Permission: {issue.permission}
          </p>
        )}
      </div>
    </div>
  )
}

export function ConnectorPermissions({
  instanceId,
  instanceName,
  className
}: ConnectorPermissionsProps): JSX.Element {
  const [permissions, setPermissions] = useState<Permission[]>([])
  const [auditResult, setAuditResult] = useState<AuditResult | null>(null)
  const [loading, setLoading] = useState(true)
  const [auditing, setAuditing] = useState(false)
  const [expandedCategories, setExpandedCategories] = useState<Set<string>>(new Set(['filesystem']))

  useEffect(() => {
    loadPermissions()
  }, [instanceId])

  const loadPermissions = async (): Promise<void> => {
    setLoading(true)
    try {
      // Load permissions from daemon
      const result = await window.conduit.getInstancePermissions?.(instanceId)
      if (result && Array.isArray(result)) {
        setPermissions(result as Permission[])
      } else {
        // Mock data for UI development
        setPermissions([
          {
            id: 'fs_read',
            name: 'Read Files',
            description: 'Read files from the configured source path',
            granted: true,
            required: true,
            category: 'filesystem'
          },
          {
            id: 'fs_write',
            name: 'Write Files',
            description: 'Write or modify files (disabled by default)',
            granted: false,
            required: false,
            category: 'filesystem'
          },
          {
            id: 'net_outbound',
            name: 'Outbound Network',
            description: 'Make outbound HTTP/HTTPS requests',
            granted: true,
            required: false,
            category: 'network'
          },
          {
            id: 'net_inbound',
            name: 'Inbound Connections',
            description: 'Accept incoming network connections',
            granted: false,
            required: false,
            category: 'network'
          },
          {
            id: 'proc_spawn',
            name: 'Spawn Processes',
            description: 'Execute external commands or scripts',
            granted: false,
            required: false,
            category: 'process'
          },
          {
            id: 'sys_env',
            name: 'Environment Variables',
            description: 'Access host environment variables',
            granted: false,
            required: false,
            category: 'system'
          }
        ])
      }
    } catch (err) {
      console.error('Failed to load permissions:', err)
    } finally {
      setLoading(false)
    }
  }

  const runAudit = async (): Promise<void> => {
    setAuditing(true)
    try {
      const result = await window.conduit.auditInstance?.(instanceId)
      if (result) {
        setAuditResult(result as AuditResult)
      } else {
        // Mock audit result
        setAuditResult({
          timestamp: new Date().toISOString(),
          passed: true,
          issues: [
            {
              severity: 'info',
              message: 'Connector is running with minimal required permissions',
              permission: undefined
            }
          ]
        })
      }
    } catch (err) {
      console.error('Audit failed:', err)
      setAuditResult({
        timestamp: new Date().toISOString(),
        passed: false,
        issues: [
          {
            severity: 'critical',
            message: 'Audit failed to complete: ' + (err as Error).message,
            permission: undefined
          }
        ]
      })
    } finally {
      setAuditing(false)
    }
  }

  const handleTogglePermission = async (permId: string, granted: boolean): Promise<void> => {
    try {
      await window.conduit.setInstancePermission?.(instanceId, permId, granted)
      setPermissions((prev) =>
        prev.map((p) => (p.id === permId ? { ...p, granted } : p))
      )
    } catch (err) {
      console.error('Failed to update permission:', err)
    }
  }

  const toggleCategory = (category: string): void => {
    setExpandedCategories((prev) => {
      const newSet = new Set(prev)
      if (newSet.has(category)) {
        newSet.delete(category)
      } else {
        newSet.add(category)
      }
      return newSet
    })
  }

  const groupedPermissions = permissions.reduce(
    (acc, perm) => {
      if (!acc[perm.category]) acc[perm.category] = []
      acc[perm.category].push(perm)
      return acc
    },
    {} as Record<string, Permission[]>
  )

  const grantedCount = permissions.filter((p) => p.granted).length
  const totalCount = permissions.length

  if (loading) {
    return (
      <div className={cn('card', className)}>
        <div className="p-8 flex items-center justify-center">
          <Loader2 className="w-6 h-6 animate-spin text-macos-text-secondary" />
        </div>
      </div>
    )
  }

  return (
    <div className={cn('card', className)}>
      {/* Header */}
      <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-2">
            <ShieldCheck className="w-5 h-5 text-macos-green" />
            <div>
              <h3 className="font-medium">Permissions</h3>
              <p className="text-xs text-macos-text-secondary">
                {instanceName}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-macos-text-secondary">
              {grantedCount}/{totalCount} granted
            </span>
            <button
              onClick={runAudit}
              disabled={auditing}
              className="btn btn-secondary text-xs py-1"
            >
              {auditing ? (
                <Loader2 className="w-3.5 h-3.5 animate-spin mr-1" />
              ) : (
                <Shield className="w-3.5 h-3.5 mr-1" />
              )}
              Run Audit
            </button>
          </div>
        </div>
      </div>

      {/* Audit Results */}
      {auditResult && (
        <div className="p-4 border-b border-macos-separator dark:border-macos-separator-dark bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30">
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              {auditResult.passed ? (
                <ShieldCheck className="w-5 h-5 text-macos-green" />
              ) : (
                <ShieldAlert className="w-5 h-5 text-macos-orange" />
              )}
              <span className="font-medium text-sm">
                Audit {auditResult.passed ? 'Passed' : 'Warnings'}
              </span>
            </div>
            <span className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
              {new Date(auditResult.timestamp).toLocaleTimeString()}
            </span>
          </div>
          {auditResult.issues.length > 0 && (
            <div className="space-y-2">
              {auditResult.issues.map((issue, i) => (
                <AuditIssueItem key={i} issue={issue} />
              ))}
            </div>
          )}
        </div>
      )}

      {/* Permission Categories */}
      <div className="divide-y divide-macos-separator dark:divide-macos-separator-dark">
        {Object.entries(groupedPermissions).map(([category, perms]) => {
          const isExpanded = expandedCategories.has(category)
          const categoryInfo = CATEGORY_LABELS[category] || {
            label: category,
            icon: Shield
          }
          const grantedInCategory = perms.filter((p) => p.granted).length

          return (
            <div key={category}>
              <button
                onClick={() => toggleCategory(category)}
                className="w-full flex items-center justify-between p-3 hover:bg-macos-bg-secondary/50 dark:hover:bg-macos-bg-dark-tertiary/50"
              >
                <div className="flex items-center gap-2">
                  {isExpanded ? (
                    <ChevronDown className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
                  ) : (
                    <ChevronRight className="w-4 h-4 text-macos-text-tertiary dark:text-macos-text-dark-tertiary" />
                  )}
                  <categoryInfo.icon className="w-4 h-4 text-macos-text-secondary" />
                  <span className="font-medium text-sm">{categoryInfo.label}</span>
                </div>
                <span className="text-xs text-macos-text-secondary">
                  {grantedInCategory}/{perms.length}
                </span>
              </button>
              {isExpanded && (
                <div className="px-3 pb-3">
                  {perms.map((perm) => (
                    <PermissionToggle
                      key={perm.id}
                      permission={perm}
                      onToggle={handleTogglePermission}
                    />
                  ))}
                </div>
              )}
            </div>
          )
        })}
      </div>

      {/* Footer */}
      <div className="px-4 py-3 bg-macos-bg-secondary/30 dark:bg-macos-bg-dark-tertiary/30 border-t border-macos-separator dark:border-macos-separator-dark">
        <p className="text-xs text-macos-text-tertiary dark:text-macos-text-dark-tertiary">
          Permissions follow the principle of least privilege. Only grant what's necessary.
        </p>
      </div>
    </div>
  )
}
