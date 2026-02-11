// Package prfiles provides chart discovery by analyzing changed files in pull requests.
package prfiles

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/google/go-github/v68/github"
	"gopkg.in/yaml.v3"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

// Adapter implements ports.ChangedChartsPort by querying the GitHub API
// for files changed in a pull request, detecting Chart.yaml changes,
// and reading chart names from the file content.
type Adapter struct {
	client *github.Client
	logger *slog.Logger
}

// New creates a new PR files adapter.
func New(client *github.Client, logger *slog.Logger) *Adapter {
	return &Adapter{
		client: client,
		logger: logger,
	}
}

// GetChangedCharts returns charts that were modified in the PR.
// It lists changed files, finds Chart.yaml changes, fetches each one,
// and parses the chart name from the YAML content.
func (a *Adapter) GetChangedCharts(ctx context.Context, pr domain.PRContext) ([]domain.ChangedChart, error) {
	// Get all changed files from GitHub
	changedFiles, err := a.listChangedFiles(ctx, pr.Owner, pr.Repo, pr.PRNumber)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}

	a.logger.Debug("found changed files in PR", "count", len(changedFiles), "files", changedFiles)

	// Find unique chart directories from any changed file under charts/{name}/
	chartDirs := make(map[string]struct{})
	for _, file := range changedFiles {
		if dir := extractChartDir(file); dir != "" {
			chartDirs[dir] = struct{}{}
			a.logger.Debug("detected chart directory from changed file", "file", file, "chartDir", dir)
		}
	}

	a.logger.Debug("extracted chart directories", "count", len(chartDirs))

	if len(chartDirs) == 0 {
		return nil, nil
	}

	// For each Chart.yaml, fetch and parse the chart name
	var charts []domain.ChangedChart
	for chartDir := range chartDirs {
		chartYamlPath := filepath.Join(chartDir, "Chart.yaml")

		a.logger.Debug("fetching Chart.yaml", "path", chartYamlPath, "ref", pr.HeadRef)
		content, err := a.fetchFile(ctx, pr.Owner, pr.Repo, pr.HeadRef, chartYamlPath)
		if err != nil {
			a.logger.Warn("failed to fetch Chart.yaml", "path", chartYamlPath, "ref", pr.HeadRef, "error", err)
			continue
		}

		name, err := parseChartName(content)
		if err != nil {
			a.logger.Warn("failed to parse chart name", "path", chartYamlPath, "error", err)
			continue
		}

		a.logger.Debug("found chart", "name", name, "path", chartDir)
		charts = append(charts, domain.ChangedChart{
			Name: name,
			Path: chartDir,
		})
	}

	return charts, nil
}

// listChangedFiles returns all file paths modified in the PR.
func (a *Adapter) listChangedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	var changedFiles []string
	opts := &github.ListOptions{PerPage: 100}

	for {
		files, resp, err := a.client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}

		for _, file := range files {
			changedFiles = append(changedFiles, file.GetFilename())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return changedFiles, nil
}

// fetchFile fetches a single file from the repository at the given ref.
func (a *Adapter) fetchFile(ctx context.Context, owner, repo, ref, filePath string) ([]byte, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}
	fileContent, _, _, err := a.client.Repositories.GetContents(ctx, owner, repo, filePath, opts)
	if err != nil {
		return nil, fmt.Errorf("fetching file %s: %w", filePath, err)
	}

	if fileContent == nil {
		return nil, fmt.Errorf("file not found: %s", filePath)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return nil, fmt.Errorf("decoding file content: %w", err)
	}

	return []byte(content), nil
}

// extractChartDir returns the chart directory (e.g., "charts/my-app") from a file path,
// or empty string if the file is not under a charts/ directory.
// E.g., "charts/my-app/env/prod-values.yaml" -> "charts/my-app"
func extractChartDir(filePath string) string {
	parts := strings.SplitN(filePath, "/", 3)
	if len(parts) < 2 || parts[0] != "charts" || parts[1] == "" {
		return ""
	}
	return parts[0] + "/" + parts[1]
}

// parseChartName extracts the chart name from Chart.yaml content.
func parseChartName(content []byte) (string, error) {
	var chart struct {
		Name string `yaml:"name"`
	}

	if err := yaml.Unmarshal(content, &chart); err != nil {
		return "", fmt.Errorf("unmarshal Chart.yaml: %w", err)
	}

	if chart.Name == "" {
		return "", errors.New("chart name is empty")
	}

	return chart.Name, nil
}
