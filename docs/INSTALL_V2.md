# Installing Conduit 2.0

Conduit 2.0 is one binary. There is no daemon, no background service, no
containers, and nothing that starts at login. Every command opens the knowledge
base file, does its work, and exits.

If you are coming from Conduit 1.x, read [Moving from
1.x](#moving-from-conduit-1x) first: installing 2.0 does not remove the 1.x
stack, and the two will coexist noisily until you tear the old one down.

---

## Requirements

| | |
|---|---|
| **Platforms** | macOS arm64 (Apple Silicon), Linux x86_64 |
| **Building from source** | Go 1.21+ and a C compiler |
| **Disk** | ~13 MB for the binary; ~262 MB more if you download the default embedding model |
| **Network** | Only to install, and once more to fetch the embedding model |

Two things are worth being explicit about.

**cgo is not optional.** The knowledge base is SQLite with the FTS5 extension.
A `CGO_ENABLED=0` build compiles fine, starts fine, and then fails every search
with `no such module: fts5`. The build in `install.sh` sets `CGO_ENABLED=1` and
`-tags fts5`; if you build by hand, so must you.

**Semantic search needs two separate things.** A `llama-server` binary to run
the model, and the model file itself. Neither is bundled. Without them Conduit
still works — search falls back to FTS5 keyword matching, which is a supported
mode and not a broken one.

### What is gone since 1.x

No Docker or Podman. No Qdrant. No FalkorDB. No `conduit-daemon`. No launchd
agent or systemd unit. No Ollama requirement.

Vectors and the knowledge graph now live in the same SQLite file as everything
else. That is why the installer got so much shorter: most of v1's install
script existed to set up infrastructure that no longer exists.

---

## Fresh install

```bash
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub
cd Conduit-AI-Intelligence-Hub
./scripts/install.sh --from-source
```

That builds the binary, installs it to `~/.local/bin`, creates the data
directory, registers the MCP server with Claude Code, and prints diagnostics.

Re-running it is safe. It is the supported way to upgrade a source install.

### Options

| Flag | Effect |
|---|---|
| `--from-source` | Build from this checkout. Needs Go and a C compiler. |
| `--version TAG` | Install a specific release (default: `latest`). |
| `--prefix DIR` | Install somewhere other than `~/.local/bin`. |
| `--model` | Also download the embedding model (a few hundred MB). |
| `--client NAME` | Configure `claude-code` (default), `cursor` or `vscode`. |
| `--no-setup` | Install the binary only; configure nothing. |

### Installing from a release

```bash
./scripts/install.sh
```

Without `--from-source` the installer fetches a published release artifact and
verifies it against the release's `SHA256SUMS` before unpacking. Verification
is mandatory; there is no flag to skip it.

**Conduit 2.0 has not published release binaries yet.** Until it does, this
mode stops with a message telling you to use `--from-source`. It does not
half-install anything.

### If `~/.local/bin` is not on your PATH

The installer tells you, and prints the line to add for your shell. Nothing is
appended to your shell profile without you doing it.

---

## The embedding model

Conduit ships no model weights. Semantic search needs a GGUF file, and it is a
few hundred megabytes, so it is never downloaded without being asked for.

```bash
conduit model list          # what is available, and what is here
conduit model download      # fetch the configured model
conduit model verify        # re-check a local file against its pin
conduit model path          # where it belongs on this machine
```

Models are **pinned**. Each one is tied to an exact file in an exact
HuggingFace repository with an exact SHA-256, recorded in
`internal/embed/registry.go`. That registry is the only place download URLs
come from.

Every download is verified against its pin before it is installed:

- the file is written to a temporary name in the destination directory and
  renamed into place only after the hash matches, so an interrupted download
  can never leave a truncated file that later looks valid;
- a mismatch deletes the download and fails, and there is no flag to install an
  unverified model;
- `Content-Length` is compared to the pinned size before any bytes are spent,
  so a changed upstream artifact fails immediately rather than after 600 MB;
- a correct file already on disk is a no-op, which is why the command is safe
  to run from a script.

### Available models

| ID | Dimensions | Context | Size |
|---|---|---|---|
| `nomic-embed-text-v1.5` *(default)* | 768 | 2048 | 261.6 MB |
| `qwen3-embedding-0.6b` | 1024 | 32768 | 609.5 MB |
| `mxbai-embed-large-v1` | 1024 | 512 | 638.6 MB |

`conduit model list` prints this table with local status alongside it.

The default is the smallest and is the only one with no open llama.cpp quality
defect against it. See `docs/EMBEDDING_SIDECAR.md` before changing it — two of
these need specific pooling modes and input prefixes to work correctly, and
getting them wrong degrades retrieval silently.

### Running the model

You also need `llama-server`:

```bash
brew install llama.cpp          # macOS
```

`conduit doctor` reports the model file and the `llama-server` binary
separately, because they fail for different reasons and have different fixes.

### Running without embeddings

Entirely supported:

```bash
conduit config set embed.provider none
```

Search then uses FTS5 keyword matching only. No model is downloaded, no port is
opened, and no process is spawned.

---

## Moving from Conduit 1.x

**Installing 2.0 does not remove 1.x.** The old daemon keeps starting at login,
and the Qdrant and FalkorDB containers keep holding ports 6333, 6334 and 6379.

Tear the old stack down first.

### 1. See what is there

```bash
./scripts/remove-v1.sh
```

Dry run is the default. Nothing is removed until you pass `--yes`. It reports:

- `conduit-qdrant` and `conduit-falkordb` containers, under both Docker and
  Podman
- launchd agents `dev.simpleflo.conduit` and `com.simpleflo.conduit`
- systemd user units `conduit.service` and `conduit-daemon.service`
- the `conduit-daemon` binary and its symlinks

Nothing under `~/.conduit` is removed without `--purge-data` — not the
knowledge base, not the models, not even the dead daemon's log.

If a container runtime is installed but not running, the script says so rather
than guessing, and prints the command to remove those containers by hand.

A data directory that is a symlink is refused rather than followed: `rm -rf
dir/` would empty what it points at, which is not what "remove the link" should
mean.

### 2. Remove it

```bash
./scripts/remove-v1.sh --yes
```

### 3. Install 2.0

```bash
./scripts/install.sh --from-source
```

### What survives the transition

Your knowledge base does. `remove-v1.sh` never touches `~/.conduit/conduit.db`,
`~/.conduit/conduit.yaml`, `~/.conduit/models` or `~/.conduit/backups`.

The old Qdrant and FalkorDB storage directories are dead weight in 2.0, since
vectors and the graph moved into SQLite. They are reported but kept unless you
ask:

```bash
./scripts/remove-v1.sh --yes --purge-data
```

That removes `~/.conduit/qdrant`, `~/.conduit/falkordb` and
`~/.conduit/daemon.log`, and nothing else. Your knowledge base, your
configuration and your downloaded models are still not touched.

### Rebuilding vectors

Documents indexed under 1.x kept their vectors in Qdrant, which is gone. To
rebuild them in SQLite:

```bash
conduit model download   # if you want semantic search
conduit kb migrate       # rebuild vectors for already-indexed documents
```

### Stale config keys

A 1.x `conduit.yaml` has keys for subsystems that no longer exist. Conduit
reports them once on stderr and ignores them:

```
warning: ~/.conduit/conduit.yaml contains 8 unrecognised key(s):
  ai, kb.kag.graph.falkordb, runtime, socket, ...
```

Harmless. Delete those sections when convenient.

---

## Verifying an install

```bash
conduit doctor
```

Every check names what is wrong and what fixes it. On a fresh install with no
documents indexed and no model downloaded, expect failures — that is the
diagnostic working, not the install being broken.

```
  ✓ knowledge base file          ~/.conduit/conduit.db (276.0 KB)
  ✓ knowledge base writable      yes
  ✓ FTS5 lexical search          available
  ✗ embedding provider           llama-server: not reachable: binary not found
      → install llama-server (brew install llama.cpp), run 'conduit model download',
        or set embed.provider to "none" for lexical-only search
  ✗ embedding model              nomic-embed-text-v1.5 not downloaded (261.6 MB)
      → run 'conduit model download nomic-embed-text-v1.5'
  ⚠ indexed content              no documents indexed
      → run 'conduit kb add <path>' then 'conduit kb sync'
  ✓ MCP client configured        [claude-code]
```

`doctor` exits 1 when a check fails, so it works in a script.

Then the real test:

```bash
conduit kb add ~/Documents/notes --name Notes
conduit kb sync
conduit kb search "something you know is in there"
```

---

## Uninstalling

```bash
./scripts/uninstall.sh
```

Removes the binary, Conduit's MCP entries from your AI clients, and the PATH
lines it added. **Your data is kept.**

```bash
./scripts/uninstall.sh --dry-run        # preview
./scripts/uninstall.sh --remove-data    # also delete the knowledge base
```

`--remove-data` prompts for confirmation unless you pass `--force`. The prompt
names the exact directory and its size, because the data directory can come from
a `conduit.yaml` in your working directory and is not always the one you have in
mind. Declining exits with status **3**, distinct from success and from failure,
so a script wrapping this can tell a refusal from a completed uninstall.

A directory holding no `conduit.db` or `conduit.yaml` is refused outright: that
is far more likely to be a mistyped `--data-dir` than a real request.

The script delegates to `conduit uninstall`, which knows what it installed. If
that binary is too old to understand the flags being used, or fails for any
other reason, the script stops and says so rather than quietly falling back —
the manual path is less careful, and downgrading to it after a failure is how a
wrapper does damage the tool it wrapped had declined to do.

The standalone path runs only when there is no binary at all. It will not edit
your MCP JSON config files from shell, because those hold your other MCP servers
and unrelated settings; it tells you which files to edit instead. It touches a
shell profile only where it finds Conduit's own `# Conduit` marker, and copies
the file to `.conduit-uninstall.bak` first.

`--prefix DIR` removes one install and only that one. Shell profiles, MCP
entries and GUI state are per-user rather than per-install, so they are left
alone under `--prefix`.

Tools you might share with other projects — Ollama, poppler, llama.cpp, Docker,
Podman — are never removed.

`uninstall.sh` knows nothing about the 1.x daemon or its containers. Use
`remove-v1.sh` for those.

---

## Windows

Not supported yet. `scripts/install-windows.ps1` says so and stops.

This is a scope decision rather than an oversight. The embedding sidecar
supervises `llama-server` with POSIX process groups and `flock`; the Windows
half of that code is a stub that has never been exercised, and there is no
signed Windows artifact.

**WSL2 works today**, with no changes:

```powershell
wsl --install
```

then, inside the WSL2 shell, follow the Linux instructions. An AI client
running on Windows can reach an MCP server running in WSL2, so this is a
working setup rather than a consolation prize.

---

## Troubleshooting

**`no such module: fts5`** — the binary was built without cgo. Rebuild with
`CGO_ENABLED=1 go build -tags fts5 ./cmd/conduit`, or use `install.sh
--from-source`, which sets both.

**`conduit: command not found` after installing** — `~/.local/bin` is not on
your PATH. The installer prints the line to add.

**An old `conduit` runs instead of the new one** — a stale symlink at
`/usr/local/bin/conduit` from a 1.x install is shadowing it. The installer
warns when it sees this; remove it with `sudo rm /usr/local/bin/conduit`.

**`model checksum mismatch`** — the download was corrupted in transit, or the
upstream artifact changed. The partial file was already deleted. Retry with
`conduit model download --force`. If it fails again with the same hash, the
pinned artifact upstream has genuinely changed and that is a bug worth
reporting; do not work around it.

`--force` never deletes the model you already have. It re-fetches into a
temporary file and replaces the old one only once the new bytes verify, so an
interrupted or offline `--force` leaves your working model exactly where it was.

**Model download fails with no network** — the error says so and points at
`embed.provider = none` for keyword-only search. A partial download is deleted,
so retrying starts clean.

**Ports 6333, 6334 or 6379 still in use** — those are the 1.x containers. They
are not used by 2.0. Run `./scripts/remove-v1.sh`.

---

## See also

- `docs/EMBEDDING_SIDECAR.md` — how the model is run, and the per-model
  pooling and prefix requirements
- `docs/QUICK_START.md` — first steps after installing
- `docs/USER_GUIDE.md` — day-to-day use
- `conduit doctor` — the fastest answer to "why is this not working"
