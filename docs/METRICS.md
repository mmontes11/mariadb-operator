# Prometheus metrics

`mariadb-operator` is able to provision MariaDB instances and automatically configure [Prometheus](https://github.com/prometheus/prometheus) to scrape their metrics so they can be used later on to build [Grafana dashboards](#grafana-dashboards) or trigger Alertmanager alerts.

## Operator metrics

In order to expose the operator internal metrics, please refer to the [recommended installation](../README.md#recommended-installation) flavour.

## Exporter

The operator configures a [prometheus/mysqld-exporter](https://github.com/prometheus/mysqld_exporter) exporter to query MariaDB and export the metrics in Prometheus format via an http endpoint.

It is important to note that, we run this exporter as an standalone `Deployment` and not as a sidecar alongside every MariaDB replica. This implies that the MariaDB lifecycle is not coupled to the exporter one, so we can upgrade them independently without affecting the availability of the other.

For being able to do this, we rely on the [multi-target](https://github.com/prometheus/mysqld_exporter?tab=readme-ov-file#multi-target-support) feature introduced in the [v0.15.0](https://github.com/prometheus/mysqld_exporter/releases/tag/v0.15.0) of [prometheus/mysqld-exporter](https://github.com/prometheus/mysqld_exporter), so make sure to specify at least v0.15.0 in the exporter image.


## `ServiceMonitor`

Once the exporter `Deployment` is ready, `mariadb-operator` creates a [ServiceMonitor](https://prometheus-operator.dev/docs/operator/api/#monitoring.coreos.com/v1.ServiceMonitor) object that will be eventually reconciled by the [Prometheus operator ](https://github.com/prometheus-operator/prometheus-operator), resulting in the Prometheus instance being configured to scrape the exporter endpoint.

As you scale your MariaDB with more or less replicas, `mariadb-operator` will reconcile the `ServiceMonitor` to add/remove targets related to the MariaDB instances. 

## Configuration

The easiest way to setup metrics in your MariaDB instance is just by setting `spec.metrics.enabled = true`, like in this [example](../examples/manifests/mariadb_metrics.yaml):

```yaml
apiVersion: k8s.mariadb.com/v1alpha1
kind: MariaDB
metadata:
  name: mariadb
spec:
...
  metrics:
    enabled: true
```

The rest of the fields are defaulted by the operator. If you need a more fine grained configuration, refer to the [API reference](./API_REFERENCE.md) and take a look at this [example](../examples/manifests/mariadb_metrics_full.yaml):

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
      resources:
        requests:
          cpu: 50m
          memory: 64Mi
        limits:
          cpu: 300m
          memory: 512Mi
      port: 9104
    serviceMonitor:
      prometheusRelease: kube-prometheus-stack
      jobLabel: mariadb-monitoring
      interval: 10s
      scrapeTimeout: 10s
    username: monitoring
    passwordSecretKeyRef:
      name: mariadb
      key: password
```

## Prometheus reference installation

The easiest way to spin up a Prometheus observability stack in Kubernetes is by installing the [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) helm chart.

We leverage this chart in our local [development](./DEVELOPMENT.md) environment and we have [configured](../hack/config/kube-prometheus-stack.yaml) it to ensure quality interactions between `mariadb-operator` and Prometheus. Feel free to install it by running:

```bash
make cluster
make install-prometheus
```

## Grafana dashboards

To visualize MariaDB metrics, our [Prometheus reference installation](#prometheus-reference-installation) has some interesting Grafana dashboards [configured](../hack/config/kube-prometheus-stack.yaml) that make use of the metrics configured by `mariadb-operator`. They are all available on [grafana.com](https://grafana.com/grafana/dashboards/):


__[MySQL Overview](https://grafana.com/grafana/dashboards/7362-mysql-overview/)__

__[MySQL Exporter Quickstart and Dashboard](https://grafana.com/grafana/dashboards/14057-mysql/)__


__[MySQL Replication](https://grafana.com/grafana/dashboards/7371-mysql-replication/)__


__[Galera/MariaDB - Overview](https://grafana.com/grafana/dashboards/13106-galera-mariadb-overview/)__

## Reference
- [API reference](./API_REFERENCE.md)
- [Example suite](../examples/)
