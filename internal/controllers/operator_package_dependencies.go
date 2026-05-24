package controllers

import (
	"context"
	"fmt"
	"sort"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const externalSecretsOperatorPackageName = "external-secrets"

func requiresExternalSecretsOperator(policy platformv1alpha1.SecretPolicySpec) bool {
	return policy.DeliveryMode == platformv1alpha1.SecretDeliveryModeExternalSecret
}

func requiredPackagesForServiceInstance(class *platformv1alpha1.ServiceClass, instance *platformv1alpha1.ServiceInstance) []string {
	packages := make([]string, 0)
	if class != nil {
		packages = append(packages, class.Spec.RequiredPackages...)
	}
	if instance != nil && requiresExternalSecretsOperator(instance.Spec.SecretPolicy) {
		packages = append(packages, externalSecretsOperatorPackageName)
	}
	return uniquePackageNames(packages)
}

func effectiveRequiredPackagesForClusterTarget(ctx context.Context, c client.Client, target *platformv1alpha1.ClusterTarget) ([]string, error) {
	packages := append([]string(nil), target.Spec.RequiredPackages...)

	var classes platformv1alpha1.ServiceClassList
	if err := c.List(ctx, &classes); err != nil {
		return nil, err
	}
	for i := range classes.Items {
		class := &classes.Items[i]
		if !class.Spec.Published {
			continue
		}
		packages = append(packages, class.Spec.RequiredPackages...)
	}

	var instances platformv1alpha1.ServiceInstanceList
	if err := c.List(ctx, &instances); err != nil {
		return nil, err
	}
	for i := range instances.Items {
		instance := &instances.Items[i]
		if instance.Status.Placement.ClusterName != target.Name {
			continue
		}
		if requiresExternalSecretsOperator(instance.Spec.SecretPolicy) {
			packages = append(packages, externalSecretsOperatorPackageName)
		}
	}

	return uniquePackageNames(packages), nil
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

func packagesReady(target *platformv1alpha1.ClusterTarget, packageNames []string) (bool, string) {
	if target == nil {
		return false, "Target cluster is not resolved yet."
	}
	for _, packageName := range uniquePackageNames(packageNames) {
		status := packageStatus(target, packageName)
		if status == nil {
			if !packageIsRequired(target, packageName) {
				continue
			}
			return false, fmt.Sprintf("OperatorPackage %q on ClusterTarget %q is still reconciling.", packageName, target.Name)
		}
		if status.Phase != platformv1alpha1.PackagePhaseInstalled {
			if status.Message != "" {
				return false, fmt.Sprintf("OperatorPackage %q on ClusterTarget %q is not ready yet: %s", packageName, target.Name, status.Message)
			}
			return false, fmt.Sprintf("OperatorPackage %q on ClusterTarget %q is not ready yet (phase %s).", packageName, target.Name, status.Phase)
		}
	}
	return true, ""
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

func uniquePackageNames(names []string) []string {
	seen := map[string]struct{}{}
	unique := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		unique = append(unique, name)
	}
	sort.Strings(unique)
	return unique
}
