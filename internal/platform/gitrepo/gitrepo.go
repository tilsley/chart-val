// Package gitrepo manages a local git clone's lifecycle: clone, pull, and periodic background sync.
package gitrepo

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// GitRepo owns the clone/pull/sync lifecycle for a single git repository.
type GitRepo struct {
	repoURL      string
	localPath    string
	syncInterval time.Duration
	logger       *slog.Logger

	ready  atomic.Bool
	stopCh chan struct{}
	onSync []func()   // callbacks after each successful sync
	mu     sync.Mutex // serializes pull + callbacks
}

// New creates a GitRepo. No I/O is performed; call Start to clone/pull.
func New(repoURL, localPath string, syncInterval time.Duration, logger *slog.Logger) *GitRepo {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &GitRepo{
		repoURL:      repoURL,
		localPath:    localPath,
		syncInterval: syncInterval,
		logger:       logger,
		stopCh:       make(chan struct{}),
	}
}

// OnSync registers a callback invoked (under mu) after each successful git pull.
func (r *GitRepo) OnSync(fn func()) {
	r.onSync = append(r.onSync, fn)
}

// Start performs the initial clone (or pull if already cloned), invokes OnSync
// callbacks, marks the repo as ready, and starts the background sync goroutine.
func (r *GitRepo) Start(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.initRepo(ctx); err != nil {
		return fmt.Errorf("initializing repo: %w", err)
	}

	r.runCallbacks()
	r.ready.Store(true)

	go r.syncLoop(ctx)
	r.logger.Info("gitrepo started", "repoURL", r.repoURL, "syncInterval", r.syncInterval)
	return nil
}

// Ready returns true after Start completes the initial clone and first callback cycle.
func (r *GitRepo) Ready() bool {
	return r.ready.Load()
}

// Path returns the local filesystem path of the cloned repository.
func (r *GitRepo) Path() string {
	return r.localPath
}

// Stop signals the background sync goroutine to exit.
func (r *GitRepo) Stop() {
	close(r.stopCh)
}

// initRepo clones the repository if it doesn't exist, or pulls latest if it does.
func (r *GitRepo) initRepo(ctx context.Context) error {
	gitDir := filepath.Join(r.localPath, ".git")

	if _, err := os.Stat(gitDir); err == nil {
		r.logger.Info("repository already exists, pulling latest")
		return r.pullRepo(ctx)
	}

	r.logger.Info("cloning repository", "repoURL", r.repoURL)
	//nolint:gosec // G204: repoURL is from trusted config, not user input
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", r.repoURL, r.localPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone failed: %w\noutput: %s", err, output)
	}
	return nil
}

// pullRepo pulls the latest changes from the remote repository.
func (r *GitRepo) pullRepo(ctx context.Context) error {
	//nolint:gosec // G204: localPath is from trusted config, not user input
	cmd := exec.CommandContext(ctx, "git", "-C", r.localPath, "pull", "--ff-only")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pull failed: %w\noutput: %s", err, output)
	}
	return nil
}

// syncLoop periodically pulls and invokes callbacks.
func (r *GitRepo) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(r.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.sync(ctx)
		case <-r.stopCh:
			r.logger.Info("stopping gitrepo sync loop")
			return
		}
	}
}

// sync performs a single pull + callback cycle under mu.
func (r *GitRepo) sync(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("syncing git repository")
	if err := r.pullRepo(ctx); err != nil {
		r.logger.Error("failed to pull repository", "error", err)
		return
	}

	r.runCallbacks()
	r.logger.Info("git repository synced successfully")
}

// runCallbacks invokes all OnSync callbacks sequentially. Must be called under mu.
func (r *GitRepo) runCallbacks() {
	for _, fn := range r.onSync {
		fn()
	}
}
