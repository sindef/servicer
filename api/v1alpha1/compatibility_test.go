package v1alpha1

import (
	"bytes"
	"io"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestStoredObjectFixturesRemainDecodable(t *testing.T) {
	payload, err := os.ReadFile("fixtures/stored-objects.yaml")
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(payload), 4096)
	seen := map[string]bool{}
	for {
		var object unstructured.Unstructured
		err := decoder.Decode(&object)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decode fixture: %v", err)
		}
		if object.GetAPIVersion() != GroupVersion.String() {
			t.Fatalf("unexpected apiVersion %q", object.GetAPIVersion())
		}
		if object.GetKind() == "" || object.GetName() == "" {
			t.Fatalf("fixture missing kind/name: %#v", object.Object)
		}
		seen[object.GetKind()] = true
	}
	for _, kind := range []string{"Tenant", "Project", "ServiceInstance"} {
		if !seen[kind] {
			t.Fatalf("missing stored object fixture for %s", kind)
		}
	}
}
