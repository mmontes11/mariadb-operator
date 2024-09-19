package controller

import (
	"time"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	"github.com/mariadb-operator/mariadb-operator/pkg/builder"
	labels "github.com/mariadb-operator/mariadb-operator/pkg/builder/labels"
	"github.com/mariadb-operator/mariadb-operator/pkg/statefulset"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("MariaDB Galera spec", func() {
	It("should default", func() {
		By("Creating MariaDB")
		key := types.NamespacedName{
			Name:      "mariadb-galera-default",
			Namespace: testNamespace,
		}
		mdb := mariadbv1alpha1.MariaDB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: mariadbv1alpha1.MariaDBSpec{
				Galera: &mariadbv1alpha1.Galera{
					Enabled: true,
				},
				Replicas: 3,
				Storage: mariadbv1alpha1.Storage{
					Size: ptr.To(resource.MustParse("300Mi")),
				},
			},
		}
		Expect(k8sClient.Create(testCtx, &mdb)).To(Succeed())
		DeferCleanup(func() {
			deleteMariadb(key)
		})

		By("Expecting to eventually default")
		Eventually(func(g Gomega) bool {
			if err := k8sClient.Get(testCtx, client.ObjectKeyFromObject(&mdb), &mdb); err != nil {
				return false
			}
			g.Expect(mdb.Spec.Galera).ToNot(BeZero())
			g.Expect(mdb.Spec.Galera.Primary).ToNot(BeZero())
			g.Expect(mdb.Spec.Galera.SST).ToNot(BeZero())
			g.Expect(mdb.Spec.Galera.ReplicaThreads).ToNot(BeZero())
			g.Expect(mdb.Spec.Galera.Recovery).ToNot(BeZero())
			g.Expect(mdb.Spec.Galera.InitContainer).ToNot(BeZero())
			g.Expect(mdb.Spec.Galera.Config).ToNot(BeZero())
			return true
		}, testTimeout, testInterval).Should(BeTrue())
	})
})

var _ = Describe("MariaDB Galera", Ordered, func() {
	var (
		key = types.NamespacedName{
			Name:      "mariadb-galera",
			Namespace: testNamespace,
		}
		mdb *mariadbv1alpha1.MariaDB
	)

	BeforeAll(func() {
		mdb = &mariadbv1alpha1.MariaDB{
			ObjectMeta: metav1.ObjectMeta{
				Name:      key.Name,
				Namespace: key.Namespace,
			},
			Spec: mariadbv1alpha1.MariaDBSpec{
				RootPasswordSecretKeyRef: mariadbv1alpha1.GeneratedSecretKeyRef{
					SecretKeySelector: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: testPwdKey.Name,
						},
						Key: testPwdSecretKey,
					},
				},
				Username: &testUser,
				PasswordSecretKeyRef: &mariadbv1alpha1.GeneratedSecretKeyRef{
					SecretKeySelector: corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: testPwdKey.Name,
						},
						Key: testPwdSecretKey,
					},
				},
				Database: &testDatabase,
				MyCnf: ptr.To(`[mariadb]
					bind-address=*
					default_storage_engine=InnoDB
					binlog_format=row
					innodb_autoinc_lock_mode=2
					max_allowed_packet=256M
					`),
				Galera: &mariadbv1alpha1.Galera{
					Enabled: true,
					GaleraSpec: mariadbv1alpha1.GaleraSpec{
						Primary: mariadbv1alpha1.PrimaryGalera{
							PodIndex:          ptr.To(0),
							AutomaticFailover: ptr.To(true),
						},
						Recovery: &mariadbv1alpha1.GaleraRecovery{
							Enabled:               true,
							ClusterHealthyTimeout: ptr.To(metav1.Duration{Duration: 10 * time.Second}),
						},
						Config: mariadbv1alpha1.GaleraConfig{
							ReuseStorageVolume: ptr.To(false),
							VolumeClaimTemplate: &mariadbv1alpha1.VolumeClaimTemplate{
								PersistentVolumeClaimSpec: corev1.PersistentVolumeClaimSpec{
									Resources: corev1.VolumeResourceRequirements{
										Requests: corev1.ResourceList{
											"storage": resource.MustParse("100Mi"),
										},
									},
									AccessModes: []corev1.PersistentVolumeAccessMode{
										corev1.ReadWriteOnce,
									},
								},
							},
						},
						InitJob: &mariadbv1alpha1.Job{
							Metadata: &mariadbv1alpha1.Metadata{
								Labels: map[string]string{
									"sidecar.istio.io/inject": "false",
								},
							},
						},
						ReplicaThreads: 1,
					},
				},
				Replicas: 3,
				Storage: mariadbv1alpha1.Storage{
					Size:                ptr.To(resource.MustParse("300Mi")),
					StorageClassName:    "standard-resize",
					ResizeInUseVolumes:  ptr.To(true),
					WaitForVolumeResize: ptr.To(true),
				},
				Service: &mariadbv1alpha1.ServiceTemplate{
					Type: corev1.ServiceTypeLoadBalancer,
					Metadata: &mariadbv1alpha1.Metadata{
						Annotations: map[string]string{
							"metallb.universe.tf/loadBalancerIPs": testCidrPrefix + ".0.150",
						},
					},
				},
				Connection: &mariadbv1alpha1.ConnectionTemplate{
					SecretName: ptr.To("mdb-galera-conn"),
					SecretTemplate: &mariadbv1alpha1.SecretTemplate{
						Key: &testConnSecretKey,
					},
				},
				PrimaryService: &mariadbv1alpha1.ServiceTemplate{
					Type: corev1.ServiceTypeLoadBalancer,
					Metadata: &mariadbv1alpha1.Metadata{
						Annotations: map[string]string{
							"metallb.universe.tf/loadBalancerIPs": testCidrPrefix + ".0.160",
						},
					},
				},
				PrimaryConnection: &mariadbv1alpha1.ConnectionTemplate{
					SecretName: ptr.To("mdb-galera-conn-primary"),
					SecretTemplate: &mariadbv1alpha1.SecretTemplate{
						Key: &testConnSecretKey,
					},
				},
				SecondaryService: &mariadbv1alpha1.ServiceTemplate{
					Type: corev1.ServiceTypeLoadBalancer,
					Metadata: &mariadbv1alpha1.Metadata{
						Annotations: map[string]string{
							"metallb.universe.tf/loadBalancerIPs": testCidrPrefix + ".0.161",
						},
					},
				},
				SecondaryConnection: &mariadbv1alpha1.ConnectionTemplate{
					SecretName: ptr.To("mdb-galera-conn-secondary"),
					SecretTemplate: &mariadbv1alpha1.SecretTemplate{
						Key: &testConnSecretKey,
					},
				},
				UpdateStrategy: mariadbv1alpha1.UpdateStrategy{
					Type: mariadbv1alpha1.ReplicasFirstPrimaryLastUpdateType,
				},
			},
		}
		applyMariadbTestConfig(mdb)

		By("Creating MariaDB Galera")
		Expect(k8sClient.Create(testCtx, mdb)).To(Succeed())
		DeferCleanup(func() {
			deleteMariadb(key)
		})
	})

	It("should reconcile", func() {
		By("Expecting MariaDB to be ready eventually")
		Eventually(func() bool {
			if err := k8sClient.Get(testCtx, key, mdb); err != nil {
				return false
			}
			return mdb.IsReady() && mdb.HasGaleraConfiguredCondition() && mdb.HasGaleraReadyCondition()
		}, testHighTimeout, testInterval).Should(BeTrue())

		By("Expecting to create a StatefulSet")
		var sts appsv1.StatefulSet
		Expect(k8sClient.Get(testCtx, key, &sts)).To(Succeed())

		By("Expecting to create a Service")
		var svc corev1.Service
		Expect(k8sClient.Get(testCtx, key, &svc)).To(Succeed())

		By("Expecting to create a primary Service")
		Expect(k8sClient.Get(testCtx, mdb.PrimaryServiceKey(), &svc)).To(Succeed())
		Expect(svc.Spec.Selector["statefulset.kubernetes.io/pod-name"]).To(Equal(statefulset.PodName(mdb.ObjectMeta, 0)))

		By("Expecting to create a secondary Service")
		Expect(k8sClient.Get(testCtx, mdb.SecondaryServiceKey(), &svc)).To(Succeed())

		By("Expecting Connection to be ready eventually")
		Eventually(func() bool {
			var conn mariadbv1alpha1.Connection
			if err := k8sClient.Get(testCtx, key, &conn); err != nil {
				return false
			}
			return conn.IsReady()
		}, testTimeout, testInterval).Should(BeTrue())

		By("Expecting primary Connection to be ready eventually")
		Eventually(func() bool {
			var conn mariadbv1alpha1.Connection
			if err := k8sClient.Get(testCtx, mdb.PrimaryConnectioneKey(), &conn); err != nil {
				return false
			}
			return conn.IsReady()
		}, testTimeout, testInterval).Should(BeTrue())

		By("Expecting secondary Connection to be ready eventually")
		Eventually(func() bool {
			var conn mariadbv1alpha1.Connection
			if err := k8sClient.Get(testCtx, mdb.SecondaryConnectioneKey(), &conn); err != nil {
				return false
			}
			return conn.IsReady()
		}, testTimeout, testInterval).Should(BeTrue())

		By("Expecting to create secondary Endpoints")
		var endpoints corev1.Endpoints
		Expect(k8sClient.Get(testCtx, mdb.SecondaryServiceKey(), &endpoints)).To(Succeed())
		Expect(endpoints.Subsets).To(HaveLen(1))
		Expect(endpoints.Subsets[0].Addresses).To(HaveLen(int(mdb.Spec.Replicas) - 1))

		By("Expecting to create a PodDisruptionBudget")
		var pdb policyv1.PodDisruptionBudget
		Expect(k8sClient.Get(testCtx, key, &pdb)).To(Succeed())

		By("Expecting to eventually update MariaDB primary")
		podIndex := 1
		Eventually(func(g Gomega) bool {
			if err := k8sClient.Get(testCtx, key, mdb); err != nil {
				return false
			}
			mdb.Spec.Galera.Primary.PodIndex = ptr.To(podIndex)
			g.Expect(k8sClient.Update(testCtx, mdb)).To(Succeed())
			return true
		}, testTimeout, testInterval).Should(BeTrue())

		By("Expecting MariaDB to eventually change primary")
		Eventually(func() bool {
			if err := k8sClient.Get(testCtx, key, mdb); err != nil {
				return false
			}
			if !mdb.IsReady() || mdb.Status.CurrentPrimaryPodIndex == nil {
				return false
			}
			return *mdb.Status.CurrentPrimaryPodIndex == podIndex
		}, testTimeout, testInterval).Should(BeTrue())

		By("Expecting primary Service to eventually change primary")
		Eventually(func() bool {
			var svc corev1.Service
			if err := k8sClient.Get(testCtx, mdb.PrimaryServiceKey(), &svc); err != nil {
				return false
			}
			return svc.Spec.Selector["statefulset.kubernetes.io/pod-name"] == statefulset.PodName(mdb.ObjectMeta, podIndex)
		}, testTimeout, testInterval).Should(BeTrue())
	})

	It("should recover after Galera cluster crash", func() {
		By("Tearing down all Pods")
		opts := []client.DeleteAllOfOption{
			client.MatchingLabels(
				labels.NewLabelsBuilder().
					WithMariaDBSelectorLabels(mdb).
					Build(),
			),
			client.InNamespace(mdb.Namespace),
		}
		Expect(k8sClient.DeleteAllOf(testCtx, &corev1.Pod{}, opts...)).To(Succeed())

		testGaleraRecovery(key)
	})

	It("should recover from existing PVCs", func() {
		By("Deleting MariaDB")
		Expect(k8sClient.Delete(testCtx, mdb)).To(Succeed())

		By("Expecting to delete Pods eventually")
		Eventually(func(g Gomega) bool {
			var podList corev1.PodList
			listOpts := []client.ListOption{
				client.MatchingLabels(
					labels.NewLabelsBuilder().
						WithMariaDBSelectorLabels(mdb).
						Build(),
				),
				client.InNamespace(mdb.Namespace),
			}
			g.Expect(k8sClient.List(testCtx, &podList, listOpts...)).To(Succeed())
			return len(podList.Items) == 0
		}, testTimeout, testInterval).Should(BeTrue())

		By("Expecting to delete PVC eventually")
		Eventually(func(g Gomega) bool {
			key := types.NamespacedName{
				Name:      mdb.PVCKey(builder.StorageVolume, 0).Name,
				Namespace: mdb.Namespace,
			}
			var pvc corev1.PersistentVolumeClaim
			g.Expect(k8sClient.Get(testCtx, key, &pvc)).To(Succeed())
			g.Expect(k8sClient.Delete(testCtx, &pvc)).To(Succeed())
			return true
		}, testTimeout, testInterval).Should(BeTrue())

		By("Creating MariaDB")
		mdb.ObjectMeta = metav1.ObjectMeta{
			Name:      key.Name,
			Namespace: key.Namespace,
		}
		Expect(k8sClient.Create(testCtx, mdb)).To(Succeed())

		testGaleraRecovery(key)
	})

	It("should update", func() {
		By("Updating MariaDB")
		testMariadbUpdate(mdb)
	})

	It("should resize PVCs", func() {
		By("Resizing MariaDB PVCs")
		testMariadbVolumeResize(mdb, "400Mi")
	})

	It("should reconcile with MaxScale", func() {
		mxs := &mariadbv1alpha1.MaxScale{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "maxscale-galera",
				Namespace: testNamespace,
			},
			Spec: mariadbv1alpha1.MaxScaleSpec{
				Replicas: 3,
				KubernetesService: &mariadbv1alpha1.ServiceTemplate{
					Type: corev1.ServiceTypeLoadBalancer,
					Metadata: &mariadbv1alpha1.Metadata{
						Annotations: map[string]string{
							"metallb.universe.tf/loadBalancerIPs": testCidrPrefix + ".0.224",
						},
					},
				},
				GuiKubernetesService: &mariadbv1alpha1.ServiceTemplate{
					Type: corev1.ServiceTypeLoadBalancer,
					Metadata: &mariadbv1alpha1.Metadata{
						Annotations: map[string]string{
							"metallb.universe.tf/loadBalancerIPs": testCidrPrefix + ".0.231",
						},
					},
				},
				Connection: &mariadbv1alpha1.ConnectionTemplate{
					SecretName: ptr.To("mxs-galera-conn"),
					HealthCheck: &mariadbv1alpha1.HealthCheck{
						Interval: ptr.To(metav1.Duration{Duration: 1 * time.Second}),
					},
				},
				Auth: mariadbv1alpha1.MaxScaleAuth{
					Generate: ptr.To(true),
					AdminPasswordSecretKeyRef: mariadbv1alpha1.GeneratedSecretKeyRef{
						SecretKeySelector: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: testPwdKey.Name,
							},
							Key: testPwdSecretKey,
						},
						Generate: false,
					},
				},
				Metrics: &mariadbv1alpha1.MaxScaleMetrics{
					Enabled: true,
				},
			},
		}

		By("Using MariaDB with MaxScale")
		testMaxscale(mdb, mxs)
	})
})

func testGaleraRecovery(key types.NamespacedName) {
	var mdb mariadbv1alpha1.MariaDB
	By("Expecting MariaDB to NOT be ready eventually")
	Eventually(func() bool {
		if err := k8sClient.Get(testCtx, key, &mdb); err != nil {
			return false
		}
		return !mdb.IsReady() && mdb.HasGaleraNotReadyCondition()
	}, testHighTimeout, testInterval).Should(BeTrue())

	By("Expecting MariaDB to be ready eventually")
	Eventually(func() bool {
		if err := k8sClient.Get(testCtx, key, &mdb); err != nil {
			return false
		}
		return mdb.IsReady() && mdb.HasGaleraReadyCondition()
	}, testVeryHighTimeout, testInterval).Should(BeTrue())

	By("Expecting Connection to be ready eventually")
	Eventually(func() bool {
		var conn mariadbv1alpha1.Connection
		if err := k8sClient.Get(testCtx, key, &conn); err != nil {
			return false
		}
		return conn.IsReady()
	}, testTimeout, testInterval).Should(BeTrue())

	By("Expecting primary Connection to be ready eventually")
	Eventually(func() bool {
		var conn mariadbv1alpha1.Connection
		if err := k8sClient.Get(testCtx, mdb.PrimaryConnectioneKey(), &conn); err != nil {
			return false
		}
		return conn.IsReady()
	}, testTimeout, testInterval).Should(BeTrue())
}
