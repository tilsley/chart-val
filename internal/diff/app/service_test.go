package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	noopmetric "go.opentelemetry.io/otel/metric/noop"
	nooptrace "go.opentelemetry.io/otel/trace/noop"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/platform/logger"
)

// Mock adapters for testing

type mockSourceControl struct {
	charts map[string]bool // map of "ref:chartPath" -> exists
}

func (m *mockSourceControl) FetchChartFiles(
	_ context.Context,
	_, _, ref, chartPath string,
) (string, func(), error) {
	key := ref + ":" + chartPath
	if !m.charts[key] {
		return "", nil, domain.NewNotFoundError(chartPath, ref)
	}
	// Return ref:chartPath as dir so renderer can differentiate
	return key, func() {}, nil
}

type mockChangedCharts struct {
	charts []domain.ChangedChart
}

func (m *mockChangedCharts) GetChangedCharts(
	_ context.Context,
	_ domain.PRContext,
) ([]domain.ChangedChart, error) {
	return m.charts, nil
}

type mockEnvConfig struct {
	config  domain.ChartConfig            // default config
	configs map[string]domain.ChartConfig // per-chart configs
}

func (m *mockEnvConfig) GetEnvironmentConfig(
	_ context.Context,
	_ domain.PRContext,
	chartName string,
) (domain.ChartConfig, error) {
	if m.configs != nil {
		if cfg, ok := m.configs[chartName]; ok {
			return cfg, nil
		}
	}
	return m.config, nil
}

type mockRenderer struct {
	manifests map[string]string // chartDir -> rendered manifest
}

func (m *mockRenderer) Render(_ context.Context, chartDir string, _ []string) ([]byte, error) {
	if m.manifests != nil {
		if content, ok := m.manifests[chartDir]; ok {
			return []byte(content), nil
		}
	}
	return []byte("dummy manifest"), nil
}

type mockReporter struct {
	results      []domain.DiffResult
	checkRunID   int64
	commentCount int
}

func (m *mockReporter) CreateInProgressCheck(_ context.Context, _ domain.PRContext) (int64, error) {
	m.checkRunID++
	return m.checkRunID, nil
}

func (m *mockReporter) UpdateCheckWithResults(
	_ context.Context,
	_ domain.PRContext,
	_ int64,
	results []domain.DiffResult,
) error {
	m.results = append(m.results, results...)
	return nil
}

func (m *mockReporter) PostComment(
	_ context.Context,
	_ domain.PRContext,
	_ []domain.DiffResult,
) error {
	m.commentCount++
	return nil
}

type mockDiff struct{}

func (m *mockDiff) ComputeDiff(baseName, headName string, base, head []byte) string {
	if string(base) != string(head) {
		return fmt.Sprintf(
			"--- %s\n+++ %s\n@@ -1 +1 @@\n-%s\n+%s",
			baseName,
			headName,
			string(base),
			string(head),
		)
	}
	return ""
}

func TestService_NoChartChanges(t *testing.T) {
	srcCtrl := &mockSourceControl{charts: map[string]bool{}}
	changedCharts := &mockChangedCharts{charts: nil}
	envConfig := &mockEnvConfig{}
	renderer := &mockRenderer{}
	reporter := &mockReporter{}
	semanticDiff := &mockDiff{}
	unifiedDiff := &mockDiff{}
	log := logger.New("error")

	svc := NewDiffService(
		srcCtrl, changedCharts, nil, envConfig, renderer, reporter,
		semanticDiff, unifiedDiff, log,
		noopmetric.NewMeterProvider().Meter("test"),
		nooptrace.NewTracerProvider().Tracer("test"),
		"charts", "chart_val",
	)

	pr := domain.PRContext{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 42,
		BaseRef:  "main",
		HeadRef:  "feat/update-readme",
		HeadSHA:  "abc123",
	}

	err := svc.Execute(context.Background(), pr)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(reporter.results) != 0 {
		t.Errorf("expected 0 results when no chart changes, got %d", len(reporter.results))
	}

	if reporter.checkRunID != 0 {
		t.Errorf("expected no check runs created, but checkRunID is %d", reporter.checkRunID)
	}

	t.Logf("✓ Test passes: no chart changes handled correctly")
}

func TestService_NewChartNotInBase(t *testing.T) {
	srcCtrl := &mockSourceControl{
		charts: map[string]bool{
			"feat/add-chart:charts/new-chart": true,
			"main:charts/new-chart":           false,
		},
	}
	changedCharts := &mockChangedCharts{
		charts: []domain.ChangedChart{
			{Name: "new-chart", Path: "charts/new-chart"},
		},
	}
	envConfig := &mockEnvConfig{
		config: domain.ChartConfig{
			Path: "charts/new-chart",
			Environments: []domain.EnvironmentConfig{
				{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
			},
		},
	}
	renderer := &mockRenderer{}
	reporter := &mockReporter{}
	semanticDiff := &mockDiff{}
	unifiedDiff := &mockDiff{}
	log := logger.New("error")

	svc := NewDiffService(
		srcCtrl, changedCharts, nil, envConfig, renderer, reporter,
		semanticDiff, unifiedDiff, log,
		noopmetric.NewMeterProvider().Meter("test"),
		nooptrace.NewTracerProvider().Tracer("test"),
		"charts", "chart_val",
	)

	pr := domain.PRContext{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 1,
		BaseRef:  "main",
		HeadRef:  "feat/add-chart",
		HeadSHA:  "abc123",
	}

	err := svc.Execute(context.Background(), pr)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(reporter.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(reporter.results))
	}

	result := reporter.results[0]

	if strings.Contains(result.Summary, "Error fetching base chart") {
		t.Errorf("should not have error result anymore, but got: %s", result.Summary)
	}

	if result.Status != domain.StatusChanges {
		t.Errorf(
			"expected StatusChanges, got %v (new chart should show all additions)",
			result.Status,
		)
	}

	if !strings.Contains(result.UnifiedDiff, "+") {
		t.Error("expected additions in diff (+ lines)")
	}

	t.Logf("✓ Test passes: new chart handled correctly")
	t.Logf("   Result summary: %s", result.Summary)
	t.Logf("   Status: %v", result.Status)
}

func TestService_ThreeChartsOneChanged(t *testing.T) {
	// 3 charts in the PR, only app-a has actual changes
	srcCtrl := &mockSourceControl{
		charts: map[string]bool{
			"main:charts/app-a":        true,
			"feat/update:charts/app-a": true,
			"main:charts/app-b":        true,
			"feat/update:charts/app-b": true,
			"main:charts/app-c":        true,
			"feat/update:charts/app-c": true,
		},
	}
	changedCharts := &mockChangedCharts{
		charts: []domain.ChangedChart{
			{Name: "app-a", Path: "charts/app-a"},
			{Name: "app-b", Path: "charts/app-b"},
			{Name: "app-c", Path: "charts/app-c"},
		},
	}
	envs := []domain.EnvironmentConfig{{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}}}
	envConfig := &mockEnvConfig{
		configs: map[string]domain.ChartConfig{
			"app-a": {Path: "charts/app-a", Environments: envs},
			"app-b": {Path: "charts/app-b", Environments: envs},
			"app-c": {Path: "charts/app-c", Environments: envs},
		},
	}
	// app-a: different manifests between base and head (has changes)
	// app-b and app-c: same manifests (no changes)
	renderer := &mockRenderer{
		manifests: map[string]string{
			"main:charts/app-a":        "replicas: 1",
			"feat/update:charts/app-a": "replicas: 3",
			// app-b and app-c: same in base and head (default "dummy manifest")
		},
	}
	reporter := &mockReporter{}
	semanticDiff := &mockDiff{}
	unifiedDiff := &mockDiff{}
	log := logger.New("error")

	svc := NewDiffService(
		srcCtrl, changedCharts, nil, envConfig, renderer, reporter,
		semanticDiff, unifiedDiff, log,
		noopmetric.NewMeterProvider().Meter("test"),
		nooptrace.NewTracerProvider().Tracer("test"),
		"charts", "chart_val",
	)

	pr := domain.PRContext{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 1,
		BaseRef:  "main",
		HeadRef:  "feat/update",
		HeadSHA:  "abc123",
	}

	err := svc.Execute(context.Background(), pr)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// ONE check run for the entire PR
	if reporter.checkRunID != 1 {
		t.Errorf("expected exactly 1 check run, got %d", reporter.checkRunID)
	}

	// 3 results total (one per chart, one env each)
	if len(reporter.results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(reporter.results))
	}

	// Only 1 comment: for app-a (the chart with changes)
	// app-b and app-c have no changes, so no comments
	if reporter.commentCount != 1 {
		t.Errorf("expected 1 comment (only for changed chart), got %d", reporter.commentCount)
	}

	// Verify which charts have changes
	changesCount := 0
	noChangesCount := 0
	for _, r := range reporter.results {
		if r.Status == domain.StatusChanges {
			changesCount++
			if r.ChartName != "app-a" {
				t.Errorf("expected changes only for app-a, got changes for %s", r.ChartName)
			}
		} else {
			noChangesCount++
		}
	}

	if changesCount != 1 {
		t.Errorf("expected 1 chart with changes, got %d", changesCount)
	}
	if noChangesCount != 2 {
		t.Errorf("expected 2 charts without changes, got %d", noChangesCount)
	}

	t.Logf("✓ 3 charts, 1 changed: 1 check run, 1 comment, 2 silent")
}

func TestExtractChartNames(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected []string
	}{
		{
			name:     "no files",
			files:    []string{},
			expected: []string{},
		},
		{
			name: "no chart files",
			files: []string{
				"README.md",
				"src/main.go",
				"config/settings.yaml",
			},
			expected: []string{},
		},
		{
			name: "single chart",
			files: []string{
				"charts/my-app/Chart.yaml",
				"charts/my-app/values.yaml",
				"charts/my-app/templates/deployment.yaml",
			},
			expected: []string{"my-app"},
		},
		{
			name: "multiple charts",
			files: []string{
				"charts/app1/Chart.yaml",
				"charts/app2/values.yaml",
				"charts/app1/templates/service.yaml",
			},
			expected: []string{"app1", "app2"},
		},
		{
			name: "mixed files",
			files: []string{
				"README.md",
				"charts/my-app/Chart.yaml",
				"src/main.go",
				"charts/other-app/values.yaml",
				".github/workflows/ci.yml",
			},
			expected: []string{"my-app", "other-app"},
		},
		{
			name: "duplicate chart references",
			files: []string{
				"charts/my-app/Chart.yaml",
				"charts/my-app/values.yaml",
				"charts/my-app/templates/deployment.yaml",
				"charts/my-app/templates/service.yaml",
			},
			expected: []string{"my-app"},
		},
		{
			name: "invalid chart paths",
			files: []string{
				"charts/",
				"charts",
				"not-charts/app/",
				"charts/app/file.yaml",
			},
			expected: []string{"app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := domain.ExtractChartNames(tt.files, "charts")

			if len(result) != len(tt.expected) {
				t.Errorf(
					"expected %d chart names, got %d: %v",
					len(tt.expected),
					len(result),
					result,
				)
				return
			}

			resultMap := make(map[string]bool)
			for _, name := range result {
				resultMap[name] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("expected chart name %q not found in result: %v", expected, result)
				}
			}
		})
	}
}
