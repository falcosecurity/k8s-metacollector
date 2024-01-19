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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/gruntwork-io/terratest/modules/logger"
	"github.com/gruntwork-io/terratest/modules/testing"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sApiErrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Deployer knows how to deploy resources.
type Deployer struct {
	resourcesPath string
	namespaces    map[string]struct{}
}

// NewDeployer returns a new deployer.
func NewDeployer(resourcePath string) Deployer {
	return Deployer{
		resourcesPath: resourcePath,
		namespaces:    make(map[string]struct{}),
	}
}

// DeployAll deploys all resources and waits until the expected pods are running.
func (dpl *Deployer) DeployAll(t testing.TestingT, writer io.Writer, namespace string, expectedPods int) error {
	log := logger.New(NewLogger(writer))
	// We use it to clean up at the end.
	dpl.namespaces[namespace] = struct{}{}
	//

	if err := k8s.CreateNamespaceE(t, &k8s.KubectlOptions{Logger: log}, namespace); err != nil {
		return err
	}

	// Deploy k8s resources in the cluster.
	kubectlOptions := k8s.NewKubectlOptions("", "", namespace)
	kubectlOptions.Logger = log
	if err := k8s.KubectlApplyE(t, kubectlOptions, fmt.Sprintf("%s/*", strings.TrimSuffix(dpl.resourcesPath, "/"))); err != nil {
		return err
	}

	return k8s.WaitUntilNumPodsCreatedE(t, kubectlOptions, v1.ListOptions{}, expectedPods, 10, time.Second*5)
}

// CleanUp removes all resources previously deployed.
func (dpl *Deployer) CleanUp(t testing.TestingT, writer io.Writer) error {
	for ns := range dpl.namespaces {
		if err := k8s.DeleteNamespaceE(t, &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}, ns); err != nil {
			return err
		}
	}

	for ns := range dpl.namespaces {
		var err error

		for !k8sApiErrors.IsNotFound(err) {
			time.Sleep(5 * time.Second)
			_, err = k8s.GetNamespaceE(t, &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}, ns)
		}

		err = nil
	}

	return nil
}

// ListPods returns all the pods running on the given node.
func (dpl *Deployer) ListPods(t testing.TestingT, writer io.Writer, node string) ([]corev1.Pod, error) {
	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	pods, err := k8s.ListPodsE(t, opt, v1.ListOptions{
		FieldSelector: NodeFieldSelector + node,
	})

	if err != nil {
		return nil, err
	}

	return pods, nil
}

// ListNamespaces returns all the namespaces related to the given node.
func (dpl *Deployer) ListNamespaces(t testing.TestingT, writer io.Writer, node string) ([]corev1.Namespace, error) {
	var namespaces []corev1.Namespace
	processed := map[string]struct{}{}

	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	pods, err := dpl.ListPods(t, writer, node)
	if err != nil {
		return nil, err
	}

	for i := range pods {
		if _, ok := processed[pods[i].Namespace]; !ok {
			ns, err := k8s.GetNamespaceE(t, opt, pods[i].Namespace)
			if err != nil {
				return nil, err
			}
			namespaces = append(namespaces, *ns)
			processed[ns.Name] = struct{}{}
		}
	}

	return namespaces, nil
}

// ListReplicaSets returns all the replicasets related to the given node.
func (dpl *Deployer) ListReplicaSets(t testing.TestingT, writer io.Writer, node string) ([]appsv1.ReplicaSet, error) {
	var replicasets []appsv1.ReplicaSet
	processed := map[string]struct{}{}

	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	pods, err := dpl.ListPods(t, writer, node)
	if err != nil {
		return nil, err
	}

	for i := range pods {
		// Get owner.
		owner := events.ManagingOwner(pods[i].OwnerReferences)
		if owner != nil && owner.Kind == "ReplicaSet" {
			opt.Namespace = pods[i].Namespace
			// Get the replica set.
			rs, err := k8s.GetReplicaSetE(t, opt, owner.Name)
			if err != nil {
				return nil, err
			}

			key := fmt.Sprintf("%s/%s", rs.Name, rs.Namespace)
			if _, ok := processed[key]; !ok {
				replicasets = append(replicasets, *rs)
				processed[key] = struct{}{}
			}
		}
	}

	return replicasets, nil
}

// ListDeployments returns all the deployments related to the given node.
func (dpl *Deployer) ListDeployments(t testing.TestingT, writer io.Writer, node string) ([]appsv1.Deployment, error) {
	var deployments []appsv1.Deployment
	processed := map[string]struct{}{}

	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	replicasets, err := dpl.ListReplicaSets(t, writer, node)
	if err != nil {
		return nil, err
	}

	for i := range replicasets {
		// Get owner.
		owner := events.ManagingOwner(replicasets[i].OwnerReferences)
		if owner != nil && owner.Kind == "Deployment" {
			opt.Namespace = replicasets[i].Namespace
			// Get the deployment.
			dpl, err := k8s.GetDeploymentE(t, opt, owner.Name)
			if err != nil {
				return nil, err
			}

			key := fmt.Sprintf("%s/%s", dpl.Name, dpl.Namespace)
			if _, ok := processed[key]; !ok {
				deployments = append(deployments, *dpl)
				processed[key] = struct{}{}
			}
		}
	}

	return deployments, err
}

// ListReplicationControllers returns all the replicationcontrollers related to the given node.
func (dpl *Deployer) ListReplicationControllers(t testing.TestingT, writer io.Writer, node string) ([]corev1.ReplicationController, error) {
	var replicationcontrollers []corev1.ReplicationController
	processed := map[string]struct{}{}

	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	pods, err := dpl.ListPods(t, writer, node)
	if err != nil {
		return nil, err
	}

	for i := range pods {
		// Get owner.
		owner := events.ManagingOwner(pods[i].OwnerReferences)
		if owner != nil && owner.Kind == "ReplicationController" {
			opt.Namespace = pods[i].Namespace
			// Get the replica set.
			clientset, err := k8s.GetKubernetesClientFromOptionsE(t, opt)
			if err != nil {
				return nil, err
			}
			rc, err := clientset.CoreV1().ReplicationControllers(opt.Namespace).Get(context.Background(), owner.Name, v1.GetOptions{})
			if err != nil {
				return nil, err
			}

			key := fmt.Sprintf("%s/%s", rc.Name, rc.Namespace)
			if _, ok := processed[key]; !ok {
				replicationcontrollers = append(replicationcontrollers, *rc)
				processed[key] = struct{}{}
			}
		}
	}

	return replicationcontrollers, nil
}

// ListDaemonsets returns all the daemonsets related to the given node.
func (dpl *Deployer) ListDaemonsets(t testing.TestingT, writer io.Writer, node string) ([]appsv1.DaemonSet, error) {
	var daemonsets []appsv1.DaemonSet
	processed := map[string]struct{}{}

	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	pods, err := dpl.ListPods(t, writer, node)
	if err != nil {
		return nil, err
	}

	for i := range pods {
		// Get owner.
		owner := events.ManagingOwner(pods[i].OwnerReferences)
		if owner != nil && owner.Kind == "Daemonset" {
			opt.Namespace = pods[i].Namespace
			// Get the replica set.
			ds, err := k8s.GetDaemonSetE(t, opt, owner.Name)
			if err != nil {
				return nil, err
			}

			key := fmt.Sprintf("%s/%s", ds.Name, ds.Namespace)
			if _, ok := processed[key]; !ok {
				daemonsets = append(daemonsets, *ds)
				processed[key] = struct{}{}
			}
		}
	}

	return daemonsets, nil
}

// ListServices returns all the services related to the given node.
func (dpl *Deployer) ListServices(t testing.TestingT, writer io.Writer, node string) ([]corev1.Service, error) {
	var svcs []corev1.Service
	processed := map[string]struct{}{}

	opt := &k8s.KubectlOptions{Logger: logger.New(NewLogger(writer))}

	// List all existing pods services.
	services, err := k8s.ListServicesE(t, opt, v1.ListOptions{})
	if err != nil {
		return nil, err
	}

	if len(services) == 0 {
		return nil, nil
	}

	for i := range services {
		opt.Namespace = services[i].Namespace
		pods, err := k8s.ListPodsE(t, opt, v1.ListOptions{
			LabelSelector: makeLabelSelector(services[i].Spec.Selector),
			FieldSelector: NodeFieldSelector + node,
		})
		if err != nil {
			return nil, err
		}

		if len(pods) != 0 {
			key := fmt.Sprintf("%s/%s", services[i].Name, services[i].Namespace)
			if _, ok := processed[key]; !ok {
				svcs = append(svcs, services[i])
				processed[key] = struct{}{}
			}
		}
	}

	return svcs, nil
}

// makeLabelSelector is a helper to format a map of label key and value pairs into a single string for use as a selector.
func makeLabelSelector(labels map[string]string) string {
	out := []string{}
	for key, value := range labels {
		out = append(out, fmt.Sprintf("%s=%s", key, value))
	}
	return strings.Join(out, ",")
}

// MetaToString returns the metadata to string. But before removes all the unnecessary fields.
func MetaToString(meta *v1.ObjectMeta) (string, error) {
	// First thing remove all metadata fields that are not of interest.
	// Current fields that are not filtered out:
	// Name, GenerateName, Namespace, UID, CreationTimestamp, Labels, OwnerReferences.
	meta.Annotations = nil
	meta.ManagedFields = nil
	meta.Annotations = nil
	meta.Finalizers = nil
	meta.ResourceVersion = ""
	meta.DeletionTimestamp = nil
	meta.Generation = 0
	meta.CreationTimestamp = v1.Time{}
	meta.OwnerReferences = nil
	meta.DeletionGracePeriodSeconds = nil

	metaUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&meta)
	if err != nil {
		return "", err
	}
	delete(metaUn, "creationTimestamp")
	data, err := json.Marshal(metaUn)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// PodStatusToString returns the pod status to string. It removes all the unnecessary fields.
func PodStatusToString(status *corev1.PodStatus) (string, error) {
	st := corev1.PodStatus{PodIP: status.PodIP}
	statusUn, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&st)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(statusUn)
	if err != nil {
		return "", err
	}

	return string(data), nil
}
