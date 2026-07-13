package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	"github.com/GreedyKomodoDragon/cabure/internal/controller"
	"github.com/GreedyKomodoDragon/cabure/internal/git"
	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	toolscache "k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var gitApplicationGVR = schema.GroupVersionResource{
	Group:    "gitops.cabure.io",
	Version:  "v1alpha1",
	Resource: "gitapplications",
}

func main() {
	var (
		metricsAddr            string
		probeAddr              string
		leaderElect            bool
		watchNamespace         string
		concurrentReconciles   int
		minimumRequeueInterval time.Duration
		allowClusterScoped     bool
		allowedRepoPrefixes    string
		fieldManager           string
		cacheDir               string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "")
	flag.BoolVar(&leaderElect, "leader-elect", true, "")
	flag.StringVar(&watchNamespace, "watch-namespace", "", "")
	flag.IntVar(&concurrentReconciles, "concurrent-reconciles", 2, "")
	flag.DurationVar(&minimumRequeueInterval, "minimum-requeue-interval", 15*time.Second, "")
	flag.BoolVar(&allowClusterScoped, "allow-cluster-scoped-resources", false, "")
	flag.StringVar(&allowedRepoPrefixes, "allowed-repository-prefixes", "", "")
	flag.StringVar(&fieldManager, "field-manager", "tiny-gitops-controller", "")
	flag.StringVar(&cacheDir, "cache-dir", "", "")
	zapOpts := zap.Options{Development: true}
	zapOpts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	ctx := ctrl.SetupSignalHandler()
	logger := ctrl.Log.WithName("setup")
	cfg := ctrl.GetConfigOrDie()

	ready := &atomic.Bool{}
	metricsServer := newMetricsServer(metricsAddr)
	probeServer := newProbeServer(probeAddr, ready)
	if err := metricsServer.Start(ctx); err != nil {
		logger.Error(err, "unable to start metrics server")
		os.Exit(1)
	}
	if err := probeServer.Start(ctx); err != nil {
		logger.Error(err, "unable to start probe server")
		os.Exit(1)
	}

	runner, err := newOperatorRunner(cfg, operatorOptions{
		WatchNamespace:         watchNamespace,
		ConcurrentReconciles:   concurrentReconciles,
		MinimumRequeueInterval: minimumRequeueInterval,
		AllowClusterScoped:     allowClusterScoped,
		AllowedRepoPrefixes:    splitAllowedPrefixes(allowedRepoPrefixes),
		FieldManager:           fieldManager,
		CacheDir:               cacheDir,
		Log:                    ctrl.Log.WithName("controllers").WithName("GitApplication"),
		Ready:                  ready,
	})
	if err != nil {
		logger.Error(err, "unable to initialize operator")
		os.Exit(1)
	}

	logger.Info("starting operator")
	if leaderElect {
		if err := runWithLeaderElection(ctx, cfg, runner.run, logger); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error(err, "problem running leader election")
			os.Exit(1)
		}
		return
	}

	if err := runner.run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error(err, "problem running operator")
		os.Exit(1)
	}
}

type operatorOptions struct {
	WatchNamespace         string
	ConcurrentReconciles   int
	MinimumRequeueInterval time.Duration
	AllowClusterScoped     bool
	AllowedRepoPrefixes    []string
	FieldManager           string
	CacheDir               string
	Log                    logr.Logger
	Ready                  *atomic.Bool
}

type operatorRunner struct {
	reconciler *controller.GitApplicationReconciler
	informer   toolscache.SharedIndexInformer
	queue      workqueue.TypedRateLimitingInterface[string]
	workers    int
	log        logr.Logger
	ready      *atomic.Bool
}

func newOperatorRunner(cfg *rest.Config, opts operatorOptions) (*operatorRunner, error) {
	scheme := runtimeScheme()
	apiClient, err := crclient.New(cfg, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("create api client: %w", err)
	}

	dynClient := dynamic.NewForConfigOrDie(cfg)
	kubeClient := kubernetes.NewForConfigOrDie(cfg)
	discoClient := discovery.NewDiscoveryClientForConfigOrDie(cfg)
	cachedDiscovery := memory.NewMemCacheClient(discoClient)
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(cachedDiscovery)

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(
		dynClient,
		0,
		watchNamespaceOrAll(opts.WatchNamespace),
		nil,
	)
	informer := factory.ForResource(gitApplicationGVR).Informer()
	queue := workqueue.NewTypedRateLimitingQueueWithConfig(
		workqueue.DefaultTypedControllerRateLimiter[string](),
		workqueue.TypedRateLimitingQueueConfig[string]{Name: "gitapplications"},
	)

	informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			enqueueObject(queue, obj)
		},
		UpdateFunc: func(_, newObj any) {
			enqueueObject(queue, newObj)
		},
		DeleteFunc: func(obj any) {
			enqueueObject(queue, obj)
		},
	})

	reconciler := &controller.GitApplicationReconciler{
		Client:    apiClient,
		Scheme:    scheme,
		Dynamic:   dynClient,
		Discovery: cachedDiscovery,
		Mapper:    mapper,
		Repo:      git.Repository{CacheDir: opts.CacheDir},
		Log:       opts.Log,
		Kube:      kubeClient,
		Config: controller.OperatorConfig{
			WatchNamespace:              opts.WatchNamespace,
			ConcurrentReconciles:        opts.ConcurrentReconciles,
			MinimumRequeueInterval:      opts.MinimumRequeueInterval,
			AllowClusterScopedResources: opts.AllowClusterScoped,
			AllowedRepositoryPrefixes:   opts.AllowedRepoPrefixes,
			FieldManager:                opts.FieldManager,
			CacheDir:                    opts.CacheDir,
		},
	}

	workers := opts.ConcurrentReconciles
	if workers <= 0 {
		workers = 1
	}

	return &operatorRunner{
		reconciler: reconciler,
		informer:   informer,
		queue:      queue,
		workers:    workers,
		log:        opts.Log,
		ready:      opts.Ready,
	}, nil
}

func (r *operatorRunner) run(ctx context.Context) error {
	r.ready.Store(false)
	defer r.ready.Store(false)
	defer r.queue.ShutDown()

	go r.informer.Run(ctx.Done())
	if !toolscache.WaitForCacheSync(ctx.Done(), r.informer.HasSynced) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("gitapplication informer cache did not sync")
	}

	r.ready.Store(true)

	var workers sync.WaitGroup
	workers.Add(r.workers)
	for i := 0; i < r.workers; i++ {
		go func() {
			defer workers.Done()
			r.runWorker(ctx)
		}()
	}

	<-ctx.Done()
	workers.Wait()
	return context.Cause(ctx)
}

func (r *operatorRunner) runWorker(ctx context.Context) {
	for {
		key, shutdown := r.queue.Get()
		if shutdown {
			return
		}

		func() {
			defer r.queue.Done(key)

			namespace, name, err := toolscache.SplitMetaNamespaceKey(key)
			if err != nil {
				r.queue.Forget(key)
				r.log.Error(err, "invalid queue key", "key", key)
				return
			}

			result, err := r.reconciler.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: namespace,
					Name:      name,
				},
			})
			if err != nil {
				r.log.Error(err, "reconciliation failed", "namespace", namespace, "name", name)
				r.queue.AddRateLimited(key)
				return
			}

			r.queue.Forget(key)
			switch {
			case result.RequeueAfter > 0:
				r.queue.AddAfter(key, result.RequeueAfter)
			case result.Requeue:
				r.queue.AddRateLimited(key)
			}
		}()

		if ctx.Err() != nil {
			return
		}
	}
}

func enqueueObject(queue workqueue.TypedRateLimitingInterface[string], obj any) {
	key, err := toolscache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		return
	}
	queue.Add(key)
}

func runWithLeaderElection(ctx context.Context, cfg *rest.Config, run func(context.Context) error, log logr.Logger) error {
	leaderNamespace, err := leaderElectionNamespace()
	if err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("read hostname: %w", err)
	}

	lock, err := resourcelock.NewFromKubeconfig(
		resourcelock.LeasesResourceLock,
		leaderNamespace,
		"cabure.gitops.cabure.io",
		resourcelock.ResourceLockConfig{Identity: hostname},
		cfg,
		10*time.Second,
	)
	if err != nil {
		return fmt.Errorf("create leader election lock: %w", err)
	}

	errCh := make(chan error, 1)
	go leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
		Lock:            lock,
		Name:            "cabure.gitops.cabure.io",
		LeaseDuration:   15 * time.Second,
		RenewDeadline:   10 * time.Second,
		RetryPeriod:     2 * time.Second,
		ReleaseOnCancel: true,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				if err := run(leaderCtx); err != nil && !errors.Is(err, context.Canceled) {
					select {
					case errCh <- err:
					default:
					}
				}
			},
			OnStoppedLeading: func() {
				if ctx.Err() == nil {
					select {
					case errCh <- fmt.Errorf("leader election lost"):
					default:
					}
				}
			},
			OnNewLeader: func(identity string) {
				if identity != hostname {
					log.Info("observed new leader", "identity", identity)
				}
			},
		},
	})

	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case err := <-errCh:
		return err
	}
}

func leaderElectionNamespace() (string, error) {
	if namespace := strings.TrimSpace(os.Getenv("POD_NAMESPACE")); namespace != "" {
		return namespace, nil
	}

	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		namespace := strings.TrimSpace(string(data))
		if namespace != "" {
			return namespace, nil
		}
	}

	return "", fmt.Errorf("unable to determine leader election namespace")
}

type httpServer struct {
	server *http.Server
	log    logr.Logger
}

func newMetricsServer(addr string) *httpServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return &httpServer{
		server: &http.Server{Addr: addr, Handler: mux},
		log:    ctrl.Log.WithName("metrics"),
	}
}

func newProbeServer(addr string, ready *atomic.Bool) *httpServer {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return &httpServer{
		server: &http.Server{Addr: addr, Handler: mux},
		log:    ctrl.Log.WithName("probes"),
	}
}

func (s *httpServer) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}()

	go func() {
		if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.log.Error(err, "http server exited")
			_ = syscall.Kill(syscall.Getpid(), syscall.SIGTERM)
		}
	}()

	return nil
}

func watchNamespaceOrAll(namespace string) string {
	if strings.TrimSpace(namespace) == "" {
		return metav1.NamespaceAll
	}
	return namespace
}

func splitAllowedPrefixes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	prefixes := strings.Split(raw, ",")
	cleaned := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		prefix = strings.TrimSpace(prefix)
		if prefix != "" {
			cleaned = append(cleaned, prefix)
		}
	}
	return cleaned
}

func runtimeScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(scheme)
	return scheme
}
