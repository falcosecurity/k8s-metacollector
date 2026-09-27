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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

// newDeploymentPod returns a pod created by the "nginx" deployment, named as the deployment controller does.
func newDeploymentPod(name, node string) *corev1.Pod {
	pod := newPod(name, node)
	pod.GenerateName = "nginx-7d9f-"
	pod.Labels["pod-template-hash"] = "7d9f"
	return pod
}

func createObj(obj client.Object) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(t *testing.T, cl client.Client, _ *subscriber.Subscribers) {
		t.Helper()
		require.NoError(t, cl.Create(context.Background(), copyObj(t, obj)))
	}
}

func relabelDeployment(t *testing.T, cl client.Client, _ *subscriber.Subscribers) {
	t.Helper()
	dpl := &appsv1.Deployment{}
	require.NoError(t, cl.Get(context.Background(), types.NamespacedName{Namespace: testNamespace, Name: "nginx"}, dpl))
	dpl.Labels = map[string]string{"version": "v2"}
	require.NoError(t, cl.Update(context.Background(), dpl))
}

func steps(fns ...func(*testing.T, client.Client, *subscriber.Subscribers)) func(*testing.T, client.Client, *subscriber.Subscribers) {
	return func(t *testing.T, cl client.Client, subs *subscriber.Subscribers) {
		t.Helper()
		for _, fn := range fns {
			fn(t, cl, subs)
		}
	}
}

func TestObjectMetaCollectorReconcileDeployment(t *testing.T) {
	deployment := &appsv1.Deployment{Name: "nginx", Namespace: testNamespace, UID: "uid-deployment"}

	tests := []struct {
		name  string
		pods  []client.Object
		steps []step
	}{
		{
			name: "no pods",
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1")},
			},
		},
		{
			name: "lifecycle of a deployment",
			pods: []client.Object{newDeploymentPod("nginx-7d9f-a", "node-1")},
			steps: []step{
				{name: "subscriber on another node", do: addSub("node-2", "sub-2")},
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{name: "reconcile without changes", do: noop},
				{name: "update", do: relabelDeployment, want: []wantEvent{{events.Update, []string{"sub-1"}}}},
				{name: "delete", do: deleteObj(deployment), want: []wantEvent{{events.Delete, []string{"sub-1"}}}},
				{name: "reconcile deleted deployment again", do: noop},
			},
		},
		{
			name: "pods on several nodes",
			pods: []client.Object{newDeploymentPod("nginx-7d9f-a", "node-1"), newDeploymentPod("nginx-7d9f-b", "node-2")},
			steps: []step{
				{
					name: "subscribe",
					do:   steps(addSub("node-1", "sub-1"), addSub("node-2", "sub-2")),
					want: []wantEvent{{events.Create, []string{"sub-1", "sub-2"}}},
				},
				{
					name: "pod leaves a node with other pods left",
					do:   deleteObj(newDeploymentPod("nginx-7d9f-b", "node-2")),
					want: []wantEvent{{events.Delete, []string{"sub-2"}}},
				},
			},
		},
		{
			name: "pod moves to another node",
			pods: []client.Object{newDeploymentPod("nginx-7d9f-a", "node-1")},
			steps: []step{
				{
					name: "subscribe",
					do:   steps(addSub("node-1", "sub-1"), addSub("node-2", "sub-2")),
					want: []wantEvent{{events.Create, []string{"sub-1"}}},
				},
				{
					name: "pod replaced on another node",
					do:   steps(deleteObj(newDeploymentPod("nginx-7d9f-a", "node-1")), createObj(newDeploymentPod("nginx-7d9f-b", "node-2"))),
					want: []wantEvent{{events.Create, []string{"sub-2"}}, {events.Delete, []string{"sub-1"}}},
				},
			},
		},
		{
			// The subscriber is still connected: it must be told that the deployment no longer runs on its node.
			name: "last pod leaves the node",
			pods: []client.Object{newDeploymentPod("nginx-7d9f-a", "node-1")},
			steps: []step{
				{name: "subscribe", do: addSub("node-1", "sub-1"), want: []wantEvent{{events.Create, []string{"sub-1"}}}},
				{name: "scale to zero", do: deleteObj(newDeploymentPod("nginx-7d9f-a", "node-1")), want: []wantEvent{{events.Delete, []string{"sub-1"}}}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cl := newFakeClient(append([]client.Object{newNamespace(), deployment.DeepCopy()}, test.pods...)...)
			q := &recordingQueue{}
			// Same pod matching as the deployment collector created by the run command.
			c := NewObjectMetaCollector(cl, q, events.NewCache(), NewPartialObjectMetadata(resource.Deployment, nil), "deployment-collector",
				WithPodMatchingFields(func(meta *metav1.ObjectMeta) client.ListOption {
					return &client.MatchingFields{podPrefixName: meta.Name}
				}))

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

func TestObjectMetaCollectorEventContent(t *testing.T) {
	tests := []struct {
		name string
		kind string
		obj  client.Object
		req  types.NamespacedName
	}{
		{
			name: "deployment",
			kind: resource.Deployment,
			obj:  &appsv1.Deployment{Name: "nginx", Namespace: testNamespace, UID: "uid-obj", ResourceVersion: "7"},
			req:  types.NamespacedName{Namespace: testNamespace, Name: "nginx"},
		},
		{
			name: "namespace",
			kind: resource.Namespace,
			obj:  &corev1.Namespace{Name: testNamespace, UID: "uid-obj", ResourceVersion: "7"},
			req:  types.NamespacedName{Name: testNamespace},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			objs := []client.Object{test.obj, newDeploymentPod("nginx-7d9f-a", "node-1")}
			if test.kind != resource.Namespace {
				objs = append(objs, newNamespace())
			}
			q := &recordingQueue{}
			// The default pod matching selects all the pods in the namespace of the resource.
			c := NewObjectMetaCollector(newFakeClient(objs...), q, events.NewCache(), NewPartialObjectMetadata(test.kind, nil), "collector")
			c.subscribers.AddSubscriberPerNode("node-1", "sub-1")

			_, err := c.Reconcile(context.Background(), reconcileRequest(test.req.Namespace, test.req.Name))
			require.NoError(t, err)
			evts := q.take()
			require.Len(t, evts, 1)

			msg := evts[0].GRPCMessage()
			require.Equal(t, events.Create, msg.GetReason())
			require.Equal(t, test.kind, msg.GetKind())
			require.Equal(t, "uid-obj", msg.GetUid())
			require.True(t, hasKey(msg.GetMeta(), "name"), msg.GetMeta())
			require.False(t, hasKey(msg.GetMeta(), "resourceVersion"), msg.GetMeta())
			require.Empty(t, msg.GetStatus())
			require.Empty(t, msg.GetRefs().GetResources())
		})
	}
}
