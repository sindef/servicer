package bff

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	"github.com/sindef/servicer/internal/adapters"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var productRequestNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

func (s *Server) handleCreateProductRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	var request ProductRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	instance, err := s.productRequestToInstance(r, actor, request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := s.client.Create(r.Context(), instance); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: instance.Name, Message: "Product request submitted."})
}

func (s *Server) handleUpdateProductRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	var request ProductRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if request.Name == "" {
		request.Name = name
	}
	if request.Name != name {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request name must match path"})
		return
	}
	var existing platformv1alpha1.ServiceInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &existing); err != nil {
		writeError(w, err)
		return
	}
	if !s.authorizeInstance(r.Context(), actor, &existing) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "instance is outside your authorized tenancy"})
		return
	}
	updated, err := s.productRequestToInstance(r, actor, request)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	existing.Spec = updated.Spec
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	existing.Annotations["servicer.io/updated-by"] = actor.Name
	existing.Annotations["servicer.io/updated-at"] = time.Now().UTC().Format(time.RFC3339)
	if err := s.client.Update(r.Context(), &existing); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: existing.Name, Message: "Product request updated."})
}

func (s *Server) handleSubmitAction(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator, roleServiceConsumer)
	if !ok {
		return
	}
	instanceName := strings.TrimSpace(r.PathValue("name"))
	var request ActionSubmitRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	actionName := strings.TrimSpace(request.Action)
	if actionName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "action is required"})
		return
	}

	var instance platformv1alpha1.ServiceInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: instanceName}, &instance); err != nil {
		writeError(w, err)
		return
	}
	if !s.authorizeInstance(r.Context(), actor, &instance) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "instance is outside your authorized tenancy"})
		return
	}
	capability, ok := actionCapabilityForClass(instance.Spec.ServiceClassRef.Name, actionName)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("action %q is not supported for %q", actionName, instance.Spec.ServiceClassRef.Name)})
		return
	}
	if capability.RequiresApproval && !actor.hasAny(rolePlatformAdmin, roleTenantOperator) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "action requires an approver role"})
		return
	}
	action := &platformv1alpha1.ActionRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s-%d", instanceName, actionName, time.Now().Unix()),
			Annotations: map[string]string{
				"servicer.io/requested-by":   actor.Name,
				"servicer.io/request-reason": request.Reason,
			},
		},
		Spec: platformv1alpha1.ActionRequestSpec{
			TargetRef: platformv1alpha1.TypedObjectReference{
				APIVersion: platformv1alpha1.GroupVersion.String(),
				Kind:       "ServiceInstance",
				Name:       instanceName,
			},
			Action:         actionName,
			IdempotencyKey: fmt.Sprintf("%s/%s/%d", instanceName, actionName, time.Now().UnixNano()),
			Parameters:     rawJSONFromMap(request.Parameters),
			Approval: platformv1alpha1.ApprovalSpec{
				Mode: approvalModeFor(actor, capability),
			},
			RequestedBy: platformv1alpha1.RequestedBySpec{
				Subject: actor.Name,
				Source:  platformv1alpha1.RequestSourceUI,
			},
		},
	}
	if err := s.client.Create(r.Context(), action); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, WriteResponse{Name: action.Name, Message: "Action request submitted."})
}

func (s *Server) handleDeleteProductRequest(w http.ResponseWriter, r *http.Request) {
	actor, ok := requireRole(w, r, rolePlatformAdmin, roleTenantOperator)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PathValue("name"))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "instance name is required"})
		return
	}
	var instance platformv1alpha1.ServiceInstance
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: name}, &instance); err != nil {
		writeError(w, err)
		return
	}
	if !s.authorizeInstance(r.Context(), actor, &instance) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "instance is outside your authorized tenancy"})
		return
	}
	if instance.Annotations == nil {
		instance.Annotations = map[string]string{}
	}
	instance.Annotations["servicer.io/deleted-by"] = actor.Name
	instance.Annotations["servicer.io/deleted-at"] = time.Now().UTC().Format(time.RFC3339)
	_ = s.client.Update(r.Context(), &instance)
	if err := s.client.Delete(r.Context(), &instance); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, WriteResponse{Name: name, Message: "Product deletion requested."})
}

func (s *Server) productRequestToInstance(r *http.Request, actor actor, request ProductRequest) (*platformv1alpha1.ServiceInstance, error) {
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" || request.ProjectName == "" || request.ServiceClass == "" || request.ServicePlan == "" {
		return nil, fmt.Errorf("name, projectName, serviceClass, and servicePlan are required")
	}
	if !productRequestNamePattern.MatchString(request.Name) {
		return nil, fmt.Errorf("name must start with a lowercase letter, end with a lowercase letter or number, and contain only lowercase letters, numbers, and hyphens")
	}
	if _, ok := implementedProducts[request.ServiceClass]; !ok {
		return nil, fmt.Errorf("service class %q is not requestable", request.ServiceClass)
	}
	var project platformv1alpha1.Project
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: request.ProjectName}, &project); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("project %q does not exist", request.ProjectName)
		}
		return nil, err
	}
	if !s.authorizeProject(r.Context(), actor, &project) {
		return nil, fmt.Errorf("project %q is outside your authorized tenancy", request.ProjectName)
	}
	var tenant platformv1alpha1.Tenant
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: project.Spec.TenantRef.Name}, &tenant); err != nil {
		return nil, err
	}
	var class platformv1alpha1.ServiceClass
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: request.ServiceClass}, &class); err != nil {
		return nil, err
	}
	var plan platformv1alpha1.ServicePlan
	if err := s.client.Get(r.Context(), types.NamespacedName{Name: request.ServicePlan}, &plan); err != nil {
		return nil, err
	}
	if plan.Spec.ServiceClassRef.Name != class.Name {
		return nil, fmt.Errorf("plan %q does not belong to class %q", plan.Name, class.Name)
	}
	if !class.Spec.Published && !class.Status.Published {
		return nil, fmt.Errorf("service class %q is not published", class.Name)
	}
	if !actor.isPlatformAdmin() && !stringInSlice(class.Name, tenant.Spec.AllowedServiceClasses) {
		return nil, fmt.Errorf("service class %q is not allowed for tenant %q", class.Name, tenant.Name)
	}
	contract, _ := adapters.KnownContract(adapters.ServiceClass(request.ServiceClass))
	if plan.Spec.Topology == "multi-region" && contract.SupportsMultiCluster {
		var hasStandbyClusters bool
		if items, ok := request.Parameters["standbyClusters"].([]interface{}); ok && len(items) > 0 {
			hasStandbyClusters = true
		}
		if !hasStandbyClusters {
			return nil, fmt.Errorf("plan %q uses multi-region topology — at least one standby cluster must be specified in parameters.standbyClusters", plan.Name)
		}
	}
	return &platformv1alpha1.ServiceInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name: request.Name,
			Annotations: map[string]string{
				"servicer.io/requested-by": actor.Name,
				"servicer.io/requested-at": time.Now().UTC().Format(time.RFC3339),
			},
		},
		Spec: platformv1alpha1.ServiceInstanceSpec{
			ProjectRef:      platformv1alpha1.LocalObjectReference{Name: request.ProjectName},
			ServiceClassRef: platformv1alpha1.LocalObjectReference{Name: request.ServiceClass},
			ServicePlanRef:  platformv1alpha1.LocalObjectReference{Name: request.ServicePlan},
			Version:         request.Version,
			Parameters:      rawJSONFromMap(request.Parameters),
			Exposure:        platformv1alpha1.ExposureSpec{Mode: platformv1alpha1.ExposureModeClusterInternal},
			SecretPolicy:    platformv1alpha1.SecretPolicySpec{DeliveryMode: platformv1alpha1.SecretDeliveryModeExternalSecret},
			DeletionPolicy:  deletionPolicyForClass(request.ServiceClass),
		},
	}, nil
}

func stringInSlice(value string, items []string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func actionCapabilityForClass(serviceClass, action string) (adapters.ActionCapability, bool) {
	contract, ok := adapters.KnownContract(adapters.ServiceClass(serviceClass))
	if !ok {
		return adapters.ActionCapability{}, false
	}
	for _, capability := range contract.Actions {
		if string(capability.Name) == action {
			return capability, true
		}
	}
	return adapters.ActionCapability{}, false
}

func approvalModeFor(actor actor, capability adapters.ActionCapability) platformv1alpha1.ApprovalMode {
	if capability.RequiresApproval {
		if actor.hasAny(rolePlatformAdmin) {
			return platformv1alpha1.ApprovalModeApproved
		}
		return platformv1alpha1.ApprovalModeRequired
	}
	return platformv1alpha1.ApprovalModeAuto
}

func deletionPolicyForClass(serviceClass string) platformv1alpha1.DeletionPolicy {
	switch adapters.ServiceClass(serviceClass) {
	case adapters.ServiceClassPostgreSQL:
		return adapters.DefaultPostgreSQLDeletionPolicy
	case adapters.ServiceClassMySQL:
		return adapters.DefaultMySQLDeletionPolicy
	case adapters.ServiceClassNATS:
		return adapters.DefaultNATSDeletionPolicy
	case adapters.ServiceClassValkey:
		return adapters.DefaultValkeyDeletionPolicy
	case adapters.ServiceClassYugabyte:
		return adapters.DefaultYugabyteDeletionPolicy
	default:
		return platformv1alpha1.DeletionPolicyDelete
	}
}

func rawJSONFromMap(values map[string]any) *apiextensionsv1.JSON {
	if len(values) == 0 {
		return nil
	}
	raw, _ := json.Marshal(values)
	return &apiextensionsv1.JSON{Raw: raw}
}
