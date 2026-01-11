# Conduit CLI Command Index

**Version**: 1.0.42
**Last Updated**: January 2026

This document provides a complete reference of all CLI commands available in Conduit.

---

## Quick Reference

| Category | Command | Description |
|----------|---------|-------------|
| **Setup** | `conduit setup` | Interactive setup wizard |
| **Setup** | `conduit doctor` | Run diagnostics |
| **Setup** | `conduit install-deps` | Install runtime dependencies |
| **Deps** | `conduit deps status` | Check dependency status |
| **Deps** | `conduit deps install` | Install a dependency |
| **Deps** | `conduit deps validate` | Validate custom binary path |
| **Service** | `conduit service install` | Install daemon as system service |
| **Service** | `conduit service start` | Start the daemon |
| **Service** | `conduit service stop` | Stop the daemon |
| **Service** | `conduit service status` | Show service status |
| **Service** | `conduit service remove` | Remove the daemon service |
| **Instance** | `conduit install` | Install a connector |
| **Instance** | `conduit create` | Create a connector instance |
| **Instance** | `conduit list` | List all instances |
| **Instance** | `conduit start <id>` | Start an instance |
| **Instance** | `conduit stop <id>` | Stop an instance |
| **Instance** | `conduit remove <id>` | Remove an instance |
| **Instance** | `conduit logs <id>` | View instance logs |
| **Instance** | `conduit audit <id>` | Show audit logs (Advanced) |
| **Client** | `conduit client list` | List detected AI clients |
| **Client** | `conduit client bind` | Bind instance to client |
| **Client** | `conduit client unbind` | Remove binding |
| **Client** | `conduit client bindings` | Show bindings for instance |
| **KB** | `conduit kb add <path>` | Add document source |
| **KB** | `conduit kb list` | List sources |
| **KB** | `conduit kb sync` | Sync documents |
| **KB** | `conduit kb search <query>` | Search documents |
| **KB** | `conduit kb stats` | Show statistics |
| **KB** | `conduit kb remove <id>` | Remove source |
| **KB** | `conduit kb migrate` | Migrate to vector store |
| **KAG** | `conduit kb kag-sync` | Extract entities from documents |
| **KAG** | `conduit kb kag-status` | Show extraction status |
| **KAG** | `conduit kb kag-query` | Query knowledge graph |
| **KAG** | `conduit kb kag-retry` | Retry failed extractions |
| **KAG** | `conduit kb kag-dedupe` | Deduplicate entities |
| **KAG** | `conduit kb kag-vectorize` | Generate entity embeddings |
| **MCP** | `conduit mcp configure` | Auto-configure MCP in AI clients |
| **MCP** | `conduit mcp status` | Show MCP server status |
| **MCP** | `conduit mcp kb` | Run KB MCP server |
| **MCP** | `conduit mcp stdio` | Run MCP server over stdio |
| **MCP** | `conduit mcp logs` | Show MCP server logs |
| **Ollama** | `conduit ollama status` | Show Ollama status |
| **Ollama** | `conduit ollama models` | List available models |
| **Ollama** | `conduit ollama pull` | Download a model |
| **Ollama** | `conduit ollama warmup` | Preload models into memory |
| **Qdrant** | `conduit qdrant install` | Install Qdrant container |
| **Qdrant** | `conduit qdrant start` | Start Qdrant |
| **Qdrant** | `conduit qdrant stop` | Stop Qdrant |
| **Qdrant** | `conduit qdrant status` | Check Qdrant status |
| **Qdrant** | `conduit qdrant attach` | Enable semantic search |
| **Qdrant** | `conduit qdrant purge` | Clear all vectors |
| **FalkorDB** | `conduit falkordb install` | Install FalkorDB container |
| **FalkorDB** | `conduit falkordb start` | Start FalkorDB |
| **FalkorDB** | `conduit falkordb stop` | Stop FalkorDB |
| **FalkorDB** | `conduit falkordb status` | Check FalkorDB status |
| **Config** | `conduit config` | Show configuration |
| **Config** | `conduit config get` | Get a config value |
| **Config** | `conduit config set` | Set a config value |
| **Config** | `conduit config unset` | Remove a config value |
| **System** | `conduit status` | Show daemon status |
| **System** | `conduit stats` | Show daemon statistics |
| **System** | `conduit backup` | Backup data |
| **System** | `conduit uninstall` | Uninstall Conduit |
| **System** | `conduit events` | Stream real-time events (SSE) |

---

## Global Options

These options are available for all commands:

| Option | Description |
|--------|-------------|
| `--help, -h` | Show help for any command |
| `--version, -v` | Show version information |
| `--config <path>` | Path to config file (default: `~/.conduit/conduit.yaml`) |
| `--socket <path>` | Path to daemon socket (default: `~/.conduit/conduit.sock`) |

---

## Setup Commands

### `conduit setup`

Run the interactive setup wizard.

```bash
conduit setup [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--non-interactive` | Skip prompts, use defaults |
| `--skip-deps` | Skip dependency installation |
| `--skip-service` | Skip service installation |

### `conduit doctor`

Run diagnostics to check system health.

```bash
conduit doctor [options]
```

**Checks performed**:
- Daemon running and responsive
- Database accessible and intact
- Container runtime available
- FTS5 extension loaded
- Semantic search components (Qdrant, Ollama)
- KAG components (if enabled)
- Client configs writable

### `conduit install-deps`

Install runtime dependencies.

```bash
conduit install-deps [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--document-tools` | Install PDF/DOC extraction tools |
| `--semantic` | Install semantic search (Qdrant, Ollama) |
| `--kag` | Install KAG dependencies (Mistral model) |
| `--verbose` | Show verbose output |

---

## Dependency Commands

Manage Conduit dependencies programmatically. These commands are used by the GUI and automation tools.

### `conduit deps status`

Check status of all dependencies.

```bash
conduit deps status [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output in JSON format |

**Dependencies checked**:
- Homebrew (package manager, macOS/Linux)
- Ollama (local AI runtime)
- Podman (container runtime, preferred)
- Docker (container runtime, alternative)

**Examples**:
```bash
# Human-readable output
conduit deps status

# JSON output for GUI
conduit deps status --json
```

### `conduit deps install <dependency>`

Install a Conduit dependency.

```bash
conduit deps install <dependency>
```

**Supported dependencies**:
| Dependency | Description |
|------------|-------------|
| `ollama` | Local AI runtime |
| `podman` | Container runtime (recommended) |
| `docker` | Container runtime (alternative) |
| `homebrew` | Package manager (macOS/Linux) |

**Installation methods by platform**:
- **macOS**: Uses Homebrew when available
- **Linux**: Uses official installers or system package managers

**Progress output**: Commands output `PROGRESS:<percent>:<message>` for GUI integration.

**Examples**:
```bash
conduit deps install ollama
conduit deps install podman
```

### `conduit deps validate`

Validate a custom binary path.

```bash
conduit deps validate <dependency> <path>
```

**Examples**:
```bash
conduit deps validate ollama /custom/path/ollama
conduit deps validate podman /usr/local/bin/podman
```

---

## Service Commands

### `conduit service install`

Install the daemon as a system service.

```bash
conduit service install
```

**Platform-specific locations**:
- **macOS**: `~/Library/LaunchAgents/dev.simpleflo.conduit.plist`
- **Linux**: `~/.config/systemd/user/conduit.service`

### `conduit service start`

Start the daemon service.

```bash
conduit service start
```

### `conduit service stop`

Stop the daemon service.

```bash
conduit service stop
```

### `conduit service status`

Show service status.

```bash
conduit service status
```

### `conduit service remove`

Remove the daemon service.

```bash
conduit service remove
```

---

## Instance Commands

### `conduit install`

Install a new connector instance.

```bash
conduit install [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--package-id <id>` | Package identifier (required) |
| `--name <name>` | Display name (required) |
| `--image <ref>` | Container image reference (required) |
| `--config <key=value>` | Configuration (repeatable) |

**Example**:
```bash
conduit install \
  --package-id "mcp/filesystem" \
  --name "My Files" \
  --image "ghcr.io/mcp/filesystem:latest" \
  --config PATH=/Users/me/docs
```

### `conduit create <package-id>`

Create a new connector instance from a package.

```bash
conduit create <package-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--name <name>` | Display name for the instance |
| `--version <ver>` | Package version (default: 1.0.0) |
| `--image <ref>` | Docker image reference |
| `--config <str>` | Instance config (format: key1=value1,key2=value2) |
| `--json` | Output as JSON (for GUI) |

The instance will be created but not started. Use `conduit start` to run it.

**Examples**:
```bash
conduit create filesystem --name "My Files"
conduit create github --name "GitHub Repos" --config "token=ghp_xxx"
conduit create filesystem --json   # JSON output for GUI
```

### `conduit list`

List all connector instances.

```bash
conduit list [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--status <status>` | Filter by status (RUNNING, STOPPED, FAILED) |
| `--json` | Output as JSON |

### `conduit start <instance-id>`

Start a connector instance.

```bash
conduit start <instance-id>
```

### `conduit stop <instance-id>`

Stop a connector instance.

```bash
conduit stop <instance-id>
```

### `conduit remove <instance-id>`

Remove a connector instance.

```bash
conduit remove <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--force` | Skip confirmation prompt |

### `conduit logs <instance-id>`

View logs for a connector instance.

```bash
conduit logs <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--follow, -f` | Follow log output |
| `--lines, -n <num>` | Number of lines to show (default: 50) |

### `conduit audit <instance-id>`

Show instance audit logs. *(Advanced Mode)*

```bash
conduit audit <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--limit <num>` | Maximum number of audit entries (default: 100) |
| `--json` | Output as JSON (for GUI) |

Audit logs track all access and operations performed by the instance. This is an Advanced Mode feature for security monitoring.

> **Note**: This feature requires daemon API support (coming in future release).

---

## Client Commands

### `conduit client list`

List detected AI clients.

```bash
conduit client list
```

**Detected clients**:
- Claude Code (`~/.claude.json`)
- Cursor (`~/.cursor/mcp.json`)
- VS Code (`~/.vscode/mcp.json`)
- Gemini CLI (`~/.gemini/mcp.json`)

### `conduit client bind`

Bind a connector instance to an AI client.

```bash
conduit client bind <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--client <name>` | Client name (required): claude-code, cursor, vscode, gemini-cli |
| `--scope <scope>` | Scope: user (default), project, workspace |

### `conduit client unbind`

Remove a binding from an AI client.

```bash
conduit client unbind <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--client <name>` | Client name (required) |

### `conduit client bindings <instance-id>`

Show all bindings for an instance.

```bash
conduit client bindings <instance-id>
```

---

## Knowledge Base Commands

### `conduit kb add <path>`

Add a document source to the knowledge base.

```bash
conduit kb add <path> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--name <name>` | Display name for the source |
| `--sync <mode>` | Sync mode: manual (default), auto |

**Example**:
```bash
conduit kb add /Users/me/docs --name "My Documentation"
```

### `conduit kb list`

List all document sources.

```bash
conduit kb list [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

### `conduit kb sync`

Sync documents from sources.

```bash
conduit kb sync [source-id] [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--rebuild-vectors` | Force regeneration of vector embeddings for all documents |

**Exit Codes**:
| Code | Description |
|------|-------------|
| 0 | Full success (FTS + semantic indexing) |
| 1 | Error (sync failed) |
| 2 | Partial success (FTS only, semantic indexing failed) |

**Examples**:
```bash
# Sync all sources
conduit kb sync

# Sync specific source
conduit kb sync abc123-def456

# Force rebuild vectors (useful after Qdrant issues)
conduit kb sync --rebuild-vectors
```

**Note**: If you see exit code 2 with "semantic indexing failed" warnings, run `conduit doctor` to diagnose the issue, then retry with `conduit kb sync --rebuild-vectors`.

### `conduit kb search <query>`

Search the knowledge base.

```bash
conduit kb search <query> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--semantic` | Force semantic search |
| `--fts5` | Force keyword search |
| `--limit <num>` | Maximum results (default: 10) |
| `--min-score <float>` | Minimum similarity threshold (0.0-1.0, default: 0.0) |
| `--semantic-weight <float>` | Semantic vs keyword balance (0.0-1.0, default: 0.5) |
| `--mmr-lambda <float>` | Relevance vs diversity (0.0-1.0, default: 0.7) |
| `--no-mmr` | Disable MMR diversity filtering |
| `--no-rerank` | Disable semantic reranking |
| `--source <id>` | Limit to specific source |
| `--json` | Output as JSON |

**Examples**:
```bash
# Hybrid search (default)
conduit kb search "how to configure authentication"

# Semantic search
conduit kb search "securing user login" --semantic

# Keyword search
conduit kb search "OAuth2 client" --fts5

# Low threshold for domain-specific terms
conduit kb search "ASL-3 safeguards" --min-score 0.0 --limit 20
```

### `conduit kb stats`

Show knowledge base statistics.

```bash
conduit kb stats [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

### `conduit kb remove <source-id>`

Remove a document source.

```bash
conduit kb remove <source-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--force` | Skip confirmation prompt |

### `conduit kb migrate`

Migrate existing documents to vector store.

```bash
conduit kb migrate [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--source <id>` | Migrate specific source only |
| `--batch-size <num>` | Documents per batch (default: 100) |

---

## KAG (Knowledge Graph) Commands

### `conduit kb kag-sync`

Extract entities and relationships from indexed documents.

```bash
conduit kb kag-sync [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--source <id>` | Sync specific source only |
| `--force` | Re-extract all chunks |
| `--workers <num>` | Number of extraction workers |

**Prerequisites**:
- KAG must be enabled in configuration
- Ollama running with extraction model (or OpenAI/Anthropic API key)

### `conduit kb kag-status`

Show KAG extraction status.

```bash
conduit kb kag-status [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

**Output includes**:
- Total chunks, extracted, pending, failed
- Entity and relation counts
- Background worker status

### `conduit kb kag-query <query>`

Query the knowledge graph.

```bash
conduit kb kag-query <query> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--entities <list>` | Comma-separated entity hints |
| `--relations` | Include relationships (default: true) |
| `--max-hops <num>` | Maximum graph hops (default: 2, max: 3) |
| `--limit <num>` | Maximum entities (default: 20, max: 100) |
| `--source <id>` | Limit to specific source |
| `--json` | Output as JSON |

**Examples**:
```bash
# Basic query
conduit kb kag-query "Kubernetes"

# With entity hints
conduit kb kag-query "container orchestration" --entities Docker,Kubernetes

# Limit scope
conduit kb kag-query "authentication" --limit 5 --max-hops 1
```

### `conduit kb kag-retry`

Retry failed KAG extractions.

```bash
conduit kb kag-retry [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--chunk-id <id>` | Specific chunk IDs to retry (repeatable) |
| `--max-retries <num>` | Maximum retry attempts (default: 2, max: 5) |
| `--dry-run` | Preview without executing |

Without flags, retries all failed chunks. Shows error breakdown by category (Incomplete JSON, Invalid escape, Schema mismatch, Timeout, Connection, Parse error).

**Examples**:
```bash
# Retry all failed chunks
conduit kb kag-retry

# Retry specific chunk
conduit kb kag-retry --chunk-id abc123

# Preview what would be retried
conduit kb kag-retry --dry-run

# Retry with 3 attempts
conduit kb kag-retry --max-retries 3
```

### `conduit kb kag-dedupe`

Deduplicate entities in the knowledge graph.

```bash
conduit kb kag-dedupe [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--dry-run` | Preview without making changes |

Merges duplicate entities that have the same normalized name and type (e.g., "Threat Model" and "threat model"). Keeps the highest confidence and best description.

**Examples**:
```bash
# Deduplicate all entities
conduit kb kag-dedupe

# Preview without making changes
conduit kb kag-dedupe --dry-run
```

### `conduit kb kag-vectorize`

Generate vector embeddings for KAG entities.

```bash
conduit kb kag-vectorize [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--batch-size <num>` | Entities per batch (default: 50) |
| `--ollama-host <url>` | Ollama host URL (default: http://localhost:11434) |
| `--qdrant-host <host>` | Qdrant host (default: localhost) |
| `--qdrant-port <port>` | Qdrant port (default: 6334) |

Generates and stores vector embeddings for all entities in the knowledge graph. Enables semantic search over entities using vector similarity. Embeddings are stored in a Qdrant collection (`conduit_entities`) separate from chunk vectors.

**Requirements**:
- Ollama running with `nomic-embed-text` model
- Qdrant running on the specified host/port

**Examples**:
```bash
conduit kb kag-vectorize
conduit kb kag-vectorize --batch-size 50
conduit kb kag-vectorize --ollama-host http://192.168.1.60:11434
```

---

## MCP Commands

Manage the Model Context Protocol (MCP) server for AI client integration.

### `conduit mcp configure`

Auto-configure the Conduit MCP KB server in AI clients.

```bash
conduit mcp configure [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--client, -c <name>` | Client to configure (default: claude-code) |
| `--force, -f` | Overwrite existing configuration |

**Supported clients**:
- `claude-code`: Claude Code CLI (`~/.claude.json`)
- `cursor`: Cursor IDE (`.cursor/settings/extensions.json`)
- `vscode`: VS Code (`.vscode/settings.json`)

**Examples**:
```bash
# Configure for Claude Code (default)
conduit mcp configure

# Configure for Cursor IDE
conduit mcp configure --client cursor

# Overwrite existing configuration
conduit mcp configure --force
```

### `conduit mcp status`

Show MCP server status and capabilities.

```bash
conduit mcp status [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON (for GUI) |

**Shows**:
- MCP configuration status in AI clients (Claude Code, Cursor, VS Code)
- Search capabilities (FTS5, semantic search availability)
- Qdrant and Ollama connectivity status
- Knowledge base sources and statistics

### `conduit mcp kb`

Run the Knowledge Base MCP server over stdio.

```bash
conduit mcp kb
```

This server provides search and document retrieval tools for AI clients to access your private knowledge base. Typically invoked by AI clients automatically.

**Example MCP client configuration**:
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

### `conduit mcp stdio`

Run an MCP server over stdio for a connector instance.

```bash
conduit mcp stdio --instance <instance-id>
```

**Options**:
| Option | Description |
|--------|-------------|
| `--instance <id>` | Connector instance ID (required) |

Proxies an MCP server over stdio. Runs a containerized MCP server with stdin/stdout attached, allowing AI clients to communicate with it via the MCP protocol.

**Example usage in AI client config**:
```json
{
  "mcpServers": {
    "my-server": {
      "command": "conduit",
      "args": ["mcp", "stdio", "--instance", "abc123"]
    }
  }
}
```

### `conduit mcp logs`

Show MCP server logs.

```bash
conduit mcp logs [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--tail, -n <num>` | Number of lines to show |
| `--follow, -f` | Follow log output |

Shows daemon logs related to MCP operations.

---

## Ollama Commands

Manage Ollama models for semantic search and entity extraction.

### `conduit ollama status`

Show Ollama status and loaded models.

```bash
conduit ollama status
```

**Shows**:
- Whether Ollama is running
- Currently loaded models (kept in memory)
- Required models for Conduit:
  - `nomic-embed-text` - Semantic search embeddings
  - `mistral:7b-instruct-q4_K_M` - Entity extraction (KAG)

Models that aren't loaded will have a cold-start delay on first use (1-2 minutes).

### `conduit ollama models`

List available Ollama models.

```bash
conduit ollama models
```

Shows all Ollama models installed on the system and indicates which required models are present.

### `conduit ollama pull <model>`

Pull (download) an Ollama model from the registry.

```bash
conduit ollama pull <model>
```

Progress is streamed to stdout, making it suitable for GUI integration. If Ollama is not running, it will be started automatically.

**Examples**:
```bash
conduit ollama pull nomic-embed-text
conduit ollama pull mistral:7b-instruct-q4_K_M
```

### `conduit ollama warmup`

Preload Ollama models into memory for faster inference.

```bash
conduit ollama warmup [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--models <list>` | Specific models to warm up (default: all required) |

By default, warms up both required models:
- `nomic-embed-text` - For semantic search
- `mistral:7b-instruct-q4_K_M` - For entity extraction

Models stay loaded based on Ollama's `keep_alive` setting (default: 5 minutes).

**Examples**:
```bash
# Warm up all required models
conduit ollama warmup

# Warm up specific model
conduit ollama warmup --models nomic-embed-text
```

---

## Qdrant Commands

Manage the Qdrant vector database for semantic search.

### `conduit qdrant install`

Install Qdrant container for semantic search.

```bash
conduit qdrant install [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--port <num>` | HTTP port (default: 6333) |
| `--grpc-port <num>` | gRPC port (default: 6334) |

### `conduit qdrant start`

Start Qdrant container.

```bash
conduit qdrant start
```

### `conduit qdrant stop`

Stop Qdrant container.

```bash
conduit qdrant stop
```

### `conduit qdrant status`

Check Qdrant status.

```bash
conduit qdrant status [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

### `conduit qdrant attach`

Enable semantic search in daemon without restart.

```bash
conduit qdrant attach [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--reindex` | Re-index existing documents after attach |

This command:
1. Verifies Qdrant is running and healthy
2. Notifies the daemon to initialize semantic search
3. Optionally triggers re-indexing of existing documents

Use this after installing Qdrant to enable semantic search without restarting the daemon.

**Examples**:
```bash
# Attach and enable semantic search
conduit qdrant attach

# Attach and re-index all documents
conduit qdrant attach --reindex
```

### `conduit qdrant purge`

Clear all vectors from the Qdrant collection.

```bash
conduit qdrant purge [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--force, -f` | Skip confirmation prompt |

Removes all vectors from the Qdrant collection. Useful when:
- You reinstalled Conduit and have orphaned vectors
- You want to start fresh with semantic search
- There's a mismatch between SQLite documents and Qdrant vectors

After purging, run `conduit kb sync` to re-index all documents.

> **Warning**: This operation cannot be undone!

---

## FalkorDB Commands

Manage the FalkorDB graph database for KAG (Knowledge-Augmented Generation).

### `conduit falkordb install`

Install FalkorDB container.

```bash
conduit falkordb install [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--port <num>` | Port to bind (default: 6379) |
| `--memory <size>` | Memory limit (e.g., "1g") |
| `--prefer-runtime <rt>` | Prefer podman or docker |

### `conduit falkordb start`

Start FalkorDB container.

```bash
conduit falkordb start
```

### `conduit falkordb stop`

Stop FalkorDB container.

```bash
conduit falkordb stop
```

### `conduit falkordb status`

Check FalkorDB status.

```bash
conduit falkordb status [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

---

## Configuration Commands

Manage Conduit configuration.

### `conduit config`

Show current Conduit configuration.

```bash
conduit config [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--all, -a` | Show all configuration options |

**Shows**:
- Paths (data directory, socket, database, logs, backups)
- Logging settings (level, format)
- AI configuration (provider, model, endpoint, timeout, confidence)
- Runtime settings (preferred runtime, timeouts)
- Knowledge Base settings (workers, max file size, chunk size) *(with --all)*
- Policy settings (network egress, forbidden paths) *(with --all)*
- API settings (timeouts) *(with --all)*

### `conduit config get <key>`

Get a specific configuration value.

```bash
conduit config get <key>
```

Keys use dot notation to access nested values.

**Examples**:
```bash
conduit config get ai.model
conduit config get deps.ollama.path
conduit config get runtime.preferred
```

### `conduit config set <key> <value>`

Set a specific configuration value.

```bash
conduit config set <key> <value>
```

Keys use dot notation. Values are stored in `~/.conduit/conduit.yaml`.

**Examples**:
```bash
conduit config set ai.model qwen2.5-coder:7b
conduit config set deps.ollama.path /custom/path/ollama
conduit config set runtime.preferred podman
```

### `conduit config unset <key>`

Remove a specific configuration value.

```bash
conduit config unset <key>
```

**Examples**:
```bash
conduit config unset deps.ollama.path
conduit config unset runtime.preferred
```

---

## System Commands

### `conduit status`

Show overall daemon status.

```bash
conduit status [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

### `conduit stats`

Show daemon statistics.

```bash
conduit stats [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON (for GUI) |

**Output includes**:
- Instance counts (total, running)
- Binding counts
- Knowledge base stats (sources, documents, chunks)
- Daemon info (version, uptime)

**Examples**:
```bash
# Human-readable output
conduit stats

# JSON output for GUI integration
conduit stats --json
```

### `conduit backup`

Backup Conduit data.

```bash
conduit backup [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--output <path>` | Output path for backup |
| `--include-vectors` | Include vector store data |

**Backup includes**:
- Database (conduit.db)
- Configuration (conduit.yaml)
- Knowledge base data
- Connector configurations

The backup is saved as a compressed tar.gz archive.

### `conduit uninstall`

Uninstall Conduit completely.

```bash
conduit uninstall [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--force` | Skip all confirmations |
| `--keep-data` | Keep data directory |
| `--remove-deps` | Also remove dependencies |

### `conduit events`

Stream real-time events from the daemon via Server-Sent Events (SSE).

```bash
conduit events [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output raw JSON events |

**Event Types**:
| Category | Event | Description |
|----------|-------|-------------|
| Instance | `instance_created` | New instance created |
| Instance | `instance_deleted` | Instance removed |
| Instance | `instance_status_changed` | Status transition (starting, running, stopped) |
| KB | `kb_source_added` | New KB source added |
| KB | `kb_source_removed` | KB source removed |
| KB | `kb_sync_started` | Sync operation started |
| KB | `kb_sync_progress` | Sync progress update |
| KB | `kb_sync_completed` | Sync completed |
| KB | `kb_sync_failed` | Sync failed |
| Binding | `binding_created` | New client binding |
| Binding | `binding_deleted` | Binding removed |
| System | `daemon_status` | Heartbeat (every 30s) |

**Examples**:
```bash
# Pretty-printed event stream
conduit events

# Raw JSON output (for piping to jq)
conduit events --json

# Example output:
# [14:32:05] kb_sync_completed
#          source_id: src_abc123
#          added: 15
#          updated: 3
#          deleted: 0
#          duration: 2.3s
```

---

## Permissions Commands

*(Advanced Mode)*

### `conduit permissions <instance-id>`

Get or set instance permissions.

```bash
conduit permissions <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--set <perm>` | Set permission (format: permission.name=true/false) |
| `--json` | Output as JSON (for GUI) |

Permissions control what operations an instance can perform. This is an Advanced Mode feature for fine-grained access control.

**Examples**:
```bash
# View permissions
conduit permissions abc123

# Set a permission
conduit permissions abc123 --set "filesystem.read=true"

# JSON output for GUI
conduit permissions abc123 --json
```

> **Note**: This feature requires daemon API support (coming in future release).

---

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONDUIT_DATA_DIR` | Data directory path | `~/.conduit` |
| `CONDUIT_SOCKET` | Socket file path | `~/.conduit/conduit.sock` |
| `CONDUIT_LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |
| `CONDUIT_RUNTIME` | Container runtime (podman/docker/auto) | `auto` |
| `CONDUIT_CONFIG` | Config file path | `~/.conduit/conduit.yaml` |
| `OPENAI_API_KEY` | OpenAI API key for KAG | (none) |
| `ANTHROPIC_API_KEY` | Anthropic API key for KAG | (none) |

---

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | General error |
| 2 | Partial success (command-specific, e.g., `kb sync` with semantic failures) |
| 3 | Daemon not running |
| 4 | Instance not found |
| 5 | Permission denied |
| 6 | Timeout |

---

## See Also

- [Quick Start Guide](QUICK_START.md) - Get started in 5 minutes
- [User Guide](USER_GUIDE.md) - Detailed usage instructions
- [Admin Guide](ADMIN_GUIDE.md) - System administration
- [Known Issues](KNOWN_ISSUES.md) - Issues and workarounds
- [KAG HLD](KAG_HLD.md) - Knowledge graph design
- [KB Search HLD](KB_SEARCH_HLD.md) - Search architecture
- [MCP Server Design](MCP_SERVER_DESIGN.md) - MCP server implementation
