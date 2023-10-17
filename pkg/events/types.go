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
//
//nolint:govet //needed for hashing.
type Resource struct {
	Kind   string
	UID    string
	Meta   string
	Spec   string
	Status string
	// Only used when storing metadata for pods.
	ResourceReferences fields.References
	// Tracks the nodes to which we have already sent the resource.
	subs fields.Subscribers `hash: "ignore"`
	// State fields that are needed to track the nodes to which we need to send
	// the resource. This fields thought to be used by the methods of the Resource.
	createdFor fields.Subscribers `hash:"ignore"`
	updatedFor fields.Subscribers `hash:"ignore"`
	deletedFor fields.Subscribers `hash:"ignore"`
	updated    bool               `hash:"ignore"`
}

// NewResource returns a new Resource.
func NewResource(kind, uid string) *Resource {
	return &Resource{
		Kind: kind,
		UID:  uid,
		subs: make(fields.Subscribers),
	}
}

// GetMetadata returns the metadata field.
func (g *Resource) GetMetadata() string {
	return g.Meta
}

// GetSpec returns the Spec field.
func (g *Resource) GetSpec() string {
	return g.Spec
}

// GetStatus returns the Status field.
func (g *Resource) GetStatus() string {
	return g.Status
}

// SetMeta sets the Meta field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *Resource) SetMeta(meta string) {
	g.Meta = meta
}

// SetSpec sets the Spec field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *Resource) SetSpec(spec string) {
	g.Spec = spec
}

// SetStatus sets the Status field if different from the existing one.
// It also sets to true the "updated" internal variable.
func (g *Resource) SetStatus(status string) {
	g.Status = status
}

// SetUpdate sets the update flag for the resource.
func (g *Resource) SetUpdate(update bool) {
	g.updated = update
}

// GenerateSubscribers populates the subscribers to which the events need to be sent, starting from the given subscribers.
// The starting point are the cached subscribers to which we already know that we have sent at least a "Create" event. From the
// cached subscribers and the passed ones it generates the subscribers to which we need to send an event and saves them in the appropriate
// field, i.e. Resource.createdFor, Resource.updatedFor and Resource.deletedFor. At the same time the Resource.nodes field is updated
// based on the new added or deleted nodes.
func (g *Resource) GenerateSubscribers(subs fields.Subscribers) fields.Subscribers {
	// Compute the subscribers to which we need to send a Create event.
	g.createdFor = subs.Difference(g.subs)
	// Compute the subscribers to which we need to send an Update event.
	if g.updated {
		g.updatedFor = subs.Intersect(g.subs)
	}
	// Compute the subscribers to which we need to send a Delete event.
	g.deletedFor = g.subs.Difference(subs)

	// Return the final subscribers for this resource.
	// Meaning the subscribers that have received or will receive an event for the
	// resource.
	return subs
}

// GetSubscribers returns the nodes.
func (g *Resource) GetSubscribers() fields.Subscribers {
	return g.subs
}

// SetSubscribers populates the nodes to which send the event.
func (g *Resource) SetSubscribers(subs fields.Subscribers) {
	g.subs = subs
}

// GetResourceReferences returns refs.
func (g *Resource) GetResourceReferences() fields.References {
	return g.ResourceReferences
}

// AddReferencesForKind adds references for a given Kind in the pod event.
func (g *Resource) AddReferencesForKind(kind string, refs []fields.Reference) {
	if g.ResourceReferences == nil {
		g.ResourceReferences = make(map[string][]fields.Reference)
	}
	g.ResourceReferences[kind] = refs
}

// ToEvents returns a slice containing Event based on the internal state of the Resource.
func (g *Resource) ToEvents() []Event {
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

	if len(g.createdFor) != 0 {
		evts[0] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Create,
				Uid:    g.UID,
				Kind:   g.Kind,
				Meta:   meta,
				Spec:   spec,
				Status: status,
				Refs:   g.grpcRefs(),
			},
			Subs: g.createdFor,
		}
		g.createdFor = nil
	}

	if len(g.updatedFor) != 0 {
		evts[1] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Update,
				Uid:    g.UID,
				Kind:   g.Kind,
				Meta:   meta,
				Spec:   spec,
				Status: status,
				Refs:   g.grpcRefs(),
			},
			Subs: g.updatedFor,
		}
		g.updatedFor = nil
	}

	if len(g.deletedFor) != 0 {
		evts[2] = &GenericEvent{
			Event: &metadata.Event{
				Reason: Delete,
				Uid:    g.UID,
				Kind:   g.Kind,
			},
			Subs: g.deletedFor,
		}
		g.deletedFor = nil
	}

	return evts
}

func (g *Resource) grpcRefs() *metadata.References {
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
