package main

import (
	"knative.dev/pkg/injection/sharedmain"

	"github.com/imjasonh/git-k8s/pkg/reconciler/sync"
)

func main() {
	sharedmain.Main("sync-controller", sync.NewController)
}
