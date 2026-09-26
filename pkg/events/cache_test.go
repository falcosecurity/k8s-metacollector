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

package events

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCacheAddAndUpdate(t *testing.T) {
	type op struct {
		update bool
		hash   uint64
	}

	tests := []struct {
		name     string
		ops      []op
		wantHash uint64
	}{
		{name: "add inserts", ops: []op{{hash: 1}}, wantHash: 1},
		{name: "add does not overwrite", ops: []op{{hash: 1}, {hash: 2}}, wantHash: 1},
		{name: "update inserts", ops: []op{{update: true, hash: 1}}, wantHash: 1},
		{name: "update overwrites", ops: []op{{update: true, hash: 1}, {update: true, hash: 2}}, wantHash: 2},
		{name: "update overwrites added entry", ops: []op{{hash: 1}, {update: true, hash: 2}}, wantHash: 2},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			c := NewCache()
			for _, o := range test.ops {
				entry := &CacheEntry{UID: "uid-1", Hash: o.hash}
				if o.update {
					c.Update("ns/pod", entry)
				} else {
					c.Add("ns/pod", entry)
				}
			}

			got, ok := c.Get("ns/pod")
			require.True(t, ok)
			require.Equal(t, test.wantHash, got.Hash)
		})
	}
}

func TestCacheGetHasDelete(t *testing.T) {
	t.Parallel()

	c := NewCache()
	got, ok := c.Get("missing")
	require.False(t, ok)
	require.Nil(t, got)
	require.False(t, c.Has("missing"))

	c.Add("ns/pod", &CacheEntry{UID: "uid-1"})
	require.True(t, c.Has("ns/pod"))

	c.Delete("ns/pod")
	require.False(t, c.Has("ns/pod"))

	// Deleting a missing key is a no-op.
	c.Delete("ns/pod")
	require.False(t, c.Has("ns/pod"))
}

func TestCacheConcurrentAccess(t *testing.T) {
	t.Parallel()

	c := NewCache()
	var wg sync.WaitGroup
	for i := range 20 {
		key := fmt.Sprintf("ns/pod-%d", i)
		wg.Go(func() {
			c.Add(key, &CacheEntry{Hash: 1})
			c.Update(key, &CacheEntry{Hash: 2})
			_, _ = c.Get(key)
			_ = c.Has(key)
		})
	}
	wg.Wait()

	for i := range 20 {
		got, ok := c.Get(fmt.Sprintf("ns/pod-%d", i))
		require.True(t, ok)
		require.Equal(t, uint64(2), got.Hash)
	}
}
