# CLAUDE.md — git-k8s

## Project Overview

git-k8s is a Kubernetes-native controller system for managing Git repositories and automated Git operations. It provides four Custom Resource Definitions (CRDs) and three controllers that enable declarative Git workflows within a Kubernetes cluster.

**Module:** `github.com/imjasonh/git-k8s`
**Language:** Go 1.24.7
**License:** Apache-2.0

## Architecture

### Four-Controller Design

1. **Push Controller** (`cmd/push-controller/`) — Watches `GitPushTransaction` resources, clones repos into memory using go-git, executes atomic pushes, and updates transaction status through phases (Pending → InProgress → Succeeded/Failed).

2. **Sync Controller** (`cmd/sync-controller/`) — Watches `GitRepoSync` and `GitBranch` resources. Compares HEAD commits between two repos, calculates merge bases, and creates push transactions to keep branches synchronized. Marks as Conflicted when both sides have diverged.

3. **Resolver Controller** (`cmd/resolver-controller/`) — Watches `GitRepoSync` resources in Conflicted phase. Performs automated 3-way merge with file-level conflict detection. Creates merge commits and push transactions to both repos. Falls back to `RequiresManualIntervention` on failure.

4. **Repo Watcher Controller** (`cmd/repo-watcher-controller/`) — Watches `GitRepository` resources and polls their remotes via `git ls-remote` on a configurable interval. Auto-creates, updates, and deletes `GitBranch` CRDs to reflect the actual state of the remote. Enables fast detection of external pushes to trigger downstream sync.

### Custom Resources (CRDs)

| CRD | Purpose |
|-----|---------|
| `GitRepository` | Defines a managed Git repo (clone URL, default branch, auth, poll interval) |
| `GitBranch` | Tracks a branch within a repo (head commit, last updated) |
| `GitPushTransaction` | Atomic push operation with refspecs and CAS support |
| `GitRepoSync` | Two-way sync relationship between two repositories |

CRD manifests live in `config/crds/`.

### Key Packages

| Package | Purpose |
|---------|---------|
| `pkg/apis/git/v1alpha1` | API type definitions, scheme registration, defaults |
| `pkg/client` | Hand-written typed client wrapper over Kubernetes dynamic client |
| `pkg/reconciler/internal` | Generic reconciler adapter (`KindReconciler[T]` interface) |
| `pkg/reconciler/push` | Push transaction reconciliation logic |
| `pkg/reconciler/sync` | Repo sync reconciliation logic |
| `pkg/reconciler/resolver` | Conflict resolution via 3-way merge |
| `pkg/reconciler/repowatcher` | Remote polling and GitBranch CRD lifecycle |

## Directory Structure

```
cmd/                          # Controller entry points
  push-controller/
  sync-controller/
  resolver-controller/
  repo-watcher-controller/
pkg/
  apis/git/v1alpha1/          # CRD types, scheme, defaults, deepcopy
  client/                     # Typed client wrapper (manual, not generated)
  reconciler/
    internal/                 # Shared reconciler adapter pattern
    push/                     # Push transaction controller + reconciler
    sync/                     # Sync controller + reconciler
    resolver/                 # Conflict resolver controller + reconciler
    repowatcher/              # Remote polling + GitBranch lifecycle
config/
  crds/                       # CRD YAML manifests
  rbac/                       # RBAC role definitions
  deployments/                # Controller deployment manifests
  core/                       # Supporting infrastructure (Gitea)
test/e2e/                     # End-to-end tests (build tag: e2e)
hack/                         # Code generation scripts
```

## Build & Development

### Prerequisites

- Go 1.24.7+
- `ko` (for container image builds)
- A Kubernetes cluster (KinD for local development)
- `kubectl`

### Common Commands

```bash
# Build all controller binaries
go build ./cmd/push-controller/
go build ./cmd/sync-controller/
go build ./cmd/resolver-controller/
go build ./cmd/repo-watcher-controller/

# Run unit tests
go test ./...

# Run linting
go vet ./...

# Verify module tidiness
go mod tidy

# Build and deploy to a cluster with ko
ko apply -f config/deployments/

# Install CRDs
kubectl apply -f config/crds/
```

### E2E Tests

E2E tests require a running Kubernetes cluster with Gitea deployed. They are gated behind a build tag:

```bash
GITEA_URL=http://localhost:3000 \
GITEA_INTERNAL_URL=http://gitea.git-system.svc.cluster.local:3000 \
  go test -v -count=1 -timeout=10m -tags=e2e ./test/e2e/
```

Environment variables:
- `GITEA_URL` — External Gitea URL accessible from the test runner
- `GITEA_INTERNAL_URL` — In-cluster Gitea URL used by controllers

## Key Dependencies

- `github.com/go-git/go-git/v5` — All Git operations (clone, push, tree diffing, merge base)
- `k8s.io/client-go` — Kubernetes client (dynamic client as foundation)
- `k8s.io/apimachinery` — API types, serialization, scheme
- `knative.dev/pkg` — Controller lifecycle, injection, logging, leader election

## Conventions & Patterns

### Reconciler Pattern
Each controller follows the Knative `KindReconciler[T]` pattern: the generic adapter in `pkg/reconciler/internal` handles key splitting, resource fetching, and not-found handling. Developers implement only the `ReconcileKind(ctx, *T)` method with business logic.

### Typed Client
The typed client in `pkg/client/` is hand-written over the Kubernetes dynamic client (not code-generated). It uses context-based injection (`client.WithClient`, `client.Get`). The `zz_generated.deepcopy.go` file is also hand-written. Both may be replaced with full code generation in the future.

### Controller Separation
Each controller is a separate binary/deployment. Controllers communicate through Kubernetes resources — for example, the sync controller creates `GitPushTransaction` resources that the push controller then processes.

### In-Memory Git Operations
All Git operations use `go-git` with `memory.NewStorage()`, keeping controllers stateless with no persistent volume requirements.

### Status & Phase Management
Resources use phase-based state machines:
- **GitPushTransaction:** `Pending` → `InProgress` → `Succeeded` / `Failed`
- **GitRepoSync:** `InSync` / `Syncing` / `Conflicted` / `RequiresManualIntervention`

### Labels & Ownership
Resources use labels (e.g., `git-k8s.imjasonh.com/repo-sync`) and owner references for resource relationships. No finalizers are used.

### API Group
All CRDs are in the `git-k8s.imjasonh.com` API group, version `v1alpha1`.

## CI/CD

GitHub Actions (`.github/workflows/ci.yaml`) runs two jobs:

1. **Build** — `go mod tidy` check, build all binaries, `go test ./...`, `go vet ./...`
2. **E2E** — Sets up KinD cluster, installs CRDs, deploys controllers via `ko`, deploys Gitea, runs e2e tests with `-tags=e2e`

Container images use `ko` with a `gcr.io/distroless/static:nonroot` base image (configured in `.ko.yaml`).

## Go Module Download Workaround

In some CI/cloud environments, `go mod download` (and `go build`, `go test`, etc.) may fail with DNS resolution errors for `storage.googleapis.com`. This happens when the `no_proxy` / `NO_PROXY` environment variable includes `*.googleapis.com`, causing Go's HTTP client to bypass the egress proxy and attempt direct DNS resolution, which fails.

**Fix:** Strip `*.googleapis.com` from `no_proxy` so that requests to `storage.googleapis.com` (where the Go module proxy stores zip files) route through the egress proxy:

```bash
NO_PROXY=$(echo "$no_proxy" | sed 's/,\*\.googleapis\.com//; s/\*\.googleapis\.com,//; s/\*\.googleapis\.com//') \
no_proxy=$(echo "$no_proxy" | sed 's/,\*\.googleapis\.com//; s/\*\.googleapis\.com,//; s/\*\.googleapis\.com//') \
  go mod download
```

Apply the same prefix to `go build`, `go test`, `go vet`, etc. when downloading new modules.
