package main

import (
	"fmt"
	"os"

	"github.com/nathantilsley/chart-val/internal/platform/config"
	"github.com/nathantilsley/chart-val/internal/platform/logger"
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

	// Build dependency container
	container, err := NewContainer(cfg, log)
	if err != nil {
		return fmt.Errorf("building container: %w", err)
	}

	// Create and run server
	server := NewServer(container)
	return server.Run()
}
