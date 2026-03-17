package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"knative.dev/pkg/logging"
	logtesting "knative.dev/pkg/logging/testing"
)

// initBareRepo creates a bare git repo on disk with an initial commit.
// Returns the repo path and the initial commit hash.
func initBareRepo(t *testing.T, dir string) (string, plumbing.Hash) {
	t.Helper()

	// Create a non-bare repo first to make commits, then clone to bare.
	workDir := filepath.Join(dir, "work")
	repo, err := git.PlainInit(workDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}

	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}

	// Create a file and commit.
	f, err := os.Create(filepath.Join(workDir, "README.md"))
	if err != nil {
		t.Fatalf("creating file: %v", err)
	}
	f.WriteString("# test repo")
	f.Close()

	if _, err := wt.Add("README.md"); err != nil {
		t.Fatalf("git add: %v", err)
	}

	hash, err := wt.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "test",
			Email: "test@example.com",
		},
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Clone to bare repo.
	bareDir := filepath.Join(dir, "bare.git")
	_, err = git.PlainClone(bareDir, true, &git.CloneOptions{
		URL: workDir,
	})
	if err != nil {
		t.Fatalf("PlainClone to bare: %v", err)
	}

	return bareDir, hash
}

func testCtx(t *testing.T) context.Context {
	t.Helper()
	logger := logtesting.TestLogger(t)
	return logging.WithLogger(context.Background(), logger)
}

func TestNewManager_EmptyBasePath(t *testing.T) {
	m := NewManager("", false)
	if m.basePath != "" {
		t.Errorf("basePath = %q, want empty", m.basePath)
	}
}

func TestNewManager_WithBasePath(t *testing.T) {
	m := NewManager("/data/repos", true)
	if m.basePath != "/data/repos" {
		t.Errorf("basePath = %q, want /data/repos", m.basePath)
	}
	if !m.shallow {
		t.Error("shallow = false, want true")
	}
}

func TestAcquire_InMemoryFallback_EmptyBasePath(t *testing.T) {
	// With empty basePath, Acquire should always use in-memory even if
	// cacheEnabled is true.
	m := NewManager("", false)
	ctx := testCtx(t)

	// This will fail to clone because the URL is invalid, but we're testing
	// the code path selection.
	_, err := m.Acquire(ctx, "https://nonexistent.invalid/repo.git", nil, true)
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
	// The error should come from in-memory clone path.
	if ws, err2 := m.Acquire(ctx, "https://nonexistent.invalid/repo.git", nil, false); err2 == nil {
		t.Fatalf("expected error, got workspace mode=%s", ws.Mode)
	}
}

func TestAcquire_InMemoryFallback_CacheDisabled(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir, false)
	ctx := testCtx(t)

	// cacheEnabled=false should force in-memory even with a basePath.
	_, err := m.Acquire(ctx, "https://nonexistent.invalid/repo.git", nil, false)
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestAcquire_DiskClone_NewRepo(t *testing.T) {
	// Create a source repo to clone from.
	sourceDir := t.TempDir()
	barePath, _ := initBareRepo(t, sourceDir)

	cacheDir := t.TempDir()
	m := NewManager(cacheDir, false)
	ctx := testCtx(t)

	// First acquire: should do a full clone (cache miss).
	ws, err := m.Acquire(ctx, barePath, nil, true)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if ws.Mode != "disk" {
		t.Errorf("Mode = %q, want disk", ws.Mode)
	}
	if ws.Repo == nil {
		t.Fatal("Repo is nil")
	}

	// Verify we can read the commit.
	head, err := ws.Repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Error("HEAD hash is zero")
	}

	m.Release(ws)
}

func TestAcquire_DiskFetch_ExistingRepo(t *testing.T) {
	// Create a source repo.
	sourceDir := t.TempDir()
	barePath, initialHash := initBareRepo(t, sourceDir)

	cacheDir := t.TempDir()
	m := NewManager(cacheDir, false)
	ctx := testCtx(t)

	// First acquire: clone.
	ws1, err := m.Acquire(ctx, barePath, nil, true)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	head1, _ := ws1.Repo.Head()
	if head1.Hash() != initialHash {
		t.Errorf("first HEAD = %s, want %s", head1.Hash(), initialHash)
	}
	m.Release(ws1)

	// Add a new commit to the bare source repo directly.
	srcBare, err := git.PlainOpen(barePath)
	if err != nil {
		t.Fatalf("opening bare source repo: %v", err)
	}
	// Create a new commit by modifying the bare repo directly.
	srcStorer := srcBare.Storer
	headRef, _ := srcBare.Head()
	parentCommit, _ := srcBare.CommitObject(headRef.Hash())
	newCommit := &object.Commit{
		Author:       object.Signature{Name: "test", Email: "test@example.com"},
		Committer:    object.Signature{Name: "test", Email: "test@example.com"},
		Message:      "second commit",
		TreeHash:     parentCommit.TreeHash,
		ParentHashes: []plumbing.Hash{headRef.Hash()},
	}
	obj := srcStorer.NewEncodedObject()
	newCommit.Encode(obj)
	newHash, _ := srcStorer.SetEncodedObject(obj)
	// Update refs/heads/main to point to the new commit.
	ref := plumbing.NewHashReference("refs/heads/main", newHash)
	srcStorer.SetReference(ref)

	// Second acquire: should fetch and get new commit via refs/heads/*.
	ws2, err := m.Acquire(ctx, barePath, nil, true)
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	// Check that the main branch ref was updated.
	mainRef, err := ws2.Repo.Reference("refs/heads/main", true)
	if err != nil {
		t.Fatalf("getting refs/heads/main: %v", err)
	}
	if mainRef.Hash() != newHash {
		t.Errorf("refs/heads/main = %s, want %s", mainRef.Hash(), newHash)
	}
	m.Release(ws2)
}

func TestAcquire_ShallowClone(t *testing.T) {
	sourceDir := t.TempDir()
	barePath, _ := initBareRepo(t, sourceDir)

	cacheDir := t.TempDir()
	m := NewManager(cacheDir, true /* shallow */)
	ctx := testCtx(t)

	ws, err := m.Acquire(ctx, barePath, nil, true)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if ws.Mode != "disk" {
		t.Errorf("Mode = %q, want disk", ws.Mode)
	}
	m.Release(ws)
}

func TestDeepen_NoOpForMemory(t *testing.T) {
	m := NewManager("", false)
	ws := &Workspace{Mode: "memory"}
	ctx := testCtx(t)

	if err := m.Deepen(ctx, ws, nil); err != nil {
		t.Fatalf("Deepen: %v", err)
	}
}

func TestRelease_NilWorkspace(t *testing.T) {
	m := NewManager("", false)
	// Should not panic.
	m.Release(nil)
}

func TestRelease_InMemoryWorkspace(t *testing.T) {
	m := NewManager("", false)
	ws := &Workspace{Mode: "memory"}
	// Should not panic (manager is nil for in-memory).
	m.Release(ws)
}

func TestGC_EmptyBasePath(t *testing.T) {
	m := NewManager("", false)
	ctx := testCtx(t)
	// Should be a no-op without panic.
	m.GC(ctx, nil)
}

func TestGC_RemovesStaleDirs(t *testing.T) {
	cacheDir := t.TempDir()
	m := NewManager(cacheDir, false)
	ctx := testCtx(t)

	// Create some directories to simulate cached repos.
	staleDir := filepath.Join(cacheDir, "stale-repo")
	activeDir := filepath.Join(cacheDir, "active-repo")
	os.MkdirAll(staleDir, 0o755)
	os.MkdirAll(activeDir, 0o755)

	// Only the active dir should survive GC.
	activeRepos := map[string]bool{
		activeDir: true,
	}
	m.GC(ctx, activeRepos)

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Error("stale dir should be removed after GC")
	}
	if _, err := os.Stat(activeDir); os.IsNotExist(err) {
		t.Error("active dir should still exist after GC")
	}
}

func TestGC_IgnoresFiles(t *testing.T) {
	cacheDir := t.TempDir()
	m := NewManager(cacheDir, false)
	ctx := testCtx(t)

	// Create a regular file (not a directory).
	os.WriteFile(filepath.Join(cacheDir, "some-file"), []byte("data"), 0o644)

	// GC should not panic or remove files.
	m.GC(ctx, map[string]bool{})

	if _, err := os.Stat(filepath.Join(cacheDir, "some-file")); os.IsNotExist(err) {
		t.Error("regular file should not be removed by GC")
	}
}

func TestRepoPath_Deterministic(t *testing.T) {
	m := NewManager("/data/repos", false)
	url := "https://github.com/example/repo.git"

	path1 := m.repoPath(url)
	path2 := m.repoPath(url)

	if path1 != path2 {
		t.Errorf("repoPath not deterministic: %q != %q", path1, path2)
	}

	// Different URL should give different path.
	path3 := m.repoPath("https://github.com/other/repo.git")
	if path1 == path3 {
		t.Errorf("different URLs gave same path: %q", path1)
	}
}

func TestCachePath_EmptyBasePath(t *testing.T) {
	m := NewManager("", false)
	if p := m.CachePath("https://example.com/repo.git"); p != "" {
		t.Errorf("CachePath = %q, want empty for no basePath", p)
	}
}

func TestCachePath_WithBasePath(t *testing.T) {
	m := NewManager("/data/repos", false)
	p := m.CachePath("https://example.com/repo.git")
	if p == "" {
		t.Error("CachePath should be non-empty")
	}
	if !filepath.IsAbs(p) {
		t.Errorf("CachePath should be absolute, got %q", p)
	}
}

func TestBasePath(t *testing.T) {
	m := NewManager("/data/repos", false)
	if m.BasePath() != "/data/repos" {
		t.Errorf("BasePath = %q, want /data/repos", m.BasePath())
	}
}

func TestContextInjection(t *testing.T) {
	m := NewManager("/test", true)
	ctx := WithManager(context.Background(), m)
	got := GetManager(ctx)
	if got != m {
		t.Error("GetManager should return the injected manager")
	}
}

func TestContextInjection_Default(t *testing.T) {
	// Without injection, should return a default in-memory manager.
	got := GetManager(context.Background())
	if got == nil {
		t.Fatal("GetManager should return non-nil default")
	}
	if got.basePath != "" {
		t.Errorf("default manager basePath = %q, want empty", got.basePath)
	}
}

func TestAcquire_InMemory_ValidLocalRepo(t *testing.T) {
	// Test that in-memory acquire works with a valid local repo.
	sourceDir := t.TempDir()
	barePath, _ := initBareRepo(t, sourceDir)

	m := NewManager("", false)
	ctx := testCtx(t)

	ws, err := m.Acquire(ctx, barePath, nil, false)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if ws.Mode != "memory" {
		t.Errorf("Mode = %q, want memory", ws.Mode)
	}
	if ws.Repo == nil {
		t.Fatal("Repo is nil")
	}

	head, err := ws.Repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.Hash().IsZero() {
		t.Error("HEAD hash is zero")
	}

	m.Release(ws)
}

// Test that the same repo can be acquired and released multiple times.
func TestAcquire_MultipleAcquireRelease(t *testing.T) {
	sourceDir := t.TempDir()
	barePath, _ := initBareRepo(t, sourceDir)

	cacheDir := t.TempDir()
	m := NewManager(cacheDir, false)
	ctx := testCtx(t)

	for i := 0; i < 3; i++ {
		ws, err := m.Acquire(ctx, barePath, nil, true)
		if err != nil {
			t.Fatalf("Acquire #%d: %v", i, err)
		}
		if ws.Mode != "disk" {
			t.Errorf("Acquire #%d Mode = %q, want disk", i, ws.Mode)
		}
		m.Release(ws)
	}
}

// Ensure memory.NewStorage-backed workspace works correctly (for buildMergedTree etc.)
func TestInMemoryStorerCompatibility(t *testing.T) {
	storer := memory.NewStorage()
	repo, err := git.Init(storer, nil)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if repo == nil {
		t.Fatal("repo is nil")
	}
}
