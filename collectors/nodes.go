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

package collectors

/*func eventsPerNode(evt events.Event, currentNodes fields.Nodes, updated bool) (added, modified, deleted, nodes fields.Nodes) {
	oldNodes := evt.Nodes()

	if len(currentNodes) == 0 {
		deleted = oldNodes
		nodes = nil
		return
	}

	if len(oldNodes) == 0 {
		added = currentNodes
		nodes = currentNodes
		return
	}

	added = make(fields.Nodes)
	modified = make(fields.Nodes)
	deleted = make(fields.Nodes)
	nodes = make(fields.Nodes)

	// Compute nodes to which send modified and added events.
	for n := range currentNodes {
		if _, ok := oldNodes[n]; ok {
			if updated {
				modified[n] = struct{}{}
			}
			nodes[n] = struct{}{}
		} else {
			added[n] = struct{}{}
			nodes[n] = struct{}{}
		}
	}

	// Compute nodes to which send deleted event.
	for n := range oldNodes {
		if _, ok := currentNodes[n]; !ok {
			deleted[n] = struct{}{}
		}
	}

	return
}*/
