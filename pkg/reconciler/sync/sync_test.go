package sync

import (
	"context"
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

func gvr(resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: gitv1alpha1.GroupName, Version: gitv1alpha1.Version, Resource: resource}
}

func toUnstructured(t *testing.T, obj interface{}) *unstructured.Unstructured {
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
	u := toUnstructured(t, obj)
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
			gvr("gitreposyncs"):        "GitRepoSyncList",
		})
	gitClient := gitclient.NewFromDynamic(dynClient)
	return &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}, dynClient
}

func TestFindBranch_Found(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	branch := &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "repo-a-main",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "repo-a",
			BranchName:    "main",
		},
		Status: gitv1alpha1.GitBranchStatus{
			HeadCommit: "abc123",
		},
	}
	seedObject(t, dynClient, gvr("gitbranches"), branch)

	got, err := r.findBranch(context.Background(), "default", "repo-a", "main")
	if err != nil {
		t.Fatalf("findBranch() error = %v", err)
	}
	if got.Name != "repo-a-main" {
		t.Errorf("findBranch() name = %q, want %q", got.Name, "repo-a-main")
	}
	if got.Status.HeadCommit != "abc123" {
		t.Errorf("findBranch() headCommit = %q, want %q", got.Status.HeadCommit, "abc123")
	}
}

func TestFindBranch_NotFound(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	seedObject(t, dynClient, gvr("gitbranches"), &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "repo-a-main", Namespace: "default"},
		Spec:       gitv1alpha1.GitBranchSpec{RepositoryRef: "repo-a", BranchName: "main"},
	})

	_, err := r.findBranch(context.Background(), "default", "repo-b", "main")
	if err == nil {
		t.Fatal("findBranch() expected error for non-existent branch")
	}
}

func TestFindBranch_WrongBranchName(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	seedObject(t, dynClient, gvr("gitbranches"), &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "repo-a-main", Namespace: "default"},
		Spec:       gitv1alpha1.GitBranchSpec{RepositoryRef: "repo-a", BranchName: "main"},
	})

	_, err := r.findBranch(context.Background(), "default", "repo-a", "develop")
	if err == nil {
		t.Fatal("findBranch() expected error for wrong branch name")
	}
}

func TestUpdateSyncStatus_InSync(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "my-sync", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA: gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB: gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}
	seedObject(t, dynClient, gvr("gitreposyncs"), syncObj)

	err := r.updateSyncStatus(context.Background(), syncObj,
		gitv1alpha1.SyncPhaseInSync, "Repos are in sync", "abc123", "abc123", "abc123")
	if err != nil {
		t.Fatalf("updateSyncStatus() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseInSync {
		t.Errorf("phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseInSync)
	}
	if syncObj.Status.LastSyncTime == nil {
		t.Error("LastSyncTime should be set for InSync phase")
	}
}

func TestUpdateSyncStatus_Conflicted(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "my-sync", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA: gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB: gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}
	seedObject(t, dynClient, gvr("gitreposyncs"), syncObj)

	err := r.updateSyncStatus(context.Background(), syncObj,
		gitv1alpha1.SyncPhaseConflicted, "Repos have diverged", "aaa", "bbb", "ccc")
	if err != nil {
		t.Fatalf("updateSyncStatus() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseConflicted {
		t.Errorf("phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseConflicted)
	}
	if syncObj.Status.LastSyncTime != nil {
		t.Error("LastSyncTime should NOT be set for non-InSync phase")
	}
	if syncObj.Status.MergeBase != "ccc" {
		t.Errorf("MergeBase = %q, want %q", syncObj.Status.MergeBase, "ccc")
	}
}

func TestCreatePushTransaction(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "my-sync", Namespace: "default", UID: "sync-uid-123"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA: gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB: gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}
	seedObject(t, dynClient, gvr("gitreposyncs"), syncObj)

	err := r.createPushTransaction(context.Background(), "default", syncObj,
		"repo-a", "new-commit-sha", "main", "old-commit-sha")
	if err != nil {
		t.Fatalf("createPushTransaction() error = %v", err)
	}

	// Verify through the dynamic client directly.
	txnList, err := dynClient.Resource(gvr("gitpushtransactions")).Namespace("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing push transactions: %v", err)
	}
	if len(txnList.Items) != 1 {
		t.Fatalf("expected 1 push transaction, got %d", len(txnList.Items))
	}

	txnU := txnList.Items[0]
	labels := txnU.GetLabels()
	if labels["git-k8s.imjasonh.com/repo-sync"] != "my-sync" {
		t.Errorf("repo-sync label = %q, want %q", labels["git-k8s.imjasonh.com/repo-sync"], "my-sync")
	}
	if labels["git-k8s.imjasonh.com/target"] != "repo-a" {
		t.Errorf("target label = %q, want %q", labels["git-k8s.imjasonh.com/target"], "repo-a")
	}
	ownerRefs := txnU.GetOwnerReferences()
	if len(ownerRefs) != 1 || ownerRefs[0].Kind != "GitRepoSync" {
		t.Errorf("expected owner reference to GitRepoSync")
	}
}

func TestReconcileKind_InSync(t *testing.T) {
	r, dynClient := newFakeReconciler(t)

	seedObject(t, dynClient, gvr("gitbranches"), &gitv1alpha1.GitBranch{
		TypeMeta:   metav1.TypeMeta{APIVersion: gitv1alpha1.SchemeGroupVersion.String(), Kind: "GitBranch"},
		ObjectMeta: metav1.ObjectMeta{Name: "repo-a-main", Namespace: "default"},
		Spec:       gitv1alpha1.GitBranchSpec{RepositoryRef: "repo-a", BranchName: "main"},
		Status:     gitv1alpha1.GitBranchStatus{HeadCommit: "same-commit-sha"},
	})
	seedObject(t, dynClient, gvr("gitbranches"), &gitv1alpha1.GitBranch{
		TypeMeta:   metav1.TypeMeta{APIVersion: gitv1alpha1.SchemeGroupVersion.String(), Kind: "GitBranch"},
		ObjectMeta: metav1.ObjectMeta{Name: "repo-b-main", Namespace: "default"},
		Spec:       gitv1alpha1.GitBranchSpec{RepositoryRef: "repo-b", BranchName: "main"},
		Status:     gitv1alpha1.GitBranchStatus{HeadCommit: "same-commit-sha"},
	})
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: gitv1alpha1.SchemeGroupVersion.String(), Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "my-sync", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA: gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB: gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}
	seedObject(t, dynClient, gvr("gitreposyncs"), syncObj)

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseInSync {
		t.Errorf("phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseInSync)
	}
}

func TestReconcileKind_BranchNotFound(t *testing.T) {
	r, dynClient := newFakeReconciler(t)
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta:   metav1.TypeMeta{APIVersion: gitv1alpha1.SchemeGroupVersion.String(), Kind: "GitRepoSync"},
		ObjectMeta: metav1.ObjectMeta{Name: "my-sync", Namespace: "default"},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA: gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB: gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}
	seedObject(t, dynClient, gvr("gitreposyncs"), syncObj)

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseConflicted {
		t.Errorf("phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseConflicted)
	}
}
