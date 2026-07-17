package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFailClearsStalledConditionOnNonStalledFailure(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	app := &v1alpha1.GitApplication{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo",
			Namespace: "app-ns",
		},
		Spec: v1alpha1.GitApplicationSpec{
			Interval: metav1.Duration{},
		},
	}
	cl := clientfake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&v1alpha1.GitApplication{}).
		WithObjects(app).
		Build()

	reconciler := &GitApplicationReconciler{Client: cl}
	ctx := context.Background()

	if err := reconciler.fail(ctx, app, "source fetch", errors.New("secret missing"), true); err == nil {
		t.Fatal("expected error")
	}
	if err := reconciler.fail(ctx, app, "render", errors.New("template failed"), false); err == nil {
		t.Fatal("expected error")
	}

	var updated v1alpha1.GitApplication
	if err := cl.Get(ctx, k8sclient.ObjectKeyFromObject(app), &updated); err != nil {
		t.Fatalf("get app: %v", err)
	}
	cond := meta.FindStatusCondition(updated.Status.Conditions, "Stalled")
	if cond == nil {
		t.Fatal("expected stalled condition")
	}
	if cond.Status != metav1.ConditionFalse {
		t.Fatalf("Stalled status = %s, want False", cond.Status)
	}
}
