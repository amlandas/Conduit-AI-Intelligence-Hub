# Conduit V0 - Implementation Outcome Document

**Version**: 0.1.0
**Date**: December 2025
**Status**: Complete

---

## Executive Summary

Conduit V0 is the foundational release of the AI Intelligence Hub - a local-first, security-first platform that connects AI clients (Claude Code, Cursor, VS Code, Gemini CLI) to external tools via MCP (Model Context Protocol) servers. This release establishes the core architecture, security model, and essential features for managing AI connector lifecycles.

**Key Highlights**:
- One-click installation with automated dependency setup
- Daemon service management (launchd/systemd integration)
- Local AI integration with Ollama
- Complete CLI for installation, service management, and operations

---

## Implemented Features

### 1. Installation & Setup (`scripts/`)

| Feature | Description | Status |
|---------|-------------|--------|
| One-Click Installer | Bash script for automated installation | Complete |
| Dependency Detection | Checks for Go, Git, Docker/Podman, Ollama, document tools | Complete |
| Dependency Installation | Installs missing dependencies interactively | Complete |
| Document Tools Installation | Installs pdftotext, antiword, unrtf for document indexing | Complete |
| Interactive Runtime Selection | User choice between Docker/Podman with platform recommendations | Complete |
| Interactive AI Provider Selection | Choice between Ollama (local) and Anthropic API (cloud) | Complete |
| Platform-Specific Handling | macOS (Homebrew) and Linux-specific installation paths | Complete |
| PATH Configuration | Automatic shell detection and PATH setup with duplicate checking | Complete |
| Daemon Service Setup | launchd (macOS) / systemd (Linux) integration | Complete |
| AI Model Setup | Downloads default Ollama model (qwen2.5-coder:7b) | Complete |
| Uninstall Wizard | Complete removal with smart dependency management | Complete |
| stdin Redirection Pattern | Support for `curl \| bash` execution with `/dev/tty` | Complete |

**Installation Command**:
```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
```

**Uninstallation Command**:
```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/uninstall.sh | bash
```

**Service Management**:
- `conduit service install` - Install daemon as system service
- `conduit service start` - Start the daemon
- `conduit service stop` - Stop the daemon
- `conduit service status` - Check service status
- `conduit service remove` - Remove the service

**Installation Features**:
- **Interactive Menus**: Clear choices for container runtime (Docker/Podman) and AI provider (Ollama/Anthropic)
- **Platform Detection**: Automatically detects macOS or Linux and adjusts installation methods
- **Smart Defaults**: Recommends Podman for Linux, Docker for macOS
- **Error Recovery**: Graceful handling of missing dependencies with clear guidance
- **Service Verification**: Validates daemon, runtime, and Ollama are running after installation

**Uninstallation Features**:
- **Smart Runtime Detection**: Reads Conduit config to identify which runtime (Docker/Podman) was actually used
- **Selective Removal**: Only removes the container runtime that Conduit was using
- **Model-Specific Detection**: Identifies and offers to remove qwen2.5-coder:7b model specifically
- **Graceful Errors**: Continues with remaining components if one fails to uninstall
- **Shell Config Cleanup**: Removes PATH entries and creates backups of modified shell configs
- **Interactive Prompts**: User control over each component removal (service, binaries, data, dependencies)

### 2. Daemon Core (`internal/daemon/`)

| Component | Description | Status |
|-----------|-------------|--------|
| Unix Socket IPC | Low-latency communication via `/Users/<user>/.conduit/conduit.sock` | Complete |
| HTTP API | RESTful endpoints for all operations | Complete |
| Background Services | Health monitoring, lifecycle management | Complete |
| Graceful Shutdown | Signal handling (SIGINT, SIGTERM) with cleanup | Complete |

**Key Endpoints**:
- `GET /api/v1/health` - Daemon health check
- `GET /api/v1/instances` - List connector instances
- `POST /api/v1/instances` - Create new instance
- `POST /api/v1/instances/{id}/start` - Start instance
- `POST /api/v1/instances/{id}/stop` - Stop instance
- `DELETE /api/v1/instances/{id}` - Remove instance

### 3. Container Runtime Abstraction (`internal/runtime/`)

| Feature | Description | Status |
|---------|-------------|--------|
| Provider Detection | Auto-detect Podman or Docker | Complete |
| Podman Support | Preferred for rootless operation | Complete |
| Docker Support | Fallback support | Complete |
| Container Lifecycle | Run, stop, remove, status operations | Complete |
| Image Management | Pull, inspect, list images | Complete |
| Image Building | Build containers from Dockerfile with streaming output | Complete |
| Interactive Mode | Run containers with stdin/stdout for MCP stdio | Complete |
| Log Streaming | Stream container logs in real-time | Complete |

**Security Defaults**:
- Read-only root filesystem
- All capabilities dropped
- No new privileges flag
- Network isolation (default: none)

**Build Features**:
- Supports Dockerfile or custom path
- Streaming build output with progress callback
- Build arguments and no-cache options
- Automatic runtime selection (Podman preferred)

### 4. Policy Engine (`internal/policy/`)

| Feature | Description | Status |
|---------|-------------|--------|
| Permission Evaluation | ALLOW/WARN/DENY decisions | Complete |
| Forbidden Path Blocking | Automatic blocking of sensitive paths | Complete |
| User Grants | Explicit permission grants per instance | Complete |
| Audit Logging | All decisions logged with IDs | Complete |

**Forbidden Paths** (always blocked):
- `/` (root filesystem)
- `/etc`, `/var`, `/root` (system directories)
- `~/.ssh`, `~/.aws`, `~/.gnupg` (credentials)
- `~/.config/gcloud`, `~/.azure`, `~/.kube` (cloud credentials)

**Allowed Exceptions**:
- `/tmp`, `/var/folders` (temp directories)

### 5. Lifecycle Manager (`internal/lifecycle/`)

| Feature | Description | Status |
|---------|-------------|--------|
| State Machine | Full instance lifecycle management | Complete |
| Concurrent Operations | Thread-safe with mutex protection | Complete |
| Operation Tracking | Long-running operations with progress | Complete |
| Health Monitoring | Periodic health checks | Complete |

**State Machine**:
```
CREATED → AUDITING → INSTALLED → STARTING → RUNNING
                  ↘              ↗        ↘
                   BLOCKED      STOPPED    DEGRADED
                        ↘         ↓
                         DISABLED → REMOVING → REMOVED
```

### 6. Client Adapters (`internal/adapters/`)

| Adapter | Config Path | Status |
|---------|-------------|--------|
| Claude Code | `~/.claude.json` | Complete |
| Cursor | `~/.cursor/mcp.json` | Complete |
| VS Code | `~/.vscode/mcp.json` | Complete |
| Gemini CLI | `~/.gemini/mcp.json` | Complete |

**Features**:
- MCP server injection into client configs
- Automatic backup before modifications
- Rollback support via change sets
- Validation of injected configurations

### 7. Knowledge Base (`internal/kb/`)

| Component | Description | Status |
|-----------|-------------|--------|
| Document Indexer | Full-text indexing with FTS5 | Complete |
| Document Extractors | Multi-format text extraction (PDF, DOC, DOCX, ODT, RTF) | Complete |
| Chunker | Smart content-aware chunking | Complete |
| Searcher | BM25 ranked search | Complete |
| Source Manager | Directory/file source management | Complete |
| MCP Server | KB exposed as MCP tool | Complete |
| **Semantic Search** | Vector-based search with embeddings | Complete |
| **Vector Store** | Qdrant integration for cosine similarity | Complete |
| **Embedding Service** | Ollama nomic-embed-text (768-dim) | Complete |
| **Hybrid Search** | RRF fusion of semantic + FTS5 | Complete |
| **Content Cleaner** | Pre-indexing boilerplate/OCR cleanup | Complete |
| **MMR Diversity** | Maximal Marginal Relevance (λ=0.7) | Complete |
| **Entity Boosting** | Proper noun detection and boosting | Complete |
| **Reranking** | Semantic re-scoring of top results | Complete |

**Supported Document Formats**:
- Text: `.md`, `.txt`, `.rst`
- Code: `.go`, `.py`, `.js`, `.ts`, `.java`, `.rs`, `.rb`, `.c`, `.cpp`, `.h`, `.hpp`, `.cs`, `.swift`, `.kt`
- Scripts: `.sh`, `.bash`, `.zsh`, `.fish`, `.ps1`, `.bat`, `.cmd`
- Config: `.json`, `.yaml`, `.yml`, `.xml`, `.jsonld`, `.toml`, `.ini`, `.cfg`
- Data: `.csv`, `.tsv`
- Documents: `.pdf`, `.doc`, `.docx`, `.odt`, `.rtf`

**Document Extraction Tools**:
- `pdftotext` (poppler) - PDF text extraction
- `textutil` (macOS built-in) - DOC/RTF extraction
- `antiword` (Linux/Windows) - DOC extraction
- `unrtf` (Linux) - RTF extraction
- Native Go - DOCX and ODT (ZIP+XML parsing)

**Search Capabilities**:
- Full-text search with SQLite FTS5
- BM25 relevance ranking
- Snippet extraction with highlighting
- Source and document filtering
- **Semantic search** with Qdrant vector database
- **Hybrid RRF search** (reciprocal rank fusion of semantic + lexical)
- **Query-adaptive mode selection** (auto-detects query type)
- **MMR diversity** (reduces redundant results, λ=0.7)
- **Entity boosting** (proper noun detection and ranking boost)
- **Semantic reranking** (re-scores top candidates)
- **Search mode flags**: `--semantic`, `--fts5`, `--hybrid` (default: auto)
- **Migration command**: `conduit kb migrate` for existing documents
- **See**: `docs/KB_SEARCH_HLD.md` for detailed architecture

### 8. Data Store (`internal/store/`)

| Feature | Description | Status |
|---------|-------------|--------|
| SQLite Backend | Embedded database | Complete |
| FTS5 Support | Full-text search extension | Complete |
| Migration System | Version-controlled schema migrations | Complete |
| Transaction Support | ACID compliance | Complete |

**Database Tables**:
- `connector_instances` - Instance metadata and state
- `client_bindings` - Client-to-instance mappings
- `config_backups` - Configuration backup records
- `user_grants` - Permission grants
- `kb_sources` - Document sources
- `kb_documents` - Indexed documents
- `kb_chunks` - Document chunks for search
- `kb_chunks_fts` - FTS5 virtual table

### 9. CLI (`cmd/conduit/`)

| Command Group | Commands | Status |
|---------------|----------|--------|
| Setup | `setup`, `install-deps`, `doctor`, `uninstall` | Complete |
| Service | `service install`, `service start`, `service stop`, `service status`, `service remove` | Complete |
| Instance | `install`, `list`, `start`, `stop`, `remove`, `logs` | Complete |
| Client | `client list`, `client bind`, `client unbind`, `client bindings` | Complete |
| Knowledge Base | `kb add`, `kb list`, `kb sync`, `kb search`, `kb stats`, `kb remove`, `kb migrate` | Complete |
| MCP | `mcp stdio`, `mcp kb` | Complete |
| System | `status`, `config`, `backup` | Complete |

**CLI Commands in Detail**:
- `conduit install <url>` - AI-powered MCP server installation with Docker/Podman build
- `conduit logs <instance>` - View container and stored logs with `--follow`, `--tail`
- `conduit config` - Display configuration with `--all` for full details
- `conduit backup` - Create tar.gz backup of data directory
- `conduit doctor` - Comprehensive diagnostics with verbose mode
- `conduit mcp stdio --instance <id>` - Run MCP server over stdio for AI client integration
- `conduit mcp kb` - Run knowledge base MCP server

### 10. Observability (`internal/observability/`)

| Feature | Description | Status |
|---------|-------------|--------|
| Structured Logging | JSON format with zerolog | Complete |
| Component Tagging | Log entries tagged by component | Complete |
| Event Logging | Lifecycle events tracked | Complete |
| Debug Mode | Verbose logging option | Complete |

---

## Architecture Decisions

### 1. Go as Primary Language
- **Rationale**: Single binary distribution, excellent concurrency, strong typing
- **Trade-off**: CGO required for SQLite FTS5

### 2. SQLite with FTS5
- **Rationale**: Local-first, no external dependencies, full-text search built-in
- **Trade-off**: Requires `-tags "fts5"` build flag and CGO

### 3. Unix Socket for IPC
- **Rationale**: Lower latency than TCP, file-system security model
- **Trade-off**: Not directly accessible over network

### 4. Podman-First Container Strategy
- **Rationale**: Rootless by default, better security model
- **Trade-off**: Requires Podman installation

### 5. Policy-Based Security
- **Rationale**: Declarative, auditable, extensible
- **Trade-off**: Requires user consent flow for permissions

---

## Test Coverage

| Package | Tests | Status |
|---------|-------|--------|
| `internal/adapters` | Registry, base adapter | Pass |
| `internal/config` | Config loading, defaults | Pass |
| `internal/installer` | Installer, service management | Pass |
| `internal/kb` | Chunker, indexer, searcher, source | Pass |
| `internal/lifecycle` | Manager, state machine | Pass |
| `internal/policy` | Engine, rules, grants | Pass |
| `internal/runtime` | Provider detection | Pass |
| `internal/store` | Store, migrations | Pass |
| `pkg/models` | Errors | Pass |
| `tests/integration` | End-to-end workflows | Pass |

**Total**: 100+ tests, all passing

---

## File Structure

```
conduit/
├── cmd/
│   ├── conduit/              # CLI application
│   │   └── main.go           # CLI with all commands
│   └── conduit-daemon/       # Background daemon
│       └── main.go
├── internal/
│   ├── adapters/             # Client adapters
│   │   ├── registry.go
│   │   ├── claude.go
│   │   ├── cursor.go
│   │   ├── vscode.go
│   │   └── gemini.go
│   ├── config/               # Configuration management
│   │   └── config.go
│   ├── daemon/               # Daemon core
│   │   ├── daemon.go
│   │   └── handlers.go
│   ├── installer/            # Dependency installer
│   │   ├── installer.go      # Dependency detection & installation
│   │   └── installer_test.go
│   ├── kb/                   # Knowledge base
│   │   ├── indexer.go
│   │   ├── searcher.go
│   │   ├── chunker.go
│   │   ├── source.go
│   │   ├── extractors.go     # Document format extractors
│   │   ├── types.go          # Types and default patterns
│   │   ├── embeddings.go     # Ollama embedding service
│   │   ├── vectorstore.go    # Qdrant vector store
│   │   ├── semantic_search.go # Semantic search with Qdrant
│   │   ├── hybrid_search.go  # Hybrid RRF fusion + MMR + reranking
│   │   ├── content_cleaner.go # Pre-indexing content cleanup
│   │   ├── retrieval_test_suite.go # Quality validation tests
│   │   └── mcp.go
│   ├── lifecycle/            # Instance lifecycle
│   │   ├── manager.go
│   │   └── types.go
│   ├── observability/        # Logging
│   │   └── logging.go
│   ├── policy/               # Security policy engine
│   │   ├── engine.go
│   │   └── types.go
│   ├── runtime/              # Container runtime
│   │   └── provider.go
│   └── store/                # Data persistence
│       ├── store.go
│       └── migrations.go
├── pkg/
│   └── models/               # Shared types
│       └── errors.go
├── scripts/
│   ├── install.sh            # One-click installation script (macOS/Linux)
│   ├── install-windows.ps1   # Windows installation script (PowerShell)
│   └── uninstall.sh          # Complete uninstallation script
├── tests/
│   └── integration/          # Integration tests
├── docs/                     # Documentation
├── bin/                      # Build output (created by make)
├── Makefile                  # Build configuration
├── go.mod                    # Go module definition
└── README.md                 # Project overview
```

---

## Dependencies

| Dependency | Version | Purpose |
|------------|---------|---------|
| `go-chi/chi` | v5.2.3 | HTTP router |
| `google/uuid` | v1.6.0 | UUID generation |
| `mattn/go-sqlite3` | v1.14.32 | SQLite driver with FTS5 |
| `rs/zerolog` | v1.34.0 | Structured logging |
| `spf13/cobra` | v1.10.2 | CLI framework |
| `spf13/viper` | v1.21.0 | Configuration management |
| `qdrant/go-client` | v1.14.0 | Qdrant vector database client |
| `ollama/ollama` | v0.5.7 | Ollama API client for embeddings |

---

## Bug Fixes & Improvements

During user testing, several critical bugs were identified and resolved:

### Installation Script Fixes

1. **Repository URL Fix** (Bug #1)
   - **Issue**: Installation failed with "repository not found" error
   - **Cause**: Script referenced placeholder URL `simpleflo/conduit` instead of actual repository
   - **Fix**: Updated to `amlandas/Conduit-AI-Intelligence-Hub`

2. **PATH Recognition Fix** (Bug #2)
   - **Issue**: Commands not recognized after installation on macOS
   - **Cause**: PATH not properly updated in current shell session
   - **Fix**: Added duplicate checking, prominent warnings, and clear instructions to source shell config or restart terminal

3. **Interactive Input Fix** (Bug #3)
   - **Issue**: Installer exited without accepting user input during interactive prompts
   - **Cause**: When running via `curl | bash`, stdin is the pipe, not the terminal
   - **Fix**: Redirected all `read` commands to use `/dev/tty` instead of stdin
   - **Code Pattern**: `read -r -p "$prompt" response </dev/tty`

4. **macOS Ollama Installation Fix** (Bug #4)
   - **Issue**: Ollama installation failed on macOS with "Linux only" error
   - **Cause**: Official Ollama script only supports Linux
   - **Fix**: Platform-specific installation using Homebrew for macOS, official script for Linux

### UX Enhancements

1. **Interactive Menus**: Added user-friendly menus for:
   - Container runtime selection (Docker vs Podman)
   - AI provider selection (Ollama vs Anthropic API)
   - Platform-specific recommendations shown for each choice

2. **Service Status Clarity**: Improved status reporting to distinguish between:
   - "not installed"
   - "not running"
   - "running"

3. **Smart Uninstallation**:
   - Detects which runtime Conduit actually used (from config file)
   - Only offers to remove the runtime that was in use
   - Specifically identifies qwen2.5-coder:7b model instead of generic "models"

### Remote Installation Fixes (January 2026)

5. **Daemon PATH Configuration** (Bug #5)
   - **Issue**: Daemon service couldn't find pdftotext and other tools on remote machines
   - **Cause**: launchd (macOS) and systemd (Linux) services don't inherit user's shell PATH
   - **Fix**: Added explicit PATH to service configurations including `/opt/homebrew/bin` and `/usr/local/bin`

6. **CLI Panic on Sync Response** (Bug #6)
   - **Issue**: CLI crashed with panic during `kb sync` when daemon returned unexpected response
   - **Cause**: Unsafe type assertions on response fields that could be nil
   - **Fix**: Added nil-safe type assertions with comma-ok pattern

7. **Docker Credential Helper Issue** (Bug #7)
   - **Issue**: Qdrant container failed to start during installation via SSH or launchd
   - **Cause**: `docker-credential-gcloud` configured in Docker config but not available in PATH
   - **Fix**: Install script temporarily disables credential helpers during container operations

8. **Qdrant Container Recreation** (Bug #8)
   - **Issue**: After uninstall/reinstall, Qdrant container had invalid volume mount
   - **Cause**: Container restarted instead of recreated when `~/.conduit/qdrant` was deleted
   - **Fix**: Install script always removes and recreates container for fresh state

9. **Qdrant Container Removal** (Bug #9)
   - **Issue**: Orphaned Qdrant container after uninstall caused issues on reinstall
   - **Cause**: Uninstall script didn't handle Qdrant container
   - **Fix**: Added `remove_qdrant_container` step to uninstall script

10. **Invalid UTF-8 Panic** (Bug #10)
    - **Issue**: Vector store panicked when content contained invalid UTF-8 sequences
    - **Cause**: Qdrant client's `NewValueMap` requires valid UTF-8 strings
    - **Fix**: Added `sanitizeUTF8()` function to replace invalid sequences with Unicode replacement character

---

## Known Limitations (V0)

1. **No Audit Subsystem**: Audit stage is placeholder (passes through)
2. **No Package Registry**: Manual image specification required
3. **No Secret Management**: Secrets must be provided via environment
4. **No Remote API**: Unix socket only, no network access
5. **No Web UI**: CLI only
6. **No Automatic Updates**: Manual update process (one-click reinstall available)
7. **Single User**: No multi-tenancy support
8. **Windows Support**: PowerShell installation script available, but less tested than macOS/Linux

---

## Performance Characteristics

| Metric | Value |
|--------|-------|
| Daemon Startup | < 500ms |
| Instance Create | < 100ms |
| Search Query (1000 docs) | < 50ms |
| Health Check | < 10ms |
| Binary Size | ~15MB |
| Memory (idle) | ~20MB |

---

## Security Model Summary

### Defense in Depth Layers

1. **Container Isolation**: Rootless containers, dropped capabilities
2. **Filesystem Protection**: Forbidden paths, read-only root
3. **Network Isolation**: Default network mode is "none"
4. **Permission System**: Explicit user grants required
5. **Audit Trail**: All decisions logged with unique IDs

### Threat Mitigations

| Threat | Mitigation |
|--------|------------|
| Credential Theft | Forbidden path blocking |
| Privilege Escalation | Rootless containers, no-new-privileges |
| Network Exfiltration | Default network isolation |
| Filesystem Tampering | Read-only root filesystem |
| Malicious Connectors | Policy evaluation before execution |

---

## V1 Roadmap

### V1.0 Focus: macOS Desktop Application

| Feature | Status | Description |
|---------|--------|-------------|
| **Desktop App** | 🔵 Designed | Native macOS app with Electron + React + shadcn/ui |
| **SSE Events** | 📋 Planned | Real-time daemon-UI sync via Server-Sent Events |
| **Mode System** | 📋 Planned | Default/Advanced/Developer tier modes |
| **Dependency Dashboard** | 📋 Planned | Status of Ollama, Qdrant, FalkorDB, container runtime |

**Design Document**: See `~/.claude/plans/zippy-riding-lemur.md` for full V1 UI design.

### Recently Completed (Post-V0)

| Feature | Status | Description |
|---------|--------|-------------|
| **KAG Entity Search** | ✅ Complete | Semantic entity search with RRF fusion |
| **Entity Vectorization** | ✅ Complete | Entities stored in Qdrant `conduit_entities` collection |
| **Entity Deduplication** | ✅ Complete | Normalized name + type deduplication |
| **Hybrid Search for KB** | ✅ Complete | RRF fusion enabled in MCP server |

### Future (V1.1+)

| Feature | Priority | Description |
|---------|----------|-------------|
| Audit Subsystem | Medium | Static analysis of connector packages |
| Package Registry | Medium | Curated connector marketplace |
| Secret Management | Medium | Integration with system keychains |
| Remote API | Low | Optional HTTPS endpoint (Developer mode) |
| Multi-User | Low | Workspace isolation |
| Windows/Linux Desktop | Low | Cross-platform desktop support |
| Metrics Export | Low | Prometheus/OpenTelemetry integration |

---

## Conclusion

Conduit V0 establishes a solid foundation for secure AI connector management. The architecture is designed for extensibility, with clear separation of concerns and comprehensive test coverage. The security-first approach ensures that AI tools operate within well-defined boundaries while remaining useful and accessible.
