package push

import (
	"context"
	"encoding/base64"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	"github.com/imjasonh/git-k8s/pkg/gitauth"
)

func TestReconcileKind_PendingToInProgress(t *testing.T) {
	// A Pending transaction with a missing repo should transition to Failed.
	txnObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitPushTransaction",
			"metadata": map[string]interface{}{
				"name":      "txn-1",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repositoryRef": "nonexistent-repo",
				"refSpecs":      []interface{}{},
			},
			"status": map[string]interface{}{
				"phase": "Pending",
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
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "nonexistent-repo",
		},
		Status: gitv1alpha1.GitPushTransactionStatus{
			Phase: gitv1alpha1.TransactionPhasePending,
		},
	}

	err := r.ReconcileKind(context.Background(), txn)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// ReconcileKind reassigns the local txn variable after UpdateStatus, so
	// read the final status from the fake client to verify the end state.
	updated, err := r.gitClient.GitPushTransactions("default").Get(context.Background(), "txn-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting txn after reconcile: %v", err)
	}
	if updated.Status.Phase != gitv1alpha1.TransactionPhaseFailed {
		t.Errorf("Phase = %q, want %q", updated.Status.Phase, gitv1alpha1.TransactionPhaseFailed)
	}
	if updated.Status.CompletionTime == nil {
		t.Error("CompletionTime should be set on failure")
	}
}

func TestReconcileKind_RepoExistsButNoAuth(t *testing.T) {
	// A transaction targeting a repo without auth should attempt push (and fail
	// because the URL is fake, but the path through resolveAuth should succeed).
	txnObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitPushTransaction",
			"metadata":   map[string]interface{}{"name": "txn-2", "namespace": "default"},
			"spec": map[string]interface{}{
				"repositoryRef": "my-repo",
				"refSpecs": []interface{}{
					map[string]interface{}{
						"source":      "abc123",
						"destination": "refs/heads/main",
					},
				},
			},
			"status": map[string]interface{}{"phase": "Pending"},
		},
	}

	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata":   map[string]interface{}{"name": "my-repo", "namespace": "default"},
			"spec": map[string]interface{}{
				"url": "https://nonexistent.invalid/repo.git",
			},
		},
	}

	r := newFakeReconciler(t, txnObj, repoObj)

	txn := &gitv1alpha1.GitPushTransaction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitPushTransaction",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "txn-2", Namespace: "default"},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{Source: "abc123", Destination: "refs/heads/main"},
			},
		},
		Status: gitv1alpha1.GitPushTransactionStatus{Phase: gitv1alpha1.TransactionPhasePending},
	}

	err := r.ReconcileKind(context.Background(), txn)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Read from fake client since ReconcileKind reassigns the local variable.
	updated, err := r.gitClient.GitPushTransactions("default").Get(context.Background(), "txn-2", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting txn after reconcile: %v", err)
	}
	if updated.Status.Phase != gitv1alpha1.TransactionPhaseFailed {
		t.Errorf("Phase = %q, want %q", updated.Status.Phase, gitv1alpha1.TransactionPhaseFailed)
	}
	if updated.Status.Message == "" {
		t.Error("Message should be set on failure")
	}
}

func TestResolveAuth_MissingSecret(t *testing.T) {
	r := newFakeReconciler(t)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "nonexistent-secret"},
			},
		},
	}

	_, err := gitauth.ResolveAuth(context.Background(), r.dynamicClient, "default", repo)
	if err == nil {
		t.Fatal("resolveAuth() should return error for missing secret")
	}
}

func TestResolveAuth_InvalidBase64(t *testing.T) {
	secretObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata":   map[string]interface{}{"name": "bad-secret", "namespace": "default"},
			"data": map[string]interface{}{
				"username": "not-valid-base64!!!",
				"password": base64.StdEncoding.EncodeToString([]byte("pass")),
			},
		},
	}

	r := newFakeReconciler(t, secretObj)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:  "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{SecretRef: &gitv1alpha1.SecretRef{Name: "bad-secret"}},
		},
	}

	_, err := gitauth.ResolveAuth(context.Background(), r.dynamicClient, "default", repo)
	if err == nil {
		t.Fatal("resolveAuth() should return error for invalid base64")
	}
}

func TestUpdateBranches_MultipleBranches(t *testing.T) {
	// Create two branches for the same repo.
	branch1 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "my-repo-main", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "my-repo", "branchName": "main"},
			"status":     map[string]interface{}{"headCommit": "old-sha"},
		},
	}
	branch2 := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "my-repo-develop", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "my-repo", "branchName": "develop"},
			"status":     map[string]interface{}{"headCommit": "old-sha-2"},
		},
	}

	r := newFakeReconciler(t, branch1, branch2)

	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{Name: "txn-1", Namespace: "default"},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{Source: "abc", Destination: "refs/heads/main"},
			},
		},
		Status: gitv1alpha1.GitPushTransactionStatus{ResultCommit: "new-sha"},
	}

	err := r.updateBranches(context.Background(), "default", txn)
	if err != nil {
		t.Fatalf("updateBranches() error = %v", err)
	}

	// Only main should be updated, develop should remain unchanged.
	main, _ := r.gitClient.GitBranches("default").Get(context.Background(), "my-repo-main", metav1.GetOptions{})
	if main.Status.HeadCommit != "new-sha" {
		t.Errorf("main HeadCommit = %q, want %q", main.Status.HeadCommit, "new-sha")
	}

	develop, _ := r.gitClient.GitBranches("default").Get(context.Background(), "my-repo-develop", metav1.GetOptions{})
	if develop.Status.HeadCommit != "old-sha-2" {
		t.Errorf("develop HeadCommit = %q, want %q (should be unchanged)", develop.Status.HeadCommit, "old-sha-2")
	}
}
