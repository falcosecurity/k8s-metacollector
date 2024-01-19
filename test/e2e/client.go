// SPDX-License-Identifier: Apache-2.0
// Copyright 2024 The Falco Authors
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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/falcosecurity/k8s-metacollector/metadata"
	"github.com/falcosecurity/k8s-metacollector/pkg/resource"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type message struct {
	grpcEvents map[string]*metadata.Event
	rwLock     sync.RWMutex
}

func (m *message) Add(item *metadata.Event) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	m.grpcEvents[item.Uid] = item
}

func (m *message) Get(uid string) (*metadata.Event, bool) {
	m.rwLock.Lock()
	defer m.rwLock.Unlock()
	evt, ok := m.grpcEvents[uid]
	return evt, ok
}

func (m *message) NumMessagesForKind(kind string) int {
	m.rwLock.RLock()
	defer m.rwLock.RUnlock()
	num := 0
	for _, val := range m.grpcEvents {
		if val.Kind == kind {
			num++
		}
	}

	return num
}

func (m *message) NumMessages() int {
	m.rwLock.RLock()
	defer m.rwLock.RUnlock()

	return len(m.grpcEvents)
}

// Client is a client that subscribes to the metacollector.
type Client struct {
	nodeName string
	message
	metaClient metadata.MetadataClient
	connection *grpc.ClientConn
}

// NewClient returns a client.
func NewClient(nodeName, port string) (Client, error) {
	serverAddr := fmt.Sprintf("localhost:%s", port)
	grpcOpts := grpc.WithTransportCredentials(insecure.NewCredentials())
	conn, err := grpc.Dial(serverAddr, grpcOpts)
	if err != nil {
		return Client{}, fmt.Errorf("unable to create grpc connections: %w", err)
	}

	metaClient := metadata.NewMetadataClient(conn)

	return Client{
		nodeName: nodeName,
		message: message{
			grpcEvents: map[string]*metadata.Event{},
			rwLock:     sync.RWMutex{},
		},
		metaClient: metaClient,
		connection: conn,
	}, nil
}

// Watch subscribes to the collector.
func (c *Client) Watch(ctx context.Context) error {
	stream, err := c.metaClient.Watch(ctx, &metadata.Selector{
		NodeName: c.nodeName,
		ResourceKinds: map[string]string{
			resource.Daemonset:             "",
			resource.Namespace:             "",
			resource.Service:               "",
			resource.Pod:                   "",
			resource.ReplicaSet:            "",
			resource.Deployment:            "",
			resource.ReplicationController: "",
		},
	})

	if err != nil {
		return fmt.Errorf("an error occurred while performing the watch procedure")
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				fmt.Println("context canceled, exiting client")
				return
			default:
				in, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					fmt.Println("received EOF, exiting watch routine")
					return
				}

				if err != nil {
					fmt.Printf("an error occurred while receiving events: %s\n", err)
					return
				}
				c.Add(in)
			}
		}
	}()

	return nil
}
