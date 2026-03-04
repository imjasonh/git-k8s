package client

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
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

func newFakeClient(objects ...runtime.Object) *GitV1alpha1Client {
	dynClient := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(newFakeScheme(), customListKinds, objects...)
	return NewFromDynamic(dynClient)
}

func TestToUnstructured(t *testing.T) {
	repo := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "git-k8s.imjasonh.com/v1alpha1",
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           "https://github.com/example/repo.git",
			DefaultBranch: "main",
		},
	}

	u, err := toUnstructured(repo)
	if err != nil {
		t.Fatalf("toUnstructured() error = %v", err)
	}

	// Verify key fields in the unstructured representation.
	if got := u.GetName(); got != "test-repo" {
		t.Errorf("Name = %q, want %q", got, "test-repo")
	}
	if got := u.GetNamespace(); got != "default" {
		t.Errorf("Namespace = %q, want %q", got, "default")
	}

	spec, ok := u.Object["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("spec field not found or wrong type")
	}
	if got := spec["url"]; got != "https://github.com/example/repo.git" {
		t.Errorf("spec.url = %v, want %q", got, "https://github.com/example/repo.git")
	}
}

func TestFromUnstructured(t *testing.T) {
	u := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "git-k8s.imjasonh.com/v1alpha1",
			"kind":       "GitRepository",
			"metadata": map[string]interface{}{
				"name":      "test-repo",
				"namespace": "default",
			},
			"spec": map[string]interface{}{
				"url":           "https://github.com/example/repo.git",
				"defaultBranch": "main",
			},
		},
	}

	repo := &gitv1alpha1.GitRepository{}
	if err := fromUnstructured(u, repo); err != nil {
		t.Fatalf("fromUnstructured() error = %v", err)
	}

	if got := repo.Name; got != "test-repo" {
		t.Errorf("Name = %q, want %q", got, "test-repo")
	}
	if got := repo.Spec.URL; got != "https://github.com/example/repo.git" {
		t.Errorf("URL = %q, want %q", got, "https://github.com/example/repo.git")
	}
	if got := repo.Spec.DefaultBranch; got != "main" {
		t.Errorf("DefaultBranch = %q, want %q", got, "main")
	}
}

func TestRoundTrip_GitRepository(t *testing.T) {
	orig := &gitv1alpha1.GitRepository{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepository",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "round-trip",
			Namespace: "test-ns",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           "https://github.com/test/repo.git",
			DefaultBranch: "develop",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "creds"},
			},
		},
	}

	u, err := toUnstructured(orig)
	if err != nil {
		t.Fatalf("toUnstructured() error = %v", err)
	}

	result := &gitv1alpha1.GitRepository{}
	if err := fromUnstructured(u, result); err != nil {
		t.Fatalf("fromUnstructured() error = %v", err)
	}

	if result.Name != orig.Name {
		t.Errorf("Name = %q, want %q", result.Name, orig.Name)
	}
	if result.Spec.URL != orig.Spec.URL {
		t.Errorf("URL = %q, want %q", result.Spec.URL, orig.Spec.URL)
	}
	if result.Spec.DefaultBranch != orig.Spec.DefaultBranch {
		t.Errorf("DefaultBranch = %q, want %q", result.Spec.DefaultBranch, orig.Spec.DefaultBranch)
	}
	if result.Spec.Auth == nil || result.Spec.Auth.SecretRef == nil {
		t.Fatal("Auth.SecretRef is nil after round trip")
	}
	if result.Spec.Auth.SecretRef.Name != "creds" {
		t.Errorf("SecretRef.Name = %q, want %q", result.Spec.Auth.SecretRef.Name, "creds")
	}
}

func TestRoundTrip_GitPushTransaction(t *testing.T) {
	orig := &gitv1alpha1.GitPushTransaction{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitPushTransaction",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "push-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			Atomic:        true,
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:            "refs/heads/main",
					Destination:       "refs/heads/feature",
					ExpectedOldCommit: "abc123",
				},
				{
					Source:      "refs/heads/develop",
					Destination: "refs/heads/staging",
				},
			},
		},
	}

	u, err := toUnstructured(orig)
	if err != nil {
		t.Fatalf("toUnstructured() error = %v", err)
	}

	result := &gitv1alpha1.GitPushTransaction{}
	if err := fromUnstructured(u, result); err != nil {
		t.Fatalf("fromUnstructured() error = %v", err)
	}

	if result.Spec.RepositoryRef != "my-repo" {
		t.Errorf("RepositoryRef = %q, want %q", result.Spec.RepositoryRef, "my-repo")
	}
	if !result.Spec.Atomic {
		t.Error("Atomic = false, want true")
	}
	if len(result.Spec.RefSpecs) != 2 {
		t.Fatalf("RefSpecs length = %d, want 2", len(result.Spec.RefSpecs))
	}
	if result.Spec.RefSpecs[0].ExpectedOldCommit != "abc123" {
		t.Errorf("RefSpecs[0].ExpectedOldCommit = %q, want %q", result.Spec.RefSpecs[0].ExpectedOldCommit, "abc123")
	}
	if result.Spec.RefSpecs[1].ExpectedOldCommit != "" {
		t.Errorf("RefSpecs[1].ExpectedOldCommit = %q, want empty", result.Spec.RefSpecs[1].ExpectedOldCommit)
	}
}

func TestRoundTrip_GitRepoSync(t *testing.T) {
	orig := &gitv1alpha1.GitRepoSync{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitRepoSync",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}

	u, err := toUnstructured(orig)
	if err != nil {
		t.Fatalf("toUnstructured() error = %v", err)
	}

	result := &gitv1alpha1.GitRepoSync{}
	if err := fromUnstructured(u, result); err != nil {
		t.Fatalf("fromUnstructured() error = %v", err)
	}

	if result.Spec.RepoA.Name != "repo-a" {
		t.Errorf("RepoA.Name = %q, want %q", result.Spec.RepoA.Name, "repo-a")
	}
	if result.Spec.RepoB.Name != "repo-b" {
		t.Errorf("RepoB.Name = %q, want %q", result.Spec.RepoB.Name, "repo-b")
	}
	if result.Spec.BranchName != "main" {
		t.Errorf("BranchName = %q, want %q", result.Spec.BranchName, "main")
	}
}

func TestContextInjection(t *testing.T) {
	client := &GitV1alpha1Client{}
	ctx := context.Background()

	ctx = WithClient(ctx, client)
	got := Get(ctx)
	if got != client {
		t.Error("Get(ctx) returned different client than WithClient stored")
	}
}

func TestGVRConstants(t *testing.T) {
	tests := []struct {
		name     string
		gvr      interface{ String() string }
		resource string
	}{
		{"gitrepositories", gitRepositoryGVR, "gitrepositories"},
		{"gitbranches", gitBranchGVR, "gitbranches"},
		{"gitpushtransactions", gitPushTransactionGVR, "gitpushtransactions"},
		{"gitreposyncs", gitRepoSyncGVR, "gitreposyncs"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.gvr.String()
			want := gitv1alpha1.GroupName + "/" + gitv1alpha1.Version
			if got != want+", Resource="+tt.resource {
				t.Errorf("GVR = %q, want group/version = %q resource = %q", got, want, tt.resource)
			}
		})
	}
}

func TestToUnstructured_PreservesJSON(t *testing.T) {
	branch := &gitv1alpha1.GitBranch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
			Kind:       "GitBranch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-branch",
			Namespace: "ns",
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "my-repo",
			BranchName:    "feature/awesome",
		},
	}

	u, err := toUnstructured(branch)
	if err != nil {
		t.Fatalf("toUnstructured() error = %v", err)
	}

	// Verify the JSON is valid by marshalling back.
	data, err := json.Marshal(u.Object)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var roundTrip map[string]interface{}
	if err := json.Unmarshal(data, &roundTrip); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	spec, ok := roundTrip["spec"].(map[string]interface{})
	if !ok {
		t.Fatal("spec not found in roundTrip")
	}
	if got := spec["repositoryRef"]; got != "my-repo" {
		t.Errorf("repositoryRef = %v, want %q", got, "my-repo")
	}
	if got := spec["branchName"]; got != "feature/awesome" {
		t.Errorf("branchName = %v, want %q", got, "feature/awesome")
	}
}

func TestNewFromDynamic(t *testing.T) {
	dynClient := fakedynamic.NewSimpleDynamicClient(newFakeScheme())
	gc := NewFromDynamic(dynClient)
	if gc == nil {
		t.Fatal("NewFromDynamic returned nil")
	}
	if gc.client != dynClient {
		t.Error("NewFromDynamic did not store the dynamic client")
	}
}

func TestGitRepositories_CRUD(t *testing.T) {
	c := newFakeClient()
	ctx := context.Background()

	// Create.
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           "https://example.com/repo.git",
			DefaultBranch: "main",
		},
	}
	created, err := c.GitRepositories("default").Create(ctx, repo, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Name != "test-repo" {
		t.Errorf("Name = %q, want %q", created.Name, "test-repo")
	}

	// Get.
	got, err := c.GitRepositories("default").Get(ctx, "test-repo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Spec.URL != "https://example.com/repo.git" {
		t.Errorf("URL = %q, want %q", got.Spec.URL, "https://example.com/repo.git")
	}

	// Update.
	got.Spec.DefaultBranch = "develop"
	updated, err := c.GitRepositories("default").Update(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if updated.Spec.DefaultBranch != "develop" {
		t.Errorf("DefaultBranch = %q, want %q", updated.Spec.DefaultBranch, "develop")
	}

	// UpdateStatus.
	now := metav1.Now()
	updated.Status.LastFetchTime = &now
	statusUpdated, err := c.GitRepositories("default").UpdateStatus(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	if statusUpdated.Status.LastFetchTime == nil {
		t.Error("LastFetchTime should be set")
	}

	// List.
	list, err := c.GitRepositories("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("List Items = %d, want 1", len(list.Items))
	}

	// Delete.
	if err := c.GitRepositories("default").Delete(ctx, "test-repo", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	list, err = c.GitRepositories("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Items) != 0 {
		t.Errorf("List after Delete = %d, want 0", len(list.Items))
	}
}

func TestGitBranches_CRUD(t *testing.T) {
	c := newFakeClient()
	ctx := context.Background()

	branch := &gitv1alpha1.GitBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-branch",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: "my-repo",
			BranchName:    "main",
		},
	}

	created, err := c.GitBranches("default").Create(ctx, branch, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Spec.BranchName != "main" {
		t.Errorf("BranchName = %q, want %q", created.Spec.BranchName, "main")
	}

	got, err := c.GitBranches("default").Get(ctx, "test-branch", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	got.Status.HeadCommit = "abc123"
	_, err = c.GitBranches("default").UpdateStatus(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	got.Spec.BranchName = "develop"
	_, err = c.GitBranches("default").Update(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	list, err := c.GitBranches("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("List Items = %d, want 1", len(list.Items))
	}

	if err := c.GitBranches("default").Delete(ctx, "test-branch", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestGitPushTransactions_CRUD(t *testing.T) {
	c := newFakeClient()
	ctx := context.Background()

	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "push-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			Atomic:        true,
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{Source: "refs/heads/main", Destination: "refs/heads/main"},
			},
		},
	}

	created, err := c.GitPushTransactions("default").Create(ctx, txn, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !created.Spec.Atomic {
		t.Error("Atomic should be true")
	}

	got, err := c.GitPushTransactions("default").Get(ctx, "push-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	got.Status.Phase = gitv1alpha1.TransactionPhaseSucceeded
	_, err = c.GitPushTransactions("default").UpdateStatus(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	got.Spec.Atomic = false
	_, err = c.GitPushTransactions("default").Update(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	list, err := c.GitPushTransactions("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("List Items = %d, want 1", len(list.Items))
	}

	if err := c.GitPushTransactions("default").Delete(ctx, "push-1", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}

func TestGitRepoSyncs_CRUD(t *testing.T) {
	c := newFakeClient()
	ctx := context.Background()

	sync := &gitv1alpha1.GitRepoSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sync-1",
			Namespace: "default",
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
	}

	created, err := c.GitRepoSyncs("default").Create(ctx, sync, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Spec.BranchName != "main" {
		t.Errorf("BranchName = %q, want %q", created.Spec.BranchName, "main")
	}

	got, err := c.GitRepoSyncs("default").Get(ctx, "sync-1", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	got.Status.Phase = gitv1alpha1.SyncPhaseInSync
	_, err = c.GitRepoSyncs("default").UpdateStatus(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}

	got.Spec.BranchName = "develop"
	_, err = c.GitRepoSyncs("default").Update(ctx, got, metav1.UpdateOptions{})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	list, err := c.GitRepoSyncs("default").List(ctx, metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("List Items = %d, want 1", len(list.Items))
	}

	if err := c.GitRepoSyncs("default").Delete(ctx, "sync-1", metav1.DeleteOptions{}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
}
