// Package github provides authenticated GitHub API clients.
package github

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gogithub "github.com/google/go-github/v68/github"
)

// NewClient creates a GitHub API client authenticated as a GitHub App installation.
// The ghinstallation transport automatically handles token renewal.
func NewClient(appID, installationID int64, privateKeyPEM string) (*gogithub.Client, error) {
	// Create installation transport - handles JWT generation and token refresh
	transport, err := ghinstallation.New(http.DefaultTransport, appID, installationID, []byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("creating github installation transport: %w", err)
	}

	// Return client with auto-renewing transport
	return gogithub.NewClient(&http.Client{Transport: transport}), nil
}
