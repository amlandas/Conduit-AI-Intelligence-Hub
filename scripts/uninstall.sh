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

# EXIT_USER_CANCELLED mirrors the binary's own exit code for "the user declined
# a confirmation". It must stay distinct from both success and failure, or a
# caller cannot tell a refusal from a completed uninstall.
readonly EXIT_USER_CANCELLED=3

DATA_DIR="${CONDUIT_DATA_DIR:-${HOME}/.conduit}"
# Tracks whether the user named a data directory or we defaulted to one. The
# distinction decides whether --data-dir is forwarded to the binary: forwarding
# a default would override the user's configured data_dir.
DATA_DIR_EXPLICIT=false
PREFIX=""
REMOVE_DATA=false
FORCE=false
DRY_RUN=false
# Skip delegation entirely and remove what is on disk. The escape hatch for a
# binary that runs but refuses, which is otherwise a blocking error by design.
MANUAL=false

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
                    or $CONDUIT_DATA_DIR). Must be an absolute path.
    --prefix DIR    Remove the install at DIR instead of searching the usual
                    locations. Use this if you installed with
                    `install.sh --prefix DIR`.
    --manual        Do not delegate to the conduit binary; remove what is on
                    disk directly. Use this when the binary runs but refuses.
                    Less thorough: MCP entries are reported rather than edited.
    --force         Skip confirmation prompts.
    --dry-run       Show what would be removed and change nothing.
    -h, --help      Show this help.

WHAT IS REMOVED
    - the conduit binary and any symlinks to it
    - Conduit's MCP entries in Claude Code, Cursor and VS Code
    - the PATH block install.sh added to your shell profile, identified by its
      "# Conduit" marker comment. A PATH line you wrote yourself is never
      touched, even if it names the same directory.
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
        --manual)      MANUAL=true; shift ;;
        --data-dir)
            [[ $# -ge 2 ]] || die "--data-dir requires a directory"
            DATA_DIR="$2"; DATA_DIR_EXPLICIT=true; shift 2 ;;
        --prefix)
            [[ $# -ge 2 ]] || die "--prefix requires a directory"
            [[ -n "${2// /}" ]] || die "--prefix requires a directory (got an empty value)"
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

# The data directory is shared between installs and lives outside every prefix,
# so --prefix and --remove-data ask for contradictory things. Refusing is the
# only safe reading: the alternative is deleting a knowledge base on the
# strength of a guess about which half the user meant.
if [[ -n "$PREFIX" && "$REMOVE_DATA" == true ]]; then
    die "--prefix and --remove-data are mutually exclusive.
  --prefix removes one install; --remove-data deletes the shared data directory.
  Run them separately if you mean both."
fi

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

# canonicalize_path resolves a path the way a deletion will see it.
#
# Every interesting way past a deny list is a spelling difference rather than a
# different directory: "~/", "//", "/.", "/Users/" and "x/.." all name places no
# guard should accept, and a string comparison waves every one of them through.
# Trailing separators are not exotic either -- tab completion appends one, so
# "$HOME/" reaching the check is the normal case.
#
# Done lexically, in pure bash, because `realpath -m` is not available on stock
# macOS and the path frequently does not exist yet.
canonicalize_path() {
    local p="$1"

    case "$p" in
        "~")   p="$HOME" ;;
        "~/"*) p="${HOME}/${p#\~/}" ;;
    esac

    [[ "$p" == /* ]] || p="$(pwd)/$p"

    # read -ra, not `for part in $p`. Unquoted word splitting also performs
    # pathname expansion, so a directory legitimately containing * or ? was
    # rewritten into whatever happened to match it in the current working
    # directory -- turning the guard's input into something that depends on
    # where the script was run from.
    local parts=() out=() part
    IFS='/' read -ra parts <<< "$p"

    for part in ${parts[@]+"${parts[@]}"}; do
        case "$part" in
            ''|.) ;;
            ..) [[ ${#out[@]} -gt 0 ]] && unset "out[$(( ${#out[@]} - 1 ))]" ;;
            *)  out+=("$part") ;;
        esac
    done

    local result=""
    for part in ${out[@]+"${out[@]}"}; do
        result="${result}/${part}"
    done
    printf '%s' "${result:-/}"
}

# path_identity prints a path's device:inode, or nothing if it does not exist.
#
# This is what makes the deny list mean anything on a case-insensitive
# filesystem. APFS is case-insensitive by default, so "/USERS/amlan" opens
# exactly the same directory as "/Users/amlan" while comparing unequal to every
# entry in the list. Unicode does the same trick: an accented home directory can
# be spelled NFC or NFD, both accepted by the filesystem, neither matching the
# other byte for byte. Device and inode are what the kernel itself uses to
# decide whether two names are one directory, and they cannot be spelled around.
path_identity() {
    stat -f '%d:%i' "$1" 2>/dev/null || stat -c '%d:%i' "$1" 2>/dev/null || printf ''
}

# protected_dirs lists every path that must never be a Conduit data directory.
#
# The mount points are here because an external disk, a network share or a
# container bind mount is somebody's entire filesystem, and "--data-dir
# /Volumes" differs from a real one by a single missing component.
protected_dirs() {
    printf '%s\n' / /usr /etc /var /opt /tmp /bin /sbin /home /Users /root \
                  /System /Library /Applications /private /dev /proc /boot \
                  /Volumes /mnt /media /net /srv

    local home_canonical
    home_canonical="$(canonicalize_path "$HOME")"
    printf '%s\n' "$home_canonical"

    # A non-standard home such as /export/people/amlan has a parent worth
    # protecting that /Users and /home do not cover.
    local parent="${home_canonical%/*}"
    if [[ -n "$parent" && "$parent" != "$home_canonical" ]]; then
        printf '%s\n' "$parent"
    fi
}

# assert_safe_data_dir refuses a --data-dir that --remove-data would turn into a
# catastrophe, and echoes the canonical form of one that is acceptable.
#
# The confirmation prompt is not a substitute for this. It asks about deleting
# "the data directory", and a user who has just fat-fingered a path has no
# reason to read that as "your entire home".
assert_safe_data_dir() {
    local dir="$1"
    [[ -n "${dir// /}" ]] || die "--data-dir cannot be empty"

    # A relative path resolves against whatever directory the script happened to
    # be run from, which is not something to guess at when the next step is a
    # recursive delete. Canonicalisation would silently make one absolute, so
    # the refusal has to come first.
    if [[ "$DATA_DIR_EXPLICIT" == true && "$dir" != /* && "$dir" != "~"* ]]; then
        die "--data-dir must be an absolute path (got: '$dir')"
    fi

    local canonical
    canonical="$(canonicalize_path "$dir")"

    local canonical_id
    canonical_id="$(path_identity "$canonical")"

    local protected protected_id
    while IFS= read -r protected; do
        [[ -n "$protected" ]] || continue

        # Lexical first: the only check available for a protected path that does
        # not exist on this machine (/mnt on a Mac, /Volumes on Linux).
        if [[ "$canonical" == "$protected" ]]; then
            die "refusing to treat $canonical (resolved from '$dir') as a Conduit data directory"
        fi

        # Then by identity, which catches every spelling the filesystem accepts
        # and the string comparison does not.
        if [[ -n "$canonical_id" ]]; then
            protected_id="$(path_identity "$protected")"
            if [[ -n "$protected_id" && "$canonical_id" == "$protected_id" ]]; then
                die "refusing to treat $canonical as a Conduit data directory (it is $protected under a different spelling)"
            fi
        fi
    done < <(protected_dirs)

    # A symlinked data directory is refused rather than followed, because the
    # two obvious behaviours disagree and both lose: on macOS `rm -rf dir/`
    # empties the TARGET and keeps the link, while the binary's os.RemoveAll
    # removes the link and keeps the data. Refusing is the only answer that is
    # the same in the script and in the binary.
    if [[ -L "$canonical" ]]; then
        die "$canonical is a symlink to $(readlink "$canonical").
  Removing it would either delete the link and keep your data, or delete the
  data and keep the link, depending on the tool. Re-run against the resolved
  path if you mean to delete the data."
    fi

    printf '%s' "$canonical"
}

DATA_DIR="$(assert_safe_data_dir "$DATA_DIR")"

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
    # `read` returns non-zero on EOF (Ctrl-D). Left bare, that aborts the whole
    # script under `set -e` with no message, which looks like a crash rather
    # than the cancellation it is.
    if ! read -r answer; then
        printf '\n'
        return 1
    fi
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

# binary_supports reports whether a binary's help text advertises a flag.
#
# Version 1 binaries have no --prefix and no --data-dir. Passing an unknown flag
# to them is not a soft failure: cobra exits non-zero without doing anything,
# which used to be indistinguishable from "the uninstall failed" and dropped the
# whole run into the manual path -- the dangerous one -- on every v1 machine.
binary_supports() {
    local conduit="$1" flag="$2" help_text

    # Captured, not piped into `grep -q`.
    #
    # grep -q exits at the first match, which SIGPIPEs the still-writing binary;
    # under `pipefail` the pipeline then reports 141 and the probe concludes the
    # flag is unsupported. Whether that happened depended on which process was
    # scheduled first, so it made the probe intermittently claim every binary
    # was a v1 one.
    help_text="$("$conduit" uninstall --help 2>&1 || true)"

    case "$help_text" in
        *"$flag"*) return 0 ;;
        *)         return 1 ;;
    esac
}

delegate_to_binary() {
    local conduit="$1"
    local -a args=()

    # --data-dir is forwarded ONLY when the user actually passed it.
    #
    # Forwarding it unconditionally is the mirror image of not forwarding it at
    # all: the script's default (~/.conduit) would override a data_dir the user
    # had configured in conduit.yaml, and the binary would then act on a
    # directory its own configuration says is the wrong one.
    if [[ "$DATA_DIR_EXPLICIT" == true ]]; then
        if binary_supports "$conduit" "--data-dir"; then
            args+=("--data-dir" "$DATA_DIR")
        else
            die "$conduit does not support --data-dir (it looks like a Conduit 1.x binary).
  Refusing to delegate: it would act on its own default data directory instead
  of the one you named, which is how a wrapper deletes the wrong thing.
  Remove that binary first, or re-run without --data-dir."
        fi
    fi

    args+=("uninstall")

    # --force is forwarded, never assumed. Hardcoding it here meant that
    # `uninstall.sh --remove-data` -- with no --force anywhere on the command
    # line -- delegated to `conduit uninstall --force --all`, which skipped the
    # binary's own "type UNINSTALL to confirm" gate. The script's confirmation
    # then ran afterwards, against a data directory that was already deleted.
    # Both prompts existed; neither one fired.
    [[ "$FORCE" == true ]] && args+=("--force")

    if [[ "$REMOVE_DATA" == true ]]; then
        args+=("--all")
    else
        args+=("--keep-data")
    fi
    [[ "$DRY_RUN" == true ]] && args+=("--dry-run")

    # Without this, `uninstall.sh --prefix /tmp/x` would delegate to a binary
    # that then removes the copy in ~/.local/bin: the exact install the flag
    # promised to leave alone. A binary too old to understand --prefix cannot
    # honour that promise, so it is not asked to try.
    if [[ -n "$PREFIX" ]]; then
        if binary_supports "$conduit" "--prefix"; then
            args+=("--prefix" "$PREFIX")
        else
            die "$conduit does not support --prefix (it looks like a Conduit 1.x binary).
  Refusing to delegate: it would remove the install in ~/.local/bin rather than
  the one in $PREFIX. Delete $PREFIX/$BINARY by hand instead."
        fi
    fi

    info "Delegating to ${conduit} uninstall"

    local rc=0
    "$conduit" "${args[@]}" || rc=$?

    case "$rc" in
        0) return 0 ;;

        "$EXIT_USER_CANCELLED")
            # The user declined the binary's confirmation. Carrying on would
            # delete the binary they just refused to remove and then ask them
            # the same question again, which is how a "no" became a "yes".
            printf '\n%s\n' "${C_YELLOW}Cancelled at your request. Nothing further was removed.${C_RESET}"
            exit "$EXIT_USER_CANCELLED"
            ;;

        126|127)
            # 126 = found but not executable, 127 = not found. In both cases the
            # binary never ran, so it expressed no opinion about anything and
            # there is nothing to respect. This is the case the manual path
            # exists for: a wrong-architecture download, a missing shared
            # library, a half-finished install. Blocking here would leave the
            # user with a broken binary they cannot remove using the tool whose
            # entire job is removing it.
            warn "${conduit} could not be executed (exit $rc: wrong architecture, missing library, or not a valid executable)."
            warn "  It never ran, so nothing it would have protected is at risk."
            warn "  Falling back to manual removal, which is less thorough:"
            warn "    - MCP client entries are reported, not edited"
            warn "    - only shell profile lines carrying Conduit's own marker are removed"
            return 1
            ;;

        *)
            # The binary ran and decided to stop. That is a judgement about this
            # machine, and the manual path -- which cannot safely edit JSON
            # configs and knows less about what was installed -- is not entitled
            # to overrule it. Silently downgrading here is how a wrapper does
            # damage the tool it wrapped had refused to do.
            die "'${conduit} uninstall' failed (exit $rc).
  The binary ran and stopped, so this is its decision, not a broken install.
  Refusing to fall back to manual removal: it is the less careful path.
  Fix whatever it reported, or pass --manual to remove the files anyway.
  Re-run with --dry-run to see what would have been removed."
            ;;
    esac
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

# CONDUIT_PATH_MARKER is the comment an installer writes above the PATH line it
# adds. It is the ONLY signature used, and it must stay identical to
# conduitPathMarker in internal/setup/setup.go.
#
# The previous pattern also matched any `export PATH` line mentioning
# .local/bin. That is not a Conduit signature: pipx, uv, poetry and pip --user
# all put that directory on PATH, and a dry run against a real machine flagged a
# line Conduit had never written. Deleting it would have removed other tools
# from the user's PATH, surfacing much later as "command not found" for
# something unrelated to Conduit.
#
# Being strict means a hand-edited profile with no marker is left alone. That is
# the right way round: a stale PATH entry pointing at a directory that no longer
# exists is harmless, and a deleted one that other tools needed is not.
readonly CONDUIT_PATH_MARKER='^[[:space:]]*# Conduit'

# profile_has_marker reports whether a file carries Conduit's marker comment.
profile_has_marker() {
    grep -qE "$CONDUIT_PATH_MARKER" "$1" 2>/dev/null
}

# strip_conduit_path_block removes each marker line and the single line that
# follows it, writing the result to stdout.
#
# awk rather than grep -v: the block is two lines and only the first is
# identifiable, so a line-independent filter cannot express it.
strip_conduit_path_block() {
    awk '
        skip == 1 { skip = 0; next }
        /^[[:space:]]*# Conduit/ { skip = 1; next }
        { print }
    ' "$1"
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
        profile_has_marker "$file" || continue

        FOUND=$((FOUND + 1))

        if [[ "$DRY_RUN" == true ]]; then
            plan "Conduit PATH block in $file"
            grep -nE -A1 "$CONDUIT_PATH_MARKER" "$file" | sed 's/^/      /'
            continue
        fi

        # Never edit a profile in place without a copy: a broken .zshrc locks
        # somebody out of their own shell.
        local backup="${file}.conduit-uninstall.bak"
        cp -- "$file" "$backup" || { bad "could not back up $file"; continue; }

        local tmp
        tmp="$(mktemp)"

        # An awk failure must not be mistaken for "nothing matched". The old
        # code ended in `|| true`, which swallowed every non-zero exit --
        # including a read error or a full disk -- and then moved the resulting
        # empty file over the user's profile.
        local rc=0
        strip_conduit_path_block "$file" > "$tmp" || rc=$?
        if [[ "$rc" -ne 0 ]]; then
            rm -f -- "$tmp"
            FAILED=$((FAILED + 1))
            bad "$file (could not be rewritten; left unchanged, backup: $backup)"
            continue
        fi

        # Preserve the original permissions. mktemp creates 0600, so moving it
        # into place unchanged would silently tighten a profile that was
        # deliberately group-readable.
        local mode
        mode="$(file_mode "$file")"
        chmod "$mode" "$tmp" 2>/dev/null || true

        if mv -f -- "$tmp" "$file"; then
            REMOVED=$((REMOVED + 1))
            ok "Conduit PATH block in $file  (backup: $backup)"
        else
            rm -f -- "$tmp"
            FAILED=$((FAILED + 1))
            bad "$file"
        fi
    done
}

# file_mode prints a file's permission bits as an octal string.
file_mode() {
    local m
    # BSD stat (macOS) and GNU stat (Linux) disagree on flags; try both.
    m="$(stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1" 2>/dev/null || true)"
    printf '%s' "${m:-644}"
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

# preview_fallback shows what the manual path would do, without doing it.
#
# It runs the same reporting functions the fallback uses, with DRY_RUN forced
# on, so the preview cannot drift away from the behaviour it describes. The
# counters are restored afterwards: this is a hypothetical, and it must not
# inflate the summary for the path that is actually running.
preview_fallback() {
    local saved_found="$FOUND" saved_removed="$REMOVED" saved_failed="$FAILED"
    local saved_dry="$DRY_RUN"

    DRY_RUN=true
    remove_shell_path_lines
    remove_mcp_entries_manually
    DRY_RUN="$saved_dry"

    FOUND="$saved_found"
    REMOVED="$saved_removed"
    FAILED="$saved_failed"
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
    if [[ "$MANUAL" == true ]]; then
        info "Skipping delegation (--manual); removing what is on disk"
        warn "Manual removal is the less thorough path: MCP entries are reported"
        warn "  rather than edited, and only marked shell profile lines are removed."
    elif conduit="$(find_binary)"; then
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
    elif [[ "$DRY_RUN" == true ]]; then
        # A dry run that only previews the delegated path is not a preview of
        # the run the user is about to make. If the binary is missing or broken
        # on the day, the real run takes the fallback -- which touches shell
        # profiles and reports MCP entries -- and none of that appeared here.
        printf '\n'
        info "If the binary were unavailable, the fallback would also:"
        preview_fallback
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
