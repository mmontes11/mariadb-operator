package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/go-multierror"
	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	"github.com/mariadb-operator/mariadb-operator/pkg/builder"
	condition "github.com/mariadb-operator/mariadb-operator/pkg/condition"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/batch"
	"github.com/mariadb-operator/mariadb-operator/pkg/refresolver"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RestoreReconciler reconciles a restore object
type RestoreReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Builder           *builder.Builder
	RefResolver       *refresolver.RefResolver
	ConditionComplete *condition.Complete
	BatchReconciler   *batch.BatchReconciler
}

//+kubebuilder:rbac:groups=mariadb.mmontes.io,resources=restores,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mariadb.mmontes.io,resources=restores/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mariadb.mmontes.io,resources=restores/finalizers,verbs=update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=list;watch;create;patch
//+kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=list;watch;create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *RestoreReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var restore mariadbv1alpha1.Restore
	if err := r.Get(ctx, req.NamespacedName, &restore); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	mariaDb, err := r.RefResolver.MariaDB(ctx, &restore.Spec.MariaDBRef, restore.Namespace)

	if mariaDb.Spec.BootstrapFrom != nil && *mariaDb.Spec.BootstrapFrom.Type == "mariabackup" {
		// skip restore reconciler for the mariadbbackup restore
		return ctrl.Result{}, nil
	}

	if err != nil {
		var mariaDbErr *multierror.Error
		mariaDbErr = multierror.Append(mariaDbErr, err)

		err = r.patchStatus(ctx, &restore, r.ConditionComplete.PatcherRefResolver(err, mariaDb))
		mariaDbErr = multierror.Append(mariaDbErr, err)

		return ctrl.Result{}, fmt.Errorf("error getting MariaDB: %v", mariaDbErr)
	}

	// We cannot check if mariaDb.IsReady() here and update the status accordingly
	// because we would be creating a deadlock when bootstrapping from backup
	// TODO: add a IsBootstrapping() method to MariaDB?

	if err := r.setDefaults(ctx, &restore); err != nil {
		var sourceErr *multierror.Error
		sourceErr = multierror.Append(sourceErr, err)

		patchErr := r.patchStatus(
			ctx,
			&restore,
			r.ConditionComplete.PatcherFailed(fmt.Sprintf("error initializing source: %v", err)),
		)
		sourceErr = multierror.Append(sourceErr, patchErr)

		return ctrl.Result{}, fmt.Errorf("error initializing source: %v", sourceErr)
	}

	var jobErr *multierror.Error
	err = r.BatchReconciler.Reconcile(ctx, &restore, mariaDb)
	jobErr = multierror.Append(jobErr, err)

	patcher, err := r.ConditionComplete.PatcherWithJob(ctx, err, req.NamespacedName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		return ctrl.Result{}, fmt.Errorf("error getting patcher for restore: %v", err)
	}

	err = r.patchStatus(ctx, &restore, patcher)
	jobErr = multierror.Append(jobErr, err)

	if err := jobErr.ErrorOrNil(); err != nil {
		return ctrl.Result{}, fmt.Errorf("error creating Job: %v", err)
	}
	return ctrl.Result{}, nil
}

func (r *RestoreReconciler) setDefaults(ctx context.Context, restore *mariadbv1alpha1.Restore) error {
	if err := r.patch(ctx, restore, func(r *mariadbv1alpha1.Restore) error {
		r.Spec.RestoreSource.SetDefaults()
		return nil
	}); err != nil {
		return fmt.Errorf("error patching restore: %v", err)
	}

	if restore.Spec.RestoreSource.IsDefaulted() {
		return nil
	}
	if restore.Spec.RestoreSource.BackupRef == nil {
		var restoreErr *multierror.Error
		restoreErr = multierror.Append(restoreErr, errors.New("unable to determine restore source, 'backupRef' is nil"))

		err := r.patchStatus(ctx, restore, r.ConditionComplete.PatcherFailed("Unable to determine restore source"))
		restoreErr = multierror.Append(restoreErr, err)

		return restoreErr
	}

	backup, err := r.RefResolver.Backup(ctx, restore.Spec.RestoreSource.BackupRef, restore.Namespace)
	if err != nil {
		var backupErr *multierror.Error
		backupErr = multierror.Append(backupErr, err)

		err = r.patchStatus(ctx, restore, r.ConditionComplete.PatcherRefResolver(err, backup))
		backupErr = multierror.Append(backupErr, err)

		return fmt.Errorf("error getting Backup: %v", backupErr)
	}

	if !backup.IsComplete() {
		var errBundle *multierror.Error
		errBundle = multierror.Append(errBundle, errors.New("Backup not complete"))

		err := r.patchStatus(ctx, restore, r.ConditionComplete.PatcherFailed("Backup not complete"))
		errBundle = multierror.Append(errBundle, err)

		return errBundle
	}

	if err := r.patch(ctx, restore, func(r *mariadbv1alpha1.Restore) error {
		return r.Spec.RestoreSource.SetDefaultsWithBackup(backup)
	}); err != nil {
		return fmt.Errorf("error patching restore: %v", err)
	}
	return nil
}

func (r *RestoreReconciler) patchStatus(ctx context.Context, restore *mariadbv1alpha1.Restore,
	patcher condition.Patcher) error {
	patch := client.MergeFrom(restore.DeepCopy())
	patcher(&restore.Status)

	if err := r.Client.Status().Patch(ctx, restore, patch); err != nil {
		return fmt.Errorf("error patching restore status: %v", err)
	}
	return nil
}

func (r *RestoreReconciler) patch(ctx context.Context, restore *mariadbv1alpha1.Restore,
	patcher func(*mariadbv1alpha1.Restore) error) error {
	patch := client.MergeFrom(restore.DeepCopy())
	if err := patcher(restore); err != nil {
		return err
	}
	return r.Client.Patch(ctx, restore, patch)
}

// SetupWithManager sets up the controller with the Manager.
func (r *RestoreReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mariadbv1alpha1.Restore{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}
