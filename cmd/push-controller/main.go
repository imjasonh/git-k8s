package main

import (
	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"

	"github.com/imjasonh/git-k8s/pkg/health"
	_ "github.com/imjasonh/git-k8s/pkg/metrics" // register Prometheus metrics
	"github.com/imjasonh/git-k8s/pkg/reconciler/push"
)

func main() {
	ctx := signals.NewContext()
	go health.ServeMetrics(ctx, ":9090") //nolint:errcheck
	sharedmain.MainWithContext(ctx, "push-controller", push.NewController)
}
