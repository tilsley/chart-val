// Package filesystem discovers environment configuration by scanning the chart's env/ directory.
package filesystem

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
)

// Adapter implements ports.EnvironmentConfigPort by scanning the chart's
// env/ subdirectory for *-values.yaml files.
type Adapter struct {
	sourceControl ports.SourceControlPort
}

// New creates a new filesystem environment config adapter.
func New(sourceControl ports.SourceControlPort) *Adapter {
	return &Adapter{
		sourceControl: sourceControl,
	}
}

// GetEnvironmentConfig implements ports.EnvironmentConfigPort.
// It fetches the chart files and discovers environments from the env/ directory.
func (a *Adapter) GetEnvironmentConfig(
	ctx context.Context,
	pr domain.PRContext,
	chartName string,
) (domain.ChartConfig, error) {
	chartPath := "charts/" + chartName

	// Fetch chart directory to discover environments
	chartDir, cleanup, err := a.sourceControl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.HeadRef, chartPath)
	if err != nil {
		return domain.ChartConfig{}, fmt.Errorf("fetching chart files: %w", err)
	}
	defer cleanup()

	// Discover environments from env/ directory
	envs := a.discoverEnvironments(chartDir)

	return domain.ChartConfig{
		Path:         chartPath,
		Environments: envs,
	}, nil
}

// discoverEnvironments scans chartDir/env/ for files matching *-values.yaml.
// If no env/ directory or no matching files exist, returns empty slice.
func (a *Adapter) discoverEnvironments(chartDir string) []domain.EnvironmentConfig {
	envDir := filepath.Join(chartDir, "env")

	entries, err := os.ReadDir(envDir)
	if err != nil {
		// No env/ directory → return empty (service will handle fallback)
		return []domain.EnvironmentConfig{}
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

	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Name < configs[j].Name
	})

	return configs
}
