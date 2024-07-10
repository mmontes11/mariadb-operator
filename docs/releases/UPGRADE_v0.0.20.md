# Upgrade guide v0.0.20

> [!NOTE]  
> APIs are currently in `v1alpha1`, which implies that non backward compatible changes might happen. See [Kubernetes API versioning](https://kubernetes.io/docs/reference/using-api/#api-versioning) for more detail.

This guide illustrates, step by step, how to migrate to `v0.0.20` from previous versions, as some breaking changes have been introduced. See:
- https://github.com/mariadb-operator/mariadb-operator/pull/197
- https://github.com/mariadb-operator/mariadb-operator/pull/211

It's important to note that this migration process only applies if:
- You have created a `MariaDB` resource using a `mariadb-operator` version < `v0.0.20` and you are upgrading to >= `v0.0.20`.
- Your `MariaDB` resource has `spec.replication` enabled.

If that's your case follow these steps for upgrading:

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

- Upgrade CRDs to `v0.0.20`:
> [!IMPORTANT]  
> Helm does not handle CRD upgrades. See [helm docs](https://helm.sh/docs/chart_best_practices/custom_resource_definitions/#some-caveats-and-explanations).

> [!WARNING]  
> This step will delete fields that are no longer supported in your resources.
```bash
kubectl replace -f https://github.com/mariadb-operator/mariadb-operator/releases/download/helm-chart-0.20.0/crds.yaml
```

- Perform migrations in your `MariaDB` resouces:
  - Set `spec.replication.enabled = true`.
  - If you had previously set `spec.replication.primary.service`, move it to `spec.primaryService`.
  - Rename your resources to point to the new `<mariadb-name>-primary` `Service` instead of  `primary-<mariadb-name>`.
  - If you had previously set  `spec.replication.primary.connection`, move it to `spec.primaryConnection`.
  - Rename your resources to point to the new `<mariadb-name>-primary` `Connection` instead of  `primary-<mariadb-name>`.
 
-  Upgrade `mariadb-operator` to `v0.0.20`:
```bash 
helm repo update mariadb-operator
helm upgrade --install mariadb-operator mariadb-operator/mariadb-operator --version 0.20.0 
```

- If you previously decided to downscale the operator, make sure you upscale it back:
```bash
kubectl scale deployment mariadb-operator -n default --replicas=1
kubectl scale deployment mariadb-operator-webhook -n default --replicas=1
```