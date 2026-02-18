// Package argo discovers environment configuration by reading Argo CD Application manifests.
package argo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
	"github.com/nathantilsley/chart-val/internal/platform/gitrepo"
)

// ErrNotAnApplication is returned when a manifest is not an Argo Application.
var ErrNotAnApplication = errors.New("not an Application")

// Adapter implements ports.EnvironmentConfigPort by reading Argo CD Application
// manifests from a locally cloned Git repository. It scans the entire repo
// for Application files and extracts environment names from directory paths.
type Adapter struct {
	repoPath      string // Local filesystem path of the cloned repo
	folderPattern string // Folder structure pattern (e.g., "{chartName}/{envName}")
	chartDir      string // Top-level chart directory (e.g., "charts")

	mu     sync.RWMutex         // Protects index during updates
	index  map[string][]AppData // Cache: chartName -> list of apps
	logger *slog.Logger
}

// AppData represents the minimal data we need from an Argo Application.
type AppData struct {
	ChartName   string   // Extracted from spec.source.path (e.g., "my-app")
	ChartPath   string   // Full path from spec.source.path (e.g., "charts/my-app")
	Environment string   // Extracted from file path (e.g., "dev", "staging", "prod")
	ValueFiles  []string // From spec.source.helm.valueFiles
	RepoURL     string   // From spec.source.repoURL
}

// New creates a new Argo apps adapter. It registers an OnSync callback with
// the provided GitRepo so the index is rebuilt after every successful pull.
func New(
	repo *gitrepo.GitRepo,
	folderPattern string,
	logger *slog.Logger,
	chartDir string,
) *Adapter {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	a := &Adapter{
		repoPath:      repo.Path(),
		folderPattern: folderPattern,
		chartDir:      chartDir,
		index:         make(map[string][]AppData),
		logger:        logger,
	}

	repo.OnSync(func() {
		if err := a.rebuildIndex(); err != nil {
			a.logger.Error("failed to rebuild argo index", "error", err)
		}
	})

	return a
}

// rebuildIndex scans the entire repo for Application manifests and builds an index.
func (a *Adapter) rebuildIndex() error {
	index := make(map[string][]AppData)
	appCount := 0

	// Walk the entire repository looking for YAML files
	err := filepath.Walk(a.repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log errors accessing individual files but continue scanning
			a.logger.Warn("error accessing path, skipping", "path", path, "error", err)
			return nil
		}

		if shouldSkipPath(info) {
			return handleSkip(info)
		}

		if !isYAMLFile(path) {
			return nil
		}

		// Process the YAML file as a potential Argo Application
		app, shouldIndex := a.processApplicationFile(path)
		if shouldIndex {
			index[app.ChartName] = append(index[app.ChartName], *app)
			appCount++
		}

		return nil
	})

	if err != nil {
		return err
	}

	a.mu.Lock()
	a.index = index
	a.mu.Unlock()

	a.logger.Info("index rebuilt", "totalApps", appCount, "uniqueCharts", len(index))

	return nil
}

// shouldSkipPath determines if a path should be skipped during scanning.
func shouldSkipPath(info os.FileInfo) bool {
	return (info.IsDir() && info.Name() == ".git") || info.IsDir()
}

// handleSkip returns the appropriate skip action for a path.
func handleSkip(info os.FileInfo) error {
	if info.IsDir() && info.Name() == ".git" {
		return filepath.SkipDir
	}
	return nil
}

// isYAMLFile checks if a file has a YAML extension.
func isYAMLFile(path string) bool {
	ext := filepath.Ext(path)
	return ext == ".yaml" || ext == ".yml"
}

// processApplicationFile attempts to parse and index an Argo Application manifest.
// Returns the parsed app and whether it should be indexed.
func (a *Adapter) processApplicationFile(path string) (*AppData, bool) {
	// Try to parse as Argo Application
	app, err := a.parseArgoApp(path)
	if errors.Is(err, ErrNotAnApplication) {
		// Not an Application manifest - skip silently
		return nil, false
	}
	if err != nil {
		// Invalid YAML or other error - log and skip
		a.logger.Warn("failed to parse file as argo application", "path", path, "error", err)
		return nil, false
	}

	// Extract chart name and environment from folder structure
	chartName, env, err := a.extractFromFolderStructure(path)
	if err != nil {
		a.logger.Warn("failed to extract chart/env from path", "path", path, "error", err)
		return nil, false
	}

	app.ChartName = chartName
	app.Environment = env

	return app, true
}

// parseArgoApp parses an Argo CD Application manifest from a file.
// Returns minimal data needed for chart validation.
// Supports both OCI charts (spec.source.chart) and Git-based charts (spec.source.path).
func (a *Adapter) parseArgoApp(path string) (*AppData, error) {
	//nolint:gosec // G304: path is from filepath.Walk, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var manifest struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
		Spec       struct {
			Source struct {
				RepoURL string `yaml:"repoURL"`
				Path    string `yaml:"path"`  // For Git-based charts
				Chart   string `yaml:"chart"` // For OCI charts
				Helm    struct {
					ValueFiles []string `yaml:"valueFiles"`
				} `yaml:"helm"`
			} `yaml:"source"`
		} `yaml:"spec"`
	}

	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	// Only process Argo Application resources
	if manifest.Kind != "Application" {
		return nil, ErrNotAnApplication
	}

	if manifest.Spec.Source.RepoURL == "" {
		return nil, errors.New("missing required field: repoURL")
	}

	// Determine chart name/path: prefer OCI chart, fall back to path
	chartIdentifier := manifest.Spec.Source.Chart
	if chartIdentifier == "" {
		chartIdentifier = manifest.Spec.Source.Path
	}

	if chartIdentifier == "" {
		return nil, errors.New("missing both spec.source.chart and spec.source.path")
	}

	return &AppData{
		ChartPath:  chartIdentifier, // Will be parsed as ChartName during indexing
		ValueFiles: manifest.Spec.Source.Helm.ValueFiles,
		RepoURL:    manifest.Spec.Source.RepoURL,
	}, nil
}

// GetEnvironmentConfig implements ports.EnvironmentConfigPort.
// It looks up environments for the given chart name from Argo Application manifests.
// If the chart is not found, returns empty environments (fallback will be used).
func (a *Adapter) GetEnvironmentConfig(
	_ context.Context,
	_ domain.PRContext,
	chartName string,
) (domain.ChartConfig, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	a.logger.Info("looking up chart in argo apps", "chartName", chartName)

	// Look up in index by chart name
	apps, exists := a.index[chartName]
	if !exists || len(apps) == 0 {
		a.logger.Info("chart not found in argo apps", "chartName", chartName)
		return domain.ChartConfig{
			Path:         a.chartDir + "/" + chartName,
			Environments: []domain.EnvironmentConfig{}, // Empty - will fall back to discovery
		}, nil
	}

	a.logger.Info("found argo apps for chart", "chartName", chartName, "count", len(apps))

	config := domain.ChartConfig{
		Path:         apps[0].ChartPath,
		Environments: []domain.EnvironmentConfig{},
	}

	for _, app := range apps {
		config.Environments = append(config.Environments, domain.EnvironmentConfig{
			Name:       app.Environment,
			ValueFiles: app.ValueFiles,
		})
	}

	a.logger.Info(
		"returning chart config",
		"chartName",
		chartName,
		"envCount",
		len(config.Environments),
	)

	return config, nil
}

// extractFromFolderStructure extracts chart name and environment from file path
// using the configured folder pattern.
// Example: pattern="{chartName}/{envName}", path="/tmp/repo/my-app/prod/app.yaml"
//
//	→ chartName="my-app", env="prod"
func (a *Adapter) extractFromFolderStructure(filePath string) (chartName, env string, err error) {
	// Get relative path from repo root
	relPath, err := filepath.Rel(a.repoPath, filePath)
	if err != nil {
		return "", "", fmt.Errorf("getting relative path: %w", err)
	}

	// Remove filename to get directory path
	dirPath := filepath.Dir(relPath)
	parts := strings.Split(dirPath, string(filepath.Separator))

	// Parse pattern to understand structure
	// Pattern: "{chartName}/{envName}" → expect 2 parts: [chartName, envName]
	patternParts := strings.Split(a.folderPattern, "/")

	if len(parts) < len(patternParts) {
		return "", "", errors.New("path has fewer components than pattern")
	}

	// Extract values based on pattern
	// Use the last N parts where N is the number of pattern parts
	offset := len(parts) - len(patternParts)
	relevantParts := parts[offset:]

	for i, patternPart := range patternParts {
		switch patternPart {
		case "{chartName}":
			chartName = relevantParts[i]
		case "{envName}":
			env = relevantParts[i]
		}
	}

	if chartName == "" {
		return "", "", errors.New("could not extract chartName from path")
	}
	if env == "" {
		return "", "", errors.New("could not extract envName from path")
	}

	return chartName, env, nil
}
