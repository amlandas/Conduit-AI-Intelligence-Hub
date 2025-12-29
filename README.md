# Conduit - AI Intelligence Hub

Conduit is a local-first, security-first AI intelligence hub that connects AI clients (CLI tools, IDEs, desktop apps) to external tools via MCP (Model Context Protocol) servers. It provides document-to-knowledge transformation, sandboxed connector execution, and unified configuration management.

## Quick Installation (Recommended)

Install Conduit with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
```

The installer will:
- Detect your OS and architecture
- Install any missing dependencies (Go, Git, Docker/Podman, Ollama)
- Build and install Conduit
- Set up the daemon as a background service
- Pull the default AI model (qwen2.5-coder:7b)
- Verify the installation

### Installation Options

```bash
# Custom install location (default: /usr/local/bin or ~/.local/bin)
curl -fsSL ... | bash -s -- --install-dir ~/.local/bin

# Skip daemon service setup
curl -fsSL ... | bash -s -- --no-service

# Verbose output for troubleshooting
curl -fsSL ... | bash -s -- --verbose

# Skip model download (download later)
curl -fsSL ... | bash -s -- --skip-model
```

After installation, add the install location to your PATH if prompted, then run:

```bash
conduit status
```

## V0 Features

- **One-Click Installation**: Automated setup with interactive prompts
- **Daemon Core**: Unix socket IPC, HTTP API, background services
- **Container Runtime**: Podman (preferred) / Docker support for sandboxed connectors
- **Policy Engine**: Permission evaluation, sensitive path protection, network controls
- **Lifecycle Manager**: Connector instance state machine, health monitoring
- **Client Adapters**: Claude Code, Cursor, VS Code, Gemini CLI injection
- **Knowledge Base**: Document indexing, full-text search with FTS5, MCP server
- **AI Integration**: Local AI with Ollama for intelligent code analysis
- **CLI**: Complete command set for all operations

## Requirements

The installer handles these automatically, but for reference:

- Go 1.21+
- Git
- SQLite 3.35+ with FTS5 extension (included in Go build)
- Podman 4.0+ (recommended) or Docker 20.10+ (for running connectors)
- Ollama (for local AI features)

## Manual Installation

If you prefer manual installation or the automated installer doesn't work for your system:

```bash
# Clone the repository
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git
cd Conduit-AI-Intelligence-Hub

# Build from source (creates bin/ directory)
make build

# This creates:
#   bin/conduit        - CLI tool
#   bin/conduit-daemon - Background daemon

# Install to PATH (optional)
sudo cp bin/conduit bin/conduit-daemon /usr/local/bin/

# Install dependencies
conduit install-deps

# Set up daemon service
conduit service install
conduit service start
```

Alternative without Make:
```bash
mkdir -p bin
go build -tags "fts5" -o bin/conduit ./cmd/conduit
go build -tags "fts5" -o bin/conduit-daemon ./cmd/conduit-daemon
```

## Quick Start

```bash
# Check Conduit is running
conduit status

# Run diagnostics to verify everything is set up correctly
conduit doctor

# Run interactive setup wizard
conduit setup

# In case of issues, view daemon logs
conduit service status
```

## Project Structure

```
conduit/
├── cmd/
│   ├── conduit/          # CLI tool
│   └── conduit-daemon/   # Background daemon
├── internal/
│   ├── adapters/         # Client adapters (Claude Code, Cursor, etc.)
│   ├── config/           # Configuration management
│   ├── daemon/           # Daemon core and HTTP handlers
│   ├── kb/               # Knowledge base (indexer, searcher, MCP)
│   ├── lifecycle/        # Instance lifecycle management
│   ├── observability/    # Logging and metrics
│   ├── policy/           # Permission policy engine
│   ├── runtime/          # Container runtime abstraction
│   └── store/            # SQLite data store
├── pkg/
│   └── models/           # Shared types and errors
├── tests/
│   └── integration/      # Integration tests
├── docs/                 # Documentation
├── artifacts/            # Build artifacts
└── scripts/              # Build and utility scripts
```

## CLI Commands

### Installation & Setup
```bash
conduit setup                 # Interactive setup wizard
conduit install-deps          # Install runtime dependencies
conduit doctor                # Run diagnostics
conduit uninstall             # Uninstall Conduit
```

### Service Management
```bash
conduit service install       # Install daemon as system service
conduit service start         # Start the daemon service
conduit service stop          # Stop the daemon service
conduit service status        # Show service status
conduit service remove        # Remove daemon service
```

### Instance Management
```bash
conduit install <package>     # Install a connector
conduit list                  # List all instances
conduit start <instance>      # Start an instance
conduit stop <instance>       # Stop an instance
conduit remove <instance>     # Remove an instance
conduit logs <instance>       # View instance logs
```

### Client Management
```bash
conduit client list           # List detected AI clients
conduit client bind           # Bind connector to client
conduit client unbind         # Unbind connector from client
```

### Knowledge Base
```bash
conduit kb add <path>         # Add document source
conduit kb list               # List document sources
conduit kb sync               # Sync all sources
conduit kb search <query>     # Search indexed documents
conduit kb stats              # Show KB statistics
```

### System
```bash
conduit status                # Show daemon status
conduit config show           # Show configuration
conduit backup                # Backup data
```

## Architecture

### Security Model

- **Rootless Containers**: Podman preferred for rootless operation
- **Capability Dropping**: All capabilities dropped by default
- **Read-only Root Filesystem**: Containers run with immutable root
- **Network Isolation**: Default to no network access
- **Sensitive Path Protection**: Automatic blocking of ~/.ssh, ~/.aws, etc.

### State Machine

```
CREATED → AUDITING → INSTALLED → STARTING → RUNNING
                  ↘              ↗        ↘
                   BLOCKED      STOPPED    DEGRADED
                        ↘         ↓
                         DISABLED → REMOVING → REMOVED
```

### MCP Protocol

Conduit exposes connectors and the knowledge base via MCP:

```json
{
  "mcpServers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}
```

## Configuration

Configuration is loaded from:
1. `~/.conduit/conduit.yaml`
2. `/etc/conduit/conduit.yaml`
3. Environment variables (CONDUIT_*)

Example configuration:
```yaml
data_dir: ~/.conduit
socket: ~/.conduit/conduit.sock
log_level: info

runtime:
  preferred: auto  # "podman", "docker", or "auto"
  health_interval: 30s

kb:
  chunk_size: 1000
  chunk_overlap: 100
  max_file_size: 10485760  # 10MB

policy:
  allow_network_egress: false
  forbidden_paths:
    - /
    - /etc
    - ~/.ssh
    - ~/.aws
```

## Development

### Running Tests

```bash
# All tests
make test

# With coverage
make test-coverage

# Specific package
go test ./internal/kb/...
```

### Building

```bash
# Build all binaries
make build

# Build for specific platform
GOOS=linux GOARCH=amd64 make build
```

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please read CONTRIBUTING.md for guidelines.
