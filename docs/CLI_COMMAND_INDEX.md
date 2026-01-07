# Conduit CLI Command Index

**Version**: 0.1.0
**Last Updated**: January 2026

This document provides a complete reference of all CLI commands available in Conduit.

---

## Quick Reference

| Category | Command | Description |
|----------|---------|-------------|
| **Setup** | `conduit setup` | Interactive setup wizard |
| **Setup** | `conduit doctor` | Run diagnostics |
| **Setup** | `conduit install-deps` | Install runtime dependencies |
| **Service** | `conduit service install` | Install daemon as system service |
| **Service** | `conduit service start` | Start the daemon |
| **Service** | `conduit service stop` | Stop the daemon |
| **Service** | `conduit service status` | Show service status |
| **Instance** | `conduit install` | Install a connector |
| **Instance** | `conduit list` | List all instances |
| **Instance** | `conduit start <id>` | Start an instance |
| **Instance** | `conduit stop <id>` | Stop an instance |
| **Instance** | `conduit remove <id>` | Remove an instance |
| **Instance** | `conduit logs <id>` | View instance logs |
| **Client** | `conduit client list` | List detected AI clients |
| **Client** | `conduit client bind` | Bind instance to client |
| **Client** | `conduit client unbind` | Remove binding |
| **KB** | `conduit kb add <path>` | Add document source |
| **KB** | `conduit kb list` | List sources |
| **KB** | `conduit kb sync` | Sync documents |
| **KB** | `conduit kb search <query>` | Search documents |
| **KB** | `conduit kb stats` | Show statistics |
| **KB** | `conduit kb remove <id>` | Remove source |
| **KAG** | `conduit kb kag-sync` | Extract entities from documents |
| **KAG** | `conduit kb kag-status` | Show extraction status |
| **KAG** | `conduit kb kag-query` | Query knowledge graph |
| **FalkorDB** | `conduit falkordb install` | Install FalkorDB container |
| **FalkorDB** | `conduit falkordb start` | Start FalkorDB |
| **FalkorDB** | `conduit falkordb stop` | Stop FalkorDB |
| **FalkorDB** | `conduit falkordb status` | Check FalkorDB status |
| **Qdrant** | `conduit qdrant install` | Install Qdrant container |
| **Qdrant** | `conduit qdrant start` | Start Qdrant |
| **Qdrant** | `conduit qdrant stop` | Stop Qdrant |
| **Qdrant** | `conduit qdrant status` | Check Qdrant status |
| **System** | `conduit status` | Show daemon status |
| **System** | `conduit config show` | Show configuration |
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

### `conduit inspect <instance-id>`

Show detailed information about an instance.

```bash
conduit inspect <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

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

---

## FalkorDB Commands

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

## Qdrant Commands

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

### `conduit config show`

Show current configuration.

```bash
conduit config show [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--json` | Output as JSON |

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
# [14:32:05] 📚 kb_sync_completed
#          source_id: src_abc123
#          added: 15
#          updated: 3
#          deleted: 0
#          duration: 2.3s
```

---

## Permissions Commands

### `conduit permissions show <instance-id>`

Show permissions for an instance.

```bash
conduit permissions show <instance-id>
```

### `conduit permissions grant <instance-id>`

Grant permissions to an instance.

```bash
conduit permissions grant <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--readonly <path>` | Grant read-only access (repeatable) |
| `--readwrite <path>` | Grant read-write access (repeatable) |

### `conduit permissions revoke <instance-id>`

Revoke permissions from an instance.

```bash
conduit permissions revoke <instance-id> [options]
```

**Options**:
| Option | Description |
|--------|-------------|
| `--type <type>` | Permission type to revoke |
| `--all` | Revoke all permissions |

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

- [User Guide](USER_GUIDE.md) - Getting started and tutorials
- [Admin Guide](ADMIN_GUIDE.md) - System administration
- [KAG HLD](KAG_HLD.md) - Knowledge graph design
- [KB Search HLD](KB_SEARCH_HLD.md) - Search architecture
