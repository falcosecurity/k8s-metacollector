/*
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
*/

package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
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
	"github.com/alacuku/k8s-metadata/internal/events"
	"github.com/alacuku/k8s-metadata/internal/resource"
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
	var nodeName string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&nodeName, "node-name", "",
		"The node name where this controller instance runs. It is used to filter out pods not scheduled on the current node.")

	opts := zap.Options{
		Development: false,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	setupLog := ctrl.Log.WithName("setup")
	// Check node name has been set.
	if nodeName == "" {
		setupLog.Error(fmt.Errorf("can not be empty"), "please set a value for", "flag", "--node-name")
		os.Exit(1)
	}

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

	eventsChan := make(chan events.Event, 1)

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
		Scheme:          mgr.GetScheme(),
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
	dplCollector := &collectors.DeploymentCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "deployment-collector",
		Queue:          queue,
		GenericSource:  deploymentSource,
		SubscriberChan: dplChanTrig,
	}

	if err = dplCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Deployment)
		os.Exit(1)
	}

	rsChanTrig := make(chan string)
	rsCollector := &collectors.ReplicasetCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "replicaset-collector",
		Queue:          queue,
		GenericSource:  replicasetSource,
		SubscriberChan: rsChanTrig,
	}

	if err = rsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.ReplicaSet)
		os.Exit(1)
	}

	nsChanTrig := make(chan string)
	nsCollector := &collectors.NamespaceCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "namespace-collector",
		Queue:          queue,
		GenericSource:  namespaceSource,
		SubscriberChan: nsChanTrig,
	}

	if err = nsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Namespace)
		os.Exit(1)
	}

	dsChanTrig := make(chan string)
	dsCollector := &collectors.DaemonsetCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "daemonset-collector",
		Queue:          queue,
		GenericSource:  daemonsetSource,
		SubscriberChan: dsChanTrig,
	}

	if err = dsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Daemonset)
		os.Exit(1)
	}

	rcChanTrig := make(chan string)
	rcCollector := &collectors.ReplicationcontrollerCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "replicationcontroller-collector",
		Queue:          queue,
		GenericSource:  rcSource,
		SubscriberChan: rcChanTrig,
	}

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
		Sink:                   eventsChan,
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
		Sink:                   eventsChan,
		ServiceCollectorSource: svc,
		PodCollectorSource:     pd,
		Pods:                   make(map[string]map[string]struct{}),
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
	})

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
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", dsCollector.Name)
		os.Exit(1)
	}

	if err = mgr.Add(nsCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", nsCollector.Name)
		os.Exit(1)
	}

	if err = mgr.Add(dplCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", dplCollector.Name)
		os.Exit(1)
	}

	if err = mgr.Add(rsCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", rsCollector.Name)
		os.Exit(1)
	}

	if err = mgr.Add(rcCollector); err != nil {
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", rcCollector.Name)
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
