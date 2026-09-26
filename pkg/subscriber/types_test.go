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

package subscriber

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/falcosecurity/k8s-metacollector/pkg/fields"
)

func TestSubscribersPerNode(t *testing.T) {
	t.Parallel()

	s := NewSubscribers()
	require.False(t, s.HasNode("node-1"))
	require.Nil(t, s.GetSubscribersPerNode("node-1"))
	require.Equal(t, 0, s.Len())

	s.AddSubscriberPerNode("node-1", "sub-1")
	s.AddSubscriberPerNode("node-1", "sub-2")
	s.AddSubscriberPerNode("node-1", "sub-2")
	s.AddSubscriberPerNode("node-2", "sub-3")

	require.True(t, s.HasNode("node-1"))
	require.True(t, s.HasNode("node-2"))
	require.Equal(t, 2, s.Len())
	require.Equal(t, fields.Subscribers{"sub-1": {}, "sub-2": {}}, s.GetSubscribersPerNode("node-1"))
	require.Equal(t, fields.Subscribers{"sub-3": {}}, s.GetSubscribersPerNode("node-2"))
}

func TestSubscribersDeleteRemovesEmptyNodes(t *testing.T) {
	t.Parallel()

	s := NewSubscribers()
	s.AddSubscriberPerNode("node-1", "sub-1")
	s.AddSubscriberPerNode("node-1", "sub-2")

	s.DeleteSubscriberPerNode("node-1", "sub-1")
	require.True(t, s.HasNode("node-1"))
	require.Equal(t, fields.Subscribers{"sub-2": {}}, s.GetSubscribersPerNode("node-1"))

	s.DeleteSubscriberPerNode("node-1", "sub-2")
	require.False(t, s.HasNode("node-1"))
	require.Nil(t, s.GetSubscribersPerNode("node-1"))
	require.Equal(t, 0, s.Len())

	// Deleting from unknown nodes or unknown subscribers is a no-op.
	s.DeleteSubscriberPerNode("missing", "sub-1")
	s.AddSubscriberPerNode("node-2", "sub-3")
	s.DeleteSubscriberPerNode("node-2", "missing")
	require.Equal(t, fields.Subscribers{"sub-3": {}}, s.GetSubscribersPerNode("node-2"))
}

func TestSubscribersGetReturnsCopy(t *testing.T) {
	t.Parallel()

	s := NewSubscribers()
	s.AddSubscriberPerNode("node-1", "sub-1")

	subs := s.GetSubscribersPerNode("node-1")
	subs.Add("sub-2")
	subs.Delete("sub-1")

	require.Equal(t, fields.Subscribers{"sub-1": {}}, s.GetSubscribersPerNode("node-1"))
}

func TestSubscribersConcurrentAccess(t *testing.T) {
	t.Parallel()

	s := NewSubscribers()
	var wg sync.WaitGroup
	for i := range 20 {
		node := fmt.Sprintf("node-%d", i%4)
		sub := fmt.Sprintf("sub-%d", i)
		wg.Go(func() {
			s.AddSubscriberPerNode(node, sub)
			_ = s.GetSubscribersPerNode(node)
			_ = s.HasNode(node)
			_ = s.Len()
			s.DeleteSubscriberPerNode(node, sub)
		})
	}
	wg.Wait()

	for i := range 4 {
		require.False(t, s.HasNode(fmt.Sprintf("node-%d", i)))
	}
}
