package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion identifies the API group and version for Servicer platform resources.
	GroupVersion = schema.GroupVersion{Group: "platform.servicer.io", Version: "v1alpha1"}

	// SchemeBuilder registers the Servicer API types with a runtime scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the Servicer API group-version to a runtime scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
