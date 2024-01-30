# k8s-metacollector

[![Falco Ecosystem Repository](https://github.com/falcosecurity/evolution/blob/main/repos/badges/falco-ecosystem-blue.svg)](https://github.com/falcosecurity/evolution/blob/main/REPOSITORIES.md#ecosystem-scope) [![Incubating](https://img.shields.io/badge/status-incubating-orange?style=for-the-badge)](https://github.com/falcosecurity/evolution/blob/main/REPOSITORIES.md#incubating)

⚠️ The repository is still a work in progress ⚠️

The `k8s-metacollector` is a self-contained module that can be deployed within a Kubernetes cluster to perform the task
of gathering metadata from various Kubernetes resources and subsequently transmitting this collected metadata to
designated subscribers.

## Description

[Falco](https://github.com/falcosecurity/falco) enriches events coming from [syscall event source](https://falco.org/docs/event-sources/) with `metadata` 
coming from other sources, for example Kubernetes API server. Historically, each instance of Falco running in a 
Kubernetes cluster would connect to the Kubernetes API server in order to fetch the metadata for a [subset of 
Kubernetes resources](https://falco.org/docs/reference/rules/supported-fields/#field-class-k8s). This approach works 
well in small Kubernetes cluster but does not scale in large environments. The following issue describes the 
problems that were affecting the old Kubernetes client: https://github.com/falcosecurity/libs/issues/987.

The aim of `k8s-metacollector` is to propose a novel approach to `k8s metadata enrichment` in Falco by moving 
the fetching logic of the metadata to a centralized component. The Falco instances would connect to this component 
and receive the metadata without the need to connect to the Kubernetes API server.
The following image shows the  deployment of `k8s-metacollector` and Falco in a kubernetes cluster.

![image](docs/images/meta-collector-in-cluster.svg "Deployment inside a Kubernetes cluster")

Having a centralized component that connects to the API server and pushes metadata to the Falco instances reduces the 
load on the Kubernetes API server. Keep in mind that Falco is deployed as a DaemonSet, one Falco instance on each node.
It also reduces the number of events sent to the Falco instances by filtering the metadata by the node. A given 
Falco instance running in a given node will receive metadata only for the resources that are related to that node:
* pods running on the node;
* namespaces that contain a pod running on the node;
* deployment, replicaset, replicationcontrollers associated with a pod running on the node;
* services serving a pod running on the node.

The filtering done by `k8s-metacollector` reduces significantly the number of events sent to the Falco instances. 
The metadata received by the subscribers is ready to be used without the need for further processing on the 
subscribers side.



### Functional Guarantees:
The `k8s-metacollector` assures that:
* subscribers (Falco instances) at subscribe time will receive all the metadata for the resources related to the 
  subscriber(node for which the subscriber wants to receive the metadata);
* a message of type `Create` is sent to the subscribers when a new resource is discovered;
  for it;
* a message of type `Update` is sent to the subscriber when an already sent resource has some fields modified;
* a message of type `Delete` is sent to the subscriber when an already sent resource is not anymore relevant for the 
  subscriber;
* only metadata for resources related to a subscriber are sent;

## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for
testing, or run against a remote cluster.

### Running on the cluster

#### Manifests
It's as easy as running:

```sh
kubectl apply -f manifests/meta-collector.yaml
```

If you want to scrape the metrics exposed by `k8s-metacollector` using prometheus then deploy the provided
`ServiceMonitor`. Make sure to add the appropriate label to the manifest file in order to be discovered and scraped by
your prometheus instance.
```shell
kubectl apply -f manifests/monitor.yaml
```
#### Helm Chart

Before installing the [chart](https://github.com/falcosecurity/charts/tree/master/charts/k8s-metacollector), add the `falcosecurity` charts repository:

```bash
helm repo add falcosecurity https://falcosecurity.github.io/charts
helm repo update
```

To install the chart with default values and release name `k8s-metacollector` run:

```bash
helm install k8s-metacollector falcosecurity/k8s-metacollector \
    --namespace metacollector \
    --create-namespace
```

After a few seconds, k8s-metacollector should be running in the `metacollector` namespace.

##### Enabling ServiceMonitor
Assuming that Prometheus scrapes only the ServiceMonitors that present a `release label` the following command will install and label the ServiceMonitor:

```bash
helm install k8s-metacollector falcosecurity/k8s-metacollector \
    --namespace metacollector \
    --create-namespace \
    --set serviceMonitor.create=true \
    --set serviceMonitor.labels.release="kube-prometheus-stack"
```

There is also a default `grafana dashboard` ready to be used under `grafana` folder.

## License

This project is licensed to you under the [Apache 2.0](https://github.com/falcosecurity/k8s-metacollector/blob/main/LICENSE) license.

