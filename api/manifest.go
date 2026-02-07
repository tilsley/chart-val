package api

// Manifest is the top-level schema of the .chart-sentry.yaml file
// stored in target repositories.
type Manifest struct {
	Charts []ManifestChart `yaml:"charts"`
}

// ManifestChart maps a chart path to its set of environment configurations.
type ManifestChart struct {
	Path         string                `yaml:"path"`
	Environments []ManifestEnvironment `yaml:"environments"`
}

// ManifestEnvironment defines an environment name and an ordered list of
// values files (Helm applies them left-to-right).
type ManifestEnvironment struct {
	Name       string   `yaml:"name"`
	ValueFiles []string `yaml:"valueFiles"`
}
