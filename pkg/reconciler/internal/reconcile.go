package internal

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	"github.com/imjasonh/git-k8s/pkg/metrics"
)

// KindReconciler is the interface that a developer implements.
// It mirrors the Knative generated reconciler pattern: the resource
// has already been fetched and the developer only provides business logic.
type KindReconciler[T any] interface {
	ReconcileKind(ctx context.Context, o *T) reconciler.Event
}

// reconcilerImpl bridges between the controller's Reconcile(ctx, key) interface
// and the developer's ReconcileKind(ctx, *T) method. It handles splitting the
// workqueue key, fetching the typed resource, and ignoring not-found (deleted)
// resources — exactly what Knative's generated reconcilers do.
type reconcilerImpl[T any] struct {
	controllerName string
	get            func(ctx context.Context, namespace, name string) (*T, error)
	inner          KindReconciler[T]
}

// Verify interface conformance.
var _ reconciler.LeaderAware = (*reconcilerImpl[any])(nil)

// NewReconciler wraps a KindReconciler into a controller-compatible reconciler
// that implements Reconcile(ctx, key) error. The get function fetches the typed
// resource by namespace and name.
func NewReconciler[T any](
	controllerName string,
	get func(ctx context.Context, namespace, name string) (*T, error),
	inner KindReconciler[T],
) *reconcilerImpl[T] {
	return &reconcilerImpl[T]{controllerName: controllerName, get: get, inner: inner}
}

// Reconcile implements the controller.Reconciler interface.
func (r *reconcilerImpl[T]) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)
	start := time.Now()

	namespace, name := splitKey(key)

	o, err := r.get(ctx, namespace, name)
	if apierrors.IsNotFound(err) {
		logger.Debugf("%s/%s no longer exists", namespace, name)
		return nil
	}
	if err != nil {
		metrics.ReconcileCount.WithLabelValues(r.controllerName, "error").Inc()
		return fmt.Errorf("getting %s/%s: %w", namespace, name, err)
	}

	reconcileErr := r.inner.ReconcileKind(ctx, o)

	duration := time.Since(start).Seconds()
	metrics.ReconcileLatency.WithLabelValues(r.controllerName).Observe(duration)
	if reconcileErr != nil {
		metrics.ReconcileCount.WithLabelValues(r.controllerName, "error").Inc()
	} else {
		metrics.ReconcileCount.WithLabelValues(r.controllerName, "success").Inc()
	}

	return reconcileErr
}

// Promote implements reconciler.LeaderAware.
func (r *reconcilerImpl[T]) Promote(bkt reconciler.Bucket, enq func(reconciler.Bucket, types.NamespacedName)) error {
	return nil
}

// Demote implements reconciler.LeaderAware.
func (r *reconcilerImpl[T]) Demote(bkt reconciler.Bucket) {
}
