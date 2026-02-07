package prfiles

import (
	"context"
	"fmt"

	"github.com/google/go-github/v68/github"
	ghclient "github.com/nathantilsley/chart-sentry/internal/platform/github"
)

// Adapter implements ports.FileChangesPort by querying the GitHub API
// for files changed in a pull request.
type Adapter struct {
	clientFactory *ghclient.ClientFactory
}

// New creates a new PR files adapter.
func New(cf *ghclient.ClientFactory) *Adapter {
	return &Adapter{clientFactory: cf}
}

// GetChangedFiles returns a list of files modified in the PR.
func (a *Adapter) GetChangedFiles(ctx context.Context, owner, repo string, prNumber int, installationID int64) ([]string, error) {
	client := a.clientFactory.ForInstallation(installationID)

	var changedFiles []string
	opts := &github.ListOptions{
		PerPage: 100,
	}

	for {
		files, resp, err := client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}

		for _, file := range files {
			changedFiles = append(changedFiles, file.GetFilename())
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return changedFiles, nil
}
