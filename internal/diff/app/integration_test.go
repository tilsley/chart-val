package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	dyffdiff "github.com/nathantilsley/chart-val/internal/diff/adapters/dyff_diff"
	envdiscovery "github.com/nathantilsley/chart-val/internal/diff/adapters/env_discovery"
	githubout "github.com/nathantilsley/chart-val/internal/diff/adapters/github_out"
	helmcli "github.com/nathantilsley/chart-val/internal/diff/adapters/helm_cli"
	linediff "github.com/nathantilsley/chart-val/internal/diff/adapters/line_diff"
	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

var update = flag.Bool("update", false, "update golden files")

func TestIntegration_FullDiffFlow(t *testing.T) {
	if _, err := helmcli.New(); err != nil {
		t.Skipf("helm not on PATH, skipping integration test: %v", err)
	}

	renderer, err := helmcli.New()
	if err != nil {
		t.Fatalf("creating helm adapter: %v", err)
	}

	ctx := context.Background()
	testdataDir := filepath.Join("testdata")
	baseChartDir := filepath.Join(testdataDir, "base", "my-app")
	headChartDir := filepath.Join(testdataDir, "head", "my-app")
	goldenDir := filepath.Join(testdataDir, "golden")

	baseRef := "main"
	headRef := "feat/update-config"

	// Discover environments from head chart dir
	discovery := envdiscovery.New()
	envs, err := discovery.DiscoverEnvironments(ctx, headChartDir)
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
			semanticDiffOutput := semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
			unifiedDiffOutput := unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)

			var status domain.Status
			var summary string
			if semanticDiffOutput != "" || unifiedDiffOutput != "" {
				status = domain.StatusChanges
				summary = fmt.Sprintf("Changes detected in my-app for environment %s.", env.Name)
			} else {
				status = domain.StatusSuccess
				summary = "No changes detected."
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
	checkRunMD := githubout.FormatCheckRunMarkdown(allResults)
	goldenFile := filepath.Join(goldenDir, "check-run-my-app.md")
	compareOrUpdateGolden(t, goldenFile, checkRunMD)

	// Generate PR summary comment - using production code
	prComment := githubout.FormatPRComment(allResults)
	goldenFile = filepath.Join(goldenDir, "pr-comment.md")
	compareOrUpdateGolden(t, goldenFile, prComment)
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
	testdataDir := filepath.Join("testdata")
	// Base chart does NOT exist - simulating new chart
	baseChartDir := filepath.Join(testdataDir, "base", "new-chart")
	headChartDir := filepath.Join(testdataDir, "head", "new-chart")
	goldenDir := filepath.Join(testdataDir, "golden")

	baseRef := "main"
	headRef := "feat/add-new-chart"

	// Check that base chart does NOT exist
	if _, err := os.Stat(baseChartDir); err == nil {
		t.Fatalf("base chart should not exist for this test, but it does at %s", baseChartDir)
	}

	// Discover environments from head chart dir
	discovery := envdiscovery.New()
	envs, err := discovery.DiscoverEnvironments(ctx, headChartDir)
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
			semanticDiffOutput := semanticDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)
			unifiedDiffOutput := unifiedDiff.ComputeDiff(baseName, headName, baseManifest, headManifest)

			var status domain.Status
			var summary string
			if semanticDiffOutput != "" || unifiedDiffOutput != "" {
				status = domain.StatusChanges
				summary = fmt.Sprintf("Changes detected in new-chart for environment %s.", env.Name)
			} else {
				status = domain.StatusSuccess
				summary = "No changes detected."
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
	checkRunMD := githubout.FormatCheckRunMarkdown(allResults)
	goldenFile := filepath.Join(goldenDir, "check-run-new-chart.md")
	compareOrUpdateGolden(t, goldenFile, checkRunMD)
}

// compareOrUpdateGolden either updates the golden file or compares against it.
func compareOrUpdateGolden(t *testing.T, path, actual string) {
	t.Helper()

	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(actual), 0o644); err != nil {
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
