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

package main

import (
	"context"
	"io"
	"net"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/falcosecurity/k8s-metacollector/metadata"
)

func TestLoadEvents(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		want    int
		wantErr bool
	}{
		{name: "empty array", data: `[]`, want: 0},
		{name: "valid event", data: `[{"reason":"Create","uid":"1","kind":"Pod","meta":"{}"}]`, want: 1},
		{
			name: "refs are decoded",
			data: `[{"reason":"Create","uid":"1","kind":"Pod","refs":{"resources":{"Namespace":{"list":["ns-uid"]}}}}]`,
			want: 1,
		},
		{name: "unknown field", data: `[{"reason":"Create","notAField":"x"}]`, wantErr: true},
		{name: "not an array", data: `{"reason":"Create"}`, wantErr: true},
		{name: "wrong field type", data: `[{"reason":1}]`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			evts, err := loadEvents([]byte(test.data))
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, evts, test.want)
		})
	}
}

// TestServerReplaysTestFile checks that test.json matches metadata.proto and that a client
// receives all its events, in order, before the stream ends.
func TestServerReplaysTestFile(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("test.json")
	require.NoError(t, err)
	evts, err := loadEvents(data)
	require.NoError(t, err)
	require.NotEmpty(t, evts)
	for i, evt := range evts {
		require.Contains(t, []string{"Create", "Update", "Delete"}, evt.GetReason(), "event %d", i)
		require.NotEmpty(t, evt.GetUid(), "event %d", i)
		require.NotEmpty(t, evt.GetKind(), "event %d", i)
	}

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	metadata.RegisterMetadataServer(srv, &server{events: evts})
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

	stream, err := metadata.NewMetadataClient(conn).Watch(t.Context(), &metadata.Selector{NodeName: "node"})
	require.NoError(t, err)

	for i, want := range evts {
		got, err := stream.Recv()
		require.NoError(t, err)
		require.True(t, proto.Equal(want, got), "event %d differs", i)
	}
	_, err = stream.Recv()
	require.ErrorIs(t, err, io.EOF, "expected end of stream")
}
