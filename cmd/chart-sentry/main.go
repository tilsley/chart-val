package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/github_in"
	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/github_out"
	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/helm_cli"
	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/pr_files"
	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/repo_cfg"
	"github.com/nathantilsley/chart-sentry/internal/diff/adapters/source_ctrl"
	"github.com/nathantilsley/chart-sentry/internal/diff/app"
	"github.com/nathantilsley/chart-sentry/internal/platform/config"
	ghclient "github.com/nathantilsley/chart-sentry/internal/platform/github"
	"github.com/nathantilsley/chart-sentry/internal/platform/logger"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	log := logger.New(cfg.LogLevel)

	clientFactory, err := ghclient.NewClientFactory(cfg.GitHubAppID, cfg.GitHubPrivateKey)
	if err != nil {
		return fmt.Errorf("creating github client factory: %w", err)
	}

	// Init adapters
	repoCfg := repocfg.New(clientFactory)
	sourceCtrl := sourcectrl.New(clientFactory)
	helmRenderer, err := helmcli.New()
	if err != nil {
		return fmt.Errorf("creating helm adapter: %w", err)
	}
	reporter := githubout.New(clientFactory)
	fileChanges := prfiles.New(clientFactory)

	// Domain service
	diffService := app.NewDiffService(sourceCtrl, repoCfg, helmRenderer, reporter, fileChanges, log)

	// Webhook handler
	webhookHandler := githubin.NewWebhookHandler(diffService, cfg.WebhookSecret, log)

	// Routes
	mux := http.NewServeMux()
	mux.Handle("POST /webhook", webhookHandler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	errCh := make(chan error, 1)
	go func() {
		log.Info("starting server", slog.Int("port", cfg.Port))
		errCh <- srv.ListenAndServe()
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		log.Info("shutting down", "signal", sig.String())
	case err := <-errCh:
		return fmt.Errorf("server error: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	log.Info("server stopped")
	return nil
}
