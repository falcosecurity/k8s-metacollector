// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 The Falco Authors
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
	"fmt"
	"time"

	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/test/e2e"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Clients on subscribe", func() {
	var (
		client     e2e.Client
		brokerPort = "45000"
		cancel     context.CancelFunc
	)

	JustBeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		Expect(client.Watch(ctx)).NotTo(HaveOccurred())
	})

	Describe("Subscribe a new client", func() {
		BeforeEach(func() {
			var err error
			client, err = e2e.NewClient(nodeName, brokerPort)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			fmt.Println("stopping client")
			cancel()
		})
		It("Should get events for all pods running on node", func(ctx SpecContext) {
			pods, err := deployer.ListPods(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())
			// Check that for each retrieved pod we received a create event from the collector.
			for _, pod := range pods {
				pod := pod
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(pod.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&pod.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					statusString, err := e2e.PodStatusToString(&pod.Status)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Status).To(Equal(statusString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.Pod)
					return len(pods) == n

				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}

		}, SpecTimeout(time.Minute*2))

		It("Should get events for all deployments related to node", func(ctx SpecContext) {
			deployments, err := deployer.ListDeployments(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())

			for _, dpl := range deployments {
				dpl := dpl
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(dpl.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&dpl.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.Deployment)
					return len(deployments) == n
				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}
		}, SpecTimeout(time.Minute*2))

		It("Should get events for all replicasets related to node", func(ctx SpecContext) {
			replicasets, err := deployer.ListReplicaSets(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())

			for _, rs := range replicasets {
				rs := rs
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(rs.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&rs.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.ReplicaSet)
					return len(replicasets) == n
				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}
		}, SpecTimeout(time.Minute*2))

		It("Should get events for all replication controllers related to node", func(ctx SpecContext) {
			rcs, err := deployer.ListReplicationControllers(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())

			for _, rc := range rcs {
				rc := rc
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(rc.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&rc.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.ReplicationController)
					return len(rcs) == n
				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}
		}, SpecTimeout(time.Minute*2))

		It("Should get events for all daemonsets related to node", func(ctx SpecContext) {
			daemonsets, err := deployer.ListDaemonsets(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())

			for _, ds := range daemonsets {
				ds := ds
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(ds.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&ds.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.Daemonset)
					return len(daemonsets) == n
				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}
		}, SpecTimeout(time.Minute*2))

		It("Should get events for all services related to node", func(ctx SpecContext) {
			services, err := deployer.ListServices(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())

			for _, svc := range services {
				svc := svc
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(svc.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&svc.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.Service)
					return len(services) == n
				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}
		}, SpecTimeout(time.Minute*2))

		It("Should get events for all namespaces related to node", func(ctx SpecContext) {
			namespaces, err := deployer.ListNamespaces(GinkgoT(), GinkgoWriter, nodeName)
			Expect(err).NotTo(HaveOccurred())

			for _, ns := range namespaces {
				ns := ns
				Eventually(func(g Gomega) bool {
					evt, ok := client.Get(string(ns.UID))
					if !ok {
						return false
					}
					Expect(evt.Reason).To(Equal("Create"))
					metaString, err := e2e.MetaToString(&ns.ObjectMeta)
					Expect(err).NotTo(HaveOccurred())
					Expect(*evt.Meta).To(Equal(metaString))
					fmt.Println(evt.String())
					return true
				}).WithContext(ctx).Should(BeTrue())

				Consistently(func() bool {
					n := client.NumMessagesForKind(resource.Namespace)
					return len(namespaces) == n
				}, time.Second*3, time.Second).WithContext(ctx).MustPassRepeatedly(3)
			}
		}, SpecTimeout(time.Minute*2))
	})

	Describe("Subscribe a new client for non existing node", func() {
		BeforeEach(func() {
			var err error
			client, err = e2e.NewClient("no-node-exists", brokerPort)
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			cancel()
		})

		It("Should not receive events at all", func(ctx SpecContext) {
			Consistently(func() bool {
				n := client.NumMessages()
				return n == 0
			}, time.Second*5, time.Second).WithContext(ctx).MustPassRepeatedly(3)

		}, SpecTimeout(time.Minute))
	})
})
