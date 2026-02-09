package main

import (
	"fmt"
	"log/slog"

	gogithub "github.com/google/go-github/v68/github"

	dyffdiff "github.com/nathantilsley/chart-val/internal/diff/adapters/dyff_diff"
	envdiscovery "github.com/nathantilsley/chart-val/internal/diff/adapters/env_discovery"
	githubin "github.com/nathantilsley/chart-val/internal/diff/adapters/github_in"
	githubout "github.com/nathantilsley/chart-val/internal/diff/adapters/github_out"
	helmcli "github.com/nathantilsley/chart-val/internal/diff/adapters/helm_cli"
	linediff "github.com/nathantilsley/chart-val/internal/diff/adapters/line_diff"
	prfiles "github.com/nathantilsley/chart-val/internal/diff/adapters/pr_files"
	sourcectrl "github.com/nathantilsley/chart-val/internal/diff/adapters/source_ctrl"
	"github.com/nathantilsley/chart-val/internal/diff/app"
	"github.com/nathantilsley/chart-val/internal/diff/ports"
	"github.com/nathantilsley/chart-val/internal/platform/config"
	ghclient "github.com/nathantilsley/chart-val/internal/platform/github"
)

// Container holds all application dependencies.
type Container struct {
	Config         config.Config
	Logger         *slog.Logger
	GitHubClient   *gogithub.Client
	DiffService    ports.DiffUseCase
	WebhookHandler *githubin.WebhookHandler
}

// NewContainer builds and wires all dependencies.
func NewContainer(cfg config.Config, log *slog.Logger) (*Container, error) {
	// Platform dependencies
	githubClient, err := ghclient.NewClient(cfg.GitHubAppID, cfg.GitHubInstallationID, cfg.GitHubPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("creating github client: %w", err)
	}

	// Adapters
	envDiscovery := envdiscovery.New()
	sourceCtrl := sourcectrl.New(githubClient)
	helmRenderer, err := helmcli.New()
	if err != nil {
		return nil, fmt.Errorf("creating helm adapter: %w", err)
	}
	reporter := githubout.New(githubClient)
	fileChanges := prfiles.New(githubClient)
	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	// Domain service
	diffService := app.NewDiffService(
		sourceCtrl,
		envDiscovery,
		helmRenderer,
		reporter,
		fileChanges,
		semanticDiff,
		unifiedDiff,
		log,
	)

	// Webhook handler
	webhookHandler := githubin.NewWebhookHandler(diffService, cfg.WebhookSecret, log)

	return &Container{
		Config:         cfg,
		Logger:         log,
		GitHubClient:   githubClient,
		DiffService:    diffService,
		WebhookHandler: webhookHandler,
	}, nil
}
