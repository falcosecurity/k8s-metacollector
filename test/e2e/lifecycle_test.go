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
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Lifecycle of metacollector", func() {
	var (
		brokerAddr  = ":45001"
		metricsAddr = ":8085"
		probeAddr   = ":8090"
		session     *gexec.Session
		err         error
	)

	JustBeforeEach(func(ctx context.Context) {
		cmd := exec.Command(k8sMetaCollectorBin, "run", "--metrics-bind-address",
			metricsAddr, "--health-probe-bind-address", probeAddr, "--broker-bind-address", brokerAddr)
		session, err = gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
		Expect(err).NotTo(HaveOccurred())
		Eventually(ctx, session.Err).Should(gbytes.Say("starting grpc server"))
	}, NodeTimeout(time.Second*30))

	JustAfterEach(func(ctx SpecContext) {
		session.Terminate()
		Eventually(ctx, session).Should(gexec.Exit(0))
	}, NodeTimeout(time.Second*31))

	Describe("Run metacollector, and then stop it", func() {
		It("Should be ready, and then exit in after each", func(ctx SpecContext) {
			Eventually(func() int {
				resp, err := http.Get("http://localhost:8090/readyz")
				if err != nil {
					return 0
				}

				return resp.StatusCode
			}).WithContext(ctx).MustPassRepeatedly(3).Should(Equal(200))
		}, SpecTimeout(time.Second*5))
	})

	Describe("Subscribe and unsubscribe same client multiple times", func() {
		It("Collector should accept connections", func(ctx SpecContext) {
			Eventually(func() int {
				resp, err := http.Get("http://localhost:8090/readyz")
				if err != nil {
					return 0
				}

				return resp.StatusCode
			}).WithContext(ctx).MustPassRepeatedly(3).Should(Equal(200))

			numConnections := 5
			for numConnections > 0 {
				numConnections--
				clientCmd := exec.Command(clientBin, "--node-name", nodeName, "--no-output=true", "--addr", brokerAddr)
				sessionClient, err := gexec.Start(clientCmd, GinkgoWriter, GinkgoWriter)
				Expect(err).NotTo(HaveOccurred())
				Eventually(ctx, session.Err).Should(gbytes.Say(fmt.Sprintf("\"broker.grpc-server\",\"msg\":\"received watch request\",\"node\":%q", nodeName)))
				sessionClient.Kill()
				Eventually(ctx, session.Err).Should(gbytes.Say(fmt.Sprintf("\"broker.grpc-server\",\"msg\":\"stream deleted\",\"subscriber\":%q", nodeName)))
			}
		}, SpecTimeout(time.Second*10))
	})

	Describe("Subscribe 100 clients and exit", func() {
		It("Collector should accept connections and then exit when terminated", func(ctx SpecContext) {
			clientBuffer := gbytes.NewBuffer()

			Eventually(func() int {
				resp, err := http.Get("http://localhost:8090/readyz")
				if err != nil {
					return 0
				}

				return resp.StatusCode
			}).WithContext(ctx).MustPassRepeatedly(3).Should(Equal(200))

			// Spawn 100 clients.
			clientCmd := exec.Command(clientBin, "--node-name", nodeName, "--no-output=true", "--num-clients=100", "--addr", brokerAddr)
			_, err = gexec.Start(clientCmd, clientBuffer, clientBuffer)
			Expect(err).NotTo(HaveOccurred())

			// Wait for all clients to connect.
			Eventually(ctx, func(g Gomega) bool {
				resp, err := http.Get("http://localhost:8085/metrics")
				g.Expect(err).NotTo(HaveOccurred())
				defer func() {
					_ = resp.Body.Close()
				}()

				body, err := io.ReadAll(resp.Body)

				g.Expect(err).NotTo(HaveOccurred())

				return strings.Contains(string(body), "meta_collector_server_subscribers 100")
			}).WithContext(ctx).Should(BeTrue())

			// Give time to informers to sync, otherwise strange errors occur.
			time.Sleep(time.Millisecond * 500)

		}, SpecTimeout(time.Second*15))
	})
})
