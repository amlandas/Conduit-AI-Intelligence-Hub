# Release process

**Status:** personal-grade. Unsigned, un-notarised, macOS + Linux only.

Cutting a release is pushing a tag. Everything else is CI.

---

## Cutting one

```bash
git checkout v2
git pull
git tag v2.0.0-beta.1
git push origin v2.0.0-beta.1
```

`.github/workflows/release.yml` triggers on `v2.*` tags and does the rest:

1. Builds `conduit` natively on `macos-latest` and `ubuntu-latest`
   (`CGO_ENABLED=1`, `-tags fts5`, `-trimpath`), with the version injected from
   the tag.
2. Smoke-tests each artifact: it must run, report the tag from
   `conduit version --json`, and report FTS5 available from
   `conduit doctor --json`.
3. Packages each as `conduit-<os>-<arch>.tar.gz`.
4. Generates `SHA256SUMS` over both.
5. Publishes a **pre-release** with the tarballs and the manifest attached.

Nothing is uploaded by hand. If a step fails, no release appears — that is the
point.

Re-pushing the same tag re-runs the workflow and replaces the assets
(`gh release upload --clobber`), so a botched run can be fixed by deleting the
tag and pushing it again.

### Version numbers

The version comes from the tag and nowhere else. `main.Version` and
`main.BuildTime` are set with `-ldflags -X`.

> Both names are **capitalised**. Go's linker silently ignores `-X` for a symbol
> that does not exist, so a lower-cased name does not fail the build — it ships
> a binary that reports `dev`. That bug was live in `install.sh --from-source`
> for the whole of v2 development and nothing caught it. If a release ever
> reports `dev`, this is why.

---

## The artifact contract

Three things have to agree, and only one of them is checked by a compiler:

| | |
|---|---|
| Archive name | `conduit-<os>-<arch>.tar.gz` |
| Archive contents | exactly one executable, `conduit`, at the root |
| Manifest | `SHA256SUMS`, `sha256sum` format, bare basenames |
| Platforms | `darwin-arm64`, `linux-amd64` |

Written in `.github/workflows/release.yml` (the `Package` and `Checksums`
steps) and consumed by `scripts/install.sh` (`install_from_release`).
`tests/scripts/release_test.go` holds both halves to it — including
`TestRealArtifactRoundTrip`, which builds the genuine binary with the
workflow's flags, serves it from a stub GitHub, installs it, and runs it.

**Changing either side means changing both**, and the round-trip test is what
will tell you if you forgot.

### Why native builds, not a cross-compile

The knowledge base is SQLite with FTS5, which comes from `mattn/go-sqlite3` — a
cgo package. Setting `GOOS`/`GOARCH` implicitly disables cgo, and the resulting
binary compiles, links, starts, and then fails every single call with:

```
Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work.
```

Forcing `CGO_ENABLED=1` does not help either: cross-compiling cgo needs a C
cross-toolchain per target that no single host has. Hence a matrix of real
runners. The same note sits in the `Makefile` where a `build-all-platforms`
target used to be.

The per-artifact FTS5 smoke test exists for the same reason. `ci.yml` proves
the *source tree* builds with FTS5; the release workflow proves the *binary a
user downloads* has it.

---

## Verifying a release by hand

```bash
TAG=v2.0.0-beta.1
BASE="https://github.com/amlandas/Conduit-AI-Intelligence-Hub/releases/download/$TAG"

curl -fLO "$BASE/SHA256SUMS"
curl -fLO "$BASE/conduit-darwin-arm64.tar.gz"

shasum -a 256 -c SHA256SUMS --ignore-missing    # or sha256sum -c
tar -tzf conduit-darwin-arm64.tar.gz            # expect exactly: conduit

tar -xzf conduit-darwin-arm64.tar.gz
./conduit version --json                         # must report $TAG, not "dev"
./conduit doctor --json --data-dir "$(mktemp -d)" | grep -A2 FTS5
```

Or just let the installer do it — verification is mandatory there and cannot be
skipped:

```bash
./scripts/install.sh --version "$TAG"
```

### Pre-releases and `/releases/latest`

Every v2 release is a GitHub **pre-release**, and GitHub's `/releases/latest`
endpoint **excludes pre-releases**. Anything reaching for that endpoint will
either 404 or silently get `v0.1.10` — the newest non-pre-release, from the
daemon era, a different product.

`install.sh` resolves the newest `v2.*` entry from the releases list API
instead. Any other tooling written against these releases must do the same.

---

## What is deliberately not here

| | Why |
|---|---|
| **Apple code signing / notarisation** | Needs a paid Developer ID and a signing identity in CI. Deferred to public launch. Note that quarantine is applied by the downloading program, so it does NOT affect `install.sh` — curl and wget do not set the attribute (verified). It affects **browser** downloads of the release tarball, where `xattr -d com.apple.quarantine` clears it. `--from-source` avoids it entirely. |
| **Windows** | No native build, no runner in the matrix, `scripts/install-windows.ps1` is untested against v2. |
| **linux-arm64, darwin-amd64** | No demand yet. `--from-source` works on both. |
| **Homebrew tap / apt repo** | Packaging overhead that a personal-grade release does not earn. |
| **Signed checksums (minisign, cosign)** | `SHA256SUMS` is served over HTTPS from the same origin as the artifacts, so it defends against corruption, not against a compromised repository. Real provenance needs signing, which is the same deferred work as above. |
| **Automatic release notes curation** | `--generate-notes` produces the commit list. Good enough for a beta. |

When any of these lands, this table is the checklist to update.

---

## Failure modes worth recognising

| Symptom | Cause |
|---|---|
| Workflow did not run at all | Tag does not match `v2.*`. The trigger is deliberately narrow so a stray `v0.1.x` tag cannot start it. |
| `version` reports `dev` | `-ldflags -X` naming a symbol that does not exist. See above. |
| Release published with one platform | Should be impossible: `fail-fast: true` plus an explicit both-artifacts-present check in the `Checksums` step. If you see it, that check regressed. |
| `install.sh` says "no Conduit 2.0 release has been published yet" | The releases list was **read successfully** and holds no `v2.*` entry — either nothing is published, or the workflow failed before the publish step. This message no longer covers a failed lookup: rate limiting, an unreachable API and a non-list response each report themselves, so seeing this one really does mean the list was empty. |
| `install.sh` says the API refused the request (403/429) | GitHub's unauthenticated API rate limit, 60/hour/IP. Nothing to do with the release. Use `--version TAG`, which does not call the API. |
| `install.sh` says the API answered with something that is not a list | A captive portal or an intercepting proxy. Also use `--version TAG`. |
| macOS refuses to run a binary downloaded **through a browser** | Gatekeeper quarantine. Not a broken build. Binaries fetched by `install.sh` (curl/wget) are not quarantined. |
