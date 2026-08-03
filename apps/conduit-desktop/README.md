# Conduit Desktop — FROZEN / UNSUPPORTED

> **⛔ This application is frozen. Do not install it, build it, or ship it.**
>
> Development was halted in **July 2026**. This directory is retained for
> historical reference only. It will not receive updates — no feature work, no
> dependency bumps, and **no security patches**.

## Status

| | |
|---|---|
| **State** | Frozen — development halted July 2026 |
| **Last release** | v0.1.43 |
| **Future updates** | None planned, ever |
| **Supported interface** | Conduit CLI + MCP server |

## Why it is frozen

- **End-of-life Electron.** The app is pinned to Electron 28, which left
  security support roughly 2.5 years ago. Its bundled Chromium carries a long
  tail of unpatched CVEs, and no upgrade path is planned.
- **Renderer-to-shell IPC hole.** The `terminal:spawn` IPC handler lets the
  renderer process execute arbitrary shell commands. Any renderer compromise
  becomes code execution as the local user.
- **Unsigned distributables.** The published DMGs are unsigned and unnotarized.

These are documented as security advisories **SEC-001**, **SEC-002**, and
**SEC-003**, published on the repository in July 2026:

- Repository security advisories:
  <https://github.com/amlandas/Conduit-AI-Intelligence-Hub/security/advisories>
- Local copies: [`docs/KNOWN_ISSUES.md`](../../docs/KNOWN_ISSUES.md)
  (see [SEC-002](../../docs/KNOWN_ISSUES.md#sec-002-desktop-app-electron-gui-is-unsupported--do-not-use-security))

## Existing DMG releases are unsupported

Every desktop release (**v0.1.0 through v0.1.43**) is unsupported and will not
be patched. If you have the app installed, remove it:

```bash
rm -rf /Applications/Conduit.app
```

## What to use instead

Conduit **v2 is CLI and MCP only — there is no GUI**, and none is planned. The
CLI has always been the source of truth for every operation the GUI exposed, so
no functionality is lost:

```bash
conduit --help
```

## Known defects (not being fixed)

- **`uninstall-ipc.ts` misreports shell PATH entries as Conduit's.** It matches
  any `export PATH` line containing `.local/bin`, which pipx, uv, poetry and
  pip `--user` all write. The CLI and `scripts/uninstall.sh` were corrected in
  v2 to key off Conduit's own `# Conduit` marker comment; this frozen copy was
  not, and is out of scope. It affects what the GUI *reports*, and anyone
  running an uninstall should use `conduit uninstall` or
  `scripts/uninstall.sh`, both of which are precise.

## Note on dependencies

Dependabot npm updates are deliberately **not** configured for this directory.
See [`.github/dependabot.yml`](../../.github/dependabot.yml) for the rationale.
The dependency tree here is known to be vulnerable and is intentionally left
frozen rather than patched.

*Freeze declared July 2026. This notice added August 2026.*

---

# Historical reference

Everything below describes the app **as it was when development stopped**. It is
preserved for archaeology only. None of it is supported, and the build
instructions are recorded rather than recommended.

## What it was

`conduit-desktop` was a macOS Electron GUI wrapping the Conduit CLI — a setup
wizard, knowledge-base browser, connector management, RAG/KAG tuning panels, and
a daemon control/log surface.

Per the project's architectural rule, the GUI was a thin client: every operation
shelled out to `conduit` CLI commands rather than calling the daemon's HTTP API
directly. See the root [`CLAUDE.md`](../../CLAUDE.md) for that rule.

## Stack

| Layer | Technology |
|---|---|
| Shell | Electron 28 |
| Build | electron-vite 2, electron-builder 24 |
| UI | React 18, TypeScript 5, Tailwind CSS 3 |
| State | Zustand 5 |
| Terminal | node-pty, xterm.js |
| Editor | Monaco |
| Updates | electron-updater |

## Layout

```
src/
  main/        Electron main process — IPC handlers, daemon client, updater
  preload/     Context bridge exposed to the renderer
  renderer/    React UI — setup wizard, KB, connectors, settings
scripts/       CLI bundling and macOS notarization helpers
build/         macOS entitlements
docs/          Implementation audits and GUI-to-CLI compliance notes
```

## Build commands (historical — do not run)

```
npm run dev            # electron-vite dev server
npm run build          # compile main/preload/renderer
npm run typecheck      # tsc --noEmit
npm run lint           # eslint src
npm run package:mac    # bundle CLI, build, produce a macOS artifact
```

## Historical documents

- [`docs/AUDIT_v0.1.20_PRE_IMPLEMENTATION.md`](docs/AUDIT_v0.1.20_PRE_IMPLEMENTATION.md)
- [`docs/AUDIT_v0.1.20_POST_IMPLEMENTATION.md`](docs/AUDIT_v0.1.20_POST_IMPLEMENTATION.md)
- [`docs/kb-cli-compliance/IMPLEMENTATION_PLAN.md`](docs/kb-cli-compliance/IMPLEMENTATION_PLAN.md)
