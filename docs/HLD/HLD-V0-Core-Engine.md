# High-Level Design V0 — Conduit Core Engine

**Version**: 1.0.42 (Updated)
**Status**: IMPLEMENTED
**Last Updated**: January 2026

This document describes the core architecture of Conduit as implemented in V1.0.

---

## Executive Summary

Conduit V0 delivers a **CLI-first private knowledge base** for AI coding tools. The core engine provides:

- **Conduit Daemon** — Long-running local service, single source of truth
- **Conduit CLI** — Thin client that calls daemon API
- **RuntimeProvider** — Container runtime abstraction (Podman/Docker)
- **Knowledge Base Subsystem** — Hybrid search with FTS5 + Semantic + KAG
- **Client Adapters** — Configuration injection for AI tools
- **Policy Engine** — Security policy evaluation and enforcement

---

## 1. System Architecture

### 1.1 Architecture Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                              User Machine                                │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                         │
│  ┌─────────────┐     ┌─────────────┐     ┌─────────────────────────┐   │
│  │ Conduit CLI │────▶│   Daemon    │────▶│   RuntimeProvider       │   │
│  └─────────────┘     │   (HTTP)    │     │  ┌─────────┬─────────┐  │   │
│                      └──────┬──────┘     │  │ Podman  │ Docker  │  │   │
│  ┌─────────────┐            │            │  └─────────┴─────────┘  │   │
│  │ Desktop GUI │────────────┤            └─────────────────────────┘   │
│  └─────────────┘            │                                           │
│                             ▼                                           │
│              ┌──────────────────────────────────────┐                   │
│              │          Core Services               │                   │
│              ├──────────────────────────────────────┤                   │
│              │  ┌────────────┐  ┌────────────────┐  │                   │
│              │  │   Policy   │  │ Client Adapters│  │                   │
│              │  │   Engine   │  │ (4 adapters)   │  │                   │
│              │  └────────────┘  └────────────────┘  │                   │
│              │  ┌────────────────────────────────┐  │                   │
│              │  │     Knowledge Base System      │  │                   │
│              │  │  ┌──────┐ ┌────────┐ ┌─────┐  │  │                   │
│              │  │  │ FTS5 │ │Semantic│ │ KAG │  │  │                   │
│              │  │  └──────┘ └────────┘ └─────┘  │  │                   │
│              │  └────────────────────────────────┘  │                   │
│              └──────────────────────────────────────┘                   │
│                             │                                           │
│              ┌──────────────┴──────────────┐                           │
│              │        Data Stores          │                           │
│              │  ┌───────┐ ┌──────┐ ┌─────┐ │                           │
│              │  │SQLite │ │Qdrant│ │Falkor│ │                           │
│              │  └───────┘ └──────┘ └─────┘ │                           │
│              └─────────────────────────────┘                           │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
                 ┌─────────────────────────┐
                 │      AI Clients         │
                 │ ┌───────┐ ┌───────────┐ │
                 │ │Claude │ │Cursor/VS  │ │
                 │ │ Code  │ │  Code     │ │
                 │ └───────┘ └───────────┘ │
                 │ ┌───────┐               │
                 │ │Gemini │               │
                 │ │ CLI   │               │
                 │ └───────┘               │
                 └─────────────────────────┘
```

---

## 2. Core Components

### 2.1 Conduit Daemon (Orchestrator)

**Location**: `internal/daemon/`

**Responsibilities**:
- Owns all state (instances, KB sources, bindings)
- Exposes local HTTP API over Unix socket
- Manages connector/KB lifecycle
- Coordinates runtime operations
- Streams SSE events for real-time updates

**Implementation**:
- Single Go process with internal worker pool
- SQLite database for persistence (with migrations)
- Unix domain socket: `~/.conduit/conduit.sock`
- OS integration for autostart via launchd/systemd

**API Endpoints** (implemented):
```
GET  /api/v1/status              - Daemon health and status
GET  /api/v1/instances           - List connector instances
POST /api/v1/instances           - Create instance
POST /api/v1/instances/:id/start - Start instance
POST /api/v1/instances/:id/stop  - Stop instance
GET  /api/v1/kb/sources          - List KB sources
POST /api/v1/kb/sources          - Add KB source
POST /api/v1/kb/sync             - Sync KB sources
POST /api/v1/kb/search           - Search KB
GET  /api/v1/events              - SSE event stream
POST /api/v1/doctor              - Run diagnostics
```

---

### 2.2 RuntimeProvider (Container Abstraction)

**Location**: `internal/runtime/`

**Purpose**: Abstract container runtime for security isolation.

**Interface Contract**:
```go
type Provider interface {
    Name() string
    Version() (string, error)
    Available() bool
    Build(ctx, spec) (string, error)
    Pull(ctx, image) error
    Run(ctx, spec) (string, error)
    Stop(ctx, containerID) error
    Remove(ctx, containerID) error
    Status(ctx, containerID) (*ContainerStatus, error)
    Logs(ctx, containerID, opts) (io.ReadCloser, error)
    Exec(ctx, containerID, cmd) (*ExecResult, error)
    Inspect(ctx, containerID) (*ContainerInfo, error)
    RunInteractive(ctx, spec) error
}
```

**Runtime Selection Algorithm**:
1. Check if Docker Desktop is installed and running → use DockerProvider
2. Else check for Podman → use PodmanProvider
3. If neither available → install Podman (rootless)

**Security Defaults** (applied to all containers):
- `--read-only` — Read-only root filesystem
- `--cap-drop=ALL` — Drop all Linux capabilities
- `--security-opt=no-new-privileges` — Prevent privilege escalation
- `--network=none` (unless egress needed)
- No inbound port exposure in V0
- Non-root user inside container

---

### 2.3 Knowledge Base Subsystem

**Location**: `internal/kb/` (13,500+ lines of code)

This is the most sophisticated component, significantly exceeding original HLD scope.

#### 2.3.1 Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                    KB Subsystem Architecture                     │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌────────────────┐                                             │
│  │ Source Manager │ ─── Directory/file source registration     │
│  └───────┬────────┘                                             │
│          │                                                       │
│          ▼                                                       │
│  ┌────────────────┐     ┌─────────────────┐                     │
│  │    Chunker     │────▶│ Content Cleaner │                     │
│  │ (Smart split)  │     │ (OCR/preproc)   │                     │
│  └───────┬────────┘     └─────────────────┘                     │
│          │                                                       │
│          ▼                                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                    Indexing Pipeline                        │ │
│  │  ┌──────────────┐  ┌────────────────┐  ┌────────────────┐  │ │
│  │  │   SQLite     │  │    Qdrant      │  │   FalkorDB     │  │ │
│  │  │   (FTS5)     │  │  (Vectors)     │  │   (Graph)      │  │ │
│  │  │ Full-text    │  │  Semantic      │  │    KAG         │  │ │
│  │  └──────────────┘  └────────────────┘  └────────────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│          │                                                       │
│          ▼                                                       │
│  ┌────────────────────────────────────────────────────────────┐ │
│  │                   Hybrid Searcher                           │ │
│  │  ┌──────────┐  ┌────────────┐  ┌────────────┐  ┌────────┐  │ │
│  │  │  Query   │─▶│  Strategy  │─▶│    RRF     │─▶│  MMR   │  │ │
│  │  │Classify  │  │  Weights   │  │  Fusion    │  │Rerank  │  │ │
│  │  └──────────┘  └────────────┘  └────────────┘  └────────┘  │ │
│  └────────────────────────────────────────────────────────────┘ │
│          │                                                       │
│          ▼                                                       │
│  ┌────────────────┐                                             │
│  │   MCP Server   │ ─── Exposed to AI clients                   │
│  └────────────────┘                                             │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

#### 2.3.2 Search Modes

| Mode | Description | Implementation |
|------|-------------|----------------|
| **Hybrid** | FTS5 + Semantic with RRF fusion | Default mode |
| **Semantic** | Vector similarity only | `--semantic` flag |
| **FTS5** | Full-text keyword search only | `--fts5` flag |

#### 2.3.3 Key Components

**SourceManager** (`source.go`):
- Directory/file source registration
- Include/exclude patterns
- Sync state tracking
- Document change detection

**Chunker** (`chunker.go`, 22KB):
- Content-aware splitting
- Respects code blocks, paragraphs
- Configurable chunk size (default: 512 tokens)
- Overlap handling

**HybridSearcher** (`hybrid_search.go`, 39KB):
- Query type classification (Exact, Entity, Conceptual, Factual, Exploratory)
- Adaptive strategy weights per query type
- RRF (Reciprocal Rank Fusion) for score combination
- MMR (Maximal Marginal Relevance) for diversity (λ=0.7)
- Semantic reranking of top candidates

**EntityExtractor** (`entity_extractor.go`):
- Named entity extraction via Mistral 7B
- Entity types, relationships, confidence scores
- FalkorDB graph storage

#### 2.3.4 Supported File Formats

| Category | Extensions |
|----------|------------|
| Documentation | `.md`, `.txt`, `.rst`, `.org` |
| Code | `.go`, `.py`, `.js`, `.ts`, `.java`, `.rb`, `.rs`, `.c`, `.cpp`, `.h` |
| Config | `.json`, `.yaml`, `.yml`, `.xml`, `.toml`, `.ini` |
| Documents | `.pdf`, `.doc`, `.docx`, `.odt`, `.rtf` |

---

### 2.4 Client Adapters

**Location**: `internal/adapters/`

**Implemented Adapters**:

| Adapter | Config Location | Transport |
|---------|-----------------|-----------|
| Claude Code | `~/.claude.json`, `.mcp.json` | stdio |
| Cursor | `~/.cursor/mcp.json`, `.cursor/mcp.json` | stdio |
| VS Code | `.vscode/mcp.json` | stdio |
| Gemini CLI | `~/.gemini/settings.json` | stdio |

**Adapter Interface**:
```go
type Adapter interface {
    ID() string
    DisplayName() string
    Version() string
    Detect(ctx) (*DetectResult, error)
    PlanInjection(ctx, plan) (*InjectionPlan, error)
    ApplyInjection(ctx, plan) (*ApplyResult, error)
    Validate(ctx, instanceID) (*ValidationResult, error)
    Rollback(ctx, changeSetID) error
    Doctor(ctx) (*DoctorResult, error)
}
```

**Injection Strategy**:
- Conduit writes to a managed section of config files
- Backups created before any modification
- Change sets tracked for rollback capability
- Validation confirms MCP server connectivity

---

### 2.5 Policy Engine

**Location**: `internal/policy/engine.go` (646 lines)

**Purpose**: Single authority for all security decisions.

**Permission Categories**:

| Category | Options |
|----------|---------|
| Filesystem | `none`, `readonly_paths[]`, `readwrite_paths[]` |
| Network | `none`, `egress` (with domain allowlist) |
| Secrets | Explicit binding required |
| Exposure | Secure Link permission (V0.5+) |

**Forbidden Paths** (always blocked):
- `/` (root)
- System directories: `/etc`, `/var`, `/usr`, `/bin`, `/sbin`, `/lib`
- Credential stores: `~/.ssh`, `~/.aws`, `~/.gnupg`, `~/.kube`
- Full home directory mount

**Decision Flow**:
```
Request → Built-in Rules → Forbidden Path Check → User Grants → Decision
                ↓                    ↓                  ↓           ↓
              Block               Deny              Evaluate   Allow/Warn/Deny
```

---

## 3. Data Model

### 3.1 Core Entities

**ConnectorPackage** (immutable definition):
```go
type ConnectorPackage struct {
    SchemaVersion string
    PackageID     string
    Version       string
    Metadata      PackageMetadata
    Distribution  DistributionSpec
    Permissions   PermissionSpec
    MCP           MCPConfig
    ConfigSchema  map[string]interface{}
    Audit         AuditConfig
}
```

**ConnectorInstance** (installed copy):
```go
type ConnectorInstance struct {
    InstanceID      string
    PackageID       string
    PackageVersion  string
    DisplayName     string
    Status          InstanceStatus
    ContainerID     string
    SocketPath      string
    ImageRef        string
    Config          map[string]string
    GrantedPerms    GrantedPermissions
    AuditResult     *AuditResult
    Health          *HealthStatus
    CreatedAt       time.Time
    UpdatedAt       time.Time
    StartedAt       *time.Time
    StoppedAt       *time.Time
}
```

### 3.2 State Machine

```
                    ┌─────────┐
                    │ CREATED │
                    └────┬────┘
                         │ install()
                         ▼
                    ┌─────────┐
                    │AUDITING │
                    └────┬────┘
                    ╱         ╲
           audit=BLOCK    audit=PASS/WARN
                  ╲           ╱
                   ▼         ▼
            ┌─────────┐  ┌─────────┐
            │ BLOCKED │  │INSTALLED│
            └─────────┘  └────┬────┘
                              │ start()
                              ▼
                         ┌─────────┐
                         │STARTING │
                         └────┬────┘
                         ╱         ╲
                 health ok    health fail
                       ╲           ╱
                        ▼         ▼
                  ┌─────────┐  ┌─────────┐
                  │ RUNNING │◀─│DEGRADED │
                  └────┬────┘  └─────────┘
                       │ stop()
                       ▼
                  ┌─────────┐
                  │ STOPPED │
                  └────┬────┘
                       │ remove()
                       ▼
                  ┌─────────┐
                  │ REMOVED │
                  └─────────┘
```

---

## 4. Storage

### 4.1 Database Schema

**Location**: `~/.conduit/conduit.db` (SQLite)

**Tables**:
- `connector_packages` — Package definitions
- `connector_instances` — Installed instances
- `client_bindings` — Instance ↔ Client mappings
- `kb_sources` — Registered document sources
- `kb_documents` — Indexed documents
- `kb_chunks` — Document chunks for search
- `kb_fts` — FTS5 virtual table
- `kb_entities` — Extracted entities (KAG)
- `kb_relations` — Entity relationships (KAG)
- `user_grants` — User permission grants
- `consent_ledger` — Audit trail (schema ready)

### 4.2 External Data Stores

| Store | Purpose | Container |
|-------|---------|-----------|
| Qdrant | Vector embeddings | `conduit-qdrant` |
| FalkorDB | Knowledge graph | `conduit-falkordb` |

---

## 5. Security Model

### 5.1 Threat Model

All third-party connectors are treated as **untrusted code**.

### 5.2 Default Posture

| Control | Default | Rationale |
|---------|---------|-----------|
| Host filesystem | No access | Prevent data exfiltration |
| Inbound network | Denied | No external access to containers |
| Outbound network | Denied unless declared | Minimize attack surface |
| Privileges | No privileged containers | Reduce escape risk |
| User | Non-root inside container | Limit blast radius |
| Secrets | None by default | Prevent accidental leakage |
| Resource limits | Enabled | Avoid runaway processes |

### 5.3 First-Party KB Server Policy

The Conduit KB MCP server runs with controlled access:
- **Filesystem**: Read-only access to user-selected KB source folders only
- **Network**: Denied (KB server doesn't need network)
- **Secrets**: None
- **Resource limits**: Standard defaults

---

## 6. Observability

### 6.1 Doctor Command

`conduit doctor` performs comprehensive diagnostics:

- Daemon running and responsive
- Database accessible and migrations applied
- Container runtime available (Podman/Docker)
- FTS5 extension loaded
- Qdrant connectivity (if semantic search enabled)
- Ollama availability (for embeddings)
- FalkorDB connectivity (if KAG enabled)
- Client config file writability

### 6.2 Logging

- Daemon logs: `~/.conduit/logs/conduit.log`
- Container logs: Streamed via RuntimeProvider
- Structured JSON format for machine parsing

### 6.3 Events

SSE event stream for real-time updates:
- `instance_status_changed`
- `kb_sync_started`, `kb_sync_progress`, `kb_sync_completed`
- `binding_created`, `binding_deleted`
- `daemon_status` (heartbeat)

---

## 7. Departures from Original HLD

### 7.1 Expanded Scope

| Component | Original HLD | Actual Implementation |
|-----------|-------------|----------------------|
| KB Search | Basic FTS + embeddings | Hybrid with RRF, MMR, reranking, query classification |
| KAG | Not in V0 HLD | Full implementation with FalkorDB, entity extraction |
| Search Modes | Single mode | Hybrid, Semantic-only, FTS-only modes |

### 7.2 Deferred/Simplified

| Component | Original HLD | Current Status |
|-----------|-------------|----------------|
| Auditor | Full static analysis | Data model only, engine not implemented |
| Secrets Manager | OS keychain integration | Not implemented |
| Connector Store | Curated marketplace | Focus on first-party KB server |
| Trust Signals | Community + audit scoring | Not implemented |

### 7.3 Design Evolution

The product focus evolved from "connector marketplace for AI tools" to "private knowledge base for AI tools". This shift explains:
- Heavy investment in KB subsystem (13.5k lines)
- Sophisticated search with hybrid/semantic/KAG
- Simpler connector model (first-party KB server primary)

---

## 8. Version History

| Version | Date | Changes |
|---------|------|---------|
| 0.1.0 | Dec 2025 | Initial implementation |
| 1.0.42 | Jan 2026 | V1 launch with comprehensive KB system |

---

## See Also

- [HLD V0.5 — Secure Link](HLD-V0.5-Secure-Link.md)
- [HLD V1 — Desktop GUI](HLD-V1-Desktop-GUI.md)
- [Implementation Status](IMPLEMENTATION_STATUS.md)
- [KB Search Architecture](../KB_SEARCH_HLD.md)
- [KAG Architecture](../KAG_HLD.md)
