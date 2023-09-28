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
)

// GenericCache for generic resources.
type GenericCache struct {
	resources map[string]*GenericResource
	rwLock    sync.RWMutex
}

// NewGenericCache creates a new GenericCache.
func NewGenericCache() *GenericCache {
	return &GenericCache{
		resources: make(map[string]*GenericResource),
		rwLock:    sync.RWMutex{},
	}
}

// Add adds a new item to the cache if it does not exist. When adding the item to the cache
// we reset the information about the nodes to which we should send an "Added" event. Before calling the Add function
// for a resource make sure that you have generated the "Added" event for the resource.
func (gc *GenericCache) Add(key string, value *GenericResource) {
	gc.rwLock.Lock()
	// Check if the resource already exists.
	if _, ok := gc.resources[key]; !ok {
		gc.resources[key] = value
	}
	// Do not track anymore the nodes for which we need to generate an "Added" event.
	value.SetCreatedFor(nil)
	gc.rwLock.Unlock()
}

// Update updates an item in the cache. At the same time, when updating the item in the cache
// we reset the information about the nodes to which we should send a "Modified" event. Before calling the Update function
// for a resource make sure that you have generated the "Modified" event for the resource.
func (gc *GenericCache) Update(key string, value *GenericResource) {
	gc.rwLock.Lock()
	gc.resources[key] = value
	// Do not track anymore the nodes for which we need to generate a "Modified" event.
	value.SetModifiedFor(nil)
	gc.rwLock.Unlock()
}

// Delete deletes an item from the cache.
func (gc *GenericCache) Delete(key string) {
	gc.rwLock.Lock()
	delete(gc.resources, key)
	gc.rwLock.Unlock()
}

// Get returns an item from the cache using the provided key.
func (gc *GenericCache) Get(key string) (*GenericResource, bool) {
	gc.rwLock.RLock()
	val, ok := gc.resources[key]
	gc.rwLock.RUnlock()
	return val, ok
}

func (gc *GenericCache) ForEach(apply func(resource *GenericResource)) {
	gc.rwLock.RLock()
	for _, res := range gc.resources {
		apply(res)
	}
	gc.rwLock.RUnlock()
}
