package controllers

import (
	"encoding/json"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func mapToJSON(values any) *apiextensionsv1.JSON {
	if values == nil {
		return nil
	}
	raw, err := json.Marshal(values)
	if err != nil || string(raw) == "null" || string(raw) == "{}" {
		return nil
	}
	return &apiextensionsv1.JSON{Raw: raw}
}
