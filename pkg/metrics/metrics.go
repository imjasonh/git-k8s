package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ReconcileCount tracks the total number of reconciliations by controller and result.
	ReconcileCount = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gitkube",
		Name:      "reconcile_count_total",
		Help:      "Total number of reconciliations.",
	}, []string{"controller", "result"})

	// ReconcileLatency tracks reconciliation duration by controller.
	ReconcileLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gitkube",
		Name:      "reconcile_duration_seconds",
		Help:      "Duration of reconciliations in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.01, 3, 10), // 10ms to ~3min
	}, []string{"controller"})

	// GitOperationDuration tracks the duration of Git operations.
	GitOperationDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gitkube",
		Name:      "git_operation_duration_seconds",
		Help:      "Duration of Git operations (clone, push, ls-remote).",
		Buckets:   prometheus.ExponentialBuckets(0.1, 3, 8), // 100ms to ~3.6min
	}, []string{"operation"})

	// WorkspaceAcquireDuration tracks how long it takes to acquire a workspace.
	WorkspaceAcquireDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gitkube",
		Name:      "workspace_acquire_duration_seconds",
		Help:      "Duration of workspace acquire operations.",
		Buckets:   prometheus.ExponentialBuckets(0.1, 3, 8),
	}, []string{"mode"})

	// WorkspaceCacheHit tracks cache hits in workspace acquisition.
	WorkspaceCacheHit = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "gitkube",
		Name:      "workspace_cache_hit_total",
		Help:      "Total number of workspace cache hits.",
	})

	// WorkspaceCacheMiss tracks cache misses in workspace acquisition.
	WorkspaceCacheMiss = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "gitkube",
		Name:      "workspace_cache_miss_total",
		Help:      "Total number of workspace cache misses.",
	})
)
