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

package resource

const (
	// Namespace kind as used by k8s resources.
	Namespace = "Namespace"
	// Daemonset kind as used by k8s resources.
	Daemonset = "DaemonSet"
	// Deployment kind as used by k8s resources.
	Deployment = "Deployment"
	// ReplicationController kind as used by k8s resources.
	ReplicationController = "ReplicationController"
	// ReplicaSet kind as used by k8s resources.
	ReplicaSet = "ReplicaSet"
	// Service kind as used by k8s resources.
	Service = "Service"
	// Pod kind as used by k8s resources.
	Pod = "Pod"
	// Endpoints kind as used by k8s resources.
	Endpoints = "Endpoints"
	// EndpointSlice kind as used by the k8s resources.
	EndpointSlice = "EndpointSlice"
)
