package app

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/helm_cli"
	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
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

	envs := []domain.EnvironmentConfig{
		{Name: "staging", ValueFiles: []string{"env/staging-values.yaml"}},
		{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
	}

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

			diff := computeDiff(
				fmt.Sprintf("my-app/%s (%s)", env.Name, baseRef),
				fmt.Sprintf("my-app/%s (%s)", env.Name, headRef),
				baseManifest,
				headManifest,
			)

			hasChanges := diff != ""
			summary := "No changes detected."
			if hasChanges {
				summary = fmt.Sprintf("Changes detected in my-app for environment %s.", env.Name)
			}

			result := domain.DiffResult{
				ChartName:   "my-app",
				Environment: env.Name,
				BaseRef:     baseRef,
				HeadRef:     headRef,
				HasChanges:  hasChanges,
				UnifiedDiff: diff,
				Summary:     summary,
			}
			allResults = append(allResults, result)

			if !hasChanges {
				t.Fatal("expected changes but got none")
			}

			// Format as check run markdown (mirrors github_out.formatDiffText + metadata)
			checkRunMD := formatCheckRunMarkdown(result)

			goldenFile := filepath.Join(goldenDir, fmt.Sprintf("check-run-my-app-%s.md", env.Name))
			compareOrUpdateGolden(t, goldenFile, checkRunMD)
		})
	}

	// Generate PR summary comment
	prComment := formatPRComment(allResults)
	goldenFile := filepath.Join(goldenDir, "pr-comment.md")
	compareOrUpdateGolden(t, goldenFile, prComment)
}

// formatCheckRunMarkdown produces the markdown that mirrors what GitHub displays
// for a Check Run created by chart-sentry.
func formatCheckRunMarkdown(r domain.DiffResult) string {
	conclusion := "success"
	if r.HasChanges {
		conclusion = "neutral"
	}

	diffText := "No changes detected."
	if r.UnifiedDiff != "" {
		diffText = "```diff\n" + r.UnifiedDiff + "\n```"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "# chart-sentry: %s/%s\n\n", r.ChartName, r.Environment)
	fmt.Fprintf(&sb, "**Status:** completed\n")
	fmt.Fprintf(&sb, "**Conclusion:** %s\n\n", conclusion)
	fmt.Fprintf(&sb, "## Helm diff — %s (%s)\n\n", r.ChartName, r.Environment)
	fmt.Fprintf(&sb, "### Summary\n")
	fmt.Fprintf(&sb, "%s\n\n", r.Summary)
	fmt.Fprintf(&sb, "### Output\n")
	fmt.Fprintf(&sb, "%s\n", diffText)

	return sb.String()
}

// formatPRComment produces a summary comment aggregating all environment diffs.
func formatPRComment(results []domain.DiffResult) string {
	var sb strings.Builder
	sb.WriteString("## Chart-Sentry Diff Report\n\n")

	// Table header
	sb.WriteString("| Chart | Environment | Status |\n")
	sb.WriteString("|-------|-------------|--------|\n")
	for _, r := range results {
		status := "No Changes"
		if r.HasChanges {
			status = "Changed"
		}
		fmt.Fprintf(&sb, "| %s | %s | %s |\n", r.ChartName, r.Environment, status)
	}
	sb.WriteString("\n")

	// Detail sections
	for _, r := range results {
		fmt.Fprintf(&sb, "### %s/%s\n", r.ChartName, r.Environment)
		if !r.HasChanges {
			sb.WriteString("No changes detected.\n\n")
			continue
		}
		sb.WriteString("<details><summary>View diff</summary>\n\n")
		fmt.Fprintf(&sb, "```diff\n%s\n```\n", r.UnifiedDiff)
		sb.WriteString("</details>\n\n")
	}

	return sb.String()
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
