# Registry

This documentation aims to provide guidance on how to configure private registries in the `mariadb-operator` CRs.

## Credentials

The first requirement to access a private registry is having credentials in the cluster that will be pulling the images. Kubernetes has a specific type of `Secret` `kubernetes.io/dockerconfigjson` designed specifically for this purpose. Please, refer to the [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/) to see how it can be configured.

For convenience, this repo provides a make target that can be used to configure your existing credentials available in `~/.docker/config` as a `kubernetes.io/dockerconfigjson` `Secret`:

```bash
REGISTRY_PULL_SECRET=registry DOCKER_CONFIG=~/.docker/config.json make registry-secret
```

## `MariaDB` 

In order to configure a private registry in your `MariaDB` resource, you can specify multiple `imagePullSecrets`:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb
spec:
  ...
  image: docker.mariadb.com/enterprise-server:10.6
  imagePullPolicy: IfNotPresent
  imagePullSecrets:
    - name: registry
    - name: another-registry
```
As a result, the `Pods` created as part of the reconciliation process will have the `imagePullSecrets`.

You can also configure credentials for the metrics exporter:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb
spec:
  ...
  metrics:
    enabled: true
    exporter:
      image: prom/mysqld-exporter:v0.15.1
      imagePullPolicy: IfNotPresent
      imagePullSecrets:
        - name: registry
```

By default, the metrics exporter `Pod` will inherit the credentials specified in the `imagePullSecrets` root field, but you can also specify dedicated credentials via `metrics.exporter.imagePullSecrets`.

## `MaxScale`

Similarly to `MariaDB`, you are able to configure private registries in your `MaxScale` resource:

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MaxScale
metadata:
  name: maxscale
spec:
  ...
  image: mariadb/maxscale:23.08
  imagePullPolicy: IfNotPresent
  imagePullSecrets:
    - name: registry
```

## `Backup`, `Restore` and `SqlJob`

The batch `Job` resources will inherit the `imagePullSecrets` from the referred `MariaDB`, as they also make use of its `image`. However, you are also able to provide dedicated `imagePullSecrets` for these resources:


```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb
spec:
  ...
  image: docker.mariadb.com/enterprise-server:10.6
  imagePullPolicy: IfNotPresent
  imagePullSecrets:
    - name: registry
```
```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: Backup
metadata:
  name: backup
spec:
  ...
  mariaDbRef:
    name: mariadb
  imagePullSecrets:
    - name: backup-registry
```

When the resources from the above examples are created, a `Job` with both `registry` and `backup-registry` `imagePullSecrets` will be reconciled.