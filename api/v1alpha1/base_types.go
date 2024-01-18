package v1alpha1

import (
	"errors"
	"fmt"
	"time"

	"github.com/mariadb-operator/mariadb-operator/pkg/webhook"
	cron "github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	inmutableWebhook = webhook.NewInmutableWebhook(
		webhook.WithTagName("webhook"),
	)
	cronParser = cron.NewParser(
		cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow,
	)
)

// MariaDBRef is a reference to a MariaDB object.
type MariaDBRef struct {
	// ObjectReference is a reference to a object.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	corev1.ObjectReference `json:",inline"`
	// WaitForIt indicates whether the controller using this reference should wait for MariaDB to be ready.
	// +optional
	// +kubebuilder:default=true
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	WaitForIt bool `json:"waitForIt"`
}

// SecretTemplate defines a template to customize Secret objects.
type SecretTemplate struct {
	// Labels to be added to the Secret object.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations to be added to the Secret object.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Annotations map[string]string `json:"annotations,omitempty"`
	// Key to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Key *string `json:"key,omitempty"`
	// Format to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Format *string `json:"format,omitempty"`
	// UsernameKey to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	UsernameKey *string `json:"usernameKey,omitempty"`
	// PasswordKey to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PasswordKey *string `json:"passwordKey,omitempty"`
	// HostKey to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	HostKey *string `json:"hostKey,omitempty"`
	// PortKey to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PortKey *string `json:"portKey,omitempty"`
	// DatabaseKey to be used in the Secret.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	DatabaseKey *string `json:"databaseKey,omitempty"`
}

// ContainerTemplate defines a template to configure Container objects.
type ContainerTemplate struct {
	// Command to be used in the Container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Command []string `json:"command,omitempty"`
	// Args to be used in the Container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Args []string `json:"args,omitempty"`
	// Env represents the environment variables to be injected in a container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Env []corev1.EnvVar `json:"env,omitempty"`
	// EnvFrom represents the references (via ConfigMap and Secrets) to environment variables to be injected in the container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`
	// VolumeMounts to be used in the Container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty" webhook:"inmutable"`
	// LivenessProbe to be used in the Container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	LivenessProbe *corev1.Probe `json:"livenessProbe,omitempty"`
	// ReadinessProbe to be used in the Container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	ReadinessProbe *corev1.Probe `json:"readinessProbe,omitempty"`
	// Resouces describes the compute resource requirements.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:resourceRequirements"}
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// SecurityContext holds security configuration that will be applied to a container.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`
}

// Container object definition.
type Container struct {
	// ContainerTemplate defines a template to configure Container objects.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	ContainerTemplate `json:",inline"`
	// Image name to be used by the MariaDB instances. The supported format is `<image>:<tag>`.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Image string `json:"image"`
	// ImagePullPolicy is the image pull policy. One of `Always`, `Never` or `IfNotPresent`. If not defined, it defaults to `IfNotPresent`.
	// +optional
	// +kubebuilder:validation:Enum=Always;Never;IfNotPresent
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"}
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`
}

// PodTemplate defines a template to configure Container objects.
type PodTemplate struct {
	// InitContainers to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	InitContainers []Container `json:"initContainers,omitempty"`
	// SidecarContainers to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SidecarContainers []Container `json:"sidecarContainers,omitempty"`
	// SecurityContext holds pod-level security attributes and common container settings.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
	// Affinity to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// NodeSelector to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Volumes to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Volumes []corev1.Volume `json:"volumes,omitempty" webhook:"inmutable"`
	// PriorityClassName to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	PriorityClassName *string `json:"priorityClassName,omitempty" webhook:"inmutable"`
	// TopologySpreadConstraints to be used in the Pod.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}

// VolumeClaimTemplate defines a template to customize PVC objects.
type VolumeClaimTemplate struct {
	// PersistentVolumeClaimSpec is the specification of a PVC.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	corev1.PersistentVolumeClaimSpec `json:",inline"`
	// Labels to be used in the PVC.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations to be used in the PVC.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Annotations map[string]string `json:"annotations,omitempty"`
}

// HealthCheck defines intervals for performing health checks.
type HealthCheck struct {
	// Interval used to perform health checks.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Interval *metav1.Duration `json:"interval,omitempty"`
	// RetryInterval is the intervañ used to perform health check retries.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	RetryInterval *metav1.Duration `json:"retryInterval,omitempty"`
}

// ConnectionTemplate defines a template to customize Connection objects.
type ConnectionTemplate struct {
	// SecretName to be used in the Connection.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SecretName *string `json:"secretName,omitempty" webhook:"inmutableinit"`
	// SecretTemplate to be used in the Connection.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SecretTemplate *SecretTemplate `json:"secretTemplate,omitempty" webhook:"inmutableinit"`
	// HealthCheck to be used in the Connection.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	HealthCheck *HealthCheck `json:"healthCheck,omitempty"`
	// Params to be used in the Connection.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Params map[string]string `json:"params,omitempty" webhook:"inmutable"`
	// ServiceName to be used in the Connection.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	ServiceName *string `json:"serviceName,omitempty" webhook:"inmutable"`
}

// SQLTemplate defines a template to customize SQL objects.
type SQLTemplate struct {
	// RequeueInterval is used to perform requeue reconcilizations.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	RequeueInterval *metav1.Duration `json:"requeueInterval,omitempty"`
	// RetryInterval is the interval used to perform retries.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	RetryInterval *metav1.Duration `json:"retryInterval,omitempty"`
}

type TLS struct {
	// Enabled is a flag to enable TLS.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch"}
	Enabled bool `json:"enabled"`
	// CASecretKeyRef is a reference to a Secret key containing a CA bundle in PEM format used to establish TLS connections with S3.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	CASecretKeyRef *corev1.SecretKeySelector `json:"caSecretKeyRef,omitempty"`
}

type S3 struct {
	// Bucket is the name Name of the bucket to store backups.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Bucket string `json:"bucket" webhook:"inmutable"`
	// Endpoint is the S3 API endpoint without scheme.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Endpoint string `json:"endpoint" webhook:"inmutable"`
	// Region is the S3 region name to use.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Region string `json:"region" webhook:"inmutable"`
	// AccessKeyIdSecretKeyRef is a reference to a Secret key containing the S3 access key id.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	AccessKeyIdSecretKeyRef corev1.SecretKeySelector `json:"accessKeyIdSecretKeyRef"`
	// AccessKeyIdSecretKeyRef is a reference to a Secret key containing the S3 secret key.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SecretAccessKeySecretKeyRef corev1.SecretKeySelector `json:"secretAccessKeySecretKeyRef"`
	// SessionTokenSecretKeyRef is a reference to a Secret key containing the S3 session token.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	SessionTokenSecretKeyRef *corev1.SecretKeySelector `json:"sessionTokenSecretKeyRef,omitempty"`
	// TLS provides the configuration required to establish TLS connections with S3.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	TLS *TLS `json:"tls,omitempty"`
	// Prefix allows backups to be placed under a specific prefix in the bucket.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Prefix string `json:"prefix"`
}

// RestoreSource defines a source for restoring a MariaDB.
type RestoreSource struct {
	// BackupRef is a reference to a Backup object. It has priority over S3 and Volume.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	BackupRef *corev1.LocalObjectReference `json:"backupRef,omitempty" webhook:"inmutableinit"`
	// S3 defines the configuration to restore backups from a S3 compatible storage. It has priority over Volume.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	S3 *S3 `json:"s3,omitempty" webhook:"inmutableinit"`
	// Volume is a Kubernetes Volume object that contains a backup.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Volume *corev1.VolumeSource `json:"volume,omitempty" webhook:"inmutableinit"`
	// TargetRecoveryTime is a RFC3339 (1970-01-01T00:00:00Z) date and time that defines the point in time recovery objective.
	// It is used to determine the closest restoration source in time.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	TargetRecoveryTime *metav1.Time `json:"targetRecoveryTime,omitempty" webhook:"inmutable"`
	// FileName is the file within the source to be restored.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	FileName *string `json:"fileName,omitempty" webhook:"inmutableinit"`
	// Type is the type of backup. (mariabackup or mariadump)
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Type *string `json:"type,omitempty" webhook:"inmutableinit"`
}

func (r *RestoreSource) Validate() error {
	if r.BackupRef == nil && r.S3 == nil && r.Volume == nil {
		return errors.New("unable to determine restore source")
	}
	return nil
}

func (r *RestoreSource) IsDefaulted() bool {
	return r.Volume != nil
}

func (r *RestoreSource) SetDefaults() {
	if r.S3 != nil {
		r.Volume = &corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		}
	}
}

func (r *RestoreSource) SetDefaultsWithBackup(backup *Backup) error {
	volume, err := backup.Volume()
	if err != nil {
		return fmt.Errorf("error getting backup volume: %v", err)
	}
	r.Volume = volume
	r.S3 = backup.Spec.Storage.S3
	return nil
}

func (r *RestoreSource) TargetRecoveryTimeOrDefault() time.Time {
	if r.TargetRecoveryTime != nil {
		return r.TargetRecoveryTime.Time
	}
	return time.Now()
}

// Schedule contains parameters to define a schedule
type Schedule struct {
	// Cron is a cron expression that defines the schedule.
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Cron string `json:"cron"`
	// Suspend defines whether the schedule is active or not.
	// +optional
	// +kubebuilder:default=false
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	Suspend bool `json:"suspend"`
}

func (s *Schedule) Validate() error {
	_, err := cronParser.Parse(s.Cron)
	return err
}
