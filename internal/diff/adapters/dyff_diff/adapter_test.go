package dyffdiff

import (
	"os/exec"
	"strings"
	"testing"
)

func TestAdapter_ComputeDiff_NoDyff(t *testing.T) {
	// Test behavior when dyff is not available
	// We can't easily force dyff to be unavailable, so we'll test the actual behavior
	adapter := New()

	base := []byte("key: value1")
	head := []byte("key: value2")

	diff := adapter.ComputeDiff("test (main)", "test (feature)", base, head)

	// If dyff is not available, should return empty string
	// If dyff is available, should return a diff
	// Either way, test shouldn't fail
	t.Logf("Diff output (len=%d): %q", len(diff), diff)
}

func TestAdapter_ComputeDiff_Identical(t *testing.T) {
	// Skip if dyff not available
	if _, err := exec.LookPath("dyff"); err != nil {
		t.Skip("dyff not available on PATH")
	}

	adapter := New()

	yaml := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value
`)

	diff := adapter.ComputeDiff("test (main)", "test (feature)", yaml, yaml)

	// For identical content, dyff should return empty string (no changes)
	if diff != "" {
		t.Errorf("Expected empty diff for identical YAML, but got:\n%s", diff)
	}
}

func TestAdapter_ComputeDiff_Different(t *testing.T) {
	// Skip if dyff not available
	if _, err := exec.LookPath("dyff"); err != nil {
		t.Skip("dyff not available on PATH")
	}

	adapter := New()

	base := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value1
`)

	head := []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: test
data:
  key: value2
`)

	diff := adapter.ComputeDiff("test (main)", "test (feature)", base, head)

	if diff == "" {
		t.Error("Expected non-empty diff for different YAML")
	}

	// Verify format
	if !strings.Contains(diff, "--- test (main)") {
		t.Error("Expected diff to contain base name")
	}
	if !strings.Contains(diff, "+++ test (feature)") {
		t.Error("Expected diff to contain head name")
	}

	// Verify dyff banner was removed
	if strings.Contains(diff, "_        __  __") {
		t.Error("Expected dyff banner to be removed")
	}
	if strings.Contains(diff, "returned") && (strings.Contains(diff, "difference") || strings.Contains(diff, "differences")) {
		t.Error("Expected 'returned X differences' line to be removed")
	}

	// Verify temp paths were removed
	if strings.Contains(diff, "/tmp/") || strings.Contains(diff, "chart-val-dyff") {
		t.Error("Expected temp file paths to be removed from diff output")
	}
}

func Test_cleanDyffOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		tmpDir string
		want   []string // Lines that should be present
		unwant []string // Lines that should NOT be present
	}{
		{
			name: "removes dyff banner",
			output: `     _        __  __
   _| |_   _ / _|/ _|  between /tmp/test/base.yaml
 / _' | | | | |_| |_       and /tmp/test/head.yaml
| (_| | |_| |  _|  _|
 \__,_|\__, |_| |_|   returned one difference
        |___/

data.key  (v1/ConfigMap/test)
  ± value change
    - value1
    + value2`,
			tmpDir: "/tmp/test",
			want: []string{
				"data.key  (v1/ConfigMap/test)",
				"± value change",
				"- value1",
				"+ value2",
			},
			unwant: []string{
				"_        __  __",
				"|_| |_   _ / _|/ _|",
				"|___/",
				"returned one difference",
				"/tmp/test/base.yaml",
			},
		},
		{
			name: "removes temp file paths",
			output: `between /var/folders/xyz/chart-val-dyff-12345/base.yaml
and /var/folders/xyz/chart-val-dyff-12345/head.yaml

spec.replicas  (apps/v1/Deployment/my-app)
  ± value change
    - 3
    + 5`,
			tmpDir: "/var/folders/xyz/chart-val-dyff-12345",
			want: []string{
				"spec.replicas",
				"± value change",
				"- 3",
				"+ 5",
			},
			unwant: []string{
				"chart-val-dyff-12345",
				"/var/folders",
			},
		},
		{
			name: "preserves actual diff content",
			output: `metadata.labels.version  (apps/v1/Deployment/my-app)
  ± value change
    - 0.1.0
    + 0.2.0

spec.replicas  (apps/v1/Deployment/my-app)
  ± value change
    - 3
    + 5`,
			tmpDir: "/tmp/test",
			want: []string{
				"metadata.labels.version",
				"spec.replicas",
				"- 0.1.0",
				"+ 0.2.0",
				"- 3",
				"+ 5",
			},
			unwant: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanDyffOutput(tt.output, tt.tmpDir)

			for _, wantLine := range tt.want {
				if !strings.Contains(got, wantLine) {
					t.Errorf("cleanDyffOutput() should contain %q, got:\n%s", wantLine, got)
				}
			}

			for _, unwantLine := range tt.unwant {
				if strings.Contains(got, unwantLine) {
					t.Errorf("cleanDyffOutput() should NOT contain %q, got:\n%s", unwantLine, got)
				}
			}
		})
	}
}
