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

	"github.com/alacuku/k8s-metadata/metadata"
)

const (
	// Added possible type for an event.
	Added = "Added"
	// Modified possible type for an event.
	Modified = "Modified"
	// Deleted possible type for an event.
	Deleted = "Deleted"
)

// GenericEvent generated for watched kubernetes resources.
type GenericEvent struct {
	*metadata.Event
	DestinationNodes []string
}

// Nodes returns the destination nodes.
func (ge *GenericEvent) Nodes() []string {
	return ge.DestinationNodes
}

// String returns the event in string format.
func (ge *GenericEvent) String() string {
	return fmt.Sprintf("Resource kind %q, event type %q, resource name %q, namespace %q, destination nodes %q",
		ge.Metadata.Kind, ge.Reason, ge.Metadata.Name, ge.Metadata.Namespace, ge.Nodes())
}

// Type returns the event type.
func (ge *GenericEvent) Type() string {
	return ge.Reason
}

// GRPCMessage returns the grpc message ready to be sent over the grpc connection.
func (ge *GenericEvent) GRPCMessage() *metadata.Event {
	return ge.Event
}
