package controllers

import (
	"context"
	"fmt"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceBindingReconciler reconciles ServiceBinding resources.
type ServiceBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ServiceBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var binding platformv1alpha1.ServiceBinding
	if err := r.Get(ctx, req.NamespacedName, &binding); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	originalStatus := binding.Status
	binding.Status.ObservedGeneration = binding.Generation

	if binding.Spec.SourceRef.Kind != "ServiceInstance" || binding.Spec.SourceRef.Name == "" {
		binding.Status.Phase = "Failed"
		setStatusCondition(&binding.Status.Conditions, binding.Generation, "Accepted", metav1.ConditionFalse, "UnsupportedSource", "Service bindings currently support ServiceInstance sources only.")
		setStatusCondition(&binding.Status.Conditions, binding.Generation, "Failed", metav1.ConditionTrue, "UnsupportedSource", "Binding source is unsupported.")
		return r.updateStatusIfChanged(ctx, &binding, originalStatus)
	}

	var source platformv1alpha1.ServiceInstance
	if err := r.Get(ctx, types.NamespacedName{Name: binding.Spec.SourceRef.Name}, &source); err != nil {
		if apierrors.IsNotFound(err) {
			binding.Status.Phase = "PendingSource"
			setStatusCondition(&binding.Status.Conditions, binding.Generation, "Ready", metav1.ConditionFalse, "SourcePending", "Binding source service instance does not exist yet.")
			return r.updateStatusIfChanged(ctx, &binding, originalStatus)
		}
		return ctrl.Result{}, err
	}
	if len(source.Status.CredentialRefs) == 0 {
		binding.Status.Phase = "PendingSource"
		setStatusCondition(&binding.Status.Conditions, binding.Generation, "Ready", metav1.ConditionFalse, "CredentialRefsPending", "Binding source has not published credentials yet.")
		return r.updateStatusIfChanged(ctx, &binding, originalStatus)
	}

	sourceRef := source.Status.CredentialRefs[0]
	var sourceSecret corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: sourceRef.Name, Namespace: sourceRef.Namespace}, &sourceSecret); err != nil {
		if apierrors.IsNotFound(err) {
			binding.Status.Phase = "PendingSource"
			setStatusCondition(&binding.Status.Conditions, binding.Generation, "Ready", metav1.ConditionFalse, "SourceSecretPending", "Binding source Secret is not available yet.")
			return r.updateStatusIfChanged(ctx, &binding, originalStatus)
		}
		return ctrl.Result{}, err
	}

	targetNamespace := binding.Spec.TargetRef.Namespace
	if targetNamespace == "" {
		targetNamespace = source.Status.Placement.Namespace
	}
	if targetNamespace == "" {
		binding.Status.Phase = "Failed"
		setStatusCondition(&binding.Status.Conditions, binding.Generation, "Failed", metav1.ConditionTrue, "TargetNamespaceMissing", "Binding target namespace could not be resolved.")
		return r.updateStatusIfChanged(ctx, &binding, originalStatus)
	}

	projectName := binding.Spec.ProjectRef.Name
	if projectName == "" {
		projectName = source.Spec.ProjectRef.Name
	}
	project, err := r.projectForBinding(ctx, projectName)
	if err != nil {
		return ctrl.Result{}, err
	}
	targetSecretName := fmt.Sprintf("%s-binding", binding.Name)

	switch binding.Spec.SecretPolicy.DeliveryMode {
	case platformv1alpha1.SecretDeliveryModeExternalSecret:
		if err := r.ensureExternalSecretBindingProjection(ctx, &binding, sourceRef, targetSecretName, targetNamespace); err != nil {
			return ctrl.Result{}, err
		}
		binding.Status.Health = platformv1alpha1.HealthStatus{Summary: fmt.Sprintf("Credentials projected via External Secrets into namespace %s for project %s.", targetNamespace, project.Name)}
	default:
		targetSecret := &corev1.Secret{}
		err = r.Get(ctx, types.NamespacedName{Name: targetSecretName, Namespace: targetNamespace}, targetSecret)
		if apierrors.IsNotFound(err) {
			targetSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      targetSecretName,
					Namespace: targetNamespace,
					Labels: map[string]string{
						"servicer.io/managed-by":      "servicebinding-controller",
						"servicer.io/service-binding": binding.Name,
					},
				},
				Type: corev1.SecretTypeOpaque,
				Data: cloneSecretData(sourceSecret.Data),
			}
			if createErr := r.Create(ctx, targetSecret); createErr != nil {
				return ctrl.Result{}, createErr
			}
		} else if err != nil {
			return ctrl.Result{}, err
		} else {
			targetSecret.Data = cloneSecretData(sourceSecret.Data)
			if targetSecret.Labels == nil {
				targetSecret.Labels = map[string]string{}
			}
			targetSecret.Labels["servicer.io/managed-by"] = "servicebinding-controller"
			targetSecret.Labels["servicer.io/service-binding"] = binding.Name
			if updateErr := r.Update(ctx, targetSecret); updateErr != nil {
				return ctrl.Result{}, updateErr
			}
		}
		binding.Status.Health = platformv1alpha1.HealthStatus{Summary: fmt.Sprintf("Credentials projected into namespace %s for project %s.", targetNamespace, project.Name)}
	}

	binding.Status.Phase = "Ready"
	binding.Status.CredentialRefs = []platformv1alpha1.NamespacedObjectReference{{
		Name:      targetSecretName,
		Namespace: targetNamespace,
	}}
	setStatusCondition(&binding.Status.Conditions, binding.Generation, "Accepted", metav1.ConditionTrue, "SourceAccepted", "Binding source and target are accepted.")
	setStatusCondition(&binding.Status.Conditions, binding.Generation, "Ready", metav1.ConditionTrue, "CredentialsProjected", "Binding credentials have been projected.")
	setStatusCondition(&binding.Status.Conditions, binding.Generation, "Failed", metav1.ConditionFalse, "CredentialsProjected", "Binding has not failed.")

	return r.updateStatusIfChanged(ctx, &binding, originalStatus)
}

func (r *ServiceBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.ServiceBinding{}).
		Complete(r)
}

func (r *ServiceBindingReconciler) updateStatusIfChanged(ctx context.Context, binding *platformv1alpha1.ServiceBinding, original platformv1alpha1.ServiceBindingStatus) (ctrl.Result, error) {
	if equality.Semantic.DeepEqual(original, binding.Status) {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, r.Status().Update(ctx, binding)
}

func (r *ServiceBindingReconciler) projectForBinding(ctx context.Context, name string) (*platformv1alpha1.Project, error) {
	var project platformv1alpha1.Project
	if err := r.Get(ctx, types.NamespacedName{Name: name}, &project); err != nil {
		return nil, err
	}
	return &project, nil
}

func cloneSecretData(source map[string][]byte) map[string][]byte {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string][]byte, len(source))
	for key, value := range source {
		cloned[key] = append([]byte(nil), value...)
	}
	return cloned
}

func (r *ServiceBindingReconciler) ensureExternalSecretBindingProjection(ctx context.Context, binding *platformv1alpha1.ServiceBinding, sourceRef platformv1alpha1.NamespacedObjectReference, targetSecretName, targetNamespace string) error {
	serviceAccountName := fmt.Sprintf("%s-eso-reader", binding.Name)
	storeName := secretStoreName(binding.Name, sourceRef.Namespace)

	serviceAccount := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceAccountName,
			Namespace: targetNamespace,
			Labels: map[string]string{
				"servicer.io/managed-by":      "servicebinding-controller",
				"servicer.io/service-binding": binding.Name,
				"servicer.io/secret-delivery": "external-secret",
			},
		},
	}
	if err := createOrUpdateObject(ctx, r.Client, serviceAccount); err != nil {
		return err
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storeName,
			Namespace: sourceRef.Namespace,
			Labels: map[string]string{
				"servicer.io/managed-by":      "servicebinding-controller",
				"servicer.io/service-binding": binding.Name,
				"servicer.io/secret-delivery": "external-secret",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{"authorization.k8s.io"}, Resources: []string{"selfsubjectrulesreviews"}, Verbs: []string{"create"}},
		},
	}
	if err := createOrUpdateObject(ctx, r.Client, role); err != nil {
		return err
	}

	roleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      storeName,
			Namespace: sourceRef.Namespace,
			Labels: map[string]string{
				"servicer.io/managed-by":      "servicebinding-controller",
				"servicer.io/service-binding": binding.Name,
				"servicer.io/secret-delivery": "external-secret",
			},
		},
		Subjects: []rbacv1.Subject{{
			Kind:      "ServiceAccount",
			Name:      serviceAccountName,
			Namespace: targetNamespace,
		}},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     storeName,
		},
	}
	if err := createOrUpdateObject(ctx, r.Client, roleBinding); err != nil {
		return err
	}

	secretStore := &unstructured.Unstructured{}
	secretStore.SetGroupVersionKind(schema.GroupVersionKind{Group: "external-secrets.io", Version: "v1", Kind: "SecretStore"})
	secretStore.SetName(storeName)
	secretStore.SetNamespace(targetNamespace)
	secretStore.SetLabels(map[string]string{
		"servicer.io/managed-by":      "servicebinding-controller",
		"servicer.io/service-binding": binding.Name,
		"servicer.io/secret-delivery": "external-secret",
	})
	secretStore.Object["spec"] = map[string]any{
		"provider": map[string]any{
			"kubernetes": map[string]any{
				"remoteNamespace": sourceRef.Namespace,
				"server": map[string]any{
					"caProvider": map[string]any{
						"type": "ConfigMap",
						"name": "kube-root-ca.crt",
						"key":  "ca.crt",
					},
				},
				"auth": map[string]any{
					"serviceAccount": map[string]any{
						"name": serviceAccountName,
					},
				},
			},
		},
	}
	if err := createOrUpdateUnstructured(ctx, r.Client, secretStore); err != nil {
		return err
	}

	externalSecret := &unstructured.Unstructured{}
	externalSecret.SetGroupVersionKind(schema.GroupVersionKind{Group: "external-secrets.io", Version: "v1", Kind: "ExternalSecret"})
	externalSecret.SetName(targetSecretName)
	externalSecret.SetNamespace(targetNamespace)
	externalSecret.SetLabels(map[string]string{
		"servicer.io/managed-by":      "servicebinding-controller",
		"servicer.io/service-binding": binding.Name,
		"servicer.io/secret-delivery": "external-secret",
	})
	externalSecret.Object["spec"] = map[string]any{
		"refreshInterval": "1h",
		"secretStoreRef": map[string]any{
			"kind": "SecretStore",
			"name": storeName,
		},
		"target": map[string]any{
			"name":           targetSecretName,
			"creationPolicy": "Owner",
			"deletionPolicy": "Delete",
		},
		"dataFrom": []any{
			map[string]any{
				"extract": map[string]any{
					"key": sourceRef.Name,
				},
			},
		},
	}
	return createOrUpdateUnstructured(ctx, r.Client, externalSecret)
}

func createOrUpdateObject(ctx context.Context, c client.Client, desired client.Object) error {
	current := desired.DeepCopyObject().(client.Object)
	key := types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	if err := c.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, desired)
		}
		return err
	}
	desired.SetResourceVersion(current.GetResourceVersion())
	return c.Update(ctx, desired)
}

func createOrUpdateUnstructured(ctx context.Context, c client.Client, desired *unstructured.Unstructured) error {
	current := &unstructured.Unstructured{}
	current.SetGroupVersionKind(desired.GroupVersionKind())
	key := types.NamespacedName{Name: desired.GetName(), Namespace: desired.GetNamespace()}
	if err := c.Get(ctx, key, current); err != nil {
		if apierrors.IsNotFound(err) {
			return c.Create(ctx, desired)
		}
		return err
	}
	desired.SetResourceVersion(current.GetResourceVersion())
	return c.Update(ctx, desired)
}
