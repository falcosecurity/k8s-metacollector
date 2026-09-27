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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

func newPod(name, node string) *corev1.Pod {
	return &corev1.Pod{
		Name:      name,
		Namespace: testNamespace,
		UID:       types.UID("uid-pod-" + name),
		Labels:    map[string]string{"app": "nginx"},
		Spec:      corev1.PodSpec{NodeName: node},
		Status:    corev1.PodStatus{PodIP: "10.0.0.1"},
	}
}

// step changes the cluster or the subscribers, then the collector reconciles the pod.
type step struct {
	name string
	do   func(t *testing.T, cl client.Client, subs *subscriber.Subscribers)
	want []wantEvent
}

func addSub(node, sub string) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(_ *testing.T, _ client.Client, subs *subscriber.Subscribers) {
		subs.AddSubscriberPerNode(node, sub)
	}
}

func delSub(node, sub string) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(_ *testing.T, _ client.Client, subs *subscriber.Subscribers) {
		subs.DeleteSubscriberPerNode(node, sub)
	}
}

func relabelPod(name string) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(t *testing.T, cl client.Client, _ *subscriber.Subscribers) {
		t.Helper()
		pod := &corev1.Pod{}
		require.NoError(t, cl.Get(context.Background(), reconcileRequest(testNamespace, name).NamespacedName, pod))
		pod.Labels["version"] = "v2"
		require.NoError(t, cl.Update(context.Background(), pod))
	}
}

func deleteObj(obj client.Object) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(t *testing.T, cl client.Client, _ *subscriber.Subscribers) {
		t.Helper()
		require.NoError(t, cl.Delete(context.Background(), copyObj(t, obj)))
	}
}

func noop(*testing.T, client.Client, *subscriber.Subscribers) {}

func TestPodCollectorReconcile(t *testing.T) {
	tests := []struct {
		name  string
		steps []step
	}{
		{
			name: "no subscribers for the node",
			steps: []step{
				{name: "reconcile", do: noop},
				{name: "subscriber on another node", do: addSub("node-2", "sub-2")},
			},
		},
		{
			name: "lifecycle of a pod",
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{name: "reconcile without changes", do: noop},
				{name: "update", do: relabelPod("pod-1"), want: []wantEvent{{events.Update, []string{"sub-1"}}}},
				{name: "new subscriber", do: addSub("node-1", "sub-2"), want: []wantEvent{{events.Create, []string{"sub-2"}}}},
				{name: "subscriber leaves", do: delSub("node-1", "sub-1"), want: []wantEvent{{events.Delete, []string{"sub-1"}}}},
				{name: "delete", do: deleteObj(newPod("pod-1", "node-1")), want: []wantEvent{{events.Delete, []string{"sub-2"}}}},
				{name: "reconcile deleted pod again", do: noop},
			},
		},
		{
			name: "update and new subscriber at once",
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{
					name: "update and subscribe",
					do: func(t *testing.T, cl client.Client, subs *subscriber.Subscribers) {
						t.Helper()
						relabelPod("pod-1")(t, cl, subs)
						addSub("node-1", "sub-2")(t, cl, subs)
					},
					want: []wantEvent{{events.Create, []string{"sub-2"}}, {events.Update, []string{"sub-1"}}},
				},
			},
		},
		{
			// A pod has no subscribers left only when all the subscribers of its node disconnected,
			// so there is nobody to notify: the cache entry is dropped without events.
			name: "last subscriber leaves and comes back",
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{name: "unsubscribe", do: delSub("node-1", "sub-1")},
				{name: "subscribe again", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cl := newFakeClient(newNamespace(), newPod("pod-1", "node-1"))
			q := &recordingQueue{}
			pc := NewPodCollector(cl, q, events.NewCache(), "pod-collector")

			for _, s := range test.steps {
				s.do(t, cl, pc.subscribers)
				_, err := pc.Reconcile(context.Background(), reconcileRequest(testNamespace, "pod-1"))
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

func TestPodCollectorEventContent(t *testing.T) {
	t.Parallel()

	isController := true
	deployment := &appsv1.Deployment{Name: "nginx", Namespace: testNamespace, UID: "uid-deployment"}
	replicaSet := &appsv1.ReplicaSet{
		Name: "nginx-abc", Namespace: testNamespace, UID: "uid-replicaset",
		OwnerReferences: []metav1.OwnerReference{{Kind: resource.Deployment, Name: "nginx", UID: "uid-deployment", Controller: &isController}}}
	pod := newPod("nginx-abc-xyz", "node-1")
	pod.OwnerReferences = []metav1.OwnerReference{{Kind: resource.ReplicaSet, Name: "nginx-abc", UID: "uid-replicaset", Controller: &isController}}
	pod.ResourceVersion = ""
	pod.CreationTimestamp = metav1.NewTime(time.Now())
	svc := func(name string, selector map[string]string) *corev1.Service {
		return &corev1.Service{
			Name: name, Namespace: testNamespace, UID: types.UID("uid-svc-" + name),
			Spec: corev1.ServiceSpec{Selector: selector},
		}
	}

	cl := newFakeClient(newNamespace(), deployment, replicaSet, pod,
		svc("matching", map[string]string{"app": "nginx"}),
		svc("other-matching", map[string]string{"app": "nginx"}),
		svc("not-matching", map[string]string{"app": "other"}),
		// A service without selector must not select every pod.
		svc("no-selector", nil),
	)
	q := &recordingQueue{}
	nsChan := make(chan event.GenericEvent, 1)
	pc := NewPodCollector(cl, q, events.NewCache(), "pod-collector",
		WithOwnerSources(map[string]chan<- event.GenericEvent{resource.Namespace: nsChan}))
	pc.subscribers.AddSubscriberPerNode("node-1", "sub-1")

	_, err := pc.Reconcile(context.Background(), reconcileRequest(testNamespace, pod.Name))
	require.NoError(t, err)
	evts := q.take()
	require.Len(t, evts, 1)

	msg := evts[0].GRPCMessage()
	require.Equal(t, events.Create, msg.GetReason())
	require.Equal(t, resource.Pod, msg.GetKind())
	require.Equal(t, string(pod.UID), msg.GetUid())

	// Fields that change on every write or are sent as references are removed from the metadata.
	for _, key := range []string{"creationTimestamp", "ownerReferences", "resourceVersion"} {
		require.False(t, hasKey(msg.GetMeta(), key), "meta contains %q: %s", key, msg.GetMeta())
	}
	var meta metav1.ObjectMeta
	require.NoError(t, json.Unmarshal([]byte(msg.GetMeta()), &meta))
	require.Equal(t, pod.Name, meta.Name)
	require.Equal(t, pod.Labels, meta.Labels)
	require.JSONEq(t, `{"podIP":"10.0.0.1"}`, msg.GetStatus())

	requireRefs(t, evts[0], map[string][]string{
		resource.Namespace:  {"uid-ns-" + testNamespace},
		resource.ReplicaSet: {"uid-replicaset"},
		resource.Deployment: {"uid-deployment"},
		resource.Service:    {"uid-svc-matching", "uid-svc-other-matching"},
	})

	// A create event triggers the namespace collector, so the subscriber also receives the namespace.
	select {
	case evt := <-nsChan:
		require.Equal(t, testNamespace, evt.Object.GetName())
	case <-time.After(5 * time.Second):
		require.FailNow(t, "namespace collector not triggered")
	}
}

func TestPodCollectorReconcileErrors(t *testing.T) {
	tests := []struct {
		name string
		objs []client.Object
	}{
		{name: "namespace not found", objs: []client.Object{newPod("pod-1", "node-1")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			q := &recordingQueue{}
			pc := NewPodCollector(newFakeClient(test.objs...), q, events.NewCache(), "pod-collector")
			pc.subscribers.AddSubscriberPerNode("node-1", "sub-1")

			_, err := pc.Reconcile(context.Background(), reconcileRequest(testNamespace, "pod-1"))
			require.Error(t, err)
			require.Empty(t, q.take())
		})
	}
}
