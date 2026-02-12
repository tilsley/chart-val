// Package argoapps provides chart configuration by reading Argo CD Application manifests.
package argoapps

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

// ErrNotAnApplication is returned when a manifest is not an Argo Application.
var ErrNotAnApplication = errors.New("not an Application")

// Adapter implements ports.ChartConfigPort by reading Argo CD Application
// manifests from a locally cloned Git repository. It scans the entire repo
// for Application files and extracts environment names from directory paths.
type Adapter struct {
	repoURL       string        // Git repository URL
	localPath     string        // Local filesystem path for clone
	syncInterval  time.Duration // How often to sync the repo
	folderPattern string        // Folder structure pattern (e.g., "{chartName}/{envName}")

	mu     sync.RWMutex         // Protects index during updates
	stopCh chan struct{}        // Signal to stop background sync
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

// New creates a new Argo apps adapter. It performs an initial clone/sync
// and starts a background goroutine to keep the repository updated.
func New(
	repoURL, localPath string,
	syncInterval time.Duration,
	folderPattern string,
	logger *slog.Logger,
) (*Adapter, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	a := &Adapter{
		repoURL:       repoURL,
		localPath:     localPath,
		syncInterval:  syncInterval,
		folderPattern: folderPattern,
		stopCh:        make(chan struct{}),
		index:         make(map[string][]AppData),
		logger:        logger,
	}

	// Initial clone/sync
	a.logger.Info("initializing argo apps repository", "repoURL", repoURL, "localPath", localPath)
	if err := a.initRepo(); err != nil {
		return nil, fmt.Errorf("initializing repo: %w", err)
	}

	// Build initial index
	a.logger.Info("scanning repo for argo applications")
	if err := a.rebuildIndex(); err != nil {
		return nil, fmt.Errorf("building initial index: %w", err)
	}

	// Start background syncer
	go a.syncLoop()
	a.logger.Info("argo apps adapter started", "syncInterval", syncInterval)

	return a, nil
}

// initRepo clones the repository if it doesn't exist, or pulls latest if it does.
func (a *Adapter) initRepo() error {
	gitDir := filepath.Join(a.localPath, ".git")

	// Check if repo already exists
	if _, err := os.Stat(gitDir); err == nil {
		a.logger.Info("repository already exists, pulling latest")
		return a.pullRepo()
	}

	// Clone fresh
	a.logger.Info("cloning repository", "repoURL", a.repoURL)
	//nolint:gosec // G204: repoURL is from trusted config, not user input
	cmd := exec.CommandContext(context.Background(), "git", "clone", "--depth=1", a.repoURL, a.localPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\noutput: %s", err, output)
	}

	return nil
}

// pullRepo pulls the latest changes from the remote repository.
func (a *Adapter) pullRepo() error {
	//nolint:gosec // G204: localPath is from trusted config, not user input
	cmd := exec.CommandContext(context.Background(), "git", "-C", a.localPath, "pull", "--ff-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w\noutput: %s", err, output)
	}
	return nil
}

// syncLoop runs in the background and periodically syncs the repository.
func (a *Adapter) syncLoop() {
	ticker := time.NewTicker(a.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			a.sync()
		case <-a.stopCh:
			a.logger.Info("stopping argo apps sync loop")
			return
		}
	}
}

// sync performs a git pull and rebuilds the index.
func (a *Adapter) sync() {
	a.logger.Info("syncing argo apps repository")

	a.mu.Lock()
	defer a.mu.Unlock()

	// Pull latest changes
	if err := a.pullRepo(); err != nil {
		a.logger.Error("failed to pull repository", "error", err)
		return
	}

	// Rebuild index
	if err := a.rebuildIndex(); err != nil {
		a.logger.Error("failed to rebuild index", "error", err)
		return
	}

	a.logger.Info("argo apps repository synced successfully", "uniqueCharts", len(a.index))
}

// rebuildIndex scans the entire repo for Application manifests and builds an index.
func (a *Adapter) rebuildIndex() error {
	index := make(map[string][]AppData)
	appCount := 0

	// Walk the entire repository looking for YAML files
	err := filepath.Walk(a.localPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
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

	a.index = index
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

// GetChartConfig implements ports.ChartConfigPort.
// It looks up environments for the given chart name from Argo Application manifests.
// If the chart is not found, returns default environments.
func (a *Adapter) GetChartConfig(_ context.Context, _ domain.PRContext, chartName string) (domain.ChartConfig, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	a.logger.Info("looking up chart in argo apps", "chartName", chartName)

	// Look up in index by chart name
	apps, exists := a.index[chartName]
	if !exists || len(apps) == 0 {
		a.logger.Info("chart not found in argo apps", "chartName", chartName)
		return domain.ChartConfig{
			Path:         "charts/" + chartName,
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

	a.logger.Info("returning chart config", "chartName", chartName, "envCount", len(config.Environments))

	return config, nil
}

// Stop signals the background sync loop to stop.
func (a *Adapter) Stop() {
	close(a.stopCh)
}

// extractFromFolderStructure extracts chart name and environment from file path
// using the configured folder pattern.
// Example: pattern="{chartName}/{envName}", path="/tmp/repo/my-app/prod/app.yaml"
//
//	→ chartName="my-app", env="prod"
func (a *Adapter) extractFromFolderStructure(filePath string) (chartName, env string, err error) {
	// Get relative path from repo root
	relPath, err := filepath.Rel(a.localPath, filePath)
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
