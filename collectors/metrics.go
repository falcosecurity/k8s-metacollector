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
)

const (
	labelAdded      = "added"
	labelUpdated    = "modified"
	labelDeleted    = "deleted"
	labelCreate     = "Create"
	labelUpdate     = "Update"
	labelDelete     = "Delete"
	labelGeneric    = "Generic"
	apiServerSource = "api-server"
)

var (
	// EventTotal is a prometheus counter metrics which holds the total
	// number of events generated per collector. It has two labels. collector label refers
	// to the collector name and type label refers to the event type, i.e.
	// added, updated, deleted.
	eventTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "collector_event_total",
		Help: "Total number of events per collector",
	}, []string{"collector", "type"})

	// eventReceived is a prometheus counter metrics which holds the total
	// number of events received from the api server per collector. It has three labels. Collector label refers
	// to the collector name, source refers to the source from where we are receiving the events,
	// and type label refers to the event type, i.e. create, update, delete, generic.
	eventReceived = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "collector_event_received",
		Help: "Total number of events received from the api-server per collector",
	}, []string{"collector", "source", "type"})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(eventTotal)
	metrics.Registry.MustRegister(eventReceived)
}

// predicatesWithMetrics tracks the number of events received from the api-server.
func predicatesWithMetrics(collectorName, sourceName string, filter func(object client.Object) bool) predicate.Funcs {
	eventReceived.WithLabelValues(collectorName, sourceName, labelCreate).Add(0)
	eventReceived.WithLabelValues(collectorName, sourceName, labelDelete).Add(0)
	eventReceived.WithLabelValues(collectorName, sourceName, labelUpdate).Add(0)
	eventReceived.WithLabelValues(collectorName, sourceName, labelGeneric).Add(0)

	return predicate.Funcs{
		CreateFunc: func(event event.CreateEvent) bool {
			eventReceived.WithLabelValues(collectorName, sourceName, labelCreate).Inc()
			if filter != nil {
				return filter(event.Object)
			}
			return true
		},
		DeleteFunc: func(event event.DeleteEvent) bool {
			eventReceived.WithLabelValues(collectorName, sourceName, labelDelete).Inc()
			if filter != nil {
				return filter(event.Object)
			}
			return true
		},
		UpdateFunc: func(event event.UpdateEvent) bool {
			eventReceived.WithLabelValues(collectorName, sourceName, labelUpdate).Inc()
			if filter != nil {
				return filter(event.ObjectNew)
			}
			return true
		},
		GenericFunc: func(event event.GenericEvent) bool {
			eventReceived.WithLabelValues(collectorName, sourceName, labelGeneric).Inc()
			if filter != nil {
				return filter(event.Object)
			}
			return true
		},
	}
}
