package builder

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	galeraresources "github.com/mariadb-operator/mariadb-operator/pkg/controller/galera/resources"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (b *Builder) buildStsContainers(mariadb *mariadbv1alpha1.MariaDB) ([]corev1.Container, error) {
	mariadbContainer := buildContainer(mariadb.Spec.Image, mariadb.Spec.ImagePullPolicy, &mariadb.Spec.ContainerTemplate)
	mariadbContainer.Name = MariaDbContainerName
	mariadbContainer.Args = buildStsArgs(mariadb)
	mariadbContainer.Env = buildStsEnv(mariadb)
	mariadbContainer.Ports = buildStsPorts(mariadb)
	mariadbContainer.VolumeMounts = buildStsVolumeMounts(mariadb)
	mariadbContainer.LivenessProbe = buildStsLivenessProbe(mariadb)
	mariadbContainer.ReadinessProbe = buildStsReadinessProbe(mariadb)

	var containers []corev1.Container
	containers = append(containers, mariadbContainer)

	if mariadb.Galera().Enabled {
		containers = append(containers, b.buildGaleraAgentContainer(mariadb))
	}
	if mariadb.Spec.SidecarContainers != nil {
		for index, container := range mariadb.Spec.SidecarContainers {
			sidecarContainer := buildContainer(container.Image, container.ImagePullPolicy, &container.ContainerTemplate)
			sidecarContainer.Name = fmt.Sprintf("sidecar-%d", index)
			if sidecarContainer.Env == nil {
				sidecarContainer.Env = buildStsEnv(mariadb)
			}
			if sidecarContainer.VolumeMounts == nil {
				sidecarContainer.VolumeMounts = buildStsVolumeMounts(mariadb)
			}
			containers = append(containers, sidecarContainer)
		}
	}

	return containers, nil
}

func (b *Builder) buildGaleraAgentContainer(mariadb *mariadbv1alpha1.MariaDB) corev1.Container {
	agent := mariadb.Galera().Agent
	container := buildContainer(agent.Image, agent.ImagePullPolicy, &agent.ContainerTemplate)
	container.Name = AgentContainerName
	container.Ports = []corev1.ContainerPort{
		{
			Name:          galeraresources.AgentPortName,
			ContainerPort: *mariadb.Galera().Agent.Port,
		},
	}
	container.Args = func() []string {
		args := container.Args
		args = append(args, []string{
			fmt.Sprintf("--addr=:%d", *mariadb.Galera().Agent.Port),
			fmt.Sprintf("--config-dir=%s", galeraresources.GaleraConfigMountPath),
			fmt.Sprintf("--state-dir=%s", StorageMountPath),
			fmt.Sprintf("--graceful-shutdown-timeout=%s", mariadb.Galera().Agent.GracefulShutdownTimeout.Duration),
		}...)
		if mariadb.Galera().Recovery.Enabled {
			args = append(args, fmt.Sprintf("--recovery-timeout=%s", mariadb.Galera().Recovery.PodRecoveryTimeout.Duration))
		}
		if mariadb.Galera().Agent.KubernetesAuth.Enabled {
			args = append(args, []string{
				"--kubernetes-auth",
				fmt.Sprintf("--kubernetes-trusted-name=%s", b.env.MariadbOperatorName),
				fmt.Sprintf("--kubernetes-trusted-namespace=%s", b.env.MariadbOperatorNamespace),
			}...)
		}
		return args
	}()
	container.VolumeMounts = buildStsVolumeMounts(mariadb)
	container.LivenessProbe = func() *corev1.Probe {
		if container.LivenessProbe != nil {
			return container.LivenessProbe
		}
		return defaultAgentProbe(mariadb.Galera())
	}()
	container.ReadinessProbe = func() *corev1.Probe {
		if container.ReadinessProbe != nil {
			return container.ReadinessProbe
		}
		return defaultAgentProbe(mariadb.Galera())
	}()
	container.SecurityContext = func() *corev1.SecurityContext {
		if container.SecurityContext != nil {
			return container.SecurityContext
		}
		runAsUser := int64(0)
		return &corev1.SecurityContext{
			RunAsUser: &runAsUser,
		}
	}()

	return container
}

func buildStsInitContainers(mariadb *mariadbv1alpha1.MariaDB) []corev1.Container {
	initContainers := []corev1.Container{}
	if mariadb.Spec.BootstrapFrom != nil && *mariadb.Spec.BootstrapFrom.Type == "mariabackup" && mariadb.Spec.BootstrapFrom.BackupRef != nil {

		cmdsCheck := []string{
			"echo 'Checking if a restore already performed...'",
			"[ -e /var/lib/mysql/restore_success ] && { echo 'No restore required, exiting...'; exit 0; }",
		}

		cmdsRestore := []string{
			"echo 'Cleaning current data folder...'",
			"rm -rf /var/lib/mysql/* /var/lib/mysql/.my-healthcheck.cnf",
			"echo 'Creating a temporary data folder...'",
			"mkdir -p /var/lib/mysql/mariabackup",
			"echo 'Moving backup to the temporary data folder...'",
			fmt.Sprintf("rsync -rtva /backup/%s/ /var/lib/mysql/mariabackup/", *mariadb.Spec.BootstrapFrom.FileName),
			"echo 'Preparing backup...'",
			"mariadb-backup --prepare --target-dir=/var/lib/mysql/mariabackup",
			"echo 'Moving restored backup to the data directory...'",
			"mv /var/lib/mysql/mariabackup/* /var/lib/mysql",
			"echo 'Removing temporary folder...'",
			"rm -rf /var/lib/mysql/mariabackup/",
			"echo 'Creating a flag file to avoid multiple restoration after restart...'",
			"touch /var/lib/mysql/restore_success",
			"echo 'Changing ownership of the files to mysql...'",
			"chown -R mysql:mysql /var/lib/mysql",
		}

		initContainer := buildContainer(mariadb.Spec.Image, mariadb.Spec.ImagePullPolicy, &mariadbv1alpha1.ContainerTemplate{
			Command: []string{"bash", "-c"},
			Args:    []string{fmt.Sprintf("%s || %s", strings.Join(cmdsCheck, " && "), strings.Join(cmdsRestore, " && "))},
		})
		initContainer.Name = "restore-init"
		initContainer.Env = buildStsEnv(mariadb)
		VolumeMounts := buildStsVolumeMounts(mariadb)
		backupVolume := corev1.VolumeMount{
			Name:      "backup",
			MountPath: "/backup",
		}
		VolumeMounts = append(VolumeMounts, backupVolume)
		initContainer.VolumeMounts = VolumeMounts
		initContainers = append(initContainers, initContainer)

	}

	if mariadb.Spec.InitContainers != nil {
		for index, container := range mariadb.Spec.InitContainers {
			initContainer := buildContainer(container.Image, container.ImagePullPolicy, &container.ContainerTemplate)
			initContainer.Name = fmt.Sprintf("init-%d", index)
			if initContainer.Env == nil {
				initContainer.Env = buildStsEnv(mariadb)
			}
			if initContainer.VolumeMounts == nil {
				initContainer.VolumeMounts = buildStsVolumeMounts(mariadb)
			}
			initContainers = append(initContainers, initContainer)
		}
	}
	if mariadb.Galera().Enabled {
		initContainers = append(initContainers, buildGaleraInitContainer(mariadb))
	}
	return initContainers
}

func buildGaleraInitContainer(mariadb *mariadbv1alpha1.MariaDB) corev1.Container {
	if !mariadb.Galera().Enabled {
		return corev1.Container{}
	}
	init := mariadb.Galera().InitContainer
	container := buildContainer(init.Image, init.ImagePullPolicy, &init.ContainerTemplate)

	container.Name = InitContainerName
	container.Args = func() []string {
		args := container.Args
		args = append(args, []string{
			fmt.Sprintf("--config-dir=%s", galeraresources.GaleraConfigMountPath),
			fmt.Sprintf("--state-dir=%s", StorageMountPath),
			fmt.Sprintf("--mariadb-name=%s", mariadb.Name),
			fmt.Sprintf("--mariadb-namespace=%s", mariadb.Namespace),
		}...)
		return args
	}()
	container.Env = buildStsEnv(mariadb)
	container.VolumeMounts = buildStsVolumeMounts(mariadb)

	return container
}

func buildStsArgs(mariadb *mariadbv1alpha1.MariaDB) []string {
	if mariadb.Replication().Enabled {
		return []string{
			"--log-bin",
			fmt.Sprintf("--log-basename=%s", mariadb.Name),
		}
	}
	return nil
}

func buildStsEnv(mariadb *mariadbv1alpha1.MariaDB) []corev1.EnvVar {
	clusterName := os.Getenv("CLUSTER_NAME")
	if clusterName == "" {
		clusterName = "cluster.local"
	}

	env := []corev1.EnvVar{
		{
			Name:  "MYSQL_TCP_PORT",
			Value: strconv.Itoa(int(mariadb.Spec.Port)),
		},
		{
			Name:  "MARIADB_ROOT_HOST",
			Value: "%",
		},
		{
			Name:  "MYSQL_INITDB_SKIP_TZINFO",
			Value: "1",
		},
		{
			Name:  "CLUSTER_NAME",
			Value: clusterName,
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	}

	if mariadb.IsRootPasswordEmpty() {
		env = append(env, corev1.EnvVar{
			Name:  "MARIADB_ALLOW_EMPTY_ROOT_PASSWORD",
			Value: "yes",
		})
	} else {
		env = append(env, corev1.EnvVar{
			Name: "MARIADB_ROOT_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &mariadb.Spec.RootPasswordSecretKeyRef,
			},
		})
	}

	if !mariadb.Replication().Enabled {
		if mariadb.Spec.Database != nil {
			env = append(env, corev1.EnvVar{
				Name:  "MARIADB_DATABASE",
				Value: *mariadb.Spec.Database,
			})
		}
		if mariadb.Spec.Username != nil {
			env = append(env, corev1.EnvVar{
				Name:  "MARIADB_USER",
				Value: *mariadb.Spec.Username,
			})
		}
		if mariadb.Spec.PasswordSecretKeyRef != nil {
			env = append(env, corev1.EnvVar{
				Name: "MARIADB_PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: mariadb.Spec.PasswordSecretKeyRef,
				},
			})
		}
	}

	if mariadb.Spec.Env != nil {
		env = append(env, mariadb.Spec.Env...)
	}

	return env
}

func buildStsVolumeMounts(mariadb *mariadbv1alpha1.MariaDB) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      StorageVolume,
			MountPath: StorageMountPath,
		},
		{
			Name:      ConfigVolume,
			MountPath: ConfigMountPath,
		},
	}
	if mariadb.Galera().Enabled {
		volumeMounts = append(volumeMounts, []corev1.VolumeMount{
			{
				Name:      galeraresources.GaleraConfigVolume,
				MountPath: galeraresources.GaleraConfigMountPath,
			},
			{
				Name:      ServiceAccountVolume,
				MountPath: ServiceAccountMountPath,
			},
		}...)
	}
	if mariadb.Spec.VolumeMounts != nil {
		volumeMounts = append(volumeMounts, mariadb.Spec.VolumeMounts...)
	}
	return volumeMounts
}

func buildStsPorts(mariadb *mariadbv1alpha1.MariaDB) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{
			Name:          MariaDbPortName,
			ContainerPort: mariadb.Spec.Port,
		},
	}
	if mariadb.Galera().Enabled {
		ports = append(ports, []corev1.ContainerPort{
			{
				Name:          galeraresources.GaleraClusterPortName,
				ContainerPort: galeraresources.GaleraClusterPort,
			},
			{
				Name:          galeraresources.GaleraISTPortName,
				ContainerPort: galeraresources.GaleraISTPort,
			},
			{
				Name:          galeraresources.GaleraSSTPortName,
				ContainerPort: galeraresources.GaleraSSTPort,
			},
		}...)
	}
	return ports
}

func buildContainer(image string, pullPolicy corev1.PullPolicy, tpl *mariadbv1alpha1.ContainerTemplate) corev1.Container {
	container := corev1.Container{
		Image:           image,
		ImagePullPolicy: pullPolicy,
		Command:         tpl.Command,
		Args:            tpl.Args,
		Env:             tpl.Env,
		EnvFrom:         tpl.EnvFrom,
		VolumeMounts:    tpl.VolumeMounts,
		LivenessProbe:   tpl.LivenessProbe,
		ReadinessProbe:  tpl.ReadinessProbe,
		SecurityContext: tpl.SecurityContext,
	}
	if tpl.Resources != nil {
		container.Resources = *tpl.Resources
	}
	return container
}

func buildStsProbe(mariadb *mariadbv1alpha1.MariaDB, probe *corev1.Probe) *corev1.Probe {
	if mariadb.Galera().Enabled {
		galerProbe := *galeraStsProbe
		if probe != nil {
			p := *probe
			galerProbe.InitialDelaySeconds = p.InitialDelaySeconds
			galerProbe.TimeoutSeconds = p.TimeoutSeconds
			galerProbe.PeriodSeconds = p.PeriodSeconds
			galerProbe.SuccessThreshold = p.SuccessThreshold
			galerProbe.FailureThreshold = p.FailureThreshold
		}
		return &galerProbe
	}
	if probe != nil {
		return probe
	}
	return &defaultStsProbe
}

func buildStsLivenessProbe(mariadb *mariadbv1alpha1.MariaDB) *corev1.Probe {
	return buildStsProbe(mariadb, mariadb.Spec.LivenessProbe)
}

func buildStsReadinessProbe(mariadb *mariadbv1alpha1.MariaDB) *corev1.Probe {
	return buildStsProbe(mariadb, mariadb.Spec.ReadinessProbe)
}

var (
	defaultStsProbe = corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"bash",
					"-c",
					"mariadb -u root -p\"${MARIADB_ROOT_PASSWORD}\" -e \"SELECT 1;\"",
				},
			},
		},
		InitialDelaySeconds: 20,
		TimeoutSeconds:      5,
		PeriodSeconds:       10,
	}
	galeraStsProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{
					"bash",
					"-c",
					"mariadb -u root -p\"${MARIADB_ROOT_PASSWORD}\" -e \"SHOW STATUS LIKE 'wsrep_ready'\" | grep -c ON ",
				},
			},
		},
		InitialDelaySeconds: 60,
		TimeoutSeconds:      5,
		PeriodSeconds:       10,
	}
	defaultAgentProbe = func(galera mariadbv1alpha1.Galera) *corev1.Probe {
		return &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(int(*galera.Agent.Port)),
				},
			},
		}
	}
)
