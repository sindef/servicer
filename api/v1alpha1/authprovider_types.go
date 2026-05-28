package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// AuthProviderType identifies the identity provider implementation.
// +kubebuilder:validation:Enum=local;oidc;ldap
type AuthProviderType string

const (
	AuthProviderTypeLocal AuthProviderType = "local"
	AuthProviderTypeOIDC  AuthProviderType = "oidc"
	AuthProviderTypeLDAP  AuthProviderType = "ldap"
)

// OIDCAuthProviderSpec configures OIDC-backed authentication.
type OIDCAuthProviderSpec struct {
	// IssuerURL is the OIDC issuer discovery URL.
	IssuerURL string `json:"issuerUrl,omitempty"`
	// ClientID is the registered OIDC client identifier.
	ClientID string `json:"clientId,omitempty"`
	// ClientSecretRef references the Secret containing the client secret.
	ClientSecretRef NamespacedObjectReference `json:"clientSecretRef,omitempty"`
	// UsernameClaim is copied into display metadata for authenticated sessions.
	UsernameClaim string `json:"usernameClaim,omitempty"`
	// EmailClaim is copied into display metadata for authenticated sessions.
	EmailClaim string `json:"emailClaim,omitempty"`
	// RolesClaim is the claim that contains upstream role names.
	RolesClaim string `json:"rolesClaim,omitempty"`
	// GroupsClaim is the claim that contains upstream group names.
	GroupsClaim string `json:"groupsClaim,omitempty"`
	// Scopes are the OAuth scopes requested during login.
	Scopes []string `json:"scopes,omitempty"`
	// RedirectPath is the callback path served by the BFF.
	RedirectPath string `json:"redirectPath,omitempty"`
	// EndSessionURL overrides the provider end-session endpoint when needed.
	EndSessionURL string `json:"endSessionUrl,omitempty"`
}

// LDAPAuthProviderSpec configures LDAP-backed username/password authentication.
type LDAPAuthProviderSpec struct {
	// URL is the LDAP or LDAPS server URL.
	URL string `json:"url,omitempty"`
	// BindSecretRef references the bind account Secret used for directory searches.
	BindSecretRef NamespacedObjectReference `json:"bindSecretRef,omitempty"`
	// UserBaseDN is the base DN used to find user entries.
	UserBaseDN string `json:"userBaseDn,omitempty"`
	// UserFilter is the LDAP filter used to search for users. Use %s for the escaped username.
	UserFilter string `json:"userFilter,omitempty"`
	// UsernameAttribute is the attribute copied into the Servicer identity when present.
	UsernameAttribute string `json:"usernameAttribute,omitempty"`
	// EmailAttribute is the attribute copied into the Servicer identity when present.
	EmailAttribute string `json:"emailAttribute,omitempty"`
	// GroupBaseDN is the base DN used to discover group membership.
	GroupBaseDN string `json:"groupBaseDn,omitempty"`
	// GroupFilter is the LDAP filter used to search for groups. Use %s for the escaped user DN.
	GroupFilter string `json:"groupFilter,omitempty"`
	// GroupNameAttribute is the group attribute whose value becomes a Servicer group name.
	GroupNameAttribute string `json:"groupNameAttribute,omitempty"`
	// StartTLS enables StartTLS on plain LDAP connections.
	StartTLS bool `json:"startTls,omitempty"`
	// InsecureSkipVerify allows insecure TLS verification for test environments.
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`
}

// AuthProviderSpec defines one enterprise authentication provider.
type AuthProviderSpec struct {
	// DisplayName is the human-readable provider name shown in the login UI.
	DisplayName string `json:"displayName"`
	// Type selects the provider implementation.
	Type AuthProviderType `json:"type"`
	// Enabled exposes the provider for login.
	Enabled bool `json:"enabled"`
	// Default marks the provider as the preferred login option.
	Default bool `json:"default,omitempty"`
	// OIDC contains the OIDC-specific configuration when type=oidc.
	OIDC *OIDCAuthProviderSpec `json:"oidc,omitempty"`
	// LDAP contains the LDAP-specific configuration when type=ldap.
	LDAP *LDAPAuthProviderSpec `json:"ldap,omitempty"`
}

// AuthProviderStatus defines the observed state of an AuthProvider.
type AuthProviderStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains current readiness and validation conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=authp
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.spec.enabled`
// +kubebuilder:printcolumn:name="Default",type=boolean,JSONPath=`.spec.default`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// AuthProvider defines one authentication provider exposed by Servicer.
type AuthProvider struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthProviderSpec   `json:"spec,omitempty"`
	Status AuthProviderStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AuthProviderList contains a list of AuthProvider resources.
type AuthProviderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuthProvider `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AuthProvider{}, &AuthProviderList{})
}
