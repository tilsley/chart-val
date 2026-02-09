package helmcli

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// Adapter implements ports.RendererPort by shelling out to the helm CLI.
type Adapter struct {
	helmBin string
}

// New creates a new Helm CLI adapter. It verifies that the helm binary
// is available on PATH at construction time.
func New() (*Adapter, error) {
	helmBin, err := exec.LookPath("helm")
	if err != nil {
		return nil, fmt.Errorf("helm binary not found: %w", err)
	}
	return &Adapter{helmBin: helmBin}, nil
}

// Render runs `helm template` on the given chart directory with the
// specified value files and returns the rendered manifest bytes.
func (a *Adapter) Render(ctx context.Context, chartDir string, valueFiles []string) ([]byte, error) {
	args := []string{"template", "chart-val-render", chartDir}
	for _, vf := range valueFiles {
		args = append(args, "-f", filepath.Join(chartDir, vf))
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	logger.Info("running helm template", "chartDir", chartDir, "valueFiles", valueFiles, "args", args)

	cmd := exec.CommandContext(ctx, a.helmBin, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Info("executing helm command")
	if err := cmd.Run(); err != nil {
		logger.Error("helm template failed", "error", err, "stderr", stderr.String())
		return nil, fmt.Errorf("helm template failed: %w\nstderr: %s", err, stderr.String())
	}

	logger.Info("helm command completed", "outputSize", len(stdout.Bytes()))
	return stdout.Bytes(), nil
}
