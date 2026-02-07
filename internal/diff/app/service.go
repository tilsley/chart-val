package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
	"github.com/nathantilsley/chart-sentry/internal/diff/ports"
)

// DiffService implements ports.DiffUseCase by orchestrating the full
// chart diff workflow: fetch config, check for chart changes, fetch chart files
// for both refs, render, compute diffs, and report.
type DiffService struct {
	sourceControl ports.SourceControlPort
	configOrder   ports.ConfigOrderingPort
	renderer      ports.RendererPort
	reporter      ports.ReportingPort
	fileChanges   ports.FileChangesPort
	logger        *slog.Logger
}

// NewDiffService creates a new DiffService wired with all driven ports.
func NewDiffService(
	sc ports.SourceControlPort,
	co ports.ConfigOrderingPort,
	rn ports.RendererPort,
	rp ports.ReportingPort,
	fc ports.FileChangesPort,
	logger *slog.Logger,
) *DiffService {
	return &DiffService{
		sourceControl: sc,
		configOrder:   co,
		renderer:      rn,
		reporter:      rp,
		fileChanges:   fc,
		logger:        logger,
	}
}

// Execute runs the diff workflow for a pull request.
func (s *DiffService) Execute(ctx context.Context, pr domain.PRContext) error {
	// Check if any files in the charts/ directory have been modified
	changedFiles, err := s.fileChanges.GetChangedFiles(ctx, pr.Owner, pr.Repo, pr.PRNumber, pr.InstallationID)
	if err != nil {
		return fmt.Errorf("fetching changed files: %w", err)
	}

	hasChartChanges := false
	for _, file := range changedFiles {
		if strings.HasPrefix(file, "charts/") {
			hasChartChanges = true
			break
		}
	}

	if !hasChartChanges {
		s.logger.Info("no changes to charts/ directory, skipping diff")
		return nil
	}

	configs, err := s.configOrder.GetOrdering(ctx, pr.Owner, pr.Repo, pr.HeadRef, pr.InstallationID)
	if err != nil {
		return fmt.Errorf("getting config ordering: %w", err)
	}

	var allResults []domain.DiffResult

	for _, chartCfg := range configs {
		chartName := filepath.Base(chartCfg.Path)

		for _, env := range chartCfg.Environments {
			s.logger.Info("diffing chart",
				"chart", chartName,
				"env", env.Name,
				"base", pr.BaseRef,
				"head", pr.HeadRef,
			)

			result, err := s.diffChartEnv(ctx, pr, chartCfg.Path, chartName, env)
			if err != nil {
				s.logger.Error("diff failed",
					"chart", chartName,
					"env", env.Name,
					"error", err,
				)
				allResults = append(allResults, domain.DiffResult{
					ChartName:   chartName,
					Environment: env.Name,
					BaseRef:     pr.BaseRef,
					HeadRef:     pr.HeadRef,
					HasChanges:  true,
					UnifiedDiff: "",
					Summary:     fmt.Sprintf("Error computing diff: %s", err),
				})
				continue
			}
			allResults = append(allResults, result)
		}
	}

	if err := s.reporter.PostResult(ctx, pr, allResults); err != nil {
		return fmt.Errorf("posting results: %w", err)
	}

	return nil
}

func (s *DiffService) diffChartEnv(ctx context.Context, pr domain.PRContext, chartPath, chartName string, env domain.EnvironmentConfig) (domain.DiffResult, error) {
	baseDir, baseCleanup, err := s.sourceControl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.BaseRef, chartPath, pr.InstallationID)
	if err != nil {
		return domain.DiffResult{}, fmt.Errorf("fetching base chart: %w", err)
	}
	defer baseCleanup()

	headDir, headCleanup, err := s.sourceControl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.HeadRef, chartPath, pr.InstallationID)
	if err != nil {
		return domain.DiffResult{}, fmt.Errorf("fetching head chart: %w", err)
	}
	defer headCleanup()

	baseManifest, err := s.renderer.Render(ctx, baseDir, env.ValueFiles)
	if err != nil {
		return domain.DiffResult{}, fmt.Errorf("rendering base: %w", err)
	}

	headManifest, err := s.renderer.Render(ctx, headDir, env.ValueFiles)
	if err != nil {
		return domain.DiffResult{}, fmt.Errorf("rendering head: %w", err)
	}

	diff := computeDiff(
		fmt.Sprintf("%s/%s (%s)", chartName, env.Name, pr.BaseRef),
		fmt.Sprintf("%s/%s (%s)", chartName, env.Name, pr.HeadRef),
		baseManifest,
		headManifest,
	)

	hasChanges := diff != ""
	summary := "No changes detected."
	if hasChanges {
		summary = fmt.Sprintf("Changes detected in %s for environment %s.", chartName, env.Name)
	}

	return domain.DiffResult{
		ChartName:   chartName,
		Environment: env.Name,
		BaseRef:     pr.BaseRef,
		HeadRef:     pr.HeadRef,
		HasChanges:  hasChanges,
		UnifiedDiff: diff,
		Summary:     summary,
	}, nil
}

func computeDiff(baseName, headName string, base, head []byte) string {
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(base)),
		B:        difflib.SplitLines(string(head)),
		FromFile: baseName,
		ToFile:   headName,
		Context:  3,
	}
	text, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return fmt.Sprintf("error computing diff: %s", err)
	}
	return strings.TrimSpace(text)
}
