package gitauth

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/go-git/go-git/v5/plumbing/transport/http"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

// ResolveAuth retrieves Git authentication credentials from the Secret
// referenced by the GitRepository's auth configuration. Returns nil if
// no auth is configured.
func ResolveAuth(ctx context.Context, dynamicClient dynamic.Interface, namespace string, repo *gitv1alpha1.GitRepository) (*http.BasicAuth, error) {
	if repo.Spec.Auth == nil || repo.Spec.Auth.SecretRef == nil {
		return nil, nil
	}

	secretGVR := corev1.SchemeGroupVersion.WithResource("secrets")
	u, err := dynamicClient.Resource(secretGVR).Namespace(namespace).Get(ctx, repo.Spec.Auth.SecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting secret %q: %w", repo.Spec.Auth.SecretRef.Name, err)
	}

	data, found, err := unstructured.NestedStringMap(u.Object, "data")
	if err != nil || !found {
		return nil, fmt.Errorf("reading secret data: found=%v err=%v", found, err)
	}

	usernameB64, ok := data["username"]
	if !ok {
		return nil, fmt.Errorf("secret %q is missing required key %q", repo.Spec.Auth.SecretRef.Name, "username")
	}
	passwordB64, ok := data["password"]
	if !ok {
		return nil, fmt.Errorf("secret %q is missing required key %q", repo.Spec.Auth.SecretRef.Name, "password")
	}

	username, err := base64.StdEncoding.DecodeString(usernameB64)
	if err != nil {
		return nil, fmt.Errorf("decoding username: %w", err)
	}
	password, err := base64.StdEncoding.DecodeString(passwordB64)
	if err != nil {
		return nil, fmt.Errorf("decoding password: %w", err)
	}

	return &http.BasicAuth{
		Username: string(username),
		Password: string(password),
	}, nil
}
