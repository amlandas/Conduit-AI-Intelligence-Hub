# Conduit

### Make Your AI Tools Smarter with a Private Knowledge Base

**Your Documents. Your AI. Your Control.**

[![Latest Release](https://img.shields.io/github/v/release/amlandas/Conduit-AI-Intelligence-Hub?label=release)](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![CLI Version](https://img.shields.io/badge/CLI-v1.0-green.svg)](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/releases/latest)
[![GitHub Discussions](https://img.shields.io/github/discussions/amlandas/Conduit-AI-Intelligence-Hub)](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/discussions)

---

Conduit transforms your local documents into a **private knowledge base** that makes AI tools like Claude Desktop, ChatGPT, Perplexity, and AI coding assistants like Claude Code, Cursor, Copilot, Kiro, and Gemini CLI significantly smarter.

**Everything stays local.** No documents or artifacts ever leave your machine.

## Why Conduit?

AI tools are powerful, but they struggle with:
- **Context bloat**: Feeding too much information overwhelms the AI
- **Missing context**: Not enough information leads to hallucinations
- **Privacy concerns**: Sensitive documents shouldn't leave your machine

Conduit solves this by:
- **Intelligent retrieval**: RAG (Retrieval-Augmented Generation) and KAG (Knowledge-Augmented Generation) find exactly the right context
- **Local-first**: All processing happens on your machine - documents never leave
- **MCP integration**: Works with any AI tool that supports [Model Context Protocol](https://modelcontextprotocol.io)

---

## Quick Start (5 minutes)

### Install via CLI (Recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
```

The installer handles everything:
- Installs Conduit CLI and daemon
- Sets up container runtime (Podman/Docker)
- Installs AI models via Ollama
- Configures vector database (Qdrant) and knowledge graph (FalkorDB)
- Auto-configures MCP server in Claude Code

### Verify Installation

```bash
# Restart terminal, then:
conduit doctor
```

### Add Your Documents

```bash
# Add a folder to your knowledge base
conduit kb add ~/Documents/my-project --name "My Project"

# Sync documents (indexes for search)
conduit kb sync

# Test search
conduit kb search "how does authentication work"
```

**That's it!** Your AI tools now have access to your private knowledge base.

---

## How It Works

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Your Local Machine                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│   ┌──────────────┐         ┌──────────────────────────────────┐    │
│   │ Your Docs    │ ──────► │        Conduit                   │    │
│   │ (PDF, MD,    │         │  ┌────────────────────────────┐  │    │
│   │  Code, etc.) │         │  │ RAG: Semantic + Keyword    │  │    │
│   └──────────────┘         │  │ KAG: Knowledge Graph       │  │    │
│                            │  └────────────────────────────┘  │    │
│                            └───────────────┬──────────────────┘    │
│                                            │                        │
│                                            │ MCP Protocol           │
│                                            ▼                        │
│   ┌────────────────────────────────────────────────────────────┐   │
│   │              AI Tools (via MCP Server)                     │   │
│   │  Claude Code │ Cursor │ Claude Desktop │ ChatGPT │ etc.   │   │
│   └────────────────────────────────────────────────────────────┘   │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Key Technologies

| Component | Purpose |
|-----------|---------|
| **RAG** (Retrieval-Augmented Generation) | Finds semantically similar documents using vector search |
| **KAG** (Knowledge-Augmented Generation) | Builds a knowledge graph for multi-hop reasoning |
| **MCP** (Model Context Protocol) | Standard protocol for AI tools to access external data |
| **Qdrant** | Vector database for semantic search (runs locally in container) |
| **FalkorDB** | Graph database for knowledge graphs (runs locally in container) |
| **Ollama** | Local AI models for embeddings and entity extraction |

---

## CLI Reference

Conduit is a CLI-first tool. For the complete command reference, see **[CLI Command Index](docs/CLI_COMMAND_INDEX.md)**.

### Essential Commands

```bash
# Setup & Health
conduit doctor              # Run diagnostics
conduit status              # Show system status

# Knowledge Base
conduit kb add <path>       # Add document folder
conduit kb sync             # Index documents
conduit kb search <query>   # Search your knowledge base
conduit kb list             # List all sources
conduit kb stats            # Show statistics

# MCP Server
conduit mcp status          # Verify MCP server is configured
conduit mcp configure       # Configure MCP for Claude Code
```

### Search Modes

```bash
# Hybrid search (default) - combines keyword + semantic
conduit kb search "authentication flow"

# Semantic search - finds by meaning
conduit kb search "securing user login" --semantic

# Keyword search - exact matches
conduit kb search "OAuth2 client_id" --fts5
```

### Knowledge Graph Queries

```bash
# Extract entities from your documents
conduit kb kag-sync

# Query relationships
conduit kb kag-query "Kubernetes"
conduit kb kag-query "authentication" --entities OAuth,JWT --max-hops 2
```

---

## MCP Server Integration

Conduit automatically creates an MCP server for your knowledge base and configures it for Claude Code.

### For Claude Code (Auto-configured)

After running `conduit kb sync`, your knowledge base is automatically available in Claude Code.

### For Other AI Tools (Manual Configuration)

Add to your AI tool's MCP configuration:

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

**Configuration locations:**
- **Claude Desktop**: `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Cursor**: `.cursor/mcp.json`
- **VS Code**: `.vscode/settings.json` (under `mcp.servers`)

### Available MCP Tools

| Tool | Description |
|------|-------------|
| `kb_search` | Hybrid search (semantic + keyword) |
| `kb_search_with_context` | Search with merged results and citations |
| `kb_list_sources` | List indexed document sources |
| `kb_get_document` | Retrieve full document content |
| `kb_stats` | Knowledge base statistics |
| `kag_query` | Query knowledge graph for entities |

---

## Installation Options

### CLI Installation (Recommended)

```bash
# Standard install
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash

# Custom options
curl -fsSL ... | bash -s -- --install-dir ~/.local/bin  # Custom location
curl -fsSL ... | bash -s -- --skip-model                # Skip AI model download
curl -fsSL ... | bash -s -- --no-kag                    # Skip knowledge graph setup
curl -fsSL ... | bash -s -- --verbose                   # Verbose output
```

### Manual Installation

```bash
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git
cd Conduit-AI-Intelligence-Hub
make build
sudo cp bin/conduit bin/conduit-daemon /usr/local/bin/
conduit setup
```

### Desktop App (Experimental)

> **Note:** The Desktop App is currently experimental and under active development. The CLI is the recommended way to use Conduit.

For users who prefer a graphical interface, download the DMG from [Releases](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/releases/latest).

<details>
<summary>Desktop App Details</summary>

#### Download

- Apple Silicon (M1/M2/M3/M4): `Conduit-x.x.x-arm64.dmg`

#### macOS Security Note

On first launch, you may see "Conduit.app is damaged". Run:

```bash
xattr -cr /Applications/Conduit.app
```

#### Features (Experimental)

- Dashboard with real-time status
- Knowledge Base management with RAG tuning
- KAG search interface
- Settings and configuration

</details>

---

## Supported Document Formats

| Category | Formats |
|----------|---------|
| **Documentation** | `.md`, `.txt`, `.rst` |
| **Code** | `.go`, `.py`, `.js`, `.ts`, `.java`, `.rs`, `.rb`, `.c`, `.cpp`, `.h`, `.cs`, `.swift`, `.kt` |
| **Scripts** | `.sh`, `.bash`, `.zsh`, `.ps1`, `.bat` |
| **Config** | `.json`, `.yaml`, `.yml`, `.xml`, `.toml`, `.ini` |
| **Data** | `.csv`, `.tsv` |
| **Documents** | `.pdf`, `.doc`, `.docx`, `.odt`, `.rtf` |

---

## Uninstalling

The recommended way to uninstall is via the CLI:

```bash
# Preview what will be removed
conduit uninstall --dry-run --all

# Uninstall (keeps your data)
conduit uninstall --keep-data

# Full uninstall (removes everything)
conduit uninstall --all
```

**Backup method** (if CLI is unavailable):

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/uninstall.sh | bash
```

---

## Troubleshooting

### Quick Diagnostics

```bash
conduit doctor    # Comprehensive health check
conduit status    # System status overview
```

### Common Issues

**Semantic search shows 0 vectors:**
```bash
conduit qdrant status              # Check Qdrant
conduit kb sync --rebuild-vectors  # Rebuild vectors
```

**MCP server not working:**
```bash
conduit mcp status     # Check MCP configuration
conduit mcp configure  # Reconfigure
```

**KAG extraction slow on first run:**
The extraction model (~4GB) loads on first use. Enable preloading in `~/.conduit/conduit.yaml`:
```yaml
kb:
  kag:
    preload_model: true
```

For more troubleshooting, see [Known Issues](docs/KNOWN_ISSUES.md).

---

## Documentation

| Document | Description |
|----------|-------------|
| [CLI Command Index](docs/CLI_COMMAND_INDEX.md) | Complete CLI reference |
| [Quick Start Guide](docs/QUICK_START.md) | Step-by-step getting started |
| [User Guide](docs/USER_GUIDE.md) | Detailed usage instructions |
| [Admin Guide](docs/ADMIN_GUIDE.md) | System administration |
| [Known Issues](docs/KNOWN_ISSUES.md) | Issues and workarounds |
| [MCP Server Design](docs/MCP_SERVER_DESIGN.md) | MCP implementation details |

---

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

- **Bug Reports**: Use the [bug report template](.github/ISSUE_TEMPLATE/bug_report.md)
- **Feature Requests**: Use the [feature request template](.github/ISSUE_TEMPLATE/feature_request.md)
- **Questions**: Ask in [GitHub Discussions](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/discussions)

---

## Requirements

The installer handles these automatically:

| Requirement | Version | Notes |
|-------------|---------|-------|
| macOS or Linux | - | Windows support planned |
| Podman or Docker | 4.0+ / 20.10+ | Container runtime |
| Ollama | Latest | Local AI models |

---

## Privacy & Security

- **100% Local**: All documents and processing stay on your machine
- **No Telemetry**: Conduit doesn't phone home
- **Sandboxed Containers**: Qdrant and FalkorDB run in isolated containers
- **Read-Only MCP**: AI tools can only read, not modify your knowledge base

---

## License

MIT License - see [LICENSE](LICENSE) for details.

---

## Acknowledgments

Built with:
- [Qdrant](https://qdrant.tech/) - Vector database
- [FalkorDB](https://www.falkordb.com/) - Graph database
- [Ollama](https://ollama.ai/) - Local AI models
- [Model Context Protocol](https://modelcontextprotocol.io/) - AI tool integration

---

<p align="center">
  <strong>Conduit v1.0</strong> — Private Knowledge Base for AI Tools
</p>
