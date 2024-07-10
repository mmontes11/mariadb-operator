# Upgrade guide v0.0.24

> [!NOTE]  
> APIs are currently in `v1alpha1`, which implies that non backward compatible changes might happen. See [Kubernetes API versioning](https://kubernetes.io/docs/reference/using-api/#api-versioning) for more detail.

This guide illustrates, step by step, how to migrate to `v0.0.24` from previous versions, as some breaking changes have been introduced. See:

`MariaDB`
- https://github.com/mariadb-operator/mariadb-operator/pull/248
- https://github.com/mariadb-operator/mariadb-operator/pull/312

`Backup`
- https://github.com/mariadb-operator/mariadb-operator/pull/314

`Restore`
- https://github.com/mariadb-operator/mariadb-operator/pull/308

Follow these steps for upgrading:

- Uninstall you current `mariadb-operator` for preventing conflicts:
```bash
helm uninstall mariadb-operator
```
Alternatively, you may only downscale and delete the webhook configurations:
```bash
kubectl scale deployment mariadb-operator --replicas=0
kubectl scale deployment mariadb-operator-webhook --replicas=0
kubectl delete validatingwebhookconfiguration mariadb-operator-webhook
kubectl delete mutatingwebhookconfiguration mariadb-operator-webhook
```

- In case you are manually applying manifests, get a copy of your `MariaDB`, `Backup` and `Restore` resources, as the CRD upgrade will wipe out fields that are no longer supported:
```bash
kubectl get mariadb mariadb -o yaml > mariadb.yaml
kubectl get backup backup -o yaml > backup.yaml
kubectl get restore restore -o yaml > restore.yaml
```

- Upgrade CRDs to `v0.0.24`:
> [!IMPORTANT]  
> Helm does not handle CRD upgrades. See [helm docs](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#some-caveats-and-explanations).

> [!WARNING]  
> This step will delete fields that are no longer supported in your resources.
```bash
kubectl replace -f https://github.com/mariadb-operator/mariadb-operator/releases/download/helm-chart-0.24.0/crds.yaml
```

- Perform migrations in your `MariaDB` resouces:
```diff
  metrics:
+   enabled: true
    exporter:
-      image: prom/mysqld-exporter:v0.14.0
+      image: prom/mysqld-exporter:v0.15.1
```
- Perform migrations in your `Backup` resouces:
```diff
-  maxRetentionDays: 30
+  maxRetention: 720h
```
- Perform migrations in your `Restore` resouces:
```diff
-  fileName: backup.2023-12-19T09:00:00Z.sql
+  targetRecoveryTime: 2023-12-19T09:00:00Z
```
 
-  Upgrade `mariadb-operator` to `v0.0.24`:
```bash 
helm repo update mariadb-operator
helm upgrade --install mariadb-operator mariadb-operator/mariadb-operator --version 0.24.0 
```

- If you previously decided to downscale the operator, make sure you upscale it back:
```bash
kubectl scale deployment mariadb-operator -n default --replicas=1
kubectl scale deployment mariadb-operator-webhook -n default --replicas=1
```

- If you have previously created `MariaDB` instances with metrics enabled and a single replica, we also need to perform the following changes in order to create a new `StatefulSet` with `spec.serviceName` pointing to the internal `Service`(see https://github.com/mariadb-operator/mariadb-operator/issues/319 for context):

```bash
kubectl delete statefulset mariadb --cascade=orphan
kubectl rollout restart statefulset mariadb
```