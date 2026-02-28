package resolver

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

var repoSyncGVR = schema.GroupVersionResource{
	Group:    gitv1alpha1.GroupName,
	Version:  gitv1alpha1.Version,
	Resource: "gitreposyncs",
}

// NewController creates a new controller for resolving conflicted GitRepoSyncs.
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

	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	impl := controller.NewContext(ctx, internal.NewReconciler(
		func(ctx context.Context, namespace, name string) (*gitv1alpha1.GitRepoSync, error) {
			return gitClient.GitRepoSyncs(namespace).Get(ctx, name, metav1.GetOptions{})
		},
		r,
	), controller.ControllerOptions{
		WorkQueueName: "GitConflictResolver",
		Logger:        logger,
	})

	logger.Info("Setting up event handlers for GitConflictResolver")

	// Only watch GitRepoSync resources that are in the Conflicted phase.
	syncInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			u, ok := obj.(*unstructured.Unstructured)
			if !ok {
				return false
			}
			phase, found, err := unstructured.NestedString(u.Object, "status", "phase")
			if err != nil || !found {
				return false
			}
			return phase == string(gitv1alpha1.SyncPhaseConflicted)
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
