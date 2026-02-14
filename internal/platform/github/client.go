// Package github provides authenticated GitHub API clients.
package github

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gogithub "github.com/google/go-github/v68/github"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewClient creates a GitHub API client authenticated as a GitHub App installation.
// The ghinstallation transport automatically handles token renewal.
func NewClient(appID, installationID int64, privateKeyPEM string) (*gogithub.Client, error) {
	// Wrap base transport with OTel HTTP instrumentation so every GitHub API
	// call appears as a child span (method, URL, status code, duration).
	// When OTel is disabled (noop global provider), this is zero-overhead.
	base := otelhttp.NewTransport(http.DefaultTransport)

	// Create installation transport - handles JWT generation and token refresh
	transport, err := ghinstallation.New(base, appID, installationID, []byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("creating github installation transport: %w", err)
	}

	// Return client with auto-renewing transport
	return gogithub.NewClient(&http.Client{Transport: transport}), nil
}
