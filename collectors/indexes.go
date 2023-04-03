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
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	nodeNameIndex = "spec.nodeName"
	podPrefixName = "metadata.generateName"
)

// IndexPodByNode adds an indexer for bots base on the NodeName where it has been scheduled.
func IndexPodByNode(ctx context.Context, fi client.FieldIndexer) error {
	return fi.IndexField(ctx, &corev1.Pod{}, nodeNameIndex, podByNode)
}

func podByNode(o client.Object) []string {
	pod, ok := o.(*corev1.Pod)
	if !ok {
		return []string{}
	}
	if pod.Spec.NodeName != "" {
		return []string{pod.Spec.NodeName}
	}
	return []string{}
}

// IndexPodByPrefixName adds an indexer for pods based on the pods prefix name.
func IndexPodByPrefixName(ctx context.Context, fi client.FieldIndexer) error {
	return fi.IndexField(ctx, &corev1.Pod{}, podPrefixName, podByPrefixName)
}

func podByPrefixName(o client.Object) []string {
	pod, ok := o.(*corev1.Pod)
	if !ok {
		return []string{}
	}
	if pod.GenerateName != "" {
		// Handling case when the pod has been created by a Deployment.
		if hash, ok := pod.Labels["pod-template-hash"]; ok {
			return []string{strings.TrimSuffix(pod.GenerateName, fmt.Sprintf("-%s-", hash)), pod.GenerateName}
		}
		// Handle all other cases when pod has a generated name.
		return []string{pod.GenerateName}
	}
	return []string{}
}
