package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	webhookMutatingConfigName   = "servicer-platform-mutating-webhooks"
	webhookValidatingConfigName = "servicer-platform-validating-webhooks"
	webhookTLSCertKey           = "tls.crt"
	webhookTLSPrivateKeyKey     = "tls.key"
	webhookCACertKey            = "ca.crt"
)

func bootstrapWebhookPKI(ctx context.Context, kubeClient client.Client, namespace, serviceName, secretName string) error {
	if kubeClient == nil {
		return fmt.Errorf("kubernetes client is required")
	}
	caCertPEM, serverCertPEM, serverKeyPEM, err := generateWebhookCertBundle(namespace, serviceName)
	if err != nil {
		return err
	}
	secret := &corev1.Secret{}
	key := client.ObjectKey{Name: secretName, Namespace: namespace}
	err = kubeClient.Get(ctx, key, secret)
	switch {
	case apierrors.IsNotFound(err):
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Type:       corev1.SecretTypeTLS,
			Data: map[string][]byte{
				webhookTLSCertKey:       serverCertPEM,
				webhookTLSPrivateKeyKey: serverKeyPEM,
				webhookCACertKey:        caCertPEM,
			},
		}
		if err := kubeClient.Create(ctx, secret); err != nil {
			return fmt.Errorf("create webhook cert secret: %w", err)
		}
	case err != nil:
		return fmt.Errorf("read webhook cert secret: %w", err)
	default:
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		secret.Type = corev1.SecretTypeTLS
		secret.Data[webhookTLSCertKey] = serverCertPEM
		secret.Data[webhookTLSPrivateKeyKey] = serverKeyPEM
		secret.Data[webhookCACertKey] = caCertPEM
		if err := kubeClient.Update(ctx, secret); err != nil {
			return fmt.Errorf("update webhook cert secret: %w", err)
		}
	}

	if err := patchMutatingWebhookCABundle(ctx, kubeClient, caCertPEM, namespace, serviceName); err != nil {
		return err
	}
	if err := patchValidatingWebhookCABundle(ctx, kubeClient, caCertPEM, namespace, serviceName); err != nil {
		return err
	}
	return nil
}

func patchMutatingWebhookCABundle(ctx context.Context, kubeClient client.Client, caBundle []byte, namespace, serviceName string) error {
	config := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := waitForObject(ctx, kubeClient, client.ObjectKey{Name: webhookMutatingConfigName}, config); err != nil {
		return fmt.Errorf("read mutating webhook configuration: %w", err)
	}
	for i := range config.Webhooks {
		if config.Webhooks[i].ClientConfig.Service == nil {
			continue
		}
		if config.Webhooks[i].ClientConfig.Service.Name == serviceName && config.Webhooks[i].ClientConfig.Service.Namespace == namespace {
			config.Webhooks[i].ClientConfig.CABundle = append([]byte(nil), caBundle...)
		}
	}
	if err := kubeClient.Update(ctx, config); err != nil {
		return fmt.Errorf("update mutating webhook configuration: %w", err)
	}
	return nil
}

func patchValidatingWebhookCABundle(ctx context.Context, kubeClient client.Client, caBundle []byte, namespace, serviceName string) error {
	config := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := waitForObject(ctx, kubeClient, client.ObjectKey{Name: webhookValidatingConfigName}, config); err != nil {
		return fmt.Errorf("read validating webhook configuration: %w", err)
	}
	for i := range config.Webhooks {
		if config.Webhooks[i].ClientConfig.Service == nil {
			continue
		}
		if config.Webhooks[i].ClientConfig.Service.Name == serviceName && config.Webhooks[i].ClientConfig.Service.Namespace == namespace {
			config.Webhooks[i].ClientConfig.CABundle = append([]byte(nil), caBundle...)
		}
	}
	if err := kubeClient.Update(ctx, config); err != nil {
		return fmt.Errorf("update validating webhook configuration: %w", err)
	}
	return nil
}

func generateWebhookCertBundle(namespace, serviceName string) ([]byte, []byte, []byte, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	caTemplate, err := certificateTemplate("servicer-webhook-ca", true, nil)
	if err != nil {
		return nil, nil, nil, err
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}
	caPEM, caKeyPEM, err := encodeCertificateMaterial(caDER, caKey)
	if err != nil {
		return nil, nil, nil, err
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, nil, nil, err
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	dnsNames := []string{
		serviceName,
		fmt.Sprintf("%s.%s", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc", serviceName, namespace),
		fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace),
	}
	serverTemplate, err := certificateTemplate(serviceName, false, dnsNames)
	if err != nil {
		return nil, nil, nil, err
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caCert, &serverKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, err
	}
	serverPEM, serverKeyPEM, err := encodeCertificateMaterial(serverDER, serverKey)
	if err != nil {
		return nil, nil, nil, err
	}
	_ = caKeyPEM
	return caPEM, serverPEM, serverKeyPEM, nil
}

func certificateTemplate(commonName string, isCA bool, dnsNames []string) (*x509.Certificate, error) {
	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: commonName,
		},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              dnsNames,
	}
	if isCA {
		template.IsCA = true
		template.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature
		template.ExtKeyUsage = nil
	}
	return template, nil
}

func encodeCertificateMaterial(certDER []byte, privateKey *ecdsa.PrivateKey) ([]byte, []byte, error) {
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return nil, nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

func waitForObject(ctx context.Context, kubeClient client.Client, key client.ObjectKey, obj client.Object) error {
	var lastErr error
	for i := 0; i < 30; i++ {
		if err := kubeClient.Get(ctx, key, obj); err == nil {
			return nil
		} else if !apierrors.IsNotFound(err) {
			return err
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timed out waiting for %s", key.Name)
	}
	return lastErr
}
