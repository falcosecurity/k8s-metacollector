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

	corev1 "k8s.io/api/core/v1"
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

	"github.com/alacuku/k8s-metadata/collectors"
	"github.com/alacuku/k8s-metadata/internal/events"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
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
				&corev1.Pod{}:       true,
				&corev1.Namespace{}: true,
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
	cm := collectors.NewChannelMetrics()
	go func() {
		for msg := range eventsChan {
			cm.Receive(msg)
			fmt.Println(msg.String())
		}
	}()

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

	externalSrc := make(map[string]chan<- event.GenericEvent)
	externalSrc["Deployment"] = dpl
	externalSrc["ReplicaSet"] = rs
	externalSrc["Namespace"] = ns
	externalSrc[collectors.Daemonset] = ds

	if err = (&collectors.PodCollector{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		Cache:           events.NewPodCache(),
		Name:            "pod-collector",
		Sink:            eventsChan,
		ChannelMetrics:  cm,
		ExternalSources: externalSrc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PodResource")
		os.Exit(1)
	}

	if err = (&collectors.DeploymentCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "deployment-collector",
		Sink:           eventsChan,
		ChannelMetrics: cm,
		GenericSource:  deploymentSource,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Deployment")
		os.Exit(1)
	}

	if err = (&collectors.ReplicasetCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "replicaset-collector",
		Sink:           eventsChan,
		ChannelMetrics: cm,
		GenericSource:  replicasetSource,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "ReplicaSet")
		os.Exit(1)
	}

	if err = (&collectors.NamespaceCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "namespace-collector",
		Sink:           eventsChan,
		ChannelMetrics: cm,
		GenericSource:  namespaceSource,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Namespace")
		os.Exit(1)
	}

	if err = (&collectors.DaemonsetCollector{
		Client:         mgr.GetClient(),
		Cache:          events.NewGenericCache(),
		Name:           "daemonset-collector",
		Sink:           eventsChan,
		ChannelMetrics: cm,
		GenericSource:  daemonsetSource,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Daemonset")
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
