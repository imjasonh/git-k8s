package main

import (
	"knative.dev/pkg/injection/sharedmain"

	"github.com/imjasonh/git-k8s/pkg/reconciler/resolver"
)

func main() {
	sharedmain.Main("resolver-controller", resolver.NewController)
}
