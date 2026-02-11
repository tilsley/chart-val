package domain

// EnvironmentConfig holds the specific environment context and
// the ordered list of values file paths (Helm applies left-to-right).
type EnvironmentConfig struct {
	Name       string
	ValueFiles []string
	Message    string // Optional message (e.g., for base charts not deployed)
}

// ChartConfig defines a chart to validate and its environments.
type ChartConfig struct {
	Path         string              // e.g., "charts/my-app"
	Environments []EnvironmentConfig // List of environments to validate
}

// ChangedChart represents a chart that was modified in a PR.
type ChangedChart struct {
	Name string // Chart name from Chart.yaml (e.g., "my-app")
	Path string // Path within repo (e.g., "charts/my-app")
}
