package v1alpha1

import (
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
)

func TestSetDefaults_GitRepository(t *testing.T) {
	tests := []struct {
		name           string
		repo           *GitRepository
		wantBranch     string
	}{
		{
			name:       "empty default branch gets set to main",
			repo:       &GitRepository{},
			wantBranch: "main",
		},
		{
			name: "explicit default branch is preserved",
			repo: &GitRepository{
				Spec: GitRepositorySpec{
					DefaultBranch: "develop",
				},
			},
			wantBranch: "develop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaults_GitRepository(tt.repo)
			if got := tt.repo.Spec.DefaultBranch; got != tt.wantBranch {
				t.Errorf("DefaultBranch = %q, want %q", got, tt.wantBranch)
			}
		})
	}
}

func TestSetDefaults_GitPushTransaction(t *testing.T) {
	tests := []struct {
		name      string
		txn       *GitPushTransaction
		wantPhase TransactionPhase
	}{
		{
			name:      "empty phase gets set to Pending",
			txn:       &GitPushTransaction{},
			wantPhase: TransactionPhasePending,
		},
		{
			name: "existing phase is preserved",
			txn: &GitPushTransaction{
				Status: GitPushTransactionStatus{
					Phase: TransactionPhaseSucceeded,
				},
			},
			wantPhase: TransactionPhaseSucceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetDefaults_GitPushTransaction(tt.txn)
			if got := tt.txn.Status.Phase; got != tt.wantPhase {
				t.Errorf("Phase = %q, want %q", got, tt.wantPhase)
			}
		})
	}
}

func TestRegisterDefaults(t *testing.T) {
	scheme := runtime.NewScheme()

	// Register the types so the scheme knows about them.
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	// Register defaults.
	if err := RegisterDefaults(scheme); err != nil {
		t.Fatalf("RegisterDefaults() error = %v", err)
	}

	// Verify GitRepository defaults are applied via scheme.
	repo := &GitRepository{}
	scheme.Default(repo)
	if repo.Spec.DefaultBranch != "main" {
		t.Errorf("DefaultBranch after scheme.Default = %q, want %q", repo.Spec.DefaultBranch, "main")
	}

	// Verify GitPushTransaction defaults are applied via scheme.
	txn := &GitPushTransaction{}
	scheme.Default(txn)
	if txn.Status.Phase != TransactionPhasePending {
		t.Errorf("Phase after scheme.Default = %q, want %q", txn.Status.Phase, TransactionPhasePending)
	}
}
