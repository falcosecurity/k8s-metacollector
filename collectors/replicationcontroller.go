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
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/alacuku/k8s-metadata/internal/events"
	"github.com/alacuku/k8s-metadata/internal/fields"
	"github.com/alacuku/k8s-metadata/internal/resource"
)

// ReplicationcontrollerCollector collects replicationcontrollers' metadata, puts them in a local cache and generates appropriate
// events when such resources change over time.
type ReplicationcontrollerCollector struct {
	client.Client
	Sink           chan<- events.Event
	ChannelMetrics *ChannelMetrics
	Cache          events.GenericCache
	GenericSource  source.Source
	Name           string
}

//+kubebuilder:rbac:groups=core,resources=replicationcontrollers,verbs=get;list;watch

// Reconcile generates events to be sent to nodes when changes are detected for the watched resources.
func (r *ReplicationcontrollerCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var rc = newPartialReplicationController
	var rcRes *events.GenericResource
	var ok, deleted, updated bool

	logger := log.FromContext(ctx)

	err = r.Get(ctx, req.NamespacedName, rc)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logger.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	if k8sApiErrors.IsNotFound(err) {
		// When the k8s resource get deleted we need to remove it from the local cache.
		if _, ok = r.Cache.Get(req.String()); ok {
			logger.Info("marking resource for deletion")
			deleted = true
		} else {
			return ctrl.Result{}, nil
		}
	}

	logger.V(5).Info("resource found")

	// Get all the nodes to which this resource is related.
	// The currentNodes are used to compute to which nodes we need to send an event
	// and of which type, Added, Deleted or Modified.
	currentNodes, err := r.Nodes(ctx, logger, &rc.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Check if the resource has already been cached.
	if rcRes, ok = r.Cache.Get(req.String()); !ok {
		// If first time, then we just create a new cache entry for it.
		logger.V(3).Info("never met this resource in my life")
		rcRes = events.NewGenericResourceFromMetadata(&rc.ObjectMeta, resource.ReplicationController)
	} else if !deleted {
		// When the resource has already been cached, check if the mutable fields have changed.
		updated = rcRes.UpdateLabels(rc.Labels)
	}

	// The resource has been created, or updated. Compute if we need to propagate events.
	// The outcome is saved internally to the resource. See AddNodes method for more info.
	if !deleted {
		// We need to know if the mutable fields has been changed. That's why AddNodes accepts
		// a bool. Otherwise, we can not tell if nodes need an "Added" event or a "Modified" one.
		rcRes.AddNodes(currentNodes.ToSlice(), updated)
	} else {
		// If the resource has been deleted from the api-server, then we send a "Deleted" event to all nodes
		rcRes.DeleteNodes(rcRes.Nodes.ToSlice())
	}

	// At this point our resource has all the necessary bits to know for each node which type of events need to be sent.
	evts := rcRes.ToEvents()

	// Enqueue events.
	for _, evt := range evts {
		if evt == nil {
			continue
		}
		switch evt.Type() {
		case events.Added:
			// Perform actions for "Added" events.
			eventTotal.WithLabelValues(r.Name, labelAdded).Inc()
			// For each resource that generates an "Added" event, we need to add it to the cache.
			// Please keep in mind that Cache operations resets the state of the resource, such as
			// resetting the info needed to generate the events.
			r.Cache.Add(req.String(), rcRes)
		case events.Modified:
			// Run specific code for "Modified" events.
			eventTotal.WithLabelValues(r.Name, labelUpdated).Inc()
			r.Cache.Update(req.String(), rcRes)
		case events.Deleted:
			// Run specific code for "Deleted" events.
			eventTotal.WithLabelValues(r.Name, labelDeleted).Inc()
			r.Cache.Delete(req.String())
		}
		// Add event to the queue.
		r.ChannelMetrics.Send(evt)
		r.Sink <- evt
	}

	return ctrl.Result{}, nil
}

func (r *ReplicationcontrollerCollector) initMetrics() {
	eventTotal.WithLabelValues(r.Name, labelAdded).Add(0)
	eventTotal.WithLabelValues(r.Name, labelUpdated).Add(0)
	eventTotal.WithLabelValues(r.Name, labelDeleted).Add(0)
}

// Nodes returns all the nodes where pods related to the current replicationcontroller are running.
func (r *ReplicationcontrollerCollector) Nodes(ctx context.Context, logger logr.Logger, meta *metav1.ObjectMeta) (fields.Nodes, error) {
	pods := corev1.PodList{}
	err := r.List(ctx, &pods, client.InNamespace(meta.Namespace), client.MatchingFields{
		podPrefixName: meta.Name + "-",
	})

	if err != nil {
		logger.Error(err, "unable to list pods related to resource", "in namespace", meta.Namespace)
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, nil
	}

	nodes := make(map[string]struct{}, len(pods.Items))
	for i := range pods.Items {
		if pods.Items[i].Spec.NodeName != "" {
			nodes[pods.Items[i].Spec.NodeName] = struct{}{}
		}
	}

	return nodes, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReplicationcontrollerCollector) SetupWithManager(mgr ctrl.Manager) error {
	r.initMetrics()

	lc, err := newLogConstructor(mgr.GetLogger(), r.Name, resource.ReplicationController)
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ReplicationController{}, builder.OnlyMetadata).
		WithOptions(controller.Options{LogConstructor: lc}).
		Watches(r.GenericSource, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
