package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	"knative.dev/pkg/logging"

	"github.com/imjasonh/git-k8s/pkg/metrics"
)

type managerKey struct{}

// WithManager returns a context with the given Manager attached.
func WithManager(ctx context.Context, m *Manager) context.Context {
	return context.WithValue(ctx, managerKey{}, m)
}

// GetManager returns the Manager attached to the context, or a default
// in-memory-only manager if none is set.
func GetManager(ctx context.Context) *Manager {
	if m, ok := ctx.Value(managerKey{}).(*Manager); ok && m != nil {
		return m
	}
	return NewManager("", false)
}

// DefaultGitTimeout is the maximum duration for a git clone or fetch.
const DefaultGitTimeout = 5 * time.Minute

// Workspace is a handle to a Git repository, backed by either disk or memory.
type Workspace struct {
	// Repo is the go-git Repository opened from the cache (or cloned in memory).
	Repo *git.Repository
	// Mode indicates whether this workspace is disk-backed or in-memory.
	Mode string // "disk" or "memory"

	path    string   // disk path (empty for in-memory)
	manager *Manager // nil for in-memory workspaces
}

// ref tracks the in-process reference count for a cached repo path.
type ref struct {
	mu    sync.Mutex
	count int
}

// Manager manages on-disk bare Git repo caches backed by a PVC.
// If basePath is empty, all Acquire calls fall back to in-memory clones.
type Manager struct {
	basePath string
	mu       sync.Mutex
	active   map[string]*ref

	// shallow controls whether initial clones use depth=1.
	// Controllers that need full history should set this to false.
	shallow bool
}

// NewManager creates a workspace Manager.
// basePath is the PVC mount point (e.g. "/data/repos"). If empty, all
// operations fall back to in-memory clones (backward compatible).
// shallow controls whether initial clones use --depth=1.
func NewManager(basePath string, shallow bool) *Manager {
	return &Manager{
		basePath: basePath,
		active:   make(map[string]*ref),
		shallow:  shallow,
	}
}

// repoPath returns the on-disk path for a given repo URL.
func (m *Manager) repoPath(repoURL string) string {
	h := sha256.Sum256([]byte(repoURL))
	return filepath.Join(m.basePath, fmt.Sprintf("%x", h[:6]))
}

// Acquire returns a Workspace for the given repo URL. If caching is enabled
// (basePath is set and cacheEnabled is true), it opens or clones a bare repo
// on disk. Otherwise it clones into memory.
func (m *Manager) Acquire(ctx context.Context, repoURL string, auth *http.BasicAuth, cacheEnabled bool) (*Workspace, error) {
	if m.basePath == "" || !cacheEnabled {
		return m.acquireInMemory(ctx, repoURL, auth)
	}
	return m.acquireOnDisk(ctx, repoURL, auth)
}

// acquireInMemory clones the repo into memory — the legacy behavior.
func (m *Manager) acquireInMemory(ctx context.Context, repoURL string, auth *http.BasicAuth) (*Workspace, error) {
	cloneCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cancel()

	start := time.Now()
	storer := memory.NewStorage()
	repo, err := git.CloneContext(cloneCtx, storer, nil, &git.CloneOptions{
		URL:  repoURL,
		Auth: auth,
	})
	metrics.WorkspaceAcquireDuration.WithLabelValues("memory").Observe(time.Since(start).Seconds())
	if err != nil {
		return nil, fmt.Errorf("in-memory clone: %w", err)
	}

	return &Workspace{
		Repo: repo,
		Mode: "memory",
	}, nil
}

// acquireOnDisk opens an existing bare repo on disk and fetches, or does an
// initial clone if the cache directory doesn't exist yet.
func (m *Manager) acquireOnDisk(ctx context.Context, repoURL string, auth *http.BasicAuth) (*Workspace, error) {
	path := m.repoPath(repoURL)

	// Per-path in-process locking.
	r := m.getRef(path)
	r.mu.Lock()

	start := time.Now()

	headPath := filepath.Join(path, "HEAD")
	if _, err := os.Stat(headPath); err == nil {
		// Cache hit: open existing bare repo and fetch.
		repo, err := git.PlainOpen(path)
		if err != nil {
			r.mu.Unlock()
			return nil, fmt.Errorf("opening cached repo at %s: %w", path, err)
		}

		fetchCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
		defer cancel()

		err = repo.FetchContext(fetchCtx, &git.FetchOptions{
			Auth:     auth,
			Force:    true,
			RefSpecs: []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"},
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			// Fetch failed — fall back to in-memory for this reconcile.
			r.mu.Unlock()
			logger := logging.FromContext(ctx)
			logger.Warnf("Fetch failed on cached repo %s, falling back to memory: %v", path, err)
			return m.acquireInMemory(ctx, repoURL, auth)
		}

		metrics.WorkspaceAcquireDuration.WithLabelValues("disk").Observe(time.Since(start).Seconds())
		metrics.WorkspaceCacheHit.Inc()

		return &Workspace{
			Repo:    repo,
			Mode:    "disk",
			path:    path,
			manager: m,
		}, nil
	}

	// Cache miss: initial bare clone.
	if err := os.MkdirAll(path, 0o755); err != nil {
		r.mu.Unlock()
		return nil, fmt.Errorf("creating cache dir %s: %w", path, err)
	}

	cloneCtx, cloneCancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cloneCancel()

	cloneOpts := &git.CloneOptions{
		URL:  repoURL,
		Auth: auth,
	}
	if m.shallow {
		cloneOpts.Depth = 1
	}

	repo, err := git.PlainCloneContext(cloneCtx, path, true /* isBare */, cloneOpts)
	if err != nil {
		// Clean up failed clone attempt.
		os.RemoveAll(path)
		r.mu.Unlock()
		// Fall back to in-memory.
		logger := logging.FromContext(ctx)
		logger.Warnf("Disk clone failed for %s, falling back to memory: %v", repoURL, err)
		return m.acquireInMemory(ctx, repoURL, auth)
	}

	metrics.WorkspaceAcquireDuration.WithLabelValues("disk").Observe(time.Since(start).Seconds())
	metrics.WorkspaceCacheMiss.Inc()

	return &Workspace{
		Repo:    repo,
		Mode:    "disk",
		path:    path,
		manager: m,
	}, nil
}

// Release releases a workspace. For disk-backed workspaces this releases
// the in-process lock. For in-memory workspaces this is a no-op.
func (m *Manager) Release(ws *Workspace) {
	if ws == nil || ws.manager == nil {
		return
	}
	m.mu.Lock()
	r, ok := m.active[ws.path]
	if !ok {
		m.mu.Unlock()
		return
	}
	r.count--
	if r.count == 0 {
		delete(m.active, ws.path)
	}
	m.mu.Unlock()
	r.mu.Unlock()
}

// getRef returns (or creates) a ref for the given path, incrementing the refcount.
func (m *Manager) getRef(path string) *ref {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.active[path]
	if !ok {
		r = &ref{}
		m.active[path] = r
	}
	r.count++
	return r
}

// GC removes cached directories that are not in the activeRepos set.
// It skips paths that have active in-process references to avoid racing
// with ongoing reconciles.
func (m *Manager) GC(ctx context.Context, activeRepos map[string]bool) {
	if m.basePath == "" {
		return
	}

	logger := logging.FromContext(ctx)
	entries, err := os.ReadDir(m.basePath)
	if err != nil {
		logger.Warnf("GC: reading cache dir: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(m.basePath, entry.Name())
		if activeRepos[dirPath] {
			continue
		}
		// Skip paths with active in-process references.
		m.mu.Lock()
		r, hasRef := m.active[dirPath]
		skip := hasRef && r.count > 0
		m.mu.Unlock()
		if skip {
			logger.Infof("GC: skipping %s (active references)", dirPath)
			continue
		}
		logger.Infof("GC: removing stale cache dir %s", dirPath)
		if err := os.RemoveAll(dirPath); err != nil {
			logger.Warnf("GC: removing %s: %v", dirPath, err)
		}
	}
}

// Deepen fetches full history for a cached repo that was initially shallow-cloned.
// This is a no-op for in-memory workspaces or already-full clones.
func (m *Manager) Deepen(ctx context.Context, ws *Workspace, auth *http.BasicAuth) error {
	if ws.Mode != "disk" {
		return nil
	}

	fetchCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cancel()

	err := ws.Repo.FetchContext(fetchCtx, &git.FetchOptions{
		Auth:     auth,
		Force:    true,
		Depth:    0, // unshallow
		RefSpecs: []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"},
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return fmt.Errorf("deepening clone: %w", err)
	}
	return nil
}

// BasePath returns the configured base path for caching.
func (m *Manager) BasePath() string {
	return m.basePath
}

// CachePath returns the on-disk path for a given repo URL, or empty string
// if caching is not configured.
func (m *Manager) CachePath(repoURL string) string {
	if m.basePath == "" {
		return ""
	}
	return m.repoPath(repoURL)
}
