// SPDX-License-Identifier: Apache-2.0
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

type reason string

const (
	// Subscribed set by a subscriber in a message when it first connects.
	Subscribed reason = "Subscribed"
	// Unsubscribed set by a subscriber when it leaves.
	Unsubscribed reason = "Unsubscribed"
)

// Message sent by a subscriber to communicate its presence/absence.
type Message struct {
	// NodeName name of the node for which we want to receive the resources.
	// All the resources that are associated to that node will be sent to the
	// subscriber.
	NodeName string
	// UID identifies the subscriber.
	UID string
	// Reason of the message. If it is subscribing or unsubscribing.
	Reason reason
}

// SubsChan a channel used to communicate when new subscribers arrive or existing ones leave.
type SubsChan chan Message
