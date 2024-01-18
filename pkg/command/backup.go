package command

import (
	"errors"
	"fmt"
	"strings"
	"time"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	backuppkg "github.com/mariadb-operator/mariadb-operator/pkg/backup"
)

type BackupOpts struct {
	CommandOpts
	Path                 string
	TargetFilePath       string
	MaxRetentionDuration time.Duration
	TargetTime           time.Time
	S3                   bool
	S3Bucket             string
	S3Endpoint           string
	S3Region             string
	S3TLS                bool
	S3CACertPath         string
	S3Prefix             string
	LogLevel             string
	DumpOpts             []string
}

type BackupOpt func(*BackupOpts)

func WithBackup(path string, targetFilePath string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.Path = path
		bo.TargetFilePath = targetFilePath
	}
}

func WithBackupMaxRetention(d time.Duration) BackupOpt {
	return func(bo *BackupOpts) {
		bo.MaxRetentionDuration = d
	}
}

func WithBackupTargetTime(t time.Time) BackupOpt {
	return func(bo *BackupOpts) {
		bo.TargetTime = t
	}
}

func WithS3(bucket, endpoint, region, prefix string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.S3 = true
		bo.S3Bucket = bucket
		bo.S3Endpoint = endpoint
		bo.S3Region = region
		bo.S3Prefix = prefix
	}
}

func WithS3TLS(caCertPath string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.S3TLS = true
		bo.S3CACertPath = caCertPath
	}
}

func WithBackupDumpOpts(opts []string) BackupOpt {
	return func(o *BackupOpts) {
		o.DumpOpts = opts
	}
}

func WithBackupUserEnv(u string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.UserEnv = u
	}
}

func WithBackupPasswordEnv(p string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.PasswordEnv = p
	}
}

func WithBackupDatabase(d string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.Database = &d
	}
}

func WithBackupLogLevel(l string) BackupOpt {
	return func(bo *BackupOpts) {
		bo.LogLevel = l
	}
}

type BackupCommand struct {
	*BackupOpts
}

func NewBackupCommand(userOpts ...BackupOpt) (*BackupCommand, error) {
	opts := &BackupOpts{}
	for _, setOpt := range userOpts {
		setOpt(opts)
	}
	if opts.Path == "" {
		return nil, errors.New("path not provided")
	}
	if opts.TargetFilePath == "" {
		return nil, errors.New("target file not provided")
	}
	if opts.MaxRetentionDuration == 0 {
		opts.MaxRetentionDuration = 30 * 24 * time.Hour
	}
	if opts.TargetTime == (time.Time{}) {
		opts.TargetTime = time.Now()
	}
	if opts.UserEnv == "" {
		return nil, errors.New("user environment variable not provided")
	}
	if opts.PasswordEnv == "" {
		return nil, errors.New("password environment variable not provided")
	}
	return &BackupCommand{opts}, nil
}

func (b *BackupCommand) MariadbBackup(backup *mariadbv1alpha1.MariaBackup,
	mariadb *mariadbv1alpha1.MariaDB) *Command {

	cmds := []string{
		"set -euo pipefail",
		"echo '💾 Taking physical backup'",
		"export backupdir=/backup/mariabackup-$(date -u +'%Y-%m-%dT%H:%M:%SZ')",
		"mkdir -p ${backupdir}",
		fmt.Sprintf(
			"mariadb-backup --host=%s-primary --backup "+
				"--target-dir=${backupdir} --user=${MARIADB_OPERATOR_USER} "+
				"--password=${MARIADB_OPERATOR_PASSWORD} ",
			mariadb.Name),
		"echo '📜 Backup completed'",
		"echo '🧹 Cleaning up old backups'",
		fmt.Sprintf(
			"find /backup/ -type d -mtime +%d -exec rm -r {} \\; || true",
			backup.Spec.MaxRetentionDays,
		),
		"echo '📜 Backup history'",
		"du -h --max-depth=1 /backup/ | sort -k2 ",
	}

	return NewBashCommand(cmds)
}

func (b *BackupCommand) MariadbDump(backup *mariadbv1alpha1.Backup,
	mariadb *mariadbv1alpha1.MariaDB) *Command {
	dumpOpts := "--single-transaction --events --routines --dump-slave=2 --master-data=2 --gtid --all-databases"
	if b.BackupOpts.DumpOpts != nil {
		dumpOpts = strings.Join(b.BackupOpts.DumpOpts, " ")
	}
	cmds := []string{
		"set -euo pipefail",
		"echo 💾 Exporting env",
		fmt.Sprintf(
			"export BACKUP_FILE=%s",
			b.newBackupFile(),
		),
		fmt.Sprintf(
			"echo 💾 Writing target file: %s",
			b.TargetFilePath,
		),
		fmt.Sprintf(
			"printf \"${BACKUP_FILE}\" > %s",
			b.TargetFilePath,
		),
		"echo 💾 Setting target file permissions",
		fmt.Sprintf(
			"chmod 777 %s",
			b.TargetFilePath,
		),
		fmt.Sprintf(
			"echo 💾 Taking backup: %s",
			b.getTargetFilePath(),
		),
		fmt.Sprintf(
			"mariadb-dump %s %s > %s",
			ConnectionFlags(&b.BackupOpts.CommandOpts, mariadb),
			dumpOpts,
			b.getTargetFilePath(),
		),
	}
	return NewBashCommand(cmds)
}

func (b *BackupCommand) MariadbOperatorBackup() *Command {
	args := []string{
		"backup",
		"--path",
		b.Path,
		"--target-file-path",
		b.TargetFilePath,
		"--max-retention",
		b.MaxRetentionDuration.String(),
		"--log-level",
		b.LogLevel,
	}
	args = append(args, b.s3Args()...)
	return NewCommand(nil, args)
}

func (b *BackupCommand) MariadbOperatorRestore() *Command {
	args := []string{
		"backup",
		"restore",
		"--path",
		b.Path,
		"--target-time",
		backuppkg.FormatBackupDate(b.TargetTime),
		"--target-file-path",
		b.TargetFilePath,
		"--log-level",
		b.LogLevel,
	}
	args = append(args, b.s3Args()...)
	return NewCommand(nil, args)
}

func (b *BackupCommand) MariadbRestore(mariadb *mariadbv1alpha1.MariaDB) *Command {
	dumpOpts := ""
	if b.BackupOpts.DumpOpts != nil {
		dumpOpts = strings.Join(b.BackupOpts.DumpOpts, " ")
	}
	cmds := []string{
		"set -euo pipefail",
		fmt.Sprintf(
			"echo 💾 Restoring backup: %s",
			b.getTargetFilePath(),
		),
		fmt.Sprintf(
			"mariadb %s %s < %s",
			ConnectionFlags(&b.BackupOpts.CommandOpts, mariadb),
			dumpOpts,
			b.getTargetFilePath(),
		),
	}
	return NewBashCommand(cmds)
}

func (b *BackupCommand) newBackupFile() string {
	return fmt.Sprintf(
		"backup.$(date -u +'%s').sql",
		"%Y-%m-%dT%H:%M:%SZ",
	)
}

func (b *BackupCommand) getTargetFilePath() string {
	return fmt.Sprintf("%s/$(cat '%s')", b.Path, b.TargetFilePath)
}

func (b *BackupCommand) s3Args() []string {
	if !b.S3 {
		return nil
	}
	args := []string{
		"--s3",
		"--s3-bucket",
		b.S3Bucket,
		"--s3-endpoint",
		b.S3Endpoint,
	}
	if b.S3Region != "" {
		args = append(args,
			"--s3-region",
			b.S3Region,
		)
	}
	if b.S3TLS {
		args = append(args,
			"--s3-tls",
		)
		if b.S3CACertPath != "" {
			args = append(args,
				"--s3-ca-cert-path",
				b.S3CACertPath,
			)
		}
	}
	if b.S3Prefix != "" {
		args = append(args,
			"--s3-prefix",
			b.S3Prefix,
		)
	}
	return args
}
