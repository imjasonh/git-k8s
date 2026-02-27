package push

import (
	"context"
	"time"

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
)

var pushTransactionGVR = schema.GroupVersionResource{
	Group:    gitv1alpha1.GroupName,
	Version:  gitv1alpha1.Version,
	Resource: "gitpushtransactions",
}

// NewController creates a new controller for GitPushTransaction resources.
func NewController(ctx context.Context, cmw configmap.Watcher) *controller.Impl {
	logger := logging.FromContext(ctx)

	// Get dynamic client from the injected config.
	cfg := injection.GetConfig(ctx)
	dynClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		logger.Fatalf("Error building dynamic client: %v", err)
	}

	gitClient := gitclient.NewFromDynamic(dynClient)
	ctx = gitclient.WithClient(ctx, gitClient)

	// Create dynamic informer for GitPushTransaction.
	factory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 30*time.Second)
	transactionInformer := factory.ForResource(pushTransactionGVR)

	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	impl := controller.NewContext(ctx, r, controller.ControllerOptions{
		WorkQueueName: "GitPushTransactions",
		Logger:        logger,
	})

	logger.Info("Setting up event handlers for GitPushTransaction")

	transactionInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			impl.Enqueue(obj)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			impl.Enqueue(newObj)
		},
	})

	// Start the informer factory.
	factory.Start(ctx.Done())
	factory.WaitForCacheSync(ctx.Done())

	return impl
}

