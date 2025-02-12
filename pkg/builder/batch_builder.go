package builder

import (
	"errors"
	"fmt"
	"path/filepath"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	metadata "github.com/mariadb-operator/mariadb-operator/pkg/builder/metadata"
	builderpki "github.com/mariadb-operator/mariadb-operator/pkg/builder/pki"
	"github.com/mariadb-operator/mariadb-operator/pkg/command"
	galeraresources "github.com/mariadb-operator/mariadb-operator/pkg/controller/galera/resources"
	kadapter "github.com/mariadb-operator/mariadb-operator/pkg/kubernetes/adapter"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	batchStorageVolume     = "backup"
	batchStorageMountPath  = "/backup"
	batchScriptsVolume     = "scripts"
	batchS3PKI             = "s3-pki"
	batchS3PKIMountPath    = "/s3/pki"
	batchScriptsMountPath  = "/opt"
	batchScriptsSqlFile    = "job.sql"
	batchUserEnv           = "MARIADB_OPERATOR_USER"
	batchPasswordEnv       = "MARIADB_OPERATOR_PASSWORD"
	batchS3AccessKeyId     = "AWS_ACCESS_KEY_ID"
	batchS3SecretAccessKey = "AWS_SECRET_ACCESS_KEY"
	batchS3SessionTokenKey = "AWS_SESSION_TOKEN"
)

var batchBackupTargetFilePath = fmt.Sprintf("%s/0-backup-target.txt", batchStorageMountPath)

func (b *Builder) BuildBackupJob(key types.NamespacedName, backup *mariadbv1alpha1.Backup,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	jobMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(backup.Spec.InheritMetadata).
			Build()
	podMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(backup.Spec.InheritMetadata).
			WithMetadata(backup.Spec.PodMetadata).
			Build()

	cmdOpts := []command.BackupOpt{
		command.WithBackup(
			batchStorageMountPath,
			batchBackupTargetFilePath,
		),
		command.WithCleanupTargetFile(backupShouldCleanupTargetFile(backup)),
		command.WithBackupMaxRetention(backup.Spec.MaxRetention.Duration),
		command.WithBackupCompression(backup.Spec.Compression),
		command.WithBackupUserEnv(batchUserEnv),
		command.WithBackupPasswordEnv(batchPasswordEnv),
		command.WithBackupLogLevel(backup.Spec.LogLevel),
		command.WithBackupDumpOpts(backup.Spec.Args),
	}
	cmdOpts = append(cmdOpts, s3Opts(backup.Spec.Storage.S3)...)

	cmd, err := command.NewBackupCommand(cmdOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building backup command: %v", err)
	}

	volume, err := backup.Volume()
	if err != nil {
		return nil, fmt.Errorf("error getting volume from Backup: %v", err)
	}
	volumes, volumeSources := jobBatchStorageVolume(volume, backup.Spec.Storage.S3, mariadb)
	affinity := ptr.Deref(backup.Spec.Affinity, mariadbv1alpha1.AffinityConfig{}).Affinity

	mariadbContainer, err := b.jobMariadbContainer(
		cmd.MariadbDump(backup, mariadb),
		volumeSources,
		jobEnv(mariadb),
		jobResources(backup.Spec.Resources),
		mariadb,
		backup.Spec.SecurityContext,
	)
	if err != nil {
		return nil, err
	}

	operatorContainer, err := b.jobMariadbOperatorContainer(
		cmd.MariadbOperatorBackup(),
		volumeSources,
		jobS3Env(backup.Spec.Storage.S3),
		jobResources(backup.Spec.Resources),
		mariadb,
		b.env,
		backup.Spec.SecurityContext,
	)
	if err != nil {
		return nil, err
	}

	securityContext, err := b.buildPodSecurityContextWithUserGroup(backup.Spec.PodSecurityContext, mysqlUser, mysqlGroup)
	if err != nil {
		return nil, err
	}

	job := &batchv1.Job{
		ObjectMeta: jobMeta,
		Spec: batchv1.JobSpec{
			BackoffLimit: &backup.Spec.BackoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: podMeta,
				Spec: corev1.PodSpec{
					ActiveDeadlineSeconds: backup.Spec.ActiveDeadlineSeconds,
					RestartPolicy:         backup.Spec.RestartPolicy,
					ImagePullSecrets:      batchImagePullSecrets(mariadb, backup.Spec.ImagePullSecrets),
					Volumes:               volumes,
					InitContainers:        []corev1.Container{*mariadbContainer},
					Containers:            []corev1.Container{*operatorContainer},
					Affinity:              ptr.To(affinity.ToKubernetesType()),
					NodeSelector:          backup.Spec.NodeSelector,
					Tolerations:           backup.Spec.Tolerations,
					SecurityContext:       securityContext,
					ServiceAccountName:    ptr.Deref(backup.Spec.ServiceAccountName, "default"),
					PriorityClassName:     ptr.Deref(backup.Spec.PriorityClassName, ""),
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(backup, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildBackupCronJob(key types.NamespacedName, backup *mariadbv1alpha1.Backup,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.CronJob, error) {
	if backup.Spec.Schedule == nil {
		return nil, errors.New("schedule field is mandatory when building a CronJob")
	}
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(backup.Spec.InheritMetadata).
			Build()
	job, err := b.BuildBackupJob(key, backup, mariadb)
	if err != nil {
		return nil, fmt.Errorf("error building Backup: %v", err)
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: objMeta,
		Spec: batchv1.CronJobSpec{
			Schedule:          backup.Spec.Schedule.Cron,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			Suspend:           &backup.Spec.Schedule.Suspend,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: job.ObjectMeta,
				Spec:       job.Spec,
			},
			SuccessfulJobsHistoryLimit: backup.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     backup.Spec.FailedJobsHistoryLimit,
			TimeZone:                   backup.Spec.TimeZone,
		},
	}
	if err := controllerutil.SetControllerReference(backup, cronJob, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to CronJob: %v", err)
	}
	return cronJob, nil
}

func (b *Builder) BuildRestoreJob(key types.NamespacedName, restore *mariadbv1alpha1.Restore,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	jobMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(restore.Spec.InheritMetadata).
			Build()
	podMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(restore.Spec.InheritMetadata).
			WithMetadata(restore.Spec.PodMetadata).
			Build()

	cmdOpts := []command.BackupOpt{
		command.WithBackup(
			batchStorageMountPath,
			batchBackupTargetFilePath,
		),
		command.WithBackupTargetTime(restore.Spec.RestoreSource.TargetRecoveryTimeOrDefault()),
		command.WithBackupUserEnv(batchUserEnv),
		command.WithBackupPasswordEnv(batchPasswordEnv),
		command.WithBackupLogLevel(restore.Spec.LogLevel),
		command.WithBackupDumpOpts(restore.Spec.Args),
	}
	cmdOpts = append(cmdOpts, s3Opts(restore.Spec.S3)...)

	cmd, err := command.NewBackupCommand(cmdOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building restore command: %v", err)
	}

	volume := ptr.Deref(restore.Spec.Volume, mariadbv1alpha1.StorageVolumeSource{})
	volumes, volumeSources := jobBatchStorageVolume(volume, restore.Spec.S3, mariadb)
	affinity := ptr.Deref(restore.Spec.Affinity, mariadbv1alpha1.AffinityConfig{}).Affinity

	operatorContainer, err := b.jobMariadbOperatorContainer(
		cmd.MariadbOperatorRestore(),
		volumeSources,
		jobS3Env(restore.Spec.S3),
		jobResources(restore.Spec.Resources),
		mariadb,
		b.env,
		restore.Spec.SecurityContext,
	)
	if err != nil {
		return nil, err
	}

	mariadbContainer, err := b.jobMariadbContainer(
		cmd.MariadbRestore(restore, mariadb),
		volumeSources,
		jobEnv(mariadb),
		jobResources(restore.Spec.Resources),
		mariadb,
		restore.Spec.SecurityContext,
	)
	if err != nil {
		return nil, err
	}

	securityContext, err := b.buildPodSecurityContextWithUserGroup(restore.Spec.PodSecurityContext, mysqlUser, mysqlGroup)
	if err != nil {
		return nil, err
	}

	job := &batchv1.Job{
		ObjectMeta: jobMeta,
		Spec: batchv1.JobSpec{
			BackoffLimit: &restore.Spec.BackoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: podMeta,
				Spec: corev1.PodSpec{
					RestartPolicy:      restore.Spec.RestartPolicy,
					ImagePullSecrets:   batchImagePullSecrets(mariadb, restore.Spec.ImagePullSecrets),
					Volumes:            volumes,
					InitContainers:     []corev1.Container{*operatorContainer},
					Containers:         []corev1.Container{*mariadbContainer},
					Affinity:           ptr.To(affinity.ToKubernetesType()),
					NodeSelector:       restore.Spec.NodeSelector,
					Tolerations:        restore.Spec.Tolerations,
					SecurityContext:    securityContext,
					ServiceAccountName: ptr.Deref(restore.Spec.ServiceAccountName, "default"),
					PriorityClassName:  ptr.Deref(restore.Spec.PriorityClassName, ""),
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(restore, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuilGaleraInitJob(key types.NamespacedName, mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
	if !galera.Enabled {
		return nil, errors.New("Galera must be enabled")
	}
	initJob := ptr.Deref(galera.InitJob, mariadbv1alpha1.GaleraInitJob{})
	extraMeta := ptr.Deref(initJob.Metadata, mariadbv1alpha1.Metadata{})
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(mariadb.Spec.InheritMetadata).
			WithMetadata(&extraMeta).
			Build()
	command := command.NewBashCommand([]string{
		filepath.Join(InitConfigPath, InitEntrypointKey),
	})

	opts := []mariadbPodOpt{
		withMeta(mariadb.Spec.InheritMetadata),
		withMeta(&extraMeta),
		withCommand(command.Command),
		withArgs(command.Args),
		withRestartPolicy(corev1.RestartPolicyOnFailure),
		withResources(jobResources(initJob.Resources)),
		withExtraVolumes([]corev1.Volume{
			{
				Name: StorageVolume,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: mariadb.PVCKey(StorageVolume, 0).Name,
					},
				},
			},
			{
				Name: InitVolume,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: mariadb.InitKey().Name,
						},
						DefaultMode: ptr.To(int32(0777)),
					},
				},
			},
		}),
		withExtraVolumeMounts([]corev1.VolumeMount{
			{
				Name:      InitVolume,
				MountPath: InitConfigPath,
			},
		}),
		withMariadbResources(false),
		withMariadbSelectorLabels(false),
		withGaleraContainers(false),
		withGaleraConfig(false),
		withServiceAccount(false),
		withPorts(false),
		withProbes(false),
		withHAAnnotations(false),
	}

	podTpl, err := b.mariadbPodTemplate(mariadb, opts...)
	if err != nil {
		return nil, fmt.Errorf("error building MariaDB Pod template: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: objMeta,
		Spec: batchv1.JobSpec{
			Template: *podTpl,
		},
	}
	if err := controllerutil.SetControllerReference(mariadb, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildGaleraRecoveryJob(key types.NamespacedName, mariadb *mariadbv1alpha1.MariaDB,
	pod *corev1.Pod) (*batchv1.Job, error) {
	galera := ptr.Deref(mariadb.Spec.Galera, mariadbv1alpha1.Galera{})
	if !galera.Enabled {
		return nil, errors.New("Galera must be enabled")
	}
	recovery := ptr.Deref(galera.Recovery, mariadbv1alpha1.GaleraRecovery{})
	if !recovery.Enabled {
		return nil, errors.New("Galera recovery must be enabled")
	}

	podIndex, err := statefulset.PodIndex(pod.Name)
	if err != nil {
		return nil, fmt.Errorf("error getting index for Pod '%s': %v", pod.Name, err)
	}
	if pod.Spec.NodeName == "" {
		return nil, errors.New("Pod must be scheduled: spec.nodeName is empty")
	}

	recoveryJob := ptr.Deref(recovery.Job, mariadbv1alpha1.GaleraRecoveryJob{})
	extraMeta := ptr.Deref(recoveryJob.Metadata, mariadbv1alpha1.Metadata{})
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(mariadb.Spec.InheritMetadata).
			WithMetadata(&extraMeta).
			Build()
	command := command.NewCommand([]string{"mariadbd"}, []string{"--wsrep-recover"})

	reuseStorageVolume := ptr.Deref(galera.Config.ReuseStorageVolume, false)

	volumes := []corev1.Volume{
		{
			Name: StorageVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: mariadb.PVCKey(StorageVolume, *podIndex).Name,
				},
			},
		},
	}
	if !reuseStorageVolume {
		volumes = append(volumes, corev1.Volume{
			Name: galeraresources.GaleraConfigVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: mariadb.PVCKey(galeraresources.GaleraConfigVolume, *podIndex).Name,
				},
			},
		})
	}

	podAffinityEnabled := ptr.Deref(recoveryJob.PodAffinity, true)

	opts := []mariadbPodOpt{
		withMeta(mariadb.Spec.InheritMetadata),
		withMeta(&extraMeta),
		withCommand(command.Command),
		withArgs(command.Args),
		withRestartPolicy(corev1.RestartPolicyOnFailure),
		withResources(jobResources(recoveryJob.Resources)),
		withExtraVolumes(volumes),
		withAffinityEnabled(false),
		withMariadbResources(false),
		withMariadbSelectorLabels(false),
		withGaleraContainers(false),
		withGaleraConfig(true),
		withServiceAccount(false),
		withPorts(false),
		withProbes(false),
		withHAAnnotations(false),
	}
	if podAffinityEnabled {
		opts = append(opts, withNodeSelector(
			map[string]string{
				"kubernetes.io/hostname": pod.Spec.NodeName,
			},
		))
	}

	podTpl, err := b.mariadbPodTemplate(mariadb, opts...)
	if err != nil {
		return nil, fmt.Errorf("error building MariaDB Pod template: %v", err)
	}

	job := &batchv1.Job{
		ObjectMeta: objMeta,
		Spec: batchv1.JobSpec{
			Template: *podTpl,
		},
	}
	if err := controllerutil.SetControllerReference(mariadb, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildSqlJob(key types.NamespacedName, sqlJob *mariadbv1alpha1.SqlJob,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	jobMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(sqlJob.Spec.InheritMetadata).
			Build()
	podMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(sqlJob.Spec.InheritMetadata).
			WithMetadata(sqlJob.Spec.PodMetadata).
			Build()

	sqlOpts := []command.SqlOpt{
		command.WithSqlUserEnv(batchUserEnv),
		command.WithSqlPasswordEnv(batchPasswordEnv),
		command.WithSqlFile(fmt.Sprintf("%s/%s", batchScriptsMountPath, batchScriptsSqlFile)),
	}
	if sqlJob.Spec.Database != nil {
		sqlOpts = append(sqlOpts, command.WithSqlDatabase(*sqlJob.Spec.Database))
	}
	if (sqlJob.Spec.TLSCACertSecretRef != nil && sqlJob.Spec.TLSClientCertSecretRef != nil) || mariadb.IsTLSEnabled() {
		sqlOpts = append(sqlOpts, command.WithSSL(
			builderpki.CACertPath,
			builderpki.ClientCertPath,
			builderpki.ClientKeyPath,
		))
	}
	cmd, err := command.NewSqlCommand(sqlOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building sql command: %v", err)
	}

	volumes, volumeMounts := sqlJobvolumes(sqlJob, mariadb)
	affinity := ptr.Deref(sqlJob.Spec.Affinity, mariadbv1alpha1.AffinityConfig{}).Affinity
	var resources *corev1.ResourceRequirements
	if sqlJob.Spec.Resources != nil {
		resources = ptr.To(sqlJob.Spec.Resources.ToKubernetesType())
	}

	container, err := b.jobMariadbContainer(
		cmd.ExecCommand(mariadb),
		volumeMounts,
		sqlJobEnv(sqlJob),
		resources,
		mariadb,
		sqlJob.Spec.SecurityContext,
	)
	if err != nil {
		return nil, err
	}

	securityContext, err := b.buildPodSecurityContext(sqlJob.Spec.PodSecurityContext)
	if err != nil {
		return nil, err
	}

	job := &batchv1.Job{
		ObjectMeta: jobMeta,
		Spec: batchv1.JobSpec{
			BackoffLimit: &sqlJob.Spec.BackoffLimit,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: podMeta,
				Spec: corev1.PodSpec{
					RestartPolicy:      sqlJob.Spec.RestartPolicy,
					ImagePullSecrets:   batchImagePullSecrets(mariadb, sqlJob.Spec.ImagePullSecrets),
					Volumes:            volumes,
					Containers:         []corev1.Container{*container},
					Affinity:           ptr.To(affinity.ToKubernetesType()),
					NodeSelector:       sqlJob.Spec.NodeSelector,
					Tolerations:        sqlJob.Spec.Tolerations,
					SecurityContext:    securityContext,
					ServiceAccountName: ptr.Deref(sqlJob.Spec.ServiceAccountName, "default"),
					PriorityClassName:  ptr.Deref(sqlJob.Spec.PriorityClassName, ""),
				},
			},
		},
	}
	if err := controllerutil.SetControllerReference(sqlJob, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildSqlCronJob(key types.NamespacedName, sqlJob *mariadbv1alpha1.SqlJob,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.CronJob, error) {
	if sqlJob.Spec.Schedule == nil {
		return nil, errors.New("schedule field is mandatory when building a CronJob")
	}
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMetadata(sqlJob.Spec.InheritMetadata).
			Build()
	job, err := b.BuildSqlJob(key, sqlJob, mariadb)
	if err != nil {
		return nil, fmt.Errorf("error building SqlJob: %v", err)
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: objMeta,
		Spec: batchv1.CronJobSpec{
			Schedule:          sqlJob.Spec.Schedule.Cron,
			ConcurrencyPolicy: batchv1.ForbidConcurrent,
			Suspend:           &sqlJob.Spec.Schedule.Suspend,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: job.ObjectMeta,
				Spec:       job.Spec,
			},
			SuccessfulJobsHistoryLimit: sqlJob.Spec.SuccessfulJobsHistoryLimit,
			FailedJobsHistoryLimit:     sqlJob.Spec.FailedJobsHistoryLimit,
			TimeZone:                   sqlJob.Spec.TimeZone,
		},
	}
	if err := controllerutil.SetControllerReference(sqlJob, cronJob, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to CronJob: %v", err)
	}
	return cronJob, nil
}

func backupShouldCleanupTargetFile(backup *mariadbv1alpha1.Backup) bool {
	return backup.Spec.Storage.S3 != nil && backup.Spec.StagingStorage != nil
}

func s3Opts(s3 *mariadbv1alpha1.S3) []command.BackupOpt {
	if s3 == nil {
		return nil
	}
	tls := ptr.Deref(s3.TLS, mariadbv1alpha1.TLSS3{})

	cmdOpts := []command.BackupOpt{
		command.WithS3(
			s3.Bucket,
			s3.Endpoint,
			s3.Region,
			s3.Prefix,
		),
		command.WithS3TLS(tls.Enabled),
	}
	if tls.Enabled && tls.CASecretKeyRef != nil {
		caCertPath := filepath.Join(batchS3PKIMountPath, s3.TLS.CASecretKeyRef.Key)
		cmdOpts = append(cmdOpts, command.WithS3CACertPath(caCertPath))
	}
	return cmdOpts
}

func batchImagePullSecrets(mariadb *mariadbv1alpha1.MariaDB,
	pullSecrets []mariadbv1alpha1.LocalObjectReference) []corev1.LocalObjectReference {
	var secrets []mariadbv1alpha1.LocalObjectReference
	secrets = append(secrets, mariadb.Spec.ImagePullSecrets...)
	secrets = append(secrets, pullSecrets...)
	return kadapter.ToKubernetesSlice(secrets)
}
