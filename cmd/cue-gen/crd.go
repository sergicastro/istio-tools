// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"encoding/json"
	"fmt"
	"log"

	"cuelang.org/go/encoding/openapi"
	apiext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	structuralschema "k8s.io/apiextensions-apiserver/pkg/apiserver/schema"
	"k8s.io/utils/pointer"
	crdutil "sigs.k8s.io/controller-tools/pkg/crd"
)

const (
	statusOutput = `
status:
  acceptedNames:
    kind: ""
    plural: ""
  conditions: null
  storedVersions: null`

	creationTimestampOutput = `
  creationTimestamp: null`
)

// patchEmtpyTypesToObjectType walks the given schema to find those properties that has no type set and makes them to be an Object.
// That's a specific patch that lets the generated CRDs be valid to install in k8s servers when they use generic types like `google.protobuf.Value`.
// https://github.com/istio/api/blob/1.15.5/operator/v1alpha1/operator.proto#L323
// This is resolved to Top type for cue (meaning it allows anything), and OpenAPI spec does is not that generic to allow this kind of `any` specification
// for the schema.
func patchEmtpyTypesToObjectType(j *apiextv1.JSONSchemaProps) {
	if j.Type == "" {
		j.Type = "object"
		j.XPreserveUnknownFields = pointer.Bool(true)
	}

	// walk properties
	ps := make(map[string]apiextv1.JSONSchemaProps)
	for s, p := range j.Properties {
		patchEmtpyTypesToObjectType(&p)
		ps[s] = p
	}
	j.Properties = ps

	// walk items
	if j.Items != nil {
		patchEmtpyTypesToObjectType(j.Items.Schema)
		for _, i := range j.Items.JSONSchemas {
			patchEmtpyTypesToObjectType(&i)
		}
	}
}

// Build CRDs based on the configuration and schema.
//
//nolint:staticcheck,interfacer,lll
func completeCRD(c *apiextv1.CustomResourceDefinition, versionSchemas map[string]*openapi.OrderedMap, statusSchema *openapi.OrderedMap, preserveUnknownFields map[string][]string) {
	for i, version := range c.Spec.Versions {

		b, err := versionSchemas[version.Name].MarshalJSON()
		if err != nil {
			log.Fatalf("Cannot marshal OpenAPI schema for %v: %v", c.Name, err)
		}

		j := &apiextv1.JSONSchemaProps{}
		if err = json.Unmarshal(b, j); err != nil {
			log.Fatalf("Cannot unmarshal raw OpenAPI schema to JSONSchemaProps for %v: %v", c.Name, err)
		}

		// mark fields as `x-kubernetes-preserve-unknown-fields: true` using the visitor
		if fs, ok := preserveUnknownFields[version.Name]; ok {
			for _, f := range fs {
				p := &preserveUnknownFieldVisitor{path: f}
				crdutil.EditSchema(j, p)
			}
		}

		patchEmtpyTypesToObjectType(j)

		version.Schema = &apiextv1.CustomResourceValidation{OpenAPIV3Schema: &apiextv1.JSONSchemaProps{
			Type: "object",
			Properties: map[string]apiextv1.JSONSchemaProps{
				"spec": *j,
			},
		}}

		// only add status schema validation when status subresource is enabled in the CRD.
		if version.Subresources != nil {
			status := &apiextv1.JSONSchemaProps{}
			if statusSchema == nil {
				status = &apiextv1.JSONSchemaProps{
					Type:                   "object",
					XPreserveUnknownFields: pointer.BoolPtr(true),
				}
			} else {
				o, err := statusSchema.MarshalJSON()
				if err != nil {
					log.Fatal("Cannot marshal OpenAPI schema for the status field")
				}

				if err = json.Unmarshal(o, status); err != nil {
					log.Fatal("Cannot unmarshal raw status schema to JSONSchemaProps")
				}
			}

			version.Schema.OpenAPIV3Schema.Properties["status"] = *status
		}

		fmt.Printf("Checking if the schema is structural for %v \n", c.Name)
		if err = validateStructural(version.Schema.OpenAPIV3Schema); err != nil {
			log.Fatal(err)
		}

		c.Spec.Versions[i] = version
	}

	c.APIVersion = apiextv1.SchemeGroupVersion.String()
	c.Kind = "CustomResourceDefinition"

	// marshal to an empty field in the output
	c.Status = apiextv1.CustomResourceDefinitionStatus{}
}

func validateStructural(s *apiextv1.JSONSchemaProps) error {
	out := &apiext.JSONSchemaProps{}
	if err := apiextv1.Convert_v1_JSONSchemaProps_To_apiextensions_JSONSchemaProps(s, out, nil); err != nil {
		return fmt.Errorf("cannot convert v1 JSONSchemaProps to JSONSchemaProps: %v", err)
	}

	r, err := structuralschema.NewStructural(out)
	if err != nil {
		return fmt.Errorf("cannot convert to a structural schema: %v", err)
	}

	if errs := structuralschema.ValidateStructural(nil, r); len(errs) != 0 {
		return fmt.Errorf("schema is not structural: %v", errs.ToAggregate().Error())
	}

	return nil
}
