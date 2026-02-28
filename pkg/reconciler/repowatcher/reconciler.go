package repowatcher

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

// DefaultPollInterval is the default interval between remote ref polls.
const DefaultPollInterval = 30 * time.Second

// Reconciler watches remote Git repositories and keeps GitBranch CRDs in sync.
type Reconciler struct {
	dynamicClient   dynamic.Interface
	gitClient       *gitclient.GitV1alpha1Client
	defaultInterval time.Duration
	impl            *controller.Impl

	// lsRemote is overridable for testing.
	lsRemote func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error)
}

// SetImpl stores the controller.Impl so the reconciler can re-enqueue with a delay.
func (r *Reconciler) SetImpl(impl *controller.Impl) {
	r.impl = impl
}

// ReconcileKind polls the remote for a single GitRepository and syncs GitBranch CRDs.
func (r *Reconciler) ReconcileKind(ctx context.Context, repo *gitv1alpha1.GitRepository) reconciler.Event {
	logger := logging.FromContext(ctx)
	namespace := repo.Namespace

	// Check if enough time has passed since the last fetch.
	interval := r.pollInterval(repo)
	if repo.Status.LastFetchTime != nil {
		elapsed := time.Since(repo.Status.LastFetchTime.Time)
		if elapsed < interval {
			r.enqueueAfter(repo, interval-elapsed)
			return nil
		}
	}

	// Resolve auth credentials.
	auth, err := r.resolveAuth(ctx, namespace, repo)
	if err != nil {
		return fmt.Errorf("resolving auth for %s/%s: %w", namespace, repo.Name, err)
	}

	// Run ls-remote to discover all remote refs.
	refs, err := r.lsRemote(repo.Spec.URL, auth)
	if err != nil {
		return fmt.Errorf("ls-remote for %s: %w", repo.Spec.URL, err)
	}

	// Build a map of branch name -> commit SHA from remote refs.
	remoteBranches := make(map[string]string)
	for _, ref := range refs {
		name := ref.Name()
		if name.IsBranch() {
			branchName := name.Short()
			remoteBranches[branchName] = ref.Hash().String()
		}
	}

	// List existing GitBranch CRDs for this repo.
	existingBranches, err := r.gitClient.GitBranches(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing branches: %w", err)
	}

	// Index existing branches by branch name for this repo.
	existingByName := make(map[string]*gitv1alpha1.GitBranch)
	for i := range existingBranches.Items {
		b := &existingBranches.Items[i]
		if b.Spec.RepositoryRef == repo.Name {
			existingByName[b.Spec.BranchName] = b
		}
	}

	// Create or update branches found on the remote.
	now := metav1.Now()
	for branchName, commitSHA := range remoteBranches {
		existing, found := existingByName[branchName]
		if !found {
			// Create new GitBranch.
			newBranch := &gitv1alpha1.GitBranch{
				ObjectMeta: metav1.ObjectMeta{
					Name:      branchCRDName(repo.Name, branchName),
					Namespace: namespace,
					Labels: map[string]string{
						"git-k8s.imjasonh.com/repository": repo.Name,
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
							Kind:       "GitRepository",
							Name:       repo.Name,
							UID:        repo.UID,
						},
					},
				},
				Spec: gitv1alpha1.GitBranchSpec{
					RepositoryRef: repo.Name,
					BranchName:    branchName,
				},
			}
			created, err := r.gitClient.GitBranches(namespace).Create(ctx, newBranch, metav1.CreateOptions{})
			if err != nil {
				if apierrors.IsAlreadyExists(err) {
					logger.Debugf("Branch %s already exists, will update on next poll", branchName)
					continue
				}
				return fmt.Errorf("creating branch %s: %w", branchName, err)
			}
			// Set the status on the newly created branch.
			created.Status.HeadCommit = commitSHA
			created.Status.LastUpdated = &now
			if _, err := r.gitClient.GitBranches(namespace).UpdateStatus(ctx, created, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("updating status for new branch %s: %w", branchName, err)
			}
			logger.Infof("Created GitBranch %s/%s (commit: %s)", namespace, created.Name, commitSHA[:minLen(len(commitSHA), 7)])
		} else if existing.Status.HeadCommit != commitSHA {
			// Update existing branch with new commit.
			existing.Status.HeadCommit = commitSHA
			existing.Status.LastUpdated = &now
			if _, err := r.gitClient.GitBranches(namespace).UpdateStatus(ctx, existing, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("updating branch %s: %w", branchName, err)
			}
			logger.Infof("Updated GitBranch %s/%s to commit %s", namespace, existing.Name, commitSHA[:minLen(len(commitSHA), 7)])
		}
	}

	// Delete branches that no longer exist on the remote.
	for branchName, existing := range existingByName {
		if _, found := remoteBranches[branchName]; !found {
			if err := r.gitClient.GitBranches(namespace).Delete(ctx, existing.Name, metav1.DeleteOptions{}); err != nil {
				if !apierrors.IsNotFound(err) {
					return fmt.Errorf("deleting branch %s: %w", existing.Name, err)
				}
			}
			logger.Infof("Deleted GitBranch %s/%s (branch %s removed from remote)", namespace, existing.Name, branchName)
		}
	}

	// Update the repo's LastFetchTime.
	repo.Status.LastFetchTime = &now
	if _, err := r.gitClient.GitRepositories(namespace).UpdateStatus(ctx, repo, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating repo status: %w", err)
	}

	// Re-enqueue for the next poll cycle.
	r.enqueueAfter(repo, interval)

	return nil
}

// pollInterval returns the effective poll interval for a repository.
func (r *Reconciler) pollInterval(repo *gitv1alpha1.GitRepository) time.Duration {
	if repo.Spec.PollInterval != nil {
		return repo.Spec.PollInterval.Duration
	}
	return r.defaultInterval
}

// enqueueAfter schedules the repository for reconciliation after the given delay.
func (r *Reconciler) enqueueAfter(repo *gitv1alpha1.GitRepository, delay time.Duration) {
	if r.impl != nil {
		r.impl.EnqueueKeyAfter(types.NamespacedName{
			Namespace: repo.Namespace,
			Name:      repo.Name,
		}, delay)
	}
}

// resolveAuth retrieves Git authentication credentials from the referenced Secret.
func (r *Reconciler) resolveAuth(ctx context.Context, namespace string, repo *gitv1alpha1.GitRepository) (*http.BasicAuth, error) {
	if repo.Spec.Auth == nil || repo.Spec.Auth.SecretRef == nil {
		return nil, nil
	}

	secretGVR := corev1.SchemeGroupVersion.WithResource("secrets")
	u, err := r.dynamicClient.Resource(secretGVR).Namespace(namespace).Get(ctx, repo.Spec.Auth.SecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting secret %q: %w", repo.Spec.Auth.SecretRef.Name, err)
	}

	data, found, err := unstructured.NestedStringMap(u.Object, "data")
	if err != nil || !found {
		return nil, fmt.Errorf("reading secret data: found=%v err=%v", found, err)
	}

	username, err := base64.StdEncoding.DecodeString(data["username"])
	if err != nil {
		return nil, fmt.Errorf("decoding username: %w", err)
	}
	password, err := base64.StdEncoding.DecodeString(data["password"])
	if err != nil {
		return nil, fmt.Errorf("decoding password: %w", err)
	}

	return &http.BasicAuth{
		Username: string(username),
		Password: string(password),
	}, nil
}

// defaultLsRemote performs a real git ls-remote using go-git.
func defaultLsRemote(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
	remote := git.NewRemote(memory.NewStorage(), &gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{url},
	})

	listOpts := &git.ListOptions{}
	if auth != nil {
		listOpts.Auth = auth
	}
	return remote.List(listOpts)
}

// branchCRDName generates a deterministic name for a GitBranch CR.
func branchCRDName(repoName, branchName string) string {
	// Replace slashes with dashes for K8s name compatibility.
	safeBranch := strings.ReplaceAll(branchName, "/", "-")
	return fmt.Sprintf("%s-%s", repoName, safeBranch)
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
