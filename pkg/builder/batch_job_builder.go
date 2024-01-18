package builder

import (
	"errors"
	"fmt"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	cmd "github.com/mariadb-operator/mariadb-operator/pkg/command"
	"github.com/mariadb-operator/mariadb-operator/pkg/environment"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type jobOption func(*jobBuilder)

func withJobMeta(meta metav1.ObjectMeta) jobOption {
	return func(b *jobBuilder) {
		b.meta = &meta
	}
}

func withJobVolumes(volumes ...corev1.Volume) jobOption {
	return func(b *jobBuilder) {
		b.volumes = volumes
	}
}

func withJobInitContainers(initContainers ...v1.Container) jobOption {
	return func(b *jobBuilder) {
		b.initContainers = initContainers
	}
}

func withJobContainers(containers ...v1.Container) jobOption {
	return func(b *jobBuilder) {
		b.containers = containers
	}
}

func withJobBackoffLimit(backoffLimit int32) jobOption {
	return func(b *jobBuilder) {
		b.backoffLimit = &backoffLimit
	}
}

func withJobRestartPolicy(restartPolicy corev1.RestartPolicy) jobOption {
	return func(b *jobBuilder) {
		b.restartPolicy = &restartPolicy
	}
}

func withAffinity(affinity *corev1.Affinity) jobOption {
	return func(b *jobBuilder) {
		b.affinity = affinity
	}
}

func withNodeSelector(nodeSelector map[string]string) jobOption {
	return func(b *jobBuilder) {
		b.nodeSelector = nodeSelector
	}
}

func withTolerations(tolerations ...corev1.Toleration) jobOption {
	return func(b *jobBuilder) {
		b.tolerations = tolerations
	}
}

func withPodSecurityContext(podSecurityContext *corev1.PodSecurityContext) jobOption {
	return func(b *jobBuilder) {
		b.podSecurityContext = podSecurityContext
	}
}

type jobBuilder struct {
	meta               *metav1.ObjectMeta
	volumes            []corev1.Volume
	initContainers     []corev1.Container
	containers         []corev1.Container
	backoffLimit       *int32
	restartPolicy      *corev1.RestartPolicy
	affinity           *corev1.Affinity
	nodeSelector       map[string]string
	tolerations        []corev1.Toleration
	podSecurityContext *corev1.PodSecurityContext
}

func newJobBuilder(opts ...jobOption) (*jobBuilder, error) {
	builder := jobBuilder{}
	for _, setOpt := range opts {
		setOpt(&builder)
	}

	if builder.meta == nil {
		return nil, errors.New("meta is mandatory")
	}
	if builder.volumes == nil {
		return nil, errors.New("volumes are mandatory")
	}
	if builder.containers == nil {
		return nil, errors.New("containers are mandatory")
	}
	return &builder, nil
}

func (b *jobBuilder) build() *batchv1.Job {
	template := corev1.PodTemplateSpec{
		ObjectMeta: *b.meta,
		Spec: corev1.PodSpec{
			Volumes:         b.volumes,
			Containers:      b.containers,
			Affinity:        b.affinity,
			NodeSelector:    b.nodeSelector,
			Tolerations:     b.tolerations,
			SecurityContext: b.podSecurityContext,
		},
	}
	if b.initContainers != nil {
		template.Spec.InitContainers = b.initContainers
	}
	if b.restartPolicy != nil {
		template.Spec.RestartPolicy = *b.restartPolicy
	}

	job := &batchv1.Job{
		ObjectMeta: *b.meta,
		Spec: batchv1.JobSpec{
			Template: template,
		},
	}
	if b.backoffLimit != nil {
		job.Spec.BackoffLimit = b.backoffLimit
	}
	return job
}

func jobContainer(name string, cmd *cmd.Command, image string, volumeMounts []corev1.VolumeMount, env []v1.EnvVar,
	resources *corev1.ResourceRequirements, mariadb *mariadbv1alpha1.MariaDB, securityContext *corev1.SecurityContext) corev1.Container {

	container := corev1.Container{
		Name:            name,
		Image:           image,
		ImagePullPolicy: mariadb.Spec.ImagePullPolicy,
		Command:         cmd.Command,
		Args:            cmd.Args,
		Env:             env,
		VolumeMounts:    volumeMounts,
		SecurityContext: securityContext,
	}
	if resources != nil {
		container.Resources = *resources
	}
	return container
}

func jobMariadbOperatorContainer(cmd *cmd.Command, volumeMounts []corev1.VolumeMount, envVar []v1.EnvVar,
	resources *corev1.ResourceRequirements, mariadb *mariadbv1alpha1.MariaDB, env *environment.Environment,
	securityContext *corev1.SecurityContext) corev1.Container {
	return jobContainer("mariadb-operator", cmd, env.MariadbOperatorImage, volumeMounts, envVar, resources, mariadb, securityContext)
}

func jobMariadbContainer(cmd *cmd.Command, volumeMounts []corev1.VolumeMount, envVar []v1.EnvVar,
	resources *corev1.ResourceRequirements, mariadb *mariadbv1alpha1.MariaDB, securityContext *corev1.SecurityContext) corev1.Container {
	return jobContainer("mariadb", cmd, mariadb.Spec.Image, volumeMounts, envVar, resources, mariadb, securityContext)
}

func jobBatchStorageVolume(volumeSource *corev1.VolumeSource, mariaDb *mariadbv1alpha1.MariaDB,
	mountDataDir bool, s3 *mariadbv1alpha1.S3) ([]corev1.Volume, []corev1.VolumeMount) {

	volumes := []corev1.Volume{
		{
			Name:         batchStorageVolume,
			VolumeSource: *volumeSource,
		}}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      batchStorageVolume,
			MountPath: batchStorageMountPath,
		}}

	if mountDataDir {

		dataVolume := corev1.Volume{
			Name: batchDataVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: fmt.Sprintf("storage-%s-%d", mariaDb.Name, *mariaDb.Status.CurrentPrimaryPodIndex),
					ReadOnly:  true,
				},
			},
		}

		dataVolumeMount := corev1.VolumeMount{
			Name:      batchDataVolume,
			MountPath: batchDataMountPath,
			ReadOnly:  true,
		}

		volumes = append(volumes, dataVolume)
		volumeMounts = append(volumeMounts, dataVolumeMount)

	}

	if s3 != nil && s3.TLS != nil && s3.TLS.Enabled && s3.TLS.CASecretKeyRef != nil {
		volumes = append(volumes, corev1.Volume{
			Name: batchS3PKI,
			VolumeSource: corev1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s3.TLS.CASecretKeyRef.Name,
				},
			},
		})
		volumeMounts = append(volumeMounts, v1.VolumeMount{
			Name:      batchS3PKI,
			MountPath: batchS3PKIMountPath,
		})
	}

	return volumes, volumeMounts

}

func jobEnv(mariadb *mariadbv1alpha1.MariaDB) []v1.EnvVar {
	return []v1.EnvVar{
		{
			Name:  batchUserEnv,
			Value: "root",
		},
		{
			Name: batchPasswordEnv,
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &mariadb.Spec.RootPasswordSecretKeyRef,
			},
		},
	}
}

func jobS3Env(s3 *mariadbv1alpha1.S3) []v1.EnvVar {
	if s3 == nil {
		return nil
	}
	env := []v1.EnvVar{
		{
			Name: batchS3AccessKeyId,
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &s3.AccessKeyIdSecretKeyRef,
			},
		},
		{
			Name: batchS3SecretAccessKey,
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &s3.SecretAccessKeySecretKeyRef,
			},
		},
	}
	if s3.SessionTokenSecretKeyRef != nil {
		env = append(env, v1.EnvVar{
			Name: batchS3SessionTokenKey,
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: s3.SessionTokenSecretKeyRef,
			},
		})
	}
	return env
}

func sqlJobvolumes(sqlJob *mariadbv1alpha1.SqlJob) ([]corev1.Volume, []corev1.VolumeMount) {
	return []corev1.Volume{
			{
				Name: batchScriptsVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: sqlJob.Spec.SqlConfigMapKeyRef.LocalObjectReference,
						Items: []corev1.KeyToPath{
							{
								Key:  sqlJob.Spec.SqlConfigMapKeyRef.Key,
								Path: batchScriptsSqlFile,
							},
						},
					},
				},
			},
		}, []corev1.VolumeMount{
			{
				Name:      batchScriptsVolume,
				MountPath: batchScriptsMountPath,
			},
		}
}

func sqlJobEnv(sqlJob *mariadbv1alpha1.SqlJob) []v1.EnvVar {
	return []v1.EnvVar{
		{
			Name:  batchUserEnv,
			Value: sqlJob.Spec.Username,
		},
		{
			Name: batchPasswordEnv,
			ValueFrom: &v1.EnvVarSource{
				SecretKeyRef: &sqlJob.Spec.PasswordSecretKeyRef,
			},
		},
	}
}
