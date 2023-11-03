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

package collectors

import (
	"context"
	"encoding/json"

	"github.com/go-logr/logr"
	"github.com/mitchellh/hashstructure/v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/alacuku/k8s-metadata/broker"
	"github.com/alacuku/k8s-metadata/pkg/events"
	"github.com/alacuku/k8s-metadata/pkg/fields"
	"github.com/alacuku/k8s-metadata/pkg/resource"
	"github.com/alacuku/k8s-metadata/pkg/subscriber"
)

// ObjectMetaCollector collects resources' metadata, puts them in a local cache and generates appropriate
// events when such resources change over time.
type ObjectMetaCollector struct {
	client.Client
	queue broker.Queue
	cache *events.Cache
	// externalSource watched for events that trigger the reconcile. In some cases changes in
	// other resources triggers the current resource. For example, when a pod is created we need to trigger the namespace
	// where the pod lives in order to send also the namespace to the node where the pod is running.
	externalSource source.Source
	// name of the collector, used in the logger.
	name string
	// subscriberChan where the collector gets notified of new subscribers and dispatches the existing events through the queue.
	subscriberChan subscriber.SubsChan
	logger         logr.Logger
	// The GVK for the resource need to be set.
	resource *metav1.PartialObjectMetadata
	// podMatchingFields returns a list options used to list existing pods previously indexed on a field.
	podMatchingFields func(metadata *metav1.ObjectMeta) client.ListOption
	// dispatcherSource is used to get events enqueued by the dispatcher based
	// on subscribers' arrival.
	dispatcherSource source.Source
	// dispatcherChan is the channel where the dispatcher pushes the new requests to be enqueued and
	// processed by the reconciler.
	dispatcherChan chan event.GenericEvent
	subscribers    *subscriber.Subscribers
}

// NewObjectMetaCollector returns a new meta collector for a given resource kind.
func NewObjectMetaCollector(cl client.Client, queue broker.Queue, cache *events.Cache,
	res *metav1.PartialObjectMetadata, name string, opt ...CollectorOption) *ObjectMetaCollector {
	opts := collectorOptions{
		podMatchingFields: func(meta *metav1.ObjectMeta) client.ListOption {
			return &client.ListOptions{}
		},
	}
	for _, o := range opt {
		o(&opts)
	}

	dc := make(chan event.GenericEvent, 1)

	return &ObjectMetaCollector{
		Client:            cl,
		queue:             queue,
		cache:             cache,
		externalSource:    opts.externalSource,
		name:              name,
		subscriberChan:    opts.subscriberChan,
		resource:          res,
		podMatchingFields: opts.podMatchingFields,
		dispatcherSource:  &source.Channel{Source: dc},
		dispatcherChan:    dc,
		subscribers:       subscriber.NewSubscribers(),
	}
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

// Reconcile generates events to be sent to nodes when changes are detected for the watched resources.
func (r *ObjectMetaCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var res *events.Resource
	var cEntry *events.CacheEntry
	var ok, deleted bool

	logger := log.FromContext(ctx)

	err = r.Get(ctx, req.NamespacedName, r.resource)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logger.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	if k8sApiErrors.IsNotFound(err) {
		// When the k8s resource gets deleted we need to remove it from the local cache.
		if r.cache.Has(req.String()) {
			logger.Info("marking resource for deletion")
			deleted = true
		} else {
			return ctrl.Result{}, nil
		}
	}

	logger.V(5).Info("resource found")

	// Create the resource and populate all its fields.
	if !deleted {
		// Get all getSubscribers for the resource based on its node name.
		// The getSubscribers are used to compute to which getSubscribers we need to send an event
		// and of which type, Create, Delete or Update
		subs, err := r.getSubscribers(ctx, logger, &r.resource.ObjectMeta)
		if err != nil {
			return ctrl.Result{}, err
		}
		// If no subscribers and not in the cache, return.
		if len(subs) == 0 && !r.cache.Has(req.String()) {
			return ctrl.Result{}, nil
		}

		// Create a new events.Resource and fill its fields.
		res = events.NewResource(r.resource.Kind, string(r.resource.UID))
		// Populate resource fields.
		if err := r.objFieldsHandler(logger, res, r.resource); err != nil {
			return ctrl.Result{}, err
		}
		// Hash the current resource.
		hash, err := hashstructure.Hash(res, hashstructure.FormatV2, nil)
		if err != nil {
			logger.Error(err, "unable to hash resource")
			return ctrl.Result{}, err
		}

		// Check if we have cached the resource previously.
		if cEntry, ok = r.cache.Get(req.String()); ok {
			// If an entry exists for the resource then check if the hashes are the same.
			// If not, it means that the resource fields have changes since the last time.
			// so mark the resource as updated. The "update" flag is needed to generate "Update"
			// events for the related getSubscribers.
			if cEntry.Hash != hash {
				res.SetUpdate(true)
				cEntry.Hash = hash
			}
			// Set the previous subscribers in the current resource.
			res.SetSubscribers(cEntry.Subs)
		} else {
			// If we never cached the resource then create an entry and add it to the cache.
			cEntry = &events.CacheEntry{
				Hash: hash,
				UID:  r.resource.UID,
				Subs: nil,
			}
			r.cache.Add(req.String(), cEntry)
		}

		// Add the new subscribers and internally compute the new subscribers to which we need to sent events.
		// Update the cache entry with the new set of getSubscribers.
		cEntry.Subs = res.GenerateSubscribers(subs)
	} else {
		// Check if we have cached the resource.
		if cEntry, ok = r.cache.Get(req.String()); ok {
			// Create the resource.
			res = events.NewResource(r.resource.Kind, string(cEntry.UID))
			// Set the previous subscribers.
			res.SetSubscribers(cEntry.Subs)
			// The resource has been deleted. We need to send a delete event to
			// the subscribers. By generating the subscribers from an empty set,
			// is the same as to generate delete events for all the subscribers to which
			// we sent an event.
			res.GenerateSubscribers(nil)
			// We are ready to remove the entry from the cache. No need to track anymore
			// the deleted resource.
			r.cache.Delete(req.String())
		} else {
			// It means that we received a delete event for a resource that we never sent to any subscriber.
			// In this case we just return.
			return ctrl.Result{}, nil
		}
	}

	// At this point our resource has all the necessary bits to know for each node which type of events need to be sent.
	evts := res.ToEvents()

	// Enqueue events.
	for _, evt := range evts {
		if evt != nil {
			// Push event to the queue
			r.queue.Push(evt)
		}
	}

	return ctrl.Result{}, nil
}

// Start implements the runnable interface needed in order to handle the start/stop
// using the manager. It starts go routines needed by the collector to interact with the
// broker.
func (r *ObjectMetaCollector) Start(ctx context.Context) error {
	return dispatch(ctx, r.logger, r.resource.Kind, r.subscriberChan, r.dispatcherChan, r.Client, r.subscribers)
}

// objFieldsHandler populates the resource from the object.
func (r *ObjectMetaCollector) objFieldsHandler(logger logr.Logger, res *events.Resource, obj *metav1.PartialObjectMetadata) error {
	if obj == nil {
		return nil
	}

	objUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		logger.Error(err, "unable to convert to unstructured")
		return err
	}

	// Remove unused meta fields
	metaUnused := []string{"creationTimestamp", "ownerReferences"}
	meta := objUn["metadata"]
	metaMap := meta.(map[string]interface{})
	for _, key := range metaUnused {
		delete(metaMap, key)
	}

	metaString, err := json.Marshal(metaMap)
	if err != nil {
		return err
	}
	res.SetMeta(string(metaString))

	return nil
}

// getSubscribers returns all the subscribers for the current resource.
// The subscribers are computed based on the nodes where a pod related to the current resource is running,
// and subscribers that want to receive events for those nodes.
func (r *ObjectMetaCollector) getSubscribers(ctx context.Context, logger logr.Logger, meta *metav1.ObjectMeta) (fields.Subscribers, error) {
	pods := corev1.PodList{}
	var namespace string
	// Special care for namespace resources.
	if r.resource.Kind == resource.Namespace {
		namespace = meta.Name
	} else {
		namespace = meta.Namespace
	}
	// List all the pods related to the current resource.
	err := r.List(ctx, &pods, client.InNamespace(namespace), r.podMatchingFields(meta))
	if err != nil {
		logger.Error(err, "unable to list pods related to resource", "in namespace", meta.Namespace)
		return nil, err
	}

	// If no pods found for the resource just return.
	if len(pods.Items) == 0 {
		return nil, nil
	}

	subs := make(fields.Subscribers)
	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName != "" {
			// Check if a subscriber exists for the node.
			if ok := r.subscribers.HasNode(pods.Items[i].Spec.NodeName); ok {
				s := r.subscribers.GetSubscribersPerNode(pods.Items[i].Spec.NodeName)
				for s1 := range s {
					subs.Add(s1)
				}
			}
		}
	}

	return subs, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ObjectMetaCollector) SetupWithManager(mgr ctrl.Manager) error {
	// Set the generic logger to be used in other function then the reconcile loop.
	r.logger = mgr.GetLogger().WithName(r.name)

	lc, err := newLogConstructor(mgr.GetLogger(), r.name, r.resource.Kind)
	if err != nil {
		return err
	}

	bld := ctrl.NewControllerManagedBy(mgr).
		For(r.resource,
			builder.OnlyMetadata,
			builder.WithPredicates(predicatesWithMetrics(r.name, apiServerSource, nil))).
		WatchesRawSource(r.dispatcherSource, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicatesWithMetrics(r.name, "dispatcher", nil))).
		WithOptions(controller.Options{LogConstructor: lc})

	if r.externalSource != nil {
		bld.WatchesRawSource(r.externalSource,
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicatesWithMetrics(r.name, resource.Pod, nil)))
	}

	return bld.Complete(r)
}

// GetName returns the name of the collector.
func (r *ObjectMetaCollector) GetName() string {
	return r.name
}

// NewPartialObjectMetadata returns a partial object metadata for a limited set of resources. It is used as a helper
// when triggering reconciles or instantiating a collector for a given resource.
func NewPartialObjectMetadata(kind string, name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	if kind == resource.Namespace || kind == resource.Service || kind == resource.ReplicationController {
		obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(kind))
	} else {
		obj.SetGroupVersionKind(appsv1.SchemeGroupVersion.WithKind(kind))
	}

	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}
