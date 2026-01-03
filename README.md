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
- Install document extraction tools (pdftotext, antiword, unrtf)
- Build and install Conduit
- Set up the daemon as a background service
- Pull the default AI model (qwen2.5-coder:7b)
- Install Qdrant vector database (via Docker) for semantic search
- Pull the embedding model (nomic-embed-text) for document vectorization
- Install FalkorDB graph database (via Docker) for knowledge graphs
- Pull the KAG extraction model (mistral:7b-instruct) for entity extraction
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

# Skip KAG components (FalkorDB + Mistral model)
curl -fsSL ... | bash -s -- --no-kag
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
- **Knowledge Base**: Document indexing with multi-format support, MCP server
  - **Semantic Search**: Find documents by meaning using Qdrant vector database + Ollama embeddings
  - **Keyword Search**: Full-text search with SQLite FTS5 as fallback
  - **RAG-Ready**: Perfect for AI client augmentation with ranked results + citations
  - **KAG (Knowledge Graph)**: Entity extraction and graph-based reasoning using FalkorDB
    - Multi-hop reasoning ("How is X related to Y across documents?")
    - Aggregation queries ("List all threat models in the KB")
    - Entity disambiguation via graph traversal
- **AI Integration**: Local AI with Ollama for intelligent code analysis
- **CLI**: Complete command set for all operations

## Requirements

The installer handles these automatically, but for reference:

- Go 1.21+
- Git
- SQLite 3.35+ with FTS5 extension (included in Go build)
- Podman 4.0+ (recommended) or Docker 20.10+ (for running connectors)
- Ollama (for local AI features)
- Qdrant (for semantic search - auto-installed via Docker)
- FalkorDB (for knowledge graphs - auto-installed via Docker)
- Document extraction tools (for KB indexing):
  - pdftotext (poppler) - for PDF files
  - textutil (macOS) / antiword (Linux/Windows) - for DOC files
  - textutil (macOS) / unrtf (Linux) - for RTF files
  - DOCX and ODT are supported natively without external tools

### Semantic Search Components

For full semantic search capabilities:
- **Qdrant**: Vector database running on localhost:6333 (auto-installed via Docker)
- **nomic-embed-text**: Embedding model via Ollama (768 dimensions, auto-pulled)

These are optional - Conduit falls back to keyword search (FTS5) if unavailable.

### Knowledge Graph Components (KAG)

For knowledge graph capabilities:
- **FalkorDB**: Graph database running on localhost:6379 (auto-installed via Docker)
- **mistral:7b-instruct**: Extraction model via Ollama (Apache 2.0 licensed, auto-pulled)

These are optional - skip with `--no-kag` during installation.

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
conduit install --document-tools  # Install document extraction tools
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
conduit kb search <query>     # Search documents (hybrid by default)
conduit kb search <query> --semantic  # Force semantic search only
conduit kb search <query> --fts5      # Force keyword search only
conduit kb remove <name>      # Remove source (cleans up FTS5 + vectors)
conduit kb migrate            # Migrate existing docs to vector store
conduit kb stats              # Show KB statistics

# Advanced search options (RAG tuning)
conduit kb search <query> --min-score 0.05      # Lower similarity threshold
conduit kb search <query> --limit 20            # More results
conduit kb search <query> --semantic-weight 0.8 # Prefer semantic over keyword
conduit kb search <query> --mmr-lambda 0.9      # More relevance, less diversity

# KAG (Knowledge Graph) operations
conduit kb kag-sync               # Extract entities from indexed documents
conduit kb kag-status             # Show extraction progress and entity counts
conduit kb kag-query <query>      # Query the knowledge graph
conduit kb kag-query <query> --entities Docker,Kubernetes  # With entity hints
conduit kb kag-query <query> --max-hops 3  # Multi-hop traversal (max: 3)
```

### Qdrant Management
```bash
conduit qdrant status         # Check Qdrant container and vector count
conduit qdrant install        # Install/start Qdrant container
conduit qdrant start          # Start existing container
conduit qdrant stop           # Stop container (preserves data)
conduit qdrant attach         # Enable semantic search without restart
conduit qdrant purge          # Clear all vectors (useful after reinstall)
```

### FalkorDB Management
```bash
conduit falkordb status       # Check FalkorDB container and graph stats
conduit falkordb install      # Install/start FalkorDB container
conduit falkordb start        # Start existing container
conduit falkordb stop         # Stop container (preserves data)
```

**Search Modes:**
- **Hybrid (default)**: Tries semantic search first, falls back to keyword search
- **Semantic (`--semantic`)**: Vector-based search using embeddings (requires Qdrant + Ollama)
- **Keyword (`--fts5`)**: Full-text keyword search using SQLite FTS5

Semantic search understands meaning - "understanding text with computers" matches documents about "natural language processing" even without exact keyword matches.

**KB Removal**: When you remove a source with `conduit kb remove`, both FTS5 entries and Qdrant vectors are automatically cleaned up. The output shows deletion statistics:
```
✓ Removed source: Project Docs (42 documents, 420 vectors)
```

**Supported Document Formats:**
- Text: `.md`, `.txt`, `.rst`
- Code: `.go`, `.py`, `.js`, `.ts`, `.java`, `.rs`, `.rb`, `.c`, `.cpp`, `.h`, `.hpp`, `.cs`, `.swift`, `.kt`
- Scripts: `.sh`, `.bash`, `.zsh`, `.fish`, `.ps1`, `.bat`, `.cmd`
- Config: `.json`, `.yaml`, `.yml`, `.xml`, `.jsonld`, `.toml`, `.ini`, `.cfg`
- Data: `.csv`, `.tsv`
- Documents: `.pdf`, `.doc`, `.docx`, `.odt`, `.rtf`

### System
```bash
conduit status                # Show daemon status
conduit config                # Show configuration
conduit config --all          # Show full configuration
conduit backup                # Backup data directory
conduit doctor                # Run comprehensive diagnostics
```

### MCP Operations
```bash
conduit mcp kb                # Run knowledge base MCP server (read-only)
conduit mcp status            # Show MCP server capabilities and health
conduit mcp logs              # View MCP-related logs
conduit mcp stdio --instance <id>  # Run connector MCP server over stdio
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

## MCP Server for AI Clients

Conduit provides a **read-only** MCP server that enables AI clients (Claude Code, Cursor, VS Code) to search and retrieve documents from your private knowledge base.

### Design Principles

- **AI Reads, Humans Write**: The MCP server provides read-only access. All administrative operations (add/remove/sync sources) are reserved for the CLI.
- **Safety First**: No destructive operations are exposed via MCP to prevent accidental or adversarial knowledge base modifications.
- **Graceful Degradation**: Works with FTS5 (keyword search) alone and enhances with semantic search when Qdrant + Ollama are available.

### Available Tools

| Tool | Description |
|------|-------------|
| `kb_search` | Search knowledge base with hybrid search (FTS5 + semantic) |
| `kb_search_with_context` | Search with merged, prompt-ready results and citations |
| `kb_list_sources` | List all indexed sources with statistics |
| `kb_get_document` | Retrieve full document content by ID |
| `kb_stats` | Get knowledge base statistics |
| `kag_query` | Query knowledge graph for entities and relationships |

### Configuration

**Claude Code** (`~/.claude.json`):
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

**Cursor** (`.cursor/settings/extensions.json`):
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

**VS Code** (`.vscode/settings.json`):
```json
{
  "mcp.servers": {
    "conduit-kb": {
      "command": "conduit",
      "args": ["mcp", "kb"]
    }
  }
}
```

### Checking MCP Status

```bash
# Show MCP server capabilities, health, and source statistics
conduit mcp status

# View MCP-related logs
conduit mcp logs
```

### Source-Aware Search

All search operations support filtering by source ID for multi-KB scenarios:

```json
{
  "name": "kb_search",
  "arguments": {
    "query": "authentication",
    "source_id": "my-project-docs",
    "limit": 10
  }
}
```

Use `kb_list_sources` to see available source IDs.

### Search Modes

The MCP server supports three search modes via the `mode` parameter:

| Mode | Description |
|------|-------------|
| `hybrid` | (Default) Combines keyword + semantic search with RRF fusion |
| `semantic` | Vector similarity only (requires Qdrant + Ollama) |
| `fts5` | Keyword matching only (always available) |

For detailed MCP server documentation, see [docs/MCP_SERVER_DESIGN.md](docs/MCP_SERVER_DESIGN.md).

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
  max_file_size: 104857600  # 100MB

  # RAG (Retrieval-Augmented Generation) tuning
  # These control how semantic search retrieves and ranks results
  rag:
    min_score: 0.0        # Minimum similarity threshold (0.0-1.0)
                          # 0 = no filtering, return all results ranked by score
    semantic_weight: 0.5  # Balance: 0.0=keyword only, 1.0=semantic only
    enable_mmr: true      # Maximal Marginal Relevance for diversity
    mmr_lambda: 0.7       # 0.0=max diversity, 1.0=max relevance
    enable_rerank: true   # Re-score top candidates semantically
    default_limit: 10     # Default number of results

  # KAG (Knowledge-Augmented Generation) settings
  kag:
    enabled: true         # Enable knowledge graph (requires FalkorDB)
    provider: ollama      # LLM provider: ollama, openai, anthropic
    ollama:
      model: mistral:7b-instruct-q4_K_M  # Extraction model
      host: http://localhost:11434
    graph:
      backend: falkordb
      falkordb:
        host: localhost
        port: 6379
        graph_name: conduit_kg
    extraction:
      confidence_threshold: 0.7  # Minimum confidence for entities
      max_entities_per_chunk: 20
      workers: 2                 # Background extraction workers

policy:
  allow_network_egress: false
  forbidden_paths:
    - /
    - /etc
    - ~/.ssh
    - ~/.aws
```

## Uninstalling

### Complete Removal (Recommended)

Remove Conduit and optionally its dependencies with one command:

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/uninstall.sh | bash
```

The uninstall script will:
- Stop and remove the daemon service
- Remove Qdrant vector database container
- Remove FalkorDB graph database container
- Remove binaries from your PATH
- Optionally remove data directory
- Clean up shell configuration
- Optionally remove dependencies (Docker/Podman, Ollama, Go)

**Options:**
```bash
# Force mode (skip confirmations)
curl -fsSL ... | bash -s -- --force

# Remove everything including dependencies
curl -fsSL ... | bash -s -- --remove-all

# Custom paths
curl -fsSL ... | bash -s -- --install-dir ~/.local/bin --conduit-home ~/.conduit
```

The script gracefully handles errors and continues with remaining components.

### Manual Uninstallation

If you prefer manual removal:

```bash
# 1. Stop and remove service
conduit service stop
conduit service remove

# 2. Remove binaries
rm -f ~/.local/bin/conduit ~/.local/bin/conduit-daemon

# 3. Remove data
rm -rf ~/.conduit

# 4. Clean shell config (remove PATH exports)
# Edit ~/.zshrc or ~/.bashrc and remove Conduit PATH line
```

## Troubleshooting

### Common Issues

**Semantic search not working (0 vectors)**
```bash
# Check if Qdrant is running
curl http://localhost:6333/collections

# Check daemon logs for errors
cat ~/.conduit/daemon.log | grep -E "(error|warn)" | tail -20

# Restart Qdrant container
podman restart conduit-qdrant  # or: docker restart conduit-qdrant
```

**Documents show 0 added after sync**
- Documents may already be indexed with matching hashes
- Check: `conduit kb stats` to see current document count
- Force re-index: Remove source and re-add it

**Daemon can't find pdftotext or other tools**
```bash
# On macOS, install poppler via Homebrew
brew install poppler

# Then restart the daemon service
conduit service stop && conduit service start
```

**Container operations fail with credential errors**
```bash
# If you see "docker-credential-gcloud" errors, the install script
# handles this automatically. For manual operation:
echo '{"auths": {}}' > ~/.docker/config.json.tmp
mv ~/.docker/config.json ~/.docker/config.json.backup
mv ~/.docker/config.json.tmp ~/.docker/config.json
# Run your container command, then restore:
mv ~/.docker/config.json.backup ~/.docker/config.json
```

**KAG entity extraction not working**
```bash
# Check if FalkorDB is running
conduit falkordb status

# Check if extraction model is available
ollama list | grep mistral

# Check extraction status
conduit kb kag-status

# Force re-extraction
conduit kb kag-sync --force
```

**KAG query returns empty results**
- Ensure documents have been synced: `conduit kb sync`
- Run entity extraction: `conduit kb kag-sync`
- Check extraction status: `conduit kb kag-status`
- Lower confidence threshold in config if extractions are being filtered

### Diagnostic Commands

```bash
# Check daemon status
conduit status

# Run comprehensive diagnostics
conduit doctor

# Check KB statistics
conduit kb stats

# View daemon logs
cat ~/.conduit/daemon.log | tail -50

# Check Qdrant vector count
curl -s http://localhost:6333/collections/conduit_kb | jq '.result.points_count'

# Check FalkorDB graph stats
conduit falkordb status

# Check KAG extraction status
conduit kb kag-status
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
