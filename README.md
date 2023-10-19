# k8s-meta-collector

The "K8s Meta Collector" is a self-contained module that can be deployed within a Kubernetes cluster to perform the task
of gathering metadata from various Kubernetes resources and subsequently transmitting this collected metadata to
designated subscribers.

## Description

[Falco](https://github.com/falcosecurity/falco) enriches events coming from [syscall event source](https://falco.org/docs/event-sources/) with `metadata` 
coming from other sources, for example Kubernetes API server. Historically, each instance of Falco running in a 
Kubernetes cluster would connect to the Kubernetes API server in order to fetch the metadata for a [subset of 
Kubernetes resources](https://falco.org/docs/reference/rules/supported-fields/#field-class-k8s). This approach works 
well in small Kubernetes cluster but does not scale in large environments. The following issue describes the 
problems that were affecting the old Kubernetes client: https://github.com/falcosecurity/libs/issues/987.

The aim of `k8s-meta-collector` is to propose a novel approach to `k8s metadata enrichment` in Falco by moving 
the fetching logic of the metadata to a centralized component. The Falco instances would connect to this component 
and receive the metadata without the need to connect to the Kubernetes API server.
The following image shows the  deployment of `k8s-meta-collector` and Falco in a kubernetes cluster.

![image](docs/images/meta-collector-in-cluster.svg "Deployment inside a Kubernetes cluster")

Having a centralized component that connects to the API server and pushes metadata to the Falco instances reduces the 
load on the Kubernetes API server. Keep in mind that Falco is deployed as a DaemonSet, one Falco instance on each node.
It also reduces the number of events sent to the Falco instances by filtering the metadata by the node. A given 
Falco instance running in a given node will receive metadata only for the resources that are related to that node:
* pods running on the node;
* namespaces that contain a pod running on the node;
* deployment, replicaset, replicationcontrollers associated with a pod running on the node;
* services serving a pod running on the node.

The filtering done by `k8s-meta-collector` reduces significantly the number of events sent to the Falco instances. 
The metadata received by the subscribers is ready to be used without the need for further processing on the 
subscribers side.



### Functional Guarantees:
The `k8s-meta-collector` assures that:
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

It's as easy as running:

```sh
kubectl apply -f manifests/meta-collector.yaml
```

If you want to scrape the metrics exposed by `k8s-meta-collector` using prometheus then deploy the provided
`ServiceMonitor`. Make sure to add the appropriate label to the manifest file in order to be discovered and scraped by
your prometheus instance.
```shell
kubectl apply -f manifests/monitor.yaml
```
There is also a default `grafana dashboard` ready to be used under `grafana` folder.

## Contributing

// TODO(user): Add detailed information on how you would like others to contribute to this project

### How it works

This project aims to follow the
Kubernetes [Operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/).

It uses [Controllers](https://kubernetes.io/docs/concepts/architecture/controller/),
which provide a reconcile function responsible for synchronizing resources until the desired state is reached on the
cluster.

### Test It Out

1. Install the CRDs into the cluster:

```sh
make install
```

2. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

