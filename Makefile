.PHONY: help build build-cli run test test-verbose test-integration test-integration-update test-e2e test-e2e-local clean fmt vet lint lint-go lint-fix lint-arch coverage

# Variables
BINARY_NAME=chart-val
CLI_BINARY_NAME=chart-val-cli
MAIN_PATH=./cmd/chart-val
CLI_MAIN_PATH=./cmd/chart-val-cli
BUILD_DIR=./bin

# GitHub App Configuration (MUST be set in .env for real deployments)
# These are placeholder values for testing only
export GITHUB_APP_ID ?= 123456
export GITHUB_INSTALLATION_ID ?= 789012
export WEBHOOK_SECRET ?= test-webhook-secret

# Load .env file if it exists (will override above defaults)
ifneq (,$(wildcard ./.env))
    include .env
    export
endif

help:
	@echo "chart-val - Makefile targets:"
	@echo ""
	@echo "Build & Run:"
	@echo "  make build              - Build the server binary to ./bin/chart-val"
	@echo "  make build-cli          - Build the CLI tool to ./bin/chart-val-cli"
	@echo "  make run                - Build and run the server locally (requires config)"
	@echo ""
	@echo "Testing:"
	@echo "  make test               - Run all tests"
	@echo "  make test-verbose       - Run all tests with verbose output"
	@echo "  make test-integration   - Run integration tests only (requires helm)"
	@echo "  make test-integration-update - Regenerate golden files for integration tests"
	@echo "  make test-e2e           - Run end-to-end tests with real GitHub PRs (requires E2E_TEST=true)"
	@echo "  make test-e2e-local     - Run E2E tests using Makefile config (just needs GITHUB_TOKEN)"
	@echo ""
	@echo "Code Quality:"
	@echo "  make fmt                - Auto-format code (gofmt, goimports, golines via golangci-lint)"
	@echo "  make vet                - Run go vet"
	@echo "  make lint               - Run all linters (fmt, vet, golangci-lint, go-arch-lint)"
	@echo "  make lint-go            - Run golangci-lint only"
	@echo "  make lint-fix           - Run golangci-lint with auto-fix (formatters + linter fixes)"
	@echo "  make lint-arch          - Run go-arch-lint (architecture validation)"
	@echo "  make coverage           - Run tests with coverage report"
	@echo ""
	@echo "Cleanup:"
	@echo "  make clean              - Remove build artifacts"
	@echo ""
	@echo "Manual Testing:"
	@echo "  1. Create .env file with your GitHub App credentials (see .env.example)"
	@echo "  2. Place GitHub App private key at: ./chart-val.pem"
	@echo "  3. Terminal 1: make run"
	@echo "  4. Terminal 2: make build-cli && ./bin/chart-val-cli https://github.com/owner/repo/pull/123"
	@echo ""
	@echo "E2E Testing (creates real PRs):"
	@echo "  1. Configure .env with your GitHub App credentials"
	@echo "  2. Place GitHub App private key at: ./chart-val.pem"
	@echo "  3. export GITHUB_TOKEN=ghp_your_token_here"
	@echo "  4. make test-e2e-local"

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
	@echo "Starting chart-val..."
	@if [ -z "$$GITHUB_APP_ID" ]; then \
		echo "Error: GITHUB_APP_ID not set. Create a .env file or export it."; \
		exit 1; \
	fi
	@if [ -z "$$WEBHOOK_SECRET" ]; then \
		echo "Error: WEBHOOK_SECRET not set."; \
		exit 1; \
	fi
	@if [ -z "$$GITHUB_PRIVATE_KEY" ] && [ ! -f chart-val.pem ]; then \
		echo "Error: GITHUB_PRIVATE_KEY not set and chart-val.pem not found."; \
		echo ""; \
		echo "Place your GitHub App private key at: ./chart-val.pem"; \
		echo "Or add to .env: GITHUB_PRIVATE_KEY=\"\$$(cat /path/to/your/key.pem)\""; \
		exit 1; \
	fi
	@echo "✓ Configuration loaded"
	@echo "  GitHub App ID: $$GITHUB_APP_ID"
	@echo "  Installation ID: $$GITHUB_INSTALLATION_ID"
	@echo "  Webhook Secret: $$WEBHOOK_SECRET"
	@echo "  Port: $${PORT:-8080}"
	@echo ""
	@if [ -z "$$GITHUB_PRIVATE_KEY" ] && [ -f chart-val.pem ]; then \
		export GITHUB_PRIVATE_KEY="$$(cat chart-val.pem)" && $(BUILD_DIR)/$(BINARY_NAME); \
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

test-e2e:
	@echo "Running E2E tests..."
	@if [ -z "$$GITHUB_TOKEN" ]; then \
		echo "Error: GITHUB_TOKEN not set. Required for E2E tests."; \
		echo "Create a personal access token and export it:"; \
		echo "  export GITHUB_TOKEN=ghp_your_token_here"; \
		exit 1; \
	fi
	@if [ "$$E2E_TEST" != "true" ]; then \
		echo "Error: E2E_TEST not set to 'true'."; \
		echo "Set it to enable E2E tests:"; \
		echo "  export E2E_TEST=true"; \
		exit 1; \
	fi
	@echo "✓ Running E2E tests against real GitHub..."
	go test ./test/e2e -v -timeout 5m

test-e2e-local:
	@echo "Running E2E tests with local configuration..."
	@if [ -z "$$GITHUB_TOKEN" ]; then \
		echo "Error: GITHUB_TOKEN not set. Required for E2E tests."; \
		echo "Create a personal access token and export it:"; \
		echo "  export GITHUB_TOKEN=ghp_your_token_here"; \
		exit 1; \
	fi
	@if [ -z "$$GITHUB_PRIVATE_KEY" ] && [ ! -f chart-val.pem ]; then \
		echo "Error: GITHUB_PRIVATE_KEY not set and chart-val.pem not found."; \
		echo ""; \
		echo "Place your GitHub App private key at: ./chart-val.pem"; \
		echo "Or add to .env: GITHUB_PRIVATE_KEY=\"\$$(cat /path/to/your/key.pem)\""; \
		exit 1; \
	fi
	@echo "✓ Configuration loaded"
	@echo "  GitHub App ID: $$GITHUB_APP_ID"
	@echo "  Installation ID: $$GITHUB_INSTALLATION_ID"
	@echo "  Webhook Secret: $$WEBHOOK_SECRET"
	@echo ""
	@if [ -z "$$GITHUB_PRIVATE_KEY" ] && [ -f chart-val.pem ]; then \
		export GITHUB_PRIVATE_KEY="$$(cat chart-val.pem)" && E2E_TEST=true go test ./test/e2e -v -timeout 5m; \
	else \
		E2E_TEST=true go test ./test/e2e -v -timeout 5m; \
	fi

fmt:
	@echo "Formatting code (gofmt, goimports, golines)..."
	@which golangci-lint > /dev/null || (echo "Error: golangci-lint not installed. Install with: brew install golangci-lint"; exit 1)
	golangci-lint fmt ./...
	@echo "✓ Code formatted"

vet:
	go vet ./...

lint-go:
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "Error: golangci-lint not installed. Install with: brew install golangci-lint"; exit 1)
	golangci-lint run ./...
	@echo "✓ golangci-lint passed"

lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	@which golangci-lint > /dev/null || (echo "Error: golangci-lint not installed. Install with: brew install golangci-lint"; exit 1)
	golangci-lint run --fix ./...
	@echo "✓ Auto-fix complete"

lint-arch:
	@echo "Running go-arch-lint (hexagonal architecture validation)..."
	@which go-arch-lint > /dev/null || (echo "Error: go-arch-lint not installed. Install with: go install github.com/fe3dback/go-arch-lint@latest"; exit 1)
	go-arch-lint check
	@echo "✓ Architecture validation passed"

lint:
	@echo "Running all linters..."
	@echo ""
	@echo "==> Running go vet..."
	@go vet ./...
	@echo "✓ go vet passed"
	@echo ""
	@echo "==> Running golangci-lint..."
	@$(MAKE) lint-go
	@echo ""
	@echo "==> Running architecture validation..."
	@$(MAKE) lint-arch
	@echo ""
	@echo "✓ All linters passed"

coverage:
	@echo "Running tests with coverage..."
	@go test ./... -coverprofile=coverage.out
	@echo ""
	@echo "Coverage by package:"
	@go tool cover -func=coverage.out | grep -v "no test files"
	@echo ""
	@echo "Total coverage:"
	@go tool cover -func=coverage.out | grep total:
	@echo ""
	@echo "To view HTML coverage report: go tool cover -html=coverage.out"

clean:
	@rm -rf $(BUILD_DIR)
	@echo "✓ Cleaned build artifacts"
