package ports

import (
	"context"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

// SourceControlPort abstracts fetching chart files from a repository at a given ref.
type SourceControlPort interface {
	FetchChartFiles(ctx context.Context, owner, repo, ref, chartPath string) (tmpDir string, cleanup func(), err error)
}

// EnvironmentDiscoveryPort abstracts discovering which environments exist for
// a chart by inspecting its directory structure after files have been fetched.
type EnvironmentDiscoveryPort interface {
	DiscoverEnvironments(ctx context.Context, chartDir string) ([]domain.EnvironmentConfig, error)
}

// RendererPort abstracts Helm template rendering, separated from source control
// so the rendering strategy is independently swappable.
type RendererPort interface {
	Render(ctx context.Context, chartDir string, valueFiles []string) ([]byte, error)
}

// ReportingPort abstracts posting diff results back to the pull request.
type ReportingPort interface {
	// CreateInProgressCheck creates a check run in "in_progress" status for a chart
	// and returns the check run ID for later updates.
	CreateInProgressCheck(ctx context.Context, pr domain.PRContext, chartName string) (checkRunID int64, err error)

	// UpdateCheckWithResults updates an existing check run with final diff results.
	UpdateCheckWithResults(ctx context.Context, pr domain.PRContext, checkRunID int64, results []domain.DiffResult) error

	// PostComment posts a PR comment with diff results for a chart.
	PostComment(ctx context.Context, pr domain.PRContext, results []domain.DiffResult) error
}

// FileChangesPort abstracts checking which files have been modified in a PR.
type FileChangesPort interface {
	GetChangedFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error)
}

// DiffPort abstracts computing diffs between two manifests.
// Different implementations can provide different diff strategies
// (e.g., semantic YAML diffing vs line-based text diffing).
type DiffPort interface {
	// ComputeDiff returns a diff between base and head manifests.
	// baseName and headName are used for labeling (e.g., "my-app/prod (main)").
	ComputeDiff(baseName, headName string, base, head []byte) string
}
