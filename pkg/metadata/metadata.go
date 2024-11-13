package metadata

var (
	WatchLabel = "k8s.mariadb.com/watch"

	ReplicationAnnotation = "k8s.mariadb.com/replication"
	GaleraAnnotation      = "k8s.mariadb.com/galera"
	MariadbAnnotation     = "k8s.mariadb.com/mariadb"

	ConfigAnnotation       = "k8s.mariadb.com/config"
	ConfigGaleraAnnotation = "k8s.mariadb.com/config-galera"

	WebhookConfigAnnotation = "k8s.mariadb.com/webhook"
)
