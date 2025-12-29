#!/bin/bash

# Setup Git and GitHub repository for Conduit

set -e

echo "Setting up Git repository..."

# Create .gitignore if it doesn't exist
if [ ! -f ".gitignore" ]; then
    cat > .gitignore << 'GITIGNORE_EOF'
# Binaries
bin/
*.exe
*.exe~
*.dll
*.so
*.dylib

# Test binary, built with `go test -c`
*.test

# Output of the go coverage tool
*.out
coverage.txt
coverage.html

# Go workspace file
go.work

# IDE
.idea/
.vscode/
*.swp
*.swo
*~

# OS
.DS_Store
Thumbs.db

# Conduit runtime data
conduit.db
conduit.db-wal
conduit.db-shm
*.sock

# Logs
*.log
logs/

# Environment files
.env
.env.local
.env.*.local

# Credentials
*.key
*.pem
credentials.json
service-account*.json

# Build artifacts
artifacts/
dist/

# Temporary files
tmp/
temp/

# Git setup script (temporary)
setup-git.sh
GITIGNORE_EOF
    echo ".gitignore created"
fi

# Initialize git if not already initialized
if [ ! -d ".git" ]; then
    git init
    echo "Git repository initialized"
else
    echo "Git repository already exists"
fi

# Check if GitHub CLI is authenticated
if ! gh auth status >/dev/null 2>&1; then
    echo "ERROR: GitHub CLI not authenticated. Please run 'gh auth login' first."
    exit 1
fi

# Add all files
git add .

# Create initial commit
git commit -m "$(cat <<'EOF'
Initial commit: Conduit V0 - AI Intelligence Hub

Features:
- One-click installation with automated dependency setup
- Daemon service management (launchd/systemd)
- Container runtime abstraction (Podman/Docker)
- Policy engine with security controls
- Knowledge base with FTS5 search
- Client adapters (Claude Code, Cursor, VS Code, Gemini)
- Complete CLI for all operations

🤖 Generated with [Claude Code](https://claude.com/claude-code)

Co-Authored-By: Claude Sonnet 4.5 <noreply@anthropic.com>
EOF
)"

echo "Initial commit created"

# Create GitHub repository
echo "Creating GitHub repository..."
gh repo create conduit \
    --public \
    --source=. \
    --description="Local-first AI Intelligence Hub - Connect AI clients to MCP servers with security and control" \
    --remote=origin

echo "GitHub repository created"

# Push to GitHub
git push -u origin main

echo "Code pushed to GitHub"
echo "Repository URL: https://github.com/amlandas/conduit"
