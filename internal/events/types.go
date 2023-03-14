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
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/alacuku/k8s-metadata/internal/fields"
)

// Generic event that holds metadata fields for k8s resources.
type Generic struct {
	EventType string
	fields.Metadata
	nodes fields.Nodes
}

// SetMetadata populates the medata fields.
func (g *Generic) SetMetadata(meta *metav1.ObjectMeta) {
	g.Set(meta)
}

// SetNodes populates the nodes to which send the event.
func (g *Generic) SetNodes(nodes fields.Nodes) {
	g.nodes = nodes
}

// Nodes returns the nodes.
func (g *Generic) Nodes() fields.Nodes {
	return g.nodes
}

// Pod event for pod resources.
type Pod struct {
	Generic
	refs fields.References
}

func (p *Pod) String() string {
	return fmt.Sprintf("name %q, namespace %q, nodes %q", p.Name(), p.Namespace(), p.nodes)
}

// AddReferencesForKind adds references for a given kind in the pod event.
func (p *Pod) AddReferencesForKind(kind string, refs []types.UID) bool {
	if p.refs == nil {
		p.refs = make(map[string][]types.UID)
	}
	refsLen := len(refs)
	// If the passed refs have length 0 for the given resource kind, which is acceptable then we delete the current one.
	if refsLen == 0 {
		delete(p.refs, kind)
		return true
	}

	// Check if we already have references for the given resource kind.
	oldRefs, ok := p.refs[kind]
	// If no refs found and the new refs are not empty then set them for the given resource kind.
	if !ok && refsLen != 0 {
		p.refs[kind] = refs
		return true
	}

	// If the number of old references is not equal to the number of new references,
	// just delete the current ones and set the new ones.
	if len(oldRefs) != refsLen {
		p.refs[kind] = refs
		return true
	}

	// Determine if we need to update the refs.
	for _, uid := range refs {
		if !contains(oldRefs, uid) {
			p.refs[kind] = refs
			return true
		}
	}

	return false
}
