# AGENTS.md

Instructions for AI coding agents working on this codebase.

## Project Overview

chart-val is a GitHub App (Go) that generates Helm chart diffs for pull requests. It uses Hexagonal Architecture (Ports & Adapters).

## Architecture Rules (STRICT)

These dependency rules are enforced by `go-arch-lint` and will fail CI:

- **Domain** (`internal/diff/domain/`) — pure business types, stdlib only. No imports from adapters, ports, platform, or external libs.
- **Ports** (`internal/diff/ports/`) — interfaces only. May import domain. Never import adapters.
- **App** (`internal/diff/app/`) — use-case orchestration. May import domain and ports. Never import adapters directly.
- **Adapters** (`internal/diff/adapters/`) — implement ports. May import domain, ports, platform. **Never import other adapters** (adapters are independent).
- **Platform** (`internal/platform/`) — cross-cutting concerns (config, telemetry). May import domain.
- **Cmd** (`cmd/chart-val/`) — composition root, wires everything together.

## Project Structure

```
cmd/chart-val/              Entry point + dependency injection (container.go)
internal/
  diff/
    domain/                 Business types: PRContext, DiffResult, ChartConfig, ChangedChart
    ports/                  Interfaces: inputs.go (DiffUseCase), outputs.go (6 driven ports)
    app/                    DiffService — orchestrates the diff workflow
    adapters/
      github_in/            Driving: webhook handler
      github_out/           Driven: Check Runs + PR comments
      pr_files/             Driven: detect changed charts via GitHub API
      environment_config/
        argo/               Driven: env config from Argo CD Application manifests
        filesystem/         Driven: env config from chart's env/ directory
      source_ctrl/          Driven: fetch chart files from GitHub
      helm_cli/             Driven: helm template rendering
      dyff_diff/            Driven: semantic YAML diff (dyff CLI)
      line_diff/            Driven: line-based unified diff (go-difflib)
  platform/
    config/                 Env var loading with defaults
    telemetry/              OpenTelemetry setup
```

## Development Commands

```bash
make build                  # Build server binary to ./bin/chart-val
make build-cli              # Build webhook simulator to ./bin/chart-val-cli
make run                    # Run server locally (loads .env)
make test                   # Run all tests
make test-integration       # Integration tests (requires helm CLI)
make test-integration-update # Regenerate golden files
make lint                   # All linters: fmt + vet + golangci-lint + go-arch-lint
make lint-fix               # Auto-fix lint issues
```

## Key Conventions

### Testing
- **Unit tests**: Table-driven tests with `t.Run()` subtests.
- **Integration tests**: Use real Helm charts in `testdata/` directories. Golden file pattern with `-update` flag.
- **Mocks**: Define mock structs inline in test files. No mock generation libraries.

### Configuration
- All configuration via environment variables with sensible defaults.
- `internal/platform/config/config.go` loads all env vars using `getEnvOrDefault()`.
- Configurable values: `APP_NAME`, `APP_URL`, `CHART_DIR`, `ENV_DIR`, `VALUES_FILE_SUFFIX`.
- See `.env.example` for the complete list.

### Error Handling
- Domain errors in `internal/diff/domain/errors.go` (sentinel errors with `errors.New`).
- Adapters wrap errors with context using `fmt.Errorf("doing X: %w", err)`.
- The service continues processing remaining charts/environments on error rather than failing fast.

### Observability
- OpenTelemetry for metrics and traces (opt-in via `OTEL_ENABLED=true`).
- Metric instruments are created once in the constructor and reused.
- Spans wrap key operations in the service layer.

### Code Style
- `golangci-lint` v2 with config in `.golangci.yml`.
- `golines` for line length formatting (130 char limit).
- No magic numbers — use named constants or config values.

## Adding a New Adapter

1. Create a new directory under `internal/diff/adapters/<name>/`.
2. Implement the relevant port interface from `internal/diff/ports/outputs.go`.
3. Accept dependencies (logger, config values) via constructor parameters.
4. Wire it in `cmd/chart-val/container.go`.
5. Run `make lint` to verify architecture rules pass.

## Adding a New Port

1. Add the interface to `internal/diff/ports/outputs.go` (or `inputs.go` for driving ports).
2. Create the adapter implementation under `internal/diff/adapters/`.
3. Add the port field to `DiffService` in `internal/diff/app/service.go`.
4. Update the constructor `NewDiffService()`.
5. Wire it in `cmd/chart-val/container.go`.
6. Update `.go-arch-lint.yml` if new packages are added.

## Common Gotchas

- The composite environment config strategy (Argo -> Filesystem -> Default) lives in `service.go:getChartConfig()`, not in a separate adapter.
- The diffing fallback (dyff -> line_diff) is handled in `service.go:computeDiff()`.
- `FormatCheckRunMarkdown()` and `FormatPRComment()` are methods on the `github_out` adapter, not standalone functions.
- Chart directory convention (`charts/`) is configurable via `CHART_DIR` — don't hardcode it.
