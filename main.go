// Copyright 2023 The Falco Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"flag"
	"os"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/alacuku/k8s-metadata/broker"
	"github.com/alacuku/k8s-metadata/collectors"
	"github.com/alacuku/k8s-metadata/pkg/events"
	"github.com/alacuku/k8s-metadata/pkg/resource"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var probeAddr string
	var brokerAddr string
	var certFilePath string
	var keyFilePath string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&brokerAddr, "broker-bind-address", ":45000", "The address the broker endpoint binds to.")
	flag.StringVar(&certFilePath, "broker-server-cert", "", "Cert file path for grpc server.")
	flag.StringVar(&keyFilePath, "broker-server-key", "", "Key file path for grpc server.")

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		NewCache: cache.BuilderWithOptions(cache.Options{
			UnsafeDisableDeepCopyByObject: map[client.Object]bool{
				&corev1.Pod{}:                   true,
				&corev1.Namespace{}:             true,
				&corev1.ReplicationController{}: true,
				&v1.Deployment{}:                true,
				&v1.ReplicaSet{}:                true,
				&v1.DaemonSet{}:                 true,
			},
			TransformByObject: cache.TransformByObject{
				&corev1.Pod{}:                collectors.PodTransformer(setupLog),
				&corev1.Service{}:            collectors.ServiceTransformer(setupLog),
				&corev1.Namespace{}:          collectors.PartialObjectTransformer(setupLog),
				&v1.Deployment{}:             collectors.PartialObjectTransformer(setupLog),
				&v1.ReplicaSet{}:             collectors.PartialObjectTransformer(setupLog),
				&v1.DaemonSet{}:              collectors.PartialObjectTransformer(setupLog),
				&discoveryv1.EndpointSlice{}: collectors.EndpointsliceTransformer(setupLog),
			},
		}),
	})

	if err != nil {
		setupLog.Error(err, "creating manager")
		os.Exit(1)
	}
	if err = collectors.IndexPodByPrefixName(context.Background(), mgr.GetFieldIndexer()); err != nil {
		setupLog.Error(err, "unable to add indexer by prefix name for pods")
		os.Exit(1)
	}

	if err = collectors.IndexPodByNode(context.Background(), mgr.GetFieldIndexer()); err != nil {
		setupLog.Error(err, "unable to add indexer by node name for pods")
		os.Exit(1)
	}

	// Create source for deployments.
	dpl := make(chan event.GenericEvent, 1)
	deploymentSource := &source.Channel{Source: dpl}

	// Create source for replicasets.
	rs := make(chan event.GenericEvent, 1)
	replicasetSource := &source.Channel{Source: rs}

	// Create source for namespaces.
	ns := make(chan event.GenericEvent, 1)
	namespaceSource := &source.Channel{Source: ns}

	// Create source for daemonsets.
	ds := make(chan event.GenericEvent, 1)
	daemonsetSource := &source.Channel{Source: ds}

	// Create source for replicationcontrollers.
	rc := make(chan event.GenericEvent, 1)
	rcSource := &source.Channel{Source: rc}

	externalSrc := make(map[string]chan<- event.GenericEvent)
	externalSrc[resource.Deployment] = dpl
	externalSrc[resource.ReplicaSet] = rs
	externalSrc[resource.Namespace] = ns
	externalSrc[resource.Daemonset] = rc

	// Create source for pods.
	pd := make(chan event.GenericEvent, 1)
	podSource := &source.Channel{Source: pd}

	// Create source for services.
	svc := make(chan event.GenericEvent, 1)
	serviceSource := &source.Channel{Source: svc}

	podChanTrig := make(chan string)

	queue := broker.NewBlockingChannel(1)
	podCollector := &collectors.PodCollector{
		Client:          mgr.GetClient(),
		Cache:           events.NewGenericCache(),
		Name:            "pod-collector",
		Queue:           queue,
		ExternalSources: externalSrc,
		EndpointsSource: podSource,
		SubscriberChan:  podChanTrig,
	}

	if err = podCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Pod)
		os.Exit(1)
	}

	dplChanTrig := make(chan string)
	dplCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewGenericCache(),
		collectors.NewPartialObjectMetadata(resource.Deployment, nil), "deployment-collector",
		collectors.WithSubscribersChan(dplChanTrig),
		collectors.WithExternalSource(deploymentSource),
		collectors.WithPodMatchingFields(func(meta *metav1.ObjectMeta) client.ListOption {
			return &client.MatchingFields{
				"metadata.generateName": meta.Name,
			}
		}))

	if err = dplCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Deployment)
		os.Exit(1)
	}

	rsChanTrig := make(chan string)
	rsCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewGenericCache(),
		collectors.NewPartialObjectMetadata(resource.ReplicaSet, nil), "replicaset-collector",
		collectors.WithSubscribersChan(rsChanTrig),
		collectors.WithExternalSource(replicasetSource),
		collectors.WithPodMatchingFields(func(meta *metav1.ObjectMeta) client.ListOption {
			return &client.MatchingFields{
				"metadata.generateName": meta.Name + "-",
			}
		}))

	if err = rsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.ReplicaSet)
		os.Exit(1)
	}

	nsChanTrig := make(chan string)
	nsCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewGenericCache(),
		collectors.NewPartialObjectMetadata(resource.Namespace, nil), "namespace-collector",
		collectors.WithSubscribersChan(nsChanTrig),
		collectors.WithExternalSource(namespaceSource))

	if err = nsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Namespace)
		os.Exit(1)
	}

	dsChanTrig := make(chan string)
	dsCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewGenericCache(),
		collectors.NewPartialObjectMetadata(resource.Daemonset, nil), "daemonset-collector",
		collectors.WithSubscribersChan(dsChanTrig),
		collectors.WithExternalSource(daemonsetSource),
		collectors.WithPodMatchingFields(func(meta *metav1.ObjectMeta) client.ListOption {
			return &client.MatchingFields{
				"metadata.generateName": meta.Name + "-",
			}
		}))

	if err = dsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Daemonset)
		os.Exit(1)
	}

	rcChanTrig := make(chan string)
	rcCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewGenericCache(),
		collectors.NewPartialObjectMetadata(resource.ReplicationController, nil), "replicationcontroller-collector",
		collectors.WithSubscribersChan(rcChanTrig),
		collectors.WithExternalSource(rcSource),
		collectors.WithPodMatchingFields(func(meta *metav1.ObjectMeta) client.ListOption {
			return &client.MatchingFields{
				"metadata.generateName": meta.Name + "-",
			}
		}))

	if err = rcCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.ReplicationController)
		os.Exit(1)
	}

	svcChanTrig := make(chan string)
	svcCollector := &collectors.ServiceCollector{
		Client:          mgr.GetClient(),
		Cache:           events.NewGenericCache(),
		Name:            "service-collector",
		Queue:           queue,
		EndpointsSource: serviceSource,
		SubscriberChan:  svcChanTrig,
	}

	if err = svcCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Service)
		os.Exit(1)
	}

	if err = (&collectors.EndpointsDispatcher{
		Client:                 mgr.GetClient(),
		Name:                   "endpoint-dispatcher",
		ServiceCollectorSource: svc,
		PodCollectorSource:     pd,
		Pods:                   make(map[string]map[string]struct{}),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create dispatcher for", "resource kind", resource.EndpointSlice)
		os.Exit(1)
	}

	if err = (&collectors.EndpointslicesDispatcher{
		Client:                 mgr.GetClient(),
		Name:                   "endpointslices-dispatcher",
		ServiceCollectorSource: svc,
		PodCollectorSource:     pd,
		Pods:                   make(map[string]map[string]struct{}),
		ServicesName:           make(map[string]string),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create dispatcher for", "resource kind", resource.EndpointSlice)
		os.Exit(1)
	}

	br, err := broker.New(ctrl.Log.WithName("broker"), queue, map[string]chan<- string{
		resource.Pod:                   podChanTrig,
		resource.Deployment:            dplChanTrig,
		resource.ReplicaSet:            rsChanTrig,
		resource.Daemonset:             dsChanTrig,
		resource.Service:               svcChanTrig,
		resource.Namespace:             nsChanTrig,
		resource.ReplicationController: rcChanTrig,
	},
		broker.WithAddress(brokerAddr),
		broker.WithTLS(certFilePath, keyFilePath))

	if err != nil {
		setupLog.Error(err, "unable to create the broker")
		os.Exit(1)
	}

	if err = mgr.Add(br); err != nil {
		setupLog.Error(err, "unable to add broker to the manager")
		os.Exit(1)
	}

	if err = mgr.Add(podCollector); err != nil {
		setupLog.Error(err, "unable to add pod collector to the manager as a runnable")
		os.Exit(1)
	}

	if err = mgr.Add(dsCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", dsCollector.GetName())
		os.Exit(1)
	}

	if err = mgr.Add(nsCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", nsCollector.GetName())
		os.Exit(1)
	}

	if err = mgr.Add(dplCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", dplCollector.GetName())
		os.Exit(1)
	}

	if err = mgr.Add(rsCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", rsCollector.GetName())
		os.Exit(1)
	}

	if err = mgr.Add(rcCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", rcCollector.GetName())
		os.Exit(1)
	}

	if err = mgr.Add(svcCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", svcCollector.Name)
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
