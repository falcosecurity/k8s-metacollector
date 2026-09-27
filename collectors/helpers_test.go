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
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
)

// testNamespace is the namespace of the resources used by the tests.
const testNamespace = "ns"

// recordingQueue is a broker.Queue that records the pushed events.
type recordingQueue struct {
	mu   sync.Mutex
	evts []events.Interface
}

func (q *recordingQueue) Push(evt events.Interface) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.evts = append(q.evts, evt)
}

func (q *recordingQueue) Pop(context.Context) events.Interface { return nil }

// take returns the recorded events and resets the queue.
func (q *recordingQueue) take() []events.Interface {
	q.mu.Lock()
	defer q.mu.Unlock()
	evts := q.evts
	q.evts = nil
	return evts
}

// wantEvent is an event expected to be pushed to the queue.
type wantEvent struct {
	reason string
	subs   []string
}

// summarize returns the reason and the sorted subscribers of each event.
func summarize(evts []events.Interface) []wantEvent {
	got := make([]wantEvent, 0, len(evts))
	for _, evt := range evts {
		subs := make([]string, 0, len(evt.Subscribers()))
		for sub := range evt.Subscribers() {
			subs = append(subs, sub)
		}
		sort.Strings(subs)
		got = append(got, wantEvent{reason: evt.Type(), subs: subs})
	}
	return got
}

// newFakeClient returns a fake client indexing pods by generate name and node, as the manager does.
func newFakeClient(objs ...client.Object) client.WithWatch {
	return fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(objs...).
		WithIndex(&corev1.Pod{}, podPrefixName, podByPrefixName).
		WithIndex(&corev1.Pod{}, nodeNameIndex, podByNode).
		Build()
}

// copyObj returns a deep copy of the object, so the fixtures are not modified by the fake client.
func copyObj(t *testing.T, obj client.Object) client.Object {
	t.Helper()

	cp, ok := obj.DeepCopyObject().(client.Object)
	require.True(t, ok)
	return cp
}

func reconcileRequest(namespace, name string) ctrl.Request {
	return ctrl.Request{Namespace: namespace, Name: name}
}

func newNamespace() *corev1.Namespace {
	return &corev1.Namespace{Name: testNamespace, UID: types.UID("uid-ns-" + testNamespace)}
}

// requireRefs checks that the gRPC references of an event contain exactly the given UIDs for each kind.
func requireRefs(t *testing.T, evt events.Interface, want map[string][]string) {
	t.Helper()

	got := map[string][]string{}
	for kind, list := range evt.GRPCMessage().GetRefs().GetResources() {
		uids := slices.Clone(list.GetList())
		sort.Strings(uids)
		got[kind] = uids
	}
	for kind := range want {
		sort.Strings(want[kind])
	}
	require.Equal(t, want, got)
}

// hasKey reports whether the JSON string contains the given object key.
func hasKey(jsonStr, key string) bool {
	return strings.Contains(jsonStr, `"`+key+`":`)
}
