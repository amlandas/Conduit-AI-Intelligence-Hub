# Conduit Makefile
# Build configuration

BINARY_NAME=conduit
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

GO=go
GOFLAGS=-trimpath
# Enable FTS5 for full-text search in SQLite
CGO_ENABLED=1
GOTAGS=-tags "fts5"

# Directories
BIN_DIR=bin
CMD_DIR=cmd
INTERNAL_DIR=internal

.PHONY: all build build-cli clean test test-critical test-high test-medium test-all lint fmt deps install help

all: build

## Build targets

# WP-3.2 deleted the daemon. There is exactly one binary target now, and
# `build` is an alias for it rather than a fan-out over two.
build: build-cli ## Build the conduit binary
	@echo "Build complete: $(BIN_DIR)/"

build-cli: deps ## Build CLI binary
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOTAGS) $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./$(CMD_DIR)/$(BINARY_NAME)
	@echo "Built: $(BIN_DIR)/$(BINARY_NAME)"

# NOTE: the `build-all-platforms` cross-compile target was removed deliberately.
#
# Conduit requires CGO (github.com/mattn/go-sqlite3) plus -tags "fts5" for
# SQLite full-text search. Setting GOOS/GOARCH implicitly disables CGO, so the
# old target built "successfully" while linking a go-sqlite3 stub whose every
# call fails at runtime with:
#   "Binary was compiled with 'CGO_ENABLED=0', go-sqlite3 requires cgo to work."
# Those binaries could not open the knowledge base at all.
#
# Forcing CGO_ENABLED=1 does not fix it either: cross-compiling cgo needs a C
# cross-toolchain per target (musl/gcc for linux, mingw-w64 for windows), which
# a single host does not have. Release artifacts must therefore be built
# natively on each platform - see .github/workflows/ci.yml for the per-OS
# matrix and .github/workflows/release.yml for packaging.

## Test targets

test: ## Run unit tests
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test $(GOTAGS) -v -race ./...

test-critical: ## Run critical tests (blocks deployment)
	$(GO) test -v -tags=integration ./tests/critical/...

test-high: ## Run high-priority tests
	$(GO) test -v -tags=integration ./tests/high/...

test-medium: ## Run medium-priority tests
	$(GO) test -v ./tests/medium/...

test-all: test test-critical test-high test-medium ## Run all tests

test-cover: ## Run tests with coverage
	CGO_ENABLED=$(CGO_ENABLED) $(GO) test $(GOTAGS) -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## Quality targets

lint: ## Run linter
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
		exit 1; \
	fi

fmt: ## Format code
	$(GO) fmt ./...
	@echo "Code formatted"

vet: ## Run go vet
	$(GO) vet ./...

## Dependency targets

deps: ## Download dependencies
	$(GO) mod download
	$(GO) mod tidy

## Install targets

install: build ## Install to GOPATH/bin
	cp $(BIN_DIR)/$(BINARY_NAME) $(GOPATH)/bin/
	@echo "Installed to $(GOPATH)/bin/"

install-local: build ## Install to /usr/local/bin (requires sudo)
	sudo cp $(BIN_DIR)/$(BINARY_NAME) /usr/local/bin/
	@echo "Installed to /usr/local/bin/"

## Cleanup targets

clean: ## Clean build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
	$(GO) clean -cache
	@echo "Cleaned"

## Development helpers

run-mcp: build-cli ## Run the KB MCP server over stdio
	./$(BIN_DIR)/$(BINARY_NAME) mcp kb

dev: build ## Build and run CLI help
	./$(BIN_DIR)/$(BINARY_NAME) --help

## Help

help: ## Show this help
	@echo "Conduit Build System"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'
