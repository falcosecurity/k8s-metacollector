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
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/alacuku/k8s-metadata/internal/resource"
)

var newPartialDeployment = partialDeployment(nil)
var newPartialReplicaset = partialReplicaset(nil)
var newPartialNamespace = partialNamespace(nil)
var newPartialDaemonset = partialDaemonsets(nil)
var newPartialReplicationController = partialReplicationController(nil)

// TODO(alacuku): merge in a single function using a switch case.

func partialDeployment(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(v1.SchemeGroupVersion.WithKind("Deployment"))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}

func partialReplicaset(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(v1.SchemeGroupVersion.WithKind("ReplicaSet"))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}

func partialNamespace(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}

func partialDaemonsets(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(v1.SchemeGroupVersion.WithKind(resource.Daemonset))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}

func partialReplicationController(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(resource.ReplicationController))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}

func partialService(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(resource.Service))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}

func partialPod(name *types.NamespacedName) *metav1.PartialObjectMetadata {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind(resource.Pod))
	if name != nil {
		obj.Name = name.Name
		obj.Namespace = name.Namespace
	}
	return obj
}
