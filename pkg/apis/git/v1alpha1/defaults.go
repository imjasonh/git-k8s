package v1alpha1

import "k8s.io/apimachinery/pkg/runtime"

// SetDefaults_GitRepository sets default values for GitRepository.
func SetDefaults_GitRepository(r *GitRepository) {
	if r.Spec.DefaultBranch == "" {
		r.Spec.DefaultBranch = "main"
	}
}

// SetDefaults_GitPushTransaction sets default values for GitPushTransaction.
func SetDefaults_GitPushTransaction(t *GitPushTransaction) {
	if t.Status.Phase == "" {
		t.Status.Phase = TransactionPhasePending
	}
}

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

// RegisterDefaults adds default functions to the given scheme.
// This is a placeholder that will be populated by code generation.
// For now we register defaults manually.
func RegisterDefaults(scheme *runtime.Scheme) error {
	scheme.AddTypeDefaultingFunc(&GitRepository{}, func(obj interface{}) {
		SetDefaults_GitRepository(obj.(*GitRepository))
	})
	scheme.AddTypeDefaultingFunc(&GitPushTransaction{}, func(obj interface{}) {
		SetDefaults_GitPushTransaction(obj.(*GitPushTransaction))
	})
	return nil
}
