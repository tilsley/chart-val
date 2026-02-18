package gitrepo

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Parallel()

	repo := New("https://example.com/repo.git", "/tmp/test", 5*time.Minute, nil)

	if repo.repoURL != "https://example.com/repo.git" {
		t.Errorf("repoURL = %q, want %q", repo.repoURL, "https://example.com/repo.git")
	}
	if repo.localPath != "/tmp/test" {
		t.Errorf("localPath = %q, want %q", repo.localPath, "/tmp/test")
	}
	if repo.syncInterval != 5*time.Minute {
		t.Errorf("syncInterval = %v, want %v", repo.syncInterval, 5*time.Minute)
	}
	if repo.Ready() {
		t.Error("Ready() should be false before Start")
	}
	if repo.Path() != "/tmp/test" {
		t.Errorf("Path() = %q, want %q", repo.Path(), "/tmp/test")
	}
}

func TestStart_CloneAndReady(t *testing.T) {
	t.Parallel()

	// Create a bare git repo to clone from
	bareDir := t.TempDir()
	cloneDir := filepath.Join(t.TempDir(), "clone")

	initBareRepo(t, bareDir)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	repo := New(bareDir, cloneDir, 1*time.Hour, logger)

	if repo.Ready() {
		t.Fatal("Ready() should be false before Start")
	}

	if err := repo.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer repo.Stop()

	if !repo.Ready() {
		t.Error("Ready() should be true after Start")
	}
	if repo.Path() != cloneDir {
		t.Errorf("Path() = %q, want %q", repo.Path(), cloneDir)
	}

	// Verify .git exists in cloneDir
	if _, err := os.Stat(filepath.Join(cloneDir, ".git")); err != nil {
		t.Errorf("expected .git directory in clone: %v", err)
	}
}

func TestStart_PullWhenAlreadyCloned(t *testing.T) {
	t.Parallel()

	bareDir := t.TempDir()
	cloneDir := filepath.Join(t.TempDir(), "clone")

	initBareRepo(t, bareDir)

	// Pre-clone so Start does a pull instead
	runGit(t, "", "clone", "--depth=1", bareDir, cloneDir)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	repo := New(bareDir, cloneDir, 1*time.Hour, logger)

	if err := repo.Start(context.Background()); err != nil {
		t.Fatalf("Start (pull path) failed: %v", err)
	}
	defer repo.Stop()

	if !repo.Ready() {
		t.Error("Ready() should be true after Start")
	}
}

func TestOnSync_CalledAfterStart(t *testing.T) {
	t.Parallel()

	bareDir := t.TempDir()
	cloneDir := filepath.Join(t.TempDir(), "clone")

	initBareRepo(t, bareDir)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	repo := New(bareDir, cloneDir, 1*time.Hour, logger)

	var called atomic.Int32
	repo.OnSync(func() { called.Add(1) })

	if err := repo.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer repo.Stop()

	if called.Load() != 1 {
		t.Errorf("OnSync callback called %d times, want 1", called.Load())
	}
}

func TestOnSync_MultipleCallbacks(t *testing.T) {
	t.Parallel()

	bareDir := t.TempDir()
	cloneDir := filepath.Join(t.TempDir(), "clone")

	initBareRepo(t, bareDir)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	repo := New(bareDir, cloneDir, 1*time.Hour, logger)

	var first, second atomic.Int32
	repo.OnSync(func() { first.Add(1) })
	repo.OnSync(func() { second.Add(1) })

	if err := repo.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer repo.Stop()

	if first.Load() != 1 {
		t.Errorf("first callback called %d times, want 1", first.Load())
	}
	if second.Load() != 1 {
		t.Errorf("second callback called %d times, want 1", second.Load())
	}
}

func TestStop(t *testing.T) {
	t.Parallel()

	bareDir := t.TempDir()
	cloneDir := filepath.Join(t.TempDir(), "clone")

	initBareRepo(t, bareDir)

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	// Use short sync interval so goroutine would tick if not stopped
	repo := New(bareDir, cloneDir, 10*time.Millisecond, logger)

	if err := repo.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Stop should not panic or block
	repo.Stop()

	// Give goroutine time to exit
	time.Sleep(50 * time.Millisecond)
}

// initBareRepo creates a git repo with one commit, suitable for cloning.
func initBareRepo(t *testing.T, dir string) {
	t.Helper()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	// Create a file and commit it
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("test"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\noutput: %s", args, err, output)
	}
}
