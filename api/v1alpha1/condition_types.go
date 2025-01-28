package v1alpha1

const (
	ConditionTypeReady           string = "Ready"
	ConditionTypeBackupRestored  string = "BackupRestored"
	ConditionTypePrimarySwitched string = "PrimarySwitched"
	// ConditionTypeGaleraReady indicates that the cluster is healthy.
	ConditionTypeGaleraReady string = "GaleraReady"
	// ConditionTypeGaleraConfigured indicates that the cluster has been successfully configured.
	ConditionTypeGaleraConfigured string = "GaleraConfigured"
	// ConditionTypeGaleraInitialized indicates that the cluster has been successfully initialized.
	ConditionTypeGaleraInitialized string = "GaleraInitialized"
	ConditionTypeComplete          string = "Complete"
	// ConditionTypeStorageResized indicates that the storage has been successfully resized.
	ConditionTypeStorageResized string = "StorageResized"
	// ConditionTypeUpdated indicates that an update has been successfully completed.
	ConditionTypeUpdated string = "Updated"

	ConditionReasonStatefulSetNotReady string = "StatefulSetNotReady"
	ConditionReasonStatefulSetReady    string = "StatefulSetReady"
	ConditionReasonRestoreBackup       string = "RestoreBackup"
	ConditionReasonSwitchPrimary       string = "SwitchPrimary"
	ConditionReasonGaleraReady         string = "GaleraReady"
	ConditionReasonGaleraNotReady      string = "GaleraNotReady"
	ConditionReasonGaleraConfigured    string = "GaleraConfigured"
	ConditionReasonGaleraInitialized   string = "GaleraInitialized"
	ConditionReasonGaleraInitializing  string = "GaleraInitializing"
	ConditionReasonResizingStorage     string = "ResizingStorage"
	ConditionReasonWaitStorageResize   string = "WaitStorageResize"
	ConditionReasonStorageResized      string = "StorageResized"
	ConditionReasonInitializing        string = "Initializing"
	ConditionReasonInitialized         string = "Initialized"
	ConditionReasonPendingUpdate       string = "PendingUpdate"
	ConditionReasonUpdating            string = "Updating"
	ConditionReasonUpdated             string = "Updated"
	ConditionReasonSuspended           string = "Suspended"
	ConditionReasonStopped             string = "Stopped"

	ConditionReasonMaxScaleNotReady string = "MaxScaleNotReady"
	ConditionReasonMaxScaleReady    string = "MaxScaleReady"

	ConditionReasonRestoreNotComplete string = "RestoreNotComplete"
	ConditionReasonRestoreComplete    string = "RestoreComplete"

	ConditionReasonJobComplete  string = "JobComplete"
	ConditionReasonJobSuspended string = "JobSuspended"
	ConditionReasonJobFailed    string = "JobFailed"
	ConditionReasonJobRunning   string = "JobRunning"

	ConditionReasonCronJobScheduled string = "CronJobScheduled"
	ConditionReasonCronJobFailed    string = "CronJobScheduled"
	ConditionReasonCronJobRunning   string = "CronJobRunning"
	ConditionReasonCronJobSuccess   string = "CronJobSucess"

	ConditionReasonConnectionFailed string = "ConnectionFailed"

	ConditionReasonCreated string = "Created"
	ConditionReasonHealthy string = "Healthy"
	ConditionReasonFailed  string = "Failed"
)
