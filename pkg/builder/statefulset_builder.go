package builder

import (
	"fmt"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	labels "github.com/mariadb-operator/mariadb-operator/pkg/builder/labels"
	metadata "github.com/mariadb-operator/mariadb-operator/pkg/builder/metadata"
	galeraresources "github.com/mariadb-operator/mariadb-operator/pkg/controller/galera/resources"
	annotation "github.com/mariadb-operator/mariadb-operator/pkg/metadata"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	StorageVolume    = "storage"
	StorageMountPath = "/var/lib/mysql"
	ConfigVolume     = "config"
	ConfigMountPath  = "/etc/mysql/conf.d"
	BackupVolume     = "backup"
	BackupMountPath  = "/backup"

	ServiceAccountVolume    = "serviceaccount"
	ServiceAccountMountPath = "/var/run/secrets/kubernetes.io/serviceaccount"

	MariaDbContainerName = "mariadb"
	MariaDbPortName      = "mariadb"

	InitContainerName  = "init"
	AgentContainerName = "agent"
)

func PVCKey(mariadb *mariadbv1alpha1.MariaDB) types.NamespacedName {
	podName := statefulset.PodName(mariadb.ObjectMeta, 0)
	if mariadb.Replication().Enabled {
		podName = statefulset.PodName(mariadb.ObjectMeta, *mariadb.Replication().Primary.PodIndex)
	}
	return types.NamespacedName{
		Name:      fmt.Sprintf("%s-%s", StorageVolume, podName),
		Namespace: mariadb.Namespace,
	}
}

func (b *Builder) BuildStatefulSet(mariadb *mariadbv1alpha1.MariaDB, key types.NamespacedName) (*appsv1.StatefulSet, error) {
	objMeta :=
		metadata.NewMetadataBuilder(key).
			WithMariaDB(mariadb).
			WithAnnotations(buildHAAnnotations(mariadb)).
			Build()
	selectorLabels :=
		labels.NewLabelsBuilder().
			WithMariaDBSelectorLabels(mariadb).
			Build()
	podTemplate, err := b.buildStsPodTemplate(mariadb, selectorLabels)
	if err != nil {
		return nil, fmt.Errorf("error building pod template: %v", err)
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: objMeta,
		Spec: appsv1.StatefulSetSpec{
			ServiceName:         mariadb.InternalServiceKey().Name,
			Replicas:            &mariadb.Spec.Replicas,
			PodManagementPolicy: buildStsPodManagementPolicy(mariadb),
			UpdateStrategy:      buildStsUpdateStrategy(mariadb),
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template:             *podTemplate,
			VolumeClaimTemplates: buildStsVolumeClaimTemplates(mariadb),
		},
	}
	if err := controllerutil.SetControllerReference(mariadb, sts, b.scheme); err != nil {
		return nil, fmt.Errorf("error setting controller reference to StatefulSet: %v", err)
	}
	return sts, nil
}

func (b *Builder) buildStsPodTemplate(mariadb *mariadbv1alpha1.MariaDB, labels map[string]string) (*corev1.PodTemplateSpec, error) {
	containers, err := b.buildStsContainers(mariadb)
	if err != nil {
		return nil, fmt.Errorf("error building MariaDB containers: %v", err)
	}
	objMeta :=
		metadata.NewMetadataBuilder(client.ObjectKeyFromObject(mariadb)).
			WithMariaDB(mariadb).
			WithLabels(labels).
			WithAnnotations(mariadb.Spec.PodAnnotations).
			WithAnnotations(buildHAAnnotations(mariadb)).
			Build()
	serviceAccount := buildStsServiceAccountName(mariadb)
	return &corev1.PodTemplateSpec{
		ObjectMeta: objMeta,
		Spec: corev1.PodSpec{
			AutomountServiceAccountToken: ptr.To(false),
			ServiceAccountName:           serviceAccount,
			InitContainers:               buildStsInitContainers(mariadb),
			Containers:                   containers,
			ImagePullSecrets:             mariadb.Spec.ImagePullSecrets,
			Volumes:                      buildStsVolumes(mariadb),
			SecurityContext:              mariadb.Spec.PodSecurityContext,
			Affinity:                     mariadb.Spec.Affinity,
			NodeSelector:                 mariadb.Spec.NodeSelector,
			Tolerations:                  mariadb.Spec.Tolerations,
			PriorityClassName:            buildPriorityClass(mariadb),
			TopologySpreadConstraints:    mariadb.Spec.TopologySpreadConstraints,
		},
	}, nil
}

func buildStsPodManagementPolicy(mariadb *mariadbv1alpha1.MariaDB) appsv1.PodManagementPolicyType {
	if mariadb.IsHAEnabled() {
		return appsv1.ParallelPodManagement
	}
	return appsv1.OrderedReadyPodManagement
}

func buildStsUpdateStrategy(mariadb *mariadbv1alpha1.MariaDB) appsv1.StatefulSetUpdateStrategy {
	if mariadb.Spec.UpdateStrategy != nil {
		return *mariadb.Spec.UpdateStrategy
	}
	return appsv1.StatefulSetUpdateStrategy{
		Type: appsv1.RollingUpdateStatefulSetStrategyType,
	}
}

func buildStsVolumeClaimTemplates(mariadb *mariadbv1alpha1.MariaDB) []corev1.PersistentVolumeClaim {
	var pvcs []corev1.PersistentVolumeClaim

	if !mariadb.IsEphemeralStorageEnabled() {
		vctpl := mariadb.Spec.VolumeClaimTemplate
		pvcs = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:        StorageVolume,
					Labels:      vctpl.Labels,
					Annotations: vctpl.Annotations,
				},
				Spec: vctpl.PersistentVolumeClaimSpec,
			},
		}
	}

	if mariadb.Galera().Enabled {
		vctpl := *mariadb.Galera().VolumeClaimTemplate
		pvcs = append(pvcs, corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:        galeraresources.GaleraConfigVolume,
				Labels:      vctpl.Labels,
				Annotations: vctpl.Annotations,
			},
			Spec: vctpl.PersistentVolumeClaimSpec,
		})
	}
	return pvcs
}

func buildStsServiceAccountName(mariadb *mariadbv1alpha1.MariaDB) (serviceAccount string) {
	if mariadb.Spec.ServiceAccountName != nil {
		return *mariadb.Spec.ServiceAccountName
	}
	return mariadb.Name
}

func buildStsVolumes(mariadb *mariadbv1alpha1.MariaDB) []corev1.Volume {

	configVolume := corev1.Volume{
		Name: ConfigVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
	if mariadb.Spec.MyCnfConfigMapKeyRef != nil {
		configVolume = corev1.Volume{
			Name: ConfigVolume,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: mariadb.Spec.MyCnfConfigMapKeyRef.Name,
					},
					Items: []corev1.KeyToPath{
						{
							Key:  mariadb.Spec.MyCnfConfigMapKeyRef.Key,
							Path: "my.cnf",
						},
					},
				},
			},
		}
	}
	volumes := []corev1.Volume{
		configVolume,
	}

	if mariadb.Spec.BootstrapFrom != nil && *mariadb.Spec.BootstrapFrom.Type == "mariabackup" && mariadb.Spec.BootstrapFrom.BackupRef != nil {

		backupVolume := corev1.Volume{
			Name: BackupVolume,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: mariadb.Spec.BootstrapFrom.BackupRef.Name,
					ReadOnly:  true,
				},
			},
		}

		volumes = append(volumes, backupVolume)
	}

	if mariadb.Galera().Enabled {
		volumes = append(volumes, corev1.Volume{
			Name: ServiceAccountVolume,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Path: "token",
							},
						},
						{
							ConfigMap: &corev1.ConfigMapProjection{
								Items: []corev1.KeyToPath{
									{
										Key:  "ca.crt",
										Path: "ca.crt",
									},
								},
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "kube-root-ca.crt",
								},
							},
						},
						{
							DownwardAPI: &corev1.DownwardAPIProjection{
								Items: []corev1.DownwardAPIVolumeFile{
									{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
										Path: "namespace",
									},
								},
							},
						},
					},
				},
			},
		})
	}
	if mariadb.IsEphemeralStorageEnabled() {
		volumes = append(volumes, corev1.Volume{
			Name: StorageVolume,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}
	if mariadb.Spec.Volumes != nil {
		volumes = append(volumes, mariadb.Spec.Volumes...)
	}
	return volumes
}

func buildHAAnnotations(mariadb *mariadbv1alpha1.MariaDB) map[string]string {
	var annotations map[string]string
	if mariadb.IsHAEnabled() {
		annotations = map[string]string{
			annotation.MariadbAnnotation: mariadb.Name,
		}
		if mariadb.Replication().Enabled {
			annotations[annotation.ReplicationAnnotation] = ""
		}
		if mariadb.Galera().Enabled {
			annotations[annotation.GaleraAnnotation] = ""
		}
	}
	return annotations
}

func buildPriorityClass(mariadb *mariadbv1alpha1.MariaDB) string {
	if mariadb.Spec.PodTemplate.PriorityClassName != nil {
		return *mariadb.Spec.PodTemplate.PriorityClassName
	}
	return ""
}
