#!/usr/bin/env bash
#
# install.sh - install Conduit 2.0.
#
# Conduit 2.0 is one binary. There is no daemon, no service to register, no
# containers to pull and no model to preload. This script copies an executable
# into place, points an AI client at it, and prints the diagnostics.
#
# That is the whole difference from v1, whose installer had to install a
# container runtime, pull two database images, start them, write a launchd or
# systemd unit, and download a model -- any one of which could fail on a machine
# where Conduit itself would have worked fine.
#
# The embedding model is NOT downloaded by default. It is a few hundred
# megabytes and this script should not start that transfer on a metered
# connection without being asked. Pass --model, or run `conduit model download`
# whenever you like; until then search works on keyword matching.
#
# Usage:
#   ./install.sh --from-source          # build from this checkout (needs Go)
#   ./install.sh                        # download a published release
#   ./install.sh --from-source --model  # build and fetch the embedding model
#
# If you are coming from Conduit 1.x, run scripts/remove-v1.sh first.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

readonly REPO="amlandas/Conduit-AI-Intelligence-Hub"
readonly BINARY="conduit"

PREFIX="${CONDUIT_PREFIX:-${HOME}/.local/bin}"
FROM_SOURCE=false
RUN_SETUP=true
DOWNLOAD_MODEL=false
MCP_CLIENT="claude-code"
RELEASE_TAG="latest"

# Populated by detect_platform.
OS=""
ARCH=""
PLATFORM=""

# Temporary working directory, cleaned up on exit.
WORKDIR=""

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

if [[ -t 1 ]]; then
    C_RESET=$'\033[0m'; C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'
    C_YELLOW=$'\033[0;33m'; C_BLUE=$'\033[0;34m'; C_BOLD=$'\033[1m'
else
    C_RESET=''; C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_BOLD=''
fi

info()    { printf '%s\n' "${C_BLUE}==>${C_RESET} $*"; }
success() { printf '%s\n' "  ${C_GREEN}OK${C_RESET} $*"; }
warn()    { printf '%s\n' "  ${C_YELLOW}!${C_RESET} $*" >&2; }
die()     { printf '%s\n' "${C_RED}error:${C_RESET} $*" >&2; exit 1; }

usage() {
    cat <<'EOF'
install.sh - install Conduit 2.0

USAGE
    ./install.sh [OPTIONS]

OPTIONS
    --from-source       Build from this checkout. Requires Go 1.21+ and a C
                        compiler: the knowledge base uses SQLite FTS5, which
                        needs cgo.
    --version TAG       Release tag to install (default: latest). Ignored with
                        --from-source.
    --prefix DIR        Where to install the binary (default: ~/.local/bin,
                        or $CONDUIT_PREFIX).
    --model             Download and verify the embedding model after install.
                        A few hundred MB. Off by default.
    --client NAME       AI client to configure: claude-code, cursor, vscode.
                        Default: claude-code.
    --no-setup          Install the binary only. Do not configure an AI client
                        and do not create the data directory.
    -h, --help          Show this help.

WHAT IT CHANGES
    - installs the conduit binary to --prefix
    - appends a PATH block to ~/.zshrc or ~/.bashrc, marked with a "# Conduit"
      comment, if the prefix is not already on PATH. That marker is what lets
      uninstall.sh remove the block later without guessing at your own edits.
    - registers the MCP server with an AI client (unless --no-setup)

    It writes nothing else, installs no services, and pulls no containers.

SUPPORTED PLATFORMS
    macOS arm64 (Apple Silicon), Linux x86_64.

    Intel Macs and Linux arm64 can build from source; there are no published
    binaries for them. Windows is not supported yet -- see install-windows.ps1.

UPGRADING FROM CONDUIT 1.x
    This script does not remove the v1 daemon, its service registration or its
    containers. Run scripts/remove-v1.sh first (it defaults to a dry run).

EXAMPLES
    ./install.sh --from-source
    ./install.sh --from-source --model --client cursor
    ./install.sh --prefix /usr/local/bin
EOF
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case "$1" in
        --from-source) FROM_SOURCE=true; shift ;;
        --model)       DOWNLOAD_MODEL=true; shift ;;
        --no-setup)    RUN_SETUP=false; shift ;;
        --prefix)
            [[ $# -ge 2 ]] || die "--prefix requires a directory"
            PREFIX="$2"; shift 2 ;;
        --version)
            [[ $# -ge 2 ]] || die "--version requires a tag"
            RELEASE_TAG="$2"; shift 2 ;;
        --client)
            [[ $# -ge 2 ]] || die "--client requires a name"
            MCP_CLIENT="$2"; shift 2 ;;
        -h|--help)     usage; exit 0 ;;
        *)             die "unknown option: $1 (try --help)" ;;
    esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

cleanup() {
    if [[ -n "$WORKDIR" && -d "$WORKDIR" ]]; then
        rm -rf -- "$WORKDIR"
    fi
    return 0
}
trap cleanup EXIT

# sha256_of prints the SHA-256 of a file, on either macOS or Linux.
sha256_of() {
    local file="$1"
    if have sha256sum; then
        sha256sum "$file" | awk '{print $1}'
    elif have shasum; then
        shasum -a 256 "$file" | awk '{print $1}'
    else
        die "no sha256sum or shasum on this machine; cannot verify the download"
    fi
}

detect_platform() {
    local raw_os raw_arch
    raw_os="$(uname -s)"
    raw_arch="$(uname -m)"

    case "$raw_os" in
        Darwin) OS="darwin" ;;
        Linux)  OS="linux" ;;
        *)      die "unsupported operating system: $raw_os (macOS and Linux only)" ;;
    esac

    case "$raw_arch" in
        arm64|aarch64) ARCH="arm64" ;;
        x86_64|amd64)  ARCH="amd64" ;;
        *)             die "unsupported architecture: $raw_arch" ;;
    esac

    PLATFORM="${OS}_${ARCH}"
}

# ---------------------------------------------------------------------------
# Install from source
# ---------------------------------------------------------------------------

repo_root() {
    local script_dir
    script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
    (cd -- "${script_dir}/.." && pwd)
}

install_from_source() {
    info "Building from source"

    local root
    root="$(repo_root)"

    [[ -f "${root}/go.mod" ]] || \
        die "no go.mod at ${root}; run this script from a Conduit checkout"

    have go || die "Go is not installed. Install Go 1.21+ from https://go.dev/dl/ and retry."

    # cgo is not optional here. The knowledge base is SQLite with FTS5, and a
    # CGO_ENABLED=0 build produces a binary that compiles, starts, and then
    # fails every single search with 'no such module: fts5'.
    if ! have cc && ! have gcc && ! have clang; then
        die "no C compiler found. cgo is required for SQLite FTS5.
  macOS: xcode-select --install
  Linux: apt install build-essential  (or the equivalent)"
    fi

    local version build_time
    version="$(git -C "$root" describe --tags --always --dirty 2>/dev/null || echo dev)"
    build_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

    WORKDIR="$(mktemp -d)"
    local out="${WORKDIR}/${BINARY}"

    info "Compiling ${BINARY} ${version} (this takes a minute)"
    (
        cd "$root"
        CGO_ENABLED=1 go build \
            -tags fts5 \
            -ldflags "-s -w -X main.version=${version} -X main.buildTime=${build_time}" \
            -o "$out" \
            ./cmd/conduit
    ) || die "build failed"

    [[ -x "$out" ]] || die "build produced no binary at $out"
    success "built ${version}"

    install_binary "$out"
}

# ---------------------------------------------------------------------------
# Install from a published release
#
# The download path is written and verified; the artifacts it expects are
# published by WP-4.1. Until then the release lookup fails and says so, rather
# than half-installing something.
# ---------------------------------------------------------------------------

release_base_url() {
    if [[ "$RELEASE_TAG" == "latest" ]]; then
        printf 'https://github.com/%s/releases/latest/download' "$REPO"
    else
        printf 'https://github.com/%s/releases/download/%s' "$REPO" "$RELEASE_TAG"
    fi
}

# fetch downloads a URL to a path. It fails on HTTP errors rather than saving
# an error page, and it never pipes anything into a shell.
fetch() {
    local url="$1" dest="$2"

    if have curl; then
        curl --fail --location --silent --show-error --proto '=https' --tlsv1.2 \
             --output "$dest" "$url"
    elif have wget; then
        wget --quiet --https-only --output-document="$dest" "$url"
    else
        die "neither curl nor wget is available; cannot download the release"
    fi
}

install_from_release() {
    info "Installing from release (${RELEASE_TAG})"

    local base tarball sums_url tar_url
    base="$(release_base_url)"
    tarball="${BINARY}_${PLATFORM}.tar.gz"
    tar_url="${base}/${tarball}"
    sums_url="${base}/SHA256SUMS"

    WORKDIR="$(mktemp -d)"

    # The checksum manifest is fetched first: if it is not there, no release
    # artifacts are, and there is no point downloading a tarball we could not
    # verify anyway.
    if ! fetch "$sums_url" "${WORKDIR}/SHA256SUMS" 2>/dev/null; then
        die "no release artifacts found for ${RELEASE_TAG}.

Conduit 2.0 has not published binaries yet. Build from this checkout instead:

    ./scripts/install.sh --from-source

That needs Go 1.21+ and a C compiler."
    fi

    local expected
    expected="$(awk -v f="$tarball" '$2 == f || $2 == "*"f {print $1}' "${WORKDIR}/SHA256SUMS" | head -1)"
    [[ -n "$expected" ]] || \
        die "release ${RELEASE_TAG} publishes no ${tarball}.
This platform (${PLATFORM}) has no prebuilt binary; use --from-source."

    info "Downloading ${tarball}"
    fetch "$tar_url" "${WORKDIR}/${tarball}" || \
        die "download failed: ${tar_url}"

    # Verification is mandatory. There is no flag to skip it.
    local actual
    actual="$(sha256_of "${WORKDIR}/${tarball}")"
    if [[ "$actual" != "$expected" ]]; then
        rm -f -- "${WORKDIR}/${tarball}"
        die "checksum mismatch for ${tarball}
  expected  ${expected}
  got       ${actual}
The download was deleted. Do not install this artifact."
    fi
    success "checksum verified"

    tar -xzf "${WORKDIR}/${tarball}" -C "$WORKDIR" || die "could not extract ${tarball}"

    local extracted="${WORKDIR}/${BINARY}"
    [[ -f "$extracted" ]] || die "${tarball} does not contain a '${BINARY}' binary"
    chmod +x "$extracted"

    install_binary "$extracted"
}

# ---------------------------------------------------------------------------
# Placing the binary
# ---------------------------------------------------------------------------

install_binary() {
    local src="$1"
    local dest="${PREFIX}/${BINARY}"

    info "Installing to ${dest}"

    mkdir -p -- "$PREFIX" || die "cannot create ${PREFIX}"
    [[ -w "$PREFIX" ]] || \
        die "${PREFIX} is not writable. Choose another --prefix, or create it with the right ownership."

    # Replacing a running or previously-installed binary: write beside it and
    # rename, so an interrupted install cannot leave a truncated executable on
    # PATH. Rename is atomic within a filesystem, which is why the temp file
    # goes in PREFIX rather than /tmp.
    # Sweep staging files from earlier runs that were killed between the copy
    # and the rename. They are inert, but they are also full-size copies of the
    # binary sitting in a directory on the user's PATH, and nothing else will
    # ever clean them up.
    local stale
    for stale in "${dest}".new.*; do
        [[ -e "$stale" ]] || continue   # no match: the glob stayed literal
        rm -f -- "$stale" || true
    done

    local staged="${dest}.new.$$"
    # Remove the staging file on any exit from here on, so an interrupted copy
    # does not become the leftover this function just swept.
    trap 'rm -f -- "'"$staged"'"' EXIT

    cp -- "$src" "$staged" || die "could not copy the binary into ${PREFIX}"
    chmod 755 "$staged"

    if ! mv -f -- "$staged" "$dest"; then
        rm -f -- "$staged"
        die "could not install to ${dest}"
    fi

    # Reinstate the workdir cleanup the staging trap displaced.
    trap cleanup EXIT

    success "installed ${dest}"

    # v1 symlinked into /usr/local/bin. A stale symlink there shadows the new
    # binary for anyone whose PATH prefers it, which looks exactly like the
    # install having silently done nothing.
    local legacy="/usr/local/bin/${BINARY}"
    if [[ -L "$legacy" && "$(readlink "$legacy")" != "$dest" ]]; then
        warn "${legacy} is a symlink to $(readlink "$legacy")"
        warn "  It shadows the binary just installed. Remove it with:"
        warn "    sudo rm ${legacy}"
    fi
}

# ---------------------------------------------------------------------------
# PATH
# ---------------------------------------------------------------------------

# CONDUIT_PATH_MARKER introduces the PATH line this script writes.
#
# It is the ONLY thing the uninstaller matches on, so the two must stay in
# step -- and it must stay identical to CONDUIT_PATH_MARKER in uninstall.sh and
# conduitPathMarker in internal/setup/setup.go.
readonly CONDUIT_PATH_MARKER="# Conduit"

# profile_for_shell returns the rc file this user's shell reads.
#
# fish is excluded deliberately: its syntax is not POSIX, so the block written
# below would be a syntax error rather than a PATH entry.
profile_for_shell() {
    case "$(basename "${SHELL:-sh}")" in
        zsh)  printf '%s' "${HOME}/.zshrc" ;;
        bash) printf '%s' "${HOME}/.bashrc" ;;
        *)    printf '' ;;
    esac
}

# add_to_path appends Conduit's PATH block to the user's shell profile.
#
# Previously this only printed the line and told the user to add it themselves.
# The result was that nothing on any machine ever carried the marker comment,
# which made the uninstaller's profile cleanup dead code: it searched for a
# signature that by construction did not exist, while the help promised to
# remove PATH entries. Either the installer writes the marker or the
# uninstaller should stop claiming to remove it; writing it is the half that
# also saves the user a manual step.
#
# The block is appended, never edited in place, and the marker is what makes it
# removable later without guessing.
add_to_path() {
    local profile
    profile="$(profile_for_shell)"

    if [[ -z "$profile" ]]; then
        warn "${PREFIX} is not on your PATH."
        case "$(basename "${SHELL:-sh}")" in
            fish) warn "  Add it with: fish_add_path ${PREFIX}" ;;
            *)    warn "  Add it with: export PATH=\"${PREFIX}:\$PATH\"" ;;
        esac
        return 0
    fi

    # Already ours? Adding a second copy would put two identical entries on
    # PATH and leave the uninstaller two blocks to remove.
    if [[ -f "$profile" ]] && grep -qF "$CONDUIT_PATH_MARKER" "$profile" 2>/dev/null; then
        info "PATH entry already present in ${profile}"
        return 0
    fi

    info "Adding ${PREFIX} to PATH in ${profile}"

    {
        printf '\n%s\n' "$CONDUIT_PATH_MARKER"
        printf 'export PATH="%s:$PATH"\n' "$PREFIX"
    } >> "$profile" || {
        warn "could not write to ${profile}. Add this yourself:"
        warn "    export PATH=\"${PREFIX}:\$PATH\""
        return 0
    }

    success "PATH updated in ${profile}"
    warn "Open a new shell, or run: export PATH=\"${PREFIX}:\$PATH\""
}

check_path() {
    case ":${PATH}:" in
        *":${PREFIX}:"*) return 0 ;;
    esac
    add_to_path
}

# ---------------------------------------------------------------------------
# Post-install
# ---------------------------------------------------------------------------

run_setup() {
    local conduit="${PREFIX}/${BINARY}"

    info "Running setup"
    # Called through the freshly installed path rather than whatever `conduit`
    # resolves to, so a stale binary elsewhere on PATH cannot configure the
    # machine on the new one's behalf.
    if ! "$conduit" setup --client "$MCP_CLIENT"; then
        warn "setup reported problems; the binary is installed and usable."
        warn "  Re-run it with: ${conduit} setup"
    fi
}

download_model() {
    local conduit="${PREFIX}/${BINARY}"

    info "Downloading the embedding model"
    if ! "$conduit" model download; then
        warn "model download failed. Conduit still works with keyword search."
        warn "  Retry with: ${conduit} model download"
    fi
}

print_doctor() {
    local conduit="${PREFIX}/${BINARY}"

    info "Diagnostics"
    # doctor exits non-zero when a check fails, which is expected on a fresh
    # install with no documents indexed. Its output is the point, not its code.
    "$conduit" doctor || true
}

warn_about_v1() {
    local found=false

    [[ -e "${HOME}/Library/LaunchAgents/dev.simpleflo.conduit.plist" ]] && found=true
    [[ -e "${HOME}/Library/LaunchAgents/com.simpleflo.conduit.plist" ]] && found=true
    [[ -e "${HOME}/.config/systemd/user/conduit.service" ]] && found=true
    [[ -e "${HOME}/.local/bin/conduit-daemon" ]] && found=true

    [[ "$found" == true ]] || return 0

    printf '\n'
    warn "This machine still has Conduit 1.x components installed:"
    warn "  a daemon and/or a service registration that v2 does not use."
    warn "  They will keep starting at login until removed."
    warn ""
    warn "  Remove them with:"
    warn "    ./scripts/remove-v1.sh            # dry run, shows what it found"
    warn "    ./scripts/remove-v1.sh --yes      # remove them"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    printf '%s\n' "${C_BOLD}Conduit 2.0 installer${C_RESET}"
    printf '%s\n\n' "One binary. No daemon, no containers, no background service."

    detect_platform
    info "Platform: ${OS} ${ARCH}"

    if [[ "$FROM_SOURCE" == true ]]; then
        install_from_source
    else
        install_from_release
    fi

    check_path

    if [[ "$RUN_SETUP" == true ]]; then
        run_setup
    else
        info "Skipping setup (--no-setup)"
    fi

    if [[ "$DOWNLOAD_MODEL" == true ]]; then
        download_model
    fi

    if [[ "$RUN_SETUP" == true ]]; then
        print_doctor
    fi

    warn_about_v1

    printf '\n%s\n' "${C_BOLD}Done.${C_RESET}"
    printf '%s\n' "  ${BINARY} kb add <folder>     # index a folder"
    printf '%s\n' "  ${BINARY} kb sync             # build the index"
    printf '%s\n' "  ${BINARY} kb search \"query\"    # check it works"
    if [[ "$DOWNLOAD_MODEL" != true ]]; then
        printf '%s\n' "  ${BINARY} model download      # enable semantic search"
    fi
}

main "$@"
