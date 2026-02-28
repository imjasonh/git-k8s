package repowatcher

import (
	"testing"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	gitv1alpha1 "github.com/imjasonh/git-k8s/pkg/apis/git/v1alpha1"
)

func TestBranchCRDName(t *testing.T) {
	tests := []struct {
		repo   string
		branch string
		want   string
	}{
		{"my-repo", "main", "my-repo-main"},
		{"my-repo", "feature/foo", "my-repo-feature-foo"},
		{"my-repo", "release/v1.0", "my-repo-release-v1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			got := branchCRDName(tt.repo, tt.branch)
			if got != tt.want {
				t.Errorf("branchCRDName(%q, %q) = %q, want %q", tt.repo, tt.branch, got, tt.want)
			}
		})
	}
}

func TestPollInterval(t *testing.T) {
	r := &Reconciler{defaultInterval: 30 * time.Second}

	// No override — use default.
	repo := &gitv1alpha1.GitRepository{}
	if got := r.pollInterval(repo); got != 30*time.Second {
		t.Errorf("pollInterval (default) = %v, want 30s", got)
	}

	// Per-repo override.
	d := metav1.Duration{Duration: 5 * time.Second}
	repo.Spec.PollInterval = &d
	if got := r.pollInterval(repo); got != 5*time.Second {
		t.Errorf("pollInterval (override) = %v, want 5s", got)
	}
}

func TestMinLen(t *testing.T) {
	if got := minLen(10, 7); got != 7 {
		t.Errorf("minLen(10, 7) = %d, want 7", got)
	}
	if got := minLen(3, 7); got != 3 {
		t.Errorf("minLen(3, 7) = %d, want 3", got)
	}
}

func TestMockLsRemote(t *testing.T) {
	// Verify the mock lsRemote pattern works.
	refs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aaaa")),
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("feature/foo"), plumbing.NewHash("bbbb")),
		plumbing.NewHashReference(plumbing.NewTagReferenceName("v1.0"), plumbing.NewHash("cccc")),
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main")),
	}

	mockLsRemote := func(url string, auth *http.BasicAuth) ([]*plumbing.Reference, error) {
		return refs, nil
	}

	got, err := mockLsRemote("http://example.com/repo.git", nil)
	if err != nil {
		t.Fatalf("mockLsRemote() error = %v", err)
	}

	// Count branch refs.
	branches := 0
	for _, ref := range got {
		if ref.Name().IsBranch() {
			branches++
		}
	}
	if branches != 2 {
		t.Errorf("branch count = %d, want 2", branches)
	}
}

func TestRefFiltering(t *testing.T) {
	// Simulate what the reconciler does: filter ls-remote output to branches only.
	refs := []*plumbing.Reference{
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), plumbing.NewHash("aaaa")),
		plumbing.NewHashReference(plumbing.NewBranchReferenceName("develop"), plumbing.NewHash("bbbb")),
		plumbing.NewHashReference(plumbing.NewTagReferenceName("v1.0"), plumbing.NewHash("cccc")),
		plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", "main"), plumbing.NewHash("dddd")),
		plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main")),
	}

	remoteBranches := make(map[string]string)
	for _, ref := range refs {
		name := ref.Name()
		if name.IsBranch() {
			remoteBranches[name.Short()] = ref.Hash().String()
		}
	}

	if len(remoteBranches) != 2 {
		t.Errorf("filtered branches = %d, want 2", len(remoteBranches))
	}
	if _, ok := remoteBranches["main"]; !ok {
		t.Error("missing branch 'main'")
	}
	if _, ok := remoteBranches["develop"]; !ok {
		t.Error("missing branch 'develop'")
	}
	// Tags and remote refs should not be included.
	if _, ok := remoteBranches["v1.0"]; ok {
		t.Error("tag v1.0 should not be in branches")
	}
}
