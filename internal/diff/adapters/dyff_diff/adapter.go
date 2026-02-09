package dyffdiff

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Adapter implements ports.DiffPort using the dyff CLI for semantic YAML diffing.
type Adapter struct{}

// New creates a new dyff-based diff adapter.
func New() *Adapter {
	return &Adapter{}
}

// ComputeDiff uses dyff CLI to compute a semantic YAML diff.
// Returns empty string if dyff is not available (caller should use fallback).
func (a *Adapter) ComputeDiff(baseName, headName string, base, head []byte) string {
	// Check if dyff is available
	dyffPath, err := exec.LookPath("dyff")
	if err != nil {
		return "" // dyff not available
	}

	// Create temp dir for manifest files
	tmpDir, err := os.MkdirTemp("", "chart-val-dyff-*")
	if err != nil {
		return ""
	}
	defer os.RemoveAll(tmpDir)

	// Write manifests to temp files
	baseFile := filepath.Join(tmpDir, "base.yaml")
	headFile := filepath.Join(tmpDir, "head.yaml")

	if err := os.WriteFile(baseFile, base, 0o600); err != nil {
		return ""
	}
	if err := os.WriteFile(headFile, head, 0o600); err != nil {
		return ""
	}

	// Run dyff between base.yaml head.yaml
	// --color=off: GitHub markdown doesn't render ANSI escape codes
	// --set-exit-code: Makes dyff return 0 for no changes, 1 for changes, 255 for errors
	cmd := exec.Command(dyffPath, "between", "--color=off", "--set-exit-code", baseFile, headFile)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	// Check exit code to determine if changes exist
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode := exitErr.ExitCode()
		if exitCode == 1 {
			// Exit code 1 = differences detected (expected)
		} else {
			// Exit code 255 or other = error, return empty (caller uses fallback)
			return ""
		}
	} else if err != nil {
		// Command failed to run, return empty (caller uses fallback)
		return ""
	} else {
		// Exit code 0 = no differences, return empty string
		return ""
	}

	// Clean up the output to remove banner and temp file paths
	output := cleanDyffOutput(stdout.String(), tmpDir)

	// Format with file names
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", baseName)
	fmt.Fprintf(&sb, "+++ %s\n\n", headName)
	sb.WriteString(output)

	return strings.TrimSpace(sb.String())
}

// cleanDyffOutput removes dyff banner and temp file paths to make output clean and deterministic.
func cleanDyffOutput(output, tmpDir string) string {
	lines := strings.Split(output, "\n")
	var cleaned []string

	for _, line := range lines {
		// Skip lines containing the temp directory path
		if strings.Contains(line, tmpDir) {
			continue
		}

		// Detect dyff banner lines and skip them
		trimmed := strings.TrimSpace(line)
		if strings.Contains(line, "_        __  __") ||
			strings.Contains(trimmed, "_| |_   _ / _|/ _|") ||
			strings.Contains(trimmed, "/ _' | | | | |_| |_") ||
			strings.Contains(trimmed, "| (_| | |_| |  _|  _|") ||
			strings.Contains(trimmed, "\\__,_|\\__, |_| |_|") ||
			strings.Contains(trimmed, "|___/") ||
			strings.Contains(line, "returned") && (strings.Contains(line, "difference") || strings.Contains(line, "differences")) {
			continue
		}

		// Skip completely empty lines at the start
		if len(cleaned) == 0 && trimmed == "" {
			continue
		}

		cleaned = append(cleaned, line)
	}

	return strings.Join(cleaned, "\n")
}
