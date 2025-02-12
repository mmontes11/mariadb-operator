package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	labels "github.com/mariadb-operator/mariadb-operator/pkg/builder/labels"
	condition "github.com/mariadb-operator/mariadb-operator/pkg/condition"
	"github.com/mariadb-operator/mariadb-operator/pkg/metadata"
	"github.com/mariadb-operator/mariadb-operator/pkg/pod"
	"github.com/mariadb-operator/mariadb-operator/pkg/predicate"
	"github.com/mariadb-operator/mariadb-operator/pkg/refresolver"
	sqlClient "github.com/mariadb-operator/mariadb-operator/pkg/sql"
	sqlClientSet "github.com/mariadb-operator/mariadb-operator/pkg/sqlset"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type StatefulSetGaleraReconciler struct {
	client.Client
	Recorder    record.EventRecorder
	RefResolver *refresolver.RefResolver
}

//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *StatefulSetGaleraReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, req.NamespacedName, &sts); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	mariadb, err := r.RefResolver.MariaDBFromAnnotation(ctx, sts.ObjectMeta)
	if err != nil {
		if errors.Is(err, refresolver.ErrMariaDBAnnotationNotFound) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Keep MariaDB Spec replicas in sync with StatefulSet replicas
	if mariadb.Spec.Replicas != *sts.Spec.Replicas {
		logger := log.FromContext(ctx).WithName("galera").WithName("replicas")
		logger.Info("Updating MariaDB Spec replicas to match StatefulSet replicas", "spec-replicas", mariadb.Spec.Replicas, "sts-replicas", *sts.Spec.Replicas)
		if err := r.patch(ctx, mariadb, func(mdb *mariadbv1alpha1.MariaDB) {
			mdb.Spec.Replicas = *sts.Spec.Replicas
		}); err != nil {
			return ctrl.Result{}, fmt.Errorf("error patching MariaDB: %v", err)
		}
	}

	if !shouldPerformClusterRecovery(mariadb) {
		return r.monitorResult(mariadb), nil
	}
	logger := log.FromContext(ctx).WithName("galera").WithName("health")
	logger.V(1).Info("Checking Galera cluster health")

	galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
	recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})

	clusterHealthyTimeout := ptr.Deref(recovery.ClusterHealthyTimeout, metav1.Duration{Duration: 30 * time.Second}).Duration
	healthyCtx, cancelHealthy := context.WithTimeout(ctx, clusterHealthyTimeout)
	defer cancelHealthy()

	healthy, err := r.pollUntilHealthyWithTimeout(healthyCtx, sts.ObjectMeta, logger)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error polling MariaDB health: %v", err)
	}
	if healthy {
		return r.monitorResult(mariadb), nil
	}
	logger.Info("Galera cluster is not healthy")
	r.Recorder.Event(mariadb, corev1.EventTypeWarning, mariadbv1alpha1.ReasonGaleraClusterNotHealthy, "Galera cluster is not healthy")

	if err := r.patchStatus(ctx, mariadb, func(status *mariadbv1alpha1.MariaDBStatus) {
		status.GaleraRecovery = nil
		condition.SetGaleraNotReady(status)
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("error patching MariaDB: %v", err)
	}
	return r.monitorResult(mariadb), nil
}

func (r *StatefulSetGaleraReconciler) pollUntilHealthyWithTimeout(ctx context.Context, stsObjMeta metav1.ObjectMeta,
	logger logr.Logger) (bool, error) {
	err := wait.PollUntilContextCancel(ctx, 1*time.Second, true, func(ctx context.Context) (bool, error) {
		return r.isHealthy(ctx, stsObjMeta, logger)
	})
	if err != nil {
		if wait.Interrupted(err) {
			return false, nil
		}
		return false, fmt.Errorf("error polling health: %v", err)
	}
	return true, nil
}

func (r *StatefulSetGaleraReconciler) isHealthy(ctx context.Context, stsObjMeta metav1.ObjectMeta, logger logr.Logger) (bool, error) {
	// Kubenetes requests should not determine whether MariaDB is healthy or not.
	// For example: control-plane down should not trigger a cluster recovery.
	// Thus, an independent context is used so the pollUntilHealthyWithTimeout logic is not affected.
	kubeCtx, kubeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer kubeCancel()

	mdb, err := r.RefResolver.MariaDBFromAnnotation(kubeCtx, stsObjMeta)
	if err != nil {
		return false, fmt.Errorf("error getting MariaDB: %v", err)
	}
	if !shouldPerformClusterRecovery(mdb) {
		return true, nil
	}

	key := types.NamespacedName{
		Name:      stsObjMeta.Name,
		Namespace: stsObjMeta.Namespace,
	}
	var sts appsv1.StatefulSet
	if r.Get(kubeCtx, key, &sts) == nil {
		if sts.Status.ReadyReplicas == 0 {
			logger.V(1).Info("No StatefulSet ready replicas. Galera cluster unhealthy")
			return false, nil
		}
	}

	clientSet := sqlClientSet.NewClientSet(mdb, r.RefResolver)
	defer clientSet.Close()

	// SQL requests to MariaDB should use the context passed by pollUntilHealthyWithTimeout and return the error unwrapped, as it is.
	// This way pollUntilHealthyWithTimeout is able to detect whether MariaDB is unhealthy via wait.Interrupted.
	client, err := r.readyClient(ctx, mdb, clientSet)
	if err != nil {
		logger.V(1).Info("Error getting ready client", "err", err)
		return false, err
	}
	size, err := client.GaleraClusterSize(ctx)
	if err != nil {
		logger.V(1).Info("Error getting Galera cluster size", "err", err)
		return false, err
	}

	galera := ptr.Deref(mdb.Spec.Galera, mariadbv1alpha1.Galera{})
	recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})
	clusterHasMinSize, err := recovery.HasMinClusterSize(size, mdb)
	if err != nil {
		return false, fmt.Errorf("error checking min cluster size: %v", err)
	}
	logger.V(1).Info("Galera cluster size", "size", size, "has-min-size", clusterHasMinSize)

	return clusterHasMinSize, nil
}

func (r *StatefulSetGaleraReconciler) readyClient(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB,
	clientSet *sqlClientSet.ClientSet) (*sqlClient.Client, error) {
	list := corev1.PodList{}
	listOpts := &client.ListOptions{
		LabelSelector: klabels.SelectorFromSet(
			labels.NewLabelsBuilder().
				WithMariaDBSelectorLabels(mariadb).
				Build(),
		),
		Namespace: mariadb.GetNamespace(),
	}
	if err := r.List(ctx, &list, listOpts); err != nil {
		return nil, fmt.Errorf("error listing Pods: %v", err)
	}

	for _, p := range list.Items {
		if !pod.PodReady(&p) {
			continue
		}
		index, err := statefulset.PodIndex(p.Name)
		if err != nil {
			return nil, fmt.Errorf("error getting Pod index: %v", err)
		}
		if client, err := clientSet.ClientForIndex(ctx, *index); err == nil {
			return client, nil
		}
	}
	return nil, errors.New("no Ready Pods were found")
}

func (r *StatefulSetGaleraReconciler) patch(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB,
	patcher func(*mariadbv1alpha1.MariaDB)) error {
	patch := client.MergeFrom(mariadb.DeepCopy())
	patcher(mariadb)
	return r.Patch(ctx, mariadb, patch)
}

func (r *StatefulSetGaleraReconciler) patchStatus(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB,
	patcher func(*mariadbv1alpha1.MariaDBStatus)) error {
	patch := client.MergeFrom(mariadb.DeepCopy())
	patcher(&mariadb.Status)
	return r.Status().Patch(ctx, mariadb, patch)
}

func (r *StatefulSetGaleraReconciler) monitorResult(mdb *mariadbv1alpha1.MariaDB) ctrl.Result {
	galera := ptr.Deref(mdb.Spec.Galera, mariadbv1alpha1.Galera{})
	recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})
	if !recovery.Enabled {
		return ctrl.Result{}
	}
	requeueAfter := ptr.Deref(recovery.ClusterMonitorInterval, metav1.Duration{Duration: 10 * time.Second}).Duration
	return ctrl.Result{RequeueAfter: requeueAfter}
}

func shouldPerformClusterRecovery(mdb *mariadbv1alpha1.MariaDB) bool {
	galera := ptr.Deref(mdb.Spec.Galera, mariadbv1alpha1.Galera{})
	recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})
	if !galera.Enabled || !recovery.Enabled {
		return false
	}
	if mdb.IsRestoringBackup() || mdb.IsResizingStorage() || !mdb.HasGaleraConfiguredCondition() || mdb.HasGaleraNotReadyCondition() || mdb.IsStopped() {
		return false
	}
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *StatefulSetGaleraReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.StatefulSet{}).
		WithEventFilter(
			predicate.PredicateWithAnnotations(
				[]string{
					metadata.MariadbAnnotation,
					metadata.GaleraAnnotation,
				},
			),
		).
		Complete(r)
}
