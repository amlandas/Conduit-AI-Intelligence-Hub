#!/bin/bash
#
# Conduit One-Click Installation Script
#
# This script handles the complete installation of Conduit:
# 1. Installs system dependencies (Go, Git)
# 2. Builds Conduit from source
# 3. Installs binaries to PATH
# 4. Installs runtime dependencies (Docker/Podman, Ollama, AI model)
# 5. Sets up the daemon as a service
# 6. Verifies the installation
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/amlandas/Conduit-AI-Intelligence-Hub/main/scripts/install.sh | bash
#
# Or with options:
#   curl -fsSL ... | bash -s -- --install-dir ~/.local/bin --no-service
#

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default configuration
INSTALL_DIR="${HOME}/.local/bin"
CONDUIT_HOME="${HOME}/.conduit"
INSTALL_SERVICE=true
SKIP_DEPS=false
VERBOSE=false
AI_PROVIDER=""  # Will be set during installation
SHELL_RC=""     # Will be detected during installation

# Minimum Go version required
MIN_GO_VERSION="1.21"

# Print functions
print_banner() {
    echo -e "${BLUE}"
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                  Conduit Installation                        ║"
    echo "║              AI Intelligence Hub for MCP                     ║"
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
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            --install-dir)
                INSTALL_DIR="$2"
                shift 2
                ;;
            --conduit-home)
                CONDUIT_HOME="$2"
                shift 2
                ;;
            --no-service)
                INSTALL_SERVICE=false
                shift
                ;;
            --skip-deps)
                SKIP_DEPS=true
                shift
                ;;
            --verbose)
                VERBOSE=true
                shift
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
    echo "Conduit Installation Script"
    echo ""
    echo "Usage: install.sh [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --install-dir DIR     Install binaries to DIR (default: ~/.local/bin)"
    echo "  --conduit-home DIR    Conduit data directory (default: ~/.conduit)"
    echo "  --no-service          Don't install as a system service"
    echo "  --skip-deps           Skip dependency installation"
    echo "  --verbose             Show verbose output"
    echo "  --help                Show this help message"
}

# Detect OS and architecture
detect_system() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case $ARCH in
        x86_64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            exit 1
            ;;
    esac

    case $OS in
        darwin|linux)
            ;;
        mingw*|msys*|cygwin*)
            OS="windows"
            warn "Windows installation is experimental. Consider using WSL2."
            ;;
        *)
            error "Unsupported operating system: $OS"
            exit 1
            ;;
    esac

    info "Detected system: $OS/$ARCH"
}

# Detect Linux distribution
detect_linux_distro() {
    if [[ "$OS" != "linux" ]]; then
        DISTRO="unknown"
        return
    fi

    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        DISTRO=$(echo "$ID" | tr '[:upper:]' '[:lower:]')
    elif [[ -f /etc/debian_version ]]; then
        DISTRO="debian"
    elif [[ -f /etc/redhat-release ]]; then
        DISTRO="rhel"
    else
        DISTRO="unknown"
    fi

    if [[ "$VERBOSE" == "true" ]]; then
        info "Linux distribution: $DISTRO"
    fi
}

# Check if a command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Prompt for confirmation
confirm() {
    local prompt="$1"
    local default="${2:-y}"

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

# Check and install Go
install_go() {
    echo ""
    echo "Step 1: Go Programming Language"
    echo "──────────────────────────────────────────────────────────────"

    if command_exists go; then
        GO_VERSION=$(go version | grep -oE 'go[0-9]+\.[0-9]+' | sed 's/go//')
        if version_gte "$GO_VERSION" "$MIN_GO_VERSION"; then
            success "Go $GO_VERSION is installed"
            return 0
        else
            warn "Go $GO_VERSION is installed but version $MIN_GO_VERSION+ is required"
        fi
    else
        info "Go is not installed"
    fi

    echo ""
    echo "Conduit requires Go $MIN_GO_VERSION or later to build from source."
    echo ""

    if ! confirm "Install Go now?"; then
        error "Go is required. Please install Go manually: https://go.dev/dl/"
        exit 1
    fi

    install_go_binary
}

# Version comparison
version_gte() {
    local v1=$1
    local v2=$2

    # Use sort -V for version comparison
    [[ "$(printf '%s\n%s' "$v2" "$v1" | sort -V | head -n1)" == "$v2" ]]
}

# Install Go binary
install_go_binary() {
    local GO_VERSION="1.22.0"
    local DOWNLOAD_URL="https://go.dev/dl/go${GO_VERSION}.${OS}-${ARCH}.tar.gz"
    local INSTALL_PATH="/usr/local"

    info "Downloading Go $GO_VERSION..."

    if [[ "$OS" == "darwin" ]]; then
        # Use Homebrew on macOS if available
        if command_exists brew; then
            info "Installing Go via Homebrew..."
            brew install go
            success "Go installed via Homebrew"
            return 0
        fi
    fi

    # Download and install to /usr/local
    local TMP_DIR=$(mktemp -d)
    curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/go.tar.gz"

    if [[ -d "$INSTALL_PATH/go" ]]; then
        warn "Existing Go installation found at $INSTALL_PATH/go"
        if confirm "Remove existing installation?"; then
            sudo rm -rf "$INSTALL_PATH/go"
        else
            error "Cannot install Go: existing installation in the way"
            exit 1
        fi
    fi

    info "Installing Go to $INSTALL_PATH/go..."
    sudo tar -C "$INSTALL_PATH" -xzf "$TMP_DIR/go.tar.gz"
    rm -rf "$TMP_DIR"

    # Add to PATH for this session
    export PATH="$INSTALL_PATH/go/bin:$PATH"

    success "Go $GO_VERSION installed"

    # Check if PATH needs to be updated in shell config
    if ! echo "$PATH" | grep -q "$INSTALL_PATH/go/bin"; then
        warn "Add this to your shell profile:"
        echo "  export PATH=\$PATH:$INSTALL_PATH/go/bin"
    fi
}

# Check and install Git
install_git() {
    echo ""
    echo "Step 2: Git"
    echo "──────────────────────────────────────────────────────────────"

    if command_exists git; then
        success "Git is installed: $(git --version)"
        return 0
    fi

    info "Git is not installed"

    if ! confirm "Install Git now?"; then
        error "Git is required. Please install Git manually."
        exit 1
    fi

    case $OS in
        darwin)
            if command_exists brew; then
                brew install git
            else
                xcode-select --install
            fi
            ;;
        linux)
            case $DISTRO in
                ubuntu|debian)
                    sudo apt-get update && sudo apt-get install -y git
                    ;;
                fedora|rhel|centos)
                    sudo dnf install -y git
                    ;;
                arch)
                    sudo pacman -S --noconfirm git
                    ;;
                *)
                    error "Please install Git manually for your distribution"
                    exit 1
                    ;;
            esac
            ;;
    esac

    success "Git installed"
}

# Build Conduit from source
build_conduit() {
    echo ""
    echo "Step 3: Build Conduit"
    echo "──────────────────────────────────────────────────────────────"

    local REPO_URL="https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git"
    local BUILD_DIR=$(mktemp -d)

    # Check if we're already in the conduit repo
    if [[ -f "go.mod" ]] && grep -q "module conduit" go.mod 2>/dev/null; then
        info "Building from current directory..."
        BUILD_DIR="."
    else
        info "Cloning Conduit repository..."
        git clone --depth 1 "$REPO_URL" "$BUILD_DIR"
        cd "$BUILD_DIR"
    fi

    info "Building Conduit binaries..."

    # Build with FTS5 support
    CGO_ENABLED=1 go build -tags "fts5" -trimpath \
        -ldflags "-X main.Version=$(git describe --tags --always 2>/dev/null || echo dev)" \
        -o "$INSTALL_DIR/conduit" ./cmd/conduit

    CGO_ENABLED=1 go build -tags "fts5" -trimpath \
        -ldflags "-X main.Version=$(git describe --tags --always 2>/dev/null || echo dev)" \
        -o "$INSTALL_DIR/conduit-daemon" ./cmd/conduit-daemon

    # Cleanup if we cloned
    if [[ "$BUILD_DIR" != "." ]]; then
        cd -
        rm -rf "$BUILD_DIR"
    fi

    success "Built conduit and conduit-daemon"
}

# Install binaries to PATH
install_binaries() {
    echo ""
    echo "Step 4: Install Binaries"
    echo "──────────────────────────────────────────────────────────────"

    # Create install directory if it doesn't exist
    mkdir -p "$INSTALL_DIR"

    # Check if binaries exist (built in previous step)
    if [[ ! -f "$INSTALL_DIR/conduit" ]]; then
        error "Binaries not found. Build step may have failed."
        exit 1
    fi

    # Make executable
    chmod +x "$INSTALL_DIR/conduit" "$INSTALL_DIR/conduit-daemon"

    success "Binaries installed to $INSTALL_DIR"

    # Check if install dir is in PATH
    if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
        warn "Installation directory is not in your PATH"
        echo ""
        echo "Add this line to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo ""
        echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
        echo ""

        # Try to add to current session
        export PATH="$PATH:$INSTALL_DIR"

        # Detect shell and offer to add automatically (set global SHELL_RC)
        case "$SHELL" in
            */bash)
                SHELL_RC="$HOME/.bashrc"
                ;;
            */zsh)
                SHELL_RC="$HOME/.zshrc"
                ;;
            */fish)
                SHELL_RC="$HOME/.config/fish/config.fish"
                ;;
        esac

        if [[ -n "$SHELL_RC" ]] && [[ -f "$SHELL_RC" ]]; then
            if confirm "Add to $SHELL_RC automatically?"; then
                # Check if already in config file
                if ! grep -q "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
                    echo "" >> "$SHELL_RC"
                    echo "# Conduit" >> "$SHELL_RC"
                    echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$SHELL_RC"
                    success "Added to $SHELL_RC"
                    warn "Please run: source $SHELL_RC"
                    warn "Or restart your terminal for the changes to take effect"
                else
                    success "PATH already configured in $SHELL_RC"
                fi
            else
                warn "You'll need to manually add $INSTALL_DIR to your PATH"
            fi
        fi
    else
        success "Installation directory is already in PATH"
    fi
}

# Install runtime dependencies
install_dependencies() {
    if [[ "$SKIP_DEPS" == "true" ]]; then
        info "Skipping dependency installation (--skip-deps)"
        return 0
    fi

    echo ""
    echo "Step 5: Runtime Dependencies"
    echo "──────────────────────────────────────────────────────────────"

    # Install container runtime
    install_container_runtime

    # Choose and install AI provider
    choose_ai_provider
}

# Install container runtime (Docker or Podman)
install_container_runtime() {
    local DOCKER_INSTALLED=false
    local PODMAN_INSTALLED=false

    if command_exists docker && docker info >/dev/null 2>&1; then
        DOCKER_INSTALLED=true
        success "Docker is installed and running: $(docker --version)"
    elif command_exists docker; then
        warn "Docker is installed but not running"
        if [[ "$OS" == "darwin" ]]; then
            warn "Please start Docker Desktop manually"
        fi
    fi

    if command_exists podman && podman info >/dev/null 2>&1; then
        PODMAN_INSTALLED=true
        success "Podman is installed and running: $(podman --version)"
    elif command_exists podman; then
        warn "Podman is installed but not running"
        if [[ "$OS" == "darwin" ]]; then
            info "Starting Podman machine..."
            podman machine start 2>/dev/null || warn "Failed to start Podman machine. Start manually with: podman machine start"
        fi
    fi

    # If either is running, ask if user wants to install the other
    if [[ "$DOCKER_INSTALLED" == "true" ]] || [[ "$PODMAN_INSTALLED" == "true" ]]; then
        echo ""
        if ! confirm "Container runtime detected. Install additional runtime?"; then
            return 0
        fi
    fi

    local RECOMMENDED="Docker"
    [[ "$OS" == "linux" ]] && RECOMMENDED="Podman"

    echo ""
    echo "Conduit needs Docker or Podman to run MCP servers in containers."
    echo ""
    echo "  [1] Install $RECOMMENDED (Recommended for $OS)"
    [[ "$RECOMMENDED" == "Docker" ]] && echo "  [2] Install Podman" || echo "  [2] Install Docker"
    echo "  [3] Skip (install manually later)"
    echo ""

    read -r -p "Choice [1/2/3]: " choice </dev/tty

    case $choice in
        1)
            [[ "$RECOMMENDED" == "Docker" ]] && install_docker || install_podman
            ;;
        2)
            [[ "$RECOMMENDED" == "Docker" ]] && install_podman || install_docker
            ;;
        *)
            warn "Skipping container runtime installation"
            ;;
    esac
}

install_docker() {
    info "Installing Docker..."

    case $OS in
        darwin)
            if command_exists brew; then
                brew install --cask docker
                open -a Docker
                warn "Docker Desktop is starting. Please complete setup in the app."
            else
                error "Install Docker Desktop manually: https://docker.com/products/docker-desktop"
            fi
            ;;
        linux)
            curl -fsSL https://get.docker.com | sh
            sudo usermod -aG docker "$USER"
            warn "Log out and back in for docker group membership to take effect"
            ;;
    esac
}

install_podman() {
    info "Installing Podman..."

    case $OS in
        darwin)
            if command_exists brew; then
                brew install podman
                podman machine init
                podman machine start
            else
                error "Install Homebrew first or install Podman manually"
            fi
            ;;
        linux)
            case $DISTRO in
                ubuntu|debian)
                    sudo apt-get update && sudo apt-get install -y podman
                    ;;
                fedora|rhel|centos)
                    sudo dnf install -y podman
                    ;;
                arch)
                    sudo pacman -S --noconfirm podman
                    ;;
            esac
            ;;
    esac

    success "Podman installed"
}

# Choose AI provider
choose_ai_provider() {
    echo ""
    echo "Step 5a: AI Provider Selection"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Conduit can use AI to analyze MCP servers and provide intelligent assistance."
    echo ""
    echo "Choose your AI provider:"
    echo ""
    echo "  [1] Ollama (Local, Free, Private)"
    echo "      - Runs AI models locally on your machine"
    echo "      - Requires ~5GB disk space for models"
    echo "      - No API keys needed"
    echo "      - Complete privacy"
    echo ""
    echo "  [2] Anthropic Claude (Cloud, Paid)"
    echo "      - Uses Claude API (requires API key)"
    echo "      - No local resources needed"
    echo "      - Best quality responses"
    echo "      - Pay per use"
    echo ""
    echo "  [3] Skip (Configure later)"
    echo ""

    read -r -p "Choice [1/2/3]: " ai_choice </dev/tty

    case $ai_choice in
        1)
            AI_PROVIDER="ollama"
            install_ollama
            ;;
        2)
            AI_PROVIDER="anthropic"
            setup_anthropic_api
            ;;
        *)
            AI_PROVIDER="none"
            warn "Skipping AI provider setup. Configure later in ~/.conduit/conduit.yaml"
            ;;
    esac
}

# Setup Anthropic API
setup_anthropic_api() {
    echo ""
    info "Setting up Anthropic Claude API..."
    echo ""
    echo "To use Claude, you need an API key from Anthropic."
    echo "Get your API key at: https://console.anthropic.com/settings/keys"
    echo ""

    if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
        success "ANTHROPIC_API_KEY environment variable detected"
    else
        echo "You can set your API key later by:"
        echo "  export ANTHROPIC_API_KEY='your-api-key-here'"
        echo ""
        warn "Add this to your shell profile (~/.bashrc, ~/.zshrc) to persist"
    fi
}

# Install Ollama
install_ollama() {
    if command_exists ollama; then
        success "Ollama is installed: $(ollama --version 2>/dev/null || echo 'version unknown')"

        # Check if running
        if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
            success "Ollama is running"
        else
            info "Starting Ollama..."
            if [[ "$OS" == "darwin" ]]; then
                ollama serve &>/dev/null &
            else
                sudo systemctl start ollama 2>/dev/null || ollama serve &>/dev/null &
            fi
            sleep 2
        fi

        # Check for model
        if ollama list 2>/dev/null | grep -q "qwen2.5-coder"; then
            success "AI model (qwen2.5-coder:7b) already installed"
            return 0
        else
            if confirm "Download AI model (qwen2.5-coder:7b, ~4.7GB)?"; then
                info "Downloading model... (this may take several minutes)"
                ollama pull qwen2.5-coder:7b
                success "Model downloaded"
            fi
        fi
        return 0
    fi

    info "Installing Ollama..."
    curl -fsSL https://ollama.com/install.sh | sh

    success "Ollama installed"

    # Start Ollama
    info "Starting Ollama service..."
    if [[ "$OS" == "darwin" ]]; then
        ollama serve &>/dev/null &
    else
        sudo systemctl enable ollama 2>/dev/null || true
        sudo systemctl start ollama 2>/dev/null || ollama serve &>/dev/null &
    fi

    sleep 3

    # Verify it's running
    if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        success "Ollama is running"
    else
        warn "Ollama may not be running. Start manually with: ollama serve"
    fi

    # Pull the model
    if confirm "Download AI model (qwen2.5-coder:7b, ~4.7GB)?"; then
        info "Downloading model... (this may take several minutes)"
        ollama pull qwen2.5-coder:7b
        success "Model downloaded"
    else
        warn "Skipped model download. Download later with: ollama pull qwen2.5-coder:7b"
    fi
}

# Setup daemon as a service
setup_service() {
    if [[ "$INSTALL_SERVICE" != "true" ]]; then
        info "Skipping service setup (--no-service)"
        return 0
    fi

    echo ""
    echo "Step 6: Daemon Service"
    echo "──────────────────────────────────────────────────────────────"

    echo ""
    echo "The Conduit daemon runs in the background to manage MCP servers."
    echo ""

    if ! confirm "Set up Conduit daemon as a system service?"; then
        warn "Skipping service setup. Start daemon manually: conduit-daemon --foreground"
        return 0
    fi

    case $OS in
        darwin)
            setup_launchd_service
            ;;
        linux)
            setup_systemd_service
            ;;
    esac
}

# Setup launchd service (macOS)
setup_launchd_service() {
    local PLIST_PATH="$HOME/Library/LaunchAgents/com.simpleflo.conduit.plist"

    mkdir -p "$HOME/Library/LaunchAgents"

    cat > "$PLIST_PATH" << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.simpleflo.conduit</string>
    <key>ProgramArguments</key>
    <array>
        <string>${INSTALL_DIR}/conduit-daemon</string>
        <string>--foreground</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>${CONDUIT_HOME}/daemon.log</string>
    <key>StandardErrorPath</key>
    <string>${CONDUIT_HOME}/daemon.log</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>HOME</key>
        <string>${HOME}</string>
    </dict>
</dict>
</plist>
EOF

    # Load the service
    launchctl unload "$PLIST_PATH" 2>/dev/null || true
    launchctl load "$PLIST_PATH"

    success "Conduit daemon installed as launchd service"
    info "Service will start automatically on login"

    # Start now
    launchctl start com.simpleflo.conduit 2>/dev/null || true
}

# Setup systemd service (Linux)
setup_systemd_service() {
    local SERVICE_PATH="$HOME/.config/systemd/user/conduit.service"

    mkdir -p "$HOME/.config/systemd/user"

    cat > "$SERVICE_PATH" << EOF
[Unit]
Description=Conduit AI Intelligence Hub Daemon
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/conduit-daemon --foreground
Restart=always
RestartSec=10
Environment=HOME=${HOME}

[Install]
WantedBy=default.target
EOF

    # Reload and enable
    systemctl --user daemon-reload
    systemctl --user enable conduit
    systemctl --user start conduit

    # Enable lingering so service runs without login
    sudo loginctl enable-linger "$USER" 2>/dev/null || true

    success "Conduit daemon installed as systemd user service"
    info "Service will start automatically on login"
}

# Create initial configuration
create_config() {
    echo ""
    echo "Step 7: Configuration"
    echo "──────────────────────────────────────────────────────────────"

    mkdir -p "$CONDUIT_HOME"

    local CONFIG_FILE="$CONDUIT_HOME/conduit.yaml"

    if [[ -f "$CONFIG_FILE" ]]; then
        success "Configuration already exists: $CONFIG_FILE"
        return 0
    fi

    # Use the AI provider selected during installation
    local CONFIG_AI_PROVIDER="${AI_PROVIDER:-ollama}"
    local AI_MODEL="qwen2.5-coder:7b"

    if [[ "$CONFIG_AI_PROVIDER" == "anthropic" ]]; then
        AI_MODEL="claude-sonnet-4-20250514"
    elif [[ "$CONFIG_AI_PROVIDER" == "none" ]]; then
        CONFIG_AI_PROVIDER="ollama"
        warn "No AI provider selected. Using Ollama as default in config."
    fi

    cat > "$CONFIG_FILE" << EOF
# Conduit Configuration
# Generated by install.sh

# Data directory
data_dir: ~/.conduit

# Unix socket path
socket: ~/.conduit/conduit.sock

# Logging
log_level: info
log_format: json

# AI Configuration
ai:
  provider: ${CONFIG_AI_PROVIDER}
  model: ${AI_MODEL}
  endpoint: http://localhost:11434
  timeout_seconds: 120
  max_retries: 2
  confidence_threshold: 0.6

# Container runtime
runtime:
  preferred: auto

# Policy settings
policy:
  allow_network_egress: false
EOF

    success "Configuration created: $CONFIG_FILE"
}

# Verify installation
verify_installation() {
    echo ""
    echo "Step 8: Verification"
    echo "──────────────────────────────────────────────────────────────"

    local ALL_GOOD=true

    # Check binaries
    if command_exists conduit; then
        success "conduit CLI: $(conduit version 2>/dev/null | head -1 || echo 'installed')"
    else
        error "conduit CLI not found in PATH"
        ALL_GOOD=false
    fi

    if command_exists conduit-daemon; then
        success "conduit-daemon: installed"
    else
        error "conduit-daemon not found in PATH"
        ALL_GOOD=false
    fi

    # Check daemon
    sleep 2  # Give daemon time to start
    if curl -s --unix-socket "$CONDUIT_HOME/conduit.sock" http://localhost/api/v1/health >/dev/null 2>&1; then
        success "Conduit daemon: running"
    else
        warn "Conduit daemon: not running"
        if [[ "$INSTALL_SERVICE" == "true" ]]; then
            info "The daemon service will start automatically on next login"
            info "To start now: $INSTALL_DIR/conduit-daemon --foreground &"
        fi
    fi

    # Check container runtime
    if command_exists docker && docker info >/dev/null 2>&1; then
        success "Docker: running"
    elif command_exists podman && podman info >/dev/null 2>&1; then
        success "Podman: running"
    elif command_exists docker; then
        warn "Docker: installed but not running"
        if [[ "$OS" == "darwin" ]]; then
            info "Start Docker Desktop from Applications"
        else
            info "Start with: sudo systemctl start docker"
        fi
    elif command_exists podman; then
        warn "Podman: installed but not running"
        if [[ "$OS" == "darwin" ]]; then
            info "Start with: podman machine start"
        else
            info "Start with: systemctl start podman"
        fi
    else
        warn "Container runtime: not installed"
        info "Install later with: conduit install-deps"
    fi

    # Check AI provider
    if [[ "$AI_PROVIDER" == "anthropic" ]]; then
        if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
            success "Anthropic API: configured (ANTHROPIC_API_KEY set)"
        else
            warn "Anthropic API: not configured"
            info "Set your API key: export ANTHROPIC_API_KEY='your-key-here'"
        fi
    elif [[ "$AI_PROVIDER" == "ollama" ]]; then
        if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
            success "Ollama: running"

            # Check model
            if ollama list 2>/dev/null | grep -q "qwen2.5-coder"; then
                success "AI Model: qwen2.5-coder:7b installed"
            else
                warn "AI Model: not installed"
                info "Pull with: ollama pull qwen2.5-coder:7b"
            fi
        else
            warn "Ollama: not running"
            info "Start with: ollama serve"
        fi
    else
        info "AI provider: not configured (will use defaults)"
    fi

    echo ""

    if [[ "$ALL_GOOD" == "true" ]]; then
        success "Installation verified!"
    else
        warn "Some components need attention"
    fi
}

# Print completion message
print_completion() {
    echo ""
    echo -e "${GREEN}══════════════════════════════════════════════════════════════${NC}"
    echo -e "${GREEN}               Installation Complete!                          ${NC}"
    echo -e "${GREEN}══════════════════════════════════════════════════════════════${NC}"
    echo ""
    echo "Conduit is now installed!"
    echo ""

    # Check if PATH needs update
    local NEEDS_PATH_UPDATE=false
    if ! command_exists conduit 2>/dev/null; then
        NEEDS_PATH_UPDATE=true
        echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo -e "${YELLOW}⚠  IMPORTANT: Restart your terminal or run:${NC}"
        echo ""
        if [[ -n "$SHELL_RC" ]]; then
            echo -e "  ${GREEN}source $SHELL_RC${NC}"
        else
            echo -e "  ${GREEN}export PATH=\"\$PATH:$INSTALL_DIR\"${NC}"
        fi
        echo ""
        echo "This is required for the 'conduit' command to work."
        echo -e "${YELLOW}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
        echo ""
    fi

    echo "Quick Start (after restarting terminal):"
    echo ""
    echo "  1. Check status:"
    echo "     conduit status"
    echo ""
    echo "  2. Run diagnostics:"
    echo "     conduit doctor"
    echo ""
    echo "  3. View all commands:"
    echo "     conduit --help"
    echo ""
    echo "Documentation: https://github.com/amlandas/Conduit-AI-Intelligence-Hub"
    echo "Report issues: https://github.com/amlandas/Conduit-AI-Intelligence-Hub/issues"
    echo ""
}

# Main installation flow
main() {
    parse_args "$@"

    print_banner
    detect_system
    detect_linux_distro

    echo ""
    echo "This script will install Conduit and its dependencies."
    echo ""
    echo "  Install directory:  $INSTALL_DIR"
    echo "  Conduit home:       $CONDUIT_HOME"
    echo "  Install service:    $INSTALL_SERVICE"
    echo ""

    if ! confirm "Proceed with installation?"; then
        echo "Installation cancelled."
        exit 0
    fi

    install_git
    install_go
    build_conduit
    install_binaries
    install_dependencies
    create_config
    setup_service
    verify_installation
    print_completion
}

# Run main
main "$@"
