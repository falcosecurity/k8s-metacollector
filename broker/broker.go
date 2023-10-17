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
	"google.golang.org/grpc/credentials"

	"github.com/alacuku/k8s-metadata/metadata"
	"github.com/alacuku/k8s-metadata/pkg/events"
	"github.com/alacuku/k8s-metadata/pkg/subscriber"
)

// Broker receives events from the collectors and sends them to the subscribers.
type Broker struct {
	queue         Queue
	subscribers   *sync.Map
	logger        logr.Logger
	server        *grpc.Server
	connectionsWg *sync.WaitGroup
	opt           options
	eventMetrics  map[string]dispatchedEventsMetrics
}

// New returns a new Broker.
func New(logger logr.Logger, queue Queue, collectors map[string]subscriber.SubsChan, opt ...Option) (*Broker, error) {
	var grpcServer *grpc.Server
	subs := &sync.Map{}
	group := &sync.WaitGroup{}

	// Apply options received from the flags.
	opts := options{}
	for _, o := range opt {
		o(&opts)
	}

	// They should be both set, but here we prefer to return an error so the user knows that one of the paths
	// is missing rather than explicitly validating the file paths.
	if opts.tlsServerKeyFilePath != "" || opts.tlsServerCertFilePath != "" {
		creds, err := credentials.NewServerTLSFromFile(opts.tlsServerCertFilePath, opts.tlsServerKeyFilePath)
		if err != nil {
			logger.Error(err, "unable to create credentials from provided files",
				"certFilePath", opts.tlsServerCertFilePath, "keyFilePAth", opts.tlsServerKeyFilePath)
			return nil, err
		}

		grpcServer = grpc.NewServer(grpc.Creds(creds))
	} else {
		grpcServer = grpc.NewServer()
	}

	// Register grpc server.
	metadata.RegisterMetadataServer(grpcServer, metadata.New(logger.WithName("grpc-server"), subs, collectors, group))

	// Create the metrics for each running collector.
	// The name of the collector is the resource kind. Same as the kind we find
	// in the events.
	eventMetrics := make(map[string]dispatchedEventsMetrics, len(collectors))
	for collector := range collectors {
		eventMetrics[collector] = newDispatchedEventsMetrics(collector)
	}

	return &Broker{
		queue:         queue,
		subscribers:   subs,
		logger:        logger,
		server:        grpcServer,
		connectionsWg: group,
		opt:           opts,
		eventMetrics:  eventMetrics,
	}, nil
}

// Start starts the grpc server and sends to subscribers the events received from the collectors.
func (br *Broker) Start(ctx context.Context) error {
	br.logger.Info("starting grpc server", "addr", br.opt.address)
	// Start the grpc server.
	lis, err := net.Listen("tcp", br.opt.address)
	if err != nil {
		return fmt.Errorf("an error occurred whil creating listener for grpc server: %w", err)
	}

	serverError := make(chan error)
	go func() {
		serverError <- br.server.Serve(lis)
	}()
	go func() {
		for {
			evt := br.queue.Pop(ctx)

			if evt == nil {
				break
			}

			br.logger.V(7).Info("received event", "event:", evt.String())

			for sub := range evt.Subscribers() {
				// Get the grpc stream for the subscriber.
				c, ok := br.subscribers.Load(sub)
				if !ok {
					continue
				}
				con, ok := c.(metadata.Connection)
				if !ok {
					br.logger.Error(fmt.Errorf("failed to cast subscriber connection %T", con), "subscriber", sub)
					continue
				}
				if err := con.Stream.Send(evt.GRPCMessage()); err != nil {
					con.Close(err)
				}
				br.eventMetricsHandler(evt)
			}
		}
	}()

	select {
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

func (br *Broker) eventMetricsHandler(evt events.Event) {
	// Get the correct counter.
	c := br.eventMetrics[evt.ResourceKind()]
	c.inc(evt)
}
