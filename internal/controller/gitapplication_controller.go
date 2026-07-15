package controller

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/GreedyKomodoDragon/cabure/api/v1alpha1"
	cabureapply "github.com/GreedyKomodoDragon/cabure/internal/apply"
	"github.com/GreedyKomodoDragon/cabure/internal/git"
	"github.com/GreedyKomodoDragon/cabure/internal/inventory"
	"github.com/GreedyKomodoDragon/cabure/internal/render"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
)

var sshSCPStyle = regexp.MustCompile(`^[^@[:space:]]+@[^:[:space:]]+:.+`)

type RepositoryCheckoutter interface {
	Checkout(ctx context.Context, repoURL, revision string, creds *git.Credentials) (string, string, error)
}

// +kubebuilder:rbac:groups=gitops.cabure.io,resources=gitapplications;gitapplications/status;gitapplications/finalizers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=,resources=secrets,verbs=get
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update;patch
// +kubebuilder:rbac:groups=*,resources=*,verbs=get;create;update;patch;delete
//
// The dynamic workload rule above is intentionally broad for v1 because the
// reconciler applies arbitrary Kubernetes objects from Git. The chart further
// scopes cluster-scoped access behind an explicit values flag.
type GitApplicationReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Dynamic   dynamic.Interface
	Discovery discovery.CachedDiscoveryInterface
	Mapper    meta.RESTMapper
	Repo      RepositoryCheckoutter
	Log       logr.Logger
	Config    OperatorConfig
	Kube      kubernetes.Interface
}

func (r *GitApplicationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var app v1alpha1.GitApplication
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !app.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	if app.Spec.Suspend {
		return r.updateSuspended(ctx, &app)
	}

	if err := validateSpec(&app, r.Config); err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "policy validation", err, true)
	}

	creds, stalled, err := r.loadCredentials(ctx, &app)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "source fetch", err, stalled)
	}

	checkoutDir, sha, err := r.Repo.Checkout(ctx, app.Spec.Source.Repository, app.Spec.Source.Revision, creds)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "checkout", err, false)
	}
	app.Status.AttemptedRevision = sha

	rendered, err := r.render(ctx, &app, checkoutDir, sha)
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "render", err, false)
	}

	normalized, desiredInventory, err := cabureapply.NormalizeAndValidate(ctx, r.Mapper, rendered, cabureapply.Policy{
		DestinationNamespace:      app.Spec.Destination.Namespace,
		ApplicationUID:            string(app.UID),
		SourceRevision:            sha,
		AllowClusterScoped:        r.Config.AllowClusterScopedResources,
		AllowedClusterScopedKinds: app.Spec.AllowedClusterScopedKinds,
		ForceApply:                app.Spec.TakeoverExistingResources,
		FieldManager:              fieldManagerOrDefault(r.Config.FieldManager),
	})
	if err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "policy validation", err, true)
	}

	if err := cabureapply.Apply(ctx, r.Dynamic, r.Mapper, normalized, fieldManagerOrDefault(r.Config.FieldManager), app.Spec.TakeoverExistingResources); err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "apply", err, false)
	}

	if app.Spec.Prune {
		if err := r.prune(ctx, &app, desiredInventory); err != nil {
			return ctrl.Result{}, r.fail(ctx, &app, "prune", err, false)
		}
	}

	if err := r.updateSuccess(ctx, &app, sha, desiredInventory); err != nil {
		return ctrl.Result{}, r.fail(ctx, &app, "status update", err, false)
	}

	interval := app.Spec.Interval.Duration
	if interval < r.Config.MinimumRequeueInterval {
		interval = r.Config.MinimumRequeueInterval
	}
	return ctrl.Result{RequeueAfter: interval}, nil
}

func (r *GitApplicationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.GitApplication{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.concurrentReconciles()}).
		Complete(r)
}

func (r *GitApplicationReconciler) concurrentReconciles() int {
	if r.Config.ConcurrentReconciles <= 0 {
		return 1
	}
	return r.Config.ConcurrentReconciles
}

func validateSpec(app *v1alpha1.GitApplication, cfg OperatorConfig) error {
	if app.Spec.Interval.Duration < minimumRequeueInterval(cfg) {
		return fmt.Errorf("interval must be at least %s", minimumRequeueInterval(cfg))
	}
	if app.Spec.Source.Repository == "" || !isSupportedRepositoryURL(app.Spec.Source.Repository) {
		return fmt.Errorf("repository must use https, ssh://, or scp-style ssh")
	}
	if len(cfg.AllowedRepositoryPrefixes) > 0 {
		allowed := false
		for _, prefix := range cfg.AllowedRepositoryPrefixes {
			if strings.HasPrefix(app.Spec.Source.Repository, prefix) {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("repository %q is not allowed", app.Spec.Source.Repository)
		}
	}
	if err := validateRelPath(app.Spec.Source.Path); err != nil {
		return fmt.Errorf("source.path: %w", err)
	}
	if app.Spec.Render.Type != "yaml" && app.Spec.Render.Type != "helm" {
		return fmt.Errorf("unsupported render type %q", app.Spec.Render.Type)
	}
	if app.Spec.Destination.Namespace == "" {
		return fmt.Errorf("destination.namespace is required")
	}
	if errs := validation.IsDNS1123Label(app.Spec.Destination.Namespace); len(errs) > 0 {
		return fmt.Errorf("destination.namespace: %s", strings.Join(errs, ", "))
	}
	if app.Spec.Render.Type == "helm" {
		if app.Spec.Render.Helm == nil {
			return fmt.Errorf("render.helm is required for helm rendering")
		}
		if app.Spec.Render.Helm.ReleaseName == "" {
			return fmt.Errorf("render.helm.releaseName is required")
		}
		for _, vf := range app.Spec.Render.Helm.ValuesFiles {
			if err := validateValuesFilePath(vf); err != nil {
				return fmt.Errorf("render.helm.valuesFiles: %w", err)
			}
		}
	}
	if err := validateAllowedClusterScopedKinds(app.Spec.AllowedClusterScopedKinds); err != nil {
		return err
	}
	return nil
}

func isSupportedRepositoryURL(repoURL string) bool {
	return strings.HasPrefix(repoURL, "https://") ||
		isSSHRepositoryURL(repoURL)
}

func isSSHRepositoryURL(repoURL string) bool {
	return strings.HasPrefix(repoURL, "ssh://") || sshSCPStyle.MatchString(repoURL)
}

func validateRelPath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	clean := filepath.Clean(path)
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(path) {
		return fmt.Errorf("path escapes checkout root")
	}
	return nil
}

func validateValuesFilePath(path string) error {
	if path == "" {
		return fmt.Errorf("path is required")
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("path must be relative to the repository root")
	}
	clean := filepath.Clean(path)
	if clean == "." {
		return fmt.Errorf("path must point to a file")
	}
	if clean != path {
		return fmt.Errorf("path must be a clean repository-relative path")
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path must stay within the repository root")
	}
	return nil
}

func validateAllowedClusterScopedKinds(kinds []string) error {
	for _, kind := range kinds {
		if !cabureapply.IsSupportedClusterScopedKind(kind) {
			return fmt.Errorf("allowedClusterScopedKinds: unsupported kind %q", kind)
		}
	}
	return nil
}

func fieldManagerOrDefault(value string) string {
	if value == "" {
		return cabureapply.DefaultFieldManager
	}
	return value
}

func minimumRequeueInterval(cfg OperatorConfig) time.Duration {
	if cfg.MinimumRequeueInterval <= 0 {
		return 15 * time.Second
	}
	return cfg.MinimumRequeueInterval
}

func (r *GitApplicationReconciler) render(ctx context.Context, app *v1alpha1.GitApplication, checkoutDir, sha string) ([]*unstructured.Unstructured, error) {
	switch app.Spec.Render.Type {
	case "yaml":
		return render.YAMLRenderer{}.Render(ctx, checkoutDir, app.Spec, sha)
	case "helm":
		return render.HelmRenderer{}.Render(ctx, checkoutDir, app.Spec, sha)
	default:
		return nil, fmt.Errorf("unsupported render type %q", app.Spec.Render.Type)
	}
}

func (r *GitApplicationReconciler) loadCredentials(ctx context.Context, app *v1alpha1.GitApplication) (*git.Credentials, bool, error) {
	if app.Spec.Source.SecretRef == nil || app.Spec.Source.SecretRef.Name == "" {
		return nil, false, nil
	}
	var secret corev1.Secret
	if r.Kube == nil {
		return nil, true, fmt.Errorf("secret client is not configured")
	}
	secretObj, err := r.Kube.CoreV1().Secrets(app.Namespace).Get(ctx, app.Spec.Source.SecretRef.Name, metav1.GetOptions{})
	if err != nil {
		return nil, apierrors.IsNotFound(err), fmt.Errorf("read credentials secret: %w", err)
	}
	secret = *secretObj
	if secret.Type == corev1.SecretTypeSSHAuth || len(secret.Data["ssh-privatekey"]) > 0 {
		privateKey := secret.Data["ssh-privatekey"]
		if len(privateKey) == 0 {
			return nil, true, fmt.Errorf("ssh secret %s/%s is missing ssh-privatekey", secret.Namespace, secret.Name)
		}
		knownHosts := secret.Data["known_hosts"]
		if len(knownHosts) == 0 {
			return nil, true, fmt.Errorf("ssh secret %s/%s is missing known_hosts", secret.Namespace, secret.Name)
		}
		return &git.Credentials{
			SSHPrivateKey: privateKey,
			KnownHosts:    knownHosts,
		}, false, nil
	}
	username := string(secret.Data["username"])
	password := string(secret.Data["password"])
	token := string(secret.Data["token"])
	if password == "" {
		password = token
	}
	if password == "" {
		return nil, true, fmt.Errorf("credentials secret %s/%s must include ssh-privatekey, password, or token", secret.Namespace, secret.Name)
	}
	return &git.Credentials{Username: username, Password: password, Token: token}, false, nil
}

func (r *GitApplicationReconciler) prune(ctx context.Context, app *v1alpha1.GitApplication, desired []v1alpha1.ResourceReference) error {
	stale := inventory.Diff(app.Status.Inventory, desired)
	for _, ref := range stale {
		obj, err := r.fetchLiveObject(ctx, ref)
		if err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return err
		}
		annotations := obj.GetAnnotations()
		labels := obj.GetLabels()
		if annotations[cabureapply.ApplicationUIDAnno] != string(app.UID) || labels[cabureapply.ManagedByLabel] != cabureapply.ManagedByValue {
			continue
		}
		if err := r.deleteObject(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}

func (r *GitApplicationReconciler) fetchLiveObject(ctx context.Context, ref v1alpha1.ResourceReference) (*unstructured.Unstructured, error) {
	mapping, err := r.Mapper.RESTMapping(schema.GroupKind{Group: ref.Group, Kind: ref.Kind}, ref.Version)
	if err != nil {
		return nil, err
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return r.Dynamic.Resource(mapping.Resource).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	}
	return r.Dynamic.Resource(mapping.Resource).Get(ctx, ref.Name, metav1.GetOptions{})
}

func (r *GitApplicationReconciler) deleteObject(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := r.Mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		return r.Dynamic.Resource(mapping.Resource).Namespace(obj.GetNamespace()).Delete(ctx, obj.GetName(), metav1.DeleteOptions{PropagationPolicy: ptr(metav1.DeletePropagationBackground)})
	}
	return r.Dynamic.Resource(mapping.Resource).Delete(ctx, obj.GetName(), metav1.DeleteOptions{PropagationPolicy: ptr(metav1.DeletePropagationBackground)})
}

func (r *GitApplicationReconciler) updateSuccess(ctx context.Context, app *v1alpha1.GitApplication, sha string, desired []v1alpha1.ResourceReference) error {
	now := metav1.Now()
	base := app.DeepCopy()
	app.Status.ObservedGeneration = app.Generation
	app.Status.AttemptedRevision = sha
	app.Status.AppliedRevision = sha
	app.Status.LastAttemptTime = &now
	app.Status.LastSuccessTime = &now
	app.Status.Inventory = inventory.Normalize(desired)
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Reconciling", Status: metav1.ConditionFalse, Reason: "Reconciled", Message: "reconciliation complete", ObservedGeneration: app.Generation, LastTransitionTime: now})
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "application is ready", ObservedGeneration: app.Generation, LastTransitionTime: now})
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Stalled", Status: metav1.ConditionFalse, Reason: "Reconciled", Message: "", ObservedGeneration: app.Generation, LastTransitionTime: now})
	return r.Status().Patch(ctx, app, client.MergeFrom(base))
}

func (r *GitApplicationReconciler) fail(ctx context.Context, app *v1alpha1.GitApplication, stage string, err error, stalled bool) error {
	now := metav1.Now()
	base := app.DeepCopy()
	app.Status.ObservedGeneration = app.Generation
	app.Status.LastAttemptTime = &now
	stalledStatus := metav1.ConditionFalse
	stalledReason := stageReason(stage)
	stalledMessage := ""
	if stalled {
		stalledStatus = metav1.ConditionTrue
		stalledMessage = stage + ": " + err.Error()
	}
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Stalled", Status: stalledStatus, Reason: stalledReason, Message: stalledMessage, ObservedGeneration: app.Generation, LastTransitionTime: now})
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Reconciling", Status: metav1.ConditionFalse, Reason: stageReason(stage), Message: stage + ": " + err.Error(), ObservedGeneration: app.Generation, LastTransitionTime: now})
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: stageReason(stage), Message: stage + ": " + err.Error(), ObservedGeneration: app.Generation, LastTransitionTime: now})
	if updateErr := r.Status().Patch(ctx, app, client.MergeFrom(base)); updateErr != nil {
		return fmt.Errorf("%s: %w", stage, updateErr)
	}
	return err
}

func (r *GitApplicationReconciler) updateSuspended(ctx context.Context, app *v1alpha1.GitApplication) (ctrl.Result, error) {
	now := metav1.Now()
	base := app.DeepCopy()
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Reconciling", Status: metav1.ConditionFalse, Reason: "Suspended", Message: "reconciliation is suspended", ObservedGeneration: app.Generation, LastTransitionTime: now})
	setCondition(&app.Status.Conditions, metav1.Condition{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Suspended", Message: "reconciliation is suspended", ObservedGeneration: app.Generation, LastTransitionTime: now})
	app.Status.LastAttemptTime = &now
	if err := r.Status().Patch(ctx, app, client.MergeFrom(base)); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: minimumRequeueInterval(r.Config)}, nil
}

func setCondition(conditions *[]metav1.Condition, condition metav1.Condition) {
	if conditions == nil {
		return
	}
	for i := range *conditions {
		if (*conditions)[i].Type == condition.Type {
			(*conditions)[i] = condition
			return
		}
	}
	*conditions = append(*conditions, condition)
}

func stageReason(stage string) string {
	stage = strings.TrimSpace(stage)
	stage = strings.ReplaceAll(stage, " ", "")
	if stage == "" {
		return "Unknown"
	}
	return strings.ToUpper(stage[:1]) + stage[1:]
}

func ptr[T any](v T) *T { return &v }
