package builder

import (
	"errors"
	"fmt"
	"path/filepath"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	metadata "github.com/mariadb-operator/mariadb-operator/pkg/builder/metadata"
	"github.com/mariadb-operator/mariadb-operator/pkg/command"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	batchDataVolume        = "data"
	batchDataMountPath     = "/var/lib/mysql"
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
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
			Build()

	cmdOpts := []command.BackupOpt{
		command.WithBackup(
			batchStorageMountPath,
			batchBackupTargetFilePath,
		),
		command.WithBackupMaxRetention(backup.Spec.MaxRetention.Duration),
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
	volumes, volumeSources := jobBatchStorageVolume(volume, mariadb, false, backup.Spec.Storage.S3)

	opts := []jobOption{
		withJobMeta(objMeta),
		withJobVolumes(volumes...),
		withJobInitContainers(
			jobMariadbContainer(
				cmd.MariadbDump(backup, mariadb),
				volumeSources,
				jobEnv(mariadb),
				backup.Spec.Resources,
				mariadb,
				backup.Spec.SecurityContext,
			),
		),
		withJobContainers(
			jobMariadbOperatorContainer(
				cmd.MariadbOperatorBackup(),
				volumeSources,
				jobS3Env(backup.Spec.Storage.S3),
				backup.Spec.Resources,
				mariadb,
				b.env,
				backup.Spec.SecurityContext,
			),
		),
		withJobBackoffLimit(backup.Spec.BackoffLimit),
		withJobRestartPolicy(backup.Spec.RestartPolicy),
		withAffinity(backup.Spec.Affinity),
		withNodeSelector(backup.Spec.NodeSelector),
		withTolerations(backup.Spec.Tolerations...),
		withPodSecurityContext(backup.Spec.PodSecurityContext),
	}

	builder, err := newJobBuilder(opts...)
	if err != nil {
		return nil, fmt.Errorf("error building backup Job: %v", err)
	}

	job := builder.build()
	if err := controllerutil.SetControllerReference(backup, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildMariaBackupJob(key types.NamespacedName, backup *mariadbv1alpha1.MariaBackup,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
			Build()

	var batchBackupTargetFilePath = fmt.Sprintf("%s/0-mariabackup-target.txt", batchStorageMountPath)

	cmdOpts := []command.BackupOpt{
		command.WithBackup(
			batchStorageMountPath,
			batchBackupTargetFilePath,
		),
		command.WithBackupUserEnv(batchUserEnv),
		command.WithBackupPasswordEnv(batchPasswordEnv),
	}

	cmd, err := command.NewBackupCommand(cmdOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building backup command: %v", err)
	}

	volume, err := backup.Volume()
	if err != nil {
		return nil, fmt.Errorf("error getting volume from Backup: %v", err)
	}
	volumes, volumeSources := jobBatchStorageVolume(volume, mariadb, true, nil)

	opts := []jobOption{
		withJobMeta(objMeta),
		withJobVolumes(volumes...),
		withJobContainers(
			jobMariadbContainer(
				cmd.MariadbBackup(backup, mariadb),
				volumeSources,
				jobEnv(mariadb),
				backup.Spec.Resources,
				mariadb,
			),
		),
		withJobBackoffLimit(backup.Spec.BackoffLimit),
		withJobRestartPolicy(backup.Spec.RestartPolicy),
		withAffinity(backup.Spec.Affinity),
		withNodeSelector(backup.Spec.NodeSelector),
		withTolerations(backup.Spec.Tolerations...),
	}

	builder, err := newJobBuilder(opts...)
	if err != nil {
		return nil, fmt.Errorf("error building backup Job: %v", err)
	}

	job := builder.build()
	if err := controllerutil.SetControllerReference(backup, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildMariaBackupCronJob(key types.NamespacedName, backup *mariadbv1alpha1.MariaBackup,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.CronJob, error) {
	if backup.Spec.Schedule == nil {
		return nil, errors.New("schedule field is mandatory when building a CronJob")
	}

	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
			Build()
	job, err := b.BuildMariaBackupJob(key, backup, mariadb)
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
		},
	}
	if err := controllerutil.SetControllerReference(backup, cronJob, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to CronJob: %v", err)
	}
	return cronJob, nil
}

func (b *Builder) BuildBackupCronJob(key types.NamespacedName, backup *mariadbv1alpha1.Backup,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.CronJob, error) {
	if backup.Spec.Schedule == nil {
		return nil, errors.New("schedule field is mandatory when building a CronJob")
	}

	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
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
		},
	}
	if err := controllerutil.SetControllerReference(backup, cronJob, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to CronJob: %v", err)
	}
	return cronJob, nil
}

func (b *Builder) BuildRestoreJob(key types.NamespacedName, restore *mariadbv1alpha1.Restore,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
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
	volumes, volumeSources := jobBatchStorageVolume(restore.Spec.RestoreSource.Volume, mariadb, false, restore.Spec.S3)

	jobOpts := []jobOption{
		withJobMeta(objMeta),
		withJobVolumes(volumes...),
		withJobInitContainers(
			jobMariadbOperatorContainer(
				cmd.MariadbOperatorRestore(),
				volumeSources,
				jobS3Env(restore.Spec.S3),
				restore.Spec.Resources,
				mariadb,
				b.env,
				restore.Spec.SecurityContext,
			),
		),
		withJobContainers(
			jobMariadbContainer(
				cmd.MariadbRestore(mariadb),
				volumeSources,
				jobEnv(mariadb),
				restore.Spec.Resources,
				mariadb,
				restore.Spec.SecurityContext,
			),
		),
		withJobBackoffLimit(restore.Spec.BackoffLimit),
		withJobRestartPolicy(restore.Spec.RestartPolicy),
		withAffinity(restore.Spec.Affinity),
		withNodeSelector(restore.Spec.NodeSelector),
		withTolerations(restore.Spec.Tolerations...),
		withPodSecurityContext(restore.Spec.PodSecurityContext),
	}

	builder, err := newJobBuilder(jobOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building backup Job: %v", err)
	}

	job := builder.build()
	if err := controllerutil.SetControllerReference(restore, job, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to Job: %v", err)
	}
	return job, nil
}

func (b *Builder) BuildSqlJob(key types.NamespacedName, sqlJob *mariadbv1alpha1.SqlJob,
	mariadb *mariadbv1alpha1.MariaDB) (*batchv1.Job, error) {
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
			Build()

	volumes, volumeMounts := sqlJobvolumes(sqlJob)

	sqlOpts := []command.SqlOpt{
		command.WithSqlUserEnv(batchUserEnv),
		command.WithSqlPasswordEnv(batchPasswordEnv),
		command.WithSqlFile(fmt.Sprintf("%s/%s", batchScriptsMountPath, batchScriptsSqlFile)),
	}
	if sqlJob.Spec.Database != nil {
		sqlOpts = append(sqlOpts, command.WithSqlDatabase(*sqlJob.Spec.Database))
	}
	cmd, err := command.NewSqlCommand(sqlOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building sql command: %v", err)
	}

	jobOpts := []jobOption{
		withJobMeta(objMeta),
		withJobVolumes(volumes...),
		withJobContainers(
			jobMariadbContainer(
				cmd.ExecCommand(mariadb),
				volumeMounts,
				sqlJobEnv(sqlJob),
				sqlJob.Spec.Resources,
				mariadb,
				sqlJob.Spec.SecurityContext,
			),
		),
		withJobBackoffLimit(sqlJob.Spec.BackoffLimit),
		withJobRestartPolicy(sqlJob.Spec.RestartPolicy),
		withAffinity(sqlJob.Spec.Affinity),
		withNodeSelector(sqlJob.Spec.NodeSelector),
		withTolerations(sqlJob.Spec.Tolerations...),
		withPodSecurityContext(sqlJob.Spec.PodSecurityContext),
	}

	builder, err := newJobBuilder(jobOpts...)
	if err != nil {
		return nil, fmt.Errorf("error building sql Job: %v", err)
	}

	job := builder.build()
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
			WithMariaDB(mariadb).
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
		},
	}
	if err := controllerutil.SetControllerReference(sqlJob, cronJob, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to CronJob: %v", err)
	}
	return cronJob, nil
}

func s3Opts(s3 *mariadbv1alpha1.S3) []command.BackupOpt {
	if s3 == nil {
		return nil
	}
	cmdOpts := []command.BackupOpt{
		command.WithS3(
			s3.Bucket,
			s3.Endpoint,
			s3.Region,
			s3.Prefix,
		),
	}
	if s3.TLS != nil && s3.TLS.Enabled {
		caCertPath := ""
		if s3.TLS.CASecretKeyRef != nil {
			caCertPath = filepath.Join(batchS3PKIMountPath, s3.TLS.CASecretKeyRef.Key)
		}
		cmdOpts = append(cmdOpts, command.WithS3TLS(caCertPath))
	}
	return cmdOpts
}
