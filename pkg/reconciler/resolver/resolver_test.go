package resolver

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing/object"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	ktesting "k8s.io/client-go/testing"

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

func TestChangeName(t *testing.T) {
	tests := []struct {
		name   string
		change *object.Change
		want   string
	}{
		{
			name: "from name takes precedence",
			change: &object.Change{
				From: object.ChangeEntry{Name: "file-from.txt"},
				To:   object.ChangeEntry{Name: "file-to.txt"},
			},
			want: "file-from.txt",
		},
		{
			name: "to name used when from is empty (new file)",
			change: &object.Change{
				From: object.ChangeEntry{Name: ""},
				To:   object.ChangeEntry{Name: "new-file.txt"},
			},
			want: "new-file.txt",
		},
		{
			name: "rename uses from name",
			change: &object.Change{
				From: object.ChangeEntry{Name: "old-name.txt"},
				To:   object.ChangeEntry{Name: "new-name.txt"},
			},
			want: "old-name.txt",
		},
		{
			name: "deleted file (no to name)",
			change: &object.Change{
				From: object.ChangeEntry{Name: "deleted.txt"},
				To:   object.ChangeEntry{Name: ""},
			},
			want: "deleted.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeName(tt.change)
			if got != tt.want {
				t.Errorf("changeName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestReconcileKind_SkipsNonConflicted(t *testing.T) {
	r := newFakeReconciler(t)

	// A sync that is not in Conflicted phase should be skipped.
	for _, phase := range []gitv1alpha1.SyncPhase{
		gitv1alpha1.SyncPhaseInSync,
		gitv1alpha1.SyncPhaseSyncing,
		gitv1alpha1.SyncPhaseRequiresManualIntervention,
	} {
		t.Run(string(phase), func(t *testing.T) {
			syncObj := &gitv1alpha1.GitRepoSync{
				ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
				Status: gitv1alpha1.GitRepoSyncStatus{
					Phase: phase,
				},
			}
			err := r.ReconcileKind(context.Background(), syncObj)
			if err != nil {
				t.Fatalf("ReconcileKind() error = %v, want nil for phase %s", err, phase)
			}
		})
	}
}

func TestReconcileKind_MissingCommitHashes(t *testing.T) {
	// When commit hashes are missing, should fall back to ManualIntervention.
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
				"phase": "Conflicted",
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
		Status: gitv1alpha1.GitRepoSyncStatus{
			Phase: gitv1alpha1.SyncPhaseConflicted,
			// All commit hashes empty.
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

func TestMarkManualIntervention(t *testing.T) {
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
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "sync-1", Namespace: "default"},
		Status:     gitv1alpha1.GitRepoSyncStatus{Phase: gitv1alpha1.SyncPhaseConflicted},
	}

	err := r.markManualIntervention(context.Background(), syncObj, "merge conflict in file.txt")
	if err != nil {
		t.Fatalf("markManualIntervention() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseRequiresManualIntervention {
		t.Errorf("Phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseRequiresManualIntervention)
	}
	if syncObj.Status.Message != "merge conflict in file.txt" {
		t.Errorf("Message = %q, want %q", syncObj.Status.Message, "merge conflict in file.txt")
	}
}

func TestCreateMergePushTransactions(t *testing.T) {
	// The fake dynamic client does not support GenerateName, so both
	// transactions end up with name "". We use a reactor to simulate
	// unique naming by assigning sequential names.
	scheme := newFakeScheme()
	dynClient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme, customListKinds)

	counter := 0
	dynClient.PrependReactor("create", "gitpushtransactions", func(action ktesting.Action) (bool, runtime.Object, error) {
		createAction := action.(ktesting.CreateAction)
		obj := createAction.GetObject().(*unstructured.Unstructured)
		if obj.GetName() == "" && obj.GetGenerateName() != "" {
			counter++
			obj.SetName(fmt.Sprintf("%s%d", obj.GetGenerateName(), counter))
		}
		return false, obj, nil
	})

	gc := gitclient.NewFromDynamic(dynClient)
	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gc,
	}

	syncObj := &gitv1alpha1.GitRepoSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-1",
			Namespace: "default",
			UID:       "uid-sync",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}

	err := r.createMergePushTransactions(context.Background(), "default", syncObj, "merge-sha", "old-a", "old-b")
	if err != nil {
		t.Fatalf("createMergePushTransactions() error = %v", err)
	}

	// Verify two transactions were created.
	txns, err := r.gitClient.GitPushTransactions("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing transactions: %v", err)
	}
	if len(txns.Items) != 2 {
		t.Fatalf("expected 2 transactions, got %d", len(txns.Items))
	}

	// Find txn targeting repo-a and repo-b.
	var txnA, txnB *gitv1alpha1.GitPushTransaction
	for i := range txns.Items {
		txn := &txns.Items[i]
		switch txn.Spec.RepositoryRef {
		case "repo-a":
			txnA = txn
		case "repo-b":
			txnB = txn
		}
	}

	if txnA == nil {
		t.Fatal("no transaction found targeting repo-a")
	}
	if txnB == nil {
		t.Fatal("no transaction found targeting repo-b")
	}

	// Verify txn A.
	if len(txnA.Spec.RefSpecs) != 1 {
		t.Fatalf("txnA RefSpecs length = %d, want 1", len(txnA.Spec.RefSpecs))
	}
	if txnA.Spec.RefSpecs[0].Source != "merge-sha" {
		t.Errorf("txnA Source = %q, want %q", txnA.Spec.RefSpecs[0].Source, "merge-sha")
	}
	if txnA.Spec.RefSpecs[0].ExpectedOldCommit != "old-a" {
		t.Errorf("txnA ExpectedOldCommit = %q, want %q", txnA.Spec.RefSpecs[0].ExpectedOldCommit, "old-a")
	}
	if txnA.Labels["git-k8s.imjasonh.com/merge"] != "true" {
		t.Error("txnA should have merge=true label")
	}

	// Verify txn B.
	if txnB.Spec.RefSpecs[0].ExpectedOldCommit != "old-b" {
		t.Errorf("txnB ExpectedOldCommit = %q, want %q", txnB.Spec.RefSpecs[0].ExpectedOldCommit, "old-b")
	}
	if txnB.Labels["git-k8s.imjasonh.com/target"] != "repo-b" {
		t.Errorf("txnB target label = %q, want %q", txnB.Labels["git-k8s.imjasonh.com/target"], "repo-b")
	}
}
