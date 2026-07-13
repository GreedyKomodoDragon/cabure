package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,path=gitapplications,shortName=ga
// +kubebuilder:printcolumn:name="Revision",type=string,JSONPath=`.status.appliedRevision`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type GitApplication struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GitApplicationSpec   `json:"spec,omitempty"`
	Status GitApplicationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GitApplicationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []GitApplication `json:"items"`
}

type GitApplicationSpec struct {
	Source                    GitSourceSpec   `json:"source"`
	Destination               DestinationSpec `json:"destination"`
	Render                    RenderSpec      `json:"render"`
	AllowedClusterScopedKinds []string        `json:"allowedClusterScopedKinds,omitempty"`
	TakeoverExistingResources bool            `json:"takeoverExistingResources,omitempty"`
	Interval                  metav1.Duration `json:"interval,omitempty"`
	Prune                     bool            `json:"prune,omitempty"`
	Suspend                   bool            `json:"suspend,omitempty"`
}

type GitSourceSpec struct {
	// +kubebuilder:validation:Pattern=`^(https://.+|ssh://.+|[^@]+@[^:]+:.+)$`
	Repository string `json:"repository"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=main
	Revision string `json:"revision,omitempty"`
	// +kubebuilder:validation:MinLength=1
	Path      string                       `json:"path"`
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
}

type DestinationSpec struct {
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Namespace string `json:"namespace"`
}

type RenderSpec struct {
	// +kubebuilder:validation:Enum=yaml;helm
	Type string          `json:"type"`
	Helm *HelmRenderSpec `json:"helm,omitempty"`
}

type HelmRenderSpec struct {
	// +kubebuilder:validation:MinLength=1
	ReleaseName string   `json:"releaseName"`
	ValuesFiles []string `json:"valuesFiles,omitempty"`
	IncludeCRDs bool     `json:"includeCRDs,omitempty"`
}

type GitApplicationStatus struct {
	ObservedGeneration int64               `json:"observedGeneration,omitempty"`
	AttemptedRevision  string              `json:"attemptedRevision,omitempty"`
	AppliedRevision    string              `json:"appliedRevision,omitempty"`
	LastAttemptTime    *metav1.Time        `json:"lastAttemptTime,omitempty"`
	LastSuccessTime    *metav1.Time        `json:"lastSuccessTime,omitempty"`
	Inventory          []ResourceReference `json:"inventory,omitempty"`
	Conditions         []metav1.Condition  `json:"conditions,omitempty"`
}

type ResourceReference struct {
	Group     string `json:"group,omitempty"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

func init() {
	SchemeBuilder.Register(&GitApplication{}, &GitApplicationList{})
}

func (in *GitApplication) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(GitApplication)
	in.DeepCopyInto(out)
	return out
}

func (in *GitApplication) DeepCopy() *GitApplication {
	if in == nil {
		return nil
	}
	out := new(GitApplication)
	in.DeepCopyInto(out)
	return out
}

func (in *GitApplicationList) DeepCopyObject() runtime.Object {
	if in == nil {
		return nil
	}
	out := new(GitApplicationList)
	in.DeepCopyInto(out)
	return out
}

func (in *GitApplicationList) DeepCopy() *GitApplicationList {
	if in == nil {
		return nil
	}
	out := new(GitApplicationList)
	in.DeepCopyInto(out)
	return out
}

func (in *GitApplication) DeepCopyInto(out *GitApplication) {
	*out = *in
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
}

func (in *GitApplicationList) DeepCopyInto(out *GitApplicationList) {
	*out = *in
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		out.Items = make([]GitApplication, len(in.Items))
		for i := range in.Items {
			in.Items[i].DeepCopyInto(&out.Items[i])
		}
	}
}

func (in *GitApplicationSpec) DeepCopyInto(out *GitApplicationSpec) {
	*out = *in
	if in.Source.SecretRef != nil {
		ref := *in.Source.SecretRef
		out.Source.SecretRef = &ref
	}
	if in.Render.Helm != nil {
		helm := *in.Render.Helm
		if in.Render.Helm.ValuesFiles != nil {
			helm.ValuesFiles = append([]string(nil), in.Render.Helm.ValuesFiles...)
		}
		out.Render.Helm = &helm
	}
	if in.AllowedClusterScopedKinds != nil {
		out.AllowedClusterScopedKinds = append([]string(nil), in.AllowedClusterScopedKinds...)
	}
}

func (in *GitApplicationStatus) DeepCopyInto(out *GitApplicationStatus) {
	*out = *in
	if in.LastAttemptTime != nil {
		t := in.LastAttemptTime.DeepCopy()
		out.LastAttemptTime = t
	}
	if in.LastSuccessTime != nil {
		t := in.LastSuccessTime.DeepCopy()
		out.LastSuccessTime = t
	}
	if in.Inventory != nil {
		out.Inventory = append([]ResourceReference(nil), in.Inventory...)
	}
	if in.Conditions != nil {
		out.Conditions = append([]metav1.Condition(nil), in.Conditions...)
	}
}

func (in *ResourceReference) DeepCopyInto(out *ResourceReference) {
	*out = *in
}

func (r ResourceReference) Key() string {
	return schema.GroupVersionKind{Group: r.Group, Version: r.Version, Kind: r.Kind}.String() + "/" + r.Namespace + "/" + r.Name
}
