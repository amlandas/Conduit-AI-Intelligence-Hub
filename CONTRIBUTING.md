# Contributing to Conduit

Thank you for your interest in contributing to Conduit! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [How to Contribute](#how-to-contribute)
- [Development Setup](#development-setup)
- [Pull Request Process](#pull-request-process)
- [Coding Standards](#coding-standards)
- [Testing](#testing)
- [Documentation](#documentation)

---

## Code of Conduct

This project follows a standard code of conduct. Please be respectful and constructive in all interactions.

- Be welcoming and inclusive
- Be respectful of differing viewpoints
- Accept constructive criticism gracefully
- Focus on what is best for the community

---

## Getting Started

### Ways to Contribute

1. **Report Bugs**: Found a bug? [Open an issue](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/new?template=bug_report.md)
2. **Suggest Features**: Have an idea? [Open a feature request](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/new?template=feature_request.md)
3. **Ask Questions**: Need help? [Start a discussion](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/discussions)
4. **Submit Code**: Fix a bug or implement a feature via pull request
5. **Improve Documentation**: Help make our docs clearer and more comprehensive

### Before You Start

- Check [existing issues](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues) to see if your issue/feature is already reported
- Check [open PRs](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/pulls) to see if someone is already working on it
- For major changes, open an issue first to discuss the approach

---

## Development Setup

### Prerequisites

- Go 1.21+
- A C compiler — **CGO is mandatory** (SQLite with the FTS5 extension)
- Git

No container runtime, no external databases, no Node.js. Conduit 2.0 is one
binary with no daemon and no services.

### Clone and Build

```bash
# Clone the repository
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git
cd Conduit-AI-Intelligence-Hub

# Build the binary
make build
# equivalently:
CGO_ENABLED=1 go build -tags fts5 -o conduit ./cmd/conduit

# Run tests
make test
# equivalently:
CGO_ENABLED=1 go test -tags fts5 ./...

# Format, vet, lint
make fmt
make vet
make lint

# List all targets
make help
```

**`CGO_ENABLED=1` and `-tags fts5` are not optional.** A build missing either
compiles cleanly, starts cleanly, and then fails every search at runtime with
`no such module: fts5`. Any build command, script or CI job you touch must set
both.

### Project Structure

```
conduit/
├── cmd/
│   └── conduit/          # The one binary
├── internal/
│   ├── cli/              # Command surface (Cobra); thin over kbservice
│   ├── kbservice/        # The in-process knowledge base library
│   ├── kb/               # Retrieval engine: chunking, FTS5, vectors, RRF, graph
│   ├── embed/            # Embedding providers: llama-server, ollama, none
│   ├── mcpserver/        # MCP server on the official Go SDK
│   ├── setup/            # Machine preparation, MCP client config, uninstall
│   ├── config/           # The configuration schema (single source of truth)
│   ├── store/            # SQLite open and migrations
│   ├── querylog/         # Local-only query-shape log
│   └── observability/    # Logging helpers
├── pkg/models/           # Shared types
├── apps/
│   └── conduit-desktop/  # FROZEN — Electron app, unsupported. Do not build.
├── docs/                 # Documentation
├── scripts/              # install.sh, uninstall.sh, remove-v1.sh
└── tests/                # Integration and script tests
```

See [CONTEXT.md](CONTEXT.md) for what each package is responsible for and the
invariants it carries.

---

## How to Contribute

### Reporting Bugs

Use the [bug report template](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/new?template=bug_report.md) and include:

1. **Description**: Clear description of the bug
2. **Steps to Reproduce**: Minimal steps to reproduce the issue
3. **Expected Behavior**: What you expected to happen
4. **Actual Behavior**: What actually happened
5. **Environment**: OS, Conduit version (`conduit --version`), container runtime
6. **Logs**: Output from `conduit doctor` and relevant log snippets

### Suggesting Features

Use the [feature request template](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues/new?template=feature_request.md) and include:

1. **Problem Statement**: What problem does this solve?
2. **Proposed Solution**: How do you envision this working?
3. **Alternatives Considered**: What other approaches did you consider?
4. **Use Case**: Who would benefit from this feature?

### Submitting Code

1. **Fork** the repository
2. **Create a branch** from `main`:
   ```bash
   git checkout -b feature/my-feature
   # or
   git checkout -b fix/issue-123
   ```
3. **Make your changes** following the [coding standards](#coding-standards)
4. **Write tests** for new functionality
5. **Run tests** locally: `make test`
6. **Commit** with a descriptive message (see [commit conventions](#commit-messages))
7. **Push** to your fork
8. **Open a Pull Request** against `main`

---

## Pull Request Process

### Before Submitting

- [ ] Code follows the project's style guidelines
- [ ] Tests pass locally (`make test`)
- [ ] New code has appropriate test coverage
- [ ] Documentation is updated if needed
- [ ] Commit messages follow conventions

### PR Title Format

Use conventional commits format:

```
type(scope): description

Examples:
feat(kb): add --rebuild-vectors flag to sync command
fix(kb): honour source filter in hybrid search
docs(readme): update installation instructions
test(kb): add integration tests for search
refactor(cli): move search formatting out of the command handler
```

### Types

| Type | Description |
|------|-------------|
| `feat` | New feature |
| `fix` | Bug fix |
| `docs` | Documentation changes |
| `test` | Test additions or modifications |
| `refactor` | Code refactoring (no functional change) |
| `perf` | Performance improvements |
| `chore` | Maintenance tasks |

### Review Process

1. Maintainers will review your PR
2. Address any feedback or requested changes
3. Once approved, a maintainer will merge your PR

---

## Coding Standards

### Go Code

- Follow [Effective Go](https://go.dev/doc/effective_go) guidelines
- Use `gofmt` for formatting (run `make fmt`)
- Use meaningful variable and function names
- Add comments for exported functions and complex logic
- Handle errors explicitly - don't ignore them

```go
// Good
result, err := doSomething()
if err != nil {
    return fmt.Errorf("doSomething failed: %w", err)
}

// Bad
result, _ := doSomething()
```

### Architecture rules

Two rules matter more than style. Both are covered in [CONTEXT.md](CONTEXT.md):

- **Library-first.** Business logic belongs in `internal/kbservice`, not in a
  Cobra `RunE`. The CLI and the MCP server are peers over the same library; a
  capability available in one and not the other means the code is in the wrong
  place.
- **Honest degradation.** Never report success for work that did not happen.
  `embed.provider: none` is a supported mode, not an error path. `kb sync`
  exits 2 on partial success for a reason.

> The Electron desktop app under `apps/conduit-desktop/` is **frozen and
> unsupported** — no feature work, no dependency bumps, no security patches.
> Do not build or ship it. See SEC-002 in
> [docs/KNOWN_ISSUES.md](docs/KNOWN_ISSUES.md).

### Commit Messages

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
type(scope): short description

Longer description if needed. Explain the "why" behind the change,
not just the "what".

Closes #123
```

**Good examples:**
```
feat(kb): store vectors in SQLite behind a VectorIndex interface

Vectors move into the knowledge base file as float32 BLOBs with a
precomputed L2 norm, searched by an exact cosine scan. Removes the
external vector server and the network listener that came with it.

Closes #42
```

```
fix(kb): quote FTS5 operator characters instead of deleting them

Query construction stripped quotes, parentheses and hyphens, which made
filenames and version strings unsearchable. They are now quoted.

Fixes #70
```

---

## Testing

### Running Tests

```bash
# All tests
make test

# With coverage
make test-cover

# Specific package (remember the build tags)
CGO_ENABLED=1 go test -tags fts5 ./internal/kb/...

# Verbose output
CGO_ENABLED=1 go test -tags fts5 -v ./internal/kb/...
```

`make test` sets `CGO_ENABLED=1` and `-tags fts5` for you. If you invoke
`go test` directly, set them yourself or the FTS5-backed tests will fail.

### Writing Tests

- Place tests in `_test.go` files next to the code they test
- Use table-driven tests for multiple scenarios
- Mock external dependencies (the embedding provider, Ollama). `internal/embed`
  ships a fake provider for this.
- Test both success and error paths

**Retrieval changes have an extra bar.** `internal/kb` carries a golden
retrieval harness (`golden_retrieval_test.go`, `retrieval_test_suite.go`) and
regression tests for issues #69–#77 (`known_bugs_test.go`). A change in ranking
must appear there as a deliberate, explained diff. Regenerating goldens to make
a failure disappear is not an acceptable fix.

```go
func TestSearch(t *testing.T) {
    tests := []struct {
        name     string
        query    string
        wantErr  bool
        wantHits int
    }{
        {"basic search", "authentication", false, 5},
        {"empty query", "", true, 0},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // ... test implementation
        })
    }
}
```

---

## Documentation

### When to Update Docs

- New CLI commands or flags → `docs/CLI_COMMAND_INDEX.md` and `docs/USER_GUIDE.md`
- New features → `README.md` and the relevant guide
- New config keys → `docs/ADMIN_GUIDE.md`
- Bug fixes with workarounds → `docs/KNOWN_ISSUES.md`
- Any user-visible change → `CHANGELOG.md`
- Architecture changes → `CONTEXT.md`

### Documentation Standards

Documentation is held to the same standard as code: **every claim must be
verifiable against the source.**

- **Never document a flag, command or config key you have not seen** in
  `--help` output or in the code. Describing features that do not exist is the
  specific failure that made the v1 documentation untrustworthy, and undoing it
  cost more than writing it did.
- Do not promise timings you have not measured. "5 minutes" was in the v1 quick
  start and was not true.
- State limitations plainly. A documented limitation is cheaper than a
  surprised user.
- Use clear, concise language
- Include code examples where helpful, and run them
- Keep formatting consistent with existing docs

---

## Questions?

- **General questions**: [GitHub Discussions](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/discussions)
- **Bug reports**: [GitHub Issues](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues)

Thank you for contributing to Conduit!
