package functional

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2" //revive:disable:dot-imports
	. "github.com/onsi/gomega"    //revive:disable:dot-imports

	//revive:disable-next-line:dot-imports
	memcachedv1 "github.com/openstack-k8s-operators/infra-operator/apis/memcached/v1beta1"
	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	. "github.com/openstack-k8s-operators/lib-common/modules/common/test/helpers"
	"github.com/openstack-k8s-operators/mariadb-operator/api/v1beta1"
	watcherv1beta1 "github.com/openstack-k8s-operators/watcher-operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var (
	MinimalWatcherAPISpec = map[string]interface{}{
		"secret":            "osp-secret",
		"memcachedInstance": "memcached",
	}
)

var _ = Describe("WatcherAPI controller with minimal spec values", func() {
	When("A Watcher instance is created from minimal spec", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, MinimalWatcherAPISpec))
		})

		It("should have the Spec fields defaulted", func() {
			WatcherAPI := GetWatcherAPI(watcherTest.WatcherAPI)
			Expect(WatcherAPI.Spec.Secret).Should(Equal("osp-secret"))
			Expect(WatcherAPI.Spec.MemcachedInstance).Should(Equal("memcached"))
			Expect(WatcherAPI.Spec.PasswordSelectors).Should(Equal(watcherv1beta1.PasswordSelector{Service: "WatcherPassword"}))
			Expect(WatcherAPI.Spec.PrometheusSecret).Should(Equal("metric-storage-prometheus-endpoint"))
		})

		It("should have the Status fields initialized", func() {
			WatcherAPI := GetWatcherAPI(watcherTest.WatcherAPI)
			Expect(WatcherAPI.Status.ObservedGeneration).To(Equal(int64(0)))
		})

		It("should have a finalizer", func() {
			// the reconciler loop adds the finalizer so we have to wait for
			// it to run
			Eventually(func() []string {
				return GetWatcherAPI(watcherTest.WatcherAPI).Finalizers
			}, timeout, interval).Should(ContainElement("openstack.org/watcherapi"))
		})

	})
})

var _ = Describe("WatcherAPI controller", func() {
	When("A WatcherAPI instance is created", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))
		})

		It("should have the Spec fields defaulted", func() {
			WatcherAPI := GetWatcherAPI(watcherTest.WatcherAPI)
			Expect(WatcherAPI.Spec.Secret).Should(Equal("test-osp-secret"))
			Expect(WatcherAPI.Spec.MemcachedInstance).Should(Equal("memcached"))
			Expect(WatcherAPI.Spec.PrometheusSecret).Should(Equal("metric-storage-prometheus-endpoint"))
		})

		It("should have the Status fields initialized", func() {
			WatcherAPI := GetWatcherAPI(watcherTest.WatcherAPI)
			Expect(WatcherAPI.Status.ObservedGeneration).To(Equal(int64(0)))
		})

		It("should have ReadyCondition false", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionFalse,
			)
		})

		It("should have input not ready", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionFalse,
			)
		})

		It("should have service config input unknown", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionUnknown,
			)
		})

		It("should have a finalizer", func() {
			// the reconciler loop adds the finalizer so we have to wait for
			// it to run
			Eventually(func() []string {
				return GetWatcherAPI(watcherTest.WatcherAPI).Finalizers
			}, timeout, interval).Should(ContainElement("openstack.org/watcherapi"))
		})
	})
	When("the secret is created with all the expected fields and has all the required infra", func() {
		BeforeEach(func() {
			secret := th.CreateSecret(
				watcherTest.InternalTopLevelSecretName,
				map[string][]byte{
					"WatcherPassword":       []byte("service-password"),
					"transport_url":         []byte("url"),
					"database_username":     []byte("username"),
					"database_password":     []byte("password"),
					"database_hostname":     []byte("hostname"),
					"database_account":      []byte("watcher"),
					"01-global-custom.conf": []byte(""),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, secret)
			prometheusSecret := th.CreateSecret(
				watcherTest.PrometheusSecretName,
				map[string][]byte{
					"host": []byte("prometheus.example.com"),
					"port": []byte("9090"),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, prometheusSecret)
			DeferCleanup(
				mariadb.DeleteDBService,
				mariadb.CreateDBService(
					watcherTest.WatcherAPI.Namespace,
					"openstack",
					corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 3306}},
					},
				),
			)
			mariadb.CreateMariaDBAccountAndSecret(
				watcherTest.WatcherDatabaseAccount,
				v1beta1.MariaDBAccountSpec{
					UserName: "watcher",
				},
			)
			mariadb.CreateMariaDBDatabase(
				watcherTest.WatcherAPI.Namespace,
				"watcher",
				v1beta1.MariaDBDatabaseSpec{
					Name: "watcher",
				},
			)
			mariadb.SimulateMariaDBAccountCompleted(watcherTest.WatcherDatabaseAccount)
			mariadb.SimulateMariaDBDatabaseCompleted(watcherTest.WatcherDatabaseName)
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(watcherTest.WatcherAPI.Namespace))
			memcachedSpec := memcachedv1.MemcachedSpec{
				MemcachedSpecCore: memcachedv1.MemcachedSpecCore{
					Replicas: ptr.To(int32(1)),
				},
			}
			DeferCleanup(infra.DeleteMemcached, infra.CreateMemcached(watcherTest.WatcherAPI.Namespace, MemcachedInstance, memcachedSpec))
			infra.SimulateMemcachedReady(watcherTest.MemcachedNamespace)

		})
		It("should have input ready", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("should have memcached ready true", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.MemcachedReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("should have config service input ready", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("creates a deployment for the watcher-api service", func() {
			th.SimulateStatefulSetReplicaReady(watcherTest.WatcherAPIStatefulSet)
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.DeploymentReadyCondition,
				corev1.ConditionTrue,
			)

			deployment := th.GetStatefulSet(watcherTest.WatcherAPIStatefulSet)
			Expect(deployment.Spec.Template.Spec.ServiceAccountName).To(Equal("watcher-sa"))
			Expect(int(*deployment.Spec.Replicas)).To(Equal(1))
			Expect(deployment.Spec.Template.Spec.Volumes).To(HaveLen(3))
			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(2))
			Expect(deployment.Spec.Selector.MatchLabels).To(Equal(map[string]string{"service": "watcher-api"}))

			container := deployment.Spec.Template.Spec.Containers[0]
			Expect(container.VolumeMounts).To(HaveLen(1))
			Expect(container.Image).To(Equal("test://watcher"))

			container = deployment.Spec.Template.Spec.Containers[1]
			Expect(container.VolumeMounts).To(HaveLen(4))
			Expect(container.Image).To(Equal("test://watcher"))

			Expect(container.LivenessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9322)))
			Expect(container.ReadinessProbe.HTTPGet.Port.IntVal).To(Equal(int32(9322)))
		})
		It("exposes the watcher-api service", func() {
			th.SimulateStatefulSetReplicaReady(watcherTest.WatcherAPIStatefulSet)
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ExposeServiceReadyCondition,
				corev1.ConditionTrue,
			)
			th.AssertRouteExists(watcherTest.WatcherRouteName)
			public := th.GetService(watcherTest.WatcherPublicServiceName)
			Expect(public.Labels["service"]).To(Equal("watcher-api"))
			Expect(public.Labels["public"]).To(Equal("true"))
			internal := th.GetService(watcherTest.WatcherInternalServiceName)
			Expect(internal.Labels["service"]).To(Equal("watcher-api"))
			Expect(internal.Labels["internal"]).To(Equal("true"))
		})
		It("created the keystone endpoint for the watcher-api service", func() {
			th.SimulateStatefulSetReplicaReady(watcherTest.WatcherAPIStatefulSet)
			keystone.SimulateKeystoneEndpointReady(watcherTest.WatcherKeystoneEndpointName)
			// it registers the endpointURL as the public endpoint and svc
			// for the internal
			keystoneEndpoint := keystone.GetKeystoneEndpoint(watcherTest.WatcherKeystoneEndpointName)
			endpoints := keystoneEndpoint.Spec.Endpoints
			// jgilaber: the public endpoint returned by the exposeEndpoint
			// function of lib-common has an empty hostname
			Expect(endpoints).To(HaveKeyWithValue("public", "http://"))
			Expect(endpoints).To(HaveKeyWithValue("internal", "http://watcher-internal."+watcherTest.WatcherAPI.Namespace+".svc:9322"))
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.KeystoneEndpointReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
	When("the secret is created but missing fields", func() {
		BeforeEach(func() {
			secret := th.CreateSecret(
				watcherTest.InternalTopLevelSecretName,
				map[string][]byte{},
			)
			DeferCleanup(k8sClient.Delete, ctx, secret)
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))
		})
		It("should have input false", func() {
			errorString := fmt.Sprintf(
				condition.InputReadyErrorMessage,
				"field 'WatcherPassword' not found in secret/test-osp-secret",
			)
			th.ExpectConditionWithDetails(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionFalse,
				condition.ErrorReason,
				errorString,
			)
		})
		It("should have config service input unknown", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionUnknown,
			)
		})
	})
	When("A WatcherAPI instance without secret is created", func() {
		BeforeEach(func() {
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))
		})
		It("is missing the secret", func() {
			th.ExpectConditionWithDetails(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				condition.InputReadyWaitingMessage,
			)
		})
	})
	When("secret and db are created, but there is no memcached", func() {
		BeforeEach(func() {
			secret := th.CreateSecret(
				watcherTest.InternalTopLevelSecretName,
				map[string][]byte{
					"WatcherPassword":       []byte("service-password"),
					"transport_url":         []byte("url"),
					"database_username":     []byte("username"),
					"database_password":     []byte("password"),
					"database_hostname":     []byte("hostname"),
					"database_account":      []byte("watcher"),
					"01-global-custom.conf": []byte(""),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, secret)
			prometheusSecret := th.CreateSecret(
				watcherTest.PrometheusSecretName,
				map[string][]byte{
					"host": []byte("prometheus.example.com"),
					"port": []byte("9090"),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, prometheusSecret)

			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))
		})
		It("should have input ready true", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("should have memcached ready false", func() {
			th.ExpectConditionWithDetails(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.MemcachedReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				condition.MemcachedReadyWaitingMessage,
			)
		})
	})
	When("prometheus config secret is not created", func() {
		BeforeEach(func() {
			secret := th.CreateSecret(
				watcherTest.InternalTopLevelSecretName,
				map[string][]byte{
					"WatcherPassword":       []byte("service-password"),
					"transport_url":         []byte("url"),
					"database_username":     []byte("username"),
					"database_password":     []byte("password"),
					"database_hostname":     []byte("hostname"),
					"database_account":      []byte("watcher"),
					"01-global-custom.conf": []byte(""),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, secret)

			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))
		})

		It("should have input ready false", func() {
			th.ExpectConditionWithDetails(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				watcherv1beta1.WatcherPrometheusSecretErrorMessage,
			)
		})
	})

	When("secret, db and memcached are created, but there is no keystoneapi", func() {
		BeforeEach(func() {
			secret := th.CreateSecret(
				watcherTest.InternalTopLevelSecretName,
				map[string][]byte{
					"WatcherPassword":       []byte("service-password"),
					"transport_url":         []byte("url"),
					"database_username":     []byte("username"),
					"database_password":     []byte("password"),
					"database_hostname":     []byte("hostname"),
					"database_account":      []byte("watcher"),
					"01-global-custom.conf": []byte(""),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, secret)
			prometheusSecret := th.CreateSecret(
				watcherTest.PrometheusSecretName,
				map[string][]byte{
					"host": []byte("prometheus.example.com"),
					"port": []byte("9090"),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, prometheusSecret)
			memcachedSpec := memcachedv1.MemcachedSpec{
				MemcachedSpecCore: memcachedv1.MemcachedSpecCore{
					Replicas: ptr.To(int32(1)),
				},
			}
			DeferCleanup(infra.DeleteMemcached, infra.CreateMemcached(watcherTest.WatcherAPI.Namespace, MemcachedInstance, memcachedSpec))
			infra.SimulateMemcachedReady(watcherTest.MemcachedNamespace)
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, GetDefaultWatcherAPISpec()))

		})
		It("should have input ready true", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.InputReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("should have memcached ready true", func() {
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.MemcachedReadyCondition,
				corev1.ConditionTrue,
			)
		})
		It("should have config service input unknown", func() {
			errorString := fmt.Sprintf(
				condition.ServiceConfigReadyErrorMessage,
				"keystoneAPI not found",
			)
			th.ExpectConditionWithDetails(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionFalse,
				condition.ErrorReason,
				errorString,
			)
		})
	})
	When("WatcherAPI is created with service overrides", func() {
		BeforeEach(func() {
			secret := th.CreateSecret(
				watcherTest.InternalTopLevelSecretName,
				map[string][]byte{
					"WatcherPassword":       []byte("service-password"),
					"transport_url":         []byte("url"),
					"database_account":      []byte("watcher"),
					"database_username":     []byte("watcher"),
					"database_password":     []byte("watcher-password"),
					"database_hostname":     []byte("db-hostname"),
					"01-global-custom.conf": []byte(""),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, secret)
			prometheusSecret := th.CreateSecret(
				watcherTest.PrometheusSecretName,
				map[string][]byte{
					"host": []byte("prometheus.example.com"),
					"port": []byte("9090"),
				},
			)
			DeferCleanup(k8sClient.Delete, ctx, prometheusSecret)
			spec := GetDefaultWatcherAPISpec()
			apiOverrideSpec := map[string]interface{}{}
			endpoint := map[string]interface{}{}
			internalEndpoint := map[string]interface{}{}
			endpoint["ipAddressPool"] = "osp-internalapi"
			endpoint["loadBalancerIPs"] = []string{"internal-lb-ip-1", "internal-lb-ip-2"}
			internalEndpoint["internal"] = endpoint
			apiOverrideSpec["service"] = internalEndpoint
			spec["override"] = apiOverrideSpec
			DeferCleanup(th.DeleteInstance, CreateWatcherAPI(watcherTest.WatcherAPI, spec))
			DeferCleanup(keystone.DeleteKeystoneAPI, keystone.CreateKeystoneAPI(watcherTest.WatcherAPI.Namespace))
			memcachedSpec := memcachedv1.MemcachedSpec{
				MemcachedSpecCore: memcachedv1.MemcachedSpecCore{
					Replicas: ptr.To(int32(1)),
				},
			}
			DeferCleanup(infra.DeleteMemcached, infra.CreateMemcached(watcherTest.WatcherAPI.Namespace, MemcachedInstance, memcachedSpec))
			infra.SimulateMemcachedReady(watcherTest.MemcachedNamespace)
			DeferCleanup(
				mariadb.DeleteDBService,
				mariadb.CreateDBService(
					watcherTest.WatcherAPI.Namespace,
					"openstack",
					corev1.ServiceSpec{
						Ports: []corev1.ServicePort{{Port: 3306}},
					},
				),
			)
			mariadb.CreateMariaDBAccountAndSecret(
				watcherTest.WatcherDatabaseAccount,
				v1beta1.MariaDBAccountSpec{
					UserName: "watcher",
				},
			)
			mariadb.CreateMariaDBDatabase(
				watcherTest.WatcherAPI.Namespace,
				"watcher",
				v1beta1.MariaDBDatabaseSpec{
					Name: "watcher",
				},
			)
			mariadb.SimulateMariaDBAccountCompleted(watcherTest.WatcherDatabaseAccount)
			mariadb.SimulateMariaDBDatabaseCompleted(watcherTest.WatcherDatabaseName)

		})
		It("creates MetalLB service", func() {
			th.SimulateStatefulSetReplicaReady(watcherTest.WatcherAPIStatefulSet)
			// simulate that the internal service got a LoadBalancerIP
			// assigned
			th.SimulateLoadBalancerServiceIP(watcherTest.WatcherInternalServiceName)

			// As the public endpoint is not mentioned in the service override
			// a generic Service and a Route is created
			public := th.GetService(watcherTest.WatcherPublicServiceName)
			Expect(public.Annotations).NotTo(HaveKey("metallb.universe.tf/address-pool"))
			Expect(public.Annotations).NotTo(HaveKey("metallb.universe.tf/allow-shared-ip"))
			Expect(public.Annotations).NotTo(HaveKey("metallb.universe.tf/loadBalancerIPs"))
			Expect(public.Labels["service"]).To(Equal("watcher-api"))
			Expect(public.Labels["public"]).To(Equal("true"))
			th.AssertRouteExists(watcherTest.WatcherRouteName)

			// As the internal endpoint is configure in the service override it
			// does not get a Route but a Service with MetalLB annotations
			// instead
			internal := th.GetService(watcherTest.WatcherInternalServiceName)
			Expect(internal.Annotations).To(HaveKeyWithValue("metallb.universe.tf/address-pool", "osp-internalapi"))
			Expect(internal.Annotations).To(HaveKeyWithValue("metallb.universe.tf/allow-shared-ip", "osp-internalapi"))
			Expect(internal.Annotations).To(HaveKeyWithValue("metallb.universe.tf/loadBalancerIPs", "internal-lb-ip-1,internal-lb-ip-2"))
			Expect(internal.Labels["service"]).To(Equal("watcher-api"))
			Expect(internal.Labels["internal"]).To(Equal("true"))
			th.AssertRouteNotExists(watcherTest.WatcherInternalRouteName)

			// simulate the keystone endpoint
			keystone.SimulateKeystoneEndpointReady(watcherTest.WatcherKeystoneEndpointName)
			th.ExpectCondition(
				watcherTest.WatcherAPI,
				ConditionGetterFunc(WatcherAPIConditionGetter),
				condition.ReadyCondition,
				corev1.ConditionTrue,
			)
		})
	})
})
