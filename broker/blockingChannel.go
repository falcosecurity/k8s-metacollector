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

	"github.com/falcosecurity/k8s-metacollector/pkg/events"
)

// BlockingChannel implements the Queue interface using a channel.
type BlockingChannel struct {
	channel        chan events.Interface
	metricsHandler *metrics
}

// NewBlockingChannel returns a BlockingChannel.
func NewBlockingChannel(bufferLen int) *BlockingChannel {
	return &BlockingChannel{
		channel:        make(chan events.Interface, bufferLen),
		metricsHandler: newMetrics("blockingChannel"),
	}
}

// Push pushes an event to the queue. It gives up when the context is canceled: at shutdown the broker stops
// popping from the queue, and a plain send would block the caller forever. The collectors pass their reconcile
// context, which lasts until shutdown as long as no ReconciliationTimeout is set: otherwise events would be dropped.
func (bc *BlockingChannel) Push(ctx context.Context, evt events.Interface) {
	// Recorded before the send: the broker may pop the event, and look up its send time, right after it.
	bc.metricsHandler.send(evt)
	select {
	case bc.channel <- evt:
	case <-ctx.Done():
	}
}

// Pop an event from the queue.
func (bc *BlockingChannel) Pop(ctx context.Context) events.Interface {
	select {
	case evt := <-bc.channel:
		bc.metricsHandler.receive(evt)
		return evt
	case <-ctx.Done():
		return nil
	}
}
