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
	nodes     map[string]map[string]uint
	sync.RWMutex
}

// NewGenericCache creates a new GenericCache.
// A GenericResource could have multiple resources to which it refers in the same node.
// For example, a Deployment could have more than on pod running on the same node. So we need to track this,
// and we do it keeping a map where for each GenericResource we keep a counter of how many related resources are living
// in a given node.
func NewGenericCache() GenericCache {
	return GenericCache{
		resources: make(map[string]*GenericResource),
		nodes:     make(map[string]map[string]uint),
	}
}

// Add adds a new item to the cache if it does not exist. If the resource we are adding already exists we make sure
// to track the new add by incrementing the counter for the node. At the same time, when adding the item to the cache
// we reset the information about the nodes to which we should send an "Added" event. Before calling the Add function
// for a resource make sure that you have generated the "Added" event for the resource.
func (gc *GenericCache) Add(key string, value *GenericResource) {
	gc.Lock()
	// Check if the resource already exists.
	if _, ok := gc.resources[key]; !ok {
		gc.resources[key] = value
		nodes, ok := gc.nodes[key]
		if !ok {
			nodes = make(map[string]uint)
		}

		for _, node := range value.AddedFor {
			if val, ok := nodes[node]; ok {
				nodes[node] = val + 1
			} else {
				nodes[node] = 1
			}
		}
		gc.nodes[key] = nodes
	}
	// Do not track anymore the nodes for which we need to generate an "Added" event.
	value.SetCreatedFor(nil)
	gc.Unlock()
}

// Update updates an item in the cache. At the same time, when updating the item in the cache
// we reset the information about the nodes to which we should send a "Modified" event. Before calling the Update function
// for a resource make sure that you have generated the "Modified" event for the resource.
func (gc *GenericCache) Update(key string, value *GenericResource) {
	gc.Lock()
	gc.resources[key] = value
	// Do not track anymore the nodes for which we need to generate a "Modified" event.
	value.SetModifiedFor(nil)
	gc.Unlock()
}

// Delete deletes an item from the cache. The item gets deleted when all the resources related to the item have been
// removed from the nodes. Otherwise, it just updates the node counter for the node and keeps the item in the cache.
func (gc *GenericCache) Delete(key string) {
	var value *GenericResource // Check if the resource already exists.
	var ok bool
	gc.Lock()
	if value, ok = gc.resources[key]; ok {
		nodes := gc.nodes[key]
		for _, node := range value.DeletedFor {
			if val, ok := nodes[node]; ok {
				if val > 1 {
					nodes[node] = val - 1
				} else {
					delete(nodes, node)
				}
			}
		}
		gc.nodes[key] = nodes
	}

	if len(gc.nodes[key]) == 0 {
		delete(gc.resources, key)
	} else {
		value.SetDeletedFor(nil)
	}
	gc.Unlock()
}

// Get returns an item from the cache using the provided key.
func (gc *GenericCache) Get(key string) (*GenericResource, bool) {
	gc.RLock()
	val, ok := gc.resources[key]
	gc.RUnlock()
	return val, ok
}

func (gc *GenericCache) ForEach(apply func(resource *GenericResource)) {
	gc.RLock()
	for _, res := range gc.resources {
		apply(res)
	}
	gc.RUnlock()
}
