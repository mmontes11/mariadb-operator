package v1alpha1

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

// BackupStorage defines the final storage for backups.
type BackupStorage struct {
	// S3 defines the configuration to store backups in a S3 compatible storage.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	S3 *S3 `json:"s3,omitempty"`
	// PersistentVolumeClaim is a Kubernetes PVC specification.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PersistentVolumeClaim *PersistentVolumeClaimSpec `json:"persistentVolumeClaim,omitempty"`
	// Volume is a Kubernetes volume specification.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Volume *StorageVolumeSource `json:"volume,omitempty"`
}

func (b *BackupStorage) Validate() error {
	storageTypes := 0
	fields := reflect.ValueOf(b).Elem()
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		if !field.IsNil() {
			storageTypes++
		}
	}
	if storageTypes != 1 {
		return errors.New("exactly one storage type should be provided")
	}
	return nil
}

// CompressAlgorithm defines the compression algorithm for a Backup resource.
type CompressAlgorithm string

const (
	// No compression
	CompressNone CompressAlgorithm = "none"
	// Bzip2 compression. Good compression ratio, but slower compression/decompression speed compared to gzip.
	CompressBzip2 CompressAlgorithm = "bzip2"
	// Gzip compression. Good compression/decompression speed, but worse compression ratio compared to bzip2.
	CompressGzip CompressAlgorithm = "gzip"
)

func (c CompressAlgorithm) Validate() error {
	switch c {
	case CompressAlgorithm(""), CompressNone, CompressBzip2, CompressGzip:
		return nil
	default:
		return fmt.Errorf("invalid compression: %v, supported agorithms: [%v|%v|%v]", c, CompressNone, CompressBzip2, CompressGzip)
	}
}

// BackupSpec defines the desired state of Backup
type BackupSpec struct {
	// JobContainerTemplate defines templates to configure Container objects.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	JobContainerTemplate `json:",inline"`
	// JobPodTemplate defines templates to configure Pod objects.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	JobPodTemplate `json:",inline"`
	// CronJobTemplate defines parameters for configuring CronJob objects.
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	CronJobTemplate `json:",inline"`
	// MariaDBRef is a reference to a MariaDB object.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	MariaDBRef MariaDBRef `json:"mariaDbRef" webhook:"inmutable"`
	// Compression algorithm to be used in the Backup.
	// +optional
	// +kubebuilder:validation:Enum=none;bzip2;gzip
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Compression CompressAlgorithm `json:"compression,omitempty"`
	// StagingStorage defines the temporary storage used to keep external backups (i.e. S3) while they are being processed.
	// It defaults to an emptyDir volume, meaning that the backups will be temporarily stored in the node where the Backup Job is scheduled.
	// The staging area gets cleaned up after each backup is completed, consider this for sizing it appropriately.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch","urn:alm:descriptor:com.tectonic.ui:advanced"}
	StagingStorage *BackupStagingStorage `json:"stagingStorage,omitempty" webhook:"inmutable"`
	// Storage defines the final storage for backups.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Storage BackupStorage `json:"storage" webhook:"inmutable"`
	// Schedule defines when the Backup will be taken.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Schedule *Schedule `json:"schedule,omitempty"`
	// MaxRetention defines the retention policy for backups. Old backups will be cleaned up by the Backup Job.
	// It defaults to 30 days.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	MaxRetention metav1.Duration `json:"maxRetention,omitempty" webhook:"inmutableinit"`
	// Databases defines the logical databases to be backed up. If not provided, all databases are backed up.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Databases []string `json:"databases,omitempty"`
	// IgnoreGlobalPriv indicates to ignore the mysql.global_priv in backups.
	// If not provided, it will default to true when the referred MariaDB instance has Galera enabled and otherwise to false.
	// See: https://github.com/mariadb-operator/mariadb-operator/issues/556
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch","urn:alm:descriptor:com.tectonic.ui:advanced"}
	IgnoreGlobalPriv *bool `json:"ignoreGlobalPriv,omitempty"`
	// LogLevel to be used n the Backup Job. It defaults to 'info'.
	// +optional
	// +kubebuilder:default=info
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:advanced"}
	LogLevel string `json:"logLevel,omitempty"`
	// BackoffLimit defines the maximum number of attempts to successfully take a Backup.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number","urn:alm:descriptor:com.tectonic.ui:advanced"}
	BackoffLimit int32 `json:"backoffLimit,omitempty"`
	// RestartPolicy to be added to the Backup Pod.
	// +optional
	// +kubebuilder:default=OnFailure
	// +kubebuilder:validation:Enum=Always;OnFailure;Never
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:advanced"}
	RestartPolicy corev1.RestartPolicy `json:"restartPolicy,omitempty" webhook:"inmutable"`
	// InheritMetadata defines the metadata to be inherited by children resources.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:advanced"}
	InheritMetadata *Metadata `json:"inheritMetadata,omitempty"`
	// ActiveDeadlineSeconds defines the maximum amount of time in seconds that the Backup Job can run.
	// +optionsl
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number","urn:alm:descriptor:com.tectonic.ui:advanced"}
	ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty"`
}

// BackupStatus defines the observed state of Backup
type BackupStatus struct {
	// Conditions for the Backup object.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors={"urn:alm:descriptor:io.kubernetes.conditions"}
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (b *BackupStatus) SetCondition(condition metav1.Condition) {
	if b.Conditions == nil {
		b.Conditions = make([]metav1.Condition, 0)
	}
	meta.SetStatusCondition(&b.Conditions, condition)
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=bmdb
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Complete",type="string",JSONPath=".status.conditions[?(@.type==\"Complete\")].status"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Complete\")].message"
// +kubebuilder:printcolumn:name="MariaDB",type="string",JSONPath=".spec.mariaDbRef.name"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{Backup,v1alpha1},{CronJob,v1},{Job,v1},{PersistentVolumeClaim,v1},{ServiceAccount,v1}}

// Backup is the Schema for the backups API. It is used to define backup jobs and its storage.
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSpec   `json:"spec,omitempty"`
	Status BackupStatus `json:"status,omitempty"`
}

func (b *Backup) IsComplete() bool {
	return meta.IsStatusConditionTrue(b.Status.Conditions, ConditionTypeComplete)
}

func (b *Backup) Validate() error {
	if b.Spec.Schedule != nil {
		if err := b.Spec.Schedule.Validate(); err != nil {
			return fmt.Errorf("invalid Schedule: %v", err)
		}
	}
	if err := b.Spec.Storage.Validate(); err != nil {
		return fmt.Errorf("invalid Storage: %v", err)
	}
	if err := b.Spec.Compression.Validate(); err != nil {
		return fmt.Errorf("invalid Compression: %v", err)
	}
	if b.Spec.Storage.S3 == nil && b.Spec.StagingStorage != nil {
		return errors.New("'spec.stagingStorage' may only be specified when 'spec.storage.s3' is set")
	}
	return nil
}

func (b *Backup) SetDefaults(mariadb *MariaDB) {
	if b.Spec.Compression == CompressAlgorithm("") {
		b.Spec.Compression = CompressNone
	}
	if b.Spec.MaxRetention == (metav1.Duration{}) {
		b.Spec.MaxRetention = metav1.Duration{Duration: 30 * 24 * time.Hour}
	}
	if b.Spec.BackoffLimit == 0 {
		b.Spec.BackoffLimit = 5
	}
	if b.Spec.IgnoreGlobalPriv == nil {
		b.Spec.IgnoreGlobalPriv = ptr.To(ptr.Deref(mariadb.Spec.Galera, Galera{}).Enabled)
	}
	b.Spec.JobPodTemplate.SetDefaults(b.ObjectMeta, mariadb.ObjectMeta)
}

func (b *Backup) Volume() (StorageVolumeSource, error) {
	if b.Spec.Storage.S3 != nil {
		stagingStorage := ptr.Deref(b.Spec.StagingStorage, BackupStagingStorage{})
		return stagingStorage.VolumeOrEmptyDir(b.StagingPVCKey()), nil
	}
	if b.Spec.Storage.PersistentVolumeClaim != nil {
		return StorageVolumeSource{
			PersistentVolumeClaim: &PersistentVolumeClaimVolumeSource{
				ClaimName: b.StoragePVCKey().Name,
			},
		}, nil
	}
	if b.Spec.Storage.Volume != nil {
		return *b.Spec.Storage.Volume, nil
	}
	return StorageVolumeSource{}, errors.New("unable to get volume for Backup")
}

// +kubebuilder:object:root=true

// BackupList contains a list of Backup
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Backup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}
