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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

func newService(selector map[string]string) *corev1.Service {
	return &corev1.Service{
		Name: "nginx", Namespace: testNamespace, UID: "uid-svc",
		Spec: corev1.ServiceSpec{Selector: selector},
	}
}

func updateService(fn func(svc *corev1.Service)) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(t *testing.T, cl client.Client, _ *subscriber.Subscribers) {
		t.Helper()
		svc := &corev1.Service{}
		require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "nginx"}, svc))
		fn(svc)
		require.NoError(t, cl.Update(context.Background(), svc))
	}
}

func TestServiceCollectorReconcile(t *testing.T) {
	selector := map[string]string{"app": "nginx"}
	podWithoutIP := newPod("pod-no-ip", "node-2")
	podWithoutIP.Status.PodIP = ""

	tests := []struct {
		name  string
		svc   *corev1.Service
		pods  []client.Object
		steps []step
	}{
		{
			name: "lifecycle of a service",
			svc:  newService(selector),
			pods: []client.Object{newPod("pod-1", "node-1")},
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{name: "reconcile without changes", do: noop},
				{
					name: "update",
					do:   updateService(func(svc *corev1.Service) { svc.Labels = map[string]string{"version": "v2"} }),
					want: []wantEvent{{events.Update, []string{"sub-1"}}},
				},
				{name: "delete", do: deleteObj(newService(selector)), want: []wantEvent{{events.Delete, []string{"sub-1"}}}},
			},
		},
		{
			name: "pods without IP are ignored",
			svc:  newService(selector),
			pods: []client.Object{podWithoutIP},
			steps: []step{
				{name: "subscribe", do: addSub("node-2", "sub-2")},
			},
		},
		{
			// The subscriber is still connected: it must be told that the service no longer targets pods on its node.
			name: "selector stops matching the pods",
			svc:  newService(selector),
			pods: []client.Object{newPod("pod-1", "node-1")},
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{
					name: "change selector",
					do:   updateService(func(svc *corev1.Service) { svc.Spec.Selector = map[string]string{"app": "other"} }),
					want: []wantEvent{{events.Delete, []string{"sub-1"}}},
				},
			},
		},
		{
			// A service without selector does not select any pod, as for the pod collector.
			name: "service without selector",
			svc:  newService(nil),
			pods: []client.Object{newPod("pod-1", "node-1")},
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1")},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cl := newFakeClient(append([]client.Object{newNamespace(), test.svc}, test.pods...)...)
			q := &recordingQueue{}
			c := NewServiceCollector(cl, q, events.NewCache(), "service-collector")

			for _, s := range test.steps {
				s.do(t, cl, c.subscribers)
				_, err := c.Reconcile(context.Background(), reconcileRequest(testNamespace, "nginx"))
				require.NoError(t, err, s.name)
				want := s.want
				if want == nil {
					want = []wantEvent{}
				}
				require.Equal(t, want, summarize(q.take()), s.name)
			}
		})
	}
}

func TestServiceCollectorEventContent(t *testing.T) {
	t.Parallel()

	svc := newService(map[string]string{"app": "nginx"})
	svc.ResourceVersion = "7"
	q := &recordingQueue{}
	c := NewServiceCollector(newFakeClient(newNamespace(), svc, newPod("pod-1", "node-1")), q, events.NewCache(), "service-collector")
	c.subscribers.AddSubscriberPerNode("node-1", "sub-1")

	_, err := c.Reconcile(context.Background(), reconcileRequest(testNamespace, "nginx"))
	require.NoError(t, err)
	evts := q.take()
	require.Len(t, evts, 1)

	msg := evts[0].GRPCMessage()
	require.Equal(t, events.Create, msg.GetReason())
	require.Equal(t, resource.Service, msg.GetKind())
	require.Equal(t, "uid-svc", msg.GetUid())
	require.True(t, hasKey(msg.GetMeta(), "name"), msg.GetMeta())
	require.False(t, hasKey(msg.GetMeta(), "resourceVersion"), msg.GetMeta())
}
