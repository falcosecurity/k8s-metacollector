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
	"sync"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func dispatch(ctx context.Context, logger logr.Logger, resourceKind string, subChan subscriber.SubsChan,
	dispatcherChan chan<- event.GenericEvent, cl client.Client, subscribers *subscriber.Subscribers) error {
	wg := sync.WaitGroup{}
	podList := &corev1.PodList{}
	serviceList := corev1.ServiceList{}
	replicaSet := NewPartialObjectMetadata(resource.ReplicaSet, nil)
	// it listens for new getSubscribers and sends the cached events to the
	// subscriber received on the channel.
	dispatchEventsOnSubscribe := func(ctx context.Context) {
		wg.Add(1)
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
					logger.Error(err, "unable to dispatch pod events", "subscriber", sub, "resourceKind", resourceKind)
				}

				for podIndex := range podList.Items {
					switch resourceKind {
					case resource.Pod:
						dispatcherChan <- event.GenericEvent{Object: &corev1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Name:      podList.Items[podIndex].Name,
								Namespace: podList.Items[podIndex].Namespace,
							},
						}}
					case resource.Namespace:
						dispatcherChan <- event.GenericEvent{Object: &corev1.Namespace{
							ObjectMeta: metav1.ObjectMeta{
								Name: podList.Items[podIndex].Namespace,
							},
						}}
					case resource.ReplicaSet:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.ReplicaSet {
							dispatcherChan <- event.GenericEvent{Object: &appsv1.ReplicaSet{
								ObjectMeta: metav1.ObjectMeta{
									Name:      owner.Name,
									Namespace: podList.Items[podIndex].Namespace,
								},
							}}
						}
					case resource.ReplicationController:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.ReplicationController {
							dispatcherChan <- event.GenericEvent{Object: &corev1.ReplicationController{
								ObjectMeta: metav1.ObjectMeta{
									Name:      owner.Name,
									Namespace: podList.Items[podIndex].Namespace,
								},
							}}
						}
					case resource.Daemonset:
						owner := events.ManagingOwner(podList.Items[podIndex].OwnerReferences)
						if owner != nil && owner.Kind == resource.Daemonset {
							dispatcherChan <- event.GenericEvent{Object: &appsv1.DaemonSet{
								ObjectMeta: metav1.ObjectMeta{
									Name:      owner.Name,
									Namespace: podList.Items[podIndex].Namespace,
								},
							}}
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
								dispatcherChan <- event.GenericEvent{Object: &appsv1.ReplicaSet{
									ObjectMeta: metav1.ObjectMeta{
										Name:      owner.Name,
										Namespace: podList.Items[podIndex].Namespace,
									},
								}}
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
								dispatcherChan <- event.GenericEvent{Object: &corev1.Service{
									ObjectMeta: metav1.ObjectMeta{
										Name:      serviceList.Items[svcIndex].Name,
										Namespace: podList.Items[podIndex].Namespace,
									},
								}}
							}
						}
					}
				}
				logger.V(2).Info("events correctly dispatched", "subscriber", sub, "resourceKind", resourceKind)

			case <-ctx.Done():
				logger.V(2).Info("stopping dispatcher on new getSubscribers", "resourceKind", resourceKind)
				wg.Done()
				return
			}
		}
	}

	logger.Info("starting event dispatcher for new subscribers", "resourceKind", resourceKind)
	// Start the dispatcher.
	go dispatchEventsOnSubscribe(ctx)

	// Wait for shutdown signal.
	<-ctx.Done()
	logger.Info("waiting for event dispatcher to finish", "resourceKind", resourceKind)
	// Wait for goroutines to stop.
	wg.Wait()
	logger.Info("dispatcher finished", "resourceKind", resourceKind)

	return nil
}
