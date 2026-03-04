# Differential Coverage Report — git-k8s

Generated using the [diffcover technique](https://research.swtch.com/diffcover):
collect per-test coverage profiles, then compare covered vs. uncovered lines
to map code snippets to features and identify under-tested components.

## Methodology

1. Ran `go test -coverprofile=coverage.out -covermode=count ./...`
2. Analyzed per-function coverage with `go tool cover -func=coverage.out`
3. Mapped covered lines to the tests that exercise them
4. Identified uncovered business-logic code that represents untested features

## Baseline Coverage (Before)

| Package | Coverage | Verdict |
|---------|----------|---------|
| `pkg/apis/git/v1alpha1` | 27.6% | Only covers helpers (DeepCopy, scheme, constants) |
| `pkg/client` | 5.6% | Only covers `toUnstructured`/`fromUnstructured`/context injection |
| `pkg/reconciler/internal` | 26.7% | Only covers `splitKey`; `Reconcile`, `NewReconciler`, `Promote`, `Demote` untested |
| `pkg/reconciler/push` | 0.0% | Only tests `base64.StdEncoding` (stdlib), no reconciler logic tested |
| `pkg/reconciler/sync` | 0.0% | Empty test file — no tests at all |
| `pkg/reconciler/resolver` | 2.1% | Only tests `changeName` helper |
| `pkg/reconciler/repowatcher` | 10.1% | Tests `branchCRDName`, `pollInterval`, `isOwnedBy`, `minLen` — no ReconcileKind |
| **Total** | **10.3%** | |

## Code-to-Feature Mapping

### Feature 1: Push Transaction Lifecycle (pkg/reconciler/push)

| Code Snippet | Lines | Feature | Tested? |
|---|---|---|---|
| `ReconcileKind` | push.go:31-86 | Phase machine: Pending→InProgress→Succeeded/Failed, status updates | **NO** |
| `executePush` | push.go:89-136 | In-memory clone + push via go-git, refspec building, atomic flag | **NO** |
| `resolveAuth` | push.go:139-172 | Secret lookup via dynamic client, base64 decode | **NO** |
| `failTransaction` | push.go:175-187 | Mark transaction as Failed with message | **NO** |
| `updateBranches` | push.go:190-212 | Update GitBranch CRDs after successful push | **NO** |

### Feature 2: Two-Way Repo Sync (pkg/reconciler/sync)

| Code Snippet | Lines | Feature | Tested? |
|---|---|---|---|
| `ReconcileKind` | sync.go:26-93 | Compare HEAD commits, decide A-ahead/B-ahead/diverged | **NO** |
| `findBranch` | sync.go:96-108 | Find GitBranch CRD by repo + branch name | **NO** |
| `calculateMergeBase` | sync.go:113-147 | In-memory clone + go-git merge base calculation | **NO** |
| `createPushTransaction` | sync.go:150-183 | Create GitPushTransaction with owner refs and labels | **NO** |
| `updateSyncStatus` | sync.go:186-200 | Update GitRepoSync status with phase/commits/message | **NO** |

### Feature 3: Conflict Resolution (pkg/reconciler/resolver)

| Code Snippet | Lines | Feature | Tested? |
|---|---|---|---|
| `ReconcileKind` | resolver.go:28-75 | Orchestrate 3-way merge for Conflicted syncs | **NO** |
| `attemptMerge` | resolver.go:81-193 | In-memory 3-way merge: tree diffing, conflict detection, merge commit | **NO** |
| `buildMergedTree` | resolver.go:198-243 | Construct merged tree with additions/deletions/renames from both branches | **NO** |
| `changeName` | resolver.go:246-251 | Extract file name from tree change | YES |
| `createMergePushTransactions` | resolver.go:254-328 | Create push transactions to both repos | **NO** |
| `markManualIntervention` | resolver.go:331-336 | Set RequiresManualIntervention phase | **NO** |

### Feature 4: Remote Polling & Branch Lifecycle (pkg/reconciler/repowatcher)

| Code Snippet | Lines | Feature | Tested? |
|---|---|---|---|
| `ReconcileKind` | reconciler.go:50-183 | Poll remote, create/update/delete GitBranch CRDs | **NO** |
| `pollInterval` | reconciler.go:186-191 | Per-repo or default poll interval | YES |
| `enqueueAfter` | reconciler.go:194-201 | Re-enqueue with delay for next poll | **NO** |
| `resolveAuth` | reconciler.go:204-233 | Secret lookup + base64 decode | **NO** |
| `defaultLsRemote` | reconciler.go:236-247 | Real git ls-remote via go-git | **NO** |
| `branchCRDName` | reconciler.go:250-254 | Deterministic CRD name from repo+branch | YES |
| `isOwnedBy` | reconciler.go:257-264 | Check owner reference chain | YES |
| `minLen` | reconciler.go:266-271 | Integer min helper | YES |

### Feature 5: Generic Reconciler Adapter (pkg/reconciler/internal)

| Code Snippet | Lines | Feature | Tested? |
|---|---|---|---|
| `NewReconciler` | reconcile.go:35-40 | Construct adapter wrapping KindReconciler | **NO** |
| `Reconcile` | reconcile.go:43-58 | Key split → get → not-found handling → ReconcileKind dispatch | **NO** |
| `Promote`/`Demote` | reconcile.go:61-67 | LeaderAware no-ops | **NO** |
| `splitKey` | key.go:7-16 | Parse "namespace/name" keys | YES |

### Feature 6: Typed Client CRUD (pkg/client)

| Code Snippet | Lines | Feature | Tested? |
|---|---|---|---|
| `NewForConfig`/`NewFromDynamic` | clientset.go:29-40 | Client construction | **NO** |
| `WithClient`/`Get` | clientset.go:45-57 | Context injection/retrieval | YES |
| `toUnstructured`/`fromUnstructured` | clientset.go:83-102 | JSON round-trip conversion | YES |
| `GitRepositories().Create/Get/Update/UpdateStatus/List/Delete` | clientset.go:105-200 | CRUD for GitRepository | **NO** |
| `GitBranches().Create/Get/Update/UpdateStatus/List/Delete` | clientset.go:203-298 | CRUD for GitBranch | **NO** |
| `GitPushTransactions().Create/Get/Update/UpdateStatus/List/Delete` | clientset.go:301-396 | CRUD for GitPushTransaction | **NO** |
| `GitRepoSyncs().Create/Get/Update/UpdateStatus/List/Delete` | clientset.go:399-494 | CRUD for GitRepoSync | **NO** |

## Gap Analysis

The existing tests cover only **pure helper functions** — no business logic is exercised:

- `push_test.go` — Tests stdlib `base64.StdEncoding.DecodeString` (not even project code)
- `sync_test.go` — Empty file (just `package sync`)
- `resolver_test.go` — Tests `changeName` (5-line helper)
- `reconciler_test.go` (repowatcher) — Tests `branchCRDName`, `pollInterval`, `isOwnedBy`, `minLen`
- `key_test.go` — Tests `splitKey`
- `clientset_test.go` — Tests `toUnstructured`/`fromUnstructured` round-trips
- `types_test.go` — Tests scheme registration, constants, DeepCopy
- `defaults_test.go` — Tests `SetDefaults_*` functions

**Zero lines of reconciler business logic are covered by tests.**

## Coverage After Improvements

| Package | Before | After | Change |
|---------|--------|-------|--------|
| `pkg/apis/git/v1alpha1` | 27.6% | 29.7% | +2.1pp |
| `pkg/client` | 5.6% | 71.8% | +66.2pp |
| `pkg/reconciler/internal` | 26.7% | 100.0% | +73.3pp |
| `pkg/reconciler/push` | 0.0% | 33.3% | +33.3pp |
| `pkg/reconciler/sync` | 0.0% | 33.0% | +33.0pp |
| `pkg/reconciler/resolver` | 2.1% | 17.0% | +14.9pp |
| `pkg/reconciler/repowatcher` | 10.1% | 56.3% | +46.2pp |
| **Total** | **10.3%** | **42.7%** | **+32.4pp** |

Key per-function highlights:
- `ReconcileKind` (repowatcher): 0% → 81.2%
- `ReconcileKind` (resolver): 0% → 48.0%
- `ReconcileKind` (sync): 0% → 40.6%
- `createPushTransaction` (sync): 0% → 100%
- `updateSyncStatus` (sync): 0% → 100%
- `createMergePushTransactions` (resolver): 0% → 71.4%
- `markManualIntervention` (resolver): 0% → 100%
- `failTransaction` (push): 0% → 88.9%
- `updateBranches` (push): 0% → 85.7%
- `resolveAuth` (push): 0% → 75.0%
- All client CRUD operations: 0% → 71.8%
- `internal/reconcile.go`: 26.7% → 100%

## Tests Added

### 1. `pkg/reconciler/internal/reconcile_test.go`
- Tests `NewReconciler` + `Reconcile` with a mock `KindReconciler`
- Tests not-found handling (deleted resources)
- Tests error propagation from the get function
- Tests `Promote`/`Demote` no-ops
- **Covers**: `reconcile.go` lines 35-67 (NewReconciler, Reconcile, Promote, Demote)

### 2. `pkg/reconciler/push/push_test.go` (rewritten)
- Tests `failTransaction`: verifies phase set to Failed, message set, status updated
- Tests `updateBranches`: verifies matching branches get updated head commit
- Tests `ReconcileKind` terminal-state skip (Succeeded/Failed transactions)
- **Covers**: `push.go` lines 36-38 (terminal skip), 175-212 (failTransaction, updateBranches)

### 3. `pkg/reconciler/sync/sync_test.go` (rewritten)
- Tests `findBranch`: found, not found, list error cases
- Tests `updateSyncStatus`: all phases including InSync (sets LastSyncTime)
- Tests `createPushTransaction`: labels, owner refs, refspecs
- **Covers**: `sync.go` lines 96-200 (findBranch, createPushTransaction, updateSyncStatus)

### 4. `pkg/reconciler/resolver/resolver_test.go` (extended)
- Tests `markManualIntervention`: phase and message set
- Tests `createMergePushTransactions`: both txn A and B created with correct labels/refs
- Tests `ReconcileKind` skip for non-Conflicted phase
- **Covers**: `resolver.go` lines 33-35 (non-Conflicted skip), 254-336 (createMergePushTransactions, markManualIntervention)

### 5. `pkg/reconciler/repowatcher/reconciler_test.go` (extended)
- Tests `ReconcileKind` full lifecycle: creates new branches, updates changed branches, deletes stale branches
- Tests poll interval throttling (skips when last fetch was recent)
- Tests `enqueueAfter` with nil impl (no panic)
- **Covers**: `reconciler.go` lines 50-183 (ReconcileKind), 194-201 (enqueueAfter)

### 6. `pkg/client/clientset_test.go` (extended)
- Tests `NewFromDynamic` constructor
- Tests full CRUD operations (Create/Get/Update/UpdateStatus/List/Delete) for all 4 resource types using `k8s.io/client-go/dynamic/fake`
- **Covers**: `clientset.go` lines 38-494 (NewFromDynamic + all CRUD methods)

### 7. `pkg/apis/git/v1alpha1/defaults_test.go` (extended)
- Tests `RegisterDefaults` with a real scheme
- **Covers**: `defaults.go` lines 20-28 (RegisterDefaults)
