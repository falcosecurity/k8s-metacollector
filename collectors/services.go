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

package collectors

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/alacuku/k8s-metadata/broker"
	"github.com/alacuku/k8s-metadata/pkg/events"
	"github.com/alacuku/k8s-metadata/pkg/fields"
	"github.com/alacuku/k8s-metadata/pkg/resource"
)

// ServiceCollector collects services' metadata, puts them in a local cache and generates appropriate
// events when such resources change over time.
type ServiceCollector struct {
	client.Client
	queue           broker.Queue
	cache           *events.Cache
	endpointsSource source.Source
	name            string
	subscriberChan  <-chan string
	logger          logr.Logger
}

// NewServiceCollector returns a new service collector.
func NewServiceCollector(cl client.Client, queue broker.Queue, cache *events.Cache, name string, opt ...CollectorOption) *ServiceCollector {
	opts := collectorOptions{}
	for _, o := range opt {
		o(&opts)
	}

	return &ServiceCollector{
		Client:          cl,
		queue:           queue,
		cache:           cache,
		endpointsSource: opts.externalSource,
		name:            name,
		subscriberChan:  opts.subscriberChan,
	}
}

//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch

// Reconcile generates events to be sent to nodes when changes are detected for the watched resources.
func (r *ServiceCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var svc = &corev1.Service{}
	var sRes *events.Resource
	var ok, serviceDeleted bool

	logger := log.FromContext(ctx)

	err = r.Get(ctx, req.NamespacedName, svc)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logger.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	if k8sApiErrors.IsNotFound(err) {
		// When the k8s resource get deleted we need to remove it from the local cache.
		if _, ok = r.cache.Get(req.String()); ok {
			logger.Info("marking resource for deletion")
			serviceDeleted = true
		} else {
			return ctrl.Result{}, nil
		}
	}

	logger.V(5).Info("resource found")

	// Get all the nodes to which this resource is related.
	// The currentNodes are used to compute to which nodes we need to send an event
	// and of which type, Create, Delete or Update.
	currentNodes, err := r.Nodes(ctx, logger, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if the resource has already been cached.
	if sRes, ok = r.cache.Get(req.String()); !ok {
		// If first time, then we just create a new cache entry for it.
		logger.V(3).Info("never met this resource in my life")
		sRes = events.NewResource(resource.Service, string(svc.UID))
	}

	// The resource has been created, or updated. Compute if we need to propagate events.
	// The outcome is saved internally to the resource. See AddNodes method for more info.
	if !serviceDeleted {
		if err := r.ObjFieldsHandler(logger, sRes, svc); err != nil {
			return ctrl.Result{}, err
		}
		sRes.AddNodes(currentNodes.ToSlice())
	} else {
		// If the resource has been deleted from the api-server, then we send a "Delete" event to all nodes
		nodes := sRes.GetNodes()
		sRes.DeleteNodes(nodes.ToSlice())
	}

	// At this point our resource has all the necessary bits to know for each node which type of events need to be sent.
	evts := sRes.ToEvents()

	// Enqueue events.
	for _, evt := range evts {
		if evt == nil {
			continue
		}
		switch evt.Type() {
		case events.Create:
			// Perform actions for "Create" events.
			// For each resource that generates an "Create" event, we need to add it to the cache.
			r.cache.Add(req.String(), sRes)
		case events.Update:
			// Run specific code for "Update" events.
			r.cache.Update(req.String(), sRes)
		case events.Delete:
			// Run specific code for "Delete" events.
			r.cache.Delete(req.String())
		}
		// Add event to the queue.
		r.queue.Push(evt)
	}

	return ctrl.Result{}, nil
}

// Start implements the runnable interface needed in order to handle the start/stop
// using the manager. It starts go routines needed by the collector to interact with the
// broker.
func (r *ServiceCollector) Start(ctx context.Context) error {
	return dispatch(ctx, r.logger, r.subscriberChan, r.queue, r.cache)
}

// ObjFieldsHandler populates the evt from the object.
func (r *ServiceCollector) ObjFieldsHandler(logger logr.Logger, evt *events.Resource, svc *corev1.Service) error {
	if svc == nil {
		return nil
	}

	svcUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(svc)
	if err != nil {
		logger.Error(err, "unable to convert to unstructured")
		return err
	}

	// Remove unused meta fields
	metaUnused := []string{"creationTimestamp", "ownerReferences"}

	meta := svcUn["metadata"]
	metaMap := meta.(map[string]interface{})
	for _, key := range metaUnused {
		delete(metaMap, key)
	}

	metaString, err := json.Marshal(metaMap)
	if err != nil {
		return err
	}
	evt.SetMeta(string(metaString))

	return nil
}

// Nodes returns all the nodes where pods related to the current deployment are running.
func (r *ServiceCollector) Nodes(ctx context.Context, logger logr.Logger, svc *corev1.Service) (fields.Nodes, error) {
	pods := corev1.PodList{}
	if err := r.List(ctx, &pods, client.InNamespace(svc.Namespace), client.MatchingLabels(svc.Spec.Selector)); err != nil {
		logger.Error(err, "unable to list pods related to resource", "in namespace", svc.Namespace)
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, nil
	}

	nodes := make(map[string]struct{}, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName != "" && pods.Items[i].Status.PodIP != "" {
			nodes[pods.Items[i].Spec.NodeName] = struct{}{}
		}
	}

	return nodes, nil
}

// GetName returns the name of the collector.
func (r *ServiceCollector) GetName() string {
	return r.name
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceCollector) SetupWithManager(mgr ctrl.Manager) error {
	// Set the generic logger to be used in other function then the reconcile loop.
	r.logger = mgr.GetLogger().WithName(r.name)

	lc, err := newLogConstructor(mgr.GetLogger(), r.name, resource.Service)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{},
			builder.WithPredicates(predicatesWithMetrics(r.name, apiServerSource, nil))).
		WithOptions(controller.Options{LogConstructor: lc}).
		WatchesRawSource(r.endpointsSource,
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicatesWithMetrics(r.name, resource.Endpoints, nil))).
		Owns(&discoveryv1.EndpointSlice{},
			builder.WithPredicates(predicatesWithMetrics(r.name, resource.EndpointSlice, nil))).
		Complete(r)
}
