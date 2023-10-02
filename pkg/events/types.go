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

// GenericResource event that holds metadata fields for k8s resources.
type GenericResource struct {
	Kind string
	UID  string
	// Only used when storing metadata for pods.
	Meta               string
	Status             string
	Spec               string
	ResourceReferences fields.References
	// Tracks the nodes to which we have already sent the resource.
	Nodes fields.Nodes
	// State fields that are needed to track the nodes to which we need to send
	// the resource. This fields thought to be used by the methods of the GenericResource.
	addedFor    []string
	modifiedFor []string
	deletedFor  []string
	updated     bool
}

// NewGenericResource returns a ne GenericResource.
func NewGenericResource(kind, uid string) *GenericResource {
	return &GenericResource{
		Kind:  kind,
		UID:   uid,
		Nodes: make(fields.Nodes),
	}
}

// GetMetadata returns the metadata field.
func (g *GenericResource) GetMetadata() string {
	return g.Meta
}

// GetSpec returns the spec field.
func (g *GenericResource) GetSpec() string {
	return g.Spec
}

// GetStatus returns the status field.
func (g *GenericResource) GetStatus() string {
	return g.Status
}

// SetMeta sets the meta field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *GenericResource) SetMeta(meta string) {
	if g.Meta != meta {
		g.Meta = meta
		g.updated = true
	}
}

// SetSpec sets the spec field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *GenericResource) SetSpec(spec string) {
	if g.Spec != spec {
		g.Spec = spec
		g.updated = true
	}
}

// SetStatus sets the status field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *GenericResource) SetStatus(status string) {
	if g.Status != status {
		g.Status = status
		g.updated = true
	}
}

// SetCreatedFor sets the nodes for which this resource needs to generate an "Added" event.
func (g *GenericResource) SetCreatedFor(nodes []string) {
	g.addedFor = nodes
}

// SetModifiedFor sets the nodes for which this resource needs to generate a "Modified" event.
func (g *GenericResource) SetModifiedFor(nodes []string) {
	g.modifiedFor = nodes
}

// SetDeletedFor sets the nodes for which this resource need to generate a "Deleted" event.
func (g *GenericResource) SetDeletedFor(nodes []string) {
	g.deletedFor = nodes
}

// AddNodes populates the nodes to which the events need to be sent, starting from the given nodes and the provided flag.
// The starting point is the cached nodes to which we already know that we have sent at least an "Added" event. From the
// cached nodes and the passed ones it generates the nodes to which we need to send an event and saves them in the appropriate
// field, i.e. GenericResource.addedFor, GenericResource.modifiedFor and GenericResource.deletedFor. Note that the updated
// flag is set to true if the mutable fields for the resource has been updated. At the same time the GenericResource.Nodes field is updated
// based on the new added or deleted nodes.
func (g *GenericResource) AddNodes(nodes []string) {
	var added, modified, deleted []string
	tmpNodes := make(fields.Nodes, len(nodes))

	for _, n := range nodes {
		tmpNodes[n] = struct{}{}
		if _, ok := g.Nodes[n]; !ok {
			g.Nodes[n] = struct{}{}
			added = append(added, n)
		} else if g.updated {
			modified = append(modified, n)
		}
	}

	for n := range g.Nodes {
		if _, ok := tmpNodes[n]; !ok {
			deleted = append(deleted, n)
			delete(g.Nodes, n)
		}
	}

	g.addedFor = added
	if g.updated {
		g.modifiedFor = modified
	}
	g.deletedFor = deleted

	g.updated = false
}

// DeleteNodes populates the GenericResource.deletedFor field with the passed nodes.
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

	g.deletedFor = deleted
}

// SetNodes populates the nodes to which send the event.
func (g *GenericResource) SetNodes(nodes fields.Nodes) {
	g.Nodes = nodes
}

// AddReferencesForKind adds references for a given kind in the pod event.
func (g *GenericResource) AddReferencesForKind(kind string, refs []fields.Reference) {
	if g.ResourceReferences == nil {
		g.ResourceReferences = make(map[string][]fields.Reference)
	}

	refsLen := len(refs)

	// Check if we already have references for the given resource kind.
	oldRefs, ok := g.ResourceReferences[kind]
	// If no refs found and the new refs are not empty then set them for the given resource kind.
	if !ok && refsLen != 0 {
		g.ResourceReferences[kind] = refs
		g.updated = true
		return
	}

	// If the passed refs have length 0 for the given resource kind, which is acceptable then we delete the current one.
	if refsLen == 0 && len(oldRefs) != 0 {
		delete(g.ResourceReferences, kind)
		g.updated = true
		return
	}

	// If the number of old references is not equal to the number of new references,
	// just delete the current ones and set the new ones.
	if len(oldRefs) != refsLen {
		g.ResourceReferences[kind] = refs
		g.updated = true
		return
	}

	// Determine if we need to update the refs.
	for _, uid := range refs {
		if !Contains(oldRefs, uid) {
			g.ResourceReferences[kind] = refs
			g.updated = true
			return
		}
	}
}

// ToEvents returns a slice containing Event based on the internal state of the PodResource.
func (g *GenericResource) ToEvents() []Event {
	evts := make([]Event, 3)
	var meta, spec, status *string
	if g.Meta != "" {
		m := g.Meta
		meta = &m
	}
	if g.Spec != "" {
		s := g.Spec
		spec = &s
	}
	if g.Status != "" {
		s := g.Status
		status = &s
	}

	if len(g.addedFor) != 0 {
		evts[0] = &GenericEvent{
			Event: &metadata.Event{
				Reason:   Added,
				Uid:      g.UID,
				Kind:     g.Kind,
				Metadata: meta,
				Spec:     spec,
				Status:   status,
				Refs:     g.grpcRefs(),
			},
			DestinationNodes: g.addedFor,
		}
	}

	if len(g.modifiedFor) != 0 {
		evts[1] = &GenericEvent{
			Event: &metadata.Event{
				Reason:   Modified,
				Uid:      g.UID,
				Kind:     g.Kind,
				Metadata: meta,
				Spec:     spec,
				Status:   status,
				Refs:     g.grpcRefs(),
			},
			DestinationNodes: g.modifiedFor,
		}
	}

	if len(g.deletedFor) != 0 {
		evts[2] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Deleted,
				Uid:    g.UID,
				Kind:   g.Kind,
			},
			DestinationNodes: g.deletedFor,
		}
	}

	return evts
}

// ToEvent returns an event based on the reason for the specified nodes.
func (g *GenericResource) ToEvent(reason string, nodes []string) Event {
	var evt *GenericEvent
	var meta, spec, status *string
	if g.Meta != "" {
		m := g.Meta
		meta = &m
	}
	if g.Spec != "" {
		s := g.Spec
		spec = &s
	}
	if g.Status != "" {
		s := g.Status
		status = &s
	}

	switch reason {
	case Added:
		evt = &GenericEvent{
			Event: &metadata.Event{
				Reason:   Added,
				Uid:      g.UID,
				Kind:     g.Kind,
				Metadata: meta,
				Spec:     spec,
				Status:   status,
				Refs:     g.grpcRefs(),
			},
			DestinationNodes: nodes,
		}
	case Modified:
		evt = &GenericEvent{
			Event: &metadata.Event{
				Reason:   Modified,
				Uid:      g.UID,
				Kind:     g.Kind,
				Metadata: meta,
				Spec:     spec,
				Status:   status,
				Refs:     g.grpcRefs(),
			},
			DestinationNodes: nodes,
		}
	case Deleted:
		evt = &GenericEvent{
			Event: &metadata.Event{
				Reason: Deleted,
				Uid:    g.UID,
				Kind:   g.Kind,
			},
			DestinationNodes: nodes,
		}
	default:
		evt = &GenericEvent{}
	}

	return evt
}

func (g *GenericResource) grpcRefs() *metadata.References {
	if g.ResourceReferences != nil && g.Kind == resource.Pod {
		// Converting the references to grpc message format.
		grpcRefs := make(map[string]*metadata.ListOfStrings, len(g.ResourceReferences))
		refs := g.ResourceReferences.ToFlatMap()
		for k, val := range refs {
			grpcRefs[k] = &metadata.ListOfStrings{List: val}
		}
		return &metadata.References{Resources: grpcRefs}
	}

	return nil
}
