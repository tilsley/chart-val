package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/nathantilsley/chart-val/internal/platform/config"
	"github.com/nathantilsley/chart-val/internal/platform/logger"
	"github.com/nathantilsley/chart-val/internal/platform/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Initialize logger
	log := logger.New(cfg.LogLevel)

	// Initialize telemetry (noop when disabled)
	ctx := context.Background()
	tel, err := telemetry.New(ctx, cfg.OTelEnabled)
	if err != nil {
		return fmt.Errorf("initializing telemetry: %w", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tel.Shutdown(shutdownCtx)
	}()

	// Build dependency container
	container, err := NewContainer(cfg, log, tel)
	if err != nil {
		return fmt.Errorf("building container: %w", err)
	}

	// Create and run server
	server := NewServer(container)
	return server.Run()
}
