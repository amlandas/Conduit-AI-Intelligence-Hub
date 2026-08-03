#!/usr/bin/env bash
#
# uninstall.sh - remove Conduit 2.0.
#
# The binary knows better than this script does what it installed: which MCP
# clients it configured, which shell profiles it edited, where its data lives.
# So wherever a `conduit` binary is still present, this script delegates to
# `conduit uninstall` and only cleans up what is left. The standalone path
# exists for the case that actually needs a script -- a binary that is missing,
# broken, or built for the wrong architecture.
#
# DATA SAFETY
#
#   Your indexed data is kept by default. Only --remove-data deletes it, and
#   only after you confirm.
#
# This script does NOT remove the Conduit 1.x daemon, its service registration
# or its containers. Use scripts/remove-v1.sh for that.
#
# Usage:
#   ./uninstall.sh                  # remove the program, keep data
#   ./uninstall.sh --remove-data    # remove everything
#   ./uninstall.sh --dry-run        # show what would happen
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

readonly BINARY="conduit"

DATA_DIR="${CONDUIT_DATA_DIR:-${HOME}/.conduit}"
PREFIX=""
REMOVE_DATA=false
FORCE=false
DRY_RUN=false

FOUND=0
REMOVED=0
FAILED=0

# Paths an install may have used. Kept in step with internal/setup/setup.go.
BINARY_PATHS=(
    "${HOME}/.local/bin/${BINARY}"
    "${HOME}/bin/${BINARY}"
)
SYMLINK_PATHS=(
    "/usr/local/bin/${BINARY}"
    "/opt/homebrew/bin/${BINARY}"
)

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

if [[ -t 1 ]]; then
    C_RESET=$'\033[0m'; C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'
    C_YELLOW=$'\033[0;33m'; C_BLUE=$'\033[0;34m'; C_BOLD=$'\033[1m'
else
    C_RESET=''; C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_BOLD=''
fi

info() { printf '%s\n' "${C_BLUE}==>${C_RESET} $*"; }
ok()   { printf '%s\n' "  ${C_GREEN}removed${C_RESET}  $*"; }
plan() { printf '%s\n' "  ${C_YELLOW}would remove${C_RESET}  $*"; }
warn() { printf '%s\n' "  ${C_YELLOW}!${C_RESET} $*" >&2; }
bad()  { printf '%s\n' "  ${C_RED}failed${C_RESET}  $*" >&2; }
die()  { printf '%s\n' "${C_RED}error:${C_RESET} $*" >&2; exit 1; }

usage() {
    cat <<'EOF'
uninstall.sh - remove Conduit 2.0

USAGE
    ./uninstall.sh [OPTIONS]

OPTIONS
    --remove-data   Also delete the data directory: the knowledge base, the
                    configuration and any downloaded embedding models.
                    Without this flag, data is kept.
    --data-dir DIR  Conduit data directory (default: ~/.conduit,
                    or $CONDUIT_DATA_DIR).
    --prefix DIR    Remove the install at DIR instead of searching the usual
                    locations. Use this if you installed with
                    `install.sh --prefix DIR`.
    --force         Skip confirmation prompts.
    --dry-run       Show what would be removed and change nothing.
    -h, --help      Show this help.

WHAT IS REMOVED
    - the conduit binary and any symlinks to it
    - Conduit's MCP entries in Claude Code, Cursor and VS Code
    - PATH lines Conduit added to your shell profile
    - the data directory, but only with --remove-data

WHAT IS NEVER REMOVED
    Tools you may share with other projects: Ollama, poppler, llama.cpp,
    Docker, Podman. Remove those yourself if nothing else needs them.

CONDUIT 1.x
    This script knows nothing about the v1 daemon, its launchd/systemd
    registration or its containers. Remove those with:
        ./scripts/remove-v1.sh --dry-run

EXAMPLES
    ./uninstall.sh                       # remove the program, keep data
    ./uninstall.sh --dry-run             # preview
    ./uninstall.sh --remove-data --force # remove everything, no prompts
EOF
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case "$1" in
        --remove-data) REMOVE_DATA=true; shift ;;
        --force|-f)    FORCE=true; shift ;;
        --dry-run)     DRY_RUN=true; shift ;;
        --data-dir)
            [[ $# -ge 2 ]] || die "--data-dir requires a directory"
            DATA_DIR="$2"; shift 2 ;;
        --prefix)
            [[ $# -ge 2 ]] || die "--prefix requires a directory"
            PREFIX="$2"; shift 2 ;;
        -h|--help)     usage; exit 0 ;;
        *)             die "unknown option: $1 (try --help)" ;;
    esac
done

# An explicit --prefix targets exactly one install and nothing else. Searching
# the default locations as well would make `--prefix /tmp/test` quietly remove
# the user's real installation, which is the opposite of what it asks for.
if [[ -n "$PREFIX" ]]; then
    BINARY_PATHS=("${PREFIX}/${BINARY}")
    SYMLINK_PATHS=()
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

# assert_safe_data_dir refuses a --data-dir that --remove-data would turn into a
# catastrophe. `--data-dir /` or `--data-dir $HOME` is a typo, not an intent,
# and the confirmation prompt is not enough on its own: it asks about deleting
# "the data directory", and the user answering has no reason to suspect it now
# means their entire home.
assert_safe_data_dir() {
    local dir="$1"

    [[ -n "$dir" ]] || die "--data-dir cannot be empty"

    case "$dir" in
        /|/usr|/etc|/var|/home|/Users|/opt|/tmp)
            die "refusing to treat $dir as a Conduit data directory" ;;
    esac

    if [[ "$dir" == "$HOME" ]]; then
        die "refusing to treat your home directory as a Conduit data directory"
    fi

    if [[ "$dir" != /* ]]; then
        die "--data-dir must be an absolute path (got: $dir)"
    fi
}

assert_safe_data_dir "$DATA_DIR"

remove_path() {
    local path="$1"

    [[ -e "$path" || -L "$path" ]] || return 0
    FOUND=$((FOUND + 1))

    if [[ "$DRY_RUN" == true ]]; then
        plan "$path"
        return 0
    fi

    if [[ ! -w "$(dirname "$path")" ]]; then
        warn "$path is not writable by this user; remove it with:"
        warn "    sudo rm -f $path"
        return 0
    fi

    if rm -rf -- "$path"; then
        REMOVED=$((REMOVED + 1))
        ok "$path"
    else
        FAILED=$((FAILED + 1))
        bad "$path"
    fi
}

confirm() {
    local prompt="$1" want="$2" answer

    [[ "$FORCE" == true ]] && return 0
    [[ "$DRY_RUN" == true ]] && return 0

    if [[ ! -t 0 ]]; then
        die "confirmation required but stdin is not a terminal; re-run with --force if you are sure"
    fi

    printf '%s' "$prompt"
    read -r answer
    [[ "$answer" == "$want" ]]
}

# find_binary locates a usable conduit executable.
#
# The ${arr[@]+"${arr[@]}"} form is not decoration: under `set -u`, bash 3.2 --
# which is still what /bin/bash is on macOS -- treats "${empty[@]}" as an
# unbound variable and aborts. --prefix empties SYMLINK_PATHS.
find_binary() {
    local path
    for path in "${BINARY_PATHS[@]}" ${SYMLINK_PATHS[@]+"${SYMLINK_PATHS[@]}"}; do
        if [[ -x "$path" ]]; then
            printf '%s' "$path"
            return 0
        fi
    done
    # An explicit --prefix means "this install and no other", so PATH is not
    # consulted: it would find somebody else's copy.
    if [[ -z "$PREFIX" ]] && have "$BINARY"; then
        command -v "$BINARY"
        return 0
    fi
    return 1
}

# ---------------------------------------------------------------------------
# Delegation
#
# `conduit uninstall` removes its own MCP entries, shell PATH lines and GUI
# state. Reimplementing that here would guarantee the two drift apart, and the
# script would be the one that is wrong.
# ---------------------------------------------------------------------------

delegate_to_binary() {
    local conduit="$1"
    local -a args=("uninstall" "--force")

    if [[ "$REMOVE_DATA" == true ]]; then
        args+=("--all")
    else
        args+=("--keep-data")
    fi
    [[ "$DRY_RUN" == true ]] && args+=("--dry-run")
    # Without this, `uninstall.sh --prefix /tmp/x` would delegate to a binary
    # that then removes the copy in ~/.local/bin: the exact install the flag
    # promised to leave alone.
    [[ -n "$PREFIX" ]] && args+=("--prefix" "$PREFIX")

    info "Delegating to ${conduit} uninstall"

    # A binary that cannot run -- wrong architecture, missing library, or
    # simply broken -- must not stop the uninstall. Fall through to the manual
    # path instead.
    if ! "$conduit" "${args[@]}"; then
        warn "'${conduit} uninstall' failed; falling back to manual removal"
        return 1
    fi
    return 0
}

# ---------------------------------------------------------------------------
# Manual removal
# ---------------------------------------------------------------------------

remove_binaries() {
    info "Binaries"

    local path
    for path in "${BINARY_PATHS[@]}"; do
        remove_path "$path"
    done

    # Only remove a symlink that points at Conduit. A real binary somebody
    # installed at /usr/local/bin/conduit by hand is theirs, not ours.
    for path in ${SYMLINK_PATHS[@]+"${SYMLINK_PATHS[@]}"}; do
        if [[ -L "$path" ]]; then
            remove_path "$path"
        elif [[ -e "$path" ]]; then
            FOUND=$((FOUND + 1))
            warn "$path exists but is not a symlink; left in place."
            warn "    Remove it yourself if you no longer want it: sudo rm $path"
        fi
    done
}

remove_shell_path_lines() {
    info "Shell profiles"

    local files=(
        "${HOME}/.zshrc"
        "${HOME}/.bashrc"
        "${HOME}/.bash_profile"
        "${HOME}/.profile"
    )

    local file
    for file in "${files[@]}"; do
        [[ -f "$file" ]] || continue
        grep -q 'conduit' "$file" 2>/dev/null || continue

        FOUND=$((FOUND + 1))

        if [[ "$DRY_RUN" == true ]]; then
            plan "Conduit lines in $file"
            grep -n 'conduit' "$file" | sed 's/^/      /'
            continue
        fi

        # Never edit a profile in place without a copy: a broken .zshrc locks
        # somebody out of their own shell.
        local backup="${file}.conduit-uninstall.bak"
        cp -- "$file" "$backup" || { bad "could not back up $file"; continue; }

        local tmp
        tmp="$(mktemp)"
        grep -v 'conduit' "$file" > "$tmp" || true

        if mv -f -- "$tmp" "$file"; then
            REMOVED=$((REMOVED + 1))
            ok "Conduit lines in $file  (backup: $backup)"
        else
            rm -f -- "$tmp"
            FAILED=$((FAILED + 1))
            bad "$file"
        fi
    done
}

remove_mcp_entries_manually() {
    info "MCP client entries"

    # Without a working binary there is no safe way to edit a JSON config from
    # shell: these files hold the user's other MCP servers and unrelated
    # settings, and a sed-based edit would eventually eat one of them.
    local configs=(
        "${HOME}/.claude.json"
        "${HOME}/.cursor/settings/extensions.json"
        "${HOME}/.vscode/settings.json"
    )

    local file listed=false
    for file in "${configs[@]}"; do
        [[ -f "$file" ]] || continue
        grep -q 'conduit-kb' "$file" 2>/dev/null || continue

        listed=true
        FOUND=$((FOUND + 1))
        warn "$file still registers the 'conduit-kb' MCP server."
    done

    if [[ "$listed" == true ]]; then
        warn "No working conduit binary was available to remove these safely."
        warn "  Delete the 'conduit-kb' entry from each file by hand: editing"
        warn "  them from a shell script risks losing your other settings."
    else
        printf '%s\n' "  none found"
    fi
}

remove_data_dir() {
    if [[ "$REMOVE_DATA" != true ]]; then
        info "Data"
        if [[ -d "$DATA_DIR" ]]; then
            printf '%s\n' "  ${C_GREEN}kept${C_RESET}  $DATA_DIR"
            printf '%s\n' "  Your knowledge base, configuration and models are untouched."
            printf '%s\n' "  Pass --remove-data to delete them."
        else
            printf '%s\n' "  no data directory at $DATA_DIR"
        fi
        return 0
    fi

    info "Data"

    if [[ ! -d "$DATA_DIR" ]]; then
        printf '%s\n' "  no data directory at $DATA_DIR"
        return 0
    fi

    local size
    size="$(du -sh "$DATA_DIR" 2>/dev/null | awk '{print $1}')"
    printf '%s\n' "  ${C_BOLD}${DATA_DIR}${C_RESET} (${size:-unknown size})"

    if ! confirm "  Permanently delete this? Type UNINSTALL to confirm: " "UNINSTALL"; then
        printf '%s\n' "  ${C_YELLOW}kept${C_RESET}  $DATA_DIR (not confirmed)"
        return 0
    fi

    remove_path "$DATA_DIR"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    printf '%s\n' "${C_BOLD}Conduit 2.0 uninstaller${C_RESET}"
    printf '%s\n' "Data directory: ${DATA_DIR}"

    if [[ "$DRY_RUN" == true ]]; then
        printf '%s\n\n' "${C_YELLOW}DRY RUN - nothing will be removed.${C_RESET}"
    else
        printf '\n'
    fi

    local conduit delegated=false
    if conduit="$(find_binary)"; then
        if delegate_to_binary "$conduit"; then
            delegated=true
        fi
    else
        info "No conduit binary found; removing what is on disk"
    fi

    # Always sweep the filesystem afterwards. `conduit uninstall` removes the
    # binary it knows about; a second copy at another prefix would otherwise
    # survive and keep answering `conduit`.
    remove_binaries

    # Shell profiles and MCP entries are per-user, not per-install. Under
    # --prefix they belong to whichever copy is on PATH -- the one the flag
    # promised not to disturb -- so the fallback path must respect that scoping
    # just as the delegated path does.
    if [[ "$delegated" != true && -z "$PREFIX" ]]; then
        remove_shell_path_lines
        remove_mcp_entries_manually
    elif [[ "$delegated" != true && -n "$PREFIX" ]]; then
        info "Shell profiles and MCP entries"
        printf '%s\n' "  skipped: --prefix targets one install, these are shared"
    fi

    remove_data_dir

    printf '\n%s\n' "${C_BOLD}Summary${C_RESET}"
    if [[ "$delegated" == true ]]; then
        # The binary reported its own removals above. These counters cover only
        # what was left for the script to clean up afterwards, and saying
        # "0 removed" without that context reads like nothing happened.
        printf '%s\n' "  conduit uninstall did the work; its report is above."
    fi
    if [[ "$DRY_RUN" == true ]]; then
        printf '%s\n' "  $FOUND additional item(s) found, 0 removed (dry run)."
    else
        printf '%s\n' "  $FOUND additional found, $REMOVED removed, $FAILED failed."
    fi

    printf '\n%s\n' "Conduit never removes tools you may share with other projects."
    printf '%s\n' "  Ollama:      rm -rf ~/.ollama && brew uninstall ollama"
    printf '%s\n' "  poppler:     brew uninstall poppler"
    printf '%s\n' "  llama.cpp:   brew uninstall llama.cpp"

    if [[ -e "${HOME}/.local/bin/conduit-daemon" ]] ||
       [[ -e "${HOME}/Library/LaunchAgents/dev.simpleflo.conduit.plist" ]] ||
       [[ -e "${HOME}/Library/LaunchAgents/com.simpleflo.conduit.plist" ]] ||
       [[ -e "${HOME}/.config/systemd/user/conduit.service" ]]; then
        printf '\n'
        warn "Conduit 1.x components are still installed on this machine."
        warn "  Remove them with: ./scripts/remove-v1.sh --dry-run"
    fi

    [[ "$FAILED" -gt 0 ]] && return 1
    return 0
}

main "$@"
