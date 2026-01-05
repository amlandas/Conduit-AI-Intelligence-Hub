#!/bin/bash
#
# Conduit Uninstallation Script
#
# This script safely removes Conduit components:
# 1. Stops and removes daemon service
# 2. Removes binaries from PATH
# 3. Optionally removes data directory
# 4. Cleans up shell configuration
#
# NOTE: Dependencies (Ollama, Podman/Docker, containers) are NOT removed.
#       These may be shared with other projects. See manual cleanup below.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/uninstall.sh | bash
#
# Or with options:
#   bash uninstall.sh --force --remove-data
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
INSTALL_DIR="${HOME}/.local/bin"
CONDUIT_HOME="${HOME}/.conduit"
FORCE=false
REMOVE_DATA=false
ERRORS=()

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')

# Print functions
print_banner() {
    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║              Conduit Uninstallation                          ║"
    echo "║       Remove Conduit and its dependencies                    ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo -e "${NC}"
}

info() {
    echo -e "${BLUE}ℹ${NC} $1"
}

success() {
    echo -e "${GREEN}✓${NC} $1"
}

warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

error() {
    echo -e "${RED}✗${NC} $1"
    ERRORS+=("$1")
}

# Prompt for confirmation
confirm() {
    local prompt="$1"
    local default="${2:-n}"

    if [[ "$FORCE" == "true" ]]; then
        return 0
    fi

    if [[ "$default" == "y" ]]; then
        prompt="$prompt [Y/n]: "
    else
        prompt="$prompt [y/N]: "
    fi

    # Use /dev/tty to read from terminal instead of stdin (fixes curl | bash)
    read -r -p "$prompt" response </dev/tty
    response=${response:-$default}

    [[ "$response" =~ ^[Yy]$ ]]
}

# Check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --force)
                FORCE=true
                shift
                ;;
            --remove-data)
                REMOVE_DATA=true
                shift
                ;;
            --install-dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            --conduit-home)
                CONDUIT_HOME="$2"
                shift 2
                ;;
            --help)
                show_help
                exit 0
                ;;
            *)
                error "Unknown option: $1"
                show_help
                exit 1
                ;;
        esac
    done
}

show_help() {
    echo "Conduit Uninstallation Script"
    echo ""
    echo "Usage: uninstall.sh [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --force              Skip confirmation prompts"
    echo "  --remove-data        Also remove data directory (~/.conduit)"
    echo "  --install-dir DIR    Binary installation directory (default: ~/.local/bin)"
    echo "  --conduit-home DIR   Conduit data directory (default: ~/.conduit)"
    echo "  --help               Show this help message"
    echo ""
    echo "NOTE: Dependencies (Ollama, Podman, containers) are NOT removed."
    echo "      To remove manually:"
    echo "        podman stop qdrant falkordb && podman rm qdrant falkordb"
    echo "        rm -rf ~/.ollama && brew uninstall ollama"
    echo "        podman machine stop && podman machine rm && brew uninstall podman"
}

# Stop Conduit daemon
stop_daemon() {
    echo ""
    echo "Step 1: Stop Conduit Daemon"
    echo "──────────────────────────────────────────────────────────────"

    # Check if daemon is running
    if curl -s --unix-socket "$CONDUIT_HOME/conduit.sock" http://localhost/api/v1/health >/dev/null 2>&1; then
        warn "Conduit daemon is running"

        if ! confirm "Stop the daemon?"; then
            warn "Daemon left running. You may need to stop it manually later."
            return 0
        fi

        # Try to stop via service
        case $OS in
            darwin)
                if launchctl list | grep -q "dev.simpleflo.conduit"; then
                    info "Stopping launchd service..."
                    launchctl stop dev.simpleflo.conduit 2>/dev/null || true
                    success "Daemon stopped"
                fi
                ;;
            linux)
                if systemctl --user is-active conduit >/dev/null 2>&1; then
                    info "Stopping systemd service..."
                    systemctl --user stop conduit || error "Failed to stop systemd service"
                    success "Daemon stopped"
                fi
                ;;
        esac

        # If still running, kill the process
        sleep 1
        if curl -s --unix-socket "$CONDUIT_HOME/conduit.sock" http://localhost/api/v1/health >/dev/null 2>&1; then
            warn "Daemon still running. Attempting to terminate process..."
            pkill -f "conduit-daemon" || error "Failed to kill daemon process"
            sleep 1
        fi

        # Final check
        if ! curl -s --unix-socket "$CONDUIT_HOME/conduit.sock" http://localhost/api/v1/health >/dev/null 2>&1; then
            success "Daemon stopped successfully"
        else
            error "Failed to stop daemon. Please stop manually: pkill conduit-daemon"
        fi
    else
        info "Daemon is not running"
    fi
}

# Remove daemon service
remove_service() {
    echo ""
    echo "Step 2: Remove Daemon Service"
    echo "──────────────────────────────────────────────────────────────"

    case $OS in
        darwin)
            local PLIST_PATH="$HOME/Library/LaunchAgents/dev.simpleflo.conduit.plist"
            if [[ -f "$PLIST_PATH" ]]; then
                if confirm "Remove launchd service?"; then
                    info "Unloading service..."
                    launchctl unload "$PLIST_PATH" 2>/dev/null || true
                    rm -f "$PLIST_PATH" || error "Failed to remove $PLIST_PATH"
                    success "Service removed"
                else
                    warn "Service configuration left in place"
                fi
            else
                info "No launchd service found"
            fi
            ;;
        linux)
            local SERVICE_PATH="$HOME/.config/systemd/user/conduit.service"
            if [[ -f "$SERVICE_PATH" ]]; then
                if confirm "Remove systemd service?"; then
                    info "Disabling and removing service..."
                    systemctl --user disable conduit 2>/dev/null || true
                    systemctl --user stop conduit 2>/dev/null || true
                    rm -f "$SERVICE_PATH" || error "Failed to remove $SERVICE_PATH"
                    systemctl --user daemon-reload || true
                    success "Service removed"
                else
                    warn "Service configuration left in place"
                fi
            else
                info "No systemd service found"
            fi
            ;;
    esac
}

# Remove binaries
remove_binaries() {
    echo ""
    echo "Step 3: Remove Binaries"
    echo "──────────────────────────────────────────────────────────────"

    local binaries=("$INSTALL_DIR/conduit" "$INSTALL_DIR/conduit-daemon")
    local found=false

    for binary in "${binaries[@]}"; do
        if [[ -f "$binary" ]]; then
            found=true
            break
        fi
    done

    if [[ "$found" == "false" ]]; then
        info "No Conduit binaries found in $INSTALL_DIR"
        return 0
    fi

    if ! confirm "Remove Conduit binaries from $INSTALL_DIR?"; then
        warn "Binaries left in place"
        return 0
    fi

    for binary in "${binaries[@]}"; do
        if [[ -f "$binary" ]]; then
            rm -f "$binary" || error "Failed to remove $binary"
            success "Removed $(basename "$binary")"
        fi
    done
}

# Remove Conduit data directory
remove_data() {
    echo ""
    echo "Step 4: Remove Conduit Data"
    echo "──────────────────────────────────────────────────────────────"

    if [[ ! -d "$CONDUIT_HOME" ]]; then
        info "No data directory found at $CONDUIT_HOME"
        return 0
    fi

    local data_size=$(du -sh "$CONDUIT_HOME" 2>/dev/null | cut -f1 || echo "unknown")

    if [[ "$REMOVE_DATA" == "true" ]] || [[ "$FORCE" == "true" ]]; then
        if [[ "$FORCE" != "true" ]]; then
            warn "This will permanently delete all Conduit data ($data_size)!"
            read -r -p "Type 'DELETE' to confirm: " confirm_delete </dev/tty
            if [[ "$confirm_delete" != "DELETE" ]]; then
                warn "Deletion cancelled. Data preserved."
                return 0
            fi
        fi
        info "Removing data directory ($data_size)..."
        rm -rf "$CONDUIT_HOME" || error "Failed to remove $CONDUIT_HOME"
        success "Data directory removed"
    else
        info "Data directory preserved at $CONDUIT_HOME ($data_size)"
        echo "  Use --remove-data flag to remove data directory"
    fi
}

# Clean up shell configuration
cleanup_shell_config() {
    echo ""
    echo "Step 5: Clean Up Shell Configuration"
    echo "──────────────────────────────────────────────────────────────"

    local shell_configs=("$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.config/fish/config.fish")
    local found=false

    for config in "${shell_configs[@]}"; do
        if [[ -f "$config" ]] && grep -q "$INSTALL_DIR" "$config" 2>/dev/null; then
            found=true
            break
        fi
    done

    if [[ "$found" == "false" ]]; then
        info "No Conduit PATH entries found in shell configs"
        return 0
    fi

    if ! confirm "Remove Conduit PATH entries from shell configuration?"; then
        warn "Shell configuration left unchanged"
        return 0
    fi

    for config in "${shell_configs[@]}"; do
        if [[ -f "$config" ]]; then
            # Create backup
            cp "$config" "$config.conduit-backup" 2>/dev/null || true

            # Remove Conduit-related lines
            if grep -q "$INSTALL_DIR" "$config" 2>/dev/null; then
                # Remove the PATH export line and the "# Conduit" comment
                sed -i.bak "/# Conduit/d; /export PATH.*${INSTALL_DIR////\\/}/d" "$config" 2>/dev/null || {
                    # macOS sed requires empty string for -i
                    sed -i '' "/# Conduit/d; /export PATH.*${INSTALL_DIR////\\/}/d" "$config" 2>/dev/null || error "Failed to update $config"
                }
                rm -f "$config.bak" 2>/dev/null || true
                success "Cleaned up $(basename "$config")"
            fi
        fi
    done

    info "Backup copies saved with .conduit-backup extension"
}

# Print summary
print_summary() {
    echo ""
    echo -e "${GREEN}══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}               Uninstallation Complete!                        ${NC}"
    echo -e "${GREEN}══════════════════════════════════════════════════════════════${NC}"
    echo ""

    if [[ ${#ERRORS[@]} -gt 0 ]]; then
        echo -e "${YELLOW}Some errors occurred during uninstallation:${NC}"
        echo ""
        for err in "${ERRORS[@]}"; do
            echo -e "  ${RED}✗${NC} $err"
        done
        echo ""
        echo "You may need to manually clean up these components."
        echo ""
    else
        echo "Conduit has been successfully removed from your system."
        echo ""
    fi

    echo "Summary:"
    echo ""
    [[ ! -f "$INSTALL_DIR/conduit" ]] && echo -e "  ${GREEN}✓${NC} Binaries removed"
    if [[ ! -d "$CONDUIT_HOME" ]]; then
        echo -e "  ${GREEN}✓${NC} Data directory removed"
    else
        echo -e "  ${YELLOW}○${NC} Data directory preserved at $CONDUIT_HOME"
    fi
    echo ""

    echo "To remove dependencies manually (if no longer needed):"
    echo "  • Containers: podman stop qdrant falkordb && podman rm qdrant falkordb"
    echo "  • Ollama: rm -rf ~/.ollama && brew uninstall ollama"
    echo "  • Podman: podman machine stop && podman machine rm && brew uninstall podman"
    echo ""

    echo "You may want to:"
    echo "  - Restart your terminal to apply shell configuration changes"
    echo "  - Review backup files (*.conduit-backup) in your home directory"
    echo ""
    echo "Thank you for trying Conduit!"
    echo ""
}

# Delegate to CLI if available (ensures consistent behavior)
delegate_to_cli() {
    local cli_path=""

    # Find conduit CLI
    if command_exists conduit; then
        cli_path=$(command -v conduit)
    elif [[ -f "$INSTALL_DIR/conduit" ]]; then
        cli_path="$INSTALL_DIR/conduit"
    fi

    if [[ -z "$cli_path" ]]; then
        return 1  # CLI not found, use fallback
    fi

    info "Using Conduit CLI for uninstallation..."
    echo ""

    # Build CLI flags
    local cli_flags=""
    if [[ "$FORCE" == "true" ]]; then
        cli_flags="--force"
    fi

    # Add --all flag if removing data
    if [[ "$REMOVE_DATA" == "true" ]]; then
        cli_flags="$cli_flags --all"
    fi

    # Execute CLI uninstall for core components
    if [[ -n "$cli_flags" ]]; then
        "$cli_path" uninstall $cli_flags || true
    else
        "$cli_path" uninstall || true
    fi

    return 0
}

# Main uninstallation flow
main() {
    parse_args "$@"

    print_banner

    echo ""
    echo "This script will remove Conduit from your system."
    echo ""
    echo "  Installation directory: $INSTALL_DIR"
    echo "  Data directory:         $CONDUIT_HOME"
    echo "  Remove data:            $REMOVE_DATA"
    echo ""

    if [[ "$FORCE" != "true" ]] && ! confirm "Proceed with uninstallation?"; then
        echo "Uninstallation cancelled."
        exit 0
    fi

    # Try to delegate to CLI for core uninstall (consistent behavior)
    # If CLI exists, it handles: daemon, service, binaries, data, shell config
    if delegate_to_cli; then
        info "Core uninstall completed via CLI"
    else
        # Fallback: CLI not available, use bash implementation
        warn "Conduit CLI not found, using fallback uninstallation..."
        echo ""

        # Execute uninstallation steps (continue on error)
        stop_daemon || true
        remove_service || true
        remove_binaries || true
        remove_data || true
        cleanup_shell_config || true
    fi

    print_summary
}

# Run main
main "$@"
