# OpenShift

> [!NOTE]  
> This documentation applies to `mariadb-operator` version >= v0.0.31

This documentation provides guidance on installing the certified [MariaDB Enterprise](https://mariadb.com/products/enterprise/) operator for OpenShift. Please refer to the Red Hat documentation for further detail about the [Red Hat OpenShift Certification program](https://connect.redhat.com/en/partner-with-us/red-hat-openshift-certification).

Operators are deployed into OpenShift by using [Operator Lifecycle Manager](https://docs.redhat.com/en/documentation/openshift_container_platform/4.2/html/operators/understanding-the-operator-lifecycle-manager-olm#olm-overview_olm-understanding-olm), which facilitates the installation, updates, and overall management of their lifecycle.

## Table of contents
<!-- toc -->
- [`PackageManifest`](#packagemanifest)
- [Certified images and registry](#certified-images-and-registry)
- [Channels](#channels)
- [`SecurityContextConstraints`](#channels)
- [Installation in all namespaces](#installation-in-all-namespaces)
- [Installation in specific namespaces](#installation-in-specific-namespaces)
- [OpenShift console](#openshift-console)
<!-- /toc -->

## `PackageManifest`

You can install the certified operator in OpenShift clusters that have the `mariadb-operator-enterprise` `packagemanifest` available. In order to check this, run the following command:

```bash
oc get packagemanifests -n openshift-marketplace mariadb-operator-enterprise

NAME                          CATALOG                 AGE
mariadb-operator-enterprise   Certified Operators     21h
``` 

## Certified images and registry

The certified operator and its related operands make use of certified [Red Hat UBI](https://catalog.redhat.com/software/base-images) based images fully complaint with the [Red Hat OpenShift Certification program](https://connect.redhat.com/en/partner-with-us/red-hat-openshift-certification). Refer to the [image documentation](./DOCKER.md) for getting a list of compatible images.

It is important to note that [MariaDB Enterprise](https://mariadb.com/products/enterprise/) server image is behind a private registry and therefore accessible only by MariaDB customers. Credentials may be configured by running the commands below:
- Extract your [global pull secret](https://docs.openshift.com/container-platform/4.10/openshift_images/managing_images/using-image-pull-secrets.html#images-update-global-pull-secret_using-image-pull-secrets):
```bash
oc extract secret/pull-secret -n openshift-config --confirm
``` 
- Obtain a [customer download token](https://mariadb.com/docs/server/deploy/deployment-methods/docker/enterprise-server/).
- Login in the MariaDB registry providing the previous token as password:
```bash
oc registry login \
  --registry="docker-registry.mariadb.com" \
  --auth-basic="<email>:<customer-download-token>" \
  --to=.dockerconfigjson
```
- Update the [global pull secret](https://docs.openshift.com/container-platform/4.10/openshift_images/managing_images/using-image-pull-secrets.html#images-update-global-pull-secret_using-image-pull-secrets):
```bash
oc set data secret/pull-secret -n openshift-config --from-file=.dockerconfigjson
```

Alternatively, instead of updating the [global pull secret](https://docs.openshift.com/container-platform/4.10/openshift_images/managing_images/using-image-pull-secrets.html#images-update-global-pull-secret_using-image-pull-secrets), you can configure credentials on a per-`MariaDB` basis:
- Create a `Secret` with the registry credentials as described in the [Kubernetes documentation](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/).
- Provide an `imagePullSecrets` field to your `MariaDB` resource as described in the [registry documentation](./REGISTRY.md).

## Channels

Currently, since the certified operator is in beta phase, we only provide `fast` as a release channel. Once it reaches GA, we have plans to create a `stable` channel.

You can read more about [channels in the OLM documentation](https://olm.operatorframework.io/docs/best-practices/channel-naming/)

## `SecurityContextConstraints`

Both the operator and the operand `Pods` run with the `restricted-v2` `SecurityContextConstraint`, the most restrictive SCC in OpenShift in terms of container permissions. This implies that OpenShift automatically assigns a `SecurityContext` for the `Pods` with minimum permissions, for example:

```yaml
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
  runAsNonRoot: true
  runAsUser: 1000660000
```

> [!IMPORTANT]  
>  OpenShift does not assign `SecurityContexts` in the `default` and `kube-system` namespaces. Please refrain from deploying operands on them, as it will result in permission errors when trying to write to the filesystem.

You can read more about [Security Context Constraints in the OpenShift documentation](https://docs.openshift.com/container-platform/4.16/authentication/managing-security-context-constraints.html)

## Installation in all namespaces

To install the operator watching resources on all namespaces, you need to to create a `Subscription` object for `mariadb-operator-enterprise` using the `fast` channel. This will use a default `OperatorGroup` called `global-operators`: 

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: mariadb-operator-enterprise
  namespace: openshift-operators
spec:
  channel: fast
  installPlanApproval: Automatic
  name: mariadb-operator-enterprise
  source: certified-operators
  sourceNamespace: openshift-marketplace
  startingCSV: mariadb-operator-enterprise.v0.0.31
```

## Installation in specific namespaces

In order to define which namespaces the operator will be watching, you need to create an `OperatorGroup`:

```yaml
apiVersion: operators.coreos.com/v1
kind: OperatorGroup
metadata:
  name: mariadb-operator-enterprise
  namespace: mariadb
spec:
  targetNamespaces:
  - mariadb
  - mariadb-foo
  - mariadb-bar
  upgradeStrategy: Default
``` 
You can read more about [`OperatorGroups` in the OLM documentation](https://olm.operatorframework.io/docs/advanced-tasks/operator-scoping-with-operatorgroups/#configuring-operatorgroups).

Then, the operator can be installed by creating a `Subscription`:

```yaml
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: mariadb-operator-enterprise
  namespace: openshift-operators
spec:
  channel: fast
  installPlanApproval: Automatic
  name: mariadb-operator-enterprise
  source: certified-operators
  sourceNamespace: openshift-marketplace
  startingCSV: mariadb-operator-enterprise.v0.0.31
``` 

## OpenShift console

As an alternative to create `Subscription` objects via the command line, you can install operators by using the OpenShift console. Go to the `Operators > OperatorHub` section and search by `mariadb`: 

![Certified Operator](https://mariadb-operator.github.io/mariadb-operator/assets/certified-operator.png)

Select `MariaDB Operator Enterprise`, click on install, and you will be able to create a `Subscription` object via the UI.

Once deployed, the operator comes with a small suite of examples that can be deployed from the console directly. For instance, to create a `MariaDB`:

![Create a MariaDB via OpenShift console](https://mariadb-operator.github.io/mariadb-operator/assets/openshift-console-form.png)

As you can see in the previous screenshot, the form view that the OpenShift console offers is limited, we recommend using the YAML view:

![Create a MariaDB via OpenShift console](https://mariadb-operator.github.io/mariadb-operator/assets/openshift-console-yaml.png)

Refer to the [full suite of examples](../examples/) and the [API reference](./API_REFERENCE.md) for further detail.