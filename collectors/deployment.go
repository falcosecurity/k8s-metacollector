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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/alacuku/k8s-metadata/internal/events"
)

// DeploymentCollector collects deployment's metadata.
type DeploymentCollector struct {
	client.Client
	Sink           chan<- events.Event
	ChannelMetrics *ChannelMetrics
	Deployments    map[string]*events.Generic
	Name           string
}

//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

func (r *DeploymentCollector) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var err error
	var dpl = newPartialDeployment
	var event *events.Generic
	var ok, created, updated bool

	logReq := log.FromContext(ctx)

	err = r.Get(ctx, req.NamespacedName, dpl)
	if err != nil && !k8sApiErrors.IsNotFound(err) {
		logReq.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	if k8sApiErrors.IsNotFound(err) || !dpl.ObjectMeta.DeletionTimestamp.IsZero() {
		// Remove the data related to this resource.
		if _, ok := r.Deployments[req.String()]; ok {
			logReq.Info("removing resource from cache")
			eventTotal.WithLabelValues(r.Name, labelDeleted).Inc()
		}
		delete(r.Deployments, req.String())
		// Delete over GRPC or just set it as deleted and send the delete
		// operation in batch.
		return ctrl.Result{}, nil
	}

	logReq.V(3).Info("resource found")

	// Check if encountered the same resource before.
	if event, ok = r.Deployments[req.String()]; !ok {
		// If first time, then we just create a new event for it.
		logReq.V(3).Info("never met this resource in my life")
		event = new(events.Generic)
		event.SetMetadata(&dpl.ObjectMeta)
	} else if event.UpdateLabels(dpl.Labels) && !created {
		updated = true
	}

	currentNodes, err := r.Nodes(ctx, logReq, &dpl.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	a, m, d, n := eventsPerNode(event, currentNodes, updated)

	if len(a) != 0 {
		event.SetNodes(a)
		event.EventType = "Added"
		logReq.V(3).Info("metadata fields added for resource", "nodes", a)
		r.ChannelMetrics.Send(event)
		r.Sink <- event
	}

	if len(m) != 0 {
		event.SetNodes(m)
		event.EventType = "Modified"
		logReq.V(3).Info("metadata fields modified for resource", "nodes", m)
		r.ChannelMetrics.Send(event)
		r.Sink <- event
	}

	if len(d) != 0 {
		event.SetNodes(d)
		event.EventType = "Deleted"
		logReq.V(3).Info("metadata fields deleted for resource", "nodes", d)
		r.ChannelMetrics.Send(event)
		r.Sink <- event
	}

	if created || updated {
		if created {
			eventTotal.WithLabelValues(r.Name, labelAdded).Inc()
		}
		if updated {
			eventTotal.WithLabelValues(r.Name, labelUpdated).Inc()
		}
	}
	event.SetNodes(n)
	r.Deployments[req.String()] = event

	return ctrl.Result{}, nil
}

func (r *DeploymentCollector) initMetrics() {
	eventTotal.WithLabelValues(r.Name, labelAdded).Add(0)
	eventTotal.WithLabelValues(r.Name, labelUpdated).Add(0)
	eventTotal.WithLabelValues(r.Name, labelDeleted).Add(0)
}

func (r *DeploymentCollector) Nodes(ctx context.Context, logger logr.Logger, meta *metav1.ObjectMeta) (map[string]struct{}, error) {
	pods := corev1.PodList{}
	err := r.List(ctx, &pods, client.InNamespace(meta.Namespace), client.MatchingFields{
		podPrefixName: meta.Name,
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
func (r *DeploymentCollector) SetupWithManager(mgr ctrl.Manager) error {
	r.initMetrics()

	lc, err := newLogConstructor(mgr.GetLogger(), r.Name, "Deployment")
	if err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Deployment{}, builder.OnlyMetadata).
		WithOptions(controller.Options{LogConstructor: lc}).
		Complete(r)
}
