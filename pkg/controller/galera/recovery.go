package galera

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	labels "github.com/mariadb-operator/mariadb-operator/pkg/builder/labels"
	galeraclient "github.com/mariadb-operator/mariadb-operator/pkg/galera/client"
	mdbhttp "github.com/mariadb-operator/mariadb-operator/pkg/http"
	"github.com/mariadb-operator/mariadb-operator/pkg/sql"
	sqlClientSet "github.com/mariadb-operator/mariadb-operator/pkg/sqlset"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	"github.com/mariadb-operator/mariadb-operator/pkg/wait"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *GaleraReconciler) reconcileRecovery(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, logger logr.Logger) error {
	pods, err := r.getPods(ctx, mariadb)
	if err != nil {
		return fmt.Errorf("error getting Pods: %v", err)
	}
	agentClientSet, err := r.newAgentClientSet(mariadb, mdbhttp.WithTimeout(5*time.Second))
	if err != nil {
		return fmt.Errorf("error getting agent client: %v", err)
	}
	sqlClientSet := sqlClientSet.NewClientSet(mariadb, r.refResolver)
	defer sqlClientSet.Close()

	rs := newRecoveryStatus(mariadb)

	if rs.bootstrapTimeout(mariadb) {
		logger.Info("Galera cluster bootstrap timed out. Resetting recovery status")
		r.recorder.Event(mariadb, corev1.EventTypeWarning, mariadbv1alpha1.ReasonGaleraClusterBootstrapTimeout,
			"Galera cluster bootstrap timed out")

		if err := r.resetRecovery(ctx, mariadb, rs); err != nil {
			return fmt.Errorf("error resetting recovery: %v", err)
		}
	}

	clusterLogger := logger.WithName("cluster")
	podLogger := logger.WithName("pod")

	if !rs.isBootstrapping() {
		logger.Info("Recovering cluster")
		if err := r.recoverCluster(ctx, mariadb, pods, rs, agentClientSet, clusterLogger); err != nil {
			return fmt.Errorf("error recovering cluster: %v", err)
		}
	}
	if !rs.podsRestarted() {
		logger.Info("Restarting Pods")
		if err := r.restartPods(ctx, mariadb, rs, agentClientSet, sqlClientSet, podLogger); err != nil {
			return fmt.Errorf("error restarting Pods: %v", err)
		}
	}
	return nil
}

func (r *GaleraReconciler) recoverCluster(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, pods []corev1.Pod,
	rs *recoveryStatus, clientSet *agentClientSet, logger logr.Logger) error {
	logger.V(1).Info("Get Galera state")
	var stateErr *multierror.Error
	err := r.getGaleraState(ctx, mariadb, pods, rs, clientSet, logger)
	stateErr = multierror.Append(stateErr, err)

	err = r.patchRecoveryStatus(ctx, mariadb, rs)
	stateErr = multierror.Append(stateErr, err)

	if err := stateErr.ErrorOrNil(); err != nil {
		return fmt.Errorf("error getting state: %v", err)
	}

	src, err := rs.bootstrapSource(pods, logger)
	if err != nil {
		logger.V(1).Info("Error getting bootstrap source", "err", err)
	}
	if src != nil {
		if err := r.bootstrap(ctx, src, rs, mariadb, clientSet, logger); err != nil {
			return fmt.Errorf("error bootstrapping: %v", err)
		}
		return r.patchRecoveryStatus(ctx, mariadb, rs)
	}

	logger.V(1).Info("Recover Galera state")
	var recoveryErr *multierror.Error
	err = r.recoverGaleraState(ctx, mariadb, pods, rs, clientSet, logger)
	recoveryErr = multierror.Append(recoveryErr, err)

	err = r.patchRecoveryStatus(ctx, mariadb, rs)
	recoveryErr = multierror.Append(recoveryErr, err)

	if err := recoveryErr.ErrorOrNil(); err != nil {
		return fmt.Errorf("error performing recovery: %v", err)
	}

	src, err = rs.bootstrapSource(pods, logger)
	if err != nil {
		return fmt.Errorf("error getting bootstrap source: %v", err)
	}
	if err := r.bootstrap(ctx, src, rs, mariadb, clientSet, logger); err != nil {
		return fmt.Errorf("error bootstrapping: %v", err)
	}
	if err := r.patchRecoveryStatus(ctx, mariadb, rs); err != nil {
		return fmt.Errorf("error patching recovery status: %v", err)
	}
	return nil
}

func (r *GaleraReconciler) restartPods(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, rs *recoveryStatus,
	agentClientSet *agentClientSet, sqlClientSet *sqlClientSet.ClientSet, logger logr.Logger) error {
	statusRecovery := ptr.Deref(mariadb.Status.GaleraRecovery, mariadbv1alpha1.GaleraRecoveryStatus{})
	bootstrap := ptr.Deref(statusRecovery.Bootstrap, mariadbv1alpha1.GaleraBootstrapStatus{})

	if bootstrap.Pod == nil {
		return errors.New("Unable to restart Pods. Cluster hasn't been bootstrapped")
	}
	index, err := statefulset.PodIndex(*bootstrap.Pod)
	if err != nil {
		return fmt.Errorf("error getting Pod index: %v", err)
	}
	client, err := agentClientSet.clientForIndex(*index)
	if err != nil {
		return fmt.Errorf("error getting agent client: %v", err)
	}

	galeraState, err := client.State.GetGaleraState(ctx)
	if err != nil {
		return fmt.Errorf("error getting Galera state: %v", err)
	}
	if !galeraState.SafeToBootstrap {
		logger.Info("Pod is no longer safe to bootstrap. Resetting recovery", "pod", *bootstrap.Pod)
		return r.resetRecovery(ctx, mariadb, rs)
	}

	bootstrapPodKey := types.NamespacedName{
		Name:      *bootstrap.Pod,
		Namespace: mariadb.Namespace,
	}
	podKeys := []types.NamespacedName{
		bootstrapPodKey,
	}
	for i := 0; i < int(mariadb.Spec.Replicas); i++ {
		name := statefulset.PodName(mariadb.ObjectMeta, i)
		if name == bootstrapPodKey.Name {
			continue
		}
		podKeys = append(podKeys, types.NamespacedName{
			Name:      name,
			Namespace: mariadb.Namespace,
		})
	}

	galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
	specRecovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})

	syncTimeout := ptr.Deref(specRecovery.PodSyncTimeout, metav1.Duration{Duration: 5 * time.Minute}).Duration
	syncContext, syncCancel := context.WithTimeout(ctx, syncTimeout)
	defer syncCancel()

	for _, key := range podKeys {
		if key.Name == bootstrapPodKey.Name {
			logger.Info("Restarting bootstrap Pod", "pod", key.Name)
		} else {
			logger.Info("Restarting Pod", "pod", key.Name)
		}

		if err := r.pollUntilPodDeleted(syncContext, key, logger); err != nil {
			return fmt.Errorf("error deleting Pod '%s': %v", key.Name, err)
		}
		if err := r.pollUntilPodSynced(syncContext, key, sqlClientSet, logger); err != nil {
			return fmt.Errorf("error waiting for Pod '%s' to be synced: %v", key.Name, err)
		}
	}

	rs.setPodsRestarted(true)
	if err := r.patchRecoveryStatus(ctx, mariadb, rs); err != nil {
		return fmt.Errorf("error patching recovery status: %v", err)
	}
	return nil
}

func (r *GaleraReconciler) getPods(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB) ([]corev1.Pod, error) {
	list := corev1.PodList{}
	listOpts := &ctrlclient.ListOptions{
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
	sort.Slice(list.Items, func(i, j int) bool {
		return list.Items[i].Name < list.Items[j].Name
	})
	return list.Items, nil
}

func (r *GaleraReconciler) getGaleraState(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, pods []corev1.Pod, rs *recoveryStatus,
	clientSet *agentClientSet, logger logr.Logger) error {
	g := new(errgroup.Group)
	g.SetLimit(len(pods))

	for _, pod := range pods {
		if _, ok := rs.state(pod.Name); ok {
			logger.V(1).Info("Skipping Pod state", "pod", pod.Name)
			continue
		}

		g.Go(func() error {
			i, err := statefulset.PodIndex(pod.Name)
			if err != nil {
				return fmt.Errorf("error getting index for Pod '%s': %v", pod.Name, err)
			}

			client, err := clientSet.clientForIndex(*i)
			if err != nil {
				return fmt.Errorf("error getting client for Pod '%s': %v", pod.Name, err)
			}

			galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
			recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})

			recoveryTimeout := ptr.Deref(recovery.PodRecoveryTimeout, metav1.Duration{Duration: 3 * time.Minute}).Duration
			recoveryCtx, cancelRecovery := context.WithTimeout(ctx, recoveryTimeout)
			defer cancelRecovery()

			err = wait.PollUntilSucessWithTimeout(recoveryCtx, logger, func(ctx context.Context) error {
				if err := r.ensurePodRunning(ctx, ctrlclient.ObjectKeyFromObject(&pod), logger); err != nil {
					return err
				}
				galeraState, err := client.State.GetGaleraState(ctx)
				if err != nil {
					return err
				}

				logger.Info("Galera state fetched in Pod", "pod", pod.Name)
				r.recorder.Eventf(mariadb, corev1.EventTypeNormal, mariadbv1alpha1.ReasonGaleraPodStateFetched,
					"Galera state fetched in Pod '%s'", pod.Name)
				rs.setState(pod.Name, galeraState)

				return nil
			})
			if err != nil {
				return fmt.Errorf("error getting Galera state for Pod '%s': %v", pod.Name, err)
			}
			return nil
		})
	}

	return g.Wait()
}

func (r *GaleraReconciler) recoverGaleraState(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, pods []corev1.Pod, rs *recoveryStatus,
	clientSet *agentClientSet, logger logr.Logger) error {
	g := new(errgroup.Group)
	g.SetLimit(len(pods))

	for _, pod := range pods {
		if _, ok := rs.recovered(pod.Name); ok {
			logger.V(1).Info("Skipping Pod recovery", "pod", pod.Name)
			continue
		}

		g.Go(func() error {
			i, err := statefulset.PodIndex(pod.Name)
			if err != nil {
				return fmt.Errorf("error getting index for Pod '%s': %v", pod.Name, err)
			}

			client, err := clientSet.clientForIndex(*i)
			if err != nil {
				return fmt.Errorf("error getting client for Pod '%s': %v", pod.Name, err)
			}

			galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
			recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})

			recoveryTimeout := ptr.Deref(recovery.PodRecoveryTimeout, metav1.Duration{Duration: 3 * time.Minute}).Duration
			recoveryCtx, cancelRecovery := context.WithTimeout(ctx, recoveryTimeout)
			defer cancelRecovery()

			logger.V(1).Info("Enabling recovery", "pod", pod.Name)
			if err = wait.PollUntilSucessWithTimeout(recoveryCtx, logger, func(ctx context.Context) error {
				if err := r.ensurePodRunning(ctx, ctrlclient.ObjectKeyFromObject(&pod), logger); err != nil {
					return err
				}
				return client.Recovery.Enable(ctx)
			}); err != nil {
				return fmt.Errorf("error enabling recovery in Pod '%s': %v", pod.Name, err)
			}

			logger.V(1).Info("Performing recovery", "pod", pod.Name)
			err = wait.PollUntilSucessWithTimeout(recoveryCtx, logger, func(ctx context.Context) error {
				if err := r.ensurePodRunning(ctx, ctrlclient.ObjectKeyFromObject(&pod), logger); err != nil {
					return err
				}
				bootstrap, err := client.Recovery.Start(ctx)
				if err != nil {
					return err
				}

				logger.Info("Recovered Galera sequence in Pod", "pod", pod.Name)
				r.recorder.Eventf(mariadb, corev1.EventTypeNormal, mariadbv1alpha1.ReasonGaleraPodRecovered,
					"Recovered Galera sequence in Pod '%s'", pod.Name)
				rs.setRecovered(pod.Name, bootstrap)
				return nil
			})
			if err != nil {
				return fmt.Errorf("error performing recovery in Pod '%s': %v", pod.Name, err)
			}

			logger.V(1).Info("Disabling recovery", "pod", pod.Name)
			err = wait.PollUntilSucessWithTimeout(recoveryCtx, logger, func(ctx context.Context) error {
				if err := r.ensurePodRunning(ctx, ctrlclient.ObjectKeyFromObject(&pod), logger); err != nil {
					return err
				}
				return client.Recovery.Disable(ctx)
			})
			if err != nil {
				return fmt.Errorf("error disabling recovery in Pod '%s': %v", pod.Name, err)
			}
			return nil
		})
	}

	return g.Wait()
}

func (r *GaleraReconciler) bootstrap(ctx context.Context, src *bootstrapSource, rs *recoveryStatus, mdb *mariadbv1alpha1.MariaDB,
	clientSet *agentClientSet, logger logr.Logger) error {
	logger.Info("Bootstrapping cluster", "pod", src.pod.Name)
	r.recorder.Eventf(mdb, corev1.EventTypeNormal, mariadbv1alpha1.ReasonGaleraClusterBootstrap,
		"Bootstrapping Galera cluster in Pod '%s'", src.pod.Name)

	idx, err := statefulset.PodIndex(src.pod.Name)
	if err != nil {
		return fmt.Errorf("error getting index for Pod '%s': %v", src.pod.Name, err)
	}
	client, err := clientSet.clientForIndex(*idx)
	if err != nil {
		return fmt.Errorf("error getting client for Pod '%s': %v", src.pod, err)
	}

	bootstrapCtx, cancelBootstrap := context.WithTimeout(ctx, 3*time.Minute)
	defer cancelBootstrap()

	if err = wait.PollUntilSucessWithTimeout(bootstrapCtx, logger, func(ctx context.Context) error {
		if err := r.ensurePodRunning(ctx, ctrlclient.ObjectKeyFromObject(src.pod), logger); err != nil {
			return err
		}
		return client.Bootstrap.Enable(ctx, src.bootstrap)
	}); err != nil {
		return fmt.Errorf("error enabling bootstrap in Pod '%s': %v", src.pod.Name, err)
	}

	rs.setBootstrapping(src.pod.Name)
	return nil
}

func (r *GaleraReconciler) ensurePodRunning(ctx context.Context, podKey types.NamespacedName, logger logr.Logger) error {
	var pod corev1.Pod
	if err := r.Get(ctx, podKey, &pod); err != nil {
		return fmt.Errorf("error getting Pod '%s': %v", podKey.Name, err)
	}
	if pod.Status.Phase == corev1.PodRunning {
		return nil
	}

	if err := r.Delete(ctx, &pod); err != nil {
		return fmt.Errorf("error deleting Pod '%s': %v", podKey.Name, err)
	}
	return r.pollUntilPodRunning(ctx, podKey, logger)
}

func (r *GaleraReconciler) pollUntilPodRunning(ctx context.Context, podKey types.NamespacedName, logger logr.Logger) error {
	return wait.PollUntilSucessWithTimeout(ctx, logger, func(ctx context.Context) error {
		var pod corev1.Pod
		if err := r.Get(ctx, podKey, &pod); err != nil {
			return fmt.Errorf("error getting Pod '%s': %v", podKey.Name, err)
		}
		if pod.Status.Phase == corev1.PodRunning {
			return nil
		}
		return errors.New("Pod not running")
	})
}

func (r *GaleraReconciler) pollUntilPodDeleted(ctx context.Context, podKey types.NamespacedName, logger logr.Logger) error {
	return wait.PollUntilSucessWithTimeout(ctx, logger, func(ctx context.Context) error {
		var pod corev1.Pod
		if err := r.Get(ctx, podKey, &pod); err != nil {
			return fmt.Errorf("error getting Pod '%s': %v", podKey.Name, err)
		}
		if err := r.Delete(ctx, &pod); err != nil {
			return fmt.Errorf("error deleting Pod '%s': %v", podKey.Name, err)
		}
		return nil
	})
}

func (r *GaleraReconciler) pollUntilPodSynced(ctx context.Context, podKey types.NamespacedName, sqlClientSet *sqlClientSet.ClientSet,
	logger logr.Logger) error {
	return wait.PollUntilSucessWithTimeout(ctx, logger, func(ctx context.Context) error {
		var pod corev1.Pod
		if err := r.Get(ctx, podKey, &pod); err != nil {
			return fmt.Errorf("error getting Pod '%s': %v", podKey.Name, err)
		}

		podIndex, err := statefulset.PodIndex(podKey.Name)
		if err != nil {
			return fmt.Errorf("error getting Pod index: %v", err)
		}
		sqlClient, err := sqlClientSet.ClientForIndex(ctx, *podIndex, sql.WithTimeout(5*time.Second))
		if err != nil {
			return fmt.Errorf("error getting SQL client: %v", err)
		}

		synced, err := galeraclient.IsPodSynced(ctx, sqlClient)
		if err != nil {
			return fmt.Errorf("error checking Pod sync: %v", err)
		}
		if !synced {
			return errors.New("Pod not synced")
		}
		return nil
	})
}

func (r *GaleraReconciler) resetRecovery(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, rs *recoveryStatus) error {
	rs.reset()
	return r.patchRecoveryStatus(ctx, mariadb, rs)
}

func (r *GaleraReconciler) patchRecoveryStatus(ctx context.Context, mdb *mariadbv1alpha1.MariaDB, rs *recoveryStatus) error {
	return r.patchStatus(ctx, mdb, func(mdbStatus *mariadbv1alpha1.MariaDBStatus) {
		galeraRecoveryStatus := rs.galeraRecoveryStatus()

		if reflect.ValueOf(galeraRecoveryStatus).IsZero() {
			mdbStatus.GaleraRecovery = nil
		} else {
			mdbStatus.GaleraRecovery = ptr.To(galeraRecoveryStatus)
		}
	})
}
