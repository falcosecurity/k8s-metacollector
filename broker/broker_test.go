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
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/falcosecurity/k8s-metacollector/metadata"
	"github.com/falcosecurity/k8s-metacollector/pkg/events"
	"github.com/falcosecurity/k8s-metacollector/pkg/fields"
	"github.com/falcosecurity/k8s-metacollector/pkg/subscriber"
)

const timeout = 5 * time.Second

// freeAddr returns a local address with a port that is free at the time of the call.
func freeAddr(t *testing.T) string {
	t.Helper()

	lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := lis.Addr().String()
	require.NoError(t, lis.Close())
	return addr
}

func watch(t *testing.T, ctx context.Context, addr, node, kind string) metadata.Metadata_WatchClient {
	t.Helper()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	stream, err := metadata.NewMetadataClient(conn).Watch(ctx, &metadata.Selector{
		NodeName:      node,
		ResourceKinds: map[string]string{kind: ""},
	}, grpc.WaitForReady(true))
	require.NoError(t, err)
	return stream
}

func recvEvent(t *testing.T, stream metadata.Metadata_WatchClient) *metadata.Event {
	t.Helper()

	type result struct {
		evt *metadata.Event
		err error
	}
	res := make(chan result, 1)
	go func() {
		evt, err := stream.Recv()
		res <- result{evt, err}
	}()

	select {
	case r := <-res:
		require.NoError(t, r.err)
		return r.evt
	case <-time.After(timeout):
		require.FailNow(t, "timed out waiting for event")
		return nil
	}
}

// subscriberUIDs reads n subscription messages and returns the subscriber UID for each node.
func subscriberUIDs(t *testing.T, ch subscriber.SubsChan, n int) map[string]string {
	t.Helper()

	uids := make(map[string]string, n)
	for range n {
		select {
		case msg := <-ch:
			require.Equal(t, subscriber.Subscribed, msg.Reason)
			uids[msg.NodeName] = msg.UID
		case <-time.After(timeout):
			require.FailNow(t, "timed out waiting for subscription")
		}
	}
	return uids
}

func TestBrokerRoutesEventsToSubscribers(t *testing.T) {
	t.Parallel()

	// The metrics are global and keyed by resource kind, so use a kind unique to this test.
	const kind = "TestBrokerRoutesEventsKind"
	collectorChan := make(subscriber.SubsChan, 4)
	queue := NewBlockingChannel(10)
	addr := freeAddr(t)

	br, err := New(logr.Discard(), queue, map[string]subscriber.SubsChan{kind: collectorChan}, WithAddress(addr))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	startErr := make(chan error, 1)
	go func() {
		startErr <- br.Start(ctx)
	}()

	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()
	stream1 := watch(t, clientCtx, addr, "node-1", kind)
	stream2 := watch(t, clientCtx, addr, "node-2", kind)
	uids := subscriberUIDs(t, collectorChan, 2)

	newEvt := func(reason, uid string, subs ...string) *events.Event {
		s := make(fields.Subscribers)
		for _, sub := range subs {
			s.Add(sub)
		}
		return &events.Event{Event: &metadata.Event{Reason: reason, Uid: uid, Kind: kind}, Subs: s}
	}

	dispatched := func(reason string) float64 {
		return testutil.ToFloat64(dispatchedEvents.WithLabelValues(kind, reason))
	}
	createBefore, updateBefore, deleteBefore := dispatched(events.Create), dispatched(events.Update), dispatched(events.Delete)

	queue.Push(newEvt(events.Create, "only-node-1", uids["node-1"]))
	queue.Push(newEvt(events.Update, "only-node-2", uids["node-2"]))
	queue.Push(newEvt(events.Delete, "both", uids["node-1"], uids["node-2"], "unknown-subscriber"))

	require.Equal(t, "only-node-1", recvEvent(t, stream1).GetUid())
	require.Equal(t, "both", recvEvent(t, stream1).GetUid())
	require.Equal(t, "only-node-2", recvEvent(t, stream2).GetUid())
	require.Equal(t, "both", recvEvent(t, stream2).GetUid())

	// Dispatched events are counted once per subscriber that received them.
	require.InDelta(t, createBefore+1, dispatched(events.Create), 0)
	require.InDelta(t, updateBefore+1, dispatched(events.Update), 0)
	require.InDelta(t, deleteBefore+2, dispatched(events.Delete), 0)

	// Disconnected subscribers are unsubscribed from the collectors.
	clientCancel()
	for range 2 {
		select {
		case msg := <-collectorChan:
			require.Equal(t, subscriber.Unsubscribed, msg.Reason)
		case <-time.After(timeout):
			require.FailNow(t, "timed out waiting for unsubscription")
		}
	}

	cancel()
	select {
	case err := <-startErr:
		require.NoError(t, err)
	case <-time.After(timeout):
		require.FailNow(t, "broker did not stop")
	}
}

func TestBrokerSetupErrors(t *testing.T) {
	const missingCert, missingKey = "/does/not/exist/tls.crt", "/does/not/exist/tls.key"

	tests := []struct {
		name string
		// opts are the options used to create the broker.
		opts []Option
		// busyAddress makes the broker bind to an address already in use.
		busyAddress bool
		wantNewErr  bool
	}{
		{
			name:       "missing TLS files",
			opts:       []Option{WithTLS(missingCert, missingKey)},
			wantNewErr: true,
		},
		{
			name:       "only TLS cert path set",
			opts:       []Option{WithTLS(missingCert, "")},
			wantNewErr: true,
		},
		{
			name:       "only TLS key path set",
			opts:       []Option{WithTLS("", missingKey)},
			wantNewErr: true,
		},
		{
			name:        "address already in use",
			busyAddress: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			opts := test.opts
			if test.busyAddress {
				lis, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
				require.NoError(t, err)
				defer lis.Close()
				opts = append(opts, WithAddress(lis.Addr().String()))
			}

			br, err := New(logr.Discard(), NewBlockingChannel(1), nil, opts...)
			if test.wantNewErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Error(t, br.Start(t.Context()))
		})
	}
}

func TestBrokerShutdownWithConnectedSubscribers(t *testing.T) {
	t.Parallel()

	const kind = "TestBrokerShutdownKind"
	collectorChan := make(subscriber.SubsChan, 4)
	addr := freeAddr(t)

	br, err := New(logr.Discard(), NewBlockingChannel(1), map[string]subscriber.SubsChan{kind: collectorChan}, WithAddress(addr))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	startErr := make(chan error, 1)
	go func() {
		startErr <- br.Start(ctx)
	}()

	// Subscribers stay connected while the broker shuts down.
	stream1 := watch(t, t.Context(), addr, "node-1", kind)
	stream2 := watch(t, t.Context(), addr, "node-2", kind)
	subscriberUIDs(t, collectorChan, 2)

	cancel()
	select {
	case err := <-startErr:
		require.NoError(t, err)
	case <-time.After(timeout):
		require.FailNow(t, "broker did not stop")
	}

	// The broker closed the streams and unsubscribed them from the collectors.
	for _, stream := range []metadata.Metadata_WatchClient{stream1, stream2} {
		_, err := stream.Recv()
		require.Error(t, err)
	}
	for range 2 {
		msg := <-collectorChan
		require.Equal(t, subscriber.Unsubscribed, msg.Reason)
	}
}
