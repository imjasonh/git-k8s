# git-k8s Codebase Assessment

## Overall Verdict: Well-Architected Beta — Not Yet Production-Hardened

The codebase has excellent architecture (generic reconciler pattern, stateless design, clean CRD state machines) and strong security fundamentals (distroless images, non-root containers, least-privilege RBAC). However, it lacks observability, health checks, and adequate unit test coverage for production use.

---

## Strengths

- **Clean reconciler pattern** — Generic `KindReconciler[T]` adapter eliminates boilerplate; each controller only implements `ReconcileKind`
- **Stateless design** — All Git ops use `go-git` with `memory.NewStorage()`, no PVCs needed, trivial horizontal scaling
- **Strong container security** — Distroless base, `readOnlyRootFilesystem: true`, `runAsNonRoot: true`, all capabilities dropped
- **Proper RBAC** — ClusterRole scoped to `git-k8s.imjasonh.com` API group, Secrets access is read-only
- **Comprehensive E2E tests** — Full KinD + Gitea setup covering push, sync, resolver, and repo watcher workflows
- **Fresh dependencies** — go-git v5.13.2, client-go v0.32.2, recent Knative build
- **Clear state machines** — `Pending→InProgress→Succeeded/Failed` for transactions, `InSync/Syncing/Conflicted/RequiresManualIntervention` for syncs

---

## Critical Gaps

### 1. No Metrics (Production Blocker)
No Prometheus `/metrics` endpoint. Zero visibility into reconciliation latency, error rates, work queue depth, or Git operation duration. Operators cannot monitor controller health.

### 2. No Health Checks (Production Blocker)
No readiness or liveness probes on any controller deployment. Kubernetes cannot detect hung controllers or restart failed ones.

### 3. Unit Test Coverage ~10% (Quality Risk)
Core reconciler logic (`push/push.go`, `sync/sync.go`, `resolver/resolver.go`) has near-zero unit test coverage. E2E tests exist but can't replace targeted unit tests for edge cases, error paths, and race conditions.

### 4. In-Memory Clones Without Bounds
Every reconciliation clones the full repository into memory. No depth limits, no timeout on Git operations, no protection against OOM from large repos. Memory limits (128-256Mi) are too low for real-world repositories.

---

## High-Priority Issues

| Issue | Impact | Location |
|-------|--------|----------|
| **No SSH key support** | Blocks enterprise adoption | `pkg/reconciler/push/push.go:139-172` |
| **No retry/backoff for Git ops** | Transient failures cause permanent `Failed` state | All reconcilers |
| **Stuck InProgress transactions** | No timeout; transaction stays InProgress forever if push succeeds but status update fails | `pkg/reconciler/push/push.go:43-49` |
| **Unbounded list operations** | `GitBranches().List()` fetches all branches across all repos, filters in-memory | `sync/sync.go:97`, `repowatcher/reconciler.go:87` |
| **Branch creation race condition** | Multiple reconcilers can attempt to create the same GitBranch simultaneously | `repowatcher/reconciler.go:106-141` |
| **No NetworkPolicy** | Controllers can reach arbitrary network endpoints | `config/` |
| **Hardcoded merge author** | `git-k8s-resolver <resolver@git-k8s.imjasonh.com>` not configurable | `resolver/resolver.go:165-166` |
| **No input validation** | Git URLs not validated (SSRF risk); branch names used directly in refs | API types |
| **HA not configured** | All deployments run 1 replica; leader election exists but untested with >1 | `config/deployments/` |

---

## Security Assessment

| Area | Status | Notes |
|------|--------|-------|
| Container image | **Pass** | Distroless, non-root, read-only FS, caps dropped |
| RBAC | **Pass** | Least privilege, status subresource separated |
| Secrets handling | **Adequate** | Not logged, loaded per-reconciliation; no SSH, no rotation, no audit trail |
| Input validation | **Missing** | No URL validation, no branch name sanitization |
| Network isolation | **Missing** | No NetworkPolicy manifests |

---

## Performance Concerns

- **Memory**: Every reconciliation does a full in-memory clone. A 500MB repo exceeds the 256Mi limit.
- **List operations**: Branch lookups are O(all branches in namespace), not filtered server-side.
- **Poll thundering herd**: No jitter on `DefaultPollInterval = 30s`. Many repos polling simultaneously can spike API server load.
- **No concurrent reconciliation limits**: Knative's workqueue helps, but nothing prevents multiple large clones from running simultaneously.

---

## What to Add Next (Prioritized)

### Tier 1 — Production Readiness

1. **Prometheus metrics** — Expose `/metrics` with reconciliation latency histograms, error counters, work queue depth, and Git operation duration. Use Knative's built-in metrics support or `prometheus/client_golang`.

2. **Health check endpoints** — Add `/healthz` (liveness) and `/readyz` (readiness) to all controllers. Check informer sync status for readiness; check goroutine health for liveness. Add probes to all deployments.

3. **Unit tests for reconciler logic** — Target 80%+ coverage on `push/push.go`, `sync/sync.go`, `resolver/resolver.go`, and `repowatcher/reconciler.go`. Mock the Git client and Kubernetes client. Cover error paths, phase transitions, and edge cases.

4. **Git operation timeouts** — Add context deadlines to all `git.CloneContext` calls. Make timeout configurable per-repo or globally. Prevents indefinite blocking on network issues or huge repos.

### Tier 2 — Reliability

5. **Exponential backoff for transient failures** — Categorize errors as transient vs. permanent. Retry transient failures (network errors, API conflicts) with backoff. Keep permanent failures terminal.

6. **SSH key authentication** — Extend `GitAuth` to support `ssh-privatekey` Secret field. Use go-git's SSH transport. Required for most enterprise Git hosting.

7. **Transaction timeout/GC** — Add a deadline to `GitPushTransaction`. If InProgress exceeds deadline, mark as Failed. Add a controller or CronJob to clean up old completed transactions.

8. **Label selectors for list operations** — Use label selectors when listing `GitBranch` resources instead of listing all and filtering in-memory. Add `git-k8s.imjasonh.com/repository` label to branches.

### Tier 3 — Operations

9. **Structured JSON logging** — Configure Knative's zap logger for JSON output. Add correlation IDs linking related reconciliations.

10. **NetworkPolicy manifests** — Restrict controller egress to the Kubernetes API server and known Git remotes.

11. **CRD validation** — Add CEL validation rules or a validating webhook. Validate Git URLs (scheme, hostname), branch names (no path traversal), and cross-field constraints.

12. **Configurable merge author** — Allow setting merge commit author/email via ConfigMap or GitRepoSync spec.

### Tier 4 — Scale

13. **Shallow clones / clone depth** — Add `Spec.CloneDepth` to reduce memory usage for large repos. Most sync operations only need recent history.

14. **HorizontalPodAutoscaler** — Add HPA manifests based on work queue depth or reconciliation latency.

15. **Poll jitter** — Add random jitter (±20%) to poll intervals to prevent thundering herd.

16. **Webhook-driven sync** — Support GitHub/Gitea webhooks as an alternative to polling, for near-instant sync triggers and reduced API load.

---

## Production Readiness Checklist

| Aspect | Status |
|--------|--------|
| RBAC | Ready |
| Container security | Ready |
| CRD design | Ready |
| E2E tests | Ready |
| CI/CD pipeline | Ready |
| Metrics | **Missing** |
| Health probes | **Missing** |
| Unit tests | **Inadequate (~10%)** |
| Git operation timeouts | **Missing** |
| Retry/backoff | **Missing** |
| SSH auth | **Missing** |
| Network isolation | **Missing** |
| Input validation | **Missing** |
| HA (multi-replica) | **Untested** |
