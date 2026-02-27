package push

import (
	"context"
	"fmt"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

// Reconciler implements the reconcile logic for GitPushTransaction.
type Reconciler struct {
	dynamicClient dynamic.Interface
	gitClient     *gitclient.GitV1alpha1Client
}

// ReconcileKind processes a single GitPushTransaction resource.
func (r *Reconciler) ReconcileKind(ctx context.Context, key string) reconciler.Event {
	logger := logging.FromContext(ctx)

	namespace, name, err := splitKey(key)
	if err != nil {
		return err
	}

	// Fetch the GitPushTransaction.
	txn, err := r.gitClient.GitPushTransactions(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting GitPushTransaction %s/%s: %w", namespace, name, err)
	}

	// Skip if already terminal.
	if txn.Status.Phase == gitv1alpha1.TransactionPhaseSucceeded ||
		txn.Status.Phase == gitv1alpha1.TransactionPhaseFailed {
		return nil
	}

	// Mark as in-progress.
	now := metav1.Now()
	txn.Status.Phase = gitv1alpha1.TransactionPhaseInProgress
	txn.Status.StartTime = &now
	if _, err := r.gitClient.GitPushTransactions(namespace).UpdateStatus(ctx, txn, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating status to InProgress: %w", err)
	}

	// Fetch the target repository.
	repo, err := r.gitClient.GitRepositories(namespace).Get(ctx, txn.Spec.RepositoryRef, metav1.GetOptions{})
	if err != nil {
		return r.failTransaction(ctx, txn, fmt.Sprintf("getting repository %q: %v", txn.Spec.RepositoryRef, err))
	}

	// Resolve authentication.
	auth, err := r.resolveAuth(ctx, namespace, repo)
	if err != nil {
		return r.failTransaction(ctx, txn, fmt.Sprintf("resolving auth: %v", err))
	}

	// Execute the push.
	resultCommit, err := r.executePush(ctx, repo.Spec.URL, txn.Spec, auth)
	if err != nil {
		return r.failTransaction(ctx, txn, fmt.Sprintf("push failed: %v", err))
	}

	// Mark as succeeded.
	completionTime := metav1.Now()
	txn.Status.Phase = gitv1alpha1.TransactionPhaseSucceeded
	txn.Status.CompletionTime = &completionTime
	txn.Status.ResultCommit = resultCommit
	txn.Status.Message = "Push completed successfully"
	if _, err := r.gitClient.GitPushTransactions(namespace).UpdateStatus(ctx, txn, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating status to Succeeded: %w", err)
	}

	// Update corresponding GitBranch resources.
	if err := r.updateBranches(ctx, namespace, txn); err != nil {
		logger.Warnf("Failed to update branch CRs after push: %v", err)
	}

	logger.Infof("Successfully pushed transaction %s/%s", namespace, name)
	return nil
}

// executePush performs the actual Git push using go-git with in-memory storage.
func (r *Reconciler) executePush(ctx context.Context, repoURL string, spec gitv1alpha1.GitPushTransactionSpec, auth *http.BasicAuth) (string, error) {
	logger := logging.FromContext(ctx)

	// Clone into memory - keeps the controller stateless.
	storer := memory.NewStorage()
	gitRepo, err := git.Clone(storer, nil, &git.CloneOptions{
		URL:  repoURL,
		Auth: auth,
	})
	if err != nil {
		return "", fmt.Errorf("cloning repository: %w", err)
	}

	// Build refspecs for the push.
	refSpecs := make([]config.RefSpec, 0, len(spec.RefSpecs))
	for _, rs := range spec.RefSpecs {
		refSpec := config.RefSpec(fmt.Sprintf("%s:%s", rs.Source, rs.Destination))
		refSpecs = append(refSpecs, refSpec)
	}

	// Execute the push with atomic option.
	pushOpts := &git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   refSpecs,
		Auth:       auth,
		Atomic:     spec.Atomic,
	}

	logger.Infof("Pushing %d refspec(s) to %s (atomic=%v)", len(refSpecs), repoURL, spec.Atomic)
	if err := gitRepo.PushContext(ctx, pushOpts); err != nil {
		if err == git.NoErrAlreadyUpToDate {
			logger.Info("Push: already up to date")
		} else {
			return "", fmt.Errorf("executing push: %w", err)
		}
	}

	// Get the resulting commit SHA from the first refspec's source.
	var resultCommit string
	if len(spec.RefSpecs) > 0 {
		ref, err := storer.Reference(plumbing.ReferenceName(spec.RefSpecs[0].Source))
		if err == nil {
			resultCommit = ref.Hash().String()
		}
	}

	return resultCommit, nil
}

// resolveAuth retrieves Git authentication credentials from the referenced Secret.
func (r *Reconciler) resolveAuth(ctx context.Context, namespace string, repo *gitv1alpha1.GitRepository) (*http.BasicAuth, error) {
	if repo.Spec.Auth == nil || repo.Spec.Auth.SecretRef == nil {
		return nil, nil
	}

	// Use the dynamic client to fetch the Secret.
	secretGVR := corev1.SchemeGroupVersion.WithResource("secrets")
	u, err := r.dynamicClient.Resource(secretGVR).Namespace(namespace).Get(ctx, repo.Spec.Auth.SecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("getting secret %q: %w", repo.Spec.Auth.SecretRef.Name, err)
	}

	data, found, err := unstructuredNestedStringMap(u.Object, "data")
	if err != nil || !found {
		return nil, fmt.Errorf("reading secret data: found=%v err=%v", found, err)
	}

	return &http.BasicAuth{
		Username: data["username"],
		Password: data["password"],
	}, nil
}

// unstructuredNestedStringMap extracts a map[string]string from an unstructured object path.
func unstructuredNestedStringMap(obj map[string]interface{}, fields ...string) (map[string]string, bool, error) {
	val, found, err := nestedFieldNoCopy(obj, fields...)
	if !found || err != nil {
		return nil, found, err
	}
	m, ok := val.(map[string]interface{})
	if !ok {
		return nil, false, fmt.Errorf("expected map, got %T", val)
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		s, ok := v.(string)
		if !ok {
			continue
		}
		result[k] = s
	}
	return result, true, nil
}

func nestedFieldNoCopy(obj map[string]interface{}, fields ...string) (interface{}, bool, error) {
	var val interface{} = obj
	for _, field := range fields {
		m, ok := val.(map[string]interface{})
		if !ok {
			return nil, false, nil
		}
		val, ok = m[field]
		if !ok {
			return nil, false, nil
		}
	}
	return val, true, nil
}

// failTransaction marks a GitPushTransaction as Failed with the given message.
func (r *Reconciler) failTransaction(ctx context.Context, txn *gitv1alpha1.GitPushTransaction, message string) error {
	logger := logging.FromContext(ctx)
	logger.Errorf("Transaction %s/%s failed: %s", txn.Namespace, txn.Name, message)

	now := metav1.Now()
	txn.Status.Phase = gitv1alpha1.TransactionPhaseFailed
	txn.Status.CompletionTime = &now
	txn.Status.Message = message
	if _, err := r.gitClient.GitPushTransactions(txn.Namespace).UpdateStatus(ctx, txn, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating status to Failed: %w", err)
	}
	return nil
}

// updateBranches updates GitBranch CRs after a successful push.
func (r *Reconciler) updateBranches(ctx context.Context, namespace string, txn *gitv1alpha1.GitPushTransaction) error {
	now := metav1.Now()
	for _, rs := range txn.Spec.RefSpecs {
		// List branches matching the destination ref.
		branches, err := r.gitClient.GitBranches(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("listing branches: %w", err)
		}
		for i := range branches.Items {
			branch := &branches.Items[i]
			destRef := fmt.Sprintf("refs/heads/%s", branch.Spec.BranchName)
			if branch.Spec.RepositoryRef == txn.Spec.RepositoryRef && destRef == rs.Destination {
				branch.Status.HeadCommit = txn.Status.ResultCommit
				branch.Status.LastUpdated = &now
				if _, err := r.gitClient.GitBranches(namespace).UpdateStatus(ctx, branch, metav1.UpdateOptions{}); err != nil {
					return fmt.Errorf("updating branch %s: %w", branch.Name, err)
				}
			}
		}
	}
	return nil
}

// splitKey splits a namespace/name key.
func splitKey(key string) (string, string, error) {
	for i := range key {
		if key[i] == '/' {
			return key[:i], key[i+1:], nil
		}
	}
	return "", key, nil
}

// Ensure Reconciler implements the reconciler interface.
var _ reconciler.LeaderAware = (*Reconciler)(nil)

// Promote implements LeaderAware.
func (r *Reconciler) Promote(bkt reconciler.Bucket, enq func(bkt reconciler.Bucket, key string) error) error {
	return nil
}

// Demote implements LeaderAware.
func (r *Reconciler) Demote(bkt reconciler.Bucket) {
}

// Reconcile implements the controller.Reconciler interface.
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	return r.ReconcileKind(ctx, key)
}
