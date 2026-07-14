package render

import (
	"context"
	"errors"
	"testing"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
)

func TestYAMLRendererHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := YAMLRenderer{}.Render(ctx, t.TempDir(), v1alpha1.GitApplicationSpec{
		Source: v1alpha1.GitSourceSpec{Path: "apps/demo"},
	}, "sha")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestHelmRendererHonorsCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := HelmRenderer{}.Render(ctx, t.TempDir(), v1alpha1.GitApplicationSpec{
		Source:      v1alpha1.GitSourceSpec{Path: "apps/demo"},
		Destination: v1alpha1.DestinationSpec{Namespace: "demo"},
		Render: v1alpha1.RenderSpec{
			Type: "helm",
			Helm: &v1alpha1.HelmRenderSpec{ReleaseName: "demo"},
		},
	}, "sha")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
