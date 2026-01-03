# Conduit Administrator Guide

**Version**: 0.1.0
**Last Updated**: December 2025

---

## Table of Contents

1. [Deployment Overview](#deployment-overview)
2. [System Requirements](#system-requirements)
3. [Installation & Configuration](#installation--configuration)
4. [Daemon Management](#daemon-management)
5. [Security Configuration](#security-configuration)
6. [Database Administration](#database-administration)
7. [Monitoring & Logging](#monitoring--logging)
8. [Backup & Recovery](#backup--recovery)
9. [Performance Tuning](#performance-tuning)
10. [Troubleshooting](#troubleshooting)
11. [Upgrading](#upgrading)
12. [Uninstallation](#uninstallation)

---

## Deployment Overview

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                         User Workstation                         │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐     ┌──────────────────────────────────────┐  │
│  │  AI Clients  │     │          Conduit Daemon               │  │
│  │              │     │                                       │  │
│  │ Claude Code ─┼────►│  ┌─────────┐  ┌──────────────────┐   │  │
│  │ Cursor      ─┼────►│  │ HTTP    │  │ Lifecycle        │   │  │
│  │ VS Code     ─┼────►│  │ API     │  │ Manager          │   │  │
│  │ Gemini CLI  ─┼────►│  └────┬────┘  └────────┬─────────┘   │  │
│  └──────────────┘     │       │                │              │  │
│                       │       ▼                ▼              │  │
│                       │  ┌─────────┐  ┌──────────────────┐   │  │
│                       │  │ Policy  │  │ Container        │   │  │
│                       │  │ Engine  │  │ Runtime          │   │  │
│                       │  └────┬────┘  └────────┬─────────┘   │  │
│                       │       │                │              │  │
│                       │       ▼                ▼              │  │
│                       │  ┌─────────────────────────────────┐ │  │
│                       │  │           SQLite DB              │ │  │
│                       │  │  (instances, bindings, kb, etc)  │ │  │
│                       │  └─────────────────────────────────┘ │  │
│                       └──────────────────────────────────────┘  │
│                                        │                         │
│                                        ▼                         │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │                  Podman/Docker Runtime                    │   │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐      │   │
│  │  │Connector│  │Connector│  │Connector│  │   KB    │      │   │
│  │  │   #1    │  │   #2    │  │   #3    │  │  MCP    │      │   │
│  │  └─────────┘  └─────────┘  └─────────┘  └─────────┘      │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Process | Port/Socket |
|-----------|---------|-------------|
| Daemon | `conduit-daemon` | Unix socket: `~/.conduit/conduit.sock` |
| CLI | `conduit` | Connects to daemon socket |
| Connectors | Container processes | Varies by connector |
| Database | Embedded SQLite | `~/.conduit/conduit.db` |

---

## System Requirements

### Minimum Requirements

| Resource | Requirement |
|----------|-------------|
| OS | macOS 12+, Ubuntu 20.04+, Windows 10+ |
| CPU | 2 cores |
| RAM | 4 GB |
| Disk | 1 GB free |
| Container Runtime | Podman 4.0+ or Docker 20.10+ |

### Recommended Requirements

| Resource | Requirement |
|----------|-------------|
| CPU | 4+ cores |
| RAM | 8+ GB |
| Disk | 10+ GB SSD |
| Container Runtime | Podman 4.0+ (rootless) |

### Software Dependencies

| Dependency | Version | Required |
|------------|---------|----------|
| Go | 1.21+ | Build only |
| SQLite | 3.35+ | Runtime |
| Podman | 4.0+ | Runtime (recommended) |
| Docker | 20.10+ | Runtime (alternative) |
| GCC | Any | Build (CGO) |

---

## Installation & Configuration

### Automated Installation (Recommended)

The easiest way to install Conduit is using the one-click installer:

```bash
curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
```

The installer handles:
- Operating system and architecture detection
- Installation of dependencies (Go, Git, Docker/Podman, Ollama)
- Building Conduit from source
- Installing binaries to PATH
- Setting up the daemon as a system service
- Downloading the default AI model

**Installation Options**:
```bash
# Custom install location
curl -fsSL ... | bash -s -- --install-dir /opt/conduit/bin

# Skip daemon service setup
curl -fsSL ... | bash -s -- --no-service

# Verbose output
curl -fsSL ... | bash -s -- --verbose
```

### Building from Source

For custom builds or development:

```bash
# Clone repository
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git
cd conduit

# Install Go dependencies
go mod download

# Build with FTS5 support (required)
make build

# Install to PATH
sudo cp bin/conduit bin/conduit-daemon /usr/local/bin/

# Install runtime dependencies
conduit install-deps

# Set up daemon service
conduit service install
conduit service start

# Verify installation
conduit doctor
```

### Build Flags

The Makefile uses these critical flags:

```makefile
CGO_ENABLED=1           # Required for SQLite
GOTAGS=-tags "fts5"     # Required for full-text search
GOFLAGS=-trimpath       # Reproducible builds
```

### Directory Structure

```
~/.conduit/
├── conduit.sock        # Unix socket for IPC
├── conduit.db          # SQLite database
├── conduit.db-wal      # Write-ahead log
├── conduit.db-shm      # Shared memory file
├── conduit.yaml        # User configuration (optional)
├── backups/            # Configuration backups
│   └── <changeset-id>/ # Per-changeset backups
└── logs/               # Log files (future)
```

### Configuration File

Create `~/.conduit/conduit.yaml`:

```yaml
# Data directory
data_dir: ~/.conduit

# Unix socket path
socket: ~/.conduit/conduit.sock

# Logging configuration
log_level: info  # debug, info, warn, error

# Container runtime
runtime:
  preferred: auto  # podman, docker, or auto
  health_interval: 30s
  default_timeout: 60s

# Knowledge base settings
kb:
  chunk_size: 1000      # Characters per chunk
  chunk_overlap: 100    # Overlap between chunks
  max_file_size: 104857600  # 100MB max file size

  # RAG (Retrieval-Augmented Generation) tuning
  # Controls how semantic search retrieves and ranks results
  rag:
    min_score: 0.0        # Minimum similarity threshold (0.0-1.0)
                          # Lower = more results, let consuming LLM decide relevance
    semantic_weight: 0.5  # Balance between semantic and keyword (0.0-1.0)
                          # 0.0 = keyword only, 1.0 = semantic only
    enable_mmr: true      # Maximal Marginal Relevance for diversity
    mmr_lambda: 0.7       # Relevance vs diversity (0.0-1.0)
                          # 0.0 = max diversity, 1.0 = max relevance
    enable_rerank: true   # Re-score top candidates semantically
    default_limit: 10     # Default number of results

# Policy settings
policy:
  allow_network_egress: false  # Default network policy
  forbidden_paths:
    - /
    - /etc
    - /var
    - ~/.ssh
    - ~/.aws
    - ~/.gnupg
    - ~/.config/gcloud
```

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `CONDUIT_DATA_DIR` | Data directory | `~/.conduit` |
| `CONDUIT_SOCKET` | Socket path | `~/.conduit/conduit.sock` |
| `CONDUIT_LOG_LEVEL` | Log level | `info` |
| `CONDUIT_RUNTIME` | Runtime preference | `auto` |
| `CONDUIT_CONFIG` | Config file path | `~/.conduit/conduit.yaml` |

---

## Daemon Management

### Using the Service Commands (Recommended)

The easiest way to manage the Conduit daemon is through the built-in service commands:

```bash
# Install daemon as a system service
conduit service install

# Start the daemon service
conduit service start

# Check service status
conduit service status

# Stop the daemon service
conduit service stop

# Remove the service (for uninstallation)
conduit service remove
```

These commands automatically use the appropriate service manager for your OS:
- **macOS**: launchd (LaunchAgents)
- **Linux**: systemd (user services)

### Manual Daemon Control

For development or troubleshooting, run the daemon manually:

```bash
# Foreground mode (development)
conduit-daemon --foreground --log-level=debug

# Background mode
conduit-daemon &

# With custom config
conduit-daemon --config /path/to/conduit.yaml
```

### Daemon Options

| Option | Description |
|--------|-------------|
| `--foreground` | Run in foreground (don't daemonize) |
| `--log-level` | Set log level (debug/info/warn/error) |
| `--config` | Path to configuration file |
| `--socket` | Override socket path |
| `--data-dir` | Override data directory |

### Stopping the Daemon

```bash
# Using service command (recommended)
conduit service stop

# Or graceful shutdown via signal
kill -TERM $(pgrep conduit-daemon)
```

### Custom systemd Service (Linux)

If you need custom service configuration, create `/etc/systemd/system/conduit.service`:

```ini
[Unit]
Description=Conduit AI Intelligence Hub
After=network.target

[Service]
Type=simple
User=%i
ExecStart=/usr/local/bin/conduit-daemon --foreground
Restart=on-failure
RestartSec=5

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=read-only
PrivateTmp=true
ReadWritePaths=/home/%i/.conduit

[Install]
WantedBy=multi-user.target
```

Enable and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable conduit@$USER
sudo systemctl start conduit@$USER
```

**Note**: The `conduit service install` command creates a user-level systemd service at `~/.config/systemd/user/conduit.service`, which doesn't require sudo.

### Custom launchd Service (macOS)

If you need custom service configuration, create `~/Library/LaunchAgents/com.simpleflo.conduit.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.simpleflo.conduit</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/conduit-daemon</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/Users/USERNAME/.conduit/logs/stdout.log</string>
    <key>StandardErrorPath</key>
    <string>/Users/USERNAME/.conduit/logs/stderr.log</string>
</dict>
</plist>
```

Load and start:
```bash
launchctl load ~/Library/LaunchAgents/com.simpleflo.conduit.plist
```

**Note**: The `conduit service install` command creates this file automatically.

---

## Security Configuration

### Policy Engine

The policy engine evaluates all permission requests:

```
Request → Built-in Rules → Forbidden Paths → User Grants → Decision
             (Priority 0)     (Blocklist)      (Allowlist)   (ALLOW/WARN/DENY)
```

### Configuring Forbidden Paths

Default forbidden paths (cannot be overridden):

```go
var forbiddenPaths = []string{
    "/",                    // Root filesystem
    "/etc",                 // System configuration
    "/var",                 // Variable data
    "/root",                // Root home
    "/System",              // macOS system
    "/Library",             // macOS libraries
    "/private",             // macOS private
    "C:\\Windows",          // Windows system
    "C:\\Program Files",    // Windows programs
    "C:\\ProgramData",      // Windows data
}
```

Forbidden patterns (relative to home):

```go
var forbiddenPatterns = []string{
    ".ssh",                 // SSH keys
    ".gnupg",               // GPG keys
    ".aws",                 // AWS credentials
    ".config/gcloud",       // GCP credentials
    ".azure",               // Azure credentials
    ".kube",                // Kubernetes config
    ".docker",              // Docker config
    "Library/Keychains",    // macOS Keychain
    "AppData/Roaming",      // Windows app data
}
```

### Allowed Exceptions

Paths that bypass forbidden checks:

```go
var allowedPaths = []string{
    "/tmp",                 // Temp directory
    "/var/folders",         // macOS temp
    "/private/var/folders", // macOS temp (resolved)
    "/var/tmp",             // Persistent temp
}
```

### Container Security Defaults

All connectors run with:

```go
Security: runtime.SecuritySpec{
    ReadOnlyRootfs:   true,   // Immutable filesystem
    NoNewPrivileges:  true,   // No privilege escalation
    DropCapabilities: []string{"ALL"},  // Drop all caps
}

Network: runtime.NetworkSpec{
    Mode: "none",  // No network by default
}
```

### Auditing

All policy decisions are logged:

```json
{
  "level": "info",
  "component": "policy",
  "decision_id": "abc123-def456",
  "decision": "DENY",
  "reason": "Forbidden filesystem access requested",
  "warning_count": 0,
  "block_count": 1,
  "time": "2024-12-28T10:30:00Z"
}
```

---

## Database Administration

### Database Location

```bash
~/.conduit/conduit.db      # Main database
~/.conduit/conduit.db-wal  # Write-ahead log
~/.conduit/conduit.db-shm  # Shared memory
```

### Schema Overview

```sql
-- Connector instances
CREATE TABLE connector_instances (
    instance_id TEXT PRIMARY KEY,
    package_id TEXT NOT NULL,
    package_version TEXT,
    display_name TEXT,
    image_ref TEXT,
    status TEXT DEFAULT 'CREATED',
    container_id TEXT,
    socket_path TEXT,
    config TEXT,  -- JSON
    error_message TEXT,
    health_status TEXT,
    last_health_check TEXT,
    created_at TEXT,
    updated_at TEXT,
    started_at TEXT,
    stopped_at TEXT
);

-- Client bindings
CREATE TABLE client_bindings (
    binding_id TEXT PRIMARY KEY,
    instance_id TEXT REFERENCES connector_instances,
    client_id TEXT NOT NULL,
    config_path TEXT,
    scope TEXT DEFAULT 'user',
    change_set_id TEXT,
    status TEXT DEFAULT 'UNBOUND',
    created_at TEXT,
    updated_at TEXT
);

-- Permission grants
CREATE TABLE user_grants (
    instance_id TEXT,
    permission_type TEXT,
    grant_data TEXT,  -- JSON
    granted_at TEXT,
    PRIMARY KEY (instance_id, permission_type)
);

-- Knowledge base sources
CREATE TABLE kb_sources (
    source_id TEXT PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    name TEXT,
    sync_mode TEXT DEFAULT 'manual',
    last_sync TEXT,
    document_count INTEGER DEFAULT 0,
    created_at TEXT
);

-- Knowledge base documents
CREATE TABLE kb_documents (
    document_id TEXT PRIMARY KEY,
    source_id TEXT REFERENCES kb_sources,
    path TEXT NOT NULL,
    title TEXT,
    mime_type TEXT,
    content_hash TEXT,
    indexed_at TEXT
);

-- Knowledge base chunks (FTS5)
CREATE VIRTUAL TABLE kb_chunks_fts USING fts5(
    chunk_id,
    document_id,
    content,
    content='kb_chunks',
    content_rowid='rowid'
);
```

### Database Maintenance

```bash
# Check database integrity
sqlite3 ~/.conduit/conduit.db "PRAGMA integrity_check;"

# Vacuum database (reclaim space)
sqlite3 ~/.conduit/conduit.db "VACUUM;"

# Analyze for query optimization
sqlite3 ~/.conduit/conduit.db "ANALYZE;"

# Check FTS5 integrity
sqlite3 ~/.conduit/conduit.db "INSERT INTO kb_chunks_fts(kb_chunks_fts) VALUES('integrity-check');"
```

### Database Backup

```bash
# Simple backup
cp ~/.conduit/conduit.db ~/.conduit/conduit.db.backup

# With WAL checkpoint
sqlite3 ~/.conduit/conduit.db "PRAGMA wal_checkpoint(TRUNCATE);"
cp ~/.conduit/conduit.db ~/.conduit/conduit.db.backup

# Using CLI
./bin/conduit backup --output /path/to/backup.db
```

---

## Monitoring & Logging

### Log Format

Conduit uses structured JSON logging:

```json
{
  "level": "info",
  "component": "daemon",
  "event": "daemon_started",
  "socket": "/Users/user/.conduit/conduit.sock",
  "data_dir": "/Users/user/.conduit",
  "time": "2024-12-28T10:30:00Z",
  "caller": "daemon.go:181"
}
```

### Log Levels

| Level | Description |
|-------|-------------|
| `debug` | Detailed debugging information |
| `info` | Normal operational messages |
| `warn` | Warning conditions |
| `error` | Error conditions |

### Key Events to Monitor

| Event | Component | Significance |
|-------|-----------|--------------|
| `daemon_started` | daemon | Daemon startup |
| `daemon_stopped` | daemon | Daemon shutdown |
| `instance status transition` | lifecycle | State changes |
| `policy decision` | policy | Permission evaluations |
| `indexed document` | kb.indexer | Document indexing |
| `search completed` | kb.searcher | Search queries |

### Health Checks

```bash
# CLI health check
./bin/conduit status

# API health check
curl --unix-socket ~/.conduit/conduit.sock http://localhost/api/v1/health
```

### Metrics (Future)

Planned metrics for V1:
- Instance count by status
- Request latency histograms
- Policy decision rates
- KB search latency
- Container resource usage

---

## Backup & Recovery

### What to Backup

| Path | Contents | Priority |
|------|----------|----------|
| `~/.conduit/conduit.db` | All state data | Critical |
| `~/.conduit/conduit.yaml` | Configuration | Important |
| `~/.conduit/backups/` | Client config backups | Important |

### Backup Procedure

```bash
#!/bin/bash
# backup-conduit.sh

BACKUP_DIR="/path/to/backups"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_PATH="$BACKUP_DIR/conduit_$TIMESTAMP"

mkdir -p "$BACKUP_PATH"

# Checkpoint WAL
sqlite3 ~/.conduit/conduit.db "PRAGMA wal_checkpoint(TRUNCATE);"

# Copy database
cp ~/.conduit/conduit.db "$BACKUP_PATH/"

# Copy config
cp ~/.conduit/conduit.yaml "$BACKUP_PATH/" 2>/dev/null || true

# Copy client backups
cp -r ~/.conduit/backups "$BACKUP_PATH/" 2>/dev/null || true

echo "Backup created: $BACKUP_PATH"
```

### Recovery Procedure

```bash
#!/bin/bash
# restore-conduit.sh

BACKUP_PATH="$1"

if [ -z "$BACKUP_PATH" ]; then
    echo "Usage: restore-conduit.sh /path/to/backup"
    exit 1
fi

# Stop daemon
pkill conduit-daemon

# Restore database
cp "$BACKUP_PATH/conduit.db" ~/.conduit/

# Restore config
cp "$BACKUP_PATH/conduit.yaml" ~/.conduit/ 2>/dev/null || true

# Restore client backups
cp -r "$BACKUP_PATH/backups" ~/.conduit/ 2>/dev/null || true

# Restart daemon
./bin/conduit-daemon &

echo "Restore completed"
```

### Disaster Recovery

In case of database corruption:

1. Stop the daemon
2. Rename corrupted database: `mv conduit.db conduit.db.corrupt`
3. Restore from backup
4. Start daemon (will run migrations if needed)
5. Re-sync knowledge base: `./bin/conduit kb sync`
6. Verify bindings: `./bin/conduit client list`

---

## FalkorDB Administration

FalkorDB is an optional graph database used for advanced KAG (Knowledge-Augmented Generation) queries. It enables multi-hop graph traversal that SQLite alone cannot efficiently handle.

### Installing FalkorDB

```bash
# Install FalkorDB container (requires Docker/Podman)
conduit falkordb install

# This pulls falkordb/falkordb:latest and creates the container
```

### Managing FalkorDB

```bash
# Start FalkorDB
conduit falkordb start

# Stop FalkorDB
conduit falkordb stop

# Check status
conduit falkordb status

# View logs
docker logs conduit-falkordb
```

### FalkorDB Status Output

```
FalkorDB Status
───────────────
Container: conduit-falkordb
Status:    Running
Port:      6379 (localhost only)
Uptime:    2h 15m

Graph Statistics:
  Graph:     conduit_kg
  Nodes:     3,456
  Edges:     1,234
  Memory:    45 MB
```

### Configuration

Add to `~/.conduit/conduit.yaml`:

```yaml
kb:
  kag:
    enabled: true
    graph:
      backend: falkordb  # or "sqlite" for basic mode
      falkordb:
        host: localhost
        port: 6379
        graph_name: conduit_kg
```

### Security Considerations

| Aspect | Default | Recommendation |
|--------|---------|----------------|
| Network binding | localhost only | Keep as-is for single-machine |
| Authentication | None | Add Redis AUTH for shared environments |
| Persistence | RDB snapshots | Enable AOF for durability |

### Enabling Redis AUTH (Optional)

For shared environments, enable authentication:

```bash
# Start with password
docker run -d --name conduit-falkordb \
  -p 127.0.0.1:6379:6379 \
  -e REDIS_ARGS="--requirepass your-secure-password" \
  falkordb/falkordb:latest
```

Update configuration:

```yaml
kb:
  kag:
    graph:
      falkordb:
        password: your-secure-password
```

### Backup and Restore

```bash
# Backup (RDB snapshot)
docker exec conduit-falkordb redis-cli BGSAVE
docker cp conduit-falkordb:/data/dump.rdb ./falkordb-backup.rdb

# Restore
docker stop conduit-falkordb
docker cp ./falkordb-backup.rdb conduit-falkordb:/data/dump.rdb
docker start conduit-falkordb
```

### Monitoring

```bash
# Memory usage
docker exec conduit-falkordb redis-cli INFO memory

# Graph statistics
docker exec conduit-falkordb redis-cli GRAPH.QUERY conduit_kg "MATCH (n) RETURN count(n)"

# Slow queries
docker exec conduit-falkordb redis-cli GRAPH.SLOWLOG conduit_kg
```

### Troubleshooting FalkorDB

#### Container Won't Start

```bash
# Check for port conflicts
lsof -i :6379

# Check container logs
docker logs conduit-falkordb

# Recreate container
docker rm conduit-falkordb
conduit falkordb install
```

#### High Memory Usage

```bash
# Check memory
docker exec conduit-falkordb redis-cli INFO memory

# Set memory limit
docker update --memory 1g conduit-falkordb
```

#### Graph Corruption

```bash
# Drop and recreate graph
docker exec conduit-falkordb redis-cli GRAPH.DELETE conduit_kg

# Re-sync entities from SQLite
conduit kb kag-sync --force
```

---

## KAG (Knowledge-Augmented Generation) Administration

### Enabling KAG

KAG is disabled by default for security. To enable:

```yaml
# ~/.conduit/conduit.yaml
kb:
  kag:
    enabled: true
    provider: ollama
    ollama:
      host: http://localhost:11434
      model: mistral:7b-instruct-q4_K_M
```

### LLM Provider Options

**Ollama (Default - Local)**:
```yaml
kb:
  kag:
    provider: ollama
    ollama:
      host: http://localhost:11434
      model: mistral:7b-instruct-q4_K_M
```

**OpenAI**:
```yaml
kb:
  kag:
    provider: openai
    openai:
      model: gpt-4o-mini
      # API key from environment: OPENAI_API_KEY
```

**Anthropic**:
```yaml
kb:
  kag:
    provider: anthropic
    anthropic:
      model: claude-3-5-sonnet-20241022
      # API key from environment: ANTHROPIC_API_KEY
```

### Extraction Configuration

```yaml
kb:
  kag:
    extraction:
      confidence_threshold: 0.6   # Min confidence (0.0-1.0)
      max_entities_per_chunk: 20  # Limit per chunk
      max_relations_per_chunk: 30 # Limit per chunk
      batch_size: 10              # Chunks per batch
      enable_background: true     # Background extraction
      background_workers: 2       # Worker count
      queue_size: 1000            # Max queue size
```

### Monitoring Extraction

```bash
# Check status
conduit kb kag-status

# View extraction logs
grep "kb.extractor" ~/.conduit/logs/conduit.log

# Check queue size
conduit kb kag-status --json | jq '.queue_size'
```

### KAG Database Tables

```sql
-- Entity storage
SELECT COUNT(*) FROM kb_entities;
SELECT type, COUNT(*) FROM kb_entities GROUP BY type;

-- Relation storage
SELECT predicate, COUNT(*) FROM kb_relations GROUP BY predicate;

-- Extraction status
SELECT status, COUNT(*) FROM kb_extraction_status GROUP BY status;

-- Failed extractions
SELECT chunk_id, error_message FROM kb_extraction_status WHERE status = 'failed';
```

### Re-extracting Failed Chunks

```bash
# Reset failed chunks
sqlite3 ~/.conduit/conduit.db "UPDATE kb_extraction_status SET status = 'pending' WHERE status = 'failed';"

# Re-sync
conduit kb kag-sync
```

### Clearing KAG Data

```bash
# Clear all KAG data (entities, relations, status)
sqlite3 ~/.conduit/conduit.db << 'EOF'
DELETE FROM kb_entities;
DELETE FROM kb_relations;
DELETE FROM kb_extraction_status;
EOF

# Also clear FalkorDB if used
docker exec conduit-falkordb redis-cli GRAPH.DELETE conduit_kg
```

---

## Performance Tuning

### SQLite Optimizations

Add to `conduit.yaml`:

```yaml
database:
  pragma:
    journal_mode: WAL      # Write-ahead logging
    synchronous: NORMAL    # Balance safety/speed
    cache_size: -64000     # 64MB cache
    mmap_size: 268435456   # 256MB memory-mapped I/O
```

### Container Runtime

For better container performance:

```yaml
runtime:
  preferred: podman       # Podman is faster for rootless
  default_memory: 256     # MB per container
  default_cpus: 0.5       # CPU cores per container
```

### Knowledge Base

For large document collections:

```yaml
kb:
  chunk_size: 500         # Smaller chunks = faster search
  chunk_overlap: 50       # Less overlap = less storage
  batch_size: 100         # Documents per transaction
  max_results: 50         # Limit search results
```

### RAG Tuning for AI Clients

When Conduit is used as a retrieval layer for AI clients (Claude, GPT, etc.), optimize for **recall over precision**. The consuming LLM has world knowledge and can filter irrelevant results.

```yaml
kb:
  rag:
    # Permissive threshold - let the LLM decide relevance
    min_score: 0.05       # Very low threshold for domain-specific terms

    # Balanced retrieval
    semantic_weight: 0.5  # Equal weight to semantic and keyword

    # Maximize result diversity for AI consumption
    enable_mmr: true
    mmr_lambda: 0.6       # Slightly favor diversity over relevance

    # Enable reranking for best quality
    enable_rerank: true

    # More results for LLM to work with
    default_limit: 15
```

**CLI Override Examples**:

```bash
# For domain-specific terminology (ASL-3, CBRN, etc.)
conduit kb search "ASL-3 deployment safeguards" --min-score 0.0 --limit 20

# For code search (exact matches matter)
conduit kb search "func NewAuthHandler" --fts5 --semantic-weight 0.2

# Maximum diversity for exploratory queries
conduit kb search "authentication patterns" --mmr-lambda 0.4
```

### Health Check Tuning

```yaml
runtime:
  health_interval: 60s    # Less frequent = less overhead
  health_timeout: 10s     # Fail fast
```

---

## Troubleshooting

### Common Issues

#### 1. Daemon Fails to Start

```
Error: create daemon: create store: migrate database: no such module: fts5
```

**Cause**: Built without FTS5 support
**Solution**: Rebuild with `make clean && make build`

#### 2. Socket Permission Denied

```
Error: dial unix ~/.conduit/conduit.sock: permission denied
```

**Cause**: Socket owned by different user
**Solution**:
```bash
rm ~/.conduit/conduit.sock
./bin/conduit-daemon --foreground
```

#### 3. Container Runtime Not Found

```
Error: no container runtime available
```

**Cause**: Neither Podman nor Docker installed/running
**Solution**: Install Podman or Docker, ensure daemon is running

#### 4. Database Locked

```
Error: database is locked
```

**Cause**: Multiple processes accessing database
**Solution**:
```bash
# Find processes
lsof ~/.conduit/conduit.db
# Kill stale processes
pkill -f conduit
```

#### 5. FTS5 Corruption

```
Error: fts5: corruption detected
```

**Solution**:
```bash
# Rebuild FTS index
sqlite3 ~/.conduit/conduit.db "INSERT INTO kb_chunks_fts(kb_chunks_fts) VALUES('rebuild');"
```

### Debug Mode

Run with maximum verbosity:

```bash
./bin/conduit-daemon --foreground --log-level=debug 2>&1 | tee debug.log
```

### Getting Support

1. Check logs: `~/.conduit/logs/` or console output
2. Run diagnostics: `./bin/conduit doctor`
3. Capture state: `./bin/conduit status --json > state.json`
4. Report issue with logs and state

---

## Upgrading

### Pre-Upgrade Checklist

1. Backup database: `./bin/conduit backup`
2. Stop daemon: `pkill conduit-daemon`
3. Record current version: `./bin/conduit --version`
4. Review release notes for breaking changes

### Upgrade Procedure

```bash
# Pull latest code
cd conduit
git pull origin main

# Rebuild
make clean
make build

# Start daemon (runs migrations automatically)
./bin/conduit-daemon --foreground --log-level=info
```

### Post-Upgrade Verification

```bash
# Check version
./bin/conduit --version

# Run diagnostics
./bin/conduit doctor

# Verify instances
./bin/conduit list

# Verify bindings
./bin/conduit client list

# Test KB search
./bin/conduit kb search "test"
```

### Rollback Procedure

```bash
# Stop new version
pkill conduit-daemon

# Checkout previous version
git checkout v0.0.1  # or previous tag

# Rebuild
make clean
make build

# Restore backup if needed
./restore-conduit.sh /path/to/backup

# Start old version
./bin/conduit-daemon &
```

---

## Uninstallation

### Using the CLI (Recommended)

The easiest way to uninstall Conduit:

```bash
conduit uninstall
```

This interactive wizard will:
1. Stop the running daemon
2. Remove the daemon service
3. Ask whether to remove data (`~/.conduit`)
4. Ask whether to remove binaries

### Manual Uninstallation

If the CLI isn't available or you need manual control:

```bash
# 1. Stop the daemon service
conduit service stop
conduit service remove

# Or if service commands aren't available:
# macOS
launchctl unload ~/Library/LaunchAgents/com.simpleflo.conduit.plist
rm ~/Library/LaunchAgents/com.simpleflo.conduit.plist

# Linux
systemctl --user stop conduit
systemctl --user disable conduit
rm ~/.config/systemd/user/conduit.service
systemctl --user daemon-reload

# 2. Remove binaries
sudo rm /usr/local/bin/conduit /usr/local/bin/conduit-daemon
# Or from local bin
rm ~/.local/bin/conduit ~/.local/bin/conduit-daemon

# 3. Remove data (CAUTION: removes all Conduit data)
rm -rf ~/.conduit

# 4. Remove config files
rm -f ~/Library/Logs/conduit/conduit.log  # macOS
```

### What Gets Removed

| Component | Location | CLI Removes |
|-----------|----------|-------------|
| Daemon service | `~/Library/LaunchAgents/` or `~/.config/systemd/` | Yes |
| Binaries | `/usr/local/bin/` or `~/.local/bin/` | Optional |
| Data directory | `~/.conduit/` | Optional |
| Database | `~/.conduit/conduit.db` | With data dir |
| Logs | `~/Library/Logs/conduit/` or `~/.conduit/logs/` | With data dir |

### Preserving Data Before Uninstall

If you might reinstall later:

```bash
# Backup database
cp ~/.conduit/conduit.db ~/conduit-backup.db

# Backup entire data directory
tar -czf ~/conduit-data-backup.tar.gz ~/.conduit/
```

---

## Appendix: API Reference

### Health Endpoint

```http
GET /api/v1/health
```

Response:
```json
{
  "status": "healthy",
  "version": "0.1.0",
  "uptime": "5h30m",
  "runtime": "podman",
  "instances": {
    "total": 5,
    "running": 3
  }
}
```

### Instances Endpoints

```http
GET /api/v1/instances
POST /api/v1/instances
GET /api/v1/instances/{id}
POST /api/v1/instances/{id}/start
POST /api/v1/instances/{id}/stop
DELETE /api/v1/instances/{id}
```

### KB Endpoints

```http
GET /api/v1/kb/sources
POST /api/v1/kb/sources
GET /api/v1/kb/search?q={query}
GET /api/v1/kb/stats
```

**Search Query Parameters**:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `q` | string | required | Search query text |
| `mode` | string | `hybrid` | Search mode: `hybrid`, `semantic`, `fts5` |
| `limit` | int | 10 | Maximum results |
| `min_score` | float | 0.0 | Minimum similarity threshold (0.0-1.0) |
| `semantic_weight` | float | 0.5 | Semantic vs keyword weight (0.0-1.0) |
| `mmr_lambda` | float | 0.7 | Relevance vs diversity (0.0-1.0) |
| `enable_mmr` | bool | true | Enable MMR diversity filtering |
| `enable_rerank` | bool | true | Enable semantic reranking |

**Example**:
```http
GET /api/v1/kb/search?q=authentication&mode=semantic&min_score=0.05&limit=20
```
