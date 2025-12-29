# Conduit User Guide

**Version**: 0.1.0
**Last Updated**: December 2025

---

## Table of Contents

1. [Introduction](#introduction)
2. [Installation](#installation)
3. [Quick Start](#quick-start)
4. [Service Management](#service-management)
5. [Managing Connectors](#managing-connectors)
6. [Binding to AI Clients](#binding-to-ai-clients)
7. [Knowledge Base](#knowledge-base)
8. [Security & Permissions](#security--permissions)
9. [Troubleshooting](#troubleshooting)
10. [Command Reference](#command-reference)

---

## Introduction

### What is Conduit?

Conduit is a local-first AI intelligence hub that connects your AI coding assistants (Claude Code, Cursor, VS Code, Gemini CLI) to external tools through MCP (Model Context Protocol) servers. Think of it as a secure bridge between your AI tools and the services they need to access.

### Why Use Conduit?

- **Security First**: All connectors run in isolated containers with minimal permissions
- **Unified Management**: Manage all your MCP servers from one place
- **Knowledge Base**: Index your documents for AI-powered search
- **Multi-Client Support**: Works with Claude Code, Cursor, VS Code, and Gemini CLI
- **Local First**: Your data stays on your machine

### Key Concepts

| Term | Description |
|------|-------------|
| **Connector** | An MCP server packaged in a container |
| **Instance** | A running copy of a connector |
| **Binding** | Connection between a connector and an AI client |
| **Knowledge Base** | Indexed documents for AI search |

---

## Installation

### One-Click Installation (Recommended)

Install Conduit with a single command:

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
```

The installer automatically:
- Detects your operating system and architecture
- Installs missing dependencies (Go, Git, Docker/Podman, Ollama)
- Builds and installs Conduit binaries
- Sets up the daemon as a background service
- Downloads the default AI model (qwen2.5-coder:7b)
- Verifies the installation

**Installation Options**:

```bash
# Custom install location
curl -fsSL ... | bash -s -- --install-dir ~/.local/bin

# Skip daemon service setup
curl -fsSL ... | bash -s -- --no-service

# Verbose output
curl -fsSL ... | bash -s -- --verbose

# Skip AI model download
curl -fsSL ... | bash -s -- --skip-model
```

After installation, add the install location to your PATH if prompted.

### Manual Installation

If you prefer manual installation or the automated installer doesn't work:

```bash
# Clone the repository
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git
cd Conduit-AI-Intelligence-Hub

# Build the binaries
make build

# Install to PATH
sudo cp bin/conduit bin/conduit-daemon /usr/local/bin/

# Install runtime dependencies
conduit install-deps

# Set up daemon service
conduit service install
conduit service start
```

**Building without Make**:
```bash
mkdir -p bin
go build -tags "fts5" -o bin/conduit ./cmd/conduit
go build -tags "fts5" -o bin/conduit-daemon ./cmd/conduit-daemon
```

### Verify Installation

```bash
# Check status
conduit status

# Run diagnostics
conduit doctor
```

### Uninstalling

To completely remove Conduit and optionally its dependencies:

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/uninstall.sh | bash
```

The uninstall script will interactively ask you about each component:
- Stop and remove the daemon service
- Remove Conduit binaries
- Remove data directory (`~/.conduit`)
- Clean up shell configuration (PATH entries)
- Optionally remove Docker/Podman
- Optionally remove Ollama and AI models
- Optionally remove Go

**Uninstall Options:**
```bash
# Skip all confirmations
curl -fsSL ... | bash -s -- --force

# Remove everything automatically
curl -fsSL ... | bash -s -- --remove-all

# Specify custom paths
bash uninstall.sh --install-dir ~/.local/bin --conduit-home ~/.conduit
```

The script gracefully handles errors and continues with remaining components. Backups of shell configurations are created with `.conduit-backup` extension

---

## Quick Start

After installation, Conduit is ready to use. The daemon starts automatically as a background service.

### Step 1: Verify Installation

```bash
# Check Conduit status
conduit status

# Run diagnostics
conduit doctor
```

Expected output:
```
Daemon Status: Running
Socket: /Users/you/.conduit/conduit.sock
Uptime: 5m30s
Instances: 0 running, 0 total
```

### Step 2: Run Setup Wizard (Optional)

If you want to reconfigure or add more options:

```bash
conduit setup
```

The setup wizard will guide you through:
- Checking dependencies
- Setting up the daemon service
- Configuring AI clients

### Step 3: Install Your First Connector

```bash
# Install a filesystem connector (example)
conduit install \
  --package-id "mcp/filesystem" \
  --name "My Files" \
  --image "ghcr.io/mcp/filesystem:latest"
```

### Step 4: Start the Connector

```bash
# List instances
conduit list

# Start the instance
conduit start <instance-id>
```

### Step 5: Bind to Claude Code

```bash
# List detected clients
conduit client list

# Bind the connector to Claude Code
conduit client bind <instance-id> --client claude-code
```

---

## Service Management

Conduit runs as a background daemon service. On macOS, it uses launchd; on Linux, it uses systemd.

### Service Commands

```bash
# Check daemon status
conduit service status

# Stop the daemon
conduit service stop

# Start the daemon
conduit service start

# Reinstall the service (if needed)
conduit service remove
conduit service install
```

### Manual Daemon Control

For development or troubleshooting, you can run the daemon manually:

```bash
# Run in foreground with debug logging
conduit-daemon --foreground --log-level=debug
```

### Service Locations

| Platform | Service File |
|----------|-------------|
| macOS | `~/Library/LaunchAgents/com.simpleflo.conduit.plist` |
| Linux | `~/.config/systemd/user/conduit.service` |

### Viewing Logs

```bash
# macOS
cat ~/Library/Logs/conduit/conduit.log

# Linux (systemd)
journalctl --user -u conduit -f
```

---

## Managing Connectors

### Installing a Connector

```bash
./bin/conduit install \
  --package-id "vendor/connector-name" \
  --name "Display Name" \
  --image "registry/image:tag" \
  --config KEY=VALUE
```

**Options**:
- `--package-id`: Unique identifier for the connector package
- `--name`: Human-readable display name
- `--image`: Container image reference
- `--config`: Configuration key-value pairs (can be repeated)

### Listing Connectors

```bash
# List all instances
./bin/conduit list

# Output:
# ID                                    NAME           STATUS     PACKAGE
# a1b2c3d4-e5f6-7890-abcd-ef1234567890  My Files       RUNNING    mcp/filesystem
# b2c3d4e5-f6a7-8901-bcde-f12345678901  GitHub Tools   STOPPED    mcp/github
```

### Starting and Stopping

```bash
# Start an instance
./bin/conduit start <instance-id>

# Stop an instance
./bin/conduit stop <instance-id>
```

### Viewing Logs

```bash
# View recent logs
./bin/conduit logs <instance-id>

# Follow logs in real-time
./bin/conduit logs <instance-id> --follow

# Show last N lines
./bin/conduit logs <instance-id> --lines 100
```

### Removing a Connector

```bash
# Remove an instance (stops if running)
./bin/conduit remove <instance-id>

# Force remove without confirmation
./bin/conduit remove <instance-id> --force
```

---

## Binding to AI Clients

### Supported Clients

| Client | Config Location | Status |
|--------|-----------------|--------|
| Claude Code | `~/.claude.json` | Supported |
| Cursor | `~/.cursor/mcp.json` | Supported |
| VS Code | `~/.vscode/mcp.json` | Supported |
| Gemini CLI | `~/.gemini/mcp.json` | Supported |

### Listing Detected Clients

```bash
./bin/conduit client list

# Output:
# CLIENT        INSTALLED    CONFIG PATH
# claude-code   Yes          /Users/you/.claude.json
# cursor        Yes          /Users/you/.cursor/mcp.json
# vscode        No           -
# gemini-cli    Yes          /Users/you/.gemini/mcp.json
```

### Creating a Binding

```bash
# Bind a connector to a client
./bin/conduit client bind <instance-id> --client claude-code

# Bind with specific scope
./bin/conduit client bind <instance-id> --client cursor --scope project
```

**Scope Options**:
- `user` (default): Available globally for the user
- `project`: Available only in the current project
- `workspace`: Available in the workspace

### Viewing Bindings

```bash
# List all bindings for an instance
./bin/conduit client bindings <instance-id>
```

### Removing a Binding

```bash
# Unbind from a client
./bin/conduit client unbind <instance-id> --client claude-code
```

### What Happens During Binding?

1. Conduit reads your AI client's config file
2. Creates a backup of the original config
3. Injects the MCP server configuration
4. Validates the modified config
5. Writes the updated config

Your AI client will automatically detect the new MCP server.

---

## Knowledge Base

The Knowledge Base allows you to index documents for AI-powered search.

### Adding a Document Source

```bash
# Add a directory as a source
./bin/conduit kb add /path/to/docs --name "Project Docs"

# Add with specific sync mode
./bin/conduit kb add /path/to/docs --name "Project Docs" --sync manual
```

**Sync Modes**:
- `manual`: Sync only when requested
- `auto`: Sync periodically (future feature)

### Listing Sources

```bash
./bin/conduit kb list

# Output:
# SOURCE ID                              NAME           PATH                    DOCUMENTS
# abc123-def456-...                      Project Docs   /path/to/docs           42
```

### Syncing Documents

```bash
# Sync all sources
./bin/conduit kb sync

# Sync a specific source
./bin/conduit kb sync <source-id>
```

### Searching

```bash
# Search indexed documents
./bin/conduit kb search "how to configure authentication"

# Search with limit
./bin/conduit kb search "API endpoints" --limit 10
```

**Search Output**:
```
Results for "how to configure authentication" (3 hits):

1. [0.85] /docs/auth/setup.md
   "...configure authentication using OAuth2. First, set up your client..."

2. [0.72] /docs/security/overview.md
   "...authentication mechanisms supported include JWT tokens and..."

3. [0.68] /docs/api/auth-endpoints.md
   "...authentication endpoint accepts POST requests with..."
```

### Viewing Statistics

```bash
./bin/conduit kb stats

# Output:
# Knowledge Base Statistics
# -------------------------
# Total Sources:    3
# Total Documents:  156
# Total Chunks:     1,247
# Database Size:    12.5 MB
# Last Sync:        2024-12-28 15:30:00
```

### Removing a Source

```bash
./bin/conduit kb remove <source-id>
```

---

## Security & Permissions

### Permission Model

Conduit uses a layered permission model:

1. **Forbidden Paths**: Always blocked (credentials, system files)
2. **Allowed Paths**: Explicitly permitted (temp directories)
3. **User Grants**: Explicit user approval for other paths

### Viewing Current Permissions

```bash
# Show permissions for an instance
./bin/conduit permissions show <instance-id>
```

### Granting Permissions

When a connector requests access to a resource, you'll be prompted:

```
Connector "My Files" requests:
  - Read access to: /Users/you/projects
  - Write access to: /Users/you/projects/output

Allow? [y/N/details]
```

To pre-grant permissions:

```bash
./bin/conduit permissions grant <instance-id> \
  --readonly /path/to/read \
  --readwrite /path/to/write
```

### Revoking Permissions

```bash
./bin/conduit permissions revoke <instance-id> --type filesystem
```

### What's Always Blocked?

These paths are **never** accessible to connectors:

- Root filesystem (`/`)
- System directories (`/etc`, `/var`, `/root`)
- SSH keys (`~/.ssh`)
- Cloud credentials (`~/.aws`, `~/.config/gcloud`, `~/.azure`, `~/.kube`)
- GPG keys (`~/.gnupg`)
- Docker/Podman configs (`~/.docker`)
- macOS Keychain (`~/Library/Keychains`)

---

## Troubleshooting

### Daemon Won't Start

**Problem**: `Error: create daemon: ...`

**Solutions**:
1. Check if another daemon is running: `pgrep conduit-daemon`
2. Remove stale socket: `rm ~/.conduit/conduit.sock`
3. Check permissions: `ls -la ~/.conduit/`

### FTS5 Not Available

**Problem**: `no such module: fts5`

**Solution**: Ensure you build with FTS5 enabled:
```bash
make clean
make build  # Uses -tags "fts5" automatically
```

### Connector Won't Start

**Problem**: `Error: container start: ...`

**Solutions**:
1. Check if Podman/Docker is running: `podman info` or `docker info`
2. Pull the image manually: `podman pull <image>`
3. Check container logs: `./bin/conduit logs <instance-id>`

### Client Config Not Updated

**Problem**: AI client doesn't see the connector

**Solutions**:
1. Restart the AI client
2. Check the config file: `cat ~/.claude.json`
3. Verify the binding: `./bin/conduit client bindings <instance-id>`

### Permission Denied Errors

**Problem**: Connector can't access files

**Solutions**:
1. Check if path is forbidden: see "What's Always Blocked?" above
2. Grant explicit permission: `./bin/conduit permissions grant ...`
3. Check container mounts: `./bin/conduit inspect <instance-id>`

### Running Diagnostics

```bash
./bin/conduit doctor

# Checks:
# ✓ Daemon running
# ✓ Database accessible
# ✓ Container runtime available
# ✓ FTS5 extension loaded
# ✓ Client configs writable
```

---

## Command Reference

### Global Options

| Option | Description |
|--------|-------------|
| `--help` | Show help for any command |
| `--version` | Show version information |
| `--config` | Path to config file |
| `--socket` | Path to daemon socket |

### Installation & Setup Commands

| Command | Description |
|---------|-------------|
| `conduit setup` | Run interactive setup wizard |
| `conduit install-deps` | Install runtime dependencies |
| `conduit doctor` | Run diagnostics |
| `conduit uninstall` | Uninstall Conduit completely |

### Service Commands

| Command | Description |
|---------|-------------|
| `conduit service install` | Install daemon as system service |
| `conduit service start` | Start the daemon service |
| `conduit service stop` | Stop the daemon service |
| `conduit service status` | Show service status |
| `conduit service remove` | Remove daemon service |

### Instance Commands

| Command | Description |
|---------|-------------|
| `conduit install` | Install a new connector |
| `conduit list` | List all instances |
| `conduit start <id>` | Start an instance |
| `conduit stop <id>` | Stop an instance |
| `conduit remove <id>` | Remove an instance |
| `conduit logs <id>` | View instance logs |
| `conduit inspect <id>` | Show instance details |

### Client Commands

| Command | Description |
|---------|-------------|
| `conduit client list` | List detected AI clients |
| `conduit client bind` | Bind instance to client |
| `conduit client unbind` | Remove binding |
| `conduit client bindings` | Show instance bindings |

### Knowledge Base Commands

| Command | Description |
|---------|-------------|
| `conduit kb add <path>` | Add document source |
| `conduit kb list` | List sources |
| `conduit kb sync` | Sync documents |
| `conduit kb search <query>` | Search documents |
| `conduit kb stats` | Show statistics |
| `conduit kb remove <id>` | Remove source |

### System Commands

| Command | Description |
|---------|-------------|
| `conduit status` | Show daemon status |
| `conduit config show` | Show configuration |
| `conduit backup` | Backup data |

---

## Getting Help

- **Documentation**: See `docs/` directory
- **Issues**: Report bugs at https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues
- **Logs**: Check `~/.conduit/logs/` for detailed logs

---

## Appendix: Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONDUIT_DATA_DIR` | Data directory path | `~/.conduit` |
| `CONDUIT_SOCKET` | Socket file path | `~/.conduit/conduit.sock` |
| `CONDUIT_LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |
| `CONDUIT_RUNTIME` | Container runtime (podman/docker/auto) | `auto` |
