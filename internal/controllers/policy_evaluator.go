package controllers

import (
	"encoding/json"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"k8s.io/apimachinery/pkg/util/validation/field"
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

func evaluateInstancePolicies(instance *platformv1alpha1.ServiceInstance, tenant *platformv1alpha1.Tenant, project *platformv1alpha1.Project, plan *platformv1alpha1.ServicePlan) field.ErrorList {
	allErrs := field.ErrorList{}
	if instance == nil {
		return allErrs
	}

	policyRefs := mergedPolicyRefs(tenant, project, plan)
	for _, policy := range policyRefs {
		switch policy {
		case "deny-public-ingress":
			if instance.Spec.Exposure.Mode == platformv1alpha1.ExposureModePublicIngress {
				allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "exposure", "mode"), "policy deny-public-ingress forbids public ingress"))
			}
		case "require-external-secrets":
			if instance.Spec.SecretPolicy.DeliveryMode != platformv1alpha1.SecretDeliveryModeExternalSecret {
				allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "secretPolicy", "deliveryMode"), "policy require-external-secrets requires external-secret delivery"))
			}
		case "require-backups":
			params := instanceServiceParameters(instance)
			if params.BackupProfile == "" {
				allErrs = append(allErrs, field.Required(field.NewPath("spec", "parameters", "backupProfile"), "policy require-backups requires backupProfile"))
			}
		case "protect-delete":
			if instance.Spec.DeletionPolicy == platformv1alpha1.DeletionPolicyDelete {
				allErrs = append(allErrs, field.Forbidden(field.NewPath("spec", "deletionPolicy"), "policy protect-delete forbids direct delete policy"))
			}
		}
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
