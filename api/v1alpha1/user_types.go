package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// ExternalIdentitySpec maps a Servicer user to an upstream provider identity.
type ExternalIdentitySpec struct {
	// ProviderRef identifies the auth provider that asserts the subject.
	ProviderRef LocalObjectReference `json:"providerRef"`
	// Subject is the stable upstream identity subject.
	Subject string `json:"subject"`
}

// LocalAuthSpec configures Servicer-managed username/password login.
type LocalAuthSpec struct {
	// Enabled controls whether local login is allowed for this user.
	Enabled bool `json:"enabled"`
	// PasswordHashSecretRef references the Secret containing the password hash.
	PasswordHashSecretRef NamespacedObjectReference `json:"passwordHashSecretRef"`
}

// UserSpec defines a Servicer user identity.
type UserSpec struct {
	// DisplayName is the human-readable name.
	DisplayName string `json:"displayName,omitempty"`
	// Email is the primary email address when known.
	Email string `json:"email,omitempty"`
	// LocalAuth enables Servicer-managed password authentication.
	LocalAuth *LocalAuthSpec `json:"localAuth,omitempty"`
	// ExternalIdentities maps this user to one or more upstream provider subjects.
	ExternalIdentities []ExternalIdentitySpec `json:"externalIdentities,omitempty"`
}

// UserStatus defines the observed state of a User.
type UserStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions contains current validation and readiness conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=usr
// +kubebuilder:printcolumn:name="Email",type=string,JSONPath=`.spec.email`

// User defines a human identity known to Servicer.
type User struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UserSpec   `json:"spec,omitempty"`
	Status UserStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// UserList contains a list of User resources.
type UserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []User `json:"items"`
}

func init() {
	SchemeBuilder.Register(&User{}, &UserList{})
}
