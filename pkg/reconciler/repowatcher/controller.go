package repowatcher

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var repoGVR = schema.GroupVersionResource{
	Group:    gitv1alpha1.GroupName,
	Version:  gitv1alpha1.Version,
	Resource: "gitrepositories",
}

// NewController creates a new controller for watching GitRepository remotes.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)

	cfg := injection.GetConfig(ctx)
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building dynamic client: %v", err)
	}

	gitClient := gitclient.NewFromDynamic(dynClient)

	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 30*time.Second)
	repoInformer := factory.ForResource(repoGVR)

	r := &Reconciler{
		dynamicClient:   dynClient,
		gitClient:       gitClient,
		defaultInterval: DefaultPollInterval,
		lsRemote:        defaultLsRemote,
	}

	impl := controller.NewContext(ctx, internal.NewReconciler(
		func(ctx context.Context, namespace, name string) (*gitv1alpha1.GitRepository, error) {
			return gitClient.GitRepositories(namespace).Get(ctx, name, metav1.GetOptions{})
		},
		r,
	), controller.ControllerOptions{
		WorkQueueName: "RepoWatcher",
		Logger:        logger,
	})

	// Give the reconciler access to the impl for re-enqueue.
	r.SetImpl(impl)

	logger.Info("Setting up event handlers for repo-watcher")

	repoInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			impl.Enqueue(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			impl.Enqueue(newObj)
		},
	})

	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	return impl
}
