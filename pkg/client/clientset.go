// Package client provides a typed Kubernetes client for the git-k8s.imjasonh.com API group.
//
// In a full Knative injection setup, this package would be auto-generated and
// clients would be retrieved from context via injection (e.g., gitclient.Get(ctx)).
// For now, we provide a manual typed client built on top of the dynamic client.
package client

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

// GitV1alpha1Client provides typed access to git-k8s.imjasonh.com/v1alpha1 resources.
type GitV1alpha1Client struct {
	client dynamic.Interface
}

// NewForConfig creates a new GitV1alpha1Client for the given config.
func NewForConfig(config *rest.Config) (*GitV1alpha1Client, error) {
	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("creating dynamic client: %w", err)
	}
	return &GitV1alpha1Client{client: dynClient}, nil
}

// NewFromDynamic creates a new GitV1alpha1Client from an existing dynamic client.
func NewFromDynamic(client dynamic.Interface) *GitV1alpha1Client {
	return &GitV1alpha1Client{client: client}
}

type contextKey struct{}

// WithClient returns a new context with the given client stored.
func WithClient(ctx context.Context, client *GitV1alpha1Client) context.Context {
	return context.WithValue(ctx, contextKey{}, client)
}

// Get retrieves the GitV1alpha1Client from the context.
func Get(ctx context.Context) *GitV1alpha1Client {
	return ctx.Value(contextKey{}).(*GitV1alpha1Client)
}

var (
	gitRepositoryGVR = schema.GroupVersionResource{
		Group:    gitv1alpha1.GroupName,
		Version:  gitv1alpha1.Version,
		Resource: "gitrepositories",
	}
	gitBranchGVR = schema.GroupVersionResource{
		Group:    gitv1alpha1.GroupName,
		Version:  gitv1alpha1.Version,
		Resource: "gitbranches",
	}
	gitPushTransactionGVR = schema.GroupVersionResource{
		Group:    gitv1alpha1.GroupName,
		Version:  gitv1alpha1.Version,
		Resource: "gitpushtransactions",
	}
	gitRepoSyncGVR = schema.GroupVersionResource{
		Group:    gitv1alpha1.GroupName,
		Version:  gitv1alpha1.Version,
		Resource: "gitreposyncs",
	}
)

// toUnstructured converts a typed object to unstructured.
func toUnstructured(obj interface{}) (*unstructured.Unstructured, error) {
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	u := &unstructured.Unstructured{}
	if err := json.Unmarshal(data, u); err != nil {
		return nil, err
	}
	return u, nil
}

// fromUnstructured converts an unstructured object to a typed object.
func fromUnstructured(u *unstructured.Unstructured, obj interface{}) error {
	data, err := json.Marshal(u.Object)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

// GitRepositories returns a client for GitRepository resources in the given namespace.
func (c *GitV1alpha1Client) GitRepositories(namespace string) *GitRepositoryClient {
	return &GitRepositoryClient{
		client:    c.client.Resource(gitRepositoryGVR).Namespace(namespace),
		namespace: namespace,
	}
}

// GitRepositoryClient provides typed operations on GitRepository resources.
type GitRepositoryClient struct {
	client    dynamic.ResourceInterface
	namespace string
}

// Create creates a new GitRepository.
func (c *GitRepositoryClient) Create(ctx context.Context, obj *gitv1alpha1.GitRepository, opts metav1.CreateOptions) (*gitv1alpha1.GitRepository, error) {
	obj.APIVersion = gitv1alpha1.SchemeGroupVersion.String()
	obj.Kind = "GitRepository"
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Create(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepository{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get retrieves a GitRepository by name.
func (c *GitRepositoryClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*gitv1alpha1.GitRepository, error) {
	result, err := c.client.Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepository{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update updates an existing GitRepository.
func (c *GitRepositoryClient) Update(ctx context.Context, obj *gitv1alpha1.GitRepository, opts metav1.UpdateOptions) (*gitv1alpha1.GitRepository, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Update(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepository{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus updates the status subresource of a GitRepository.
func (c *GitRepositoryClient) UpdateStatus(ctx context.Context, obj *gitv1alpha1.GitRepository, opts metav1.UpdateOptions) (*gitv1alpha1.GitRepository, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.UpdateStatus(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepository{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists GitRepository resources.
func (c *GitRepositoryClient) List(ctx context.Context, opts metav1.ListOptions) (*gitv1alpha1.GitRepositoryList, error) {
	result, err := c.client.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepositoryList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.UnstructuredContent(), out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete deletes a GitRepository by name.
func (c *GitRepositoryClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete(ctx, name, opts)
}

// GitBranches returns a client for GitBranch resources in the given namespace.
func (c *GitV1alpha1Client) GitBranches(namespace string) *GitBranchClient {
	return &GitBranchClient{
		client:    c.client.Resource(gitBranchGVR).Namespace(namespace),
		namespace: namespace,
	}
}

// GitBranchClient provides typed operations on GitBranch resources.
type GitBranchClient struct {
	client    dynamic.ResourceInterface
	namespace string
}

// Create creates a new GitBranch.
func (c *GitBranchClient) Create(ctx context.Context, obj *gitv1alpha1.GitBranch, opts metav1.CreateOptions) (*gitv1alpha1.GitBranch, error) {
	obj.APIVersion = gitv1alpha1.SchemeGroupVersion.String()
	obj.Kind = "GitBranch"
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Create(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitBranch{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get retrieves a GitBranch by name.
func (c *GitBranchClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*gitv1alpha1.GitBranch, error) {
	result, err := c.client.Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitBranch{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update updates an existing GitBranch.
func (c *GitBranchClient) Update(ctx context.Context, obj *gitv1alpha1.GitBranch, opts metav1.UpdateOptions) (*gitv1alpha1.GitBranch, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Update(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitBranch{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus updates the status subresource of a GitBranch.
func (c *GitBranchClient) UpdateStatus(ctx context.Context, obj *gitv1alpha1.GitBranch, opts metav1.UpdateOptions) (*gitv1alpha1.GitBranch, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.UpdateStatus(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitBranch{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists GitBranch resources.
func (c *GitBranchClient) List(ctx context.Context, opts metav1.ListOptions) (*gitv1alpha1.GitBranchList, error) {
	result, err := c.client.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitBranchList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.UnstructuredContent(), out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete deletes a GitBranch by name.
func (c *GitBranchClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete(ctx, name, opts)
}

// GitPushTransactions returns a client for GitPushTransaction resources in the given namespace.
func (c *GitV1alpha1Client) GitPushTransactions(namespace string) *GitPushTransactionClient {
	return &GitPushTransactionClient{
		client:    c.client.Resource(gitPushTransactionGVR).Namespace(namespace),
		namespace: namespace,
	}
}

// GitPushTransactionClient provides typed operations on GitPushTransaction resources.
type GitPushTransactionClient struct {
	client    dynamic.ResourceInterface
	namespace string
}

// Create creates a new GitPushTransaction.
func (c *GitPushTransactionClient) Create(ctx context.Context, obj *gitv1alpha1.GitPushTransaction, opts metav1.CreateOptions) (*gitv1alpha1.GitPushTransaction, error) {
	obj.APIVersion = gitv1alpha1.SchemeGroupVersion.String()
	obj.Kind = "GitPushTransaction"
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Create(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitPushTransaction{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get retrieves a GitPushTransaction by name.
func (c *GitPushTransactionClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*gitv1alpha1.GitPushTransaction, error) {
	result, err := c.client.Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitPushTransaction{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update updates an existing GitPushTransaction.
func (c *GitPushTransactionClient) Update(ctx context.Context, obj *gitv1alpha1.GitPushTransaction, opts metav1.UpdateOptions) (*gitv1alpha1.GitPushTransaction, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Update(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitPushTransaction{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus updates the status subresource of a GitPushTransaction.
func (c *GitPushTransactionClient) UpdateStatus(ctx context.Context, obj *gitv1alpha1.GitPushTransaction, opts metav1.UpdateOptions) (*gitv1alpha1.GitPushTransaction, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.UpdateStatus(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitPushTransaction{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists GitPushTransaction resources.
func (c *GitPushTransactionClient) List(ctx context.Context, opts metav1.ListOptions) (*gitv1alpha1.GitPushTransactionList, error) {
	result, err := c.client.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitPushTransactionList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.UnstructuredContent(), out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete deletes a GitPushTransaction by name.
func (c *GitPushTransactionClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete(ctx, name, opts)
}

// GitRepoSyncs returns a client for GitRepoSync resources in the given namespace.
func (c *GitV1alpha1Client) GitRepoSyncs(namespace string) *GitRepoSyncClient {
	return &GitRepoSyncClient{
		client:    c.client.Resource(gitRepoSyncGVR).Namespace(namespace),
		namespace: namespace,
	}
}

// GitRepoSyncClient provides typed operations on GitRepoSync resources.
type GitRepoSyncClient struct {
	client    dynamic.ResourceInterface
	namespace string
}

// Create creates a new GitRepoSync.
func (c *GitRepoSyncClient) Create(ctx context.Context, obj *gitv1alpha1.GitRepoSync, opts metav1.CreateOptions) (*gitv1alpha1.GitRepoSync, error) {
	obj.APIVersion = gitv1alpha1.SchemeGroupVersion.String()
	obj.Kind = "GitRepoSync"
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Create(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepoSync{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get retrieves a GitRepoSync by name.
func (c *GitRepoSyncClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*gitv1alpha1.GitRepoSync, error) {
	result, err := c.client.Get(ctx, name, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepoSync{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Update updates an existing GitRepoSync.
func (c *GitRepoSyncClient) Update(ctx context.Context, obj *gitv1alpha1.GitRepoSync, opts metav1.UpdateOptions) (*gitv1alpha1.GitRepoSync, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.Update(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepoSync{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateStatus updates the status subresource of a GitRepoSync.
func (c *GitRepoSyncClient) UpdateStatus(ctx context.Context, obj *gitv1alpha1.GitRepoSync, opts metav1.UpdateOptions) (*gitv1alpha1.GitRepoSync, error) {
	u, err := toUnstructured(obj)
	if err != nil {
		return nil, err
	}
	result, err := c.client.UpdateStatus(ctx, u, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepoSync{}
	if err := fromUnstructured(result, out); err != nil {
		return nil, err
	}
	return out, nil
}

// List lists GitRepoSync resources.
func (c *GitRepoSyncClient) List(ctx context.Context, opts metav1.ListOptions) (*gitv1alpha1.GitRepoSyncList, error) {
	result, err := c.client.List(ctx, opts)
	if err != nil {
		return nil, err
	}
	out := &gitv1alpha1.GitRepoSyncList{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(result.UnstructuredContent(), out); err != nil {
		return nil, err
	}
	return out, nil
}

// Delete deletes a GitRepoSync by name.
func (c *GitRepoSyncClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	return c.client.Delete(ctx, name, opts)
}
