package main

import (
	"os"

	"knative.dev/pkg/injection/sharedmain"
	"knative.dev/pkg/signals"

	"github.com/imjasonh/git-k8s/pkg/health"
	_ "github.com/imjasonh/git-k8s/pkg/metrics" // register Prometheus metrics
	"github.com/imjasonh/git-k8s/pkg/reconciler/sync"
	"github.com/imjasonh/git-k8s/pkg/workspace"
)

func main() {
	ctx := signals.NewContext()
	go health.ServeMetrics(ctx, ":9090") //nolint:errcheck

	// GIT_CACHE_DIR enables PVC-backed workspace caching when set.
	// Sync controller uses shallow clones initially but deepens as needed
	// for merge-base calculations.
	cacheDir := os.Getenv("GIT_CACHE_DIR")
	wsMgr := workspace.NewManager(cacheDir, true /* shallow */)
	ctx = workspace.WithManager(ctx, wsMgr)

	sharedmain.MainWithContext(ctx, "sync-controller", sync.NewController)
}
