// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 The Falco Authors
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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

func newSubs(subs ...string) Subscribers {
	s := make(Subscribers, len(subs))
	for _, sub := range subs {
		s.Add(sub)
	}
	return s
}

func TestReferencesToFlatMap(t *testing.T) {
	tests := []struct {
		name     string
		refs     References
		expected map[string][]string
	}{
		{
			name:     "empty references",
			refs:     References{},
			expected: map[string][]string{},
		},
		{
			name: "multiple kinds keep UIDs in order",
			refs: References{
				"Pod": {
					{Name: types.NamespacedName{Namespace: "ns", Name: "a"}, UID: "uid-a"},
					{Name: types.NamespacedName{Namespace: "ns", Name: "b"}, UID: "uid-b"},
				},
				"Namespace": {
					{Name: types.NamespacedName{Name: "ns"}, UID: "uid-ns"},
				},
			},
			expected: map[string][]string{
				"Pod":       {"uid-a", "uid-b"},
				"Namespace": {"uid-ns"},
			},
		},
		{
			name:     "kind without references",
			refs:     References{"Service": {}},
			expected: map[string][]string{"Service": {}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.expected, test.refs.ToFlatMap())
		})
	}
}

func TestSubscribersAddDeleteHas(t *testing.T) {
	tests := []struct {
		name     string
		initial  Subscribers
		add      []string
		del      []string
		expected Subscribers
	}{
		{name: "add to empty set", initial: newSubs(), add: []string{"a"}, expected: newSubs("a")},
		{name: "add is idempotent", initial: newSubs("a"), add: []string{"a", "a"}, expected: newSubs("a")},
		{name: "delete existing", initial: newSubs("a", "b"), del: []string{"a"}, expected: newSubs("b")},
		{name: "delete missing is a no-op", initial: newSubs("a"), del: []string{"missing"}, expected: newSubs("a")},
		{name: "add then delete", initial: newSubs(), add: []string{"a", "b"}, del: []string{"a", "b"}, expected: newSubs()},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			s := test.initial
			for _, sub := range test.add {
				s.Add(sub)
				require.True(t, s.Has(sub))
			}
			for _, sub := range test.del {
				s.Delete(sub)
				require.False(t, s.Has(sub))
			}
			require.Equal(t, test.expected, s)
		})
	}
}

func TestSubscribersIntersect(t *testing.T) {
	tests := []struct {
		name     string
		s1, s2   Subscribers
		expected Subscribers
	}{
		{
			name:     "partial overlap",
			s1:       newSubs("a", "b", "c"),
			s2:       newSubs("b", "c", "d"),
			expected: newSubs("b", "c"),
		},
		{
			name:     "receiver larger than argument",
			s1:       newSubs("a", "b", "c", "d"),
			s2:       newSubs("d"),
			expected: newSubs("d"),
		},
		{
			name:     "disjoint sets",
			s1:       newSubs("a"),
			s2:       newSubs("b"),
			expected: newSubs(),
		},
		{
			name:     "nil argument",
			s1:       newSubs("a"),
			s2:       nil,
			expected: newSubs(),
		},
		{
			name:     "nil receiver",
			s1:       nil,
			s2:       newSubs("a"),
			expected: newSubs(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.expected, test.s1.Intersect(test.s2))
			// Intersection is commutative.
			require.Equal(t, test.expected, test.s2.Intersect(test.s1))
		})
	}
}

func TestSubscribersDifference(t *testing.T) {
	tests := []struct {
		name     string
		s1, s2   Subscribers
		expected Subscribers
	}{
		{
			name:     "partial overlap",
			s1:       newSubs("a", "b", "c"),
			s2:       newSubs("b", "d"),
			expected: newSubs("a", "c"),
		},
		{
			name:     "identical sets",
			s1:       newSubs("a", "b"),
			s2:       newSubs("a", "b"),
			expected: newSubs(),
		},
		{
			name:     "nil argument returns a copy of the receiver",
			s1:       newSubs("a", "b"),
			s2:       nil,
			expected: newSubs("a", "b"),
		},
		{
			name:     "nil receiver",
			s1:       nil,
			s2:       newSubs("a"),
			expected: newSubs(),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.expected, test.s1.Difference(test.s2))
		})
	}
}

func TestSubscribersSetOperationsDoNotMutateInputs(t *testing.T) {
	t.Parallel()

	s1 := newSubs("a", "b")
	s2 := newSubs("b", "c")

	s1.Intersect(s2).Add("x")
	s1.Difference(s2).Add("y")

	require.Equal(t, newSubs("a", "b"), s1)
	require.Equal(t, newSubs("b", "c"), s2)
}
