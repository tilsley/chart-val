package ports

import (
	"context"

	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
)

// SourceControlPort abstracts fetching chart files from a repository at a given ref.
type SourceControlPort interface {
	FetchChartFiles(ctx context.Context, owner, repo, ref, chartPath string, installationID int64) (tmpDir string, cleanup func(), err error)
}

// ConfigOrderingPort abstracts reading the chart-sentry manifest that defines
// which charts and environments to diff.
type ConfigOrderingPort interface {
	GetOrdering(ctx context.Context, owner, repo, ref string, installationID int64) ([]domain.ChartConfig, error)
}

// RendererPort abstracts Helm template rendering, separated from source control
// so the rendering strategy is independently swappable.
type RendererPort interface {
	Render(ctx context.Context, chartDir string, valueFiles []string) ([]byte, error)
}

// ReportingPort abstracts posting diff results back to the pull request.
type ReportingPort interface {
	PostResult(ctx context.Context, pr domain.PRContext, results []domain.DiffResult) error
}

// FileChangesPort abstracts checking which files have been modified in a PR.
type FileChangesPort interface {
	GetChangedFiles(ctx context.Context, owner, repo string, prNumber int, installationID int64) ([]string, error)
}
