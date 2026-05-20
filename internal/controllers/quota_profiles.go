package controllers

import platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"

func effectiveProjectQuota(project *platformv1alpha1.Project, tenant *platformv1alpha1.Tenant) platformv1alpha1.ProjectQuotasSpec {
	if project == nil {
		return platformv1alpha1.ProjectQuotasSpec{}
	}
	quota := platformv1alpha1.ProjectQuotasSpec{}
	if tenant != nil {
		quota = quotaProfile(tenant.Spec.QuotaProfileRef.Name)
	}
	if project.Spec.Quotas.MaxServices != nil {
		quota.MaxServices = project.Spec.Quotas.MaxServices
	}
	if project.Spec.Quotas.MaxNamespaces != nil {
		quota.MaxNamespaces = project.Spec.Quotas.MaxNamespaces
	}
	return quota
}

func quotaProfile(name string) platformv1alpha1.ProjectQuotasSpec {
	switch name {
	case "tiny":
		return projectQuota(1, 1)
	case "sandbox":
		return projectQuota(3, 2)
	case "development":
		return projectQuota(10, 5)
	case "standard", "standard-tenant":
		return projectQuota(20, 10)
	default:
		return platformv1alpha1.ProjectQuotasSpec{}
	}
}

func projectQuota(maxServices, maxNamespaces int32) platformv1alpha1.ProjectQuotasSpec {
	return platformv1alpha1.ProjectQuotasSpec{
		MaxServices:   int32Ptr(maxServices),
		MaxNamespaces: int32Ptr(maxNamespaces),
	}
}
