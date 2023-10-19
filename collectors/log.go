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

package collectors

import (
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type logConstructor func(request *reconcile.Request) logr.Logger

func newLogConstructor(log logr.Logger, name, resourceKind string) (logConstructor, error) {
	if log.GetSink() == nil {
		return nil, fmt.Errorf("unable to create the logConstructor, please provide a valid logger")
	}
	if name == "" {
		return nil, fmt.Errorf("unable to create the logConstructor, expected a non empty name for logger")
	}

	if resourceKind == "" {
		return nil, fmt.Errorf("unable to create the logConstructor, expected a non empty resource kind")
	}

	log = log.WithName(name)

	return func(req *reconcile.Request) logr.Logger {
		log := log
		if req != nil {
			log = log.WithValues(resourceKind, klog.KRef(req.Namespace, req.Name))
		}
		return log
	}, nil
}
