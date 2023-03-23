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
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/apps/v1"
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
	event2 "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/alacuku/k8s-metadata/internal/events"
	"github.com/alacuku/k8s-metadata/internal/fields"
)

// PodCollector collects pods' metadata, puts them in a local cache and generates appropriated
// events when such resources change over time.
type PodCollector struct {
	client.Client
	Scheme          *runtime.Scheme
	Sink            chan<- events.Event
	ChannelMetrics  *ChannelMetrics
	Cache           events.PodCache
	Refs            references
	ExternalSources map[string]chan<- event2.GenericEvent
	Name            string
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch

// Reconcile generates events to be sent to nodes when changes are detected for the watched resources.
func (pc *PodCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod
	var pRes *events.PodResource
	var err error
	var ok, podDeleted, ownerRefs, svcRefs, updated bool

	logReq := log.FromContext(ctx)

	err = pc.Get(ctx, req.NamespacedName, &pod)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logReq.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	logReq = logReq.WithValues("node", pod.Spec.NodeName)

	if k8sApiErrors.IsNotFound(err) {
		// When the k8s resource get deleted we need to remove it from the local cache.
		if _, ok = pc.Cache.Get(req.String()); ok {
			logReq.V(3).Info("marking resource for deletion")
			podDeleted = true
		} else {
			return ctrl.Result{}, nil
		}
	}

	logReq.V(5).Info("pod found")

	// Check if the resource has already been cached.
	if pRes, ok = pc.Cache.Get(req.String()); !ok {
		// If first time, then we just create a new cache entry for it.
		logReq.V(3).Info("never met this resource in my life")
		pRes = events.NewPodResourceFromMetadata(&pod.ObjectMeta)
	} else if !podDeleted {
		// When the resource has already been cached, check if the mutable fields have changed.
		updated = pRes.UpdateLabels(pod.Labels)
	}

	// The resource has been created, or updated. Compute if we need to propagate events.
	// The outcome is saved internally to the resource. See AddNodes method for more info.
	if !podDeleted {
		// Get the owner references for the current resource. Note that we get the owner references
		// only for the one that are controllers.
		ownerRefs, err = pc.OwnerRefsHandler(ctx, logReq, pRes, &pod)
		if err != nil {
			return ctrl.Result{}, err
		}

		// Get references for all the services that are serving traffic to the current pod.
		svcRefs, err = pc.ServiceRefsHandler(ctx, logReq, pRes, &pod)
		if err != nil {
			return ctrl.Result{}, err
		}

		pRes.AddNodes([]string{pod.Spec.NodeName}, ownerRefs || svcRefs || updated)
	} else {
		// If the resource has been deleted from the api-server, then we send a "Deleted" event to all nodes
		pRes.DeleteNodes(pRes.Nodes.ToSlice())
	}

	// At this point our resource has all the necessary bits to know for each node which type of events need to be sent.
	evts := pRes.ToEvents()

	// Enqueue events.
	for _, evt := range evts {
		if evt == nil {
			continue
		}
		switch evt.Type() {
		case "Added":
			// Perform actions for "Added" events.
			eventTotal.WithLabelValues(pc.Name, labelAdded).Inc()
			// For each resource that generates an "Added" event, we need to add it to the cache.
			// Please keep in mind that Cache operations resets the state of the resource, such as
			// resetting the info needed to generate the events.
			pc.Cache.Add(req.String(), pRes)
		case "Modified":
			eventTotal.WithLabelValues(pc.Name, labelUpdated).Inc()
			pc.Cache.Update(req.String(), pRes)
		case "Deleted":
			eventTotal.WithLabelValues(pc.Name, labelDeleted).Inc()
			pc.triggerOwnersOnDeleteEvent(pRes)
			pc.Cache.Delete(req.String())
		}
		// Add event to the queue.
		pc.ChannelMetrics.Send(evt)
		pc.Sink <- evt
	}

	return ctrl.Result{}, nil
}

// OwnerRefsHandler extracts the owner references for a given pod and updates the related event.
// It takes in account only references for the owners that are also controllers of the pod resource.
func (pc *PodCollector) OwnerRefsHandler(ctx context.Context, logger logr.Logger, evt *events.PodResource, pod *corev1.Pod) (bool, error) {
	if pod == nil {
		return false, nil
	}

	var updated bool
	// Get the owner reference, and if set, get the uid of the owner.
	owner := events.ManagingOwner(pod.OwnerReferences)
	if owner != nil {
		updated = evt.AddReferencesForKind(owner.Kind, []fields.Reference{{
			Name: types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      owner.Name,
			},
			UID: owner.UID,
		}})
		// If we are handling a replicaset, then fetch it and check if it has an owner.
		if owner.Kind == "ReplicaSet" {
			replicaset := v1.ReplicaSet{}
			err := pc.Get(ctx, types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      owner.Name,
			}, &replicaset)
			if err != nil && !k8sApiErrors.IsNotFound(err) {
				logger.Error(err, "unable to get resource related to", "ReplicaSet", klog.KRef(pod.Namespace, owner.Name))
				return updated, err
			}
			owner = events.ManagingOwner(replicaset.OwnerReferences)
			if owner != nil {
				if evt.AddReferencesForKind(owner.Kind, []fields.Reference{{
					Name: types.NamespacedName{
						Namespace: pod.Namespace,
						Name:      owner.Name,
					},
					UID: owner.UID,
				}}) && !updated {
					updated = true
				}
			}
		}
	}

	return updated, nil
}

// ServiceRefsHandler get the UID of each service serving the given pod and update the related event.
func (pc *PodCollector) ServiceRefsHandler(ctx context.Context, logger logr.Logger, evt *events.PodResource, pod *corev1.Pod) (bool, error) {
	if pod == nil {
		return false, nil
	}

	// List all the services in the pod's namespace.
	services := corev1.ServiceList{}
	err := pc.List(ctx, &services, &client.ListOptions{Namespace: pod.Namespace})
	if err != nil {
		logger.Error(err, "unable to get services list", "in namespace", pod.Namespace)
		return false, err
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

	if evt.AddReferencesForKind("Service", svcRefs) {
		evt.SetModifiedFor([]string{pod.Spec.NodeName})
	}

	return false, nil
}

func (pc *PodCollector) triggerOwnersOnDeleteEvent(evt *events.PodResource) {
	for kind, refs := range evt.ResourceReferences {
		ch, ok := pc.ExternalSources[kind]
		if !ok {
			continue
		}

		for _, ref := range refs {
			go func(name types.NamespacedName) {
				dpl := partialDeployment(&name)
				time.Sleep(10 * time.Second)
				ch <- event2.GenericEvent{Object: dpl}
			}(ref.Name)
		}
	}
}

// initMetrics initializes the custom metrics for the pod collector.
func (pc *PodCollector) initMetrics() {
	eventTotal.WithLabelValues(pc.Name, labelAdded).Add(0)
	eventTotal.WithLabelValues(pc.Name, labelUpdated).Add(0)
	eventTotal.WithLabelValues(pc.Name, labelDeleted).Add(0)
}

// SetupWithManager sets up the controller with the Manager.
func (pc *PodCollector) SetupWithManager(mgr ctrl.Manager) error {
	pc.initMetrics()

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

	lc, err := newLogConstructor(mgr.GetLogger(), pc.Name, "PodResource")
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}, builder.WithPredicates(predicate.NewPredicateFuncs(nodeNameFilter))).
		WithOptions(controller.Options{LogConstructor: lc}).
		Complete(pc)
}
