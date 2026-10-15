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
	"fmt"
	"strings"
	"time"

	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/falcosecurity/k8s-metacollector/test/e2e"
)

// livePod is a pod pinned to the subscribed node, so it does not depend on the scheduler.
const livePod = `
apiVersion: v1
kind: Pod
metadata:
  name: live
  labels:
    app: live
spec:
  nodeName: %s
  containers:
    - name: nginx
      image: nginx:1.14.2
`

// gatedOwner is an owner whose pod is created behind a scheduling gate, so the test decides when the pod lands on the
// node, and never becomes ready, so the owner status does not change once the pod is scheduled. The arguments are the
// apiVersion, the kind, the spec fields before the template, and the node name. Scheduling gates need Kubernetes 1.27
// or newer: on older clusters the gate is dropped and the pod lands on the node right away.
const gatedOwner = `
apiVersion: %s
kind: %s
metadata:
  name: owner
spec:
%s
  template:
    metadata:
      labels:
        app: owner
    spec:
      schedulingGates:
        - name: e2e/gated
      terminationGracePeriodSeconds: 0
      nodeSelector:
        kubernetes.io/hostname: %s
      tolerations:
        - operator: Exists
      containers:
        - name: nginx
          image: nginx:1.14.2
          readinessProbe:
            exec:
              command: ["false"]
            periodSeconds: 1
`

const (
	appsOwnerSelector = "  selector:\n    matchLabels:\n      app: owner"
	singleReplica     = "  replicas: 1\n"
)

const liveService = `
apiVersion: v1
kind: Service
metadata:
  name: live
spec:
  selector:
    app: live
  ports:
    - port: 80
`

var _ = Describe("Clients on live changes", func() {
	var (
		client *e2e.Client
		opts   *k8s.KubectlOptions
	)

	BeforeEach(func(ctx SpecContext) {
		client = subscribe(nodeName)

		// Each spec uses its own namespace, to not interfere with the resources checked by the other specs.
		ns := "e2e-live-" + rand.String(5)
		opts = k8s.NewKubectlOptions("", "", ns)
		opts.Logger = logger.New(e2e.NewLogger(GinkgoWriter))
		Expect(k8s.CreateNamespaceContextE(GinkgoT(), ctx, opts, ns)).To(Succeed())
		// Wait for the namespace to be gone: the other specs list the pods on the node, terminating ones included.
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "delete", "namespace", ns, "--wait=true", "--timeout=2m")).To(Succeed())
		})
	})

	// expectEventSince waits until an event with the given reason, and metadata containing the given string, is
	// received for the resource, among the events received after the first since ones.
	expectEventSince := func(ctx SpecContext, uid string, since int, reason, metaContains string) {
		Eventually(func() []string {
			reasons := []string{}
			for _, evt := range client.History(uid)[since:] {
				if evt.GetReason() == reason && strings.Contains(evt.GetMeta(), metaContains) {
					return nil
				}
				reasons = append(reasons, evt.GetReason())
			}
			return reasons
		}).WithContext(ctx).WithTimeout(30*time.Second).Should(BeNil(),
			"no %s event with %q in the metadata for %s, received", reason, metaContains, uid)
	}

	// expectEvent checks the whole history: e.g. a pod gets updates while it starts, after its create event.
	expectEvent := func(ctx SpecContext, uid, reason, metaContains string) {
		expectEventSince(ctx, uid, 0, reason, metaContains)
	}

	kubectl := func(ctx SpecContext, args ...string) string {
		out, err := k8s.RunKubectlAndGetOutputContextE(GinkgoT(), ctx, opts, args...)
		Expect(err).NotTo(HaveOccurred())
		return out
	}

	createLivePod := func(ctx SpecContext) string {
		Expect(k8s.KubectlApplyFromStringContextE(GinkgoT(), ctx, opts, fmt.Sprintf(livePod, nodeName))).To(Succeed())
		Expect(k8s.WaitUntilPodAvailableContextE(GinkgoT(), ctx, opts, "live", 30, 2*time.Second)).To(Succeed())
		pod, err := k8s.GetPodContextE(GinkgoT(), ctx, opts, "live")
		Expect(err).NotTo(HaveOccurred())
		return string(pod.UID)
	}

	It("Should send create, update and delete events for a pod and its namespace", func(ctx SpecContext) {
		podUID := createLivePod(ctx)
		ns, err := k8s.GetNamespaceContextE(GinkgoT(), ctx, opts, opts.Namespace)
		Expect(err).NotTo(HaveOccurred())
		nsUID := string(ns.UID)

		expectEvent(ctx, podUID, events.Create, "")
		expectEvent(ctx, nsUID, events.Create, "")

		Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "label", "pod", "live", "version=v2")).To(Succeed())
		expectEvent(ctx, podUID, events.Update, `"version":"v2"`)

		since := len(client.History(nsUID))
		Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "label", "namespace", opts.Namespace, "version=v2")).To(Succeed())
		expectEventSince(ctx, nsUID, since, events.Update, `"version":"v2"`)

		Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "delete", "pod", "live", "--wait=true")).To(Succeed())
		expectEvent(ctx, podUID, events.Delete, "")
		// The namespace still exists, but no longer has pods on the node.
		expectEvent(ctx, nsUID, events.Delete, "")
	}, SpecTimeout(2*time.Minute))

	// A namespace is removed only once its pods are gone, so its delete event comes from its last pod leaving the node.
	It("Should send delete events when the namespace is deleted", func(ctx SpecContext) {
		podUID := createLivePod(ctx)
		ns, err := k8s.GetNamespaceContextE(GinkgoT(), ctx, opts, opts.Namespace)
		Expect(err).NotTo(HaveOccurred())
		nsUID := string(ns.UID)
		expectEvent(ctx, nsUID, events.Create, "")

		Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "delete", "namespace", opts.Namespace, "--wait=true")).To(Succeed())
		expectEvent(ctx, podUID, events.Delete, "")
		expectEvent(ctx, nsUID, events.Delete, "")
		// Recreate it, for the cleanup.
		Expect(k8s.CreateNamespaceContextE(GinkgoT(), ctx, opts, opts.Namespace)).To(Succeed())
	}, SpecTimeout(2*time.Minute))

	DescribeTable("Should follow an owner through its pods on the node",
		func(ctx SpecContext, kind, apiVersion, spec string) {
			Expect(k8s.KubectlApplyFromStringContextE(GinkgoT(), ctx, opts,
				fmt.Sprintf(gatedOwner, apiVersion, kind, spec, nodeName))).To(Succeed())
			// Remove the pods before their owners: the other specs list the pods on the node and fetch their owners.
			DeferCleanup(func(ctx SpecContext) {
				kubectl(ctx, "delete", "deployments,replicasets,daemonsets,replicationcontrollers,pods", "--all",
					"--cascade=foreground", "--wait=true")
			})
			uid := kubectl(ctx, "get", kind, "owner", "-o", "jsonpath={.metadata.uid}")

			// gatedPod returns the pod of the owner waiting behind the scheduling gate.
			gatedPod := func() string {
				var name string
				Eventually(func() string {
					name = kubectl(ctx, "get", "pods", "-l", "app=owner", "-o", "jsonpath={.items[?(@.spec.schedulingGates)].metadata.name}")
					return name
				}).WithContext(ctx).WithTimeout(30 * time.Second).ShouldNot(BeEmpty())
				return name
			}
			ungate := func(pod string) {
				kubectl(ctx, "patch", "pod", pod, "--type=json", "-p", `[{"op":"remove","path":"/spec/schedulingGates"}]`)
			}

			pod := gatedPod()
			Expect(client.History(uid)).To(BeEmpty(), "the %s has no pods on the node yet", kind)

			By("scheduling its pod on the node")
			// Only the pod reaching the node can trigger the owner: its status stays the same, the pod is never ready.
			ungate(pod)
			expectEvent(ctx, uid, events.Create, "")

			By("changing its metadata")
			since := len(client.History(uid))
			kubectl(ctx, "label", kind, "owner", "version=v2")
			expectEventSince(ctx, uid, since, events.Update, `"version":"v2"`)

			By("removing its last pod from the node")
			// The replacement pod stays behind the gate, off the node.
			since = len(client.History(uid))
			kubectl(ctx, "delete", "pod", pod, "--wait=true")
			expectEventSince(ctx, uid, since, events.Delete, "")

			By("scheduling a pod on the node again")
			since = len(client.History(uid))
			ungate(gatedPod())
			expectEventSince(ctx, uid, since, events.Create, "")

			By("deleting it while its pod stays on the node")
			since = len(client.History(uid))
			kubectl(ctx, "delete", kind, "owner", "--cascade=orphan", "--wait=true")
			expectEventSince(ctx, uid, since, events.Delete, "")

			// Each step sends exactly one event: no duplicates from the owner being triggered by its pods as well.
			Consistently(func() []string {
				history := client.History(uid)
				reasons := make([]string, 0, len(history))
				for _, evt := range history {
					reasons = append(reasons, evt.GetReason())
				}
				return reasons
			}, 3*time.Second, time.Second).WithContext(ctx).Should(Equal(
				[]string{events.Create, events.Update, events.Delete, events.Create, events.Delete}))
		},
		Entry("replicaset", resource.ReplicaSet, "apps/v1", singleReplica+appsOwnerSelector, SpecTimeout(3*time.Minute)),
		Entry("deployment", resource.Deployment, "apps/v1", singleReplica+appsOwnerSelector, SpecTimeout(3*time.Minute)),
		Entry("daemonset", resource.Daemonset, "apps/v1", appsOwnerSelector, SpecTimeout(3*time.Minute)),
		Entry("replicationcontroller", resource.ReplicationController, "v1", singleReplica+"  selector:\n    app: owner",
			SpecTimeout(3*time.Minute)),
	)

	It("Should send create, update and delete events for a service selecting pods on the node", func(ctx SpecContext) {
		createLivePod(ctx)
		Expect(k8s.KubectlApplyFromStringContextE(GinkgoT(), ctx, opts, liveService)).To(Succeed())
		svcUID := kubectl(ctx, "get", "service", "live", "-o", "jsonpath={.metadata.uid}")
		expectEvent(ctx, svcUID, events.Create, "")

		since := len(client.History(svcUID))
		kubectl(ctx, "label", "service", "live", "version=v2")
		expectEventSince(ctx, svcUID, since, events.Update, `"version":"v2"`)

		since = len(client.History(svcUID))
		kubectl(ctx, "delete", "service", "live", "--wait=true")
		expectEventSince(ctx, svcUID, since, events.Delete, "")
	}, SpecTimeout(2*time.Minute))

	It("Should send a delete event when a service no longer selects pods on the node", func(ctx SpecContext) {
		createLivePod(ctx)
		Expect(k8s.KubectlApplyFromStringContextE(GinkgoT(), ctx, opts, liveService)).To(Succeed())
		svc, err := k8s.GetServiceContextE(GinkgoT(), ctx, opts, "live")
		Expect(err).NotTo(HaveOccurred())
		svcUID := string(svc.UID)

		expectEvent(ctx, svcUID, events.Create, "")

		Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "patch", "service", "live", "-p", `{"spec":{"selector":{"app":"none"}}}`)).To(Succeed())
		expectEvent(ctx, svcUID, events.Delete, "")
	}, SpecTimeout(2*time.Minute))
})
