#Requires -Version 5.1
<#
.SYNOPSIS
    Conduit One-Click Installation Script for Windows

.DESCRIPTION
    This script handles the complete installation of Conduit on Windows:
    1. Installs Chocolatey (if needed)
    2. Installs system dependencies (Go, Git)
    3. Builds Conduit from source
    4. Installs binaries to PATH
    5. Installs runtime dependencies (Docker Desktop, Ollama)
    6. Installs document extraction tools (poppler, antiword)
    7. Verifies the installation

.PARAMETER InstallDir
    Installation directory for binaries. Default: $env:LOCALAPPDATA\Conduit\bin

.PARAMETER ConduitHome
    Conduit data directory. Default: $env:USERPROFILE\.conduit

.PARAMETER SkipDeps
    Skip dependency installation

.PARAMETER NoService
    Skip Windows service setup

.EXAMPLE
    # Run with default settings
    .\install-windows.ps1

.EXAMPLE
    # Custom install directory
    .\install-windows.ps1 -InstallDir "C:\Conduit\bin"

.NOTES
    Requires Administrator privileges for some operations.
    Visit https://github.com/amlandas/Conduit-AI-Intelligence-Hub for documentation.
#>

[CmdletBinding()]
param(
    [string]$InstallDir = "$env:LOCALAPPDATA\Conduit\bin",
    [string]$ConduitHome = "$env:USERPROFILE\.conduit",
    [switch]$SkipDeps,
    [switch]$NoService,
    [switch]$Verbose
)

# Strict mode for better error handling
Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Colors for output
function Write-ColorOutput {
    param(
        [string]$Message,
        [string]$Color = "White",
        [string]$Prefix = ""
    )
    Write-Host "$Prefix$Message" -ForegroundColor $Color
}

function Write-Info { Write-ColorOutput -Message $args[0] -Color "Cyan" -Prefix "[i] " }
function Write-Success { Write-ColorOutput -Message $args[0] -Color "Green" -Prefix "[+] " }
function Write-Warn { Write-ColorOutput -Message $args[0] -Color "Yellow" -Prefix "[!] " }
function Write-Err { Write-ColorOutput -Message $args[0] -Color "Red" -Prefix "[-] " }

function Write-Banner {
    Write-Host ""
    Write-Host "================================================================" -ForegroundColor Blue
    Write-Host "               Conduit Installation for Windows                 " -ForegroundColor Blue
    Write-Host "                AI Intelligence Hub for MCP                     " -ForegroundColor Blue
    Write-Host "================================================================" -ForegroundColor Blue
    Write-Host ""
}

function Test-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}

function Test-CommandExists {
    param([string]$Command)
    $null -ne (Get-Command $Command -ErrorAction SilentlyContinue)
}

function Confirm-Action {
    param(
        [string]$Message,
        [bool]$Default = $true
    )
    $suffix = if ($Default) { "[Y/n]" } else { "[y/N]" }
    $response = Read-Host "$Message $suffix"
    if ([string]::IsNullOrWhiteSpace($response)) {
        return $Default
    }
    return $response -match "^[Yy]"
}

# Ensure Chocolatey is installed
function Install-Chocolatey {
    Write-Host ""
    Write-Host "Chocolatey Package Manager" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    if (Test-CommandExists "choco") {
        Write-Success "Chocolatey is installed"
        return $true
    }

    Write-Info "Chocolatey is not installed"
    Write-Host ""
    Write-Host "Chocolatey is the recommended package manager for Windows."
    Write-Host "It will be used to install dependencies."
    Write-Host ""

    if (-not (Confirm-Action "Install Chocolatey now?")) {
        Write-Warn "Some dependencies may need to be installed manually"
        return $false
    }

    if (-not (Test-Administrator)) {
        Write-Err "Administrator privileges required to install Chocolatey"
        Write-Host "Please run this script as Administrator"
        return $false
    }

    Write-Info "Installing Chocolatey..."

    try {
        Set-ExecutionPolicy Bypass -Scope Process -Force
        [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072
        Invoke-Expression ((New-Object System.Net.WebClient).DownloadString('https://community.chocolatey.org/install.ps1'))

        # Refresh environment
        $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
                    [System.Environment]::GetEnvironmentVariable("Path", "User")

        if (Test-CommandExists "choco") {
            Write-Success "Chocolatey installed successfully"
            return $true
        } else {
            Write-Err "Chocolatey installation may have failed"
            return $false
        }
    } catch {
        Write-Err "Failed to install Chocolatey: $_"
        Write-Host ""
        Write-Host "Install manually from: https://chocolatey.org/install"
        return $false
    }
}

# Check for winget as alternative
function Test-Winget {
    try {
        $null = winget --version
        return $true
    } catch {
        return $false
    }
}

# Install Git
function Install-Git {
    Write-Host ""
    Write-Host "Step 1: Git" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    if (Test-CommandExists "git") {
        Write-Success "Git is installed: $(git --version)"
        return $true
    }

    Write-Info "Git is not installed"

    if (-not (Confirm-Action "Install Git now?")) {
        Write-Err "Git is required. Please install manually."
        return $false
    }

    if (Test-CommandExists "choco") {
        Write-Info "Installing Git via Chocolatey..."
        choco install git -y
    } elseif (Test-Winget) {
        Write-Info "Installing Git via winget..."
        winget install --id Git.Git -e --source winget
    } else {
        Write-Err "No package manager available"
        Write-Host "Download Git from: https://git-scm.com/download/win"
        return $false
    }

    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
                [System.Environment]::GetEnvironmentVariable("Path", "User")

    if (Test-CommandExists "git") {
        Write-Success "Git installed successfully"
        return $true
    } else {
        Write-Warn "Git may require a terminal restart to be available"
        return $true
    }
}

# Install Go
function Install-Go {
    Write-Host ""
    Write-Host "Step 2: Go Programming Language" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    $minVersion = [Version]"1.21.0"

    if (Test-CommandExists "go") {
        $goVersion = (go version) -replace "go version go", "" -replace " windows.*", ""
        try {
            if ([Version]$goVersion -ge $minVersion) {
                Write-Success "Go $goVersion is installed"
                return $true
            } else {
                Write-Warn "Go $goVersion is installed but version 1.21+ is required"
            }
        } catch {
            Write-Success "Go is installed"
            return $true
        }
    } else {
        Write-Info "Go is not installed"
    }

    if (-not (Confirm-Action "Install Go now?")) {
        Write-Err "Go is required. Please install manually: https://go.dev/dl/"
        return $false
    }

    if (Test-CommandExists "choco") {
        Write-Info "Installing Go via Chocolatey..."
        choco install golang -y
    } elseif (Test-Winget) {
        Write-Info "Installing Go via winget..."
        winget install --id GoLang.Go -e --source winget
    } else {
        Write-Err "No package manager available"
        Write-Host "Download Go from: https://go.dev/dl/"
        return $false
    }

    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
                [System.Environment]::GetEnvironmentVariable("Path", "User")

    Write-Success "Go installed"
    return $true
}

# Build Conduit
function Build-Conduit {
    Write-Host ""
    Write-Host "Step 3: Build Conduit" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    $repoUrl = "https://github.com/amlandas/Conduit-AI-Intelligence-Hub.git"
    $buildDir = Join-Path $env:TEMP "conduit-build-$(Get-Random)"

    # Check if we're in the repo
    if ((Test-Path "go.mod") -and (Select-String -Path "go.mod" -Pattern "module conduit" -Quiet)) {
        Write-Info "Building from current directory..."
        $buildDir = Get-Location
    } else {
        Write-Info "Cloning Conduit repository..."
        git clone --depth 1 $repoUrl $buildDir
        Push-Location $buildDir
    }

    Write-Info "Building Conduit binaries..."

    # Create install directory
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    # Build with CGO (required for SQLite FTS5)
    $env:CGO_ENABLED = "1"

    # Try to get version from git
    $version = "dev"
    try {
        $version = git describe --tags --always 2>$null
    } catch {}

    # Build CLI
    go build -tags "fts5" -trimpath -ldflags "-X main.Version=$version" -o "$InstallDir\conduit.exe" .\cmd\conduit

    # Build daemon
    go build -tags "fts5" -trimpath -ldflags "-X main.Version=$version" -o "$InstallDir\conduit-daemon.exe" .\cmd\conduit-daemon

    # Cleanup if we cloned
    if ($buildDir -ne (Get-Location)) {
        Pop-Location
        Remove-Item -Recurse -Force $buildDir -ErrorAction SilentlyContinue
    }

    Write-Success "Built conduit.exe and conduit-daemon.exe"
    return $true
}

# Add to PATH
function Install-ToPath {
    Write-Host ""
    Write-Host "Step 4: Install to PATH" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    # Check if already in PATH
    $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($currentPath -split ";" -contains $InstallDir) {
        Write-Success "Install directory already in PATH"
        return $true
    }

    Write-Info "Adding $InstallDir to PATH..."

    $newPath = $currentPath + ";" + $InstallDir
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")

    # Update current session
    $env:Path = $env:Path + ";" + $InstallDir

    Write-Success "Added to PATH"
    Write-Warn "You may need to restart your terminal for changes to take effect"

    return $true
}

# Install Docker Desktop
function Install-Docker {
    Write-Host ""
    Write-Host "Step 5: Container Runtime" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    if (Test-CommandExists "docker") {
        try {
            docker info 2>$null | Out-Null
            Write-Success "Docker is installed and running"
            return $true
        } catch {
            Write-Warn "Docker is installed but not running"
            Write-Host "Please start Docker Desktop"
        }
    }

    Write-Info "Docker Desktop is recommended for running MCP servers"

    if (-not (Confirm-Action "Install Docker Desktop?")) {
        Write-Warn "Skipping Docker installation"
        return $true
    }

    if (Test-CommandExists "choco") {
        Write-Info "Installing Docker Desktop via Chocolatey..."
        choco install docker-desktop -y
    } elseif (Test-Winget) {
        Write-Info "Installing Docker Desktop via winget..."
        winget install --id Docker.DockerDesktop -e --source winget
    } else {
        Write-Host "Download Docker Desktop from: https://docker.com/products/docker-desktop"
        return $true
    }

    Write-Success "Docker Desktop installed"
    Write-Warn "Please start Docker Desktop and complete initial setup"

    return $true
}

# Install Ollama
function Install-Ollama {
    Write-Host ""
    Write-Host "Step 5a: AI Provider (Ollama)" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    if (Test-CommandExists "ollama") {
        Write-Success "Ollama is installed"

        # Check if running
        try {
            Invoke-RestMethod -Uri "http://localhost:11434/api/tags" -TimeoutSec 2 | Out-Null
            Write-Success "Ollama is running"
        } catch {
            Write-Warn "Ollama is installed but not running"
            Write-Host "Start with: ollama serve"
        }

        # Check for model
        $models = ollama list 2>$null
        if ($models -match "qwen2.5-coder") {
            Write-Success "AI model (qwen2.5-coder:7b) is installed"
        } else {
            if (Confirm-Action "Download AI model (qwen2.5-coder:7b, ~4.7GB)?") {
                Write-Info "Downloading model..."
                ollama pull qwen2.5-coder:7b
                Write-Success "Model downloaded"
            }
        }
        return $true
    }

    Write-Info "Ollama provides local AI capabilities"

    if (-not (Confirm-Action "Install Ollama?")) {
        Write-Warn "Skipping Ollama installation"
        return $true
    }

    if (Test-CommandExists "choco") {
        Write-Info "Installing Ollama via Chocolatey..."
        choco install ollama -y
    } elseif (Test-Winget) {
        Write-Info "Installing Ollama via winget..."
        winget install --id Ollama.Ollama -e --source winget
    } else {
        Write-Host "Download Ollama from: https://ollama.com/download/windows"
        return $true
    }

    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
                [System.Environment]::GetEnvironmentVariable("Path", "User")

    Write-Success "Ollama installed"

    if (Confirm-Action "Download AI model (qwen2.5-coder:7b, ~4.7GB)?") {
        Write-Info "Downloading model..."
        ollama pull qwen2.5-coder:7b
        Write-Success "Model downloaded"
    }

    return $true
}

# Install document extraction tools
function Install-DocumentTools {
    Write-Host ""
    Write-Host "Step 5b: Document Extraction Tools" -ForegroundColor White
    Write-Host "------------------------------------------------------------"
    Write-Host ""
    Write-Host "Conduit's Knowledge Base can index various document formats."
    Write-Host "Some formats require external tools for text extraction."
    Write-Host ""
    Write-Host "Formats and required tools:"
    Write-Host "  - PDF files (.pdf)     -> pdftotext (from poppler)"
    Write-Host "  - Word docs (.doc)     -> antiword"
    Write-Host "  - RTF files (.rtf)     -> unrtf or LibreOffice"
    Write-Host "  - DOCX/ODT files       -> No tools needed (native support)"
    Write-Host ""

    if (-not (Confirm-Action "Install document extraction tools?")) {
        Write-Warn "Skipping document tools. Some formats may not be indexable."
        return $true
    }

    if (-not (Test-CommandExists "choco")) {
        Write-Warn "Chocolatey required for document tools installation"
        Write-Host "Install manually:"
        Write-Host "  - poppler: https://github.com/oschwartz10612/poppler-windows/releases"
        Write-Host "  - antiword: Search for Windows build"
        return $true
    }

    # Install poppler (includes pdftotext)
    if (Test-CommandExists "pdftotext") {
        Write-Success "pdftotext: already installed"
    } else {
        Write-Info "Installing poppler (for PDF extraction)..."
        try {
            choco install poppler -y
            Write-Success "poppler installed"
        } catch {
            Write-Warn "poppler installation failed"
            Write-Host "Download from: https://github.com/oschwartz10612/poppler-windows/releases"
        }
    }

    # Install antiword (for .doc)
    if (Test-CommandExists "antiword") {
        Write-Success "antiword: already installed"
    } else {
        Write-Info "Installing antiword (for .doc extraction)..."
        try {
            choco install antiword -y
            Write-Success "antiword installed"
        } catch {
            Write-Warn "antiword installation failed (optional)"
        }
    }

    # Refresh PATH
    $env:Path = [System.Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
                [System.Environment]::GetEnvironmentVariable("Path", "User")

    return $true
}

# Create configuration
function New-Configuration {
    Write-Host ""
    Write-Host "Step 6: Configuration" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    if (-not (Test-Path $ConduitHome)) {
        New-Item -ItemType Directory -Path $ConduitHome -Force | Out-Null
    }

    $configFile = Join-Path $ConduitHome "conduit.yaml"

    if (Test-Path $configFile) {
        Write-Success "Configuration already exists: $configFile"
        return $true
    }

    $configContent = @"
# Conduit Configuration
# Generated by install-windows.ps1

# Data directory
data_dir: ~/.conduit

# Unix socket path (Windows uses named pipes internally)
socket: ~/.conduit/conduit.sock

# Logging
log_level: info
log_format: json

# AI Configuration
ai:
  provider: ollama
  model: qwen2.5-coder:7b
  endpoint: http://localhost:11434
  timeout_seconds: 120
  max_retries: 2
  confidence_threshold: 0.6

# Container runtime
runtime:
  preferred: docker

# Policy settings
policy:
  allow_network_egress: false
"@

    $configContent | Out-File -FilePath $configFile -Encoding UTF8

    Write-Success "Configuration created: $configFile"
    return $true
}

# Verify installation
function Test-Installation {
    Write-Host ""
    Write-Host "Step 7: Verification" -ForegroundColor White
    Write-Host "------------------------------------------------------------"

    $allGood = $true

    # Check binaries
    if (Test-Path "$InstallDir\conduit.exe") {
        Write-Success "conduit.exe: installed"
    } else {
        Write-Err "conduit.exe: not found"
        $allGood = $false
    }

    if (Test-Path "$InstallDir\conduit-daemon.exe") {
        Write-Success "conduit-daemon.exe: installed"
    } else {
        Write-Err "conduit-daemon.exe: not found"
        $allGood = $false
    }

    # Check Docker
    if (Test-CommandExists "docker") {
        try {
            docker info 2>$null | Out-Null
            Write-Success "Docker: running"
        } catch {
            Write-Warn "Docker: installed but not running"
        }
    } else {
        Write-Warn "Docker: not installed"
    }

    # Check Ollama
    if (Test-CommandExists "ollama") {
        try {
            Invoke-RestMethod -Uri "http://localhost:11434/api/tags" -TimeoutSec 2 | Out-Null
            Write-Success "Ollama: running"
        } catch {
            Write-Warn "Ollama: not running"
        }
    } else {
        Write-Warn "Ollama: not installed"
    }

    # Check document tools
    Write-Host ""
    Write-Host "Document Extraction Tools:" -ForegroundColor White
    if (Test-CommandExists "pdftotext") {
        Write-Success "  PDF:  pdftotext available"
    } else {
        Write-Warn "  PDF:  pdftotext not installed"
    }
    if (Test-CommandExists "antiword") {
        Write-Success "  DOC:  antiword available"
    } else {
        Write-Warn "  DOC:  antiword not installed"
    }
    Write-Success "  DOCX: native support"
    Write-Success "  ODT:  native support"

    Write-Host ""

    if ($allGood) {
        Write-Success "Installation verified!"
    } else {
        Write-Warn "Some components need attention"
    }

    return $allGood
}

# Print completion message
function Write-Completion {
    Write-Host ""
    Write-Host "================================================================" -ForegroundColor Green
    Write-Host "               Installation Complete!                           " -ForegroundColor Green
    Write-Host "================================================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "Conduit is now installed!"
    Write-Host ""
    Write-Host "Quick Start:" -ForegroundColor White
    Write-Host ""
    Write-Host "  1. Start the daemon:"
    Write-Host "     conduit-daemon --foreground"
    Write-Host ""
    Write-Host "  2. In another terminal, check status:"
    Write-Host "     conduit status"
    Write-Host ""
    Write-Host "  3. Run diagnostics:"
    Write-Host "     conduit doctor"
    Write-Host ""
    Write-Host "Documentation: https://github.com/amlandas/Conduit-AI-Intelligence-Hub"
    Write-Host ""
}

# Main installation flow
function Main {
    Write-Banner

    Write-Host "This script will install Conduit and its dependencies."
    Write-Host ""
    Write-Host "  Install directory:  $InstallDir"
    Write-Host "  Conduit home:       $ConduitHome"
    Write-Host ""

    if (-not (Confirm-Action "Proceed with installation?")) {
        Write-Host "Installation cancelled."
        exit 0
    }

    # Install Chocolatey first
    $chocoAvailable = Install-Chocolatey

    # Core dependencies
    if (-not (Install-Git)) { exit 1 }
    if (-not (Install-Go)) { exit 1 }

    # Build and install
    if (-not (Build-Conduit)) { exit 1 }
    if (-not (Install-ToPath)) { exit 1 }

    # Runtime dependencies
    if (-not $SkipDeps) {
        Install-Docker
        Install-Ollama
        Install-DocumentTools
    }

    # Configuration
    New-Configuration

    # Verify
    Test-Installation

    # Done
    Write-Completion
}

# Run main
Main
