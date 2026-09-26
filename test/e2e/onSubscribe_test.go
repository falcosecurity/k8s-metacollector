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

package e2e_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/test/e2e"
)

// expectedObject is a resource for which the subscriber is expected to receive an event.
type expectedObject struct {
	meta *metav1.ObjectMeta
	// podStatus is set only for pods, the only resource whose status is sent to subscribers.
	podStatus *corev1.PodStatus
}

// lister returns the resources related to the given node.
type lister func(ctx context.Context, node string) ([]expectedObject, error)

// toExpected converts a list of resources to the objects expected by the subscriber.
func toExpected[T any](items []T, err error, meta func(*T) *metav1.ObjectMeta) ([]expectedObject, error) {
	if err != nil {
		return nil, err
	}
	objs := make([]expectedObject, 0, len(items))
	for i := range items {
		objs = append(objs, expectedObject{meta: meta(&items[i])})
	}
	return objs, nil
}

var (
	listPods lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		pods, err := deployer.ListPods(ctx, GinkgoT(), GinkgoWriter, node)
		if err != nil {
			return nil, err
		}
		objs := make([]expectedObject, 0, len(pods))
		for i := range pods {
			objs = append(objs, expectedObject{meta: &pods[i].ObjectMeta, podStatus: &pods[i].Status})
		}
		return objs, nil
	}
	listDeployments lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		items, err := deployer.ListDeployments(ctx, GinkgoT(), GinkgoWriter, node)
		return toExpected(items, err, func(o *appsv1.Deployment) *metav1.ObjectMeta { return &o.ObjectMeta })
	}
	listReplicaSets lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		items, err := deployer.ListReplicaSets(ctx, GinkgoT(), GinkgoWriter, node)
		return toExpected(items, err, func(o *appsv1.ReplicaSet) *metav1.ObjectMeta { return &o.ObjectMeta })
	}
	listReplicationControllers lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		items, err := deployer.ListReplicationControllers(ctx, GinkgoT(), GinkgoWriter, node)
		return toExpected(items, err, func(o *corev1.ReplicationController) *metav1.ObjectMeta { return &o.ObjectMeta })
	}
	listDaemonSets lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		items, err := deployer.ListDaemonsets(ctx, GinkgoT(), GinkgoWriter, node)
		return toExpected(items, err, func(o *appsv1.DaemonSet) *metav1.ObjectMeta { return &o.ObjectMeta })
	}
	listServices lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		items, err := deployer.ListServices(ctx, GinkgoT(), GinkgoWriter, node)
		return toExpected(items, err, func(o *corev1.Service) *metav1.ObjectMeta { return &o.ObjectMeta })
	}
	listNamespaces lister = func(ctx context.Context, node string) ([]expectedObject, error) {
		items, err := deployer.ListNamespaces(ctx, GinkgoT(), GinkgoWriter, node)
		return toExpected(items, err, func(o *corev1.Namespace) *metav1.ObjectMeta { return &o.ObjectMeta })
	}
)

// subscribe creates a client for the given node and starts watching. The subscription lives until the
// end of the spec: it must not use the context of the setup node, which Ginkgo cancels when the node returns.
func subscribe(node string) *e2e.Client {
	client, err := e2e.NewClient(node, "45000")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(client.Close)

	ctx, cancel := context.WithCancel(context.Background())
	DeferCleanup(cancel)
	Expect(client.Watch(ctx)).To(Succeed())

	return &client
}

var _ = Describe("Clients on subscribe", func() {
	var client *e2e.Client

	Describe("Subscribe a new client", func() {
		BeforeEach(func() {
			client = subscribe(nodeName)
		})

		DescribeTable("Should get a create event for each resource related to the node",
			func(ctx SpecContext, kind string, list lister) {
				objs, err := list(ctx, nodeName)
				Expect(err).NotTo(HaveOccurred())
				Expect(objs).NotTo(BeEmpty(), "no %s found for node %q", kind, nodeName)

				for _, obj := range objs {
					wantMeta, err := e2e.MetaToString(obj.meta.DeepCopy())
					Expect(err).NotTo(HaveOccurred())
					var wantStatus string
					if obj.podStatus != nil {
						wantStatus, err = e2e.PodStatusToString(obj.podStatus)
						Expect(err).NotTo(HaveOccurred())
					}

					Eventually(func(g Gomega) {
						evt, ok := client.Get(string(obj.meta.UID))
						g.Expect(ok).To(BeTrue(), "no event received for %s %s/%s", kind, obj.meta.Namespace, obj.meta.Name)
						g.Expect(evt.GetReason()).To(Equal(events.Create))
						g.Expect(evt.GetKind()).To(Equal(kind))
						g.Expect(evt.GetMeta()).To(Equal(wantMeta))
						if obj.podStatus != nil {
							g.Expect(evt.GetStatus()).To(Equal(wantStatus))
						}
					}).WithContext(ctx).Should(Succeed())
				}

				// No events for resources unrelated to the node.
				expected := make(map[string]struct{}, len(objs))
				for _, obj := range objs {
					expected[string(obj.meta.UID)] = struct{}{}
				}
				Consistently(func() []string {
					var unexpected []string
					for _, evt := range client.EventsForKind(kind) {
						if _, ok := expected[evt.GetUid()]; !ok {
							unexpected = append(unexpected, evt.GetReason()+" "+evt.GetMeta())
						}
					}
					return unexpected
				}, 3*time.Second, time.Second).WithContext(ctx).Should(BeEmpty(), "events for %s not related to node %q", kind, nodeName)
			},
			Entry("pods", resource.Pod, listPods, SpecTimeout(2*time.Minute)),
			Entry("deployments", resource.Deployment, listDeployments, SpecTimeout(2*time.Minute)),
			Entry("replicasets", resource.ReplicaSet, listReplicaSets, SpecTimeout(2*time.Minute)),
			Entry("replication controllers", resource.ReplicationController, listReplicationControllers, SpecTimeout(2*time.Minute)),
			Entry("daemonsets", resource.Daemonset, listDaemonSets, SpecTimeout(2*time.Minute)),
			Entry("services", resource.Service, listServices, SpecTimeout(2*time.Minute)),
			Entry("namespaces", resource.Namespace, listNamespaces, SpecTimeout(2*time.Minute)),
		)
	})

	Describe("Subscribe a new client for non existing node", func() {
		BeforeEach(func() {
			client = subscribe("no-node-exists")
		})

		It("Should not receive events at all", func(ctx SpecContext) {
			Consistently(func() int {
				return client.NumMessages()
			}, 5*time.Second, time.Second).WithContext(ctx).Should(BeZero())
		}, SpecTimeout(time.Minute))
	})
})
