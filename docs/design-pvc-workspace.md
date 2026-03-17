# Design: PVC-Backed Git Workspace Cache

## Problem

Every reconciliation performs a full `git clone` into `memory.NewStorage()`. For
large repositories this is expensive in both time and memory. The same repo may
be cloned dozens of times per minute across the push, sync, and resolver
controllers.

## Goals

1. Clone each repo at most once; subsequent reconciliations use `git fetch`.
2. Keep controllers restartable — a cold cache is slow but correct.
3. No worktree needed — all access is bare-repo object access.
4. Opt-in per GitRepository via a new `spec.cache` stanza.
5. Backward compatible — `memory.NewStorage()` remains the default.

## Non-Goals

- Shared cache across controllers (each deployment gets its own PVC).
- SSH key-based auth changes (orthogonal).
- Garbage collection of packfiles (deferred to `git gc` cron or future work).

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│ Controller Pod (e.g. push-controller)               │
│                                                     │
│  ┌───────────────┐     ┌──────────────────────────┐ │
│  │  Reconciler    │────▶│  pkg/workspace.Manager   │ │
│  │  ReconcileKind │     │                          │ │
│  └───────────────┘     │  Acquire(ctx, repo) ->   │ │
│                        │    *Workspace             │ │
│                        │  Release(ws)              │ │
│                        └─────────┬────────────────┘ │
│                                  │                   │
│  ┌───────────────────────────────▼──────────────┐   │
│  │  PVC: git-cache-push-controller              │   │
│  │                                              │   │
│  │  /data/repos/<namespace>/<repo-name>.git/    │   │
│  │  /data/repos/<namespace>/<repo-name>.lock    │   │
│  └──────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────┘
```

### Component: `pkg/workspace`

```go
// Manager manages on-disk bare Git repo caches backed by a PVC.
type Manager struct {
    basePath string          // e.g. "/data/repos"
    mu       sync.Mutex      // protects concurrent Acquire for same repo
    active   map[string]*ref // refcount per repo path
}

// Workspace is a handle to a cached bare repo.
type Workspace struct {
    // Repo is the go-git Repository opened from disk.
    Repo    *git.Repository
    Storer  storage.Storer
    path    string
    manager *Manager
}
```

#### `Acquire(ctx, repoURL, auth) (*Workspace, error)`

1. Derive path: `sha256(repoURL)[:12]` under `basePath`.
2. Take per-path lock (in-process `sync.Mutex` map + `flock` on `<path>.lock`
   for cross-pod safety if replicas > 1).
3. If `<path>/HEAD` exists → `git.PlainOpen` + `git fetch --all`.
4. Else → `git.PlainClone` with `--bare` (no worktree, `IsBare: true`).
5. Return `*Workspace` with the opened `*git.Repository`.

#### `Release(ws)`

1. Decrement in-process refcount.
2. Release `flock`.
3. (Repo stays on disk for next Acquire.)

#### Fallback to in-memory

If `basePath` is empty (no PVC mounted), `Acquire` returns a `Workspace`
backed by `memory.NewStorage()` with a full clone — identical to today's
behavior. This keeps the feature opt-in and backward compatible.

---

## API Changes

### GitRepository `spec.cache` (optional)

```yaml
apiVersion: git-k8s.imjasonh.com/v1alpha1
kind: GitRepository
metadata:
  name: my-large-repo
spec:
  url: https://github.com/example/large-repo.git
  cache:
    enabled: true
```

No new CRD needed. The `cache.enabled` field signals to controllers that this
repo benefits from on-disk caching. Controllers without a PVC mounted ignore it.

### GitRepository `status.cache` (new)

```yaml
status:
  cache:
    lastCloneTime: "2026-03-17T10:00:00Z"
    lastFetchTime: "2026-03-17T10:05:00Z"
    sizeBytes: 104857600
```

Gives operators visibility into cache health.

---

## Deployment Changes

Each controller deployment that performs clones gets an optional PVC:

```yaml
# config/deployments/push-controller.yaml (additions)
spec:
  template:
    spec:
      containers:
        - name: controller
          env:
            - name: GIT_CACHE_DIR
              value: /data/repos
          volumeMounts:
            - name: git-cache
              mountPath: /data/repos
      volumes:
        - name: git-cache
          persistentVolumeClaim:
            claimName: git-cache-push-controller
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: git-cache-push-controller
  namespace: git-system
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 10Gi
```

The `GIT_CACHE_DIR` env var controls whether workspace caching is active.
When unset, controllers fall back to pure in-memory clones.

### Controllers that need PVCs

| Controller | Clones? | Benefits from PVC |
|---|---|---|
| push-controller | Yes (clone + push) | **Yes** — avoids full re-clone per push |
| sync-controller | Yes (clone for merge-base) | **Yes** — merge-base calc on large repos |
| resolver-controller | Yes (clone for 3-way merge) | **Yes** — tree diffing is object-heavy |
| repo-watcher-controller | No (ls-remote only) | **No** — metadata-only, no clone |

---

## Reconciler Integration

Each reconciler replaces the inline `memory.NewStorage()` + `git.CloneContext`
pattern with a call to the workspace manager:

```go
// Before (current):
storer := memory.NewStorage()
repo, err := git.CloneContext(ctx, storer, nil, &git.CloneOptions{URL: url})

// After:
ws, err := r.workspaces.Acquire(ctx, url, auth)
if err != nil { ... }
defer r.workspaces.Release(ws)
repo := ws.Repo
```

The rest of the reconciler logic (CommitObject, Tree, Diff, Push) works
unchanged since `*git.Repository` is the same interface whether backed by
memory or filesystem storage.

### Push controller special case

The push controller needs to push after acquiring the workspace. Since the
workspace is a bare clone with all refs fetched, `PushContext` works directly.
The `Acquire` call's internal `git fetch` ensures the repo has the latest
objects before the push refspec is evaluated.

---

## Concurrency & Locking

### Single-replica (default)

In-process `sync.Mutex` per repo path. Multiple reconcile goroutines for
different repos proceed concurrently; reconciles for the same repo serialize.

### Multi-replica (HA)

If `replicas > 1` with `ReadWriteMany` PVC, use `flock(2)` on
`<path>.lock` for cross-process safety. This is optional and only needed for
HA deployments — single-replica deployments (the default) don't need it.

### Workqueue natural serialization

Knative's controller workqueue already serializes reconciles for the same key.
The lock is a safety net for the workspace layer, not the primary concurrency
control.

---

## Cache Lifecycle

### Warm-up

On first reconcile after pod start, `Acquire` does a full `git clone` to disk.
Subsequent reconciles for the same repo do `git fetch origin` (seconds, not
minutes).

### Staleness

The `Acquire` method always runs `git fetch` before returning. The caller gets
an up-to-date view. There is no TTL — freshness is guaranteed per-acquire.

### Eviction

Repos deleted from the cluster (GitRepository CR deleted) leave stale
directories on disk. A periodic cleanup goroutine in the Manager removes
directories for repos no longer present:

```go
func (m *Manager) GC(ctx context.Context, activeRepos map[string]bool)
```

Called by each controller on startup and every 10 minutes.

### Disk pressure

If the PVC fills up, `Acquire` falls back to `memory.NewStorage()` for that
reconcile and emits a warning metric. The operator can resize the PVC.

---

## Metrics

New metrics for workspace operations:

```
gitkube_workspace_acquire_duration_seconds{mode="disk|memory"}
gitkube_workspace_cache_hit_total
gitkube_workspace_cache_miss_total
gitkube_workspace_disk_bytes{controller}
```

---

## Migration Path

1. Add `pkg/workspace` with `Manager` and `Workspace` types.
2. Add `spec.cache` to `GitRepository` CRD (optional field, no migration).
3. Thread `workspace.Manager` into each reconciler via controller constructor.
4. Replace inline clone calls with `Acquire/Release`.
5. Add PVC manifests to `config/deployments/` (commented out by default).
6. Update CLAUDE.md with PVC instructions.

Each step is independently deployable. Existing deployments without PVCs
continue to work with pure in-memory clones.

---

## Open Questions

1. **Shared vs per-controller PVC**: Current design uses one PVC per controller.
   A shared PVC with `ReadWriteMany` would reduce storage but adds cross-process
   locking complexity. Start with per-controller, revisit if storage is a concern.

2. **Shallow clones**: `git clone --depth=1` would reduce initial clone time and
   disk usage, but merge-base calculation requires full history. Could use
   `--depth=1` for push controller and full clones for sync/resolver.

3. **Filesystem storage backend**: go-git's `filesystem.NewStorage()` vs bare
   `git.PlainClone`. The `PlainClone` with `IsBare: true` is the most natural
   fit since we never need a worktree.
