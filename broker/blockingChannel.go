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

package broker

import (
	"context"

	"github.com/alacuku/k8s-metadata/pkg/events"
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

// Push pushes an event to the queue.
func (bc *BlockingChannel) Push(evt events.Interface) {
	bc.metricsHandler.send(evt)
	bc.channel <- evt
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
