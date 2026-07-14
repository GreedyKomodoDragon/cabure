package render

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderYAMLFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "objects.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo
---

apiVersion: v1
kind: Secret
metadata:
  name: hidden
`), 0o600); err != nil {
		t.Fatal(err)
	}
	objs, err := renderYAMLFile(context.Background(), path)
	if err != nil {
		t.Fatalf("renderYAMLFile: %v", err)
	}
	if len(objs) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(objs))
	}
	if objs[0].GetKind() != "ConfigMap" || objs[1].GetKind() != "Secret" {
		t.Fatalf("unexpected kinds: %s, %s", objs[0].GetKind(), objs[1].GetKind())
	}
}
