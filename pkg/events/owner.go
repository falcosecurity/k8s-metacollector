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

package events

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagingOwner returns the controller owner of the resource if present.
func ManagingOwner(owners []v1.OwnerReference) *v1.OwnerReference {
	for _, o := range owners {
		if o.Controller != nil && *o.Controller {
			return &o
		}
	}
	return nil
}
