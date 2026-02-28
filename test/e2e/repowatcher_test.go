//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestRepoWatcherDiscoversBranches verifies that when a GitRepository is created
// pointing at a Gitea repo with a "main" branch, the repo-watcher controller
// auto-creates a GitBranch CRD with the correct headCommit.
func TestRepoWatcherDiscoversBranches(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-discover"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-discover-creds")

	// Use a short poll interval so we don't wait long.
	interval := metav1.Duration{Duration: 2 * time.Second}
	repo := createGitRepositoryWithPollInterval(t, "e2e-watcher-discover-repo", repoName, secretName, interval)
	_ = repo

	// The repo-watcher should auto-create a GitBranch for "main".
	branchCRDName := "e2e-watcher-discover-repo-main"
	branch := waitForGitBranchExists(t, branchCRDName)

	// Verify the branch spec.
	if branch.Spec.RepositoryRef != "e2e-watcher-discover-repo" {
		t.Errorf("GitBranch repositoryRef = %q, want %q", branch.Spec.RepositoryRef, "e2e-watcher-discover-repo")
	}
	if branch.Spec.BranchName != "main" {
		t.Errorf("GitBranch branchName = %q, want %q", branch.Spec.BranchName, "main")
	}

	// Verify headCommit matches what Gitea reports.
	expectedCommit := getGiteaBranchCommit(t, repoName, "main")
	commit := waitForGitBranchCommit(t, branchCRDName)
	if commit != expectedCommit {
		t.Errorf("GitBranch headCommit = %q, want %q", commit, expectedCommit)
	}

	// Cleanup: the watcher-created branch won't be cleaned up by our test helpers
	// since we didn't create it. Delete it explicitly.
	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(context.Background(), branchCRDName, metav1.DeleteOptions{})
	})
}

// TestRepoWatcherOwnerReferences verifies that auto-created GitBranch CRDs have
// correct owner references pointing back to the GitRepository.
func TestRepoWatcherOwnerReferences(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-ownerref"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-ownerref-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	repo := createGitRepositoryWithPollInterval(t, "e2e-watcher-ownerref-repo", repoName, secretName, interval)

	branchCRDName := "e2e-watcher-ownerref-repo-main"
	branch := waitForGitBranchExists(t, branchCRDName)

	// Verify owner reference.
	if len(branch.OwnerReferences) == 0 {
		t.Fatal("GitBranch has no owner references")
	}
	ownerRef := branch.OwnerReferences[0]
	if ownerRef.Kind != "GitRepository" {
		t.Errorf("owner reference kind = %q, want %q", ownerRef.Kind, "GitRepository")
	}
	if ownerRef.Name != "e2e-watcher-ownerref-repo" {
		t.Errorf("owner reference name = %q, want %q", ownerRef.Name, "e2e-watcher-ownerref-repo")
	}
	if ownerRef.UID != repo.UID {
		t.Errorf("owner reference UID = %q, want %q", ownerRef.UID, repo.UID)
	}

	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(context.Background(), branchCRDName, metav1.DeleteOptions{})
	})
}

// TestRepoWatcherDetectsNewBranch verifies that when a new branch is created on
// Gitea, the repo-watcher creates a corresponding GitBranch CRD.
func TestRepoWatcherDetectsNewBranch(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-newbranch"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-newbranch-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-newbranch-repo", repoName, secretName, interval)

	// Wait for the initial "main" branch to be discovered.
	mainBranchCRD := "e2e-watcher-newbranch-repo-main"
	waitForGitBranchExists(t, mainBranchCRD)
	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(context.Background(), mainBranchCRD, metav1.DeleteOptions{})
	})

	// Create a new branch on Gitea.
	createGiteaBranch(t, repoName, "feature-new", "main")

	// The watcher should detect the new branch.
	featureBranchCRD := "e2e-watcher-newbranch-repo-feature-new"
	branch := waitForGitBranchExists(t, featureBranchCRD)

	if branch.Spec.BranchName != "feature-new" {
		t.Errorf("GitBranch branchName = %q, want %q", branch.Spec.BranchName, "feature-new")
	}

	// The feature branch should have the same commit as main (branched from it).
	expectedCommit := getGiteaBranchCommit(t, repoName, "feature-new")
	commit := waitForGitBranchCommit(t, featureBranchCRD)
	if commit != expectedCommit {
		t.Errorf("GitBranch headCommit = %q, want %q", commit, expectedCommit)
	}

	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(context.Background(), featureBranchCRD, metav1.DeleteOptions{})
	})
}

// TestRepoWatcherDetectsCommitUpdate verifies that when a new commit is pushed to
// a branch on Gitea, the repo-watcher updates the GitBranch headCommit.
func TestRepoWatcherDetectsCommitUpdate(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-commit"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-commit-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-commit-repo", repoName, secretName, interval)

	// Wait for initial discovery.
	branchCRDName := "e2e-watcher-commit-repo-main"
	waitForGitBranchExists(t, branchCRDName)
	oldCommit := waitForGitBranchCommit(t, branchCRDName)
	t.Logf("Initial headCommit: %s", oldCommit)

	// Push a new commit by creating a file on main via Gitea API.
	fileContent := base64.StdEncoding.EncodeToString([]byte("hello from e2e test"))
	newCommit := createGiteaFile(t, repoName, "main", "test-file.txt", fileContent)
	t.Logf("New commit after file creation: %s", newCommit)

	// Verify the commit changed on Gitea.
	giteaCommit := getGiteaBranchCommit(t, repoName, "main")
	if giteaCommit == oldCommit {
		t.Fatal("Gitea commit did not change after creating file")
	}

	// Wait for the watcher to pick up the new commit.
	waitForGitBranchHeadCommit(t, branchCRDName, giteaCommit)

	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(context.Background(), branchCRDName, metav1.DeleteOptions{})
	})
}

// TestRepoWatcherDeletesBranch verifies that when a branch is deleted on Gitea,
// the repo-watcher deletes the corresponding GitBranch CRD.
func TestRepoWatcherDeletesBranch(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-delete"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-delete-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-delete-repo", repoName, secretName, interval)

	// Create a feature branch on Gitea.
	createGiteaBranch(t, repoName, "to-delete", "main")

	// Wait for both branches to be discovered.
	mainCRD := "e2e-watcher-delete-repo-main"
	featureCRD := "e2e-watcher-delete-repo-to-delete"
	waitForGitBranchExists(t, mainCRD)
	waitForGitBranchExists(t, featureCRD)
	t.Logf("Both branches discovered")

	// Delete the feature branch on Gitea.
	deleteGiteaBranch(t, repoName, "to-delete")

	// Wait for the watcher to delete the GitBranch CRD.
	waitForGitBranchDeleted(t, featureCRD)

	// Main should still exist.
	ctx := context.Background()
	_, err := gitClient.GitBranches(testNamespace).Get(ctx, mainCRD, metav1.GetOptions{})
	if err != nil {
		t.Errorf("main GitBranch should still exist, but got error: %v", err)
	}

	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(ctx, mainCRD, metav1.DeleteOptions{})
	})
}

// TestRepoWatcherMultipleBranches verifies that the watcher discovers all branches
// on a repository, not just the default one.
func TestRepoWatcherMultipleBranches(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-multi"
	createGiteaRepo(t, repoName)

	// Create several branches on Gitea before creating the GitRepository CR.
	createGiteaBranch(t, repoName, "develop", "main")
	createGiteaBranch(t, repoName, "release-v1", "main")
	createGiteaBranch(t, repoName, "hotfix", "main")

	secretName := createCredentialsSecret(t, "e2e-watcher-multi-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-multi-repo", repoName, secretName, interval)

	// All four branches should be discovered.
	expectedBranches := map[string]string{
		"e2e-watcher-multi-repo-main":       "main",
		"e2e-watcher-multi-repo-develop":    "develop",
		"e2e-watcher-multi-repo-release-v1": "release-v1",
		"e2e-watcher-multi-repo-hotfix":     "hotfix",
	}

	ctx := context.Background()
	for crdName, branchName := range expectedBranches {
		branch := waitForGitBranchExists(t, crdName)
		if branch.Spec.BranchName != branchName {
			t.Errorf("GitBranch %q has branchName %q, want %q", crdName, branch.Spec.BranchName, branchName)
		}
		commit := waitForGitBranchCommit(t, crdName)
		if commit == "" {
			t.Errorf("GitBranch %q has empty headCommit", crdName)
		}
		t.Cleanup(func() {
			gitClient.GitBranches(testNamespace).Delete(ctx, crdName, metav1.DeleteOptions{})
		})
	}

	t.Logf("All %d branches discovered", len(expectedBranches))
}

// TestRepoWatcherLastFetchTime verifies that the controller updates
// GitRepository.status.lastFetchTime after each poll.
func TestRepoWatcherLastFetchTime(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-fetchtime"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-fetchtime-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-fetchtime-repo", repoName, secretName, interval)

	// Wait for the branch to be discovered (proves at least one poll happened).
	mainCRD := "e2e-watcher-fetchtime-repo-main"
	waitForGitBranchExists(t, mainCRD)

	// Check that lastFetchTime is set on the GitRepository.
	ctx := context.Background()
	deadline := time.Now().Add(pollTimeout)
	for time.Now().Before(deadline) {
		repo, err := gitClient.GitRepositories(testNamespace).Get(ctx, "e2e-watcher-fetchtime-repo", metav1.GetOptions{})
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if repo.Status.LastFetchTime != nil {
			t.Logf("GitRepository lastFetchTime: %v", repo.Status.LastFetchTime.Time)

			// Wait a few polls and check that it advances.
			firstFetch := repo.Status.LastFetchTime.Time
			time.Sleep(5 * time.Second)

			repo2, err := gitClient.GitRepositories(testNamespace).Get(ctx, "e2e-watcher-fetchtime-repo", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("getting GitRepository: %v", err)
			}
			if repo2.Status.LastFetchTime == nil {
				t.Fatal("lastFetchTime is nil after second check")
			}
			if !repo2.Status.LastFetchTime.Time.After(firstFetch) {
				t.Errorf("lastFetchTime did not advance: first=%v second=%v", firstFetch, repo2.Status.LastFetchTime.Time)
			}
			t.Logf("lastFetchTime advanced from %v to %v", firstFetch, repo2.Status.LastFetchTime.Time)

			t.Cleanup(func() {
				gitClient.GitBranches(testNamespace).Delete(context.Background(), mainCRD, metav1.DeleteOptions{})
			})
			return
		}
		time.Sleep(pollInterval)
	}
	t.Fatal("timed out waiting for lastFetchTime to be set")
}

// TestRepoWatcherLabels verifies that auto-created GitBranch CRDs have the
// repository label set correctly.
func TestRepoWatcherLabels(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-labels"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-watcher-labels-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-labels-repo", repoName, secretName, interval)

	branchCRDName := "e2e-watcher-labels-repo-main"
	branch := waitForGitBranchExists(t, branchCRDName)

	// Verify label.
	expectedLabel := "git-k8s.imjasonh.com/repository"
	labelValue, ok := branch.Labels[expectedLabel]
	if !ok {
		t.Fatalf("GitBranch missing label %q, labels: %v", expectedLabel, branch.Labels)
	}
	if labelValue != "e2e-watcher-labels-repo" {
		t.Errorf("label %q = %q, want %q", expectedLabel, labelValue, "e2e-watcher-labels-repo")
	}

	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(context.Background(), branchCRDName, metav1.DeleteOptions{})
	})
}

// TestRepoWatcherSlashBranchName verifies that branches with slashes in the name
// (e.g., "feature/foo") are handled correctly, with slashes replaced by dashes
// in the CRD name.
func TestRepoWatcherSlashBranchName(t *testing.T) {
	t.Parallel()

	repoName := "e2e-watcher-slash"
	createGiteaRepo(t, repoName)

	// Create a branch with a slash in the name.
	createGiteaBranch(t, repoName, "feature/my-thing", "main")

	secretName := createCredentialsSecret(t, "e2e-watcher-slash-creds")
	interval := metav1.Duration{Duration: 2 * time.Second}
	createGitRepositoryWithPollInterval(t, "e2e-watcher-slash-repo", repoName, secretName, interval)

	// The CRD name should have slashes replaced with dashes.
	mainCRD := "e2e-watcher-slash-repo-main"
	featureCRD := "e2e-watcher-slash-repo-feature-my-thing"

	waitForGitBranchExists(t, mainCRD)
	branch := waitForGitBranchExists(t, featureCRD)

	// But the spec.branchName should preserve the original slash.
	if branch.Spec.BranchName != "feature/my-thing" {
		t.Errorf("GitBranch branchName = %q, want %q", branch.Spec.BranchName, "feature/my-thing")
	}

	expectedCommit := getGiteaBranchCommit(t, repoName, fmt.Sprintf("feature/my-thing"))
	commit := waitForGitBranchCommit(t, featureCRD)
	if commit != expectedCommit {
		t.Errorf("headCommit = %q, want %q", commit, expectedCommit)
	}

	ctx := context.Background()
	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(ctx, mainCRD, metav1.DeleteOptions{})
		gitClient.GitBranches(testNamespace).Delete(ctx, featureCRD, metav1.DeleteOptions{})
	})
}
