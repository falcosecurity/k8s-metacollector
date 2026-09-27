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
		DeferCleanup(func(ctx SpecContext) {
			Expect(k8s.DeleteNamespaceContextE(GinkgoT(), ctx, opts, ns)).To(Succeed())
		})
	})

	// expectEvent waits until an event with the given reason, and metadata containing the given string, is received
	// for the resource. The whole history is checked: e.g. a pod gets updates while it starts, after its create event.
	expectEvent := func(ctx SpecContext, uid, reason, metaContains string) {
		Eventually(func() []string {
			var reasons []string
			for _, evt := range client.History(uid) {
				if evt.GetReason() == reason && strings.Contains(evt.GetMeta(), metaContains) {
					return nil
				}
				reasons = append(reasons, evt.GetReason())
			}
			if reasons == nil {
				reasons = []string{}
			}
			return reasons
		}).WithContext(ctx).WithTimeout(30*time.Second).Should(BeNil(),
			"no %s event with %q in the metadata for %s, received", reason, metaContains, uid)
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

		Expect(k8s.RunKubectlContextE(GinkgoT(), ctx, opts, "delete", "pod", "live", "--wait=true")).To(Succeed())
		expectEvent(ctx, podUID, events.Delete, "")
		// The namespace still exists, but no longer has pods on the node.
		expectEvent(ctx, nsUID, events.Delete, "")
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
