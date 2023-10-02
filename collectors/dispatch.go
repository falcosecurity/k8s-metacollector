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
	"context"
	"sync"

	"github.com/go-logr/logr"

	"github.com/alacuku/k8s-metadata/broker"
	"github.com/alacuku/k8s-metadata/pkg/events"
)

// Dispatch starts a go routing which waits for subscribers and dispatches the events to them. Events
// are taken from the cache and sent through the queue. It does not return until the context is closed.
func dispatch(ctx context.Context, logger logr.Logger,
	subChan <-chan string, queue broker.Queue, cache *events.GenericCache) error {
	wg := sync.WaitGroup{}
	// it listens for new subscribers and sends the cached events to the
	// subscriber received on the channel.
	dispatchEventsOnSubcribe := func(ctx context.Context) {
		wg.Add(1)
		for {
			select {
			case sub := <-subChan:
				logger.V(2).Info("Dispatching events", "subscriber", sub)
				dispatch := func(res *events.Resource) {
					// Check if the pod is related to the subscriber.
					nodes := res.GetNodes()
					if _, ok := nodes[sub]; ok {
						queue.Push(res.ToEvent(events.Added, []string{sub}))
					}
				}
				cache.ForEach(dispatch)
				logger.V(2).Info("Events correctly dispatched", "subscriber", sub)

			case <-ctx.Done():
				logger.V(2).Info("Stopping dispatcher on new subscribers")
				wg.Done()
				return
			}
		}
	}

	logger.Info("Starting event dispatcher for new subscribers")
	// Start the dispatcher.
	go dispatchEventsOnSubcribe(ctx)

	// Wait for shutdown signal.
	<-ctx.Done()
	logger.Info("Waiting for event dispatcher to finish")
	// Wait for goroutines to stop.
	wg.Wait()
	logger.Info("Dispatcher finished")

	return nil
}
