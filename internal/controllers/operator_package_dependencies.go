package controllers

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const externalSecretsOperatorPackageName = "external-secrets"

func requiresExternalSecretsOperator(policy platformv1alpha1.SecretPolicySpec) bool {
	return policy.DeliveryMode == platformv1alpha1.SecretDeliveryModeExternalSecret
}

func resolveClusterTargetForProject(ctx context.Context, c client.Client, project *platformv1alpha1.Project, fallbackClusterName string) (*platformv1alpha1.ClusterTarget, error) {
	clusterName := firstNonEmptyTrimmed(
		project.Status.Placement.ClusterName,
		fallbackClusterName,
	)
	if clusterName == "" && project != nil && project.Spec.TargetSelector.ClusterRef != nil {
		clusterName = firstNonEmptyTrimmed(project.Spec.TargetSelector.ClusterRef.Name)
	}
	if clusterName == "" {
		return nil, nil
	}

	var target platformv1alpha1.ClusterTarget
	if err := c.Get(ctx, client.ObjectKey{Name: clusterName}, &target); err != nil {
		return nil, err
	}
	return &target, nil
}

func externalSecretsPackageReady(target *platformv1alpha1.ClusterTarget) (bool, string) {
	if target == nil {
		return true, ""
	}
	status := packageStatus(target, externalSecretsOperatorPackageName)
	if !packageIsRequired(target, externalSecretsOperatorPackageName) && status == nil {
		return true, ""
	}
	if !packageIsRequired(target, externalSecretsOperatorPackageName) && status != nil && status.Phase == platformv1alpha1.PackagePhaseInstalled {
		return true, ""
	}
	if packageIsRequired(target, externalSecretsOperatorPackageName) && status == nil {
		return false, fmt.Sprintf("ClusterTarget %q is still reconciling OperatorPackage %q.", target.Name, externalSecretsOperatorPackageName)
	}
	if status == nil {
		return true, ""
	}
	if status.Phase == platformv1alpha1.PackagePhaseInstalled {
		return true, ""
	}
	if status.Message != "" {
		return false, fmt.Sprintf("OperatorPackage %q on ClusterTarget %q is not ready yet: %s", externalSecretsOperatorPackageName, target.Name, status.Message)
	}
	return false, fmt.Sprintf("OperatorPackage %q on ClusterTarget %q is not ready yet (phase %s).", externalSecretsOperatorPackageName, target.Name, status.Phase)
}

func packageIsRequired(target *platformv1alpha1.ClusterTarget, packageName string) bool {
	if target == nil {
		return false
	}
	for _, requiredPackage := range target.Spec.RequiredPackages {
		if requiredPackage == packageName {
			return true
		}
	}
	return false
}

func packageStatus(target *platformv1alpha1.ClusterTarget, packageName string) *platformv1alpha1.PackageStatus {
	if target == nil {
		return nil
	}
	for i := range target.Status.Packages {
		if target.Status.Packages[i].Name == packageName {
			return &target.Status.Packages[i]
		}
	}
	return nil
}
