package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
)

const noChangesMessage = "No changes detected."

// DiffService implements ports.DiffUseCase by orchestrating the full
// chart diff workflow: discover charts, fetch chart files, render, compute diffs, and report.
type DiffService struct {
	sourceControl   ports.SourceControlPort
	changedCharts   ports.ChangedChartsPort
	argoChartConfig ports.ChartConfigPort // Optional: Argo CD apps (source of truth)
	discoveryConfig ports.ChartConfigPort // Fallback: discover from chart's env/ directory
	renderer        ports.RendererPort
	reporter        ports.ReportingPort
	semanticDiff    ports.DiffPort // Semantic YAML diff (e.g., dyff)
	unifiedDiff     ports.DiffPort // Line-based diff (e.g., go-difflib)
	logger          *slog.Logger
}

// NewDiffService creates a new DiffService wired with all driven ports.
// argoConfig is optional (can be nil) - if provided, it's used as source of truth with discoveryConfig as fallback.
func NewDiffService(
	sc ports.SourceControlPort,
	cc ports.ChangedChartsPort,
	argoConfig ports.ChartConfigPort,
	discoveryConfig ports.ChartConfigPort,
	rn ports.RendererPort,
	rp ports.ReportingPort,
	semanticDiff ports.DiffPort,
	unifiedDiff ports.DiffPort,
	logger *slog.Logger,
) *DiffService {
	return &DiffService{
		sourceControl:   sc,
		changedCharts:   cc,
		argoChartConfig: argoConfig,
		discoveryConfig: discoveryConfig,
		renderer:        rn,
		reporter:        rp,
		semanticDiff:    semanticDiff,
		unifiedDiff:     unifiedDiff,
		logger:          logger,
	}
}

// Execute runs the diff workflow for a pull request.
func (s *DiffService) Execute(ctx context.Context, pr domain.PRContext) error {
	// Detect which charts changed in this PR
	changedCharts, err := s.changedCharts.GetChangedCharts(ctx, pr)
	if err != nil {
		return fmt.Errorf("getting changed charts: %w", err)
	}

	if len(changedCharts) == 0 {
		s.logger.Info("no charts to validate")
		return nil
	}

	s.logger.Info("found charts to validate", "count", len(changedCharts))

	// Create a single check run for the entire PR
	checkRunID, err := s.reporter.CreateInProgressCheck(ctx, pr)
	if err != nil {
		return fmt.Errorf("creating in-progress check: %w", err)
	}

	// Process each changed chart, collecting all results
	var allResults []domain.DiffResult
	chartResults := make(map[string][]domain.DiffResult) // grouped by chart name

	for _, chart := range changedCharts {
		s.logger.Info("processing chart", "chartName", chart.Name, "path", chart.Path)

		config, err := s.getChartConfig(ctx, pr, chart.Name)
		if err != nil {
			s.logger.Error("failed to get chart config", "chart", chart.Name, "error", err)
			continue
		}

		results := s.processChart(ctx, pr, config)
		allResults = append(allResults, results...)
		chartResults[chart.Name] = results
	}

	// Update check run with all results
	if err := s.reporter.UpdateCheckWithResults(ctx, pr, checkRunID, allResults); err != nil {
		s.logger.Error("failed to update check run", "checkRunID", checkRunID, "error", err)
	}

	// Post per-chart comment only for charts with changes
	for chartName, results := range chartResults {
		if hasChanges(results) {
			if err := s.reporter.PostComment(ctx, pr, results); err != nil {
				s.logger.Error("failed to post PR comment", "chart", chartName, "error", err)
			}
		} else {
			s.logger.Info("no changes for chart, skipping comment", "chart", chartName)
		}
	}

	return nil
}

// getChartConfig gets chart configuration using the composite strategy:
// 1. Try Argo CD apps (source of truth for deployed charts)
// 2. Fall back to discovering from chart's env/ directory (for new charts)
// 3. If no environments found, treat as base chart (not deployed)
func (s *DiffService) getChartConfig(
	ctx context.Context,
	pr domain.PRContext,
	chartName string,
) (domain.ChartConfig, error) {
	// Try Argo apps first (if configured)
	if s.argoChartConfig != nil {
		config, err := s.argoChartConfig.GetChartConfig(ctx, pr, chartName)
		if err == nil && len(config.Environments) > 0 {
			s.logger.Info(
				"using argo apps for chart config",
				"chartName",
				chartName,
				"envCount",
				len(config.Environments),
			)
			return config, nil
		}
	}

	// Fall back to discovering from chart's env/ directory
	s.logger.Info("no argo apps found, falling back to discovery", "chartName", chartName)
	config, err := s.discoveryConfig.GetChartConfig(ctx, pr, chartName)
	if err != nil {
		return domain.ChartConfig{}, err
	}

	if len(config.Environments) > 0 {
		s.logger.Info("using discovered environments", "chartName", chartName, "envCount", len(config.Environments))
		return config, nil
	}

	// No environments found - treat as base chart (not deployed)
	s.logger.Info("no environments found, treating as base chart", "chartName", chartName)
	return domain.ChartConfig{
		Path: config.Path,
		Environments: []domain.EnvironmentConfig{{
			Name:    "base",
			Message: "This chart is not deployed (may be used as a base chart)",
		}},
	}, nil
}

// processChart handles fetching and diffing a single chart using the provided config.
// Returns all diff results for the chart (including errors as DiffResult entries).
func (s *DiffService) processChart(
	ctx context.Context,
	pr domain.PRContext,
	config domain.ChartConfig,
) []domain.DiffResult {
	var results []domain.DiffResult
	chartName := extractChartNameFromPath(config.Path)
	chartPath := config.Path

	// Fetch base chart files
	baseDir, baseCleanup, err := s.sourceControl.FetchChartFiles(ctx, pr.Owner, pr.Repo, pr.BaseRef, chartPath)
	baseExists := true
	if err != nil {
		if domain.IsNotFound(err) {
			s.logger.Info(
				"chart not found in base ref, treating as new chart",
				"chart",
				chartName,
				"base_ref",
				pr.BaseRef,
			)
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
				Status:      domain.StatusError,
				Summary:     fmt.Sprintf("❌ Error fetching base chart: %s", err),
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
			Status:      domain.StatusError,
			Summary:     fmt.Sprintf("❌ Error fetching head chart: %s", err),
		}}
	}
	defer headCleanup()

	// Use environments from config (not discovered)
	envs := config.Environments
	s.logger.Info("processing environments from config", "chart", chartName, "envCount", len(envs))

	// Diff each environment
	for _, env := range envs {
		// Handle special case: environment with message but no value files (e.g., base chart)
		if env.Message != "" && len(env.ValueFiles) == 0 {
			s.logger.Info(
				"environment has message, skipping diff",
				"chart",
				chartName,
				"env",
				env.Name,
				"message",
				env.Message,
			)
			results = append(results, domain.DiffResult{
				ChartName:   chartName,
				Environment: env.Name,
				BaseRef:     pr.BaseRef,
				HeadRef:     pr.HeadRef,
				Status:      domain.StatusSuccess,
				Summary:     env.Message,
			})
			continue
		}

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
				Status:      domain.StatusError,
				Summary:     err.Error(),
			})
			continue
		}
		s.logger.Info("appending diff result", "chart", chartName, "env", env.Name, "status", result.Status)
		results = append(results, result)
	}

	return results
}

func (s *DiffService) diffChartEnv(
	ctx context.Context,
	pr domain.PRContext,
	chartName, baseDir, headDir string,
	baseExists bool,
	env domain.EnvironmentConfig,
) (domain.DiffResult, error) {
	var baseManifest []byte
	var err error

	if baseExists {
		s.logger.Info(
			"rendering base manifest",
			"chart",
			chartName,
			"env",
			env.Name,
			"baseDir",
			baseDir,
			"valueFiles",
			env.ValueFiles,
		)
		baseManifest, err = s.renderer.Render(ctx, baseDir, env.ValueFiles)
		if err != nil {
			return domain.DiffResult{}, fmt.Errorf("failed to render base branch: %w", err)
		}
		s.logger.Info("base manifest rendered", "chart", chartName, "env", env.Name, "size", len(baseManifest))
	} else {
		s.logger.Info("skipping base render (chart not in base)", "chart", chartName, "env", env.Name)
	}

	s.logger.Info(
		"rendering head manifest",
		"chart",
		chartName,
		"env",
		env.Name,
		"headDir",
		headDir,
		"valueFiles",
		env.ValueFiles,
	)
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
		summary = noChangesMessage
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

// hasChanges returns true if any result has changes or errors.
func hasChanges(results []domain.DiffResult) bool {
	for _, r := range results {
		if r.Status == domain.StatusChanges || r.Status == domain.StatusError {
			return true
		}
	}
	return false
}

// extractChartNameFromPath extracts the chart name from a path.
// E.g., "charts/my-app" -> "my-app"
func extractChartNameFromPath(path string) string {
	return filepath.Base(path)
}
