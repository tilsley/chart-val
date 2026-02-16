# chart-val

_Your charts, validated before they deploy._

A GitHub App that generates Helm chart diffs for pull requests. When a PR modifies files under `charts/`, chart-val renders each chart per environment, computes diffs against `main`, and posts the results as a GitHub Check Run and PR comment.

## Architecture

Built with Hexagonal Architecture (Ports & Adapters). See [ARCHITECTURE.md](ARCHITECTURE.md) for full details.

<!-- TODO: embed docs/architecture/system-architecture diagram here -->

## Setup

### Prerequisites

- Go 1.24+
- Helm CLI
- A GitHub App with permissions: **Checks** (R/W), **Contents** (R), **Pull Requests** (R/W)

### Configuration

```bash
cp /path/to/your/key.pem chart-val.pem   # GitHub App private key
cp .env.example .env                      # Edit with your credentials
```

Required env vars: `GITHUB_APP_ID`, `GITHUB_INSTALLATION_ID`, `WEBHOOK_SECRET`. See [.env.example](.env.example) for all options including Argo CD integration and OpenTelemetry.

## Development

```bash
make build        # Build server binary
make run          # Run locally (loads .env)
make test         # Run all tests
make lint         # Format + vet + golangci-lint + go-arch-lint
```

### Manual Testing

```bash
make build-cli    # Build webhook simulator
make run          # Terminal 1: start server
./bin/chart-val-cli -owner myorg -repo myrepo -pr 123 -head feat/branch  # Terminal 2
```

See `./bin/chart-val-cli -help` for all CLI options.

### Integration & E2E Tests

```bash
make test-integration         # Run integration tests (requires helm)
make test-integration-update  # Regenerate golden files
```

See [test/e2e/](test/e2e/) for end-to-end test documentation.

## How It Works

1. Receives `pull_request` webhook from GitHub
2. Detects changed charts via the GitHub API
3. Discovers environments per chart (Argo CD apps or `env/` directory scan)
4. Fetches base and head chart files from GitHub
5. Renders each environment with `helm template`
6. Computes diffs (dyff for semantic YAML, line-diff fallback)
7. Posts results as a Check Run and PR comment

## Configuration Options

| Category | Env Var | Default | Description |
|----------|---------|---------|-------------|
| App Identity | `APP_NAME` | `chart-val` | Check run name, comment marker, OTel service |
| | `APP_URL` | _(empty)_ | Footer link in PR comments |
| Chart Layout | `CHART_DIR` | `charts` | Top-level chart directory |
| | `ENV_DIR` | `env` | Environment overrides subdirectory |
| | `VALUES_FILE_SUFFIX` | `-values.yaml` | Value file pattern |
| Argo CD | `ARGO_APPS_REPO` | _(disabled)_ | Git repo with Argo Application manifests |
| Observability | `OTEL_ENABLED` | `false` | Enable OpenTelemetry metrics/traces |

See [.env.example](.env.example) for the complete list.

## License

MIT
