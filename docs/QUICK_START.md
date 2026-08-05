# Conduit Quick Start Guide

Getting a private knowledge base your AI tools can search.

**How long this takes**: the build is a minute or two. The embedding model is a
262 MB download, so that step depends entirely on your connection. Indexing
time depends on how much you index. Plan on a coffee, not five minutes.

For the full installation guide — release binaries, options, upgrading, moving
from Conduit 1.x — see [INSTALL_V2.md](INSTALL_V2.md).

---

## Step 1: Install

```bash
git clone https://github.com/amlandas/Conduit-AI-Intelligence-Hub
cd Conduit-AI-Intelligence-Hub
./scripts/install.sh
```

That downloads the newest published v2 release and verifies its SHA-256 against
the release's `SHA256SUMS` before unpacking. No Go toolchain needed.

To build from source instead — required on Intel Macs and Linux arm64, which
have no published binaries:

```bash
./scripts/install.sh --from-source     # needs Go 1.21+ and a C compiler
```

**What this does:**

- Puts one binary (`conduit`) in `~/.local/bin`
- Creates the data directory `~/.conduit`
- Registers the MCP server with Claude Code
- Prints diagnostics

**What it does not do:** there is no daemon, no background service, no
containers, and nothing that starts at login. Conduit runs when you call it and
exits. It also does not install document extraction tools; see
[INSTALL_V2.md](INSTALL_V2.md#document-extraction-tools) if you index PDFs.

**Requirements:** macOS arm64 or Linux x86_64. About 13 MB for the binary, plus
Go 1.21+ and a C compiler if you use `--from-source`.

If `~/.local/bin` was not already on your `PATH`, the installer appends a block
to your shell's startup file and **names the file it wrote to** on the last
line of its output. Open a new terminal, or source that file — which one it is
depends on your shell and platform (`~/.zshrc` for zsh, `~/.bash_profile` for
bash on macOS, `~/.bashrc` for bash on Linux), so use the name the installer
printed rather than guessing.

> **Coming from Conduit 1.x?** Installing v2 does not remove the v1 daemon or
> its containers. Run `./scripts/remove-v1.sh` first — it is a dry run by
> default. There is no data migration; you re-add your sources below.

---

## Step 2: Verify

```bash
conduit doctor
```

Exit code 0 means everything needed works. Exit code 1 means a check failed,
and `doctor` will tell you which one and what to do about it.

---

## Step 3: Get the embedding model (optional)

```bash
conduit model download
```

This fetches `nomic-embed-text-v1.5` (262 MB) and verifies its SHA-256 against
a pinned registry entry. A mismatch deletes the download and fails.

**You can skip this.** Without a model, Conduit searches with FTS5 keyword
matching, which is a supported mode and not a broken install. Semantic search
also needs a `llama-server` binary on your `PATH`; see
[EMBEDDING_SIDECAR.md](EMBEDDING_SIDECAR.md).

To make lexical-only the intended configuration rather than a fallback:

```bash
conduit config set embed.provider none
```

---

## Step 4: Add your documents

```bash
conduit kb add ~/Documents/notes --name "Notes"
conduit kb add ./docs --name "Project Docs"
```

By default Conduit indexes documentation, source code, config, CSV/TSV, and
PDF/Word/RTF documents, skipping `node_modules`, `.git`, `vendor`, `dist`,
`build` and similar.

Some directories are refused outright because indexing them would copy secrets
into a database your AI client can search — `~/.ssh`, `~/.aws`, `~/.gnupg`,
`/etc` and others. That is deliberate. See the
[User Guide](USER_GUIDE.md#managing-sources).

---

## Step 5: Index

```bash
conduit kb sync
```

Watch the exit code:

| Code | Meaning |
|---|---|
| 0 | Full success |
| 1 | Error |
| 2 | Partial — keyword indexing worked, semantic indexing failed |

Code 2 means search works on keywords. Run `conduit doctor` to find out why the
embedding step failed; a cold model often just needs
`conduit doctor --probe-timeout 30`.

---

## Step 6: Try it

```bash
conduit kb search "how does authentication work"
conduit kb stats
```

If results look thin, try `--recall high` — it turns off diversity filtering.
For an exact identifier, use `--fts5`.

---

## Step 7: Connect your AI client

```bash
conduit setup                            # Claude Code (default)
conduit setup --client cursor
conduit mcp configure --client vscode
```

Then **restart the client**. Verify with:

```bash
conduit mcp status
```

Your assistant now has seven tools: `kb_search`, `kb_lexical_search`,
`kb_search_with_context`, `kb_list_sources`, `kb_get_document`, `kb_stats` and
`kag_query`. You do not need to name them — ask a question your documents can
answer and a well-behaved client will reach for them.

---

## One thing worth knowing

Conduit hands **raw chunks of your indexed documents** straight to the AI
client. That is what keeps it fast and private — no model in between.

It also means an indexed document can carry instructions to your assistant. If
you index untrusted content (third-party PDFs, scraped pages, shared drives),
treat it the way you would treat a fetched web page. Keep untrusted corpora in
a separate knowledge base with `--db`.

See SEC-003 in [KNOWN_ISSUES.md](KNOWN_ISSUES.md).

---

## Common next steps

**Separate knowledge bases** — one binary, many indexes:

```bash
conduit --db ~/work.db kb add ./work-docs --name "Work"
conduit --db ~/work.db kb search "Q3 planning"
```

**Backup** — it is one file:

```bash
cp ~/.conduit/conduit.db ~/backups/conduit.db
```

**Check configuration**:

```bash
conduit config
```

If you carried a config file over from Conduit 1.x, you will see a warning
naming keys that no longer exist. The file still loads; delete those keys to
silence it.

**Uninstall**:

```bash
conduit uninstall --dry-run
conduit uninstall            # keeps your data
conduit uninstall --all      # removes data too
```

---

## Where to go next

| Document | Contents |
|---|---|
| [USER_GUIDE.md](USER_GUIDE.md) | Sources, search tuning, MCP tools, workspaces |
| [ADMIN_GUIDE.md](ADMIN_GUIDE.md) | Full configuration schema, diagnostics, backup |
| [INSTALL_V2.md](INSTALL_V2.md) | Installation options, upgrades, 1.x migration |
| [KNOWN_ISSUES.md](KNOWN_ISSUES.md) | Security advisories and current limitations |
| [../README.md](../README.md) | What Conduit is, and what changed in v2 |
