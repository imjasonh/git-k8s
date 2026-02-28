//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
	gitclient "github.com/imjasonh/git-k8s/pkg/client"
)

const (
	testNamespace = "default"
	giteaUsername = "testadmin"
	giteaPassword = "testpassword123"

	pollInterval = 1 * time.Second
	pollTimeout  = 2 * time.Minute
)

var (
	gitClient  *gitclient.GitV1alpha1Client
	kubeClient kubernetes.Interface
	giteaURL   string // localhost URL for test process (via port-forward)
	giteaInURL string // in-cluster URL for controllers
)

func TestMain(m *testing.M) {
	// Resolve Gitea URLs.
	giteaURL = envOrDefault("GITEA_URL", "http://localhost:3000")
	giteaInURL = envOrDefault("GITEA_INTERNAL_URL", "http://gitea.git-system.svc.cluster.local:3000")

	// Build clients from KUBECONFIG.
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = clientcmd.RecommendedHomeFile
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "building kubeconfig: %v\n", err)
		os.Exit(1)
	}

	kubeClient, err = kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating kubernetes client: %v\n", err)
		os.Exit(1)
	}

	dynClient, err := dynamic.NewForConfig(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "creating dynamic client: %v\n", err)
		os.Exit(1)
	}
	gitClient = gitclient.NewFromDynamic(dynClient)

	os.Exit(m.Run())
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// createGiteaRepo creates a repository on Gitea via the API.
func createGiteaRepo(t *testing.T, name string) {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"name":           name,
		"auto_init":      true,
		"default_branch": "main",
	})
	req, err := http.NewRequest("POST", giteaURL+"/api/v1/user/repos", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(giteaUsername, giteaPassword)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("creating Gitea repo %q: %v", name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("creating Gitea repo %q: status %d", name, resp.StatusCode)
	}
	t.Logf("Created Gitea repo %q", name)
}

// getGiteaBranchCommit fetches the HEAD commit of a branch from Gitea.
func getGiteaBranchCommit(t *testing.T, repo, branch string) string {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s", giteaURL, giteaUsername, repo, branch)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(giteaUsername, giteaPassword)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("getting branch %s/%s: %v", repo, branch, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("getting branch %s/%s: status %d", repo, branch, resp.StatusCode)
	}

	var result struct {
		Commit struct {
			ID string `json:"id"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decoding branch response: %v", err)
	}
	return result.Commit.ID
}

// giteaBranchExists checks if a branch exists on Gitea.
func giteaBranchExists(t *testing.T, repo, branch string) bool {
	t.Helper()
	url := fmt.Sprintf("%s/api/v1/repos/%s/%s/branches/%s", giteaURL, giteaUsername, repo, branch)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(giteaUsername, giteaPassword)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("checking branch %s/%s: %v", repo, branch, err)
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// createCredentialsSecret creates a Secret with Gitea credentials.
// It returns the secret name. The secret is cleaned up when the test finishes.
func createCredentialsSecret(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		StringData: map[string]string{
			"username": giteaUsername,
			"password": giteaPassword,
		},
	}
	_, err := kubeClient.CoreV1().Secrets(testNamespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating secret %q: %v", name, err)
	}
	t.Cleanup(func() {
		kubeClient.CoreV1().Secrets(testNamespace).Delete(ctx, name, metav1.DeleteOptions{})
	})
	return name
}

// createGitRepository creates a GitRepository CR pointing at a Gitea repo.
func createGitRepository(t *testing.T, name, repoName, secretName string) *gitv1alpha1.GitRepository {
	t.Helper()
	ctx := context.Background()
	repo := &gitv1alpha1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitRepositorySpec{
			URL:           fmt.Sprintf("%s/%s/%s.git", giteaInURL, giteaUsername, repoName),
			DefaultBranch: "main",
			Auth: &gitv1alpha1.GitAuth{
				SecretRef: &gitv1alpha1.SecretRef{Name: secretName},
			},
		},
	}
	created, err := gitClient.GitRepositories(testNamespace).Create(ctx, repo, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitRepository %q: %v", name, err)
	}
	t.Cleanup(func() {
		gitClient.GitRepositories(testNamespace).Delete(ctx, name, metav1.DeleteOptions{})
	})
	return created
}

// createGitBranch creates a GitBranch CR.
func createGitBranch(t *testing.T, name, repoRef, branchName string) *gitv1alpha1.GitBranch {
	t.Helper()
	ctx := context.Background()
	branch := &gitv1alpha1.GitBranch{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Spec: gitv1alpha1.GitBranchSpec{
			RepositoryRef: repoRef,
			BranchName:    branchName,
		},
	}
	created, err := gitClient.GitBranches(testNamespace).Create(ctx, branch, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("creating GitBranch %q: %v", name, err)
	}
	t.Cleanup(func() {
		gitClient.GitBranches(testNamespace).Delete(ctx, name, metav1.DeleteOptions{})
	})
	return created
}

// waitForPushTransaction polls until the GitPushTransaction reaches a terminal phase.
func waitForPushTransaction(t *testing.T, name string) *gitv1alpha1.GitPushTransaction {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(pollTimeout)

	var lastTxn *gitv1alpha1.GitPushTransaction
	for time.Now().Before(deadline) {
		txn, err := gitClient.GitPushTransactions(testNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			t.Logf("getting push transaction %q: %v (retrying)", name, err)
			time.Sleep(pollInterval)
			continue
		}
		lastTxn = txn
		t.Logf("GitPushTransaction %q phase: %s", name, txn.Status.Phase)
		switch txn.Status.Phase {
		case gitv1alpha1.TransactionPhaseSucceeded:
			return txn
		case gitv1alpha1.TransactionPhaseFailed:
			t.Fatalf("GitPushTransaction %q failed: %s", name, txn.Status.Message)
		}
		time.Sleep(pollInterval)
	}
	if lastTxn != nil {
		t.Logf("Last seen state: phase=%q message=%q resourceVersion=%s", lastTxn.Status.Phase, lastTxn.Status.Message, lastTxn.ResourceVersion)
	}
	t.Fatalf("timed out waiting for GitPushTransaction %q", name)
	return nil
}

// waitForGitBranchCommit polls until the GitBranch has a non-empty headCommit.
func waitForGitBranchCommit(t *testing.T, name string) string {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(pollTimeout)

	for time.Now().Before(deadline) {
		branch, err := gitClient.GitBranches(testNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if branch.Status.HeadCommit != "" {
			return branch.Status.HeadCommit
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("timed out waiting for GitBranch %q headCommit", name)
	return ""
}

// waitForSyncPhase polls until the GitRepoSync reaches a non-empty phase.
func waitForSyncPhase(t *testing.T, name string) *gitv1alpha1.GitRepoSync {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(pollTimeout)

	for time.Now().Before(deadline) {
		syncObj, err := gitClient.GitRepoSyncs(testNamespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			time.Sleep(pollInterval)
			continue
		}
		if syncObj.Status.Phase != "" {
			t.Logf("GitRepoSync %q phase: %s", name, syncObj.Status.Phase)
			return syncObj
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("timed out waiting for GitRepoSync %q phase", name)
	return nil
}
