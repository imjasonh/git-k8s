package repowatcher

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func TestReconcileKind_LsRemoteError(t *testing.T) {
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata":   map[string]interface{}{"name": "my-repo", "namespace": "default", "uid": "repo-uid"},
			"spec":       map[string]interface{}{"url": "https://example.com/repo.git"},
			"status":     map[string]interface{}{},
		},
	}

	failingLsRemote := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return nil, fmt.Errorf("network error")
	}

	r := newFakeReconciler(t, failingLsRemote, repoObj)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepository"},
		ObjectMeta: metav1.ObjectMeta{Name: "my-repo", Namespace: "default", UID: "repo-uid"},
		Spec:       gitv1alpha1.GitRepositorySpec{URL: "https://example.com/repo.git"},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err == nil {
		t.Fatal("ReconcileKind() should return error when ls-remote fails")
	}
}

func TestReconcileKind_EmptyRemote(t *testing.T) {
	// Remote has no branches. Should still succeed and update LastFetchTime.
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata":   map[string]interface{}{"name": "my-repo", "namespace": "default", "uid": "repo-uid"},
			"spec":       map[string]interface{}{"url": "https://example.com/repo.git"},
			"status":     map[string]interface{}{},
		},
	}

	emptyLsRemote := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return []*plumbing.Reference{}, nil
	}

	r := newFakeReconciler(t, emptyLsRemote, repoObj)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepository"},
		ObjectMeta: metav1.ObjectMeta{Name: "my-repo", Namespace: "default", UID: "repo-uid"},
		Spec:       gitv1alpha1.GitRepositorySpec{URL: "https://example.com/repo.git"},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if repo.Status.LastFetchTime == nil {
		t.Error("LastFetchTime should be set")
	}
}

func TestReconcileKind_BranchUnchanged(t *testing.T) {
	// Existing branch has the same commit as remote - should not update.
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata":   map[string]interface{}{"name": "my-repo", "namespace": "default", "uid": "repo-uid"},
			"spec":       map[string]interface{}{"url": "https://example.com/repo.git"},
			"status":     map[string]interface{}{},
		},
	}

	sameSHA := "aabb00aabb00aabb00aabb00aabb00aabb00aabb"
	existingBranch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name": "my-repo-main", "namespace": "default",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
						"kind": "GitRepository", "name": "my-repo", "uid": "repo-uid",
					},
				},
			},
			"spec":   map[string]interface{}{"repositoryRef": "my-repo", "branchName": "main"},
			"status": map[string]interface{}{"headCommit": sameSHA},
		},
	}

	mockRefs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash(sameSHA)),
	}

	lsRemoteFn := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return mockRefs, nil
	}

	r := newFakeReconciler(t, lsRemoteFn, repoObj, existingBranch)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepository"},
		ObjectMeta: metav1.ObjectMeta{Name: "my-repo", Namespace: "default", UID: "repo-uid"},
		Spec:       gitv1alpha1.GitRepositorySpec{URL: "https://example.com/repo.git"},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Branch should still have same commit (no update needed).
	branch, _ := r.gitClient.GitBranches("default").Get(context.Background(), "my-repo-main", metav1.GetOptions{})
	if branch.Status.HeadCommit != sameSHA {
		t.Errorf("HeadCommit = %q, want %q (should be unchanged)", branch.Status.HeadCommit, sameSHA)
	}
}
