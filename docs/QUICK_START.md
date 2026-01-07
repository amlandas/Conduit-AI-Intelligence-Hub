# Conduit Quick Start Guide

Get your private knowledge base up and running in 5 minutes.

---

## Step 1: Install Conduit

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
```

**What this installs:**
- Conduit CLI and daemon
- Container runtime (Podman or Docker)
- Ollama for local AI models
- Qdrant vector database
- FalkorDB graph database
- Required AI models

**After installation, restart your terminal** (or run `source ~/.zshrc`).

---

## Step 2: Verify Installation

```bash
conduit doctor
```

You should see all checks passing:

```
Conduit Doctor
──────────────────────────────────────────
✓ Daemon running
✓ Database accessible
✓ Container runtime available
✓ Ollama available
✓ Qdrant available
✓ FalkorDB available
```

If any checks fail, see [Troubleshooting](#troubleshooting).

---

## Step 3: Add Your Documents

```bash
# Add a folder to your knowledge base
conduit kb add ~/Documents/my-project --name "My Project"
```

**Supported formats:**
- Documentation: `.md`, `.txt`, `.rst`
- Code: `.go`, `.py`, `.js`, `.ts`, `.java`, `.rb`, `.rs`
- Documents: `.pdf`, `.doc`, `.docx`
- Config: `.json`, `.yaml`, `.xml`, `.toml`

---

## Step 4: Index Documents

```bash
# Sync and index all documents
conduit kb sync
```

This creates:
- Full-text search index (FTS5) for keyword search
- Vector embeddings (Qdrant) for semantic search

**First sync may take a few minutes** depending on document count.

---

## Step 5: Test Search

```bash
# Hybrid search (keyword + semantic)
conduit kb search "how does authentication work"

# Semantic search (meaning-based)
conduit kb search "user login security" --semantic

# Keyword search (exact matches)
conduit kb search "OAuth2 client_id" --fts5
```

---

## Step 6: Use with AI Tools

### Claude Code (Auto-configured)

Your knowledge base is automatically available in Claude Code after `conduit kb sync`.

### Other AI Tools

Add to your tool's MCP configuration:

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
| Tool | Config File |
|------|-------------|
| Claude Desktop | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Cursor | `.cursor/mcp.json` |
| VS Code | `.vscode/settings.json` (under `mcp.servers`) |

---

## Optional: Enable Knowledge Graph (KAG)

For multi-hop reasoning queries:

```bash
# Extract entities and relationships
conduit kb kag-sync

# Query the knowledge graph
conduit kb kag-query "Kubernetes"
```

---

## Common Workflows

### Check System Status

```bash
conduit status              # Quick status
conduit doctor              # Full diagnostics
conduit kb stats            # Knowledge base statistics
```

### Manage Sources

```bash
conduit kb list             # List all sources
conduit kb sync             # Sync all sources
conduit kb remove <id>      # Remove a source
```

### Update Documents

```bash
# After adding/changing documents, re-sync
conduit kb sync

# Force rebuild vectors (after Qdrant issues)
conduit kb sync --rebuild-vectors
```

---

## Troubleshooting

### Daemon Not Running

```bash
conduit service start
```

### 0 Vectors in Status

```bash
conduit qdrant status              # Check Qdrant
conduit qdrant start               # Start if stopped
conduit kb sync --rebuild-vectors  # Rebuild vectors
```

### MCP Server Not Working

```bash
conduit mcp status     # Check configuration
conduit mcp configure  # Reconfigure for Claude Code
```

### Slow First KAG Query

The extraction model (~4GB) loads on first use. To preload:

```bash
conduit ollama warmup
```

Or enable in config (`~/.conduit/conduit.yaml`):

```yaml
kb:
  kag:
    preload_model: true
```

---

## Next Steps

- **[CLI Command Index](CLI_COMMAND_INDEX.md)** - Complete command reference
- **[User Guide](USER_GUIDE.md)** - Detailed usage instructions
- **[Known Issues](KNOWN_ISSUES.md)** - Issues and workarounds

---

## Getting Help

- **Questions**: [GitHub Discussions](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/discussions)
- **Bug Reports**: [GitHub Issues](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues)
- **Documentation**: [CLI Command Index](CLI_COMMAND_INDEX.md)
