package gitauth

import (
	"context"
	"encoding/base64"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func newFakeSecret(name, namespace, username, password string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion("v1")
	obj.SetKind("Secret")
	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.Object["data"] = map[string]interface{}{
		"username": base64.StdEncoding.EncodeToString([]byte(username)),
		"password": base64.StdEncoding.EncodeToString([]byte(password)),
	}
	return obj
}

func TestResolveAuth_NoAuth(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fakedynamic.NewSimpleDynamicClient(scheme)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{URL: "https://example.com/repo.git"},
	}
	auth, err := ResolveAuth(context.Background(), client, "default", repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != nil {
		t.Fatal("expected nil auth for repo without auth config")
	}
}

func TestResolveAuth_NoSecretRef(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fakedynamic.NewSimpleDynamicClient(scheme)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:  "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{},
		},
	}
	auth, err := ResolveAuth(context.Background(), client, "default", repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth != nil {
		t.Fatal("expected nil auth for repo with nil secretRef")
	}
}

func TestResolveAuth_ValidSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	secret := newFakeSecret("my-secret", "default", "myuser", "mypass")
	client := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "secrets"}: "SecretList",
		},
		secret,
	)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "my-secret"},
			},
		},
	}
	auth, err := ResolveAuth(context.Background(), client, "default", repo)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth == nil {
		t.Fatal("expected non-nil auth")
	}
	if auth.Username != "myuser" {
		t.Errorf("username = %q, want %q", auth.Username, "myuser")
	}
	if auth.Password != "mypass" {
		t.Errorf("password = %q, want %q", auth.Password, "mypass")
	}
}

func TestResolveAuth_MissingUsernameKey(t *testing.T) {
	scheme := runtime.NewScheme()
	secret := &unstructured.Unstructured{}
	secret.SetAPIVersion("v1")
	secret.SetKind("Secret")
	secret.SetName("bad-secret")
	secret.SetNamespace("default")
	secret.Object["data"] = map[string]interface{}{
		"password": base64.StdEncoding.EncodeToString([]byte("pass")),
	}
	client := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "secrets"}: "SecretList",
		},
		secret,
	)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "bad-secret"},
			},
		},
	}
	_, err := ResolveAuth(context.Background(), client, "default", repo)
	if err == nil {
		t.Fatal("expected error for missing username key")
	}
	if got := err.Error(); !contains(got, "missing required key") {
		t.Errorf("error = %q, want to contain %q", got, "missing required key")
	}
}

func TestResolveAuth_MissingPasswordKey(t *testing.T) {
	scheme := runtime.NewScheme()
	secret := &unstructured.Unstructured{}
	secret.SetAPIVersion("v1")
	secret.SetKind("Secret")
	secret.SetName("bad-secret")
	secret.SetNamespace("default")
	secret.Object["data"] = map[string]interface{}{
		"username": base64.StdEncoding.EncodeToString([]byte("user")),
	}
	client := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "secrets"}: "SecretList",
		},
		secret,
	)

	repo := &gitv1alpha1.GitRepository{
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "bad-secret"},
			},
		},
	}
	_, err := ResolveAuth(context.Background(), client, "default", repo)
	if err == nil {
		t.Fatal("expected error for missing password key")
	}
	if got := err.Error(); !contains(got, "missing required key") {
		t.Errorf("error = %q, want to contain %q", got, "missing required key")
	}
}

func TestResolveAuth_SecretNotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	client := fakedynamic.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			{Group: "", Version: "v1", Resource: "secrets"}: "SecretList",
		},
	)

	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{Name: "repo", Namespace: "default"},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL: "https://example.com/repo.git",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: "nonexistent"},
			},
		},
	}
	_, err := ResolveAuth(context.Background(), client, "default", repo)
	if err == nil {
		t.Fatal("expected error for missing secret")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
