// SPDX-License-Identifier: Apache-2.0
// Copyright 2023 The Falco Authors
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
	"fmt"

	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
)

// PodTransformer transforms the pod objects received from the api-server
// before adding them to the cache.
var PodTransformer = func(logger logr.Logger) toolscache.TransformFunc {
	return func(i interface{}) (interface{}, error) {
		pod, ok := i.(*corev1.Pod)
		if !ok {
			err := fmt.Errorf("unable to convert object to %T", corev1.Pod{})
			logger.Error(err, "transformer", "kind", resource.Pod)
			return nil, err
		}

		podIP := pod.Status.PodIP
		pod.Status = corev1.PodStatus{PodIP: podIP}
		nodeName := pod.Spec.NodeName
		pod.Spec = corev1.PodSpec{NodeName: nodeName}
		filterOutMetaFields(&pod.ObjectMeta)
		return pod, nil
	}
}

// PartialObjectTransformer PodTransformer transforms the metadata objects received from the api-server
// before adding them to the cache.
var PartialObjectTransformer = func(logger logr.Logger) toolscache.TransformFunc {
	return func(i interface{}) (interface{}, error) {
		meta, ok := i.(*metav1.PartialObjectMetadata)
		if !ok {
			err := fmt.Errorf("unable to convert object to %T", metav1.PartialObjectMetadata{})
			logger.Error(err, "transformer", "kind", "PartialObjectMetadata")
			return nil, err
		}

		filterOutMetaFields(&meta.ObjectMeta)
		return meta, nil
	}
}

// ServiceTransformer transforms the service objects received from the api-server
// before adding them to the cache.
var ServiceTransformer = func(logger logr.Logger) toolscache.TransformFunc {
	return func(i interface{}) (interface{}, error) {
		svc, ok := i.(*corev1.Service)
		if !ok {
			err := fmt.Errorf("unable to convert object to %T", corev1.Service{})
			logger.Error(err, "transformer", "kind", resource.Service)
			return nil, err
		}

		selector := svc.Spec.Selector
		svc.Spec = corev1.ServiceSpec{Selector: selector}
		svc.Status = corev1.ServiceStatus{}
		filterOutMetaFields(&svc.ObjectMeta)
		return svc, nil
	}
}

// EndpointsliceTransformer transforms the endpointslice objects received from the api-server
// before adding them to the cache.
var EndpointsliceTransformer = func(logger logr.Logger) toolscache.TransformFunc {
	return func(i interface{}) (interface{}, error) {
		ep, ok := i.(*discoveryv1.EndpointSlice)
		if !ok {
			err := fmt.Errorf("unable to convert object to %T", discoveryv1.EndpointSlice{})
			logger.Error(err, "transformer", "kind", resource.EndpointSlice)
			return nil, err
		}

		ep.Ports = nil
		filterOutMetaFields(&ep.ObjectMeta)
		return ep, nil
	}
}

// filterOutMetaFields removes unused metadata fields.
func filterOutMetaFields(meta *metav1.ObjectMeta) {
	// Current fields that are not filtered out:
	// Name, GenerateName, Namespace, UID, CreationTimestamp, Labels, OwnerReferences.
	meta.Annotations = nil
	meta.ManagedFields = nil
	meta.Annotations = nil
	meta.Finalizers = nil
	meta.ResourceVersion = ""
	meta.DeletionTimestamp = nil
	meta.Generation = 0
	meta.DeletionGracePeriodSeconds = nil
}
