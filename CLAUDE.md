# Conduit - Claude Code Project Guidelines

This document provides guidelines for Claude Code when working on the Conduit
project.

**Architecture context**: read [CONTEXT.md](CONTEXT.md) first. Conduit 2.0 is a
single binary with no daemon, no containers and no external services. If you
find a document describing a daemon, Qdrant, FalkorDB, Podman or the Electron
GUI as a current feature, it is stale — check [CHANGELOG.md](CHANGELOG.md).

---

## GitHub Workflow Rules

### 1. Bug Tracking

When working on bugs:
- **Always create a bug report in GitHub Issues first** before starting work on the bug
- Track what bugs are being worked on and what fixes have been rolled out
- You may consolidate related bugs into a single report, file them separately, or organize them as you see fit
- Use clear, descriptive titles and provide reproduction steps when applicable

### 2. Feature Development

For feature updates and new feature requests:
- **Always create a Pull Request (PR)** when working on feature updates
- This maintains an audit trail of:
  - Features worked on for this project
  - Commits related to each feature
  - Discussion and review history

### 3. Commit References

After the initial repository setup:
- **Reference the bug report or PR number** in commit messages
- Use conventional commit format when appropriate
- Example: `fix(kb): honour source filter in hybrid search (#76)`
- Example: `feat: add one-click installation script (#15)`
- **Close bug reports** when a commit has resolved the issue
- **Mark PRs as ready for review** when feature work is complete

### 4. Confirmation Required

**MOST IMPORTANT**: Always get confirmation before:
- Creating bug reports or issues
- Creating pull requests
- Updating bug reports or PRs (status changes, labels, assignments)
- Deleting/closing bug reports or PRs
- Any CUD (Create, Update, Delete) operations on GitHub issues/PRs

### 5. Commit Confirmation

After making code changes:
- **Always ask for permission** before committing changes to GitHub
- Provide a summary of changes made
- Wait for explicit approval before running `git add`, `git commit`, or `git push`

---

## Library-First Architecture (Design Principle)

> **`internal/kbservice` is the single source of truth. Every frontend is a
> thin shell over it.**

This is the **ABSOLUTE** architectural principle for Conduit 2.0. No
exceptions.

It replaces v1's "GUI must call the CLI" rule. That rule existed because the
daemon held the real logic and the GUI kept reaching around the CLI to talk to
it directly. There is no daemon now, and no GUI. The logic lives in a library,
in process, and the rule is correspondingly simpler.

### Core Rules

1. **Business logic goes in `internal/kbservice`, not in a command handler.**
   - A Cobra `RunE` should parse flags, call one library method, and format the
     result. If it contains branching business logic, that logic belongs one
     layer down.
   - The MCP server (`internal/mcpserver`) and the CLI (`internal/cli`) are
     peers. Both call the same library methods. Neither may hold behaviour the
     other cannot reach.
   - If a capability exists in one frontend and not the other, that is a bug in
     where the code was placed.

2. **Output is an API contract.**
   - Human-readable output and every `--json` shape are consumed by scripts and
     by the frozen desktop GUI. Do not change them casually.
   - When a command's backend is genuinely removed, document the removal in
     `internal/cli/removed.go` rather than silently changing behaviour.

3. **Never add a process boundary.**
   - No daemon, no socket, no HTTP API, no background service. A command opens
     the SQLite file, does its work, and exits.
   - Concurrency is SQLite's job: WAL mode plus a busy timeout. Do not
     introduce a coordinating process to serialise access.

4. **`internal/kb` invariants are load-bearing.**
   - RRF is the only fusion method. Do not add a second one.
   - If you add a retrieval option, prove with a test that changing it changes
     an observable result. v1 shipped 13 option fields the engine ignored.
   - The golden harness (`internal/kb/golden_retrieval_test.go`,
     `known_bugs_test.go`) must show any ranking change as a deliberate diff.
     Do not weaken the regression tests for issues #69–#77.

### Violation Examples (Don't Do This)

```go
// WRONG: business logic in the command handler
RunE: func(cmd *cobra.Command, args []string) error {
    rows, _ := db.Query("SELECT ...")
    for rows.Next() { /* ranking, filtering, merging ... */ }
}

// CORRECT: the handler calls the library and formats
RunE: func(cmd *cobra.Command, args []string) error {
    res, err := svc.Search(ctx, query, opts)
    if err != nil { return err }
    return printResults(res)
}
```

```go
// WRONG: a frontend reaching past the library to the database
func (s *Server) toolStats(...) { db.QueryRow("SELECT COUNT(*) FROM kb_chunks") }

// CORRECT: both frontends call the same method
func (s *Server) toolStats(...) { return s.svc.Stats(ctx, sourceID) }
```

### Why This Matters

- **Consistency**: the CLI and the MCP server behave identically because they
  run identical code.
- **Testability**: the library is testable without spawning a process.
- **Maintainability**: one implementation of every behaviour.

---

## Project-Specific Guidelines

### Build Requirements

Conduit is **one binary**: `cmd/conduit`. There is nothing else to build.

- Go 1.21+
- A C compiler. **CGO is mandatory** — the knowledge base is SQLite with the
  FTS5 extension.
- Build with `make build`, or:

  ```bash
  CGO_ENABLED=1 go build -tags fts5 -o conduit ./cmd/conduit
  ```

**Both `CGO_ENABLED=1` and `-tags fts5` are required.** A build missing either
compiles cleanly, starts cleanly, and then fails every search at runtime with
`no such module: fts5`. Any build command, script or CI job you touch must set
both.

### Basic Testing

```bash
CGO_ENABLED=1 go test -tags fts5 ./...   # or: make test
```

- Run tests before committing. Ensure all pass.
- Add tests for new features.
- Retrieval changes must be reflected in the golden harness deliberately, not
  by regenerating goldens to make a failure disappear.

### Bug Investigation & Fix Validation (CRITICAL)

**Root Cause Analysis:**
- **Always perform in-depth investigation** to find the true root cause for bugs
- **Never assume, guess, or use trial-and-error methods** for bug fixes
- Only proceed with a fix when you have **high confidence** in the root cause analysis
- **Maintain transparency** with the user about findings and reasoning
- Fix at the root cause, not at the symptom. If the correct fix is deleting a
  feature that never worked, delete it and say so.

**Investigation workflow**
1. Investigate and identify root cause with high confidence
2. Implement the fix

### END-TO-END TESTING (MANDATORY)

**Local machine testing is mandatory.**
- **Always perform full-loop UX testing on this local machine** for any new
  feature or bug fix, to validate that it actually works
- Testing must simulate actual user workflows, not just verify that code
  compiles
- Document test results before marking work as complete

**Remote machine testing is deferred.**
- Testing against the remote machine (`amlandas@192.168.1.60`) is **deferred by
  owner decision, 2026-08-03**, and is not a gate on completing work
- Do not block a change on remote validation, and do not claim remote
  validation was performed

**Testing Workflow:**
1. Test on local machine (full user workflow)
2. Then commit

### POST END-TO-END TESTING TASKS (REQUIRED)

**Post-Fix Completion:**
- **Always merge changes** in GitHub by opening, updating, and closing PRs or bug reports
- **Always update the README**, the changelog, and any other affected docs
- **Always update the install scripts** (`scripts/install.sh`) and the
  `conduit uninstall` command if the change affects what lands on a machine

### Documentation

Documentation is held to the same standard as code: **every claim must be
verifiable against the source.** Do not document a flag, command or config key
you have not seen in `--help` output or in the code. Describing features that
do not exist is the specific failure that made the v1 docs untrustworthy.

When updating features, ensure these are updated:
- `README.md` — what Conduit is, quick start, security posture
- `CHANGELOG.md` — every user-visible change
- `docs/USER_GUIDE.md` — user-facing features
- `docs/ADMIN_GUIDE.md` — configuration and administration
- `docs/KNOWN_ISSUES.md` — new limitations and advisories
- `CONTEXT.md` — architecture changes

### Code Style

- Follow Go best practices
- Use meaningful variable names
- Add comments for complex logic, and for any non-obvious invariant a future
  reader could break silently
- Keep functions focused and small

### Security

- Never commit secrets or credentials
- Follow the mandatory security rules in `~/.claude/CLAUDE.md`
- Implement principle of least privilege
- **Conduit opens no listening socket for its own API.** The MCP server talks
  over stdio. The only local service is the optional embedding sidecar, which
  must stay bound to `127.0.0.1`. Do not bind anything to `0.0.0.0` — that was
  SEC-001 in v1.
- **`policy.forbidden_paths` is enforced on `conduit kb add`** after symlink
  resolution. Do not add an ingest path that bypasses `internal/kbservice`
  path safety; the failure mode is indexing private keys into a database the
  MCP server hands to AI clients.
- **Model downloads are SHA-256 verified** against the pinned registry. Never
  add an override that installs an unverified artifact.
- **`internal/querylog` must never gain a field that can hold query text**,
  entity names, paths, snippets or results. Its privacy guarantee is
  structural, and a test enforces it.
- Remember SEC-003: indexed document content flows verbatim to AI clients, so
  an indexed document is a prompt-injection vector. Anything that widens what
  reaches the client deserves explicit thought.

---

## Commit Message Format

Use conventional commits for consistency:

```
<type>(<scope>): <subject>

<body>

<footer>
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Test additions or modifications
- `refactor`: Code refactoring
- `chore`: Maintenance tasks
- `perf`: Performance improvements

**Example**:
```
feat(installer): add one-click installation script

Implement automated installation with dependency detection,
model download and MCP client configuration.

Closes #15
```

---

## Branch Strategy

- `main` - Conduit 1.x (frozen; security backports only)
- `v2` - Conduit 2.0 development line
- `feature/*` - New features
- `bugfix/*` - Bug fixes
- `docs/*` - Documentation updates

Always create feature branches for non-trivial changes.

---

## Review Checklist

Before requesting review on a PR:
- [ ] All tests pass (`CGO_ENABLED=1 go test -tags fts5 ./...`)
- [ ] Local end-to-end testing performed and documented
- [ ] Documentation updated, and every documented flag/command/key verified
      against `--help` or source
- [ ] No secrets committed
- [ ] Code follows project style
- [ ] Commit messages are clear
- [ ] PR description explains changes

---

**Last Updated**: August 2026 (Conduit 2.0.0-beta)
