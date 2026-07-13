package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

const (
	ManagedByLabel      = "app.kubernetes.io/managed-by"
	ManagedByValue      = "tiny-gitops"
	ApplicationUIDAnno  = "gitops.cabure.io/application-uid"
	SourceRevisionAnno  = "gitops.cabure.io/source-revision"
	DefaultFieldManager = "tiny-gitops-controller"
	MaxObjectSizeBytes  = 1024 * 1024
)

var supportedClusterScopedKinds = map[string]struct{}{
	"namespace":                {},
	"customresourcedefinition": {},
	"clusterrole":              {},
	"clusterrolebinding":       {},
}

type Policy struct {
	DestinationNamespace      string
	ApplicationUID            string
	SourceRevision            string
	AllowClusterScoped        bool
	AllowedClusterScopedKinds []string
	ForceApply                bool
	FieldManager              string
}

func NormalizeAndValidate(ctx context.Context, mapper meta.RESTMapper, objects []*unstructured.Unstructured, policy Policy) ([]*unstructured.Unstructured, []v1alpha1.ResourceReference, error) {
	_ = ctx
	if policy.FieldManager == "" {
		policy.FieldManager = DefaultFieldManager
	}
	var out []*unstructured.Unstructured
	var refs []v1alpha1.ResourceReference
	for idx, obj := range objects {
		if obj == nil {
			return nil, nil, fmt.Errorf("object %d: nil object", idx+1)
		}
		if err := basicValidate(obj); err != nil {
			return nil, nil, fmt.Errorf("object %d: %w", idx+1, err)
		}
		gvk := obj.GroupVersionKind()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return nil, nil, fmt.Errorf("%s/%s: discovery: %w", gvk.GroupKind().String(), obj.GetName(), err)
		}
		if mapping.Scope.Name() == meta.RESTScopeNameRoot {
			if !policy.AllowClusterScoped {
				return nil, nil, fmt.Errorf("%s/%s: cluster-scoped resources are disabled", gvk.Kind, obj.GetName())
			}
			if !clusterScopedKindAllowed(gvk.Kind, policy.AllowedClusterScopedKinds) {
				return nil, nil, fmt.Errorf("%s/%s: cluster-scoped kind %q is not allowed", gvk.Kind, obj.GetName(), gvk.Kind)
			}
		}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if ns := obj.GetNamespace(); ns == "" {
				obj.SetNamespace(policy.DestinationNamespace)
			} else if ns != policy.DestinationNamespace {
				return nil, nil, fmt.Errorf("%s/%s: namespace %q is outside destination namespace %q", gvk.Kind, obj.GetName(), ns, policy.DestinationNamespace)
			}
		} else {
			obj.SetNamespace("")
		}
		ensureMetadata(obj, policy)
		if err := sizeCheck(obj); err != nil {
			return nil, nil, fmt.Errorf("%s/%s: %w", gvk.Kind, obj.GetName(), err)
		}
		out = append(out, obj)
		refs = append(refs, resourceReferenceFor(mapping.Resource, obj))
	}
	sort.SliceStable(out, func(i, j int) bool {
		return applyPriority(out[i]) < applyPriority(out[j])
	})
	sort.SliceStable(refs, func(i, j int) bool {
		return refs[i].Key() < refs[j].Key()
	})
	return out, refs, nil
}

func Apply(ctx context.Context, dyn dynamic.Interface, mapper meta.RESTMapper, objects []*unstructured.Unstructured, fieldManager string, forceApply bool) error {
	if fieldManager == "" {
		fieldManager = DefaultFieldManager
	}
	for _, obj := range objects {
		patch, err := json.Marshal(obj.Object)
		if err != nil {
			return err
		}
		gvk := obj.GroupVersionKind()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return err
		}
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			_, err = dyn.Resource(mapping.Resource).Namespace(obj.GetNamespace()).Patch(ctx, obj.GetName(), types.ApplyPatchType, patch, metav1.PatchOptions{
				FieldManager: fieldManager,
				Force:        ptr(forceApply),
			})
		} else {
			_, err = dyn.Resource(mapping.Resource).Patch(ctx, obj.GetName(), types.ApplyPatchType, patch, metav1.PatchOptions{
				FieldManager: fieldManager,
				Force:        ptr(forceApply),
			})
		}
		if err != nil {
			return fmt.Errorf("%s/%s: apply: %w", gvk.Kind, obj.GetName(), err)
		}
	}
	return nil
}

func ApplyPriority(kind string) int {
	switch strings.ToLower(kind) {
	case "namespace":
		return 0
	case "customresourcedefinition":
		return 1
	case "serviceaccount":
		return 2
	case "configmap", "secret":
		return 3
	case "role", "clusterrole":
		return 4
	case "rolebinding", "clusterrolebinding":
		return 5
	case "service":
		return 6
	case "persistentvolumeclaim":
		return 7
	case "deployment", "statefulset", "daemonset", "job", "cronjob":
		return 8
	case "ingress":
		return 9
	default:
		return 10
	}
}

func applyPriority(obj *unstructured.Unstructured) int {
	return ApplyPriority(obj.GetKind())
}

func basicValidate(obj *unstructured.Unstructured) error {
	if obj.GetAPIVersion() == "" || obj.GetKind() == "" {
		return fmt.Errorf("missing apiVersion or kind")
	}
	if obj.GetName() == "" {
		if _, found, _ := unstructured.NestedString(obj.Object, "metadata", "generateName"); found {
			return fmt.Errorf("generateName without name is not allowed")
		}
		return fmt.Errorf("metadata.name is required")
	}
	return nil
}

func ensureMetadata(obj *unstructured.Unstructured, policy Policy) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[ManagedByLabel] = ManagedByValue
	obj.SetLabels(labels)

	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[ApplicationUIDAnno] = policy.ApplicationUID
	annotations[SourceRevisionAnno] = policy.SourceRevision
	obj.SetAnnotations(annotations)
}

func sizeCheck(obj *unstructured.Unstructured) error {
	raw, err := json.Marshal(obj.Object)
	if err != nil {
		return err
	}
	if len(raw) > MaxObjectSizeBytes {
		return fmt.Errorf("object exceeds size limit (%d bytes)", len(raw))
	}
	return nil
}

func resourceReferenceFor(res schema.GroupVersionResource, obj *unstructured.Unstructured) v1alpha1.ResourceReference {
	return v1alpha1.ResourceReference{
		Group:     res.Group,
		Version:   res.Version,
		Kind:      obj.GetKind(),
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func ptr[T any](v T) *T { return &v }

func IsSupportedClusterScopedKind(kind string) bool {
	_, ok := supportedClusterScopedKinds[strings.ToLower(kind)]
	return ok
}

func clusterScopedKindAllowed(kind string, allowed []string) bool {
	if len(allowed) == 0 {
		return false
	}
	candidate := strings.ToLower(kind)
	for _, item := range allowed {
		if strings.ToLower(item) == candidate {
			return IsSupportedClusterScopedKind(candidate)
		}
	}
	return false
}
