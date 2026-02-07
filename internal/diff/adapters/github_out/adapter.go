package githubout

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/nathantilsley/chart-sentry/internal/diff/domain"
	ghclient "github.com/nathantilsley/chart-sentry/internal/platform/github"
)

const maxCheckRunTextLen = 65535

// Adapter implements ports.ReportingPort by posting results via the
// GitHub Checks API.
type Adapter struct {
	clientFactory *ghclient.ClientFactory
}

// New creates a new GitHub reporting adapter.
func New(cf *ghclient.ClientFactory) *Adapter {
	return &Adapter{clientFactory: cf}
}

// PostResult creates one GitHub Check Run per environment with the diff output.
func (a *Adapter) PostResult(ctx context.Context, pr domain.PRContext, results []domain.DiffResult) error {
	client := a.clientFactory.ForInstallation(pr.InstallationID)

	for _, r := range results {
		conclusion := "success"
		if r.HasChanges {
			conclusion = "neutral"
		}

		text := formatDiffText(r.UnifiedDiff)

		_, _, err := client.Checks.CreateCheckRun(ctx, pr.Owner, pr.Repo, gogithub.CreateCheckRunOptions{
			Name:       fmt.Sprintf("chart-sentry: %s/%s", r.ChartName, r.Environment),
			HeadSHA:    pr.HeadSHA,
			Status:     gogithub.Ptr("completed"),
			Conclusion: gogithub.Ptr(conclusion),
			Output: &gogithub.CheckRunOutput{
				Title:   gogithub.Ptr(fmt.Sprintf("Helm diff — %s (%s)", r.ChartName, r.Environment)),
				Summary: gogithub.Ptr(r.Summary),
				Text:    gogithub.Ptr(text),
			},
		})
		if err != nil {
			return fmt.Errorf("creating check run for %s/%s: %w", r.ChartName, r.Environment, err)
		}
	}

	return nil
}

func formatDiffText(diff string) string {
	if diff == "" {
		return "No changes detected."
	}

	text := "```diff\n" + diff + "\n```"
	if len(text) > maxCheckRunTextLen {
		truncMsg := "\n\n... (output truncated)"
		text = text[:maxCheckRunTextLen-len(truncMsg)] + truncMsg
	}
	return text
}
