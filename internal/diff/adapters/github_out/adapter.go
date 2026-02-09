package githubout

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
)

const maxCheckRunTextLen = 65535

// Adapter implements ports.ReportingPort by posting results via the
// GitHub Checks API.
type Adapter struct {
	client *gogithub.Client
}

// New creates a new GitHub reporting adapter.
func New(client *gogithub.Client) *Adapter {
	return &Adapter{client: client}
}

// CreateInProgressCheck creates a check run in "in_progress" status.
func (a *Adapter) CreateInProgressCheck(ctx context.Context, pr domain.PRContext, chartName string) (int64, error) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	logger.Info("creating in-progress check", "chart", chartName, "pr", pr.PRNumber)

	client := a.client

	checkRun, _, err := client.Checks.CreateCheckRun(ctx, pr.Owner, pr.Repo, gogithub.CreateCheckRunOptions{
		Name:    fmt.Sprintf("chart-sentry: %s", chartName),
		HeadSHA: pr.HeadSHA,
		Status:  gogithub.Ptr("in_progress"),
		Output: &gogithub.CheckRunOutput{
			Title:   gogithub.Ptr(fmt.Sprintf("Helm diff — %s", chartName)),
			Summary: gogithub.Ptr("Analyzing chart changes..."),
		},
	})
	if err != nil {
		return 0, fmt.Errorf("creating in-progress check: %w", err)
	}

	logger.Info("in-progress check created", "chart", chartName, "checkRunID", checkRun.GetID())
	return checkRun.GetID(), nil
}

// UpdateCheckWithResults updates an existing check run with final results.
func (a *Adapter) UpdateCheckWithResults(ctx context.Context, pr domain.PRContext, checkRunID int64, results []domain.DiffResult) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	logger.Info("updating check run with results", "checkRunID", checkRunID, "numResults", len(results))

	if len(results) == 0 {
		return fmt.Errorf("no results to update check run")
	}

	client := a.client
	conclusion, summary, text := formatChartCheckRun(results)

	_, _, err := client.Checks.UpdateCheckRun(ctx, pr.Owner, pr.Repo, checkRunID, gogithub.UpdateCheckRunOptions{
		Name:       fmt.Sprintf("chart-sentry: %s", results[0].ChartName),
		Status:     gogithub.Ptr("completed"),
		Conclusion: gogithub.Ptr(conclusion),
		Output: &gogithub.CheckRunOutput{
			Title:   gogithub.Ptr(fmt.Sprintf("Helm diff — %s", results[0].ChartName)),
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

// PostComment posts a PR comment with the diff summary for a chart.
func (a *Adapter) PostComment(ctx context.Context, pr domain.PRContext, results []domain.DiffResult) error {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	logger.Info("posting PR comment", "chart", results[0].ChartName, "pr", pr.PRNumber)

	if len(results) == 0 {
		return fmt.Errorf("no results to post comment")
	}

	client := a.client
	chartName := results[0].ChartName
	commentMarker := fmt.Sprintf("<!-- chart-sentry: %s -->", chartName)

	// Delete old comments for this chart to avoid bloat
	logger.Info("checking for existing comments to delete", "chart", chartName)
	comments, _, err := client.Issues.ListComments(ctx, pr.Owner, pr.Repo, pr.PRNumber, &gogithub.IssueListCommentsOptions{})
	if err != nil {
		logger.Warn("failed to list comments, continuing anyway", "error", err)
	} else {
		for _, comment := range comments {
			if strings.Contains(comment.GetBody(), commentMarker) {
				logger.Info("deleting old comment", "commentID", comment.GetID())
				_, err := client.Issues.DeleteComment(ctx, pr.Owner, pr.Repo, comment.GetID())
				if err != nil {
					logger.Warn("failed to delete old comment", "commentID", comment.GetID(), "error", err)
				}
			}
		}
	}

	// Format and post new comment
	commentBody := formatPRComment(results)

	_, _, err = client.Issues.CreateComment(ctx, pr.Owner, pr.Repo, pr.PRNumber, &gogithub.IssueComment{
		Body: gogithub.Ptr(commentBody),
	})
	if err != nil {
		return fmt.Errorf("creating PR comment: %w", err)
	}

	logger.Info("PR comment posted successfully", "chart", results[0].ChartName)
	return nil
}

// formatChartCheckRun builds the conclusion, summary, and collapsible text
// for a single chart's Check Run.
func formatChartCheckRun(group []domain.DiffResult) (conclusion, summary, text string) {
	success, changes, errors := domain.CountByStatus(group)

	// Set conclusion based on errors only
	// Success = diff completed successfully (regardless of whether changes exist)
	// Failure = error occurred during diff
	if errors > 0 {
		conclusion = "failure"
		summary = fmt.Sprintf("❌ Failed to analyze chart (see details below)")
	} else {
		conclusion = "success"
		summary = buildSummary(len(group), changes, success)
	}

	var sb strings.Builder
	for i, r := range group {
		if i > 0 {
			sb.WriteString("\n")
		}

		// Determine status label for this environment
		var statusLabel string
		switch r.Status {
		case domain.StatusError:
			statusLabel = "Error"
		case domain.StatusChanges:
			statusLabel = "Changed"
		case domain.StatusSuccess:
			statusLabel = "No Changes"
		}

		fmt.Fprintf(&sb, "<details><summary>%s — %s</summary>\n\n", r.Environment, statusLabel)

		// Show error or diff
		if r.Status == domain.StatusError {
			fmt.Fprintf(&sb, "%s\n", r.Summary)
		} else if r.UnifiedDiff == "" && r.SemanticDiff == "" {
			sb.WriteString("No changes detected.\n")
		} else {
			// Check Runs: Show both diffs (semantic first, then unified)
			if r.SemanticDiff != "" {
				sb.WriteString("**Semantic Diff (dyff):**\n")
				fmt.Fprintf(&sb, "```diff\n%s\n```\n\n", r.SemanticDiff)
			}
			if r.UnifiedDiff != "" {
				sb.WriteString("**Unified Diff (line-based):**\n")
				fmt.Fprintf(&sb, "```diff\n%s\n```\n", r.UnifiedDiff)
			}
		}

		sb.WriteString("\n</details>\n")
	}

	text = sb.String()
	if len(text) > maxCheckRunTextLen {
		truncMsg := "\n\n... (output truncated)"
		text = text[:maxCheckRunTextLen-len(truncMsg)] + truncMsg
	}

	return conclusion, summary, text
}

func buildSummary(total, changed, unchanged int) string {
	return fmt.Sprintf("Analyzed %d environment(s): %d changed, %d unchanged", total, changed, unchanged)
}

// formatPRComment formats a PR comment body for a chart's diff results.
func formatPRComment(results []domain.DiffResult) string {
	if len(results) == 0 {
		return ""
	}

	chartName := results[0].ChartName
	var sb strings.Builder

	// Hidden marker for identifying this comment (for deletion on updates)
	fmt.Fprintf(&sb, "<!-- chart-sentry: %s -->\n", chartName)

	// Header
	fmt.Fprintf(&sb, "## 📊 Helm Diff Report: `%s`\n\n", chartName)

	// Summary counts
	_, changes, errors := domain.CountByStatus(results)

	if errors > 0 {
		fmt.Fprintf(&sb, "❌ **Status:** Failed to analyze chart\n\n")
	} else if changes > 0 {
		fmt.Fprintf(&sb, "✅ **Status:** Analysis complete — %d environment(s) with changes\n\n", changes)
	} else {
		fmt.Fprintf(&sb, "✅ **Status:** Analysis complete — No changes detected\n\n")
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
			// Show error details
			fmt.Fprintf(&sb, "<details>\n<summary><b>%s</b> — Error details</summary>\n\n", r.Environment)
			fmt.Fprintf(&sb, "%s\n\n", r.Summary)
			sb.WriteString("</details>\n\n")
		case domain.StatusChanges:
			// Show diff - use PreferredDiff (semantic if available, otherwise unified)
			fmt.Fprintf(&sb, "<details>\n<summary><b>%s</b> — View diff</summary>\n\n", r.Environment)
			fmt.Fprintf(&sb, "```diff\n%s\n```\n\n", r.PreferredDiff())
			sb.WriteString("</details>\n\n")
		case domain.StatusSuccess:
			// Skip environments with no changes (already shown in table)
		}
	}

	sb.WriteString("---\n")
	sb.WriteString("_Posted by [chart-sentry](https://github.com/nathantilsley/chart-sentry)_\n")

	return sb.String()
}
