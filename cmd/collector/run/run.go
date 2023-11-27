// SPDX-License-Identifier: Apache-2.0
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

package run

import (
	"context"
	"flag"
	"os"

	"github.com/falcosecurity/k8s-metacollector/broker"
	"github.com/falcosecurity/k8s-metacollector/collectors"
	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

type flags struct {
	metricsAddr  string
	probeAddr    string
	brokerAddr   string
	certFilePath string
	keyFilePath  string
}

func (fl *flags) add(flags *pflag.FlagSet) {
	flags.StringVar(&fl.metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to")
	flags.StringVar(&fl.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to")
	flags.StringVar(&fl.brokerAddr, "broker-bind-address", ":45000", "The address the broker endpoint binds to")
	flags.StringVar(&fl.certFilePath, "broker-server-cert", "", "Cert file path for grpc server")
	flags.StringVar(&fl.keyFilePath, "broker-server-key", "", "Key file path for grpc server")
}

type options struct {
	flags
	logger     *logr.Logger
	loggerOpts *zap.Options
}

// New returns a new run command.
func New(ctx context.Context, logger *logr.Logger) *cobra.Command {
	opts := options{
		flags: flags{},
	}

	cmd := &cobra.Command{
		Use:   "run [flags]",
		Short: "Runs the metacollector",
		Long:  "Runs the metacollector",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			opts.Run(ctx)
		},
	}

	// If the logger is not set, then create logger options and bind the flags.
	if logger == nil {
		logOpts := zap.Options{
			Development: false,
		}
		logOpts.BindFlags(flag.CommandLine)
		opts.loggerOpts = &logOpts
	} else {
		opts.logger = logger
	}

	// Add the go flags to the cobra flagset. It registers the flags for the kubeconfig path
	// and the logger.
	cmd.Flags().AddGoFlagSet(flag.CommandLine)
	opts.flags.add(cmd.Flags())

	return cmd
}

// Run starts the metacollector.
func (opts *options) Run(ctx context.Context) {
	// Set the logger.
	if opts.logger != nil {
		ctrl.SetLogger(*opts.logger)
	} else {
		ctrl.SetLogger(zap.New(zap.UseFlagOptions(opts.loggerOpts)))
	}

	setupLog := ctrl.Log.WithName("setup")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: opts.metricsAddr,
		},
		HealthProbeBindAddress: opts.probeAddr,
		Cache: cache.Options{
			DefaultUnsafeDisableDeepCopy: ptr.To(true),
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Pod{}: {
					Transform: collectors.PodTransformer(setupLog),
				},
				&corev1.Service{}: {
					Transform: collectors.ServiceTransformer(setupLog),
				},
				&corev1.Namespace{}: {
					Transform: collectors.PartialObjectTransformer(setupLog),
				},
				&corev1.ReplicationController{}: {
					Transform: collectors.PartialObjectTransformer(setupLog),
				},
				&v1.Deployment{}: {
					Transform: collectors.PartialObjectTransformer(setupLog),
				},
				&v1.ReplicaSet{}: {
					Transform: collectors.PartialObjectTransformer(setupLog),
				},
				&v1.DaemonSet{}: {
					Transform: collectors.PartialObjectTransformer(setupLog),
				},
				&discoveryv1.EndpointSlice{}: {
					Transform: collectors.EndpointsliceTransformer(setupLog),
				},
			},
		},
	})
	if err != nil {
		setupLog.Error(err, "creating manager")
		os.Exit(1)
	}
	if err = collectors.IndexPodByPrefixName(ctx, mgr.GetFieldIndexer()); err != nil {
		setupLog.Error(err, "unable to add indexer by prefix name for pods")
		os.Exit(1)
	}

	if err = collectors.IndexPodByNode(ctx, mgr.GetFieldIndexer()); err != nil {
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

	podChanTrig := make(subscriber.SubsChan)

	queue := broker.NewBlockingChannel(1)

	podCollector := collectors.NewPodCollector(mgr.GetClient(), queue, events.NewCache(), "pod-collector",
		collectors.WithOwnerSources(externalSrc),
		collectors.WithSubscribersChan(podChanTrig),
		collectors.WithExternalSource(podSource))

	if err = podCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Pod)
		os.Exit(1)
	}

	dplChanTrig := make(subscriber.SubsChan)
	dplCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewCache(),
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

	rsChanTrig := make(subscriber.SubsChan)
	rsCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewCache(),
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

	nsChanTrig := make(subscriber.SubsChan)
	nsCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewCache(),
		collectors.NewPartialObjectMetadata(resource.Namespace, nil), "namespace-collector",
		collectors.WithSubscribersChan(nsChanTrig),
		collectors.WithExternalSource(namespaceSource))

	if err = nsCollector.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create collector for", "resource kind", resource.Namespace)
		os.Exit(1)
	}

	dsChanTrig := make(subscriber.SubsChan)
	dsCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewCache(),
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

	rcChanTrig := make(subscriber.SubsChan)
	rcCollector := collectors.NewObjectMetaCollector(mgr.GetClient(), queue, events.NewCache(),
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

	svcChanTrig := make(subscriber.SubsChan)

	svcCollector := collectors.NewServiceCollector(mgr.GetClient(), queue, events.NewCache(), "service-collector",
		collectors.WithExternalSource(serviceSource),
		collectors.WithSubscribersChan(svcChanTrig))

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

	br, err := broker.New(ctrl.Log.WithName("broker"), queue, map[string]subscriber.SubsChan{
		resource.Pod:                   podChanTrig,
		resource.Deployment:            dplChanTrig,
		resource.ReplicaSet:            rsChanTrig,
		resource.Daemonset:             dsChanTrig,
		resource.Service:               svcChanTrig,
		resource.Namespace:             nsChanTrig,
		resource.ReplicationController: rcChanTrig,
	},
		broker.WithAddress(opts.brokerAddr),
		broker.WithTLS(opts.certFilePath, opts.keyFilePath))

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
		setupLog.Error(err, "unable to add %s collector to the manager as a runnable", svcCollector.GetName())
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
