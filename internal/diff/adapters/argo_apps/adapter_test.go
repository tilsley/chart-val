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
		content     string
		wantApp     *AppData
		wantErr     bool
		wantNil     bool
		errContains string
	}{
		{
			name: "valid application with path",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-prod.yaml
`,
			wantApp: &AppData{
				ChartPath:  "charts/my-app",
				ValueFiles: []string{"values-prod.yaml"},
				RepoURL:    "https://github.com/example/charts",
			},
			wantErr: false,
		},
		{
			name: "valid application with OCI chart",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: oci://registry.example.com
    chart: my-app
    helm:
      valueFiles:
        - values.yaml
`,
			wantApp: &AppData{
				ChartPath:  "my-app",
				ValueFiles: []string{"values.yaml"},
				RepoURL:    "oci://registry.example.com",
			},
			wantErr: false,
		},
		{
			name: "non-application manifest",
			content: `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
data:
  key: value
`,
			wantNil:     true,
			wantErr:     true,
			errContains: "not an Application",
		},
		{
			name: "deployment manifest",
			content: `apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-deployment
spec:
  replicas: 3
`,
			wantNil:     true,
			wantErr:     true,
			errContains: "not an Application",
		},
		{
			name: "invalid yaml",
			content: `this is not: valid: yaml:
  - broken
    - indentation
`,
			wantErr:     true,
			errContains: "yaml",
		},
		{
			name: "missing repoURL",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    path: charts/my-app
`,
			wantErr:     true,
			errContains: "repoURL",
		},
		{
			name: "missing both chart and path",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
`,
			wantErr:     true,
			errContains: "chart and",
		},
		{
			name: "application without valueFiles",
			content: `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/simple-app
`,
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

			// Write test content to temp file
			tmpFile := filepath.Join(t.TempDir(), "test.yaml")
			if err := os.WriteFile(tmpFile, []byte(tt.content), 0o600); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			app, err := adapter.parseArgoApp(tmpFile)

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

	// Create test directory with some valid apps and an inaccessible file
	testFiles := map[string]string{
		"my-app/prod/app.yaml": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/my-app
`,
		"broken/invalid.yaml": "this will be made inaccessible",
		"other-app/dev/app.yaml": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/other-app
`,
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write test file %s: %v", path, err)
		}
	}

	// Make the broken file inaccessible by removing all permissions
	brokenPath := filepath.Join(tmpDir, "broken/invalid.yaml")
	if err := os.Chmod(brokenPath, 0o000); err != nil {
		t.Fatalf("failed to chmod test file: %v", err)
	}
	// Restore permissions for cleanup
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

	// Create test directory structure
	// my-app/prod/app.yaml - valid Application
	// my-app/dev/app.yaml - valid Application
	// other-app/staging/app.yaml - valid Application
	// config/configmap.yaml - ConfigMap (not an Application)
	// random.txt - non-YAML file
	// .git/config - should be skipped

	testFiles := map[string]string{
		"my-app/prod/app.yaml": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-prod.yaml
`,
		"my-app/dev/app.yaml": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/my-app
    helm:
      valueFiles:
        - values-dev.yaml
`,
		"other-app/staging/app.yaml": `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/other-app
`,
		"config/configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: my-config
`,
		"random.txt":  "not yaml content",
		".git/config": "[core]\nrepositoryformatversion = 0",
	}

	for path, content := range testFiles {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write test file %s: %v", path, err)
		}
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

	// Create a minimal git repo with an Application manifest
	if err := os.MkdirAll(filepath.Join(repoDir, "my-app", "prod"), 0o755); err != nil {
		t.Fatalf("failed to create test directories: %v", err)
	}

	appManifest := `apiVersion: argoproj.io/v1alpha1
kind: Application
spec:
  source:
    repoURL: https://github.com/example/charts
    path: charts/my-app
`
	appPath := filepath.Join(repoDir, "my-app", "prod", "app.yaml")
	if err := os.WriteFile(appPath, []byte(appManifest), 0o600); err != nil {
		t.Fatalf("failed to write test app: %v", err)
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
		exec := execCommand(cmd[0], cmd[1:]...)
		exec.Dir = repoDir
		if output, err := exec.CombinedOutput(); err != nil {
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

var execCommand = exec.Command // For potential test mocking
