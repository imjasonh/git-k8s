package internal

import (
	"context"
	"fmt"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"knative.dev/pkg/reconciler"
)

// testResource is a simple type to use with the generic reconciler.
type testResource struct {
	Namespace string
	Name      string
}

// testKindReconciler implements KindReconciler[testResource] for testing.
type testKindReconciler struct {
	called   bool
	received *testResource
	err      error
}

func (r *testKindReconciler) ReconcileKind(_ context.Context, o *testResource) reconciler.Event {
	r.called = true
	r.received = o
	return r.err
}

func TestNewReconciler(t *testing.T) {
	inner := &testKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return &testResource{Namespace: ns, Name: name}, nil
	}
	r := NewReconciler("test", get, inner)
	if r == nil {
		t.Fatal("NewReconciler returned nil")
	}
	if r.inner != inner {
		t.Error("NewReconciler did not store the inner reconciler")
	}
}

func TestReconcile_Success(t *testing.T) {
	resource := &testResource{Namespace: "default", Name: "my-obj"}
	inner := &testKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		if ns == "default" && name == "my-obj" {
			return resource, nil
		}
		return nil, fmt.Errorf("unexpected get(%q, %q)", ns, name)
	}

	r := NewReconciler("test", get, inner)
	err := r.Reconcile(context.Background(), "default/my-obj")
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
	inner := &testKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return nil, apierrors.NewNotFound(schema.GroupResource{Group: "test", Resource: "resources"}, name)
	}

	r := NewReconciler("test", get, inner)
	err := r.Reconcile(context.Background(), "default/deleted-obj")
	if err != nil {
		t.Fatalf("Reconcile() should return nil for not-found, got %v", err)
	}
	if inner.called {
		t.Error("ReconcileKind should not be called for deleted resources")
	}
}

func TestReconcile_GetError(t *testing.T) {
	inner := &testKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return nil, fmt.Errorf("connection refused")
	}

	r := NewReconciler("test", get, inner)
	err := r.Reconcile(context.Background(), "default/my-obj")
	if err == nil {
		t.Fatal("Reconcile() should return error on get failure")
	}
	if inner.called {
		t.Error("ReconcileKind should not be called when get fails")
	}
}

func TestReconcile_ReconcileKindError(t *testing.T) {
	inner := &testKindReconciler{err: fmt.Errorf("reconcile failed")}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return &testResource{Namespace: ns, Name: name}, nil
	}

	r := NewReconciler("test", get, inner)
	err := r.Reconcile(context.Background(), "default/my-obj")
	if err == nil {
		t.Fatal("Reconcile() should propagate ReconcileKind error")
	}
}

func TestReconcile_ClusterScopedKey(t *testing.T) {
	inner := &testKindReconciler{}
	get := func(ctx context.Context, ns, name string) (*testResource, error) {
		return &testResource{Namespace: ns, Name: name}, nil
	}

	r := NewReconciler("test", get, inner)
	err := r.Reconcile(context.Background(), "cluster-resource")
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !inner.called {
		t.Error("ReconcileKind was not called for cluster-scoped key")
	}
	if inner.received.Namespace != "" {
		t.Errorf("expected empty namespace for cluster-scoped key, got %q", inner.received.Namespace)
	}
	if inner.received.Name != "cluster-resource" {
		t.Errorf("expected name 'cluster-resource', got %q", inner.received.Name)
	}
}

func TestPromote(t *testing.T) {
	r := NewReconciler("test",
		func(ctx context.Context, ns, name string) (*testResource, error) { return nil, nil },
		&testKindReconciler{},
	)
	// Promote should be a no-op and return nil.
	err := r.Promote(reconciler.UniversalBucket(), func(b reconciler.Bucket, nn types.NamespacedName) {})
	if err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
}

func TestDemote(t *testing.T) {
	r := NewReconciler("test",
		func(ctx context.Context, ns, name string) (*testResource, error) { return nil, nil },
		&testKindReconciler{},
	)
	// Demote should be a no-op (just ensure no panic).
	r.Demote(reconciler.UniversalBucket())
}
