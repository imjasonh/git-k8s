package sync

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func TestReconcileKind_BranchBNotFound(t *testing.T) {
	// Branch A exists, Branch B does not - should set Conflicted.
	branchA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "repo-a-main", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "repo-a", "branchName": "main"},
			"status":     map[string]interface{}{"headCommit": "aaa"},
		},
	}
	syncObjU := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepoSync",
			"metadata":   map[string]interface{}{"name": "sync-1", "namespace": "default"},
			"spec": map[string]interface{}{
				"repoA":      map[string]interface{}{"name": "repo-a"},
				"repoB":      map[string]interface{}{"name": "repo-b"},
				"branchName": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	r := newFakeReconciler(t, branchA, syncObjU)

	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseConflicted {
		t.Errorf("Phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseConflicted)
	}
}

func TestUpdateSyncStatus_InSyncSetsLastSyncTime(t *testing.T) {
	syncObjU := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepoSync",
			"metadata":   map[string]interface{}{"name": "sync-1", "namespace": "default"},
			"spec": map[string]interface{}{
				"repoA":      map[string]interface{}{"name": "repo-a"},
				"repoB":      map[string]interface{}{"name": "repo-b"},
				"branchName": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	r := newFakeReconciler(t, syncObjU)

	sync := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
	}

	err := r.updateSyncStatus(context.Background(), sync, gitv1alpha1.SyncPhaseInSync, "synced", "a", "b", "c")
	if err != nil {
		t.Fatalf("updateSyncStatus() error = %v", err)
	}
	if sync.Status.LastSyncTime == nil {
		t.Error("LastSyncTime should be set for InSync phase")
	}
}

func TestUpdateSyncStatus_SyncingDoesNotSetLastSyncTime(t *testing.T) {
	syncObjU := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepoSync",
			"metadata":   map[string]interface{}{"name": "sync-1", "namespace": "default"},
			"spec": map[string]interface{}{
				"repoA":      map[string]interface{}{"name": "repo-a"},
				"repoB":      map[string]interface{}{"name": "repo-b"},
				"branchName": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	r := newFakeReconciler(t, syncObjU)

	sync := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
	}

	err := r.updateSyncStatus(context.Background(), sync, gitv1alpha1.SyncPhaseSyncing, "pushing", "a", "b", "c")
	if err != nil {
		t.Fatalf("updateSyncStatus() error = %v", err)
	}
	if sync.Status.LastSyncTime != nil {
		t.Error("LastSyncTime should NOT be set for Syncing phase")
	}
}

func TestFindBranch_MultipleBranches(t *testing.T) {
	// Two branches for different repos with the same branch name.
	branchA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "repo-a-main", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "repo-a", "branchName": "main"},
			"status":     map[string]interface{}{"headCommit": "aaa"},
		},
	}
	branchB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "repo-b-main", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "repo-b", "branchName": "main"},
			"status":     map[string]interface{}{"headCommit": "bbb"},
		},
	}

	r := newFakeReconciler(t, branchA, branchB)

	// Should find repo-a's branch.
	branch, err := r.findBranch(context.Background(), "default", "repo-a", "main")
	if err != nil {
		t.Fatalf("findBranch() error = %v", err)
	}
	if branch.Status.HeadCommit != "aaa" {
		t.Errorf("HeadCommit = %q, want %q", branch.Status.HeadCommit, "aaa")
	}

	// Should find repo-b's branch.
	branch, err = r.findBranch(context.Background(), "default", "repo-b", "main")
	if err != nil {
		t.Fatalf("findBranch() error = %v", err)
	}
	if branch.Status.HeadCommit != "bbb" {
		t.Errorf("HeadCommit = %q, want %q", branch.Status.HeadCommit, "bbb")
	}
}
