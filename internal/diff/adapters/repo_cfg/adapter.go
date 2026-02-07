package repocfg

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"
	"gopkg.in/yaml.v3"

	"github.com/nathantilsley/chart-sentry/api"
	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
	ghclient "github.com/nathantilsley/chart-sentry/internal/platform/github"
)

const manifestPath = ".chart-sentry.yaml"

// Default environments and value file pattern when no manifest is provided
var defaultEnvironments = []string{"sbx", "bbp", "dev", "stg", "prd", "tools", "client"}

// Adapter implements ports.ConfigOrderingPort by reading the
// .chart-sentry.yaml manifest from the target repository.
type Adapter struct {
	clientFactory *ghclient.ClientFactory
}

// New creates a new repo config adapter.
func New(cf *ghclient.ClientFactory) *Adapter {
	return &Adapter{clientFactory: cf}
}

// GetOrdering fetches .chart-sentry.yaml from the given repo at the
// specified ref and returns the parsed chart configurations.
// If no manifest is found, returns default config for all charts in charts/ directory.
func (a *Adapter) GetOrdering(ctx context.Context, owner, repo, ref string, installationID int64) ([]domain.ChartConfig, error) {
	client := a.clientFactory.ForInstallation(installationID)

	fileContent, _, resp, err := client.Repositories.GetContents(ctx, owner, repo, manifestPath, &gogithub.RepositoryContentGetOptions{
		Ref: ref,
	})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			// No manifest found - use default configuration
			return getDefaultConfig(), nil
		}
		return nil, fmt.Errorf("fetching manifest: %w", err)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decoding manifest content: %w", err)
	}

	var manifest api.Manifest
	if err := yaml.Unmarshal([]byte(content), &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest YAML: %w", err)
	}

	configs := make([]domain.ChartConfig, 0, len(manifest.Charts))
	for _, c := range manifest.Charts {
		envs := make([]domain.EnvironmentConfig, 0, len(c.Environments))
		for _, e := range c.Environments {
			envs = append(envs, domain.EnvironmentConfig{
				Name:       e.Name,
				ValueFiles: e.ValueFiles,
			})
		}
		configs = append(configs, domain.ChartConfig{
			Path:         c.Path,
			Environments: envs,
		})
	}

	return configs, nil
}

// getDefaultConfig returns a default configuration that monitors all charts
// in the charts/ directory with standard environments using env/{env}-values.yaml pattern.
func getDefaultConfig() []domain.ChartConfig {
	// Create environments with env/{env}-values.yaml pattern
	envs := make([]domain.EnvironmentConfig, 0, len(defaultEnvironments))
	for _, env := range defaultEnvironments {
		envs = append(envs, domain.EnvironmentConfig{
			Name:       env,
			ValueFiles: []string{"env/" + env + "-values.yaml"},
		})
	}

	// Return single chart config for charts/ directory
	return []domain.ChartConfig{
		{
			Path:         "charts",
			Environments: envs,
		},
	}
}
