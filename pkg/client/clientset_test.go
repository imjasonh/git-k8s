package client

import (
	"context"
	"encoding/json"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

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
