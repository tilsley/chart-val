package sourcectrl

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/nathantilsley/chart-val/internal/diff/domain"
)

// Adapter implements ports.SourceControlPort by downloading a repo
// tarball and extracting the chart directory.
type Adapter struct {
	client *gogithub.Client
}

// New creates a new source control adapter.
func New(client *gogithub.Client) *Adapter {
	return &Adapter{client: client}
}

// FetchChartFiles downloads the repo tarball at the given ref, extracts it
// to a temp directory, and returns the path to the chart subdirectory.
// The caller must invoke cleanup() when done to remove the temp files.
func (a *Adapter) FetchChartFiles(ctx context.Context, owner, repo, ref, chartPath string) (string, func(), error) {
	client := a.client

	archiveURL, _, err := client.Repositories.GetArchiveLink(ctx, owner, repo, gogithub.Tarball, &gogithub.RepositoryContentGetOptions{
		Ref: ref,
	}, 10)
	if err != nil {
		return "", nil, fmt.Errorf("getting archive link: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL.String(), nil)
	if err != nil {
		return "", nil, fmt.Errorf("creating archive request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("downloading archive: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected status downloading archive: %d", resp.StatusCode)
	}

	tmpDir, err := os.MkdirTemp("", "chart-val-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	cleanup := func() { os.RemoveAll(tmpDir) }

	if err := extractTarGz(resp.Body, tmpDir); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("extracting archive: %w", err)
	}

	// GitHub tarballs contain a single top-level directory (e.g. owner-repo-sha/).
	// Find it so we can resolve the chart path relative to it.
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("reading temp dir: %w", err)
	}
	if len(entries) == 0 {
		cleanup()
		return "", nil, fmt.Errorf("empty archive")
	}

	repoRoot := filepath.Join(tmpDir, entries[0].Name())
	chartDir := filepath.Join(repoRoot, chartPath)

	if _, err := os.Stat(chartDir); err != nil {
		cleanup()
		// Wrap with NotFoundError so service can detect new charts
		return "", nil, domain.NewNotFoundError(chartPath, ref)
	}

	return chartDir, cleanup, nil
}

func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar entry: %w", err)
		}

		target := filepath.Join(dest, header.Name)

		// Guard against zip-slip
		if !strings.HasPrefix(filepath.Clean(target), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("creating directory: %w", err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("creating parent directory: %w", err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("creating file: %w", err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("writing file: %w", err)
			}
			f.Close()
		}
	}
	return nil
}
