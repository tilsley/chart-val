// Package config provides application configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	Port                 int
	WebhookSecret        string
	GitHubAppID          int64
	GitHubInstallationID int64
	GitHubPrivateKey     string // PEM file contents
	LogLevel             string

	// Argo CD integration (optional)
	ArgoAppsRepo          string        // Git repo containing Argo apps (e.g., "https://github.com/org/gitops")
	ArgoAppsLocalPath     string        // Local path for clone (e.g., "/tmp/chart-val-argocd")
	ArgoAppsSyncInterval  time.Duration // How often to sync repo (e.g., 1h)
	ArgoAppsFolderPattern string        // Folder structure pattern (e.g., "apps/{chartName}/{envName}")

	// OpenTelemetry (optional)
	OTelEnabled bool // OTEL_ENABLED feature flag
}

// Load reads configuration from environment variables, validates required
// fields, and applies defaults for Port (8080) and LogLevel ("info").
func Load() (Config, error) {
	cfg := Config{
		Port:     8080,
		LogLevel: "info",
	}

	if err := loadCoreConfig(&cfg); err != nil {
		return Config{}, err
	}

	if err := loadArgoConfig(&cfg); err != nil {
		return Config{}, err
	}

	loadOTelConfig(&cfg)

	return cfg, nil
}

func loadCoreConfig(cfg *Config) error {
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid PORT %q: %w", v, err)
		}
		cfg.Port = p
	}

	cfg.WebhookSecret = os.Getenv("WEBHOOK_SECRET")
	if cfg.WebhookSecret == "" {
		return errors.New("WEBHOOK_SECRET is required")
	}

	var err error
	cfg.GitHubAppID, err = parseRequiredInt64("GITHUB_APP_ID")
	if err != nil {
		return err
	}

	cfg.GitHubInstallationID, err = parseRequiredInt64("GITHUB_INSTALLATION_ID")
	if err != nil {
		return err
	}

	cfg.GitHubPrivateKey = os.Getenv("GITHUB_PRIVATE_KEY")
	if cfg.GitHubPrivateKey == "" {
		return errors.New("GITHUB_PRIVATE_KEY is required")
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	return nil
}

func loadArgoConfig(cfg *Config) error {
	cfg.ArgoAppsRepo = os.Getenv("ARGO_APPS_REPO")
	if cfg.ArgoAppsRepo == "" {
		return nil // Argo integration is optional
	}

	cfg.ArgoAppsLocalPath = getEnvOrDefault("ARGO_APPS_LOCAL_PATH", "/tmp/chart-val-argocd")
	cfg.ArgoAppsFolderPattern = getEnvOrDefault("ARGO_APPS_FOLDER_PATTERN", "{chartName}/{envName}")

	dur, err := parseDurationOrDefault("ARGO_APPS_SYNC_INTERVAL", 1*time.Hour)
	if err != nil {
		return err
	}
	cfg.ArgoAppsSyncInterval = dur

	return nil
}

func parseRequiredInt64(envKey string) (int64, error) {
	v := os.Getenv(envKey)
	if v == "" {
		return 0, fmt.Errorf("%s is required", envKey)
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", envKey, v, err)
	}
	return id, nil
}

func getEnvOrDefault(envKey, defaultValue string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return defaultValue
}

func loadOTelConfig(cfg *Config) {
	cfg.OTelEnabled = os.Getenv("OTEL_ENABLED") == "true"
}

func parseDurationOrDefault(envKey string, defaultValue time.Duration) (time.Duration, error) {
	v := os.Getenv(envKey)
	if v == "" {
		return defaultValue, nil
	}
	dur, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", envKey, v, err)
	}
	return dur, nil
}
