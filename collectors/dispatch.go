// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 The Falco Authors
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
	"sync"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

//nolint:gocyclo // complexity is inherent to the dispatch switch
func dispatch(ctx context.Context, logger logr.Logger, resourceKind string, subChan subscriber.SubsChan,
	dispatcherChan chan<- event.GenericEvent, cl client.Client, subscribers *subscriber.Subscribers) error {
	wg := sync.WaitGroup{}
	podList := &corev1.PodList{}
	serviceList := corev1.ServiceList{}
	replicaSet := NewPartialObjectMetadata(resource.ReplicaSet, nil)
	// it listens for new getSubscribers and sends the cached events to the
	// subscriber received on the channel.
	dispatchEventsOnSubscribe := func(ctx context.Context) {
		defer wg.Done()
		// send enqueues a reconcile request for the object. It returns false on shutdown, when the
		// controller no longer reads the requests and a send could block forever.
		send := func(obj client.Object) bool {
			select {
			case dispatcherChan <- event.GenericEvent{Object: obj}:
				return true
			case <-ctx.Done():
				return false
			}
		}
		for {
			select {
			case sub := <-subChan:
				if sub.Reason == subscriber.Unsubscribed {
					// Delete the subscriber for the given node.
					subscribers.DeleteSubscriberPerNode(sub.NodeName, sub.UID)
				} else {
					// Add the subscriber for the given node.
					subscribers.AddSubscriberPerNode(sub.NodeName, sub.UID)
				}
				logger.V(2).Info("Dispatching events", "subscriber", sub, "resourceKind", resourceKind)

				// List all pods related to the given node.
				if err := cl.List(ctx, podList, client.MatchingFields{
					nodeNameIndex: sub.NodeName,
				}); err != nil {
					// Do not dispatch the pods left in the list by the previous subscriber.
					logger.Error(err, "unable to dispatch pod events", "subscriber", sub, "resourceKind", resourceKind)
					continue
				}

				for podIndex := range podList.Items {
					switch resourceKind {
					case resource.Pod:
						if !send(&corev1.Pod{
							Name:      podList.Items[podIndex].Name,
							Namespace: podList.Items[podIndex].Namespace,
						}) {
							return
						}
					case resource.Namespace:
						if !send(&corev1.Namespace{
							Name: podList.Items[podIndex].Namespace,
						}) {
							return
						}
					case resource.ReplicaSet:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.ReplicaSet {
							if !send(&appsv1.ReplicaSet{
								Name:      owner.Name,
								Namespace: podList.Items[podIndex].Namespace,
							}) {
								return
							}
						}
					case resource.ReplicationController:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.ReplicationController {
							if !send(&corev1.ReplicationController{
								Name:      owner.Name,
								Namespace: podList.Items[podIndex].Namespace,
							}) {
								return
							}
						}
					case resource.Daemonset:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.Daemonset {
							if !send(&appsv1.DaemonSet{
								Name:      owner.Name,
								Namespace: podList.Items[podIndex].Namespace,
							}) {
								return
							}
						}
					case resource.Deployment:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.ReplicaSet {
							// Get the replicaset.
							if err := cl.Get(ctx, types.NamespacedName{
								Namespace: podList.Items[podIndex].Namespace,
								Name:      owner.Name,
							}, replicaSet); err != nil {
								logger.Error(err, "unable to dispatch events", "subscriber", sub, "resourceKind", resourceKind)
								continue
							}
							owner = events.ManagingOwner(replicaSet.OwnerReferences)
							if owner != nil && owner.Kind == resource.Deployment {
								if !send(&appsv1.ReplicaSet{
									Name:      owner.Name,
									Namespace: podList.Items[podIndex].Namespace,
								}) {
									return
								}
							}
						}
					case resource.Service:
						err := cl.List(ctx, &serviceList, &client.ListOptions{Namespace: podList.Items[podIndex].Namespace})
						if err != nil {
							logger.Error(err, "unable to get services list", "subscriber", sub, "resourceKind", resourceKind)
							continue
						}
						for svcIndex := range serviceList.Items {
							sel := labels.SelectorFromValidatedSet(serviceList.Items[svcIndex].Spec.Selector)
							if !sel.Empty() && sel.Matches(labels.Set(podList.Items[podIndex].GetLabels())) {
								if !send(&corev1.Service{
									Name:      serviceList.Items[svcIndex].Name,
									Namespace: podList.Items[podIndex].Namespace,
								}) {
									return
								}
							}
						}
					}
				}
				logger.V(2).Info("events correctly dispatched", "subscriber", sub, "resourceKind", resourceKind)

			case <-ctx.Done():
				// No need to wait for the subscribers to unsubscribe: on shutdown the grpc server
				// stops notifying the collectors about them.
				logger.V(2).Info("stopping dispatcher on new subscribers", "resourceKind", resourceKind)
				return
			}
		}
	}

	logger.Info("starting event dispatcher for new subscribers", "resourceKind", resourceKind)
	// Start the dispatcher.
	wg.Add(1)
	go dispatchEventsOnSubscribe(ctx)

	// Wait for shutdown signal.
	<-ctx.Done()
	logger.Info("waiting for event dispatcher to finish", "resourceKind", resourceKind)
	// Wait for goroutines to stop.
	wg.Wait()
	logger.Info("dispatcher finished", "resourceKind", resourceKind)

	return nil
}
