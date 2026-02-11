// Package discoveredcharts provides an adapter that discovers changed charts from PR files.
package discoveredcharts

import (
	"context"
	"fmt"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
)

// Adapter implements ports.ChartConfigPort by discovering environments
// from the chart directory structure (the original behavior before Argo integration).
type Adapter struct {
	envDiscovery ports.EnvironmentDiscoveryPort
	sourceCtrl   ports.SourceControlPort
}

// New creates a new discovered charts adapter.
func New(envDiscovery ports.EnvironmentDiscoveryPort, sourceCtrl ports.SourceControlPort) *Adapter {
	return &Adapter{
		envDiscovery: envDiscovery,
		sourceCtrl:   sourceCtrl,
	}
}

// GetChartConfig returns the chart config by fetching the chart and discovering environments.
func (a *Adapter) GetChartConfig(
	ctx context.Context,
	pr domain.PRContext,
	chartName string,
) (domain.ChartConfig, error) {
	chartPath := "charts/" + chartName

	// Fetch HEAD version to discover environments
	headDir, cleanup, err := a.sourceCtrl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.HeadRef, chartPath)
	if err != nil {
		return domain.ChartConfig{}, fmt.Errorf("fetching chart %s: %w", chartName, err)
	}
	defer cleanup()

	// Discover environments from the chart directory
	envs, err := a.envDiscovery.DiscoverEnvironments(ctx, headDir)
	if err != nil {
		return domain.ChartConfig{}, fmt.Errorf("discovering environments for %s: %w", chartName, err)
	}

	return domain.ChartConfig{
		Path:         chartPath,
		Environments: envs,
	}, nil
}
