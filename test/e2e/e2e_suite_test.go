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
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/falcosecurity/k8s-metacollector/test/e2e"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gexec"
)

var (
	k8sMetaCollectorBin string
	clientBin           string
	nodeName            string
	// collectorMainSession is the global running collector.
	collectorMainSession *gexec.Session
	deployer             e2e.Deployer
	mainNamespace        = "nginx-test"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2e Suite")
}

var _ = BeforeSuite(func(ctx context.Context) {
	var err error
	var ok bool
	// First check that configuration variables are set.
	nodeName, ok = os.LookupEnv(e2e.NodeNameEnv)
	Expect(ok).To(BeTrue())

	// Build the meta collector.
	k8sMetaCollectorBin, err = gexec.Build("../../main.go")
	Expect(err).NotTo(HaveOccurred())

	// Build the clint.
	clientBin, err = gexec.Build("../client/main.go")
	Expect(err).NotTo(HaveOccurred())

	DeferCleanup(gexec.CleanupBuildArtifacts)

	// Start the collector.
	cmd := exec.Command(k8sMetaCollectorBin, "run") //nolint:gosec //Path is not predictable.
	collectorMainSession, err = gexec.Start(cmd, GinkgoWriter, GinkgoWriter)
	Expect(err).NotTo(HaveOccurred())
	// Wait for the collector to be ready.
	Eventually(func() int {
		resp, err := http.Get("http://localhost:8081/readyz")
		if err != nil {
			return 0
		}

		return resp.StatusCode
	}).WithContext(ctx).MustPassRepeatedly(3).Should(Equal(200))

	// Create the deployer
	deployer = e2e.NewDeployer("./resources/")
	// Deploy all resources.
	Expect(deployer.DeployAll(GinkgoT(), GinkgoWriter, mainNamespace, 6)).NotTo(HaveOccurred())
}, NodeTimeout(time.Minute*1))

var _ = AfterSuite(func(ctx context.Context) {
	// Remove all deployed resources.
	Expect(deployer.CleanUp(GinkgoT(), GinkgoWriter)).NotTo(HaveOccurred())
	// Stop the collector.
	collectorMainSession.Terminate()
	Eventually(ctx, collectorMainSession).Should(gexec.Exit(0))
}, NodeTimeout(time.Minute*3))
