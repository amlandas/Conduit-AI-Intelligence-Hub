# Changelog

All notable changes to Conduit will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [1.0.42] - 2026-01-07

### V1.0 Release - Private Knowledge Base for AI Tools

This is the first stable release of Conduit, a private knowledge base that makes AI tools smarter.

### Highlights

- **RAG (Retrieval-Augmented Generation)**: Hybrid search combining semantic and keyword matching
- **KAG (Knowledge-Augmented Generation)**: Knowledge graph for multi-hop reasoning
- **MCP Integration**: Works with Claude Code, Cursor, and other AI tools via Model Context Protocol
- **100% Local**: All documents and processing stay on your machine

### Added

- `--rebuild-vectors` flag for `conduit kb sync` to force vector regeneration
- Exit code 2 for partial success when semantic indexing fails
- Clear warnings with actionable guidance for sync issues
- `docs/KNOWN_ISSUES.md` documenting common issues and workarounds
- GitHub Discussions for community support
- GitHub issue templates for bug reports and feature requests
- Comprehensive `CONTRIBUTING.md` guide
- `docs/QUICK_START.md` for new users

### Changed

- README.md completely revamped with new positioning as "Private Knowledge Base for AI Tools"
- CLI installation promoted as the primary method
- Desktop App moved to "Experimental" status
- Documentation reorganized by user type (Quick Start, Power User, Developer)

### Fixed

- Silent fallback to FTS-only when Qdrant fails (#41)
- Single-source sync now correctly passes `--rebuild-vectors` flag

---

## Pre-1.0 History

Conduit v1.0 is the culmination of the v0.x development cycle. Key milestones:

- **v0.1.41**: KB CLI compliance fixes, RAG tuning panel
- **v0.1.40**: Dashboard infrastructure status fixes
- **v0.1.39**: FalkorDB and KAG integration
- **v0.1.30**: Hybrid RAG search with MMR diversity
- **v0.1.20**: Qdrant vector database integration
- **v0.1.10**: Dependency management system
- **v0.1.0**: Initial release with MCP server and FTS5 search

For detailed history, see the [GitHub Releases](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/releases) page.

---

## Release Types

- **Major (x.0.0)**: Breaking changes or major new capabilities
- **Minor (1.x.0)**: New features, backwards compatible
- **Patch (1.0.x)**: Bug fixes and minor improvements
