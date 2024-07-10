# MaxScale

> [!IMPORTANT]  
> MaxScale 23.08 is licensed under [Business Source License](https://github.com/mariadb-corporation/MaxScale/blob/23.08/LICENSE2308.TXT). Make sure you understand the implications before using it!

MaxScale is a sophisticated database proxy, router, and load balancer designed specifically for and by MariaDB. It provides a range of features that ensure optimal high availability:
- Query-based routing: Transparently route write queries to the primary nodes and read queries to the replica nodes.
- Connection-based routing: Load balance connections between multiple servers.
- Automatic primary failover based on MariaDB internals.
- Replay pending transactions when a server goes down.
- Support for Galera and Replication.

To better understand what MaxScale is capable of you may check the [product page](https://mariadb.com/docs/server/products/mariadb-maxscale/) and the [documentation](https://mariadb.com/kb/en/maxscale/).

## MaxScale resources

Prior to configuring MaxScale within Kubernetes, it's essential to have a basic understanding of the resources managed through its API.

#### Servers

A server defines the backend database servers that MaxScale forwards traffic to. For more detailed information, please consult the [server reference](https://mariadb.com/kb/en/mariadb-maxscale-2308-mariadb-maxscale-configuration-guide/#server).

#### Monitors

A monitor is an agent that queries the state of the servers and makes it available to the services in order to route traffic based on it. For more detailed information, please consult the [monitor reference](https://mariadb.com/kb/en/mariadb-maxscale-2308-mariadb-maxscale-configuration-guide/#monitor).

Depending on which highly available configuration your servers have, you will need to choose betweeen the following modules:
- [Galera Monitor](https://mariadb.com/kb/en/mariadb-maxscale-2308-galera-monitor/): Detects whether servers are part of the cluster, ensuring synchronization among them, and assigning primary and replica roles as needed.
- [MariaDB Monitor](https://mariadb.com/kb/en/mariadb-maxscale-2308-mariadb-monitor/): Probes the state of the cluster, assigns roles to the servers, and executes failover, switchover, and rejoin operations as necessary.
#### Services

A service defines how the traffic is routed to the servers based on a routing algorithm that takes into account the state of the servers and its role. For more detailed information, please consult the [service reference](https://mariadb.com/kb/en/mariadb-maxscale-2308-mariadb-maxscale-configuration-guide/#service).

Depending on your requirements to route traffic, you may choose between the following routers:
- [Readwritesplit](https://mariadb.com/kb/en/mariadb-maxscale-2308-readwritesplit/): Route write queries to the primary server and read queries to the replica servers.
- [Readconnroute](https://mariadb.com/kb/en/mariadb-maxscale-2308-readconnroute/): Load balance connections between multiple servers.

#### Listeners

A listener specifies a port where MaxScale listens for incoming connections. It is associated with a service that handles the requests received on that port. For more detailed information, please consult the [listener reference](https://mariadb.com/kb/en/mariadb-maxscale-2308-mariadb-maxscale-configuration-guide/#listener).

## `MaxScale` CR

The minimal spec you need to provision a MaxScale instance is just a reference to a `MariaDB` resource:
```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
  mariaDbRef:
    name: mariadb-galera
```

This will provision a new `StatefulSet` for running MaxScale and configure the servers specified by the `MariaDB` resource. Refer to the [Server configuration](#server-configuration) section if you want to manually configure the MariaDB servers.

The rest of the configuration uses reasonable [defaults](#defaults) set automatically by the operator. If you need a more fine grained configuration, you can provide this values yourself:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  mariaDbRef:
    name: mariadb-galera

  services:
    - name: rw-router
      router: readwritesplit
      params:
        transaction_replay: "true"
        transaction_replay_attempts: "10"
        transaction_replay_timeout: "5s"
        max_slave_connections: "255"
        max_replication_lag: "3s"
        master_accept_reads: "true"
      listener:
        port: 3306
        protocol: MariaDBProtocol
        params:
          connection_metadata: "tx_isolation=auto"
    - name: rconn-master-router
      router: readconnroute
      params:
        router_options: "master"
        max_replication_lag: "3s"
        master_accept_reads: "true"
      listener:
        port: 3307
    - name: rconn-slave-router
      router: readconnroute
      params:
        router_options: "slave"
        max_replication_lag: "3s"
      listener:
        port: 3308

  monitor:
    interval: 2s
    cooperativeMonitoring: majority_of_all
    params:
      disable_master_failback: "false"
      available_when_donor: "false"
      disable_master_role_setting: "false"

  kubernetesService:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.224
```

As you can see, the [MaxScale resources](#maxscale-resources) we previously mentioned have a counterpart resource in the `MaxScale` CR. 

The previous example configured a `MaxScale` for a Galera cluster, but you may also configure `MaxScale` with a `MariaDB` that uses replication. It is important to note that the monitor module is automatically inferred by the operator based on the `MariaDB` reference you provided, however, its parameters are specific to each monitor module:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-repl
spec:
...
  mariaDbRef:
    name: mariadb-repl

  services:
    - name: rw-router
      router: readwritesplit
      params:
        transaction_replay: "true"
        transaction_replay_attempts: "10"
        transaction_replay_timeout: "5s"
        max_slave_connections: "255"
        max_replication_lag: "3s"
        master_accept_reads: "true"
      listener:
        port: 3306
        protocol: MariaDBProtocol
        params:
          connection_metadata: "tx_isolation=auto"
    - name: rconn-master-router
      router: readconnroute
      params:
        router_options: "master"
        max_replication_lag: "3s"
        master_accept_reads: "true"
      listener:
        port: 3307
    - name: rconn-slave-router
      router: readconnroute
      params:
        router_options: "slave"
        max_replication_lag: "3s"
      listener:
        port: 3308

  monitor:
    interval: 2s
    cooperativeMonitoring: majority_of_all
    params:
      auto_failover: "true"
      auto_rejoin: "true"
      switchover_on_low_disk_space: "true"

  kubernetesService:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.214
```

You also need to set a reference in the `MariaDB` resource to make it `MaxScale`-aware. This is explained in the [MariaDB CR](#mariadb-cr) section.

Refer to the [Reference](#reference) section for further detail.

## `MariaDB` CR

You can set a `spec.maxScaleRef` in your `MariaDB` resource to make it `MaxScale`-aware. By doing so, the primary server reported by `MaxScale` will be used in `MariaDB` and the high availability tasks such the primary failover will be delegated to `MaxScale`:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb-galera
spec:
...
  maxScaleRef:
    name: maxscale-galera

  galera:
    enabled: true
```

Refer to the [Reference](#reference) section for further detail.

##  `MaxScale` embedded in `MariaDB`

To streamline the setup outlined in the [MaxScale CR](#mariadb-cr) and [MariaDB CR](#mariadb-cr) sections, you can provision a `MaxScale` to be used with `MariaDB` in just one resource:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb-galera
spec:
...
  maxScale:
    enabled: true

    kubernetesService:
      type: LoadBalancer
      metadata:
        annotations:
          metallb.universe.tf/loadBalancerIPs: 172.18.0.229

  galera:
    enabled: true
```
This will automatically set the references between `MariaDB` and `MaxScale` and [default](#defaults) the rest of the fields.

It is important to note that, this is intended for simple use cases where no further modifications are done on the `spec.maxscale` field. If you need a more fine grained configuration and perform further updates to the `MaxScale` resource, please use a dedicated `MaxScale` as described in the [MaxScale CR](#mariadb-cr) section.

Refer to the [Reference](#reference) section for further detail.

## Defaults

`mariadb-operator` aims to provide highly configurable CRs, but at the same maximize its usability by providing reasonable defaults. In the case of `MaxScale`, the following defaulting logic is applied:
- `spec.servers` are inferred from `spec.mariaDbRef`.
- `spec.monitor.module` is inferred from the `spec.mariaDbRef`.
- `spec.monitor.cooperativeMonitoring` is set if [high availability](#high-availability) is enabled.
- If `spec.services` is not provided, the following are configured by default:
  - `readwritesplit` service on port `3306`.
  - `readconnroute` service pointing to the primary node on port `3307`.
  - `readconnroute` service pointing to the replica nodes on port `3308`.

## Server configuration

As an alternative to provide a reference to a `MariaDB` via `spec.mariaDbRef`, you can also specify the servers manually:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  servers:
    - name: mariadb-0
      address: mariadb-galera-0.mariadb-galera-internal.default.svc.cluster.local
    - name: mariadb-1
      address: mariadb-galera-1.mariadb-galera-internal.default.svc.cluster.local
    - name: mariadb-2
      address: mariadb-galera-2.mariadb-galera-internal.default.svc.cluster.local
```

As you could see, you can refer to in-cluser MariaDB servers by providing the DNS names of the `MariaDB` `Pods` as server addresses. In addition, you can also refer to external MariaDB instances running outside of the Kubernetes cluster where `mariadb-operator` was deployed:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  servers:
    - name: mariadb-0
      address: 172.18.0.140
      port: 3306
    - name: mariadb-1
      address: 172.18.0.141
    - name: mariadb-2
      address: 172.18.0.142

  monitor:
    name: mariadb-monitor
    module: galeramon
    interval: 2s
    cooperativeMonitoring: majority_of_all
    params:
      disable_master_failback: "false"
      available_when_donor: "false"
      disable_master_role_setting: "false"

  auth:
    adminUsername: mariadb-operator
    adminPasswordSecretKeyRef:
      name: maxscale
      key: password
    clientUsername: maxscale-client
    clientPasswordSecretKeyRef:
      name: maxscale
      key: password
    serverUsername: maxscale-server
    serverPasswordSecretKeyRef:
      name: maxscale
      key: password
    monitorUsername: maxscale-monitor
    monitorPasswordSecretKeyRef:
      name: maxscale
      key: password
    syncUsername: maxscale-sync
    syncPasswordSecretKeyRef:
      name: maxscale
      key: password
```

⚠️ Pointing to external MariaDBs has a some limitations ⚠️. Since the operator doesn't have a reference to a `MariaDB` resource (`spec.mariaDbRef`), it will be unable to perform the following actions:
- Infer the monitor module (`spec.monitor.module`), so it will need to be provided by the user.
- Autogenerate authentication credentials (`spec.auth`), so they will need to be provided by the user. See [Authentication](#authentication) section. 

## Server maintenance

You can put servers in maintenance mode by setting `maintenance = true`:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  servers:
    - name: mariadb-0
      address: mariadb-galera-0.mariadb-galera-internal.default.svc.cluster.local
      port: 3306
      protocol: MariaDBBackend
      maintenance: true
```

Maintenance mode prevents MaxScale from routing traffic to the server and also excludes it from being elected as the new primary during failover events.

## Configuration

Similar to MariaDB, MaxScale allows you to provide global configuration parameters in a `maxscale.conf` file. You don't need to provide this config file directly, but instead you can use the `spec.config.params` to instruct the operator to create the `maxscale.conf`:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  config:
    params:
      log_info: "true"
    volumeClaimTemplate:
      resources:
        requests:
          storage: 100Mi
      accessModes:
        - ReadWriteOnce
```

Both this global configuration and the resources created by the operator using the [MaxScale API](#maxscale-api) are stored under a volume provisioned by the `spec.config.volumeClaimTemplate`. Refer to the [troubleshooting](#troubleshooting) if you are getting errors writing on this volume.

Refer to the [MaxScale reference](https://mariadb.com/kb/en/mariadb-maxscale-2308-mariadb-maxscale-configuration-guide/) to provide global configuration.

## Authentication

MaxScale requires authentication with differents levels of permissions for the following components/actors:
- [MaxScale API](#maxscale-api) consumed by `mariadb-operator`.
- Clients connecting to MaxScale.
- MaxScale connecting to MariaDB servers.
- MaxScale monitor connecting to MariaDB servers.
- MaxScale configuration sync to connect to MariaDB servers. See [high availability](#high-availability) section.

By default, `mariadb-operator` autogenerates this credentials when `spec.mariaDbRef` is set and `spec.auth.generate = true`, but you are still able to provide your own:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  auth:
    generate: false
    adminUsername: mariadb-operator
    adminPasswordSecretKeyRef:
      name: maxscale
      key: password
    deleteDefaultAdmin: true
    clientUsername: maxscale-client
    clientPasswordSecretKeyRef:
      name: maxscale
      key: password
    clientMaxConnections: 90
    serverUsername: maxscale-server
    serverPasswordSecretKeyRef:
      name: maxscale
      key: password
    serverMaxConnections: 90 
    monitorUsername: maxscale-monitor
    monitorPasswordSecretKeyRef:
      name: maxscale
      key: password
    monitorMaxConnections: 90 
    syncUsername: maxscale-sync
    syncPasswordSecretKeyRef:
      name: maxscale
      key: password
    syncMaxConnections: 90
```

As you could see, you are also able to limit the number of connections for each component/actor. Bear in mind that, when running in [high availability](#high-availability), you may need to increase this number, as more MaxScale instances implies more connections.

## Kubernetes `Services`

To enable your applications to communicate with MaxScale, a Kubernetes `Service` is provisioned with all the ports specified in the MaxScale listeners. You have the flexibility to provide a template to customize this `Service`:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  kubernetesService:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.224
```

This results in the reconciliation of the following `Service`:

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    metallb.universe.tf/loadBalancerIPs: 172.18.0.229
  name: maxscale-galera
spec:
...
  ports:
  - name: admin
    port: 8989
    targetPort: 8989
  - name: rw-router-listener
    port: 3306
    targetPort: 3306
  - name: rconn-master-router-listener
    port: 3307
    targetPort: 3307
  - name: rconn-slave-router-listener
    port: 3308
    targetPort: 3308
  selector:
    app.kubernetes.io/instance: maxscale-galera
    app.kubernetes.io/name: maxscale
  type: LoadBalancer
```

There is also another Kubernetes `Service` to access the GUI, please refer to the [MaxScale GUI](#maxscale-gui) section for further detail.

## Connection

You can leverage the `Connection` resource to automatically configure connection strings as `Secret` resources that your applications can mount:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: Connection
metadata:
  name: connection-maxscale
spec:
  maxScaleRef:
    name: maxscale-galera
  username: maxscale-galera-client
  passwordSecretKeyRef:
    name: maxscale-galera-client
    key: password
  secretName: conn-mxs
  port: 3306
```

Alternatively, you can also provide a connection template to your `MaxScale` resource:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...  
  connection:
    secretName: mxs-galera-conn
    port: 3306
```

Note that, the `Connection` uses the `Service` described in the [Kubernetes Service](#kubernetes-service) section and you are able to specify which MaxScale service to connect to by providing the port (`spec.port`) of the corresponding MaxScale listener.

## High availability

To synchronize the configuration state across multiple replicas, MaxScale stores the configuration externally in a MariaDB table and conducts periodic polling across all replicas. By default, the table `mysql.maxscale_config` is used, but this can be configured by the user as well as the synchronization interval.

Another crucial aspect to consider regarding HA is that only one monitor can be running at any given time to avoid conflicts. This can be achieved via cooperative locking, which can be configured by the user. Refer to [MaxScale docs](https://mariadb.com/docs/server/architecture/components/maxscale/monitors/mariadbmon/use-cooperative-locking-ha-maxscale-mariadb-monitor/) for more information.

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  replicas: 3

  monitor:
    name: mariadb-monitor
    module: galeramon
    interval: 2s
    cooperativeMonitoring: majority_of_all
    params:
      disable_master_failback: "false"
      available_when_donor: "false"
      disable_master_role_setting: "false"   

  config:
    sync:
      database: mysql
      interval: 5s
      timeout: 10s
```

Multiple `MaxScale` replicas can be specified by providing the `spec.replicas` field. Note that, `MaxScale` exposes the [scale subresource](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/#scale-subresource), so you can scale/downscale it by running the following command:

```bash
kubectl scale maxscale maxscale-galera --replicas 3
```
Or even configuring an `HorizontalPodAutoscaler` to do the job automatically.

## Suspend resources

In order to enable this feature, you must set the `--feature-maxscale-suspend` feature flag:

```bash
helm upgrade --install mariadb-operator mariadb-operator/mariadb-operator --set extraArgs={--feature-maxscale-suspend}
```

Then you will be able to suspend any [MaxScale resources](#maxscale-resources), for instance, you can suspend a monitor:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  monitor:
    name: mariadb-monitor
    module: galeramon
    interval: 2s
    cooperativeMonitoring: majority_of_all
    params:
      disable_master_failback: "false"
      available_when_donor: "false"
      disable_master_role_setting: "false"   
    suspend: true
```

## MaxScale GUI

MaxScale offers a shiny user interface that provides very useful information about the [MaxScale resources](#maxscale-resources). You can  enable it providing the following configuration:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale-galera
spec:
...
  admin:
    port: 8989
    guiEnabled: true
  guiKubernetesService:
    type: LoadBalancer
    metadata:
      metadata:
        annotations:
          metallb.universe.tf/loadBalancerIPs: 172.18.0.231
```

The GUI is exposed via a dedicated Kubernetes `Service` in the same port as the [MaxScale API](#maxscale-api). Once you access, you will need to enter the [MaxScale API](#maxscale-api) credentials configured by `mariadb-operator` in a `Secret`. See the [Authentication](#authentication) section for more details.

![MaxScale GUI](https://mariadb-operator.github.io/mariadb-operator/assets/maxscale-gui.png)

## MaxScale API

`mariadb-operator` interacts with the [MaxScale REST API](https://mariadb.com/kb/en/mariadb-maxscale-23-08-rest-api/) to reconcile the specification provided by the user, considering both the MaxScale status retrieved from the API and the provided spec.

[<img src="https://run.pstmn.io/button.svg" alt="Run In Postman" style="width: 128px; height: 32px;">](https://www.postman.com/mariadb-operator/workspace/mariadb-operator/collection/9776-74dfd54a-2b2b-451f-95ab-006e1d9d9998?action=share&creator=9776&active-environment=9776-a841398f-204a-48c8-ac04-6f6c3bb1d268)

## Troubleshooting

`mariadb-operator` tracks both the `MaxScale` status in regards to Kubernetes resources as well as the status of the [MaxScale API](#maxscale-api) resources. This information is available on the status field of the `MaxScale` resource, it may be very useful for debugging purposes:

```yaml
status:
  conditions:
  - lastTransitionTime: "2024-02-08T17:29:01Z"
    message: Running
    reason: MaxScaleReady
    status: "True"
    type: Ready
  configSync:
    databaseVersion: 20
    maxScaleVersion: 20
  listeners:
  - name: rconn-master-router-listener
    state: Running
  - name: rconn-slave-router-listener
    state: Running
  - name: rw-router-listener
    state: Running
  monitor:
    name: galeramon-monitor
    state: Running
  primaryServer: mariadb-galera-1
  replicas: 1
  servers:
  - name: mariadb-galera-0
    state: Slave, Synced, Running
  - name: mariadb-galera-1
    state: Master, Synced, Running
  - name: mariadb-galera-2
    state: Slave, Synced, Running
  services:
  - name: rconn-master-router
    state: Started
  - name: rconn-slave-router
    state: Started
  - name: rw-router
    state: Started
```

Kubernetes events emitted by `mariadb-operator` may also be very relevant for debugging. For instance, an event is emitted whenever the primary server changes:

```bash
kubectl get events --field-selector involvedObject.name=mariadb-repl-maxscale --sort-by='.lastTimestamp'

LAST SEEN   TYPE      REASON                         OBJECT                           MESSAGE
24s         Normal    MaxScalePrimaryServerChanged   maxscale/mariadb-repl-maxscale   MaxScale primary server changed from 'mariadb-repl-0' to 'mariadb-repl-1'
```

`mariadb-operator` logs can also be a good source of information for troubleshooting. You can increase its verbosity and enable [MaxScale API](#maxscale-api) request logs by running:

```bash
helm upgrade --install mariadb-operator mariadb-operator/mariadb-operator --set logLevel=debug --set extraArgs={--log-maxscale}
```

### Common errors

#### Permission denied writing `/var/lib/maxscale`

This error occurs when the user that runs the container does not have enough privileges to write in `/var/lib/maxscale`:

```bash
Failed to create directory '/var/lib/maxscale/maxscale.cnf.d': 13, Permission denied
```

To mitigate this, by default, the operator sets the following `securityContext` in the `MaxScale`'s `StatefulSet`:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: maxscale-galera
spec:
  securityContext:
    fsGroup: 996
    runAsGroup: 996
    runAsNonRoot: true
    runAsUser: 998
```

This enables the `CSIDriver` and the kubelet to recursively set the ownership ofr the `/var/lib/maxscale` folder to the group `996`, which is the one expected by MaxScale. It is important to note that not all the `CSIDrivers` implementations support this feature, see the [CSIDriver documentation](https://kubernetes-csi.github.io/docs/support-fsgroup.html) for further information.

## Reference
- [API reference](./API_REFERENCE.md)
- [Example suite](../examples/)
