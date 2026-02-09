package envdiscovery

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

func TestAdapter_DiscoverEnvironments(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) string // Returns chartDir path
		want     []domain.EnvironmentConfig
		wantErr  bool
	}{
		{
			name: "no env directory returns default",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				chartDir := filepath.Join(tmpDir, "my-chart")
				if err := os.MkdirAll(chartDir, 0o755); err != nil {
					t.Fatal(err)
				}
				// No env/ directory
				return chartDir
			},
			want: []domain.EnvironmentConfig{
				{Name: "default", ValueFiles: nil},
			},
		},
		{
			name: "empty env directory returns default",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				chartDir := filepath.Join(tmpDir, "my-chart")
				envDir := filepath.Join(chartDir, "env")
				if err := os.MkdirAll(envDir, 0o755); err != nil {
					t.Fatal(err)
				}
				// Empty env/ directory
				return chartDir
			},
			want: []domain.EnvironmentConfig{
				{Name: "default", ValueFiles: nil},
			},
		},
		{
			name: "single environment file",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				chartDir := filepath.Join(tmpDir, "my-chart")
				envDir := filepath.Join(chartDir, "env")
				if err := os.MkdirAll(envDir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(envDir, "prod-values.yaml"), []byte("replicas: 3"), 0o644); err != nil {
					t.Fatal(err)
				}
				return chartDir
			},
			want: []domain.EnvironmentConfig{
				{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
			},
		},
		{
			name: "multiple environments sorted alphabetically",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				chartDir := filepath.Join(tmpDir, "my-chart")
				envDir := filepath.Join(chartDir, "env")
				if err := os.MkdirAll(envDir, 0o755); err != nil {
					t.Fatal(err)
				}
				files := []string{"staging-values.yaml", "prod-values.yaml", "dev-values.yaml"}
				for _, f := range files {
					if err := os.WriteFile(filepath.Join(envDir, f), []byte("test"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return chartDir
			},
			want: []domain.EnvironmentConfig{
				{Name: "dev", ValueFiles: []string{"env/dev-values.yaml"}},
				{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
				{Name: "staging", ValueFiles: []string{"env/staging-values.yaml"}},
			},
		},
		{
			name: "ignores non-matching files",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				chartDir := filepath.Join(tmpDir, "my-chart")
				envDir := filepath.Join(chartDir, "env")
				if err := os.MkdirAll(envDir, 0o755); err != nil {
					t.Fatal(err)
				}
				// Create matching and non-matching files
				if err := os.WriteFile(filepath.Join(envDir, "prod-values.yaml"), []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(envDir, "README.md"), []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(envDir, ".gitignore"), []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(envDir, "values.yaml"), []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
				return chartDir
			},
			want: []domain.EnvironmentConfig{
				{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
			},
		},
		{
			name: "ignores subdirectories",
			setup: func(t *testing.T) string {
				tmpDir := t.TempDir()
				chartDir := filepath.Join(tmpDir, "my-chart")
				envDir := filepath.Join(chartDir, "env")
				if err := os.MkdirAll(filepath.Join(envDir, "subdir"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(envDir, "prod-values.yaml"), []byte("test"), 0o644); err != nil {
					t.Fatal(err)
				}
				return chartDir
			},
			want: []domain.EnvironmentConfig{
				{Name: "prod", ValueFiles: []string{"env/prod-values.yaml"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chartDir := tt.setup(t)
			adapter := New()

			got, err := adapter.DiscoverEnvironments(context.Background(), chartDir)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverEnvironments() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if len(got) != len(tt.want) {
				t.Fatalf("DiscoverEnvironments() got %d environments, want %d\nGot: %+v\nWant: %+v", len(got), len(tt.want), got, tt.want)
			}

			for i := range got {
				if got[i].Name != tt.want[i].Name {
					t.Errorf("Environment[%d].Name = %v, want %v", i, got[i].Name, tt.want[i].Name)
				}
				if len(got[i].ValueFiles) != len(tt.want[i].ValueFiles) {
					t.Errorf("Environment[%d].ValueFiles length = %v, want %v", i, len(got[i].ValueFiles), len(tt.want[i].ValueFiles))
					continue
				}
				for j := range got[i].ValueFiles {
					if got[i].ValueFiles[j] != tt.want[i].ValueFiles[j] {
						t.Errorf("Environment[%d].ValueFiles[%d] = %v, want %v", i, j, got[i].ValueFiles[j], tt.want[i].ValueFiles[j])
					}
				}
			}
		})
	}
}
