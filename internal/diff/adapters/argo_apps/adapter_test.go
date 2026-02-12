package argoapps

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

func TestParseArgoApp(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	adapter := &Adapter{logger: logger}

	tests := []struct {
		name        string
		fixture     string // Path relative to testdata/
		wantApp     *AppData
		wantErr     bool
		wantNil     bool
		errContains string
	}{
		{
			name:    "valid application with path",
			fixture: "applications/valid-app-path.yaml",
			wantApp: &AppData{
				ChartPath:  "charts/my-app",
				ValueFiles: []string{"values-prod.yaml"},
				RepoURL:    "https://github.com/example/charts",
			},
			wantErr: false,
		},
		{
			name:    "valid application with OCI chart",
			fixture: "applications/valid-app-oci.yaml",
			wantApp: &AppData{
				ChartPath:  "my-app",
				ValueFiles: []string{"values.yaml"},
				RepoURL:    "oci://registry.example.com",
			},
			wantErr: false,
		},
		{
			name:        "non-application manifest",
			fixture:     "non-applications/configmap.yaml",
			wantNil:     true,
			wantErr:     true,
			errContains: "not an Application",
		},
		{
			name:        "deployment manifest",
			fixture:     "non-applications/deployment.yaml",
			wantNil:     true,
			wantErr:     true,
			errContains: "not an Application",
		},
		{
			name:        "invalid yaml",
			fixture:     "invalid/invalid.yaml",
			wantErr:     true,
			errContains: "yaml",
		},
		{
			name:        "missing repoURL",
			fixture:     "invalid/missing-repo-url.yaml",
			wantErr:     true,
			errContains: "repoURL",
		},
		{
			name:        "missing both chart and path",
			fixture:     "invalid/missing-chart-and-path.yaml",
			wantErr:     true,
			errContains: "chart and",
		},
		{
			name:    "application without valueFiles",
			fixture: "applications/app-no-valuefiles.yaml",
			wantApp: &AppData{
				ChartPath:  "charts/simple-app",
				ValueFiles: nil,
				RepoURL:    "https://github.com/example/charts",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixturePath := filepath.Join("testdata", tt.fixture)
			app, err := adapter.parseArgoApp(fixturePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if tt.wantNil {
				if app != nil {
					t.Errorf("expected nil app, got %+v", app)
				}
				return
			}

			if app == nil {
				t.Errorf("expected app, got nil")
				return
			}

			if app.ChartPath != tt.wantApp.ChartPath {
				t.Errorf("ChartPath = %q, want %q", app.ChartPath, tt.wantApp.ChartPath)
			}
			if app.RepoURL != tt.wantApp.RepoURL {
				t.Errorf("RepoURL = %q, want %q", app.RepoURL, tt.wantApp.RepoURL)
			}
			if !equalStringSlices(app.ValueFiles, tt.wantApp.ValueFiles) {
				t.Errorf("ValueFiles = %v, want %v", app.ValueFiles, tt.wantApp.ValueFiles)
			}
		})
	}
}

func TestExtractFromFolderStructure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		pattern     string
		filePath    string
		wantChart   string
		wantEnv     string
		wantErr     bool
		errContains string
	}{
		{
			name:      "simple pattern matching",
			pattern:   "{chartName}/{envName}",
			filePath:  filepath.Join(tmpDir, "my-app", "prod", "app.yaml"),
			wantChart: "my-app",
			wantEnv:   "prod",
			wantErr:   false,
		},
		{
			name:      "nested path with pattern at end",
			pattern:   "{chartName}/{envName}",
			filePath:  filepath.Join(tmpDir, "nested", "path", "my-app", "staging", "app.yaml"),
			wantChart: "my-app",
			wantEnv:   "staging",
			wantErr:   false,
		},
		{
			name:        "path too short for pattern",
			pattern:     "{chartName}/{envName}",
			filePath:    filepath.Join(tmpDir, "my-app", "app.yaml"),
			wantErr:     true,
			errContains: "fewer components",
		},
		{
			name:        "cannot extract chart name",
			pattern:     "{envName}",
			filePath:    filepath.Join(tmpDir, "prod", "app.yaml"),
			wantErr:     true,
			errContains: "chartName",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			adapter := &Adapter{
				localPath:     tmpDir,
				folderPattern: tt.pattern,
				logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
			}

			chartName, env, err := adapter.extractFromFolderStructure(tt.filePath)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				} else if tt.errContains != "" && !contains(err.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if chartName != tt.wantChart {
				t.Errorf("chartName = %q, want %q", chartName, tt.wantChart)
			}
			if env != tt.wantEnv {
				t.Errorf("env = %q, want %q", env, tt.wantEnv)
			}
		})
	}
}

func TestRebuildIndex_ContinuesOnErrors(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Copy testdata files and add an inaccessible one
	testdataPath := filepath.Join("testdata", "repos", "multi-env")
	if err := copyDir(testdataPath, tmpDir); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	// Create an inaccessible file
	brokenPath := filepath.Join(tmpDir, "broken", "invalid.yaml")
	if err := os.MkdirAll(filepath.Dir(brokenPath), 0o755); err != nil {
		t.Fatalf("failed to create broken dir: %v", err)
	}
	if err := os.WriteFile(brokenPath, []byte("content"), 0o000); err != nil {
		t.Fatalf("failed to create inaccessible file: %v", err)
	}
	defer func() {
		_ = os.Chmod(brokenPath, 0o600)
	}()

	adapter := &Adapter{
		localPath:     tmpDir,
		folderPattern: "{chartName}/{envName}",
		index:         make(map[string][]AppData),
		logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	// Should successfully build index despite the inaccessible file
	if err := adapter.rebuildIndex(); err != nil {
		t.Fatalf("rebuildIndex should not fail on individual file errors: %v", err)
	}

	// Should have indexed the 2 valid apps, skipping the broken one
	if len(adapter.index) != 2 {
		t.Errorf("expected 2 charts in index, got %d", len(adapter.index))
	}

	if _, exists := adapter.index["my-app"]; !exists {
		t.Errorf("my-app should be indexed")
	}
	if _, exists := adapter.index["other-app"]; !exists {
		t.Errorf("other-app should be indexed")
	}
}

func TestRebuildIndex(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Copy entire multi-env testdata repo structure
	testdataPath := filepath.Join("testdata", "repos", "multi-env")
	if err := copyDir(testdataPath, tmpDir); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	// Create .git directory to test skipping
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("failed to create .git dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), []byte("[core]\n"), 0o600); err != nil {
		t.Fatalf("failed to create .git/config: %v", err)
	}

	adapter := &Adapter{
		localPath:     tmpDir,
		folderPattern: "{chartName}/{envName}",
		index:         make(map[string][]AppData),
		logger:        slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	if err := adapter.rebuildIndex(); err != nil {
		t.Fatalf("rebuildIndex failed: %v", err)
	}

	// Verify index contains expected data
	if len(adapter.index) != 2 {
		t.Errorf("expected 2 charts in index, got %d", len(adapter.index))
	}

	// Check my-app has 2 environments
	myAppEnvs, exists := adapter.index["my-app"]
	if !exists {
		t.Errorf("my-app not found in index")
	} else {
		if len(myAppEnvs) != 2 {
			t.Errorf("expected 2 environments for my-app, got %d", len(myAppEnvs))
		}

		// Check environments
		envMap := make(map[string]AppData)
		for _, app := range myAppEnvs {
			envMap[app.Environment] = app
		}

		if _, hasProd := envMap["prod"]; !hasProd {
			t.Errorf("my-app missing prod environment")
		}
		if _, hasDev := envMap["dev"]; !hasDev {
			t.Errorf("my-app missing dev environment")
		}
	}

	// Check other-app has 1 environment
	otherAppEnvs, exists := adapter.index["other-app"]
	if !exists {
		t.Errorf("other-app not found in index")
	} else {
		if len(otherAppEnvs) != 1 {
			t.Errorf("expected 1 environment for other-app, got %d", len(otherAppEnvs))
		}
		if otherAppEnvs[0].Environment != "staging" {
			t.Errorf("expected staging environment, got %s", otherAppEnvs[0].Environment)
		}
	}
}

func TestGetChartConfig(t *testing.T) {
	t.Parallel()

	adapter := &Adapter{
		index: map[string][]AppData{
			"my-app": {
				{
					ChartName:   "my-app",
					ChartPath:   "charts/my-app",
					Environment: "prod",
					ValueFiles:  []string{"values-prod.yaml"},
					RepoURL:     "https://github.com/example/charts",
				},
				{
					ChartName:   "my-app",
					ChartPath:   "charts/my-app",
					Environment: "dev",
					ValueFiles:  []string{"values-dev.yaml"},
					RepoURL:     "https://github.com/example/charts",
				},
			},
		},
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	tests := []struct {
		name        string
		chartName   string
		wantPath    string
		wantEnvs    int
		wantDefault bool
	}{
		{
			name:      "chart found in index",
			chartName: "my-app",
			wantPath:  "charts/my-app",
			wantEnvs:  2,
		},
		{
			name:        "chart not found - returns default",
			chartName:   "unknown-app",
			wantPath:    "charts/unknown-app",
			wantEnvs:    0,
			wantDefault: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config, err := adapter.GetChartConfig(context.Background(), domain.PRContext{}, tt.chartName)
			if err != nil {
				t.Fatalf("GetChartConfig failed: %v", err)
			}

			if config.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", config.Path, tt.wantPath)
			}

			if len(config.Environments) != tt.wantEnvs {
				t.Errorf("got %d environments, want %d", len(config.Environments), tt.wantEnvs)
			}

			if !tt.wantDefault && len(config.Environments) > 0 {
				// Verify we got the expected environments
				envMap := make(map[string]domain.EnvironmentConfig)
				for _, env := range config.Environments {
					envMap[env.Name] = env
				}

				if _, hasProd := envMap["prod"]; !hasProd {
					t.Errorf("missing prod environment")
				}
				if _, hasDev := envMap["dev"]; !hasDev {
					t.Errorf("missing dev environment")
				}
			}
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	// This test requires git to be available
	tmpDir := t.TempDir()
	repoDir := filepath.Join(tmpDir, "test-repo")

	// Copy testdata to create a git repo
	testdataPath := filepath.Join("testdata", "repos", "single-env")
	if err := copyDir(testdataPath, repoDir); err != nil {
		t.Fatalf("failed to copy testdata: %v", err)
	}

	// Initialize git repo
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit"},
	}

	for _, cmd := range cmds {
		execCmd := execCommand(cmd[0], cmd[1:]...)
		execCmd.Dir = repoDir
		if output, err := execCmd.CombinedOutput(); err != nil {
			t.Fatalf("git command %v failed: %v\noutput: %s", cmd, err, output)
		}
	}

	// Test New with local "clone"
	cloneDir := filepath.Join(tmpDir, "clone")
	adapter, err := New(
		repoDir, // Use file:// URL for local git repo
		cloneDir,
		1*time.Hour,
		"{chartName}/{envName}",
		slog.New(slog.NewTextHandler(os.Stderr, nil)),
	)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer adapter.Stop()

	// Verify adapter was initialized correctly
	if len(adapter.index) != 1 {
		t.Errorf("expected 1 chart in index, got %d", len(adapter.index))
	}

	if apps, exists := adapter.index["my-app"]; !exists {
		t.Errorf("my-app not found in index")
	} else if len(apps) != 1 {
		t.Errorf("expected 1 environment for my-app, got %d", len(apps))
	}
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get the relative path from src
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		// Copy file
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(targetPath, data, 0o600)
	})
}

var execCommand = exec.Command // For potential test mocking
