package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestMetricsRegistered(t *testing.T) {
	// Record some data so counters/histograms appear in Gather output.
	ReconcileCount.WithLabelValues("test", "success").Inc()
	ReconcileLatency.WithLabelValues("test").Observe(0.1)
	GitOperationDuration.WithLabelValues("clone").Observe(0.5)

	metrics, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	found := make(map[string]bool)
	for _, mf := range metrics {
		found[mf.GetName()] = true
	}

	for _, name := range []string{
		"gitkube_reconcile_count_total",
		"gitkube_reconcile_duration_seconds",
		"gitkube_git_operation_duration_seconds",
	} {
		if !found[name] {
			t.Errorf("metric %q not found in registry", name)
		}
	}
}

func TestMetricsIncrement(t *testing.T) {
	// Verify we can record metrics without panicking.
	ReconcileCount.WithLabelValues("test-controller", "success").Inc()
	ReconcileCount.WithLabelValues("test-controller", "error").Inc()
	ReconcileLatency.WithLabelValues("test-controller").Observe(0.5)
	GitOperationDuration.WithLabelValues("clone").Observe(1.2)
	GitOperationDuration.WithLabelValues("push").Observe(0.3)
	GitOperationDuration.WithLabelValues("ls-remote").Observe(0.1)
}
