package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ActionRequestSpec defines the desired state of an ActionRequest.
type ActionRequestSpec struct {
	// TargetRef identifies the resource the action will operate on.
	TargetRef TypedObjectReference `json:"targetRef"`
	// Action is the platform action name, such as backup or restart.
	// +kubebuilder:validation:MinLength=1
	Action string `json:"action"`
	// IdempotencyKey prevents accidental duplicate submissions.
	// +kubebuilder:validation:MinLength=1
	IdempotencyKey string `json:"idempotencyKey"`
	// Parameters holds action-specific configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	Parameters *apiextensionsv1.JSON `json:"parameters,omitempty"`
	// Approval captures the approval state for the action.
	Approval ApprovalSpec `json:"approval"`
	// RequestedBy identifies the actor that initiated the request.
	RequestedBy RequestedBySpec `json:"requestedBy"`
}

// ActionRequestStatus defines the observed state of an ActionRequest.
type ActionRequestStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse execution summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// StartedAt is when execution began.
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// CompletedAt is when execution completed.
	CompletedAt *metav1.Time `json:"completedAt,omitempty"`
	// OperationRef identifies the backing operation object if one exists.
	OperationRef *TypedObjectReference `json:"operationRef,omitempty"`
	// Result captures the execution outcome.
	Result ActionResultStatus `json:"result,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=actreq
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// ActionRequest is the audit envelope for asynchronous day-2 platform actions.
type ActionRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ActionRequestSpec   `json:"spec,omitempty"`
	Status ActionRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ActionRequestList contains a list of ActionRequest resources.
type ActionRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ActionRequest `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ActionRequest{}, &ActionRequestList{})
}
