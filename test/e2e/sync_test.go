//go:build e2e

package e2e

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func TestRepoSync(t *testing.T) {
	t.Parallel()

	// Create two repos on Gitea.
	createGiteaRepo(t, "e2e-sync-a")
	createGiteaRepo(t, "e2e-sync-b")

	secretName := createCredentialsSecret(t, "e2e-sync-creds")
	createGitRepository(t, "e2e-sync-repo-a", "e2e-sync-a", secretName)
	createGitRepository(t, "e2e-sync-repo-b", "e2e-sync-b", secretName)

	// Create GitBranch CRs and populate their headCommit via the push controller.
	// First, push main to itself to get the push controller to set headCommit.
	branchA := createGitBranch(t, "e2e-sync-branch-a", "e2e-sync-repo-a", "main")
	branchB := createGitBranch(t, "e2e-sync-branch-b", "e2e-sync-repo-b", "main")

	// We need headCommit set on both branches for the sync controller.
	// Patch them with the actual commit from Gitea.
	commitA := getGiteaBranchCommit(t, "e2e-sync-a", "main")
	commitB := getGiteaBranchCommit(t, "e2e-sync-b", "main")
	t.Logf("Repo A main commit: %s", commitA)
	t.Logf("Repo B main commit: %s", commitB)

	ctx := context.Background()
	branchA.Status.HeadCommit = commitA
	if _, err := gitClient.GitBranches(testNamespace).UpdateStatus(ctx, branchA, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("updating branch A status: %v", err)
	}
	branchB.Status.HeadCommit = commitB
	if _, err := gitClient.GitBranches(testNamespace).UpdateStatus(ctx, branchB, metav1.UpdateOptions{}); err != nil {
		t.Fatalf("updating branch B status: %v", err)
	}

	// Create a GitRepoSync between the two repos.
	syncObj := &gitv1alpha1.GitRepoSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-sync",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitRepoSyncSpec{
			RepoA:      gitv1alpha1.SyncRepoRef{Name: "e2e-sync-repo-a"},
			RepoB:      gitv1alpha1.SyncRepoRef{Name: "e2e-sync-repo-b"},
			BranchName: "main",
		},
	}
	_, err := gitClient.GitRepoSyncs(testNamespace).Create(ctx, syncObj, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitRepoSync: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitRepoSyncs(testNamespace).Delete(ctx, "e2e-sync", metav1.DeleteOptions{})
	})

	// Wait for the sync controller to set a phase.
	result := waitForSyncPhase(t, "e2e-sync")
	t.Logf("GitRepoSync phase: %s, message: %s", result.Status.Phase, result.Status.Message)

	// The sync controller should have processed this.
	// Since both repos are different auto_init repos, commits differ,
	// so we expect Conflicted (no common ancestor) or another non-empty phase.
	if result.Status.Phase == "" {
		t.Fatal("sync controller did not set a phase")
	}
}
