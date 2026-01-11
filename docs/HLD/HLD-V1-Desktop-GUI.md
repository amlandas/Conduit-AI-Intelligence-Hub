# High-Level Design V1 — Desktop GUI & Intelligence Hub

**Version**: 1.0.42 (Updated)
**Status**: SUBSTANTIALLY IMPLEMENTED
**Last Updated**: January 2026

This document describes the V1 desktop application layer and its current implementation status.

---

## Executive Summary

V1 transforms Conduit from a CLI-only tool into a **cross-platform desktop product** with:

- **Desktop GUI** — Electron-based management interface
- **Policy Engine** — Security policy evaluation (COMPLETE)
- **Consent Ledger** — Audit trail (SCHEMA READY)
- **MCP Auto-Configuration** — One-click AI client setup
- **Update System** — Bundled CLI with version management

---

## 1. V1 Goals

### Achieved

1. **Make Conduit usable by non-CLI users** — Desktop GUI with visual workflows
2. **Security that is legible** — Policy engine with clear permission model
3. **Reliable multi-client experience** — 4 client adapters with consistent behavior
4. **Operational maturity** — Doctor diagnostics, real-time status updates

### Deferred

- Enterprise org admin controls (RBAC)
- Rich connector store with curation
- Full consent ledger integration
- Signed artifact verification

---

## 2. Architecture

### 2.1 V1 Component Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Conduit Desktop                                  │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                     Electron Main Process                        │   │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────────────┐   │   │
│  │  │  IPC Bridge  │  │ CLI Bundle   │  │  Auto-Updater        │   │   │
│  │  └──────────────┘  └──────────────┘  └──────────────────────┘   │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                              │                                          │
│                              ▼                                          │
│  ┌─────────────────────────────────────────────────────────────────┐   │
│  │                     Electron Renderer                            │   │
│  │  ┌────────────┐  ┌────────────┐  ┌────────────┐  ┌───────────┐  │   │
│  │  │ Dashboard  │  │  KB View   │  │ Connectors │  │ Settings  │  │   │
│  │  └────────────┘  └────────────┘  └────────────┘  └───────────┘  │   │
│  │                                                                  │   │
│  │  ┌─────────────────────────────────────────────────────────────┐ │   │
│  │  │                    Zustand Stores                            │ │   │
│  │  │  daemon │ instances │ kb │ settings │ setup                  │ │   │
│  │  └─────────────────────────────────────────────────────────────┘ │   │
│  └─────────────────────────────────────────────────────────────────┘   │
│                              │                                          │
└──────────────────────────────┼──────────────────────────────────────────┘
                               │ CLI Execution
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                         Conduit CLI                                      │
│  (Bundled binary, executes conduit commands)                            │
└─────────────────────────────────────────────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       Conduit Daemon                                     │
│  (Long-running service, source of truth)                                │
└─────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Design Principle

**GUI is not the brain. The daemon is the brain. The GUI is a cockpit.**

- GUI never directly edits client configs
- GUI never runs containers
- GUI never stores secrets
- GUI calls CLI → CLI calls Daemon → Daemon performs actions

---

## 3. Desktop GUI Implementation

**Location**: `apps/conduit-desktop/`

### 3.1 Technology Stack

| Layer | Technology |
|-------|------------|
| Framework | Electron 28 |
| UI | React 18 + TypeScript |
| Bundler | electron-vite |
| Styling | Tailwind CSS + macOS patterns |
| State | Zustand |
| IPC | electron IPC bridge |

### 3.2 Views

| View | Purpose | Status |
|------|---------|--------|
| Dashboard | System overview, service status | Implemented |
| Knowledge Base | Source management, sync, search | Implemented |
| Connectors | Instance list, start/stop | Implemented |
| Settings | Configuration, preferences | Implemented |
| Setup Wizard | First-run onboarding | Implemented |

### 3.3 State Management

Zustand stores in `src/renderer/stores/`:

```typescript
// daemon.ts - Daemon connection and status
interface DaemonStore {
  connected: boolean
  version: string
  services: ServiceStatus[]
  refresh(): Promise<void>
}

// instances.ts - Connector instances
interface InstancesStore {
  instances: ConnectorInstance[]
  loading: boolean
  fetchInstances(): Promise<void>
  startInstance(id: string): Promise<void>
  stopInstance(id: string): Promise<void>
}

// kb.ts - Knowledge base state
interface KBStore {
  sources: KBSource[]
  stats: KBStats
  syncing: boolean
  addSource(path: string): Promise<void>
  syncSources(): Promise<void>
}
```

### 3.4 IPC Bridge

**Location**: `src/preload/index.ts`

The preload script exposes a `conduit` API to the renderer:

```typescript
contextBridge.exposeInMainWorld('conduit', {
  // CLI execution
  exec: (args: string[]) => ipcRenderer.invoke('conduit:exec', args),

  // Service management
  startService: (opts) => ipcRenderer.invoke('service:start', opts),
  stopService: (opts) => ipcRenderer.invoke('service:stop', opts),

  // Events
  onEvent: (callback) => ipcRenderer.on('conduit:event', callback),

  // Version
  getVersion: () => ipcRenderer.invoke('conduit:version'),
})
```

---

## 4. Policy Engine (Complete)

**Location**: `internal/policy/engine.go`

### 4.1 Implementation Status: COMPLETE

| Feature | Status |
|---------|--------|
| Permission categories | Implemented |
| Built-in blocklist rules | Implemented |
| Forbidden path checking | Implemented |
| User grants evaluation | Implemented |
| Decision recording | Implemented |
| Policy API | Implemented |

### 4.2 Permission Model

**User-facing permissions**:

| Category | Options |
|----------|---------|
| Files | No access / Read-only folders / Read-write folders |
| Network | No network / Outbound only / Outbound with allowlist |
| Secrets | Named secrets (explicit binding) |
| Exposure | Not exposed / Secure Link (future) |

### 4.3 Decision Flow

```go
func (e *PolicyEngine) Evaluate(ctx, request) (*Decision, error) {
    // Phase 1: Built-in rules
    if decision := e.evaluateBuiltinRules(request); decision != nil {
        return decision, nil
    }

    // Phase 2: Forbidden paths
    if decision := e.checkForbiddenPaths(request); decision != nil {
        return decision, nil
    }

    // Phase 3: User grants
    return e.evaluateUserGrants(ctx, request)
}
```

---

## 5. Consent Ledger (Partial)

**Location**: `internal/store/store.go` (Migration 001)

### 5.1 Implementation Status: SCHEMA ONLY

| Component | Status |
|-----------|--------|
| Database schema | Implemented |
| Hash chaining | Schema ready |
| Write operations | Not implemented |
| Read/audit API | Not implemented |
| UI integration | Not implemented |

### 5.2 Schema

```sql
CREATE TABLE consent_ledger (
    entry_id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    action TEXT NOT NULL,
    details TEXT,
    prev_hash TEXT,
    entry_hash TEXT NOT NULL,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 5.3 Planned Events

| Event Type | Entity | Action |
|------------|--------|--------|
| permission_granted | instance | grant_filesystem_read |
| permission_revoked | instance | revoke_network |
| connector_installed | instance | install |
| binding_created | binding | bind_client |
| secure_link_enabled | instance | enable_exposure |

---

## 6. Update System

### 6.1 CLI Bundling

The desktop app bundles the CLI binary:

**Location**: `scripts/bundle-cli.js`

```javascript
// Bundle process:
// 1. Copy conduit binary to resources/bin/
// 2. Create manifest.json with version info
// 3. Package with electron-builder
```

**Manifest** (`resources/bin/manifest.json`):
```json
{
  "version": "1.0.42",
  "bundled_at": "2026-01-10T00:00:00Z",
  "platform": "darwin",
  "arch": "arm64"
}
```

### 6.2 Version Compatibility

```json
// package.json
{
  "conduit": {
    "minCLIVersion": "0.1.0",
    "bundledCLIVersion": "1.0.42",
    "compatibilityNotes": "Requires CLI 0.1.0 or later"
  }
}
```

### 6.3 Auto-Updater (Planned)

Using `electron-updater` for:
- App updates (GUI)
- CLI updates (re-bundle)
- First-party container image updates

---

## 7. Key Flows

### 7.1 KB Onboarding (Implemented)

```
User opens Conduit Desktop
        │
        ▼
┌───────────────────┐
│  Setup Wizard     │
│  1. Add sources   │
│  2. Index docs    │
│  3. Configure MCP │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│  KB View          │
│  - Sources list   │
│  - Sync status    │
│  - Stats          │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│  Test Search      │
│  - Query input    │
│  - Results        │
│  - Citations      │
└───────────────────┘
```

### 7.2 MCP Configuration (Implemented)

```
User: conduit mcp configure --client claude-code
        │
        ▼
┌───────────────────┐
│  Detect client    │
│  installation     │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│  Generate config  │
│  {                │
│    "mcpServers":  │
│      "conduit-kb" │
│  }                │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│  Write config     │
│  ~/.claude.json   │
└─────────┬─────────┘
          │
          ▼
┌───────────────────┐
│  Validate         │
│  connection       │
└───────────────────┘
```

### 7.3 Health Dashboard (Implemented)

Dashboard shows:
- Daemon status (running/stopped)
- Container runtime (Podman/Docker)
- Qdrant status (for semantic search)
- FalkorDB status (for KAG)
- Ollama status (for embeddings)
- KB statistics (sources, documents, vectors)

---

## 8. Departures from Original V1 HLD

### 8.1 Simplified Scope

| Original HLD | Current Implementation |
|--------------|----------------------|
| Full connector store | Focus on first-party KB |
| Rich permissions UI | Basic permission display |
| Consent ledger integration | Schema only |
| Signed artifact verification | Not implemented |
| Adapter validation harness | Manual testing |

### 8.2 Focus Shift

The V1 implementation prioritizes:
1. **Private KB experience** over connector marketplace
2. **CLI-first** with GUI as optional cockpit
3. **Local-only** operation without remote access
4. **Developer audience** over general users

---

## 9. Future Roadmap

### Phase 1: V1.x Polish

- [ ] Full consent ledger integration
- [ ] Richer KB management UI
- [ ] Improved diagnostics display
- [ ] Better error handling/recovery

### Phase 2: V1.5 Features

- [ ] Connector store UI
- [ ] Permission grant/revoke UI
- [ ] Export/import KB
- [ ] Multi-KB support

### Phase 3: V2 Vision

- [ ] Secure Link implementation
- [ ] Team collaboration
- [ ] Cloud backup (optional)
- [ ] Enterprise admin controls

---

## 10. Version History

| Version | Date | Changes |
|---------|------|---------|
| Original | Dec 2025 | Initial V1 HLD |
| Updated | Jan 2026 | Documented actual implementation, focus shift |

---

## See Also

- [HLD V0 — Core Engine](HLD-V0-Core-Engine.md)
- [HLD V0.5 — Secure Link](HLD-V0.5-Secure-Link.md)
- [Implementation Status](IMPLEMENTATION_STATUS.md)
