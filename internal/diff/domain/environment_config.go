package domain

// EnvironmentConfig holds the specific environment context and
// the ordered list of values file paths (Helm applies left-to-right).
type EnvironmentConfig struct {
	Name       string
	ValueFiles []string
}

// ChartConfig groups a chart path with its environment configurations.
type ChartConfig struct {
	Path         string
	Environments []EnvironmentConfig
}
