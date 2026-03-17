//go:build e2e

package e2e

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

// TestPushTransaction_CacheFree verifies that the push controller works
// correctly without PVC-backed caching (the default, backward-compatible mode).
// The GitRepository has no cache stanza, so controllers use in-memory clones.
func TestPushTransaction_CacheFree(t *testing.T) {
	t.Parallel()

	repoName := "e2e-push-nocache"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-push-nocache-creds")
	// No cache config — controllers fall back to in-memory.
	createGitRepository(t, "e2e-push-nocache-repo", repoName, secretName)
	createGitBranch(t, "e2e-push-nocache-feature", "e2e-push-nocache-repo", "feature-nocache")

	ctx := context.Background()
	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-nocache-txn",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "e2e-push-nocache-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/feature-nocache",
				},
			},
		},
	}
	_, err := gitClient.GitPushTransactions(testNamespace).Create(ctx, txn, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitPushTransaction: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitPushTransactions(testNamespace).Delete(ctx, "e2e-push-nocache-txn", metav1.DeleteOptions{})
	})

	result := waitForPushTransaction(t, "e2e-push-nocache-txn")
	if result.Status.ResultCommit == "" {
		t.Fatal("push transaction succeeded but resultCommit is empty")
	}
	t.Logf("Cache-free push result commit: %s", result.Status.ResultCommit)

	if !giteaBranchExists(t, repoName, "feature-nocache") {
		t.Fatal("branch feature-nocache does not exist on Gitea after cache-free push")
	}
}

// TestPushTransaction_CacheEnabled verifies that the push controller works
// correctly when the GitRepository has cache.enabled=true. Even if the
// controller pod doesn't have a PVC mounted (GIT_CACHE_DIR unset), the
// workspace manager falls back to in-memory and the push succeeds.
func TestPushTransaction_CacheEnabled(t *testing.T) {
	t.Parallel()

	repoName := "e2e-push-cached"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-push-cached-creds")

	// Create a GitRepository with cache.enabled=true.
	ctx := context.Background()
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-cached-repo",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           giteaInURL + "/" + giteaUsername + "/" + repoName + ".git",
			DefaultBranch: "main",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: secretName},
			},
			Cache: &gitv1alpha1.CacheConfig{
				Enabled: true,
			},
		},
	}
	_, err := gitClient.GitRepositories(testNamespace).Create(ctx, repo, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitRepository: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitRepositories(testNamespace).Delete(ctx, "e2e-push-cached-repo", metav1.DeleteOptions{})
	})

	createGitBranch(t, "e2e-push-cached-feature", "e2e-push-cached-repo", "feature-cached")

	txn := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-cached-txn",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "e2e-push-cached-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{
					Source:      "refs/heads/main",
					Destination: "refs/heads/feature-cached",
				},
			},
		},
	}
	_, err = gitClient.GitPushTransactions(testNamespace).Create(ctx, txn, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitPushTransaction: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitPushTransactions(testNamespace).Delete(ctx, "e2e-push-cached-txn", metav1.DeleteOptions{})
	})

	result := waitForPushTransaction(t, "e2e-push-cached-txn")
	if result.Status.ResultCommit == "" {
		t.Fatal("push transaction succeeded but resultCommit is empty")
	}
	t.Logf("Cache-enabled push result commit: %s", result.Status.ResultCommit)

	if !giteaBranchExists(t, repoName, "feature-cached") {
		t.Fatal("branch feature-cached does not exist on Gitea after cache-enabled push")
	}
}

// TestCacheConfig_SerializesCorrectly verifies that GitRepository resources
// with cache configuration can be created and read back correctly.
func TestCacheConfig_SerializesCorrectly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	repoName := "e2e-cache-serialize"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-cache-serialize-creds")

	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-cache-serialize-repo",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           giteaInURL + "/" + giteaUsername + "/" + repoName + ".git",
			DefaultBranch: "main",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: secretName},
			},
			Cache: &gitv1alpha1.CacheConfig{
				Enabled: true,
			},
		},
	}
	_, err := gitClient.GitRepositories(testNamespace).Create(ctx, repo, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitRepository: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitRepositories(testNamespace).Delete(ctx, "e2e-cache-serialize-repo", metav1.DeleteOptions{})
	})

	// Read back and verify cache config.
	got, err := gitClient.GitRepositories(testNamespace).Get(ctx, "e2e-cache-serialize-repo", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("getting GitRepository: %v", err)
	}
	if got.Spec.Cache == nil {
		t.Fatal("Spec.Cache is nil after round-trip")
	}
	if !got.Spec.Cache.Enabled {
		t.Error("Spec.Cache.Enabled = false, want true")
	}
}

// TestPushTransaction_CacheEnabled_MultiplePushes verifies that multiple
// push transactions to the same cache-enabled repo work correctly.
// This exercises the cache hit path if a PVC is mounted.
func TestPushTransaction_CacheEnabled_MultiplePushes(t *testing.T) {
	t.Parallel()

	repoName := "e2e-push-multi-cache"
	createGiteaRepo(t, repoName)

	secretName := createCredentialsSecret(t, "e2e-push-multi-cache-creds")

	ctx := context.Background()
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-multi-cache-repo",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           giteaInURL + "/" + giteaUsername + "/" + repoName + ".git",
			DefaultBranch: "main",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: secretName},
			},
			Cache: &gitv1alpha1.CacheConfig{Enabled: true},
		},
	}
	_, err := gitClient.GitRepositories(testNamespace).Create(ctx, repo, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitRepository: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitRepositories(testNamespace).Delete(ctx, "e2e-push-multi-cache-repo", metav1.DeleteOptions{})
	})

	// First push.
	createGitBranch(t, "e2e-push-mc-b1", "e2e-push-multi-cache-repo", "branch-mc-one")
	txn1 := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-mc-txn1",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "e2e-push-multi-cache-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{Source: "refs/heads/main", Destination: "refs/heads/branch-mc-one"},
			},
		},
	}
	_, err = gitClient.GitPushTransactions(testNamespace).Create(ctx, txn1, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating first GitPushTransaction: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitPushTransactions(testNamespace).Delete(ctx, "e2e-push-mc-txn1", metav1.DeleteOptions{})
	})

	result1 := waitForPushTransaction(t, "e2e-push-mc-txn1")
	if result1.Status.ResultCommit == "" {
		t.Fatal("first push succeeded but resultCommit is empty")
	}

	// Second push to a different branch.
	createGitBranch(t, "e2e-push-mc-b2", "e2e-push-multi-cache-repo", "branch-mc-two")
	txn2 := &gitv1alpha1.GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-push-mc-txn2",
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitPushTransactionSpec{
			RepositoryRef: "e2e-push-multi-cache-repo",
			RefSpecs: []gitv1alpha1.PushRefSpec{
				{Source: "refs/heads/main", Destination: "refs/heads/branch-mc-two"},
			},
		},
	}
	_, err = gitClient.GitPushTransactions(testNamespace).Create(ctx, txn2, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating second GitPushTransaction: %v", err)
	}
	t.Cleanup(func() {
		gitClient.GitPushTransactions(testNamespace).Delete(ctx, "e2e-push-mc-txn2", metav1.DeleteOptions{})
	})

	result2 := waitForPushTransaction(t, "e2e-push-mc-txn2")
	if result2.Status.ResultCommit == "" {
		t.Fatal("second push succeeded but resultCommit is empty")
	}

	// Verify both branches exist.
	if !giteaBranchExists(t, repoName, "branch-mc-one") {
		t.Error("branch-mc-one does not exist")
	}
	if !giteaBranchExists(t, repoName, "branch-mc-two") {
		t.Error("branch-mc-two does not exist")
	}
}
