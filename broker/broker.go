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
	"fmt"
	"net"
	"sync"

	"github.com/go-logr/logr"
	"google.golang.org/grpc"

	"github.com/alacuku/k8s-metadata/internal/events"
	"github.com/alacuku/k8s-metadata/metadata"
)

// Broker receives events from the collectors and sends them to the subscribers.
type Broker struct {
	queue         <-chan events.Event
	subscribers   *sync.Map
	logger        logr.Logger
	server        *grpc.Server
	connectionsWg *sync.WaitGroup
}

// New returns a new Broker.
func New(logger logr.Logger, queue <-chan events.Event, collectors map[string]chan<- string) (*Broker, error) {
	subs := &sync.Map{}
	group := &sync.WaitGroup{}
	grpcServer := grpc.NewServer()
	metadata.RegisterMetadataServer(grpcServer, metadata.New(logger.WithName("grpc-server"), subs, collectors, group))

	return &Broker{
		queue:         queue,
		subscribers:   subs,
		logger:        logger,
		server:        grpcServer,
		connectionsWg: group,
	}, nil
}

// Start starts the grpc server and sends to subscribers the events received from the collectors.
func (br *Broker) Start(ctx context.Context) error {
	br.logger.Info("starting grpc server")
	// Start the grpc server.
	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 45000))
	if err != nil {
		return fmt.Errorf("an error occurred whil creating listener for grpc server: %w", err)
	}

	br.logger.Info("starting", "grpc", "server")

	serverError := make(chan error)
	go func() {
		serverError <- br.server.Serve(lis)
	}()

	for {
		select {
		case evt := <-br.queue:
			for _, node := range evt.Nodes() {
				// Get the grpc stream for the subscriber.
				c, ok := br.subscribers.Load(node)
				if !ok {
					br.logger.Info("no stream found for", "node", node)
					break
				}
				con, ok := c.(metadata.Connection)
				if !ok {
					br.logger.Error(fmt.Errorf("failed to cast subscriber connection %T", con), "node", node)
					break
				}

				meta := &metadata.Event{
					Reason: evt.Type(),
					Metadata: &metadata.Fields{
						Uid:       string(evt.GetMetadata().UID()),
						Kind:      evt.GetMetadata().Kind(),
						Name:      evt.GetMetadata().Name(),
						Namespace: evt.GetMetadata().Namespace(),
						Labels:    evt.GetMetadata().Labels(),
					},
					Refs: &metadata.References{Refs: evt.Refs()},
				}
				if err := con.Stream.Send(meta); err != nil {
					con.Error <- err
				}
			}
			// Wait for the context to be canceled. In that case we gracefully stop the broker.
		case <-ctx.Done():
			br.logger.Info("Shutdown signal received, waiting for grpc connections to close")
			br.server.Stop()
			br.connectionsWg.Wait()
			br.logger.Info("All grpc connections closed")
			return nil
		// If the grpc server errors, the error is returned and the manager is stopped causing the application to exit.
		case err := <-serverError:
			br.logger.Error(err, "grpc server failed to start")
			return err
		}
	}
}
