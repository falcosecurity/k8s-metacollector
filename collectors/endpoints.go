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

	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// EndpointsDispatcher each time an endpoint changes it triggers a reconcile for the pods and services to which it relates.
type EndpointsDispatcher struct {
	client.Client
	// For each endpoint we save the pods' names that belong to it.
	Pods                   map[string]map[string]struct{}
	PodCollectorSource     chan<- event.GenericEvent
	ServiceCollectorSource chan<- event.GenericEvent
	Name                   string
}

//+kubebuilder:rbac:groups=core,resources=endpoints,verbs=get;list;watch

// Reconcile if a new pod has been added/removed it sends an event (triggers) to the pod collector. The
// same is done for the Service to which the endpoint belongs.
func (r *EndpointsDispatcher) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var eps = &corev1.Endpoints{}

	logger := log.FromContext(ctx)

	err = r.Get(ctx, req.NamespacedName, eps)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logger.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	if k8sApiErrors.IsNotFound(err) {
		// Notify the pods that have been removed from the endpoint slice.
		pods, ok := r.Pods[req.String()]
		if ok {
			logger.V(3).Info("triggering pods and service since the resource has been deleted")
			r.triggerPods(req.Namespace, pods)
			r.triggerService(req.NamespacedName)
		}
		// When the k8s resource get deleted we need to remove it from the local cache.
		delete(r.Pods, req.String())
		return ctrl.Result{}, nil
	}

	logger.V(5).Info("resource found")

	// Get all the pods to which this resource is related.
	addedPods, deletedPods := r.getPods(eps, &req)

	// Trigger the pods.
	r.triggerPods(eps.Namespace, addedPods)
	r.triggerPods(eps.Namespace, deletedPods)
	r.triggerService(req.NamespacedName)

	return ctrl.Result{}, nil
}

func (r *EndpointsDispatcher) triggerPods(namespace string, pods map[string]struct{}) {
	for p := range pods {
		obj := NewPartialObjectMetadata(resource.Pod, &types.NamespacedName{
			Namespace: namespace,
			Name:      p,
		})

		r.PodCollectorSource <- event.GenericEvent{Object: obj}
	}
}

func (r *EndpointsDispatcher) triggerService(meta types.NamespacedName) {
	// Endpoints name is the same as the one of the service to which refers.
	obj := NewPartialObjectMetadata(resource.Service, &meta)

	r.ServiceCollectorSource <- event.GenericEvent{Object: obj}
}

func (r *EndpointsDispatcher) getPods(eps *corev1.Endpoints, req *ctrl.Request) (added, deleted map[string]struct{}) {
	existingPods, ok := r.Pods[req.String()]
	currentPods := make(map[string]struct{})
	added = make(map[string]struct{})
	deleted = make(map[string]struct{})

	if !ok {
		existingPods = make(map[string]struct{})
		for i := range eps.Subsets {
			for j := range eps.Subsets[i].Addresses {
				if eps.Subsets[i].Addresses[j].TargetRef != nil {
					existingPods[eps.Subsets[i].Addresses[j].TargetRef.Name] = struct{}{}
				}
			}
		}
		r.Pods[req.String()] = existingPods
		return existingPods, deleted
	}

	for i := range eps.Subsets {
		for j := range eps.Subsets[i].Addresses {
			if eps.Subsets[i].Addresses[j].TargetRef != nil {
				currentPods[eps.Subsets[i].Addresses[j].TargetRef.Name] = struct{}{}
				if _, ok := existingPods[eps.Subsets[i].Addresses[j].TargetRef.Name]; !ok {
					added[eps.Subsets[i].Addresses[j].TargetRef.Name] = struct{}{}
					existingPods[eps.Subsets[i].Addresses[j].TargetRef.Name] = struct{}{}
				}
			}
		}
	}
	for pod := range existingPods {
		if _, ok := currentPods[pod]; !ok {
			deleted[pod] = struct{}{}
		}
	}

	r.Pods[req.String()] = currentPods
	return added, deleted
}

// SetupWithManager sets up the controller with the Manager.
func (r *EndpointsDispatcher) SetupWithManager(mgr ctrl.Manager) error {
	lc, err := newLogConstructor(mgr.GetLogger(), r.Name, resource.Endpoints)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Endpoints{},
			builder.WithPredicates(predicatesWithMetrics(r.Name, apiServerSource, nil))).
		WithOptions(controller.Options{LogConstructor: lc}).
		Complete(r)
}
