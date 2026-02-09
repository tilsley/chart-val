package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
)

// DiffService implements ports.DiffUseCase by orchestrating the full
// chart diff workflow: discover charts from changed files, fetch chart files,
// discover environments, render, compute diffs, and report.
type DiffService struct {
	sourceControl ports.SourceControlPort
	envDiscovery  ports.EnvironmentDiscoveryPort
	renderer      ports.RendererPort
	reporter      ports.ReportingPort
	fileChanges   ports.FileChangesPort
	semanticDiff  ports.DiffPort // Semantic YAML diff (e.g., dyff)
	unifiedDiff   ports.DiffPort // Line-based diff (e.g., go-difflib)
	logger        *slog.Logger
}

// NewDiffService creates a new DiffService wired with all driven ports.
func NewDiffService(
	sc ports.SourceControlPort,
	ed ports.EnvironmentDiscoveryPort,
	rn ports.RendererPort,
	rp ports.ReportingPort,
	fc ports.FileChangesPort,
	semanticDiff ports.DiffPort,
	unifiedDiff ports.DiffPort,
	logger *slog.Logger,
) *DiffService {
	return &DiffService{
		sourceControl: sc,
		envDiscovery:  ed,
		renderer:      rn,
		reporter:      rp,
		fileChanges:   fc,
		semanticDiff:  semanticDiff,
		unifiedDiff:   unifiedDiff,
		logger:        logger,
	}
}

// Execute runs the diff workflow for a pull request.
func (s *DiffService) Execute(ctx context.Context, pr domain.PRContext) error {
	changedFiles, err := s.fileChanges.GetChangedFiles(ctx, pr.Owner, pr.Repo, pr.PRNumber)
	if err != nil {
		return fmt.Errorf("fetching changed files: %w", err)
	}

	chartNames := domain.ExtractChartNames(changedFiles)
	if len(chartNames) == 0 {
		s.logger.Info("no changes to charts/ directory, skipping diff")
		return nil
	}

	for _, chartName := range chartNames {
		// Create in-progress check immediately
		checkRunID, err := s.reporter.CreateInProgressCheck(ctx, pr, chartName)
		if err != nil {
			s.logger.Error("failed to create in-progress check", "chart", chartName, "error", err)
			continue
		}

		// Process the chart and update check with results
		results := s.processChart(ctx, pr, chartName)

		if err := s.reporter.UpdateCheckWithResults(ctx, pr, checkRunID, results); err != nil {
			s.logger.Error("failed to update check run", "chart", chartName, "checkRunID", checkRunID, "error", err)
		}

		// Post PR comment with diff summary
		if err := s.reporter.PostComment(ctx, pr, results); err != nil {
			s.logger.Error("failed to post PR comment", "chart", chartName, "error", err)
		}
	}

	return nil
}

// processChart handles fetching, discovering envs, and diffing a single chart.
// Returns all diff results for the chart (including errors as DiffResult entries).
func (s *DiffService) processChart(ctx context.Context, pr domain.PRContext, chartName string) []domain.DiffResult {
	var results []domain.DiffResult
	chartPath := "charts/" + chartName

	// Fetch base chart files
	baseDir, baseCleanup, err := s.sourceControl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.BaseRef, chartPath)
	baseExists := true
	if err != nil {
		if domain.IsNotFound(err) {
			s.logger.Info("chart not found in base ref, treating as new chart", "chart", chartName, "base_ref", pr.BaseRef)
			baseExists = false
			baseDir = ""
			baseCleanup = func() {}
		} else {
			s.logger.Error("failed to fetch base chart", "chart", chartName, "error", err)
			return []domain.DiffResult{{
				ChartName:   chartName,
				Environment: "all",
				BaseRef:     pr.BaseRef,
				HeadRef:     pr.HeadRef,
				Status:  domain.StatusError,
				Summary: fmt.Sprintf("❌ Error fetching base chart: %s", err),
			}}
		}
	}
	defer baseCleanup()

	// Fetch head chart files
	headDir, headCleanup, err := s.sourceControl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.HeadRef, chartPath)
	if err != nil {
		s.logger.Error("failed to fetch head chart", "chart", chartName, "error", err)
		return []domain.DiffResult{{
			ChartName:   chartName,
			Environment: "all",
			BaseRef:     pr.BaseRef,
			HeadRef:     pr.HeadRef,
			Status:  domain.StatusError,
			Summary: fmt.Sprintf("❌ Error fetching head chart: %s", err),
		}}
	}
	defer headCleanup()

	// Discover environments
	envs, err := s.envDiscovery.DiscoverEnvironments(ctx, headDir)
	if err != nil {
		s.logger.Error("failed to discover environments", "chart", chartName, "error", err)
		return []domain.DiffResult{{
			ChartName:   chartName,
			Environment: "all",
			BaseRef:     pr.BaseRef,
			HeadRef:     pr.HeadRef,
			Status:  domain.StatusError,
			Summary: fmt.Sprintf("❌ Error discovering environments: %s", err),
		}}
	}

	// Diff each environment
	for _, env := range envs {
		s.logger.Info("diffing chart",
			"chart", chartName,
			"env", env.Name,
			"base", pr.BaseRef,
			"head", pr.HeadRef,
		)

		result, err := s.diffChartEnv(ctx, pr, chartName, baseDir, headDir, baseExists, env)
		if err != nil {
			s.logger.Error("diff failed",
				"chart", chartName,
				"env", env.Name,
				"error", err,
			)
			results = append(results, domain.DiffResult{
				ChartName:   chartName,
				Environment: env.Name,
				BaseRef:     pr.BaseRef,
				HeadRef:     pr.HeadRef,
				Status:  domain.StatusError,
				Summary: err.Error(),
			})
			continue
		}
		s.logger.Info("appending diff result", "chart", chartName, "env", env.Name, "status", result.Status)
		results = append(results, result)
	}

	return results
}

func (s *DiffService) diffChartEnv(ctx context.Context, pr domain.PRContext, chartName, baseDir, headDir string, baseExists bool, env domain.EnvironmentConfig) (domain.DiffResult, error) {
	var baseManifest []byte
	var err error

	if baseExists {
		s.logger.Info("rendering base manifest", "chart", chartName, "env", env.Name, "baseDir", baseDir, "valueFiles", env.ValueFiles)
		baseManifest, err = s.renderer.Render(ctx, baseDir, env.ValueFiles)
		if err != nil {
			return domain.DiffResult{}, fmt.Errorf("failed to render base branch: %w", err)
		}
		s.logger.Info("base manifest rendered", "chart", chartName, "env", env.Name, "size", len(baseManifest))
	} else {
		s.logger.Info("skipping base render (chart not in base)", "chart", chartName, "env", env.Name)
	}

	s.logger.Info("rendering head manifest", "chart", chartName, "env", env.Name, "headDir", headDir, "valueFiles", env.ValueFiles)
	headManifest, err := s.renderer.Render(ctx, headDir, env.ValueFiles)
	if err != nil {
		return domain.DiffResult{}, fmt.Errorf("failed to render PR changes: %w", err)
	}
	s.logger.Info("head manifest rendered", "chart", chartName, "env", env.Name, "size", len(headManifest))

	s.logger.Info("computing diffs", "chart", chartName, "env", env.Name)
	baseName := domain.DiffLabel(chartName, env.Name, pr.BaseRef)
	headName := domain.DiffLabel(chartName, env.Name, pr.HeadRef)

	// Compute semantic diff (dyff) - may be empty if dyff not available
	semanticDiff := s.semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
	s.logger.Info("semantic diff computed", "chart", chartName, "env", env.Name, "size", len(semanticDiff))

	// Always compute unified diff as fallback
	unifiedDiff := s.unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
	s.logger.Info("unified diff computed", "chart", chartName, "env", env.Name, "size", len(unifiedDiff))

	var status domain.Status
	var summary string

	if unifiedDiff != "" || semanticDiff != "" {
		status = domain.StatusChanges
		summary = fmt.Sprintf("Changes detected in %s for environment %s.", chartName, env.Name)
	} else {
		status = domain.StatusSuccess
		summary = "No changes detected."
	}

	return domain.DiffResult{
		ChartName:    chartName,
		Environment:  env.Name,
		BaseRef:      pr.BaseRef,
		HeadRef:      pr.HeadRef,
		Status:       status,
		UnifiedDiff:  unifiedDiff,
		SemanticDiff: semanticDiff,
		Summary:      summary,
	}, nil
}
