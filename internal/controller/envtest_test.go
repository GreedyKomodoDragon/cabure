package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	cabureapply "github.com/GreedyKomodoDragon/cabure/internal/apply"
	"github.com/GreedyKomodoDragon/cabure/internal/git"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestEnvtestReconcile(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("envtest binaries not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd", "bases")},
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("start envtest: %v", err)
	}
	defer func() { _ = testEnv.Stop() }()

	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	k8sClient, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	checkoutDir := t.TempDir()
	appPath := filepath.Join(checkoutDir, "apps", "demo")
	if err := os.MkdirAll(appPath, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(appPath, "config.yaml"), []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: demo-config
data:
  value: hello
`), 0o600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	app := &v1alpha1.GitApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "gitops-system",
		},
		Spec: v1alpha1.GitApplicationSpec{
			Source: v1alpha1.GitSourceSpec{
				Repository: "https://example.com/repo.git",
				Revision:   "main",
				Path:       "apps/demo",
			},
			Destination: v1alpha1.DestinationSpec{Namespace: "demo"},
			Render:      v1alpha1.RenderSpec{Type: "yaml"},
			Interval:    metav1.Duration{Duration: time.Minute},
		},
	}
	if err := k8sClient.Create(ctx, app); err != nil {
		t.Fatalf("create app: %v", err)
	}

	initial := &unstructured.Unstructured{}
	initial.SetAPIVersion("v1")
	initial.SetKind("ConfigMap")
	initial.SetNamespace("demo")
	initial.SetName("demo-config")
	dyn := fake.NewSimpleDynamicClient(scheme, initial)

	reconciler := &GitApplicationReconciler{
		Client:  k8sClient,
		Dynamic: dyn,
		Mapper:  newTestRESTMapper(),
		Repo: fakeRepo{
			dir: checkoutDir,
			sha: "0123456789abcdef0123456789abcdef01234567",
		},
		Log: zap.New(zap.UseDevMode(true)),
		Config: OperatorConfig{
			MinimumRequeueInterval: 15 * time.Second,
		},
	}

	if _, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(app)}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	var updated v1alpha1.GitApplication
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: app.Namespace}, &updated); err != nil {
		t.Fatalf("get app: %v", err)
	}
	if updated.Status.AppliedRevision == "" {
		t.Fatal("expected applied revision")
	}

	obj, err := dyn.Resource(schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}).Namespace("demo").Get(ctx, "demo-config", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get managed object: %v", err)
	}
	if obj.GetLabels()[cabureapply.ManagedByLabel] != cabureapply.ManagedByValue {
		t.Fatalf("missing managed-by label: %#v", obj.GetLabels())
	}
}

type fakeRepo struct {
	dir string
	sha string
}

func (f fakeRepo) Checkout(context.Context, string, string, *git.Credentials) (string, string, error) {
	return filepath.Clean(f.dir), f.sha, nil
}

type testRESTMapper struct {
	mapping *fakeRESTMapping
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

func newTestRESTMapper() *testRESTMapper {
	return &testRESTMapper{
		mapping: &fakeRESTMapping{
			Resource: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"},
			Kind:     schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"},
			Scope:    testScope{name: meta.RESTScopeNameNamespace},
		},
	}
}

func (m *testRESTMapper) KindFor(resource schema.GroupVersionResource) (schema.GroupVersionKind, error) {
	if resource.Resource == "configmaps" {
		return m.mapping.Kind, nil
	}
	return schema.GroupVersionKind{}, fmt.Errorf("unknown resource %s", resource.Resource)
}

func (m *testRESTMapper) KindsFor(resource schema.GroupVersionResource) ([]schema.GroupVersionKind, error) {
	kind, err := m.KindFor(resource)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionKind{kind}, nil
}

func (m *testRESTMapper) ResourceFor(input schema.GroupVersionResource) (schema.GroupVersionResource, error) {
	if input.Resource == "configmaps" {
		return m.mapping.Resource, nil
	}
	return schema.GroupVersionResource{}, fmt.Errorf("unknown resource %s", input.Resource)
}

func (m *testRESTMapper) ResourcesFor(input schema.GroupVersionResource) ([]schema.GroupVersionResource, error) {
	resource, err := m.ResourceFor(input)
	if err != nil {
		return nil, err
	}
	return []schema.GroupVersionResource{resource}, nil
}

func (m *testRESTMapper) RESTMapping(gk schema.GroupKind, versions ...string) (*meta.RESTMapping, error) {
	if gk.Kind == "ConfigMap" {
		return &meta.RESTMapping{Resource: m.mapping.Resource, GroupVersionKind: m.mapping.Kind, Scope: m.mapping.Scope}, nil
	}
	return nil, fmt.Errorf("unknown kind %s", gk.Kind)
}

func (m *testRESTMapper) RESTMappings(gk schema.GroupKind, versions ...string) ([]*meta.RESTMapping, error) {
	mapping, err := m.RESTMapping(gk, versions...)
	if err != nil {
		return nil, err
	}
	return []*meta.RESTMapping{mapping}, nil
}

func (m *testRESTMapper) ResourceSingularizer(resource string) (string, error) {
	if resource == "configmaps" {
		return "configmap", nil
	}
	return resource, nil
}
