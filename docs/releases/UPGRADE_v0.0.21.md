# Upgrade guide v0.0.21

> [!NOTE]  
> APIs are currently in `v1alpha1`, which implies that non backward compatible changes might happen. See [Kubernetes API versioning](https://kubernetes.io/docs/reference/using-api/#api-versioning) for more detail.

This guide illustrates, step by step, how to migrate to `v0.0.21` from previous versions, as some breaking changes have been introduced. See:
- https://github.com/mariadb-operator/mariadb-operator/pull/248

Follow these steps for upgrading:

- Uninstall you current `mariadb-operator` for preventing conflicts:
```bash
helm uninstall mariadb-operator
```
Alternatively, you may only downscale and delete the webhook configurations:
```bash
kubectl scale deployment mariadb-operator -n default --replicas=0
kubectl scale deployment mariadb-operator-webhook -n default --replicas=0
kubectl delete validatingwebhookconfiguration mariadb-operator-webhook
kubectl delete mutatingwebhookconfiguration mariadb-operator-webhook
```

- In case you are manually applying manifests, get a copy of your `MariaDB` resources, as the CRD upgrade will wipe out fields that are no longer supported:
```bash
kubectl get mariadb mariadb-repl -n default -o yaml > mariadb-repl.yaml
```

- Upgrade CRDs to `v0.0.21`:
> [!IMPORTANT]  
> Helm does not handle CRD upgrades. See [helm docs](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#some-caveats-and-explanations).

> [!WARNING]  
> This step will delete fields that are no longer supported in your resources.
```bash
kubectl replace -f https://github.com/mariadb-operator/mariadb-operator/releases/download/helm-chart-0.21.0/crds.yaml
```

- Perform migrations in your `MariaDB` resouces:
  - `MariaDB` standalone migration
   ```diff
   - image:
  -   repository: mariadb
  -   tag: "11.0.3"
  -   pullPolicy: IfNotPresent
  + image: mariadb:11.0.3
  + imagePullPolicy: IfNotPresent
   ```
   - `MariaDB` galera migration
    ```diff
   - image:
  -   repository: mariadb
  -   tag: "11.0.3"
  -   pullPolicy: IfNotPresent
  + image: mariadb:11.0.3
  + imagePullPolicy: IfNotPresent
  galera:
    agent:
  -   image:
  -     repository: ghcr.io/mariadb-operator/mariadb-operator
  -     tag: "v0.0.25"
  -     pullPolicy: IfNotPresent
  +   image: ghcr.io/mariadb-operator/mariadb-operator:v0.0.26
  +   imagePullPolicy: IfNotPresent
    initContainer:
  -   image:
  -     repository: ghcr.io/mariadb-operator/init
  -     tag: "v0.0.5"
  -     pullPolicy: IfNotPresent
  +   image: ghcr.io/mariadb-operator/init:v0.0.6
  +   imagePullPolicy: IfNotPresent
   ```
 
-  Upgrade `mariadb-operator` to `v0.0.21`:
```bash 
helm repo update mariadb-operator
helm upgrade --install mariadb-operator mariadb-operator/mariadb-operator --version 0.21.0 
```

- If you previously decided to downscale the operator, make sure you upscale it back:
```bash
kubectl scale deployment mariadb-operator -n default --replicas=1
kubectl scale deployment mariadb-operator-webhook -n default --replicas=1
```