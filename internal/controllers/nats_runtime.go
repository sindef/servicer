package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	platformv1alpha1 "github.com/sindef/servicer/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type natsCredentialMaterialization struct {
	AdminSecretName      string
	AuthConfigSecretName string
	AppCredentials       []natsAppCredentialMaterialization
}

type natsAppCredentialMaterialization struct {
	Name         string
	SecretName   string
	Username     string
	Description  string
	Publish      []string
	Subscribe    []string
	AllowReplies bool
}

func loadNATSCredentialMaterialization(instance *platformv1alpha1.ServiceInstance) (natsCredentialMaterialization, error) {
	if instance == nil {
		return natsCredentialMaterialization{}, nil
	}
	spec := natsCredentialMaterialization{
		AdminSecretName:      fmt.Sprintf("%s-auth", instance.Name),
		AuthConfigSecretName: fmt.Sprintf("%s-auth-config", instance.Name),
	}
	var raw struct {
		AppCredentials []struct {
			Name        string `json:"name,omitempty"`
			Username    string `json:"username,omitempty"`
			Description string `json:"description,omitempty"`
			Permissions struct {
				Publish        []string `json:"publish,omitempty"`
				Subscribe      []string `json:"subscribe,omitempty"`
				AllowResponses bool     `json:"allowResponses,omitempty"`
			} `json:"permissions,omitempty"`
		} `json:"appCredentials,omitempty"`
	}
	if instance.Spec.Parameters != nil && len(instance.Spec.Parameters.Raw) > 0 {
		if err := json.Unmarshal(instance.Spec.Parameters.Raw, &raw); err != nil {
			return spec, err
		}
	}
	for _, credential := range raw.AppCredentials {
		name := strings.TrimSpace(credential.Name)
		if name == "" {
			continue
		}
		username := strings.TrimSpace(credential.Username)
		if username == "" {
			username = name
		}
		spec.AppCredentials = append(spec.AppCredentials, natsAppCredentialMaterialization{
			Name:         name,
			SecretName:   fmt.Sprintf("%s-%s-auth", instance.Name, name),
			Username:     username,
			Description:  firstNonEmpty(strings.TrimSpace(credential.Description), fmt.Sprintf("Servicer managed credential %s", name)),
			Publish:      append([]string(nil), credential.Permissions.Publish...),
			Subscribe:    append([]string(nil), credential.Permissions.Subscribe...),
			AllowReplies: credential.Permissions.AllowResponses,
		})
	}
	sort.Slice(spec.AppCredentials, func(i, j int) bool { return spec.AppCredentials[i].Name < spec.AppCredentials[j].Name })
	return spec, nil
}

func (r *ServiceInstanceReconciler) ensureNATSCredentialSecrets(ctx context.Context, instance *platformv1alpha1.ServiceInstance, namespace string) error {
	spec, err := loadNATSCredentialMaterialization(instance)
	if err != nil {
		return fmt.Errorf("decode nats app credentials: %w", err)
	}
	desired := map[string]struct{}{
		spec.AdminSecretName:      {},
		spec.AuthConfigSecretName: {},
	}

	adminSecret, err := r.ensureOpaqueSecret(ctx, namespace, spec.AdminSecretName, instance.Name, map[string]string{
		"username": "servicer",
		"url":      fmt.Sprintf("nats://%s.%s.svc.cluster.local:4222", instance.Name, namespace),
	}, "credential")
	if err != nil {
		return err
	}

	passwords := map[string]string{
		"servicer": string(adminSecret.Data["password"]),
	}
	for _, credential := range spec.AppCredentials {
		desired[credential.SecretName] = struct{}{}
		secret, err := r.ensureOpaqueSecret(ctx, namespace, credential.SecretName, instance.Name, map[string]string{
			"username":    credential.Username,
			"description": credential.Description,
			"url":         fmt.Sprintf("nats://%s.%s.svc.cluster.local:4222", instance.Name, namespace),
			"permissions": natsPermissionsJSON(credential),
		}, "credential")
		if err != nil {
			return err
		}
		passwords[credential.Username] = string(secret.Data["password"])
	}

	authSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.AuthConfigSecretName,
			Namespace: namespace,
			Labels: map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/service-instance": instance.Name,
				"servicer.io/nats-secret-role": "auth-config",
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"users.conf": []byte(renderNATSUsersConfig(spec, passwords)),
		},
	}
	var existing corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Name: spec.AuthConfigSecretName, Namespace: namespace}, &existing); err == nil {
		existing.Data = authSecret.Data
		if existing.Labels == nil {
			existing.Labels = authSecret.Labels
		} else {
			for key, value := range authSecret.Labels {
				existing.Labels[key] = value
			}
		}
		if updateErr := r.Update(ctx, &existing); updateErr != nil {
			return updateErr
		}
	} else if apierrors.IsNotFound(err) {
		if err := r.Create(ctx, &authSecret); err != nil {
			return err
		}
	} else {
		return err
	}

	var secrets corev1.SecretList
	if err := r.List(ctx, &secrets, client.InNamespace(namespace)); err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		if secret.Labels["servicer.io/managed-by"] != "servicer" || secret.Labels["servicer.io/service-instance"] != instance.Name {
			continue
		}
		if secret.Labels["servicer.io/nats-secret-role"] == "" {
			continue
		}
		if _, keep := desired[secret.Name]; keep {
			continue
		}
		if err := r.Delete(ctx, &secret); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *ServiceInstanceReconciler) ensureOpaqueSecret(ctx context.Context, namespace, secretName, instanceName string, staticData map[string]string, role string) (*corev1.Secret, error) {
	var secret corev1.Secret
	err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &secret)
	if err == nil {
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		if len(secret.Data["password"]) == 0 {
			password, genErr := randomPassword()
			if genErr != nil {
				return nil, genErr
			}
			secret.Data["password"] = []byte(password)
		}
		for key, value := range staticData {
			secret.Data[key] = []byte(value)
		}
		if secret.Labels == nil {
			secret.Labels = map[string]string{}
		}
		secret.Labels["servicer.io/managed-by"] = "servicer"
		secret.Labels["servicer.io/service-instance"] = instanceName
		secret.Labels["servicer.io/nats-secret-role"] = role
		if updateErr := r.Update(ctx, &secret); updateErr != nil {
			return nil, updateErr
		}
		return &secret, nil
	}
	if !apierrors.IsNotFound(err) {
		return nil, err
	}
	password, genErr := randomPassword()
	if genErr != nil {
		return nil, genErr
	}
	data := map[string][]byte{"password": []byte(password)}
	for key, value := range staticData {
		data[key] = []byte(value)
	}
	secret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"servicer.io/managed-by":       "servicer",
				"servicer.io/service-instance": instanceName,
				"servicer.io/nats-secret-role": role,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: data,
	}
	if err := r.Create(ctx, &secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

func renderNATSUsersConfig(spec natsCredentialMaterialization, passwords map[string]string) string {
	lines := []string{
		"authorization {",
		"  users: [",
		fmt.Sprintf("    { user: %q, password: %q, permissions: { publish: \">\", subscribe: \">\" } }", "servicer", passwords["servicer"]),
	}
	for _, credential := range spec.AppCredentials {
		entry := fmt.Sprintf("    { user: %q, password: %q", credential.Username, passwords[credential.Username])
		perms := []string{}
		if len(credential.Publish) > 0 {
			perms = append(perms, "publish: "+natsStringArray(credential.Publish))
		}
		if len(credential.Subscribe) > 0 {
			perms = append(perms, "subscribe: "+natsStringArray(credential.Subscribe))
		}
		if credential.AllowReplies {
			perms = append(perms, "allow_responses: true")
		}
		if len(perms) > 0 {
			entry += ", permissions: { " + strings.Join(perms, ", ") + " }"
		}
		entry += " }"
		lines = append(lines, entry)
	}
	lines = append(lines, "  ]", "}")
	return strings.Join(lines, "\n") + "\n"
}

func natsStringArray(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, fmt.Sprintf("%q", value))
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func natsPermissionsJSON(credential natsAppCredentialMaterialization) string {
	payload := map[string]any{
		"publish":        credential.Publish,
		"subscribe":      credential.Subscribe,
		"allowResponses": credential.AllowReplies,
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
