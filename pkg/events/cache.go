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
	"sync"

	"k8s.io/apimachinery/pkg/types"

	"github.com/alacuku/k8s-metadata/pkg/fields"
)

// Cache for resources items. For each resource that is sent to at least
// a subscriber it is saved in a cache. The cache is useful for this scenarios:
//  1. each time a new subscriber arrives the cache is used to trigger the respective
//     collector for the cached resources. This way the subscriber receives all the existing
//     resources.
//  2. When a resource is deleted we need to know the subscribers that need a Delete event.
//     The cache provides the sayed subscribers.
//  3. When a resource is updated the cache knows the subscribers that need an Update event.
type Cache struct {
	items  map[string]*CacheEntry
	rwLock sync.RWMutex
}

// CacheEntry items that can be saved in the cache.
type CacheEntry struct {
	Hash uint64
	UID  types.UID
	Refs fields.References
	Subs fields.Subscribers
}

// NewCache creates a new Cache.
func NewCache() *Cache {
	return &Cache{
		items:  make(map[string]*CacheEntry),
		rwLock: sync.RWMutex{},
	}
}

// Add adds a new item to the cache if it does not exist.
func (gc *Cache) Add(key string, value *CacheEntry) {
	gc.rwLock.Lock()
	// Check if the CacheEntry already exists.
	if _, ok := gc.items[key]; !ok {
		gc.items[key] = value
	}
	gc.rwLock.Unlock()
}

// Update updates an item in the cache.
func (gc *Cache) Update(key string, value *CacheEntry) {
	gc.rwLock.Lock()
	gc.items[key] = value
	gc.rwLock.Unlock()
}

// Delete deletes an item from the cache.
func (gc *Cache) Delete(key string) {
	gc.rwLock.Lock()
	delete(gc.items, key)
	gc.rwLock.Unlock()
}

// Get returns an item from the cache using the provided key.
func (gc *Cache) Get(key string) (*CacheEntry, bool) {
	gc.rwLock.RLock()
	val, ok := gc.items[key]
	gc.rwLock.RUnlock()
	return val, ok
}

// Has returns true if a key is present.
func (gc *Cache) Has(key string) bool {
	gc.rwLock.RLock()
	defer gc.rwLock.RUnlock()
	_, ok := gc.items[key]
	return ok
}
