package replication

import (
	"context"
	"fmt"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	"github.com/mariadb-operator/mariadb-operator/pkg/builder"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/secret"
	"github.com/mariadb-operator/mariadb-operator/pkg/refresolver"
	sqlClient "github.com/mariadb-operator/mariadb-operator/pkg/sql"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	replUser       = "repl"
	replUserHost   = "%"
	connectionName = "mariadb-operator"
)

type ReplicationConfig struct {
	client.Client
	builder          *builder.Builder
	refResolver      *refresolver.RefResolver
	secretReconciler *secret.SecretReconciler
}

func NewReplicationConfig(client client.Client, builder *builder.Builder, secretReconciler *secret.SecretReconciler) *ReplicationConfig {
	return &ReplicationConfig{
		Client:           client,
		builder:          builder,
		refResolver:      refresolver.New(client),
		secretReconciler: secretReconciler,
	}
}

func (r *ReplicationConfig) ConfigurePrimary(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, client *sqlClient.Client,
	podIndex int) error {
	if err := client.StopAllSlaves(ctx); err != nil {
		return fmt.Errorf("error stopping slaves: %v", err)
	}
	if err := client.ResetAllSlaves(ctx); err != nil {
		return fmt.Errorf("error resetting slave: %v", err)
	}
	if err := client.ResetSlavePos(ctx); err != nil {
		return fmt.Errorf("error resetting slave position: %v", err)
	}
	if err := client.DisableReadOnly(ctx); err != nil {
		return fmt.Errorf("error disabling read_only: %v", err)
	}
	if err := r.reconcilePrimarySql(ctx, mariadb, client); err != nil {
		return fmt.Errorf("error reconciling primary SQL: %v", err)
	}
	if err := r.configurePrimaryVars(ctx, mariadb, client, podIndex); err != nil {
		return fmt.Errorf("error configuring replication variables: %v", err)
	}
	return nil
}

func (r *ReplicationConfig) ConfigureReplica(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, client *sqlClient.Client,
	replicaPodIndex, primaryPodIndex int, resetSlavePos bool) error {
	if err := client.ResetMaster(ctx); err != nil {
		return fmt.Errorf("error resetting master: %v", err)
	}
	if err := client.StopAllSlaves(ctx); err != nil {
		return fmt.Errorf("error stopping slaves: %v", err)
	}
	if resetSlavePos {
		if err := client.ResetSlavePos(ctx); err != nil {
			return fmt.Errorf("error resetting slave position: %v", err)
		}
	}
	if err := client.EnableReadOnly(ctx); err != nil {
		return fmt.Errorf("error enabling read_only: %v", err)
	}
	if err := r.configureReplicaVars(ctx, mariadb, client, replicaPodIndex); err != nil {
		return fmt.Errorf("error configuring replication variables: %v", err)
	}
	if err := r.changeMaster(ctx, mariadb, client, primaryPodIndex); err != nil {
		return fmt.Errorf("error changing master: %v", err)
	}
	if err := client.StartSlave(ctx, connectionName); err != nil {
		return fmt.Errorf("error starting slave: %v", err)
	}
	return nil
}

func (r *ReplicationConfig) configurePrimaryVars(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, client *sqlClient.Client,
	primaryPodIndex int) error {
	kv := map[string]string{
		"sync_binlog":                  binaryFromBool(mariadb.Replication().SyncBinlog),
		"rpl_semi_sync_master_enabled": "ON",
		"rpl_semi_sync_master_timeout": func() string {
			return fmt.Sprint(mariadb.Replication().Replica.ConnectionTimeout.Milliseconds())
		}(),
		"rpl_semi_sync_slave_enabled": "OFF",
		"server_id":                   serverId(primaryPodIndex),
	}
	if mariadb.Replication().Replica.WaitPoint != nil {
		waitPoint, err := mariadb.Replication().Replica.WaitPoint.MariaDBFormat()
		if err != nil {
			return fmt.Errorf("error getting wait point: %v", err)
		}
		kv["rpl_semi_sync_master_wait_point"] = waitPoint
	}
	if err := client.SetSystemVariables(ctx, kv); err != nil {
		return fmt.Errorf("error setting replication vars: %v", err)
	}
	return nil
}

func (r *ReplicationConfig) configureReplicaVars(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB,
	client *sqlClient.Client, ordinal int) error {
	kv := map[string]string{
		"sync_binlog":                  binaryFromBool(mariadb.Replication().SyncBinlog),
		"rpl_semi_sync_master_enabled": "OFF",
		"rpl_semi_sync_slave_enabled":  "ON",
		"server_id":                    serverId(ordinal),
	}
	if err := client.SetSystemVariables(ctx, kv); err != nil {
		return fmt.Errorf("error setting replication vars: %v", err)
	}
	return nil
}

func (r *ReplicationConfig) changeMaster(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, client *sqlClient.Client,
	primaryPodIndex int) error {
	replPasswordRef := newReplPasswordRef(mariadb)

	password, err := r.refResolver.SecretKeyRef(ctx, replPasswordRef.SecretKeySelector, mariadb.Namespace)
	if err != nil {
		return fmt.Errorf("error getting replication password: %v", err)
	}

	gtid := mariadbv1alpha1.GtidCurrentPos
	if mariadb.Replication().Replica.Gtid != nil {
		gtid = *mariadb.Replication().Replica.Gtid
	}
	gtidString, err := gtid.MariaDBFormat()
	if err != nil {
		return fmt.Errorf("error getting GTID: %v", err)
	}

	changeMasterOpts := &sqlClient.ChangeMasterOpts{
		Connection: connectionName,
		// MariaDB 10.5 has a limitation of 60 characters in this host.
		Host: statefulset.PodShortFQDNWithService(
			mariadb.ObjectMeta,
			primaryPodIndex,
			mariadb.InternalServiceKey().Name,
		),
		User:     replUser,
		Password: password,
		Gtid:     gtidString,
		Retries:  *mariadb.Replication().Replica.ConnectionRetries,
	}
	if err := client.ChangeMaster(ctx, changeMasterOpts); err != nil {
		return fmt.Errorf("error changing master: %v", err)
	}
	return nil
}

func (r *ReplicationConfig) reconcilePrimarySql(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, client *sqlClient.Client) error {
	opts := userSqlOpts{
		username:   replUser,
		host:       replUserHost,
		privileges: []string{"REPLICATION REPLICA"},
	}
	if err := r.reconcileUserSql(ctx, mariadb, client, &opts); err != nil {
		return fmt.Errorf("error reconciling '%s' SQL user: %v", replUser, err)
	}
	return nil
}

type userSqlOpts struct {
	username   string
	host       string
	privileges []string
}

func (r *ReplicationConfig) reconcileUserSql(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, client *sqlClient.Client,
	opts *userSqlOpts) error {
	replPasswordRef := newReplPasswordRef(mariadb)
	var replPassword string
	var user *mariadbv1alpha1.User

	req := secret.PasswordRequest{
		Owner:    mariadb,
		Metadata: mariadb.Spec.InheritMetadata,
		Key: types.NamespacedName{
			Name:      replPasswordRef.Name,
			Namespace: mariadb.Namespace,
		},
		SecretKey: replPasswordRef.Key,
		Generate:  replPasswordRef.Generate,
	}
	replPassword, err := r.secretReconciler.ReconcilePassword(ctx, req)
	if err != nil {
		return fmt.Errorf("error reconciling replication passsword: %v", err)
	}

	accountName := formatAccountName(opts.username, opts.host)
	exists, err := client.UserExists(ctx, opts.username, opts.host)
	if err != nil {
		return fmt.Errorf("error checking if replication user exists: %v", err)
	}
	if exists {
		maxUserConnections := user.Spec.MaxUserConnections
		if err := client.AlterUser(ctx, opts.username, replPassword, maxUserConnections); err != nil {
			return fmt.Errorf("error altering replication user: %v", err)
		}
	} else {
		if err := client.CreateUser(ctx, accountName, sqlClient.WithIdentifiedBy(replPassword)); err != nil {
			return fmt.Errorf("error creating replication user: %v", err)
		}
	}
	if err := client.Grant(
		ctx,
		opts.privileges,
		"*",
		"*",
		accountName,
	); err != nil {
		return fmt.Errorf("error creating grant: %v", err)
	}
	return nil
}

func newReplPasswordRef(mariadb *mariadbv1alpha1.MariaDB) mariadbv1alpha1.GeneratedSecretKeyRef {
	if mariadb.Replication().Enabled && mariadb.Replication().Replica.ReplPasswordSecretKeyRef != nil {
		return *mariadb.Replication().Replica.ReplPasswordSecretKeyRef
	}
	return mariadbv1alpha1.GeneratedSecretKeyRef{
		SecretKeySelector: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: fmt.Sprintf("repl-password-%s", mariadb.Name),
			},
			Key: "password",
		},
		Generate: true,
	}
}

func serverId(index int) string {
	return fmt.Sprint(10 + index)
}

func binaryFromBool(b *bool) string {
	if b != nil && *b {
		return "1"
	}
	return "0"
}

func formatAccountName(username, host string) string {
	return fmt.Sprintf("'%s'@'%s'", username, host)
}
