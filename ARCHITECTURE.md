# Architecture

chart-val follows **Hexagonal Architecture** (Ports & Adapters). The core business logic has no knowledge of GitHub, Helm, or any external system — it depends only on port interfaces.

## Layers

```
cmd/chart-val/          Composition root — wires adapters to ports
internal/platform/      Cross-cutting: config, telemetry
internal/diff/domain/   Business types (PRContext, DiffResult, ChartConfig)
internal/diff/ports/    Interfaces (driving + driven)
internal/diff/app/      Use-case orchestration (DiffService)
internal/diff/adapters/ Implementations of ports
```

## Ports

### Driving (Input)

| Port | Description |
|------|-------------|
| `DiffUseCase` | Entry point — receives a `PRContext`, runs the full diff flow |

### Driven (Output)

| Port | Adapter(s) | Description |
|------|-----------|-------------|
| `ChangedChartsPort` | `pr_files` | Detects which charts changed in a PR via GitHub API |
| `ReportingPort` | `github_out` | Creates Check Runs and posts PR comments |
| `EnvironmentConfigPort` | `environment_config/argo`, `environment_config/filesystem` | Discovers environments and value files |
| `SourceControlPort` | `source_ctrl` | Fetches chart files from GitHub at a given ref |
| `RendererPort` | `helm_cli` | Runs `helm template` |
| `DiffPort` | `dyff_diff`, `line_diff` | Computes diffs between rendered manifests |

## Execution Flow

Per pull request event, `DiffService.Execute()` calls ports in this order:

```
① ChangedChartsPort.GetChangedCharts()     — which charts changed?
② ReportingPort.CreateInProgressCheck()     — open a check run
③ EnvironmentConfigPort.GetEnvironmentConfig() — per chart: what envs/values?
④ SourceControlPort.FetchChartFiles()       — per env: fetch base + head files
⑤ RendererPort.Render()                     — per env: helm template
⑥ DiffPort.ComputeDiff()                    — per env: compute diff
⑦ ReportingPort.UpdateCheckWithResults()    — post results
   ReportingPort.PostComment()
```

Steps ③–⑥ repeat per chart and per environment.

## Dependency Rules

| Layer | May Import |
|-------|-----------|
| Domain | stdlib only |
| Ports | Domain |
| App | Domain, Ports |
| Adapters | Domain, Ports, Platform, external libs |
| Cmd | Everything (composition root) |

**Forbidden:** Domain → Adapters, Ports → Adapters, App → Adapters, Adapter → Adapter.

Enforced by [go-arch-lint](https://github.com/fe3dback/go-arch-lint) — see `.go-arch-lint.yml`.

## Composite Strategy: Environment Config

The service uses a fallback chain to resolve environment configuration:

1. **Argo CD adapter** — queries Argo Application manifests for chart deployments
2. **Filesystem adapter** — scans the chart's `env/` directory for value files
3. **Default** — returns a "base" environment (chart is not deployed)

This logic lives in `service.go:getChartConfig()`.

## Diffing Strategy

Two `DiffPort` implementations are composed:

1. **dyff** (primary) — Kubernetes-aware semantic YAML diff via the [dyff CLI](https://github.com/homeport/dyff)
2. **line_diff** (fallback) — traditional unified text diff via go-difflib

The service tries dyff first and falls back to line_diff if dyff is unavailable or fails.

## Code Quality

```bash
make lint           # golangci-lint + go-arch-lint + fmt + vet
make test           # All unit + integration tests
make lint-fix       # Auto-fix lint issues where possible
```

## Observability

OpenTelemetry metrics and traces are available when `OTEL_ENABLED=true`. The OTel SDK auto-discovers standard environment variables (`OTEL_EXPORTER_OTLP_ENDPOINT`, etc.). See [.env.example](.env.example).
