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
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

const (
	labelAdded   = "added"
	labelUpdated = "modified"
	labelDeleted = "deleted"
	labelChannel = "pipeline"
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

	// EventLatency is a prometheus metric which keeps track of the duration
	// of sending events from collectors to the message broker.
	EventLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "event_latency",
		Help:    "How long in seconds an event stays in the channel before being requested.",
		Buckets: prometheus.ExponentialBuckets(10e-9, 10, 10),
	}, []string{"event"})

	eventSent = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "event_sent_total",
		Help: "Total number of events sent to message broker",
	}, []string{"event"})
)

func init() {
	// Register custom metrics with the global prometheus registry
	metrics.Registry.MustRegister(eventTotal)
	metrics.Registry.MustRegister(EventLatency)
	metrics.Registry.MustRegister(eventSent)
}

// ChannelMetrics holds the metrics related to the events sent in a given channel.
type ChannelMetrics struct {
	lock sync.Mutex
	// Total number of events sent in the channel
	sents *prometheus.CounterVec
	// how long an item stays in the channel
	latency *prometheus.HistogramVec

	sentTimes map[interface{}]time.Time
}

// NewChannelMetrics returns a new ChannelMetrics ready to be used.
func NewChannelMetrics() *ChannelMetrics {
	eventSent.WithLabelValues(labelChannel).Add(0)
	return &ChannelMetrics{
		sents:     eventSent,
		latency:   EventLatency,
		sentTimes: make(map[interface{}]time.Time),
	}
}

// Send to be called before sending an item to the given channel.
func (cm *ChannelMetrics) Send(evt interface{}) {
	if cm == nil {
		return
	}
	cm.lock.Lock()
	defer cm.lock.Unlock()
	cm.sents.WithLabelValues(labelChannel).Inc()

	if _, ok := cm.sentTimes[evt]; !ok {
		cm.sentTimes[evt] = time.Now()
	}
}

// Receive to be called after the item has been received on the given channel.
func (cm *ChannelMetrics) Receive(evt interface{}) {
	if cm == nil {
		return
	}
	cm.lock.Lock()
	defer cm.lock.Unlock()

	if startTime, ok := cm.sentTimes[evt]; ok {
		cm.latency.WithLabelValues(labelChannel).Observe(time.Since(startTime).Seconds())
		delete(cm.sentTimes, evt)
	}
}
