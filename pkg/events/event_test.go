// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 The Falco Authors
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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/falcosecurity/k8s-metacollector/metadata"
	"github.com/falcosecurity/k8s-metacollector/pkg/fields"
)

func TestEventAccessors(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		kind   string
		subs   fields.Subscribers
	}{
		{name: "create", reason: Create, kind: "Pod", subs: fields.Subscribers{"node-1": {}}},
		{name: "update", reason: Update, kind: "Deployment", subs: fields.Subscribers{"node-1": {}, "node-2": {}}},
		{name: "delete", reason: Delete, kind: "Service", subs: fields.Subscribers{"node-2": {}}},
		{name: "no subscribers", reason: Create, kind: "Namespace", subs: nil},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			msg := &metadata.Event{
				Reason: test.reason,
				Uid:    "uid-1",
				Kind:   test.kind,
				Meta:   new(`{"name":"nginx"}`),
			}
			evt := &Event{Event: msg, Subs: test.subs}

			require.Equal(t, test.reason, evt.Type())
			require.Equal(t, test.kind, evt.ResourceKind())
			require.Equal(t, test.subs, evt.Subscribers())
			require.Same(t, msg, evt.GRPCMessage())

			str := evt.String()
			require.Contains(t, str, `"`+test.kind+`"`)
			require.Contains(t, str, `"`+test.reason+`"`)
			require.Contains(t, str, "nginx")
			for sub := range test.subs {
				require.Contains(t, str, sub)
			}
		})
	}
}
