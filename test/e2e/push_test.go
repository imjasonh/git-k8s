//go:build e2e

package e2e

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func TestPushTransaction(t *testing.T) {
	t.Parallel()

	repoName := "e2e-push"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-push-creds")
	createGitRepository(t, "e2e-push-repo", repoName, secretName)
	createGitBranch(t, "e2e-push-feature", "e2e-push-repo", "feature-push")

	// Create a push transaction: push main to a new branch.
	ctx := context.Background()
	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-txn",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "e2e-push-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/feature-push",
				},
			},
		},
	}
	_, err := gitClient.GitPushTransactions(testNamespace).Create(ctx, txn, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitPushTransaction: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitPushTransactions(testNamespace).Delete(ctx, "e2e-push-txn", metav1.DeleteOptions{})
	})

	// Wait for the push to succeed.
	result := waitForPushTransaction(t, "e2e-push-txn")
	if result.Status.ResultCommit == "" {
		t.Fatal("push transaction succeeded but resultCommit is empty")
	}
	t.Logf("Push transaction result commit: %s", result.Status.ResultCommit)

	// Verify the GitBranch was updated by the controller.
	headCommit := waitForGitBranchCommit(t, "e2e-push-feature")
	t.Logf("GitBranch headCommit: %s", headCommit)

	// Verify the branch actually exists on the git server.
	if !giteaBranchExists(t, repoName, "feature-push") {
		t.Fatal("branch feature-push does not exist on Gitea after push")
	}
}

func TestPushTransactionMultipleRefSpecs(t *testing.T) {
	t.Parallel()

	repoName := "e2e-push-multi"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-push-multi-creds")
	createGitRepository(t, "e2e-push-multi-repo", repoName, secretName)
	createGitBranch(t, "e2e-push-multi-b1", "e2e-push-multi-repo", "branch-one")
	createGitBranch(t, "e2e-push-multi-b2", "e2e-push-multi-repo", "branch-two")

	// Push main to two branches at once.
	ctx := context.Background()
	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-multi-txn",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "e2e-push-multi-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/branch-one",
				},
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/branch-two",
				},
			},
		},
	}
	_, err := gitClient.GitPushTransactions(testNamespace).Create(ctx, txn, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitPushTransaction: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitPushTransactions(testNamespace).Delete(ctx, "e2e-push-multi-txn", metav1.DeleteOptions{})
	})

	result := waitForPushTransaction(t, "e2e-push-multi-txn")
	if result.Status.ResultCommit == "" {
		t.Fatal("push transaction succeeded but resultCommit is empty")
	}

	// Both branches should exist on Gitea.
	if !giteaBranchExists(t, repoName, "branch-one") {
		t.Error("branch-one does not exist on Gitea")
	}
	if !giteaBranchExists(t, repoName, "branch-two") {
		t.Error("branch-two does not exist on Gitea")
	}
}
