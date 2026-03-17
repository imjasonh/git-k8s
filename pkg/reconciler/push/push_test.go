package push

import (
	"context"
	"encoding/base64"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
	"github.com/imjasonh/git-k8s/pkg/workspace"
)

// newFakeReconciler creates a Reconciler with a fake dynamic client
// pre-loaded with the given objects.
func newFakeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	for _, kind := range []string{
		"GitRepository", "GitBranch", "GitPushTransaction", "GitRepoSync",
	} {
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Kind: kind},
			&unstructured.Unstructured{},
		)
		scheme.AddKnownTypeWithName(
			schema.GroupVersionKind{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Kind: kind + "List"},
			&unstructured.UnstructuredList{},
		)
	}
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"},
		&unstructured.Unstructured{},
	)
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "", Version: "v1", Kind: "SecretList"},
		&unstructured.UnstructuredList{},
	)
	return scheme
}

var customListKinds = map[schema.GroupVersionResource]string{
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitrepositories"}:    "GitRepositoryList",
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitbranches"}:         "GitBranchList",
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitpushtransactions"}: "GitPushTransactionList",
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitreposyncs"}:        "GitRepoSyncList",
	{Group: "", Version: "v1", Resource: "secrets"}:                                        "SecretList",
}

func newFakeReconciler(t *testing.T, objects ...*unstructured.Unstructured) *Reconciler {
	t.Helper()
	dynClient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(newFakeScheme(), customListKinds)
	gc := gitclient.NewFromDynamic(dynClient)

	// Seed objects through the API using correct GVRs.
	for _, obj := range objects {
		gvr, ok := gvrForKind(obj.GetKind())
		if !ok {
			t.Fatalf("unknown kind %q", obj.GetKind())
		}
		ns := obj.GetNamespace()
		if _, err := dynClient.Resource(gvr).Namespace(ns).Create(context.Background(), obj, metav1.CreateOptions{}); err != nil {
			t.Fatalf("seeding %s/%s: %v", ns, obj.GetName(), err)
		}
	}

	return &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gc,
		workspaces:    workspace.NewManager("", false),
	}
}

func gvrForKind(kind string) (schema.GroupVersionResource, bool) {
	m := map[string]schema.GroupVersionResource{
		"GitRepository":      {Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitrepositories"},
		"GitBranch":          {Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitbranches"},
		"GitPushTransaction": {Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitpushtransactions"},
		"GitRepoSync":        {Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitreposyncs"},
		"Secret":             {Group: "", Version: "v1", Resource: "secrets"},
	}
	gvr, ok := m[kind]
	return gvr, ok
}

func TestBase64Decoding(t *testing.T) {
	// Verify that values typically stored in Kubernetes secrets
	// (base64-encoded via dynamic client) decode correctly.
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

func TestReconcileKind_SkipsTerminalPhase(t *testing.T) {
	// Transactions already in Succeeded or Failed phase should be skipped.
	for _, phase := range []gitv1alpha1.TransactionPhase{
		gitv1alpha1.TransactionPhaseSucceeded,
		gitv1alpha1.TransactionPhaseFailed,
	} {
		t.Run(string(phase), func(t *testing.T) {
			r := newFakeReconciler(t)
			txn := &gitv1alpha1.GitPushTransaction{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-txn",
					Namespace: "default",
				},
				Status: gitv1alpha1.GitPushTransactionStatus{
					Phase: phase,
				},
			}
			err := r.ReconcileKind(context.Background(), txn)
			if err != nil {
				t.Fatalf("ReconcileKind() error = %v, want nil for terminal phase %s", err, phase)
			}
		})
	}
}

func TestFailTransaction(t *testing.T) {
	// Create the transaction in the fake client so UpdateStatus can find it.
	txnObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitPushTransaction",
			"metadata": map[string]interface{}{
				"name":      "txn-1",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repositoryRef": "my-repo",
				"refSpecs":      []interface{}{},
			},
			"status": map[string]interface{}{
				"phase": "InProgress",
			},
		},
	}
	r := newFakeReconciler(t, txnObj)

	txn := &gitv1alpha1.GitPushTransaction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitPushTransaction",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "txn-1",
			Namespace: "default",
		},
		Status: gitv1alpha1.GitPushTransactionStatus{
			Phase: gitv1alpha1.TransactionPhaseInProgress,
		},
	}

	err := r.failTransaction(context.Background(), txn, "push rejected")
	if err != nil {
		t.Fatalf("failTransaction() error = %v", err)
	}

	// Verify the transaction status was updated.
	if txn.Status.Phase != gitv1alpha1.TransactionPhaseFailed {
		t.Errorf("phase = %q, want %q", txn.Status.Phase, gitv1alpha1.TransactionPhaseFailed)
	}
	if txn.Status.Message != "push rejected" {
		t.Errorf("message = %q, want %q", txn.Status.Message, "push rejected")
	}
	if txn.Status.CompletionTime == nil {
		t.Error("CompletionTime should be set")
	}
}

func TestUpdateBranches(t *testing.T) {
	// Pre-create a branch CRD in the fake client.
	branchObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "my-repo-main",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repositoryRef": "my-repo",
				"branchName":    "main",
			},
			"status": map[string]interface{}{
				"headCommit": "old-sha",
			},
		},
	}

	r := newFakeReconciler(t, branchObj)

	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "txn-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "abc123",
					Destination: "refs/heads/main",
				},
			},
		},
		Status: gitv1alpha1.GitPushTransactionStatus{
			ResultCommit: "new-sha",
		},
	}

	err := r.updateBranches(context.Background(), "default", txn)
	if err != nil {
		t.Fatalf("updateBranches() error = %v", err)
	}

	// Verify the branch was updated via the fake client.
	updatedBranch, err := r.gitClient.GitBranches("default").Get(context.Background(), "my-repo-main", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting branch after update: %v", err)
	}
	if updatedBranch.Status.HeadCommit != "new-sha" {
		t.Errorf("HeadCommit = %q, want %q", updatedBranch.Status.HeadCommit, "new-sha")
	}
}

func TestUpdateBranches_NoMatchingBranch(t *testing.T) {
	// Branch for a different repo should not be updated.
	branchObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "other-repo-main",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repositoryRef": "other-repo",
				"branchName":    "main",
			},
			"status": map[string]interface{}{
				"headCommit": "old-sha",
			},
		},
	}

	r := newFakeReconciler(t, branchObj)

	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "txn-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{Source: "abc123", Destination: "refs/heads/main"},
			},
		},
		Status: gitv1alpha1.GitPushTransactionStatus{ResultCommit: "new-sha"},
	}

	err := r.updateBranches(context.Background(), "default", txn)
	if err != nil {
		t.Fatalf("updateBranches() error = %v", err)
	}

	// Verify the other branch was NOT updated.
	otherBranch, err := r.gitClient.GitBranches("default").Get(context.Background(), "other-repo-main", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting branch: %v", err)
	}
	if otherBranch.Status.HeadCommit != "old-sha" {
		t.Errorf("HeadCommit = %q, want %q (should be unchanged)", otherBranch.Status.HeadCommit, "old-sha")
	}
}

func TestResolveAuth_NoAuth(t *testing.T) {
	r := newFakeReconciler(t)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
		},
	}

	auth, err := r.resolveAuth(context.Background(), "default", repo)
	if err != nil {
		t.Fatalf("resolveAuth() error = %v", err)
	}
	if auth != nil {
		t.Errorf("auth = %v, want nil for repo without auth", auth)
	}
}

func TestResolveAuth_WithSecret(t *testing.T) {
	secretObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "git-creds",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"username": base64.StdEncoding.EncodeToString([]byte("myuser")),
				"password": base64.StdEncoding.EncodeToString([]byte("mypass")),
			},
		},
	}

	r := newFakeReconciler(t, secretObj)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "git-creds"},
			},
		},
	}

	auth, err := r.resolveAuth(context.Background(), "default", repo)
	if err != nil {
		t.Fatalf("resolveAuth() error = %v", err)
	}
	if auth == nil {
		t.Fatal("auth = nil, want non-nil")
	}
	if auth.Username != "myuser" {
		t.Errorf("username = %q, want %q", auth.Username, "myuser")
	}
	if auth.Password != "mypass" {
		t.Errorf("password = %q, want %q", auth.Password, "mypass")
	}
}
