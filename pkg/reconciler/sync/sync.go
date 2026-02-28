package sync

import (
	"context"
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

// Reconciler implements the reconcile logic for GitRepoSync.
type Reconciler struct {
	dynamicClient dynamic.Interface
	gitClient     *gitclient.GitV1alpha1Client
}

// Reconcile implements the controller.Reconciler interface.
func (r *Reconciler) Reconcile(ctx context.Context, key string) error {
	logger := logging.FromContext(ctx)

	namespace, name, err := splitKey(key)
	if err != nil {
		return err
	}

	// Fetch the GitRepoSync resource.
	syncObj, err := r.gitClient.GitRepoSyncs(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting GitRepoSync %s/%s: %w", namespace, name, err)
	}

	// Get the branches for both repos.
	branchA, err := r.findBranch(ctx, namespace, syncObj.Spec.RepoA.Name, syncObj.Spec.BranchName)
	if err != nil {
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseConflicted,
			fmt.Sprintf("finding branch for repo A: %v", err), "", "", "")
	}

	branchB, err := r.findBranch(ctx, namespace, syncObj.Spec.RepoB.Name, syncObj.Spec.BranchName)
	if err != nil {
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseConflicted,
			fmt.Sprintf("finding branch for repo B: %v", err), "", "", "")
	}

	commitA := branchA.Status.HeadCommit
	commitB := branchB.Status.HeadCommit

	// If both branches point to the same commit, they're in sync.
	if commitA == commitB {
		logger.Infof("GitRepoSync %s/%s: repos are in sync at %s", namespace, name, commitA)
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseInSync, "Repos are in sync", commitA, commitB, commitA)
	}

	// Repos differ - calculate merge base to determine which is ahead.
	logger.Infof("GitRepoSync %s/%s: commits differ (A=%s, B=%s), calculating merge base", namespace, name, commitA, commitB)

	// Fetch repo A to get commit objects.
	repoASpec, err := r.gitClient.GitRepositories(namespace).Get(ctx, syncObj.Spec.RepoA.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting repo A: %w", err)
	}

	mergeBase, err := r.calculateMergeBase(ctx, repoASpec.Spec.URL, commitA, commitB)
	if err != nil {
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseConflicted,
			fmt.Sprintf("calculating merge base: %v", err), commitA, commitB, "")
	}

	switch {
	case mergeBase == commitA:
		// B is ahead of A. Create a push transaction to update A.
		logger.Infof("Repo B is ahead. Creating push transaction to update A.")
		if err := r.createPushTransaction(ctx, namespace, syncObj, syncObj.Spec.RepoA.Name, commitB, syncObj.Spec.BranchName, commitA); err != nil {
			return fmt.Errorf("creating push transaction for A: %w", err)
		}
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseSyncing,
			"Repo B is ahead; pushing to A", commitA, commitB, mergeBase)

	case mergeBase == commitB:
		// A is ahead of B. Create a push transaction to update B.
		logger.Infof("Repo A is ahead. Creating push transaction to update B.")
		if err := r.createPushTransaction(ctx, namespace, syncObj, syncObj.Spec.RepoB.Name, commitA, syncObj.Spec.BranchName, commitB); err != nil {
			return fmt.Errorf("creating push transaction for B: %w", err)
		}
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseSyncing,
			"Repo A is ahead; pushing to B", commitA, commitB, mergeBase)

	default:
		// Diverged - both repos have changes since the merge base.
		logger.Infof("Repos have diverged (mergeBase=%s). Marking as conflicted.", mergeBase)
		return r.updateSyncStatus(ctx, syncObj, gitv1alpha1.SyncPhaseConflicted,
			fmt.Sprintf("Repos have diverged from merge base %s", mergeBase), commitA, commitB, mergeBase)
	}
}

// findBranch finds a GitBranch matching the given repository and branch name.
func (r *Reconciler) findBranch(ctx context.Context, namespace, repoName, branchName string) (*gitv1alpha1.GitBranch, error) {
	branches, err := r.gitClient.GitBranches(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("listing branches: %w", err)
	}
	for i := range branches.Items {
		b := &branches.Items[i]
		if b.Spec.RepositoryRef == repoName && b.Spec.BranchName == branchName {
			return b, nil
		}
	}
	return nil, fmt.Errorf("branch %q not found for repo %q", branchName, repoName)
}

// calculateMergeBase computes the merge base between two commits entirely in memory.
// It clones the repository into memory, then walks the commit history to find the
// common ancestor.
func (r *Reconciler) calculateMergeBase(ctx context.Context, repoURL, commitAHash, commitBHash string) (string, error) {
	// Clone into memory.
	storer := memory.NewStorage()
	repo, err := git.Clone(storer, nil, &git.CloneOptions{
		URL: repoURL,
	})
	if err != nil {
		return "", fmt.Errorf("cloning for merge base: %w", err)
	}

	hashA := plumbing.NewHash(commitAHash)
	hashB := plumbing.NewHash(commitBHash)

	commitA, err := repo.CommitObject(hashA)
	if err != nil {
		return "", fmt.Errorf("getting commit A (%s): %w", commitAHash, err)
	}

	commitB, err := repo.CommitObject(hashB)
	if err != nil {
		return "", fmt.Errorf("getting commit B (%s): %w", commitBHash, err)
	}

	// Use go-git's merge base calculation.
	bases, err := commitA.MergeBase(commitB)
	if err != nil {
		return "", fmt.Errorf("calculating merge base: %w", err)
	}

	if len(bases) == 0 {
		return "", fmt.Errorf("no common ancestor found between %s and %s", commitAHash, commitBHash)
	}

	return bases[0].Hash.String(), nil
}

// createPushTransaction creates a GitPushTransaction to push a commit to a target repo's branch.
func (r *Reconciler) createPushTransaction(ctx context.Context, namespace string, syncObj *gitv1alpha1.GitRepoSync, targetRepo, sourceCommit, branchName, expectedOld string) error {
	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-sync-", syncObj.Name),
			Namespace:    namespace,
			Labels: map[string]string{
				"git.k8s.io/repo-sync": syncObj.Name,
				"git.k8s.io/target":    targetRepo,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: gitv1alpha1.SchemeGroupVersion.String(),
					Kind:       "GitRepoSync",
					Name:       syncObj.Name,
					UID:        syncObj.UID,
				},
			},
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: targetRepo,
			Atomic:        true,
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:            sourceCommit,
					Destination:       fmt.Sprintf("refs/heads/%s", branchName),
					ExpectedOldCommit: expectedOld,
				},
			},
		},
	}

	_, err := r.gitClient.GitPushTransactions(namespace).Create(ctx, txn, metav1.CreateOptions{})
	return err
}

// updateSyncStatus updates the GitRepoSync status fields.
func (r *Reconciler) updateSyncStatus(ctx context.Context, syncObj *gitv1alpha1.GitRepoSync, phase gitv1alpha1.SyncPhase, message, commitA, commitB, mergeBase string) error {
	syncObj.Status.Phase = phase
	syncObj.Status.Message = message
	syncObj.Status.RepoACommit = commitA
	syncObj.Status.RepoBCommit = commitB
	syncObj.Status.MergeBase = mergeBase

	if phase == gitv1alpha1.SyncPhaseInSync {
		now := metav1.Now()
		syncObj.Status.LastSyncTime = &now
	}

	_, err := r.gitClient.GitRepoSyncs(syncObj.Namespace).UpdateStatus(ctx, syncObj, metav1.UpdateOptions{})
	return err
}

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

func (r *Reconciler) Promote(bkt reconciler.Bucket, enq func(reconciler.Bucket, types.NamespacedName)) error {
	return nil
}

func (r *Reconciler) Demote(bkt reconciler.Bucket) {
}

