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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/falcosecurity/k8s-metacollector/broker"
	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/fields"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
)

// TestEndpointslicesDispatcherReconcileReturnsOnCanceledContext reproduces the shutdown hang observed in the
// e2e "Lifecycle of metacollector" specs. At manager shutdown the pod and service collectors stop reading
// their source channels; a Reconcile that still has triggers to send must return once its context is
// canceled instead of blocking forever, otherwise the controller worker never finishes and the manager
// waits for the whole graceful shutdown period before exiting with an error.
func TestEndpointslicesDispatcherReconcileReturnsOnCanceledContext(t *testing.T) {
	eps := &discoveryv1.EndpointSlice{
		Name: "nginx-abc12", Namespace: "nginx-test", GenerateName: "nginx-",
		Endpoints: []discoveryv1.Endpoint{
			{TargetRef: &corev1.ObjectReference{Kind: resource.Pod, Name: "pod-1"}},
			{TargetRef: &corev1.ObjectReference{Kind: resource.Pod, Name: "pod-2"}},
			{TargetRef: &corev1.ObjectReference{Kind: resource.Pod, Name: "pod-3"}},
		},
	}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(eps).Build()

	// Same channel capacity as cmd/collector/run/run.go, and nobody reading: the collectors already stopped.
	dispatcher := &EndpointslicesDispatcher{
		Client:                 cl,
		Name:                   "endpointslices-dispatcher",
		PodCollectorSource:     make(chan event.GenericEvent, 1),
		ServiceCollectorSource: make(chan event.GenericEvent, 1),
		Pods:                   make(map[string]map[string]struct{}),
		ServicesName:           make(map[string]string),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = dispatcher.Reconcile(ctx, ctrl.Request{
			Namespace: eps.Namespace, Name: eps.Name,
		})
	}()

	// Simulate the manager shutdown: cancel the context while Reconcile is blocked sending triggers.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Reconcile did not return after the context was canceled: blocked on a channel send")
	}
}

// TestCollectorsReconcileReturnsOnCanceledContext covers the same shutdown hang for the collectors pushing events
// to the broker: at shutdown the broker stops popping from the queue, and a Reconcile with an event to push must
// return once its context is canceled instead of blocking the controller worker forever.
func TestCollectorsReconcileReturnsOnCanceledContext(t *testing.T) {
	pod := newPod("nginx-a", "node-1")
	svc := &corev1.Service{Name: "nginx", Namespace: testNamespace, UID: "uid-svc",
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "nginx"}}}

	tests := []struct {
		name      string
		reconcile func(ctx context.Context, cl client.Client, q broker.Queue) error
	}{
		{
			name: "pod",
			reconcile: func(ctx context.Context, cl client.Client, q broker.Queue) error {
				c := NewPodCollector(cl, q, events.NewCache(), "pod-collector")
				c.subscribers.AddSubscriberPerNode("node-1", "sub-1")
				_, err := c.Reconcile(ctx, reconcileRequest(testNamespace, pod.Name))
				return err
			},
		},
		{
			name: "object metadata",
			reconcile: func(ctx context.Context, cl client.Client, q broker.Queue) error {
				c := NewObjectMetaCollector(cl, q, events.NewCache(), NewPartialObjectMetadata(resource.Namespace, nil), "namespace-collector")
				c.subscribers.AddSubscriberPerNode("node-1", "sub-1")
				_, err := c.Reconcile(ctx, reconcileRequest("", testNamespace))
				return err
			},
		},
		{
			name: "service",
			reconcile: func(ctx context.Context, cl client.Client, q broker.Queue) error {
				c := NewServiceCollector(cl, q, events.NewCache(), "service-collector")
				c.subscribers.AddSubscriberPerNode("node-1", "sub-1")
				_, err := c.Reconcile(ctx, reconcileRequest(testNamespace, svc.Name))
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := newFakeClient(newNamespace(), copyObj(t, pod), copyObj(t, svc))
			// Same queue as cmd/collector/run/run.go, already full, and nobody popping: the broker already stopped.
			q := broker.NewBlockingChannel(1)
			other := events.NewResource(resource.Pod, "uid-other")
			other.GenerateSubscribers(fields.Subscribers{"sub-other": struct{}{}})
			q.Push(context.Background(), other.ToEvents()[0])

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			done := make(chan error, 1)
			go func() { done <- tt.reconcile(ctx, cl, q) }()

			// Simulate the manager shutdown: cancel the context while Reconcile is blocked pushing its event.
			time.Sleep(200 * time.Millisecond)
			cancel()

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("Reconcile did not return after the context was canceled: blocked pushing to the queue")
			}
		})
	}
}
