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
	Queue           broker.Queue
	Cache           *events.Cache
	EndpointsSource source.Source
	Name            string
	SubscriberChan  <-chan string
	logger          logr.Logger
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
		if _, ok = r.Cache.Get(req.String()); ok {
			logger.Info("marking resource for deletion")
			serviceDeleted = true
		} else {
			return ctrl.Result{}, nil
		}
	}

	logger.V(5).Info("resource found")

	// Get all the nodes to which this resource is related.
	// The currentNodes are used to compute to which nodes we need to send an event
	// and of which type, Added, Deleted or Modified.
	currentNodes, err := r.Nodes(ctx, logger, svc)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if the resource has already been cached.
	if sRes, ok = r.Cache.Get(req.String()); !ok {
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
		// We need to know if the mutable fields has been changed. That's why AddNodes accepts
		// a bool. Otherwise, we can not tell if nodes need an "Added" event or a "Modified" one.
		sRes.AddNodes(currentNodes.ToSlice())
	} else {
		// If the resource has been deleted from the api-server, then we send a "Deleted" event to all nodes
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
		case events.Added:
			// Perform actions for "Added" events.
			generatedEvents.WithLabelValues(r.Name, labelCreate).Inc()
			// For each resource that generates an "Added" event, we need to add it to the cache.
			// Please keep in mind that Cache operations resets the state of the resource, such as
			// resetting the info needed to generate the events.
			r.Cache.Add(req.String(), sRes)
		case events.Modified:
			// Run specific code for "Modified" events.
			generatedEvents.WithLabelValues(r.Name, labelUpdate).Inc()
			r.Cache.Update(req.String(), sRes)
		case events.Deleted:
			// Run specific code for "Deleted" events.
			generatedEvents.WithLabelValues(r.Name, labelDelete).Inc()
			r.Cache.Delete(req.String())
		}
		// Add event to the queue.
		r.Queue.Push(evt)
	}

	return ctrl.Result{}, nil
}

// Start implements the runnable interface needed in order to handle the start/stop
// using the manager. It starts go routines needed by the collector to interact with the
// broker.
func (r *ServiceCollector) Start(ctx context.Context) error {
	return dispatch(ctx, r.logger, r.SubscriberChan, r.Queue, r.Cache)
}

func (r *ServiceCollector) initMetrics() {
	generatedEvents.WithLabelValues(r.Name, labelCreate).Add(0)
	generatedEvents.WithLabelValues(r.Name, labelUpdate).Add(0)
	generatedEvents.WithLabelValues(r.Name, labelDelete).Add(0)
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

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceCollector) SetupWithManager(mgr ctrl.Manager) error {
	r.initMetrics()

	// Set the generic logger to be used in other function then the reconcile loop.
	r.logger = mgr.GetLogger().WithName(r.Name)

	lc, err := newLogConstructor(mgr.GetLogger(), r.Name, resource.Service)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{},
			builder.WithPredicates(predicatesWithMetrics(r.Name, apiServerSource, nil))).
		WithOptions(controller.Options{LogConstructor: lc}).
		Watches(r.EndpointsSource,
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicatesWithMetrics(r.Name, resource.Endpoints, nil))).
		Owns(&discoveryv1.EndpointSlice{},
			builder.WithPredicates(predicatesWithMetrics(r.Name, resource.EndpointSlice, nil))).
		Complete(r)
}
