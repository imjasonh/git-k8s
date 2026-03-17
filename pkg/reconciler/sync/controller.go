package sync

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
	"knative.dev/pkg/configmap"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/logging"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
	"github.com/imjasonh/git-k8s/pkg/reconciler/internal"
)

var (
	repoSyncGVR = schema.GroupVersionResource{
		Group:    gitv1alpha1.GroupName,
		Version:  gitv1alpha1.Version,
		Resource: "gitreposyncs",
	}
	branchGVR = schema.GroupVersionResource{
		Group:    gitv1alpha1.GroupName,
		Version:  gitv1alpha1.Version,
		Resource: "gitbranches",
	}
)

// NewController creates a new controller for GitRepoSync resources.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)

	cfg := injection.GetConfig(ctx)
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building dynamic client: %v", err)
	}

	gitClient := gitclient.NewFromDynamic(dynClient)

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 30*time.Second)
	syncInformer := factory.ForResource(repoSyncGVR)
	branchInformer := factory.ForResource(branchGVR)

	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	impl := controller.NewContext(ctx, internal.NewReconciler(
		"sync",
		func(ctx context.Context, namespace, name string) (*gitv1alpha1.GitRepoSync, error) {
			return gitClient.GitRepoSyncs(namespace).Get(ctx, name, metav1.GetOptions{})
		},
		r,
	), controller.ControllerOptions{
		WorkQueueName: "GitRepoSyncs",
		Logger:        logger,
	})

	logger.Info("Setting up event handlers for GitRepoSync")

	// Watch GitRepoSync resources directly.
	syncInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			impl.Enqueue(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			impl.Enqueue(newObj)
		},
	})

	// Watch GitBranch resources and enqueue the owning GitRepoSync.
	// This ensures syncs are re-evaluated when branches change.
	branchInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			// Only process branches that have an owner reference to a GitRepoSync.
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return false
			}
			for _, ref := range u.GetOwnerReferences() {
				if ref.Kind == "GitRepoSync" {
					return true
				}
			}
			return false
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				impl.Enqueue(obj)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				impl.Enqueue(newObj)
			},
		},
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	return impl
}
