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

package broker

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/falcosecurity/k8s-metacollector/metadata"
	"github.com/falcosecurity/k8s-metacollector/pkg/events"
)

func newEvent(reason, uid string) *events.Event {
	return &events.Event{Event: &metadata.Event{Reason: reason, Uid: uid, Kind: "Pod"}}
}

func TestBlockingChannelFIFO(t *testing.T) {
	t.Parallel()

	bc := NewBlockingChannel(3)
	evts := []*events.Event{
		newEvent(events.Create, "1"),
		newEvent(events.Update, "2"),
		newEvent(events.Delete, "3"),
	}
	for _, evt := range evts {
		bc.Push(evt)
	}

	for _, evt := range evts {
		require.Same(t, evt, bc.Pop(t.Context()))
	}
}

func TestBlockingChannelPopReturnsNilOnCanceledContext(t *testing.T) {
	t.Parallel()

	bc := NewBlockingChannel(1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.Nil(t, bc.Pop(ctx))
}

func TestBlockingChannelPopWaitsForPush(t *testing.T) {
	t.Parallel()

	bc := NewBlockingChannel(0)
	evt := newEvent(events.Create, "1")

	got := make(chan events.Interface)
	go func() {
		got <- bc.Pop(t.Context())
	}()

	select {
	case <-got:
		t.Fatal("Pop returned before any event was pushed")
	case <-time.After(50 * time.Millisecond):
	}

	bc.Push(evt)
	select {
	case res := <-got:
		require.Same(t, evt, res)
	case <-time.After(5 * time.Second):
		t.Fatal("Pop did not return after Push")
	}
}

func TestBlockingChannelMetrics(t *testing.T) {
	tests := []struct {
		name   string
		reason string
		// want is the expected increment of the added events counter for each reason.
		want map[string]float64
	}{
		{name: "create", reason: events.Create, want: map[string]float64{events.Create: 1}},
		{name: "update", reason: events.Update, want: map[string]float64{events.Update: 1}},
		{name: "delete", reason: events.Delete, want: map[string]float64{events.Delete: 1}},
		{name: "unknown reason is not counted", reason: "Generic", want: map[string]float64{}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// The metrics are global and keyed by queue name, so use a name unique to each case
			// and compare deltas, since the counters survive repeated runs (-count).
			name := "test-blocking-channel-metrics-" + test.name
			bc := &BlockingChannel{
				channel:        make(chan events.Interface, 1),
				metricsHandler: newMetrics(name),
			}

			reasons := []string{events.Create, events.Update, events.Delete}
			added := func(reason string) float64 {
				return testutil.ToFloat64(adds.WithLabelValues(name, reason))
			}
			before := make(map[string]float64, len(reasons))
			for _, reason := range reasons {
				before[reason] = added(reason)
			}

			bc.Push(newEvent(test.reason, "1"))
			for _, reason := range reasons {
				require.InDelta(t, before[reason]+test.want[reason], added(reason), 0, "reason %s", reason)
			}

			require.NotNil(t, bc.Pop(t.Context()))

			// Once popped, events are no longer tracked for latency.
			bc.metricsHandler.Lock()
			defer bc.metricsHandler.Unlock()
			require.Empty(t, bc.metricsHandler.sentTimes)
		})
	}
}
