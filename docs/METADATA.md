# Metadata

This documentation shows how to configure metadata in the `mariadb-operator` CRs.

## Children object metadata

`MariaDB` and `MaxScale` resources allow you to propagate metadata to all the children objects by specifying the `inheritMetadata` field:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb-galera
spec:
  inheritMetadata:
    labels:
      database.myorg.io: mariadb
    annotations:
      database.myorg.io: mariadb
```

This means that all the reconciled objects will inherit these labels and annotations. For instance, see the `Services` and `Pods`:

```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    database.myorg.io: mariadb
  labels:
    database.myorg.io: mariadb
  name: mariadb-galera-primary
  namespace: default
``` 

```yaml
apiVersion: v1
kind: Pod
metadata:
  annotations:
    database.myorg.io: mariadb
  labels:
    database.myorg.io: mariadb
  name: mariadb-galera-0
  namespace: default
``` 

## `Pod` metadata

You have the ability to provide dedicated metadata for `Pods` by specifying the `podMetadata` field in any CR that reconciles a `Pod`, for instance: `MariaDB`, `MaxScale`, `Backup`, `Restore` and `SqlJobs`:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: Backup
metadata:
  name: backup
spec:
  inheritMetadata:
    labels:
      sidecar.istio.io/inject: "true"
    annotations:
      database.myorg.io: mariadb
  podMetadata:
    labels:
      sidecar.istio.io/inject: "false"
```

It is important to note that the `podMetadata` field superseeds the `inheritMetadata` field, therefore the labels and annotations provided in the former will override the ones in the latter.

## `Service` metadata

Provision dedicated metadata for `Services` in the `MariaDB` resources can be done via the `service`, `primaryService` and `secondaryService` fields:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb-galera
spec:
  service:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.150

  primaryService:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.160

  secondaryService:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.161
```

In the case of `MaxScale`, you can also do this via the `kubernetesService` field.

Refer to the [HA documentation](./HA.md) to know more about the `Service` fields and `MaxScale`.

## `PVC` metadata

Both `MariaDB` and `MaxScale` allow you to define a `volumeClaimTemplate` to be used by the underlying `StatefulSet`. You may also define metadata for it:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb-galera
spec:
  storage:
    size: 1Gi
    volumeClaimTemplate:
      metadata:
        annotations:
          database.myorg.io: mariadb
        labels:
          database.myorg.io: mariadb
      accessModes:
      - ReadWriteOnce
      resources:
        requests:
          storage: 1Gi
```

## Use cases

Being able to provide metadata allows you to integrate with other CNCF landscape projects:

#### Metallb

If you run on bare metal and you use [Metallb](https://metallb.universe.tf/) for managing the `LoadBalancer` objects, you can declare its IPs via annotations:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb-galera
spec:
  service:
    type: LoadBalancer
    metadata:
      annotations:
        metallb.universe.tf/loadBalancerIPs: 172.18.0.150
```

#### Istio

[Istio](https://istio.io/) injects the data plane container to all `Pods`, but you might want to opt-out of this feature in some cases:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: Backup
metadata:
  name: backup
spec:
  podMetadata:
    labels:
      sidecar.istio.io/inject: "false"
```

For instance, you probably don't want to inject the Istio sidecar to `Backup` `Pods`, as it will prevent the `Jobs` from finishing and therefore your backup process will hang.

## Reference
- [API reference](./API_REFERENCE.md)
- [Example suite](../examples/)
