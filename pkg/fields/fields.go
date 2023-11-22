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

package fields

import (
	"k8s.io/apimachinery/pkg/types"
)

// Reference used to reference objects to which a resource is related.
type Reference struct {
	Name types.NamespacedName
	UID  types.UID
}

// References custom type for a resource's references.
// Key is the kind of the resource and the values is a slice containing all the references for objects
// of the same kind as the key.
type References map[string][]Reference

// ToFlatMap return the references in a map where the key is the kind of the object for which the references
// are saved. The value is slice containing all the types.UID for objects of the same kind as the key.
func (r *References) ToFlatMap() map[string][]string {
	flatMap := make(map[string][]string)

	for key, val := range *r {
		refs := make([]string, len(val))
		for i := range val {
			refs[i] = string(val[i].UID)
		}
		flatMap[key] = refs
	}
	return flatMap
}

// Subscribers custom type for subscribers to which a given event need to be sent.
// The key is the subscriber's UID.
type Subscribers map[string]struct{}

// Add adds a subscriber.
func (s Subscribers) Add(sub string) {
	s[sub] = struct{}{}
}

// Delete deletes a subscriber.
func (s Subscribers) Delete(sub string) {
	delete(s, sub)
}

// Has returns true if a subcriber is present.
func (s Subscribers) Has(sub string) bool {
	_, ok := s[sub]
	return ok
}

// Intersect returns the intersection with the given set.
func (s Subscribers) Intersect(subs Subscribers) Subscribers {
	intersection := make(Subscribers)
	s1, s2 := s, subs
	if len(s1) > len(s2) {
		s1, s2 = s2, s1
	}
	for sub := range s1 {
		if _, ok := s2[sub]; ok {
			intersection[sub] = struct{}{}
		}
	}
	return intersection
}

// Difference returns the difference ( all the members of the initial set that are not members of the given set).
func (s Subscribers) Difference(subs Subscribers) Subscribers {
	setDifference := make(Subscribers)
	for sub := range s {
		if _, ok := subs[sub]; !ok {
			setDifference[sub] = struct{}{}
		}
	}
	return setDifference
}
