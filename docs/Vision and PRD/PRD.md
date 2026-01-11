# Product Requirements Document (PRD) — Simpleflo Conduit

**Product**: Simpleflo Conduit
**Expanded Title**: Private Knowledge Base for AI Coding Tools
**Version**: 1.0.42
**Status**: Implemented (V1 Launch)
**Last Updated**: January 2026
**Owner**: AD
**Platforms**: macOS (primary), Windows, Ubuntu
**Primary Users**: Developers using AI coding tools
**Design Intent**: CLI-first experience with optional desktop GUI

---

## Executive Summary

Conduit V1 delivers a **private knowledge base for AI coding tools** that transforms user documents into searchable, AI-accessible knowledge via MCP (Model Context Protocol). The implementation prioritizes sophisticated retrieval (hybrid search, knowledge graphs) over the originally planned connector marketplace.

**Core Value Proposition**: Your documents, your AI tools, zero cloud dependency.

---

## 1. Background and Context

### 1.1 What Conduit Is

Conduit is a native application that helps developers:
- **Transform private documents** into an AI-searchable knowledge base
- **Connect that knowledge** to AI coding tools (Claude Code, Cursor, VS Code, Gemini CLI)
- **Maintain privacy** through local-only operation with container isolation

Conduit removes the friction of manually uploading documents, configuring retrieval systems, and connecting them to multiple AI clients.

### 1.2 Why This Matters

AI coding tools are powerful but lack personal context. Developers work with:
- Private documentation (internal wikis, design docs, runbooks)
- Project-specific knowledge (architecture decisions, coding standards)
- Historical context (past decisions, deprecated patterns)

Conduit bridges this gap by making private knowledge accessible to AI tools through a standard protocol (MCP).

### 1.3 Evolution from Original Vision

The original vision included a "connector marketplace" for discovering and installing third-party MCP servers. Market feedback showed stronger demand for the private KB use case, leading to a strategic pivot:

| Original Vision | V1 Implementation |
|-----------------|-------------------|
| App Store + Firewall + KB Bridge | **Private KB Bridge** + Policy Engine |
| Connector discovery via NLP | Direct document ingestion |
| Third-party MCP servers | First-party KB MCP server |
| ChatGPT remote connectivity | Local MCP clients only |

See [ORIGINAL_VISION.md](ORIGINAL_VISION.md) for the complete original product vision.

---

## 2. Vision

### 2.1 Vision Statement

Become the **standard way developers bring private knowledge to AI coding tools**: easy ingestion, sophisticated retrieval, secure local operation.

### 2.2 Core Product Thesis

**"Your Docs + Your AI Tools + Zero Cloud"**

---

## 3. Goals, Non-Goals, and Principles

### 3.1 Goals (V1 — Achieved)

1. **Fast time-to-value**: User goes from documents to working KB in minutes
2. **Sophisticated retrieval**: Hybrid search that outperforms naive RAG
3. **Multi-client support**: Configure once, use across 4+ AI clients
4. **Security by default**: Container isolation, policy engine, local-only operation
5. **CLI-first experience**: Power users get full control via command line

### 3.2 Non-Goals (V1)

- Connector marketplace / discovery (deferred to V2)
- Third-party MCP server management (deferred)
- Remote client support / Secure Link (deferred)
- Enterprise RBAC / org policy engines (explicitly later)
- Cloud sync of documents (local stays local)
- Trust signals / community scoring (deferred)

### 3.3 Principles

- **Local-first**: Documents never leave the user's machine
- **CLI-first, GUI-optional**: Full functionality via CLI; GUI is a cockpit
- **Retrieval quality over quantity**: Better search beats more features
- **Secure by default**: Isolation, least privilege, visible permissions

---

## 4. Target Users and Personas

### Persona A: Developer Using AI Coding Tools (Primary)

A developer who uses Claude Code, Cursor, or similar AI-assisted coding tools and wants to augment them with private documentation.

**Top Needs**:
- Ingest project documentation without manual uploads
- Query private docs from within AI coding workflow
- Cross-tool consistency (same KB works in all tools)
- No cloud dependencies for sensitive docs

### Persona B: Technical Lead / Architect (Secondary)

A technical leader who wants their team's architectural decisions, coding standards, and runbooks accessible to AI tools.

**Top Needs**:
- Bulk ingestion of documentation repositories
- High-quality retrieval with citations
- Minimal maintenance burden
- Works reliably across team's tool choices

---

## 5. User-Facing Outcomes and Key Use Cases

### 5.1 Primary Use Cases

1. **Connect private docs → AI coding tool**
   "Use my project's architecture docs as context when I ask Claude Code questions."

2. **Cross-client parity**
   "If my KB works in Claude Code, it should also work in Cursor and VS Code."

3. **Sophisticated search**
   "Find relevant context even when my query uses different terminology than the docs."

4. **Knowledge graph queries**
   "Show me all dependencies of the AuthService and how they connect."

### 5.2 Deferred Use Cases (V2)

- Connect third-party tools → AI client (Notion, Jira, etc.)
- Remote access via Secure Link (ChatGPT)
- Connector discovery and lifecycle management

---

## 6. End-to-End UX Flows (V1)

### UX Loop A — Documents → Private KB → AI Tools

```
1. User installs Conduit: conduit install
2. User adds document sources: conduit kb add ~/docs/project
3. Conduit ingests and indexes documents (chunking, embedding, graph extraction)
4. User configures AI client: conduit mcp configure --client claude-code
5. User queries KB from AI client: "What's the auth flow?"
6. AI receives relevant context with citations
```

### UX Loop B — Advanced Search Workflow

```
1. User asks complex question in AI tool
2. Conduit classifies query type (definition, procedural, exploratory, etc.)
3. Conduit executes hybrid search:
   - Full-text search (FTS5) for exact matches
   - Semantic search (Qdrant) for conceptual similarity
   - Graph search (FalkorDB) for entity relationships
4. Results fused via Reciprocal Rank Fusion (RRF)
5. Diversity filtering (MMR) removes redundant results
6. Semantic reranking prioritizes most relevant chunks
7. AI receives high-quality context
```

### UX Loop C — Maintenance

```
1. User adds new documents: conduit kb add ~/docs/new-feature
2. Conduit incrementally indexes new content
3. User syncs all sources: conduit kb sync
4. User views KB stats: conduit kb stats
5. User manages sources: conduit kb sources
```

---

## 7. Product Scope: What's in V1

### 7.1 Supported AI Clients

| Client | Transport | Status |
|--------|-----------|--------|
| Claude Code | stdio | Complete |
| Cursor | stdio | Complete |
| VS Code (Copilot/Cline) | stdio | Complete |
| Gemini CLI | stdio | Complete |

### 7.2 Supported Document Formats

30+ formats including: Markdown, PDF, Word, Excel, PowerPoint, HTML, JSON, YAML, source code files, and more.

### 7.3 Search Capabilities

| Capability | Technology | Status |
|------------|-----------|--------|
| Full-text search | SQLite FTS5 | Complete |
| Semantic search | Qdrant (768-dim) | Complete |
| Graph search | FalkorDB | Complete |
| Hybrid fusion | RRF algorithm | Complete |
| Diversity filtering | MMR (λ=0.7) | Complete |
| Semantic reranking | Top-30 → Top-10 | Complete |
| Query classification | 5 query types | Complete |

---

## 8. User Stories

### Epic 1: Document Ingestion

**Story 1.1: Add Document Sources**
> As a developer, I want to add local folders and files to Conduit, so that my AI tools can access my documentation.

**Acceptance Criteria**:
- User can add sources via `conduit kb add <path>`
- Conduit supports 30+ document formats
- Progress shown during ingestion
- Duplicate detection prevents re-indexing

**Story 1.2: Sync and Re-index**
> As a developer, I want to sync my KB when documents change, so that my AI tools have current information.

**Acceptance Criteria**:
- `conduit kb sync` re-indexes changed files
- Incremental indexing (only changed files)
- Clear status reporting

---

### Epic 2: Sophisticated Retrieval

**Story 2.1: Hybrid Search**
> As a developer, I want search that combines keyword matching and semantic understanding, so that I find relevant content even with imprecise queries.

**Acceptance Criteria**:
- FTS5 for exact/keyword matches
- Semantic search for conceptual similarity
- RRF fusion combines results
- Configurable weights and limits

**Story 2.2: Knowledge Graph Queries**
> As a developer, I want to query relationships between code entities, so that I understand how components connect.

**Acceptance Criteria**:
- Entity extraction from documents
- Graph storage in FalkorDB
- `kag_query` MCP tool for graph traversal
- Returns entity relationships and context

**Story 2.3: Query Classification**
> As a developer, I want Conduit to understand my query type, so that it applies the right search strategy.

**Acceptance Criteria**:
- Classify: definition, procedural, exploratory, factual, comparative
- Adjust search parameters per query type
- Improve result relevance

---

### Epic 3: Multi-Client Integration

**Story 3.1: One-Click Client Configuration**
> As a developer, I want to configure my AI client with one command, so that I don't manually edit config files.

**Acceptance Criteria**:
- `conduit mcp configure --client <name>` writes config
- Validates configuration after writing
- Supports 4 client types

**Story 3.2: MCP Server Management**
> As a developer, I want the KB MCP server to start automatically and stay running, so that my AI tools always have access.

**Acceptance Criteria**:
- Daemon manages MCP server lifecycle
- Auto-restart on failure
- Health monitoring via `conduit daemon status`

---

### Epic 4: Security and Policy

**Story 4.1: Container Isolation**
> As a security-conscious developer, I want KB processing to run in isolated containers, so that my system is protected.

**Acceptance Criteria**:
- Container runtime detection (Podman/Docker)
- Services run in isolated containers
- No host filesystem access unless granted

**Story 4.2: Policy Engine**
> As a developer, I want to control what my KB can access, so that I maintain security boundaries.

**Acceptance Criteria**:
- Forbidden path blocking
- Permission grants for filesystem access
- Policy evaluation logged

---

### Epic 5: Operations and Diagnostics

**Story 5.1: Health Diagnostics**
> As a developer, I want to diagnose issues quickly, so that I can fix problems without deep debugging.

**Acceptance Criteria**:
- `conduit doctor` checks all components
- Clear pass/fail/warn status
- Actionable recommendations

**Story 5.2: KB Statistics**
> As a developer, I want to see my KB stats, so that I understand what's indexed.

**Acceptance Criteria**:
- Source count, document count, vector count
- Service status (Qdrant, FalkorDB, Ollama)
- `conduit kb stats` command

---

## 9. Functional Requirements

### 9.1 KB Builder

- Document ingestion with format detection
- Intelligent chunking (code-aware, markdown-aware)
- Embedding generation (Ollama + nomic-embed-text)
- Entity extraction (Mistral 7B via Ollama)
- Graph construction (FalkorDB)

### 9.2 KB MCP Server

Exposes 8+ tools via MCP:
- `search_kb` — Hybrid search with configurable parameters
- `get_kb_stats` — Knowledge base statistics
- `list_sources` — Document sources
- `get_document_content` — Full document retrieval
- `search_entities` — Entity lookup
- `get_entity_context` — Entity details
- `get_entity_relationships` — Graph relationships
- `kag_query` — Natural language graph queries

### 9.3 RuntimeProvider

- Container runtime abstraction (Podman/Docker)
- Service lifecycle management
- Health monitoring
- Auto-recovery

### 9.4 Policy Engine

- Permission evaluation (filesystem, network, exposure)
- Forbidden path enforcement
- User grant management
- Decision logging

### 9.5 Client Adapters

- Claude Code adapter
- Cursor adapter
- VS Code adapter
- Gemini CLI adapter

Each adapter handles:
- Config file location detection
- MCP server configuration injection
- Configuration validation

---

## 10. Non-Functional Requirements

### Performance

| Metric | Target | Achieved |
|--------|--------|----------|
| Search latency (cached) | < 500ms | Yes |
| Document ingestion | 100+ docs/min | Yes |
| Embedding generation | Depends on Ollama | Yes |
| Startup time | < 5 seconds | Yes |

### Reliability

- Daemon survives reboots (launchd/systemd)
- Auto-recovery on service failure
- Incremental indexing (no full re-index required)

### Security

- Local-only operation (no cloud calls)
- Container isolation for services
- Policy engine for permission control
- No secret transmission to external servers

### Compatibility

- macOS 12+ (primary)
- Ubuntu 22.04+
- Windows 10+ (via WSL2)

---

## 11. Metrics and Success Criteria

### North Star Metric

**Time-to-First-Query (TTFQ)**: Time from `conduit install` to first successful KB query in AI client.

**Target**: < 10 minutes for a developer with existing documents.

### Supporting Metrics

| Metric | Description |
|--------|-------------|
| Install success rate | % completing installation without errors |
| Client configuration success | % successfully configuring AI client |
| Search quality | Relevance of returned results |
| KB utilization | Queries per day per user |
| Document coverage | % of user docs successfully indexed |

---

## 12. Release History

### V0 — CLI-first "Conduit Engine" (December 2025)

- Core daemon and CLI
- RuntimeProvider (Podman/Docker)
- KB ingestion and FTS5 search
- 4 client adapters
- Policy engine

### V0.5 — Semantic Search (December 2025)

- Qdrant integration
- Embedding generation (Ollama)
- Hybrid search (RRF)
- MMR diversity filtering

### V1.0 — Knowledge Graphs (January 2026)

- FalkorDB integration
- Entity extraction (Mistral 7B)
- KAG queries
- Desktop GUI (85%)
- Semantic reranking
- Query classification

---

## 13. Future Roadmap

### V1.x — Polish and Stability

- [ ] Complete consent ledger integration
- [ ] Secrets Manager (OS keychain)
- [ ] KB export/import
- [ ] Improved error handling

### V2.0 — Connector Ecosystem

- [ ] Connector marketplace
- [ ] Third-party MCP server management
- [ ] Trust signals and community scoring
- [ ] Secure Link for remote clients
- [ ] Auditor (security scanning)

### V3.0 — Enterprise

- [ ] Team/org KB sharing
- [ ] RBAC and admin controls
- [ ] Cloud backup (optional)
- [ ] Audit trails

---

## 14. Risks and Mitigations

| Risk | Mitigation |
|------|------------|
| Large document sets slow indexing | Incremental indexing, parallel processing |
| Ollama resource usage | Optional cloud embedding providers (future) |
| Container setup friction | Zero-touch runtime detection, guided setup |
| Search quality variance | Multiple search strategies, reranking |
| Client config format changes | Adapter abstraction, version detection |

---

## 15. Glossary

| Term | Definition |
|------|------------|
| **MCP** | Model Context Protocol — Standard for AI tool integration |
| **KB** | Knowledge Base — Indexed document collection |
| **FTS5** | SQLite Full-Text Search extension |
| **RRF** | Reciprocal Rank Fusion — Score combination algorithm |
| **MMR** | Maximal Marginal Relevance — Diversity algorithm |
| **KAG** | Knowledge-Augmented Generation — Graph-enhanced retrieval |
| **RuntimeProvider** | Container runtime abstraction layer |

---

## Appendix A: Architecture Reference

See [HLD Documentation](../HLD/) for detailed architecture:
- [DESIGN.md](../HLD/DESIGN.md) — Architecture overview
- [HLD-V0-Core-Engine.md](../HLD/HLD-V0-Core-Engine.md) — Core components
- [HLD-V1-Desktop-GUI.md](../HLD/HLD-V1-Desktop-GUI.md) — Desktop application
- [IMPLEMENTATION_STATUS.md](../HLD/IMPLEMENTATION_STATUS.md) — Feature matrix

## Appendix B: CLI Reference

See [CLI Command Index](../CLI_COMMAND_INDEX.md) for complete command documentation.

---

**Document History**

| Version | Date | Changes |
|---------|------|---------|
| 1.0.42 | Jan 2026 | V1 PRD reflecting actual implementation |

---

*This PRD reflects the implemented V1 product. For the original product vision, see [ORIGINAL_VISION.md](ORIGINAL_VISION.md).*
