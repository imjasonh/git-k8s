package sync

import (
	"context"
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
	return scheme
}

var customListKinds = map[schema.GroupVersionResource]string{
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitrepositories"}:    "GitRepositoryList",
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitbranches"}:         "GitBranchList",
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitpushtransactions"}: "GitPushTransactionList",
	{Group: "git-k8s.imjasonh.com", Version: "v1alpha1", Resource: "gitreposyncs"}:        "GitRepoSyncList",
}

func newFakeReconciler(t *testing.T, objects ...*unstructured.Unstructured) *Reconciler {
	t.Helper()
	dynClient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(newFakeScheme(), customListKinds)
	gc := gitclient.NewFromDynamic(dynClient)
	for _, obj := range objects {
		gvr, ok := gvrForKind(obj.GetKind())
		if !ok {
			t.Fatalf("unknown kind %q", obj.GetKind())
		}
		if _, err := dynClient.Resource(gvr).Namespace(obj.GetNamespace()).Create(context.Background(), obj, metav1.CreateOptions{}); err != nil {
			t.Fatalf("seeding %s/%s: %v", obj.GetNamespace(), obj.GetName(), err)
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
	}
	gvr, ok := m[kind]
	return gvr, ok
}

func TestFindBranch_Found(t *testing.T) {
	branchObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "repo-a-main",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repositoryRef": "repo-a",
				"branchName":    "main",
			},
			"status": map[string]interface{}{
				"headCommit": "abc123",
			},
		},
	}

	r := newFakeReconciler(t, branchObj)
	branch, err := r.findBranch(context.Background(), "default", "repo-a", "main")
	if err != nil {
		t.Fatalf("findBranch() error = %v", err)
	}
	if branch.Spec.RepositoryRef != "repo-a" {
		t.Errorf("RepositoryRef = %q, want %q", branch.Spec.RepositoryRef, "repo-a")
	}
	if branch.Spec.BranchName != "main" {
		t.Errorf("BranchName = %q, want %q", branch.Spec.BranchName, "main")
	}
	if branch.Status.HeadCommit != "abc123" {
		t.Errorf("HeadCommit = %q, want %q", branch.Status.HeadCommit, "abc123")
	}
}

func TestFindBranch_NotFound(t *testing.T) {
	r := newFakeReconciler(t)
	_, err := r.findBranch(context.Background(), "default", "repo-a", "main")
	if err == nil {
		t.Fatal("findBranch() should return error when branch not found")
	}
}

func TestFindBranch_WrongRepo(t *testing.T) {
	// Branch exists but for a different repo.
	branchObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "repo-b-main",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repositoryRef": "repo-b",
				"branchName":    "main",
			},
		},
	}

	r := newFakeReconciler(t, branchObj)
	_, err := r.findBranch(context.Background(), "default", "repo-a", "main")
	if err == nil {
		t.Fatal("findBranch() should return error when branch belongs to different repo")
	}
}

func TestUpdateSyncStatus(t *testing.T) {
	syncObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepoSync",
			"metadata": map[string]interface{}{
				"name":      "sync-1",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"repoA":      map[string]interface{}{"name": "repo-a"},
				"repoB":      map[string]interface{}{"name": "repo-b"},
				"branchName": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	tests := []struct {
		name    string
		phase   gitv1alpha1.SyncPhase
		message string
	}{
		{"InSync", gitv1alpha1.SyncPhaseInSync, "Repos are in sync"},
		{"Syncing", gitv1alpha1.SyncPhaseSyncing, "Pushing to A"},
		{"Conflicted", gitv1alpha1.SyncPhaseConflicted, "Repos diverged"},
		{"ManualIntervention", gitv1alpha1.SyncPhaseRequiresManualIntervention, "Merge failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newFakeReconciler(t, syncObj.DeepCopy())

			sync := &gitv1alpha1.GitRepoSync{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "git-k8s.imjasonh.com/v1alpha1",
					Kind:       "GitRepoSync",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sync-1",
					Namespace: "default",
				},
			}

			err := r.updateSyncStatus(context.Background(), sync, tt.phase, tt.message, "aaa", "bbb", "ccc")
			if err != nil {
				t.Fatalf("updateSyncStatus() error = %v", err)
			}
			if sync.Status.Phase != tt.phase {
				t.Errorf("Phase = %q, want %q", sync.Status.Phase, tt.phase)
			}
			if sync.Status.Message != tt.message {
				t.Errorf("Message = %q, want %q", sync.Status.Message, tt.message)
			}
			if sync.Status.RepoACommit != "aaa" {
				t.Errorf("RepoACommit = %q, want %q", sync.Status.RepoACommit, "aaa")
			}
			if sync.Status.RepoBCommit != "bbb" {
				t.Errorf("RepoBCommit = %q, want %q", sync.Status.RepoBCommit, "bbb")
			}
			if sync.Status.MergeBase != "ccc" {
				t.Errorf("MergeBase = %q, want %q", sync.Status.MergeBase, "ccc")
			}

			// InSync should set LastSyncTime.
			if tt.phase == gitv1alpha1.SyncPhaseInSync {
				if sync.Status.LastSyncTime == nil {
					t.Error("LastSyncTime should be set for InSync phase")
				}
			}
		})
	}
}

func TestCreatePushTransaction(t *testing.T) {
	r := newFakeReconciler(t)

	syncObj := &gitv1alpha1.GitRepoSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-1",
			Namespace: "default",
			UID:       "uid-123",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}

	err := r.createPushTransaction(context.Background(), "default", syncObj, "repo-a", "abc123", "main", "old-sha")
	if err != nil {
		t.Fatalf("createPushTransaction() error = %v", err)
	}

	// Verify the push transaction was created.
	txns, err := r.gitClient.GitPushTransactions("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing transactions: %v", err)
	}
	if len(txns.Items) != 1 {
		t.Fatalf("expected 1 transaction, got %d", len(txns.Items))
	}

	txn := txns.Items[0]
	if txn.Spec.RepositoryRef != "repo-a" {
		t.Errorf("RepositoryRef = %q, want %q", txn.Spec.RepositoryRef, "repo-a")
	}
	if !txn.Spec.Atomic {
		t.Error("Atomic should be true")
	}
	if len(txn.Spec.RefSpecs) != 1 {
		t.Fatalf("RefSpecs length = %d, want 1", len(txn.Spec.RefSpecs))
	}
	if txn.Spec.RefSpecs[0].Source != "abc123" {
		t.Errorf("Source = %q, want %q", txn.Spec.RefSpecs[0].Source, "abc123")
	}
	if txn.Spec.RefSpecs[0].Destination != "refs/heads/main" {
		t.Errorf("Destination = %q, want %q", txn.Spec.RefSpecs[0].Destination, "refs/heads/main")
	}
	if txn.Spec.RefSpecs[0].ExpectedOldCommit != "old-sha" {
		t.Errorf("ExpectedOldCommit = %q, want %q", txn.Spec.RefSpecs[0].ExpectedOldCommit, "old-sha")
	}

	// Verify owner references.
	if len(txn.OwnerReferences) != 1 {
		t.Fatalf("OwnerReferences length = %d, want 1", len(txn.OwnerReferences))
	}
	if txn.OwnerReferences[0].Name != "sync-1" {
		t.Errorf("OwnerRef Name = %q, want %q", txn.OwnerReferences[0].Name, "sync-1")
	}
	if txn.OwnerReferences[0].Kind != "GitRepoSync" {
		t.Errorf("OwnerRef Kind = %q, want %q", txn.OwnerReferences[0].Kind, "GitRepoSync")
	}

	// Verify labels.
	if txn.Labels["git-k8s.imjasonh.com/repo-sync"] != "sync-1" {
		t.Errorf("label repo-sync = %q, want %q", txn.Labels["git-k8s.imjasonh.com/repo-sync"], "sync-1")
	}
	if txn.Labels["git-k8s.imjasonh.com/target"] != "repo-a" {
		t.Errorf("label target = %q, want %q", txn.Labels["git-k8s.imjasonh.com/target"], "repo-a")
	}
}

func TestReconcileKind_BothInSync(t *testing.T) {
	// When both branches point to the same commit, status should be InSync.
	branchA := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "repo-a-main", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "repo-a", "branchName": "main"},
			"status":     map[string]interface{}{"headCommit": "same-sha"},
		},
	}
	branchB := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata":   map[string]interface{}{"name": "repo-b-main", "namespace": "default"},
			"spec":       map[string]interface{}{"repositoryRef": "repo-b", "branchName": "main"},
			"status":     map[string]interface{}{"headCommit": "same-sha"},
		},
	}
	syncObjU := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepoSync",
			"metadata":   map[string]interface{}{"name": "sync-1", "namespace": "default"},
			"spec": map[string]interface{}{
				"repoA": map[string]interface{}{"name": "repo-a"},
				"repoB": map[string]interface{}{"name": "repo-b"},
				"branchName": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	r := newFakeReconciler(t, branchA, branchB, syncObjU)

	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepoSync",
		},
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
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseInSync {
		t.Errorf("Phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseInSync)
	}
}

func TestReconcileKind_BranchNotFound(t *testing.T) {
	// When a branch CRD is not found, should set Conflicted status.
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

	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepoSync",
		},
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
