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

package subscriber

import (
	"sync"

	"github.com/alacuku/k8s-metadata/pkg/fields"
)

// Subscribers for generic items.
type Subscribers struct {
	items  map[string]fields.Subscribers
	rwLock sync.RWMutex
}

// NewSubscribers creates a new Subscribers.
func NewSubscribers() *Subscribers {
	return &Subscribers{
		items:  make(map[string]fields.Subscribers),
		rwLock: sync.RWMutex{},
	}
}

// AddSubscriberPerNode adds a new subscriber for the given node.
func (gc *Subscribers) AddSubscriberPerNode(node, sub string) {
	gc.rwLock.Lock()
	defer gc.rwLock.Unlock()
	// Get subscribers per node.
	subs, ok := gc.items[node]
	if !ok {
		subs = make(map[string]struct{})
	}

	subs[sub] = struct{}{}
	gc.items[node] = subs
}

// GetSubscribersPerNode returns the subscribers for a given node.
// It's a copy, safe to save it for later use or modify it.
func (gc *Subscribers) GetSubscribersPerNode(node string) fields.Subscribers {
	gc.rwLock.RLock()
	defer gc.rwLock.RUnlock()
	s, ok := gc.items[node]
	if ok {
		subs := make(fields.Subscribers, len(s))
		for sub := range s {
			subs[sub] = struct{}{}
		}
		return subs
	}
	return nil
}

// DeleteSubscriberPerNode given the node and the subscribers UID it removes it.
func (gc *Subscribers) DeleteSubscriberPerNode(node, sub string) {
	gc.rwLock.Lock()
	defer gc.rwLock.Unlock()
	// Get subscribers per node.
	subs, ok := gc.items[node]
	if ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(gc.items, node)
		} else {
			gc.items[node] = subs
		}
	}
}

// HasNode returns true if a node has subscribers.
func (gc *Subscribers) HasNode(node string) bool {
	gc.rwLock.RLock()
	defer gc.rwLock.RUnlock()
	_, ok := gc.items[node]
	return ok
}
