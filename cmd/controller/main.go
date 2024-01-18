/*
Copyright © 2023 Martín Montes <martin11lrx@gmail.com>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	backupcmd "github.com/mariadb-operator/mariadb-operator/cmd/backup"
	"github.com/mariadb-operator/mariadb-operator/controller"
	"github.com/mariadb-operator/mariadb-operator/pkg/builder"
	condition "github.com/mariadb-operator/mariadb-operator/pkg/condition"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/batch"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/configmap"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/deployment"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/endpoints"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/galera"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/rbac"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/replication"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/secret"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/service"
	"github.com/mariadb-operator/mariadb-operator/pkg/controller/servicemonitor"
	"github.com/mariadb-operator/mariadb-operator/pkg/discovery"
	"github.com/mariadb-operator/mariadb-operator/pkg/environment"
	"github.com/mariadb-operator/mariadb-operator/pkg/log"
	"github.com/mariadb-operator/mariadb-operator/pkg/metadata"
	"github.com/mariadb-operator/mariadb-operator/pkg/refresolver"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	scheme            = runtime.NewScheme()
	setupLog          = ctrl.Log.WithName("setup")
	metricsAddr       string
	healthAddr        string
	logLevel          string
	logTimeEncoder    string
	logDev            bool
	leaderElect       bool
	requeueConnection time.Duration
	requeueSql        time.Duration
	requeueSqlJob     time.Duration
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(mariadbv1alpha1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))

	rootCmd.PersistentFlags().StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	rootCmd.PersistentFlags().StringVar(&healthAddr, "health-addr", ":8081", "The address the probe endpoint binds to.")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level to use, one of: "+
		"debug, info, warn, error, dpanic, panic, fatal.")
	rootCmd.PersistentFlags().StringVar(&logTimeEncoder, "log-time-encoder", "epoch", "Log time encoder to use, one of: "+
		"epoch, millis, nano, iso8601, rfc3339 or rfc3339nano")
	rootCmd.PersistentFlags().BoolVar(&logDev, "log-dev", false, "Enable development logs.")
	rootCmd.PersistentFlags().BoolVar(&leaderElect, "leader-elect", false, "Enable leader election for controller manager.")
	rootCmd.Flags().DurationVar(&requeueConnection, "requeue-connection", 30*time.Second, "The interval at which Connections are requeued.")
	rootCmd.Flags().DurationVar(&requeueSql, "requeue-sql", 30*time.Second, "The interval at which SQL objects are requeued.")
	rootCmd.Flags().DurationVar(&requeueSqlJob, "requeue-sqljob", 5*time.Second, "The interval at which SqlJobs are requeued.")
}

var rootCmd = &cobra.Command{
	Use:   "mariadb-operator",
	Short: "MariaDB operator.",
	Long:  `Run and operate MariaDB in a cloud native way.`,
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		log.SetupLogger(logLevel, logTimeEncoder, logDev)

		ctx, cancel := signal.NotifyContext(context.Background(), []os.Signal{
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGKILL,
			syscall.SIGHUP,
			syscall.SIGQUIT}...,
		)
		defer cancel()

		restConfig, err := ctrl.GetConfig()
		if err != nil {
			setupLog.Error(err, "Unable to get config")
			os.Exit(1)
		}
		env, err := environment.GetEnvironment(ctx)
		if err != nil {
			setupLog.Error(err, "Error getting environment")
			os.Exit(1)
		}

		mgrOpts := ctrl.Options{
			Scheme: scheme,
			Metrics: metricsserver.Options{
				BindAddress: metricsAddr,
			},
			HealthProbeBindAddress: healthAddr,
			LeaderElection:         leaderElect,
			LeaderElectionID:       "mariadb-operator.mmontes.io",
		}
		if env.WatchNamespace != "" {
			namespaces, err := env.WatchNamespaces()
			if err != nil {
				setupLog.Error(err, "Error getting namespaces to watch")
				os.Exit(1)
			}
			setupLog.Info("Watching namespaces", "namespaces", namespaces)
			mgrOpts.Cache.DefaultNamespaces = make(map[string]cache.Config, len(namespaces))
			for _, ns := range namespaces {
				mgrOpts.Cache.DefaultNamespaces[ns] = cache.Config{}
			}
		} else {
			setupLog.Info("Watching all namespaces")
		}
		mgr, err := ctrl.NewManager(restConfig, mgrOpts)
		if err != nil {
			setupLog.Error(err, "Unable to start manager")
			os.Exit(1)
		}

		client := mgr.GetClient()
		scheme := mgr.GetScheme()
		galeraRecorder := mgr.GetEventRecorderFor("galera")
		replRecorder := mgr.GetEventRecorderFor("replication")

		discoveryClient, err := discovery.NewDiscoveryClient(restConfig)
		if err != nil {
			setupLog.Error(err, "Error getting discovery client")
			os.Exit(1)
		}

		builder := builder.NewBuilder(scheme, env)
		refResolver := refresolver.New(client)

		conditionReady := condition.NewReady()
		conditionComplete := condition.NewComplete(client)

		configMapReconciler := configmap.NewConfigMapReconciler(client, builder)
		secretReconciler := secret.NewSecretReconciler(client, builder)
		serviceReconciler := service.NewServiceReconciler(client)
		endpointsReconciler := endpoints.NewEndpointsReconciler(client, builder)
		batchReconciler := batch.NewBatchReconciler(client, builder)
		rbacReconciler := rbac.NewRBACReconiler(client, builder)
		deployReconciler := deployment.NewDeploymentReconciler(client)
		svcMonitorReconciler := servicemonitor.NewServiceMonitorReconciler(client)

		replConfig := replication.NewReplicationConfig(client, builder, secretReconciler)
		replicationReconciler := replication.NewReplicationReconciler(
			client,
			replRecorder,
			builder,
			replConfig,
			replication.WithRefResolver(refResolver),
			replication.WithSecretReconciler(secretReconciler),
			replication.WithServiceReconciler(serviceReconciler),
		)
		galeraReconciler := galera.NewGaleraReconciler(
			client,
			galeraRecorder,
			env,
			builder,
			galera.WithRefResolver(refResolver),
			galera.WithConfigMapReconciler(configMapReconciler),
			galera.WithServiceReconciler(serviceReconciler),
		)

		podReplicationController := controller.NewPodController(
			client,
			refResolver,
			controller.NewPodReplicationController(
				client,
				replRecorder,
				builder,
				refResolver,
				replConfig,
			),
			[]string{
				metadata.MariadbAnnotation,
				metadata.ReplicationAnnotation,
			},
		)
		podGaleraController := controller.NewPodController(
			client,
			refResolver,
			controller.NewPodGaleraController(client, galeraRecorder),
			[]string{
				metadata.MariadbAnnotation,
				metadata.GaleraAnnotation,
			},
		)

		if err = (&controller.MariaDBReconciler{
			Client:   client,
			Scheme:   scheme,
			Recorder: mgr.GetEventRecorderFor("mariadb"),

			Environment:     env,
			Builder:         builder,
			RefResolver:     refResolver,
			ConditionReady:  conditionReady,
			DiscoveryClient: discoveryClient,

			ConfigMapReconciler:      configMapReconciler,
			SecretReconciler:         secretReconciler,
			ServiceReconciler:        serviceReconciler,
			EndpointsReconciler:      endpointsReconciler,
			RBACReconciler:           rbacReconciler,
			DeploymentReconciler:     deployReconciler,
			ServiceMonitorReconciler: svcMonitorReconciler,

			ReplicationReconciler: replicationReconciler,
			GaleraReconciler:      galeraReconciler,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "MariaDB")
			os.Exit(1)
		}
		if err = (&controller.BackupReconciler{
			Client:            client,
			Scheme:            scheme,
			Builder:           builder,
			RefResolver:       refResolver,
			ConditionComplete: conditionComplete,
			BatchReconciler:   batchReconciler,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "Backup")
			os.Exit(1)
		}

		if err = (&controller.MariaBackupReconciler{
			Client:            client,
			Scheme:            scheme,
			Builder:           builder,
			RefResolver:       refResolver,
			ConditionComplete: conditionComplete,
			BatchReconciler:   batchReconciler,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "MariaBackup")
			os.Exit(1)
		}

		if err = (&controller.RestoreReconciler{
			Client:            client,
			Scheme:            scheme,
			Builder:           builder,
			RefResolver:       refResolver,
			ConditionComplete: conditionComplete,
			BatchReconciler:   batchReconciler,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "restore")
			os.Exit(1)
		}
		if err = controller.NewUserReconciler(client, refResolver, conditionReady, requeueSql).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "User")
			os.Exit(1)
		}
		if err = controller.NewGrantReconciler(client, refResolver, conditionReady, requeueSql).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "Grant")
			os.Exit(1)
		}
		if err = controller.NewDatabaseReconciler(client, refResolver, conditionReady, requeueSql).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "Database")
			os.Exit(1)
		}
		if err = (&controller.ConnectionReconciler{
			Client:          client,
			Scheme:          scheme,
			Builder:         builder,
			RefResolver:     refResolver,
			ConditionReady:  conditionReady,
			RequeueInterval: requeueConnection,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "Connection")
			os.Exit(1)
		}
		if err = (&controller.SqlJobReconciler{
			Client:              client,
			Scheme:              scheme,
			Builder:             builder,
			RefResolver:         refResolver,
			ConfigMapReconciler: configMapReconciler,
			ConditionComplete:   conditionComplete,
			RequeueInterval:     requeueSqlJob,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "SqlJob")
			os.Exit(1)
		}
		if err = podReplicationController.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "PodReplication")
			os.Exit(1)
		}
		if err := podGaleraController.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "PodGalera")
			os.Exit(1)
		}
		if err = (&controller.StatefulSetGaleraReconciler{
			Client:      client,
			RefResolver: refResolver,
			Recorder:    galeraRecorder,
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "Unable to create controller", "controller", "StatefulSetGalera")
			os.Exit(1)
		}

		setupLog.Info("Starting manager")
		if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
			setupLog.Error(err, "Error running manager")
			os.Exit(1)
		}
	},
}

func main() {
	rootCmd.AddCommand(certControllerCmd)
	rootCmd.AddCommand(webhookCmd)
	rootCmd.AddCommand(backupcmd.RootCmd)

	cobra.CheckErr(rootCmd.Execute())
}
