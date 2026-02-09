package envdiscovery

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

// Adapter implements ports.EnvironmentDiscoveryPort by scanning the chart's
// env/ subdirectory for *-values.yaml files.
type Adapter struct{}

// New creates a new environment discovery adapter.
func New() *Adapter {
	return &Adapter{}
}

// DiscoverEnvironments inspects chartDir/env/ for files matching
// *-values.yaml and returns an EnvironmentConfig per environment.
// If no env/ directory or no matching files exist, returns a single
// "default" environment with no value file overrides.
func (a *Adapter) DiscoverEnvironments(_ context.Context, chartDir string) ([]domain.EnvironmentConfig, error) {
	envDir := filepath.Join(chartDir, "env")

	entries, err := os.ReadDir(envDir)
	if err != nil {
		// No env/ directory → default environment
		return []domain.EnvironmentConfig{
			{Name: "default", ValueFiles: nil},
		}, nil
	}

	var configs []domain.EnvironmentConfig
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, "-values.yaml") {
			continue
		}
		envName := strings.TrimSuffix(name, "-values.yaml")
		configs = append(configs, domain.EnvironmentConfig{
			Name:       envName,
			ValueFiles: []string{filepath.Join("env", name)},
		})
	}

	if len(configs) == 0 {
		return []domain.EnvironmentConfig{
			{Name: "default", ValueFiles: nil},
		}, nil
	}

	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Name < configs[j].Name
	})

	return configs, nil
}
