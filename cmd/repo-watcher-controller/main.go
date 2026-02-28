package main

import (
	"knative.dev/pkg/injection/sharedmain"

	"github.com/imjasonh/git-k8s/pkg/reconciler/repowatcher"
)

func main() {
	sharedmain.Main("repo-watcher-controller", repowatcher.NewController)
}
