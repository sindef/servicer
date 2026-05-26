package controllers

import (
	"context"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestDiscoveredOperatorInventoryMergesDeclaredAndDetected(t *testing.T) {
	scheme := inventoryTestScheme(t)
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}

	targetClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(
			testCRD("clusters.postgresql.cnpg.io"),
			testCRD("externalsecrets.external-secrets.io"),
			testCRD("ybuniverses.operator.yugabyte.io"),
			testCRD(kubeVirtVirtualMachineCRDName),
			testCRD(kubeVirtDataVolumeCRDName),
		).
		Build()

	inventory, err := discoveredOperatorInventory(context.Background(), targetClient, []string{"cnpg", " custom ", "external-secrets"})
	if err != nil {
		t.Fatalf("discoveredOperatorInventory returned error: %v", err)
	}

	if strings.Join(inventory, ",") != "cnpg,custom,external-secrets,kubevirt,yugabyte" {
		t.Fatalf("unexpected operator inventory %#v", inventory)
	}
}

func TestDiscoveredOperatorInventoryRequiresBothKubeVirtCRDs(t *testing.T) {
	scheme := inventoryTestScheme(t)
	if err := apiextensionsv1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme returned error: %v", err)
	}

	for _, tc := range []struct {
		name    string
		crds    []string
		wantHas bool
	}{
		{
			name:    "only vm crd",
			crds:    []string{kubeVirtVirtualMachineCRDName},
			wantHas: false,
		},
		{
			name:    "only datavolume crd",
			crds:    []string{kubeVirtDataVolumeCRDName},
			wantHas: false,
		},
		{
			name:    "both kubevirt crds",
			crds:    []string{kubeVirtVirtualMachineCRDName, kubeVirtDataVolumeCRDName},
			wantHas: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objects := make([]client.Object, 0, len(tc.crds))
			for _, crdName := range tc.crds {
				objects = append(objects, testCRD(crdName))
			}

			targetClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			inventory, err := discoveredOperatorInventory(context.Background(), targetClient, nil)
			if err != nil {
				t.Fatalf("discoveredOperatorInventory returned error: %v", err)
			}

			hasKubeVirt := false
			for _, item := range inventory {
				if item == kubeVirtServiceClassDriver {
					hasKubeVirt = true
					break
				}
			}
			if hasKubeVirt != tc.wantHas {
				t.Fatalf("expected kubevirt presence=%t, inventory=%#v", tc.wantHas, inventory)
			}
		})
	}
}

func testCRD(name string) *apiextensionsv1.CustomResourceDefinition {
	return &apiextensionsv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{Name: name},
	}
}
