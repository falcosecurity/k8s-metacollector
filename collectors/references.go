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

type references map[string]map[string]struct{}

func (r references) Add(key, value string) bool {
	nodes, ok := r[key]
	if !ok {
		m := make(map[string]struct{})
		m[value] = struct{}{}
		r[key] = m
		return true
	}

	if _, ok := nodes[value]; !ok {
		nodes[value] = struct{}{}
		return true
	}

	return false
}

func (r references) Delete(key, value string) bool {
	nodes, ok := r[key]
	if !ok {
		return false
	}

	if _, ok = nodes[value]; ok {
		delete(nodes, value)
		if len(nodes) == 0 {
			delete(r, key)
		}
		return true
	}

	return false
}
