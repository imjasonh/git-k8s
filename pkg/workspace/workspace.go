package workspace

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/filesystem"
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
// Disk-backed workspaces are isolated from each other via git alternates,
// allowing concurrent operations on different branches of the same repo.
type Workspace struct {
	// Repo is the go-git Repository opened from the cache (or cloned in memory).
	Repo *git.Repository
	// Mode indicates whether this workspace is disk-backed or in-memory.
	Mode string // "disk" or "memory"

	path      string   // worktree temp dir (empty for in-memory)
	cachePath string   // cache bare repo path (empty for in-memory)
	manager   *Manager // nil for in-memory workspaces
}

// ref tracks the cache state: a mutex for serializing fetch/clone operations
// and a count of active worktrees referencing this cache path.
type ref struct {
	mu    sync.Mutex
	count int
}

// Manager manages on-disk bare Git repo caches backed by a PVC.
// If basePath is empty, all Acquire calls fall back to in-memory clones.
//
// Each Acquire creates an isolated workspace directory that shares the cache's
// object store via git alternates. This allows concurrent operations on
// different branches of the same repo without interference.
type Manager struct {
	basePath string
	mu       sync.Mutex
	active   map[string]*ref

	// worktrees tracks active worktree paths so GC can skip them.
	worktrees map[string]bool

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
		basePath:  basePath,
		active:    make(map[string]*ref),
		worktrees: make(map[string]bool),
		shallow:   shallow,
	}
}

// repoPath returns the on-disk path for a given repo URL.
func (m *Manager) repoPath(repoURL string) string {
	h := sha256.Sum256([]byte(repoURL))
	return filepath.Join(m.basePath, fmt.Sprintf("%x", h[:6]))
}

// Acquire returns a Workspace for the given repo URL. If caching is enabled
// (basePath is set and cacheEnabled is true), it creates an isolated workspace
// backed by a shared object cache via git alternates. Otherwise it clones
// into memory.
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

// acquireOnDisk updates the shared cache and creates an isolated workspace.
// The cache lock is only held during the fetch/clone, not for the workspace
// lifetime, so multiple workspaces for the same repo can be active concurrently.
func (m *Manager) acquireOnDisk(ctx context.Context, repoURL string, auth *http.BasicAuth) (*Workspace, error) {
	cachePath := m.repoPath(repoURL)
	logger := logging.FromContext(ctx)
	start := time.Now()

	// Step 1: Update the shared cache (briefly locked).
	r := m.getRef(cachePath)
	r.mu.Lock()
	cacheErr := m.updateCache(ctx, cachePath, repoURL, auth)
	r.mu.Unlock()

	if cacheErr != nil {
		logger.Warnf("Cache update failed for %s, falling back to memory: %v", repoURL, cacheErr)
		return m.acquireInMemory(ctx, repoURL, auth)
	}

	// Step 2: Track active usage for GC protection.
	m.incRef(cachePath)

	// Step 3: Create an isolated worktree from the cache.
	ws, err := m.createWorktree(cachePath)
	if err != nil {
		m.decRef(cachePath)
		logger.Warnf("Worktree creation failed for %s, falling back to memory: %v", repoURL, err)
		return m.acquireInMemory(ctx, repoURL, auth)
	}

	metrics.WorkspaceAcquireDuration.WithLabelValues("disk").Observe(time.Since(start).Seconds())
	return ws, nil
}

// updateCache fetches into an existing cache or performs the initial clone.
// Caller must hold the cache lock.
func (m *Manager) updateCache(ctx context.Context, cachePath, repoURL string, auth *http.BasicAuth) error {
	headPath := filepath.Join(cachePath, "HEAD")
	if _, err := os.Stat(headPath); err == nil {
		// Cache hit: fetch.
		repo, err := git.PlainOpen(cachePath)
		if err != nil {
			return fmt.Errorf("opening cached repo at %s: %w", cachePath, err)
		}

		fetchCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
		defer cancel()

		err = repo.FetchContext(fetchCtx, &git.FetchOptions{
			Auth:     auth,
			Force:    true,
			RefSpecs: []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"},
		})
		if err != nil && err != git.NoErrAlreadyUpToDate {
			return fmt.Errorf("fetching into cache: %w", err)
		}
		metrics.WorkspaceCacheHit.Inc()
		return nil
	}

	// Cache miss: initial bare clone.
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		return fmt.Errorf("creating cache dir %s: %w", cachePath, err)
	}

	cloneCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cancel()

	cloneOpts := &git.CloneOptions{
		URL:  repoURL,
		Auth: auth,
	}
	if m.shallow {
		cloneOpts.Depth = 1
	}

	_, err := git.PlainCloneContext(cloneCtx, cachePath, true /* isBare */, cloneOpts)
	if err != nil {
		os.RemoveAll(cachePath)
		return fmt.Errorf("cloning into cache: %w", err)
	}

	metrics.WorkspaceCacheMiss.Inc()
	return nil
}

// createWorktree creates an isolated bare repo that shares objects with the
// cache via git alternates. This allows concurrent operations on different
// branches of the same repo without interference: each workspace has its own
// refs, HEAD, and local object storage, while sharing the bulk object store.
func (m *Manager) createWorktree(cachePath string) (*Workspace, error) {
	wtBase := filepath.Join(m.basePath, "worktrees")
	if err := os.MkdirAll(wtBase, 0o755); err != nil {
		return nil, fmt.Errorf("creating worktrees dir: %w", err)
	}

	wtDir, err := os.MkdirTemp(wtBase, "ws-")
	if err != nil {
		return nil, fmt.Errorf("creating worktree temp dir: %w", err)
	}

	cleanup := func() { os.RemoveAll(wtDir) }

	// Initialize a bare repo.
	_, err = git.PlainInit(wtDir, true /* isBare */)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("initializing worktree repo: %w", err)
	}

	// Set up alternates to share the cache's object store.
	alternatesDir := filepath.Join(wtDir, "objects", "info")
	if err := os.MkdirAll(alternatesDir, 0o755); err != nil {
		cleanup()
		return nil, fmt.Errorf("creating alternates dir: %w", err)
	}
	cacheObjects := filepath.Join(cachePath, "objects")
	if err := os.WriteFile(filepath.Join(alternatesDir, "alternates"), []byte(cacheObjects+"\n"), 0o644); err != nil {
		cleanup()
		return nil, fmt.Errorf("writing alternates file: %w", err)
	}

	// Re-open with AlternatesFS set to the root filesystem so go-git
	// can resolve absolute paths in the alternates file. PlainOpen uses
	// a ChrootHelper that prevents escaping the repo root, which would
	// block access to the cache's object store.
	repoFS := osfs.New(wtDir)
	storage := filesystem.NewStorageWithOptions(
		repoFS,
		cache.NewObjectLRUDefault(),
		filesystem.Options{
			AlternatesFS: osfs.New("/"),
		},
	)
	repo, err := git.Open(storage, nil)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("opening worktree with alternates: %w", err)
	}

	// Copy refs from cache.
	cacheRepo, err := git.PlainOpen(cachePath)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("opening cache for ref copy: %w", err)
	}

	refs, err := cacheRepo.References()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("listing cache refs: %w", err)
	}
	if err := refs.ForEach(func(ref *plumbing.Reference) error {
		return repo.Storer.SetReference(ref)
	}); err != nil {
		cleanup()
		return nil, fmt.Errorf("copying refs: %w", err)
	}

	// Copy HEAD (may be symbolic, e.g. ref: refs/heads/main).
	headRef, err := cacheRepo.Reference(plumbing.HEAD, false)
	if err == nil {
		repo.Storer.SetReference(headRef)
	}

	// Copy remote config so push operations use the real remote URL.
	cacheRemote, err := cacheRepo.Remote("origin")
	if err == nil {
		repo.CreateRemote(cacheRemote.Config())
	}

	// Track this worktree path so GC skips it.
	m.mu.Lock()
	m.worktrees[wtDir] = true
	m.mu.Unlock()

	return &Workspace{
		Repo:      repo,
		Mode:      "disk",
		path:      wtDir,
		cachePath: cachePath,
		manager:   m,
	}, nil
}

// Release releases a workspace. For disk-backed workspaces this flushes any
// new objects back to the cache (so other workspaces can find them) and
// removes the isolated worktree directory. For in-memory workspaces this
// is a no-op.
func (m *Manager) Release(ws *Workspace) {
	if ws == nil || ws.manager == nil {
		return
	}

	if ws.path != "" {
		// Flush new objects from the worktree to the cache so that
		// subsequently created workspaces (e.g. push controller) can
		// access objects created during this workspace's lifetime
		// (e.g. merge commits from the resolver).
		if ws.cachePath != "" {
			m.flushObjects(ws.path, ws.cachePath)
		}

		// Untrack this worktree.
		m.mu.Lock()
		delete(m.worktrees, ws.path)
		m.mu.Unlock()

		os.RemoveAll(ws.path)
	}

	if ws.cachePath != "" {
		m.decRef(ws.cachePath)
	}
}

// flushObjects copies loose objects from the worktree's object store back to
// the cache. This is necessary because objects created in the worktree (e.g.
// merge commits) are stored locally in the worktree, not in the cache. Other
// workspaces that share the cache via alternates won't see them unless they
// are copied back. Since objects are content-addressed, this is idempotent.
func (m *Manager) flushObjects(wtPath, cachePath string) {
	wtObjects := filepath.Join(wtPath, "objects")
	cacheObjects := filepath.Join(cachePath, "objects")

	entries, err := os.ReadDir(wtObjects)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "info" || entry.Name() == "pack" {
			continue
		}
		// Two-character hex directories contain loose objects.
		srcDir := filepath.Join(wtObjects, entry.Name())
		dstDir := filepath.Join(cacheObjects, entry.Name())
		os.MkdirAll(dstDir, 0o755)

		objects, err := os.ReadDir(srcDir)
		if err != nil {
			continue
		}
		for _, obj := range objects {
			src := filepath.Join(srcDir, obj.Name())
			dst := filepath.Join(dstDir, obj.Name())
			if _, err := os.Stat(dst); err == nil {
				continue // already exists in cache
			}
			data, err := os.ReadFile(src)
			if err != nil {
				continue
			}
			os.WriteFile(dst, data, 0o444)
		}
	}
}

// getRef returns the ref for the given cache path, creating one if needed.
// Does NOT increment the worktree count.
func (m *Manager) getRef(path string) *ref {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.active[path]
	if !ok {
		r = &ref{}
		m.active[path] = r
	}
	return r
}

// incRef increments the active worktree count for a cache path.
func (m *Manager) incRef(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.active[path]
	if !ok {
		r = &ref{}
		m.active[path] = r
	}
	r.count++
}

// decRef decrements the active worktree count, removing the entry when zero.
func (m *Manager) decRef(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.active[path]
	if !ok {
		return
	}
	r.count--
	if r.count <= 0 {
		delete(m.active, path)
	}
}

// GC removes cached directories that are not in the activeRepos set.
// It skips paths that have active in-process references to avoid racing
// with ongoing reconciles. It also cleans up stale worktree directories
// left behind by crashes.
func (m *Manager) GC(ctx context.Context, activeRepos map[string]bool) {
	if m.basePath == "" {
		return
	}

	logger := logging.FromContext(ctx)

	// Clean up stale worktree directories (from crashes).
	wtBase := filepath.Join(m.basePath, "worktrees")
	if entries, err := os.ReadDir(wtBase); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dirPath := filepath.Join(wtBase, entry.Name())
			// Skip worktrees that are still active.
			m.mu.Lock()
			active := m.worktrees[dirPath]
			m.mu.Unlock()
			if active {
				continue
			}
			logger.Infof("GC: removing stale worktree %s", dirPath)
			os.RemoveAll(dirPath)
		}
	}

	// Clean up stale cache directories.
	entries, err := os.ReadDir(m.basePath)
	if err != nil {
		logger.Warnf("GC: reading cache dir: %v", err)
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == "worktrees" {
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
// The fetch targets the shared cache repo; the workspace sees the new objects
// automatically via alternates. This is a no-op for in-memory workspaces or
// workspaces without a cache path.
func (m *Manager) Deepen(ctx context.Context, ws *Workspace, auth *http.BasicAuth) error {
	if ws.Mode != "disk" || ws.cachePath == "" {
		return nil
	}

	// Lock the cache briefly for the fetch.
	r := m.getRef(ws.cachePath)
	r.mu.Lock()
	defer r.mu.Unlock()

	cacheRepo, err := git.PlainOpen(ws.cachePath)
	if err != nil {
		return fmt.Errorf("opening cache for deepen: %w", err)
	}

	fetchCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cancel()

	err = cacheRepo.FetchContext(fetchCtx, &git.FetchOptions{
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
