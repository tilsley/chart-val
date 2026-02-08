package domain

import "testing"

func TestStatus_String(t *testing.T) {
	tests := []struct {
		status Status
		want   string
	}{
		{StatusSuccess, "Success"},
		{StatusChanges, "Changes"},
		{StatusError, "Error"},
		{Status(99), "Unknown"}, // Invalid status
		{Status(-1), "Unknown"}, // Negative status
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.status.String()
			if got != tt.want {
				t.Errorf("Status.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDiffResult_PreferredDiff(t *testing.T) {
	tests := []struct {
		name         string
		result       DiffResult
		wantContains string
	}{
		{
			name: "prefers semantic diff when available",
			result: DiffResult{
				SemanticDiff: "semantic diff content",
				UnifiedDiff:  "unified diff content",
			},
			wantContains: "semantic",
		},
		{
			name: "falls back to unified diff when semantic is empty",
			result: DiffResult{
				SemanticDiff: "",
				UnifiedDiff:  "unified diff content",
			},
			wantContains: "unified",
		},
		{
			name: "returns empty when both are empty",
			result: DiffResult{
				SemanticDiff: "",
				UnifiedDiff:  "",
			},
			wantContains: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.result.PreferredDiff()
			if tt.wantContains != "" && got != tt.result.SemanticDiff && got != tt.result.UnifiedDiff {
				t.Errorf("PreferredDiff() = %v, want one of semantic or unified", got)
			}
			if tt.wantContains == "" && got != "" {
				t.Errorf("PreferredDiff() = %v, want empty", got)
			}
		})
	}
}

func TestCountByStatus(t *testing.T) {
	tests := []struct {
		name         string
		results      []DiffResult
		wantSuccess  int
		wantChanges  int
		wantErrors   int
	}{
		{
			name:        "empty results",
			results:     []DiffResult{},
			wantSuccess: 0,
			wantChanges: 0,
			wantErrors:  0,
		},
		{
			name: "all success",
			results: []DiffResult{
				{Status: StatusSuccess},
				{Status: StatusSuccess},
			},
			wantSuccess: 2,
			wantChanges: 0,
			wantErrors:  0,
		},
		{
			name: "all changes",
			results: []DiffResult{
				{Status: StatusChanges},
				{Status: StatusChanges},
			},
			wantSuccess: 0,
			wantChanges: 2,
			wantErrors:  0,
		},
		{
			name: "all errors",
			results: []DiffResult{
				{Status: StatusError},
				{Status: StatusError},
			},
			wantSuccess: 0,
			wantChanges: 0,
			wantErrors:  2,
		},
		{
			name: "mixed statuses",
			results: []DiffResult{
				{Status: StatusSuccess},
				{Status: StatusChanges},
				{Status: StatusError},
				{Status: StatusSuccess},
				{Status: StatusChanges},
			},
			wantSuccess: 2,
			wantChanges: 2,
			wantErrors:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSuccess, gotChanges, gotErrors := CountByStatus(tt.results)
			if gotSuccess != tt.wantSuccess {
				t.Errorf("CountByStatus() success = %v, want %v", gotSuccess, tt.wantSuccess)
			}
			if gotChanges != tt.wantChanges {
				t.Errorf("CountByStatus() changes = %v, want %v", gotChanges, tt.wantChanges)
			}
			if gotErrors != tt.wantErrors {
				t.Errorf("CountByStatus() errors = %v, want %v", gotErrors, tt.wantErrors)
			}
		})
	}
}

func TestDiffLabel(t *testing.T) {
	tests := []struct {
		name      string
		chartName string
		envName   string
		ref       string
		want      string
	}{
		{
			name:      "standard formatting",
			chartName: "my-app",
			envName:   "prod",
			ref:       "main",
			want:      "my-app/prod (main)",
		},
		{
			name:      "feature branch",
			chartName: "api-service",
			envName:   "staging",
			ref:       "feat/new-endpoint",
			want:      "api-service/staging (feat/new-endpoint)",
		},
		{
			name:      "empty environment",
			chartName: "web-app",
			envName:   "",
			ref:       "develop",
			want:      "web-app/ (develop)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DiffLabel(tt.chartName, tt.envName, tt.ref)
			if got != tt.want {
				t.Errorf("DiffLabel() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGroupByChart(t *testing.T) {
	tests := []struct {
		name    string
		results []DiffResult
		want    [][]DiffResult
	}{
		{
			name:    "empty results",
			results: []DiffResult{},
			want:    [][]DiffResult{},
		},
		{
			name: "single chart",
			results: []DiffResult{
				{ChartName: "app1", Environment: "prod"},
				{ChartName: "app1", Environment: "staging"},
			},
			want: [][]DiffResult{
				{
					{ChartName: "app1", Environment: "prod"},
					{ChartName: "app1", Environment: "staging"},
				},
			},
		},
		{
			name: "multiple charts",
			results: []DiffResult{
				{ChartName: "app1", Environment: "prod"},
				{ChartName: "app2", Environment: "prod"},
				{ChartName: "app1", Environment: "staging"},
			},
			want: [][]DiffResult{
				{
					{ChartName: "app1", Environment: "prod"},
					{ChartName: "app1", Environment: "staging"},
				},
				{
					{ChartName: "app2", Environment: "prod"},
				},
			},
		},
		{
			name: "preserves insertion order",
			results: []DiffResult{
				{ChartName: "zebra", Environment: "prod"},
				{ChartName: "alpha", Environment: "prod"},
				{ChartName: "zebra", Environment: "staging"},
			},
			want: [][]DiffResult{
				{
					{ChartName: "zebra", Environment: "prod"},
					{ChartName: "zebra", Environment: "staging"},
				},
				{
					{ChartName: "alpha", Environment: "prod"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GroupByChart(tt.results)

			if len(got) != len(tt.want) {
				t.Fatalf("GroupByChart() returned %d groups, want %d", len(got), len(tt.want))
			}

			for i := range got {
				if len(got[i]) != len(tt.want[i]) {
					t.Errorf("Group %d has %d results, want %d", i, len(got[i]), len(tt.want[i]))
					continue
				}

				for j := range got[i] {
					if got[i][j].ChartName != tt.want[i][j].ChartName {
						t.Errorf("Group %d, result %d: ChartName = %q, want %q",
							i, j, got[i][j].ChartName, tt.want[i][j].ChartName)
					}
					if got[i][j].Environment != tt.want[i][j].Environment {
						t.Errorf("Group %d, result %d: Environment = %q, want %q",
							i, j, got[i][j].Environment, tt.want[i][j].Environment)
					}
				}
			}
		})
	}
}
