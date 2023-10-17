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

package broker

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/alacuku/k8s-metadata/pkg/consts"
	"github.com/alacuku/k8s-metadata/pkg/events"
)

const (
	brokerSubsystem     = "broker"
	queueLatencyKey     = "queue_duration_seconds"
	addsKey             = "queue_adds"
	dispatchedEventsKey = "dispatched_events"
)

var (
	// latency is a prometheus metric which keeps track of the duration
	// of sending events from collectors to the message broker.
	latency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: consts.MetricsNamespace,
		Subsystem: brokerSubsystem,
		Name:      queueLatencyKey,
		Help:      "How long in seconds an event stays in the queue before being requested.",
		Buckets:   prometheus.ExponentialBuckets(10e-9, 10, 10),
	}, []string{"name"})

	adds = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: consts.MetricsNamespace,
		Subsystem: brokerSubsystem,
		Name:      addsKey,
		Help:      "Total number of events handled by the queue",
	}, []string{"name", "type"})

	// dispatchedEvents is a prometheus counter metrics which holds the total
	// number of events generated per resource kind. It has two labels. kind label refers
	// to the resource kind and type label refers to the event type, i.e.
	// added, updated, deleted.
	dispatchedEvents = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: consts.MetricsNamespace,
		Subsystem: brokerSubsystem,
		Name:      dispatchedEventsKey,
		Help: "Total number of events generated per resource kind destined to subscribers. kind label refers to the " +
			"resource kind and type label refers to the event type, i.e. create, update, delete, generic",
	}, []string{"kind", "type"})
)

func init() {
	// Register custom metrics with the global prometheus registry.
	ctrlmetrics.Registry.MustRegister(latency)
	ctrlmetrics.Registry.MustRegister(adds)
	ctrlmetrics.Registry.MustRegister(dispatchedEvents)
}

// metrics holds the metrics related to queue. It tracks the number of produced events for each type of events.
// Also tracks the latency of the queue.
type metrics struct {
	sync.Mutex
	addCounter      prometheus.Counter
	updateCounter   prometheus.Counter
	deleteCounter   prometheus.Counter
	latencyObserver prometheus.Observer
	sentTimes       map[interface{}]time.Time
}

// newMetrics returns a new ChannelMetrics ready to be used.
func newMetrics(name string) *metrics {
	// Initialize counters.
	addCounter := adds.WithLabelValues(name, events.Create)
	addCounter.Add(0)
	updateCounter := adds.WithLabelValues(name, events.Update)
	updateCounter.Add(0)
	deleteCounter := adds.WithLabelValues(name, events.Delete)
	deleteCounter.Add(0)
	latencyObserver := latency.WithLabelValues(name)

	return &metrics{
		Mutex:           sync.Mutex{},
		addCounter:      addCounter,
		updateCounter:   updateCounter,
		deleteCounter:   deleteCounter,
		latencyObserver: latencyObserver,
		sentTimes:       make(map[interface{}]time.Time),
	}
}

// send to be called before adding the item to the queue.
func (m *metrics) send(evt events.Interface) {
	if m == nil {
		return
	}
	m.Lock()
	defer m.Unlock()

	switch evt.Type() {
	case events.Create:
		m.addCounter.Inc()
	case events.Update:
		m.updateCounter.Inc()
	case events.Delete:
		m.deleteCounter.Inc()
	}

	if _, ok := m.sentTimes[evt]; !ok {
		m.sentTimes[evt] = time.Now()
	}
}

// receive to be called after the item has been pooped from the queue.
func (m *metrics) receive(evt interface{}) {
	if m == nil {
		return
	}
	m.Lock()
	defer m.Unlock()

	if startTime, ok := m.sentTimes[evt]; ok {
		m.latencyObserver.Observe(time.Since(startTime).Seconds())
		delete(m.sentTimes, evt)
	}
}

type dispatchedEventsMetrics struct {
	createCounter prometheus.Counter
	updateCounter prometheus.Counter
	deleteCounter prometheus.Counter
}

func (dem *dispatchedEventsMetrics) inc(evt events.Interface) {
	switch evt.Type() {
	case events.Create:
		dem.createCounter.Inc()
	case events.Update:
		dem.updateCounter.Inc()
	case events.Delete:
		dem.deleteCounter.Inc()
	}
}

func newDispatchedEventsMetrics(name string) dispatchedEventsMetrics {
	createCounter := dispatchedEvents.WithLabelValues(name, events.Create)
	createCounter.Add(0)
	updateCounter := dispatchedEvents.WithLabelValues(name, events.Update)
	updateCounter.Add(0)
	deleteCounter := dispatchedEvents.WithLabelValues(name, events.Delete)
	deleteCounter.Add(0)

	return dispatchedEventsMetrics{
		createCounter: createCounter,
		updateCounter: updateCounter,
		deleteCounter: deleteCounter,
	}
}
