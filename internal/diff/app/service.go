package app

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
)

const noChangesMessage = "No changes detected."

// DiffService implements ports.DiffUseCase by orchestrating the full
// chart diff workflow: discover charts, fetch chart files, render, compute diffs, and report.
type DiffService struct {
	sourceControl ports.SourceControlPort
	changedCharts ports.ChangedChartsPort
	argoEnvConfig ports.EnvironmentConfigPort // Optional: Argo CD apps (source of truth)
	fsEnvConfig   ports.EnvironmentConfigPort // Fallback: discovers from chart's env/ folder
	renderer      ports.RendererPort
	reporter      ports.ReportingPort
	semanticDiff  ports.DiffPort // Semantic YAML diff (e.g., dyff)
	unifiedDiff   ports.DiffPort // Line-based diff (e.g., go-difflib)
	logger        *slog.Logger
	tracer        trace.Tracer

	// Pre-created metric instruments (created once, reused per call)
	execCounter     metric.Int64Counter
	execDuration    metric.Float64Histogram
	chartsProcessed metric.Int64Counter
	envsProcessed   metric.Int64Counter
	diffStatus      metric.Int64Counter
	renderDuration  metric.Float64Histogram
	diffDuration    metric.Float64Histogram
}

// NewDiffService creates a new DiffService wired with all driven ports.
// argoEnvConfig is optional (can be nil) - if provided, it's used as source of truth with filesystem as fallback.
func NewDiffService(
	sc ports.SourceControlPort,
	cc ports.ChangedChartsPort,
	argoEnvConfig ports.EnvironmentConfigPort,
	fsEnvConfig ports.EnvironmentConfigPort,
	rn ports.RendererPort,
	rp ports.ReportingPort,
	semanticDiff ports.DiffPort,
	unifiedDiff ports.DiffPort,
	logger *slog.Logger,
	meter metric.Meter,
	tracer trace.Tracer,
) *DiffService {
	execCounter, _ := meter.Int64Counter("chart_val.executions",
		metric.WithUnit("{invocation}"),
		metric.WithDescription("Number of Execute() invocations"),
	)
	execDuration, _ := meter.Float64Histogram("chart_val.execution.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Execute() calls"),
	)
	chartsProcessed, _ := meter.Int64Counter("chart_val.charts.processed",
		metric.WithUnit("{chart}"),
		metric.WithDescription("Number of charts processed"),
	)
	envsProcessed, _ := meter.Int64Counter("chart_val.environments.processed",
		metric.WithUnit("{environment}"),
		metric.WithDescription("Number of environments processed"),
	)
	diffStatus, _ := meter.Int64Counter("chart_val.diff.status",
		metric.WithUnit("{result}"),
		metric.WithDescription("Diff results by status"),
	)
	renderDuration, _ := meter.Float64Histogram("chart_val.render.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of Render() calls"),
	)
	diffDuration, _ := meter.Float64Histogram("chart_val.diff.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of ComputeDiff() calls"),
	)

	return &DiffService{
		sourceControl:   sc,
		changedCharts:   cc,
		argoEnvConfig:   argoEnvConfig,
		fsEnvConfig:     fsEnvConfig,
		renderer:        rn,
		reporter:        rp,
		semanticDiff:    semanticDiff,
		unifiedDiff:     unifiedDiff,
		logger:          logger,
		tracer:          tracer,
		execCounter:     execCounter,
		execDuration:    execDuration,
		chartsProcessed: chartsProcessed,
		envsProcessed:   envsProcessed,
		diffStatus:      diffStatus,
		renderDuration:  renderDuration,
		diffDuration:    diffDuration,
	}
}

// Execute runs the diff workflow for a pull request.
func (s *DiffService) Execute(ctx context.Context, pr domain.PRContext) error {
	ctx, span := s.tracer.Start(ctx, "Execute",
		trace.WithAttributes(
			attribute.String("pr.owner", pr.Owner),
			attribute.String("pr.repo", pr.Repo),
			attribute.Int("pr.number", pr.PRNumber),
		),
	)
	defer span.End()

	start := time.Now()
	s.execCounter.Add(ctx, 1)
	defer func() {
		s.execDuration.Record(ctx, time.Since(start).Seconds())
	}()

	// Detect which charts changed in this PR
	changedCharts, err := s.changedCharts.GetChangedCharts(ctx, pr)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "getting changed charts")
		return fmt.Errorf("getting changed charts: %w", err)
	}

	if len(changedCharts) == 0 {
		s.logger.Info("no charts to validate")
		return nil
	}

	span.SetAttributes(attribute.Int("charts.count", len(changedCharts)))
	s.logger.Info("found charts to validate", "count", len(changedCharts))

	// Create a single check run for the entire PR
	checkRunID, err := s.reporter.CreateInProgressCheck(ctx, pr)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "creating in-progress check")
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

// getChartConfig gets environment configuration using the composite strategy:
// 1. Try Argo CD apps (source of truth for deployed charts)
// 2. Fall back to discovering from chart's env/ directory (for new charts)
// 3. If no environments found, render with default values.yaml only
func (s *DiffService) getChartConfig(
	ctx context.Context,
	pr domain.PRContext,
	chartName string,
) (domain.ChartConfig, error) {
	ctx, span := s.tracer.Start(ctx, "getChartConfig",
		trace.WithAttributes(attribute.String("chart.name", chartName)),
	)
	defer span.End()

	chartPath := "charts/" + chartName

	// Try Argo apps first (if configured)
	if s.argoEnvConfig != nil {
		config, err := s.argoEnvConfig.GetEnvironmentConfig(ctx, pr, chartName)
		if err == nil && len(config.Environments) > 0 {
			s.logger.Info(
				"using argo apps for environment config",
				"chartName",
				chartName,
				"envCount",
				len(config.Environments),
			)
			span.SetAttributes(attribute.String("config.source", "argo"))
			return config, nil
		}
	}

	// Fall back to discovering from chart's env/ directory
	s.logger.Info("no argo apps found, falling back to filesystem discovery", "chartName", chartName)

	config, err := s.fsEnvConfig.GetEnvironmentConfig(ctx, pr, chartName)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "discovering environments")
		return domain.ChartConfig{}, fmt.Errorf("discovering environments for %s: %w", chartName, err)
	}

	if len(config.Environments) > 0 {
		s.logger.Info(
			"using filesystem discovered environments",
			"chartName", chartName,
			"envCount", len(config.Environments),
		)
		span.SetAttributes(attribute.String("config.source", "filesystem"))
		return config, nil
	}

	// No environment overrides found — render with just the chart's default values.yaml
	s.logger.Info("no environment overrides found, using default values", "chartName", chartName)
	span.SetAttributes(attribute.String("config.source", "default"))
	return domain.ChartConfig{
		Path: chartPath,
		Environments: []domain.EnvironmentConfig{{
			Name: "default",
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

	ctx, span := s.tracer.Start(ctx, "processChart",
		trace.WithAttributes(
			attribute.String("chart.name", chartName),
			attribute.Int("chart.environments", len(config.Environments)),
		),
	)
	defer span.End()

	s.chartsProcessed.Add(ctx, 1, metric.WithAttributes(attribute.String("chart", chartName)))

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
			span.RecordError(err)
			span.SetStatus(codes.Error, "fetching base chart")
			s.diffStatus.Add(ctx, 1, metric.WithAttributes(
				attribute.String("chart", chartName),
				attribute.String("environment", "all"),
				attribute.String("status", domain.StatusError.String()),
			))
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
		span.RecordError(err)
		span.SetStatus(codes.Error, "fetching head chart")
		s.diffStatus.Add(ctx, 1, metric.WithAttributes(
			attribute.String("chart", chartName),
			attribute.String("environment", "all"),
			attribute.String("status", domain.StatusError.String()),
		))
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
			s.diffStatus.Add(ctx, 1, metric.WithAttributes(
				attribute.String("chart", chartName),
				attribute.String("environment", env.Name),
				attribute.String("status", domain.StatusError.String()),
			))
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
	ctx, span := s.tracer.Start(ctx, "diffChartEnv",
		trace.WithAttributes(
			attribute.String("chart.name", chartName),
			attribute.String("environment", env.Name),
		),
	)
	defer span.End()

	s.envsProcessed.Add(ctx, 1, metric.WithAttributes(
		attribute.String("chart", chartName),
		attribute.String("environment", env.Name),
	))

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
		renderStart := time.Now()
		baseManifest, err = s.renderer.Render(ctx, baseDir, env.ValueFiles)
		s.renderDuration.Record(ctx, time.Since(renderStart).Seconds(), metric.WithAttributes(
			attribute.String("chart", chartName),
			attribute.String("ref", "base"),
		))
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "rendering base")
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
	renderStart := time.Now()
	headManifest, err := s.renderer.Render(ctx, headDir, env.ValueFiles)
	s.renderDuration.Record(ctx, time.Since(renderStart).Seconds(), metric.WithAttributes(
		attribute.String("chart", chartName),
		attribute.String("ref", "head"),
	))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "rendering head")
		return domain.DiffResult{}, fmt.Errorf("failed to render PR changes: %w", err)
	}
	s.logger.Info("head manifest rendered", "chart", chartName, "env", env.Name, "size", len(headManifest))

	s.logger.Info("computing diffs", "chart", chartName, "env", env.Name)
	baseName := domain.DiffLabel(chartName, env.Name, pr.BaseRef)
	headName := domain.DiffLabel(chartName, env.Name, pr.HeadRef)

	// Compute semantic diff (dyff) - may be empty if dyff not available
	diffStart := time.Now()
	semanticDiff := s.semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
	s.logger.Info("semantic diff computed", "chart", chartName, "env", env.Name, "size", len(semanticDiff))

	// Always compute unified diff as fallback
	unifiedDiff := s.unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
	s.diffDuration.Record(ctx, time.Since(diffStart).Seconds(), metric.WithAttributes(
		attribute.String("chart", chartName),
		attribute.String("environment", env.Name),
	))
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

	span.SetAttributes(attribute.String("diff.status", status.String()))
	s.diffStatus.Add(ctx, 1, metric.WithAttributes(
		attribute.String("chart", chartName),
		attribute.String("environment", env.Name),
		attribute.String("status", status.String()),
	))

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
