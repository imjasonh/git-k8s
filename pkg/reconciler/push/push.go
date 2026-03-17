package push

import (
	"context"
	"fmt"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
	"github.com/imjasonh/git-k8s/pkg/gitauth"
	"github.com/imjasonh/git-k8s/pkg/metrics"
	"github.com/imjasonh/git-k8s/pkg/workspace"
)

// DefaultGitTimeout is the maximum duration for a single Git operation (clone, push).
const DefaultGitTimeout = 5 * time.Minute

// Reconciler implements the reconcile logic for GitPushTransaction.
type Reconciler struct {
	dynamicClient dynamic.Interface
	gitClient     *gitclient.GitV1alpha1Client
	workspaces    *workspace.Manager
}

// ReconcileKind processes a single GitPushTransaction resource.
func (r *Reconciler) ReconcileKind(ctx context.Context, txn *gitv1alpha1.GitPushTransaction) reconciler.Event {
	logger := logging.FromContext(ctx)
	namespace := txn.Namespace

	// Skip if already terminal.
	if txn.Status.Phase == gitv1alpha1.TransactionPhaseSucceeded ||
		txn.Status.Phase == gitv1alpha1.TransactionPhaseFailed {
		return nil
	}

	// Mark as in-progress. Capture the returned object to get the
	// updated resourceVersion for subsequent status updates.
	now := metav1.Now()
	txn.Status.Phase = gitv1alpha1.TransactionPhaseInProgress
	txn.Status.StartTime = &now
	txn, err := r.gitClient.GitPushTransactions(namespace).UpdateStatus(ctx, txn, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("updating status to InProgress: %w", err)
	}

	// Fetch the target repository.
	repo, err := r.gitClient.GitRepositories(namespace).Get(ctx, txn.Spec.RepositoryRef, metav1.GetOptions{})
	if err != nil {
		return r.failTransaction(ctx, txn, fmt.Sprintf("getting repository %q: %v", txn.Spec.RepositoryRef, err))
	}

	// Resolve authentication.
	auth, err := gitauth.ResolveAuth(ctx, r.dynamicClient, namespace, repo)
	if err != nil {
		return r.failTransaction(ctx, txn, fmt.Sprintf("resolving auth: %v", err))
	}

	// Execute the push.
	cacheEnabled := repo.Spec.Cache != nil && repo.Spec.Cache.Enabled
	resultCommit, err := r.executePush(ctx, repo.Spec.URL, txn.Spec, auth, cacheEnabled)
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

	logger.Infof("Successfully pushed transaction %s/%s", namespace, txn.Name)
	return nil
}

// executePush performs the actual Git push using the workspace manager.
func (r *Reconciler) executePush(ctx context.Context, repoURL string, spec gitv1alpha1.GitPushTransactionSpec, auth *http.BasicAuth, cacheEnabled bool) (string, error) {
	logger := logging.FromContext(ctx)

	ws, err := r.workspaces.Acquire(ctx, repoURL, auth, cacheEnabled)
	if err != nil {
		return "", fmt.Errorf("acquiring workspace: %w", err)
	}
	defer r.workspaces.Release(ws)

	gitRepo := ws.Repo

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

	pushCtx, pushCancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer pushCancel()

	pushStart := time.Now()
	logger.Infof("Pushing %d refspec(s) to %s (atomic=%v, mode=%s)", len(refSpecs), repoURL, spec.Atomic, ws.Mode)
	if err := gitRepo.PushContext(pushCtx, pushOpts); err != nil {
		if err == git.NoErrAlreadyUpToDate {
			logger.Info("Push: already up to date")
		} else {
			metrics.GitOperationDuration.WithLabelValues("push").Observe(time.Since(pushStart).Seconds())
			return "", fmt.Errorf("executing push: %w", err)
		}
	}
	metrics.GitOperationDuration.WithLabelValues("push").Observe(time.Since(pushStart).Seconds())

	// Get the resulting commit SHA from the first refspec's source.
	var resultCommit string
	if len(spec.RefSpecs) > 0 {
		ref, err := gitRepo.Reference(plumbing.ReferenceName(spec.RefSpecs[0].Source), true)
		if err == nil {
			resultCommit = ref.Hash().String()
		}
	}

	return resultCommit, nil
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
	// List branches once rather than per-refspec.
	branches, err := r.gitClient.GitBranches(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("listing branches: %w", err)
	}

	now := metav1.Now()
	for _, rs := range txn.Spec.RefSpecs {
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
