// Package main provides the chart-val webhook server for validating Helm chart changes.
package main

import (
	"fmt"
	"log/slog"

	gogithub "github.com/google/go-github/v68/github"

	dyffdiff "github.com/nathantilsley/chart-val/internal/diff/adapters/dyff_diff"
	argoenv "github.com/nathantilsley/chart-val/internal/diff/adapters/environment_config/argo"
	fsenv "github.com/nathantilsley/chart-val/internal/diff/adapters/environment_config/filesystem"
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
	"github.com/nathantilsley/chart-val/internal/platform/telemetry"
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
func NewContainer(cfg config.Config, log *slog.Logger, tel *telemetry.Telemetry) (*Container, error) {
	// Platform dependencies
	githubClient, err := ghclient.NewClient(cfg.GitHubAppID, cfg.GitHubInstallationID, cfg.GitHubPrivateKey)
	if err != nil {
		return nil, fmt.Errorf("creating github client: %w", err)
	}

	// Adapters
	sourceCtrl := sourcectrl.New(githubClient)
	helmRenderer, err := helmcli.New()
	if err != nil {
		return nil, fmt.Errorf("creating helm adapter: %w", err)
	}
	reporter := githubout.New(githubClient)
	changedCharts := prfiles.New(githubClient, log)
	semanticDiff := dyffdiff.New()
	unifiedDiff := linediff.New()

	// Environment config adapters (both discover where charts are deployed)
	// Filesystem adapter - discovers from chart's env/ folder
	filesystemEnvConfig := fsenv.New(sourceCtrl)

	// Optionally create Argo adapter (source of truth when available)
	var argoEnvConfig ports.EnvironmentConfigPort
	if cfg.ArgoAppsRepo != "" {
		log.Info("argo apps integration enabled",
			"repo", cfg.ArgoAppsRepo,
			"syncInterval", cfg.ArgoAppsSyncInterval,
			"folderPattern", cfg.ArgoAppsFolderPattern,
		)
		adapter, err := argoenv.New(
			cfg.ArgoAppsRepo,
			cfg.ArgoAppsLocalPath,
			cfg.ArgoAppsSyncInterval,
			cfg.ArgoAppsFolderPattern,
			log,
		)
		if err != nil {
			return nil, fmt.Errorf("creating argo environment config adapter: %w", err)
		}
		argoEnvConfig = adapter
	} else {
		log.Info("argo apps not configured, using filesystem discovery only")
	}

	// Domain service (handles composite strategy: Argo → Filesystem → Base chart)
	diffService := app.NewDiffService(
		sourceCtrl,
		changedCharts,
		argoEnvConfig,       // nil if not configured
		filesystemEnvConfig, // always present - discovers from chart's env/ folder
		helmRenderer,
		reporter,
		semanticDiff,
		unifiedDiff,
		log,
		tel.Meter,
		tel.Tracer,
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
