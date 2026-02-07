package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	Port              int
	WebhookSecret     string
	GitHubAppID       int64
	GitHubPrivateKey  string // PEM file contents
	LogLevel          string
}

// Load reads configuration from environment variables, validates required
// fields, and applies defaults for Port (8080) and LogLevel ("info").
func Load() (Config, error) {
	cfg := Config{
		Port:     8080,
		LogLevel: "info",
	}

	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, fmt.Errorf("invalid PORT %q: %w", v, err)
		}
		cfg.Port = p
	}

	cfg.WebhookSecret = os.Getenv("WEBHOOK_SECRET")
	if cfg.WebhookSecret == "" {
		return Config{}, fmt.Errorf("WEBHOOK_SECRET is required")
	}

	if v := os.Getenv("GITHUB_APP_ID"); v == "" {
		return Config{}, fmt.Errorf("GITHUB_APP_ID is required")
	} else {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return Config{}, fmt.Errorf("invalid GITHUB_APP_ID %q: %w", v, err)
		}
		cfg.GitHubAppID = id
	}

	cfg.GitHubPrivateKey = os.Getenv("GITHUB_PRIVATE_KEY")
	if cfg.GitHubPrivateKey == "" {
		return Config{}, fmt.Errorf("GITHUB_PRIVATE_KEY is required")
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	return cfg, nil
}
