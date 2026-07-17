package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
)

func TestHelmRendererAllowsSiblingValuesFilesWithinCheckoutRoot(t *testing.T) {
	checkoutRoot := t.TempDir()
	chartRoot := filepath.Join(checkoutRoot, "infra", "database-charts", "dragonfly")
	valuesPath := filepath.Join(checkoutRoot, "infra", "dev-overrides", "dragonfly-dev.yaml")

	if err := os.MkdirAll(filepath.Join(chartRoot, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir chart: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(valuesPath), 0o755); err != nil {
		t.Fatalf("mkdir values dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartRoot, "Chart.yaml"), []byte(`apiVersion: v2
name: dragonfly
version: 0.1.0
`), 0o600); err != nil {
		t.Fatalf("write chart: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartRoot, "templates", "configmap.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}
data:
  message: {{ .Values.data.message | quote }}
`), 0o600); err != nil {
		t.Fatalf("write template: %v", err)
	}
	if err := os.WriteFile(valuesPath, []byte(`data:
  message: hello from overlay
`), 0o600); err != nil {
		t.Fatalf("write values: %v", err)
	}

	objs, err := HelmRenderer{}.Render(context.Background(), checkoutRoot, v1alpha1.GitApplicationSpec{
		Source: v1alpha1.GitSourceSpec{Path: "infra/database-charts/dragonfly"},
		Destination: v1alpha1.DestinationSpec{
			Namespace: "dragonfly",
		},
		Render: v1alpha1.RenderSpec{
			Type: "helm",
			Helm: &v1alpha1.HelmRenderSpec{
				ReleaseName: "dragonfly",
				ValuesFiles: []string{"infra/dev-overrides/dragonfly-dev.yaml"},
			},
		},
	}, "sha")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if len(objs) != 1 {
		t.Fatalf("expected 1 object, got %d", len(objs))
	}
	if got := objs[0].GetKind(); got != "ConfigMap" {
		t.Fatalf("unexpected kind: %s", got)
	}
	if got := objs[0].Object["data"].(map[string]any)["message"]; got != "hello from overlay" {
		t.Fatalf("unexpected value: %v", got)
	}
}
