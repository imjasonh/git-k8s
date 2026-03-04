package resolver

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

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

// storeBlob writes a blob to the in-memory storage and returns its hash.
func storeBlob(t *testing.T, storer *memory.Storage, content string) plumbing.Hash {
	t.Helper()
	obj := storer.NewEncodedObject()
	obj.SetType(plumbing.BlobObject)
	w, err := obj.Writer()
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(content))
	w.Close()
	hash, err := storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

// storeTree writes a tree with the given entries to storage and returns it.
func storeTree(t *testing.T, storer *memory.Storage, entries []object.TreeEntry) (*object.Tree, plumbing.Hash) {
	t.Helper()
	tree := &object.Tree{Entries: entries}
	obj := storer.NewEncodedObject()
	if err := tree.Encode(obj); err != nil {
		t.Fatal(err)
	}
	hash, err := storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	tree.Hash = hash
	return tree, hash
}

func TestBuildMergedTree_AdditionFromB(t *testing.T) {
	// Tree A has file1. diffB adds file2. Merged tree should have both.
	storer := memory.NewStorage()

	blob1 := storeBlob(t, storer, "content of file1")
	blob2 := storeBlob(t, storer, "content of file2")

	treeA, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "file1.txt", Mode: filemode.Regular, Hash: blob1},
	})

	treeB, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "file1.txt", Mode: filemode.Regular, Hash: blob1},
		{Name: "file2.txt", Mode: filemode.Regular, Hash: blob2},
	})

	diffB := object.Changes{
		&object.Change{
			From: object.ChangeEntry{Name: ""},
			To:   object.ChangeEntry{Name: "file2.txt"},
		},
	}

	mergedHash, err := buildMergedTree(storer, treeA, treeB, diffB)
	if err != nil {
		t.Fatalf("buildMergedTree() error = %v", err)
	}
	if mergedHash == plumbing.ZeroHash {
		t.Fatal("buildMergedTree() returned zero hash")
	}

	mergedObj, err := storer.EncodedObject(plumbing.TreeObject, mergedHash)
	if err != nil {
		t.Fatalf("getting merged tree: %v", err)
	}
	merged := &object.Tree{}
	if err := merged.Decode(mergedObj); err != nil {
		t.Fatalf("decoding merged tree: %v", err)
	}

	if len(merged.Entries) != 2 {
		t.Fatalf("merged tree entries = %d, want 2", len(merged.Entries))
	}

	names := make(map[string]bool)
	for _, e := range merged.Entries {
		names[e.Name] = true
	}
	if !names["file1.txt"] {
		t.Error("merged tree missing file1.txt")
	}
	if !names["file2.txt"] {
		t.Error("merged tree missing file2.txt")
	}
}

func TestBuildMergedTree_DeletionFromB(t *testing.T) {
	// Tree A has file1 and file2. diffB deletes file2. Merged tree should only have file1.
	storer := memory.NewStorage()

	blob1 := storeBlob(t, storer, "content of file1")
	blob2 := storeBlob(t, storer, "content of file2")

	treeA, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "file1.txt", Mode: filemode.Regular, Hash: blob1},
		{Name: "file2.txt", Mode: filemode.Regular, Hash: blob2},
	})

	treeB, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "file1.txt", Mode: filemode.Regular, Hash: blob1},
	})

	diffB := object.Changes{
		&object.Change{
			From: object.ChangeEntry{Name: "file2.txt"},
			To:   object.ChangeEntry{Name: ""},
		},
	}

	mergedHash, err := buildMergedTree(storer, treeA, treeB, diffB)
	if err != nil {
		t.Fatalf("buildMergedTree() error = %v", err)
	}

	mergedObj, err := storer.EncodedObject(plumbing.TreeObject, mergedHash)
	if err != nil {
		t.Fatalf("getting merged tree: %v", err)
	}
	merged := &object.Tree{}
	if err := merged.Decode(mergedObj); err != nil {
		t.Fatalf("decoding merged tree: %v", err)
	}

	if len(merged.Entries) != 1 {
		t.Fatalf("merged tree entries = %d, want 1", len(merged.Entries))
	}
	if merged.Entries[0].Name != "file1.txt" {
		t.Errorf("remaining entry = %q, want %q", merged.Entries[0].Name, "file1.txt")
	}
}

func TestBuildMergedTree_ModificationFromB(t *testing.T) {
	// Tree A has file1 with old content. diffB modifies it. Merged tree uses B's version.
	storer := memory.NewStorage()

	blobOld := storeBlob(t, storer, "old content")
	blobNew := storeBlob(t, storer, "new content from B")

	treeA, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "file1.txt", Mode: filemode.Regular, Hash: blobOld},
	})

	treeB, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "file1.txt", Mode: filemode.Regular, Hash: blobNew},
	})

	diffB := object.Changes{
		&object.Change{
			From: object.ChangeEntry{Name: "file1.txt"},
			To:   object.ChangeEntry{Name: "file1.txt"},
		},
	}

	mergedHash, err := buildMergedTree(storer, treeA, treeB, diffB)
	if err != nil {
		t.Fatalf("buildMergedTree() error = %v", err)
	}

	mergedObj, err := storer.EncodedObject(plumbing.TreeObject, mergedHash)
	if err != nil {
		t.Fatalf("getting merged tree: %v", err)
	}
	merged := &object.Tree{}
	if err := merged.Decode(mergedObj); err != nil {
		t.Fatalf("decoding merged tree: %v", err)
	}

	if len(merged.Entries) != 1 {
		t.Fatalf("merged tree entries = %d, want 1", len(merged.Entries))
	}
	if merged.Entries[0].Hash != blobNew {
		t.Errorf("merged file hash = %s, want %s (B's version)", merged.Entries[0].Hash, blobNew)
	}
}

func TestBuildMergedTree_RenameFromB(t *testing.T) {
	// Tree A has old-name.txt. diffB renames it to new-name.txt.
	storer := memory.NewStorage()

	blob := storeBlob(t, storer, "some content")

	treeA, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "old-name.txt", Mode: filemode.Regular, Hash: blob},
	})

	treeB, _ := storeTree(t, storer, []object.TreeEntry{
		{Name: "new-name.txt", Mode: filemode.Regular, Hash: blob},
	})

	diffB := object.Changes{
		&object.Change{
			From: object.ChangeEntry{Name: "old-name.txt"},
			To:   object.ChangeEntry{Name: "new-name.txt"},
		},
	}

	mergedHash, err := buildMergedTree(storer, treeA, treeB, diffB)
	if err != nil {
		t.Fatalf("buildMergedTree() error = %v", err)
	}

	mergedObj, err := storer.EncodedObject(plumbing.TreeObject, mergedHash)
	if err != nil {
		t.Fatalf("getting merged tree: %v", err)
	}
	merged := &object.Tree{}
	if err := merged.Decode(mergedObj); err != nil {
		t.Fatalf("decoding merged tree: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range merged.Entries {
		names[e.Name] = true
	}
	if names["old-name.txt"] {
		t.Error("merged tree should not contain old-name.txt after rename")
	}
	if !names["new-name.txt"] {
		t.Error("merged tree should contain new-name.txt after rename")
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

func newFakeDynamicClient(t *testing.T, objects ...interface{}) *dynamicfake.FakeDynamicClient {
	t.Helper()
	scheme := runtime.NewScheme()
	unstructuredObjects := make([]runtime.Object, 0, len(objects))
	for _, obj := range objects {
		unstructuredObjects = append(unstructuredObjects, toUnstructuredObj(t, obj))
	}
	counter := 0
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			gvr("gitreposyncs"):        "GitRepoSyncList",
			gvr("gitpushtransactions"): "GitPushTransactionList",
			gvr("gitrepositories"):     "GitRepositoryList",
		}, unstructuredObjects...)
	// Simulate GenerateName by assigning unique names on create.
	client.PrependReactor("create", "*", func(action k8stesting.Action) (bool, runtime.Object, error) {
		createAction := action.(k8stesting.CreateAction)
		obj := createAction.GetObject().(*unstructured.Unstructured)
		if obj.GetName() == "" && obj.GetGenerateName() != "" {
			counter++
			obj.SetName(fmt.Sprintf("%s%d", obj.GetGenerateName(), counter))
		}
		return false, nil, nil
	})
	return client
}

func TestMarkManualIntervention(t *testing.T) {
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-sync",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
		Status: gitv1alpha1.GitRepoSyncStatus{
			Phase: gitv1alpha1.SyncPhaseConflicted,
		},
	}

	dynClient := newFakeDynamicClient(t, syncObj)
	gitClient := gitclient.NewFromDynamic(dynClient)
	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	err := r.markManualIntervention(context.Background(), syncObj, "cannot merge: binary conflict")
	if err != nil {
		t.Fatalf("markManualIntervention() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseRequiresManualIntervention {
		t.Errorf("phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseRequiresManualIntervention)
	}
	if syncObj.Status.Message != "cannot merge: binary conflict" {
		t.Errorf("message = %q, want %q", syncObj.Status.Message, "cannot merge: binary conflict")
	}
}

func TestCreateMergePushTransactions(t *testing.T) {
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-sync",
			Namespace: "default",
			UID:       "sync-uid-123",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}

	dynClient := newFakeDynamicClient(t, syncObj)
	gitClient := gitclient.NewFromDynamic(dynClient)
	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	err := r.createMergePushTransactions(context.Background(), "default", syncObj, "merge-commit-hash", "commit-a", "commit-b")
	if err != nil {
		t.Fatalf("createMergePushTransactions() error = %v", err)
	}

	txns, err := gitClient.GitPushTransactions("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing transactions: %v", err)
	}
	if len(txns.Items) != 2 {
		t.Fatalf("expected 2 push transactions, got %d", len(txns.Items))
	}

	targets := make(map[string]gitv1alpha1.GitPushTransaction)
	for _, txn := range txns.Items {
		targets[txn.Spec.RepositoryRef] = txn
	}

	txnA, ok := targets["repo-a"]
	if !ok {
		t.Fatal("missing transaction for repo-a")
	}
	if txnA.Spec.RefSpecs[0].Source != "merge-commit-hash" {
		t.Errorf("txnA source = %q, want %q", txnA.Spec.RefSpecs[0].Source, "merge-commit-hash")
	}
	if txnA.Spec.RefSpecs[0].ExpectedOldCommit != "commit-a" {
		t.Errorf("txnA expectedOld = %q, want %q", txnA.Spec.RefSpecs[0].ExpectedOldCommit, "commit-a")
	}
	if txnA.Labels["git-k8s.imjasonh.com/merge"] != "true" {
		t.Error("txnA missing merge label")
	}

	txnB, ok := targets["repo-b"]
	if !ok {
		t.Fatal("missing transaction for repo-b")
	}
	if txnB.Spec.RefSpecs[0].ExpectedOldCommit != "commit-b" {
		t.Errorf("txnB expectedOld = %q, want %q", txnB.Spec.RefSpecs[0].ExpectedOldCommit, "commit-b")
	}
}

func TestReconcileKind_SkipsNonConflicted(t *testing.T) {
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-sync",
			Namespace: "default",
		},
		Status: gitv1alpha1.GitRepoSyncStatus{
			Phase: gitv1alpha1.SyncPhaseInSync,
		},
	}

	dynClient := newFakeDynamicClient(t, syncObj)
	gitClient := gitclient.NewFromDynamic(dynClient)
	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseInSync {
		t.Errorf("phase changed to %q, should remain %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseInSync)
	}
}

func TestReconcileKind_MissingCommitHashes(t *testing.T) {
	syncObj := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-sync",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
		Status: gitv1alpha1.GitRepoSyncStatus{
			Phase: gitv1alpha1.SyncPhaseConflicted,
		},
	}

	repoA := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "repo-a",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo-a.git",
		},
	}

	dynClient := newFakeDynamicClient(t, syncObj, repoA)
	gitClient := gitclient.NewFromDynamic(dynClient)
	r := &Reconciler{
		dynamicClient: dynClient,
		gitClient:     gitClient,
	}

	err := r.ReconcileKind(context.Background(), syncObj)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseRequiresManualIntervention {
		t.Errorf("phase = %q, want %q", syncObj.Status.Phase, gitv1alpha1.SyncPhaseRequiresManualIntervention)
	}
}
