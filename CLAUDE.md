# Conduit - Claude Code Project Guidelines

This document provides guidelines for Claude Code when working on the Conduit project.

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
- Example: `fix: resolve daemon startup race condition (#42)`
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

## Project-Specific Guidelines

### Build Requirements

- Go 1.21+
- CGO enabled (required for SQLite FTS5)
- Build with: `make build` or `go build -tags "fts5"`

### Testing

- Run tests before committing: `make test`
- Ensure all tests pass
- Add tests for new features

### Bug Investigation & Fix Validation (CRITICAL)

**Root Cause Analysis:**
- **Always perform in-depth investigation** to find the true root cause for bugs
- **Never assume, guess, or use trial-and-error methods** for bug fixes
- Only proceed with a fix when you have **high confidence** in the root cause analysis
- **Maintain transparency** with the user about findings and reasoning

**End-to-End User Experience Testing:**
- **Always perform full-loop UX testing on this local machine** for any new features or bug fixes to validate they work
- **Always perform full-loop UX testing on the remote machine** (`amlandas@192.168.1.60`) to validate features/fixes work on other machines
- Testing must simulate actual user workflows, not just verify code compiles
- Document test results before marking work as complete

**Testing Workflow:**
1. Investigate and identify root cause with high confidence
2. Implement the fix
3. Test on local machine (full user workflow)
4. Test on remote machine (full user workflow)
5. Only then commit and push changes

### Documentation

When updating features, ensure documentation is updated:
- README.md - Installation and quick start
- docs/USER_GUIDE.md - User-facing features
- docs/ADMIN_GUIDE.md - Administration and configuration
- docs/V0_OUTCOME.md - Implementation tracking

### Code Style

- Follow Go best practices
- Use meaningful variable names
- Add comments for complex logic
- Keep functions focused and small

### Security

- Never commit secrets or credentials
- Follow the mandatory security rules in `~/.claude/CLAUDE.md`
- All connectors run in isolated containers
- Implement principle of least privilege

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
service setup, and AI model download.

Closes #15
```

---

## Branch Strategy

- `main` - Production-ready code
- `feature/*` - New features
- `bugfix/*` - Bug fixes
- `docs/*` - Documentation updates

Always create feature branches for non-trivial changes.

---

## Review Checklist

Before requesting review on a PR:
- [ ] All tests pass
- [ ] Documentation updated
- [ ] No secrets committed
- [ ] Code follows project style
- [ ] Commit messages are clear
- [ ] PR description explains changes

---

**Last Updated**: January 2026
