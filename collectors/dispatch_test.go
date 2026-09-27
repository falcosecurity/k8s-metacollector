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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

// startDispatch runs dispatch until the test ends and returns the channel it reads the subscribers from.
func startDispatch(t *testing.T, ctx context.Context, kind string, cl client.Client,
	dispatcherChan chan event.GenericEvent, subs *subscriber.Subscribers) (subChan subscriber.SubsChan, done <-chan error) {
	t.Helper()

	subChan = make(subscriber.SubsChan)
	errChan := make(chan error, 1)
	go func() {
		errChan <- dispatch(ctx, logr.Discard(), kind, subChan, dispatcherChan, cl, subs)
	}()
	return subChan, errChan
}

// dispatchedKeys returns the namespace/name of the requests dispatched so far.
func dispatchedKeys(ch chan event.GenericEvent) map[string]struct{} {
	keys := map[string]struct{}{}
	for {
		select {
		case evt := <-ch:
			keys[evt.Object.GetNamespace()+"/"+evt.Object.GetName()] = struct{}{}
		default:
			return keys
		}
	}
}

func TestDispatchOnNewSubscriber(t *testing.T) {
	isController := true
	owned := func(pod *corev1.Pod, kind, name string) *corev1.Pod {
		pod.OwnerReferences = []metav1.OwnerReference{{Kind: kind, Name: name, Controller: &isController}}
		return pod
	}
	objs := []client.Object{
		newNamespace(),
		&appsv1.ReplicaSet{
			Name: "nginx-abc", Namespace: testNamespace,
			OwnerReferences: []metav1.OwnerReference{{Kind: resource.Deployment, Name: "nginx", Controller: &isController}},
		},
		owned(newPod("from-rs", "node-1"), resource.ReplicaSet, "nginx-abc"),
		owned(newPod("from-ds", "node-1"), resource.Daemonset, "ds"),
		owned(newPod("from-rc", "node-1"), resource.ReplicationController, "rc"),
		// Pods on other nodes are not dispatched.
		owned(newPod("other-node", "node-2"), resource.Daemonset, "other-ds"),
		newService(map[string]string{"app": "nginx"}),
	}

	tests := []struct {
		kind string
		want []string
	}{
		{kind: resource.Pod, want: []string{"ns/from-rs", "ns/from-ds", "ns/from-rc"}},
		{kind: resource.Namespace, want: []string{"/ns"}},
		{kind: resource.ReplicaSet, want: []string{"ns/nginx-abc"}},
		{kind: resource.Deployment, want: []string{"ns/nginx"}},
		{kind: resource.Daemonset, want: []string{"ns/ds"}},
		{kind: resource.ReplicationController, want: []string{"ns/rc"}},
		{kind: resource.Service, want: []string{"ns/nginx"}},
	}

	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			t.Parallel()

			dispatcherChan := make(chan event.GenericEvent, 100)
			subs := subscriber.NewSubscribers()
			// The fake client modifies the objects, so each subtest gets its own copies.
			copies := make([]client.Object, 0, len(objs))
			for _, obj := range objs {
				copies = append(copies, copyObj(t, obj))
			}
			subChan, _ := startDispatch(t, t.Context(), test.kind, newFakeClient(copies...), dispatcherChan, subs)

			subChan <- subscriber.Message{NodeName: "node-1", UID: "sub-1", Reason: subscriber.Subscribed}
			// The channel is unbuffered: once this message is received, the previous one has been fully handled.
			subChan <- subscriber.Message{NodeName: "node-3", UID: "sub-3", Reason: subscriber.Unsubscribed}

			require.True(t, subs.HasNode("node-1"))
			want := map[string]struct{}{}
			for _, key := range test.want {
				want[key] = struct{}{}
			}
			require.Equal(t, want, dispatchedKeys(dispatcherChan))
		})
	}
}

func TestDispatchUnsubscribe(t *testing.T) {
	t.Parallel()

	subs := subscriber.NewSubscribers()
	subChan, _ := startDispatch(t, t.Context(), resource.Pod, newFakeClient(), make(chan event.GenericEvent, 1), subs)

	subChan <- subscriber.Message{NodeName: "node-1", UID: "sub-1", Reason: subscriber.Subscribed}
	subChan <- subscriber.Message{NodeName: "node-1", UID: "sub-1", Reason: subscriber.Unsubscribed}
	// Wait for the previous message to be handled.
	subChan <- subscriber.Message{NodeName: "node-3", UID: "sub-3", Reason: subscriber.Unsubscribed}

	require.False(t, subs.HasNode("node-1"))
}

func TestDispatchSkipsSubscriberWhenListingPodsFails(t *testing.T) {
	t.Parallel()

	// Without the node index, listing the pods of a node fails.
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(newNamespace(), newPod("pod-1", "node-1")).Build()
	dispatcherChan := make(chan event.GenericEvent, 10)
	subChan, _ := startDispatch(t, t.Context(), resource.Pod, cl, dispatcherChan, subscriber.NewSubscribers())

	subChan <- subscriber.Message{NodeName: "node-1", UID: "sub-1", Reason: subscriber.Subscribed}
	subChan <- subscriber.Message{NodeName: "node-3", UID: "sub-3", Reason: subscriber.Unsubscribed}

	require.Empty(t, dispatchedKeys(dispatcherChan))
}

func TestDispatchStopsWhenControllerNoLongerReads(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Unbuffered and never read, as when the controller has already stopped.
	dispatcherChan := make(chan event.GenericEvent)
	subChan, done := startDispatch(t, ctx, resource.Pod, newFakeClient(newNamespace(), newPod("pod-1", "node-1")),
		dispatcherChan, subscriber.NewSubscribers())

	subChan <- subscriber.Message{NodeName: "node-1", UID: "sub-1", Reason: subscriber.Subscribed}
	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "dispatch did not stop")
	}
}
