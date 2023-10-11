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
	"github.com/alacuku/k8s-metadata/metadata"
	"github.com/alacuku/k8s-metadata/pkg/fields"
	"github.com/alacuku/k8s-metadata/pkg/resource"
)

// Resource event that holds metadata fields for k8s resources.
type Resource struct {
	kind   string
	uid    string
	meta   string
	spec   string
	status string
	// Only used when storing metadata for pods.
	resourceReferences fields.References
	// Tracks the nodes to which we have already sent the resource.
	nodes fields.Nodes
	// State fields that are needed to track the nodes to which we need to send
	// the resource. This fields thought to be used by the methods of the Resource.
	addedFor    []string
	modifiedFor []string
	deletedFor  []string
	updated     bool
}

// NewResource returns a new Resource.
func NewResource(kind, uid string) *Resource {
	return &Resource{
		kind:  kind,
		uid:   uid,
		nodes: make(fields.Nodes),
	}
}

// GetMetadata returns the metadata field.
func (g *Resource) GetMetadata() string {
	return g.meta
}

// GetSpec returns the spec field.
func (g *Resource) GetSpec() string {
	return g.spec
}

// GetStatus returns the status field.
func (g *Resource) GetStatus() string {
	return g.status
}

// SetMeta sets the meta field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *Resource) SetMeta(meta string) {
	if g.meta != meta {
		g.meta = meta
		g.updated = true
	}
}

// SetSpec sets the spec field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *Resource) SetSpec(spec string) {
	if g.spec != spec {
		g.spec = spec
		g.updated = true
	}
}

// SetStatus sets the status field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *Resource) SetStatus(status string) {
	if g.status != status {
		g.status = status
		g.updated = true
	}
}

// SetCreatedFor sets the nodes for which this resource needs to generate an "Create" event.
func (g *Resource) SetCreatedFor(nodes []string) {
	g.addedFor = nodes
}

// SetModifiedFor sets the nodes for which this resource needs to generate a "Update" event.
func (g *Resource) SetModifiedFor(nodes []string) {
	g.modifiedFor = nodes
}

// SetDeletedFor sets the nodes for which this resource need to generate a "Delete" event.
func (g *Resource) SetDeletedFor(nodes []string) {
	g.deletedFor = nodes
}

// AddNodes populates the nodes to which the events need to be sent, starting from the given nodes and the provided flag.
// The starting point is the cached nodes to which we already know that we have sent at least an "Create" event. From the
// cached nodes and the passed ones it generates the nodes to which we need to send an event and saves them in the appropriate
// field, i.e. Resource.addedFor, Resource.modifiedFor and Resource.deletedFor. Note that the updated
// flag is set to true if the mutable fields for the resource has been updated. At the same time the Resource.nodes field is updated
// based on the new added or deleted nodes.
func (g *Resource) AddNodes(nodes []string) {
	var added, modified, deleted []string
	tmpNodes := make(fields.Nodes, len(nodes))

	for _, n := range nodes {
		tmpNodes[n] = struct{}{}
		if _, ok := g.nodes[n]; !ok {
			g.nodes[n] = struct{}{}
			added = append(added, n)
		} else if g.updated {
			modified = append(modified, n)
		}
	}

	for n := range g.nodes {
		if _, ok := tmpNodes[n]; !ok {
			deleted = append(deleted, n)
			delete(g.nodes, n)
		}
	}

	g.addedFor = added
	if g.updated {
		g.modifiedFor = modified
	}
	g.deletedFor = deleted

	g.updated = false
}

// DeleteNodes populates the Resource.deletedFor field with the passed nodes.
// At the same time the Resource.nodes field is updated based on the new added
// or deleted nodes.
func (g *Resource) DeleteNodes(nodes []string) {
	var deleted []string

	for _, n := range nodes {
		if _, ok := g.nodes[n]; ok {
			deleted = append(deleted, n)
			delete(g.nodes, n)
		}
	}

	g.deletedFor = deleted
}

// GetNodes returns the nodes.
func (g *Resource) GetNodes() fields.Nodes {
	return g.nodes
}

// SetNodes populates the nodes to which send the event.
func (g *Resource) SetNodes(nodes fields.Nodes) {
	g.nodes = nodes
}

// GetResourceReferences returns refs.
func (g *Resource) GetResourceReferences() fields.References {
	return g.resourceReferences
}

// AddReferencesForKind adds references for a given kind in the pod event.
func (g *Resource) AddReferencesForKind(kind string, refs []fields.Reference) {
	if g.resourceReferences == nil {
		g.resourceReferences = make(map[string][]fields.Reference)
	}

	refsLen := len(refs)

	// Check if we already have references for the given resource kind.
	oldRefs, ok := g.resourceReferences[kind]
	// If no refs found and the new refs are not empty then set them for the given resource kind.
	if !ok && refsLen != 0 {
		g.resourceReferences[kind] = refs
		g.updated = true
		return
	}

	// If the passed refs have length 0 for the given resource kind, which is acceptable then we delete the current one.
	if refsLen == 0 && len(oldRefs) != 0 {
		delete(g.resourceReferences, kind)
		g.updated = true
		return
	}

	// If the number of old references is not equal to the number of new references,
	// just delete the current ones and set the new ones.
	if len(oldRefs) != refsLen {
		g.resourceReferences[kind] = refs
		g.updated = true
		return
	}

	// Determine if we need to update the refs.
	for _, uid := range refs {
		if !Contains(oldRefs, uid) {
			g.resourceReferences[kind] = refs
			g.updated = true
			return
		}
	}
}

// ToEvents returns a slice containing Event based on the internal state of the PodResource.
func (g *Resource) ToEvents() []Event {
	evts := make([]Event, 3)
	var meta, spec, status *string
	if g.meta != "" {
		m := g.meta
		meta = &m
	}
	if g.spec != "" {
		s := g.spec
		spec = &s
	}
	if g.status != "" {
		s := g.status
		status = &s
	}

	if len(g.addedFor) != 0 {
		evts[0] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Create,
				Uid:    g.uid,
				Kind:   g.kind,
				Meta:   meta,
				Spec:   spec,
				Status: status,
				Refs:   g.grpcRefs(),
			},
			DestinationNodes: g.addedFor,
		}
		g.addedFor = nil
	}

	if len(g.modifiedFor) != 0 {
		evts[1] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Update,
				Uid:    g.uid,
				Kind:   g.kind,
				Meta:   meta,
				Spec:   spec,
				Status: status,
				Refs:   g.grpcRefs(),
			},
			DestinationNodes: g.modifiedFor,
		}
		g.modifiedFor = nil
	}

	if len(g.deletedFor) != 0 {
		evts[2] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Delete,
				Uid:    g.uid,
				Kind:   g.kind,
			},
			DestinationNodes: g.deletedFor,
		}
		g.deletedFor = nil
	}

	return evts
}

// ToEvent returns an event based on the reason for the specified nodes.
func (g *Resource) ToEvent(reason string, nodes []string) Event {
	var evt *GenericEvent
	var meta, spec, status *string
	if g.meta != "" {
		m := g.meta
		meta = &m
	}
	if g.spec != "" {
		s := g.spec
		spec = &s
	}
	if g.status != "" {
		s := g.status
		status = &s
	}

	switch reason {
	case Create:
		evt = &GenericEvent{
			Event: &metadata.Event{
				Reason: Create,
				Uid:    g.uid,
				Kind:   g.kind,
				Meta:   meta,
				Spec:   spec,
				Status: status,
				Refs:   g.grpcRefs(),
			},
			DestinationNodes: nodes,
		}
	case Update:
		evt = &GenericEvent{
			Event: &metadata.Event{
				Reason: Update,
				Uid:    g.uid,
				Kind:   g.kind,
				Meta:   meta,
				Spec:   spec,
				Status: status,
				Refs:   g.grpcRefs(),
			},
			DestinationNodes: nodes,
		}
	case Delete:
		evt = &GenericEvent{
			Event: &metadata.Event{
				Reason: Delete,
				Uid:    g.uid,
				Kind:   g.kind,
			},
			DestinationNodes: nodes,
		}
	default:
		evt = &GenericEvent{}
	}

	return evt
}

func (g *Resource) grpcRefs() *metadata.References {
	if g.resourceReferences != nil && g.kind == resource.Pod {
		// Converting the references to grpc message format.
		grpcRefs := make(map[string]*metadata.ListOfStrings, len(g.resourceReferences))
		refs := g.resourceReferences.ToFlatMap()
		for k, val := range refs {
			grpcRefs[k] = &metadata.ListOfStrings{List: val}
		}
		return &metadata.References{Resources: grpcRefs}
	}

	return nil
}
