package main

import (
	"knative.dev/pkg/injection/sharedmain"

	"github.com/imjasonh/git-k8s/pkg/reconciler/push"
)

func main() {
	sharedmain.Main("push-controller", push.NewController)
}
