package conditions

import (
	"fmt"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	jobpkg "github.com/mariadb-operator/mariadb-operator/pkg/job"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func SetReadyHealthy(c Conditioner) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  mariadbv1alpha1.ConditionReasonHealthy,
		Message: "Healthy",
	})
}

func SetReadyUnhealthyWithError(c Conditioner, err error) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonHealthy,
		Message: err.Error(),
	})
}

func SetReadyCreatedWithMessage(c Conditioner, message string) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  mariadbv1alpha1.ConditionReasonCreated,
		Message: message,
	})
}

func SetReadyCreated(c Conditioner) {
	SetReadyCreatedWithMessage(c, "Created")
}

func SetReadyFailedWithMessage(c Conditioner, message string) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonFailed,
		Message: message,
	})
}

func SetReadyFailed(c Conditioner) {
	SetReadyFailedWithMessage(c, "Failed")
}

func SetReadyWithStatefulSet(c Conditioner, sts *appsv1.StatefulSet) {
	if sts.Status.Replicas == 0 || sts.Status.ReadyReplicas != sts.Status.Replicas {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  mariadbv1alpha1.ConditionReasonStatefulSetNotReady,
			Message: "Not ready",
		})
		return
	}
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  mariadbv1alpha1.ConditionReasonStatefulSetReady,
		Message: "Running",
	})
}

func SetReadyWithMariaDB(c Conditioner, sts *appsv1.StatefulSet, mdb *mariadbv1alpha1.MariaDB) {
	if mdb.IsGaleraEnabled() && mdb.IsGaleraInitializing() {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  mariadbv1alpha1.ConditionReasonInitializing,
			Message: "Initializing",
		})
		return
	}
	if mdb.IsUpdating() {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  mariadbv1alpha1.ConditionReasonUpdating,
			Message: "Updating",
		})
		return
	}
	if sts.Status.Replicas == 0 || sts.Status.ReadyReplicas != sts.Status.Replicas {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  mariadbv1alpha1.ConditionReasonStatefulSetNotReady,
			Message: "Not ready",
		})
		return
	}

	if mdb.HasPendingUpdate() {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  mariadbv1alpha1.ConditionReasonPendingUpdate,
			Message: "Pending update",
		})
		return
	}
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  mariadbv1alpha1.ConditionReasonStatefulSetReady,
		Message: "Running",
	})
}

func SetReadyWithInitJob(c Conditioner, job *batchv1.Job) {
	if jobpkg.IsJobComplete(job) {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  mariadbv1alpha1.ConditionReasonInitializing,
			Message: "Initialized",
		})
	} else {
		c.SetCondition(metav1.Condition{
			Type:    mariadbv1alpha1.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  mariadbv1alpha1.ConditionReasonInitializing,
			Message: "Initializing",
		})
	}
}

func SetReadyWithMaxScaleStatus(c Conditioner, mss *mariadbv1alpha1.MaxScaleStatus) {
	for _, srv := range mss.Servers {
		if srv.IsReady() {
			continue
		}
		if srv.InMaintenance() {
			c.SetCondition(metav1.Condition{
				Type:    mariadbv1alpha1.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  mariadbv1alpha1.ConditionReasonMaxScaleNotReady,
				Message: fmt.Sprintf("Server %s in maintenance", srv.Name),
			})
		} else {
			c.SetCondition(metav1.Condition{
				Type:    mariadbv1alpha1.ConditionTypeReady,
				Status:  metav1.ConditionFalse,
				Reason:  mariadbv1alpha1.ConditionReasonMaxScaleNotReady,
				Message: fmt.Sprintf("Server %s not ready", srv.Name),
			})
		}
		return
	}

	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionTrue,
		Reason:  mariadbv1alpha1.ConditionReasonMaxScaleReady,
		Message: "Running",
	})
}

func SetReadyStorageResizing(c Conditioner) {
	msg := "Resizing storage"
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonResizingStorage,
		Message: msg,
	})
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeStorageResized,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonResizingStorage,
		Message: msg,
	})
}

func SetReadyWaitingStorageResize(c Conditioner) {
	msg := "Waiting for storage resize"
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonWaitStorageResize,
		Message: msg,
	})
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeStorageResized,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonWaitStorageResize,
		Message: msg,
	})
}

func SetReadyStorageResized(c Conditioner) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeStorageResized,
		Status:  metav1.ConditionTrue,
		Reason:  mariadbv1alpha1.ConditionReasonStorageResized,
		Message: "Storage resized",
	})
}

func SetReadySuspended(c Conditioner) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonSuspended,
		Message: "Suspended",
	})
}

func SetReadyStopped(c Conditioner) {
	c.SetCondition(metav1.Condition{
		Type:    mariadbv1alpha1.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  mariadbv1alpha1.ConditionReasonStopped,
		Message: "Stopped",
	})
}
