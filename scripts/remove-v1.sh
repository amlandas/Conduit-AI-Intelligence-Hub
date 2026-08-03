#!/usr/bin/env bash
#
# remove-v1.sh - remove the Conduit 1.x stack from this machine.
#
# Conduit 2.0 is a single binary. Version 1 was a background daemon plus two
# containerised databases, registered with launchd or systemd so it started at
# login. Installing v2 does not remove any of that: the daemon keeps starting,
# the containers keep holding ports 6333/6334/6379, and nothing tells the user
# why their machine is still busy. This script is the teardown.
#
# WHAT IT WILL NOT DO
#
#   It never deletes your knowledge base, your configuration or your embedding
#   models. Those belong to v2 as much as they did to v1. Only --purge-data
#   removes anything under the data directory, and even then only the two
#   container storage directories that v2 has no use for.
#
#   To remove Conduit entirely, data included, use `conduit uninstall --all`.
#
# SAFETY
#
#   Dry run is the default. Nothing is removed unless you pass --yes.
#
# Usage:
#   ./remove-v1.sh                 # report what is present, change nothing
#   ./remove-v1.sh --yes           # remove the v1 stack, keep all data
#   ./remove-v1.sh --yes --purge-data   # also drop the Qdrant/FalkorDB stores
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------

DRY_RUN=true
PURGE_DATA=false

DATA_DIR="${CONDUIT_DATA_DIR:-${HOME}/.conduit}"

# Containers the v1 installer created.
readonly V1_CONTAINERS=(conduit-qdrant conduit-falkordb)

# launchd labels. dev.simpleflo.conduit is what the last v1 installer wrote;
# com.simpleflo.conduit predates it and is still present on early installs.
readonly V1_LAUNCHD_LABELS=(dev.simpleflo.conduit com.simpleflo.conduit)

# systemd user units.
readonly V1_SYSTEMD_UNITS=(conduit.service conduit-daemon.service)

# Counters, reported at the end so the user knows whether anything happened.
FOUND=0
REMOVED=0
FAILED=0
SKIPPED_RUNTIME=0

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

if [[ -t 1 ]]; then
    C_RESET=$'\033[0m'; C_RED=$'\033[0;31m'; C_GREEN=$'\033[0;32m'
    C_YELLOW=$'\033[0;33m'; C_BLUE=$'\033[0;34m'; C_BOLD=$'\033[1m'
else
    C_RESET=''; C_RED=''; C_GREEN=''; C_YELLOW=''; C_BLUE=''; C_BOLD=''
fi

info()  { printf '%s\n' "${C_BLUE}==>${C_RESET} $*"; }
ok()    { printf '%s\n' "  ${C_GREEN}removed${C_RESET}  $*"; }
plan()  { printf '%s\n' "  ${C_YELLOW}would remove${C_RESET}  $*"; }
warn()  { printf '%s\n' "  ${C_YELLOW}warning${C_RESET}  $*" >&2; }
fail()  { printf '%s\n' "  ${C_RED}failed${C_RESET}  $*" >&2; }
found() { printf '%s\n' "  ${C_BOLD}found${C_RESET}  $*"; }

die() { printf '%s\n' "${C_RED}error:${C_RESET} $*" >&2; exit 1; }

usage() {
    cat <<'EOF'
remove-v1.sh - remove the Conduit 1.x stack (daemon, containers, services)

USAGE
    ./remove-v1.sh [OPTIONS]

OPTIONS
    --dry-run       Report what would be removed and change nothing.
                    THIS IS THE DEFAULT. Removal requires --yes.
    --yes           Actually perform the removal.
    --purge-data    Also delete the v1 container storage directories
                    (<data-dir>/qdrant and <data-dir>/falkordb). Without this
                    flag no file under the data directory is touched.
    --data-dir DIR  Conduit data directory (default: ~/.conduit,
                    or $CONDUIT_DATA_DIR).
    -h, --help      Show this help.

WHAT IS REMOVED
    - conduit-qdrant and conduit-falkordb containers (docker and podman)
    - launchd agents: dev.simpleflo.conduit, com.simpleflo.conduit
    - systemd user units: conduit.service, conduit-daemon.service
    - the conduit-daemon binary and its symlinks
    - <data-dir>/daemon.log

WHAT IS NEVER REMOVED
    - the knowledge base (<data-dir>/conduit.db)
    - the configuration (<data-dir>/conduit.yaml)
    - downloaded embedding models (<data-dir>/models)
    - the v2 `conduit` binary
    - Docker, Podman, Ollama or any other shared tool

    Only --purge-data removes anything under the data directory, and only the
    two container storage directories. To remove Conduit and its data
    completely, use `conduit uninstall --all`.

EXAMPLES
    ./remove-v1.sh                      # see what is there
    ./remove-v1.sh --yes                # tear it down, keep all data
    ./remove-v1.sh --yes --purge-data   # also drop the old vector/graph stores
EOF
}

# ---------------------------------------------------------------------------
# Arguments
# ---------------------------------------------------------------------------

while [[ $# -gt 0 ]]; do
    case "$1" in
        --dry-run)    DRY_RUN=true; shift ;;
        --yes|-y)     DRY_RUN=false; shift ;;
        --purge-data) PURGE_DATA=true; shift ;;
        --data-dir)
            [[ $# -ge 2 ]] || die "--data-dir requires a directory"
            DATA_DIR="$2"; shift 2 ;;
        -h|--help)    usage; exit 0 ;;
        *)            die "unknown option: $1 (try --help)" ;;
    esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

have() { command -v "$1" >/dev/null 2>&1; }

# remove_path deletes a file or directory, honouring dry-run.
remove_path() {
    local path="$1" label="${2:-$1}"

    [[ -e "$path" || -L "$path" ]] || return 0
    FOUND=$((FOUND + 1))

    if [[ "$DRY_RUN" == true ]]; then
        plan "$label"
        return 0
    fi

    if rm -rf -- "$path"; then
        REMOVED=$((REMOVED + 1))
        ok "$label"
    else
        FAILED=$((FAILED + 1))
        fail "$label"
    fi
}

# ---------------------------------------------------------------------------
# Containers
# ---------------------------------------------------------------------------

# container_runtime_ready reports whether a runtime can actually be talked to.
# A runtime that is installed but not running is not a failure: it means we
# cannot inspect it, which the user needs to be told rather than have guessed.
container_runtime_ready() {
    local runtime="$1"
    "$runtime" ps >/dev/null 2>&1
}

remove_containers_for() {
    local runtime="$1"

    have "$runtime" || return 0

    if ! container_runtime_ready "$runtime"; then
        SKIPPED_RUNTIME=$((SKIPPED_RUNTIME + 1))
        warn "$runtime is installed but not running; its containers cannot be inspected."
        warn "  Start it and re-run this script, or remove them by hand:"
        warn "    $runtime rm -f ${V1_CONTAINERS[*]}"
        return 0
    fi

    local existing
    existing="$("$runtime" ps -a --format '{{.Names}}' 2>/dev/null || true)"

    local name
    for name in "${V1_CONTAINERS[@]}"; do
        # Exact-line match: a container called conduit-qdrant-backup is not ours.
        printf '%s\n' "$existing" | grep -qx "$name" || continue

        FOUND=$((FOUND + 1))
        local state
        state="$("$runtime" inspect -f '{{.State.Status}}' "$name" 2>/dev/null || echo unknown)"

        if [[ "$DRY_RUN" == true ]]; then
            plan "$runtime container $name (state: $state)"
            continue
        fi

        if "$runtime" rm -f "$name" >/dev/null 2>&1; then
            REMOVED=$((REMOVED + 1))
            ok "$runtime container $name"
        else
            FAILED=$((FAILED + 1))
            fail "$runtime container $name"
        fi
    done
}

remove_containers() {
    info "Container runtimes"

    if ! have docker && ! have podman; then
        printf '%s\n' "  no docker or podman on this machine; nothing to inspect"
        return 0
    fi

    remove_containers_for docker
    remove_containers_for podman
}

# ---------------------------------------------------------------------------
# Services
# ---------------------------------------------------------------------------

remove_launchd() {
    [[ "$(uname -s)" == "Darwin" ]] || return 0
    have launchctl || return 0

    info "launchd agents"

    local label plist loaded
    for label in "${V1_LAUNCHD_LABELS[@]}"; do
        plist="${HOME}/Library/LaunchAgents/${label}.plist"
        loaded=false
        if launchctl list 2>/dev/null | grep -q "[[:space:]]${label}\$"; then
            loaded=true
        fi

        if [[ ! -e "$plist" && "$loaded" == false ]]; then
            continue
        fi

        FOUND=$((FOUND + 1))

        if [[ "$DRY_RUN" == true ]]; then
            if [[ "$loaded" == true ]]; then
                plan "launchd agent $label (currently loaded)"
            else
                plan "launchd agent $label (plist present, not loaded)"
            fi
            [[ -e "$plist" ]] && plan "  $plist"
            continue
        fi

        # Stop and unload before deleting the plist, or launchd keeps the job.
        launchctl stop "$label" >/dev/null 2>&1 || true
        launchctl unload "$plist" >/dev/null 2>&1 || true
        launchctl remove "$label" >/dev/null 2>&1 || true

        if [[ -e "$plist" ]]; then
            if rm -f -- "$plist"; then
                REMOVED=$((REMOVED + 1))
                ok "launchd agent $label ($plist)"
            else
                FAILED=$((FAILED + 1))
                fail "launchd agent $label ($plist)"
            fi
        else
            REMOVED=$((REMOVED + 1))
            ok "launchd agent $label (unloaded; no plist on disk)"
        fi
    done
}

remove_systemd() {
    [[ "$(uname -s)" == "Linux" ]] || return 0
    have systemctl || return 0

    info "systemd user units"

    local unit path
    for unit in "${V1_SYSTEMD_UNITS[@]}"; do
        path="${HOME}/.config/systemd/user/${unit}"
        local active=false
        if systemctl --user is-active --quiet "$unit" 2>/dev/null; then
            active=true
        fi

        if [[ ! -e "$path" && "$active" == false ]]; then
            continue
        fi

        FOUND=$((FOUND + 1))

        if [[ "$DRY_RUN" == true ]]; then
            plan "systemd user unit $unit (active: $active)"
            [[ -e "$path" ]] && plan "  $path"
            continue
        fi

        systemctl --user stop "$unit" >/dev/null 2>&1 || true
        systemctl --user disable "$unit" >/dev/null 2>&1 || true

        if [[ -e "$path" ]] && ! rm -f -- "$path"; then
            FAILED=$((FAILED + 1))
            fail "systemd user unit $unit ($path)"
            continue
        fi

        REMOVED=$((REMOVED + 1))
        ok "systemd user unit $unit"
    done

    systemctl --user daemon-reload >/dev/null 2>&1 || true
}

# ---------------------------------------------------------------------------
# Binaries
# ---------------------------------------------------------------------------

# stop_running_daemon kills a conduit-daemon still in memory. Deleting the
# binary from underneath a running process leaves it running with no file to
# point at, which is worse than either state on its own.
stop_running_daemon() {
    have pgrep || return 0

    local pids
    pids="$(pgrep -x conduit-daemon 2>/dev/null || true)"
    [[ -n "$pids" ]] || return 0

    FOUND=$((FOUND + 1))

    if [[ "$DRY_RUN" == true ]]; then
        plan "running conduit-daemon process(es): $(printf '%s' "$pids" | tr '\n' ' ')"
        return 0
    fi

    # SIGTERM first; the daemon has a shutdown path worth letting it take.
    printf '%s\n' "$pids" | while read -r pid; do
        [[ -n "$pid" ]] && kill "$pid" 2>/dev/null || true
    done
    sleep 2
    printf '%s\n' "$pids" | while read -r pid; do
        [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
    done

    REMOVED=$((REMOVED + 1))
    ok "stopped conduit-daemon"
}

remove_binaries() {
    info "v1 binaries"

    stop_running_daemon

    # conduit-daemon only. The v2 `conduit` binary lives at the same paths and
    # is replaced by install.sh, so removing it here would break a machine
    # mid-transition for no benefit.
    local candidates=(
        "${HOME}/.local/bin/conduit-daemon"
        "${HOME}/bin/conduit-daemon"
        "/usr/local/bin/conduit-daemon"
        "/opt/homebrew/bin/conduit-daemon"
    )

    local path
    for path in "${candidates[@]}"; do
        [[ -e "$path" || -L "$path" ]] || continue

        # A system path may need privileges we do not have and will not take.
        if [[ ! -w "$(dirname "$path")" ]]; then
            FOUND=$((FOUND + 1))
            warn "$path is not writable by this user; remove it with:"
            warn "    sudo rm -f $path"
            continue
        fi

        remove_path "$path" "$path"
    done
}

# ---------------------------------------------------------------------------
# Data
# ---------------------------------------------------------------------------

remove_v1_logs() {
    info "v1 logs"
    # The daemon log describes a daemon that no longer exists. It is not user
    # content and carries no data, so it goes without --purge-data.
    remove_path "${DATA_DIR}/daemon.log" "${DATA_DIR}/daemon.log"
}

report_or_purge_data() {
    info "v1 data stores"

    local stores=("${DATA_DIR}/qdrant" "${DATA_DIR}/falkordb")
    local present=()
    local path
    for path in "${stores[@]}"; do
        [[ -d "$path" ]] && present+=("$path")
    done

    if [[ ${#present[@]} -eq 0 ]]; then
        printf '%s\n' "  none present"
        return 0
    fi

    if [[ "$PURGE_DATA" != true ]]; then
        for path in "${present[@]}"; do
            FOUND=$((FOUND + 1))
            found "$path  (kept; pass --purge-data to remove)"
        done
        printf '%s\n' "  v2 stores vectors and the graph in SQLite, so these are dead weight."
        return 0
    fi

    for path in "${present[@]}"; do
        remove_path "$path" "$path  (--purge-data)"
    done
}

# report_protected_data names what was deliberately left alone, so the user
# does not go looking for it or assume it was destroyed.
report_protected_data() {
    info "Left untouched"

    local protected=(
        "${DATA_DIR}/conduit.db:knowledge base"
        "${DATA_DIR}/conduit.yaml:configuration"
        "${DATA_DIR}/models:embedding models"
        "${DATA_DIR}/backups:backups"
    )

    local shown=false entry path label
    for entry in "${protected[@]}"; do
        path="${entry%%:*}"
        label="${entry##*:}"
        if [[ -e "$path" ]]; then
            printf '%s\n' "  ${C_GREEN}kept${C_RESET}  $path  ($label)"
            shown=true
        fi
    done

    [[ "$shown" == false ]] && printf '%s\n' "  no Conduit data on this machine"
    return 0
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
    printf '%s\n' "${C_BOLD}Conduit v1 teardown${C_RESET}"
    printf '%s\n' "Data directory: ${DATA_DIR}"

    if [[ "$DRY_RUN" == true ]]; then
        printf '%s\n\n' "${C_YELLOW}DRY RUN - nothing will be removed. Re-run with --yes to apply.${C_RESET}"
    else
        printf '%s\n\n' "${C_RED}LIVE RUN - the items below will be removed.${C_RESET}"
    fi

    remove_containers
    remove_launchd
    remove_systemd
    remove_binaries
    remove_v1_logs
    report_or_purge_data
    report_protected_data

    printf '\n%s\n' "${C_BOLD}Summary${C_RESET}"
    if [[ "$FOUND" -eq 0 ]]; then
        printf '%s\n' "  No Conduit v1 components found. This machine is already clean."
        return 0
    fi

    if [[ "$DRY_RUN" == true ]]; then
        printf '%s\n' "  $FOUND v1 component(s) found, 0 removed (dry run)."
        printf '%s\n' "  Re-run with --yes to remove them."
    else
        printf '%s\n' "  $FOUND found, $REMOVED removed, $FAILED failed."
    fi

    if [[ "$SKIPPED_RUNTIME" -gt 0 ]]; then
        printf '%s\n' "  ${C_YELLOW}$SKIPPED_RUNTIME container runtime(s) could not be inspected (not running).${C_RESET}"
    fi

    if [[ "$PURGE_DATA" != true ]]; then
        printf '%s\n' "  No user data was touched."
    fi

    printf '\n%s\n' "Next: install Conduit 2.0 with scripts/install.sh"

    [[ "$FAILED" -gt 0 ]] && return 1
    return 0
}

main "$@"
