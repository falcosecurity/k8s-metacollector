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

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
)

// ownerKinds is the set of owner kinds run.go wires into the owner sources map.
var ownerKinds = []string{
	resource.Deployment,
	resource.ReplicaSet,
	resource.Namespace,
	resource.Daemonset,
	resource.ReplicationController,
}

func newOwnerSources() (map[string]chan<- event.GenericEvent, map[string]chan event.GenericEvent) {
	sources := map[string]chan<- event.GenericEvent{}
	channels := map[string]chan event.GenericEvent{}
	for _, kind := range ownerKinds {
		ch := make(chan event.GenericEvent, 4)
		channels[kind] = ch
		sources[kind] = ch
	}
	return sources, channels
}

// drainOwnerSources counts the events that landed on each owner channel. The
// triggers are sent from their own goroutines, so it waits for them first.
func drainOwnerSources(channels map[string]chan event.GenericEvent) map[string]int {
	time.Sleep(200 * time.Millisecond)

	counts := map[string]int{}
	for kind, ch := range channels {
		for {
			select {
			case <-ch:
				counts[kind]++
				continue
			default:
			}
			break
		}
	}
	return counts
}

// reconcilePodDeletion reconciles a pod owned by owner, deletes it, then
// reconciles again, and reports which owner channels were triggered by the
// deletion.
func reconcilePodDeletion(t *testing.T, owner metav1.OwnerReference, ownerObj client.Object) map[string]int {
	t.Helper()

	isController := true
	owner.Controller = &isController

	pod := newPod("workload-xyz", "node-1")
	pod.OwnerReferences = []metav1.OwnerReference{owner}

	cl := newFakeClient(newNamespace(), pod, ownerObj)
	sources, channels := newOwnerSources()
	queue := &recordingQueue{}

	pc := NewPodCollector(cl, queue, events.NewCache(), "pod-collector", WithOwnerSources(sources))
	pc.subscribers.AddSubscriberPerNode("node-1", "sub-1")

	ctx := context.Background()
	req := reconcileRequest(testNamespace, pod.Name)

	_, err := pc.Reconcile(ctx, req)
	require.NoError(t, err)
	created := queue.take()
	require.Len(t, created, 1)
	require.Equal(t, "Create", created[0].Type())
	drainOwnerSources(channels)

	require.NoError(t, cl.Delete(ctx, pod))

	_, err = pc.Reconcile(ctx, req)
	require.NoError(t, err)
	deleted := queue.take()
	require.Len(t, deleted, 1)
	require.Equal(t, "Delete", deleted[0].Type())

	return drainOwnerSources(channels)
}

func TestPodCollectorTriggersOwnersOnDelete(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		ownerObj client.Object
		// triggered is false for an owner kind that has no channel in the
		// owner sources map, where nothing may fire.
		triggered bool
	}{
		{
			name:      "replica set owner",
			kind:      resource.ReplicaSet,
			ownerObj:  &appsv1.ReplicaSet{Name: "workload", Namespace: testNamespace, UID: "uid-rs"},
			triggered: true,
		},
		{
			name:      "daemon set owner",
			kind:      resource.Daemonset,
			ownerObj:  &appsv1.DaemonSet{Name: "workload", Namespace: testNamespace, UID: "uid-ds"},
			triggered: true,
		},
		{
			name:      "replication controller owner",
			kind:      resource.ReplicationController,
			ownerObj:  &corev1.ReplicationController{Name: "workload", Namespace: testNamespace, UID: "uid-rc"},
			triggered: true,
		},
		{
			name:      "stateful set owner",
			kind:      "StatefulSet",
			ownerObj:  &appsv1.StatefulSet{Name: "workload", Namespace: testNamespace, UID: "uid-sts"},
			triggered: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			counts := reconcilePodDeletion(t,
				metav1.OwnerReference{Kind: tt.kind, Name: "workload", UID: tt.ownerObj.GetUID()},
				tt.ownerObj)

			require.Positive(t, counts[resource.Namespace], "namespace owner should always be triggered")
			if tt.triggered {
				require.Positive(t, counts[tt.kind], "owner kind %q was not triggered", tt.kind)
			} else {
				require.Zero(t, counts[tt.kind], "owner kind %q has no owner source", tt.kind)
			}
		})
	}
}
