package internal

import (
	"context"
	"errors"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"knative.dev/pkg/reconciler"
)

// testResource is a simple type for testing the generic reconciler.
type testResource struct {
	Name      string
	Namespace string
}

// fakeKindReconciler records calls to ReconcileKind.
type fakeKindReconciler struct {
	called   bool
	received *testResource
	err      error
}

func (f *fakeKindReconciler) ReconcileKind(ctx context.Context, o *testResource) reconciler.Event {
	f.called = true
	f.received = o
	return f.err
}

func TestNewReconciler(t *testing.T) {
	inner := &fakeKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return &testResource{Name: name, Namespace: ns}, nil
	}
	r := NewReconciler(get, inner)
	if r == nil {
		t.Fatal("NewReconciler returned nil")
	}
	if r.inner != inner {
		t.Error("inner reconciler not stored correctly")
	}
}

func TestReconcile_Success(t *testing.T) {
	resource := &testResource{Name: "my-resource", Namespace: "default"}
	inner := &fakeKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		if ns != "default" || name != "my-resource" {
			t.Errorf("get called with ns=%q name=%q, want default/my-resource", ns, name)
		}
		return resource, nil
	}

	r := NewReconciler(get, inner)
	err := r.Reconcile(context.Background(), "default/my-resource")
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !inner.called {
		t.Error("ReconcileKind was not called")
	}
	if inner.received != resource {
		t.Error("ReconcileKind received wrong resource")
	}
}

func TestReconcile_NotFound(t *testing.T) {
	inner := &fakeKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "test", Resource: "resources"}, name)
	}

	r := NewReconciler(get, inner)
	err := r.Reconcile(context.Background(), "default/deleted-resource")
	if err != nil {
		t.Fatalf("Reconcile() should return nil for NotFound, got %v", err)
	}
	if inner.called {
		t.Error("ReconcileKind should not be called for deleted resources")
	}
}

func TestReconcile_GetError(t *testing.T) {
	inner := &fakeKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return nil, errors.New("connection refused")
	}

	r := NewReconciler(get, inner)
	err := r.Reconcile(context.Background(), "default/my-resource")
	if err == nil {
		t.Fatal("Reconcile() should return error when get fails")
	}
	if !inner.called == false {
		// inner should NOT have been called
	}
	if inner.called {
		t.Error("ReconcileKind should not be called when get fails")
	}
}

func TestReconcile_ReconcileKindError(t *testing.T) {
	resource := &testResource{Name: "my-resource", Namespace: "default"}
	inner := &fakeKindReconciler{err: errors.New("reconcile failed")}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return resource, nil
	}

	r := NewReconciler(get, inner)
	err := r.Reconcile(context.Background(), "default/my-resource")
	if err == nil {
		t.Fatal("Reconcile() should propagate ReconcileKind error")
	}
	if err.Error() != "reconcile failed" {
		t.Errorf("error = %q, want %q", err.Error(), "reconcile failed")
	}
}

func TestReconcile_ClusterScopedKey(t *testing.T) {
	inner := &fakeKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		if ns != "" {
			t.Errorf("namespace = %q, want empty for cluster-scoped", ns)
		}
		if name != "cluster-resource" {
			t.Errorf("name = %q, want %q", name, "cluster-resource")
		}
		return &testResource{Name: name}, nil
	}

	r := NewReconciler(get, inner)
	err := r.Reconcile(context.Background(), "cluster-resource")
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !inner.called {
		t.Error("ReconcileKind was not called")
	}
}

func TestPromote(t *testing.T) {
	r := NewReconciler(
		func(ctx context.Context, ns, name string) (*testResource, error) { return nil, nil },
		&fakeKindReconciler{},
	)
	// Promote should be a no-op, returning nil.
	if err := r.Promote(reconciler.UniversalBucket(), nil); err != nil {
		t.Errorf("Promote() error = %v", err)
	}
}

func TestDemote(t *testing.T) {
	r := NewReconciler(
		func(ctx context.Context, ns, name string) (*testResource, error) { return nil, nil },
		&fakeKindReconciler{},
	)
	// Demote should be a no-op and not panic.
	r.Demote(reconciler.UniversalBucket())
}
