package github

import (
	"fmt"
	"net/http"

	"github.com/bradleyfalzon/ghinstallation/v2"
	gogithub "github.com/google/go-github/v68/github"
)

// ClientFactory creates per-installation GitHub API clients using
// GitHub App JWT authentication.
type ClientFactory struct {
	transport *ghinstallation.AppsTransport
}

// NewClientFactory initialises a ClientFactory from the App ID and
// private key PEM contents.
func NewClientFactory(appID int64, privateKeyPEM string) (*ClientFactory, error) {
	transport, err := ghinstallation.NewAppsTransport(http.DefaultTransport, appID, []byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("creating github app transport: %w", err)
	}
	return &ClientFactory{transport: transport}, nil
}

// ForInstallation returns an authenticated GitHub client scoped to
// the given installation ID.
func (f *ClientFactory) ForInstallation(installationID int64) *gogithub.Client {
	transport := ghinstallation.NewFromAppsTransport(f.transport, installationID)
	return gogithub.NewClient(&http.Client{Transport: transport})
}
