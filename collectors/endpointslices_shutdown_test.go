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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
)

// TestEndpointslicesDispatcherReconcileReturnsOnCanceledContext reproduces the shutdown hang observed in the
// e2e "Lifecycle of metacollector" specs. At manager shutdown the pod and service collectors stop reading
// their source channels; a Reconcile that still has triggers to send must return once its context is
// canceled instead of blocking forever, otherwise the controller worker never finishes and the manager
// waits for the whole graceful shutdown period before exiting with an error.
func TestEndpointslicesDispatcherReconcileReturnsOnCanceledContext(t *testing.T) {
	eps := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Name: "nginx-abc12", Namespace: "nginx-test", GenerateName: "nginx-"},
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
			NamespacedName: types.NamespacedName{Namespace: eps.Namespace, Name: eps.Name},
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
