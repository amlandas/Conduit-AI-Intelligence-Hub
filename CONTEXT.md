# Conduit - Claude Code Context Document

**Purpose**: This document provides all necessary context for Claude Code (Opus 4.5 or Sonnet 4.5) to continue development on the Conduit project.

**Last Updated**: December 2025
**Current Version**: V0.1.0 (Complete)

---

## Quick Start for Claude Code

### 1. Project Location
```
/Users/amlandas/workplace/Simpleflo - Conduit AI Intelligence Hub/Simpleflo-Conduit-Dev-CC/Conduit-Dev-ClaudeCode/conduit
```

### 2. Essential Commands
```bash
# Build the project
make build

# Run all tests
make test

# Run daemon
./bin/conduit-daemon --foreground --log-level=debug

# Check status
./bin/conduit status
```

### 3. Key Files to Read First
- `README.md` - Project overview
- `docs/V0_OUTCOME.md` - What's implemented
- `go.mod` - Dependencies
- `Makefile` - Build configuration

---

## Project Overview

### What is Conduit?

Conduit is a **local-first, security-first AI intelligence hub** that:
1. Connects AI clients (Claude Code, Cursor, VS Code, Gemini CLI) to MCP servers
2. Runs connectors in isolated containers (Podman/Docker)
3. Manages permissions with a policy engine
4. Provides a searchable knowledge base with FTS5

### Architecture Summary

```
AI Clients → Unix Socket → Conduit Daemon → Container Runtime
                              │
                              ├── Policy Engine (permissions)
                              ├── Lifecycle Manager (state machine)
                              ├── Client Adapters (config injection)
                              ├── Knowledge Base (FTS5 search)
                              └── SQLite Store (persistence)
```

---

## Codebase Structure

```
conduit/
├── cmd/
│   ├── conduit/              # CLI application
│   │   ├── main.go           # Entry point
│   │   └── commands/         # Cobra command handlers
│   │       ├── root.go
│   │       ├── install.go
│   │       ├── list.go
│   │       ├── start.go
│   │       ├── stop.go
│   │       ├── remove.go
│   │       ├── client.go
│   │       ├── kb.go
│   │       ├── status.go
│   │       └── doctor.go
│   └── conduit-daemon/       # Background daemon
│       └── main.go
│
├── internal/
│   ├── adapters/             # Client adapters (Claude, Cursor, etc.)
│   │   ├── registry.go       # Adapter registry
│   │   ├── base.go           # Base adapter with common logic
│   │   ├── claude.go         # Claude Code adapter
│   │   ├── cursor.go         # Cursor adapter
│   │   ├── vscode.go         # VS Code adapter
│   │   └── gemini.go         # Gemini CLI adapter
│   │
│   ├── config/               # Configuration management
│   │   └── config.go         # Viper-based config loading
│   │
│   ├── daemon/               # Daemon core
│   │   ├── daemon.go         # Main daemon struct and lifecycle
│   │   └── handlers.go       # HTTP API handlers
│   │
│   ├── kb/                   # Knowledge base
│   │   ├── indexer.go        # Document indexing
│   │   ├── searcher.go       # FTS5 search
│   │   ├── chunker.go        # Text chunking
│   │   ├── source.go         # Source management
│   │   ├── types.go          # KB types
│   │   └── mcp.go            # MCP server for KB
│   │
│   ├── lifecycle/            # Instance lifecycle management
│   │   ├── manager.go        # State machine, operations
│   │   └── types.go          # Status enums, transitions
│   │
│   ├── observability/        # Logging
│   │   └── logging.go        # Zerolog configuration
│   │
│   ├── policy/               # Security policy engine
│   │   ├── engine.go         # Permission evaluation
│   │   └── types.go          # Permission types
│   │
│   ├── runtime/              # Container runtime abstraction
│   │   └── provider.go       # Podman/Docker provider
│   │
│   └── store/                # Data persistence
│       ├── store.go          # SQLite wrapper
│       └── migrations.go     # Schema migrations
│
├── pkg/
│   └── models/               # Shared types
│       └── errors.go         # Custom error types
│
├── tests/
│   └── integration/          # Integration tests
│       ├── lifecycle_integration_test.go
│       └── kb_integration_test.go
│
├── docs/                     # Documentation
│   ├── V0_OUTCOME.md         # Implementation summary
│   ├── USER_GUIDE.md         # User documentation
│   ├── ADMIN_GUIDE.md        # Administrator guide
│   └── USE_CASES.md          # Real-world examples
│
├── Makefile                  # Build configuration
├── go.mod                    # Go module definition
├── README.md                 # Project overview
└── CONTEXT.md                # This file
```

---

## Technology Stack

| Component | Technology | Notes |
|-----------|------------|-------|
| Language | Go 1.21+ | CGO required for SQLite |
| Database | SQLite + FTS5 | Full-text search |
| HTTP Router | go-chi/chi v5 | Lightweight router |
| CLI Framework | spf13/cobra | Command structure |
| Config | spf13/viper | YAML/env config |
| Logging | rs/zerolog | JSON structured logs |
| SQLite Driver | mattn/go-sqlite3 | CGO-based |
| UUID | google/uuid | Instance IDs |

### Build Requirements

```makefile
CGO_ENABLED=1           # Required for SQLite
GOTAGS=-tags "fts5"     # Required for full-text search
```

---

## Key Patterns & Conventions

### 1. Error Handling

```go
// Use wrapped errors with context
if err != nil {
    return fmt.Errorf("operation description: %w", err)
}
```

### 2. Logging

```go
// Use component-tagged loggers
logger := observability.Logger("component-name")
logger.Info().
    Str("key", "value").
    Msg("message")
```

### 3. Database Access

```go
// Use context for cancellation
row := db.QueryRowContext(ctx, "SELECT ...")

// Use sql.NullString for nullable columns
var nullableField sql.NullString
err := row.Scan(&nullableField)

// Parse datetime strings from SQLite
createdAt, _ := time.Parse("2006-01-02 15:04:05", createdAtStr)
```

### 4. State Machine

```go
// Always check valid transitions
if !IsValidTransition(instance.Status, newStatus) {
    return fmt.Errorf("invalid transition from %s to %s",
        instance.Status, newStatus)
}
```

### 5. Container Security

```go
// Default security settings
Security: runtime.SecuritySpec{
    ReadOnlyRootfs:   true,
    NoNewPrivileges:  true,
    DropCapabilities: []string{"ALL"},
}
```

---

## Current State (V0 Complete)

### What's Implemented

| Feature | Status | Location |
|---------|--------|----------|
| Daemon Core | Complete | `internal/daemon/` |
| Container Runtime | Complete | `internal/runtime/` |
| Policy Engine | Complete | `internal/policy/` |
| Lifecycle Manager | Complete | `internal/lifecycle/` |
| Client Adapters | Complete | `internal/adapters/` |
| Knowledge Base | Complete | `internal/kb/` |
| CLI Commands | Complete | `cmd/conduit/commands/` |
| SQLite Store | Complete | `internal/store/` |
| Integration Tests | Complete | `tests/integration/` |

### Test Status

All 93 tests passing:
```
ok  internal/adapters
ok  internal/config
ok  internal/kb
ok  internal/lifecycle
ok  internal/policy
ok  internal/runtime
ok  internal/store
ok  pkg/models
ok  tests/integration
```

---

## Known Issues & Workarounds

### 1. SQLite DateTime Handling

**Issue**: SQLite stores datetime as TEXT; Go's `database/sql` can't scan directly to `time.Time`.

**Solution** (in `internal/lifecycle/manager.go`):
```go
var createdAt, updatedAt string  // Scan to strings
row.Scan(&createdAt, &updatedAt)
inst.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
```

### 2. FTS5 Build Flag

**Issue**: FTS5 not available without build flag.

**Solution**: Always build with:
```bash
CGO_ENABLED=1 go build -tags "fts5" ...
```

### 3. Foreign Key Constraints

**Issue**: KB documents require valid source_id.

**Solution**: Always create source before indexing documents (see `tests/integration/kb_integration_test.go`).

### 4. Temp Directory Policy

**Issue**: macOS temp directories (`/var/folders/...`) were blocked.

**Solution**: Added `allowedPaths` exception list in `internal/policy/engine.go`.

---

## V1 Roadmap (Not Implemented)

| Feature | Priority | Complexity |
|---------|----------|------------|
| Audit Subsystem | High | Medium |
| Package Registry | High | High |
| Secret Management | High | Medium |
| Remote API (HTTPS) | Medium | Medium |
| Web Dashboard | Medium | High |
| Multi-User Support | Low | High |
| Metrics Export | Low | Low |

---

## Development Workflow

### Adding a New Feature

1. **Plan** - Define the feature scope
2. **Design** - Create types in appropriate `types.go`
3. **Implement** - Write implementation with tests
4. **Test** - Run `make test`
5. **Document** - Update relevant docs

### Adding a New Command

```bash
# 1. Create command file
touch cmd/conduit/commands/newcommand.go

# 2. Add to root.go
rootCmd.AddCommand(newCommandCmd)

# 3. Implement handler
```

### Adding a New Adapter

```bash
# 1. Create adapter file
touch internal/adapters/newclient.go

# 2. Implement Adapter interface
type newClientAdapter struct {
    baseAdapter
}

# 3. Register in registry.go
registry.Register("new-client", &newClientAdapter{})
```

---

## Testing Guidelines

### Running Tests

```bash
# All tests
make test

# Specific package
CGO_ENABLED=1 go test -tags "fts5" -v ./internal/kb/...

# With race detection
CGO_ENABLED=1 go test -tags "fts5" -race ./...

# Integration only
CGO_ENABLED=1 go test -tags "fts5" -v ./tests/integration/...
```

### Writing Tests

```go
func TestFeature(t *testing.T) {
    // Setup
    st := testStore(t)  // Helper creates temp store
    if st == nil {
        t.Skip("FTS5 not available")
    }
    defer st.Close()

    // Test
    result, err := feature.Do(ctx, args)

    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result != expected {
        t.Errorf("expected %v, got %v", expected, result)
    }
}
```

---

## API Reference

### Daemon HTTP Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/api/v1/health` | Health check |
| GET | `/api/v1/instances` | List instances |
| POST | `/api/v1/instances` | Create instance |
| GET | `/api/v1/instances/{id}` | Get instance |
| POST | `/api/v1/instances/{id}/start` | Start instance |
| POST | `/api/v1/instances/{id}/stop` | Stop instance |
| DELETE | `/api/v1/instances/{id}` | Remove instance |
| GET | `/api/v1/kb/sources` | List KB sources |
| POST | `/api/v1/kb/sources` | Add KB source |
| GET | `/api/v1/kb/search` | Search KB |

### Key Interfaces

```go
// runtime.Provider
type Provider interface {
    Name() string
    Available() bool
    Run(ctx, spec) (containerID, error)
    Stop(ctx, containerID, timeout) error
    Remove(ctx, containerID, force) error
    Status(ctx, containerID) (string, error)
    Pull(ctx, image, opts) error
}

// adapters.Adapter
type Adapter interface {
    ID() string
    Name() string
    Installed() bool
    ConfigPath() string
    Inject(ctx, plan) error
    Eject(ctx, bindingID) error
    Validate(ctx, bindingID) (*ValidationResult, error)
}
```

---

## Related Documents

| Document | Path | Purpose |
|----------|------|---------|
| Project README | `README.md` | Overview and quick start |
| V0 Outcome | `docs/V0_OUTCOME.md` | Implementation details |
| User Guide | `docs/USER_GUIDE.md` | End-user documentation |
| Admin Guide | `docs/ADMIN_GUIDE.md` | Administrator documentation |
| Use Cases | `docs/USE_CASES.md` | Real-world examples |
| Makefile | `Makefile` | Build configuration |
| Go Module | `go.mod` | Dependencies |

---

## Model-Specific Notes

### For Opus 4.5

- Full context available for complex architectural decisions
- Can handle multi-file refactoring
- Best for: new feature implementation, architectural changes

### For Sonnet 4.5

- Focus on specific, well-defined tasks
- Provide explicit file paths
- Best for: bug fixes, tests, documentation updates

### Context Loading Suggestions

When starting a new session:

1. **Minimal Context** (quick tasks):
   - This file (`CONTEXT.md`)
   - Specific file(s) being modified

2. **Standard Context** (feature work):
   - This file
   - `README.md`
   - `docs/V0_OUTCOME.md`
   - Relevant package files

3. **Full Context** (architecture work):
   - All docs in `docs/`
   - `internal/` structure
   - `Makefile`

---

## Quick Reference: Common Tasks

### Fix a Bug

```bash
# 1. Reproduce the issue
./bin/conduit-daemon --foreground --log-level=debug

# 2. Find the source
# Check the stack trace/logs for component

# 3. Write a test that fails
# 4. Fix the code
# 5. Verify tests pass
make test
```

### Add a CLI Command

1. Create `cmd/conduit/commands/newcmd.go`
2. Define cobra command
3. Add to parent command
4. Implement handler (call daemon API or internal logic)
5. Add tests

### Add a Database Table

1. Add migration in `internal/store/migrations.go`
2. Increment migration version
3. Add CRUD methods to store
4. Add types to appropriate package
5. Test with `make test`

### Add a Policy Rule

1. Edit `internal/policy/engine.go`
2. Add to `forbiddenPaths` or `forbiddenPatterns`
3. Or add new rule to `initBuiltinRules()`
4. Add test case
5. Verify with `make test`

---

## Contact & Resources

- **Repository**: https://github.com/amlandas/Conduit-AI-Intelligence-Hub
- **Documentation**: `docs/` directory
- **Issues**: https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues

---

*This context document should be updated whenever significant changes are made to the project structure, patterns, or state.*
