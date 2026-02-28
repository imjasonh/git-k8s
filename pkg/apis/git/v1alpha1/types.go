package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"knative.dev/pkg/apis/duck/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitRepository represents a Git repository managed by the control plane.
type GitRepository struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitRepositorySpec   `json:"spec"`
	Status GitRepositoryStatus `json:"status,omitempty"`
}

// GitRepositorySpec defines the desired state of a GitRepository.
type GitRepositorySpec struct {
	// URL is the clone URL of the Git repository.
	URL string `json:"url"`

	// DefaultBranch is the default branch name (e.g., "main").
	// +optional
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// Auth contains authentication configuration for the repository.
	// +optional
	Auth *GitAuth `json:"auth,omitempty"`

	// PollInterval is how often to poll the remote for ref changes.
	// Specified as a duration string (e.g., "5s", "30s", "1m").
	// If unset, the controller's default poll interval is used.
	// +optional
	PollInterval *metav1.Duration `json:"pollInterval,omitempty"`
}

// GitAuth contains authentication details for accessing a Git repository.
type GitAuth struct {
	// SecretRef references a Secret containing credentials.
	// The Secret should have keys "username" and "password" or "ssh-privatekey".
	// +optional
	SecretRef *SecretRef `json:"secretRef,omitempty"`
}

// SecretRef is a reference to a Kubernetes Secret.
type SecretRef struct {
	// Name is the name of the Secret.
	Name string `json:"name"`
}

// GitRepositoryStatus defines the observed state of a GitRepository.
type GitRepositoryStatus struct {
	v1.Status `json:",inline"`

	// LastFetchTime is the timestamp of the last successful fetch.
	// +optional
	LastFetchTime *metav1.Time `json:"lastFetchTime,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitRepositoryList is a list of GitRepository resources.
type GitRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitRepository `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitBranch represents a branch within a GitRepository.
type GitBranch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitBranchSpec   `json:"spec"`
	Status GitBranchStatus `json:"status,omitempty"`
}

// GitBranchSpec defines the desired state of a GitBranch.
type GitBranchSpec struct {
	// RepositoryRef references the parent GitRepository.
	RepositoryRef string `json:"repositoryRef"`

	// BranchName is the name of the branch (e.g., "main", "feature/foo").
	BranchName string `json:"branchName"`
}

// GitBranchStatus defines the observed state of a GitBranch.
type GitBranchStatus struct {
	v1.Status `json:",inline"`

	// HeadCommit is the SHA of the current HEAD commit on this branch.
	// +optional
	HeadCommit string `json:"headCommit,omitempty"`

	// LastUpdated is the timestamp when the branch was last updated.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitBranchList is a list of GitBranch resources.
type GitBranchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitBranch `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitPushTransaction represents an atomic push operation to a Git repository.
type GitPushTransaction struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitPushTransactionSpec   `json:"spec"`
	Status GitPushTransactionStatus `json:"status,omitempty"`
}

// GitPushTransactionSpec defines the desired state of a GitPushTransaction.
type GitPushTransactionSpec struct {
	// RepositoryRef references the target GitRepository.
	RepositoryRef string `json:"repositoryRef"`

	// RefSpecs defines the Git refspecs for this push.
	RefSpecs []PushRefSpec `json:"refSpecs"`

	// Atomic indicates whether the push should be atomic.
	// +optional
	Atomic bool `json:"atomic,omitempty"`
}

// PushRefSpec defines a single refspec for a push operation.
type PushRefSpec struct {
	// Source is the local ref (commit SHA or branch name).
	Source string `json:"source"`

	// Destination is the remote ref to update.
	Destination string `json:"destination"`

	// ExpectedOldCommit is the expected current commit SHA of the destination.
	// Used for compare-and-swap (CAS) to prevent races.
	// +optional
	ExpectedOldCommit string `json:"expectedOldCommit,omitempty"`
}

// TransactionPhase represents the current phase of a push transaction.
type TransactionPhase string

const (
	TransactionPhasePending    TransactionPhase = "Pending"
	TransactionPhaseInProgress TransactionPhase = "InProgress"
	TransactionPhaseSucceeded  TransactionPhase = "Succeeded"
	TransactionPhaseFailed     TransactionPhase = "Failed"
)

// GitPushTransactionStatus defines the observed state of a GitPushTransaction.
type GitPushTransactionStatus struct {
	v1.Status `json:",inline"`

	// Phase indicates the current phase of the transaction.
	// +optional
	Phase TransactionPhase `json:"phase,omitempty"`

	// ResultCommit is the SHA of the resulting commit after a successful push.
	// +optional
	ResultCommit string `json:"resultCommit,omitempty"`

	// StartTime is when the push operation started.
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the push operation completed.
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message provides additional details about the current state.
	// +optional
	Message string `json:"message,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitPushTransactionList is a list of GitPushTransaction resources.
type GitPushTransactionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitPushTransaction `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitRepoSync defines a two-way sync relationship between two GitRepositories.
type GitRepoSync struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitRepoSyncSpec   `json:"spec"`
	Status GitRepoSyncStatus `json:"status,omitempty"`
}

// GitRepoSyncSpec defines the desired state of a GitRepoSync.
type GitRepoSyncSpec struct {
	// RepoA references the first GitRepository.
	RepoA SyncRepoRef `json:"repoA"`

	// RepoB references the second GitRepository.
	RepoB SyncRepoRef `json:"repoB"`

	// BranchName is the branch to keep in sync between the two repos.
	BranchName string `json:"branchName"`
}

// SyncRepoRef references a GitRepository and optional branch override.
type SyncRepoRef struct {
	// Name is the name of the GitRepository resource.
	Name string `json:"name"`
}

// SyncPhase represents the current phase of a repo sync.
type SyncPhase string

const (
	SyncPhaseInSync                     SyncPhase = "InSync"
	SyncPhaseSyncing                    SyncPhase = "Syncing"
	SyncPhaseConflicted                 SyncPhase = "Conflicted"
	SyncPhaseRequiresManualIntervention SyncPhase = "RequiresManualIntervention"
)

// GitRepoSyncStatus defines the observed state of a GitRepoSync.
type GitRepoSyncStatus struct {
	v1.Status `json:",inline"`

	// Phase indicates the current phase of the sync.
	// +optional
	Phase SyncPhase `json:"phase,omitempty"`

	// LastSyncTime is the timestamp of the last successful sync.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// RepoACommit is the last known commit SHA of repo A.
	// +optional
	RepoACommit string `json:"repoACommit,omitempty"`

	// RepoBCommit is the last known commit SHA of repo B.
	// +optional
	RepoBCommit string `json:"repoBCommit,omitempty"`

	// MergeBase is the common ancestor commit SHA.
	// +optional
	MergeBase string `json:"mergeBase,omitempty"`

	// Message provides additional details about the current state.
	// +optional
	Message string `json:"message,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GitRepoSyncList is a list of GitRepoSync resources.
type GitRepoSyncList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GitRepoSync `json:"items"`
}
