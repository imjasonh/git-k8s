package resolver

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/reconciler"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

// Reconciler implements the reconcile logic for resolving conflicted GitRepoSyncs.
type Reconciler struct {
	dynamicClient dynamic.Interface
	gitClient     *gitclient.GitV1alpha1Client
}

// ReconcileKind processes a single GitRepoSync resource that is in the Conflicted phase.
func (r *Reconciler) ReconcileKind(ctx context.Context, syncObj *gitv1alpha1.GitRepoSync) reconciler.Event {
	logger := logging.FromContext(ctx)
	namespace := syncObj.Namespace

	// Only process if still conflicted.
	if syncObj.Status.Phase != gitv1alpha1.SyncPhaseConflicted {
		return nil
	}

	logger.Infof("Attempting to resolve conflict for %s/%s", namespace, syncObj.Name)

	// Get the repo URLs.
	repoA, err := r.gitClient.GitRepositories(namespace).Get(ctx, syncObj.Spec.RepoA.Name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("getting repo A: %w", err)
	}

	commitAHash := syncObj.Status.RepoACommit
	commitBHash := syncObj.Status.RepoBCommit
	mergeBaseHash := syncObj.Status.MergeBase

	if commitAHash == "" || commitBHash == "" || mergeBaseHash == "" {
		return r.markManualIntervention(ctx, syncObj, "Missing commit hashes in status; cannot resolve")
	}

	// Attempt automated 3-way merge entirely in memory.
	mergeCommitHash, err := r.attemptMerge(ctx, repoA.Spec.URL, commitAHash, commitBHash, mergeBaseHash)
	if err != nil {
		logger.Warnf("Automated merge failed: %v", err)
		return r.markManualIntervention(ctx, syncObj, fmt.Sprintf("Automated merge failed: %v", err))
	}

	logger.Infof("Automated merge succeeded, merge commit: %s", mergeCommitHash)

	// Create push transactions targeting both repos with the merge commit.
	if err := r.createMergePushTransactions(ctx, namespace, syncObj, mergeCommitHash, commitAHash, commitBHash); err != nil {
		return fmt.Errorf("creating merge push transactions: %w", err)
	}

	// Update sync status to Syncing.
	syncObj.Status.Phase = gitv1alpha1.SyncPhaseSyncing
	syncObj.Status.Message = fmt.Sprintf("Automated merge commit %s created; pushing to both repos", mergeCommitHash)
	if _, err := r.gitClient.GitRepoSyncs(namespace).UpdateStatus(ctx, syncObj, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("updating sync status: %w", err)
	}

	return nil
}

// attemptMerge performs a 3-way merge in memory using go-git.
// It compares the trees of both commits against the merge base to detect conflicts.
// If there are no file-level conflicts (disjoint changes), it builds a merged tree
// that includes changes from both branches and creates a merge commit.
func (r *Reconciler) attemptMerge(ctx context.Context, repoURL, commitAHash, commitBHash, mergeBaseHash string) (string, error) {
	logger := logging.FromContext(ctx)

	// Clone into memory.
	storer := memory.NewStorage()
	repo, err := git.Clone(storer, nil, &git.CloneOptions{
		URL: repoURL,
	})
	if err != nil {
		return "", fmt.Errorf("cloning for merge: %w", err)
	}

	hashA := plumbing.NewHash(commitAHash)
	hashB := plumbing.NewHash(commitBHash)
	hashBase := plumbing.NewHash(mergeBaseHash)

	commitA, err := repo.CommitObject(hashA)
	if err != nil {
		return "", fmt.Errorf("getting commit A: %w", err)
	}

	commitB, err := repo.CommitObject(hashB)
	if err != nil {
		return "", fmt.Errorf("getting commit B: %w", err)
	}

	commitBase, err := repo.CommitObject(hashBase)
	if err != nil {
		return "", fmt.Errorf("getting merge base commit: %w", err)
	}

	// Get trees for all three commits.
	treeA, err := commitA.Tree()
	if err != nil {
		return "", fmt.Errorf("getting tree A: %w", err)
	}

	treeB, err := commitB.Tree()
	if err != nil {
		return "", fmt.Errorf("getting tree B: %w", err)
	}

	treeBase, err := commitBase.Tree()
	if err != nil {
		return "", fmt.Errorf("getting base tree: %w", err)
	}

	// Calculate diffs: base->A and base->B.
	diffA, err := treeBase.Diff(treeA)
	if err != nil {
		return "", fmt.Errorf("computing diff base->A: %w", err)
	}

	diffB, err := treeBase.Diff(treeB)
	if err != nil {
		return "", fmt.Errorf("computing diff base->B: %w", err)
	}

	// Check for file-level conflicts: files modified in both diffs.
	filesA := make(map[string]bool)
	for _, change := range diffA {
		name := changeName(change)
		filesA[name] = true
	}

	for _, change := range diffB {
		name := changeName(change)
		if filesA[name] {
			return "", fmt.Errorf("conflict: file %q modified in both branches", name)
		}
	}

	// No file-level conflicts. Build a merged tree that starts from A's tree
	// (which already has A's changes) and applies B's changes on top.
	logger.Info("No file-level conflicts detected, building merged tree")

	mergedTreeHash, err := buildMergedTree(storer, treeA, treeB, diffB)
	if err != nil {
		return "", fmt.Errorf("building merged tree: %w", err)
	}

	now := time.Now()
	mergeCommit := &object.Commit{
		Author: object.Signature{
			Name:  "git-k8s-resolver",
			Email: "resolver@git-k8s.imjasonh.com",
			When:  now,
		},
		Committer: object.Signature{
			Name:  "git-k8s-resolver",
			Email: "resolver@git-k8s.imjasonh.com",
			When:  now,
		},
		Message:  fmt.Sprintf("Merge %s and %s\n\nAutomated merge by git-k8s conflict resolver.", commitAHash[:8], commitBHash[:8]),
		TreeHash: mergedTreeHash,
		ParentHashes: []plumbing.Hash{
			hashA,
			hashB,
		},
	}

	encodedObj := storer.NewEncodedObject()
	if err := mergeCommit.Encode(encodedObj); err != nil {
		return "", fmt.Errorf("encoding merge commit: %w", err)
	}

	mergeHash, err := storer.SetEncodedObject(encodedObj)
	if err != nil {
		return "", fmt.Errorf("storing merge commit: %w", err)
	}

	return mergeHash.String(), nil
}

// buildMergedTree constructs a tree that contains A's tree with B's non-conflicting
// changes applied. It processes diffB to apply additions, modifications, and deletions
// from branch B onto tree A.
func buildMergedTree(storer *memory.Storage, treeA, treeB *object.Tree, diffB object.Changes) (plumbing.Hash, error) {
	// Start with all entries from tree A.
	entries := make(map[string]object.TreeEntry)
	for _, entry := range treeA.Entries {
		entries[entry.Name] = entry
	}

	// Apply B's changes.
	for _, change := range diffB {
		if change.To.Name != "" {
			// File added or modified in B: use B's version.
			entry, err := treeB.FindEntry(change.To.Name)
			if err != nil {
				return plumbing.ZeroHash, fmt.Errorf("finding entry %q in tree B: %w", change.To.Name, err)
			}
			entries[change.To.Name] = *entry
		}
		if change.From.Name != "" && change.To.Name == "" {
			// File deleted in B: remove from merged tree.
			delete(entries, change.From.Name)
		}
		if change.From.Name != "" && change.To.Name != "" && change.From.Name != change.To.Name {
			// File renamed in B: remove old name.
			delete(entries, change.From.Name)
		}
	}

	// Build sorted tree entries.
	sortedEntries := make([]object.TreeEntry, 0, len(entries))
	for _, entry := range entries {
		sortedEntries = append(sortedEntries, entry)
	}
	sort.Slice(sortedEntries, func(i, j int) bool {
		return sortedEntries[i].Name < sortedEntries[j].Name
	})

	mergedTree := &object.Tree{Entries: sortedEntries}
	encodedObj := storer.NewEncodedObject()
	if err := mergedTree.Encode(encodedObj); err != nil {
		return plumbing.ZeroHash, fmt.Errorf("encoding merged tree: %w", err)
	}

	hash, err := storer.SetEncodedObject(encodedObj)
	if err != nil {
		return plumbing.ZeroHash, fmt.Errorf("storing merged tree: %w", err)
	}

	return hash, nil
}

// changeName extracts the file name from a tree change.
func changeName(change *object.Change) string {
	if change.From.Name != "" {
		return change.From.Name
	}
	return change.To.Name
}

// createMergePushTransactions creates push transactions for both repos.
func (r *Reconciler) createMergePushTransactions(ctx context.Context, namespace string, syncObj *gitv1alpha1.GitRepoSync, mergeCommit, expectedOldA, expectedOldB string) error {
	// Push to repo A.
	txnA := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-merge-a-", syncObj.Name),
			Namespace:    namespace,
			Labels: map[string]string{
				"git-k8s.imjasonh.com/repo-sync": syncObj.Name,
				"git-k8s.imjasonh.com/target":    syncObj.Spec.RepoA.Name,
				"git-k8s.imjasonh.com/merge":     "true",
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
			RepositoryRef: syncObj.Spec.RepoA.Name,
			Atomic:        true,
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:            mergeCommit,
					Destination:       fmt.Sprintf("refs/heads/%s", syncObj.Spec.BranchName),
					ExpectedOldCommit: expectedOldA,
				},
			},
		},
	}

	// Push to repo B.
	txnB := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("%s-merge-b-", syncObj.Name),
			Namespace:    namespace,
			Labels: map[string]string{
				"git-k8s.imjasonh.com/repo-sync": syncObj.Name,
				"git-k8s.imjasonh.com/target":    syncObj.Spec.RepoB.Name,
				"git-k8s.imjasonh.com/merge":     "true",
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
			RepositoryRef: syncObj.Spec.RepoB.Name,
			Atomic:        true,
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:            mergeCommit,
					Destination:       fmt.Sprintf("refs/heads/%s", syncObj.Spec.BranchName),
					ExpectedOldCommit: expectedOldB,
				},
			},
		},
	}

	if _, err := r.gitClient.GitPushTransactions(namespace).Create(ctx, txnA, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating push transaction for repo A: %w", err)
	}

	if _, err := r.gitClient.GitPushTransactions(namespace).Create(ctx, txnB, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("creating push transaction for repo B: %w", err)
	}

	return nil
}

// markManualIntervention updates the GitRepoSync status to RequiresManualIntervention.
func (r *Reconciler) markManualIntervention(ctx context.Context, syncObj *gitv1alpha1.GitRepoSync, message string) error {
	syncObj.Status.Phase = gitv1alpha1.SyncPhaseRequiresManualIntervention
	syncObj.Status.Message = message
	_, err := r.gitClient.GitRepoSyncs(syncObj.Namespace).UpdateStatus(ctx, syncObj, metav1.UpdateOptions{})
	return err
}
