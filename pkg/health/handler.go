package health

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"knative.dev/pkg/logging"
)

// ServeMetrics starts an HTTP server that exposes Prometheus /metrics on addr
// (e.g. ":9090"). Blocks until the context is cancelled.
func ServeMetrics(ctx context.Context, addr string) error {
	logger := logging.FromContext(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background()) //nolint:errcheck
	}()

	logger.Infof("Metrics server listening on %s", addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}
