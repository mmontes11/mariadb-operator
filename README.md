<p align="center" width="100%">
<img src="https://mariadb-operator.github.io/mariadb-operator/assets/mariadb_centered_whitebg.svg" alt="mariadb" width="500"/>
</p>

<p align="center">
<a href="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/ci.yml"><img src="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
<a href="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/release.yml"><img src="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/release.yml/badge.svg" alt="Release"></a>
<a href="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/helm.yml"><img src="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/helm.yml/badge.svg" alt="Helm"></a>
<a href="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/helm-release.yml"><img src="https://github.com/mariadb-operator/mariadb-operator/actions/workflows/helm-release.yml/badge.svg" alt="Helm release"></a>
</p>

<p align="center">
<a href="https://goreportcard.com/report/github.com/mariadb-operator/mariadb-operator"><img src="https://goreportcard.com/badge/github.com/mariadb-operator/mariadb-operator" alt="Go Report Card"></a>
<a href="https://pkg.go.dev/github.com/mariadb-operator/mariadb-operator"><img src="https://pkg.go.dev/badge/github.com/mariadb-operator/mariadb-operator.svg" alt="Go Reference"></a>
<a href="https://r.mariadb.com/join-community-slack"><img alt="Slack" src="https://img.shields.io/badge/slack-join_chat-blue?logo=Slack&label=slack&style=flat"></a>
<a href="https://artifacthub.io/packages/helm/mariadb-operator/mariadb-operator"><img src="https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/mariadb-operator" alt="Artifact Hub"></a>
<a href="https://operatorhub.io/operator/mariadb-operator"><img src="https://img.shields.io/badge/Operator%20Hub-mariadb--operator-red" alt="Operator Hub"></a>
</p>

# 🦭 mariadb-operator

Run and operate MariaDB in a cloud native way. Declaratively manage your MariaDB using Kubernetes [CRDs](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/) rather than imperative commands.
- [Easily provision](./examples/manifests/mariadb_minimal.yaml) MariaDB servers in Kubernetes.
- [Highly configurable](./examples/manifests/mariadb_full.yaml) MariaDB servers.
- Multiple [HA modes](./docs/HA.md): Galera and SemiSync Replication.
- Automated [primary failover](./docs/HA.md).
- Automated [Galera cluster recovery](./docs/GALERA.md#galera-cluster-recovery).
- Enhanced HA with [MaxScale](./docs/MAXSCALE.md): a sophisticated database proxy, router, and load balancer designed specifically for and by MariaDB.
- Flexible [storage](./docs/STORAGE.md) configuration. [Volume expansion](./docs/STORAGE.md#volume-resize).
- Take and restore [backups](./docs/BACKUP.md). 
- Scheduled [backups](./docs/BACKUP.md/#scheduling). 
- Multiple [backup storage types](./docs/BACKUP.md#storage-types): S3 compatible, PVCs and Kubernetes volumes.
- [Backup retention policy](./docs/BACKUP.md#retention-policy).
- [Target recovery time](./docs/BACKUP.md#target-recovery-time): infer which backup to restore.
- [Bootstrap new instances](./docs/BACKUP.md#bootstrap-new-mariadb-instances-from-backups) from: Backups, S3, PVCs ...
- [Cluster aware rolling update](./docs/UPDATES.md): roll out replica Pods one by one, wait for each of them to become ready, and then proceed with the primary Pod.
- [my.cnf configuration](./docs/CONFIGURATION.md#mycnf). Automatically trigger [rolling updates](./docs/UPDATES.md) when my.cnf changes.
- [Suspend](./docs/SUSPEND.md) operator reconciliation for maintenance and progressive upgrades.
- [Prometheus metrics](./docs/METRICS.md) via [mysqld-exporter](https://github.com/prometheus/mysqld_exporter).
- Declaratively manage [SQL resources](./docs/SQL_RESOURCES.md): [users](./examples/manifests/user.yaml), [grants](./examples/manifests/grant.yaml) and logical [databases](./examples/manifests/database.yaml).
- Configure [connections](./examples/manifests/connection.yaml) for your applications.
- Orchestrate and schedule [sql scripts](./examples/manifests/sqljobs).
- Validation webhooks to provide CRD immutability.
- Additional printer columns to report the current CRD status.
- CRDs designed according to the Kubernetes [API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md).
- [GitOps](#gitops) friendly.
- Multi-arch distroless based [image](https://github.com/orgs/mariadb-operator/packages/container/package/mariadb-operator).
- Install it using [kubectl](./deploy/manifests), [helm](https://artifacthub.io/packages/helm/mariadb-operator/mariadb-operator) or [OLM](https://operatorhub.io/operator/mariadb-operator).

Please, refer to the [documentation](./docs/), the [API reference](./docs/API_REFERENCE.md) and the [example suite](./examples/) for further detail.

## Bare minimum installation

This installation flavour provides the minimum resources required to run `mariadb-operator` in your cluster.

```bash
helm repo add mariadb-operator https://helm.mariadb.com/mariadb-operator
helm install mariadb-operator mariadb-operator/mariadb-operator
```
## Recommended installation

The recommended installation includes the following features:
- **Metrics**: Leverage [prometheus operator](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) to scrape the `mariadb-operator` internal metrics.
- **Webhook certificate renewal**: Automatic webhook certificate issuance and renewal using  [cert-manager](https://cert-manager.io/docs/installation/). By default, a static self-signed certificate is generated.

```bash
helm repo add mariadb-operator https://helm.mariadb.com/mariadb-operator
helm install mariadb-operator mariadb-operator/mariadb-operator \
  --set metrics.enabled=true --set webhook.cert.certManager.enabled=true
```

## Openshift installation

We have 2 different operator flavours to install MariaDB in OpenShift:
- Certified operator for MariaDB Enterprise. This is the recommended way, please refer to the [OpenShift documentation](./docs/OPENSHIFT.md).
- Helm-based operator for MariaDB Community. It is maintained on a best-effort basis and managed by the [mariadb-operator-helm repository](https://github.com/mariadb-operator/mariadb-operator-helm).

## Image compatibility
`mariadb-operator` is only compatible with official MariaDB images. Refer to the [images documentation](./docs/DOCKER.md) for further detail.

## MariaDB compatibility
- MariaDB Community >= 10.5
- MariaDB Enterprise >= 10.5

## Kubernetes compatibility
- Kubernetes >= 1.26
- OpenShift >= 1.13

## Migrate your MariaDB instance to Kubernetes

This [migration guide](./docs/BACKUP.md#migrating-an-external-mariadb-to-a-mariadb-running-in-kubernetes) will streamline your onboarding process and assist you in migrating your data into a `MariaDB` instance running on Kubernetes.

## Quickstart

Let's see `mariadb-operator`🦭 in action! First of all, install the following configuration manifests that will be referenced by the CRDs further:
```bash
kubectl apply -f examples/manifests/config
```

Next, you can proceed with the installation of a `MariaDB` instance:
```bash
kubectl apply -f examples/manifests/mariadb.yaml
```
```bash
kubectl get mariadbs
NAME      READY   STATUS    PRIMARY POD     AGE
mariadb   True    Running   mariadb-0       3m57s

kubectl get statefulsets
NAME      READY   AGE
mariadb   1/1     2m12s

kubectl get services
NAME         TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)             AGE
mariadb      ClusterIP   10.96.235.145   <none>        3306/TCP,9104/TCP   2m17s
```
Up and running 🚀, we can now create our first logical database and grant access to users:
```bash
kubectl apply -f examples/manifests/database.yaml
kubectl apply -f examples/manifests/user.yaml
kubectl apply -f examples/manifests/grant.yaml
```
```bash
kubectl get databases
NAME        READY   STATUS    CHARSET   COLLATE           AGE
data-test   True    Created   utf8      utf8_general_ci   22s

kubectl get users
NAME              READY   STATUS    MAXCONNS   AGE
user              True    Created   20         29s

kubectl get grants
NAME              READY   STATUS    DATABASE   TABLE   USERNAME          GRANTOPT   AGE
user              True    Created   *          *       user              true       36s
```
At this point, we can run our database initialization scripts:
```bash
kubectl apply -f examples/manifests/sqljobs
```
```bash
kubectl get sqljobs
NAME       COMPLETE   STATUS    MARIADB   AGE
01-users   True       Success   mariadb   2m47s
02-repos   True       Success   mariadb   2m47s
03-stars   True       Success   mariadb   2m47s

kubectl get jobs
NAME                  COMPLETIONS   DURATION   AGE
01-users              1/1           10s        3m23s
02-repos              1/1           11s        3m13s
03-stars-28067562     1/1           10s        106s

kubectl get cronjobs
NAME       SCHEDULE      SUSPEND   ACTIVE   LAST SCHEDULE   AGE
03-stars   */1 * * * *   False     0        57s             2m33s
```

Now that the database has been initialized, let's take a backup:
```bash
kubectl apply -f examples/manifests/backup.yaml
``` 
```bash
kubectl get backups
NAME               COMPLETE   STATUS    MARIADB   AGE
backup             True       Success   mariadb   15m

kubectl get jobs
NAME               COMPLETIONS   DURATION   AGE
backup-27782894    1/1           4s         3m2s
```
Last but not least, let's provision a second `MariaDB` instance bootstrapping from the previous backup:
```bash
kubectl apply -f examples/manifests/mariadb_from_backup.yaml
``` 
```bash
kubectl get mariadbs
NAME                  READY   STATUS    PRIMARY POD             AGE
mariadb               True    Running   mariadb-0               7m47s
mariadb-from-backup   True    Running   mariadb-from-backup-0   53s

kubectl get restores
NAME                                         COMPLETE   STATUS    MARIADB               AGE
bootstrap-restore-mariadb-from-backup        True       Success   mariadb-from-backup   72s

kubectl get jobs
NAME                                         COMPLETIONS   DURATION   AGE
backup                                       1/1           9s         12m
bootstrap-restore-mariadb-from-backup        1/1           5s         84s
``` 

## Documentation

- [Index](./docs/)
- [API reference](./docs/API_REFERENCE.md)
- [Example suite](./examples/)

## GitOps

You can embrace [GitOps](https://opengitops.dev/) best practises by using this operator, just place your CRDs in a git repo and reconcile them with your favorite tool, see an example with [flux](https://fluxcd.io/):
- [Run and operate MariaDB in a GitOps fashion using Flux](./examples/flux/)

## Roadmap

Take a look at our [roadmap](./ROADMAP.md) and feel free to open an issue to suggest new features.

## Adopters

Please create a PR and add your company or project to our [ADOPTERS.md file](./ADOPTERS.md) if you are using our project!

## Contributing

We welcome and encourage contributions to this project! Please check our [contributing](./CONTRIBUTING.md) and [development](./docs/DEVELOPMENT.md) guides. PRs welcome!

## Community

- [We Tested and Compared 6 Database Operators. The Results are In!](https://www.youtube.com/watch?v=l33pcnQ4cUQ&t=17m25s) - KubeCon EU, March 2024
- [Get Started with MariaDB in Kubernetes and mariadb-operator](https://mariadb.com/resources/blog/get-started-with-mariadb-in-kubernetes-and-mariadb-operator/) - MariaDB Corporation blog, February 2024
- [Run and operate MariaDB in Kubernetes with mariadb-operator](https://mariadb.org/mariadb-in-kubernetes-with-mariadb-operator/) - MariaDB Foundation blog, July 2023
- [L'enfer des DB SQL sur Kubernetes face à la promesse des opérateurs](https://www.youtube.com/watch?v=d_ka7PlWo1I&t=2415s&ab_channel=KCDFrance) - KCD France, March 2023

## Get in touch

- [Slack](https://r.mariadb.com/join-community-slack)
- mariadb-operator@proton.me
