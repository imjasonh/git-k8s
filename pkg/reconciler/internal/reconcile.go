package internal

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"
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
	get   func(ctx context.Context, namespace, name string) (*T, error)
	inner KindReconciler[T]
}

// Verify interface conformance.
var _ reconciler.LeaderAware = (*reconcilerImpl[any])(nil)

// NewReconciler wraps a KindReconciler into a controller-compatible reconciler
// that implements Reconcile(ctx, key) error. The get function fetches the typed
// resource by namespace and name.
func NewReconciler[T any](
	get func(ctx context.Context, namespace, name string) (*T, error),
	inner KindReconciler[T],
) *reconcilerImpl[T] {
	return &reconcilerImpl[T]{get: get, inner: inner}
}

// Reconcile implements the controller.Reconciler interface.
func (r *reconcilerImpl[T]) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	namespace, name := splitKey(key)

	o, err := r.get(ctx, namespace, name)
	if apierrors.IsNotFound(err) {
		logger.Debugf("%s/%s no longer exists", namespace, name)
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting %s/%s: %w", namespace, name, err)
	}

	return r.inner.ReconcileKind(ctx, o)
}

// Promote implements reconciler.LeaderAware.
func (r *reconcilerImpl[T]) Promote(bkt reconciler.Bucket, enq func(reconciler.Bucket, types.NamespacedName)) error {
	return nil
}

// Demote implements reconciler.LeaderAware.
func (r *reconcilerImpl[T]) Demote(bkt reconciler.Bucket) {
}
