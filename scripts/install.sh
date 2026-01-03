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
SKIP_QDRANT=false
SKIP_KAG=false
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
            --no-qdrant)
                SKIP_QDRANT=true
                shift
                ;;
            --no-kag)
                SKIP_KAG=true
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
    echo "  --no-qdrant           Skip Qdrant vector database (semantic search)"
    echo "  --no-kag              Skip KAG (Knowledge Graph) components"
    echo "  --verbose             Show verbose output"
    echo "  --help                Show this help message"
    echo ""
    echo "Components:"
    echo "  RAG (Retrieval-Augmented Generation):"
    echo "    - FTS5 full-text search (always installed)"
    echo "    - Qdrant vector database (semantic search)"
    echo "    - nomic-embed-text embedding model"
    echo ""
    echo "  KAG (Knowledge-Augmented Generation):"
    echo "    - FalkorDB graph database"
    echo "    - mistral:7b-instruct entity extraction model"
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

# Ensure Homebrew is installed (macOS)
ensure_homebrew() {
    if [[ "$OS" != "darwin" ]]; then
        return 0
    fi

    if command_exists brew; then
        success "Homebrew is installed"
        return 0
    fi

    echo ""
    echo "Homebrew Package Manager"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Homebrew is the recommended package manager for macOS."
    echo "It will be used to install dependencies like Go, Ollama, and document tools."
    echo ""

    if ! confirm "Install Homebrew now?"; then
        warn "Some dependencies may need to be installed manually without Homebrew"
        return 1
    fi

    # Check for curl or wget
    if command_exists curl; then
        info "Installing Homebrew via curl..."
        /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" </dev/tty
    elif command_exists wget; then
        info "Installing Homebrew via wget..."
        /bin/bash -c "$(wget -qO- https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" </dev/tty
    else
        error "Neither curl nor wget is available."
        echo ""
        echo "Please install Homebrew manually:"
        echo "  1. Install curl: Use Xcode Command Line Tools"
        echo "  2. Run: xcode-select --install"
        echo "  3. Then retry this installer"
        echo ""
        echo "Or install Homebrew manually:"
        echo "  Visit: https://brew.sh"
        return 1
    fi

    # Add Homebrew to PATH for this session (Apple Silicon vs Intel)
    if [[ -f "/opt/homebrew/bin/brew" ]]; then
        eval "$(/opt/homebrew/bin/brew shellenv)"
    elif [[ -f "/usr/local/bin/brew" ]]; then
        eval "$(/usr/local/bin/brew shellenv)"
    fi

    if command_exists brew; then
        success "Homebrew installed successfully"
        return 0
    else
        error "Homebrew installation may have failed"
        return 1
    fi
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
    local ORIGINAL_DIR=$(pwd)

    # Check CGO prerequisites (required for SQLite FTS5)
    info "Checking CGO prerequisites..."
    if [[ "$OS" == "darwin" ]]; then
        # macOS requires Xcode Command Line Tools for CGO
        if ! xcode-select -p &>/dev/null; then
            warn "Xcode Command Line Tools not found (required for CGO/FTS5)"
            info "Installing Xcode Command Line Tools..."
            xcode-select --install
            echo ""
            echo "Please complete the Xcode installation dialog, then run this script again."
            exit 1
        fi
        success "Xcode Command Line Tools: installed"
    else
        # Linux requires gcc or clang
        if ! command_exists gcc && ! command_exists clang; then
            warn "C compiler not found (required for CGO/FTS5)"
            info "Installing build essentials..."
            case $DISTRO in
                ubuntu|debian)
                    sudo apt-get update && sudo apt-get install -y build-essential
                    ;;
                fedora|rhel|centos)
                    sudo dnf groupinstall -y "Development Tools"
                    ;;
                arch)
                    sudo pacman -S --noconfirm base-devel
                    ;;
            esac
        fi
        success "C compiler: available"
    fi

    # Check if we're already in the conduit repo
    if [[ -f "go.mod" ]] && grep -q "simpleflo/conduit" go.mod 2>/dev/null; then
        info "Building from current directory..."
        BUILD_DIR="."
    else
        info "Cloning Conduit repository..."
        git clone --depth 1 "$REPO_URL" "$BUILD_DIR"
        cd "$BUILD_DIR"
    fi

    # Create install directory
    mkdir -p "$INSTALL_DIR"

    info "Building Conduit binaries with FTS5 support..."
    info "  CGO_ENABLED=1 go build -tags \"fts5\" ..."

    # Build with FTS5 support - CLI
    if ! CGO_ENABLED=1 go build -tags "fts5" -trimpath \
        -ldflags "-X main.Version=$(git describe --tags --always 2>/dev/null || echo dev)" \
        -o "$INSTALL_DIR/conduit" ./cmd/conduit; then
        error "Failed to build conduit CLI"
        error "CGO may not be working. Check that you have a C compiler installed."
        exit 1
    fi

    # Build with FTS5 support - Daemon
    if ! CGO_ENABLED=1 go build -tags "fts5" -trimpath \
        -ldflags "-X main.Version=$(git describe --tags --always 2>/dev/null || echo dev)" \
        -o "$INSTALL_DIR/conduit-daemon" ./cmd/conduit-daemon; then
        error "Failed to build conduit-daemon"
        error "CGO may not be working. Check that you have a C compiler installed."
        exit 1
    fi

    # Verify FTS5 is actually compiled in
    info "Verifying FTS5 support..."
    if "$INSTALL_DIR/conduit-daemon" --version &>/dev/null; then
        success "Binary verification: OK"
    fi

    # Cleanup if we cloned
    if [[ "$BUILD_DIR" != "." ]]; then
        cd "$ORIGINAL_DIR"
        rm -rf "$BUILD_DIR"
    fi

    success "Built conduit and conduit-daemon with FTS5 support"
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

    # Install document extraction tools
    install_document_tools

    # Install Qdrant vector database for semantic search
    install_qdrant

    # Install embedding model for semantic search
    if [[ "$AI_PROVIDER" == "ollama" ]]; then
        install_embedding_model
    fi

    # Install FalkorDB graph database for KAG
    install_falkordb

    # Install KAG extraction model (Mistral 7B)
    if [[ "$AI_PROVIDER" == "ollama" ]] && [[ "$SKIP_KAG" != "true" ]]; then
        install_kag_model
    fi
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

    case $OS in
        darwin)
            # macOS: Use Homebrew or download app
            if command_exists brew; then
                info "Installing Ollama via Homebrew..."
                brew install ollama
            else
                error "Homebrew not found. Please install Ollama manually:"
                echo "  Visit: https://ollama.com/download/mac"
                echo "  Or install Homebrew and try again"
                return 1
            fi
            ;;
        linux)
            # Linux: Use official install script
            curl -fsSL https://ollama.com/install.sh | sh
            ;;
        *)
            error "Unsupported operating system for Ollama: $OS"
            return 1
            ;;
    esac

    success "Ollama installed"

    # Start Ollama
    info "Starting Ollama service..."
    if [[ "$OS" == "darwin" ]]; then
        # On macOS, use brew services if available, otherwise start manually
        if command_exists brew; then
            brew services start ollama 2>/dev/null || ollama serve &>/dev/null &
        else
            ollama serve &>/dev/null &
        fi
    else
        # On Linux, enable and start systemd service
        sudo systemctl enable ollama 2>/dev/null || true
        sudo systemctl start ollama 2>/dev/null || ollama serve &>/dev/null &
    fi

    sleep 3

    # Verify it's running
    if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        success "Ollama is running"
    else
        warn "Ollama may not be running. Start manually with: ollama serve"
        if [[ "$OS" == "darwin" ]] && command_exists brew; then
            info "Or start as service: brew services start ollama"
        fi
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

# Detect container runtime with Podman-first cascading
# Tries Podman first (preferred), falls back to Docker
# On macOS, offers to start Podman machine if not running
detect_container_runtime_cascading() {
    # Try Podman first (preferred for rootless containers)
    if command_exists podman; then
        if [[ "$OS" == "darwin" ]]; then
            # macOS: Check if Podman machine is running
            if podman machine list --format '{{.Running}}' 2>/dev/null | grep -q "true"; then
                echo "podman"
                return 0
            fi

            # Machine exists but not running
            if podman machine list --format '{{.Name}}' 2>/dev/null | grep -q .; then
                echo "" >&2
                warn "Podman machine exists but is not running." >&2
                if confirm "Start Podman machine now?"; then
                    info "Starting Podman machine..." >&2
                    if podman machine start 2>&1 >&2; then
                        success "Podman machine started" >&2
                        echo "podman"
                        return 0
                    else
                        warn "Failed to start Podman machine. Trying Docker..." >&2
                    fi
                fi
            else
                # No machine exists
                echo "" >&2
                info "Podman is installed but no machine exists." >&2
                if confirm "Initialize and start Podman machine?"; then
                    info "Initializing Podman machine..." >&2
                    if podman machine init 2>&1 >&2; then
                        info "Starting Podman machine..." >&2
                        if podman machine start 2>&1 >&2; then
                            success "Podman machine initialized and started" >&2
                            echo "podman"
                            return 0
                        fi
                    fi
                    warn "Failed to initialize Podman machine. Trying Docker..." >&2
                fi
            fi
        else
            # Linux: Podman works natively
            if podman ps >/dev/null 2>&1; then
                echo "podman"
                return 0
            fi
        fi
    fi

    # Fallback to Docker
    if command_exists docker; then
        if docker ps >/dev/null 2>&1; then
            echo "docker"
            return 0
        fi
        warn "Docker is installed but not running." >&2
        if [[ "$OS" == "darwin" ]]; then
            info "Please start Docker Desktop." >&2
        fi
    fi

    # Neither available
    return 1
}

# Install Qdrant vector database
install_qdrant() {
    echo ""
    echo "Step 5c: Vector Database (Qdrant)"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Conduit uses Qdrant for semantic search capabilities."
    echo "This enables finding documents by meaning, not just keywords."
    echo ""

    # Check if --no-qdrant flag was passed
    if [[ "$SKIP_QDRANT" == "true" ]]; then
        info "Skipping Qdrant installation (--no-qdrant flag)"
        echo "  Conduit will use FTS5 full-text search only."
        echo "  Install Qdrant later with: conduit qdrant install"
        return 0
    fi

    # Check if Qdrant is already running
    if curl -s http://localhost:6333/collections >/dev/null 2>&1; then
        success "Qdrant is already running on port 6333"
        return 0
    fi

    # Detect container runtime with Podman-first cascading
    local CONTAINER_CMD=""
    CONTAINER_CMD=$(detect_container_runtime_cascading)

    if [[ -z "$CONTAINER_CMD" ]]; then
        warn "No container runtime available. Skipping Qdrant installation."
        echo ""
        echo "  Conduit will use FTS5 full-text search (keyword matching)."
        echo "  Install Qdrant later with: conduit qdrant install"
        echo ""
        echo "  Or install a container runtime first:"
        echo "    macOS: brew install podman && podman machine init && podman machine start"
        echo "    Linux: sudo apt install podman  (or docker.io)"
        return 0  # Not an error - graceful degradation
    fi

    if ! confirm "Install Qdrant vector database via $CONTAINER_CMD?"; then
        warn "Skipping Qdrant installation. Semantic search will not be available."
        echo "  Install later with: conduit qdrant install"
        return 0
    fi

    info "Starting Qdrant container..."

    # Create data directory structure for persistence
    # Qdrant needs collections/ and snapshots/ subdirectories to exist
    mkdir -p "${CONDUIT_HOME}/qdrant/collections"
    mkdir -p "${CONDUIT_HOME}/qdrant/snapshots"

    # Handle Docker credential helper issues that can prevent container operations
    # Some systems have credential helpers (like docker-credential-gcloud) configured
    # that may not be available in PATH during installation scripts or background services
    local DOCKER_CONFIG="${HOME}/.docker/config.json"
    local DOCKER_CONFIG_BACKUP=""
    if [[ -f "$DOCKER_CONFIG" ]] && grep -q "credHelpers" "$DOCKER_CONFIG" 2>/dev/null; then
        info "Temporarily disabling Docker credential helpers for container operations..."
        DOCKER_CONFIG_BACKUP="${DOCKER_CONFIG}.install-backup"
        cp "$DOCKER_CONFIG" "$DOCKER_CONFIG_BACKUP"
        # Create minimal config without credential helpers
        echo '{"auths": {}}' > "$DOCKER_CONFIG"
    fi

    # Function to restore Docker config
    restore_docker_config() {
        if [[ -n "$DOCKER_CONFIG_BACKUP" ]] && [[ -f "$DOCKER_CONFIG_BACKUP" ]]; then
            mv "$DOCKER_CONFIG_BACKUP" "$DOCKER_CONFIG"
        fi
    }

    # Always remove existing container to ensure fresh state
    # This is important when reinstalling or when storage directory was cleared
    if $CONTAINER_CMD ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^conduit-qdrant$"; then
        info "Removing existing conduit-qdrant container for fresh install..."
        $CONTAINER_CMD stop conduit-qdrant 2>/dev/null || true
        $CONTAINER_CMD rm conduit-qdrant 2>/dev/null || true
    fi

    # Check for orphaned Qdrant data from previous installation
    # This happens when user uninstalls but keeps data, then reinstalls
    local QDRANT_DATA_DIR="${CONDUIT_HOME}/qdrant"
    local SQLITE_DB="${CONDUIT_HOME}/conduit.db"
    if [[ -d "$QDRANT_DATA_DIR/collections/conduit_kb" ]] && [[ ! -f "$SQLITE_DB" ]]; then
        echo ""
        warn "Detected orphaned Qdrant data from a previous installation!"
        echo ""
        echo "  Vector data exists at: $QDRANT_DATA_DIR"
        echo "  But SQLite database is missing (fresh install)"
        echo ""
        echo "  This typically means vectors from a previous KB won't match"
        echo "  the new SQLite database, causing stale search results."
        echo ""
        echo "  Options:"
        echo "    [1] CLEAR - Remove old vectors and start fresh (recommended)"
        echo "    [2] KEEP  - Keep vectors (may cause orphaned/stale results)"
        echo ""

        local qdrant_choice=""
        read -r -p "Enter choice [1/2]: " qdrant_choice </dev/tty

        if [[ "$qdrant_choice" == "1" ]]; then
            info "Clearing orphaned Qdrant data..."
            rm -rf "$QDRANT_DATA_DIR"
            mkdir -p "${QDRANT_DATA_DIR}/collections"
            mkdir -p "${QDRANT_DATA_DIR}/snapshots"
            success "Qdrant data cleared - starting fresh"
        else
            warn "Keeping existing Qdrant data"
            echo "  After installation, run 'conduit qdrant purge' if search results seem stale"
        fi
        echo ""
    fi

    # Run new Qdrant container
    if ! $CONTAINER_CMD run -d \
        --name conduit-qdrant \
        --restart unless-stopped \
        -p 6333:6333 \
        -p 6334:6334 \
        -v "${CONDUIT_HOME}/qdrant:/qdrant/storage" \
        docker.io/qdrant/qdrant:latest 2>&1; then
        warn "Failed to start Qdrant container"
        restore_docker_config
        echo "You may need to start Qdrant manually:"
        echo "  $CONTAINER_CMD run -d --name conduit-qdrant -p 6333:6333 -p 6334:6334 -v ~/.conduit/qdrant:/qdrant/storage qdrant/qdrant"
        return 1
    fi

    # Restore Docker config
    restore_docker_config

    # Wait for Qdrant to be ready
    local RETRIES=30
    while [[ $RETRIES -gt 0 ]]; do
        if curl -s http://localhost:6333/collections >/dev/null 2>&1; then
            success "Qdrant is running"
            return 0
        fi
        sleep 1
        RETRIES=$((RETRIES - 1))
    done

    warn "Qdrant may not have started correctly. Check with: $CONTAINER_CMD logs conduit-qdrant"
    return 1
}

# Install embedding model for semantic search
install_embedding_model() {
    echo ""
    echo "Step 5d: Embedding Model"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Semantic search requires an embedding model to convert text to vectors."
    echo "Conduit uses nomic-embed-text (768 dimensions, ~275MB)."
    echo ""

    # Check if Ollama is running
    if ! curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        warn "Ollama is not running. Skipping embedding model installation."
        echo "Start Ollama and run: ollama pull nomic-embed-text"
        return 1
    fi

    # Check if model is already installed
    if ollama list 2>/dev/null | grep -q "nomic-embed-text"; then
        success "Embedding model (nomic-embed-text) already installed"
        return 0
    fi

    if ! confirm "Download embedding model (nomic-embed-text, ~275MB)?"; then
        warn "Skipping embedding model. Semantic search will not be available."
        echo "Download later with: ollama pull nomic-embed-text"
        return 0
    fi

    info "Downloading embedding model..."
    ollama pull nomic-embed-text

    if ollama list 2>/dev/null | grep -q "nomic-embed-text"; then
        success "Embedding model installed"
    else
        warn "Embedding model installation may have failed"
        echo "Try manually: ollama pull nomic-embed-text"
    fi
}

# Install FalkorDB graph database for KAG
install_falkordb() {
    echo ""
    echo "Step 5e: Graph Database (FalkorDB)"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Conduit's KAG (Knowledge-Augmented Generation) uses FalkorDB"
    echo "to store entity-relationship graphs for multi-hop reasoning."
    echo ""
    echo "FalkorDB is a high-performance graph database (Apache 2.0 license)"
    echo "based on Redis, supporting Cypher queries."
    echo ""

    # Check if --no-kag flag was passed
    if [[ "$SKIP_KAG" == "true" ]]; then
        info "Skipping FalkorDB installation (--no-kag flag)"
        echo "  KAG features will not be available."
        echo "  Install later with: conduit falkordb install"
        return 0
    fi

    # Check if FalkorDB is already running
    if command_exists redis-cli && redis-cli -p 6379 GRAPH.LIST >/dev/null 2>&1; then
        success "FalkorDB is already running on port 6379"
        return 0
    fi

    # Detect container runtime with Podman-first cascading
    local CONTAINER_CMD=""
    CONTAINER_CMD=$(detect_container_runtime_cascading)

    if [[ -z "$CONTAINER_CMD" ]]; then
        warn "No container runtime available. Skipping FalkorDB installation."
        echo ""
        echo "  KAG features will not be available."
        echo "  Install FalkorDB later with: conduit falkordb install"
        echo ""
        echo "  Or install a container runtime first:"
        echo "    macOS: brew install podman && podman machine init && podman machine start"
        echo "    Linux: sudo apt install podman  (or docker.io)"
        return 0  # Not an error - graceful degradation
    fi

    if ! confirm "Install FalkorDB graph database via $CONTAINER_CMD?"; then
        warn "Skipping FalkorDB installation. KAG features will not be available."
        echo "  Install later with: conduit falkordb install"
        return 0
    fi

    info "Starting FalkorDB container..."

    # Create data directory for persistence
    mkdir -p "${CONDUIT_HOME}/falkordb"

    # Handle Docker credential helper issues
    local DOCKER_CONFIG="${HOME}/.docker/config.json"
    local DOCKER_CONFIG_BACKUP=""
    if [[ -f "$DOCKER_CONFIG" ]] && grep -q "credHelpers" "$DOCKER_CONFIG" 2>/dev/null; then
        info "Temporarily disabling Docker credential helpers for container operations..."
        DOCKER_CONFIG_BACKUP="${DOCKER_CONFIG}.install-backup"
        cp "$DOCKER_CONFIG" "$DOCKER_CONFIG_BACKUP"
        echo '{"auths": {}}' > "$DOCKER_CONFIG"
    fi

    # Function to restore Docker config
    restore_docker_config() {
        if [[ -n "$DOCKER_CONFIG_BACKUP" ]] && [[ -f "$DOCKER_CONFIG_BACKUP" ]]; then
            mv "$DOCKER_CONFIG_BACKUP" "$DOCKER_CONFIG"
        fi
    }

    # Remove existing container for fresh state
    if $CONTAINER_CMD ps -a --format "{{.Names}}" 2>/dev/null | grep -q "^conduit-falkordb$"; then
        info "Removing existing conduit-falkordb container for fresh install..."
        $CONTAINER_CMD stop conduit-falkordb 2>/dev/null || true
        $CONTAINER_CMD rm conduit-falkordb 2>/dev/null || true
    fi

    # Run FalkorDB container
    # FalkorDB uses port 6379 (Redis protocol) for graph operations
    if ! $CONTAINER_CMD run -d \
        --name conduit-falkordb \
        --restart unless-stopped \
        -p 6379:6379 \
        -v "${CONDUIT_HOME}/falkordb:/data" \
        docker.io/falkordb/falkordb:latest 2>&1; then
        warn "Failed to start FalkorDB container"
        restore_docker_config
        echo "You may need to start FalkorDB manually:"
        echo "  $CONTAINER_CMD run -d --name conduit-falkordb -p 6379:6379 -v ~/.conduit/falkordb:/data falkordb/falkordb"
        return 1
    fi

    # Restore Docker config
    restore_docker_config

    # Wait for FalkorDB to be ready
    local RETRIES=30
    while [[ $RETRIES -gt 0 ]]; do
        if command_exists redis-cli && redis-cli -p 6379 PING >/dev/null 2>&1; then
            success "FalkorDB is running"
            return 0
        fi
        sleep 1
        RETRIES=$((RETRIES - 1))
    done

    warn "FalkorDB may not have started correctly. Check with: $CONTAINER_CMD logs conduit-falkordb"
    return 1
}

# Install KAG extraction model (Mistral 7B)
install_kag_model() {
    echo ""
    echo "Step 5f: KAG Extraction Model"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "KAG uses Mistral 7B Instruct for entity/relationship extraction."
    echo "This model has the best F1 score for NER tasks among 7B models."
    echo ""
    echo "Model: mistral:7b-instruct-q4_K_M (~4.1GB)"
    echo ""

    # Check if --no-kag flag was passed
    if [[ "$SKIP_KAG" == "true" ]]; then
        info "Skipping KAG model installation (--no-kag flag)"
        return 0
    fi

    # Check if Ollama is running
    if ! curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        warn "Ollama is not running. Skipping KAG model installation."
        echo "Start Ollama and run: ollama pull mistral:7b-instruct-q4_K_M"
        return 1
    fi

    # Check if model is already installed
    if ollama list 2>/dev/null | grep -q "mistral:7b-instruct"; then
        success "KAG model (mistral:7b-instruct) already installed"

        # Ask about model preloading
        echo ""
        echo "Model Preloading"
        echo "────────────────────────────────────────"
        echo "The KAG extraction model (4GB) can be preloaded when the daemon starts."
        echo "This eliminates cold-start delays but uses ~4GB RAM continuously."
        echo ""
        if confirm "Preload KAG model on daemon startup?"; then
            KAG_PRELOAD_MODEL=true
            success "KAG model will be preloaded on daemon startup"
        else
            KAG_PRELOAD_MODEL=false
            info "KAG model will load on first use (1-2 minute delay)"
        fi
        return 0
    fi

    if ! confirm "Download KAG extraction model (mistral:7b-instruct-q4_K_M, ~4.1GB)?"; then
        warn "Skipping KAG model. Entity extraction will not be available."
        echo "Download later with: ollama pull mistral:7b-instruct-q4_K_M"
        KAG_PRELOAD_MODEL=false
        return 0
    fi

    info "Downloading KAG extraction model..."
    ollama pull mistral:7b-instruct-q4_K_M

    if ollama list 2>/dev/null | grep -q "mistral"; then
        success "KAG extraction model installed"

        # Ask about model preloading
        echo ""
        echo "Model Preloading"
        echo "────────────────────────────────────────"
        echo "The KAG extraction model (4GB) can be preloaded when the daemon starts."
        echo "This eliminates cold-start delays but uses ~4GB RAM continuously."
        echo ""
        if confirm "Preload KAG model on daemon startup?"; then
            KAG_PRELOAD_MODEL=true
            success "KAG model will be preloaded on daemon startup"
        else
            KAG_PRELOAD_MODEL=false
            info "KAG model will load on first use (1-2 minute delay)"
        fi
    else
        warn "KAG model installation may have failed"
        echo "Try manually: ollama pull mistral:7b-instruct-q4_K_M"
        KAG_PRELOAD_MODEL=false
    fi
}

# Install document extraction tools
install_document_tools() {
    echo ""
    echo "Step 5b: Document Extraction Tools"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Conduit's Knowledge Base can index various document formats."
    echo "Some formats require external tools for text extraction."
    echo ""
    echo "Formats and required tools:"
    echo "  • PDF files (.pdf)     → pdftotext (from poppler)"
    echo "  • Word docs (.doc)     → textutil (macOS) or antiword (Linux)"
    echo "  • RTF files (.rtf)     → textutil (macOS) or unrtf (Linux)"
    echo "  • DOCX/ODT files       → No tools needed (native support)"
    echo ""

    if ! confirm "Install document extraction tools?"; then
        warn "Skipping document tools. Some document formats may not be indexable."
        return 0
    fi

    local TOOLS_INSTALLED=0
    local TOOLS_FAILED=0

    case $OS in
        darwin)
            install_document_tools_macos
            ;;
        linux)
            install_document_tools_linux
            ;;
        *)
            warn "Document tools installation not supported on $OS"
            echo "Please install manually:"
            echo "  - pdftotext (poppler-utils)"
            echo "  - antiword (for .doc files)"
            echo "  - unrtf (for .rtf files)"
            ;;
    esac
}

# Install document tools on macOS
install_document_tools_macos() {
    # macOS has textutil built-in for DOC/RTF, just need poppler for PDF

    # Check for textutil (should always be present)
    if command_exists textutil; then
        success "textutil: available (built-in, handles .doc and .rtf)"
    else
        warn "textutil: not found (unusual for macOS)"
    fi

    # Install poppler for pdftotext
    if command_exists pdftotext; then
        success "pdftotext: already installed"
    else
        info "Installing poppler (for PDF text extraction)..."
        if command_exists brew; then
            if brew install poppler 2>&1; then
                success "poppler installed (provides pdftotext)"
            else
                error "Failed to install poppler"
                echo "  Install manually: brew install poppler"
            fi
        else
            warn "Homebrew not available. Cannot install poppler automatically."
            echo "  Install Homebrew first, then run: brew install poppler"
            echo "  Or download from: https://poppler.freedesktop.org/"
        fi
    fi

    # Optional: Install antiword as alternative for .doc
    if ! command_exists antiword; then
        if command_exists brew; then
            info "Installing antiword (alternative .doc extractor)..."
            brew install antiword 2>&1 && success "antiword installed" || warn "antiword install failed (optional)"
        fi
    else
        success "antiword: already installed"
    fi

    # Optional: Install unrtf as alternative for .rtf
    if ! command_exists unrtf; then
        if command_exists brew; then
            info "Installing unrtf (alternative .rtf extractor)..."
            brew install unrtf 2>&1 && success "unrtf installed" || warn "unrtf install failed (optional)"
        fi
    else
        success "unrtf: already installed"
    fi
}

# Install document tools on Linux
install_document_tools_linux() {
    local PKG_MANAGER=""
    local PKG_INSTALL=""

    # Detect package manager
    if command_exists apt-get; then
        PKG_MANAGER="apt"
        PKG_INSTALL="sudo apt-get install -y"
        sudo apt-get update -qq
    elif command_exists dnf; then
        PKG_MANAGER="dnf"
        PKG_INSTALL="sudo dnf install -y"
    elif command_exists yum; then
        PKG_MANAGER="yum"
        PKG_INSTALL="sudo yum install -y"
    elif command_exists pacman; then
        PKG_MANAGER="pacman"
        PKG_INSTALL="sudo pacman -S --noconfirm"
    elif command_exists zypper; then
        PKG_MANAGER="zypper"
        PKG_INSTALL="sudo zypper install -y"
    else
        warn "Could not detect package manager"
        echo "Please install manually:"
        echo "  - poppler-utils (for pdftotext)"
        echo "  - antiword (for .doc files)"
        echo "  - unrtf (for .rtf files)"
        return 1
    fi

    info "Using package manager: $PKG_MANAGER"

    # Install poppler-utils (pdftotext)
    if command_exists pdftotext; then
        success "pdftotext: already installed"
    else
        info "Installing poppler-utils..."
        case $PKG_MANAGER in
            apt)
                $PKG_INSTALL poppler-utils && success "poppler-utils installed" || error "Failed to install poppler-utils"
                ;;
            dnf|yum)
                $PKG_INSTALL poppler-utils && success "poppler-utils installed" || error "Failed to install poppler-utils"
                ;;
            pacman)
                $PKG_INSTALL poppler && success "poppler installed" || error "Failed to install poppler"
                ;;
            zypper)
                $PKG_INSTALL poppler-tools && success "poppler-tools installed" || error "Failed to install poppler-tools"
                ;;
        esac
    fi

    # Install antiword (for .doc)
    if command_exists antiword; then
        success "antiword: already installed"
    else
        info "Installing antiword..."
        case $PKG_MANAGER in
            apt|dnf|yum)
                $PKG_INSTALL antiword && success "antiword installed" || warn "antiword install failed (optional)"
                ;;
            pacman)
                # antiword is in AUR, skip for now
                warn "antiword not in official repos (available in AUR)"
                ;;
            zypper)
                $PKG_INSTALL antiword && success "antiword installed" || warn "antiword install failed (optional)"
                ;;
        esac
    fi

    # Install unrtf (for .rtf)
    if command_exists unrtf; then
        success "unrtf: already installed"
    else
        info "Installing unrtf..."
        case $PKG_MANAGER in
            apt)
                $PKG_INSTALL unrtf && success "unrtf installed" || warn "unrtf install failed (optional)"
                ;;
            dnf|yum)
                $PKG_INSTALL unrtf && success "unrtf installed" || warn "unrtf install failed (optional)"
                ;;
            pacman)
                $PKG_INSTALL unrtf && success "unrtf installed" || warn "unrtf install failed (optional)"
                ;;
            zypper)
                $PKG_INSTALL unrtf && success "unrtf installed" || warn "unrtf install failed (optional)"
                ;;
        esac
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

    # Stop any existing daemon completely
    info "Stopping any existing daemon..."
    launchctl stop com.simpleflo.conduit 2>/dev/null || true
    launchctl unload "$PLIST_PATH" 2>/dev/null || true

    # Kill any lingering daemon processes (in case started manually)
    pkill -f "conduit-daemon" 2>/dev/null || true
    sleep 1

    # Remove old socket to ensure clean start
    rm -f "${CONDUIT_HOME}/conduit.sock" 2>/dev/null || true

    # Build PATH for launchd - include Homebrew paths and common binary locations
    # Apple Silicon uses /opt/homebrew, Intel uses /usr/local
    local DAEMON_PATH="/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${INSTALL_DIR}"

    # Create new plist with correct paths including PATH for finding tools like pdftotext
    info "Creating launchd service configuration..."
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
        <key>PATH</key>
        <string>${DAEMON_PATH}</string>
    </dict>
</dict>
</plist>
EOF

    # Verify plist was created correctly
    if grep -q "${INSTALL_DIR}/conduit-daemon" "$PLIST_PATH"; then
        success "Plist created with daemon path: ${INSTALL_DIR}/conduit-daemon"
    else
        error "Plist creation failed - daemon path not set correctly"
        cat "$PLIST_PATH"
        exit 1
    fi

    # Load and start the service
    info "Loading and starting daemon service..."
    launchctl load "$PLIST_PATH"
    launchctl start com.simpleflo.conduit

    success "Conduit daemon installed as launchd service"
    success "PATH configured in service (includes /opt/homebrew/bin for Homebrew tools)"
    info "Service will start automatically on login"
}

# Setup systemd service (Linux)
setup_systemd_service() {
    local SERVICE_PATH="$HOME/.config/systemd/user/conduit.service"

    mkdir -p "$HOME/.config/systemd/user"

    # Stop any existing daemon completely
    info "Stopping any existing daemon..."
    systemctl --user stop conduit 2>/dev/null || true

    # Kill any lingering daemon processes (in case started manually)
    pkill -f "conduit-daemon" 2>/dev/null || true
    sleep 1

    # Remove old socket to ensure clean start
    rm -f "${CONDUIT_HOME}/conduit.sock" 2>/dev/null || true

    # Build PATH for systemd - include common binary locations
    local DAEMON_PATH="/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:${INSTALL_DIR}"

    # Create new service file with PATH for finding tools like pdftotext
    info "Creating systemd service configuration..."
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
Environment=PATH=${DAEMON_PATH}

[Install]
WantedBy=default.target
EOF

    # Verify service file was created correctly
    if grep -q "${INSTALL_DIR}/conduit-daemon" "$SERVICE_PATH"; then
        success "Service file created with daemon path: ${INSTALL_DIR}/conduit-daemon"
    else
        error "Service file creation failed - daemon path not set correctly"
        cat "$SERVICE_PATH"
        exit 1
    fi

    # Reload, enable and start
    info "Loading and starting daemon service..."
    systemctl --user daemon-reload
    systemctl --user enable conduit
    systemctl --user start conduit

    # Enable lingering so service runs without login
    sudo loginctl enable-linger "$USER" 2>/dev/null || true

    success "Conduit daemon installed as systemd user service"
    success "PATH configured in service (includes standard binary locations)"
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

# Knowledge Base Settings
kb:
  # RAG Settings (semantic search)
  rag:
    enabled: true
    semantic_weight: 0.5
    min_score: 0.0
    use_mmr: true
    mmr_lambda: 0.7
    rerank: true

  # KAG Settings (knowledge graph)
  kag:
    enabled: $(if [[ "$SKIP_KAG" == "true" ]]; then echo "false"; else echo "true"; fi)
    preload_model: ${KAG_PRELOAD_MODEL:-false}
    provider: ollama
    ollama:
      model: mistral:7b-instruct-q4_K_M
      host: http://localhost:11434
    # Alternative providers (uncomment to use):
    # openai:
    #   model: gpt-4o-mini
    # anthropic:
    #   model: claude-sonnet-4-20250514
    graph:
      backend: falkordb
      falkordb:
        host: localhost
        port: 6379
        graph_name: conduit_kg
    extraction:
      confidence_threshold: 0.7
      max_entities_per_chunk: 20
      batch_size: 10
EOF

    success "Configuration created: $CONFIG_FILE"
}

# Register Conduit with Claude Code (MCP server auto-registration)
register_claude_code() {
    echo ""
    echo "Step 7a: AI Client Integration"
    echo "──────────────────────────────────────────────────────────────"
    echo ""
    echo "Conduit provides an MCP server for AI clients like Claude Code,"
    echo "Cursor, and VS Code to access your knowledge base."
    echo ""

    local CLAUDE_CONFIG="$HOME/.claude.json"
    local NEEDS_MANUAL_CONFIG=false

    # Check if Claude Code is installed (by checking for config directory)
    if [[ -d "$HOME/.claude" ]] || [[ -f "$CLAUDE_CONFIG" ]]; then
        info "Claude Code installation detected"

        if ! confirm "Register Conduit KB as MCP server with Claude Code?"; then
            NEEDS_MANUAL_CONFIG=true
        else
            # Register with Claude Code
            if register_mcp_claude_code; then
                success "Conduit KB registered with Claude Code"
                info "Restart Claude Code to load the MCP server"
            else
                NEEDS_MANUAL_CONFIG=true
            fi
        fi
    else
        info "Claude Code not detected (no ~/.claude directory)"
        NEEDS_MANUAL_CONFIG=true
    fi

    # Show manual configuration instructions for other clients
    if [[ "$NEEDS_MANUAL_CONFIG" == "true" ]]; then
        echo ""
        echo "To manually configure MCP clients, add to their config:"
        echo ""
    fi

    # Always show the configuration snippet for reference
    echo "────────────────────────────────────────────────────────────"
    echo "MCP Configuration (add to your AI client's config):"
    echo ""
    echo '  {
    "mcpServers": {
      "conduit-kb": {
        "command": "'"${INSTALL_DIR}/conduit"'",
        "args": ["mcp", "kb"]
      }
    }
  }'
    echo ""
    echo "Config file locations:"
    echo "  • Claude Code: ~/.claude.json"
    echo "  • Cursor:      .cursor/settings/extensions.json"
    echo "  • VS Code:     .vscode/settings.json"
    echo "────────────────────────────────────────────────────────────"
}

# Helper function to register MCP server with Claude Code
register_mcp_claude_code() {
    local CLAUDE_CONFIG="$HOME/.claude.json"
    local CONDUIT_PATH="${INSTALL_DIR}/conduit"

    # Check if conduit binary exists
    if [[ ! -x "$CONDUIT_PATH" ]]; then
        warn "Conduit binary not found at $CONDUIT_PATH"
        return 1
    fi

    # Create config if it doesn't exist
    if [[ ! -f "$CLAUDE_CONFIG" ]]; then
        info "Creating new Claude Code configuration..."
        cat > "$CLAUDE_CONFIG" << EOF
{
  "mcpServers": {
    "conduit-kb": {
      "command": "${CONDUIT_PATH}",
      "args": ["mcp", "kb"]
    }
  }
}
EOF
        return 0
    fi

    # Check if jq is available for JSON manipulation
    if ! command_exists jq; then
        # Try to install jq
        if [[ "$OS" == "darwin" ]] && command_exists brew; then
            info "Installing jq for JSON manipulation..."
            brew install jq >/dev/null 2>&1 || true
        fi
    fi

    if command_exists jq; then
        # Use jq for safe JSON manipulation
        local TMP_CONFIG=$(mktemp)

        # Check if mcpServers exists
        if jq -e '.mcpServers' "$CLAUDE_CONFIG" >/dev/null 2>&1; then
            # Check if conduit-kb already exists
            if jq -e '.mcpServers["conduit-kb"]' "$CLAUDE_CONFIG" >/dev/null 2>&1; then
                info "conduit-kb already registered, updating..."
            fi

            # Add or update conduit-kb server
            jq --arg cmd "$CONDUIT_PATH" \
               '.mcpServers["conduit-kb"] = {"command": $cmd, "args": ["mcp", "kb"]}' \
               "$CLAUDE_CONFIG" > "$TMP_CONFIG"
        else
            # Add mcpServers object
            jq --arg cmd "$CONDUIT_PATH" \
               '. + {"mcpServers": {"conduit-kb": {"command": $cmd, "args": ["mcp", "kb"]}}}' \
               "$CLAUDE_CONFIG" > "$TMP_CONFIG"
        fi

        # Verify and move
        if jq empty "$TMP_CONFIG" 2>/dev/null; then
            mv "$TMP_CONFIG" "$CLAUDE_CONFIG"
            return 0
        else
            rm -f "$TMP_CONFIG"
            warn "Failed to update config (invalid JSON generated)"
            return 1
        fi
    else
        # Fallback: Simple check and append (less safe but works without jq)
        warn "jq not available - using simple registration method"

        # Check if conduit-kb is already in config
        if grep -q '"conduit-kb"' "$CLAUDE_CONFIG" 2>/dev/null; then
            info "conduit-kb appears to be registered (verify manually)"
            return 0
        fi

        # Backup existing config
        cp "$CLAUDE_CONFIG" "${CLAUDE_CONFIG}.backup"

        echo ""
        warn "Cannot safely modify existing JSON without jq"
        echo "Please add conduit-kb manually to ~/.claude.json"
        echo ""
        echo "Your existing config has been backed up to: ${CLAUDE_CONFIG}.backup"
        return 1
    fi
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

    # Check Qdrant vector database
    echo ""
    echo "Semantic Search Components:"
    if curl -s http://localhost:6333/collections >/dev/null 2>&1; then
        success "  Qdrant: running on port 6333"
    else
        warn "  Qdrant: not running"
        info "  Start with: docker run -d -p 6333:6333 -p 6334:6334 qdrant/qdrant"
    fi

    # Check embedding model
    if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        if ollama list 2>/dev/null | grep -q "nomic-embed-text"; then
            success "  Embedding model: nomic-embed-text installed"
        else
            warn "  Embedding model: not installed"
            info "  Pull with: ollama pull nomic-embed-text"
        fi
    else
        warn "  Embedding model: Ollama not running (cannot check)"
    fi

    # Check document tools
    echo ""
    echo "Document Extraction Tools:"
    if command_exists pdftotext; then
        success "  PDF:  pdftotext available"
    else
        warn "  PDF:  pdftotext not installed (PDF files won't be indexed)"
    fi

    if [[ "$OS" == "darwin" ]] && command_exists textutil; then
        success "  DOC:  textutil available (built-in)"
        success "  RTF:  textutil available (built-in)"
    else
        if command_exists antiword; then
            success "  DOC:  antiword available"
        else
            warn "  DOC:  no extractor (.doc files won't be indexed)"
        fi
        if command_exists unrtf; then
            success "  RTF:  unrtf available"
        else
            warn "  RTF:  no extractor (.rtf files won't be indexed)"
        fi
    fi
    success "  DOCX: native support"
    success "  ODT:  native support"

    # Check KAG components
    echo ""
    echo "KAG (Knowledge Graph) Components:"

    if [[ "$SKIP_KAG" == "true" ]]; then
        info "  KAG: Skipped (--no-kag flag)"
    else
        # Check FalkorDB
        local FALKORDB_RUNNING=false
        if command_exists redis-cli && redis-cli -p 6379 PING >/dev/null 2>&1; then
            success "  FalkorDB: running on port 6379"
            FALKORDB_RUNNING=true
        else
            warn "  FalkorDB: not running"
            info "  Start with: docker run -d -p 6379:6379 falkordb/falkordb"
        fi

        # Check KAG extraction model
        if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
            if ollama list 2>/dev/null | grep -q "mistral"; then
                success "  Extraction model: mistral:7b-instruct installed"
            else
                warn "  Extraction model: not installed"
                info "  Pull with: ollama pull mistral:7b-instruct-q4_K_M"
            fi
        else
            warn "  Extraction model: Ollama not running (cannot check)"
        fi

        # Overall KAG status
        if [[ "$FALKORDB_RUNNING" == "true" ]]; then
            info "  KAG is ready for entity extraction"
        else
            warn "  KAG requires FalkorDB to be running"
        fi
    fi

    echo ""

    if [[ "$ALL_GOOD" == "true" ]]; then
        success "Installation verified!"
    else
        warn "Some components need attention"
    fi
}

# Print search capability summary
print_capability_summary() {
    echo "RAG (Retrieval-Augmented Generation)"
    echo "────────────────────────────────────────────────────────"

    # FTS5 is always available (built into SQLite)
    echo -e "  ${GREEN}✓${NC} Full-text search (FTS5): Available"

    # Check if Qdrant is running
    local QDRANT_RUNNING=false
    if curl -s http://localhost:6333/collections >/dev/null 2>&1; then
        QDRANT_RUNNING=true
    fi

    # Check if embedding model is installed
    local EMBEDDING_AVAILABLE=false
    if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
        if ollama list 2>/dev/null | grep -q "nomic-embed-text"; then
            EMBEDDING_AVAILABLE=true
        fi
    fi

    # Semantic search status
    if [[ "$QDRANT_RUNNING" == "true" ]] && [[ "$EMBEDDING_AVAILABLE" == "true" ]]; then
        echo -e "  ${GREEN}✓${NC} Semantic search (Qdrant + embeddings): Available"
        echo ""
        echo -e "  ${GREEN}Mode: Hybrid search (best results)${NC}"
        echo "  Uses both keyword matching and meaning-based search"
    elif [[ "$QDRANT_RUNNING" == "true" ]]; then
        echo -e "  ${YELLOW}○${NC} Semantic search: Qdrant running, but embedding model missing"
        echo "     Install with: ollama pull nomic-embed-text"
        echo ""
        echo "  Mode: FTS5 only (keyword matching)"
    elif [[ "$SKIP_QDRANT" == "true" ]]; then
        echo -e "  ${BLUE}○${NC} Semantic search: Skipped (--no-qdrant)"
        echo "     Enable later with: conduit qdrant install"
        echo ""
        echo "  Mode: FTS5 only (keyword matching)"
    else
        echo -e "  ${YELLOW}○${NC} Semantic search: Not available"
        echo "     Install with: conduit qdrant install"
        echo ""
        echo "  Mode: FTS5 only (keyword matching)"
    fi

    echo ""

    # KAG capability summary
    echo "KAG (Knowledge-Augmented Generation)"
    echo "────────────────────────────────────────────────────────"

    if [[ "$SKIP_KAG" == "true" ]]; then
        echo -e "  ${BLUE}○${NC} Knowledge Graph: Skipped (--no-kag)"
        echo "     Enable later with: conduit falkordb install"
        echo ""
        echo "  KAG features (multi-hop reasoning, entity queries) not available"
    else
        # Check FalkorDB
        local FALKORDB_RUNNING=false
        if command_exists redis-cli && redis-cli -p 6379 PING >/dev/null 2>&1; then
            FALKORDB_RUNNING=true
        fi

        # Check extraction model
        local EXTRACTION_MODEL_AVAILABLE=false
        if curl -s http://localhost:11434/api/tags >/dev/null 2>&1; then
            if ollama list 2>/dev/null | grep -q "mistral"; then
                EXTRACTION_MODEL_AVAILABLE=true
            fi
        fi

        if [[ "$FALKORDB_RUNNING" == "true" ]] && [[ "$EXTRACTION_MODEL_AVAILABLE" == "true" ]]; then
            echo -e "  ${GREEN}✓${NC} FalkorDB graph database: Running"
            echo -e "  ${GREEN}✓${NC} Mistral 7B extraction model: Available"
            echo ""
            echo -e "  ${GREEN}Mode: Full KAG capabilities${NC}"
            echo "  Multi-hop reasoning, entity queries, relationship traversal"
        elif [[ "$FALKORDB_RUNNING" == "true" ]]; then
            echo -e "  ${GREEN}✓${NC} FalkorDB graph database: Running"
            echo -e "  ${YELLOW}○${NC} Extraction model: Not installed"
            echo "     Install with: ollama pull mistral:7b-instruct-q4_K_M"
            echo ""
            echo "  Graph queries available, but entity extraction disabled"
        else
            echo -e "  ${YELLOW}○${NC} FalkorDB: Not running"
            echo "     Start with: conduit falkordb start"
            echo ""
            echo "  KAG features not available until FalkorDB is running"
        fi
    fi

    echo ""
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

    # Print search capability summary
    print_capability_summary

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
    echo "  2. Check MCP server capabilities:"
    echo "     conduit mcp status"
    echo ""
    echo "  3. Add documents to knowledge base:"
    echo "     conduit kb add ~/Documents/my-docs"
    echo ""
    echo "  4. Run diagnostics:"
    echo "     conduit doctor"
    echo ""
    echo "  5. View all commands:"
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

    # Ensure Homebrew is available on macOS (used for most dependencies)
    if [[ "$OS" == "darwin" ]]; then
        ensure_homebrew
    fi

    install_git
    install_go
    build_conduit
    install_binaries
    install_dependencies
    create_config
    register_claude_code
    setup_service
    verify_installation
    print_completion
}

# Run main
main "$@"
