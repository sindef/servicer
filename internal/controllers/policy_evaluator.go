package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type planConstraints struct {
	AllowedExposureModes       []string `json:"allowedExposureModes,omitempty"`
	AllowedDeletionPolicies    []string `json:"allowedDeletionPolicies,omitempty"`
	RequiredSecretDeliveryMode string   `json:"requiredSecretDeliveryMode,omitempty"`
	MaxReplicas                *int32   `json:"maxReplicas,omitempty"`
	RequireBackupProfile       bool     `json:"requireBackupProfile,omitempty"`
}

type serviceInstanceParameters struct {
	Replicas      *int32 `json:"replicas,omitempty"`
	Instances     *int32 `json:"instances,omitempty"`
	BackupProfile string `json:"backupProfile,omitempty"`
}

func evaluateInstancePolicies(ctx context.Context, c client.Client, instance *platformv1alpha1.ServiceInstance, tenant *platformv1alpha1.Tenant, project *platformv1alpha1.Project, plan *platformv1alpha1.ServicePlan) field.ErrorList {
	allErrs := field.ErrorList{}
	if instance == nil {
		return allErrs
	}

	policyRefs := mergedPolicyRefs(tenant, project, plan)
	for _, policy := range policyRefs {
		if curatedErrs, handled := evaluateCuratedPolicy(policy, instance); handled {
			allErrs = append(allErrs, curatedErrs...)
			continue
		}
		if c == nil {
			allErrs = append(allErrs, field.InternalError(field.NewPath("spec"), fmt.Errorf("policy client is unavailable for referenced policy %q", policy)))
			continue
		}
		var definedPolicy platformv1alpha1.Policy
		if err := c.Get(ctx, client.ObjectKey{Name: policy}, &definedPolicy); err != nil {
			if apierrors.IsNotFound(err) {
				allErrs = append(allErrs, field.NotFound(field.NewPath("spec", "policyRefs"), policy))
				continue
			}
			allErrs = append(allErrs, field.InternalError(field.NewPath("spec", "policyRefs"), fmt.Errorf("resolve policy %q: %w", policy, err)))
			continue
		}
		allErrs = append(allErrs, evaluateDefinedPolicy(&definedPolicy, instance)...)
	}

	constraints := decodePlanConstraints(plan)
	if len(constraints.AllowedExposureModes) > 0 && !stringInSlice(string(instance.Spec.Exposure.Mode), constraints.AllowedExposureModes) {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "exposure", "mode"), instance.Spec.Exposure.Mode, constraints.AllowedExposureModes))
	}
	if len(constraints.AllowedDeletionPolicies) > 0 && !stringInSlice(string(instance.Spec.DeletionPolicy), constraints.AllowedDeletionPolicies) {
		allErrs = append(allErrs, field.NotSupported(field.NewPath("spec", "deletionPolicy"), instance.Spec.DeletionPolicy, constraints.AllowedDeletionPolicies))
	}
	if constraints.RequiredSecretDeliveryMode != "" && string(instance.Spec.SecretPolicy.DeliveryMode) != constraints.RequiredSecretDeliveryMode {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "secretPolicy", "deliveryMode"), instance.Spec.SecretPolicy.DeliveryMode, fmt.Sprintf("plan requires %q", constraints.RequiredSecretDeliveryMode)))
	}
	params := instanceServiceParameters(instance)
	replicas := firstNonNilInt32(params.Replicas, params.Instances)
	if constraints.MaxReplicas != nil && replicas != nil && *replicas > *constraints.MaxReplicas {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "parameters", "replicas"), *replicas, fmt.Sprintf("plan allows at most %d replicas", *constraints.MaxReplicas)))
	}
	if constraints.RequireBackupProfile && params.BackupProfile == "" {
		allErrs = append(allErrs, field.Required(field.NewPath("spec", "parameters", "backupProfile"), "plan requires backupProfile"))
	}
	return allErrs
}

func evaluateCuratedPolicy(policy string, instance *platformv1alpha1.ServiceInstance) (field.ErrorList, bool) {
	allErrs := field.ErrorList{}
	switch policy {
	case "deny-public-ingress":
		if instance.Spec.Exposure.Mode == platformv1alpha1.ExposureModePublicIngress {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "exposure", "mode"), "policy deny-public-ingress forbids public ingress"))
		}
		return allErrs, true
	case "require-external-secrets":
		if instance.Spec.SecretPolicy.DeliveryMode != platformv1alpha1.SecretDeliveryModeExternalSecret {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "secretPolicy", "deliveryMode"), "policy require-external-secrets requires external-secret delivery"))
		}
		return allErrs, true
	case "require-backups":
		params := instanceServiceParameters(instance)
		if params.BackupProfile == "" {
			allErrs = append(allErrs, field.Required(field.NewPath("spec", "parameters", "backupProfile"), "policy require-backups requires backupProfile"))
		}
		return allErrs, true
	case "protect-delete":
		if instance.Spec.DeletionPolicy == platformv1alpha1.DeletionPolicyDelete {
			allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "deletionPolicy"), "policy protect-delete forbids direct delete policy"))
		}
		return allErrs, true
	}
	return nil, false
}

func evaluateDefinedPolicy(policy *platformv1alpha1.Policy, instance *platformv1alpha1.ServiceInstance) field.ErrorList {
	allErrs := field.ErrorList{}
	if policy == nil || instance == nil {
		return allErrs
	}
	if !policyTargetsKind(policy, platformv1alpha1.PolicyTargetServiceInstance) {
		return allErrs
	}
	evalContext := buildPolicyEvaluationContext(instance)
	for _, rule := range policy.Spec.Rules {
		violated, err := evaluatePolicyRule(rule, evalContext)
		if err != nil {
			allErrs = append(allErrs, field.InternalError(field.NewPath("spec", "policyRefs"), fmt.Errorf("policy %q rule %q: %w", policy.Name, rule.Name, err)))
			continue
		}
		if !violated {
			continue
		}
		message := strings.TrimSpace(rule.Message)
		if message == "" {
			message = fmt.Sprintf("policy %s rule %s was violated", policy.Name, rule.Name)
		}
		path := policyRuleFieldPath(rule.Path)
		switch rule.Operator {
		case platformv1alpha1.PolicyOperatorExists, platformv1alpha1.PolicyOperatorNotExists, platformv1alpha1.PolicyOperatorEmpty, platformv1alpha1.PolicyOperatorNotEmpty:
			allErrs = append(allErrs, field.Forbidden(path, message))
		default:
			allErrs = append(allErrs, field.Invalid(path, rule.Value, message))
		}
	}
	return allErrs
}

func validatePolicySpec(policy *platformv1alpha1.Policy) error {
	if policy == nil {
		return nil
	}
	for _, rule := range policy.Spec.Rules {
		if strings.TrimSpace(rule.Name) == "" {
			return fmt.Errorf("policy rules must declare a name")
		}
		switch rule.Operator {
		case platformv1alpha1.PolicyOperatorExists, platformv1alpha1.PolicyOperatorNotExists, platformv1alpha1.PolicyOperatorEmpty, platformv1alpha1.PolicyOperatorNotEmpty:
			if strings.TrimSpace(rule.Path) == "" {
				return fmt.Errorf("rule %q must declare a path", rule.Name)
			}
		case platformv1alpha1.PolicyOperatorIn, platformv1alpha1.PolicyOperatorNotIn:
			if strings.TrimSpace(rule.Path) == "" || len(rule.Values) == 0 {
				return fmt.Errorf("rule %q must declare a path and non-empty values", rule.Name)
			}
		default:
			if strings.TrimSpace(rule.Path) == "" || strings.TrimSpace(rule.Value) == "" {
				return fmt.Errorf("rule %q must declare a path and value", rule.Name)
			}
		}
	}
	return nil
}

func mergedPolicyRefs(tenant *platformv1alpha1.Tenant, project *platformv1alpha1.Project, plan *platformv1alpha1.ServicePlan) []string {
	var refs []string
	if tenant != nil {
		for _, ref := range tenant.Spec.PolicyRefs {
			refs = append(refs, ref.Name)
		}
	}
	if project != nil {
		for _, ref := range project.Spec.PolicyRefs {
			refs = append(refs, ref.Name)
		}
	}
	if plan != nil {
		for _, ref := range plan.Spec.PolicyRefs {
			refs = append(refs, ref.Name)
		}
	}
	return refs
}

func decodePlanConstraints(plan *platformv1alpha1.ServicePlan) planConstraints {
	if plan == nil || plan.Spec.Constraints == nil || len(plan.Spec.Constraints.Raw) == 0 {
		return planConstraints{}
	}
	var constraints planConstraints
	_ = json.Unmarshal(plan.Spec.Constraints.Raw, &constraints)
	return constraints
}

func instanceServiceParameters(instance *platformv1alpha1.ServiceInstance) serviceInstanceParameters {
	if instance == nil || instance.Spec.Parameters == nil || len(instance.Spec.Parameters.Raw) == 0 {
		return serviceInstanceParameters{}
	}
	var params serviceInstanceParameters
	_ = json.Unmarshal(instance.Spec.Parameters.Raw, &params)
	return params
}

func firstNonNilInt32(values ...*int32) *int32 {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func policyTargetsKind(policy *platformv1alpha1.Policy, kind platformv1alpha1.PolicyTargetKind) bool {
	for _, targetKind := range policy.Spec.TargetKinds {
		if targetKind == kind {
			return true
		}
	}
	return false
}

func buildPolicyEvaluationContext(instance *platformv1alpha1.ServiceInstance) map[string]any {
	parameters := map[string]any{}
	if instance != nil && instance.Spec.Parameters != nil && len(instance.Spec.Parameters.Raw) > 0 {
		_ = json.Unmarshal(instance.Spec.Parameters.Raw, &parameters)
	}
	metadata := map[string]any{}
	spec := map[string]any{}
	if instance != nil {
		metadata["name"] = instance.Name
		metadata["labels"] = mapStringToAny(instance.Labels)
		spec["projectRef"] = map[string]any{"name": instance.Spec.ProjectRef.Name}
		spec["serviceClassRef"] = map[string]any{"name": instance.Spec.ServiceClassRef.Name}
		spec["servicePlanRef"] = map[string]any{"name": instance.Spec.ServicePlanRef.Name}
		spec["version"] = instance.Spec.Version
		spec["exposure"] = map[string]any{"mode": string(instance.Spec.Exposure.Mode)}
		spec["secretPolicy"] = map[string]any{
			"deliveryMode":           string(instance.Spec.SecretPolicy.DeliveryMode),
			"externalSecretProvider": string(instance.Spec.SecretPolicy.ExternalSecretProvider),
		}
		spec["deletionPolicy"] = string(instance.Spec.DeletionPolicy)
	}
	return map[string]any{
		"metadata":   metadata,
		"spec":       spec,
		"parameters": parameters,
	}
}

func evaluatePolicyRule(rule platformv1alpha1.PolicyRule, root map[string]any) (bool, error) {
	value, exists := resolvePolicyValue(root, rule.Path)
	switch rule.Operator {
	case platformv1alpha1.PolicyOperatorExists:
		return exists, nil
	case platformv1alpha1.PolicyOperatorNotExists:
		return !exists, nil
	case platformv1alpha1.PolicyOperatorEmpty:
		return !exists || isEmptyValue(value), nil
	case platformv1alpha1.PolicyOperatorNotEmpty:
		return exists && !isEmptyValue(value), nil
	}
	if !exists {
		return false, nil
	}
	switch rule.Operator {
	case platformv1alpha1.PolicyOperatorEquals:
		return compareString(value, rule.Value) == 0, nil
	case platformv1alpha1.PolicyOperatorNotEquals:
		return compareString(value, rule.Value) != 0, nil
	case platformv1alpha1.PolicyOperatorIn:
		for _, candidate := range rule.Values {
			if compareString(value, candidate) == 0 {
				return true, nil
			}
		}
		return false, nil
	case platformv1alpha1.PolicyOperatorNotIn:
		for _, candidate := range rule.Values {
			if compareString(value, candidate) == 0 {
				return false, nil
			}
		}
		return true, nil
	case platformv1alpha1.PolicyOperatorGT, platformv1alpha1.PolicyOperatorGTE, platformv1alpha1.PolicyOperatorLT, platformv1alpha1.PolicyOperatorLTE:
		left, err := asFloat(value)
		if err != nil {
			return false, err
		}
		right, err := strconv.ParseFloat(rule.Value, 64)
		if err != nil {
			return false, fmt.Errorf("parse numeric rule value %q: %w", rule.Value, err)
		}
		switch rule.Operator {
		case platformv1alpha1.PolicyOperatorGT:
			return left > right, nil
		case platformv1alpha1.PolicyOperatorGTE:
			return left >= right, nil
		case platformv1alpha1.PolicyOperatorLT:
			return left < right, nil
		case platformv1alpha1.PolicyOperatorLTE:
			return left <= right, nil
		}
	}
	return false, fmt.Errorf("unsupported operator %q", rule.Operator)
}

func resolvePolicyValue(root map[string]any, path string) (any, bool) {
	current := any(root)
	for _, segment := range strings.Split(strings.TrimSpace(path), ".") {
		if segment == "" {
			continue
		}
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := node[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func policyRuleFieldPath(path string) *field.Path {
	path = strings.TrimSpace(path)
	if path == "" {
		return field.NewPath("spec")
	}
	parts := strings.Split(path, ".")
	p := field.NewPath(parts[0])
	for _, part := range parts[1:] {
		p = p.Child(part)
	}
	return p
}

func compareString(value any, candidate string) int {
	return strings.Compare(strings.TrimSpace(fmt.Sprint(value)), strings.TrimSpace(candidate))
}

func asFloat(value any) (float64, error) {
	switch typed := value.(type) {
	case float64:
		return typed, nil
	case float32:
		return float64(typed), nil
	case int:
		return float64(typed), nil
	case int32:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case json.Number:
		return typed.Float64()
	default:
		return strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(value)), 64)
	}
}

func isEmptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	default:
		return strings.TrimSpace(fmt.Sprint(value)) == ""
	}
}

func mapStringToAny(values map[string]string) map[string]any {
	if len(values) == 0 {
		return map[string]any{}
	}
	converted := make(map[string]any, len(values))
	for key, value := range values {
		converted[key] = value
	}
	return converted
}
