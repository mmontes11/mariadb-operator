package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/go-multierror"
	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	labels "github.com/mariadb-operator/mariadb-operator/pkg/builder/labels"
	condition "github.com/mariadb-operator/mariadb-operator/pkg/condition"
	conditions "github.com/mariadb-operator/mariadb-operator/pkg/condition"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/replication"
	podpkg "github.com/mariadb-operator/mariadb-operator/pkg/pod"
	stspkg "github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *MariaDBReconciler) reconcileStatus(ctx context.Context, mdb *mariadbv1alpha1.MariaDB) (ctrl.Result, error) {
	if mdb.IsSuspended() {
		return ctrl.Result{}, r.patchStatus(ctx, mdb, func(status *mariadbv1alpha1.MariaDBStatus) error {
			condition.SetReadySuspended(status)
			return nil
		})
	}
	logger := log.FromContext(ctx).WithName("status").V(1)

	if mdb.IsStopped() {
		return ctrl.Result{}, r.patchStatus(ctx, mdb, func(status *mariadbv1alpha1.MariaDBStatus) error {
			condition.SetReadyStopped(status)
			return nil
		})
	}

	var sts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKeyFromObject(mdb), &sts); err != nil {
		logger.Info("error getting StatefulSet", "err", err)
	}

	replicationStatus, replErr := r.getReplicationStatus(ctx, mdb)
	if replErr != nil {
		logger.Info("error getting replication status", "err", replErr)
	}

	mxsPrimaryPodIndex, mxsErr := r.getMaxScalePrimaryPod(ctx, mdb)
	if mxsErr != nil {
		logger.Info("error getting MaxScale primary Pod", "err", mxsErr)
	}

	tlsStatus, err := r.getTLSStatus(ctx, mdb)
	if err != nil {
		logger.Info("error getting TLS status", "err", err)
	}

	return ctrl.Result{}, r.patchStatus(ctx, mdb, func(status *mariadbv1alpha1.MariaDBStatus) error {
		status.DefaultVersion = r.Environment.MariadbDefaultVersion
		status.Replicas = sts.Status.ReadyReplicas
		defaultPrimary(mdb)
		setMaxScalePrimary(mdb, mxsPrimaryPodIndex)

		if replicationStatus != nil {
			status.ReplicationStatus = replicationStatus
		}

		if tlsStatus != nil {
			status.TLS = tlsStatus
		}

		if apierrors.IsNotFound(mxsErr) && !ptr.Deref(mdb.Spec.MaxScale, mariadbv1alpha1.MariaDBMaxScaleSpec{}).Enabled {
			r.ConditionReady.PatcherRefResolver(mxsErr, mariadbv1alpha1.MaxScale{})(&mdb.Status)
			return nil
		}
		if mdb.IsRestoringBackup() || mdb.IsResizingStorage() || mdb.IsSwitchingPrimary() || mdb.HasGaleraNotReadyCondition() {
			return nil
		}

		if err := r.setUpdatedCondition(ctx, mdb); err != nil {
			log.FromContext(ctx).V(1).Info("error setting MariaDB updated condition", "err", err)
		}
		condition.SetReadyWithMariaDB(&mdb.Status, &sts, mdb)
		return nil
	})
}

func (r *MariaDBReconciler) getReplicationStatus(ctx context.Context,
	mdb *mariadbv1alpha1.MariaDB) (mariadbv1alpha1.ReplicationStatus, error) {
	if !mdb.Replication().Enabled {
		return nil, nil
	}

	clientSet, err := replication.NewReplicationClientSet(mdb, r.RefResolver)
	if err != nil {
		return nil, fmt.Errorf("error creating mariadb clientset: %v", err)
	}
	defer clientSet.Close()

	replicationStatus := make(mariadbv1alpha1.ReplicationStatus)
	logger := log.FromContext(ctx)
	for i := 0; i < int(mdb.Spec.Replicas); i++ {
		pod := stspkg.PodName(mdb.ObjectMeta, i)

		client, err := clientSet.ClientForIndex(ctx, i)
		if err != nil {
			logger.V(1).Info("error getting client for Pod", "err", err, "pod", pod)
			continue
		}

		var aggErr *multierror.Error

		masterEnabled, err := client.IsSystemVariableEnabled(ctx, "rpl_semi_sync_master_enabled")
		aggErr = multierror.Append(aggErr, err)
		slaveEnabled, err := client.IsSystemVariableEnabled(ctx, "rpl_semi_sync_slave_enabled")
		aggErr = multierror.Append(aggErr, err)

		if err := aggErr.ErrorOrNil(); err != nil {
			logger.V(1).Info("error checking Pod replication state", "err", err, "pod", pod)
			continue
		}

		state := mariadbv1alpha1.ReplicationStateNotConfigured
		if masterEnabled {
			state = mariadbv1alpha1.ReplicationStateMaster
		} else if slaveEnabled {
			state = mariadbv1alpha1.ReplicationStateSlave
		}
		replicationStatus[pod] = state
	}
	return replicationStatus, nil
}

func (r *MariaDBReconciler) getMaxScalePrimaryPod(ctx context.Context, mdb *mariadbv1alpha1.MariaDB) (*int, error) {
	if !mdb.IsMaxScaleEnabled() {
		return nil, nil
	}
	mxs, err := r.RefResolver.MaxScale(ctx, mdb.Spec.MaxScaleRef, mdb.Namespace)
	if err != nil {
		return nil, err
	}
	primarySrv := mxs.Status.GetPrimaryServer()
	if primarySrv == nil {
		return nil, errors.New("MaxScale primary server not found")
	}
	podIndex, err := podIndexForServer(*primarySrv, mxs, mdb)
	if err != nil {
		return nil, fmt.Errorf("error getting Pod for MaxScale server '%s': %v", *primarySrv, err)
	}
	return podIndex, nil
}

func (r *MariaDBReconciler) setUpdatedCondition(ctx context.Context, mdb *mariadbv1alpha1.MariaDB) error {
	stsUpdateRevision, err := r.getStatefulSetRevision(ctx, mdb)
	if err != nil {
		return err
	}
	if stsUpdateRevision == "" {
		return nil
	}

	list := corev1.PodList{}
	listOpts := &client.ListOptions{
		LabelSelector: klabels.SelectorFromSet(
			labels.NewLabelsBuilder().
				WithMariaDBSelectorLabels(mdb).
				Build(),
		),
		Namespace: mdb.GetNamespace(),
	}
	if err := r.List(ctx, &list, listOpts); err != nil {
		return fmt.Errorf("error listing Pods: %v", err)
	}

	podsUpdated := 0
	for _, pod := range list.Items {
		if podpkg.PodUpdated(&pod, stsUpdateRevision) {
			podsUpdated++
		}
	}

	logger := log.FromContext(ctx)

	if podsUpdated >= int(mdb.Spec.Replicas) {
		logger.V(1).Info("MariaDB is up to date")
		condition.SetUpdated(&mdb.Status)
	} else if podsUpdated > 0 {
		logger.V(1).Info("MariaDB update in progress")
		conditions.SetUpdating(&mdb.Status)
	} else {
		logger.V(1).Info("MariaDB has a pending update")
		conditions.SetPendingUpdate(&mdb.Status)
	}
	return nil
}

func (r *MariaDBReconciler) getStatefulSetRevision(ctx context.Context, mdb *mariadbv1alpha1.MariaDB) (string, error) {
	var sts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKeyFromObject(mdb), &sts); err != nil {
		return "", err
	}
	return sts.Status.UpdateRevision, nil
}

func podIndexForServer(serverName string, mxs *mariadbv1alpha1.MaxScale, mdb *mariadbv1alpha1.MariaDB) (*int, error) {
	var server *mariadbv1alpha1.MaxScaleServer
	for _, srv := range mxs.Spec.Servers {
		if serverName == srv.Name {
			server = &srv
			break
		}
	}
	if server == nil {
		return nil, fmt.Errorf("MaxScale server '%s' not found", serverName)
	}

	for i := 0; i < int(mdb.Spec.Replicas); i++ {
		address := stspkg.PodFQDNWithService(mdb.ObjectMeta, i, mdb.InternalServiceKey().Name)
		if server.Address == address {
			return &i, nil
		}
	}
	return nil, fmt.Errorf("MariaDB Pod with address '%s' not found", server.Address)
}

func defaultPrimary(mdb *mariadbv1alpha1.MariaDB) {
	if mdb.Status.CurrentPrimaryPodIndex != nil || mdb.Status.CurrentPrimary != nil {
		return
	}
	podIndex := 0
	if mdb.IsGaleraEnabled() {
		galera := ptr.Deref(mdb.Spec.Galera, mariadbv1alpha1.Galera{})
		podIndex = ptr.Deref(galera.Primary.PodIndex, 0)
	}
	if mdb.Replication().Enabled {
		primaryReplication := ptr.Deref(mdb.Replication().Primary, mariadbv1alpha1.PrimaryReplication{})
		podIndex = ptr.Deref(primaryReplication.PodIndex, 0)
	}
	mdb.Status.CurrentPrimaryPodIndex = &podIndex
	mdb.Status.CurrentPrimary = ptr.To(stspkg.PodName(mdb.ObjectMeta, podIndex))
}

func setMaxScalePrimary(mdb *mariadbv1alpha1.MariaDB, podIndex *int) {
	if !mdb.IsMaxScaleEnabled() || podIndex == nil {
		return
	}
	mdb.Status.CurrentPrimaryPodIndex = podIndex
	mdb.Status.CurrentPrimary = ptr.To(stspkg.PodName(mdb.ObjectMeta, *podIndex))
}
