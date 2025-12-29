# Conduit Makefile
# Build configuration

BINARY_NAME=conduit
DAEMON_NAME=conduit-daemon
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

# Platforms
PLATFORMS=darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: all build build-cli build-daemon clean test test-critical test-high test-medium test-all lint fmt deps install help

all: build

## Build targets

build: build-cli build-daemon ## Build CLI and daemon
	@echo "Build complete: $(BIN_DIR)/"

build-cli: deps ## Build CLI binary
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOTAGS) $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(BINARY_NAME) ./$(CMD_DIR)/$(BINARY_NAME)
	@echo "Built: $(BIN_DIR)/$(BINARY_NAME)"

build-daemon: deps ## Build daemon binary
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=$(CGO_ENABLED) $(GO) build $(GOTAGS) $(GOFLAGS) $(LDFLAGS) -o $(BIN_DIR)/$(DAEMON_NAME) ./$(CMD_DIR)/$(DAEMON_NAME)
	@echo "Built: $(BIN_DIR)/$(DAEMON_NAME)"

build-all-platforms: deps ## Build for all platforms
	@for platform in $(PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GO) build $(GOFLAGS) $(LDFLAGS) \
			-o $(BIN_DIR)/$(BINARY_NAME)-$${platform%/*}-$${platform#*/}$$(if [ "$${platform%/*}" = "windows" ]; then echo ".exe"; fi) \
			./$(CMD_DIR)/$(BINARY_NAME); \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} \
		$(GO) build $(GOFLAGS) $(LDFLAGS) \
			-o $(BIN_DIR)/$(DAEMON_NAME)-$${platform%/*}-$${platform#*/}$$(if [ "$${platform%/*}" = "windows" ]; then echo ".exe"; fi) \
			./$(CMD_DIR)/$(DAEMON_NAME); \
	done
	@echo "Built all platforms in $(BIN_DIR)/"

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
	cp $(BIN_DIR)/$(DAEMON_NAME) $(GOPATH)/bin/
	@echo "Installed to $(GOPATH)/bin/"

install-local: build ## Install to /usr/local/bin (requires sudo)
	sudo cp $(BIN_DIR)/$(BINARY_NAME) /usr/local/bin/
	sudo cp $(BIN_DIR)/$(DAEMON_NAME) /usr/local/bin/
	@echo "Installed to /usr/local/bin/"

## Cleanup targets

clean: ## Clean build artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
	$(GO) clean -cache
	@echo "Cleaned"

## Development helpers

run-daemon: build-daemon ## Run daemon in foreground
	./$(BIN_DIR)/$(DAEMON_NAME) --log-level=debug

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
