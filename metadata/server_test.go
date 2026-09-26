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

package metadata

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

const timeout = 5 * time.Second

// startServer starts a Server over an in-memory connection and returns a client connected to it.
func startServer(t *testing.T, collectors map[string]subscriber.SubsChan) (MetadataClient, *sync.Map) {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	subs := &sync.Map{}

	srv := grpc.NewServer()
	RegisterMetadataServer(srv, New(logr.Discard(), subs, collectors))
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return NewMetadataClient(conn), subs
}

func receiveMsg(t *testing.T, ch subscriber.SubsChan) subscriber.Message {
	t.Helper()

	select {
	case msg := <-ch:
		return msg
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for subscriber message")
		return subscriber.Message{}
	}
}

func loadConnection(t *testing.T, subs *sync.Map, uid string) Connection {
	t.Helper()

	c, ok := subs.Load(uid)
	require.True(t, ok, "connection for subscriber %q not found", uid)
	conn, ok := c.(Connection)
	require.True(t, ok)
	return conn
}

func TestWatchSubscribesAndUnsubscribes(t *testing.T) {
	t.Parallel()

	podChan := make(subscriber.SubsChan, 1)
	svcChan := make(subscriber.SubsChan, 1)
	client, subs := startServer(t, map[string]subscriber.SubsChan{
		"Pod":     podChan,
		"Service": svcChan,
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// Deployment has no collector and must be ignored.
	_, err := client.Watch(ctx, &Selector{
		NodeName:      "node-1",
		ResourceKinds: map[string]string{"Pod": "", "Deployment": ""},
	})
	require.NoError(t, err)

	msg := receiveMsg(t, podChan)
	require.Equal(t, "node-1", msg.NodeName)
	require.Equal(t, subscriber.Subscribed, msg.Reason)
	require.NotEmpty(t, msg.UID)

	conn := loadConnection(t, subs, msg.UID)
	require.Equal(t, "node-1", conn.Selector.NodeName)

	// Only collectors for the requested kinds are notified.
	require.Empty(t, svcChan)

	// Closing the stream from the client unsubscribes it from the collectors.
	cancel()
	msg2 := receiveMsg(t, podChan)
	require.Equal(t, subscriber.Unsubscribed, msg2.Reason)
	require.Equal(t, msg.UID, msg2.UID)

	require.Eventually(t, func() bool {
		_, ok := subs.Load(msg.UID)
		return !ok
	}, timeout, 10*time.Millisecond)
}

func TestWatchStreamsEventsSentOnConnection(t *testing.T) {
	t.Parallel()

	podChan := make(subscriber.SubsChan, 2)
	client, subs := startServer(t, map[string]subscriber.SubsChan{"Pod": podChan})

	stream, err := client.Watch(t.Context(), &Selector{
		NodeName:      "node-1",
		ResourceKinds: map[string]string{"Pod": ""},
	})
	require.NoError(t, err)

	msg := receiveMsg(t, podChan)
	conn := loadConnection(t, subs, msg.UID)

	require.NoError(t, conn.Stream.Send(&Event{Reason: "Create", Uid: "uid-1", Kind: "Pod"}))

	evt, err := stream.Recv()
	require.NoError(t, err)
	require.Equal(t, "Create", evt.GetReason())
	require.Equal(t, "uid-1", evt.GetUid())
	require.Equal(t, "Pod", evt.GetKind())
}

func TestConnectionCloseTerminatesWatch(t *testing.T) {
	t.Parallel()

	podChan := make(subscriber.SubsChan, 2)
	client, subs := startServer(t, map[string]subscriber.SubsChan{"Pod": podChan})

	stream, err := client.Watch(t.Context(), &Selector{
		NodeName:      "node-1",
		ResourceKinds: map[string]string{"Pod": ""},
	})
	require.NoError(t, err)

	msg := receiveMsg(t, podChan)
	conn := loadConnection(t, subs, msg.UID)

	// Close must be safe to call more than once.
	conn.Close(errors.New("send failed"))
	conn.Close(errors.New("send failed again"))

	_, err = stream.Recv()
	require.ErrorContains(t, err, "send failed")

	msg2 := receiveMsg(t, podChan)
	require.Equal(t, subscriber.Unsubscribed, msg2.Reason)
	require.Equal(t, msg.UID, msg2.UID)
}
