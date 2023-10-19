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
	"sort"

	"github.com/go-logr/logr"
	"github.com/mitchellh/hashstructure/v2"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
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

// PodCollector collects pods' metadata, puts them in a local cache and generates appropriate
// events when such resources change over time.
type PodCollector struct {
	client.Client
	queue broker.Queue
	cache *events.Cache
	// Sources where to trigger events when an owner need to be notified about a change.
	ownersSources   map[string]chan<- event.GenericEvent
	endpointsSource source.Source
	name            string
	// subscriberChan where new subscribers notify their presence.
	subscriberChan subscriber.SubsChan
	logger         logr.Logger
	// dispatcherSource is used to get events enqueued by the dispatcher based
	// on subscribers' arrival.
	dispatcherSource source.Source
	// dispatcherChan is the channel where the dispatcher pushes the new requests to be enqueued and
	// processed by the reconciler.
	dispatcherChan chan event.GenericEvent
	// subscribers current subscribers that are interested for pod resources.
	subscribers *subscriber.Subscribers
}

// NewPodCollector returns a new pod collector.
func NewPodCollector(cl client.Client, queue broker.Queue, cache *events.Cache, name string, opt ...CollectorOption) *PodCollector {
	opts := collectorOptions{}
	for _, o := range opt {
		o(&opts)
	}

	dc := make(chan event.GenericEvent, 1)

	return &PodCollector{
		Client:           cl,
		queue:            queue,
		cache:            cache,
		ownersSources:    opts.ownerSources,
		endpointsSource:  opts.externalSource,
		name:             name,
		subscriberChan:   opts.subscriberChan,
		dispatcherSource: &source.Channel{Source: dc},
		dispatcherChan:   dc,
		subscribers:      subscriber.NewSubscribers(),
	}
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch

// Reconcile generates events to be sent to nodes when changes are detected for the watched resources.
func (pc *PodCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod
	var pRes *events.Resource
	var cEntry *events.CacheEntry

	var err error
	var ok, podDeleted bool
	logReq := log.FromContext(ctx)

	err = pc.Get(ctx, req.NamespacedName, &pod)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logReq.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	logReq = logReq.WithValues("node", pod.Spec.NodeName)

	if k8sApiErrors.IsNotFound(err) {
		// When the k8s resource get deleted we need to remove it from the local cache.
		if _, ok = pc.cache.Get(req.String()); ok {
			logReq.V(3).Info("marking resource for deletion")
			podDeleted = true
		} else {
			return ctrl.Result{}, nil
		}
	}

	logReq.V(5).Info("pod found")

	// Create the resource and populate all its fields.
	if !podDeleted {
		// Get all subscribers for the resource based on its node name.
		// The subscribers are used to compute to which subscribers we need to send an event
		// and of which type, Create, Delete or Update.
		subs := pc.subscribers.GetSubscribersPerNode(pod.Spec.NodeName)
		// If no subscribers, just exit.
		if subs == nil {
			// Make sure to remove the cache entry for the resource.
			// This could happen when a subscriber closes its connection.
			pc.cache.Delete(req.String())
			return ctrl.Result{}, nil
		}

		// Create a new events.Resource and fill its fields.
		pRes = events.NewResource(resource.Pod, string(pod.UID))
		// Add namespace reference.
		if err = pc.namespaceRefsHandler(ctx, logReq, pRes, &pod); err != nil {
			return ctrl.Result{}, err
		}
		// Get the owner references for the current resource. Note that we get the owner references
		// only for the one that are controllers.
		if err := pc.ownerRefsHandler(ctx, logReq, pRes, &pod); err != nil {
			return ctrl.Result{}, err
		}
		// Get references for all the services that are serving traffic to the current pod.
		if err = pc.serviceRefsHandler(ctx, logReq, pRes, &pod); err != nil {
			return ctrl.Result{}, err
		}
		// Fill resource fields.
		if err = pc.objFieldsHandler(logReq, pRes, &pod); err != nil {
			return ctrl.Result{}, err
		}

		// Hash the current resource.
		hash, err := hashstructure.Hash(*pRes, hashstructure.FormatV2, nil)
		if err != nil {
			logReq.Error(err, "unable to hash resource")
			return ctrl.Result{}, err
		}

		// Check if we have cached the resource previously.
		if cEntry, ok = pc.cache.Get(req.String()); ok {
			// If an entry exists for the resource then check if the hashes are the same.
			// If not, it means that the resource fields have changes since the last time.
			// so mark the resource as updated. The "update" flag is needed to generate "Update"
			// events for the related subscribers.
			if cEntry.Hash != hash {
				pRes.SetUpdate(true)
				cEntry.Hash = hash
			}
			// Set the previous subscribers in the current resource.
			pRes.SetSubscribers(cEntry.Subs)
		} else {
			// If we never cached the resource then create an entry and add it to the cache.
			cEntry = &events.CacheEntry{
				Hash: hash,
				UID:  pod.UID,
				Subs: nil,
			}
			pc.cache.Add(req.String(), cEntry)
		}

		// Generate the subscribers, and save them in the entry cache.
		cEntry.Subs = pRes.GenerateSubscribers(subs)
		// Save the references. Needed when the resource is deleted.
		cEntry.Refs = pRes.GetResourceReferences()
	} else {
		// Check if we have cached the resource.
		if cEntry, ok = pc.cache.Get(req.String()); ok {
			// Create the resource.
			pRes = events.NewResource(resource.Pod, string(cEntry.UID))
			// Set the previous subscribers and references.
			pRes.SetSubscribers(cEntry.Subs)
			pRes.ResourceReferences = cEntry.Refs
			// The resource has been deleted. We need to send a delete event to
			// the subscribers. By generating the subscribers from an empty set,
			// is the same as to generate delete events for all the subscribers to which
			// we sent an event.
			pRes.GenerateSubscribers(nil)
			// We are ready to remove the entry from the cache. No need to track anymore
			// the deleted resource.
			pc.cache.Delete(req.String())
		} else {
			// It means that we received a delete event for a resource that we never sent to any subscriber.
			// In this case we just return.
			return ctrl.Result{}, nil
		}
	}

	// At this point our resource has all the necessary bits to know for each node which type of events need to be sent.
	evts := pRes.ToEvents()

	// Enqueue events.
	for _, evt := range evts {
		if evt == nil {
			continue
		}
		switch evt.Type() {
		case "Create":
			// Perform actions for "Create" events.
			pc.triggerOwnersOnCreateEvent(pRes)
		case "Delete":
			pc.triggerOwnersOnDeleteEvent(pRes)
		}
		// Push event to the queue.
		pc.queue.Push(evt)
	}

	return ctrl.Result{}, nil
}

// ownerRefsHandler extracts the owner references for a given pod and updates the related event.
// It takes in account only references for the owners that are also controllers of the pod resource.
func (pc *PodCollector) ownerRefsHandler(ctx context.Context, logger logr.Logger, res *events.Resource, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}

	// Get the owner reference, and if set, get the uid of the owner.
	owner := events.ManagingOwner(pod.OwnerReferences)
	if owner != nil {
		res.AddReferencesForKind(owner.Kind, []fields.Reference{{
			Name: types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      owner.Name,
			},
			UID: owner.UID,
		}})
		// If we are handling a replicaset, then fetch it and check if it has an owner.
		if owner.Kind == resource.ReplicaSet {
			replicaset := NewPartialObjectMetadata(resource.ReplicaSet, nil)
			err := pc.Get(ctx, types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      owner.Name,
			}, replicaset)
			if err != nil && !k8sApiErrors.IsNotFound(err) {
				logger.Error(err, "unable to get resource related to", "ReplicaSet", klog.KRef(pod.Namespace, owner.Name))
				return err
			}
			owner = events.ManagingOwner(replicaset.OwnerReferences)
			if owner != nil {
				res.AddReferencesForKind(owner.Kind, []fields.Reference{{
					Name: types.NamespacedName{
						Namespace: pod.Namespace,
						Name:      owner.Name,
					},
					UID: owner.UID,
				}})
			}
		}
	}

	return nil
}

// serviceRefsHandler get the UID of each service serving the given pod and update the related resource.
func (pc *PodCollector) serviceRefsHandler(ctx context.Context, logger logr.Logger, res *events.Resource, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}

	// List all the services in the pod's namespace.
	services := corev1.ServiceList{}
	err := pc.List(ctx, &services, &client.ListOptions{Namespace: pod.Namespace})
	if err != nil {
		logger.Error(err, "unable to get services list", "in namespace", pod.Namespace)
		return err
	}
	var svcRefs []fields.Reference
	for i := range services.Items {
		sel := labels.SelectorFromValidatedSet(services.Items[i].Spec.Selector)
		if !sel.Empty() && sel.Matches(labels.Set(pod.GetLabels())) {
			logger.V(3).Info("found service related to resource", "Service", klog.KRef(services.Items[i].Namespace, services.Items[i].Name))
			svcRefs = append(svcRefs, fields.Reference{
				Name: types.NamespacedName{
					Namespace: services.Items[i].Namespace,
					Name:      services.Items[i].Name,
				},
				UID: services.Items[i].UID,
			})
		}
	}

	// Need to sort for the hashing function.
	sort.Slice(svcRefs, func(i, j int) bool {
		return svcRefs[i].UID < svcRefs[j].UID
	})
	res.AddReferencesForKind(resource.Service, svcRefs)

	return nil
}

// objFieldsHandler populates the resource from the object.
func (pc *PodCollector) objFieldsHandler(logger logr.Logger, res *events.Resource, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}

	podUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(pod)
	if err != nil {
		logger.Error(err, "unable to convert to unstructured")
		return err
	}

	// Remove unused meta fields.
	metaUnused := []string{"creationTimestamp", "ownerReferences"}
	meta := podUn["metadata"]
	metaMap := meta.(map[string]interface{})
	for _, key := range metaUnused {
		delete(metaMap, key)
	}

	metaString, err := json.Marshal(metaMap)
	if err != nil {
		return err
	}
	res.SetMeta(string(metaString))

	// Marshal status to json.
	statusString, err := json.Marshal(podUn["status"])
	if err != nil {
		return err
	}
	res.SetStatus(string(statusString))

	return nil
}

// namespaceRefsHandler get the UID of the namespace of the given pod and update the related event.
func (pc *PodCollector) namespaceRefsHandler(ctx context.Context, logger logr.Logger, res *events.Resource, pod *corev1.Pod) error {
	if pod == nil {
		return nil
	}

	// Get the pod's namespace.
	namespace := NewPartialObjectMetadata(resource.Namespace, nil)
	nsKey := types.NamespacedName{
		Namespace: "",
		Name:      pod.Namespace,
	}
	err := pc.Get(ctx, nsKey, namespace)
	if err != nil {
		logger.Error(err, "unable to get", "namespace", pod.Namespace)
		return err
	}

	res.AddReferencesForKind(resource.Namespace, []fields.Reference{{
		Name: nsKey,
		UID:  namespace.UID,
	}})

	return nil
}

func (pc *PodCollector) triggerOwnersOnDeleteEvent(res *events.Resource) {
	refs := res.GetResourceReferences()
	for kind, refs := range refs {
		ch, ok := pc.ownersSources[kind]
		if !ok {
			continue
		}

		for _, ref := range refs {
			go func(name types.NamespacedName, kind string) {
				var obj client.Object
				switch kind {
				case resource.Deployment, resource.ReplicaSet, resource.Namespace:
					obj = NewPartialObjectMetadata(kind, &name)
				}
				if obj != nil {
					ch <- event.GenericEvent{Object: obj}
				}
			}(ref.Name, kind)
		}
	}
}

func (pc *PodCollector) triggerOwnersOnCreateEvent(res *events.Resource) {
	refs := res.GetResourceReferences()
	for kind, refs := range refs {
		ch, ok := pc.ownersSources[kind]
		if !ok {
			continue
		}

		for _, ref := range refs {
			go func(name types.NamespacedName, kind string) {
				var obj client.Object
				if kind == resource.Namespace {
					obj = NewPartialObjectMetadata(kind, &name)
				}
				if obj != nil {
					ch <- event.GenericEvent{Object: obj}
				}
			}(ref.Name, kind)
		}
	}
}

// Start implements the runnable interface needed in order to handle the start/stop
// using the manager. It starts go routines needed by the collector to interact with the
// broker.
func (pc *PodCollector) Start(ctx context.Context) error {
	return dispatch(ctx, pc.logger, resource.Pod, pc.subscriberChan, pc.dispatcherChan, pc.Client, pc.subscribers)
}

// SetupWithManager sets up the controller with the Manager.
func (pc *PodCollector) SetupWithManager(mgr ctrl.Manager) error {
	nodeNameFilter := func(obj client.Object) bool {
		// Check if the object is a pod.
		p, ok := obj.(*corev1.Pod)
		if !ok {
			return false
		}
		// If the pod has been already assigned to a node then proceed.
		if p.Spec.NodeName != "" {
			return true
		}

		return false
	}
	// Set the generic logger to be used in other function then the reconcile loop.
	pc.logger = mgr.GetLogger().WithName(pc.name)

	lc, err := newLogConstructor(mgr.GetLogger(), pc.name, resource.Pod)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{},
			builder.WithPredicates(predicatesWithMetrics(pc.name, apiServerSource, nodeNameFilter))).
		WatchesRawSource(pc.endpointsSource,
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicatesWithMetrics(pc.name, resource.EndpointSlice, nil))).
		WatchesRawSource(pc.dispatcherSource,
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicatesWithMetrics(pc.name, "dispatcher", nil))).
		WithOptions(controller.Options{LogConstructor: lc}).
		Complete(pc)
}
