package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSchemeRegistration(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme() error = %v", err)
	}

	// Verify all types are registered.
	types := []runtime.Object{
		&GitRepository{},
		&GitRepositoryList{},
		&GitBranch{},
		&GitBranchList{},
		&GitPushTransaction{},
		&GitPushTransactionList{},
		&GitRepoSync{},
		&GitRepoSyncList{},
	}

	for _, obj := range types {
		gvks, _, err := scheme.ObjectKinds(obj)
		if err != nil {
			t.Errorf("ObjectKinds(%T) error = %v", obj, err)
			continue
		}
		if len(gvks) == 0 {
			t.Errorf("ObjectKinds(%T) returned no GVKs", obj)
			continue
		}
		if gvks[0].Group != GroupName {
			t.Errorf("ObjectKinds(%T) group = %q, want %q", obj, gvks[0].Group, GroupName)
		}
		if gvks[0].Version != Version {
			t.Errorf("ObjectKinds(%T) version = %q, want %q", obj, gvks[0].Version, Version)
		}
	}
}

func TestGroupVersionConstants(t *testing.T) {
	if got := GroupName; got != "git-k8s.imjasonh.com" {
		t.Errorf("GroupName = %q, want %q", got, "git-k8s.imjasonh.com")
	}
	if got := Version; got != "v1alpha1" {
		t.Errorf("Version = %q, want %q", got, "v1alpha1")
	}
	if got := SchemeGroupVersion.String(); got != "git-k8s.imjasonh.com/v1alpha1" {
		t.Errorf("SchemeGroupVersion = %q, want %q", got, "git-k8s.imjasonh.com/v1alpha1")
	}
}

func TestKindAndResource(t *testing.T) {
	gk := Kind("GitRepository")
	if gk.Kind != "GitRepository" {
		t.Errorf("Kind().Kind = %q, want %q", gk.Kind, "GitRepository")
	}
	if gk.Group != GroupName {
		t.Errorf("Kind().Group = %q, want %q", gk.Group, GroupName)
	}

	gr := Resource("gitrepositories")
	if gr.Resource != "gitrepositories" {
		t.Errorf("Resource().Resource = %q, want %q", gr.Resource, "gitrepositories")
	}
	if gr.Group != GroupName {
		t.Errorf("Resource().Group = %q, want %q", gr.Group, GroupName)
	}
}

func TestTransactionPhaseConstants(t *testing.T) {
	phases := map[TransactionPhase]string{
		TransactionPhasePending:    "Pending",
		TransactionPhaseInProgress: "InProgress",
		TransactionPhaseSucceeded:  "Succeeded",
		TransactionPhaseFailed:     "Failed",
	}
	for phase, want := range phases {
		if string(phase) != want {
			t.Errorf("TransactionPhase constant = %q, want %q", phase, want)
		}
	}
}

func TestSyncPhaseConstants(t *testing.T) {
	phases := map[SyncPhase]string{
		SyncPhaseInSync:                     "InSync",
		SyncPhaseSyncing:                    "Syncing",
		SyncPhaseConflicted:                 "Conflicted",
		SyncPhaseRequiresManualIntervention: "RequiresManualIntervention",
	}
	for phase, want := range phases {
		if string(phase) != want {
			t.Errorf("SyncPhase constant = %q, want %q", phase, want)
		}
	}
}

func TestDeepCopy_GitRepository(t *testing.T) {
	orig := &GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-repo",
			Namespace: "default",
		},
		Spec: GitRepositorySpec{
			URL:           "https://github.com/example/repo.git",
			DefaultBranch: "main",
			Auth: &GitAuth{
				SecretRef: &SecretRef{Name: "my-secret"},
			},
		},
	}

	copy := orig.DeepCopy()

	// Verify values match.
	if copy.Name != orig.Name {
		t.Errorf("DeepCopy Name = %q, want %q", copy.Name, orig.Name)
	}
	if copy.Spec.URL != orig.Spec.URL {
		t.Errorf("DeepCopy URL = %q, want %q", copy.Spec.URL, orig.Spec.URL)
	}
	if copy.Spec.Auth.SecretRef.Name != orig.Spec.Auth.SecretRef.Name {
		t.Errorf("DeepCopy SecretRef = %q, want %q", copy.Spec.Auth.SecretRef.Name, orig.Spec.Auth.SecretRef.Name)
	}

	// Verify mutation isolation.
	copy.Spec.Auth.SecretRef.Name = "changed"
	if orig.Spec.Auth.SecretRef.Name == "changed" {
		t.Error("DeepCopy did not isolate Auth.SecretRef mutation")
	}
}

func TestDeepCopy_GitPushTransaction(t *testing.T) {
	now := metav1.Now()
	orig := &GitPushTransaction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-push",
			Namespace: "default",
		},
		Spec: GitPushTransactionSpec{
			RepositoryRef: "my-repo",
			Atomic:        true,
			RefSpecs: []PushRefSpec{
				{
					Source:            "refs/heads/main",
					Destination:       "refs/heads/feature",
					ExpectedOldCommit: "abc123",
				},
			},
		},
		Status: GitPushTransactionStatus{
			Phase:          TransactionPhaseSucceeded,
			ResultCommit:   "def456",
			StartTime:      &now,
			CompletionTime: &now,
			Message:        "done",
		},
	}

	copy := orig.DeepCopy()

	if len(copy.Spec.RefSpecs) != 1 {
		t.Fatalf("DeepCopy RefSpecs length = %d, want 1", len(copy.Spec.RefSpecs))
	}
	if copy.Spec.RefSpecs[0].Source != "refs/heads/main" {
		t.Errorf("DeepCopy RefSpec Source = %q, want %q", copy.Spec.RefSpecs[0].Source, "refs/heads/main")
	}

	// Verify mutation isolation on slice.
	copy.Spec.RefSpecs[0].Source = "changed"
	if orig.Spec.RefSpecs[0].Source == "changed" {
		t.Error("DeepCopy did not isolate RefSpecs slice mutation")
	}
}

func TestDeepCopy_GitRepoSync(t *testing.T) {
	orig := &GitRepoSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sync",
			Namespace: "default",
		},
		Spec: GitRepoSyncSpec{
			RepoA:      SyncRepoRef{Name: "repo-a"},
			RepoB:      SyncRepoRef{Name: "repo-b"},
			BranchName: "main",
		},
		Status: GitRepoSyncStatus{
			Phase:       SyncPhaseInSync,
			RepoACommit: "aaa",
			RepoBCommit: "bbb",
			MergeBase:   "ccc",
			Message:     "synced",
		},
	}

	copy := orig.DeepCopy()

	if copy.Spec.RepoA.Name != "repo-a" {
		t.Errorf("DeepCopy RepoA = %q, want %q", copy.Spec.RepoA.Name, "repo-a")
	}
	if copy.Status.Phase != SyncPhaseInSync {
		t.Errorf("DeepCopy Phase = %q, want %q", copy.Status.Phase, SyncPhaseInSync)
	}

	copy.Status.Phase = SyncPhaseConflicted
	if orig.Status.Phase == SyncPhaseConflicted {
		t.Error("DeepCopy did not isolate Status mutation")
	}
}
