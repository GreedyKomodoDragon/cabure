package render

import (
	"context"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Renderer interface {
	Render(ctx context.Context, checkoutRoot string, spec v1alpha1.GitApplicationSpec, resolvedRevision string) ([]*unstructured.Unstructured, error)
}
