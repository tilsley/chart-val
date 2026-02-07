.PHONY: help build build-cli run test test-verbose test-integration test-integration-update clean fmt vet lint

# Variables
BINARY_NAME=chart-sentry
CLI_BINARY_NAME=chart-sentry-cli
MAIN_PATH=./cmd/chart-sentry
CLI_MAIN_PATH=./cmd/chart-sentry-cli
BUILD_DIR=./bin

# GitHub App Configuration (can be overridden by .env)
export GITHUB_APP_ID ?= 2814878
export GITHUB_INSTALLATION_ID ?= 108584464
export WEBHOOK_SECRET ?= test

# Load .env file if it exists (will override above defaults)
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

help:
	@echo "chart-sentry - Makefile targets:"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build              - Build the server binary to ./bin/chart-sentry"
	@echo "  make build-cli          - Build the CLI tool to ./bin/chart-sentry-cli"
	@echo "  make run                - Build and run the server locally (requires config)"
	@echo ""
	@echo "Testing:"
	@echo "  make test               - Run all tests"
	@echo "  make test-verbose       - Run all tests with verbose output"
	@echo "  make test-integration   - Run integration tests only (requires helm)"
	@echo "  make test-integration-update - Regenerate golden files for integration tests"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt                - Format code with gofmt"
	@echo "  make vet                - Run go vet"
	@echo "  make lint               - Run go fmt/vet checks"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean              - Remove build artifacts"
	@echo ""
	@echo "Manual Testing (all config in Makefile, just add chart-sentry.pem):"
	@echo "  1. Place GitHub App private key at: ./chart-sentry.pem"
	@echo "  2. Terminal 1: make run"
	@echo "  3. Terminal 2: make build-cli && ./bin/chart-sentry-cli https://github.com/owner/repo/pull/123"

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "✓ Built: $(BUILD_DIR)/$(BINARY_NAME)"

build-cli:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(CLI_BINARY_NAME) $(CLI_MAIN_PATH)
	@echo "✓ Built: $(BUILD_DIR)/$(CLI_BINARY_NAME)"
	@echo ""
	@echo "Usage:"
	@echo "  $(BUILD_DIR)/$(CLI_BINARY_NAME) https://github.com/owner/repo/pull/123"

run: build
	@echo "Starting chart-sentry..."
	@if [ -z "$$GITHUB_APP_ID" ]; then \
		echo "Error: GITHUB_APP_ID not set. Create a .env file or export it."; \
		exit 1; \
	fi
	@if [ -z "$$WEBHOOK_SECRET" ]; then \
		echo "Error: WEBHOOK_SECRET not set."; \
		exit 1; \
	fi
	@if [ -z "$$GITHUB_PRIVATE_KEY" ] && [ ! -f chart-sentry.pem ]; then \
		echo "Error: GITHUB_PRIVATE_KEY not set and chart-sentry.pem not found."; \
		echo ""; \
		echo "Place your GitHub App private key at: ./chart-sentry.pem"; \
		echo "Or add to .env: GITHUB_PRIVATE_KEY=\"\$$(cat /path/to/your/key.pem)\""; \
		exit 1; \
	fi
	@echo "✓ Configuration loaded"
	@echo "  GitHub App ID: $$GITHUB_APP_ID"
	@echo "  Installation ID: $$GITHUB_INSTALLATION_ID"
	@echo "  Webhook Secret: $$WEBHOOK_SECRET"
	@echo "  Port: $${PORT:-8080}"
	@echo ""
	@if [ -z "$$GITHUB_PRIVATE_KEY" ] && [ -f chart-sentry.pem ]; then \
		export GITHUB_PRIVATE_KEY="$$(cat chart-sentry.pem)" && $(BUILD_DIR)/$(BINARY_NAME); \
	else \
		$(BUILD_DIR)/$(BINARY_NAME); \
	fi

test:
	go test ./...

test-verbose:
	go test ./... -v

test-integration:
	go test ./internal/diff/app/ -run Integration -v

test-integration-update:
	go test ./internal/diff/app/ -run Integration -update -v

fmt:
	gofmt -w -s ./cmd ./internal

lint:
	@echo "Running gofmt check..."
	@! gofmt -l -s ./cmd ./internal | grep -q . || (echo "Code needs formatting. Run: make fmt"; exit 1)
	@echo "Running go vet..."
	go vet ./...
	@echo "✓ All checks passed"

vet:
	go vet ./...

clean:
	@rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned build artifacts"
