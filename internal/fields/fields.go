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

package fields

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Metadata fields to collect from resources.
type Metadata struct {
	uid       types.UID
	name      string
	namespace string
	labels    map[string]string
}

// Set populates the metadata fields.
func (m *Metadata) Set(meta *metav1.ObjectMeta) {
	m.uid = meta.UID
	m.name = meta.Name
	m.namespace = meta.Namespace
	m.labels = meta.Labels
}

// UID returns the UID.
func (m *Metadata) UID() types.UID {
	return m.uid
}

// Name returns the name.
func (m *Metadata) Name() string {
	return m.name
}

// Namespace returns the namespace.
func (m *Metadata) Namespace() string {
	return m.namespace
}

// Labels returns the labels.
func (m *Metadata) Labels() map[string]string {
	return m.labels
}

// UpdateLabels updates the labels.
func (m *Metadata) UpdateLabels(l map[string]string) bool {
	if !reflect.DeepEqual(m.labels, l) {
		m.labels = l
		return true
	}

	return false
}

// References custom type for a resource's references.
type References map[string][]types.UID

// Nodes custom type for nodes to which a given event need to be sent.
type Nodes map[string]struct{}
