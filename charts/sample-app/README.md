# Sample App Helm Chart

This is a sample Helm chart that demonstrates environment-specific configurations using the `env` folder pattern.

## Structure

```
sample-app/
├── Chart.yaml              # Chart metadata
├── values.yaml             # Default values
├── env/                    # Environment-specific value overrides
│   ├── dev-values.yaml     # Development environment
│   ├── stg-values.yaml     # Staging environment
│   └── prd-values.yaml     # Production environment
└── templates/              # Kubernetes resource templates
    ├── _helpers.tpl        # Template helpers
    ├── deployment.yaml     # Deployment resource
    └── service.yaml        # Service resource
```

## Environment Configurations

### Development (dev-values.yaml)
- 1 replica
- Debug logging enabled
- Lower resource limits (200m CPU, 256Mi memory)
- Development image tag

### Staging (stg-values.yaml)
- 2 replicas
- Info-level logging
- Medium resource limits (500m CPU, 512Mi memory)
- Auto-scaling enabled (2-5 replicas)
- Monitoring enabled

### Production (prd-values.yaml)
- 3 replicas
- Warning-level logging
- High resource limits (1 CPU, 1Gi memory)
- Auto-scaling enabled (3-10 replicas)
- Production node selector
- Production tolerations
- High availability enabled

## Usage

### Install with default values:
```bash
helm install my-release charts/sample-app
```

### Install with environment-specific values:
```bash
# Development
helm install my-release charts/sample-app -f charts/sample-app/env/dev-values.yaml

# Staging
helm install my-release charts/sample-app -f charts/sample-app/env/stg-values.yaml

# Production
helm install my-release charts/sample-app -f charts/sample-app/env/prd-values.yaml
```

### Template rendering (dry-run):
```bash
# Development
helm template my-release charts/sample-app -f charts/sample-app/env/dev-values.yaml

# Staging
helm template my-release charts/sample-app -f charts/sample-app/env/stg-values.yaml

# Production
helm template my-release charts/sample-app -f charts/sample-app/env/prd-values.yaml
```

## Integration with chart-sentry

This chart is configured in `.chart-sentry.yaml` to automatically generate diffs for all three environments when changes are made in pull requests.
