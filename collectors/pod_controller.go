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

	"github.com/go-logr/logr"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/alacuku/k8s-metadata/internal/events"
)

// PodCollector reconciles a Pod object.
type PodCollector struct {
	client.Client
	Scheme         *runtime.Scheme
	Sink           chan<- events.Event
	ChannelMetrics *ChannelMetrics
	Pods           map[string]*events.Pod
	Name           string
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch
//+kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (pc *PodCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var pod corev1.Pod
	var event *events.Pod
	var err error
	var ok, created, updated bool

	logReq := log.FromContext(ctx)

	err = pc.Get(ctx, req.NamespacedName, &pod)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logReq.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	logReq = logReq.WithValues("node", pod.Spec.NodeName)

	if k8sApiErrors.IsNotFound(err) {
		// Remove the data related to this resource.
		if event, ok = pc.Pods[req.String()]; ok {
			logReq.Info("removing resource from cache")
			eventTotal.WithLabelValues(pc.Name, labelDeleted).Inc()
			event.EventType = "Deleted"
			pc.ChannelMetrics.Send(event)
			pc.Sink <- event
		}
		delete(pc.Pods, req.String())
		// Delete over GRPC or just set it as deleted and send the delete
		// operation in batch.
		return ctrl.Result{}, nil
	}

	logReq.V(3).Info("pod found")

	// Check if encountered the same pod before.
	if event, ok = pc.Pods[req.String()]; !ok {
		// If first time, then we just create a new fields.Pod for it.
		logReq.V(3).Info("never met this resource in my life")
		event = new(events.Pod)
		event.SetMetadata(&pod.ObjectMeta)
		created = true
	} else if event.UpdateLabels(pod.Labels) && !created {
		updated = true
	}

	ok, err = pc.OwnerRefsHandler(ctx, logReq, event, &pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if ok && !created {
		updated = true
	}

	ok, err = pc.ServiceRefsHandler(ctx, logReq, event, &pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	if ok && !created {
		updated = true
	}

	if created || updated {
		if created {
			eventTotal.WithLabelValues(pc.Name, labelAdded).Inc()
			event.EventType = "Added"
		}
		if updated {
			eventTotal.WithLabelValues(pc.Name, labelUpdated).Inc()
			event.EventType = "Modified"
		}

		logReq.Info("metadata fields updated for resources")
		pc.Pods[req.String()] = event
		// Here we need to send it over GRPC
		pc.ChannelMetrics.Send(event)
		pc.Sink <- event
	}

	// If the pod has been marked to be deleted than we requeue it.
	// If during the next reconcile it is not found in the cache, then we delete it.
	if !pod.ObjectMeta.DeletionTimestamp.IsZero() {
		// The default terminationGracePeriodSeconds is 30 seconds, so we requeue it after 30 seconds.
		return ctrl.Result{RequeueAfter: 30}, nil
	}

	return ctrl.Result{}, nil
}

// OwnerRefsHandler extracts the owner references for a given pod and updates the related event.
func (pc *PodCollector) OwnerRefsHandler(ctx context.Context, logger logr.Logger, evt *events.Pod, pod *corev1.Pod) (bool, error) {
	var updated bool
	// Get the owner reference, and if set, get the uid of the owner.
	owner := events.ManagingOwner(pod.OwnerReferences)
	if owner != nil {
		updated = evt.AddReferencesForKind(owner.Kind, []types.UID{owner.UID})
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
				if evt.AddReferencesForKind(owner.Kind, []types.UID{owner.UID}) && !updated {
					updated = true
				}
			}
		}
	}

	return updated, nil
}

// ServiceRefsHandler get the UID of each service serving the given pod and update the related event.
func (pc *PodCollector) ServiceRefsHandler(ctx context.Context, logger logr.Logger, evt *events.Pod, pod *corev1.Pod) (bool, error) {
	// List all the services in the pod's namespace.
	services := corev1.ServiceList{}
	err := pc.List(ctx, &services, &client.ListOptions{Namespace: pod.Namespace})
	if err != nil {
		logger.Error(err, "unable to get services list", "in namespace", pod.Namespace)
		return false, err
	}
	svcRefs := []types.UID{}
	for i := range services.Items {
		sel := labels.SelectorFromValidatedSet(services.Items[i].Spec.Selector)
		if !sel.Empty() && sel.Matches(labels.Set(pod.GetLabels())) {
			logger.V(3).Info("found service related to resource", "Service", klog.KRef(services.Items[i].Namespace, services.Items[i].Name))
			svcRefs = append(svcRefs, services.Items[i].UID)
		}
	}

	return evt.AddReferencesForKind("Service", svcRefs), nil
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

	lc, err := newLogConstructor(mgr.GetLogger(), pc.Name, "Pod")
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithOptions(controller.Options{LogConstructor: lc}).
		Complete(pc)
}
