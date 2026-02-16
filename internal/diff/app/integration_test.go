// Package app provides integration tests for the diff service.
package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	dyffdiff "github.com/nathantilsley/chart-val/internal/diff/adapters/dyff_diff"
	githubout "github.com/nathantilsley/chart-val/internal/diff/adapters/github_out"
	helmcli "github.com/nathantilsley/chart-val/internal/diff/adapters/helm_cli"
	linediff "github.com/nathantilsley/chart-val/internal/diff/adapters/line_diff"
	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

var update = flag.Bool("update", false, "update golden files")

const (
	testBranchMain = "main"
	testdataDir    = "testdata"
)

func TestIntegration_FullDiffFlow(t *testing.T) {
	if _, err := helmcli.New(); err != nil {
		t.Skipf("helm not on PATH, skipping integration test: %v", err)
	}

	renderer, err := helmcli.New()
	if err != nil {
		t.Fatalf("creating helm adapter: %v", err)
	}

	ctx := context.Background()
	baseChartDir := filepath.Join(testdataDir, "base", "my-app")
	headChartDir := filepath.Join(testdataDir, "head", "my-app")
	goldenDir := filepath.Join(testdataDir, "golden")

	baseRef := testBranchMain
	headRef := "feat/update-config"

	// Discover environments from head chart dir
	envs, err := discoverEnvironmentsFromDir(headChartDir)
	if err != nil {
		t.Fatalf("discovering environments: %v", err)
	}

	// Init diff adapters
	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	var allResults []domain.DiffResult

	for _, env := range envs {
		t.Run(env.Name, func(t *testing.T) {
			baseManifest, err := renderer.Render(ctx, baseChartDir, env.ValueFiles)
			if err != nil {
				t.Fatalf("rendering base for %s: %v", env.Name, err)
			}

			headManifest, err := renderer.Render(ctx, headChartDir, env.ValueFiles)
			if err != nil {
				t.Fatalf("rendering head for %s: %v", env.Name, err)
			}

			baseName := domain.DiffLabel("my-app", env.Name, baseRef)
			headName := domain.DiffLabel("my-app", env.Name, headRef)

			// Compute both diffs
			//nolint:contextcheck // ComputeDiff doesn't take context parameter
			semanticDiffOutput := semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
			unifiedDiffOutput := unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)

			var status domain.Status
			var summary string
			if semanticDiffOutput != "" || unifiedDiffOutput != "" {
				status = domain.StatusChanges
				summary = fmt.Sprintf("Changes detected in my-app for environment %s.", env.Name)
			} else {
				status = domain.StatusSuccess
				summary = noChangesMessage
			}

			result := domain.DiffResult{
				ChartName:    "my-app",
				Environment:  env.Name,
				BaseRef:      baseRef,
				HeadRef:      headRef,
				Status:       status,
				UnifiedDiff:  unifiedDiffOutput,
				SemanticDiff: semanticDiffOutput,
				Summary:      summary,
			}
			allResults = append(allResults, result)

			if status != domain.StatusChanges {
				t.Fatal("expected changes but got none")
			}
		})
	}

	// Generate grouped check run markdown (one per chart) - using production code
	reporter := githubout.New(nil, "chart-val", "")
	checkRunMD := reporter.FormatCheckRunMarkdown(allResults)
	goldenFile := filepath.Join(goldenDir, "check-run-my-app.md")
	compareOrUpdateGolden(t, goldenFile, checkRunMD)

	// Generate PR summary comment - using production code
	prComment := reporter.FormatPRComment(allResults)
	goldenFile = filepath.Join(goldenDir, "pr-comment.md")
	compareOrUpdateGolden(t, goldenFile, prComment)

	// Generate unified diff PR comment - using production code
	prCommentUnified := reporter.FormatPRCommentUnified(allResults)
	goldenFile = filepath.Join(goldenDir, "pr-comment-unified.md")
	compareOrUpdateGolden(t, goldenFile, prCommentUnified)
}

// TestIntegration_NewChart tests the scenario where a chart is being added
// for the first time (exists in HEAD but not in BASE).
func TestIntegration_NewChart(t *testing.T) {
	if _, err := helmcli.New(); err != nil {
		t.Skipf("helm not on PATH, skipping integration test: %v", err)
	}

	renderer, err := helmcli.New()
	if err != nil {
		t.Fatalf("creating helm adapter: %v", err)
	}

	ctx := context.Background()
	// Base chart does NOT exist - simulating new chart
	baseChartDir := filepath.Join(testdataDir, "base", "new-chart")
	headChartDir := filepath.Join(testdataDir, "head", "new-chart")
	goldenDir := filepath.Join(testdataDir, "golden")

	baseRef := testBranchMain
	headRef := "feat/add-new-chart"

	// Check that base chart does NOT exist
	if _, err := os.Stat(baseChartDir); err == nil {
		t.Fatalf("base chart should not exist for this test, but it does at %s", baseChartDir)
	}

	// Discover environments from head chart dir
	envs, err := discoverEnvironmentsFromDir(headChartDir)
	if err != nil {
		t.Fatalf("discovering environments: %v", err)
	}

	// Init diff adapters
	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	var allResults []domain.DiffResult

	for _, env := range envs {
		t.Run(env.Name, func(t *testing.T) {
			// For a new chart, base manifest should be empty
			var baseManifest []byte
			if _, err := os.Stat(baseChartDir); err == nil {
				baseManifest, err = renderer.Render(ctx, baseChartDir, env.ValueFiles)
				if err != nil {
					t.Fatalf("rendering base for %s: %v", env.Name, err)
				}
			}
			// else: baseManifest remains empty (nil/empty byte slice)

			headManifest, err := renderer.Render(ctx, headChartDir, env.ValueFiles)
			if err != nil {
				t.Fatalf("rendering head for %s: %v", env.Name, err)
			}

			baseName := domain.DiffLabel("new-chart", env.Name, baseRef)
			headName := domain.DiffLabel("new-chart", env.Name, headRef)

			// Compute both diffs
			//nolint:contextcheck // ComputeDiff doesn't take context parameter
			semanticDiffOutput := semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
			unifiedDiffOutput := unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)

			var status domain.Status
			var summary string
			if semanticDiffOutput != "" || unifiedDiffOutput != "" {
				status = domain.StatusChanges
				summary = fmt.Sprintf("Changes detected in new-chart for environment %s.", env.Name)
			} else {
				status = domain.StatusSuccess
				summary = noChangesMessage
			}

			result := domain.DiffResult{
				ChartName:    "new-chart",
				Environment:  env.Name,
				BaseRef:      baseRef,
				HeadRef:      headRef,
				Status:       status,
				UnifiedDiff:  unifiedDiffOutput,
				SemanticDiff: semanticDiffOutput,
				Summary:      summary,
			}
			allResults = append(allResults, result)

			if status != domain.StatusChanges {
				t.Fatal("expected changes but got none (new chart should show all additions)")
			}
		})
	}

	// Generate grouped check run markdown - using production code
	reporter := githubout.New(nil, "chart-val", "")
	checkRunMD := reporter.FormatCheckRunMarkdown(allResults)
	goldenFile := filepath.Join(goldenDir, "check-run-new-chart.md")
	compareOrUpdateGolden(t, goldenFile, checkRunMD)
}

// TestIntegration_ThreeChartsOneChanged tests the scenario where 3 charts are in
// a PR but only 1 has actual changes. Verifies:
// - Changed chart produces diff results
// - Unchanged charts produce no-change results
// - Check run groups changed/unchanged correctly
// - Only the changed chart gets a PR comment
func TestIntegration_ThreeChartsOneChanged(t *testing.T) {
	if _, err := helmcli.New(); err != nil {
		t.Skipf("helm not on PATH, skipping integration test: %v", err)
	}

	renderer, err := helmcli.New()
	if err != nil {
		t.Fatalf("creating helm adapter: %v", err)
	}

	ctx := context.Background()
	goldenDir := filepath.Join(testdataDir, "golden")

	baseRef := testBranchMain
	headRef := "feat/update-config"

	// 3 charts: my-app (changed), stable-app (unchanged), another-app (unchanged)
	charts := []struct {
		name       string
		hasChanges bool
	}{
		{"my-app", true},
		{"stable-app", false},
		{"another-app", false},
	}

	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	allResults := make([]domain.DiffResult, 0, len(charts))
	changedResults := make([]domain.DiffResult, 0, len(charts))

	for _, chart := range charts {
		baseChartDir := filepath.Join(testdataDir, "base", chart.name)
		headChartDir := filepath.Join(testdataDir, "head", chart.name)

		envs, err := discoverEnvironmentsFromDir(headChartDir)
		if err != nil {
			t.Fatalf("discovering environments for %s: %v", chart.name, err)
		}

		for _, env := range envs {
			baseManifest, err := renderer.Render(ctx, baseChartDir, env.ValueFiles)
			if err != nil {
				t.Fatalf("rendering base for %s/%s: %v", chart.name, env.Name, err)
			}

			headManifest, err := renderer.Render(ctx, headChartDir, env.ValueFiles)
			if err != nil {
				t.Fatalf("rendering head for %s/%s: %v", chart.name, env.Name, err)
			}

			baseName := domain.DiffLabel(chart.name, env.Name, baseRef)
			headName := domain.DiffLabel(chart.name, env.Name, headRef)

			semanticDiffOutput := semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
			unifiedDiffOutput := unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)

			var status domain.Status
			var summary string
			if semanticDiffOutput != "" || unifiedDiffOutput != "" {
				status = domain.StatusChanges
				summary = fmt.Sprintf("Changes detected in %s for environment %s.", chart.name, env.Name)
			} else {
				status = domain.StatusSuccess
				summary = noChangesMessage
			}

			result := domain.DiffResult{
				ChartName:    chart.name,
				Environment:  env.Name,
				BaseRef:      baseRef,
				HeadRef:      headRef,
				Status:       status,
				UnifiedDiff:  unifiedDiffOutput,
				SemanticDiff: semanticDiffOutput,
				Summary:      summary,
			}
			allResults = append(allResults, result)
		}
	}

	// Verify: only my-app has changes
	for _, r := range allResults {
		hasChange := r.Status == domain.StatusChanges
		isMyApp := r.ChartName == "my-app"

		if hasChange && !isMyApp {
			t.Errorf("unexpected change in %s/%s", r.ChartName, r.Environment)
		}
		if !hasChange && isMyApp {
			t.Errorf("expected change in %s/%s but got none", r.ChartName, r.Environment)
		}
		if hasChange {
			changedResults = append(changedResults, r)
		}
	}

	// Check run should show all charts (changed + unchanged)
	reporter := githubout.New(nil, "chart-val", "")
	checkRunMD := reporter.FormatCheckRunMarkdown(allResults)
	goldenFile := filepath.Join(goldenDir, "check-run-three-charts.md")
	compareOrUpdateGolden(t, goldenFile, checkRunMD)

	// PR comment should only be for the changed chart (my-app)
	prComment := reporter.FormatPRComment(changedResults)
	goldenFile = filepath.Join(goldenDir, "pr-comment-three-charts.md")
	compareOrUpdateGolden(t, goldenFile, prComment)

	// Unified diff PR comment for the changed chart
	prCommentUnified := reporter.FormatPRCommentUnified(changedResults)
	goldenFile = filepath.Join(goldenDir, "pr-comment-three-charts-unified.md")
	compareOrUpdateGolden(t, goldenFile, prCommentUnified)
}

// compareOrUpdateGolden either updates the golden file or compares against it.
func compareOrUpdateGolden(t *testing.T, path, actual string) {
	t.Helper()

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o600); err != nil {
			t.Fatalf("writing golden file %s: %v", path, err)
		}
		t.Logf("updated golden file: %s", path)
		return
	}

	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden file %s (run with -update to create): %v", path, err)
	}

	if string(expected) != actual {
		t.Errorf("output does not match golden file %s\n\n--- expected ---\n%s\n--- actual ---\n%s",
			path, string(expected), actual)
	}
}

// discoverEnvironmentsFromDir scans chartDir/env/ for files matching *-values.yaml.
// This is a test helper that mirrors the filesystem adapter's discovery logic.
func discoverEnvironmentsFromDir(chartDir string) ([]domain.EnvironmentConfig, error) {
	envDir := filepath.Join(chartDir, "env")

	entries, err := os.ReadDir(envDir)
	if err != nil {
		return nil, fmt.Errorf("reading env directory: %w", err)
	}

	var configs []domain.EnvironmentConfig
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, "-values.yaml") {
			continue
		}
		envName := strings.TrimSuffix(name, "-values.yaml")
		configs = append(configs, domain.EnvironmentConfig{
			Name:       envName,
			ValueFiles: []string{filepath.Join("env", name)},
		})
	}

	sort.Slice(configs, func(i, j int) bool {
		return configs[i].Name < configs[j].Name
	})

	return configs, nil
}
