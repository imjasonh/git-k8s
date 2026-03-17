package resolver

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func TestReconcileKind_ConflictedWithMissingRepo(t *testing.T) {
	// A conflicted sync with commit hashes but missing repo should return error.
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
			"status": map[string]interface{}{
				"phase":       "Conflicted",
				"repoACommit": "aaaa1111",
				"repoBCommit": "bbbb2222",
				"mergeBase":   "cccc3333",
			},
		},
	}

	r := newFakeReconciler(t, syncObjU)

	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
		Status: gitv1alpha1.GitRepoSyncStatus{
			Phase:       gitv1alpha1.SyncPhaseConflicted,
			RepoACommit: "aaaa1111",
			RepoBCommit: "bbbb2222",
			MergeBase:   "cccc3333",
		},
	}

	err := r.ReconcileKind(context.Background(), syncObj)
	if err == nil {
		t.Fatal("ReconcileKind() should return error when repo-a doesn't exist")
	}
}

func TestReconcileKind_ConflictedPartialHashes(t *testing.T) {
	// Test that missing RepoACommit triggers ManualIntervention.
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
			"status": map[string]interface{}{
				"phase":       "Conflicted",
				"repoACommit": "aaaa1111",
				// repoBCommit missing
				"mergeBase": "cccc3333",
			},
		},
	}

	repoA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata":   map[string]interface{}{"name": "repo-a", "namespace": "default"},
			"spec":       map[string]interface{}{"url": "https://example.com/repo-a.git"},
		},
	}

	r := newFakeReconciler(t, syncObjU, repoA)

	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
		Status: gitv1alpha1.GitRepoSyncStatus{
			Phase:       gitv1alpha1.SyncPhaseConflicted,
			RepoACommit: "aaaa1111",
			MergeBase:   "cccc3333",
			// RepoBCommit is empty
		},
	}

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseRequiresManualIntervention {
		t.Errorf("Phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseRequiresManualIntervention)
	}
}

func TestReconcileKind_EmptyPhase(t *testing.T) {
	// A sync with empty phase should be skipped.
	r := newFakeReconciler(t)

	syncObj := &gitv1alpha1.GitRepoSync{
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
		Status:     gitv1alpha1.GitRepoSyncStatus{Phase: ""},
	}

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v, want nil for non-conflicted phase", err)
	}
}

func TestMarkManualIntervention_SetsFields(t *testing.T) {
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
			"status": map[string]interface{}{"phase": "Conflicted"},
		},
	}

	r := newFakeReconciler(t, syncObjU)

	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: "git-k8s.imjasonh.com/v1alpha1", Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
		Status:     gitv1alpha1.GitRepoSyncStatus{Phase: gitv1alpha1.SyncPhaseConflicted},
	}

	err := r.markManualIntervention(context.Background(), syncObj, "test message")
	if err != nil {
		t.Fatalf("markManualIntervention() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseRequiresManualIntervention {
		t.Errorf("Phase = %q, want RequiresManualIntervention", syncObj.Status.Phase)
	}
	if syncObj.Status.Message != "test message" {
		t.Errorf("Message = %q, want %q", syncObj.Status.Message, "test message")
	}
}
