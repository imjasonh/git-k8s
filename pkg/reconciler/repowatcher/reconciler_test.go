package repowatcher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

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
		switch v := obj.(type) {
		case *unstructured.Unstructured:
			unstructuredObjects = append(unstructuredObjects, v)
		default:
			unstructuredObjects = append(unstructuredObjects, toUnstructuredObj(t, obj))
		}
	}
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			gvr("gitbranches"):     "GitBranchList",
			gvr("gitrepositories"): "GitRepositoryList",
		}, unstructuredObjects...)
}

func TestEnqueueAfter_NilImpl(t *testing.T) {
	// When impl is nil, enqueueAfter should be a no-op (not panic).
	r := &Reconciler{}
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
		},
	}
	// Should not panic.
	r.enqueueAfter(repo, 5*time.Second)
}

func TestSetImpl(t *testing.T) {
	r := &Reconciler{}
	if r.impl != nil {
		t.Error("impl should be nil initially")
	}
	// SetImpl with nil is valid (just stores nil).
	r.SetImpl(nil)
	if r.impl != nil {
		t.Error("impl should be nil after SetImpl(nil)")
	}
}

func TestResolveAuth_NoAuth(t *testing.T) {
	dynClient := newFakeDynamicClient(t)
	r := &Reconciler{
		dynamicClient: dynClient,
	}

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
		t.Error("auth should be nil when no auth is configured")
	}
}

func TestResolveAuth_NilSecretRef(t *testing.T) {
	dynClient := newFakeDynamicClient(t)
	r := &Reconciler{
		dynamicClient: dynClient,
	}

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:  "https://github.com/example/repo.git",
			Auth: &gitv1alpha1.GitAuth{},
		},
	}

	auth, err := r.resolveAuth(context.Background(), "default", repo)
	if err != nil {
		t.Fatalf("resolveAuth() error = %v", err)
	}
	if auth != nil {
		t.Error("auth should be nil when SecretRef is nil")
	}
}

func TestResolveAuth_WithSecret(t *testing.T) {
	// Create the secret as an unstructured object.
	secret := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]interface{}{
				"name":      "my-creds",
				"namespace": "default",
			},
			"data": map[string]interface{}{
				"username": base64.StdEncoding.EncodeToString([]byte("admin")),
				"password": base64.StdEncoding.EncodeToString([]byte("s3cret")),
			},
		},
	}

	dynClient := newFakeDynamicClient(t, secret)
	r := &Reconciler{
		dynamicClient: dynClient,
	}

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "my-creds"},
			},
		},
	}

	auth, err := r.resolveAuth(context.Background(), "default", repo)
	if err != nil {
		t.Fatalf("resolveAuth() error = %v", err)
	}
	if auth == nil {
		t.Fatal("auth should not be nil when secret exists")
	}
	if auth.Username != "admin" {
		t.Errorf("username = %q, want %q", auth.Username, "admin")
	}
	if auth.Password != "s3cret" {
		t.Errorf("password = %q, want %q", auth.Password, "s3cret")
	}
}

func TestReconcileKind_CreatesAndDeletesBranches(t *testing.T) {

	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
			UID:       "repo-uid-123",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
		},
	}

	// Pre-existing branch that should get deleted (no longer on remote).
	staleBranch := &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo-stale",
			Namespace: "default",
			Labels: map[string]string{
				"git-k8s.imjasonh.com/repository": "my-repo",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
					Kind:       "GitRepository",
					Name:       "my-repo",
					UID:        "repo-uid-123",
				},
			},
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "my-repo",
			BranchName:    "stale",
		},
		Status: gitv1alpha1.GitBranchStatus{
			HeadCommit: "old-stale-commit",
		},
	}

	dynClient := newFakeDynamicClient(t, repo, staleBranch)
	gitClient := gitclient.NewFromDynamic(dynClient)

	r := &Reconciler{
		dynamicClient:   dynClient,
		gitClient:       gitClient,
		defaultInterval: 30 * time.Second,
		lsRemote: func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
			return []*plumbing.Reference{
				plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aabbccdd")),
				plumbing.NewHashReference(plumbing.NewBranchReferenceName("develop"), plumbing.NewHash("eeff0011")),
			}, nil
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Verify branches were created.
	branches, err := gitClient.GitBranches("default").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("listing branches: %v", err)
	}

	// stale branch should be deleted, main and develop should be created.
	branchNames := make(map[string]bool)
	for _, b := range branches.Items {
		branchNames[b.Spec.BranchName] = true
	}

	if branchNames["stale"] {
		t.Error("stale branch should have been deleted")
	}
	if !branchNames["main"] {
		t.Error("main branch should have been created")
	}
	if !branchNames["develop"] {
		t.Error("develop branch should have been created")
	}

	// Verify repo status was updated.
	if repo.Status.LastFetchTime == nil {
		t.Error("LastFetchTime should be set after reconcile")
	}
}

func TestReconcileKind_SkipsRecentlyFetched(t *testing.T) {

	now := metav1.Now()
	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
		},
		Status: gitv1alpha1.GitRepositoryStatus{
			LastFetchTime: &now,
		},
	}

	dynClient := newFakeDynamicClient(t, repo)
	gitClient := gitclient.NewFromDynamic(dynClient)

	lsRemoteCalled := false
	r := &Reconciler{
		dynamicClient:   dynClient,
		gitClient:       gitClient,
		defaultInterval: 30 * time.Second,
		lsRemote: func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
			lsRemoteCalled = true
			return nil, nil
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}
	if lsRemoteCalled {
		t.Error("lsRemote should not be called when poll interval has not elapsed")
	}
}

func TestReconcileKind_UpdatesExistingBranch(t *testing.T) {

	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo",
			Namespace: "default",
			UID:       "repo-uid-123",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://github.com/example/repo.git",
		},
	}

	// Pre-existing branch with old commit.
	existingBranch := &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-repo-main",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{UID: "repo-uid-123"},
			},
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "my-repo",
			BranchName:    "main",
		},
		Status: gitv1alpha1.GitBranchStatus{
			HeadCommit: "old-commit",
		},
	}

	dynClient := newFakeDynamicClient(t, repo, existingBranch)
	gitClient := gitclient.NewFromDynamic(dynClient)

	r := &Reconciler{
		dynamicClient:   dynClient,
		gitClient:       gitClient,
		defaultInterval: 30 * time.Second,
		lsRemote: func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
			return []*plumbing.Reference{
				plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("new-commit-sha-1234567890abcdef")),
			}, nil
		},
	}

	err := r.ReconcileKind(context.Background(), repo)
	if err != nil {
		t.Fatalf("ReconcileKind() error = %v", err)
	}

	// Verify the branch was updated.
	updated, err := gitClient.GitBranches("default").Get(context.Background(), "my-repo-main", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting updated branch: %v", err)
	}
	if updated.Status.HeadCommit == "old-commit" {
		t.Error("HeadCommit should have been updated from old-commit")
	}
}
