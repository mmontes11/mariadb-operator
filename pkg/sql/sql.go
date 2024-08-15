package sql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/hashicorp/go-multierror"
	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	"github.com/mariadb-operator/mariadb-operator/pkg/environment"
	"github.com/mariadb-operator/mariadb-operator/pkg/refresolver"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
)

var (
	ErrWaitReplicaTimeout = errors.New("timeout waiting for replica to be synced")
)

type Opts struct {
	Username string
	Password string
	Host     string
	Port     int32
	Database string
	Params   map[string]string
	Timeout  *time.Duration
}

type Opt func(*Opts)

func WithUsername(username string) Opt {
	return func(o *Opts) {
		o.Username = username
	}
}

func WithPassword(password string) Opt {
	return func(o *Opts) {
		o.Password = password
	}
}

func WitHost(host string) Opt {
	return func(o *Opts) {
		o.Host = host
	}
}

func WithPort(port int32) Opt {
	return func(o *Opts) {
		o.Port = port
	}
}

func WithDatabase(database string) Opt {
	return func(o *Opts) {
		o.Database = database
	}
}

func WithParams(params map[string]string) Opt {
	return func(o *Opts) {
		o.Params = params
	}
}

func WithTimeout(d time.Duration) Opt {
	return func(o *Opts) {
		o.Timeout = &d
	}
}

type Client struct {
	db *sql.DB
}

func NewClient(clientOpts ...Opt) (*Client, error) {
	opts := Opts{}
	for _, setOpt := range clientOpts {
		setOpt(&opts)
	}
	dsn, err := BuildDSN(opts)
	if err != nil {
		return nil, fmt.Errorf("error building DSN: %v", err)
	}
	db, err := Connect(dsn)
	if err != nil {
		return nil, err
	}
	return &Client{
		db: db,
	}, nil
}

func NewClientWithMariaDB(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, refResolver *refresolver.RefResolver,
	clientOpts ...Opt) (*Client, error) {
	password, err := refResolver.SecretKeyRef(ctx, mariadb.Spec.RootPasswordSecretKeyRef.SecretKeySelector, mariadb.Namespace)
	if err != nil {
		return nil, fmt.Errorf("error reading root password secret: %v", err)
	}
	opts := []Opt{
		WithUsername("root"),
		WithPassword(password),
		WitHost(func() string {
			if mariadb.IsHAEnabled() {
				return statefulset.ServiceFQDNWithService(
					mariadb.ObjectMeta,
					mariadb.PrimaryServiceKey().Name,
				)
			}
			return statefulset.ServiceFQDN(mariadb.ObjectMeta)
		}()),
		WithPort(mariadb.Spec.Port),
	}
	opts = append(opts, clientOpts...)
	return NewClient(opts...)
}

func NewInternalClientWithPodIndex(ctx context.Context, mariadb *mariadbv1alpha1.MariaDB, refResolver *refresolver.RefResolver,
	podIndex int, clientOpts ...Opt) (*Client, error) {
	opts := []Opt{
		WitHost(
			statefulset.PodFQDNWithService(
				mariadb.ObjectMeta,
				podIndex,
				mariadb.InternalServiceKey().Name,
			),
		),
	}
	opts = append(opts, clientOpts...)
	return NewClientWithMariaDB(ctx, mariadb, refResolver, opts...)
}

func NewLocalClientWithPodEnv(ctx context.Context, env *environment.PodEnvironment, clientOpts ...Opt) (*Client, error) {
	port, err := env.Port()
	if err != nil {
		return nil, fmt.Errorf("error getting port: %v", err)
	}
	opts := []Opt{
		WithUsername("root"),
		WithPassword(env.MariadbRootPassword),
		WitHost("localhost"),
		WithPort(port),
	}
	opts = append(opts, clientOpts...)
	return NewClient(opts...)
}

func BuildDSN(opts Opts) (string, error) {
	if opts.Host == "" || opts.Port == 0 {
		return "", errors.New("invalid opts: host and port are mandatory")
	}
	config := mysql.NewConfig()
	config.Net = "tcp"
	config.Addr = fmt.Sprintf("%s:%d", opts.Host, opts.Port)

	if opts.Timeout != nil {
		config.Timeout = *opts.Timeout
	} else {
		config.Timeout = 5 * time.Second
	}
	if opts.Username != "" && opts.Password != "" {
		config.User = opts.Username
		config.Passwd = opts.Password
	}
	if opts.Database != "" {
		config.DBName = opts.Database
	}
	if opts.Params != nil {
		config.Params = opts.Params
	}

	return config.FormatDSN(), nil
}

func Connect(dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	return db, nil
}

func ConnectWithOpts(opts Opts) (*sql.DB, error) {
	dsn, err := BuildDSN(opts)
	if err != nil {
		return nil, fmt.Errorf("error building DNS: %v", err)
	}
	return Connect(dsn)
}

func (c *Client) Close() error {
	return c.db.Close()
}

func (c *Client) Exec(ctx context.Context, sql string, args ...any) error {
	_, err := c.db.ExecContext(ctx, sql, args...)
	return err
}

func (c *Client) ExecFlushingPrivileges(ctx context.Context, sql string, args ...any) error {
	var errBundle *multierror.Error
	if err := c.Exec(ctx, sql, args...); err != nil {
		errBundle = multierror.Append(errBundle, err)
	}
	if err := c.FlushPrivileges(ctx); err != nil {
		errBundle = multierror.Append(errBundle, err)
	}
	return errBundle.ErrorOrNil()
}

type CreateUserOpts struct {
	IdentifiedBy       string
	MaxUserConnections int32
}

type CreateUserOpt func(*CreateUserOpts)

func WithIdentifiedBy(password string) CreateUserOpt {
	return func(cuo *CreateUserOpts) {
		cuo.IdentifiedBy = password
	}
}

func WithMaxUserConnections(maxConns int32) CreateUserOpt {
	return func(cuo *CreateUserOpts) {
		cuo.MaxUserConnections = maxConns
	}
}

func (c *Client) CreateUser(ctx context.Context, accountName string, createUserOpts ...CreateUserOpt) error {
	opts := CreateUserOpts{}
	for _, setOpt := range createUserOpts {
		setOpt(&opts)
	}

	query := fmt.Sprintf("CREATE USER IF NOT EXISTS %s ", accountName)
	if opts.IdentifiedBy != "" {
		query += fmt.Sprintf("IDENTIFIED BY '%s' ", opts.IdentifiedBy)
	}
	if opts.MaxUserConnections > 0 {
		query += fmt.Sprintf("WITH MAX_USER_CONNECTIONS %d ", opts.MaxUserConnections)
	}
	if opts.IdentifiedBy == "" {
		query += "ACCOUNT LOCK PASSWORD EXPIRE "
	}
	query += ";"

	return c.ExecFlushingPrivileges(ctx, query)
}

func (c *Client) DropUser(ctx context.Context, accountName string) error {
	query := fmt.Sprintf("DROP USER IF EXISTS %s;", accountName)

	return c.ExecFlushingPrivileges(ctx, query)
}

func (c *Client) AlterUser(ctx context.Context, accountName, password string, maxConnections int32) error {
	query := fmt.Sprintf("ALTER USER '%s' IDENTIFIED BY '%s' MAX_USER_CONNECTIONS %d;",
		accountName, password, maxConnections)

	return c.ExecFlushingPrivileges(ctx, query)
}

func (c *Client) UserExists(ctx context.Context, username, host string) (bool, error) {
	row := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mysql.user WHERE user=? AND host=?", username, host)
	var count int
	if err := row.Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

type grantOpts struct {
	grantOption bool
}

type GrantOption func(*grantOpts)

func WithGrantOption() GrantOption {
	return func(o *grantOpts) {
		o.grantOption = true
	}
}

func (c *Client) Grant(
	ctx context.Context,
	privileges []string,
	database string,
	table string,
	accountName string,
	opts ...GrantOption,
) error {
	var grantOpts grantOpts
	for _, setOpt := range opts {
		setOpt(&grantOpts)
	}

	query := fmt.Sprintf("GRANT %s ON %s.%s TO %s ",
		strings.Join(privileges, ","),
		escapeWildcard(database),
		escapeWildcard(table),
		accountName,
	)
	if grantOpts.grantOption {
		query += "WITH GRANT OPTION "
	}
	query += ";"

	return c.ExecFlushingPrivileges(ctx, query)
}

func (c *Client) Revoke(
	ctx context.Context,
	privileges []string,
	database string,
	table string,
	accountName string,
	opts ...GrantOption,
) error {
	var grantOpts grantOpts
	for _, setOpt := range opts {
		setOpt(&grantOpts)
	}

	if grantOpts.grantOption {
		privileges = append(privileges, "GRANT OPTION")
	}
	query := fmt.Sprintf("REVOKE %s ON %s.%s FROM %s",
		strings.Join(privileges, ","),
		escapeWildcard(database),
		escapeWildcard(table),
		accountName,
	)

	return c.ExecFlushingPrivileges(ctx, query)
}

func (c *Client) FlushPrivileges(ctx context.Context) error {
	return c.Exec(ctx, "FLUSH PRIVILEGES;")
}

func escapeWildcard(s string) string {
	if s == "*" {
		return s
	}
	return fmt.Sprintf("`%s`", s)
}

type DatabaseOpts struct {
	CharacterSet string
	Collate      string
}

func (c *Client) CreateDatabase(ctx context.Context, database string, opts DatabaseOpts) error {
	query := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` ", database)
	if opts.CharacterSet != "" {
		query += fmt.Sprintf("CHARACTER SET = '%s' ", opts.CharacterSet)
	}
	if opts.Collate != "" {
		query += fmt.Sprintf("COLLATE = '%s' ", opts.Collate)
	}
	query += ";"

	return c.Exec(ctx, query)
}

func (c *Client) DropDatabase(ctx context.Context, database string) error {
	return c.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;", database))
}

func (c *Client) SystemVariable(ctx context.Context, variable string) (string, error) {
	sql := fmt.Sprintf("SELECT @@global.%s;", variable)
	row := c.db.QueryRowContext(ctx, sql)

	var val string
	if err := row.Scan(&val); err != nil {
		return "", nil
	}
	return val, nil
}

func (c *Client) IsSystemVariableEnabled(ctx context.Context, variable string) (bool, error) {
	val, err := c.SystemVariable(ctx, variable)
	if err != nil {
		return false, err
	}
	return val == "1" || val == "ON", nil
}

func (c *Client) SetSystemVariable(ctx context.Context, variable string, value string) error {
	sql := fmt.Sprintf("SET @@global.%s=%s;", variable, value)
	return c.Exec(ctx, sql)
}

func (c *Client) SetSystemVariables(ctx context.Context, keyVal map[string]string) error {
	for k, v := range keyVal {
		if err := c.SetSystemVariable(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) LockTablesWithReadLock(ctx context.Context) error {
	return c.Exec(ctx, "FLUSH TABLES WITH READ LOCK;")
}

func (c *Client) UnlockTables(ctx context.Context) error {
	return c.Exec(ctx, "UNLOCK TABLES;")
}

func (c *Client) EnableReadOnly(ctx context.Context) error {
	return c.SetSystemVariable(ctx, "read_only", "1")
}

func (c *Client) DisableReadOnly(ctx context.Context) error {
	return c.SetSystemVariable(ctx, "read_only", "0")
}

func (c *Client) ResetMaster(ctx context.Context) error {
	return c.Exec(ctx, "RESET MASTER;")
}

func (c *Client) StartSlave(ctx context.Context, connName string) error {
	sql := fmt.Sprintf("START SLAVE '%s';", connName)
	return c.Exec(ctx, sql)
}

func (c *Client) StopAllSlaves(ctx context.Context) error {
	return c.Exec(ctx, "STOP ALL SLAVES;")
}

func (c *Client) ResetAllSlaves(ctx context.Context) error {
	return c.Exec(ctx, "RESET SLAVE ALL;")
}

func (c *Client) WaitForReplicaGtid(ctx context.Context, gtid string, timeout time.Duration) error {
	sql := fmt.Sprintf("SELECT MASTER_GTID_WAIT('%s', %d);", gtid, int(timeout.Seconds()))
	row := c.db.QueryRowContext(ctx, sql)

	var result int
	if err := row.Scan(&result); err != nil {
		return fmt.Errorf("error scanning result: %v", err)
	}

	switch result {
	case 0:
		return nil
	case -1:
		return ErrWaitReplicaTimeout
	default:
		return fmt.Errorf("unexpected result: %d", result)
	}
}

type ChangeMasterOpts struct {
	Connection string
	Host       string
	User       string
	Password   string
	Gtid       string
	Retries    int
}

func (c *Client) ChangeMaster(ctx context.Context, opts *ChangeMasterOpts) error {
	tpl := createTpl("change-master.sql", `CHANGE MASTER '{{ .Connection }}' TO
MASTER_HOST='{{ .Host }}',
MASTER_USER='{{ .User }}',
MASTER_PASSWORD='{{ .Password }}',
MASTER_USE_GTID={{ .Gtid }},
MASTER_CONNECT_RETRY={{ .Retries }};
`)
	buf := new(bytes.Buffer)
	err := tpl.Execute(buf, opts)
	if err != nil {
		return fmt.Errorf("error generating change master query: %v", err)
	}
	return c.Exec(ctx, buf.String())
}

func (c *Client) ResetSlavePos(ctx context.Context) error {
	sql := fmt.Sprintf("SET @@global.%s='';", "gtid_slave_pos")
	return c.Exec(ctx, sql)
}

const statusVariableSql = "SELECT variable_value FROM information_schema.global_status WHERE variable_name=?;"

func (c *Client) StatusVariable(ctx context.Context, variable string) (string, error) {
	row := c.db.QueryRowContext(ctx, statusVariableSql, variable)
	var val string
	if err := row.Scan(&val); err != nil {
		return "", err
	}
	return val, nil
}

func (c *Client) StatusVariableInt(ctx context.Context, variable string) (int, error) {
	row := c.db.QueryRowContext(ctx, statusVariableSql, variable)
	var val int
	if err := row.Scan(&val); err != nil {
		return 0, err
	}
	return val, nil
}

func (c *Client) GaleraClusterSize(ctx context.Context) (int, error) {
	return c.StatusVariableInt(ctx, "wsrep_cluster_size")
}

func (c *Client) GaleraClusterStatus(ctx context.Context) (string, error) {
	return c.StatusVariable(ctx, "wsrep_cluster_status")
}

func (c *Client) GaleraLocalState(ctx context.Context) (string, error) {
	return c.StatusVariable(ctx, "wsrep_local_state_comment")
}

func (c *Client) MaxScaleConfigSyncVersion(ctx context.Context) (int, error) {
	row := c.db.QueryRowContext(ctx, "SELECT version FROM maxscale_config")
	var version int
	if err := row.Scan(&version); err != nil {
		return 0, err
	}
	return version, nil
}

func (c *Client) TruncateMaxScaleConfig(ctx context.Context) error {
	return c.Exec(ctx, "TRUNCATE TABLE maxscale_config")
}

func (c *Client) DropMaxScaleConfig(ctx context.Context) error {
	return c.Exec(ctx, "DROP TABLE maxscale_config")
}

func createTpl(name, t string) *template.Template {
	return template.Must(template.New(name).Parse(t))
}
