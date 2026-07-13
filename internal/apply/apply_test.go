package apply

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestNormalizeAndValidateAllowsSelectedClusterScopedKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		obj  *unstructured.Unstructured
	}{
		{
			name: "namespace",
			obj:  newObject("Namespace", "v1", "example", ""),
		},
		{
			name: "crd",
			obj:  newObject("CustomResourceDefinition", "apiextensions.k8s.io/v1", "widgets.example.com", "ignored"),
		},
		{
			name: "clusterrole",
			obj:  newObject("ClusterRole", "rbac.authorization.k8s.io/v1", "example-role", "ignored"),
		},
		{
			name: "clusterrolebinding",
			obj:  newObject("ClusterRoleBinding", "rbac.authorization.k8s.io/v1", "example-binding", "ignored"),
		},
	}

	mapper := newFakeRESTMapper(restMapperCasesFromObjects(cases)...)
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, refs, err := NormalizeAndValidate(context.Background(), mapper, []*unstructured.Unstructured{tc.obj.DeepCopy()}, Policy{
				DestinationNamespace:      "apps",
				ApplicationUID:            "uid-1",
				SourceRevision:            "main",
				AllowClusterScoped:        true,
				AllowedClusterScopedKinds: []string{"Namespace", "CustomResourceDefinition", "ClusterRole", "ClusterRoleBinding"},
				FieldManager:              "cabure",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := out[0].GetNamespace(); got != "" {
				t.Fatalf("expected cluster-scoped object namespace to be cleared, got %q", got)
			}
			if len(refs) != 1 {
				t.Fatalf("expected 1 resource reference, got %d", len(refs))
			}
			if refs[0].Kind != tc.obj.GetKind() {
				t.Fatalf("expected kind %q, got %q", tc.obj.GetKind(), refs[0].Kind)
			}
		})
	}
}

func TestNormalizeAndValidateRejectsUnsupportedClusterScopedKind(t *testing.T) {
	t.Parallel()

	obj := newObject("Namespace", "v1", "apps", "")
	mapper := newFakeRESTMapper(restMapperCase{obj: obj})

	_, _, err := NormalizeAndValidate(context.Background(), mapper, []*unstructured.Unstructured{obj}, Policy{
		DestinationNamespace:      "apps",
		ApplicationUID:            "uid-1",
		SourceRevision:            "main",
		AllowClusterScoped:        true,
		AllowedClusterScopedKinds: []string{"ClusterRole"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("expected not allowed error, got %v", err)
	}
}

func TestNormalizeAndValidateRejectsClusterScopedWhenDisabled(t *testing.T) {
	t.Parallel()

	obj := newObject("ClusterRole", "rbac.authorization.k8s.io/v1", "example-role", "")
	mapper := newFakeRESTMapper(restMapperCase{obj: obj})

	_, _, err := NormalizeAndValidate(context.Background(), mapper, []*unstructured.Unstructured{obj}, Policy{
		DestinationNamespace: "apps",
		ApplicationUID:       "uid-1",
		SourceRevision:       "main",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("expected disabled error, got %v", err)
	}
}

type fakeRESTMapper struct {
	mappings map[string]fakeRESTMapping
}

type fakeRESTMapping struct {
	Resource schema.GroupVersionResource
	Kind     schema.GroupVersionKind
	Scope    testScope
}

type testScope struct {
	name meta.RESTScopeName
}

func (s testScope) Name() meta.RESTScopeName { return s.name }

type restMapperCase struct {
	obj *unstructured.Unstructured
}

func restMapperCasesFromObjects(cases []struct {
	name string
	obj  *unstructured.Unstructured
}) []restMapperCase {
	out := make([]restMapperCase, 0, len(cases))
	for _, tc := range cases {
		out = append(out, restMapperCase{obj: tc.obj})
	}
	return out
}

func newFakeRESTMapper(cases ...restMapperCase) *fakeRESTMapper {
	mappings := make(map[string]fakeRESTMapping, len(cases))
	for _, tc := range cases {
		if tc.obj == nil {
			continue
		}
		gvk := tc.obj.GroupVersionKind()
		mappings[gvk.Kind] = fakeRESTMapping{
			Resource: schema.GroupVersionResource{Group: gvk.Group, Version: gvk.Version, Resource: strings.ToLower(gvk.Kind) + "s"},
			Kind:     gvk,
			Scope:    testScope{name: meta.RESTScopeNameRoot},
		}
	}
	return &fakeRESTMapper{mappings: mappings}
}

func (m *fakeRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	for _, mapping := range m.mappings {
		if mapping.Resource.Group == resource.Group && mapping.Resource.Version == resource.Version && mapping.Resource.Resource == resource.Resource {
			return mapping.Kind, nil
		}
	}
	return schema.GroupVersionKind{}, fmt.Errorf("unknown resource %s", resource.Resource)
}

func (m *fakeRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	kind, err := m.KindFor(resource)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionKind{kind}, nil
}

func (m *fakeRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	for _, mapping := range m.mappings {
		if mapping.Resource.Group == input.Group && mapping.Resource.Version == input.Version && mapping.Resource.Resource == input.Resource {
			return mapping.Resource, nil
		}
	}
	return schema.GroupVersionResource{}, fmt.Errorf("unknown resource %s", input.Resource)
}

func (m *fakeRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	resource, err := m.ResourceFor(input)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionResource{resource}, nil
}

func (m *fakeRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if mapping, ok := m.mappings[gk.Kind]; ok {
		return &meta.RESTMapping{Resource: mapping.Resource, GroupVersionKind: mapping.Kind, Scope: mapping.Scope}, nil
	}
	return nil, fmt.Errorf("unknown kind %s", gk.Kind)
}

func (m *fakeRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	mapping, err := m.RESTMapping(gk, versions...)
	if err != nil {
		return nil, err
	}
	return []*meta.RESTMapping{mapping}, nil
}

func (m *fakeRESTMapper) ResourceSingularizer(resource string) (string, error) {
	return strings.TrimSuffix(resource, "s"), nil
}

func newObject(kind, apiVersion, name, namespace string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata": map[string]any{
			"name": name,
		},
	}}
	if namespace != "" {
		_ = unstructured.SetNestedField(obj.Object, namespace, "metadata", "namespace")
	}
	return obj
}

var _ meta.RESTMapper = (*fakeRESTMapper)(nil)
var _ meta.RESTScope = testScope{}
