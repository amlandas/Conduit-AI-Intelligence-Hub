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
- Git
- Docker or Podman (for running tests with containers)
- Node.js 18+ (for desktop app development)

### Clone and Build

```bash
# Clone the repository
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git
cd Conduit-AI-Intelligence-Hub

# Build CLI and daemon
make build

# Run tests
make test

# Run linter
make lint
```

### Project Structure

```
conduit/
├── cmd/
│   ├── conduit/          # CLI tool
│   └── conduit-daemon/   # Background daemon
├── internal/
│   ├── adapters/         # Client adapters (Claude Code, Cursor, etc.)
│   ├── daemon/           # Daemon core and HTTP handlers
│   ├── kb/               # Knowledge base (indexer, searcher, MCP)
│   └── ...               # Other internal packages
├── apps/
│   └── conduit-desktop/  # Electron desktop app
├── docs/                 # Documentation
├── scripts/              # Build and utility scripts
└── tests/                # Integration tests
```

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
fix(daemon): resolve race condition in startup
docs(readme): update installation instructions
test(kb): add integration tests for search
refactor(adapters): simplify client binding logic
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

### TypeScript/React (Desktop App)

- Use TypeScript strict mode
- Follow the existing component patterns
- Use functional components with hooks
- Keep components focused and small

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
feat(kb): add semantic search with Qdrant integration

Adds vector-based search using Qdrant for improved result relevance.
Falls back to FTS5 when Qdrant is unavailable.

Closes #42
```

```
fix(daemon): prevent panic on nil pointer in status handler

The status handler could panic when Qdrant was unavailable.
Now returns graceful error message instead.

Fixes #78
```

---

## Testing

### Running Tests

```bash
# All tests
make test

# With coverage
make test-coverage

# Specific package
go test ./internal/kb/...

# Verbose output
go test -v ./internal/kb/...
```

### Writing Tests

- Place tests in `_test.go` files next to the code they test
- Use table-driven tests for multiple scenarios
- Mock external dependencies (Qdrant, FalkorDB, Ollama)
- Test both success and error paths

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

- New CLI commands or flags → Update `docs/CLI_COMMAND_INDEX.md`
- New features → Update `README.md` and relevant guides
- Bug fixes with workarounds → Update `docs/KNOWN_ISSUES.md`
- API changes → Update relevant technical docs

### Documentation Standards

- Use clear, concise language
- Include code examples where helpful
- Keep formatting consistent with existing docs
- Test any code examples you provide

---

## Questions?

- **General questions**: [GitHub Discussions](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/discussions)
- **Bug reports**: [GitHub Issues](https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues)

Thank you for contributing to Conduit!
