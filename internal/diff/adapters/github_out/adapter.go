// Package githubout handles GitHub output (check runs and PR comments).
package githubout

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

const maxCheckRunTextLen = 65535

// Adapter implements ports.ReportingPort by posting results via the
// GitHub Checks API.
type Adapter struct {
	client  *gogithub.Client
	appName string
	appURL  string
}

// New creates a new GitHub reporting adapter.
func New(client *gogithub.Client, appName, appURL string) *Adapter {
	return &Adapter{client: client, appName: appName, appURL: appURL}
}

// CreateInProgressCheck creates a single check run in "in_progress" status for the PR.
func (a *Adapter) CreateInProgressCheck(ctx context.Context, pr domain.PRContext) (int64, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	logger.Info("creating in-progress check", "pr", pr.PRNumber)

	client := a.client

	checkRun, _, err := client.Checks.CreateCheckRun(ctx, pr.Owner, pr.Repo, gogithub.CreateCheckRunOptions{
		Name:    a.appName,
		HeadSHA: pr.HeadSHA,
		Status:  gogithub.Ptr("in_progress"),
		Output: &gogithub.CheckRunOutput{
			Title:   gogithub.Ptr("Helm Diff"),
			Summary: gogithub.Ptr("Analyzing chart changes..."),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("creating in-progress check: %w", err)
	}

	logger.Info("in-progress check created", "checkRunID", checkRun.GetID())
	return checkRun.GetID(), nil
}

// UpdateCheckWithResults updates an existing check run with final results.
func (a *Adapter) UpdateCheckWithResults(
	ctx context.Context,
	pr domain.PRContext,
	checkRunID int64,
	results []domain.DiffResult,
) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	logger.Info("updating check run with results", "checkRunID", checkRunID, "numResults", len(results))

	if len(results) == 0 {
		return errors.New("no results to update check run")
	}

	client := a.client
	conclusion, summary, text := formatCheckRun(results)

	_, _, err := client.Checks.UpdateCheckRun(ctx, pr.Owner, pr.Repo, checkRunID, gogithub.UpdateCheckRunOptions{
		Name:       a.appName,
		Status:     gogithub.Ptr("completed"),
		Conclusion: gogithub.Ptr(conclusion),
		Output: &gogithub.CheckRunOutput{
			Title:   gogithub.Ptr("Helm Diff"),
			Summary: gogithub.Ptr(summary),
			Text:    gogithub.Ptr(text),
		},
	})
	if err != nil {
		return fmt.Errorf("updating check run: %w", err)
	}

	logger.Info("check run updated successfully", "checkRunID", checkRunID)
	return nil
}

// PostComment posts a PR comment with the diff summary for a single chart.
func (a *Adapter) PostComment(ctx context.Context, pr domain.PRContext, results []domain.DiffResult) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(results) == 0 {
		return errors.New("no results to post comment")
	}

	chartName := results[0].ChartName
	logger.Info("posting PR comment", "chart", chartName, "pr", pr.PRNumber)

	client := a.client
	commentMarker := fmt.Sprintf("<!-- %s: %s -->", a.appName, chartName)

	// Delete old comments for this chart to avoid bloat
	a.deleteMatchingComments(ctx, pr, commentMarker)

	commentBody := a.FormatPRComment(results)

	_, _, err := client.Issues.CreateComment(ctx, pr.Owner, pr.Repo, pr.PRNumber, &gogithub.IssueComment{
		Body: gogithub.Ptr(commentBody),
	})
	if err != nil {
		return fmt.Errorf("creating PR comment: %w", err)
	}

	logger.Info("PR comment posted successfully", "chart", chartName)
	return nil
}

// deleteMatchingComments deletes comments containing the given marker.
func (a *Adapter) deleteMatchingComments(ctx context.Context, pr domain.PRContext, marker string) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	client := a.client

	comments, _, err := client.Issues.ListComments(
		ctx,
		pr.Owner,
		pr.Repo,
		pr.PRNumber,
		&gogithub.IssueListCommentsOptions{},
	)
	if err != nil {
		logger.Warn("failed to list comments, continuing anyway", "error", err)
		return
	}
	for _, comment := range comments {
		if strings.Contains(comment.GetBody(), marker) {
			logger.Info("deleting old comment", "commentID", comment.GetID())
			_, err := client.Issues.DeleteComment(ctx, pr.Owner, pr.Repo, comment.GetID())
			if err != nil {
				logger.Warn("failed to delete old comment", "commentID", comment.GetID(), "error", err)
			}
		}
	}
}

// FormatCheckRunMarkdown formats a complete check run markdown document for testing.
// This includes the metadata header that GitHub displays.
func (a *Adapter) FormatCheckRunMarkdown(results []domain.DiffResult) string {
	if len(results) == 0 {
		return ""
	}

	conclusion, summary, text := formatCheckRun(results)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", a.appName)
	sb.WriteString("**Status:** completed\n")
	fmt.Fprintf(&sb, "**Conclusion:** %s\n\n", conclusion)
	sb.WriteString("## Helm Diff\n\n")
	fmt.Fprintf(&sb, "### Summary\n%s\n\n", summary)
	fmt.Fprintf(&sb, "### Output\n%s", text)

	return sb.String()
}

// formatCheckRun builds the conclusion, summary, and collapsible text for the check run.
// Groups results by chart, showing diffs for changed charts and listing unchanged charts.
func formatCheckRun(results []domain.DiffResult) (conclusion, summary, text string) {
	_, _, errorCount := domain.CountByStatus(results)
	conclusion = determineConclusion(errorCount)

	grouped, chartOrder := groupResultsByChart(results)
	changedCharts, unchangedCharts := separateChangedCharts(grouped, chartOrder)

	summary = buildSummary(chartOrder, changedCharts, unchangedCharts)
	text = buildCheckRunText(grouped, changedCharts, unchangedCharts)

	return conclusion, summary, text
}

func determineConclusion(errorCount int) string {
	if errorCount > 0 {
		return "failure"
	}
	return "success"
}

func groupResultsByChart(results []domain.DiffResult) (map[string][]domain.DiffResult, []string) {
	grouped := make(map[string][]domain.DiffResult)
	var chartOrder []string
	for _, r := range results {
		if _, exists := grouped[r.ChartName]; !exists {
			chartOrder = append(chartOrder, r.ChartName)
		}
		grouped[r.ChartName] = append(grouped[r.ChartName], r)
	}
	return grouped, chartOrder
}

func separateChangedCharts(grouped map[string][]domain.DiffResult, chartOrder []string) (changed, unchanged []string) {
	for _, name := range chartOrder {
		if chartHasChanges(grouped[name]) {
			changed = append(changed, name)
		} else {
			unchanged = append(unchanged, name)
		}
	}
	return changed, unchanged
}

func buildSummary(chartOrder, changedCharts, unchangedCharts []string) string {
	return fmt.Sprintf("Analyzed %d chart(s): %d with changes, %d unchanged",
		len(chartOrder), len(changedCharts), len(unchangedCharts))
}

func buildCheckRunText(grouped map[string][]domain.DiffResult, changedCharts, unchangedCharts []string) string {
	var sb strings.Builder
	formatChangedCharts(&sb, grouped, changedCharts)
	formatUnchangedCharts(&sb, unchangedCharts)
	return truncateIfNeeded(sb.String())
}

func formatChangedCharts(sb *strings.Builder, grouped map[string][]domain.DiffResult, changedCharts []string) {
	for _, chartName := range changedCharts {
		fmt.Fprintf(sb, "## %s\n\n", chartName)
		for _, r := range grouped[chartName] {
			formatEnvironmentResult(sb, r)
		}
	}
}

func formatEnvironmentResult(sb *strings.Builder, r domain.DiffResult) {
	statusLabel := getStatusLabel(r.Status)
	fmt.Fprintf(sb, "<details><summary>%s — %s</summary>\n\n", r.Environment, statusLabel)

	switch {
	case r.Status == domain.StatusError:
		fmt.Fprintf(sb, "%s\n", r.Summary)
	case r.UnifiedDiff == "" && r.SemanticDiff == "":
		sb.WriteString("No changes detected.\n")
	default:
		formatDiffs(sb, r)
	}

	sb.WriteString("\n</details>\n\n")
}

func getStatusLabel(status domain.Status) string {
	switch status {
	case domain.StatusError:
		return "Error"
	case domain.StatusChanges:
		return "Changed"
	case domain.StatusSuccess:
		return "No Changes"
	default:
		return "Unknown"
	}
}

func formatDiffs(sb *strings.Builder, r domain.DiffResult) {
	if r.SemanticDiff != "" {
		sb.WriteString("**Semantic Diff (dyff):**\n")
		fmt.Fprintf(sb, "```diff\n%s\n```\n\n", r.SemanticDiff)
	}
	if r.UnifiedDiff != "" {
		sb.WriteString("**Unified Diff (line-based):**\n")
		fmt.Fprintf(sb, "```diff\n%s\n```\n", r.UnifiedDiff)
	}
}

func formatUnchangedCharts(sb *strings.Builder, unchangedCharts []string) {
	if len(unchangedCharts) == 0 {
		return
	}

	sb.WriteString("## Unchanged charts\n\n")
	sb.WriteString("The following charts were analyzed and had no changes across all environments:\n\n")
	for _, name := range unchangedCharts {
		fmt.Fprintf(sb, "- `%s`\n", name)
	}
	sb.WriteString("\n")
}

func truncateIfNeeded(text string) string {
	if len(text) > maxCheckRunTextLen {
		truncMsg := "\n\n... (output truncated)"
		return text[:maxCheckRunTextLen-len(truncMsg)] + truncMsg
	}
	return text
}

// chartHasChanges returns true if any result for a chart has changes or errors.
func chartHasChanges(results []domain.DiffResult) bool {
	for _, r := range results {
		if r.Status == domain.StatusChanges || r.Status == domain.StatusError {
			return true
		}
	}
	return false
}

// FormatPRComment formats a PR comment body for a single chart's diff results.
// Exported for use in integration tests.
func (a *Adapter) FormatPRComment(results []domain.DiffResult) string {
	if len(results) == 0 {
		return ""
	}

	chartName := results[0].ChartName
	var sb strings.Builder

	// Hidden marker for identifying this comment (for deletion on updates)
	fmt.Fprintf(&sb, "<!-- %s: %s -->\n", a.appName, chartName)

	// Header
	fmt.Fprintf(&sb, "## 📊 Helm Diff Report: `%s`\n\n", chartName)

	// Summary counts
	_, changes, errorCount := domain.CountByStatus(results)

	switch {
	case errorCount > 0:
		sb.WriteString("❌ **Status:** Failed to analyze chart\n\n")
	case changes > 0:
		fmt.Fprintf(&sb, "✅ **Status:** Analysis complete — %d environment(s) with changes\n\n", changes)
	default:
		sb.WriteString("✅ **Status:** Analysis complete — No changes detected\n\n")
	}

	// Environment details table
	sb.WriteString("| Environment | Status |\n")
	sb.WriteString("|-------------|--------|\n")
	for _, r := range results {
		var statusLabel string
		switch r.Status {
		case domain.StatusError:
			statusLabel = "❌ Error"
		case domain.StatusChanges:
			statusLabel = "📝 Changed"
		case domain.StatusSuccess:
			statusLabel = "✅ No changes"
		}
		fmt.Fprintf(&sb, "| `%s` | %s |\n", r.Environment, statusLabel)
	}
	sb.WriteString("\n")

	// Detailed diffs per environment - prefer semantic diff in PR comments
	for _, r := range results {
		switch r.Status {
		case domain.StatusError:
			fmt.Fprintf(&sb, "<details>\n<summary><b>%s</b> — Error details</summary>\n\n", r.Environment)
			fmt.Fprintf(&sb, "%s\n\n", r.Summary)
			sb.WriteString("</details>\n\n")
		case domain.StatusChanges:
			fmt.Fprintf(&sb, "<details>\n<summary><b>%s</b> — View diff</summary>\n\n", r.Environment)
			fmt.Fprintf(&sb, "```diff\n%s\n```\n\n", r.PreferredDiff())
			sb.WriteString("</details>\n\n")
		case domain.StatusSuccess:
			// Skip environments with no changes (already shown in table)
		}
	}

	sb.WriteString("---\n")
	if a.appURL != "" {
		fmt.Fprintf(&sb, "_Posted by [%s](%s)_\n", a.appName, a.appURL)
	} else {
		fmt.Fprintf(&sb, "_Posted by %s_\n", a.appName)
	}

	return sb.String()
}
