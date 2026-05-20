package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// PolicyTargetKind identifies the Servicer resource kind a policy applies to.
// +kubebuilder:validation:Enum=ServiceInstance
type PolicyTargetKind string

const (
	// PolicyTargetServiceInstance evaluates policy rules against ServiceInstance requests.
	PolicyTargetServiceInstance PolicyTargetKind = "ServiceInstance"
)

// PolicyOperator identifies the rule comparison operator.
// +kubebuilder:validation:Enum=equals;not-equals;in;not-in;exists;not-exists;gt;gte;lt;lte;empty;not-empty
type PolicyOperator string

const (
	PolicyOperatorEquals    PolicyOperator = "equals"
	PolicyOperatorNotEquals PolicyOperator = "not-equals"
	PolicyOperatorIn        PolicyOperator = "in"
	PolicyOperatorNotIn     PolicyOperator = "not-in"
	PolicyOperatorExists    PolicyOperator = "exists"
	PolicyOperatorNotExists PolicyOperator = "not-exists"
	PolicyOperatorGT        PolicyOperator = "gt"
	PolicyOperatorGTE       PolicyOperator = "gte"
	PolicyOperatorLT        PolicyOperator = "lt"
	PolicyOperatorLTE       PolicyOperator = "lte"
	PolicyOperatorEmpty     PolicyOperator = "empty"
	PolicyOperatorNotEmpty  PolicyOperator = "not-empty"
)

// PolicyRule defines one user-authored validation rule.
type PolicyRule struct {
	// Name is a stable identifier for the rule inside the policy.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// Path identifies the evaluated field path, such as spec.exposure.mode or parameters.replicas.
	Path string `json:"path,omitempty"`
	// Operator selects the comparison operation.
	Operator PolicyOperator `json:"operator"`
	// Value is the single comparison value used by scalar operators.
	Value string `json:"value,omitempty"`
	// Values are the comparison values used by set operators.
	Values []string `json:"values,omitempty"`
	// Message is returned when the rule is violated.
	Message string `json:"message,omitempty"`
}

// PolicySpec defines the desired state of a Policy.
type PolicySpec struct {
	// DisplayName is the human-readable name for the policy.
	// +kubebuilder:validation:MinLength=1
	DisplayName string `json:"displayName"`
	// Description summarizes the policy intent.
	Description string `json:"description,omitempty"`
	// TargetKinds lists the Servicer kinds this policy can evaluate.
	// +kubebuilder:validation:MinItems=1
	TargetKinds []PolicyTargetKind `json:"targetKinds"`
	// Rules are evaluated as deny rules when referenced by a resource.
	// +kubebuilder:validation:MinItems=1
	Rules []PolicyRule `json:"rules"`
}

// PolicyStatus defines the observed state of a Policy.
type PolicyStatus struct {
	// ObservedGeneration is the most recent processed generation.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is the coarse lifecycle summary.
	Phase string `json:"phase,omitempty"`
	// Conditions contains the current status conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// RuleCount is the number of accepted rules.
	RuleCount int32 `json:"ruleCount,omitempty"`
	// TargetKinds echoes the accepted target kinds for quick status inspection.
	TargetKinds []string `json:"targetKinds,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=pol
// +kubebuilder:printcolumn:name="Targets",type=string,JSONPath=`.status.targetKinds`
// +kubebuilder:printcolumn:name="Rules",type=integer,JSONPath=`.status.ruleCount`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`

// Policy is a reusable user-defined policy bundle referenced by tenancy and catalog objects.
type Policy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PolicySpec   `json:"spec,omitempty"`
	Status PolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PolicyList contains a list of Policy resources.
type PolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Policy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Policy{}, &PolicyList{})
}

func (in *PolicyRule) DeepCopyInto(out *PolicyRule) {
	*out = *in
	if in.Values != nil {
		in, out := &in.Values, &out.Values
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *PolicyRule) DeepCopy() *PolicyRule {
	if in == nil {
		return nil
	}
	out := new(PolicyRule)
	in.DeepCopyInto(out)
	return out
}

func (in *PolicySpec) DeepCopyInto(out *PolicySpec) {
	*out = *in
	if in.TargetKinds != nil {
		in, out := &in.TargetKinds, &out.TargetKinds
		*out = make([]PolicyTargetKind, len(*in))
		copy(*out, *in)
	}
	if in.Rules != nil {
		in, out := &in.Rules, &out.Rules
		*out = make([]PolicyRule, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *PolicySpec) DeepCopy() *PolicySpec {
	if in == nil {
		return nil
	}
	out := new(PolicySpec)
	in.DeepCopyInto(out)
	return out
}

func (in *PolicyStatus) DeepCopyInto(out *PolicyStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]metav1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.TargetKinds != nil {
		in, out := &in.TargetKinds, &out.TargetKinds
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
}

func (in *PolicyStatus) DeepCopy() *PolicyStatus {
	if in == nil {
		return nil
	}
	out := new(PolicyStatus)
	in.DeepCopyInto(out)
	return out
}

func (in *Policy) DeepCopyInto(out *Policy) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *Policy) DeepCopy() *Policy {
	if in == nil {
		return nil
	}
	out := new(Policy)
	in.DeepCopyInto(out)
	return out
}

func (in *Policy) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

func (in *PolicyList) DeepCopyInto(out *PolicyList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Policy, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

func (in *PolicyList) DeepCopy() *PolicyList {
	if in == nil {
		return nil
	}
	out := new(PolicyList)
	in.DeepCopyInto(out)
	return out
}

func (in *PolicyList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
