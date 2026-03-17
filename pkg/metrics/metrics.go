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
)
