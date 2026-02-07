In Go, combining **Hexagonal Architecture** with **Feature-Based Packaging** (often called "Screaming Architecture") ensures that your folder structure reflects what the app *does* rather than just what it *is*.

For cross-cutting concerns (logging, metrics, telemetry), we treat them as **Infrastructure Adapters** or **Middlewares**, but we define their "contracts" (interfaces) in a shared internal package so the Domain remains agnostic.

### The Proposed Folder Structure

```text
.
├── cmd/
│   └── diff-app/           # Main entry point (Wiring everything together)
├── internal/
│   ├── platform/           # Cross-cutting concerns (The "Infra Setup")
│   │   ├── logger/         # Structured logging setup (Zap/Slog)
│   │   ├── metrics/        # Prometheus/OpenTelemetry setup
│   │   └── config/         # App-wide env/file config loading
│   ├── diff/               # FEATURE: The Core Domain
│   │   ├── domain/         # Entities: Chart, Values, DiffResult
│   │   │   ├── chart.go
│   │   │   └── diff_service.go
│   │   ├── ports/          # Interfaces (Driving & Driven)
│   │   │   ├── inputs.go   # DiffUseCase
│   │   │   └── outputs.go  # SourceControlPort, ConfigOrderingPort
│   │   ├── adapters/       # Implementation details
│   │   │   ├── github_in/  # Webhook handlers
│   │   │   ├── github_out/ # Checks API reporters
│   │   │   ├── helm_cli/   # Local shell execution logic
│   │   │   └── remote_cfg/ # External ordering adapter
│   └── lint/               # FEATURE: Future expansion (Linting logic)
├── pkg/                    # Public-facing code (if other apps use your SDK)
├── api/                    # Webhook definitions/OpenAPI specs
└── go.mod

```

---

### 1. Handling Cross-Cutting Concerns

The key to Hexagonal Architecture is that your **Domain** should not depend on a specific logger like `Zap`. Instead:

* **Platform Folder:** Put your initialization logic here. This is where you configure your Prometheus registry or your Slog handler.
* **Dependency Injection:** Pass the logger or metrics collector into your Adapters and Domain Services via their constructors (`NewService(l Logger)`).
* **Middleware:** For "Automatic" cross-cutting concerns (e.g., logging every incoming GitHub webhook), implement them as decorators in the **Adapter** layer.

---

### 2. The Domain Model (internal/diff/domain)

This is where your business logic lives. It shouldn't know GitHub exists.

```go
package domain

// Chart represents the state of a Helm chart at a specific point
type Chart struct {
    Name     string
    Version  string
    Manifest []byte // The rendered YAML
}

// DiffService handles the comparison logic
type DiffService struct {
    // Port interfaces go here
}

func (s *DiffService) Compare(base, head Chart) (DiffResult, error) {
    // Business logic to identify deletions, additions, etc.
}

```

---

### 3. The Adapters (internal/diff/adapters)

This is where the "heavy lifting" happens.

* **Helm CLI Adapter:** Will use `os/exec` to run `helm template`. It will accept the `Ordering` provided by the external adapter.
* **External Config Adapter:** This implements your `ConfigOrderingPort`. It might call a REST API or fetch a JSON file that says: `"For env 'prod', use [values.yaml, prod.yaml]"`.

---

### 4. Wiring it together (cmd/diff-app/main.go)

This is your **Composition Root**. This is the only place that knows about every layer.

1. Initialize `platform/logger`.
2. Initialize `adapters/remote_cfg` (External ordering).
3. Initialize `adapters/helm_cli` (Source control).
4. Inject those into `domain/DiffService`.
5. Pass the `DiffService` into the `adapters/github_in` (Webhook handler).
6. Start the HTTP server.

### Next Step

Would you like me to write the Go code for the **Domain Service** and the **Port interfaces** to show how the value-ordering logic would be injected?
