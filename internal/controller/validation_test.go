package controller

import (
	"testing"
	"time"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateSpecRejectsShortInterval(t *testing.T) {
	app := &v1alpha1.GitApplication{
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "https://example.com/repo.git",
				Path:       "apps/demo",
			},
			Destination: v1alpha1.DestinationSpec{Namespace: "demo"},
			Render:      v1alpha1.RenderSpec{Type: "yaml"},
			Interval:    metav1.Duration{Duration: 10 * time.Second},
		},
	}
	if err := validateSpec(app, OperatorConfig{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestValidateSpecHonorsConfiguredMinimumInterval(t *testing.T) {
	app := &v1alpha1.GitApplication{
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "https://example.com/repo.git",
				Path:       "apps/demo",
			},
			Destination: v1alpha1.DestinationSpec{Namespace: "demo"},
			Render:      v1alpha1.RenderSpec{Type: "yaml"},
			Interval:    metav1.Duration{Duration: 10 * time.Second},
		},
	}
	if err := validateSpec(app, OperatorConfig{MinimumRequeueInterval: 5 * time.Second}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSpecAcceptsHelm(t *testing.T) {
	app := &v1alpha1.GitApplication{
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "https://example.com/repo.git",
				Path:       "apps/demo",
			},
			Destination: v1alpha1.DestinationSpec{Namespace: "demo"},
			Render: v1alpha1.RenderSpec{
				Type: "helm",
				Helm: &v1alpha1.HelmRenderSpec{
					ReleaseName: "demo",
					ValuesFiles: []string{"values.yaml"},
				},
			},
			Interval: metav1.Duration{Duration: time.Minute},
		},
	}
	if err := validateSpec(app, OperatorConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSpecAcceptsSSHRepository(t *testing.T) {
	app := &v1alpha1.GitApplication{
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "git@example.com:org/repo.git",
				Path:       "apps/demo",
			},
			Destination: v1alpha1.DestinationSpec{Namespace: "demo"},
			Render:      v1alpha1.RenderSpec{Type: "yaml"},
			Interval:    metav1.Duration{Duration: time.Minute},
		},
	}
	if err := validateSpec(app, OperatorConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSpecAcceptsClusterScopedAllowList(t *testing.T) {
	app := &v1alpha1.GitApplication{
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "https://example.com/repo.git",
				Path:       "apps/demo",
			},
			Destination:               v1alpha1.DestinationSpec{Namespace: "demo"},
			Render:                    v1alpha1.RenderSpec{Type: "yaml"},
			AllowedClusterScopedKinds: []string{"Namespace", "ClusterRole", "ClusterRoleBinding", "CustomResourceDefinition"},
			Interval:                  metav1.Duration{Duration: time.Minute},
		},
	}
	if err := validateSpec(app, OperatorConfig{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateSpecRejectsUnsupportedClusterScopedAllowList(t *testing.T) {
	app := &v1alpha1.GitApplication{
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "https://example.com/repo.git",
				Path:       "apps/demo",
			},
			Destination:               v1alpha1.DestinationSpec{Namespace: "demo"},
			Render:                    v1alpha1.RenderSpec{Type: "yaml"},
			AllowedClusterScopedKinds: []string{"StatefulSet"},
			Interval:                  metav1.Duration{Duration: time.Minute},
		},
	}
	if err := validateSpec(app, OperatorConfig{}); err == nil {
		t.Fatal("expected validation error")
	}
}
