package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/platform/logger"
)

// Mock adapters for testing

type mockSourceControl struct {
	charts map[string]bool // map of chartPath -> exists
}

func (m *mockSourceControl) FetchChartFiles(_ context.Context, _, _, ref, chartPath string) (string, func(), error) {
	key := ref + ":" + chartPath
	if !m.charts[key] {
		return "", nil, domain.NewNotFoundError(chartPath, ref)
	}
	// Return a dummy path - won't actually be used in this test
	return "/tmp/dummy", func() {}, nil
}

type mockEnvDiscovery struct{}

func (m *mockEnvDiscovery) DiscoverEnvironments(_ context.Context, _ string) ([]domain.EnvironmentConfig, error) {
	return []domain.EnvironmentConfig{
		{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
	}, nil
}

type mockRenderer struct{}

func (m *mockRenderer) Render(_ context.Context, _ string, _ []string) ([]byte, error) {
	return []byte("dummy manifest"), nil
}

type mockReporter struct {
	results    []domain.DiffResult
	checkRunID int64
}

func (m *mockReporter) CreateInProgressCheck(_ context.Context, _ domain.PRContext, _ string) (int64, error) {
	m.checkRunID++
	return m.checkRunID, nil
}

func (m *mockReporter) UpdateCheckWithResults(_ context.Context, _ domain.PRContext, _ int64, results []domain.DiffResult) error {
	m.results = append(m.results, results...)
	return nil
}

func (m *mockReporter) PostComment(_ context.Context, _ domain.PRContext, _ []domain.DiffResult) error {
	return nil
}

type mockFileChanges struct {
	files []string
}

func (m *mockFileChanges) GetChangedFiles(_ context.Context, _, _ string, _ int) ([]string, error) {
	return m.files, nil
}

type mockDiff struct{}

func (m *mockDiff) ComputeDiff(baseName, headName string, base, head []byte) string {
	// Simple mock: return non-empty if content differs
	if string(base) != string(head) {
		return fmt.Sprintf("--- %s\n+++ %s\n@@ -1 +1 @@\n-%s\n+%s", baseName, headName, string(base), string(head))
	}
	return ""
}

func TestService_NoChartChanges(t *testing.T) {
	// Setup mocks
	srcCtrl := &mockSourceControl{charts: map[string]bool{}}
	envDiscovery := &mockEnvDiscovery{}
	renderer := &mockRenderer{}
	reporter := &mockReporter{}
	fileChanges := &mockFileChanges{
		// Changed files NOT in charts/ directory
		files: []string{
			"README.md",
			"src/main.go",
			"config/settings.yaml",
			".github/workflows/ci.yml",
		},
	}

	log := logger.New("error")
	semanticDiff := &mockDiff{}
	unifiedDiff := &mockDiff{}
	svc := NewDiffService(srcCtrl, envDiscovery, renderer, reporter, fileChanges, semanticDiff, unifiedDiff, log)

	pr := domain.PRContext{
		Owner:    "test-owner",
		Repo:     "test-repo",
		PRNumber: 1,
		BaseRef:  "main",
		HeadRef:  "feat/update-readme",
		HeadSHA:  "abc123",
	}

	err := svc.Execute(context.Background(), pr)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Should have NO results - no check runs created
	if len(reporter.results) != 0 {
		t.Errorf("expected 0 results when no chart changes, got %d", len(reporter.results))
	}

	// Should have NO check runs created
	if reporter.checkRunID != 0 {
		t.Errorf("expected no check runs created, but checkRunID is %d", reporter.checkRunID)
	}

	t.Logf("✓ Test passes: no chart changes handled correctly")
}

func TestService_NewChartNotInBase(t *testing.T) {
	// Setup mocks
	srcCtrl := &mockSourceControl{
		charts: map[string]bool{
			// new-chart exists in HEAD but NOT in BASE
			"feat/add-chart:charts/new-chart": true,
			// base ref doesn't have it
			"main:charts/new-chart": false,
		},
	}
	envDiscovery := &mockEnvDiscovery{}
	renderer := &mockRenderer{}
	reporter := &mockReporter{}
	fileChanges := &mockFileChanges{
		files: []string{"charts/new-chart/Chart.yaml"},
	}

	log := logger.New("error") // Quiet logs for test
	semanticDiff := &mockDiff{}
	unifiedDiff := &mockDiff{}
	svc := NewDiffService(srcCtrl, envDiscovery, renderer, reporter, fileChanges, semanticDiff, unifiedDiff, log)

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

	// Check the results - should have one error result
	if len(reporter.results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(reporter.results))
	}

	result := reporter.results[0]

	// With the fix, we should NOT get an error result
	// Instead, we should get a proper diff showing all additions
	if strings.Contains(result.Summary, "Error fetching base chart") {
		t.Errorf("should not have error result anymore, but got: %s", result.Summary)
	}

	if result.Status != domain.StatusChanges {
		t.Errorf("expected StatusChanges, got %v (new chart should show all additions)", result.Status)
	}

	if !strings.Contains(result.UnifiedDiff, "+") {
		t.Error("expected additions in diff (+ lines)")
	}

	t.Logf("✓ Test passes: new chart handled correctly")
	t.Logf("   Result summary: %s", result.Summary)
	t.Logf("   Status: %v", result.Status)
}

func TestExtractChartNames(t *testing.T) {
	// This now tests the domain function
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
				"charts/",         // no chart name
				"charts",          // not a path
				"not-charts/app/", // wrong prefix
				"charts/app/file.yaml",
			},
			expected: []string{"app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := domain.ExtractChartNames(tt.files)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d chart names, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			// Convert to maps for easier comparison (order doesn't matter)
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
