# Chart Configuration Architecture

This document explains how chart-val determines what charts to validate and which environments/value files to use.

## Overview

Chart-val uses a **composite strategy** to determine chart environments:

1. **Argo CD Apps** (source of truth when available)
2. **Discovered from chart's `env/` directory** (fallback for new charts)
3. **Base chart** (no deployments - used as base/library chart)

## Architecture

```
┌─────────────────────────────────────────┐
│       ChartConfigPort (interface)       │
└─────────────────────────────────────────┘
                    ▲
                    │
        ┌───────────┴───────────┐
        │                       │
┌───────┴────────┐    ┌────────┴────────┐
│  Argo Apps     │    │   Discovery     │
│   Adapter      │    │    Adapter      │
└────────────────┘    └─────────────────┘
        │                       │
        └───────────┬───────────┘
                    │
        ┌───────────┴────────────┐
        │   Composite Adapter    │
        │                        │
        │  1. Try Argo           │
        │  2. Fallback Discovery │
        │  3. Base chart         │
        └────────────────────────┘
```

## 1. Argo Apps Adapter

**When to use**: When `ARGO_APPS_REPO` is configured

**How it works**:
- Clones and syncs an Argo apps repository
- Scans for Argo Application manifests
- Extracts chart name and environment from **folder structure**
- Gets value files from `spec.source.helm.valueFiles`

**Folder structure**:
```
apps/
  my-app/           ← Chart name
    dev/            ← Environment name
      application.yaml
    prod/           ← Environment name
      application.yaml
```

**Configuration**:
- `ARGO_APPS_REPO`: Git repo URL (e.g., `https://github.com/myorg/gitops`)
- `ARGO_APPS_LOCAL_PATH`: Local clone path (default: `/tmp/chart-val-argocd`)
- `ARGO_APPS_SYNC_INTERVAL`: How often to sync (default: `1h`)
- `ARGO_APPS_FOLDER_PATTERN`: Folder structure pattern (default: `{chartName}/{envName}`)

**Example Application manifest** (OCI chart):
```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app-prod
spec:
  source:
    chart: my-app              # Chart name
    repoURL: oci://ghcr.io/myorg/charts
    targetRevision: 1.0.0
    helm:
      valueFiles:
        - env/prod-values.yaml # Value files to apply
```

**Result**:
- Chart: `my-app`
- Environment: `prod` (from folder structure)
- Value files: `[env/prod-values.yaml]`

## 2. Discovery Adapter

**When to use**: Fallback when no Argo apps found for a chart

**How it works**:
- Scans the chart's `env/` directory in the PR
- For each `env/{name}-values.yaml` file:
  - Environment name: `{name}`
  - Value files: `[env/{name}-values.yaml]`

**Example chart structure**:
```
charts/my-app/
  Chart.yaml
  values.yaml
  env/
    dev-values.yaml    → Environment: "dev"
    prod-values.yaml   → Environment: "prod"
  templates/
    deployment.yaml
```

**Result**:
- Chart: `my-app`
- Environments:
  - `dev` with value files `[env/dev-values.yaml]`
  - `prod` with value files `[env/prod-values.yaml]`

## 3. Base Chart (No Deployments)

**When to use**: No Argo apps and no `env/*.yaml` files found

**How it works**:
- Returns a special "base" environment with a message
- No helm rendering is performed
- Used for library/base charts not deployed directly

**Result**:
- Chart: `my-app`
- Environment: `base`
- Message: "This chart is not deployed (may be used as a base chart)"
- Status: Success (no validation errors)

## Composite Strategy Flow

```
For each changed chart:

1. Try Argo Apps Adapter
   ├─ Found apps? → Use Argo environments ✓
   └─ Not found → Continue to step 2

2. Try Discovery Adapter
   ├─ Found env/*.yaml? → Use discovered environments ✓
   └─ Not found → Continue to step 3

3. Base Chart
   └─ Return "base" environment with message ✓
```

## Configuration Examples

### With Argo Apps (Recommended)

```bash
# .env
ARGO_APPS_REPO=https://github.com/myorg/gitops
ARGO_APPS_FOLDER_PATTERN={chartName}/{envName}
ARGO_APPS_SYNC_INTERVAL=30m
```

**Result**: Uses Argo as source of truth, falls back to discovery for new charts

### Without Argo Apps

```bash
# .env
# (No ARGO_APPS_REPO configured)
```

**Result**: Always uses discovery adapter (scans `env/` directory)

## Custom Folder Patterns

The folder pattern can be customized to match your repository structure:

| Pattern | Example Path | Chart | Env |
|---------|-------------|-------|-----|
| `{chartName}/{envName}` | `my-app/prod/app.yaml` | `my-app` | `prod` |
| `apps/{chartName}/{envName}` | `apps/my-app/dev/app.yaml` | `my-app` | `dev` |
| `{envName}/{chartName}` | `prod/my-app/app.yaml` | `my-app` | `prod` |

**Note**: The pattern must contain both `{chartName}` and `{envName}` placeholders.

## Value Files

Value files are **always** relative to the chart directory:

- ✅ `env/prod-values.yaml`
- ✅ `env/prod-values.yaml` + `env/prod-secrets.yaml`
- ❌ `prod-values.yaml` (wrong - must be in `env/` folder)

Helm applies value files left-to-right (later files override earlier ones):
```yaml
valueFiles:
  - env/prod-values.yaml      # Base prod config
  - env/prod-secrets.yaml     # Secrets override
```

## Validation Behavior

| Scenario | Argo Apps | Discovery | Result |
|----------|-----------|-----------|--------|
| **Deployed chart** | Found | - | Validates using Argo environments |
| **New chart** | Not found | Found env files | Validates using discovered environments |
| **Base chart** | Not found | No env files | Shows "not deployed" message, no errors |
| **Legacy chart** | Found | - | Validates using Argo (even if no env files) |

## Implementation Details

### Composite Adapter

```go
type CompositeChartConfig struct {
    argoApps  ports.ChartConfigPort
    discovery ports.ChartConfigPort
    logger    *slog.Logger
}

func (c *CompositeChartConfig) GetChartConfig(ctx, pr, chartName) {
    // Try Argo first
    config := c.argoApps.GetChartConfig(ctx, pr, chartName)
    if len(config.Environments) > 0 {
        return config  // Found in Argo
    }

    // Fall back to discovery
    config = c.discovery.GetChartConfig(ctx, pr, chartName)
    if len(config.Environments) > 0 {
        return config  // Found in env/ directory
    }

    // Base chart (not deployed)
    return ChartConfig{
        Environments: [{
            Name: "base",
            Message: "This chart is not deployed (may be used as a base chart)",
        }],
    }
}
```

### Service Layer Handling

When an environment has a `Message` but no `ValueFiles`:
- Skips helm rendering
- Returns success result with the message
- No validation errors

```go
if env.Message != "" && len(env.ValueFiles) == 0 {
    return DiffResult{
        Status:  StatusSuccess,
        Summary: env.Message,
    }
}
```

## Migration Guide

### From Git-based Charts to OCI Charts

The Argo adapter supports both:

**Git-based** (old):
```yaml
source:
  repoURL: https://github.com/myorg/charts
  path: charts/my-app
```

**OCI** (new):
```yaml
source:
  repoURL: oci://ghcr.io/myorg/charts
  chart: my-app
```

Both work seamlessly - the adapter prefers `chart` but falls back to `path`.

### From Hardcoded Defaults to Discovery

**Before**: Hardcoded `values-dev.yaml`, `values-staging.yaml`, `values-prod.yaml`

**After**: Discovers from `env/` directory automatically

No migration needed - just ensure your value files follow the `env/{name}-values.yaml` pattern.
