package repowatcher

import (
	"context"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
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
	// Core types for secrets.
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

func newFakeReconciler(t *testing.T, lsRemoteFn func(string, *http.BasicAuth) ([]*plumbing.Reference, error), objects ...*unstructured.Unstructured) *Reconciler {
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
		dynamicClient:   dynClient,
		gitClient:       gc,
		defaultInterval: 30 * time.Second,
		lsRemote:        lsRemoteFn,
	}
}

func TestBranchCRDName(t *testing.T) {
	tests := []struct {
		repo   string
		branch string
		want   string
	}{
		{"my-repo", "main", "my-repo-main"},
		{"my-repo", "feature/foo", "my-repo-feature-foo"},
		{"my-repo", "release/v1.0", "my-repo-release-v1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := branchCRDName(tt.repo, tt.branch)
			if got != tt.want {
				t.Errorf("branchCRDName(%q, %q) = %q, want %q", tt.repo, tt.branch, got, tt.want)
			}
		})
	}
}

func TestPollInterval(t *testing.T) {
	r := &Reconciler{defaultInterval: 30 * time.Second}

	// No override — use default.
	repo := &gitv1alpha1.GitRepository{}
	if got := r.pollInterval(repo); got != 30*time.Second {
		t.Errorf("pollInterval (default) = %v, want 30s", got)
	}

	// Per-repo override.
	d := metav1.Duration{Duration: 5 * time.Second}
	repo.Spec.PollInterval = &d
	if got := r.pollInterval(repo); got != 5*time.Second {
		t.Errorf("pollInterval (override) = %v, want 5s", got)
	}
}

func TestMinLen(t *testing.T) {
	if got := minLen(10, 7); got != 7 {
		t.Errorf("minLen(10, 7) = %d, want 7", got)
	}
	if got := minLen(3, 7); got != 3 {
		t.Errorf("minLen(3, 7) = %d, want 3", got)
	}
}

func TestMockLsRemote(t *testing.T) {
	// Verify the mock lsRemote pattern works.
	refs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aaaa")),
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("feature/foo"), plumbing.NewHash("bbbb")),
		plumbing.NewHashReference(plumbing.NewTagReferenceName("v1.0"), plumbing.NewHash("cccc")),
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main")),
	}

	mockLsRemote := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return refs, nil
	}

	got, err := mockLsRemote("http://example.com/repo.git", nil)
	if err != nil {
		t.Fatalf("mockLsRemote() error = %v", err)
	}

	// Count branch refs.
	branches := 0
	for _, ref := range got {
		if ref.Name().IsBranch() {
			branches++
		}
	}
	if branches != 2 {
		t.Errorf("branch count = %d, want 2", branches)
	}
}

func TestIsOwnedBy(t *testing.T) {
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-repo",
			UID:  "repo-uid-123",
		},
	}

	// Branch with matching owner reference.
	ownedBranch := &gitv1alpha1.GitBranch{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
					Kind:       "GitRepository",
					Name:       "my-repo",
					UID:        "repo-uid-123",
				},
			},
		},
	}
	if !isOwnedBy(ownedBranch, repo) {
		t.Error("isOwnedBy should return true for owned branch")
	}

	// Branch with no owner reference (manually created).
	manualBranch := &gitv1alpha1.GitBranch{}
	if isOwnedBy(manualBranch, repo) {
		t.Error("isOwnedBy should return false for branch without owner reference")
	}

	// Branch owned by a different repo.
	otherBranch := &gitv1alpha1.GitBranch{
		ObjectMeta: metav1.ObjectMeta{
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: "other-uid-456",
				},
			},
		},
	}
	if isOwnedBy(otherBranch, repo) {
		t.Error("isOwnedBy should return false for branch owned by different repo")
	}
}

func TestRefFiltering(t *testing.T) {
	// Simulate what the reconciler does: filter ls-remote output to branches only.
	refs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aaaa")),
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("develop"), plumbing.NewHash("bbbb")),
		plumbing.NewHashReference(plumbing.NewTagReferenceName("v1.0"), plumbing.NewHash("cccc")),
		plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", "main"), plumbing.NewHash("dddd")),
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main")),
	}

	remoteBranches := make(map[string]string)
	for _, ref := range refs {
		name := ref.Name()
		if name.IsBranch() {
			remoteBranches[name.Short()] = ref.Hash().String()
		}
	}

	if len(remoteBranches) != 2 {
		t.Errorf("filtered branches = %d, want 2", len(remoteBranches))
	}
	if _, ok := remoteBranches["main"]; !ok {
		t.Error("missing branch 'main'")
	}
	if _, ok := remoteBranches["develop"]; !ok {
		t.Error("missing branch 'develop'")
	}
	// Tags and remote refs should not be included.
	if _, ok := remoteBranches["v1.0"]; ok {
		t.Error("tag v1.0 should not be in branches")
	}
}

func TestEnqueueAfter_NilImpl(t *testing.T) {
	// enqueueAfter with nil impl should not panic.
	r := &Reconciler{}
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
		},
	}
	r.enqueueAfter(repo, 5*time.Second) // should not panic
}

func TestReconcileKind_CreatesNewBranches(t *testing.T) {
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata": map[string]interface{}{
				"name":      "my-repo",
				"namespace": "default",
				"uid":       "repo-uid",
			},
			"spec": map[string]interface{}{
				"url":           "https://example.com/repo.git",
				"defaultBranch": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	mockRefs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aaa111aaa111aaa111aaa111aaa111aaa111aaa1")),
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("develop"), plumbing.NewHash("bbb222bbb222bbb222bbb222bbb222bbb222bbb2")),
	}

	lsRemoteFn := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return mockRefs, nil
	}

	r := newFakeReconciler(t, lsRemoteFn, repoObj)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
			UID:       "repo-uid",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           "https://example.com/repo.git",
			DefaultBranch: "main",
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Verify branches were created.
	branches, err := r.gitClient.GitBranches("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}
	if len(branches.Items) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(branches.Items))
	}

	// Verify repo status was updated.
	if repo.Status.LastFetchTime == nil {
		t.Error("LastFetchTime should be set after successful poll")
	}
}

func TestReconcileKind_UpdatesExistingBranch(t *testing.T) {
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata": map[string]interface{}{
				"name":      "my-repo",
				"namespace": "default",
				"uid":       "repo-uid",
			},
			"spec": map[string]interface{}{
				"url":           "https://example.com/repo.git",
				"defaultBranch": "main",
			},
			"status": map[string]interface{}{},
		},
	}

	existingBranch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "my-repo-main",
				"namespace": "default",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
						"kind":       "GitRepository",
						"name":       "my-repo",
						"uid":        "repo-uid",
					},
				},
			},
			"spec": map[string]interface{}{
				"repositoryRef": "my-repo",
				"branchName":    "main",
			},
			"status": map[string]interface{}{
				"headCommit": "ccdd00ccdd00ccdd00ccdd00ccdd00ccdd00ccdd",
			},
		},
	}

	newSHA := "aabb00aabb00aabb00aabb00aabb00aabb00aabb"
	mockRefs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash(newSHA)),
	}

	lsRemoteFn := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return mockRefs, nil
	}

	r := newFakeReconciler(t, lsRemoteFn, repoObj, existingBranch)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
			UID:       "repo-uid",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Verify branch was updated.
	branch, err := r.gitClient.GitBranches("default").Get(context.Background(), "my-repo-main", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting branch: %v", err)
	}
	if branch.Status.HeadCommit != newSHA {
		t.Errorf("HeadCommit = %q, want %q", branch.Status.HeadCommit, newSHA)
	}
}

func TestReconcileKind_DeletesStaleBranch(t *testing.T) {
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata": map[string]interface{}{
				"name":      "my-repo",
				"namespace": "default",
				"uid":       "repo-uid",
			},
			"spec": map[string]interface{}{
				"url": "https://example.com/repo.git",
			},
			"status": map[string]interface{}{},
		},
	}

	staleBranch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "my-repo-old-branch",
				"namespace": "default",
				"ownerReferences": []interface{}{
					map[string]interface{}{
						"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
						"kind":       "GitRepository",
						"name":       "my-repo",
						"uid":        "repo-uid",
					},
				},
			},
			"spec": map[string]interface{}{
				"repositoryRef": "my-repo",
				"branchName":    "old-branch",
			},
			"status": map[string]interface{}{
				"headCommit": "old-sha",
			},
		},
	}

	// Remote only has "main", but the CRD has "old-branch" -> should be deleted.
	mockRefs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aaa111aaa111aaa111aaa111aaa111aaa111aaa1")),
	}

	lsRemoteFn := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return mockRefs, nil
	}

	r := newFakeReconciler(t, lsRemoteFn, repoObj, staleBranch)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
			UID:       "repo-uid",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Verify stale branch was deleted.
	branches, err := r.gitClient.GitBranches("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}

	for _, b := range branches.Items {
		if b.Spec.BranchName == "old-branch" {
			t.Error("stale branch 'old-branch' should have been deleted")
		}
	}
}

func TestReconcileKind_SkipsRecentlyFetched(t *testing.T) {
	lsRemoteCalled := false
	lsRemoteFn := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		lsRemoteCalled = true
		return nil, nil
	}

	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata": map[string]interface{}{
				"name":      "my-repo",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"url": "https://example.com/repo.git",
			},
			"status": map[string]interface{}{},
		},
	}

	r := newFakeReconciler(t, lsRemoteFn, repoObj)

	now := metav1.Now()
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
		},
		Status: gitv1alpha1.GitRepositoryStatus{
			LastFetchTime: &now, // Just fetched.
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	if lsRemoteCalled {
		t.Error("lsRemote should NOT be called when last fetch was recent")
	}
}

func TestReconcileKind_SkipsManualBranch(t *testing.T) {
	// A branch not owned by the repo should NOT be deleted even if missing from remote.
	repoObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata": map[string]interface{}{
				"name":      "my-repo",
				"namespace": "default",
				"uid":       "repo-uid",
			},
			"spec": map[string]interface{}{
				"url": "https://example.com/repo.git",
			},
			"status": map[string]interface{}{},
		},
	}

	manualBranch := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitBranch",
			"metadata": map[string]interface{}{
				"name":      "my-repo-manual",
				"namespace": "default",
				// No ownerReferences — manual branch.
			},
			"spec": map[string]interface{}{
				"repositoryRef": "my-repo",
				"branchName":    "manual",
			},
			"status": map[string]interface{}{
				"headCommit": "sha",
			},
		},
	}

	// Remote has no branches at all.
	lsRemoteFn := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return []*plumbing.Reference{}, nil
	}

	r := newFakeReconciler(t, lsRemoteFn, repoObj, manualBranch)

	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
			UID:       "repo-uid",
		},
		Spec: gitv1alpha1.GitRepositorySpec{URL: "https://example.com/repo.git"},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Manual branch should still exist.
	branches, err := r.gitClient.GitBranches("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}
	if len(branches.Items) != 1 {
		t.Fatalf("expected 1 branch (manual), got %d", len(branches.Items))
	}
	if branches.Items[0].Spec.BranchName != "manual" {
		t.Errorf("expected manual branch to survive, got %q", branches.Items[0].Spec.BranchName)
	}
}
