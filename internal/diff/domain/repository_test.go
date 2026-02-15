package domain

import "testing"

func TestExtractChartNames(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected []string
	}{
		{
			name:     "no files",
			files:    []string{},
			expected: []string{},
		},
		{
			name: "no chart files",
			files: []string{
				"README.md",
				"src/main.go",
				"config/settings.yaml",
			},
			expected: []string{},
		},
		{
			name: "single chart",
			files: []string{
				"charts/my-app/Chart.yaml",
				"charts/my-app/values.yaml",
				"charts/my-app/templates/deployment.yaml",
			},
			expected: []string{"my-app"},
		},
		{
			name: "multiple charts",
			files: []string{
				"charts/app1/Chart.yaml",
				"charts/app2/values.yaml",
				"charts/app1/templates/service.yaml",
			},
			expected: []string{"app1", "app2"},
		},
		{
			name: "mixed files",
			files: []string{
				"README.md",
				"charts/my-app/Chart.yaml",
				"src/main.go",
				"charts/other-app/values.yaml",
				".github/workflows/ci.yml",
			},
			expected: []string{"my-app", "other-app"},
		},
		{
			name: "duplicate chart references",
			files: []string{
				"charts/my-app/Chart.yaml",
				"charts/my-app/values.yaml",
				"charts/my-app/templates/deployment.yaml",
				"charts/my-app/templates/service.yaml",
			},
			expected: []string{"my-app"},
		},
		{
			name: "invalid chart paths",
			files: []string{
				"charts/",         // no chart name
				"charts",          // not a path
				"not-charts/app/", // wrong prefix
				"charts/app/file.yaml",
			},
			expected: []string{"app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractChartNames(tt.files, "charts")

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d chart names, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			// Convert to maps for easier comparison (order doesn't matter)
			resultMap := make(map[string]bool)
			for _, name := range result {
				resultMap[name] = true
			}

			for _, expected := range tt.expected {
				if !resultMap[expected] {
					t.Errorf("expected chart name %q not found in result: %v", expected, result)
				}
			}
		})
	}
}
