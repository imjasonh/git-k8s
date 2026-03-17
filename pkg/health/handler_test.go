package health

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestServeMetrics(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ServeMetrics(ctx, ":0") //nolint:errcheck

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)
	cancel()
}

func TestServeMetrics_MetricsEndpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a fixed port for testing.
	addr := ":19090"
	go ServeMetrics(ctx, addr) //nolint:errcheck

	time.Sleep(100 * time.Millisecond)

	resp, err := http.Get("http://localhost" + addr + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET /metrics status = %d, want 200", resp.StatusCode)
	}
}
