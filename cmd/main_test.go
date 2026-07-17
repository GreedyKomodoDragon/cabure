package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"github.com/go-logr/logr"
	"k8s.io/client-go/tools/cache"
	cachetesting "k8s.io/client-go/tools/cache/testing"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
)

type panicReconciler struct {
	once   sync.Once
	called chan struct{}
}

func (p *panicReconciler) Reconcile(context.Context, ctrl.Request) (ctrl.Result, error) {
	p.once.Do(func() {
		close(p.called)
	})
	panic("boom")
}

func TestRunWorkerRecoversFromPanic(t *testing.T) {
	t.Parallel()

	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: "gitapplications"},
	)
	queue.Add("default/demo")

	called := make(chan struct{})
	runner := &operatorRunner{
		reconciler: &panicReconciler{called: called},
		queue:      queue,
		log:        logr.Discard(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		runner.runWorker(ctx)
	}()

	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for reconciler to run")
	}

	queue.ShutDown()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for worker to exit")
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	t.Parallel()

	source := cachetesting.NewFakeControllerSource()
	informer := cache.NewSharedIndexInformer(source, &v1alpha1.GitApplication{}, 0, cache.Indexers{})
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: "gitapplications"},
	)
	ready := &atomic.Bool{}
	runner := &operatorRunner{
		reconciler: &panicReconciler{called: make(chan struct{})},
		informer:   informer,
		queue:      queue,
		workers:    1,
		log:        logr.Discard(),
		ready:      ready,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.run(ctx)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for runner to exit")
	}
}
