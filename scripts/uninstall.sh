#!/bin/bash
#
# Conduit Complete Uninstallation Script
#
# This script safely removes Conduit and optionally its dependencies:
# 1. Stops and removes daemon service
# 2. Removes binaries from PATH
# 3. Removes data directory
# 4. Cleans up shell configuration
# 5. Optionally removes dependencies (Docker/Podman, Ollama, Go)
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/uninstall.sh | bash
#
# Or with options:
#   bash uninstall.sh --force --remove-all
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
REMOVE_ALL=false
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
            --remove-all)
                REMOVE_ALL=true
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
    echo "  --remove-all         Remove all dependencies automatically"
    echo "  --install-dir DIR    Binary installation directory (default: ~/.local/bin)"
    echo "  --conduit-home DIR   Conduit data directory (default: ~/.conduit)"
    echo "  --help               Show this help message"
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
                if launchctl list | grep -q "com.simpleflo.conduit"; then
                    info "Stopping launchd service..."
                    launchctl stop com.simpleflo.conduit 2>/dev/null || true
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
            local PLIST_PATH="$HOME/Library/LaunchAgents/com.simpleflo.conduit.plist"
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

# Remove Qdrant container
remove_qdrant_container() {
    echo ""
    echo "Step 3b: Remove Qdrant Container"
    echo "──────────────────────────────────────────────────────────────"

    # Detect container runtime
    local CONTAINER_CMD=""
    if command_exists podman; then
        CONTAINER_CMD="podman"
    elif command_exists docker; then
        CONTAINER_CMD="docker"
    else
        info "No container runtime found, skipping Qdrant removal"
        return 0
    fi

    # Check if Qdrant container exists
    if ! $CONTAINER_CMD ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^conduit-qdrant$"; then
        info "No Qdrant container found"
        return 0
    fi

    if confirm "Remove Qdrant vector database container?"; then
        info "Stopping and removing Qdrant container..."
        $CONTAINER_CMD stop conduit-qdrant 2>/dev/null || true
        $CONTAINER_CMD rm conduit-qdrant 2>/dev/null || true
        success "Qdrant container removed"
    else
        warn "Qdrant container left in place"
        echo "  Note: If you remove the data directory, the container will have orphaned storage."
        echo "  To remove manually: $CONTAINER_CMD rm -f conduit-qdrant"
    fi
}

# Remove data directory
remove_data() {
    echo ""
    echo "Step 4: Remove Data Directory"
    echo "──────────────────────────────────────────────────────────────"

    if [[ ! -d "$CONDUIT_HOME" ]]; then
        info "Data directory $CONDUIT_HOME does not exist"
        return 0
    fi

    # Show what will be removed
    local size=$(du -sh "$CONDUIT_HOME" 2>/dev/null | cut -f1 || echo "unknown")
    warn "This will remove:"
    echo "  - Directory: $CONDUIT_HOME"
    echo "  - Size: $size"
    echo "  - All configuration, databases, and logs"
    echo ""

    if ! confirm "Remove data directory? (This cannot be undone)" "n"; then
        warn "Data directory left in place"
        return 0
    fi

    rm -rf "$CONDUIT_HOME" || error "Failed to remove $CONDUIT_HOME"
    success "Data directory removed"
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

# Remove Docker
remove_docker() {
    if ! command_exists docker; then
        info "Docker is not installed"
        return 0
    fi

    echo ""
    echo "Docker Removal"
    echo "──────────────────────────────────────────────────────────────"

    if [[ "$REMOVE_ALL" != "true" ]]; then
        if ! confirm "Remove Docker (used by Conduit)?"; then
            info "Docker left in place"
            return 0
        fi
    fi

    # Check if Docker is running
    if docker info >/dev/null 2>&1; then
        warn "Docker is running and may have other containers"
        if ! confirm "Stop Docker and proceed with removal?"; then
            warn "Docker left in place"
            return 0
        fi
    fi

    case $OS in
        darwin)
            if command_exists brew && brew list --cask | grep -q docker; then
                info "Removing Docker Desktop..."
                # Close Docker Desktop first
                osascript -e 'quit app "Docker"' 2>/dev/null || true
                sleep 2
                brew uninstall --cask docker || error "Failed to uninstall Docker"
                success "Docker Desktop removed"
            else
                warn "Docker Desktop was not installed via Homebrew"
                warn "Please uninstall manually from Applications folder"
            fi
            ;;
        linux)
            info "Removing Docker..."
            sudo apt-get remove -y docker docker-engine docker.io containerd runc 2>/dev/null || \
            sudo dnf remove -y docker docker-client docker-client-latest docker-common docker-latest docker-latest-logrotate docker-logrotate docker-engine 2>/dev/null || \
            error "Failed to remove Docker (unsupported package manager)"
            success "Docker removed"
            ;;
    esac
}

# Remove Podman
remove_podman() {
    if ! command_exists podman; then
        info "Podman is not installed"
        return 0
    fi

    echo ""
    echo "Podman Removal"
    echo "──────────────────────────────────────────────────────────────"

    if [[ "$REMOVE_ALL" != "true" ]]; then
        if ! confirm "Remove Podman (used by Conduit)?"; then
            info "Podman left in place"
            return 0
        fi
    fi

    # Stop podman machine on macOS
    if [[ "$OS" == "darwin" ]]; then
        if podman machine list 2>/dev/null | grep -q "Currently running"; then
            info "Stopping Podman machine..."
            podman machine stop 2>/dev/null || true
            sleep 2
        fi
        if podman machine list 2>/dev/null | grep -q "podman-machine"; then
            info "Removing Podman machine..."
            podman machine rm -f podman-machine-default 2>/dev/null || true
        fi
    fi

    case $OS in
        darwin)
            if command_exists brew && brew list | grep -q podman; then
                info "Removing Podman..."
                brew uninstall podman || error "Failed to uninstall Podman"
                success "Podman removed"
            else
                warn "Podman was not installed via Homebrew"
            fi
            ;;
        linux)
            info "Removing Podman..."
            sudo apt-get remove -y podman 2>/dev/null || \
            sudo dnf remove -y podman 2>/dev/null || \
            sudo pacman -R --noconfirm podman 2>/dev/null || \
            error "Failed to remove Podman (unsupported package manager)"
            success "Podman removed"
            ;;
    esac
}

# Remove Ollama
remove_ollama() {
    if ! command_exists ollama; then
        info "Ollama is not installed"
        return 0
    fi

    echo ""
    echo "Ollama Removal"
    echo "──────────────────────────────────────────────────────────────"

    # Check specifically for qwen2.5-coder model
    local has_qwen_model=false
    local model_size=""
    if ollama list 2>/dev/null | grep -q "qwen2.5-coder"; then
        has_qwen_model=true
        model_size=$(ollama list 2>/dev/null | grep "qwen2.5-coder" | awk '{print $2}' || echo "~4.7GB")
    fi

    # Show what will be removed
    if [[ "$has_qwen_model" == "true" ]]; then
        warn "Ollama is installed with qwen2.5-coder:7b model ($model_size)"
    else
        local models=$(ollama list 2>/dev/null | tail -n +2 | wc -l | tr -d ' ')
        if [[ "$models" -gt 0 ]]; then
            warn "Ollama has $models model(s) installed"
            ollama list 2>/dev/null | tail -n +2 || true
        fi
    fi

    if [[ "$REMOVE_ALL" != "true" ]]; then
        local prompt="Remove Ollama"
        if [[ "$has_qwen_model" == "true" ]]; then
            prompt="Remove Ollama and qwen2.5-coder:7b model ($model_size)?"
        else
            prompt="Remove Ollama and all models?"
        fi

        if ! confirm "$prompt"; then
            info "Ollama left in place"
            return 0
        fi
    fi

    # Stop Ollama service
    case $OS in
        darwin)
            pkill -f "ollama serve" 2>/dev/null || true
            ;;
        linux)
            sudo systemctl stop ollama 2>/dev/null || true
            sudo systemctl disable ollama 2>/dev/null || true
            ;;
    esac

    # Remove Ollama
    info "Removing Ollama..."
    if [[ -f /usr/local/bin/ollama ]]; then
        sudo rm -f /usr/local/bin/ollama || error "Failed to remove Ollama binary"
    fi
    if [[ -f /usr/bin/ollama ]]; then
        sudo rm -f /usr/bin/ollama || error "Failed to remove Ollama binary"
    fi

    # Remove models and data
    if [[ -d "$HOME/.ollama" ]]; then
        local ollama_size=$(du -sh "$HOME/.ollama" 2>/dev/null | cut -f1 || echo "unknown")
        info "Removing Ollama data directory (~$ollama_size)..."
        rm -rf "$HOME/.ollama" || error "Failed to remove Ollama data"
    fi

    success "Ollama removed"
}

# Remove Go
remove_go() {
    if ! command_exists go; then
        info "Go is not installed"
        return 0
    fi

    echo ""
    echo "Go Removal"
    echo "──────────────────────────────────────────────────────────────"

    warn "Go is a general-purpose programming language used by many tools"
    warn "Removing it may affect other software on your system"
    echo ""

    if [[ "$REMOVE_ALL" != "true" ]] && ! confirm "Remove Go programming language?" "n"; then
        info "Go left in place"
        return 0
    fi

    # Check if installed via package manager
    case $OS in
        darwin)
            if command_exists brew && brew list | grep -q "^go$"; then
                info "Removing Go (Homebrew)..."
                brew uninstall go || error "Failed to uninstall Go"
                success "Go removed"
                return 0
            fi
            ;;
        linux)
            if dpkg -l | grep -q "golang-go"; then
                info "Removing Go (apt)..."
                sudo apt-get remove -y golang-go || error "Failed to remove Go"
                success "Go removed"
                return 0
            elif rpm -q golang >/dev/null 2>&1; then
                info "Removing Go (dnf/yum)..."
                sudo dnf remove -y golang || sudo yum remove -y golang || error "Failed to remove Go"
                success "Go removed"
                return 0
            fi
            ;;
    esac

    # Manual installation removal
    if [[ -d "/usr/local/go" ]]; then
        info "Removing manually installed Go..."
        sudo rm -rf /usr/local/go || error "Failed to remove /usr/local/go"
        success "Go removed"

        # Clean up shell config
        for config in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
            if [[ -f "$config" ]] && grep -q "/usr/local/go/bin" "$config"; then
                sed -i.bak "/export PATH.*\/usr\/local\/go\/bin/d" "$config" 2>/dev/null || \
                sed -i '' "/export PATH.*\/usr\/local\/go\/bin/d" "$config" 2>/dev/null || true
                rm -f "$config.bak" 2>/dev/null || true
            fi
        done
    else
        warn "Go installation not found in expected location"
    fi
}

# Detect which container runtime Conduit was using
detect_conduit_runtime() {
    local RUNTIME=""

    # Check Conduit config if it exists
    if [[ -f "$CONDUIT_HOME/conduit.yaml" ]]; then
        # Extract preferred runtime from config
        RUNTIME=$(grep "preferred:" "$CONDUIT_HOME/conduit.yaml" 2>/dev/null | awk '{print $2}' | tr -d '"' || echo "")
    fi

    # If config says "auto" or not found, detect from what's running
    if [[ -z "$RUNTIME" ]] || [[ "$RUNTIME" == "auto" ]]; then
        if command_exists docker && docker info >/dev/null 2>&1; then
            RUNTIME="docker"
        elif command_exists podman && podman info >/dev/null 2>&1; then
            RUNTIME="podman"
        fi
    fi

    echo "$RUNTIME"
}

# Remove dependencies
remove_dependencies() {
    echo ""
    echo "Step 6: Remove Dependencies"
    echo "──────────────────────────────────────────────────────────────"

    # Detect which runtime Conduit was using
    local CONDUIT_RUNTIME=$(detect_conduit_runtime)

    if [[ "$REMOVE_ALL" != "true" ]]; then
        echo ""
        echo "Conduit installed or used the following dependencies:"
        echo ""

        # Show runtime Conduit was actually using
        if [[ "$CONDUIT_RUNTIME" == "docker" ]]; then
            echo "  - Docker (used by Conduit)"
        elif [[ "$CONDUIT_RUNTIME" == "podman" ]]; then
            echo "  - Podman (used by Conduit)"
        else
            # Show both if we can't determine
            command_exists docker && echo "  - Docker"
            command_exists podman && echo "  - Podman"
        fi

        command_exists ollama && echo "  - Ollama (with qwen2.5-coder:7b model)"
        command_exists go && echo "  - Go"
        echo ""

        if ! confirm "Remove dependencies?"; then
            info "Dependencies left in place"
            return 0
        fi
    fi

    # Remove the container runtime Conduit was using
    if [[ "$CONDUIT_RUNTIME" == "docker" ]]; then
        remove_docker || true
    elif [[ "$CONDUIT_RUNTIME" == "podman" ]]; then
        remove_podman || true
    elif [[ "$REMOVE_ALL" == "true" ]]; then
        # In --remove-all mode, remove both if present
        remove_docker || true
        remove_podman || true
    else
        # Ask user which one they want to remove
        if command_exists docker || command_exists podman; then
            echo ""
            warn "Could not determine which container runtime Conduit was using"
            command_exists docker && remove_docker || true
            command_exists podman && remove_podman || true
        fi
    fi

    # Always ask about Ollama and Go
    remove_ollama || true
    remove_go || true
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

    echo "Summary of removals:"
    echo ""
    [[ ! -f "$INSTALL_DIR/conduit" ]] && echo -e "  ${GREEN}✓${NC} Binaries removed"
    [[ ! -d "$CONDUIT_HOME" ]] && echo -e "  ${GREEN}✓${NC} Data directory removed"
    ! command_exists docker && echo -e "  ${GREEN}✓${NC} Docker removed"
    ! command_exists podman && echo -e "  ${GREEN}✓${NC} Podman removed"
    ! command_exists ollama && echo -e "  ${GREEN}✓${NC} Ollama removed"
    ! command_exists go && echo -e "  ${GREEN}✓${NC} Go removed"
    echo ""

    echo "You may want to:"
    echo "  - Restart your terminal to apply shell configuration changes"
    echo "  - Review backup files (*.conduit-backup) in your home directory"
    echo ""
    echo "Thank you for trying Conduit!"
    echo ""
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
    echo "  Remove dependencies:    $REMOVE_ALL"
    echo ""

    if [[ "$FORCE" != "true" ]] && ! confirm "Proceed with uninstallation?"; then
        echo "Uninstallation cancelled."
        exit 0
    fi

    # Execute uninstallation steps (continue on error)
    stop_daemon || true
    remove_service || true
    remove_binaries || true
    remove_qdrant_container || true
    remove_data || true
    cleanup_shell_config || true
    remove_dependencies || true

    print_summary
}

# Run main
main "$@"
