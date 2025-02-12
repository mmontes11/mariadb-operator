package controller

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"text/template"
	"time"

	"github.com/hashicorp/go-multierror"
	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	"github.com/mariadb-operator/mariadb-operator/pkg/builder"
	labels "github.com/mariadb-operator/mariadb-operator/pkg/builder/labels"
	condition "github.com/mariadb-operator/mariadb-operator/pkg/condition"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/auth"
	certctrl "github.com/mariadb-operator/mariadb-operator/pkg/controller/certificate"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/configmap"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/deployment"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/endpoints"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/galera"
	galeraresources "github.com/mariadb-operator/mariadb-operator/pkg/controller/galera/resources"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/maxscale"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/rbac"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/replication"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/secret"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/service"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/servicemonitor"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/statefulset"
	"github.com/mariadb-operator/mariadb-operator/pkg/discovery"
	"github.com/mariadb-operator/mariadb-operator/pkg/environment"
	"github.com/mariadb-operator/mariadb-operator/pkg/health"
	kadapter "github.com/mariadb-operator/mariadb-operator/pkg/kubernetes/adapter"
	mdbpod "github.com/mariadb-operator/mariadb-operator/pkg/pod"
	"github.com/mariadb-operator/mariadb-operator/pkg/refresolver"
	sts "github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	ErrSkipReconciliationPhase = errors.New("skipping reconciliation phase")
)

// MariaDBReconciler reconciles a MariaDB object
type MariaDBReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder

	Builder        *builder.Builder
	RefResolver    *refresolver.RefResolver
	ConditionReady *condition.Ready
	Environment    *environment.OperatorEnv
	Discovery      *discovery.Discovery

	ConfigMapReconciler      *configmap.ConfigMapReconciler
	SecretReconciler         *secret.SecretReconciler
	StatefulSetReconciler    *statefulset.StatefulSetReconciler
	ServiceReconciler        *service.ServiceReconciler
	EndpointsReconciler      *endpoints.EndpointsReconciler
	RBACReconciler           *rbac.RBACReconciler
	AuthReconciler           *auth.AuthReconciler
	DeploymentReconciler     *deployment.DeploymentReconciler
	ServiceMonitorReconciler *servicemonitor.ServiceMonitorReconciler
	CertReconciler           *certctrl.CertReconciler

	ReplicationReconciler *replication.ReplicationReconciler
	GaleraReconciler      *galera.GaleraReconciler
	MaxScaleReconciler    *maxscale.MaxScaleReconciler
}

type reconcilePhaseMariaDB struct {
	Name      string
	Reconcile func(context.Context, *mariadbv1alpha1.MariaDB) (ctrl.Result, error)
}

type patcherMariaDB func(*mariadbv1alpha1.MariaDBStatus) error

//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=mariadbs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=mariadbs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=mariadbs/finalizers,verbs=update
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=maxscale;restores;connections;users;grants,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;patch;delete
//+kubebuilder:rbac:groups="",resources=services,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=create;patch;get;list;watch
//+kubebuilder:rbac:groups="",resources=endpoints/restricted,verbs=create;patch;get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;patch;delete
//+kubebuilder:rbac:groups="",resources=pods/log,verbs=get
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=list
//+kubebuilder:rbac:groups="",resources=events,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=list;watch;create;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=list;watch;create;patch;delete
//+kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings;clusterrolebindings,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
//+kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *MariaDBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var mariadb mariadbv1alpha1.MariaDB
	if err := r.Get(ctx, req.NamespacedName, &mariadb); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	phases := []reconcilePhaseMariaDB{
		{
			Name:      "Spec",
			Reconcile: r.setSpecDefaults,
		},
		{
			Name:      "Status",
			Reconcile: r.reconcileStatus,
		},
		{
			Name:      "Suspend",
			Reconcile: r.reconcileSuspend,
		},
		{
			Name:      "Secret",
			Reconcile: r.reconcileSecret,
		},
		{
			Name:      "ConfigMap",
			Reconcile: r.reconcileConfigMap,
		},
		{
			Name:      "TLS",
			Reconcile: r.reconcileTLS,
		},
		{
			Name:      "RBAC",
			Reconcile: r.reconcileRBAC,
		},
		{
			Name:      "Init",
			Reconcile: r.reconcileInit,
		},
		{
			Name:      "Storage",
			Reconcile: r.reconcileStorage,
		},
		{
			Name:      "StatefulSet",
			Reconcile: r.reconcileStatefulSet,
		},
		{
			Name:      "PodDisruptionBudget",
			Reconcile: r.reconcilePodDisruptionBudget,
		},
		{
			Name:      "Service",
			Reconcile: r.reconcileService,
		},
		{
			Name:      "Replication",
			Reconcile: r.ReplicationReconciler.Reconcile,
		},
		{
			Name:      "Labels",
			Reconcile: r.reconcilePodLabels,
		},
		{
			Name:      "Galera",
			Reconcile: r.GaleraReconciler.Reconcile,
		},
		{
			Name:      "Restore",
			Reconcile: r.reconcileRestore,
		},
		{
			Name:      "MaxScale",
			Reconcile: r.MaxScaleReconciler.Reconcile,
		},
		{
			Name:      "SQL",
			Reconcile: r.reconcileSQL,
		},
		{
			Name:      "Metrics",
			Reconcile: r.reconcileMetrics,
		},
		{
			Name:      "Connection",
			Reconcile: r.reconcileConnection,
		},
	}

	for _, p := range phases {
		result, err := p.Reconcile(ctx, &mariadb)
		if err != nil {
			if shouldSkipPhase(err) {
				continue
			}
			var errBundle *multierror.Error
			errBundle = multierror.Append(errBundle, err)

			msg := fmt.Sprintf("Error reconciling %s: %v", p.Name, err)
			patchErr := r.patchStatus(ctx, &mariadb, func(s *mariadbv1alpha1.MariaDBStatus) error {
				patcher := r.ConditionReady.PatcherFailed(msg)
				patcher(s)
				return nil
			})
			if !apierrors.IsNotFound(patchErr) {
				errBundle = multierror.Append(errBundle, patchErr)
			}

			if err := errBundle.ErrorOrNil(); err != nil {
				return ctrl.Result{}, fmt.Errorf("error reconciling %s: %v", p.Name, err)
			}
		}
		if !result.IsZero() {
			return result, err
		}
	}
	return requeueResult(ctx, &mariadb)
}

func shouldSkipPhase(err error) bool {
	if apierrors.IsNotFound(err) {
		return true
	}
	if errors.Is(err, ErrSkipReconciliationPhase) {
		return true
	}
	return false
}

func requeueResult(ctx context.Context, mdb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mdb.IsTLSEnabled() {
		log.FromContext(ctx).V(1).Info("Requeuing MariaDB")
		return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil // ensure certificates get renewed
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcileSecret(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	var secretKeyRefs []mariadbv1alpha1.GeneratedSecretKeyRef

	if !mariadb.IsRootPasswordEmpty() {
		secretKeyRefs = append(secretKeyRefs, mariadb.Spec.RootPasswordSecretKeyRef)
	}
	if mariadb.Spec.PasswordSecretKeyRef != nil {
		secretKeyRefs = append(secretKeyRefs, *mariadb.Spec.PasswordSecretKeyRef)
	}
	galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
	basicAuth := ptr.Deref(galera.Agent.BasicAuth, mariadbv1alpha1.BasicAuth{})
	if galera.Enabled && basicAuth.Enabled && !reflect.ValueOf(galera.Agent.BasicAuth.PasswordSecretKeyRef).IsZero() {
		secretKeyRefs = append(secretKeyRefs, galera.Agent.BasicAuth.PasswordSecretKeyRef)
	}

	for _, secretKeyRef := range secretKeyRefs {
		req := secret.PasswordRequest{
			Owner:    mariadb,
			Metadata: mariadb.Spec.InheritMetadata,
			Key: types.NamespacedName{
				Name:      secretKeyRef.Name,
				Namespace: mariadb.Namespace,
			},
			SecretKey: secretKeyRef.Key,
			Generate:  secretKeyRef.Generate,
		}
		_, err := r.SecretReconciler.ReconcilePassword(ctx, req)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcileConfigMap(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	defaultConfigMapKeyRef := mariadb.DefaultConfigMapKeyRef()
	config, err := defaultConfig(mariadb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting default config: %v", err)
	}

	req := configmap.ReconcileRequest{
		Metadata: mariadb.Spec.InheritMetadata,
		Owner:    mariadb,
		Key: types.NamespacedName{
			Name:      defaultConfigMapKeyRef.Name,
			Namespace: mariadb.Namespace,
		},
		Data: map[string]string{
			defaultConfigMapKeyRef.Key: config,
		},
	}
	if err := r.ConfigMapReconciler.Reconcile(ctx, &req); err != nil {
		return ctrl.Result{}, err
	}

	if mariadb.Spec.MyCnf != nil && mariadb.Spec.MyCnfConfigMapKeyRef != nil {
		configMapKeyRef := *mariadb.Spec.MyCnfConfigMapKeyRef
		req := configmap.ReconcileRequest{
			Metadata: mariadb.Spec.InheritMetadata,
			Owner:    mariadb,
			Key: types.NamespacedName{
				Name:      configMapKeyRef.Name,
				Namespace: mariadb.Namespace,
			},
			Data: map[string]string{
				configMapKeyRef.Key: *mariadb.Spec.MyCnf,
			},
		}
		if err := r.ConfigMapReconciler.Reconcile(ctx, &req); err != nil {
			return ctrl.Result{}, err
		}
	}

	if mariadb.Replication().Enabled && ptr.Deref(mariadb.Replication().ProbesEnabled, false) {
		configMapKeyRef := mariadb.ReplConfigMapKeyRef()
		if err := r.ReplicationReconciler.ReconcileProbeConfigMap(ctx, configMapKeyRef, mariadb); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcileRBAC(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	return ctrl.Result{}, r.RBACReconciler.ReconcileMariadbRBAC(ctx, mariadb)
}

func (r *MariaDBReconciler) reconcileInit(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mariadb.IsGaleraEnabled() {
		if result, err := r.GaleraReconciler.ReconcileInit(ctx, mariadb); !result.IsZero() || err != nil {
			return result, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcileStatefulSet(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	key := client.ObjectKeyFromObject(mariadb)
	updateAnnotations, err := r.getUpdateAnnotations(ctx, mariadb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting Pod annotations: %v", err)
	}

	desiredSts, err := r.Builder.BuildMariadbStatefulSet(mariadb, key, updateAnnotations)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error building StatefulSet: %v", err)
	}
	shouldUpdate := mariadb.Spec.UpdateStrategy.Type != mariadbv1alpha1.NeverUpdateType

	if err := r.StatefulSetReconciler.ReconcileWithUpdates(ctx, desiredSts, shouldUpdate); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling StatefulSet: %v", err)
	}

	if result, err := r.reconcileUpdates(ctx, mariadb); !result.IsZero() || err != nil {
		return result, err
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcilePodLabels(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mariadb.Status.CurrentPrimaryPodIndex == nil {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	podList := corev1.PodList{}
	listOpts := &client.ListOptions{
		LabelSelector: klabels.SelectorFromSet(
			labels.NewLabelsBuilder().
				WithMariaDBSelectorLabels(mariadb).
				Build(),
		),
		Namespace: mariadb.GetNamespace(),
	}
	if err := r.List(ctx, &podList, listOpts); err != nil {
		return ctrl.Result{}, fmt.Errorf("error listing Pods: %v", err)
	}

	for _, pod := range podList.Items {
		var role = "replica"

		if pod.Status.PodIP == "" || pod.Spec.NodeName == "" {
			continue
		}
		podIndex, err := sts.PodIndex(pod.Name)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error getting Pod '%s' index: %v", pod.Name, err)
		}

		if *podIndex == *mariadb.Status.CurrentPrimaryPodIndex {
			role = "primary"
		}

		p := client.MergeFrom(pod.DeepCopy())
		pod.Labels = labels.NewLabelsBuilder().WithLabels(pod.Labels).WithPodRole(role).Build()

		if mdbpod.PodReady(&pod) {
			if err := r.Patch(ctx, &pod, p); err != nil {
				if apierrors.IsConflict(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				if apierrors.IsNotFound(err) {
					return ctrl.Result{Requeue: true}, nil
				}
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcilePodDisruptionBudget(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mariadb.IsHAEnabled() && mariadb.Spec.PodDisruptionBudget == nil {
		return ctrl.Result{}, r.reconcileHighAvailabilityPDB(ctx, mariadb)
	}
	return ctrl.Result{}, r.reconcileDefaultPDB(ctx, mariadb)
}

func (r *MariaDBReconciler) reconcileService(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if err := r.reconcileInternalService(ctx, mariadb); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling internal Service: %v", err)
	}
	if err := r.reconcileDefaultService(ctx, mariadb); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling default Service: %v", err)
	}
	if mariadb.IsHAEnabled() {
		if result, err := r.reconcilePrimaryService(ctx, mariadb); !result.IsZero() || err != nil {
			return ctrl.Result{}, err
		}
		if result, err := r.reconcileSecondaryService(ctx, mariadb); !result.IsZero() || err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func shouldReconcileRestore(mdb *mariadbv1alpha1.MariaDB) bool {
	if mdb.IsUpdating() || mdb.IsResizingStorage() || mdb.IsSwitchingPrimary() || mdb.HasGaleraNotReadyCondition() {
		return false
	}
	if mdb.HasRestoredBackup() || mdb.Spec.BootstrapFrom == nil {
		return false
	}
	return true
}

func (r *MariaDBReconciler) reconcileRestore(ctx context.Context, mdb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if !shouldReconcileRestore(mdb) {
		return ctrl.Result{}, nil
	}
	if mdb.IsRestoringBackup() {
		var existingRestore mariadbv1alpha1.Restore
		if err := r.Get(ctx, mdb.RestoreKey(), &existingRestore); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.patchStatus(ctx, mdb, func(status *mariadbv1alpha1.MariaDBStatus) error {
			if existingRestore.IsComplete() {
				if err := r.Delete(ctx, &existingRestore); err != nil {
					return err
				}
				condition.SetRestoredBackup(status)
			} else {
				condition.SetRestoringBackup(status)
			}
			return nil
		})
	}

	healthy, err := health.IsStatefulSetHealthy(
		ctx,
		r.Client,
		client.ObjectKeyFromObject(mdb),
		health.WithDesiredReplicas(mdb.Spec.Replicas),
		health.WithPort(mdb.Spec.Port),
		health.WithEndpointPolicy(health.EndpointPolicyAll),
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error checking MariaDB health: %v", err)
	}
	if !healthy {
		return ctrl.Result{}, nil
	}

	var existingRestore mariadbv1alpha1.Restore
	if err := r.Get(ctx, mdb.RestoreKey(), &existingRestore); err == nil {
		return ctrl.Result{}, nil
	}

	if err := r.patchStatus(ctx, mdb, func(status *mariadbv1alpha1.MariaDBStatus) error {
		condition.SetRestoringBackup(status)
		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("error patching status: %v", err)
	}

	restore, err := r.Builder.BuildRestore(mdb, mdb.RestoreKey())
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error building restore: %v", err)
	}
	return ctrl.Result{}, r.Create(ctx, restore)
}

func (r *MariaDBReconciler) reconcileDefaultPDB(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) error {
	if mariadb.Spec.PodDisruptionBudget == nil {
		return nil
	}

	key := client.ObjectKeyFromObject(mariadb)
	var existingPDB policyv1.PodDisruptionBudget
	if err := r.Get(ctx, key, &existingPDB); err == nil {
		return nil
	}

	selectorLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			Build()
	opts := builder.PodDisruptionBudgetOpts{
		Metadata:       mariadb.Spec.InheritMetadata,
		Key:            key,
		MinAvailable:   mariadb.Spec.PodDisruptionBudget.MinAvailable,
		MaxUnavailable: mariadb.Spec.PodDisruptionBudget.MaxUnavailable,
		SelectorLabels: selectorLabels,
	}
	pdb, err := r.Builder.BuildPodDisruptionBudget(opts, mariadb)
	if err != nil {
		return fmt.Errorf("error building PodDisruptionBudget: %v", err)
	}
	return r.Create(ctx, pdb)
}

func (r *MariaDBReconciler) reconcileHighAvailabilityPDB(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) error {
	key := client.ObjectKeyFromObject(mariadb)
	var existingPDB policyv1.PodDisruptionBudget
	if err := r.Get(ctx, key, &existingPDB); err == nil {
		return nil
	}

	selectorLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			Build()
	minAvailable := intstr.FromString("50%")
	opts := builder.PodDisruptionBudgetOpts{
		Metadata:       mariadb.Spec.InheritMetadata,
		Key:            key,
		MinAvailable:   &minAvailable,
		SelectorLabels: selectorLabels,
	}
	pdb, err := r.Builder.BuildPodDisruptionBudget(opts, mariadb)
	if err != nil {
		return fmt.Errorf("error building PodDisruptionBudget: %v", err)
	}
	return r.Create(ctx, pdb)
}

func (r *MariaDBReconciler) reconcileDefaultService(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) error {
	key := client.ObjectKeyFromObject(mariadb)
	ports := []corev1.ServicePort{
		{
			Name: builder.MariadbPortName,
			Port: mariadb.Spec.Port,
		},
	}
	if mariadb.Spec.ServicePorts != nil {
		ports = append(ports, kadapter.ToKubernetesSlice(mariadb.Spec.ServicePorts)...)
	}
	selectorLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			Build()
	opts := builder.ServiceOpts{
		Ports:          ports,
		SelectorLabels: selectorLabels,
		ExtraMeta:      mariadb.Spec.InheritMetadata,
	}
	if mariadb.Spec.Service != nil {
		opts.ServiceTemplate = *mariadb.Spec.Service
	}

	desiredSvc, err := r.Builder.BuildService(key, mariadb, opts)
	if err != nil {
		return fmt.Errorf("error building Service: %v", err)
	}
	return r.ServiceReconciler.Reconcile(ctx, desiredSvc)
}

func (r *MariaDBReconciler) reconcileInternalService(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) error {
	key := mariadb.InternalServiceKey()
	ports := []corev1.ServicePort{
		{
			Name: builder.MariadbPortName,
			Port: mariadb.Spec.Port,
		},
	}
	if mariadb.Spec.ServicePorts != nil {
		ports = append(ports, kadapter.ToKubernetesSlice(mariadb.Spec.ServicePorts)...)
	}
	if mariadb.IsGaleraEnabled() {
		agent := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{}).Agent
		ports = append(ports, []corev1.ServicePort{
			{
				Name: galeraresources.GaleraClusterPortName,
				Port: galeraresources.GaleraClusterPort,
			},
			{
				Name: galeraresources.GaleraISTPortName,
				Port: galeraresources.GaleraISTPort,
			},
			{
				Name: galeraresources.GaleraSSTPortName,
				Port: galeraresources.GaleraSSTPort,
			},
			{
				Name: galeraresources.AgentPortName,
				Port: agent.Port,
			},
			{
				Name: galeraresources.AgentProbePortName,
				Port: agent.ProbePort,
			},
		}...)
	}
	selectorLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			Build()

	opts := builder.ServiceOpts{
		Ports:          ports,
		Headless:       true,
		SelectorLabels: selectorLabels,
		ExtraMeta:      mariadb.Spec.InheritMetadata,
	}
	desiredSvc, err := r.Builder.BuildService(key, mariadb, opts)
	if err != nil {
		return fmt.Errorf("error building internal Service: %v", err)
	}
	return r.ServiceReconciler.Reconcile(ctx, desiredSvc)
}

func (r *MariaDBReconciler) reconcilePrimaryService(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mariadb.Status.CurrentPrimaryPodIndex == nil && !mariadb.IsStopped() {
		log.FromContext(ctx).V(1).Info("'status.currentPrimaryPodIndex' must be set")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	key := mariadb.PrimaryServiceKey()
	ports := []corev1.ServicePort{
		{
			Name: builder.MariadbPortName,
			Port: mariadb.Spec.Port,
		},
	}
	if mariadb.Spec.ServicePorts != nil {
		ports = append(ports, kadapter.ToKubernetesSlice(mariadb.Spec.ServicePorts)...)
	}
	serviceLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			WithStatefulSetPod(mariadb.ObjectMeta, *mariadb.Status.CurrentPrimaryPodIndex).
			Build()
	opts := builder.ServiceOpts{
		Ports:          ports,
		SelectorLabels: serviceLabels,
		ExtraMeta:      mariadb.Spec.InheritMetadata,
	}
	if mariadb.Spec.PrimaryService != nil {
		opts.ServiceTemplate = *mariadb.Spec.PrimaryService
	}

	desiredSvc, err := r.Builder.BuildService(key, mariadb, opts)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error building Service: %v", err)
	}
	return ctrl.Result{}, r.ServiceReconciler.Reconcile(ctx, desiredSvc)
}

func (r *MariaDBReconciler) reconcileSecondaryService(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	key := mariadb.SecondaryServiceKey()
	ports := []corev1.ServicePort{
		{
			Name: builder.MariadbPortName,
			Port: mariadb.Spec.Port,
		},
	}
	if mariadb.Spec.ServicePorts != nil {
		ports = append(ports, kadapter.ToKubernetesSlice(mariadb.Spec.ServicePorts)...)
	}
	selectorLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			Build()
	opts := builder.ServiceOpts{
		ExcludeSelectorLabels: true,
		Ports:                 ports,
		SelectorLabels:        selectorLabels,
		ExtraMeta:             mariadb.Spec.InheritMetadata,
	}
	if mariadb.Spec.SecondaryService != nil {
		opts.ServiceTemplate = *mariadb.Spec.SecondaryService
	}

	desiredSvc, err := r.Builder.BuildService(key, mariadb, opts)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error building Service: %v", err)
	}
	if err := r.ServiceReconciler.Reconcile(ctx, desiredSvc); err != nil {
		return ctrl.Result{}, err
	}
	return r.EndpointsReconciler.Reconcile(ctx, mariadb.SecondaryServiceKey(), mariadb)
}

func (r *MariaDBReconciler) reconcileSQL(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if !mariadb.IsReady() && !mariadb.IsStopped() {
		log.FromContext(ctx).V(1).Info("MariaDB not ready. Requeuing SQL resources")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	if err := r.reconcileDatabase(ctx, mariadb); err != nil {
		return ctrl.Result{}, fmt.Errorf("error reconciling database: %v", err)
	}
	if result, err := r.reconcileUsers(ctx, mariadb); !result.IsZero() || err != nil {
		return result, err
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcileDatabase(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) error {
	if mariadb.Spec.Database == nil {
		return nil
	}
	var database mariadbv1alpha1.Database
	err := r.Get(ctx, mariadb.MariadbDatabaseKey(), &database)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("error getting database: %v", err)
	}

	opts := builder.DatabaseOpts{
		Name:     *mariadb.Spec.Database,
		Metadata: mariadb.Spec.InheritMetadata,
		MariaDBRef: mariadbv1alpha1.MariaDBRef{
			ObjectReference: mariadbv1alpha1.ObjectReference{
				Name:      mariadb.Name,
				Namespace: mariadb.Namespace,
			},
		},
	}
	db, err := r.Builder.BuildDatabase(mariadb.MariadbDatabaseKey(), mariadb, opts)
	if err != nil {
		return fmt.Errorf("error building database: %v", err)
	}
	return r.Create(ctx, db)
}

func (r *MariaDBReconciler) reconcileUsers(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	sysUserKey := mariadb.MariadbSysUserKey()
	sysGrantKey := mariadb.MariadbSysGrantKey()
	userOpts := builder.UserOpts{
		MariaDBRef: mariadbv1alpha1.MariaDBRef{
			ObjectReference: mariadbv1alpha1.ObjectReference{
				Name:      mariadb.Name,
				Namespace: mariadb.Namespace,
			},
		},
		Metadata:             mariadb.Spec.InheritMetadata,
		MaxUserConnections:   20,
		Name:                 "mariadb.sys",
		Host:                 "localhost",
		PasswordSecretKeyRef: nil,
		CleanupPolicy:        ptr.To(mariadbv1alpha1.CleanupPolicySkip),
	}
	grantOpts := auth.GrantOpts{
		Key: sysGrantKey,
		GrantOpts: builder.GrantOpts{
			MariaDBRef: mariadbv1alpha1.MariaDBRef{
				ObjectReference: mariadbv1alpha1.ObjectReference{
					Name:      mariadb.Name,
					Namespace: mariadb.Namespace,
				},
			},
			Metadata: mariadb.Spec.InheritMetadata,
			Privileges: []string{
				"SELECT",
				"UPDATE",
				"DELETE",
			},
			Database:      "mysql",
			Table:         "global_priv",
			Username:      "mariadb.sys",
			Host:          "localhost",
			CleanupPolicy: ptr.To(mariadbv1alpha1.CleanupPolicySkip),
		},
	}

	if result, err := r.AuthReconciler.ReconcileUserGrant(ctx, sysUserKey, mariadb, userOpts, grantOpts); !result.IsZero() || err != nil {
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("error reconciling mariadb.sys user auth: %v", err)
		}
		return result, err
	}

	var sysGrant mariadbv1alpha1.Grant
	if err := r.Get(ctx, sysGrantKey, &sysGrant); err != nil {
		return ctrl.Result{}, fmt.Errorf("error getting mariadb.sys Grant: %v", err)
	}
	if !sysGrant.IsReady() {
		log.FromContext(ctx).V(1).Info("mariadb.sys Grant not ready. Requeuing...")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}

	if mariadb.IsInitialUserEnabled() {
		userKey := mariadb.MariadbUserKey()
		user := builder.UserOpts{
			MariaDBRef: mariadbv1alpha1.MariaDBRef{
				ObjectReference: mariadbv1alpha1.ObjectReference{
					Name:      mariadb.Name,
					Namespace: mariadb.Namespace,
				},
			},
			Metadata:           mariadb.Spec.InheritMetadata,
			MaxUserConnections: 20,
			Name:               *mariadb.Spec.Username,
			Host:               "%",
		}
		if mariadb.Spec.PasswordPlugin != nil {
			user.PasswordPlugin = mariadb.Spec.PasswordPlugin
		} else if mariadb.Spec.PasswordHashSecretKeyRef != nil {
			user.PasswordHashSecretKeyRef = mariadb.Spec.PasswordHashSecretKeyRef
		} else if mariadb.Spec.PasswordSecretKeyRef != nil {
			user.PasswordSecretKeyRef = &mariadb.Spec.PasswordSecretKeyRef.SecretKeySelector
		}
		grant := auth.GrantOpts{
			Key: mariadb.MariadbGrantKey(),
			GrantOpts: builder.GrantOpts{
				MariaDBRef: mariadbv1alpha1.MariaDBRef{
					ObjectReference: mariadbv1alpha1.ObjectReference{
						Name:      mariadb.Name,
						Namespace: mariadb.Namespace,
					},
				},
				Metadata: mariadb.Spec.InheritMetadata,
				Privileges: []string{
					"ALL PRIVILEGES",
				},
				Database: *mariadb.Spec.Database,
				Table:    "*",
				Username: *mariadb.Spec.Username,
				Host:     "%",
			},
		}

		if result, err := r.AuthReconciler.ReconcileUserGrant(ctx, userKey, mariadb, user, grant); !result.IsZero() || err != nil {
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("error reconciling user auth: %v", err)
			}
			return result, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) setSpecDefaults(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	return ctrl.Result{}, r.patch(ctx, mariadb, func(mdb *mariadbv1alpha1.MariaDB) error {
		return mdb.SetDefaults(r.Environment)
	})
}

func (r *MariaDBReconciler) reconcileSuspend(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mariadb.IsSuspended() && !mariadb.IsStopped() {
		log.FromContext(ctx).V(1).Info("MariaDB is suspended. Skipping...")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	// Skip reconciliation if the MariaDB CR is being deleted
	if !mariadb.DeletionTimestamp.IsZero() {
		log.FromContext(ctx).Info("MariaDB is being deleted. Skipping...")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return ctrl.Result{}, nil
}

func (r *MariaDBReconciler) reconcileConnection(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if !mariadb.IsInitialUserEnabled() {
		return ctrl.Result{}, nil
	}
	if !mariadb.IsReady() && !mariadb.IsStopped() {
		return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
	}
	if mariadb.IsHAEnabled() {
		if mariadb.Spec.PrimaryConnection != nil {
			key := mariadb.PrimaryConnectioneKey()
			connTpl := mariadb.Spec.PrimaryConnection
			connTpl.ServiceName = ptr.To(mariadb.PrimaryServiceKey().Name)

			if err := r.reconcileConnectionTemplate(ctx, key, connTpl, mariadb); err != nil {
				return ctrl.Result{}, err
			}
		}
		if mariadb.Spec.SecondaryConnection != nil {
			key := mariadb.SecondaryConnectioneKey()
			connTpl := mariadb.Spec.SecondaryConnection
			connTpl.ServiceName = ptr.To(mariadb.SecondaryServiceKey().Name)

			if err := r.reconcileConnectionTemplate(ctx, key, connTpl, mariadb); err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, r.reconcileDefaultConnection(ctx, mariadb)
}

func (r *MariaDBReconciler) reconcileConnectionTemplate(ctx context.Context, key types.NamespacedName,
	connTpl *mariadbv1alpha1.ConnectionTemplate, mariadb *mariadbv1alpha1.MariaDB) error {
	var existingConn mariadbv1alpha1.Connection
	if err := r.Get(ctx, key, &existingConn); err == nil {
		return nil
	}

	if mariadb.Spec.Username == nil || mariadb.Spec.PasswordSecretKeyRef == nil {
		log.FromContext(ctx).Error(
			errors.New("unable to reconcile Connection"),
			"spec.user and spec.passwordSecretKeyRef must have been initialized",
			"user", mariadb.Spec.Username,
			"passwordKeyRef", mariadb.Spec.PasswordSecretKeyRef,
		)
		return nil
	}
	connOpts := builder.ConnectionOpts{
		Metadata:             mariadb.Spec.InheritMetadata,
		MariaDB:              mariadb,
		Key:                  key,
		Username:             *mariadb.Spec.Username,
		PasswordSecretKeyRef: &mariadb.Spec.PasswordSecretKeyRef.SecretKeySelector,
		Database:             mariadb.Spec.Database,
		Template:             connTpl,
	}
	conn, err := r.Builder.BuildConnection(connOpts, mariadb)
	if err != nil {
		return fmt.Errorf("error building Connection: %v", err)
	}
	return r.Create(ctx, conn)
}

func (r *MariaDBReconciler) reconcileDefaultConnection(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) error {
	if mariadb.Spec.Connection == nil {
		return nil
	}
	key := client.ObjectKeyFromObject(mariadb)
	var existingConn mariadbv1alpha1.Connection
	if err := r.Get(ctx, key, &existingConn); err == nil {
		return nil
	}

	if mariadb.Spec.Username == nil || mariadb.Spec.PasswordSecretKeyRef == nil {
		log.FromContext(ctx).Error(
			errors.New("unable to reconcile default Connection"),
			"spec.user and spec.passwordSecretKeyRef must have been initialized",
			"user", mariadb.Spec.Username,
			"passwordKeyRef", mariadb.Spec.PasswordSecretKeyRef,
		)
		return nil
	}
	connOpts := builder.ConnectionOpts{
		Metadata:             mariadb.Spec.InheritMetadata,
		MariaDB:              mariadb,
		Key:                  key,
		Username:             *mariadb.Spec.Username,
		PasswordSecretKeyRef: &mariadb.Spec.PasswordSecretKeyRef.SecretKeySelector,
		Database:             mariadb.Spec.Database,
		Template:             mariadb.Spec.Connection,
	}
	conn, err := r.Builder.BuildConnection(connOpts, mariadb)
	if err != nil {
		return fmt.Errorf("error building Connection: %v", err)
	}
	return r.Create(ctx, conn)
}

func (r *MariaDBReconciler) patchStatus(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB,
	patcher patcherMariaDB) error {
	patch := client.MergeFrom(mariadb.DeepCopy())
	if err := patcher(&mariadb.Status); err != nil {
		return err
	}
	return r.Status().Patch(ctx, mariadb, patch)
}

func (r *MariaDBReconciler) patch(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB,
	patcher func(*mariadbv1alpha1.MariaDB) error) error {
	patch := client.MergeFrom(mariadb.DeepCopy())
	if err := patcher(mariadb); err != nil {
		return err
	}
	return r.Patch(ctx, mariadb, patch)
}

// SetupWithManager sets up the controller with the Manager.
func (r *MariaDBReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, env *environment.OperatorEnv,
	opts controller.Options) error {
	builder := ctrl.NewControllerManagedBy(mgr).
		For(&mariadbv1alpha1.MariaDB{}).
		Owns(&mariadbv1alpha1.MaxScale{}).
		Owns(&mariadbv1alpha1.Connection{}).
		Owns(&mariadbv1alpha1.Restore{}).
		Owns(&mariadbv1alpha1.User{}).
		Owns(&mariadbv1alpha1.Grant{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.Event{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&appsv1.Deployment{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		WithOptions(opts)

	currentNamespaceOnly, err := env.CurrentNamespaceOnly()
	if err != nil {
		return fmt.Errorf("error checking operator watch scope: %v", err)
	}
	if !currentNamespaceOnly {
		builder = builder.Owns(&rbacv1.ClusterRoleBinding{})
	}

	if err := mariadbv1alpha1.IndexMariaDB(ctx, mgr, builder, r.Client); err != nil {
		return fmt.Errorf("error indexing MariaDB: %v", err)
	}

	return builder.Complete(r)
}

func defaultConfig(mariadb *mariadbv1alpha1.MariaDB) (string, error) {
	tpl := createTpl("0-default.cnf", `[mariadb]
skip-name-resolve
temp-pool
{{- with .TimeZone }}
default_time_zone = {{ . }}
{{- end }}
`)

	buf := new(bytes.Buffer)
	err := tpl.Execute(buf, struct {
		TimeZone *string
	}{
		TimeZone: mariadb.Spec.TimeZone,
	})
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

func createTpl(name, t string) *template.Template {
	return template.Must(template.New(name).Parse(t))
}
