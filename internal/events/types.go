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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/alacuku/k8s-metadata/internal/fields"
)

// GenericResource event that holds metadata fields for k8s resources.
type GenericResource struct {
	Metadata    fields.Metadata
	Nodes       fields.Nodes
	AddedFor    []string
	ModifiedFor []string
	DeletedFor  []string
}

// NewGenericResourceFromMetadata creates a new GenericResource containing the given metadata of the given kind.
func NewGenericResourceFromMetadata(meta *metav1.ObjectMeta, kind string) *GenericResource {
	gr := new(GenericResource)
	gr.Nodes = make(fields.Nodes)
	gr.SetMetadata(meta, kind)
	return gr
}

// SetCreatedFor sets the nodes for which this resource needs to generate an "Added" event.
func (g *GenericResource) SetCreatedFor(nodes []string) {
	g.AddedFor = nodes
}

// SetModifiedFor sets the nodes for which this resource needs to generate a "Modified" event.
func (g *GenericResource) SetModifiedFor(nodes []string) {
	g.ModifiedFor = nodes
}

// SetDeletedFor sets the nodes for which this resource need to generate a "Deleted" event.
func (g *GenericResource) SetDeletedFor(nodes []string) {
	g.DeletedFor = nodes
}

// AddNodes populates the nodes to which the events need to be sent, starting from the given nodes and the provided flag.
// The starting point is the cached nodes to which we already know that we have sent at least an "Added" event. From the
// cached nodes and the passed ones it generates the nodes to which we need to send an event and saves them in the appropriate
// field, i.e. GenericResource.AddedFor, GenericResource.ModifiedFor and GenericResource.DeletedFor. Note that the updated
// flag is set to true if the mutable fields for the resource has been updated. At the same time the GenericResource.Nodes field is updated
// based on the new added or deleted nodes.
func (g *GenericResource) AddNodes(nodes []string, updated bool) {
	var added, modified, deleted []string
	tmpNodes := make(fields.Nodes, len(nodes))

	for _, n := range nodes {
		tmpNodes[n] = struct{}{}
		if _, ok := g.Nodes[n]; !ok {
			g.Nodes[n] = struct{}{}
			added = append(added, n)
		} else if updated {
			modified = append(modified, n)
		}
	}

	for n := range g.Nodes {
		if _, ok := tmpNodes[n]; !ok {
			deleted = append(deleted, n)
			delete(g.Nodes, n)
		}
	}

	g.AddedFor = added
	if updated {
		g.ModifiedFor = modified
	}
	g.DeletedFor = deleted
}

// DeleteNodes populates the GenericResource.DeletedFor field with the passed nodes.
// At the same time the GenericResource.Nodes field is updated based on the new added
// or deleted nodes.
func (g *GenericResource) DeleteNodes(nodes []string) {
	var deleted []string

	for _, n := range nodes {
		if _, ok := g.Nodes[n]; ok {
			deleted = append(deleted, n)
			delete(g.Nodes, n)
		}
	}

	g.DeletedFor = deleted
}

// SetMetadata populates the medata fields.
func (g *GenericResource) SetMetadata(meta *metav1.ObjectMeta, kind string) {
	g.Metadata.Set(meta, kind)
}

// UpdateLabels updates the labels fields.
func (g *GenericResource) UpdateLabels(l map[string]string) bool {
	return g.Metadata.UpdateLabels(l)
}

// SetNodes populates the nodes to which send the event.
func (g *GenericResource) SetNodes(nodes fields.Nodes) {
	g.Nodes = nodes
}

// ToEvents returns a slice containing Event based on the internal state of the GenericResource.
func (g *GenericResource) ToEvents() []Event {
	evts := make([]Event, 3)

	if len(g.AddedFor) != 0 {
		evts[0] = &GenericEvent{
			Reason:           Added,
			Metadata:         g.Metadata.DeepCopy(),
			DestinationNodes: g.AddedFor,
		}
	}

	if len(g.ModifiedFor) != 0 {
		evts[1] = &GenericEvent{
			Reason:           Modified,
			Metadata:         g.Metadata.DeepCopy(),
			DestinationNodes: g.ModifiedFor,
		}
	}

	if len(g.DeletedFor) != 0 {
		evts[2] = &GenericEvent{
			Reason:           Deleted,
			Metadata:         g.Metadata.DeepCopy(),
			DestinationNodes: g.DeletedFor,
		}
	}

	return evts
}

// ToEvent returns an event based on the reason for the specified nodes.
func (g *GenericResource) ToEvent(reason string, nodes []string) Event {
	var evt *GenericEvent
	switch reason {
	case Added:
		evt = &GenericEvent{
			Reason:           Added,
			Metadata:         g.Metadata.DeepCopy(),
			DestinationNodes: nodes,
		}
	case Modified:
		evt = &GenericEvent{
			Reason:           Modified,
			Metadata:         g.Metadata.DeepCopy(),
			DestinationNodes: nodes,
		}
	case Deleted:
		evt = &GenericEvent{
			Reason:           Deleted,
			Metadata:         g.Metadata.DeepCopy(),
			DestinationNodes: nodes,
		}
	default:
		evt = &GenericEvent{}
	}

	return evt
}

// NewPodResourceFromMetadata creates a new PodResource containing the given metadata.
func NewPodResourceFromMetadata(meta *metav1.ObjectMeta) *PodResource {
	return &PodResource{
		GenericResource:    *NewGenericResourceFromMetadata(meta, "Pod"),
		ResourceReferences: make(fields.References),
	}
}

// PodResource event for pod resources.
type PodResource struct {
	GenericResource
	ResourceReferences fields.References
}

// AddReferencesForKind adds references for a given kind in the pod event.
func (p *PodResource) AddReferencesForKind(kind string, refs []fields.Reference) bool {
	if p.ResourceReferences == nil {
		p.ResourceReferences = make(map[string][]fields.Reference)
	}

	refsLen := len(refs)

	// Check if we already have references for the given resource kind.
	oldRefs, ok := p.ResourceReferences[kind]
	// If no refs found and the new refs are not empty then set them for the given resource kind.
	if !ok && refsLen != 0 {
		p.ResourceReferences[kind] = refs
		return true
	}

	// If the passed refs have length 0 for the given resource kind, which is acceptable then we delete the current one.
	if refsLen == 0 && len(oldRefs) != 0 {
		delete(p.ResourceReferences, kind)
		return true
	}

	// If the number of old references is not equal to the number of new references,
	// just delete the current ones and set the new ones.
	if len(oldRefs) != refsLen {
		p.ResourceReferences[kind] = refs
		return true
	}

	// Determine if we need to update the refs.
	for _, uid := range refs {
		if !Contains(oldRefs, uid) {
			p.ResourceReferences[kind] = refs
			return true
		}
	}

	return false
}

// ToEvents returns a slice containing Event based on the internal state of the PodResource.
func (p *PodResource) ToEvents() []Event {
	evts := make([]Event, 3)

	if len(p.AddedFor) != 0 {
		evts[0] = &GenericEvent{
			Reason:           Added,
			Metadata:         p.Metadata.DeepCopy(),
			DestinationNodes: p.AddedFor,
			References:       p.ResourceReferences.ToFlatMap(),
		}
	}

	if len(p.ModifiedFor) != 0 {
		evts[1] = &GenericEvent{
			Reason:           Modified,
			Metadata:         p.Metadata.DeepCopy(),
			DestinationNodes: p.ModifiedFor,
			References:       p.ResourceReferences.ToFlatMap(),
		}
	}

	if len(p.DeletedFor) != 0 {
		evts[2] = &GenericEvent{
			Reason:           Deleted,
			Metadata:         p.Metadata.DeepCopy(),
			DestinationNodes: p.DeletedFor,
			References:       p.ResourceReferences.ToFlatMap(),
		}
	}

	return evts
}

// ToEvent returns an event based on the reason for the specified nodes.
func (p *PodResource) ToEvent(reason string, nodes []string) Event {
	var evt *GenericEvent
	switch reason {
	case Added:
		evt = &GenericEvent{
			Reason:           Added,
			Metadata:         p.Metadata.DeepCopy(),
			DestinationNodes: nodes,
			References:       p.ResourceReferences.ToFlatMap(),
		}
	case Modified:
		evt = &GenericEvent{
			Reason:           Modified,
			Metadata:         p.Metadata.DeepCopy(),
			DestinationNodes: nodes,
			References:       p.ResourceReferences.ToFlatMap(),
		}
	case Deleted:
		evt = &GenericEvent{
			Reason:           Deleted,
			Metadata:         p.Metadata.DeepCopy(),
			DestinationNodes: nodes,
			References:       p.ResourceReferences.ToFlatMap(),
		}
	default:
		evt = &GenericEvent{}
	}

	return evt
}
