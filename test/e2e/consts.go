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

// NodeNameEnv variable name that contains the node name used to subscribe to the collector.
const (
	// NodeNameEnv name of the environment variable that holds the node name.
	NodeNameEnv = "NODENAME"
	// NodeFieldSelector selector used to get pods for a specific node.
	NodeFieldSelector = "spec.nodeName="
)
