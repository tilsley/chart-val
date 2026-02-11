# chart-val

_Your charts, validated before they deploy._

A GitHub App that automatically generates Helm chart diffs for pull requests using the GitHub Checks API.

## Features

- Automatically detects Helm chart changes in PRs
- Renders charts with environment-specific values
- Posts unified diffs as GitHub Check Runs
- Multi-environment support (staging, prod, etc.)
- Real Helm template rendering for accurate diffs
- **Argo CD integration**: Read chart configs from Argo Application manifests (see [docs/ARGO_INTEGRATION.md](docs/ARGO_INTEGRATION.md))

## Setup

### 1. Prerequisites

- Go 1.24+
- Helm CLI (for running the app and integration tests)
- A GitHub App with the following permissions:
  - **Checks**: Read & Write
  - **Contents**: Read
  - **Pull Requests**: Read

### 2. Configuration

**Step 1:** Place your GitHub App private key in the project root:

```bash
# Copy your GitHub App private key to the project root
cp /path/to/your/key.pem chart-val.pem
```

**Step 2:** Create a `.env` file with your GitHub App credentials:

```bash
# Copy the example file
cp .env.example .env

# Edit .env and add your real values:
# GITHUB_APP_ID=your-app-id
# GITHUB_INSTALLATION_ID=your-installation-id
# WEBHOOK_SECRET=your-webhook-secret
```

Both `chart-val.pem` and `.env` are gitignored and will NOT be committed.

### 3. Chart Configuration

**Option A: Repository Config File (Simple)**

Add a `.chart-val.yaml` file to the root of repositories you want to monitor:

```yaml
charts:
  - path: charts/my-app
    environments:
      - name: staging
        valueFiles:
          - env/staging-values.yaml
      - name: prod
        valueFiles:
          - env/prod-values.yaml
```

**Option B: Argo CD Integration (Advanced)**

For organizations with many Helm charts managed by Argo CD, chart-val can automatically discover charts from your Argo Application manifests:

```bash
# Add to .env
ARGO_APPS_REPO=https://github.com/myorg/gitops
ARGO_APPS_PATH=argocd/applications
ARGO_APPS_SYNC_INTERVAL=1h
```

Benefits:
- ✅ Single source of truth (Argo CD Applications)
- ✅ No duplicate configuration in chart repos
- ✅ Automatic discovery of new environments
- ✅ Fast indexed lookups (~1ms)
- ✅ Works with large repos (1000s of apps)

See [docs/ARGO_INTEGRATION.md](docs/ARGO_INTEGRATION.md) for details.

## Development

### Build & Run

```bash
# Build the binary
make build

# Run locally (requires .env configuration)
make run

# Clean build artifacts
make clean
```

### Testing

```bash
# Run all tests
make test

# Run tests with verbose output
make test-verbose

# Run integration tests only (requires helm)
make test-integration

# Regenerate integration test golden files
make test-integration-update
```

### Code Quality

```bash
# Format code
make fmt

# Run go vet
make vet

# Run all checks (fmt + vet)
make lint
```

### Manual Testing

You can test chart-val locally by simulating GitHub webhook requests:

#### 1. Ensure you have a `.env` file configured (see Configuration section)

#### 2. Build the CLI tool

```bash
make build-cli
```

#### 3. Run the server

**Terminal 1:**
```bash
make run
```

#### 4. Send a test webhook

**Terminal 2:**
```bash
./bin/chart-val-cli \
  -owner myorg \
  -repo myrepo \
  -pr 123 \
  -head feat/my-feature
```

Or with a specific SHA:
```bash
./bin/chart-val-cli \
  -owner myorg \
  -repo myrepo \
  -pr 123 \
  -head feat/my-feature \
  -sha abc123def456
```

The CLI tool will:
- Construct a valid GitHub `pull_request` webhook payload
- Sign it with HMAC SHA256 using the secret "test"
- POST it to `http://localhost:8080/webhook`
- Display the server's response

The server will then process the PR and post Check Runs to GitHub.

#### CLI Options

```bash
-owner string         GitHub repository owner (required)
-repo string          GitHub repository name (required)
-pr int               Pull request number (required)
-head string          Head branch name (required)
-sha string           Head commit SHA (default: dummy SHA for testing)
-base string          Base branch (default "main")
-action string        PR action (default "synchronize")
-url string           Webhook URL (default "http://localhost:8080/webhook")
-secret string        Webhook secret (must match your .env WEBHOOK_SECRET)
-installation-id int  GitHub App installation ID (from your .env)
```

## How It Works

1. **Webhook Reception**: Receives `pull_request` events from GitHub
2. **Config Loading**: Reads `.chart-val.yaml` from the repository
3. **Chart Fetching**: Downloads base (main) and head (PR) chart versions via GitHub API
4. **Rendering**: Runs `helm template` for each environment
5. **Diff Generation**: Computes unified diffs between rendered manifests
6. **Reporting**: Posts results as GitHub Check Runs (one per chart/environment)

## Architecture

Built using Hexagonal Architecture (Ports & Adapters):

- **Domain**: Pure business logic (`internal/diff/domain/`)
- **Application**: Use cases and orchestration (`internal/diff/app/`)
- **Ports**: Interfaces for I/O (`internal/diff/ports/`)
- **Adapters**: External integrations (`internal/diff/adapters/`)
  - `github_in`: Webhook handler
  - `github_out`: Check Run reporter
  - `helm_cli`: Helm renderer
  - `source_ctrl`: Chart file fetcher
  - `repo_cfg`: Manifest loader

## Integration Testing

The project includes end-to-end integration tests that use real Helm charts:

- **Test Location**: `internal/diff/app/integration_test.go`
- **Fixtures**: Sample charts in `testdata/{base,head}/my-app/`
- **Golden Files**: Expected outputs in `testdata/golden/*.md`

The golden files serve as both test assertions and living documentation of the tool's output format.

## License

MIT
