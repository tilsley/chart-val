# Argo CD Integration

chart-val can read chart configurations from Argo CD Application manifests instead of (or in addition to) `.chart-val.yaml` files.

## Overview

When you have hundreds of Argo CD Applications in a GitOps repository, chart-val can automatically discover which charts to validate by reading your Application manifests. This eliminates the need to maintain separate `.chart-val.yaml` files in each chart repository.

## Architecture

```
┌────────────────────────────────────────────────────┐
│  chart-val receives PR webhook                     │
└────────────────┬───────────────────────────────────┘
                 │
                 ▼
┌────────────────────────────────────────────────────┐
│  ArgoAppsAdapter (background synced local clone)   │
│  ┌──────────────────────────────────────────────┐  │
│  │  Index: repoURL -> Applications              │  │
│  │  https://github.com/org/charts -> [          │  │
│  │    {path: charts/my-app, envs: [staging]},   │  │
│  │    {path: charts/my-app, envs: [prod]}       │  │
│  │  ]                                            │  │
│  └──────────────────────────────────────────────┘  │
└────────────────────────────────────────────────────┘
```

**Performance:**
- Local clone synced every 1 hour (configurable)
- Indexed lookup: ~1ms
- No GitHub API rate limits
- Works with large repos (1000s of apps)

## Configuration

### 1. Environment Variables

Add to your `.env` file:

```bash
# Argo CD integration
ARGO_APPS_REPO=https://github.com/myorg/gitops
ARGO_APPS_LOCAL_PATH=/tmp/chart-val-argocd
ARGO_APPS_SYNC_INTERVAL=1h
```

| Variable | Description | Default |
|----------|-------------|---------|
| `ARGO_APPS_REPO` | Git repository containing Argo apps | *(required)* |
| `ARGO_APPS_LOCAL_PATH` | Local path for clone | `/tmp/chart-val-argocd` |
| `ARGO_APPS_SYNC_INTERVAL` | Sync frequency | `1h` |

### 2. Repository Structure

Your GitOps repository can have any structure. chart-val will scan the entire repository for Argo CD Application manifests and extract the environment name from the folder path:

```
gitops/
├── clusters/
│   ├── staging/          # Environment extracted from path
│   │   └── team-a/
│   │       └── my-app.yaml
│   └── prod/             # Environment extracted from path
│       └── team-a/
│           └── my-app.yaml
```

Or:

```
gitops/
├── teams/
│   └── team-a/
│       ├── dev/          # Environment extracted from path
│       │   └── my-app.yaml
│       └── prod/         # Environment extracted from path
│           └── my-app.yaml
```

### 3. Application Manifest Format

chart-val reads these fields from your Application manifests:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/myorg/charts  # Must match PR repo
    path: charts/my-app                       # Chart path
    helm:
      valueFiles:                             # Values files for this env
        - values-staging.yaml
```

**Note:** The environment name is extracted from the folder path where the Application manifest is located, not from the Application name. For example, if this file is at `clusters/staging/my-app.yaml`, the environment will be `staging`.

## How It Works

### 1. Initial Clone & Index

On startup, chart-val:
1. Clones the GitOps repo to `/tmp/chart-val-argocd`
2. Scans the entire repository for `.yaml` and `.yml` files
3. Parses Application manifests
4. Extracts environment name from the folder path
5. Builds an index: `repoURL -> []Application`

### 2. Background Sync

Every hour (configurable), chart-val:
1. Runs `git pull`
2. Rebuilds the index
3. Updates are atomic (read-write lock)

### 3. PR Validation

When a PR webhook arrives:
1. Extract repo URL: `https://github.com/myorg/charts`
2. Index lookup: find all Applications pointing to that repo
3. Group by chart path
4. Validate each chart/environment combination

**Example:**
```
PR to: https://github.com/myorg/charts

Index lookup finds Applications with matching repoURL:
- File: clusters/staging/my-app.yaml (env: staging, path: charts/my-app)
- File: clusters/prod/my-app.yaml    (env: prod, path: charts/my-app)

Result: Validate charts/my-app for staging + prod
```

## Environment Name Extraction

chart-val extracts environment names from the **folder path** where the Application manifest is located:

| File Path | Extracted Environment |
|-----------|----------------------|
| `clusters/staging/team-a/app.yaml` | `staging` |
| `clusters/prod/team-b/app.yaml` | `prod` |
| `teams/team-a/dev/app.yaml` | `dev` |
| `environments/qa/apps/app.yaml` | `qa` |

Recognized environments: `dev`, `development`, `staging`, `stage`, `prod`, `production`, `qa`, `test`, `uat`, `preprod`

The adapter scans each path component (folder name) and uses the first one that matches a recognized environment. If no match is found, it falls back to the first non-hidden directory name in the path.

## Migration Guide

### From .chart-val.yaml

**Before** (in chart repo):
```yaml
# charts/my-app/.chart-val.yaml
charts:
  - path: .
    environments:
      - name: staging
        valueFiles:
          - values-staging.yaml
      - name: prod
        valueFiles:
          - values-prod.yaml
```

**After** (in GitOps repo):
```yaml
# gitops/clusters/staging/my-app.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app-staging
spec:
  source:
    repoURL: https://github.com/myorg/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-staging.yaml
---
# gitops/clusters/prod/my-app.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app-prod
spec:
  source:
    repoURL: https://github.com/myorg/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-prod.yaml
```

Note: The environment name is extracted from the folder structure (`clusters/staging/` and `clusters/prod/`), not from the Application name.

Benefits:
- ✅ Single source of truth (Argo Applications)
- ✅ No duplicate configuration
- ✅ Automatic discovery of new environments

## Backward Compatibility

If `ARGO_APPS_REPO` is **not** configured, chart-val falls back to the original behavior:
- Discovers charts from changed files in PR
- Scans `env/` directory for `*-values.yaml` files
- Works without any configuration files

## Troubleshooting

### Check Sync Status

```bash
# View chart-val logs
docker logs chart-val | grep "argo apps"

# Expected output:
# level=INFO msg="using argo apps for chart discovery" repo=https://github.com/org/gitops syncInterval=1h
# level=INFO msg="initializing argo apps repository" repoURL=... localPath=...
# level=INFO msg="scanning repo for argo applications"
# level=INFO msg="index rebuilt" totalApps=247 uniqueRepos=12
# level=INFO msg="argo apps adapter started" syncInterval=1h
```

### Verify Index Contents

Add debug logging to see what's indexed:

```bash
# In container
ls -la /tmp/chart-val-argocd/

# Should show:
# drwxr-xr-x  .git/
# drwxr-xr-x  argocd/
```

### Test Locally

```bash
# Clone your GitOps repo
git clone https://github.com/myorg/gitops /tmp/test-argocd

# Check structure (find all Argo Application manifests)
find /tmp/test-argocd -name "*.yaml" -o -name "*.yml" | while read file; do
  kind=$(yq eval '.kind' "$file" 2>/dev/null)
  if [ "$kind" = "Application" ]; then
    echo "Found Application: $file"
  fi
done

# Test environment extraction from a sample path
# Example: /tmp/test-argocd/clusters/staging/team-a/my-app.yaml
# Expected: Environment = "staging"
```

### Common Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| "no charts to validate" | Repo URL mismatch | Ensure Application `spec.source.repoURL` matches PR repo exactly |
| "failed to pull repository" | Git credentials | Use HTTPS URLs or configure SSH keys |
| Stale results | Sync interval too long | Reduce `ARGO_APPS_SYNC_INTERVAL` |
| High memory usage | Very large repo | Use shallow clone (`--depth=1`) or filter paths |

## Performance Tuning

### For Large Repos (1000+ apps)

```bash
# Reduce sync frequency (less disk I/O)
ARGO_APPS_SYNC_INTERVAL=6h

# Use persistent volume (avoid re-cloning on restart)
ARGO_APPS_LOCAL_PATH=/var/lib/chart-val/argocd

# Enable debug logging to monitor index build time
LOG_LEVEL=debug
```

### Metrics

- **Index build time:** ~1-5 seconds for 1000 apps
- **Lookup time:** ~1ms
- **Memory usage:** ~5-10MB for index
- **Disk usage:** Same as git repo size

## Examples

### Multi-Environment App

```yaml
# clusters/dev/my-app.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/myorg/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-dev.yaml
---
# clusters/staging/my-app.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/myorg/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-staging.yaml
---
# clusters/prod/my-app.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: my-app
spec:
  source:
    repoURL: https://github.com/myorg/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-prod.yaml
```

Result: When PR changes `charts/my-app`, validates against dev, staging, and prod. The environment names are extracted from the folder paths (`clusters/dev/`, `clusters/staging/`, `clusters/prod/`).

### Multiple Charts in One PR

If PR modifies:
- `charts/app-a/Chart.yaml`
- `charts/app-b/values.yaml`

chart-val validates:
- All environments for `app-a` (based on Applications pointing to `charts/app-a`)
- All environments for `app-b` (based on Applications pointing to `charts/app-b`)
