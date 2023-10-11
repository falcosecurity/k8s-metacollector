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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/alacuku/k8s-metadata/pkg/consts"
)

const (
	collectorSubsystem = "collector"
	eventReceivedKey   = "event_api_server_received"

	labelCreate  = "create"
	labelUpdate  = "update"
	labelDelete  = "delete"
	labelGeneric = "generic"

	apiServerSource = "api-server"
)

var (
	// ingestedEvents is a prometheus counter metrics which holds the total
	// number of events received from the api server/external sources per collector. It has three labels. Name label refers
	// to the collector name, source refers to the source from where we are receiving the events,
	// and type label refers to the event type, i.e. create, update, delete, generic.
	ingestedEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: consts.MetricsNamespace,
		Subsystem: collectorSubsystem,
		Name:      eventReceivedKey,
		Help: "Total number of events received from the api-server per collector  Name label refers to the collector" +
			" name, source refers to the source from where we are receiving the events,and type label refers to the" +
			" event type, i.e. create, update, delete, generic.",
	}, []string{"name", "source", "type"})
)

func init() {
	// Register custom metrics with the global prometheus registry

	metrics.Registry.MustRegister(ingestedEvents)
}

// predicatesWithMetrics tracks the number of events received from the api-server.
func predicatesWithMetrics(collectorName, sourceName string, filter func(object client.Object) bool) predicate.Funcs {
	createCounter := ingestedEvents.WithLabelValues(collectorName, sourceName, labelCreate)
	createCounter.Add(0)
	updateCounter := ingestedEvents.WithLabelValues(collectorName, sourceName, labelDelete)
	updateCounter.Add(0)
	deleteCounter := ingestedEvents.WithLabelValues(collectorName, sourceName, labelUpdate)
	deleteCounter.Add(0)
	genericCounter := ingestedEvents.WithLabelValues(collectorName, sourceName, labelGeneric)
	genericCounter.Add(0)

	return predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			createCounter.Inc()
			if filter != nil {
				return filter(event.Object)
			}
			return true
		},
		DeleteFunc: func(event event.DeleteEvent) bool {
			deleteCounter.Inc()
			if filter != nil {
				return filter(event.Object)
			}
			return true
		},
		UpdateFunc: func(event event.UpdateEvent) bool {
			updateCounter.Inc()
			if filter != nil {
				return filter(event.ObjectNew)
			}
			return true
		},
		GenericFunc: func(event event.GenericEvent) bool {
			genericCounter.Inc()
			if filter != nil {
				return filter(event.Object)
			}
			return true
		},
	}
}
