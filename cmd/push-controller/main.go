package main

import (
	"os"

	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"

	"github.com/imjasonh/git-k8s/pkg/health"
	_ "github.com/imjasonh/git-k8s/pkg/metrics" // register Prometheus metrics
	"github.com/imjasonh/git-k8s/pkg/reconciler/push"
	"github.com/imjasonh/git-k8s/pkg/workspace"
)

func main() {
	ctx := signals.NewContext()
	go health.ServeMetrics(ctx, ":9090") //nolint:errcheck

	// GIT_CACHE_DIR enables PVC-backed workspace caching when set.
	// Push controller uses shallow clones since it only needs to push refs.
	cacheDir := os.Getenv("GIT_CACHE_DIR")
	wsMgr := workspace.NewManager(cacheDir, true /* shallow */)
	ctx = workspace.WithManager(ctx, wsMgr)

	sharedmain.MainWithContext(ctx, "push-controller", push.NewController)
}
