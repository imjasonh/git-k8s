package push

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

func TestBase64Decoding(t *testing.T) {
	tests := []struct {
		name     string
		encoded  string
		wantText string
	}{
		{
			name:     "simple username",
			encoded:  base64.StdEncoding.EncodeToString([]byte("admin")),
			wantText: "admin",
		},
		{
			name:     "password with special chars",
			encoded:  base64.StdEncoding.EncodeToString([]byte("p@ss!w0rd#123")),
			wantText: "p@ss!w0rd#123",
		},
		{
			name:     "empty string",
			encoded:  base64.StdEncoding.EncodeToString([]byte("")),
			wantText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded, err := base64.StdEncoding.DecodeString(tt.encoded)
			if err != nil {
				t.Fatalf("DecodeString() error = %v", err)
			}
			if string(decoded) != tt.wantText {
				t.Errorf("decoded = %q, want %q", string(decoded), tt.wantText)
			}
		})
	}
}

func gvr(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: gitv1alpha1.GroupName, Version: gitv1alpha1.Version, Resource: resource}
}

func toUnstructuredObj(t *testing.T, obj interface{}) *unstructured.Unstructured {
	t.Helper()
	data, err := json.Marshal(obj)
	if err != nil {
		t.Fatal(err)
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, u); err != nil {
		t.Fatal(err)
	}
	return u
}

// seedObject creates an object in the fake dynamic client using the correct GVR.
func seedObject(t *testing.T, dynClient *dynamicfake.FakeDynamicClient, gvr schema.GroupVersionResource, obj interface{}) {
	t.Helper()
	u := toUnstructuredObj(t, obj)
	ns := u.GetNamespace()
	_, err := dynClient.Resource(gvr).Namespace(ns).Create(context.Background(), u, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("seeding object: %v", err)
	}
}

func newFakeReconciler(t *testing.T) (*Reconciler, *dynamicfake.FakeDynamicClient) {
	t.Helper()
	scheme := runtime.NewScheme()
	dynClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			gvr("gitbranches"):         "GitBranchList",
			gvr("gitrepositories"):     "GitRepositoryList",
			gvr("gitpushtransactions"): "GitPushTransactionList",
		})
	gitClient := gitclient.NewFromDynamic(dynClient)
	return &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}, dynClient
}

func TestFailTransaction(t *testing.T) {
	txn := &gitv1alpha1.GitPushTransaction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitPushTransaction",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "push-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
		},
		Status: gitv1alpha1.GitPushTransactionStatus{
			Phase: gitv1alpha1.TransactionPhaseInProgress,
		},
	}

	r, dynClient := newFakeReconciler(t)
	seedObject(t, dynClient, gvr("gitpushtransactions"), txn)

	err := r.failTransaction(context.Background(), txn, "push failed: connection refused")
	if err != nil {
		t.Fatalf("failTransaction() error = %v", err)
	}
	if txn.Status.Phase != gitv1alpha1.TransactionPhaseFailed {
		t.Errorf("phase = %q, want %q", txn.Status.Phase, gitv1alpha1.TransactionPhaseFailed)
	}
	if txn.Status.Message != "push failed: connection refused" {
		t.Errorf("message = %q, want %q", txn.Status.Message, "push failed: connection refused")
	}
	if txn.Status.CompletionTime == nil {
		t.Error("CompletionTime should be set on failure")
	}
}

func TestUpdateBranches(t *testing.T) {
	branch := &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo-main",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "my-repo",
			BranchName:    "main",
		},
		Status: gitv1alpha1.GitBranchStatus{
			HeadCommit: "old-commit",
		},
	}

	txn := &gitv1alpha1.GitPushTransaction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitPushTransaction",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "push-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/main",
				},
			},
		},
		Status: gitv1alpha1.GitPushTransactionStatus{
			Phase:        gitv1alpha1.TransactionPhaseSucceeded,
			ResultCommit: "new-commit-sha",
		},
	}

	r, dynClient := newFakeReconciler(t)
	seedObject(t, dynClient, gvr("gitbranches"), branch)
	seedObject(t, dynClient, gvr("gitpushtransactions"), txn)

	err := r.updateBranches(context.Background(), "default", txn)
	if err != nil {
		t.Fatalf("updateBranches() error = %v", err)
	}

	// Verify the branch was updated with the new commit.
	updated, err := r.gitClient.GitBranches("default").Get(context.Background(), "my-repo-main", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting updated branch: %v", err)
	}
	if updated.Status.HeadCommit != "new-commit-sha" {
		t.Errorf("HeadCommit = %q, want %q", updated.Status.HeadCommit, "new-commit-sha")
	}
	if updated.Status.LastUpdated == nil {
		t.Error("LastUpdated should be set after branch update")
	}
}

func TestUpdateBranches_NoMatchingBranch(t *testing.T) {
	branch := &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other-repo-main",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "other-repo",
			BranchName:    "main",
		},
	}

	txn := &gitv1alpha1.GitPushTransaction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitPushTransaction",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "push-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/main",
				},
			},
		},
		Status: gitv1alpha1.GitPushTransactionStatus{
			ResultCommit: "new-commit-sha",
		},
	}

	r, dynClient := newFakeReconciler(t)
	seedObject(t, dynClient, gvr("gitbranches"), branch)
	seedObject(t, dynClient, gvr("gitpushtransactions"), txn)

	err := r.updateBranches(context.Background(), "default", txn)
	if err != nil {
		t.Fatalf("updateBranches() should not error when no branches match, got: %v", err)
	}
}

func TestResolveAuth_NoAuth(t *testing.T) {
	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
		},
	}

	r, _ := newFakeReconciler(t)
	auth, err := r.resolveAuth(context.Background(), "default", repo)
	if err != nil {
		t.Fatalf("resolveAuth() error = %v", err)
	}
	if auth != nil {
		t.Error("auth should be nil when no auth is configured")
	}
}

func TestResolveAuth_NilSecretRef(t *testing.T) {
	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:  "https://github.com/example/repo.git",
			Auth: &gitv1alpha1.GitAuth{},
		},
	}

	r, _ := newFakeReconciler(t)
	auth, err := r.resolveAuth(context.Background(), "default", repo)
	if err != nil {
		t.Fatalf("resolveAuth() error = %v", err)
	}
	if auth != nil {
		t.Error("auth should be nil when SecretRef is nil")
	}
}

func TestReconcileKind_SkipsTerminal(t *testing.T) {
	tests := []struct {
		name  string
		phase gitv1alpha1.TransactionPhase
	}{
		{"succeeded", gitv1alpha1.TransactionPhaseSucceeded},
		{"failed", gitv1alpha1.TransactionPhaseFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txn := &gitv1alpha1.GitPushTransaction{
				TypeMeta: metav1.TypeMeta{
					APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
					Kind:       "GitPushTransaction",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "push-1",
					Namespace: "default",
				},
				Status: gitv1alpha1.GitPushTransactionStatus{
					Phase: tt.phase,
				},
			}

			r, dynClient := newFakeReconciler(t)
			seedObject(t, dynClient, gvr("gitpushtransactions"), txn)

			err := r.ReconcileKind(context.Background(), txn)
			if err != nil {
				t.Fatalf("ReconcileKind() error = %v", err)
			}
			if txn.Status.Phase != tt.phase {
				t.Errorf("phase changed to %q, should remain %q", txn.Status.Phase, tt.phase)
			}
		})
	}
}
