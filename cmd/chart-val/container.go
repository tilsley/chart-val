// Package main provides the chart-val webhook server for validating Helm chart changes.
package main

import (
	"fmt"
	"log/slog"

	gogithub "github.com/google/go-github/v68/github"

	argoapps "github.com/nathantilsley/chart-val/internal/diff/adapters/argo_apps"
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
	changedCharts := prfiles.New(githubClient, log)
	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	// Optionally create Argo adapter (source of truth when available)
	var argoAdapter ports.ChartConfigPort
	if cfg.ArgoAppsRepo != "" {
		log.Info("argo apps integration enabled",
			"repo", cfg.ArgoAppsRepo,
			"syncInterval", cfg.ArgoAppsSyncInterval,
			"folderPattern", cfg.ArgoAppsFolderPattern,
		)
		adapter, err := argoapps.New(
			cfg.ArgoAppsRepo,
			cfg.ArgoAppsLocalPath,
			cfg.ArgoAppsSyncInterval,
			cfg.ArgoAppsFolderPattern,
			log,
		)
		if err != nil {
			return nil, fmt.Errorf("creating argo apps adapter: %w", err)
		}
		argoAdapter = adapter
	} else {
		log.Info("argo apps not configured, using env discovery only")
	}

	// Domain service (handles composite strategy: Argo → Env Discovery → Base chart)
	diffService := app.NewDiffService(
		sourceCtrl,
		changedCharts,
		argoAdapter,  // nil if not configured
		envDiscovery, // always present - discovers from chart's env/ directory
		helmRenderer,
		reporter,
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
